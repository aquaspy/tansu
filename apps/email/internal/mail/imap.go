package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap"
	"github.com/emersion/go-imap/client"
	"github.com/emersion/go-imap/commands"
	"github.com/emersion/go-imap/responses"
)

const logoutTimeout = 2 * time.Second

// Session is one logged-in IMAP connection. Close it when the request ends.
// Commands on a session are not safe to run concurrently.
type Session struct {
	cl   *client.Client
	svc  *Service
	stop func()
}

// Open dials, negotiates TLS, and logs in. The context cancels the connection.
func (s *Service) Open(ctx context.Context, c Creds) (*Session, error) {
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	cl, stop, err := dialIMAP(ctx, c, s.dialTimeout(), s.commandTimeout())
	if err != nil {
		s.release()
		return nil, err
	}
	sess := &Session{cl: cl, svc: s}
	var once sync.Once
	sess.stop = func() {
		once.Do(func() {
			stop()
			s.release()
		})
	}
	return sess, nil
}

// Close logs out and returns the connection slot. It is safe to call twice.
func (s *Session) Close() {
	if s == nil || s.stop == nil {
		return
	}
	s.stop()
}

// Folders returns LIST results on this connection.
func (s *Session) Folders() ([]Folder, error) {
	return fetchList(s.cl)
}

// List returns one header page, using the service cache when it is warm.
func (s *Session) List(accountID int64, folder, query string, page int) (Page, error) {
	return s.svc.listCached(s.cl, accountID, folder, query, page)
}

// Read fetches one message body on this connection and marks it \Seen when
// it was unread. accountID patches the header cache; zero skips that.
func (s *Session) Read(accountID int64, folder string, uid uint32) (*Message, error) {
	folder = defaultFolder(folder)
	msg, err := readOn(s.cl, folder, uid)
	if err != nil {
		return nil, err
	}
	if msg.Seen {
		s.svc.markCachedSeen(accountID, folder, uid, true)
	}
	return msg, nil
}

func (s *Service) listCached(cl *client.Client, accountID int64, folder, query string, page int) (Page, error) {
	page = clampPage(page)
	folder = defaultFolder(folder)
	query = trimQuery(query)
	if p, ok := s.cache.get(accountID, folder, query, page); ok {
		return p, nil
	}
	p, err := listOn(cl, folder, query, page)
	if err != nil {
		return Page{}, err
	}
	s.cache.put(accountID, folder, query, page, p)
	return p, nil
}

func dialIMAP(ctx context.Context, c Creds, dialTO, cmdTO time.Duration) (*client.Client, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	if dialTO <= 0 {
		dialTO = defaultDialTimeout
	}
	if cmdTO <= 0 {
		cmdTO = defaultCommandTimeout
	}
	addr := net.JoinHostPort(c.IMAPHost, fmt.Sprint(c.IMAPPort))
	raw, err := (&net.Dialer{Timeout: dialTO}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, nil, err
	}
	if err := raw.SetDeadline(connDeadline(ctx, dialTO)); err != nil {
		raw.Close()
		return nil, nil, err
	}
	conn := net.Conn(raw)
	mode := c.IMAPTLS
	if mode == "" {
		mode = "tls"
	}
	if mode == "starttls" {
		cl, err := greet(conn)
		if err != nil {
			return nil, nil, err
		}
		stop := finishIMAP(ctx, cl, cmdTO)
		cl.Timeout = cmdTO
		tlsCfg := &tls.Config{ServerName: c.IMAPHost, MinVersion: tls.VersionTLS12}
		if err := cl.StartTLS(tlsCfg); err != nil {
			stop()
			return nil, nil, err
		}
		if err := cl.Login(c.Username, c.Password); err != nil {
			stop()
			return nil, nil, err
		}
		return cl, stop, nil
	}
	if mode != "none" {
		tlsConn := tls.Client(raw, &tls.Config{ServerName: c.IMAPHost, MinVersion: tls.VersionTLS12})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			raw.Close()
			return nil, nil, err
		}
		conn = tlsConn
	}
	cl, err := greet(conn)
	if err != nil {
		return nil, nil, err
	}
	stop := finishIMAP(ctx, cl, cmdTO)
	cl.Timeout = cmdTO
	if err := cl.Login(c.Username, c.Password); err != nil {
		stop()
		return nil, nil, err
	}
	return cl, stop, nil
}

