package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
)

const dialTimeout = 15 * time.Second

func dialIMAP(ctx context.Context, c Creds) (*client.Client, error) {
	addr := net.JoinHostPort(c.IMAPHost, fmt.Sprint(c.IMAPPort))
	d := &net.Dialer{Timeout: dialTimeout}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, err
	}
	_ = raw.SetDeadline(time.Now().Add(dialTimeout))
	var conn net.Conn = raw
	switch c.IMAPTLS {
	case "none":
	case "starttls":
		cl, err := client.New(raw)
		if err != nil {
			raw.Close()
			return nil, err
		}
		if err := cl.StartTLS(&tls.Config{ServerName: c.IMAPHost}); err != nil {
			cl.Logout()
			return nil, err
		}
		if err := cl.Login(c.Username, c.Password); err != nil {
			cl.Logout()
			return nil, err
		}
		cl.Timeout = dialTimeout
		return cl, nil
	default:
		tlsConn := tls.Client(raw, &tls.Config{ServerName: c.IMAPHost})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, err
		}
		conn = tlsConn
	}
	cl, err := client.New(conn)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if err := cl.Login(c.Username, c.Password); err != nil {
		cl.Logout()
		return nil, err
	}
	cl.Timeout = dialTimeout
	return cl, nil
}

func testIMAP(ctx context.Context, c Creds) error {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return err
	}
	defer cl.Logout()
	_, err = fetchList(cl)
	return err
}

func listFolders(ctx context.Context, c Creds) ([]Folder, error) {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return nil, err
	}
	defer cl.Logout()
	return fetchList(cl)
}

func fetchList(cl *client.Client) ([]Folder, error) {
	ch := make(chan *imap.MailboxInfo, 32)
	errCh := make(chan error, 1)
	go func() { errCh <- cl.List("", "*", ch) }()
	var out []Folder
	for info := range ch {
		f := Folder{Name: info.Name, Special: specialOf(info), Selectable: selectable(info)}
		if f.Name == "" || !f.Selectable {
			continue
		}
		out = append(out, f)
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	sort.SliceStable(out, func(i, j int) bool {
		return specialRank(out[i].Special) < specialRank(out[j].Special) ||
			(specialRank(out[i].Special) == specialRank(out[j].Special) && out[i].Name < out[j].Name)
	})
	return out, nil
}

func selectable(info *imap.MailboxInfo) bool {
	for _, a := range info.Attributes {
		if strings.EqualFold(a, `\Noselect`) || strings.EqualFold(a, `\NonExistent`) {
			return false
		}
	}
	return true
}

func specialOf(info *imap.MailboxInfo) string {
	for _, a := range info.Attributes {
		switch strings.ToLower(a) {
		case `\inbox`:
			return "inbox"
		case `\sent`:
			return "sent"
		case `\trash`:
			return "trash"
		case `\drafts`:
			return "drafts"
		case `\junk`:
			return "junk"
		case `\archive`:
			return "archive"
		}
	}
	name := strings.ToLower(info.Name)
	name = strings.TrimPrefix(name, "inbox.")
	name = strings.TrimPrefix(name, "inbox/")
	switch {
	case strings.EqualFold(info.Name, "INBOX"):
		return "inbox"
	case name == "sent" || name == "sent mail" || name == "sent items" || strings.HasSuffix(name, "/sent") || strings.HasSuffix(name, "/sent mail"):
		return "sent"
	case name == "trash" || name == "deleted" || name == "deleted items" || name == "bin" || strings.HasSuffix(name, "/trash"):
		return "trash"
	case name == "drafts" || name == "draft":
		return "drafts"
	case name == "junk" || name == "spam":
		return "junk"
	default:
		return ""
	}
}

func specialRank(s string) int {
	switch s {
	case "inbox":
		return 0
	case "sent":
		return 1
	case "drafts":
		return 2
	case "archive":
		return 3
	case "junk":
		return 4
	case "trash":
		return 5
	default:
		return 6
	}
}

func trashName(folders []Folder) string {
	for _, f := range folders {
		if f.Special == "trash" {
			return f.Name
		}
	}
	return ""
}

func listMessages(ctx context.Context, c Creds, folder, query string, page int) (Page, error) {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return Page{}, err
	}
	defer cl.Logout()
	if _, err := cl.Select(folder, true); err != nil {
		return Page{}, err
	}
	uids, err := cl.UidSearch(searchCriteria(query))
	if err != nil {
		return Page{}, err
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] > uids[j] })
	total := len(uids)
	start := (page - 1) * PageSize
	if start > total {
		start = total
	}
	end := start + PageSize
	if end > total {
		end = total
	}
	slice := uids[start:end]
	headers, err := fetchHeaders(cl, slice)
	if err != nil {
		return Page{}, err
	}
	return Page{Messages: headers, Total: total, Page: page, PageSize: PageSize, Folder: folder, Query: query}, nil
}

