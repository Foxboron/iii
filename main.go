package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
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

type Server struct {
	server     string
	conn       net.Conn
	port       string
	nick       string
	realName   string
	password   string
	channels   map[string]bool
	msgChan    chan Msg
	serverChan chan string
	tls        bool
	Dir        string
}

type Parsed struct {
	nick     string
	userinfo string
	event    string
	channel  string
	raw      string
	args     []string
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

func (server *Server) writeOutLog(channel string, text Parsed) {
	createFiles(server.Dir + "/" + channel)
	f, _ := os.OpenFile(server.Dir+"/"+channel+"/out", os.O_RDWR|os.O_APPEND, 0660)
	defer f.Close()

	t := time.Now()
	currTime := fmt.Sprintf("%s", t.Format("2006-01-02 15:04:05"))

	msg := ""
	if text.event == "PRIVMSG" {
		msg = fmt.Sprintf("%s <%s> %s", currTime, text.nick, text.args[1])
	} else if text.event == "JOIN" {
		msg = fmt.Sprintf("%s -!- %s(~%s) has joined %s", currTime, text.nick, text.userinfo, text.channel)
	} else if text.event == "PART" {
		msg = fmt.Sprintf("%s -!- %s(~%s) has left %s", currTime, text.nick, text.userinfo, text.channel)
	} else if text.event == "QUIT" {
		msg = fmt.Sprintf("%s -!- %s(~%s) has quit", currTime, text.nick, text.userinfo)
	} else if text.event == "MODE" {
		msg = fmt.Sprintf("%s -!- %s changed mode/%s -> %s", currTime, text.nick, text.channel, text.args[1])
	} else if text.event == "NOTICE" {
		msg = fmt.Sprintf("%s -!- NOTICE %s", currTime, text.args[1])
	} else if text.event == "KICK" {
		msg = fmt.Sprintf("%s -!- %s kicked %s (\"%s\")", currTime, text.nick, text.args[1], text.args[2])
	} else if text.event == "TOPIC" {
		msg = fmt.Sprintf("%s -!- %s changed topic to \"%s\"", currTime, text.nick, text.args[1])
	}
	if msg != "" {
		_, _ = f.WriteString(msg + "\n")
	}
}

func (server *Server) Write(msg string) {
	server.conn.Write([]byte(msg + "\n"))
}

func (server *Server) Writef(msg string, arg ...interface{}) {
	server.Write(fmt.Sprintf(msg, arg...))
}

func (server *Server) listenFile(channel string) {
	channel = strings.ToLower(channel)
	filePath := server.Dir + "/" + channel
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
			server.msgChan <- Msg{channel: channel, msg: string(bytes), file: filePath}
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (server *Server) listenServer() {
	if server.password != "" {
		server.Writef("PASS %s", server.password)
	}
	server.Writef("USER %s 0 * :%s", server.nick, server.realName)
	server.Writef("NICK %s", server.nick)
	buffer := bufio.NewScanner(server.conn)
	for {
		for buffer.Scan() {
			server.serverChan <- buffer.Text()
			if strings.Split(buffer.Text(), " :")[0] == "ERROR" {
				return
			}
		}
	}
}

func (server *Server) createServer() {
	var tlsConn net.Conn
	var err error
	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%s", server.server, server.port))
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
	if server.tls {
		tlsConn = tls.Client(conn, &tls.Config{
			InsecureSkipVerify: true,
		})
	}
	server.conn = tlsConn
}

func (server *Server) handleMsg(msg Msg) {
	events := strings.SplitN(msg.msg, " ", 3)
	// Events
	if "/j" == events[0] {
		_, ok := server.channels[events[1]]
		if ok == false {
			server.Writef("JOIN :%s", events[1])
			go server.listenFile(events[1])
			server.channels[events[1]] = true
			if len(events) > 2 {
				server.Writef("PRIVMSG %s :%s", events[1], events[2])
			}
		}
	} else if "/a" == events[0] {
		server.Writef("AWAY :%s", strings.Join(events[1:], " "))
	} else if "/n" == events[0] {
		server.Writef("NICK %s", events[1])
	} else if "/t" == events[0] {
		server.Writef("TOPIC %s :%s", events[1], strings.Join(events[2:], " "))
	} else if "/l" == events[0] {
		server.Writef("PART %s", events[1])
		delete(server.channels, events[1])
	} else {
		server.Writef("PRIVMSG %s :%s", msg.channel, msg.msg)
	}
}

func (server *Server) rejoinChannels() {
	for channel, _ := range server.channels {
		server.Writef("JOIN :%s", channel)
	}
}

func (server *Server) handleServer(s string) {
	msg := parse(s)
	if msg.event == "ERROR" {
		server.createServer()
		go server.listenServer()
		return
	}
	if msg.event == "266" {
		// Rejoin channels
		server.rejoinChannels()
		return
	}
	if msg.event == "PING" {
		server.Writef("PONG %s", msg.args[0])
		return
	}
	if len(msg.nick) == 0 && msg.channel == server.nick || msg.channel == "*" || msg.event == "QUIT" {
		server.writeOutLog("", msg)
		return
	}
	var channel string
	if msg.channel == server.nick {
		channel = strings.ToLower(msg.nick)
	} else {
		channel = strings.ToLower(msg.channel)
	}
	// Check if we have a thread on the channel
	// Create if there isnt
	_, ok := server.channels[channel]
	if ok == false {
		go server.listenFile(channel)
		server.channels[channel] = true
	}
	server.writeOutLog(channel, msg)
}

func (server *Server) Run() {
	go server.listenServer()
	go server.listenFile("")
	ticker := time.NewTicker(1 * time.Minute)
	for {
		select {
		case <-ticker.C:
			server.Writef("PING %d", time.Now().UnixNano())
		case s := <-server.serverChan:
			server.handleServer(s)
		case s := <-server.msgChan:
			server.handleMsg(s)
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

	serverRun := Server{
		server:     *server,
		port:       *port,
		nick:       *nick,
		realName:   *realName,
		password:   os.Getenv(*pass),
		channels:   map[string]bool{},
		msgChan:    make(chan Msg),
		serverChan: make(chan string),
		tls:        *tls,
		Dir:        *path + "/" + *server}
	serverRun.createServer()
	serverRun.Run()
}