func greet(conn net.Conn) (*client.Client, error) {
	cl, err := client.New(conn)
	if err != nil {
		if cl != nil {
			_ = cl.Terminate()
		} else {
			conn.Close()
		}
		return nil, err
	}
	return cl, nil
}

// finishIMAP closes the connection when ctx ends, and on the returned stop.
// Timeout is applied by the caller before the next command so Login cannot
// clear the dial deadline and then wait forever.
func finishIMAP(ctx context.Context, cl *client.Client, cmdTO time.Duration) func() {
	stopWatch := watchCancel(ctx, cl)
	var once sync.Once
	return func() {
		once.Do(func() {
			stopWatch()
			closeClient(cl, cmdTO)
		})
	}
}

func watchCancel(ctx context.Context, cl *client.Client) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = cl.Terminate()
		case <-done:
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

func closeClient(cl *client.Client, cmdTO time.Duration) {
	if cl == nil {
		return
	}
	// Logout waits for the server. A short budget, then drop the socket.
	if cmdTO <= 0 || cmdTO > logoutTimeout {
		cl.Timeout = logoutTimeout
	}
	if err := cl.Logout(); err != nil {
		_ = cl.Terminate()
	}
}

func connDeadline(ctx context.Context, d time.Duration) time.Time {
	deadline := time.Now().Add(d)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		return dl
	}
	return deadline
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
	if len(out) > maxFolders {
		out = out[:maxFolders]
	}
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

func listOn(cl *client.Client, folder, query string, page int) (Page, error) {
	page = clampPage(page)
	query = trimQuery(query)
	folder = defaultFolder(folder)
	mbox, err := cl.Select(folder, true)
	if err != nil {
		return Page{}, err
	}
	if query == "" {
		return listRecent(cl, mbox, folder, page)
	}
	return listSearch(cl, mbox, folder, query, page)
}

// listRecent fetches one page of headers by sequence number. The highest
// sequences are the newest messages, so an inbox open does not SEARCH ALL.
func listRecent(cl *client.Client, mbox *imap.MailboxStatus, folder string, page int) (Page, error) {
	total := 0
	if mbox != nil {
		total = int(mbox.Messages)
	}
	out := Page{Messages: []Header{}, Total: total, Page: page, PageSize: PageSize, Folder: folder}
	if total == 0 {
		return out, nil
	}
	end := total - (page-1)*PageSize
	if end < 1 {
		return out, nil
	}
	start := end - PageSize + 1
	if start < 1 {
		start = 1
	}
	set := new(imap.SeqSet)
	set.AddRange(uint32(start), uint32(end))
	headers, err := fetchHeaders(cl, set, false)
	if err != nil {
		return Page{}, err
	}
	sort.SliceStable(headers, func(i, j int) bool { return headers[i].seq > headers[j].seq })
	out.Messages = headers
	return out, nil
}

func listSearch(cl *client.Client, mbox *imap.MailboxStatus, folder, query string, page int) (Page, error) {
	crit := searchCriteria(query)
	windowed, err := limitBodyWindow(cl, mbox, crit)
	if err != nil {
		return Page{}, err
	}
	uids, more, err := uidSearch(cl, crit)
	if err != nil {
		return Page{}, err
	}
	capped := windowed || more
	total := len(uids)
	start := (page - 1) * PageSize
	if start > total {
		start = total
	}
	end := start + PageSize
	if end > total {
		end = total
	}
	out := Page{Messages: []Header{}, Total: total, Page: page, PageSize: PageSize, Folder: folder, Query: query, Capped: capped}
	slice := uids[start:end]
	if len(slice) == 0 {
		return out, nil
	}
	set := new(imap.SeqSet)
	set.AddNum(slice...)
	headers, err := fetchHeaders(cl, set, true)
	if err != nil {
		return Page{}, err
	}
	byUID := map[uint32]Header{}
	for _, h := range headers {
		byUID[h.UID] = h
	}
	ordered := make([]Header, 0, len(slice))
	for _, uid := range slice {
		if h, ok := byUID[uid]; ok {
			ordered = append(ordered, h)
		}
	}
	out.Messages = ordered
	return out, nil
}

