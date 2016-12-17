package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strings"
	"syscall"
	"time"
)

type Msg struct {
	channel string
	file    string
	msg     string
}

type Parsed struct {
	nick     string
	userinfo string
	event    string
	channel  string
	raw      string
	args     []string
}

var chanCreated = make(map[string]bool)
var clientNick, ircPath string
var serverChan = make(chan string) // raw output from server
var msgChan = make(chan Msg)       // user input

func mustWriteln(w io.Writer, s string) {
	if _, err := fmt.Fprint(w, s+"\r\n"); err != nil {
		log.Fatal(err)
	}
}

func mustWritef(w io.Writer, form string, args ...interface{}) {
	mustWriteln(w, fmt.Sprintf(form, args...))
}

// Thanks Twisted
func parse(s string) Parsed {
	raw := s
	var prefix string
	var command string
	var args []string
	var trailing []string
	var nick string
	var userinfo string

	if string(s[0]) == ":" {
		ret := strings.SplitN(s[1:], " ", 2)
		prefix = ret[0]
		s = ret[1]
	}
	if strings.Index(s, " :") != -1 {
		ret := strings.SplitN(s, " :", 2)
		s = ret[0]
		trailing = ret[1:]

		args = strings.Split(s, " ")
		args = append(args, trailing...)
	} else {
		args = strings.Split(s, " ")
	}
	command = args[0]
	args = args[1:]

	prefixSplit := strings.Split(prefix, "!")
	if len(prefixSplit) == 1 {
		nick = ""
		userinfo = ""
	} else {
		nick = prefixSplit[0]
		userinfo = prefixSplit[1]
	}

	return Parsed{nick: nick,
		channel:  args[0],
		userinfo: userinfo,
		event:    command,
		raw:      raw,
		args:     args}
}

// Creates the fifo files and directories
func createFiles(directory string) bool {
	if _, err := os.Stat(directory); err == nil {
		return false
	}
	err := os.MkdirAll(directory, 0744)
	if err != nil {
		log.Print("Tried making directory:", directory, err)
	}
	f, err := os.OpenFile(directory+"/out", os.O_CREATE, 0660)
	defer f.Close()
	if err != nil {
		log.Print("Tried opening out file for directory:", directory, err)
	}
	err = syscall.Mkfifo(directory+"/in", 0700)
	if err != nil {
		log.Print("Tried creating fifo file for directory:", directory, err)
	}
	return true
}

func writeOutLog(channel string, text Parsed) {
	msg := ""
	if text.event == "PRIVMSG" {
		msg = fmt.Sprintf("<%s> %s", text.nick, text.args[1])
	} else if text.event == "JOIN" {
		msg = fmt.Sprintf("-!- %s(~%s) has joined %s", text.nick, text.userinfo, text.channel)
	} else if text.event == "PART" {
		msg = fmt.Sprintf("-!- %s(~%s) has left %s", text.nick, text.userinfo, text.channel)
	} else if text.event == "QUIT" {
		msg = fmt.Sprintf("-!- %s(~%s) has quit", text.nick, text.userinfo)
	} else if text.event == "MODE" {
		msg = fmt.Sprintf("-!- %s changed mode/%s -> %s", text.nick, text.channel, text.args[1])
	} else if text.event == "NOTICE" {
		msg = fmt.Sprintf("-!- NOTICE %s", text.args[1])
	} else if text.event == "KICK" {
		msg = fmt.Sprintf("-!- %s kicked %s (\"%s\")", text.nick, text.args[1], text.args[2])
	} else if text.event == "TOPIC" {
		msg = fmt.Sprintf("-!- %s changed topic to \"%s\"", text.nick, text.args[1])
	}
	writeChannel(channel, msg)
}

func writeChannel(channel string, msg string) {
	if msg != "" {
		createFiles(ircPath + "/" + channel)
		f, _ := os.OpenFile(ircPath+"/"+channel+"/out", os.O_RDWR|os.O_APPEND, 0660)
		defer f.Close()
		t := time.Now()
		f.WriteString(fmt.Sprintf("%s %s\n", t.Format("2006-01-02 15:04:05"), msg))
	}
}

