package mail

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kuraemail/internal/mailtest"
)

func testCreds(imap *mailtest.IMAP, smtp *mailtest.SMTP) Creds {
	return Creds{
		IMAPHost: imap.Host, IMAPPort: imap.Port, IMAPTLS: "none",
		SMTPHost: smtp.Host, SMTPPort: smtp.Port, SMTPTLS: "none",
		Username: "ada", Password: "secret",
		From: "ada@example.com", DisplayName: "Ada",
	}
}

func TestOnDemandListSearchReadMoveSend(t *testing.T) {
	im, err := mailtest.StartIMAP("ada", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	sm, err := mailtest.StartSMTP("ada", "secret")
	if err != nil {
		t.Fatal(err)
	}
	defer sm.Close()
	svc := NewService()
	ctx := context.Background()
	c := testCreds(im, sm)
	if err := svc.Test(ctx, c); err != nil {
		t.Fatal(err)
	}
	folders, err := svc.Folders(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) < 3 || folders[0].Special != "inbox" {
		t.Fatalf("folders %+v", folders)
	}
	page, err := svc.List(ctx, 1, c, "INBOX", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total < 1 || len(page.Messages) == 0 || page.Messages[0].Subject != "Hello" {
		t.Fatalf("page %+v", page)
	}
	again, err := svc.List(ctx, 1, c, "INBOX", "", 1)
	if err != nil || again.Messages[0].UID != page.Messages[0].UID {
		t.Fatal(err)
	}
	found, err := svc.List(ctx, 1, c, "INBOX", "subject:Hello", 1)
	if err != nil || found.Total < 1 {
		t.Fatalf("%+v %v", found, err)
	}
	msg, err := svc.Read(ctx, c, "INBOX", page.Messages[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Text, "Hello") && msg.Subject != "Hello" {
		t.Fatalf("message %+v", msg)
	}
	if err := svc.Trash(ctx, 1, c, "INBOX", page.Messages[0].UID); err != nil {
		t.Fatal(err)
	}
	if len(im.Moved) == 0 && len(im.Deleted) == 0 {
		t.Fatal("move did not reach the server")
	}
	if _, err := svc.List(ctx, 1, c, "INBOX", "", 1); err != nil {
		t.Fatal(err)
	}
	preview, err := svc.Preview(c, Outgoing{To: []string{"bob@example.com"}, Subject: "Hi", Body: "There"})
	if err != nil || !strings.Contains(preview.From, "Ada") {
		t.Fatal(preview, err)
	}
	if err := svc.Send(ctx, 1, c, Outgoing{To: []string{"bob@example.com"}, Subject: "Hi", Body: "There"}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sm.Data, "Subject:") || strings.Contains(sm.Data, "secret") {
		t.Fatalf("smtp data %q", sm.Data)
	}
}

func TestSearchCriteria(t *testing.T) {
	c := searchCriteria("from:ada@example.com")
	if c.Header.Get("From") == "" {
		t.Fatal("from")
	}
	c = searchCriteria("since:2026-09-01")
	if c.Since.IsZero() {
		t.Fatal("since")
	}
	c = searchCriteria("packing list")
	if len(c.Or) != 1 || len(c.Text) != 0 {
		t.Fatal("bare query should search headers")
	}
	c = searchCriteria("text:packing list")
	if len(c.Text) != 1 || c.Text[0] != "packing list" {
		t.Fatal(c.Text)
	}
	if n := len([]rune(trimQuery(strings.Repeat("a", 250)))); n != maxQueryRunes {
		t.Fatalf("query len %d", n)
	}
}

func TestBuildDoesNotEmbedPassword(t *testing.T) {
	raw, err := buildRFC822(Creds{From: "ada@example.com", DisplayName: "Ada", Password: "s3cret"}, Outgoing{
		To: []string{"bob@example.com"}, Subject: "Hi", Body: "Hello",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "s3cret") {
		t.Fatal(string(raw))
	}
	msg, err := parseRFC822(raw)
	if err != nil || !strings.Contains(msg.Text, "Hello") {
		t.Fatal(msg, err)
	}
}

func TestSilentServerRespectsContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			time.Sleep(30 * time.Second)
			c.Close()
		}
	}()
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	svc := NewService()
	svc.DialTimeout = 30 * time.Second
	svc.CommandTimeout = 30 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = svc.Folders(ctx, Creds{IMAPHost: host, IMAPPort: port, IMAPTLS: "none", Username: "a", Password: "b"})
	if err == nil {
		t.Fatal("expected error")
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("hung for %s: %v", time.Since(start), err)
	}
}

func TestSearchTimeoutIsAnError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go serveHangSearch(ln)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	svc := NewService()
	svc.DialTimeout = time.Second
	svc.CommandTimeout = 400 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := Creds{IMAPHost: host, IMAPPort: port, IMAPTLS: "none", Username: "a", Password: "b"}
	page, err := svc.List(ctx, 1, c, "INBOX", "", 1)
	if err != nil || page.Total < 1 || len(page.Messages) == 0 {
		t.Fatalf("unfiltered list should not SEARCH: %+v %v", page, err)
	}
	start := time.Now()
	_, err = svc.List(ctx, 1, c, "INBOX", "grok", 1)
	if err == nil {
		t.Fatal("expected search error")
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("search hung for %s", time.Since(start))
	}
}

func serveHangSearch(ln net.Listener) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_, _ = c.Write([]byte("* OK ready\r\n"))
			buf := make([]byte, 4096)
			var acc strings.Builder
			for {
				_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
				n, err := c.Read(buf)
				if n > 0 {
					acc.Write(buf[:n])
				}
				for {
					s := acc.String()
					i := strings.Index(s, "\n")
					if i < 0 {
						break
					}
					line := s[:i]
					acc.Reset()
					acc.WriteString(s[i+1:])
					if strings.Contains(strings.ToUpper(line), " SEARCH") {
						return
					}
					tag, _, _ := strings.Cut(line, " ")
					upper := strings.ToUpper(line)
					switch {
					case strings.Contains(upper, "EXAMINE"), strings.Contains(upper, "SELECT"):
						_, _ = c.Write([]byte("* 1 EXISTS\r\n* 0 RECENT\r\n" + tag + " OK [READ-ONLY] SELECT\r\n"))
					case strings.Contains(upper, "FETCH"):
						body := "From: Ada <ada@example.com>\r\nSubject: Hello\r\n\r\n"
						_, _ = c.Write([]byte("* 1 FETCH (UID 11 FLAGS (\\Seen) BODY[HEADER] {" + strconv.Itoa(len(body)) + "}\r\n" + body + ")\r\n" + tag + " OK FETCH\r\n"))
					default:
						_, _ = c.Write([]byte(tag + " OK done\r\n"))
					}
				}
				if err != nil {
					return
				}
			}
		}(c)
	}
}

func TestHTMLFallback(t *testing.T) {
	raw := []byte("From: a@b.c\r\nSubject: S\r\nContent-Type: text/html\r\n\r\n<p>Hello <b>there</b></p>\r\n")
	msg, err := parseRFC822(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg.Text, "Hello") || strings.Contains(msg.Text, "<b>") {
		t.Fatal(msg.Text)
	}
}
