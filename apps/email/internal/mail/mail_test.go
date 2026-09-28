package mail

import (
	"bytes"
	"context"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aquasp/kuraemail/internal/mailtest"
	"github.com/emersion/go-imap"
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
	if searchCharset(searchCriteria("grok")) != "" {
		t.Fatal("ascii search should omit CHARSET")
	}
	if searchCharset(searchCriteria("não")) != "UTF-8" {
		t.Fatal("non-ascii search should use UTF-8")
	}
	bare := formatSearch(uidSearchCmd{esearch: true, criteria: searchCriteria("grok")})
	if !strings.Contains(bare, "RETURN (ALL)") || strings.Contains(bare, "TEXT") || strings.Contains(bare, "CHARSET") {
		t.Fatalf("header command %s", bare)
	}
	body := searchCriteria("text:grok")
	if !limitBodyScan(body, 7800) {
		t.Fatal("expected a body window")
	}
	got := formatSearch(uidSearchCmd{esearch: true, criteria: body})
	if !strings.Contains(got, "7800:*") || !strings.Contains(got, `TEXT "grok"`) {
		t.Fatalf("body command %s", got)
	}
	if limitBodyScan(searchCriteria("grok"), 7800) {
		t.Fatal("header search must not grow a UID window")
	}
}

func TestNewestUIDsStopsAtLimit(t *testing.T) {
	set, err := imap.ParseSeqSet("1:10000000,10000002")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	uids, more := newestUIDs(set, 3)
	if time.Since(start) > time.Second {
		t.Fatalf("expanded a huge range in %s", time.Since(start))
	}
	if !more || len(uids) != 3 || uids[0] != 10000002 || uids[1] != 10000000 || uids[2] != 9999999 {
		t.Fatalf("uids %v more %v", uids, more)
	}
	fields := []interface{}{[]interface{}{"TAG", "A"}, "UID", "ALL", "10:12,20"}
	parsed, err := seqSetFromEsearch(fields)
	if err != nil {
		t.Fatal(err)
	}
	uids, more = newestUIDs(parsed, 10)
	if more || strings.Trim(fmtUids(uids), " ") != "20 12 11 10" {
		t.Fatalf("parsed %v more %v", uids, more)
	}
	empty, err := seqSetFromEsearch([]interface{}{[]interface{}{"TAG", "A"}, "UID", "COUNT", "0"})
	if err != nil || empty == nil || len(empty.Set) != 0 {
		t.Fatalf("empty %+v %v", empty, err)
	}
}

func fmtUids(uids []uint32) string {
	var b strings.Builder
	for _, u := range uids {
		b.WriteByte(' ')
		b.WriteString(strconv.FormatUint(uint64(u), 10))
	}
	return b.String()
}

func formatSearch(cmd imap.Commander) string {
	var buf bytes.Buffer
	if err := cmd.Command().WriteTo(imap.NewWriter(&buf)); err != nil {
		return err.Error()
	}
	return buf.String()
}

