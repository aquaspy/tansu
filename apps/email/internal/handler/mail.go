package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraemail/internal/i18n"
	"github.com/aquasp/kuraemail/internal/mail"
	"github.com/aquasp/kuraemail/internal/secret"
	"github.com/aquasp/kuraemail/internal/store"
	"github.com/aquasp/kuraemail/internal/views"
	"github.com/go-chi/chi/v5"
)

// mailOpTimeout bounds one IMAP/SMTP round trip from the browser.
// Command timeouts inside the client are shorter; cancelling this context
// closes the socket instead of leaving a read running.
const mailOpTimeout = 32 * time.Second

func (s *Server) handleMailIndex(w http.ResponseWriter, r *http.Request) {
	s.renderMail(w, r, false, false)
}

func (s *Server) handleMailRead(w http.ResponseWriter, r *http.Request) {
	s.renderMail(w, r, true, false)
}

func (s *Server) handleCompose(w http.ResponseWriter, r *http.Request) {
	s.renderMail(w, r, false, true)
}

func (s *Server) renderMail(w http.ResponseWriter, r *http.Request, reading, composing bool) {
	l := LocaleOf(r)
	user := UserOf(r)
	list, err := s.Store.ListMailboxes(user.ID)
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	d := views.MailData{AutoLock: AutoLockEnabled(r), Empty: len(list) == 0}
	accountID := queryID(r, "account")
	if accountID == 0 && len(list) > 0 {
		accountID = list[0].ID
	}
	d.Accounts = mailboxRows(l, list, accountID)
	d.AccountID = accountID
	d.Folder = r.URL.Query().Get("folder")
	d.Query = strings.TrimSpace(r.URL.Query().Get("q"))
	d.Page = 1
	if n, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && n > 0 {
		d.Page = n
		if d.Page > 200 {
			d.Page = 200
		}
	}
	d.ComposeHref = views.ComposeHref(accountID, d.Folder, 0)
	d.ListHref = views.MailHref(accountID, d.Folder, d.Query, d.Page)
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	if accountID == 0 {
		render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
		return
	}
	_, creds, err := s.openBox(user.ID, accountID)
	if err != nil {
		p.Alert = i18n.T(l, "mail.secrets_missing")
		d.Problem = "secrets"
		render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mailOpTimeout)
	defer cancel()
	sess, err := s.Mail.Open(ctx, creds)
	if err != nil {
		p.Alert = i18n.T(l, "mail.unavailable")
		d.Problem = "unavailable"
		render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
		return
	}
	defer sess.Close()
	folders, ferr := sess.Folders()
	if ferr != nil {
		p.Alert = i18n.T(l, "mail.unavailable")
		d.Problem = "unavailable"
	}
	if d.Folder == "" {
		d.Folder = inboxName(folders)
	}
	for _, f := range folders {
		d.Folders = append(d.Folders, views.FolderRow{
			Name: f.Name, Special: f.Special, Label: viewsFolderLabel(l, f),
			Active: f.Name == d.Folder,
			Href:   views.MailHref(accountID, f.Name, d.Query, 1),
		})
	}
	if ferr == nil {
		page, err := sess.List(accountID, d.Folder, d.Query, d.Page)
		if err != nil {
			p.Alert = i18n.T(l, "mail.unavailable")
			d.Problem = "unavailable"
		} else {
			d.Total = page.Total
			d.Pages = pageCount(page.Total)
			if d.Page > 1 {
				d.PrevHref = views.MailHref(accountID, d.Folder, d.Query, d.Page-1)
			}
			if d.Page < d.Pages {
				d.NextHref = views.MailHref(accountID, d.Folder, d.Query, d.Page+1)
			}
			d.Capped = page.Capped
			var openUID uint32
			if reading {
				openUID, _ = parseUID(r.URL.Query().Get("uid"))
			}
			for _, m := range page.Messages {
				d.Messages = append(d.Messages, views.MessageRow{
					UID: m.UID, From: m.From, Subject: m.Subject, Seen: m.Seen,
					When:   views.FormatWhen(l, m.Date),
					Active: m.UID == openUID,
					Href:   views.ReadHref(accountID, d.Folder, m.UID, d.Query),
				})
			}
		}
	}
	if reading {
		uid, ok := parseUID(r.URL.Query().Get("uid"))
		if ok && ferr == nil && d.Problem == "" {
			msg, err := sess.Read(d.Folder, uid)
			if err != nil {
				p.Alert = i18n.T(l, "mail.unavailable")
			} else {
				d.Open = &views.OpenMessage{
					From: msg.From, To: msg.To, Cc: msg.Cc, Subject: msg.Subject,
					When: views.FormatWhen(l, msg.Date), Text: msg.Text,
					UID: uid, Folder: d.Folder, AccountID: accountID,
					ReplyHref:   views.ComposeHref(accountID, d.Folder, uid),
					Attachments: msg.Attachments,
				}
			}
		}
	}
	if composing {
		c := &views.Composer{}
		if uid, ok := parseUID(r.URL.Query().Get("uid")); ok && ferr == nil && d.Problem == "" {
			if msg, err := sess.Read(d.Folder, uid); err == nil {
				out := quoteOutgoing(msg)
				c.To = strings.Join(out.To, ", ")
				c.Subject = out.Subject
				c.Body = out.Body
				c.InReplyTo = out.InReplyTo
				c.References = out.References
			}
		}
		d.Compose = c
	}
	d.ListHref = views.MailHref(accountID, d.Folder, d.Query, d.Page)
	d.ComposeHref = views.ComposeHref(accountID, d.Folder, 0)
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) handleMailTrash(w http.ResponseWriter, r *http.Request) {
	s.moveWeb(w, r, true)
}

