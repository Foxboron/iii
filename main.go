package main

import (
	"bufio"
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"
)

type Channel struct {
	channel string
	msg     string
}

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
		"user":    user,
		"msg":     splitted[len(splitted)-1],
		"event":   event,
		"channel": channel,
		"raw":     msg,
	}
	return info
}

const IRCDir = "./irc"
const SSL = true

func createChannel(channel string) {
	err := os.MkdirAll(IRCDir+"/"+channel, 0744)
	if err != nil {
		log.Fatal(err)
	}

	f, _ := os.OpenFile(IRCDir+"/"+channel+"/out", os.O_CREATE, 0660)
	defer f.Close()
	err = syscall.Mkfifo(IRCDir+"/"+channel+"/in", 0700)
	if err != nil {
		log.Print(err)
	}
}

func writeOutLog(channel string, text string) {
	f, _ := os.OpenFile(IRCDir+"/"+channel+"/out", os.O_RDWR|os.O_APPEND, 0660)
	_, _ = f.WriteString(text + "\n")
	f.Close()
}

func listenChannel(channel string, c chan Channel) {
	go func(channel string) {
		file, err := os.OpenFile(IRCDir+"/"+channel+"/in", os.O_CREATE|syscall.O_RDONLY|syscall.O_NONBLOCK, os.ModeNamedPipe)
		fmt.Println(channel)
		defer file.Close()
		if err != nil {
			log.Print(err)

		}
		buffer := bufio.NewReader(file)
		for {
			bytes, _, _ := buffer.ReadLine()
			if len(bytes) != 0 {
				msg := string(bytes)
				c <- Channel{channel: channel, msg: msg}
			}
		}
	}(channel)
}

func listenFile(channel string, c chan string) {
	go func(channel string) {
		file, err := os.OpenFile(channel, os.O_CREATE|syscall.O_RDONLY|syscall.O_NONBLOCK, os.ModeNamedPipe)
		defer file.Close()
		if err != nil {
			log.Print(err)

		}
		buffer := bufio.NewReader(file)
		for {
			bytes, _, _ := buffer.ReadLine()
			if len(bytes) != 0 {
				msg := string(bytes)
				c <- msg
			}
		}
	}(channel)
}

func listenServer(conn *tls.Conn, c chan string) {
	go func() {

		user_msg := fmt.Sprintf("USER %s %s %s :Go FTW", nick, nick, nick)
		conn.Write([]byte(user_msg + "\n"))

		nick_msg := fmt.Sprintf("NICK %s", nick)
		conn.Write([]byte(nick_msg + "\n"))

		buffer := bufio.NewReader(conn)
		for {
			bytes, _, _ := buffer.ReadLine()
			if len(bytes) != 0 {
				c <- string(bytes)
			}
		}
	}()
}

func createServer() *tls.Conn {
	conf := &tls.Config{
		InsecureSkipVerify: true,
	}

	conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%s", IRCServer, IRCPort), conf)
	if err != nil {
		println("Dial failed:", err.Error())
		os.Exit(1)
	}
	return conn
}

func main() {
	err := os.MkdirAll(IRCDir, 0744)
	if err != nil {
		log.Fatal(err)
	}
	_, _ = os.OpenFile(IRCDir+"/out", os.O_CREATE, 0600)
	err = syscall.Mkfifo(IRCDir+"/in", 0700)
	if err != nil {
		log.Print(err)
	}

	conn := createServer()

	serverConnection := make(chan string)
	go func() { listenServer(conn, serverConnection) }()

	server := make(chan string)
	go func() { listenFile(IRCDir+"/in", server) }()

	channels := make(chan Channel)

	for {
		select {
		case s := <-serverConnection:
			msg := parse(s)
			fmt.Println(s)
			if msg["event"] == "PING" {
				fmt.Println(msg["msg"])
				conn.Write([]byte(fmt.Sprintf("PONG :%s", msg["msg"]) + "\n"))
			}
			if len(msg["channel"]) != 0 {
				if string(msg["channel"][0]) == string("#") {
					fmt.Println("Wrote log")
					writeOutLog(msg["channel"], msg["raw"])
				}
			}
			fmt.Println(msg["channel"])
		case s := <-server:
			events := strings.Split(s, " ")

			// Events
			if "/j" == events[0] {
				conn.Write([]byte(fmt.Sprintf("JOIN :%s", events[1]) + "\n"))
				createChannel(events[1])
				go func() { listenChannel(events[1], channels) }()
			}

		case s := <-channels:
			conn.Write([]byte(fmt.Sprintf(s.msg) + "\n"))
		}
	}
}
