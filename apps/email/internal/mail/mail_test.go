package mail

import (
	"context"
	"strings"
	"testing"

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
	if len(c.Text) != 1 {
		t.Fatal(c.Text)
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
