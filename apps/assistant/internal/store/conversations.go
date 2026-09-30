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
	Mode                string // assistant (default) or chat; anonymous is never stored
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

const (
	// ModeAssistant is the default persisted chat: sibling-app tools on.
	ModeAssistant = "assistant"
	// ModeChat is a persisted chat with no sibling-app tools.
	ModeChat = "chat"
	// ModeAnonymous is client-only. It must never be written to this table.
	ModeAnonymous = "anonymous"
)

// NormalizeStoredMode keeps assistant or chat. Anything else, including
// anonymous, becomes assistant so a crafted form cannot hide a thread
// from the account or smuggle it into the anonymous path.
func NormalizeStoredMode(mode string) string {
	if strings.TrimSpace(mode) == ModeChat {
		return ModeChat
	}
	return ModeAssistant
}

// AllowsTools reports whether this thread may call sibling apps.
// Empty mode is the pre-migration default: Assistente.
func (c *Conversation) AllowsTools() bool {
	return c != nil && c.Mode != ModeChat && c.Mode != ModeAnonymous
}

func scanConversation(row interface {
	Scan(...any) error
}) (*Conversation, error) {
	c := &Conversation{}
	var summary, token, archived, model, effort, mode, created, updated sql.NullString
	var through sql.NullInt64
	var web, deep int
	err := row.Scan(&c.ID, &c.UserID, &c.Title, &summary, &through, &token, &archived,
		&model, &web, &deep, &effort, &mode, &created, &updated)
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
	c.Mode = mode.String
	if c.Mode == "" {
		c.Mode = ModeAssistant
	}
	if through.Valid {
		c.SummarizedThroughID = through.Int64
	}
	c.CreatedAt, _ = parseTime(created.String)
	c.UpdatedAt, _ = parseTime(updated.String)
	return c, nil
}

const conversationCols = `id, user_id, title, summary, summarized_through_id,
	share_token, archived_at, model, web_search, deep_search, effort,
	mode, created_at, updated_at`

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
	if err != nil {
		return nil, err
	}
	if c.Mode == ModeAnonymous {
		return nil, ErrNotFound
	}
	return c, nil
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
	q := `SELECT ` + conversationCols + ` FROM conversations WHERE user_id = ? AND mode != ?`
	var args []any
	args = append(args, userID, ModeAnonymous)
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

// OpenDraftFor returns the newest blank draft of the user's sticky
// personality (Assistente or Conversa). New chats inherit that choice
// instead of resetting to Assistente.
func (s *Store) OpenDraftFor(userID int64) (*Conversation, error) {
	return s.OpenDraftForMode(userID, s.StickyPersonality(userID))
}

// StickyPersonality is the last personality the user selected. An explicit
// preference on the user row wins; otherwise the newest saved conversation
// supplies it. Anonymous is never a personality.
func (s *Store) StickyPersonality(userID int64) string {
	if mode, ok := s.PreferredPersonality(userID); ok {
		return mode
	}
	return s.LastPersonality(userID)
}

// PreferredPersonality reads users.preferred_mode. ok is false when the
// user has never chosen (empty), so callers can fall back to history.
func (s *Store) PreferredPersonality(userID int64) (string, bool) {
	var mode string
	err := s.db.QueryRow(`SELECT preferred_mode FROM users WHERE id = ?`, userID).Scan(&mode)
	if err != nil || strings.TrimSpace(mode) == "" {
		return "", false
	}
	return NormalizeStoredMode(mode), true
}

// SetPreferredPersonality remembers Assistente or Conversa on the user.
// Anonymous is coerced to Assistente and is not stored as a preference.
func (s *Store) SetPreferredPersonality(userID int64, mode string) error {
	mode = NormalizeStoredMode(mode)
	_, err := s.db.Exec(`UPDATE users SET preferred_mode = ? WHERE id = ?`, mode, userID)
	return err
}

// LastPersonality is the mode of the newest saved conversation.
func (s *Store) LastPersonality(userID int64) string {
	var mode string
	err := s.db.QueryRow(`SELECT mode FROM conversations
		WHERE user_id = ? AND mode != ?
		ORDER BY updated_at DESC, id DESC LIMIT 1`, userID, ModeAnonymous).Scan(&mode)
	if err != nil {
		return ModeAssistant
	}
	return NormalizeStoredMode(mode)
}

// SetDraftMode changes personality on a blank draft in place. A thread
// that already has messages returns ErrNotFound so the caller can open
// a new draft instead of rewriting a saved chat.
func (s *Store) SetDraftMode(userID, id int64, mode string) error {
	mode = NormalizeStoredMode(mode)
	res, err := s.db.Exec(`UPDATE conversations SET mode = ?, updated_at = ?
		WHERE id = ? AND user_id = ? AND title = '' AND share_token IS NULL
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = conversations.id)`,
		mode, now(), id, userID)
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
	return nil
}

// OpenDraftForMode returns the newest blank draft of mode (assistant or
// chat), creating one when missing, and deletes the other blanks.
// New drafts inherit the effort of the user's most recent chat.
// Anonymous is not a stored mode; it is coerced to assistant.
func (s *Store) OpenDraftForMode(userID int64, mode string) (*Conversation, error) {
	mode = NormalizeStoredMode(mode)
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var id int64
	err = tx.QueryRow(`SELECT c.id FROM conversations c
		WHERE c.user_id = ? AND c.mode = ? AND c.title = '' AND c.share_token IS NULL
		AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id = c.id)
		ORDER BY c.updated_at DESC, c.id DESC LIMIT 1`, userID, mode).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		ts := now()
		effort := ""
		_ = tx.QueryRow(`SELECT effort
			FROM conversations WHERE user_id = ?
			ORDER BY updated_at DESC, id DESC LIMIT 1`, userID).
			Scan(&effort)
		res, err := tx.Exec(`INSERT INTO conversations
			(user_id, title, mode, effort, created_at, updated_at)
			VALUES (?, '', ?, ?, ?, ?)`, userID, mode, effort, ts, ts)
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
	Model  string
	Web    bool
	Deep   bool
	Effort string
}

// UpdateConversationSettings stores the sticky state without touching
// updated_at (a flip is not activity).
func (s *Store) UpdateConversationSettings(userID, id int64, st ConversationSettings) error {
	_, err := s.db.Exec(`UPDATE conversations
		SET model = ?, web_search = ?, deep_search = ?, effort = ?
		WHERE id = ? AND user_id = ?`,
		st.Model, boolInt(st.Web), boolInt(st.Deep), st.Effort, id, userID)
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
