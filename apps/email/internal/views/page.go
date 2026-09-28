package views

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/aquasp/kuraemail/internal/i18n"
	"github.com/aquasp/kuraemail/internal/mail"
)

// Page carries the data every full page needs.
type Page struct {
	L          i18n.Locale
	Title      string
	BodyClass  string
	CSRF       string
	Notice     string
	Alert      string
	KuraLogin  bool
	AccountURL string
}

func (p Page) T(key string, pairs ...string) string { return i18n.T(p.L, key, pairs...) }

func (p Page) Lang() string { return i18n.HTMLLang(p.L) }

func (p Page) OtherLocale() string {
	if p.L == i18n.PT {
		return "en"
	}
	return "pt"
}

func (p Page) OtherLocaleName() string {
	if p.L == i18n.PT {
		return "English"
	}
	return "Português"
}

func (p Page) I18nJSON() string {
	b, _ := json.Marshal(i18n.JS(p.L))
	return string(b)
}

// MailboxRow is one connected account in the switcher.
type MailboxRow struct {
	ID       int64
	Label    string
	Active   bool
	LastErr  string
	From     string
	Username string
	IMAP     string
	SMTP     string
	LastOK   string
}

// FolderRow is one IMAP folder.
type FolderRow struct {
	Name    string
	Special string
	Label   string
	Active  bool
	Href    string
}

// MessageRow is one list entry.
type MessageRow struct {
	UID     uint32
	From    string
	Subject string
	When    string
	Seen    bool
	Active  bool
	Href    string
}

// OpenMessage is the reading pane.
type OpenMessage struct {
	From        string
	To          string
	Cc          string
	Subject     string
	When        string
	Text        string
	UID         uint32
	Folder      string
	AccountID   int64
	ReplyHref   string
	Attachments []mail.Attachment
}

// Composer is the send form.
type Composer struct {
	To         string
	Cc         string
	Bcc        string
	Subject    string
	Body       string
	InReplyTo  string
	References string
}

// MailData drives the three-column shell.
type MailData struct {
	Accounts  []MailboxRow
	AccountID int64
	Folder    string
	Query     string
	Page      int
	Pages     int
	Total     int
	Folders   []FolderRow
	Messages  []MessageRow
	Open      *OpenMessage
	Compose   *Composer
	AutoLock  bool
	Empty     bool
	// Problem is "", "unavailable", or "secrets". The list must not look empty.
	Problem     string
	Capped      bool
	PrevHref    string
	NextHref    string
	ComposeHref string
	ListHref    string
}

// AccountForm is the connect/edit form.
type AccountForm struct {
	ID          int64
	DisplayName string
	From        string
	Username    string
	IMAPHost    string
	IMAPPort    string
	IMAPTLS     string
	SMTPHost    string
	SMTPPort    string
	SMTPTLS     string
	Editing     bool
	Rows        []MailboxRow
}

func MailHref(account int64, folder, q string, page int) string {
	v := url.Values{}
	if account > 0 {
		v.Set("account", strconv.FormatInt(account, 10))
	}
	if folder != "" {
		v.Set("folder", folder)
	}
	if q != "" {
		v.Set("q", q)
	}
	if page > 1 {
		v.Set("page", strconv.Itoa(page))
	}
	if len(v) == 0 {
		return "/"
	}
	return "/?" + v.Encode()
}

func ReadHref(account int64, folder string, uid uint32, q string) string {
	v := url.Values{}
	v.Set("account", strconv.FormatInt(account, 10))
	v.Set("folder", folder)
	v.Set("uid", strconv.FormatUint(uint64(uid), 10))
	if q != "" {
		v.Set("q", q)
	}
	return "/read?" + v.Encode()
}

func ComposeHref(account int64, folder string, uid uint32) string {
	v := url.Values{}
	if account > 0 {
		v.Set("account", strconv.FormatInt(account, 10))
	}
	if folder != "" {
		v.Set("folder", folder)
	}
	if uid > 0 {
		v.Set("uid", strconv.FormatUint(uint64(uid), 10))
	}
	return "/compose?" + v.Encode()
}

func FormatWhen(l i18n.Locale, t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return i18n.TimeShort(l, t)
}

func listProblem(p Page, d MailData) string {
	if d.Problem == "secrets" {
		return p.T("mail.secrets_missing")
	}
	if d.Query != "" {
		return p.T("mail.search_failed")
	}
	return p.T("mail.list_failed")
}

func shellClass(d MailData) string {
	if d.Open != nil || d.Compose != nil {
		return "app-shell is-editing"
	}
	return "app-shell"
}

func tlsLabel(p Page, mode string) string {
	switch mode {
	case "starttls":
		return "STARTTLS"
	case "none":
		return p.T("mail.tls_none")
	default:
		return "TLS"
	}
}

func selected(on bool) string {
	if on {
		return "selected"
	}
	return ""
}

func joinAddrs(list []string) string { return strings.Join(list, ", ") }

func uidStr(n uint32) string { return strconv.FormatUint(uint64(n), 10) }

func idStr(n int64) string { return strconv.FormatInt(n, 10) }

func attachmentHref(account int64, folder string, uid uint32, index int) string {
	v := url.Values{}
	v.Set("account", strconv.FormatInt(account, 10))
	v.Set("folder", folder)
	v.Set("uid", strconv.FormatUint(uint64(uid), 10))
	v.Set("part", strconv.Itoa(index))
	return "/attachment?" + v.Encode()
}

// Raw renders pre-sanitized HTML.
func Raw(s string) templ.Component { return templ.Raw(s) }

func folderLabel(p Page, f mail.Folder) string {
	switch f.Special {
	case "inbox":
		return p.T("mail.inbox")
	case "sent":
		return p.T("mail.sent")
	case "trash":
		return p.T("mail.trash")
	case "drafts":
		return p.T("mail.drafts")
	case "junk":
		return p.T("mail.junk")
	case "archive":
		return p.T("mail.archive")
	default:
		return f.Name
	}
}

func pages(total, page int) int {
	if total <= 0 {
		return 1
	}
	n := (total + mail.PageSize - 1) / mail.PageSize
	if n < 1 {
		return 1
	}
	if page > n {
		return n
	}
	return n
}

func fmtPages(page, pages int) string { return fmt.Sprintf("%d / %d", page, pages) }
