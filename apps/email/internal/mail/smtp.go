package mail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

func testSMTP(ctx context.Context, c Creds) error {
	cl, err := dialSMTP(ctx, c)
	if err != nil {
		return err
	}
	defer cl.close()
	return cl.auth(c)
}

func sendSMTP(ctx context.Context, c Creds, raw []byte, rcpts []string) error {
	cl, err := dialSMTP(ctx, c)
	if err != nil {
		return err
	}
	defer cl.close()
	if err := cl.auth(c); err != nil {
		return err
	}
	if err := cl.cmdOK("MAIL FROM:<%s>", envelopeAddr(c.From)); err != nil {
		return err
	}
	for _, rcpt := range rcpts {
		if err := cl.cmdOK("RCPT TO:<%s>", envelopeAddr(rcpt)); err != nil {
			return err
		}
	}
	if err := cl.cmdExpect(354, "DATA"); err != nil {
		return err
	}
	if _, err := cl.w.Write(dotStuff(raw)); err != nil {
		return err
	}
	if _, err := cl.w.Write([]byte("\r\n.\r\n")); err != nil {
		return err
	}
	_, _, err = cl.read()
	return err
}

func envelopeAddr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "<"); i >= 0 && strings.HasSuffix(s, ">") {
		s = strings.TrimSuffix(s[i+1:], ">")
	}
	return s
}

func dotStuff(raw []byte) []byte {
	s := string(raw)
	if !strings.HasSuffix(s, "\r\n") {
		s += "\r\n"
	}
	lines := strings.Split(s, "\r\n")
	for i, line := range lines {
		if strings.HasPrefix(line, ".") {
			lines[i] = "." + line
		}
	}
	return []byte(strings.Join(lines, "\r\n"))
}

type smtpConn struct {
	c    net.Conn
	r    *lineReader
	w    net.Conn
	stop func()
}

func dialSMTP(ctx context.Context, c Creds) (*smtpConn, error) {
	addr := net.JoinHostPort(c.SMTPHost, fmt.Sprint(c.SMTPPort))
	d := &net.Dialer{Timeout: defaultDialTimeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := raw.SetDeadline(connDeadline(ctx, defaultDialTimeout)); err != nil {
		raw.Close()
		return nil, err
	}
	conn := raw
	if c.SMTPTLS != "starttls" && c.SMTPTLS != "none" {
		tlsConn := tls.Client(raw, &tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		conn = tlsConn
	}
	sc := &smtpConn{c: conn, r: newLineReader(conn), w: conn}
	code, _, err := sc.read()
	if err != nil {
		sc.close()
		return nil, err
	}
	if code != 220 {
		sc.close()
		return nil, fmt.Errorf("smtp greeting %d", code)
	}
	if err := sc.cmdOK("EHLO tansu"); err != nil {
		sc.close()
		return nil, err
	}
	if c.SMTPTLS == "starttls" {
		if err := sc.cmdExpect(220, "STARTTLS"); err != nil {
			sc.close()
			return nil, err
		}
		tlsConn := tls.Client(conn, &tls.Config{ServerName: c.SMTPHost, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			sc.close()
			return nil, err
		}
		sc.c = tlsConn
		sc.w = tlsConn
		sc.r = newLineReader(tlsConn)
		conn = tlsConn
		if err := sc.cmdOK("EHLO tansu"); err != nil {
			sc.close()
			return nil, err
		}
	}
	// The dial deadline covered the handshake. The rest of the session
	// gets its own budget, and a cancelled request closes the socket.
	if err := conn.SetDeadline(connDeadline(ctx, defaultCommandTimeout)); err != nil {
		sc.close()
		return nil, err
	}
	sc.stop = watchConn(ctx, conn)
	return sc, nil
}

func (s *smtpConn) auth(c Creds) error {
	plain := base64.StdEncoding.EncodeToString([]byte("\x00" + c.Username + "\x00" + c.Password))
	if _, err := fmt.Fprintf(s.w, "AUTH PLAIN %s\r\n", plain); err != nil {
		return err
	}
	code, _, err := s.read()
	if err != nil {
		return err
	}
	if code == 334 {
		if _, err := fmt.Fprintf(s.w, "%s\r\n", plain); err != nil {
			return err
		}
		code, _, err = s.read()
		if err != nil {
			return err
		}
	}
	if code != 235 {
		return fmt.Errorf("smtp auth failed")
	}
	return nil
}

func (s *smtpConn) cmdOK(format string, args ...any) error {
	return s.cmdExpect(250, format, args...)
}

func (s *smtpConn) cmdExpect(want int, format string, args ...any) error {
	if _, err := fmt.Fprintf(s.w, format+"\r\n", args...); err != nil {
		return err
	}
	code, text, err := s.read()
	if err != nil {
		return err
	}
	if code != want {
		return fmt.Errorf("smtp %d %s", code, text)
	}
	return nil
}

func (s *smtpConn) read() (int, string, error) {
	var last string
	for {
		line, err := s.r.ReadLine()
		if err != nil {
			return 0, "", err
		}
		if len(line) < 3 {
			return 0, line, fmt.Errorf("smtp short")
		}
		code := 0
		fmt.Sscanf(line[:3], "%d", &code)
		last = line
		if len(line) == 3 || line[3] != '-' {
			return code, last, nil
		}
	}
}

func (s *smtpConn) close() {
	if s == nil || s.c == nil {
		return
	}
	if s.stop != nil {
		s.stop()
		s.stop = nil
	}
	_ = s.c.SetDeadline(time.Now().Add(logoutTimeout))
	_, _ = fmt.Fprintf(s.w, "QUIT\r\n")
	_ = s.c.Close()
}

func watchConn(ctx context.Context, conn net.Conn) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

type lineReader struct {
	c   net.Conn
	buf []byte
}

func newLineReader(c net.Conn) *lineReader { return &lineReader{c: c} }

func (r *lineReader) ReadLine() (string, error) {
	for {
		if i := strings.Index(string(r.buf), "\n"); i >= 0 {
			line := string(r.buf[:i])
			r.buf = r.buf[i+1:]
			return strings.TrimRight(line, "\r"), nil
		}
		tmp := make([]byte, 1024)
		n, err := r.c.Read(tmp)
		if n > 0 {
			r.buf = append(r.buf, tmp[:n]...)
		}
		if err != nil {
			return "", err
		}
	}
}