func (s *Server) handleMailDelete(w http.ResponseWriter, r *http.Request) {
	s.moveWeb(w, r, false)
}

func (s *Server) moveWeb(w http.ResponseWriter, r *http.Request, trash bool) {
	l := LocaleOf(r)
	user := UserOf(r)
	id := queryID(r, "account")
	uid, ok := parseUID(r.FormValue("uid"))
	folder := r.FormValue("folder")
	if !ok || id == 0 {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	_, creds, err := s.openBox(user.ID, id)
	if err != nil {
		flashAlert(s, r, i18n.T(l, "mail.secrets_missing"))
		http.Redirect(w, r, views.MailHref(id, folder, "", 1), http.StatusSeeOther)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mailOpTimeout)
	defer cancel()
	if trash {
		err = s.Mail.Trash(ctx, id, creds, folder, uid)
	} else {
		err = s.Mail.Delete(ctx, id, creds, folder, uid)
	}
	if err != nil {
		flashAlert(s, r, i18n.T(l, "mail.unavailable"))
	} else if trash {
		flashNotice(s, r, i18n.T(l, "mail.trashed"))
	} else {
		flashNotice(s, r, i18n.T(l, "mail.deleted"))
	}
	http.Redirect(w, r, views.MailHref(id, folder, "", 1), http.StatusSeeOther)
}

func (s *Server) handleComposeSend(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id := queryID(r, "account")
	if id == 0 {
		id, _ = strconv.ParseInt(r.FormValue("account"), 10, 64)
	}
	back := views.ComposeHref(id, "", 0)
	_, creds, err := s.openBox(user.ID, id)
	if err != nil {
		flashAlert(s, r, i18n.T(l, "mail.secrets_missing"))
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	msg := mail.Outgoing{
		To: splitAddrs(r.FormValue("to")), Cc: splitAddrs(r.FormValue("cc")), Bcc: splitAddrs(r.FormValue("bcc")),
		Subject: r.FormValue("subject"), Body: r.FormValue("body"),
		InReplyTo: r.FormValue("in_reply_to"), References: r.FormValue("references"),
	}
	ctx, cancel := context.WithTimeout(r.Context(), mailOpTimeout)
	defer cancel()
	if err := s.Mail.Send(ctx, id, creds, msg); err != nil {
		flashAlert(s, r, secret.Scrub(err, creds.Password))
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	flashNotice(s, r, i18n.T(l, "mail.sent_ok"))
	http.Redirect(w, r, views.MailHref(id, "", "", 1), http.StatusSeeOther)
}

func (s *Server) handleAttachment(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id := queryID(r, "account")
	uid, ok := parseUID(r.URL.Query().Get("uid"))
	part, err := strconv.Atoi(r.URL.Query().Get("part"))
	if !ok || err != nil {
		s.notFound(w, r)
		return
	}
	_, creds, err := s.openBox(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), mailOpTimeout)
	defer cancel()
	name, mimeType, body, err := s.Mail.Attachment(ctx, creds, r.URL.Query().Get("folder"), uid, part)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}
	w.Header().Set("Content-Type", mimeType)
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(name, "\"", "")+"\"")
	_, _ = w.Write(body)
}

func (s *Server) handleAccounts(w http.ResponseWriter, r *http.Request) {
	s.renderAccounts(w, r, views.AccountForm{
		IMAPPort: "993", SMTPPort: "465", IMAPTLS: "tls", SMTPTLS: "tls",
	}, http.StatusOK)
}

func (s *Server) handleAccountEdit(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	box, err := s.Store.FindMailbox(user.ID, id)
	if err != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	s.renderAccounts(w, r, formFromBox(box), http.StatusOK)
}

func (s *Server) handleAccountsCreate(w http.ResponseWriter, r *http.Request) {
	s.saveAccountWeb(w, r, 0)
}

func (s *Server) handleAccountsUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	s.saveAccountWeb(w, r, id)
}

