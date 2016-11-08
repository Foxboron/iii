package main

import (
	"bufio"
	"crypto/tls"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"syscall"
	"time"
)

type Msg struct {
	channel string
	file    string
	msg     string
}
type ServerInterface interface {
	listenFile()
}
type Server struct {
	server     string
	conn       net.Conn
	port       string
	nick       string
	realName   string
	channels   map[string]bool
	msgChan    chan Msg
	serverChan chan string
	ssl        bool
	Dir        string
}

// TODO: Rewrite. This is horrible code
func parse(msg string) map[string]string {
	splitted := strings.SplitN(msg, " :", 3)
	userinfo := strings.Split(splitted[0], " ")
	event := ""
	channel := ""
	user := ""

	if len(userinfo) > 1 {
		event = userinfo[1]
		if len(userinfo) >= 3 {
			channel = userinfo[2]
			user = strings.Trim(strings.Split(userinfo[0], "!")[0], ":")
		}
	} else {
		event = splitted[0]
	}

	info := map[string]string{
		"user":    strings.ToLower(user),
		"msg":     splitted[len(splitted)-1],
		"event":   event,
		"channel": strings.ToLower(channel),
		"raw":     msg,
	}
	return info
}

const IRCDir = "./irc"
const SSL = true

// Creates the fifo files and directories
func createFiles(directory string) bool {
	if _, err := os.Stat(directory); err == nil {
		return false
	}

	err := os.MkdirAll(directory, 0744)
	if err != nil {
		log.Print(err)
	}

	f, err := os.OpenFile(directory+"/out", os.O_CREATE, 0660)
	defer f.Close()
	if err != nil {
		log.Print(err)
	}

	err = syscall.Mkfifo(directory+"/in", 0700)
	if err != nil {
		log.Print(err)
	}
	return true
}

func (server *Server) writeOutLog(channel string, text string) {
	createFiles(server.Dir + "/" + channel)
	f, _ := os.OpenFile(server.Dir+"/"+channel+"/out", os.O_RDWR|os.O_APPEND, 0660)
	_, _ = f.WriteString(text + "\n")
	f.Close()
}

func (server *Server) listenFile(channel string) {
	channel = strings.ToLower(channel)
	filePath := server.Dir + "/" + channel
	if channel != "" {
		filePath = filePath + "/"
	}

	createFiles(filePath)

	go func(channel string, filePath string) {
		file, err := os.OpenFile(filePath+"in", os.O_CREATE|syscall.O_RDONLY|syscall.O_NONBLOCK, os.ModeNamedPipe)
		defer file.Close()
		if err != nil {
			log.Print(err)

		}
		buffer := bufio.NewReader(file)
		for {
			bytes, _, _ := buffer.ReadLine()
			if len(bytes) != 0 {
				server.msgChan <- Msg{channel: channel, msg: string(bytes), file: filePath}
			}
			time.Sleep(10 * time.Millisecond)
		}
	}(channel, filePath)
}

func (server *Server) listenServer() {
	go func() {
		user_msg := fmt.Sprintf("USER %s %s %s :Go FTW", server.nick, server.nick, server.nick)
		server.conn.Write([]byte(user_msg + "\n"))

		nick_msg := fmt.Sprintf("NICK %s", nick)
		server.conn.Write([]byte(nick_msg + "\n"))

		buffer := bufio.NewScanner(server.conn)
		for {
			for buffer.Scan() {
				server.serverChan <- buffer.Text()
			}
		}
	}()
}

func (server *Server) createServer() {
	var conn net.Conn
	var err error
	if server.ssl {
		conf := &tls.Config{
			InsecureSkipVerify: true,
		}
		conn, err = tls.Dial("tcp", fmt.Sprintf("%s:%s", server.server, server.port), conf)
	} else {
		conn, err = net.Dial("tcp", fmt.Sprintf("%s:%s", server.server, server.port))
	}

	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}
	server.conn = conn
}

func (server *Server) handleMsg(msg Msg) {
	events := strings.SplitN(msg.msg, " ", 3)
	// Events
	if "/j" == events[0] {
		_, ok := server.channels[events[1]]
		if ok == false {
			server.conn.Write([]byte(fmt.Sprintf("JOIN :%s", events[1]) + "\n"))
			go server.listenFile(events[1])
			server.channels[events[1]] = true
		}

	} else if "/m" == events[0] {
		server.conn.Write([]byte(fmt.Sprintf("PRIVMSG %s :%s", events[1], events[2]) + "\n"))
	} else {
		server.conn.Write([]byte(fmt.Sprintf("PRIVMSG %s :%s", msg.channel, msg.msg) + "\n"))
	}
}

func (server *Server) handleServer(s string) {
	msg := parse(s)

	if msg["event"] == "PING" {
		server.conn.Write([]byte(fmt.Sprintf("PONG :%s", msg["msg"]) + "\n"))
	}
	if len(msg["channel"]) != 0 && msg["event"] == "PRIVMSG" {
		var channel string
		if msg["channel"] == strings.ToLower(nick) {
			channel = msg["user"]
		} else {
			channel = msg["channel"]
		}
		_, ok := server.channels[channel]
		if ok == false {
			go server.listenFile(channel)
			server.channels[channel] = true
		}
		server.writeOutLog(channel, msg["raw"])
	} else {
		server.writeOutLog("", msg["raw"])
	}
}

func (server *Server) Run() {
	go server.listenServer()
	go server.listenFile("")

	for {
		select {
		case s := <-server.serverChan:
			server.handleServer(s)
		case s := <-server.msgChan:
			server.handleMsg(s)
		}
	}
}

func start() {

}
func main() {
	server := flag.String("s", "irc.freenode.net", "Specify server")
	port := flag.String("p", "", "Server port (default 6667, SSL default 6697)")
	ssl := flag.Bool("tls", false, "Use TLS for the connection (default false)")
	_ = flag.String("k", "IIPASS", "Specify a environment variable for your IRC password")
	path := flag.String("i", "~/irc", "Specify a path for the IRC connection")
	nick := flag.String("n", "iii", "Speciy a default nick")
	realName := flag.String("f", "ii Improved", "Speciy a default real name")

	flag.Parse()

	serverRun := Server{
		server:     *server,
		port:       *port,
		nick:       *nick,
		realName:   *realName,
		channels:   map[string]bool{},
		msgChan:    make(chan Msg),
		serverChan: make(chan string),
		ssl:        *ssl,
		Dir:        *path + "/" + *server}
	serverRun.createServer()
	serverRun.Run()
}
