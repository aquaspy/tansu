package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

const (
	// TokenPrefix mirrors ApiToken::PREFIX.
	TokenPrefix = "kura_"
	// TokenCap mirrors ApiToken::PER_USER_CAP.
	TokenCap = 10
	// TokenNameMax mirrors ApiToken::NAME_MAX.
	TokenNameMax = 80
)

var (
	ErrTokenNameBlank = errors.New("name blank")
	ErrTokenTooMany   = errors.New("too many tokens")
)

type APIToken struct {
	ID         int64
	UserID     int64
	Name       string
	Prefix     string
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// GenerateRawToken mirrors ApiToken.generate_for: kura_ + 24 random bytes as
// URL-safe base64 (32 chars).
func GenerateRawToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return TokenPrefix + base64.RawURLEncoding.EncodeToString(buf), nil
}

func DigestToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	const hexdigits = "0123456789abcdef"
	var b [64]byte
	for i, v := range sum {
		b[i*2] = hexdigits[v>>4]
		b[i*2+1] = hexdigits[v&0x0f]
	}
	return string(b[:])
}

func scanToken(row interface{ Scan(...any) error }) (*APIToken, error) {
	t := &APIToken{}
	var lastUsed, created, updated sql.NullString
	err := row.Scan(&t.ID, &t.UserID, &t.Name, &t.Prefix, &lastUsed, &created, &updated)
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		if ts, err := parseTime(lastUsed.String); err == nil {
			t.LastUsedAt = &ts
		}
	}
	t.CreatedAt, _ = parseTime(created.String)
	return t, nil
}

const tokenCols = `id, user_id, name, prefix, last_used_at, created_at, updated_at`

// CreateToken validates the name and per-user cap, then stores the digest.
// It returns the token plus the raw value (shown once).
func (s *Store) CreateToken(userID int64, name string) (*APIToken, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, "", ErrTokenNameBlank
	}
	if len([]rune(name)) > TokenNameMax {
		name = string([]rune(name)[:TokenNameMax])
	}
	var count int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM api_tokens WHERE user_id = ?`,
		userID).Scan(&count); err != nil {
		return nil, "", err
	}
	if count >= TokenCap {
		return nil, "", ErrTokenTooMany
	}
	raw, err := GenerateRawToken()
	if err != nil {
		return nil, "", err
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO api_tokens
		(user_id, name, prefix, token_digest, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		userID, name, raw[:12], DigestToken(strings.TrimSpace(raw)), ts, ts)
	if err != nil {
		return nil, "", err
	}
	id, _ := res.LastInsertId()
	tok := &APIToken{ID: id, UserID: userID, Name: name, Prefix: raw[:12]}
	tok.CreatedAt, _ = parseTime(ts)
	return tok, raw, nil
}

func (s *Store) ListTokens(userID int64) ([]*APIToken, error) {
	rows, err := s.db.Query(`SELECT `+tokenCols+` FROM api_tokens
		WHERE user_id = ? ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*APIToken
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// AuthenticateToken mirrors ApiToken.authenticate.
func (s *Store) AuthenticateToken(raw string) (*APIToken, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrNotFound
	}
	t, err := scanToken(s.db.QueryRow(`SELECT `+tokenCols+` FROM api_tokens
		WHERE token_digest = ?`, DigestToken(raw)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) TouchTokenLastUsed(id int64) error {
	_, err := s.db.Exec(`UPDATE api_tokens SET last_used_at = ? WHERE id = ?`, now(), id)
	return err
}

// FindToken scopes the lookup to the owner, like api_tokens.find.
func (s *Store) FindToken(userID, id int64) (*APIToken, error) {
	t, err := scanToken(s.db.QueryRow(`SELECT `+tokenCols+` FROM api_tokens
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return t, err
}

func (s *Store) DeleteToken(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM api_tokens WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