func (s *Server) saveAccountWeb(w http.ResponseWriter, r *http.Request, id int64) {
	l := LocaleOf(r)
	user := UserOf(r)
	in := formMailbox(r)
	var box *store.Mailbox
	var err error
	if id == 0 {
		box, err = s.saveNewMailbox(user.ID, in)
	} else {
		box, err = s.saveMailbox(user.ID, id, in)
	}
	if err != nil {
		if errors.Is(err, secret.ErrKey) {
			flashAlert(s, r, i18n.T(l, "mail.secrets_missing"))
		} else {
			flashAlert(s, r, err.Error())
		}
		f := views.AccountForm{
			ID: id, Editing: id != 0, DisplayName: in.DisplayName, From: in.FromAddress,
			Username: in.Username, IMAPHost: in.IMAPHost, SMTPHost: in.SMTPHost,
			IMAPPort: strconv.Itoa(in.IMAPPort), SMTPPort: strconv.Itoa(in.SMTPPort),
			IMAPTLS: in.IMAPTLS, SMTPTLS: in.SMTPTLS,
		}
		s.renderAccounts(w, r, f, http.StatusUnprocessableEntity)
		return
	}
	if box.LastError != "" {
		flashAlert(s, r, i18n.T(l, "mail.saved_error"))
	} else {
		flashNotice(s, r, i18n.T(l, "mail.saved"))
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleAccountsDestroy(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		http.Redirect(w, r, "/accounts", http.StatusSeeOther)
		return
	}
	_ = s.Store.DeleteMailbox(UserOf(r).ID, id)
	flashNotice(s, r, i18n.T(l, "mail.removed"))
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}

func (s *Server) renderAccounts(w http.ResponseWriter, r *http.Request, f views.AccountForm, status int) {
	list, _ := s.Store.ListMailboxes(UserOf(r).ID)
	f.Rows = mailboxRows(LocaleOf(r), list, f.ID)
	p := s.page(w, r, pTitle(r, "titles.accounts"), "auth-body")
	render(w, r, status, views.Layout(p, views.NoHead(), views.AccountsPage(p, f)))
}

func formFromBox(box *store.Mailbox) views.AccountForm {
	return views.AccountForm{
		ID: box.ID, Editing: true, DisplayName: box.DisplayName, From: box.FromAddress,
		Username: box.Username, IMAPHost: box.IMAPHost, SMTPHost: box.SMTPHost,
		IMAPPort: strconv.Itoa(box.IMAPPort), SMTPPort: strconv.Itoa(box.SMTPPort),
		IMAPTLS: box.IMAPTLS, SMTPTLS: box.SMTPTLS,
	}
}

func queryID(r *http.Request, key string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	if n == 0 {
		n, _ = strconv.ParseInt(r.FormValue(key), 10, 64)
	}
	return n
}

func inboxName(folders []mail.Folder) string {
	for _, f := range folders {
		if f.Special == "inbox" {
			return f.Name
		}
	}
	if len(folders) > 0 {
		return folders[0].Name
	}
	return "INBOX"
}

func pageCount(total int) int {
	if total <= 0 {
		return 1
	}
	n := (total + mail.PageSize - 1) / mail.PageSize
	if n < 1 {
		return 1
	}
	return n
}

func viewsFolderLabel(l i18n.Locale, f mail.Folder) string {
	switch f.Special {
	case "inbox":
		return i18n.T(l, "mail.inbox")
	case "sent":
		return i18n.T(l, "mail.sent")
	case "trash":
		return i18n.T(l, "mail.trash")
	case "drafts":
		return i18n.T(l, "mail.drafts")
	case "junk":
		return i18n.T(l, "mail.junk")
	case "archive":
		return i18n.T(l, "mail.archive")
	default:
		return f.Name
	}
}

func quoteOutgoing(msg *mail.Message) mail.Outgoing {
	out := mail.Outgoing{Subject: msg.Subject, InReplyTo: msg.MessageID, References: msg.References}
	if addr := firstAddr(msg.From); addr != "" {
		out.To = []string{addr}
	}
	if !strings.HasPrefix(strings.ToLower(out.Subject), "re:") {
		out.Subject = "Re: " + out.Subject
	}
	var b strings.Builder
	b.WriteString("\n\n")
	for _, line := range strings.Split(msg.Text, "\n") {
		b.WriteString("> ")
		b.WriteString(strings.TrimRight(line, "\r"))
		b.WriteString("\n")
	}
	out.Body = b.String()
	if msg.MessageID != "" {
		if out.References != "" {
			out.References += " "
		}
		out.References += msg.MessageID
	}
	return out
}

func firstAddr(raw string) string {
	raw = strings.TrimSpace(raw)
	if i := strings.LastIndex(raw, "<"); i >= 0 && strings.HasSuffix(raw, ">") {
		return strings.TrimSuffix(raw[i+1:], ">")
	}
	return raw
}
