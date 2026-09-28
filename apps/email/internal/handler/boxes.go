package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraemail/internal/i18n"
	"github.com/aquasp/kuraemail/internal/mail"
	"github.com/aquasp/kuraemail/internal/secret"
	"github.com/aquasp/kuraemail/internal/store"
	"github.com/aquasp/kuraemail/internal/views"
)

func (s *Server) openBox(userID, id int64) (*store.Mailbox, mail.Creds, error) {
	box, err := s.Store.FindMailbox(userID, id)
	if err != nil {
		return nil, mail.Creds{}, err
	}
	creds, err := s.creds(box)
	if err != nil {
		return box, mail.Creds{}, err
	}
	return box, creds, nil
}

func (s *Server) creds(box *store.Mailbox) (mail.Creds, error) {
	if len(s.Config.SecretsKey) != 32 {
		return mail.Creds{}, secret.ErrKey
	}
	pass, err := secret.Decrypt(s.Config.SecretsKey, box.PasswordEnc)
	if err != nil {
		return mail.Creds{}, err
	}
	return mail.Creds{
		IMAPHost: box.IMAPHost, IMAPPort: box.IMAPPort, IMAPTLS: box.IMAPTLS,
		SMTPHost: box.SMTPHost, SMTPPort: box.SMTPPort, SMTPTLS: box.SMTPTLS,
		Username: box.Username, Password: pass,
		From: box.FromAddress, DisplayName: box.DisplayName,
	}, nil
}

func (s *Server) seal(password string) (string, error) {
	if len(s.Config.SecretsKey) != 32 {
		return "", secret.ErrKey
	}
	return secret.Encrypt(s.Config.SecretsKey, password)
}

