package store

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// AgentGrantTTL is how long an exchange code stays valid.
const AgentGrantTTL = 2 * time.Minute

// ErrGrantGone is an unknown, used, or expired exchange code.
var ErrGrantGone = errors.New("grant gone")

// NewAgentCode returns a 32-byte hex code and its SHA-256 hex digest.
func NewAgentCode() (raw, digest string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	raw = hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(raw))
	return raw, hex.EncodeToString(sum[:]), nil
}

// DigestAgentCode is the SHA-256 hex of an exchange code.
func DigestAgentCode(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// SaveAgentGrant stores a single-use code, the PKCE challenge, and the raw
// token that exchange will hand back. The row is deleted on consume.
func (s *Store) SaveAgentGrant(userID int64, codeDigest, challenge, rawToken string, ttl time.Duration) error {
	ts := now()
	exp := formatTime(time.Now().Add(ttl))
	_, err := s.db.Exec(`INSERT INTO agent_grants
		(code_digest, user_id, challenge, token, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, codeDigest, userID, challenge, rawToken, exp, ts)
	return err
}

// AgentGrant is what a successful consume returns.
type AgentGrant struct {
	UserID    int64
	Challenge string
	Token     string
}

// ConsumeAgentGrant deletes the code and returns it. A second call, or an
// expired row, returns ErrGrantGone. An expired row is still deleted.
func (s *Store) ConsumeAgentGrant(codeDigest string) (AgentGrant, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return AgentGrant{}, err
	}
	defer tx.Rollback()
	var g AgentGrant
	var exp string
	err = tx.QueryRow(`SELECT user_id, challenge, token, expires_at FROM agent_grants WHERE code_digest = ?`,
		codeDigest).Scan(&g.UserID, &g.Challenge, &g.Token, &exp)
	if errors.Is(err, sql.ErrNoRows) {
		return AgentGrant{}, ErrGrantGone
	}
	if err != nil {
		return AgentGrant{}, err
	}
	if _, err := tx.Exec(`DELETE FROM agent_grants WHERE code_digest = ?`, codeDigest); err != nil {
		return AgentGrant{}, err
	}
	if err := tx.Commit(); err != nil {
		return AgentGrant{}, err
	}
	until, perr := parseTime(exp)
	if perr != nil || !until.After(time.Now()) {
		return AgentGrant{}, ErrGrantGone
	}
	return g, nil
}

// DeleteStaleAgentGrants revokes the API token named in each expired grant
// and drops the row. An abandoned confirm otherwise leaves the raw token
// in this table.
func (s *Store) DeleteStaleAgentGrants() error {
	cutoff := formatTime(time.Now())
	rows, err := s.db.Query(`SELECT token FROM agent_grants WHERE expires_at <= ?`, cutoff)
	if err != nil {
		return err
	}
	var tokens []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		tokens = append(tokens, raw)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, raw := range tokens {
		if raw == "" {
			continue
		}
		_, _ = s.db.Exec(`DELETE FROM api_tokens WHERE token_digest = ?`, DigestToken(raw))
	}
	_, err = s.db.Exec(`DELETE FROM agent_grants WHERE expires_at <= ?`, cutoff)
	return err
}
