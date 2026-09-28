// Package mailtest speaks just enough IMAP and SMTP for unit tests.
package mailtest

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
)

// IMAP is an in-process mailbox with two messages in INBOX.
type IMAP struct {
	ln       net.Listener
	Addr     string
	Host     string
	Port     int
	User     string
	Password string
	Moved    []uint32
	Deleted  []uint32
	mu       sync.Mutex
}

func StartIMAP(user, password string) (*IMAP, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	s := &IMAP{ln: ln, Addr: ln.Addr().String(), Host: host, Port: port, User: user, Password: password}
	go s.serve()
	return s, nil
}

func (s *IMAP) Close() { _ = s.ln.Close() }

func (s *IMAP) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *IMAP) handle(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	fmt.Fprintf(w, "* OK [CAPABILITY IMAP4rev1 MOVE] mock ready\r\n")
	w.Flush()
	selected := ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if os.Getenv("MAILTEST_DEBUG") == "1" {
			log.Printf("IMAP C: %s", line)
		}
		tag, rest, _ := strings.Cut(line, " ")
		cmd, arg, _ := strings.Cut(rest, " ")
		cmd = strings.ToUpper(cmd)
		switch cmd {
		case "CAPABILITY":
			fmt.Fprintf(w, "* CAPABILITY IMAP4rev1 MOVE\r\n%s OK CAPABILITY\r\n", tag)
		case "LOGIN":
			fmt.Fprintf(w, "%s OK logged in\r\n", tag)
		case "LIST":
			fmt.Fprintf(w, "* LIST (\\HasNoChildren) \"/\" \"INBOX\"\r\n")
			fmt.Fprintf(w, "* LIST (\\Sent) \"/\" \"Sent\"\r\n")
			fmt.Fprintf(w, "* LIST (\\Trash) \"/\" \"Trash\"\r\n")
			fmt.Fprintf(w, "%s OK LIST\r\n", tag)
		case "SELECT", "EXAMINE":
			name := strings.Trim(arg, "\"")
			selected = name
			fmt.Fprintf(w, "* 2 EXISTS\r\n* 0 RECENT\r\n* OK [UIDVALIDITY 1]\r\n* OK [UIDNEXT 12]\r\n%s OK [READ-WRITE] SELECT\r\n", tag)
		case "UID":
			s.uid(w, r, tag, arg, selected)
		case "LOGOUT":
			fmt.Fprintf(w, "* BYE\r\n%s OK\r\n", tag)
			w.Flush()
			return
		case "NOOP":
			fmt.Fprintf(w, "%s OK done\r\n", tag)
		default:
			fmt.Fprintf(w, "%s OK done\r\n", tag)
		}
		w.Flush()
	}
}

func fetchSection(rest string) string {
	upper := strings.ToUpper(rest)
	if i := strings.Index(upper, "BODY.PEEK["); i >= 0 {
		inner := rest[i+len("BODY.PEEK"):]
		if end := strings.Index(inner, "]"); end >= 0 {
			return "BODY" + inner[:end+1]
		}
	}
	if i := strings.Index(upper, "BODY["); i >= 0 {
		inner := rest[i+len("BODY"):]
		if end := strings.Index(inner, "]"); end >= 0 {
			return "BODY" + inner[:end+1]
		}
	}
	return "BODY[]"
}

func (s *IMAP) uid(w io.Writer, r *bufio.Reader, tag, arg, selected string) {
	op, rest, _ := strings.Cut(arg, " ")
	switch strings.ToUpper(op) {
	case "SEARCH":
		if strings.Contains(strings.ToUpper(rest), "TEXT") && strings.Contains(strings.ToLower(rest), "missing") {
			fmt.Fprintf(w, "* SEARCH\r\n%s OK SEARCH\r\n", tag)
			return
		}
		if strings.Contains(strings.ToLower(rest), "hello") || strings.Contains(strings.ToUpper(rest), "FROM") {
			fmt.Fprintf(w, "* SEARCH 11\r\n%s OK SEARCH\r\n", tag)
			return
		}
		fmt.Fprintf(w, "* SEARCH 11 10\r\n%s OK SEARCH\r\n", tag)
	case "FETCH":
		header := "From: Ada <ada@example.com>\r\nTo: Bob <bob@example.com>\r\nSubject: Hello\r\nDate: Mon, 28 Sep 2026 12:00:00 +0000\r\nMessage-Id: <m1@example.com>\r\n\r\n"
		body := header + "Hello from Ada.\r\n"
		payload := header
		section := fetchSection(rest)
		if section == "BODY[]" {
			payload = body
		}
		uids := []string{"11"}
		if strings.Contains(rest, "10") {
			uids = []string{"11", "10"}
		}
		for i, uid := range uids {
			fmt.Fprintf(w, "* %d FETCH (UID %s FLAGS (\\Seen) %s {%d}\r\n%s)\r\n", i+1, uid, section, len(payload), payload)
		}
		fmt.Fprintf(w, "%s OK FETCH\r\n", tag)
	case "MOVE":
		s.mu.Lock()
		s.Moved = append(s.Moved, 11)
		s.mu.Unlock()
		fmt.Fprintf(w, "%s OK MOVE\r\n", tag)
	case "STORE":
		s.mu.Lock()
		s.Deleted = append(s.Deleted, 11)
		s.mu.Unlock()
		fmt.Fprintf(w, "%s OK STORE\r\n", tag)
	case "COPY":
		fmt.Fprintf(w, "%s OK COPY\r\n", tag)
	default:
		fmt.Fprintf(w, "%s OK done\r\n", tag)
	}
}

// SMTP accepts one authenticated session and records the DATA payload.
type SMTP struct {
	ln       net.Listener
	Host     string
	Port     int
	User     string
	Password string
	mu       sync.Mutex
	Data     string
	AuthOK   bool
}

func StartSMTP(user, password string) (*SMTP, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	s := &SMTP{ln: ln, Host: host, Port: port, User: user, Password: password}
	go s.serve()
	return s, nil
}

func (s *SMTP) Close() { _ = s.ln.Close() }

func (s *SMTP) serve() {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.handle(c)
	}
}

func (s *SMTP) handle(c net.Conn) {
	defer c.Close()
	w := bufio.NewWriter(c)
	r := bufio.NewReader(c)
	fmt.Fprintf(w, "220 mock ESMTP\r\n")
	w.Flush()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "EHLO"), strings.HasPrefix(upper, "HELO"):
			fmt.Fprintf(w, "250-mock\r\n250 AUTH PLAIN\r\n")
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			s.mu.Lock()
			s.AuthOK = true
			s.mu.Unlock()
			fmt.Fprintf(w, "235 ok\r\n")
		case strings.HasPrefix(upper, "MAIL FROM"), strings.HasPrefix(upper, "RCPT TO"):
			fmt.Fprintf(w, "250 ok\r\n")
		case upper == "DATA":
			fmt.Fprintf(w, "354 go\r\n")
			w.Flush()
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if strings.TrimRight(l, "\r\n") == "." {
					break
				}
				b.WriteString(l)
			}
			s.mu.Lock()
			s.Data = b.String()
			s.mu.Unlock()
			fmt.Fprintf(w, "250 queued\r\n")
		case upper == "QUIT":
			fmt.Fprintf(w, "221 bye\r\n")
			w.Flush()
			return
		default:
			fmt.Fprintf(w, "250 ok\r\n")
		}
		w.Flush()
	}
}
