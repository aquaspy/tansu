package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// PasswordResetTTL is how long a reset link stays valid. The
// auth.reset_text and auth.reset_html strings say "1 hour"; keep them
// in step with this constant.
const PasswordResetTTL = time.Hour

// PasswordReset is a live link for an existing user. The raw token is
// emailed once; only its SHA-256 digest is stored.
type PasswordReset struct {
	UserID    int64
	Locale    string
	ExpiresAt time.Time
	CreatedAt time.Time
}

// PasswordResetRollback restores the previous link when the replacement
// email does not go out. A zero value is a no-op.
type PasswordResetRollback struct {
	userID int64
	digest string
	prior  *resetPrior
}

type resetPrior struct {
	tokenDigest string
	locale      string
	expiresAt   string
	createdAt   string
}

// SavePasswordReset replaces any earlier reset for the same user and
// returns a fresh token. If the caller cannot deliver the email, it
// passes the rollback back so a still-current row is restored or removed
// without touching a newer request.
func (s *Store) SavePasswordReset(userID int64, locale string) (string, PasswordResetRollback, error) {
	raw, digest, err := newOpaqueToken()
	if err != nil {
		return "", PasswordResetRollback{}, err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return "", PasswordResetRollback{}, err
	}
	defer tx.Rollback()

	var prior *resetPrior
	row := resetPrior{}
	err = tx.QueryRow(`SELECT token_digest, locale, expires_at, created_at
		FROM password_resets WHERE user_id = ?`, userID).Scan(
		&row.tokenDigest, &row.locale, &row.expiresAt, &row.createdAt)
	if err == nil {
		prior = &row
	} else if !errors.Is(err, sql.ErrNoRows) {
		return "", PasswordResetRollback{}, err
	}
	ts := now()
	exp := formatTime(time.Now().UTC().Add(PasswordResetTTL))
	_, err = tx.Exec(`INSERT INTO password_resets
		(user_id, token_digest, locale, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(user_id) DO UPDATE SET
			token_digest = excluded.token_digest,
			locale = excluded.locale,
			expires_at = excluded.expires_at,
			created_at = excluded.created_at`,
		userID, digest, normalizePendingLocale(locale), exp, ts)
	if err != nil {
		return "", PasswordResetRollback{}, err
	}
	if err := tx.Commit(); err != nil {
		return "", PasswordResetRollback{}, err
	}
	return raw, PasswordResetRollback{userID: userID, digest: digest, prior: prior}, nil
}

// RollbackPasswordReset undoes SavePasswordReset after a failed send.
// It matches the token digest, so a newer reset for the same user stays.
func (s *Store) RollbackPasswordReset(rb PasswordResetRollback) error {
	if rb.digest == "" {
		return nil
	}
	if rb.prior == nil {
		_, err := s.db.Exec(`DELETE FROM password_resets WHERE user_id = ? AND token_digest = ?`, rb.userID, rb.digest)
		return err
	}
	p := rb.prior
	_, err := s.db.Exec(`UPDATE password_resets SET
		token_digest = ?, locale = ?, expires_at = ?, created_at = ?
		WHERE user_id = ? AND token_digest = ?`,
		p.tokenDigest, p.locale, p.expiresAt, p.createdAt,
		rb.userID, rb.digest)
	return err
}

// FindPasswordResetByToken returns the live reset for token. Expired
// rows are deleted by their digest and reported as not found.
func (s *Store) FindPasswordResetByToken(token string) (*PasswordReset, error) {
	if strings.TrimSpace(token) == "" {
		return nil, ErrNotFound
	}
	digest := tokenDigest(token)
	p := &PasswordReset{}
	var expires, created string
	err := s.db.QueryRow(`SELECT user_id, locale, expires_at, created_at
		FROM password_resets WHERE token_digest = ?`, digest).Scan(
		&p.UserID, &p.Locale, &expires, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	p.ExpiresAt, _ = parseTime(expires)
	p.CreatedAt, _ = parseTime(created)
	if !pendingLive(p.ExpiresAt) {
		_, _ = s.db.Exec(`DELETE FROM password_resets WHERE token_digest = ?`, digest)
		return nil, ErrNotFound
	}
	return p, nil
}

// ConsumePasswordReset sets the password and deletes the reset row, every
// Account session, in-flight auth codes, and access tokens for that user.
// Suite apps keep their own local sessions. A second call, or an expired
// token, returns ErrNotFound.
func (s *Store) ConsumePasswordReset(token, passwordDigest string) error {
	if strings.TrimSpace(token) == "" || passwordDigest == "" {
		return ErrNotFound
	}
	digest := tokenDigest(token)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var userID int64
	var expiresRaw string
	err = tx.QueryRow(`SELECT user_id, expires_at FROM password_resets WHERE token_digest = ?`, digest).
		Scan(&userID, &expiresRaw)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	expires, _ := parseTime(expiresRaw)
	if !pendingLive(expires) {
		if _, err := tx.Exec(`DELETE FROM password_resets WHERE token_digest = ?`, digest); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrNotFound
	}
	res, err := tx.Exec(`UPDATE users SET password_digest = ?, updated_at = ? WHERE id = ?`,
		passwordDigest, now(), userID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.Exec(`DELETE FROM password_resets WHERE token_digest = ?`, digest); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM auth_codes WHERE user_id = ?`, userID); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM access_tokens WHERE user_id = ?`, userID); err != nil {
		return err
	}
	return tx.Commit()
}
