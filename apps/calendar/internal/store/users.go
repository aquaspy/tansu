package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

var ErrNotFound = errors.New("not found")

type User struct {
	ID             int64
	Email          string
	PasswordDigest string
	// AccountSub is the Kura Account subject ("" = standalone-only).
	// Stable across email changes; enforced unique when non-empty.
	AccountSub string
	// Timezone is the cached IANA zone. SSO copies zoneinfo; standalone
	// edits it. Empty and invalid values read back as UTC.
	Timezone         string
	HolidayCountries string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// HolidayPacks mirrors User::HOLIDAY_PACKS (Holidays::CODES).
var HolidayPacks = []string{"BR", "US", "SI", "CZ"}

func validHolidayPack(code string) bool {
	for _, c := range HolidayPacks {
		if c == code {
			return true
		}
	}
	return false
}

// HolidayCountryCodes mirrors User#holiday_country_codes.
func (u *User) HolidayCountryCodes() []string {
	var out []string
	seen := map[string]bool{}
	for _, part := range strings.FieldsFunc(u.HolidayCountries, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n'
	}) {
		code := strings.ToUpper(part)
		if validHolidayPack(code) && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	return out
}

// SetHolidayCountryCodes mirrors User#holiday_country_codes=.
func (u *User) SetHolidayCountryCodes(codes []string) {
	var out []string
	seen := map[string]bool{}
	for _, part := range codes {
		code := strings.ToUpper(strings.TrimSpace(part))
		if validHolidayPack(code) && !seen[code] {
			seen[code] = true
			out = append(out, code)
		}
	}
	u.HolidayCountries = strings.Join(out, ",")
}

// ErrTimezone is an IANA name LoadLocation will not accept.
var ErrTimezone = errors.New("invalid timezone")

// MaxTimezoneLen is the raw form cap, in bytes.
const MaxTimezoneLen = 64

// NormalizeTimezone accepts an IANA name. Empty after trim is UTC.
// Abbreviations, numeric offsets, Local, and anything LoadLocation
// rejects are errors. Etc/UTC is stored as UTC.
func NormalizeTimezone(raw string) (string, error) {
	if len(raw) > MaxTimezoneLen {
		return "", ErrTimezone
	}
	name := strings.TrimSpace(raw)
	if name == "" {
		return "UTC", nil
	}
	if strings.EqualFold(name, "Local") {
		return "", ErrTimezone
	}
	if name[0] == '+' || name[0] == '-' {
		return "", ErrTimezone
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil {
		return "", ErrTimezone
	}
	got := loc.String()
	if got == "" || got == "Local" {
		return "", ErrTimezone
	}
	if got == "UTC" || got == "Etc/UTC" {
		return "UTC", nil
	}
	// Abbreviations such as EST load as legacy zones. Store only
	// region/city names (America/Sao_Paulo) besides UTC.
	if !strings.Contains(got, "/") {
		return "", ErrTimezone
	}
	return got, nil
}

// CanonicalTimezone is the zone to cache from userinfo. Missing and
// invalid values are UTC.
func CanonicalTimezone(raw string) string {
	zone, err := NormalizeTimezone(raw)
	if err != nil {
		return "UTC"
	}
	return zone
}

// LoadZone resolves a stored name. A missing or unknown name is UTC,
// never the process zone.
func LoadZone(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || strings.EqualFold(name, "Local") {
		return time.UTC
	}
	loc, err := time.LoadLocation(name)
	if err != nil || loc == nil || loc.String() == "Local" {
		return time.UTC
	}
	return loc
}

// Zone is this user's location for display and ICS projection.
func (u *User) Zone() *time.Location {
	if u == nil {
		return time.UTC
	}
	return LoadZone(u.Timezone)
}

// NormalizeEmail mirrors the Rails normalizes (strip + downcase).
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// ValidEmail is a light format check (the Rails app uses the mailto regexp).
func ValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 || strings.Contains(email[at+1:], "@") {
		return false
	}
	dot := strings.LastIndex(email, ".")
	return dot > at+1 && dot < len(email)-1 && !strings.Contains(email, " ")
}

func (s *Store) CreateUser(email, passwordDigest string) (*User, error) {
	email = NormalizeEmail(email)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO users (email, password_digest, timezone, created_at, updated_at)
		VALUES (?, ?, 'UTC', ?, ?)`, email, passwordDigest, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &User{ID: id, Email: email, PasswordDigest: passwordDigest,
		Timezone: "UTC", HolidayCountries: "BR", CreatedAt: t, UpdatedAt: t}, nil
}

const userCols = `id, email, password_digest, account_sub, timezone, holiday_countries, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var created, updated string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordDigest, &u.AccountSub, &u.Timezone, &u.HolidayCountries, &created, &updated)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = parseTime(created)
	u.UpdatedAt, _ = parseTime(updated)
	return u, nil
}

func (s *Store) FindUser(id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) FindUserByEmail(email string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ?`,
		NormalizeEmail(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) UpdateUserPassword(id int64, digest string) error {
	_, err := s.db.Exec(`UPDATE users SET password_digest = ?, updated_at = ? WHERE id = ?`,
		digest, now(), id)
	return err
}

// FindUserBySub locates a user by Kura Account subject.
func (s *Store) FindUserBySub(sub string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE account_sub = ?`, sub))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// SetUserSub links an existing user to a Kura Account subject.
func (s *Store) SetUserSub(id int64, sub string) error {
	_, err := s.db.Exec(`UPDATE users SET account_sub = ?, updated_at = ? WHERE id = ?`,
		sub, now(), id)
	return err
}

func (s *Store) UpdateUserTimezone(id int64, zone string) error {
	_, err := s.db.Exec(`UPDATE users SET timezone = ?, updated_at = ? WHERE id = ?`,
		zone, now(), id)
	return err
}

func (s *Store) UpdateUserHolidayCountries(id int64, raw string) error {
	_, err := s.db.Exec(`UPDATE users SET holiday_countries = ?, updated_at = ? WHERE id = ?`,
		raw, now(), id)
	return err
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// IsUniqueViolation reports UNIQUE constraint failures (email, token
// digest) across driver error shapes.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type coder interface{ Code() int }
	var ce coder
	if errors.As(err, &ce) && ce.Code() == 2067 { // SQLITE_CONSTRAINT_UNIQUE
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
