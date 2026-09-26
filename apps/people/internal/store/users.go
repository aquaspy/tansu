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
	CreatedAt  time.Time
	UpdatedAt  time.Time
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
	res, err := s.db.Exec(`INSERT INTO users (email, password_digest, created_at, updated_at)
		VALUES (?, ?, ?, ?)`, email, passwordDigest, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &User{ID: id, Email: email, PasswordDigest: passwordDigest,
		CreatedAt: t, UpdatedAt: t}, nil
}

const userCols = `id, email, password_digest, account_sub, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var created, updated string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordDigest, &u.AccountSub, &created, &updated)
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
