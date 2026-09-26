package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"time"
)

// PendingSignupTTL is how long a confirmation link stays valid.
const PendingSignupTTL = 24 * time.Hour

// ConfirmAttemptLimit is how many wrong passwords burn one pending signup.
const ConfirmAttemptLimit = 5

// PendingSignup is an account that does not exist yet. The users row is
// inserted only when the inbox token and the chosen password are both
// presented.
type PendingSignup struct {
	Email          string
	PasswordDigest string
	Next           string
	Locale         string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	Attempts       int
}

// PendingRollback restores the previous unconfirmed signup when the
// email for the replacement token does not go out. A zero value is a no-op.
type PendingRollback struct {
	email  string
	digest string
	prior  *pendingPrior
}

type pendingPrior struct {
	passwordDigest string
	tokenDigest    string
	next           string
	locale         string
	expiresAt      string
	createdAt      string
	attempts       int
}

const pendingCols = `email, password_digest, token_digest, next, locale, expires_at, created_at, attempts`

func scanPending(row interface{ Scan(...any) error }) (*PendingSignup, error) {
	p := &PendingSignup{}
	var digest, expires, created string
	if err := row.Scan(&p.Email, &p.PasswordDigest, &digest, &p.Next, &p.Locale, &expires, &created, &p.Attempts); err != nil {
		return nil, err
	}
	p.ExpiresAt, _ = parseTime(expires)
	p.CreatedAt, _ = parseTime(created)
	return p, nil
}

func tokenDigest(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizePendingLocale(locale string) string {
	switch strings.ToLower(strings.TrimSpace(locale)) {
	case "pt", "pt-br":
		return "pt"
	default:
		return "en"
	}
}

func pendingLive(expires time.Time) bool {
	return time.Now().UTC().Before(expires)
}

// SavePendingSignup replaces any earlier unconfirmed request for the
// same email and returns a fresh token. The token is not stored; only
// its SHA-256 digest is. If the caller cannot deliver the email, it
// passes the rollback back so a still-current row is restored or removed
// without touching a newer request.
func (s *Store) SavePendingSignup(email, passwordDigest, next, locale string) (string, PendingRollback, error) {
	email = NormalizeEmail(email)
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", PendingRollback{}, err
	}
	raw := base64.RawURLEncoding.EncodeToString(buf)
	digest := tokenDigest(raw)
	tx, err := s.db.Begin()
	if err != nil {
		return "", PendingRollback{}, err
	}
	defer tx.Rollback()

	var prior *pendingPrior
	row := pendingPrior{}
	err = tx.QueryRow(`SELECT password_digest, token_digest, next, locale, expires_at, created_at, attempts
		FROM pending_signups WHERE email = ?`, email).Scan(
		&row.passwordDigest, &row.tokenDigest, &row.next, &row.locale, &row.expiresAt, &row.createdAt, &row.attempts)
	if err == nil {
		prior = &row
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", PendingRollback{}, err
	}
	ts := now()
	exp := formatTime(time.Now().UTC().Add(PendingSignupTTL))
	_, err = tx.Exec(`INSERT INTO pending_signups
		(email, password_digest, token_digest, next, locale, expires_at, created_at, attempts)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0)
		ON CONFLICT(email) DO UPDATE SET
			password_digest = excluded.password_digest,
			token_digest = excluded.token_digest,
			next = excluded.next,
			locale = excluded.locale,
			expires_at = excluded.expires_at,
			created_at = excluded.created_at,
			attempts = 0`,
		email, passwordDigest, digest, next, normalizePendingLocale(locale), exp, ts)
	if err != nil {
		return "", PendingRollback{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", PendingRollback{}, err
	}
	return raw, PendingRollback{email: email, digest: digest, prior: prior}, nil
}

// RollbackPending undoes SavePendingSignup after a failed send.
// It matches the token digest, so a newer signup for the same email stays.
func (s *Store) RollbackPending(rb PendingRollback) error {
	if rb.digest == "" {
		return nil
	}
	if rb.prior == nil {
		_, err := s.db.Exec(`DELETE FROM pending_signups WHERE email = ? AND token_digest = ?`, rb.email, rb.digest)
		return err
	}
	p := rb.prior
	_, err := s.db.Exec(`UPDATE pending_signups SET
		password_digest = ?, token_digest = ?, next = ?, locale = ?, expires_at = ?, created_at = ?, attempts = ?
		WHERE email = ? AND token_digest = ?`,
		p.passwordDigest, p.tokenDigest, p.next, p.locale, p.expiresAt, p.createdAt, p.attempts,
		rb.email, rb.digest)
	return err
}

// FindPendingByToken returns the live request for token. Expired rows
// are deleted by their digest and reported as not found.
func (s *Store) FindPendingByToken(token string) (*PendingSignup, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrNotFound
	}
	digest := tokenDigest(token)
	p, err := scanPending(s.db.QueryRow(
		`SELECT `+pendingCols+` FROM pending_signups WHERE token_digest = ?`, digest))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !pendingLive(p.ExpiresAt) {
		_, _ = s.db.Exec(`DELETE FROM pending_signups WHERE token_digest = ?`, digest)
		return nil, ErrNotFound
	}
	return p, nil
}

// RecordConfirmFailure counts a wrong password against this token.
// The ConfirmAttemptLimit-th failure deletes the row and reports burned.
func (s *Store) RecordConfirmFailure(token string) (bool, error) {
	if strings.TrimSpace(token) == "" {
		return false, ErrNotFound
	}
	digest := tokenDigest(token)
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var attempts int
	err = tx.QueryRow(`SELECT attempts FROM pending_signups WHERE token_digest = ?`, digest).Scan(&attempts)
	if errors.Is(err, sql.ErrNoRows) {
		return false, ErrNotFound
	}
	if err != nil {
		return false, err
	}
	attempts++
	if attempts >= ConfirmAttemptLimit {
		if _, err := tx.Exec(`DELETE FROM pending_signups WHERE token_digest = ?`, digest); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		return true, nil
	}
	if _, err := tx.Exec(`UPDATE pending_signups SET attempts = ? WHERE token_digest = ?`, attempts, digest); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return false, nil
}

// ConsumePendingSignup inserts the user and deletes the pending row in
// one transaction. A second call, or an expired token, returns ErrNotFound.
// A users.email clash returns the driver's UNIQUE error.
func (s *Store) ConsumePendingSignup(token string) (*User, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrNotFound
	}
	digest := tokenDigest(token)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	p, err := scanPending(tx.QueryRow(
		`SELECT `+pendingCols+` FROM pending_signups WHERE token_digest = ?`, digest))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !pendingLive(p.ExpiresAt) {
		if _, err := tx.Exec(`DELETE FROM pending_signups WHERE token_digest = ?`, digest); err != nil {
			return nil, err
		}
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, ErrNotFound
	}
	ts := now()
	res, err := tx.Exec(`INSERT INTO users (email, password_digest, created_at, updated_at)
		VALUES (?, ?, ?, ?)`, p.Email, p.PasswordDigest, ts, ts)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM pending_signups WHERE token_digest = ?`, digest); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	created, _ := parseTime(ts)
	return &User{
		ID: id, Email: p.Email, PasswordDigest: p.PasswordDigest,
		CreatedAt: created, UpdatedAt: created,
	}, nil
}
