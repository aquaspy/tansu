package store

import (
	"database/sql"
	"errors"
	"time"
)

// KuraLoginTTL bounds how long an SSO round-trip may take.
const KuraLoginTTL = 10 * time.Minute

// CreateKuraLogin records a pending SSO attempt: state -> verifier.
func (s *Store) CreateKuraLogin(state, verifier string) error {
	_, err := s.db.Exec(`INSERT INTO kura_logins (state, verifier, created_at)
		VALUES (?, ?, ?)`, state, verifier, now())
	return err
}

// ConsumeKuraLogin returns the verifier for state and deletes the row,
// so a callback URL can only be replayed once.
func (s *Store) ConsumeKuraLogin(state string, ttl time.Duration) (string, error) {
	var verifier, created string
	err := s.db.QueryRow(`SELECT verifier, created_at FROM kura_logins WHERE state = ?`, state).
		Scan(&verifier, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_, _ = s.db.Exec(`DELETE FROM kura_logins WHERE state = ?`, state)
	ts, _ := parseTime(created)
	if time.Since(ts) > ttl {
		return "", ErrNotFound
	}
	return verifier, nil
}

// DeleteStaleKuraLogins drops attempts older than ttl.
func (s *Store) DeleteStaleKuraLogins(ttl time.Duration) error {
	_, err := s.db.Exec(`DELETE FROM kura_logins WHERE created_at < ?`,
		formatTime(time.Now().UTC().Add(-ttl)))
	return err
}
