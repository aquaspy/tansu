package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"time"
)

// AppConnectTTL is how long a link attempt's verifier stays valid.
const AppConnectTTL = 10 * time.Minute

// AppLink is one sibling connection. Token is ciphertext.
type AppLink struct {
	UserID      int64
	App         string
	Token       string
	TokenPrefix string
	Email       string
	AccountSub  string
}

func (s *Store) ListAppLinks(userID int64) ([]AppLink, error) {
	rows, err := s.db.Query(`SELECT user_id, app, token, token_prefix, email, account_sub
		FROM app_links WHERE user_id = ? ORDER BY app`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppLink
	for rows.Next() {
		var l AppLink
		if err := rows.Scan(&l.UserID, &l.App, &l.Token, &l.TokenPrefix, &l.Email, &l.AccountSub); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAppLink(l AppLink) error {
	ts := now()
	_, err := s.db.Exec(`INSERT INTO app_links
		(user_id, app, token, token_prefix, email, account_sub, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, app) DO UPDATE SET
		  token = excluded.token, token_prefix = excluded.token_prefix,
		  email = excluded.email, account_sub = excluded.account_sub, updated_at = excluded.updated_at`,
		l.UserID, l.App, l.Token, l.TokenPrefix, l.Email, l.AccountSub, ts, ts)
	return err
}

func (s *Store) DeleteAppLink(userID int64, app string) error {
	_, err := s.db.Exec(`DELETE FROM app_links WHERE user_id = ? AND app = ?`, userID, app)
	return err
}

// SaveAppConnect stores a PKCE verifier for a link the user just started.
func (s *Store) SaveAppConnect(userID int64, app, state, verifier string) error {
	_, err := s.db.Exec(`INSERT INTO app_connects (state, user_id, app, verifier, created_at)
		VALUES (?, ?, ?, ?, ?)`, state, userID, app, verifier, now())
	return err
}

// ConsumeAppConnect returns the verifier when state belongs to userID and
// is younger than ttl. The row is deleted either way once seen.
func (s *Store) ConsumeAppConnect(state string, userID int64, ttl time.Duration) (app, verifier string, err error) {
	var created string
	var owner int64
	err = s.db.QueryRow(`SELECT user_id, app, verifier, created_at FROM app_connects WHERE state = ?`, state).
		Scan(&owner, &app, &verifier, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	_, _ = s.db.Exec(`DELETE FROM app_connects WHERE state = ?`, state)
	if owner != userID {
		return "", "", ErrNotFound
	}
	ts, perr := parseTime(created)
	if perr != nil || time.Since(ts) > ttl {
		return "", "", ErrNotFound
	}
	return app, verifier, nil
}

// DeleteStaleAppConnects drops link attempts older than ttl. The rows hold
// only random verifiers.
func (s *Store) DeleteStaleAppConnects(ttl time.Duration) error {
	cutoff := formatTime(time.Now().Add(-ttl))
	_, err := s.db.Exec(`DELETE FROM app_connects WHERE created_at < ?`, cutoff)
	return err
}

// NewConnectState returns a 32-byte hex state and a verifier of the same shape.
func NewConnectState() (state, verifier string, err error) {
	buf := make([]byte, 32)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	state = hex.EncodeToString(buf)
	if _, err = rand.Read(buf); err != nil {
		return "", "", err
	}
	return state, hex.EncodeToString(buf), nil
}

// SetToolTrace replaces the JSON trace on an assistant row.
func (s *Store) SetToolTrace(id int64, trace string) error {
	_, err := s.db.Exec(`UPDATE messages SET tool_trace = ?, updated_at = ? WHERE id = ?`,
		nullIfEmpty(trace), now(), id)
	return err
}

// ClaimConfirm moves confirming → pending without touching content.
// A unique-index error means another turn is already in flight.
func (s *Store) ClaimConfirm(id int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE messages SET status = 'pending', updated_at = ?
		WHERE id = ? AND status = 'confirming'`, now(), id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetMessageStatus sets status when it currently equals from. False means
// another request won the race. A unique-index failure is reported as err.
func (s *Store) SetMessageStatus(id int64, from, to, content string) (bool, error) {
	res, err := s.db.Exec(`UPDATE messages SET status = ?, content = ?, updated_at = ?
		WHERE id = ? AND status = ?`, to, content, now(), id, from)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// CancelConfirming completes every confirming row in the conversation,
// appending note to the content so the line stays visible.
func (s *Store) CancelConfirming(conversationID int64, note string) error {
	rows, err := s.db.Query(`SELECT id, content FROM messages
		WHERE conversation_id = ? AND role = 'assistant' AND status = 'confirming'`, conversationID)
	if err != nil {
		return err
	}
	defer rows.Close()
	type row struct {
		id      int64
		content string
	}
	var pending []row
	for rows.Next() {
		var r row
		var content sql.NullString
		if err := rows.Scan(&r.id, &content); err != nil {
			return err
		}
		r.content = content.String
		pending = append(pending, r)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range pending {
		text := r.content
		if text != "" {
			text += "\n\n"
		}
		text += note
		if _, err := s.SetMessageStatus(r.id, StatusConfirming, StatusComplete, text); err != nil {
			return err
		}
	}
	return nil
}
