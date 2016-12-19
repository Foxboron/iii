package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/user"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/unix"
)

type Msg struct {
	channel string // channel destination
	s       string // user input
}

type Parsed struct {
	nick    string   // source nick (RFC 1459 <servername>)
	uinf    string   // user info
	cmd     string   // IRC command
	channel string   // normalized channel name
	raw     string   // raw message
	args    []string // parsed message parameters
}

// commands to be logged to server output
var glMsgs = [...]string{"ERROR", "NICK", "QUIT"}
var globalCmds = make(map[string]struct{})

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

func isNumeric(s string) bool {
	if _, err := strconv.Atoi(s); err == nil {
		return true
	}
	return false
}

// parse returns a filled Parsed structure representing its input.
func parse(input string) (Parsed, error) {
	var p Parsed
	p.raw = string(input)

	// step over leading :
	if input[0] == ':' {
		input = input[1:]
	}

	// split on spaces, unless a trailing param is found
	splf := func(data []byte, atEOF bool) (advance int, token []byte,
		err error) {
		if data[0] == ':' && len(data) > 1 { // trailing
			return 0, data[1:], bufio.ErrFinalToken
		}

		if !bytes.ContainsRune(data, ' ') {
			return 0, data, bufio.ErrFinalToken
		}

		i := 0
		for ; i < len(data); i++ {
			if data[i] == ' ' {
				break
			}
		}
		return i + 1, data[:i], nil
	}
	in := bufio.NewScanner(strings.NewReader(input))
	in.Split(splf)

	// prefix
	if ok := in.Scan(); !ok {
		return p, fmt.Errorf("expected prefix")
	}
	if strings.Contains(in.Text(), "!") { // userinfo included
		pref := strings.Split(in.Text(), "!")
		p.nick = pref[0]
		p.uinf = pref[1]
	} else {
		p.nick = in.Text()
	}

	// command
	if ok := in.Scan(); !ok {
		return p, fmt.Errorf("expected command")
	}
	p.cmd = in.Text()

	// params
	for i := 0; in.Scan(); i++ {
		p.args = append(p.args, in.Text())
	}

	// set channel of normal messages. numeric (server) replies and
	// non-channel-specific commands will have .channel = ""
	if _, ok := globalCmds[p.cmd]; !ok && !isNumeric(p.cmd) {
		p.channel = strings.ToLower(p.args[0])
	}
	return p, nil
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
func createFiles(dir string) error {
	fi, err := os.Stat(dir)
	if err == nil && fi.Mode().IsDir() {
		return nil // already created
	}

	if err = os.MkdirAll(dir, 0744); err != nil {
		return err
	}
	f, err := os.OpenFile(dir+"/out", os.O_CREATE, 0660)
	if err != nil {
		return err
	}
	defer f.Close()
	if err = unix.Mkfifo(dir+"/in", 0700); err != nil {
		return err
	}
	return nil
}

// Log pretty prints the receiver’s contents to
// the appropriate channel out.
func (p Parsed) Log() {
	var s string

	switch p.cmd {
	case "ERROR":
		s = fmt.Sprintf("-!- ERROR: %s", p.args[0])
	case "JOIN":
		s = fmt.Sprintf("-!- %s (%s) has joined %s", p.nick,
			p.uinf, p.channel)
	case "KICK":
		var t string
		if len(p.args) > 2 { // comment included
			t = p.args[2]
		}
		s = fmt.Sprintf("-!- %s kicked %s from %s (\"%s\")", p.nick,
			p.args[1], p.args[0], t)
	case "MODE":
		s = fmt.Sprintf("-!- %s changed mode/%s -> %s", p.nick,
			p.args[0], p.args[1])
	case "NICK":
		s = fmt.Sprintf("-!- %s changed nick to %s", p.nick,
			p.args[0])
	case "NOTICE":
		s = fmt.Sprintf("-!- NOTICE: %s", p.args[1])
	case "QUIT":
		s = fmt.Sprintf("-!- %s (%s) has quit (%s)", p.nick,
			p.uinf, p.args[1])
	case "PART":
		s = fmt.Sprintf("-!- %s (%s) has left %s", p.nick, p.uinf,
			p.args[0])
	case "PRIVMSG":
		s = fmt.Sprintf("<%s> %s", p.nick, p.args[1])
	case "TOPIC":
		var t string
		if len(p.args) > 1 { // new topic
			t = p.args[1]
		}
		s = fmt.Sprintf("-!- %s changed the topic to \"%s\"", p.nick, t)
	default: // server commands, etc.
		s = strings.Join(p.args[1:], " ")
	}

	if s != "" {
		if err := writeChannel(p.channel, s); err != nil {
			log.Print(err)
		}
	}
}

func writeChannel(channel string, msg string) error {
	createFiles(ircPath + "/" + channel)
	f, err := os.OpenFile(ircPath+"/"+channel+"/out", os.O_WRONLY|os.O_APPEND,
		0660)
	if err != nil {
		return err
	}
	defer f.Close()
	t := time.Now()
	f.WriteString(fmt.Sprintf("%s %s\n", t.Format("2006-01-02 15:04:05"), msg))
	return nil
}

// listenFile continuously scans for user input to channel,
// marshals it and sends it on msgChan.
func listenFile(channel string) {
	filePath := ircPath + "/"

	if channel != "" {
		filePath += strings.ToLower(channel) + "/"
	}

	if err := createFiles(filePath); err != nil {
		log.Print(err)
		return
	}
	chanCreated[channel] = true

	// need O_RDWR to avoid blocking open
	file, err := os.OpenFile(filePath+"in", os.O_RDWR, 0700)
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
		msgChan <- Msg{channel: channel, s: in.Text()}
	}
}

