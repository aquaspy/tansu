// Package mail talks to IMAP and SMTP on demand. Nothing here stores
// message bodies. Passwords are arguments, never log fields.
package mail

import (
	"context"
	"errors"
	"time"
)

const (
	// PageSize is how many headers one list page fetches.
	PageSize = 30
	// maxPage stops a huge page query from walking an entire mailbox.
	maxPage = 200
	// maxSearchHits is how many newest matches a search will page through.
	maxSearchHits = 1000
	// maxQueryRunes caps a search string before it is sent to IMAP.
	maxQueryRunes = 200
	// maxFolders caps LIST results kept for the sidebar.
	maxFolders = 400
	// maxCacheEntries bounds the short header-page cache.
	maxCacheEntries = 64
	// imapSlots bounds concurrent IMAP sessions for this process.
	imapSlots = 6
)

// Dial and command budgets. A zero value on Service uses these.
var (
	defaultDialTimeout    = 10 * time.Second
	defaultCommandTimeout = 25 * time.Second
)

// Creds is one mailbox's connection. Password is plaintext in memory only.
type Creds struct {
	IMAPHost    string
	IMAPPort    int
	IMAPTLS     string
	SMTPHost    string
	SMTPPort    int
	SMTPTLS     string
	Username    string
	Password    string
	From        string
	DisplayName string
}

// Folder is one LIST entry the UI can open.
type Folder struct {
	Name       string `json:"name"`
	Special    string `json:"special"`
	Selectable bool   `json:"selectable"`
}

