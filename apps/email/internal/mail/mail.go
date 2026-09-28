// Package mail talks to IMAP and SMTP on demand. Nothing here stores
// message bodies. Passwords are arguments, never log fields.
package mail

import (
	"context"
	"errors"
	"time"
)

const PageSize = 30

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
type Service struct {
	cache *listCache
}

func NewService() *Service {
	return &Service{cache: newListCache(20 * time.Second)}
}

func (s *Service) invalidate(accountID int64) {
	if s != nil && s.cache != nil {
		s.cache.drop(accountID)
	}
}

// Test logs into IMAP and authenticates to SMTP. It does not send mail.
func (s *Service) Test(ctx context.Context, c Creds) error {
	if err := testIMAP(ctx, c); err != nil {
		return err
	}
	return testSMTP(ctx, c)
}

// Folders returns LIST results worth showing.
func (s *Service) Folders(ctx context.Context, c Creds) ([]Folder, error) {
	return listFolders(ctx, c)
}

// List returns one page of headers. query is delegated to the server.
func (s *Service) List(ctx context.Context, accountID int64, c Creds, folder, query string, page int) (Page, error) {
	if page < 1 {
		page = 1
	}
	folder = defaultFolder(folder)
	if p, ok := s.cache.get(accountID, folder, query, page); ok {
		return p, nil
	}
	p, err := listMessages(ctx, c, folder, query, page)
	if err != nil {
		return Page{}, err
	}
	s.cache.put(accountID, folder, query, page, p)
	return p, nil
}

// Read fetches one message body.
func (s *Service) Read(ctx context.Context, c Creds, folder string, uid uint32) (*Message, error) {
	return fetchMessage(ctx, c, defaultFolder(folder), uid)
}

// Attachment returns one part's bytes.
func (s *Service) Attachment(ctx context.Context, c Creds, folder string, uid uint32, index int) (name, mime string, body []byte, err error) {
	msg, err := fetchMessage(ctx, c, defaultFolder(folder), uid)
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
	return moveToTrash(ctx, c, defaultFolder(folder), uid)
}

// Delete expunges a message from its folder.
func (s *Service) Delete(ctx context.Context, accountID int64, c Creds, folder string, uid uint32) error {
	defer s.invalidate(accountID)
	return deleteMessage(ctx, c, defaultFolder(folder), uid)
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