// limitBodyWindow pins text: and body: to the newest bodySearchSpan messages.
// The start UID is fetched from the oldest of those sequences, so an expunge
// gap does not slide the window past mail that is still in the box. Header
// searches are not limited: Dovecot answers those without reading bodies.
func limitBodyWindow(cl *client.Client, mbox *imap.MailboxStatus, crit *imap.SearchCriteria) (bool, error) {
	if crit == nil || (len(crit.Text) == 0 && len(crit.Body) == 0) {
		return false, nil
	}
	if mbox == nil || mbox.Messages <= bodySearchSpan {
		return false, nil
	}
	seq := mbox.Messages - bodySearchSpan + 1
	uid, err := fetchOneUID(cl, seq)
	if err != nil || uid <= 1 {
		if mbox.UidNext > bodySearchSpan+1 {
			return limitBodyScan(crit, mbox.UidNext-bodySearchSpan), nil
		}
		if err != nil {
			return false, err
		}
		return false, nil
	}
	return limitBodyScan(crit, uid), nil
}

func fetchOneUID(cl *client.Client, seq uint32) (uint32, error) {
	set := new(imap.SeqSet)
	set.AddNum(seq)
	ch := make(chan *imap.Message, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- cl.Fetch(set, []imap.FetchItem{imap.FetchUid}, ch)
	}()
	var uid uint32
	for msg := range ch {
		if msg != nil && msg.SeqNum == seq && msg.Uid != 0 {
			uid = msg.Uid
		}
	}
	if err := <-errCh; err != nil {
		return 0, err
	}
	if uid == 0 {
		return 0, fmt.Errorf("imap: no uid for sequence %d", seq)
	}
	return uid, nil
}

// uidSearch runs UID SEARCH. When the server advertises ESEARCH, the result
// is RETURN (ALL), a sequence-set, instead of one number per hit. go-imap's
// UidSearch always asks for the expanded list and always sends CHARSET UTF-8.
// ASCII criteria omit CHARSET; Dovecot accepts that and it avoids a charset
// retry. The returned UIDs are newest first, at most maxSearchHits.
func uidSearch(cl *client.Client, criteria *imap.SearchCriteria) ([]uint32, bool, error) {
	esearch, err := cl.Support("ESEARCH")
	if err != nil {
		return nil, false, err
	}
	set, status, err := runUIDSearch(cl, criteria, esearch, searchCharset(criteria))
	if err != nil {
		return nil, false, err
	}
	if esearch && status != nil && status.Type == imap.StatusRespBad {
		set, status, err = runUIDSearch(cl, criteria, false, searchCharset(criteria))
		if err != nil {
			return nil, false, err
		}
	}
	if status != nil {
		if err := status.Err(); err != nil {
			return nil, false, err
		}
	}
	uids, more := newestUIDs(set, maxSearchHits)
	return uids, more, nil
}

type uidSearchCmd struct {
	esearch  bool
	charset  string
	criteria *imap.SearchCriteria
}

func (c uidSearchCmd) Command() *imap.Command {
	inner := &imap.Command{Name: "SEARCH"}
	if c.esearch {
		inner.Arguments = append(inner.Arguments, imap.RawString("RETURN"), []interface{}{imap.RawString("ALL")})
	}
	if c.charset != "" {
		inner.Arguments = append(inner.Arguments, imap.RawString("CHARSET"), imap.RawString(c.charset))
	}
	if c.criteria == nil {
		inner.Arguments = append(inner.Arguments, imap.RawString("ALL"))
	} else {
		inner.Arguments = append(inner.Arguments, c.criteria.Format()...)
	}
	return (&commands.Uid{Cmd: inner}).Command()
}