func listenFile(channel string) {
	channel = strings.ToLower(channel)
	filePath := ircPath + "/" + channel
	if channel != "" {
		filePath = filePath + "/"
	}

	createFiles(filePath)
	file, err := os.OpenFile(filePath+"in", os.O_CREATE|syscall.O_RDONLY|syscall.O_NONBLOCK, os.ModeNamedPipe)
	defer file.Close()
	if err != nil {
		log.Print("Tried listening on channel:", channel, err)
	}
	buffer := bufio.NewReader(file)
	for {
		bytes, _, _ := buffer.ReadLine()
		if len(bytes) != 0 {
			msgChan <- Msg{channel: channel, msg: string(bytes), file: filePath}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func listenServer(conn net.Conn, server, pass, realName string) {
	if pass != "" {
		fmt.Fprintf(conn, "PASS %s", pass)
	}
	fmt.Fprintf(conn, "USER %s 0 * :%s", clientNick, realName)
	fmt.Fprintf(conn, "NICK %s", clientNick)
	buffer := bufio.NewScanner(conn)
	for {
		for buffer.Scan() {
			serverChan <- buffer.Text()
			if strings.Split(buffer.Text(), " :")[0] == "ERROR" {
				return
			}
		}
	}
}

func connServer(server, port string, useTLS bool) net.Conn {
	var err error
	tcpAddr, err := net.ResolveTCPAddr("tcp", server+":"+port)
	if err != nil {
		log.Fatal("ResolveTCPAddr failed:", err.Error())
		os.Exit(1)
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Fatal("Connection blew up:", err)
		os.Exit(1)
	}
	err = conn.SetKeepAlive(true)
	if err != nil {
		log.Print("Could not set keep alive:", err)
	}
	if useTLS {
		return tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
		})
	}

	return conn
}

func handleMsg(conn net.Conn, msg Msg) {
	events := strings.SplitN(msg.msg, " ", 3)
	// Events
	if "/j" == events[0] && !chanCreated[events[1]] {
		mustWritef(conn, "JOIN :%s", events[1])
		go listenFile(events[1])
		chanCreated[events[1]] = true
		if len(events) > 2 {
			mustWritef(conn, "PRIVMSG %s :%s", events[1], events[2])
		}
	} else if "/a" == events[0] {
		mustWritef(conn, "AWAY :%s", strings.Join(events[1:], " "))
	} else if "/n" == events[0] {
		mustWritef(conn, "NICK %s", events[1])
	} else if "/t" == events[0] {
		mustWritef(conn, "TOPIC %s :%s", events[1], strings.Join(events[2:], " "))
	} else if "/l" == events[0] {
		mustWritef(conn, "PART %s", events[1])
		delete(chanCreated, events[1])
	} else {
		mustWritef(conn, "PRIVMSG %s :%s", msg.channel, msg.msg)
		s := fmt.Sprintf("<%s> %s", clientNick, msg.msg)
		writeChannel(msg.channel, s)
	}
}

func rejoinAll(conn net.Conn) {
	for c := range chanCreated {
		mustWritef(conn, "JOIN :%s", c)
	}
}

func handleServer(conn net.Conn, s string) {
	msg := parse(s)
	fmt.Println(s)
	/*	if msg.event == "ERROR" {
	 *		server.createServer()
	 *		go server.listenServer()
	 *		return
	 *	}
	 */
	if msg.event == "266" {
		rejoinAll(conn)
		return
	}
	if msg.event == "PING" {
		mustWritef(conn, "PONG %s", msg.args[0])
		return
	}
	if len(msg.nick) == 0 && msg.channel == clientNick || msg.channel == "*" || msg.event == "QUIT" {
		writeOutLog("", msg)
		return
	}
	var channel string
	if msg.channel == clientNick {
		channel = strings.ToLower(msg.nick)
	} else {
		channel = strings.ToLower(msg.channel)
	}
	// Check if we have a thread on the channel
	// Create if there isnt
	if !chanCreated[channel] {
		go listenFile(channel)
		chanCreated[channel] = true
	}
	writeOutLog(channel, msg)
}

func run(conn net.Conn, server, pass, realName string) {
	go listenServer(conn, server, pass, realName)
	go listenFile("")
	ticker := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-ticker.C:
			fmt.Fprintf(conn, "PING %d\r\n", time.Now().UnixNano())
		case s := <-serverChan:
			handleServer(conn, s)
		case m := <-msgChan:
			handleMsg(conn, m)
		}
	}
}

func main() {
	server := flag.String("s", "irc.freenode.net", "Specify server")
	port := flag.String("p", "", "Server port (default 6667, TLS default 6697)")
	tls := flag.Bool("tls", false, "Use TLS for the connection (default false)")
	pass := flag.String("k", "IIPASS", "Specify a environment variable for your IRC password")
	path := flag.String("i", "", "Specify a path for the IRC connection (default ~/irc)")
	nick := flag.String("n", "iii", "Speciy a default nick")
	realName := flag.String("f", "ii Improved", "Speciy a default real name")
	flag.Parse()

	if *port == "" {
		if *tls {
			*port = "6697"
		} else {
			*port = "6667"
		}
	}

	if *path == "" {
		usr, err := user.Current()
		if err != nil {
			log.Fatal("Could not get home directory", err)
		}
		*path = usr.HomeDir + "/irc"
	}

	password := os.Getenv(*pass)
	ircPath = *path + "/" + *server
	clientNick = *nick

	conn := connServer(*server, *port, *tls)
	run(conn, *server, password, *realName)
}