type mailboxFields struct {
	DisplayName string `json:"display_name"`
	FromAddress string `json:"from_address"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	IMAPHost    string `json:"imap_host"`
	IMAPPort    int    `json:"imap_port"`
	IMAPTLS     string `json:"imap_tls"`
	SMTPHost    string `json:"smtp_host"`
	SMTPPort    int    `json:"smtp_port"`
	SMTPTLS     string `json:"smtp_tls"`
}

func (f mailboxFields) input() store.MailboxInput {
	return store.MailboxInput{
		DisplayName: strings.TrimSpace(f.DisplayName),
		FromAddress: strings.TrimSpace(f.FromAddress),
		Username:    strings.TrimSpace(f.Username),
		Password:    f.Password,
		IMAPHost:    strings.TrimSpace(f.IMAPHost),
		IMAPPort:    f.IMAPPort,
		IMAPTLS:     f.IMAPTLS,
		SMTPHost:    strings.TrimSpace(f.SMTPHost),
		SMTPPort:    f.SMTPPort,
		SMTPTLS:     f.SMTPTLS,
	}
}

func decodeMailbox(r *http.Request) (store.MailboxInput, error) {
	var wrap struct {
		Mailbox mailboxFields `json:"mailbox"`
		mailboxFields
	}
	dec := json.NewDecoder(io.LimitReader(r.Body, 1<<16))
	if err := dec.Decode(&wrap); err != nil {
		return store.MailboxInput{}, err
	}
	f := wrap.mailboxFields
	if wrap.Mailbox.Username != "" || wrap.Mailbox.IMAPHost != "" || wrap.Mailbox.FromAddress != "" {
		f = wrap.Mailbox
	}
	return f.input(), nil
}

func formMailbox(r *http.Request) store.MailboxInput {
	imapPort, _ := strconv.Atoi(r.FormValue("imap_port"))
	smtpPort, _ := strconv.Atoi(r.FormValue("smtp_port"))
	return store.MailboxInput{
		DisplayName: strings.TrimSpace(r.FormValue("display_name")),
		FromAddress: strings.TrimSpace(r.FormValue("from_address")),
		Username:    strings.TrimSpace(r.FormValue("username")),
		Password:    r.FormValue("password"),
		IMAPHost:    strings.TrimSpace(r.FormValue("imap_host")),
		IMAPPort:    imapPort,
		IMAPTLS:     r.FormValue("imap_tls"),
		SMTPHost:    strings.TrimSpace(r.FormValue("smtp_host")),
		SMTPPort:    smtpPort,
		SMTPTLS:     r.FormValue("smtp_tls"),
	}
}

func normalizeMailbox(in store.MailboxInput, needPassword bool) (store.MailboxInput, []string) {
	var errs []string
	in.FromAddress = store.NormalizeEmail(in.FromAddress)
	if !store.ValidEmail(in.FromAddress) {
		errs = append(errs, "from_address")
	}
	if in.Username == "" {
		errs = append(errs, "username")
	}
	if needPassword && strings.TrimSpace(in.Password) == "" {
		errs = append(errs, "password")
	}
	if in.IMAPHost == "" || strings.ContainsAny(in.IMAPHost, " /") {
		errs = append(errs, "imap_host")
	}
	if in.SMTPHost == "" || strings.ContainsAny(in.SMTPHost, " /") {
		errs = append(errs, "smtp_host")
	}
	if in.IMAPPort < 1 || in.IMAPPort > 65535 {
		errs = append(errs, "imap_port")
	}
	if in.SMTPPort < 1 || in.SMTPPort > 65535 {
		errs = append(errs, "smtp_port")
	}
	imapTLS, ok := store.NormalizeTLS(in.IMAPTLS)
	if !ok {
		errs = append(errs, "imap_tls")
	}
	smtpTLS, ok := store.NormalizeTLS(in.SMTPTLS)
	if !ok {
		errs = append(errs, "smtp_tls")
	}
	in.IMAPTLS, in.SMTPTLS = imapTLS, smtpTLS
	if in.DisplayName == "" {
		in.DisplayName = in.FromAddress
	}
	return in, errs
}

func publicMailbox(m *store.Mailbox) map[string]any {
	var lastOK any
	if m.LastOK != nil {
		lastOK = m.LastOK.UTC().Format(time.RFC3339)
	}
	return map[string]any{
		"id":           m.ID,
		"display_name": m.DisplayName,
		"from_address": m.FromAddress,
		"username":     m.Username,
		"imap_host":    m.IMAPHost,
		"imap_port":    m.IMAPPort,
		"imap_tls":     m.IMAPTLS,
		"smtp_host":    m.SMTPHost,
		"smtp_port":    m.SMTPPort,
		"smtp_tls":     m.SMTPTLS,
		"last_ok":      lastOK,
		"last_error":   m.LastError,
		"password_set": m.PasswordEnc != "",
	}
}

func (s *Server) saveNewMailbox(userID int64, in store.MailboxInput) (*store.Mailbox, error) {
	in, errs := normalizeMailbox(in, true)
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, ", "))
	}
	enc, err := s.seal(in.Password)
	if err != nil {
		return nil, err
	}
	box, err := s.Store.CreateMailbox(userID, in, enc)
	if err != nil {
		return nil, err
	}
	s.probe(userID, box, in.Password)
	return s.Store.FindMailbox(userID, box.ID)
}

func (s *Server) probe(userID int64, box *store.Mailbox, password string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	creds := mail.Creds{
		IMAPHost: box.IMAPHost, IMAPPort: box.IMAPPort, IMAPTLS: box.IMAPTLS,
		SMTPHost: box.SMTPHost, SMTPPort: box.SMTPPort, SMTPTLS: box.SMTPTLS,
		Username: box.Username, Password: password,
		From: box.FromAddress, DisplayName: box.DisplayName,
	}
	err := s.Mail.Test(ctx, creds)
	if err != nil {
		_ = s.Store.SetMailboxStatus(userID, box.ID, false, secret.Scrub(err, password))
		return
	}
	_ = s.Store.SetMailboxStatus(userID, box.ID, true, "")
}

func (s *Server) saveMailbox(userID, id int64, in store.MailboxInput) (*store.Mailbox, error) {
	cur, err := s.Store.FindMailbox(userID, id)
	if err != nil {
		return nil, err
	}
	need := strings.TrimSpace(in.Password) != ""
	in, errs := normalizeMailbox(in, false)
	if cur.PasswordEnc == "" && !need {
		errs = append(errs, "password")
	}
	if len(errs) > 0 {
		return nil, errors.New(strings.Join(errs, ", "))
	}
	enc := ""
	pass := in.Password
	if need {
		enc, err = s.seal(in.Password)
		if err != nil {
			return nil, err
		}
	} else {
		creds, err := s.creds(cur)
		if err != nil {
			return nil, err
		}
		pass = creds.Password
	}
	box, err := s.Store.UpdateMailbox(userID, id, in, enc)
	if err != nil {
		return nil, err
	}
	s.probe(userID, box, pass)
	return s.Store.FindMailbox(userID, id)
}

func flashNotice(s *Server, r *http.Request, notice string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, notice, "")
	}
}

func flashAlert(s *Server, r *http.Request, alert string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, "", alert)
	}
}

func mailboxRows(l i18n.Locale, list []*store.Mailbox, active int64) []views.MailboxRow {
	var rows []views.MailboxRow
	for _, m := range list {
		label := m.DisplayName
		if label == "" {
			label = m.FromAddress
		}
		last := ""
		if m.LastOK != nil {
			last = views.FormatWhen(l, *m.LastOK)
		}
		rows = append(rows, views.MailboxRow{
			ID: m.ID, Label: label, Active: m.ID == active, LastErr: m.LastError,
			From: m.FromAddress, Username: m.Username,
			IMAP: m.IMAPHost, SMTP: m.SMTPHost, LastOK: last,
		})
	}
	return rows
}

func parseUID(raw string) (uint32, bool) {
	n, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || n == 0 {
		return 0, false
	}
	return uint32(n), true
}

func splitAddrs(raw string) []string {
	raw = strings.ReplaceAll(raw, ";", ",")
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if v := strings.TrimSpace(part); v != "" {
			out = append(out, v)
		}
	}
	return out
}