type uidSearchResp struct {
	set *imap.SeqSet
}

func (r *uidSearchResp) Handle(resp imap.Resp) error {
	name, fields, ok := imap.ParseNamedResp(resp)
	if !ok {
		return responses.ErrUnhandled
	}
	var (
		set *imap.SeqSet
		err error
	)
	switch name {
	case "SEARCH":
		set, err = seqSetFromIDs(fields)
	case "ESEARCH":
		set, err = seqSetFromEsearch(fields)
	default:
		return responses.ErrUnhandled
	}
	if err != nil {
		return err
	}
	r.set = set
	return nil
}

func runUIDSearch(cl *client.Client, criteria *imap.SearchCriteria, esearch bool, charset string) (*imap.SeqSet, *imap.StatusResp, error) {
	res := &uidSearchResp{}
	status, err := cl.Execute(uidSearchCmd{esearch: esearch, charset: charset, criteria: criteria}, res)
	if err != nil {
		return nil, status, err
	}
	if res.set == nil {
		res.set = new(imap.SeqSet)
	}
	return res.set, status, nil
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

// fetchHeaders loads one page of list rows in a single FETCH: UID, FLAGS,
// and BODY.PEEK[HEADER.FIELDS ...]. FLAGS carries \Seen for the unread
// badge. This does not SEARCH UNSEEN and does not fetch bodies.
func fetchHeaders(cl *client.Client, set *imap.SeqSet, byUID bool) ([]Header, error) {
	if set == nil {
		return []Header{}, nil
	}
	section := headerFetchSection()
	items := []imap.FetchItem{imap.FetchUid, imap.FetchFlags, section.FetchItem()}
	ch := make(chan *imap.Message, PageSize)
	errCh := make(chan error, 1)
	go func() {
		if byUID {
			errCh <- cl.UidFetch(set, items, ch)
		} else {
			errCh <- cl.Fetch(set, items, ch)
		}
	}()
	var out []Header
	for msg := range ch {
		if msg == nil {
			continue
		}
		h := Header{UID: msg.Uid, Seen: hasFlag(msg.Flags, imap.SeenFlag), seq: msg.SeqNum}
		lit := msg.GetBody(section)
		if lit != nil {
			raw, _ := io.ReadAll(io.LimitReader(lit, 1<<20))
			parsed := headerOnly(raw)
			h.From, h.To, h.Subject, h.Date, h.MessageID = parsed.From, parsed.To, parsed.Subject, parsed.Date, parsed.MessageID
		}
		out = append(out, h)
	}
	if err := <-errCh; err != nil {
		return nil, err
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

// readOn loads the body once with BODY.PEEK so the server does not set
// \Seen as a fetch side effect. An unread message is then marked with one
// UID STORE +FLAGS.SILENT (\Seen), which does not return a new body.
// The mailbox is selected read-write so that STORE is allowed.
func readOn(cl *client.Client, folder string, uid uint32) (*Message, error) {
	if _, err := cl.Select(folder, false); err != nil {
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
	if parsed.Seen {
		return parsed, nil
	}
	if err := storeSeen(cl, set, true); err != nil {
		return parsed, nil
	}
	parsed.Seen = true
	return parsed, nil
}

func setSeenOn(cl *client.Client, folder string, uid uint32, seen bool) error {
	if _, err := cl.Select(folder, false); err != nil {
		return err
	}
	set := new(imap.SeqSet)
	set.AddNum(uid)
	return storeSeen(cl, set, seen)
}

func storeSeen(cl *client.Client, set *imap.SeqSet, seen bool) error {
	op := imap.FlagsOp(imap.AddFlags)
	if !seen {
		op = imap.RemoveFlags
	}
	// A nil update channel asks go-imap for FLAGS.SILENT, so the server
	// does not send the message back.
	return cl.UidStore(set, imap.FormatFlagsOp(op, true), []any{imap.SeenFlag}, nil)
}

func moveOn(cl *client.Client, folder string, uid uint32) error {
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