// listenServer scans for server messages on conn and sends
// them on serverChan.
func listenServer(conn net.Conn) {
	in := bufio.NewScanner(conn)
	for in.Scan() {
		if p, err := parse(in.Text()); err != nil {
			log.Print("parse error:", err)
		} else {
			serverChan <- p
		}
	}

	// tell all listening goroutines to exit
	close(done)
}

func connServer(server, port string, useTLS bool) net.Conn {
	tcpAddr, err := net.ResolveTCPAddr("tcp", server+":"+port)
	if err != nil {
		log.Fatal(err)
	}
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		log.Fatal(err)
	}
	err = conn.SetKeepAlive(true)
	if err != nil {
		log.Print(err)
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

func handleServer(conn net.Conn, p Parsed) {
	switch p.cmd {
	case "PING":
		mustWritef(conn, "PONG %s", p.args[0])
	case "PONG":
		break
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
		p.Log()
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
	nick := flag.String("n", "", "IRC nick ($USER)")
	pass := flag.String("k", "", "Read password from variable (e.g. IIPASS)")
	path := flag.String("i", "", "IRC path (~/irc)")
	port := flag.String("p", "", "Server port (6667/TLS: 6697)")
	realName := flag.String("f", "", "Real name (nick)")
	server := flag.String("s", "", "Server to connect to")
	tls := flag.Bool("t", false, "Use TLS")
	flag.Parse()

	// initialize set of channel-less commands
	for _, s := range glMsgs {
		globalCmds[s] = struct{}{}
	}

	if *port == "" {
		if *tls {
			*port = "6697"
		} else {
			*port = "6667"
		}
	}

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}
	if *path == "" {
		*path = usr.HomeDir + "/irc"
	}
	if *nick == "" {
		*nick = usr.Username
	}
	if *realName == "" {
		*realName = *nick
	}
	ircPath = *path + "/" + *server
	clientNick = *nick

	conn := connServer(*server, *port, *tls)
	defer conn.Close()
	login(conn, *server, os.Getenv(*pass), *realName)
	run(conn, *server)
}
