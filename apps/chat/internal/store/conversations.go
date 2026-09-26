package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

type Conversation struct {
	ID                  int64
	UserID              int64
	Title               string
	Summary             string
	SummarizedThroughID int64 // 0 = none
	ShareToken          string
	ArchivedAt          string
	Model               string // sticky model slug, "" = server default
	WebSearch           bool   // sticky web-search toggle
	DeepSearch          bool   // sticky deep-search modifier
	Effort              string // sticky reasoning effort, "" = server default
	VoiceReadAloud      bool   // sticky: speak every reply
	VoiceAutoSend       bool   // sticky: mic sends right away (false = stage)
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

func scanConversation(row interface {
	Scan(...any) error
}) (*Conversation, error) {
	c := &Conversation{}
	var summary, token, archived, model, effort, created, updated sql.NullString
	var through sql.NullInt64
	var web, deep, readAloud, autoSend int
	err := row.Scan(&c.ID, &c.UserID, &c.Title, &summary, &through, &token, &archived,
		&model, &web, &deep, &effort, &readAloud, &autoSend, &created, &updated)
	if err != nil {
		return nil, err
	}
	c.Summary = summary.String
	c.ShareToken = token.String
	c.ArchivedAt = archived.String
	c.Model = model.String
	c.WebSearch = web != 0
	c.DeepSearch = deep != 0
	c.Effort = effort.String
	c.VoiceReadAloud = readAloud != 0
	c.VoiceAutoSend = autoSend != 0
	if through.Valid {
		c.SummarizedThroughID = through.Int64
	}
	c.CreatedAt, _ = parseTime(created.String)
	c.UpdatedAt, _ = parseTime(updated.String)
	return c, nil
}

const conversationCols = `id, user_id, title, summary, summarized_through_id,
	share_token, archived_at, model, web_search, deep_search, effort,
	voice_read_aloud, voice_auto_send, created_at, updated_at`

func (s *Store) CreateConversation(userID int64) (*Conversation, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO conversations (user_id, title, created_at, updated_at)
		VALUES (?, '', ?, ?)`, userID, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.FindConversation(userID, id)
}

func (s *Store) FindConversation(userID, id int64) (*Conversation, error) {
	c, err := scanConversation(s.db.QueryRow(
		`SELECT `+conversationCols+` FROM conversations WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// GetConversation loads by id without a user scope (completer, events).
func (s *Store) GetConversation(id int64) (*Conversation, error) {
	c, err := scanConversation(s.db.QueryRow(
		`SELECT `+conversationCols+` FROM conversations WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

func (s *Store) FindConversationByShareToken(token string) (*Conversation, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	c, err := scanConversation(s.db.QueryRow(
		`SELECT `+conversationCols+` FROM conversations WHERE share_token = ?`, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return c, err
}

// ListConversations returns the sidebar scope: newest first, optional
// case-insensitive title filter with LIKE metacharacters escaped.
func (s *Store) ListConversations(userID int64, query string) ([]*Conversation, error) {
	q := `SELECT ` + conversationCols + ` FROM conversations WHERE user_id = ?`
	var args []any
	args = append(args, userID)
	if strings.TrimSpace(query) != "" {
		q += ` AND title LIKE ? ESCAPE '\'`
		args = append(args, "%"+escapeLike(query)+"%")
	}
	q += ` ORDER BY updated_at DESC, id DESC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Conversation
	for rows.Next() {
		c, err := scanConversation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// OpenDraftFor returns the newest blank draft (no title, no share, no
// messages), creating one when missing, and deletes the other blanks.
// New drafts inherit the voice toggles and effort of the user's most
// recent chat, so flipping them once sticks for future chats.
func (s *Store) OpenDraftFor(userID int64) (*Conversation, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`SELECT c.id FROM conversations c
		WHERE c.user_id = ? AND c.title = '' AND c.share_token IS NULL
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id)
		ORDER BY c.updated_at DESC, c.id DESC LIMIT 1`, userID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		ts := now()
		readAloud, autoSend, effort := 0, 1, ""
		_ = tx.QueryRow(`SELECT voice_read_aloud, voice_auto_send, effort
			FROM conversations WHERE user_id = ?
			ORDER BY updated_at DESC, id DESC LIMIT 1`, userID).
			Scan(&readAloud, &autoSend, &effort)
		res, err := tx.Exec(`INSERT INTO conversations
			(user_id, title, voice_read_aloud, voice_auto_send, effort, created_at, updated_at)
			VALUES (?, '', ?, ?, ?, ?, ?)`, userID, readAloud, autoSend, effort, ts, ts)
		if err != nil {
			return nil, err
		}
		id, _ = res.LastInsertId()
	} else if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM conversations
		WHERE user_id = ? AND id != ? AND title = '' AND share_token IS NULL
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = conversations.id)`,
		userID, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.FindConversation(userID, id)
}

// DeleteAbandonedDrafts removes blank drafts except keepID (0 = all).
// Mirrors ConversationsController#load_list.
func (s *Store) DeleteAbandonedDrafts(userID, keepID int64) error {
	_, err := s.db.Exec(`DELETE FROM conversations
		WHERE user_id = ? AND id != ? AND title = '' AND share_token IS NULL
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = conversations.id)`,
		userID, keepID)
	return err
}

func (s *Store) UpdateConversationTitle(userID, id int64, title string) error {
	_, err := s.db.Exec(`UPDATE conversations SET title = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`, title, now(), id, userID)
	return err
}

// ConversationSettings is the sticky per-chat state; "" means the
// server default (model, effort).
type ConversationSettings struct {
	Model          string
	Web            bool
	Deep           bool
	Effort         string
	VoiceReadAloud bool
	VoiceAutoSend  bool
}

// UpdateConversationSettings stores the sticky state without touching
// updated_at (a flip is not activity).
func (s *Store) UpdateConversationSettings(userID, id int64, st ConversationSettings) error {
	_, err := s.db.Exec(`UPDATE conversations
		SET model = ?, web_search = ?, deep_search = ?, effort = ?,
			voice_read_aloud = ?, voice_auto_send = ?
		WHERE id = ? AND user_id = ?`,
		st.Model, boolInt(st.Web), boolInt(st.Deep), st.Effort,
		boolInt(st.VoiceReadAloud), boolInt(st.VoiceAutoSend), id, userID)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) TouchConversation(id int64) error {
	_, err := s.db.Exec(`UPDATE conversations SET updated_at = ? WHERE id = ?`, now(), id)
	return err
}

func (s *Store) DeleteConversation(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM conversations WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

func (s *Store) DeleteAllConversations(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM conversations WHERE user_id = ?`, userID)
	return err
}

func (s *Store) CountConversations(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM conversations WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// GenerateShareToken assigns a fresh random token, retrying on collision.
func (s *Store) GenerateShareToken(userID, id int64) (string, error) {
	for range 5 {
		buf := make([]byte, 18)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		token := base64.RawURLEncoding.EncodeToString(buf)
		_, err := s.db.Exec(`UPDATE conversations SET share_token = ?, updated_at = ?
			WHERE id = ? AND user_id = ?`, token, now(), id, userID)
		if err == nil {
			return token, nil
		}
		if !IsUniqueViolation(err) {
			return "", err
		}
	}
	return "", errors.New("could not generate a share token")
}

func (s *Store) RevokeShareToken(userID, id int64) error {
	_, err := s.db.Exec(`UPDATE conversations SET share_token = NULL, updated_at = ?
		WHERE id = ? AND user_id = ?`, now(), id, userID)
	return err
}

func (s *Store) UpdateConversationSummary(id int64, summary string, throughID int64) error {
	_, err := s.db.Exec(`UPDATE conversations SET summary = ?, summarized_through_id = ?, updated_at = ?
		WHERE id = ?`, summary, throughID, now(), id)
	return err
}

// ReclaimSpace runs VACUUM unless a completion is in flight anywhere.
func (s *Store) ReclaimSpace() {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM messages
		WHERE role = 'assistant' AND status IN ('pending', 'streaming')`).Scan(&n); err != nil || n > 0 {
		return
	}
	_, _ = s.db.Exec(`VACUUM`)
}
