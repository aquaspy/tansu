package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// Session is an opaque login. UnlockedAt == nil means the session is
// locked (same semantics as a blank session[:unlocked_at] in Rails).
type Session struct {
	ID          string
	UserID      int64
	UnlockedAt  *time.Time
	FlashNotice string
	FlashAlert  string
	CreatedAt   time.Time
	LastSeen    time.Time
}

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func scanSession(row interface{ Scan(...any) error }) (*Session, error) {
	s := &Session{}
	var unlocked, notice, alert, created, seen sql.NullString
	err := row.Scan(&s.ID, &s.UserID, &unlocked, &notice, &alert, &created, &seen)
	if err != nil {
		return nil, err
	}
	if unlocked.Valid {
		if t, err := parseTime(unlocked.String); err == nil {
			s.UnlockedAt = &t
		}
	}
	s.FlashNotice = notice.String
	s.FlashAlert = alert.String
	s.CreatedAt, _ = parseTime(created.String)
	s.LastSeen, _ = parseTime(seen.String)
	return s, nil
}

// SessionMaxAge bounds both the login cookie lifetime and the
// server-side stale sweep, so they expire together.
const SessionMaxAge = 30 * 24 * time.Hour

const sessionCols = `id, user_id, unlocked_at, flash_notice, flash_alert,
	created_at, last_seen_at`

func (s *Store) CreateSession(userID int64) (*Session, error) {
	id, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	ts := now()
	_, err = s.db.Exec(`INSERT INTO sessions (id, user_id, unlocked_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)`, id, userID, ts, ts, ts)
	if err != nil {
		return nil, err
	}
	return s.GetSession(id)
}

func (s *Store) GetSession(id string) (*Session, error) {
	if id == "" {
		return nil, ErrNotFound
	}
	sess, err := scanSession(s.db.QueryRow(
		`SELECT `+sessionCols+` FROM sessions WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return sess, err
}

// TouchSession updates last_seen and returns the fresh row.
func (s *Store) TouchSession(id string) (*Session, error) {
	if _, err := s.db.Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, now(), id); err != nil {
		return nil, err
	}
	return s.GetSession(id)
}

func (s *Store) UnlockSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET unlocked_at = ?, last_seen_at = ? WHERE id = ?`,
		now(), now(), id)
	return err
}

func (s *Store) LockSession(id string) error {
	_, err := s.db.Exec(`UPDATE sessions SET unlocked_at = NULL WHERE id = ?`, id)
	return err
}

func (s *Store) DeleteSession(id string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// SetFlash stores a one-shot notice/alert shown on the next page.
func (s *Store) SetFlash(id, notice, alert string) error {
	_, err := s.db.Exec(`UPDATE sessions SET flash_notice = ?, flash_alert = ? WHERE id = ?`,
		nullIfEmpty(notice), nullIfEmpty(alert), id)
	return err
}

// TakeFlash returns the flash and clears it.
func (s *Store) TakeFlash(id string) (notice, alert string, err error) {
	sess, err := s.GetSession(id)
	if err != nil {
		return "", "", err
	}
	_, err = s.db.Exec(`UPDATE sessions SET flash_notice = NULL, flash_alert = NULL WHERE id = ?`, id)
	return sess.FlashNotice, sess.FlashAlert, err
}

// DeleteStaleSessions drops sessions unseen for longer than maxAge.
func (s *Store) DeleteStaleSessions(maxAge time.Duration) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE last_seen_at < ?`,
		formatTime(time.Now().Add(-maxAge)))
	return err
}
