package store

import (
	"strings"
	"testing"
)

func TestMailboxCRUD(t *testing.T) {
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	user, err := st.CreateUser("ada@example.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	in := MailboxInput{
		DisplayName: "Ada", FromAddress: "ada@example.com", Username: "ada",
		IMAPHost: "imap.example", IMAPPort: 993, IMAPTLS: "tls",
		SMTPHost: "smtp.example", SMTPPort: 465, SMTPTLS: "tls",
	}
	box, err := st.CreateMailbox(user.ID, in, "cipher-text")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(box.PasswordEnc, "plaintext") {
		t.Fatal("unexpected")
	}
	got, err := st.FindMailbox(user.ID, box.ID)
	if err != nil || got.FromAddress != "ada@example.com" || got.PasswordEnc != "cipher-text" {
		t.Fatalf("%+v %v", got, err)
	}
	in.DisplayName = "Ada Lovelace"
	updated, err := st.UpdateMailbox(user.ID, box.ID, in, "")
	if err != nil || updated.DisplayName != "Ada Lovelace" || updated.PasswordEnc != "cipher-text" {
		t.Fatalf("keep secret: %+v %v", updated, err)
	}
	if err := st.SetMailboxStatus(user.ID, box.ID, false, "login failed"); err != nil {
		t.Fatal(err)
	}
	got, _ = st.FindMailbox(user.ID, box.ID)
	if got.LastError != "login failed" || got.LastOK != nil {
		t.Fatalf("status %+v", got)
	}
	if err := st.SetMailboxStatus(user.ID, box.ID, true, ""); err != nil {
		t.Fatal(err)
	}
	got, _ = st.FindMailbox(user.ID, box.ID)
	if got.LastOK == nil || got.LastError != "" {
		t.Fatalf("ok %+v", got)
	}
	list, err := st.ListMailboxes(user.ID)
	if err != nil || len(list) != 1 {
		t.Fatal(list, err)
	}
	if _, err := st.FindMailbox(user.ID+9, box.ID); err != ErrNotFound {
		t.Fatal(err)
	}
	if err := st.DeleteMailbox(user.ID, box.ID); err != nil {
		t.Fatal(err)
	}
	if st.CountMailboxes(user.ID) != 0 {
		t.Fatal("count")
	}
}