func headerFetchSection() *imap.BodySectionName {
	return &imap.BodySectionName{
		BodyPartName: imap.BodyPartName{
			Specifier: imap.HeaderSpecifier,
			Fields:    []string{"From", "To", "Cc", "Subject", "Date", "Message-Id"},
		},
		Peek: true,
	}
}

func fetchHeaders(cl *client.Client, uids []uint32) ([]Header, error) {
	if len(uids) == 0 {
		return []Header{}, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(uids...)
	section := headerFetchSection()
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, section.FetchItem()}
	ch := make(chan *imap.Message, len(uids))
	errCh := make(chan error, 1)
	go func() { errCh <- cl.UidFetch(set, items, ch) }()
	byUID := map[uint32]Header{}
	for msg := range ch {
		if msg == nil {
			continue
		}
		h := Header{UID: msg.Uid, Seen: hasFlag(msg.Flags, imap.SeenFlag)}
		lit := msg.GetBody(section)
		if lit != nil {
			raw, _ := io.ReadAll(lit)
			parsed := headerOnly(raw)
			h.From, h.To, h.Subject, h.Date, h.MessageID = parsed.From, parsed.To, parsed.Subject, parsed.Date, parsed.MessageID
		}
		byUID[msg.Uid] = h
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	out := make([]Header, 0, len(uids))
	for _, uid := range uids {
		if h, ok := byUID[uid]; ok {
			out = append(out, h)
		}
	}
	return out, nil
}

func hasFlag(flags []string, want string) bool {
	for _, f := range flags {
		if strings.EqualFold(f, want) {
			return true
		}
	}
	return false
}

func fetchMessage(ctx context.Context, c Creds, folder string, uid uint32) (*Message, error) {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return nil, err
	}
	defer cl.Logout()
	if _, err := cl.Select(folder, true); err != nil {
		return nil, err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	section := &imap.BodySectionName{Peek: true}
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, section.FetchItem()}
	ch := make(chan *imap.Message, 1)
	errCh := make(chan error, 1)
	go func() { errCh <- cl.UidFetch(set, items, ch) }()
	var raw []byte
	var flags []string
	var got bool
	for msg := range ch {
		if msg == nil || msg.Uid != uid {
			continue
		}
		got = true
		flags = msg.Flags
		if lit := msg.GetBody(section); lit != nil {
			raw, _ = io.ReadAll(io.LimitReader(lit, 12<<20))
		}
	}
	if err := <-errCh; err != nil {
		return nil, err
	}
	if !got || len(raw) == 0 {
		return nil, ErrNotFound
	}
	parsed, err := parseRFC822(raw)
	if err != nil {
		return nil, err
	}
	parsed.UID = uid
	parsed.Seen = hasFlag(flags, imap.SeenFlag)
	return parsed, nil
}

func moveToTrash(ctx context.Context, c Creds, folder string, uid uint32) error {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return err
	}
	defer cl.Logout()
	folders, err := fetchList(cl)
	if err != nil {
		return err
	}
	trash := trashName(folders)
	if trash == "" {
		return ErrNoTrash
	}
	if sameFolder(folder, trash) {
		return deleteOn(cl, folder, uid)
	}
	if _, err := cl.Select(folder, false); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	return cl.UidMove(set, trash)
}

func deleteMessage(ctx context.Context, c Creds, folder string, uid uint32) error {
	cl, err := dialIMAP(ctx, c)
	if err != nil {
		return err
	}
	defer cl.Logout()
	return deleteOn(cl, folder, uid)
}

func deleteOn(cl *client.Client, folder string, uid uint32) error {
	if _, err := cl.Select(folder, false); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	return markDeleted(cl, set)
}

func markDeleted(cl *client.Client, set *imap.SeqSet) error {
	item := imap.FormatFlagsOp(imap.AddFlags, true)
	flags := []any{imap.DeletedFlag}
	if err := cl.UidStore(set, item, flags, nil); err != nil {
		return err
	}
	return cl.Expunge(nil)
}

func sameFolder(a, b string) bool {
	return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
}
