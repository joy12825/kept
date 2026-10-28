// Package smtptest is a tiny SMTP server for tests: plain connections,
// optional AUTH PLAIN, and every accepted message kept in memory.
package smtptest

import (
	"bufio"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Message is one accepted message.
type Message struct {
	From string
	To   []string
	Data string
}

// Config sets up the server before it starts.
type Config struct {
	// Username and Password, when set, are required (AUTH PLAIN).
	Username, Password string
	// Reject makes RCPT fail with 550 for these addresses.
	Reject map[string]bool
}

type Server struct {
	Host string
	Port int
	cfg  Config

	ln   net.Listener
	mu   sync.Mutex
	msgs []Message
}

// Start listens on 127.0.0.1 until the test ends.
func Start(t testing.TB, cfg Config) *Server {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ln: ln, cfg: cfg, Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// Messages returns what was accepted so far.
func (s *Server) Messages() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.msgs...)
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	r := bufio.NewReader(c)
	reply := func(lines ...string) {
		for _, l := range lines {
			_, _ = c.Write([]byte(l + "\r\n"))
		}
	}
	reply("220 smtptest ready")
	var cur Message
	authed := s.cfg.Username == ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		verb := strings.ToUpper(strings.SplitN(line, " ", 2)[0])
		arg := strings.TrimSpace(strings.TrimPrefix(line, strings.SplitN(line, " ", 2)[0]))
		switch verb {
		case "EHLO":
			reply("250-smtptest", "250-AUTH PLAIN", "250 8BITMIME")
		case "HELO":
			reply("250 smtptest")
		case "AUTH":
			parts := strings.Fields(arg)
			raw, _ := base64.StdEncoding.DecodeString(parts[len(parts)-1])
			f := strings.Split(string(raw), "\x00")
			if len(f) == 3 && f[1] == s.cfg.Username && f[2] == s.cfg.Password {
				authed = true
				reply("235 ok")
			} else {
				reply("535 authentication failed")
			}
		case "MAIL":
			if !authed {
				reply("530 authentication required")
				continue
			}
			cur = Message{From: addr(arg)}
			reply("250 ok")
		case "RCPT":
			a := addr(arg)
			if s.cfg.Reject[a] {
				reply("550 no such user " + a)
				continue
			}
			cur.To = append(cur.To, a)
			reply("250 ok")
		case "DATA":
			reply("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.Data = b.String()
			s.mu.Lock()
			s.msgs = append(s.msgs, cur)
			s.mu.Unlock()
			reply("250 queued as " + strconv.Itoa(len(s.msgs)))
		case "RSET", "NOOP":
			reply("250 ok")
		case "QUIT":
			reply("221 bye")
			return
		default:
			reply("502 not implemented")
		}
	}
}

func addr(arg string) string {
	i, j := strings.Index(arg, "<"), strings.Index(arg, ">")
	if i < 0 || j < i {
		return ""
	}
	return arg[i+1 : j]
}
