package store

import (
	"database/sql"
	"errors"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Caps mirror the Rails MAX_PER_* constants.
const (
	MaxProfilesPerUser = 12
	MaxSitesPerProfile = 60
	MaxStackPerProfile = 80
	MaxProfileName     = 40
	MaxSiteTitle       = 80
	MaxSiteHint        = 120
	MaxURL             = 2048
	MaxStackCategory   = 60
	MaxStackChoice     = 80
	MaxStackOrigin     = 80
	MaxStackNote       = 160
)

// Palette mirrors HasIcon::PALETTE.
var Palette = []string{"#b55220", "#3d6b5a", "#5c4a7a", "#7a4a3a", "#3a5a7a", "#6b5a3d", "#7a5c2e", "#4a6b4a"}

type Profile struct {
	ID        int64
	UserID    int64
	Name      string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type Site struct {
	ID        int64
	ProfileID int64
	Title     string
	URL       string
	Hint      string
	IconURL   string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

type StackItem struct {
	ID        int64
	ProfileID int64
	Category  string
	Choice    string
	Origin    string
	Note      string
	URL       string
	IconURL   string
	Position  int
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (s *StackItem) Linked() bool { return s.URL != "" }

// ValError is one validation failure. Attr "" means a base error.
// Handlers render it through the i18n err.* tables.
type ValError struct {
	Attr  string
	Code  string
	Count int
}

func (e *ValError) Error() string { return e.Attr + " " + e.Code }

// Squash mirrors the Rails normalizes (strip + collapse whitespace).
func Squash(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

var schemeRe = regexp.MustCompile(`\A[a-z][a-z0-9+.-]*://`)

// NormalizeURL prepends https:// when no scheme is present, like the
// Rails before_validation hooks. required=false maps blank to "".
func NormalizeURL(raw string, required bool) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if required {
			return ""
		}
		return ""
	}
	if !schemeRe.MatchString(strings.ToLower(raw)) {
		raw = "https://" + raw
	}
	return raw
}

// ValidHTTPURL mirrors the Rails http_url checks: http(s), host, no userinfo.
func ValidHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	if u.Hostname() == "" || u.User != nil {
		return false
	}
	return true
}

func tooLong(s string, max int) bool { return utf8.RuneCountInString(s) > max }

// Host mirrors HasIcon#host (www. stripped).
func Host(rawurl string) string {
	u, err := url.Parse(rawurl)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

var alnumRe = regexp.MustCompile(`[^\p{L}\p{N}]`)

// Letter mirrors HasIcon#letter: first alnum char of the label, else of
// the host, else "?".
func Letter(label, rawurl string) string {
	stripped := alnumRe.ReplaceAllString(label, "")
	if r := firstRune(stripped); r != "" {
		return strings.ToUpper(r)
	}
	if r := firstRune(alnumRe.ReplaceAllString(Host(rawurl), "")); r != "" {
		return strings.ToUpper(r)
	}
	return "?"
}

func firstRune(s string) string {
	for _, r := range s {
		return string(r)
	}
	return ""
}

// Color mirrors HasIcon#color: palette pick from the host (or letter).
func Color(rawurl, label string) string {
	key := Host(rawurl)
	if key == "" {
		key = Letter(label, rawurl)
	}
	sum := 0
	for i := 0; i < len(key); i++ {
		sum += int(key[i])
	}
	return Palette[sum%len(Palette)]
}

// IconSrc mirrors HasIcon#icon_src.
func IconSrc(iconURL, rawurl string) string {
	if iconURL != "" {
		return iconURL
	}
	if h := Host(rawurl); h != "" {
		return "https://icons.duckduckgo.com/ip3/" + h + ".ico"
	}
	return ""
}

// SiteIcon helpers for views.
func (s *Site) Host() string    { return Host(s.URL) }
func (s *Site) Letter() string  { return Letter(s.Title, s.URL) }
func (s *Site) Color() string   { return Color(s.URL, s.Title) }
func (s *Site) IconSrc() string { return IconSrc(s.IconURL, s.URL) }
func (s *Site) HintOrHost() string {
	if s.Hint != "" {
		return s.Hint
	}
	return s.Host()
}

func (s *StackItem) Host() string    { return Host(s.URL) }
func (s *StackItem) Letter() string  { return Letter(s.Choice, s.URL) }
func (s *StackItem) Color() string   { return Color(s.URL, s.Choice) }
func (s *StackItem) IconSrc() string { return IconSrc(s.IconURL, s.URL) }

// --- profiles ---

func scanProfile(row interface{ Scan(...any) error }) (*Profile, error) {
	p := &Profile{}
	var created, updated string
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Position, &created, &updated)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = parseTime(created)
	p.UpdatedAt, _ = parseTime(updated)
	return p, nil
}

const profileCols = `id, user_id, name, position, created_at, updated_at`

func (s *Store) ValidateProfile(userID int64, id int64, name string, isCreate bool) []*ValError {
	var out []*ValError
	name = Squash(name)
	switch {
	case name == "":
		out = append(out, &ValError{Attr: "name", Code: "blank"})
	case tooLong(name, MaxProfileName):
		out = append(out, &ValError{Attr: "name", Code: "too_long", Count: MaxProfileName})
	default:
		var n int
		err := s.db.QueryRow(`SELECT COUNT(*) FROM profiles
			WHERE user_id = ? AND id != ? AND LOWER(name) = LOWER(?)`, userID, id, name).Scan(&n)
		if err == nil && n > 0 {
			out = append(out, &ValError{Attr: "name", Code: "taken"})
		}
	}
	if isCreate {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE user_id = ?`, userID).Scan(&n); err == nil && n >= MaxProfilesPerUser {
			out = append(out, &ValError{Code: "too_many"})
		}
	}
	return out
}

func (s *Store) CreateProfile(userID int64, name string, position int) (*Profile, error) {
	name = Squash(name)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO profiles (user_id, name, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`, userID, name, position, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &Profile{ID: id, UserID: userID, Name: name, Position: position, CreatedAt: t, UpdatedAt: t}, nil
}

func (s *Store) UpdateProfile(id int64, name string) error {
	_, err := s.db.Exec(`UPDATE profiles SET name = ?, updated_at = ? WHERE id = ?`,
		Squash(name), now(), id)
	return err
}

func (s *Store) DeleteProfile(id int64) error {
	_, err := s.db.Exec(`DELETE FROM profiles WHERE id = ?`, id)
	return err
}

func (s *Store) FindProfile(userID, id int64) (*Profile, error) {
	p, err := scanProfile(s.db.QueryRow(
		`SELECT `+profileCols+` FROM profiles WHERE user_id = ? AND id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

func (s *Store) ListProfiles(userID int64) ([]*Profile, error) {
	rows, err := s.db.Query(`SELECT `+profileCols+` FROM profiles
		WHERE user_id = ? ORDER BY position, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Profile
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) MaxProfilePosition(userID int64) int {
	var v sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM profiles WHERE user_id = ?`, userID).Scan(&v)
	return int(v.Int64)
}

func (s *Store) CountProfiles(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE user_id = ?`, userID).Scan(&n)
	return n
}

// Destroyable mirrors Profile#destroyable?.
func (s *Store) ProfileDestroyable(userID, id int64) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM profiles WHERE user_id = ? AND id != ?`, userID, id).Scan(&n)
	return n > 0
}

// --- sites ---

func scanSite(row interface{ Scan(...any) error }) (*Site, error) {
	s := &Site{}
	var created, updated string
	err := row.Scan(&s.ID, &s.ProfileID, &s.Title, &s.URL, &s.Hint, &s.IconURL, &s.Position, &created, &updated)
	if err != nil {
		return nil, err
	}
	s.CreatedAt, _ = parseTime(created)
	s.UpdatedAt, _ = parseTime(updated)
	return s, nil
}

const siteCols = `id, profile_id, title, url, hint, icon_url, position, created_at, updated_at`

// NormalizeSite applies the Rails normalizations in place.
func NormalizeSite(site *Site) {
	site.Title = Squash(site.Title)
	site.Hint = Squash(site.Hint)
	site.URL = NormalizeURL(site.URL, true)
	if site.Title == "" {
		site.Title = Host(site.URL)
	}
	site.IconURL = NormalizeURL(strings.TrimSpace(site.IconURL), false)
}

func ValidateSiteFields(site *Site) []*ValError {
	var out []*ValError
	switch {
	case site.Title == "":
		out = append(out, &ValError{Attr: "title", Code: "blank"})
	case tooLong(site.Title, MaxSiteTitle):
		out = append(out, &ValError{Attr: "title", Code: "too_long", Count: MaxSiteTitle})
	}
	switch {
	case site.URL == "":
		out = append(out, &ValError{Attr: "url", Code: "blank"})
	case tooLong(site.URL, MaxURL):
		out = append(out, &ValError{Attr: "url", Code: "too_long", Count: MaxURL})
	case !ValidHTTPURL(site.URL):
		out = append(out, &ValError{Attr: "url", Code: "invalid"})
	}
	if tooLong(site.Hint, MaxSiteHint) {
		out = append(out, &ValError{Attr: "hint", Code: "too_long", Count: MaxSiteHint})
	}
	switch {
	case tooLong(site.IconURL, MaxURL):
		out = append(out, &ValError{Attr: "icon_url", Code: "too_long", Count: MaxURL})
	case site.IconURL != "" && !ValidHTTPURL(site.IconURL):
		out = append(out, &ValError{Attr: "icon_url", Code: "invalid"})
	}
	return out
}

func (s *Store) ValidateSite(profileID int64, site *Site, isCreate bool) []*ValError {
	out := ValidateSiteFields(site)
	if isCreate {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM sites WHERE profile_id = ?`, profileID).Scan(&n); err == nil && n >= MaxSitesPerProfile {
			out = append(out, &ValError{Code: "too_many"})
		}
	}
	return out
}

func (s *Store) CreateSite(site *Site) (*Site, error) {
	NormalizeSite(site)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO sites (profile_id, title, url, hint, icon_url, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		site.ProfileID, site.Title, site.URL, site.Hint, site.IconURL, site.Position, ts, ts)
	if err != nil {
		return nil, err
	}
	site.ID, _ = res.LastInsertId()
	t, _ := parseTime(ts)
	site.CreatedAt, site.UpdatedAt = t, t
	return site, nil
}

func (s *Store) UpdateSite(site *Site) error {
	NormalizeSite(site)
	_, err := s.db.Exec(`UPDATE sites SET title = ?, url = ?, hint = ?, icon_url = ?, updated_at = ?
		WHERE id = ?`, site.Title, site.URL, site.Hint, site.IconURL, now(), site.ID)
	return err
}

func (s *Store) DeleteSite(id int64) error {
	_, err := s.db.Exec(`DELETE FROM sites WHERE id = ?`, id)
	return err
}

// FindOwnedSite scopes to the user's sites (through profiles).
func (s *Store) FindOwnedSite(userID, id int64) (*Site, error) {
	site, err := scanSite(s.db.QueryRow(
		`SELECT s.`+strings.ReplaceAll(siteCols, ", ", ", s.")+`
		FROM sites s JOIN profiles p ON p.id = s.profile_id
		WHERE p.user_id = ? AND s.id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return site, err
}

func (s *Store) ListSites(profileID int64) ([]*Site, error) {
	rows, err := s.db.Query(`SELECT `+siteCols+` FROM sites
		WHERE profile_id = ? ORDER BY position, id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Site
	for rows.Next() {
		site, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, site)
	}
	return out, rows.Err()
}

func (s *Store) MaxSitePosition(profileID int64) int {
	var v sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM sites WHERE profile_id = ?`, profileID).Scan(&v)
	return int(v.Int64)
}

func (s *Store) CountSites(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM sites s JOIN profiles p ON p.id = s.profile_id
		WHERE p.user_id = ?`, userID).Scan(&n)
	return n
}

// ReorderSites sets positions by id order, scoping ids to the user.
func (s *Store) ReorderSites(userID int64, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		_, err := tx.Exec(`UPDATE sites SET position = ? WHERE id = ?
			AND profile_id IN (SELECT id FROM profiles WHERE user_id = ?)`, i, id, userID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// --- stack items ---

func scanStackItem(row interface{ Scan(...any) error }) (*StackItem, error) {
	s := &StackItem{}
	var created, updated string
	err := row.Scan(&s.ID, &s.ProfileID, &s.Category, &s.Choice, &s.Origin, &s.Note, &s.URL, &s.IconURL, &s.Position, &created, &updated)
	if err != nil {
		return nil, err
	}
	s.CreatedAt, _ = parseTime(created)
	s.UpdatedAt, _ = parseTime(updated)
	return s, nil
}

const stackCols = `id, profile_id, category, choice, origin, note, url, icon_url, position, created_at, updated_at`

// NormalizeStackItem applies the Rails normalizations in place.
func NormalizeStackItem(item *StackItem) {
	item.Category = Squash(item.Category)
	item.Choice = Squash(item.Choice)
	item.Origin = Squash(item.Origin)
	item.Note = Squash(item.Note)
	item.URL = NormalizeURL(item.URL, false)
	item.IconURL = NormalizeURL(strings.TrimSpace(item.IconURL), false)
}

func ValidateStackItemFields(item *StackItem) []*ValError {
	var out []*ValError
	switch {
	case item.Category == "":
		out = append(out, &ValError{Attr: "category", Code: "blank"})
	case tooLong(item.Category, MaxStackCategory):
		out = append(out, &ValError{Attr: "category", Code: "too_long", Count: MaxStackCategory})
	}
	switch {
	case item.Choice == "":
		out = append(out, &ValError{Attr: "choice", Code: "blank"})
	case tooLong(item.Choice, MaxStackChoice):
		out = append(out, &ValError{Attr: "choice", Code: "too_long", Count: MaxStackChoice})
	}
	if tooLong(item.Origin, MaxStackOrigin) {
		out = append(out, &ValError{Attr: "origin", Code: "too_long", Count: MaxStackOrigin})
	}
	if tooLong(item.Note, MaxStackNote) {
		out = append(out, &ValError{Attr: "note", Code: "too_long", Count: MaxStackNote})
	}
	switch {
	case tooLong(item.URL, MaxURL):
		out = append(out, &ValError{Attr: "url", Code: "too_long", Count: MaxURL})
	case item.URL != "" && !ValidHTTPURL(item.URL):
		out = append(out, &ValError{Attr: "url", Code: "invalid"})
	}
	switch {
	case tooLong(item.IconURL, MaxURL):
		out = append(out, &ValError{Attr: "icon_url", Code: "too_long", Count: MaxURL})
	case item.IconURL != "" && !ValidHTTPURL(item.IconURL):
		out = append(out, &ValError{Attr: "icon_url", Code: "invalid"})
	}
	return out
}

func (s *Store) ValidateStackItem(profileID int64, item *StackItem, isCreate bool) []*ValError {
	out := ValidateStackItemFields(item)
	if isCreate {
		var n int
		if err := s.db.QueryRow(`SELECT COUNT(*) FROM stack_items WHERE profile_id = ?`, profileID).Scan(&n); err == nil && n >= MaxStackPerProfile {
			out = append(out, &ValError{Code: "too_many"})
		}
	}
	return out
}

func (s *Store) CreateStackItem(item *StackItem) (*StackItem, error) {
	NormalizeStackItem(item)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO stack_items
		(profile_id, category, choice, origin, note, url, icon_url, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		item.ProfileID, item.Category, item.Choice, item.Origin, item.Note,
		item.URL, item.IconURL, item.Position, ts, ts)
	if err != nil {
		return nil, err
	}
	item.ID, _ = res.LastInsertId()
	t, _ := parseTime(ts)
	item.CreatedAt, item.UpdatedAt = t, t
	return item, nil
}

func (s *Store) UpdateStackItem(item *StackItem) error {
	NormalizeStackItem(item)
	_, err := s.db.Exec(`UPDATE stack_items
		SET category = ?, choice = ?, origin = ?, note = ?, url = ?, icon_url = ?, updated_at = ?
		WHERE id = ?`, item.Category, item.Choice, item.Origin, item.Note,
		item.URL, item.IconURL, now(), item.ID)
	return err
}

func (s *Store) DeleteStackItem(id int64) error {
	_, err := s.db.Exec(`DELETE FROM stack_items WHERE id = ?`, id)
	return err
}

func (s *Store) FindOwnedStackItem(userID, id int64) (*StackItem, error) {
	item, err := scanStackItem(s.db.QueryRow(
		`SELECT i.`+strings.ReplaceAll(stackCols, ", ", ", i.")+`
		FROM stack_items i JOIN profiles p ON p.id = i.profile_id
		WHERE p.user_id = ? AND i.id = ?`, userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return item, err
}

func (s *Store) ListStackItems(profileID int64) ([]*StackItem, error) {
	rows, err := s.db.Query(`SELECT `+stackCols+` FROM stack_items
		WHERE profile_id = ? ORDER BY position, id`, profileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*StackItem
	for rows.Next() {
		item, err := scanStackItem(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) MaxStackItemPosition(profileID int64) int {
	var v sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(position) FROM stack_items WHERE profile_id = ?`, profileID).Scan(&v)
	return int(v.Int64)
}

func (s *Store) CountStackItems(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM stack_items i JOIN profiles p ON p.id = i.profile_id
		WHERE p.user_id = ?`, userID).Scan(&n)
	return n
}

// ReorderStackItems sets positions by id order, scoping ids to the user.
func (s *Store) ReorderStackItems(userID int64, ids []int64) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for i, id := range ids {
		_, err := tx.Exec(`UPDATE stack_items SET position = ? WHERE id = ?
			AND profile_id IN (SELECT id FROM profiles WHERE user_id = ?)`, i, id, userID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
