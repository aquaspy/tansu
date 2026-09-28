package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrTimezone is an IANA name LoadLocation will not accept.
var ErrTimezone = errors.New("invalid timezone")

// MaxTimezoneLen is the raw form cap, in bytes.
const MaxTimezoneLen = 64

var ErrNotFound = errors.New("not found")

type User struct {
	ID             int64
	Email          string
	PasswordDigest string
	// Timezone is an IANA name. Existing rows and new signups are UTC
	// until the hub form saves another zone.
	Timezone  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

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
		Timezone: "UTC", CreatedAt: t, UpdatedAt: t}, nil
}

const userCols = `id, email, password_digest, timezone, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var created, updated string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordDigest, &u.Timezone, &created, &updated)
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

// UpdateUserTimezone stores a name NormalizeTimezone already accepted.
func (s *Store) UpdateUserTimezone(id int64, zone string) error {
	_, err := s.db.Exec(`UPDATE users SET timezone = ?, updated_at = ? WHERE id = ?`,
		zone, now(), id)
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