func TestDovecotSearchStrategy(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var mu sync.Mutex
	var lines []string
	go serveDovecotSearch(ln, &mu, &lines)
	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	svc := NewService()
	svc.DialTimeout = time.Second
	svc.CommandTimeout = time.Second
	c := Creds{IMAPHost: host, IMAPPort: port, IMAPTLS: "none", Username: "a", Password: "b"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	header, err := svc.List(ctx, 1, c, "INBOX", "grok", 1)
	if err != nil || header.Total != 1 || len(header.Messages) != 1 || header.Capped {
		t.Fatalf("header %+v %v", header, err)
	}
	body, err := svc.List(ctx, 1, c, "INBOX", "text:grok", 1)
	if err != nil || body.Total != 1 || len(body.Messages) != 1 || !body.Capped {
		t.Fatalf("body %+v %v", body, err)
	}
	mu.Lock()
	defer mu.Unlock()
	var sawHeader, sawBody, sawProbe bool
	for _, line := range lines {
		upper := strings.ToUpper(line)
		if strings.Contains(upper, "SEARCH") && strings.Contains(upper, "RETURN (ALL)") && strings.Contains(upper, "OR") && !strings.Contains(upper, "TEXT") {
			sawHeader = true
		}
		if strings.Contains(upper, "TEXT") && strings.Contains(line, "7800:*") && strings.Contains(upper, "RETURN (ALL)") {
			sawBody = true
		}
		if strings.Contains(upper, "FETCH 4801") {
			sawProbe = true
		}
	}
	if !sawHeader || !sawBody || !sawProbe {
		t.Fatalf("header %v body %v probe %v\n%s", sawHeader, sawBody, sawProbe, strings.Join(lines, "\n"))
	}
}

func serveDovecotSearch(ln net.Listener, mu *sync.Mutex, lines *[]string) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			_, _ = c.Write([]byte("* OK [CAPABILITY IMAP4rev1 ESEARCH] ready\r\n"))
			buf := make([]byte, 8192)
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
					line := strings.TrimRight(s[:i], "\r")
					acc.Reset()
					acc.WriteString(s[i+1:])
					mu.Lock()
					*lines = append(*lines, line)
					mu.Unlock()
					if !writeDovecotSearch(c, line) {
						return
					}
				}
				if err != nil {
					return
				}
			}
		}(c)
	}
}

func writeDovecotSearch(c net.Conn, line string) bool {
	tag, _, _ := strings.Cut(line, " ")
	upper := strings.ToUpper(line)
	switch {
	case strings.Contains(upper, "SEARCH"):
		if strings.Contains(upper, "TEXT") && !strings.Contains(line, ":*") {
			return false
		}
		ids := "11"
		if strings.Contains(upper, "TEXT") {
			ids = "7900"
		}
		_, _ = c.Write([]byte("* ESEARCH (TAG \"t\") UID ALL " + ids + "\r\n" + tag + " OK SEARCH\r\n"))
	case strings.Contains(upper, "EXAMINE"), strings.Contains(upper, "SELECT"):
		_, _ = c.Write([]byte("* FLAGS (\\Seen)\r\n* 5000 EXISTS\r\n* 0 RECENT\r\n* OK [UIDVALIDITY 1] UIDs valid\r\n* OK [UIDNEXT 8000] Predicted next UID\r\n" + tag + " OK [READ-ONLY] EXAMINE\r\n"))
	case strings.Contains(upper, "LOGIN"):
		_, _ = c.Write([]byte(tag + " OK [CAPABILITY IMAP4rev1 ESEARCH] logged in\r\n"))
	case strings.Contains(upper, "FETCH"):
		if strings.Contains(upper, "BODY") {
			uid := fieldAfter(upper, "FETCH")
			body := "From: Ada <ada@example.com>\r\nSubject: Hello\r\n\r\n"
			section := bodySection(line)
			_, _ = c.Write([]byte("* 1 FETCH (UID " + uid + " FLAGS (\\Seen) " + section + " {" + strconv.Itoa(len(body)) + "}\r\n" + body + ")\r\n" + tag + " OK FETCH\r\n"))
		} else {
			seq := fieldAfter(upper, "FETCH")
			_, _ = c.Write([]byte("* " + seq + " FETCH (UID 7800)\r\n" + tag + " OK FETCH\r\n"))
		}
	default:
		_, _ = c.Write([]byte(tag + " OK done\r\n"))
	}
	return true
}

func fieldAfter(upper, word string) string {
	fields := strings.Fields(upper)
	for i, f := range fields {
		if f == word && i+1 < len(fields) {
			return strings.Trim(fields[i+1], "()")
		}
	}
	return "1"
}

func bodySection(line string) string {
	upper := strings.ToUpper(line)
	i := strings.Index(upper, "BODY.PEEK[")
	if i < 0 {
		i = strings.Index(upper, "BODY[")
	}
	if i < 0 {
		return "BODY[HEADER]"
	}
	rest := line[i:]
	if strings.HasPrefix(strings.ToUpper(rest), "BODY.PEEK") {
		rest = "BODY" + rest[len("BODY.PEEK"):]
	}
	if end := strings.Index(rest, "]"); end >= 0 {
		return rest[:end+1]
	}
	return "BODY[HEADER]"
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