// Header is one row in a folder page. Bodies are not included.
type Header struct {
	UID       uint32    `json:"uid"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Subject   string    `json:"subject"`
	Date      time.Time `json:"date"`
	Seen      bool      `json:"seen"`
	MessageID string    `json:"message_id,omitempty"`
	seq       uint32
}

// Attachment is metadata for one MIME part. Bytes are fetched separately.
type Attachment struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	MIME  string `json:"mime"`
	Size  int    `json:"size"`
}

// Message is a message opened by the reader.
type Message struct {
	Header
	Cc          string       `json:"cc,omitempty"`
	Text        string       `json:"text"`
	InReplyTo   string       `json:"in_reply_to,omitempty"`
	References  string       `json:"references,omitempty"`
	Attachments []Attachment `json:"attachments,omitempty"`
	parts       [][]byte
}

// Outgoing is a message the user (or a confirmed tool) wants to send.
type Outgoing struct {
	To         []string `json:"to"`
	Cc         []string `json:"cc"`
	Bcc        []string `json:"bcc"`
	Subject    string   `json:"subject"`
	Body       string   `json:"body"`
	InReplyTo  string   `json:"in_reply_to,omitempty"`
	References string   `json:"references,omitempty"`
}

// Page is one folder page. Total is the server's match count.
type Page struct {
	Messages []Header `json:"messages"`
	Total    int      `json:"total"`
	Page     int      `json:"page"`
	PageSize int      `json:"page_size"`
	Folder   string   `json:"folder"`
	Query    string   `json:"query,omitempty"`
	// Capped is true when Total is the newest maxSearchHits, not every match.
	Capped bool `json:"capped,omitempty"`
}

// Preview is a send that has not touched SMTP.
type Preview struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Cc      []string `json:"cc,omitempty"`
	Bcc     []string `json:"bcc,omitempty"`
	Subject string   `json:"subject"`
	Body    string   `json:"body"`
}

var (
	ErrNoTrash  = errors.New("no trash folder")
	ErrNotFound = errors.New("not found")
)

// Service is the on-demand mail client plus a short header-page cache.
// DialTimeout and CommandTimeout override the defaults when set (tests).
type Service struct {
	cache          *listCache
	sem            chan struct{}
	DialTimeout    time.Duration
	CommandTimeout time.Duration
}

func NewService() *Service {
	return &Service{
		cache: newListCache(20 * time.Second),
		sem:   make(chan struct{}, imapSlots),
	}
}

func (s *Service) dialTimeout() time.Duration {
	if s != nil && s.DialTimeout > 0 {
		return s.DialTimeout
	}
	return defaultDialTimeout
}

func (s *Service) commandTimeout() time.Duration {
	if s != nil && s.CommandTimeout > 0 {
		return s.CommandTimeout
	}
	return defaultCommandTimeout
}

func (s *Service) acquire(ctx context.Context) error {
	if s == nil || s.sem == nil {
		return nil
	}
	select {
	case s.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) release() {
	if s == nil || s.sem == nil {
		return
	}
	select {
	case <-s.sem:
	default:
	}
}

func (s *Service) invalidate(accountID int64) {
	if s != nil && s.cache != nil {
		s.cache.drop(accountID)
	}
}

// Test logs into IMAP and authenticates to SMTP. It does not send mail.
func (s *Service) Test(ctx context.Context, c Creds) error {
	sess, err := s.Open(ctx, c)
	if err != nil {
		return err
	}
	_, err = sess.Folders()
	sess.Close()
	if err != nil {
		return err
	}
	return testSMTP(ctx, c)
}

// Folders returns LIST results worth showing.
func (s *Service) Folders(ctx context.Context, c Creds) ([]Folder, error) {
	sess, err := s.Open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return sess.Folders()
}

// List returns one page of headers. An empty query fetches that page by
// sequence number. A query is an IMAP SEARCH, then one header FETCH.
func (s *Service) List(ctx context.Context, accountID int64, c Creds, folder, query string, page int) (Page, error) {
	page = clampPage(page)
	folder = defaultFolder(folder)
	query = trimQuery(query)
	if p, ok := s.cache.get(accountID, folder, query, page); ok {
		return p, nil
	}
	sess, err := s.Open(ctx, c)
	if err != nil {
		return Page{}, err
	}
	defer sess.Close()
	return s.listCached(sess.cl, accountID, folder, query, page)
}

// Read fetches one message body.
func (s *Service) Read(ctx context.Context, c Creds, folder string, uid uint32) (*Message, error) {
	sess, err := s.Open(ctx, c)
	if err != nil {
		return nil, err
	}
	defer sess.Close()
	return readOn(sess.cl, defaultFolder(folder), uid)
}

// Attachment returns one part's bytes.
func (s *Service) Attachment(ctx context.Context, c Creds, folder string, uid uint32, index int) (name, mime string, body []byte, err error) {
	msg, err := s.Read(ctx, c, folder, uid)
	if err != nil {
		return "", "", nil, err
	}
	if index < 0 || index >= len(msg.Attachments) || index >= len(msg.parts) {
		return "", "", nil, ErrNotFound
	}
	a := msg.Attachments[index]
	return a.Name, a.MIME, msg.parts[index], nil
}

// Trash moves a message into the Trash folder. Already-trashed messages
// are deleted.
func (s *Service) Trash(ctx context.Context, accountID int64, c Creds, folder string, uid uint32) error {
	defer s.invalidate(accountID)
	sess, err := s.Open(ctx, c)
	if err != nil {
		return err
	}
	defer sess.Close()
	return moveOn(sess.cl, defaultFolder(folder), uid)
}

// Delete expunges a message from its folder.
func (s *Service) Delete(ctx context.Context, accountID int64, c Creds, folder string, uid uint32) error {
	defer s.invalidate(accountID)
	sess, err := s.Open(ctx, c)
	if err != nil {
		return err
	}
	defer sess.Close()
	return deleteOn(sess.cl, defaultFolder(folder), uid)
}

// Preview checks a draft and returns what send would transmit.
func (s *Service) Preview(c Creds, msg Outgoing) (Preview, error) {
	if err := validateOutgoing(msg); err != nil {
		return Preview{}, err
	}
	return Preview{
		From:    formatAddress(c.DisplayName, c.From),
		To:      msg.To,
		Cc:      msg.Cc,
		Bcc:     msg.Bcc,
		Subject: msg.Subject,
		Body:    msg.Body,
	}, nil
}

// Send transmits a message over SMTP and drops the header cache.
func (s *Service) Send(ctx context.Context, accountID int64, c Creds, msg Outgoing) error {
	if _, err := s.Preview(c, msg); err != nil {
		return err
	}
	defer s.invalidate(accountID)
	raw, err := buildRFC822(c, msg)
	if err != nil {
		return err
	}
	return sendSMTP(ctx, c, raw, recipients(msg))
}

func defaultFolder(name string) string {
	if name == "" {
		return "INBOX"
	}
	return name
}

func clampPage(page int) int {
	if page < 1 {
		return 1
	}
	if page > maxPage {
		return maxPage
	}
	return page
}
