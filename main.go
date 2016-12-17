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
	s       string
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
var serverChan = make(chan Parsed) // output from server
var msgChan = make(chan Msg)       // user input
var done = make(chan struct{})

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

func hasQuit() bool {
	select {
	case <-done:
		return true
	default:
		return false
	}
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

// listenFile continuously scans for user input to channel,
// marshals it and sends it on msgChan.
func listenFile(channel string) {
	filePath := ircPath + "/"

	if channel != "" {
		filePath += strings.ToLower(channel) + "/"
	}

	createFiles(filePath)
	file, err := os.Open(filePath + "in")
	if err != nil {
		log.Print("Tried listening on channel:", channel, err)
		return
	}
	defer file.Close()

	in := bufio.NewScanner(file)
	for in.Scan() {
		if hasQuit() || !chanCreated[channel] {
			break
		}
		msgChan <- Msg{channel: channel, s: in.Text(), file: filePath}
	}
}

// listenServer scans for server messages on conn and sends
// them on serverChan.
func listenServer(conn net.Conn) {
	in := bufio.NewScanner(conn)
	for in.Scan() {
		serverChan <- parse(in.Text())
		if strings.HasPrefix(in.Text(), "ERROR") {
			break
		}
	}

	// tell all listening goroutines to exit
	close(done)
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

// Send formats the receiver’s contents as an IRC
// message and writes it to conn.
func (m Msg) Send(conn net.Conn) {
	if m.s[0] != '/' {
		mustWritef(conn, "PRIVMSG %s :%s", m.channel, m.s)
		return
	}

	args := strings.SplitN(m.s, " ", 3)
	switch args[0] {
	case "/a":
		mustWritef(conn, "AWAY :%s", strings.Join(args[1:], " "))
	case "/j":
		if args[1] != "" && !chanCreated[args[1]] {
			// FIXME: handle key field
			mustWritef(conn, "JOIN %s", args[1])
			go listenFile(args[1])
			chanCreated[args[1]] = true
		}
	case "/l":
		if m.channel != "" {
			mustWritef(conn, "PART %s", m.channel)
			delete(chanCreated, m.channel)
		}
	case "/n":
		mustWritef(conn, "NICK %s", args[1])
	case "/t":
		mustWritef(conn, "TOPIC %s :%s", args[1],
			strings.Join(args[2:], " "))
	default: // raw command
		mustWriteln(conn, m.s)
	}
}

func rejoinAll(conn net.Conn) {
	for c := range chanCreated {
		mustWritef(conn, "JOIN :%s", c)
	}
}

func handleServer(conn net.Conn, p Parsed) {
	switch p.event {
	case "266":
		rejoinAll(conn)
	case "QUIT":
		if p.nick == "" && p.channel == clientNick || p.channel == "*" {
			writeOutLog("", p)
		}
	case "PING":
		mustWritef(conn, "PONG %s", p.args[0])
	default:
		var c string
		if p.channel == clientNick {
			c = strings.ToLower(p.nick)
		} else {
			c = strings.ToLower(p.channel)
		}

		// create files and listening goroutine if needed
		if !chanCreated[c] {
			chanCreated[c] = true
			go listenFile(c)
		}
		writeOutLog(c, p)
	}
}

func login(conn net.Conn, server, pass, name string) {
	if pass != "" {
		mustWritef(conn, "PASS %s", pass)
	}
	mustWritef(conn, "NICK %s", clientNick)
	mustWritef(conn, "USER %s localhost %s :%s", clientNick, server, name)
}

func run(conn net.Conn, server string) {
	go listenServer(conn)
	go listenFile("") // server input
	ticker := time.NewTicker(1 * time.Minute)
loop:
	for {
		select {
		case <-done:
			for range msgChan {
				// drain remaining
			}
			break loop
		case <-ticker.C: // FIXME: ping timeout check
			mustWritef(conn, "PING %s", server)
		case s := <-serverChan:
			handleServer(conn, s)
		case m := <-msgChan:
			m.Send(conn)
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
	defer conn.Close()
	login(conn, *server, password, *realName)
	run(conn, *server)
}
