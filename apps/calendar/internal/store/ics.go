package store

import (
	"database/sql"
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// ICSFeedNameMax caps the label a person gives a subscription.
	ICSFeedNameMax = 80
	// ICSFeedURLMax caps the stored link. Tokens in the path still fit.
	ICSFeedURLMax = 2048
	// ICSFeedsPerUserCap keeps the subscription list small.
	ICSFeedsPerUserCap = 8
	// ICSEventsPerFeedCap is separate from EventPerUserCap so a busy
	// rota cannot push native events out.
	ICSEventsPerFeedCap = 800
)

var (
	// ErrICSNameBlank is an empty feed name.
	ErrICSNameBlank = errors.New("ics name blank")
	// ErrICSURL is a missing or oversized link. SSRF checks live in the
	// ics package; this only guards what we persist.
	ErrICSURL = errors.New("ics url")
	// ErrICSTooMany is the per-user feed cap.
	ErrICSTooMany = errors.New("ics too many feeds")
	// ErrICSNotFound is a missing feed, or one owned by someone else.
	ErrICSNotFound = errors.New("ics feed not found")
)

// ICSFeed is one subscribed HTTPS calendar. URL is a secret (the path
// often authenticates the feed) and must not be logged or rendered back.
type ICSFeed struct {
	ID            int64
	UserID        int64
	Name          string
	URL           string
	Paused        bool
	LastFetchedAt *time.Time
	LastError     string
	EventCount    int
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Host is the safe label for the list: hostname only, never the path.
func (f *ICSFeed) Host() string {
	if f == nil {
		return ""
	}
	u, err := url.Parse(f.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// ErrorKey maps LastError onto an i18n key. Empty means no message.
func (f *ICSFeed) ErrorKey() string {
	if f == nil {
		return ""
	}
	switch f.LastError {
	case "":
		return ""
	case "capped":
		return "feeds.capped"
	case "blocked":
		return "feeds.err_blocked"
	case "parse":
		return "feeds.err_parse"
	case "large":
		return "feeds.err_large"
	default:
		return "feeds.err_fetch"
	}
}

// ICSEvent is one read-only occurrence copied from a feed.
type ICSEvent struct {
	ID       int64
	UserID   int64
	FeedID   int64
	FeedName string
	UID      string
	Title    string
	Body     string
	AllDay   bool
	StartsOn string
	EndsOn   string
	StartsAt string
	EndsAt   string
}

// TimeLabel matches Event.TimeLabel for the day panel.
func (e *ICSEvent) TimeLabel() string {
	if e.AllDay || e.StartsAt == "" {
		return ""
	}
	if e.EndsAt == "" || e.EndsAt == e.StartsAt {
		return e.StartsAt
	}
	return e.StartsAt + "–" + e.EndsAt
}

// ICSEventInput is one row the sync wants stored. UID is the stable key
// (feed id + this uid).
type ICSEventInput struct {
	UID      string
	Title    string
	Body     string
	AllDay   bool
	StartsOn string
	EndsOn   string
	StartsAt string
	EndsAt   string
}

const icsFeedCols = `id, user_id, name, url, paused, last_fetched_at, last_error,
	event_count, created_at, updated_at`

func scanICSFeed(row interface{ Scan(...any) error }) (*ICSFeed, error) {
	f := &ICSFeed{}
	var paused int
	var fetched sql.NullString
	var created, updated string
	if err := row.Scan(&f.ID, &f.UserID, &f.Name, &f.URL, &paused, &fetched,
		&f.LastError, &f.EventCount, &created, &updated); err != nil {
		return nil, err
	}
	f.Paused = paused != 0
	if fetched.Valid && fetched.String != "" {
		if t, err := parseTime(fetched.String); err == nil {
			f.LastFetchedAt = &t
		}
	}
	f.CreatedAt, _ = parseTime(created)
	f.UpdatedAt, _ = parseTime(updated)
	return f, nil
}

// CreateICSFeed inserts a subscription. The caller has already checked the
// URL; this only stores it.
func (s *Store) CreateICSFeed(userID int64, name, rawURL string) (*ICSFeed, error) {
	name = strings.TrimSpace(name)
	rawURL = strings.TrimSpace(rawURL)
	if name == "" || utf8.RuneCountInString(name) > ICSFeedNameMax {
		return nil, ErrICSNameBlank
	}
	if rawURL == "" || len(rawURL) > ICSFeedURLMax {
		return nil, ErrICSURL
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM ics_feeds WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return nil, err
	}
	if n >= ICSFeedsPerUserCap {
		return nil, ErrICSTooMany
	}
	ts := now()
	res, err := tx.Exec(`INSERT INTO ics_feeds
		(user_id, name, url, paused, last_error, event_count, created_at, updated_at)
		VALUES (?, ?, ?, 0, '', 0, ?, ?)`, userID, name, rawURL, ts, ts)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.FindICSFeed(userID, id)
}

// ListICSFeeds returns the user's subscriptions, name then id.
func (s *Store) ListICSFeeds(userID int64) ([]*ICSFeed, error) {
	rows, err := s.db.Query(`SELECT `+icsFeedCols+` FROM ics_feeds
		WHERE user_id = ? ORDER BY name, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanICSFeeds(rows)
}

// ListActiveICSFeeds is the background sync set: every unpaused feed.
func (s *Store) ListActiveICSFeeds() ([]*ICSFeed, error) {
	rows, err := s.db.Query(`SELECT ` + icsFeedCols + ` FROM ics_feeds
		WHERE paused = 0 ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanICSFeeds(rows)
}

func scanICSFeeds(rows *sql.Rows) ([]*ICSFeed, error) {
	var out []*ICSFeed
	for rows.Next() {
		f, err := scanICSFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FindICSFeed loads one feed owned by userID.
func (s *Store) FindICSFeed(userID, id int64) (*ICSFeed, error) {
	f, err := scanICSFeed(s.db.QueryRow(`SELECT `+icsFeedCols+` FROM ics_feeds
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrICSNotFound
	}
	return f, err
}

// SetICSFeedPaused stops or resumes the periodic fetch. Events stay.
func (s *Store) SetICSFeedPaused(userID, id int64, paused bool) error {
	flag := 0
	if paused {
		flag = 1
	}
	res, err := s.db.Exec(`UPDATE ics_feeds SET paused = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, flag, now(), id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrICSNotFound
	}
	return nil
}

// DeleteICSFeed removes the feed and, via the foreign key, its events.
func (s *Store) DeleteICSFeed(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM ics_feeds WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrICSNotFound
	}
	return nil
}

// MarkICSFeedError records a fetch/parse failure without touching events.
func (s *Store) MarkICSFeedError(userID, id int64, code string) error {
	res, err := s.db.Exec(`UPDATE ics_feeds SET last_error = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, code, now(), id, userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrICSNotFound
	}
	return nil
}

// ReplaceICSEvents upserts the feed's rows by UID and deletes UIDs that
// are no longer present. Over the per-feed cap, the earliest rows in the
// slice (caller sorts by start) are kept and capped is true.
func (s *Store) ReplaceICSEvents(userID, feedID int64, events []ICSEventInput) (count int, capped bool, err error) {
	if _, err := s.FindICSFeed(userID, feedID); err != nil {
		return 0, false, err
	}
	dedup := map[string]ICSEventInput{}
	for _, e := range events {
		e.UID = strings.TrimSpace(e.UID)
		if e.UID == "" {
			continue
		}
		if len(e.UID) > 500 {
			e.UID = e.UID[:500]
		}
		e, ok := normalizeICSInput(e)
		if !ok {
			continue
		}
		dedup[e.UID] = e
	}
	list := make([]ICSEventInput, 0, len(dedup))
	for _, e := range dedup {
		list = append(list, e)
	}
	sortICSInputs(list)
	if len(list) > ICSEventsPerFeedCap {
		list = list[:ICSEventsPerFeedCap]
		capped = true
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback()
	ts := now()
	for _, e := range list {
		allDay := 0
		if e.AllDay {
			allDay = 1
		}
		if _, err := tx.Exec(`INSERT INTO ics_events
			(user_id, feed_id, uid, title, body, all_day, starts_on, ends_on, starts_at, ends_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(feed_id, uid) DO UPDATE SET
				title = excluded.title,
				body = excluded.body,
				all_day = excluded.all_day,
				starts_on = excluded.starts_on,
				ends_on = excluded.ends_on,
				starts_at = excluded.starts_at,
				ends_at = excluded.ends_at,
				updated_at = excluded.updated_at`,
			userID, feedID, e.UID, e.Title, e.Body, allDay, e.StartsOn, e.EndsOn,
			nullIfEmpty(e.StartsAt), nullIfEmpty(e.EndsAt), ts, ts); err != nil {
			return 0, false, err
		}
	}
	if len(list) == 0 {
		if _, err := tx.Exec(`DELETE FROM ics_events WHERE feed_id = ? AND user_id = ?`, feedID, userID); err != nil {
			return 0, false, err
		}
	} else {
		args := make([]any, 0, len(list)+2)
		args = append(args, feedID, userID)
		ph := make([]string, len(list))
		for i, e := range list {
			ph[i] = "?"
			args = append(args, e.UID)
		}
		q := `DELETE FROM ics_events WHERE feed_id = ? AND user_id = ? AND uid NOT IN (` + strings.Join(ph, ",") + `)`
		if _, err := tx.Exec(q, args...); err != nil {
			return 0, false, err
		}
	}
	code := ""
	if capped {
		code = "capped"
	}
	if _, err := tx.Exec(`UPDATE ics_feeds SET last_fetched_at = ?, last_error = ?,
		event_count = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		ts, code, len(list), ts, feedID, userID); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return len(list), capped, nil
}

func normalizeICSInput(e ICSEventInput) (ICSEventInput, bool) {
	e.Title = strings.TrimSpace(e.Title)
	if e.Title == "" {
		e.Title = "ICS"
	}
	if utf8.RuneCountInString(e.Title) > EventTitleMax {
		e.Title = trimRunes(e.Title, EventTitleMax)
	}
	e.Body = strings.TrimSpace(e.Body)
	if utf8.RuneCountInString(e.Body) > EventBodyMax {
		e.Body = trimRunes(e.Body, EventBodyMax)
	}
	if _, ok := ParseDate(e.StartsOn); !ok {
		return ICSEventInput{}, false
	}
	if _, ok := ParseDate(e.EndsOn); !ok {
		e.EndsOn = ""
	}
	if e.EndsOn == "" || e.EndsOn < e.StartsOn {
		e.EndsOn = e.StartsOn
	}
	if e.AllDay {
		e.StartsAt, e.EndsAt = "", ""
	} else {
		e.StartsAt = NormalizeClock(e.StartsAt)
		e.EndsAt = NormalizeClock(e.EndsAt)
		if e.StartsAt == "" {
			e.AllDay = true
			e.EndsAt = ""
		}
	}
	return e, true
}

func trimRunes(s string, max int) string {
	i := 0
	for n := 0; n < max && i < len(s); n++ {
		_, w := utf8.DecodeRuneInString(s[i:])
		i += w
	}
	return s[:i]
}

func sortICSInputs(list []ICSEventInput) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && lessICSInput(list[j], list[j-1]); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

func lessICSInput(a, b ICSEventInput) bool {
	if a.StartsOn != b.StartsOn {
		return a.StartsOn < b.StartsOn
	}
	if a.StartsAt != b.StartsAt {
		return a.StartsAt < b.StartsAt
	}
	return a.UID < b.UID
}

// ICSEventsInRange returns subscribed events overlapping [from, to]
// (inclusive YYYY-MM-DD), with the feed name attached.
func (s *Store) ICSEventsInRange(userID int64, from, to string) ([]*ICSEvent, error) {
	rows, err := s.db.Query(`SELECT e.id, e.user_id, e.feed_id, f.name, e.uid, e.title, e.body,
		e.all_day, e.starts_on, e.ends_on, e.starts_at, e.ends_at
		FROM ics_events e
		JOIN ics_feeds f ON f.id = e.feed_id
		WHERE e.user_id = ? AND e.starts_on <= ? AND e.ends_on >= ?
		ORDER BY e.starts_on, e.starts_at, e.id`, userID, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ICSEvent
	for rows.Next() {
		e := &ICSEvent{}
		var allDay int
		var startsAt, endsAt sql.NullString
		if err := rows.Scan(&e.ID, &e.UserID, &e.FeedID, &e.FeedName, &e.UID, &e.Title, &e.Body,
			&allDay, &e.StartsOn, &e.EndsOn, &startsAt, &endsAt); err != nil {
			return nil, err
		}
		e.AllDay = allDay != 0
		if startsAt.Valid {
			e.StartsAt = startsAt.String
		}
		if endsAt.Valid {
			e.EndsAt = endsAt.String
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
