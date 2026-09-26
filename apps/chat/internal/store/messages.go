package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"

	StatusPending   = "pending"
	StatusStreaming = "streaming"
	StatusComplete  = "complete"
	StatusFailed    = "failed"
)

type Message struct {
	ID             int64
	ConversationID int64
	Role           string
	Status         string // "" for user rows
	Content        string
	Error          string
	Web            bool
	Deep           bool
	Citations      string // JSON array or ""
	Raw            string // JSON object or ""
	TokenUsage     string // JSON object or ""
	CreatedAt      time.Time
	UpdatedAt      time.Time

	Images    []*Image    // populated by Transcript
	Documents []*Document // populated by Transcript
}

func scanMessage(row interface{ Scan(...any) error }) (*Message, error) {
	m := &Message{}
	var status, content, errStr, citations, raw, usage sql.NullString
	var web, deep int
	var created, updated string
	err := row.Scan(&m.ID, &m.ConversationID, &m.Role, &status, &content, &errStr,
		&web, &deep, &citations, &raw, &usage, &created, &updated)
	if err != nil {
		return nil, err
	}
	m.Status = status.String
	m.Content = content.String
	m.Error = errStr.String
	m.Web = web != 0
	m.Deep = deep != 0
	m.Citations = citations.String
	m.Raw = raw.String
	m.TokenUsage = usage.String
	m.CreatedAt, _ = parseTime(created)
	m.UpdatedAt, _ = parseTime(updated)
	return m, nil
}

const messageCols = `id, conversation_id, role, status, content, error, web,
	deep, citations, raw, token_usage, created_at, updated_at`

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// ErrInflight reports a completion already running in the conversation.
var ErrInflight = errors.New("completion in flight")

// CreateTurn inserts the user row and its pending assistant row atomically.
// The partial unique index is the final arbiter under races.
func (s *Store) CreateTurn(conversationID int64, content string, web, deep bool) (*Message, *Message, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM messages WHERE conversation_id = ?
		AND role = 'assistant' AND status IN ('pending', 'streaming')`, conversationID).Scan(&n); err != nil {
		return nil, nil, err
	}
	if n > 0 {
		return nil, nil, ErrInflight
	}
	ts := now()
	res, err := tx.Exec(`INSERT INTO messages
		(conversation_id, role, content, web, deep, created_at, updated_at)
		VALUES (?, 'user', ?, ?, ?, ?, ?)`, conversationID, nullIfEmpty(content), boolInt(web), boolInt(deep), ts, ts)
	if err != nil {
		return nil, nil, err
	}
	userID, _ := res.LastInsertId()
	res, err = tx.Exec(`INSERT INTO messages
		(conversation_id, role, status, content, created_at, updated_at)
		VALUES (?, 'assistant', 'pending', '', ?, ?)`, conversationID, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	asstID, _ := res.LastInsertId()
	if _, err := tx.Exec(`UPDATE conversations SET updated_at = ? WHERE id = ?`, ts, conversationID); err != nil {
		return nil, nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, nil, err
	}
	user, err := s.GetMessage(userID)
	if err != nil {
		return nil, nil, err
	}
	asst, err := s.GetMessage(asstID)
	if err != nil {
		return nil, nil, err
	}
	return user, asst, nil
}

// DeleteMessage removes one row (rollback path for failed turns).
func (s *Store) DeleteMessage(id int64) error {
	_, err := s.db.Exec(`DELETE FROM messages WHERE id = ?`, id)
	return err
}

func (s *Store) CreateUserMessage(conversationID int64, content string, web, deep bool) (*Message, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO messages
		(conversation_id, role, content, web, deep, created_at, updated_at)
		VALUES (?, 'user', ?, ?, ?, ?, ?)`, conversationID, nullIfEmpty(content), boolInt(web), boolInt(deep), ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_ = s.TouchConversation(conversationID)
	return s.GetMessage(id)
}

// CreateAssistantMessage inserts the pending row. The partial unique index
// rejects a second inflight row per conversation (chat.in_flight).
func (s *Store) CreateAssistantMessage(conversationID int64) (*Message, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO messages
		(conversation_id, role, status, content, created_at, updated_at)
		VALUES (?, 'assistant', 'pending', '', ?, ?)`, conversationID, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	_ = s.TouchConversation(conversationID)
	return s.GetMessage(id)
}

func (s *Store) GetMessage(id int64) (*Message, error) {
	m, err := scanMessage(s.db.QueryRow(
		`SELECT `+messageCols+` FROM messages WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

// Transcript returns visible rows oldest-first with images attached:
// all user rows, inflight/failed assistants, and complete assistants
// that have content.
func (s *Store) Transcript(conversationID int64) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE conversation_id = ?
		AND (role = 'user'
			OR (role = 'assistant' AND status IN ('pending', 'streaming', 'failed'))
			OR (role = 'assistant' AND status = 'complete' AND content IS NOT NULL AND content != ''))
		ORDER BY id`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, m := range out {
		if m.Role != RoleUser {
			continue
		}
		imgs, err := s.ListImages(m.ID)
		if err != nil {
			return nil, err
		}
		m.Images = imgs
		docs, err := s.ListDocuments(m.ID)
		if err != nil {
			return nil, err
		}
		m.Documents = docs
	}
	return out, nil
}

// WindowRows returns chronological rows for the model window: every row
// except excludeID, newer than throughID (0 = all).
func (s *Store) WindowRows(conversationID, excludeID, throughID int64) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE conversation_id = ? AND id != ? AND (? = 0 OR id > ?)
		ORDER BY id`, conversationID, excludeID, throughID, throughID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// CompactRows returns transcript-scope rows at or below the assistant row,
// newer than throughID (0 = all), oldest-first.
func (s *Store) CompactRows(conversationID, assistantID, throughID int64) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE conversation_id = ? AND id <= ? AND (? = 0 OR id > ?)
		AND (role = 'user'
			OR (role = 'assistant' AND status IN ('pending', 'streaming', 'failed'))
			OR (role = 'assistant' AND status = 'complete' AND content IS NOT NULL AND content != ''))
		ORDER BY id`, conversationID, assistantID, throughID, throughID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Message
	for rows.Next() {
		m, err := scanMessage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) InflightExists(conversationID int64) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE conversation_id = ?
		AND role = 'assistant' AND status IN ('pending', 'streaming')`, conversationID).Scan(&n)
	return n > 0, err
}

func (s *Store) FirstUserMessage(conversationID int64) (*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE conversation_id = ? AND role = 'user' ORDER BY id LIMIT 1`, conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	return scanMessage(rows)
}

// LastUserMessageBefore returns the newest user row below id (the turn
// being answered).
func (s *Store) LastUserMessageBefore(conversationID, id int64) (*Message, error) {
	rows, err := s.db.Query(`SELECT `+messageCols+` FROM messages
		WHERE conversation_id = ? AND role = 'user' AND id < ?
		ORDER BY id DESC LIMIT 1`, conversationID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, ErrNotFound
	}
	return scanMessage(rows)
}

// TokenUsages plucks stored assistant usage blobs for cost display.
func (s *Store) TokenUsages(conversationID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT token_usage FROM messages
		WHERE conversation_id = ? AND role = 'assistant' AND token_usage IS NOT NULL`,
		conversationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// SetAssistantStreaming marks the row streaming (touches the conversation,
// like update!).
func (s *Store) SetAssistantStreaming(id int64) error {
	_, err := s.db.Exec(`UPDATE messages SET status = 'streaming', updated_at = ? WHERE id = ?`,
		now(), id)
	return err
}

// WriteStreamContent persists streamed text without touching the
// conversation (mirrors update_columns in flush!).
func (s *Store) WriteStreamContent(id int64, content string) error {
	_, err := s.db.Exec(`UPDATE messages SET content = ?, updated_at = ? WHERE id = ?`,
		content, now(), id)
	return err
}

// Heartbeat bumps updated_at so the stale sweep skips live streams.
func (s *Store) Heartbeat(id int64) error {
	_, err := s.db.Exec(`UPDATE messages SET updated_at = ? WHERE id = ?`, now(), id)
	return err
}

type Completion struct {
	Content    string
	Citations  string // JSON array
	TokenUsage string // JSON object, "" when absent
	Raw        string // JSON object, "" when unchanged
	Error      string // e.g. truncated_repetition
}

// CompleteAssistant finalizes the row and touches the conversation.
func (s *Store) CompleteAssistant(id int64, c Completion) error {
	_, err := s.db.Exec(`UPDATE messages
		SET status = 'complete', content = ?, citations = ?, token_usage = ?,
		    raw = COALESCE(?, raw), error = ?, updated_at = ?
		WHERE id = ?`,
		c.Content, nullIfEmpty(c.Citations), nullIfEmpty(c.TokenUsage),
		nullIfEmpty(c.Raw), nullIfEmpty(c.Error), now(), id)
	return err
}

func (s *Store) FailAssistant(id int64, code string) error {
	_, err := s.db.Exec(`UPDATE messages SET status = 'failed', error = ?, updated_at = ?
		WHERE id = ?`, code, now(), id)
	if err == nil {
		_ = s.touchConversationOf(id)
	}
	return err
}

func (s *Store) ResetForRetry(id int64) error {
	_, err := s.db.Exec(`UPDATE messages
		SET status = 'pending', error = NULL, content = '', updated_at = ? WHERE id = ?`,
		now(), id)
	if err == nil {
		_ = s.touchConversationOf(id)
	}
	return err
}

func (s *Store) touchConversationOf(messageID int64) error {
	var convID int64
	if err := s.db.QueryRow(`SELECT conversation_id FROM messages WHERE id = ?`,
		messageID).Scan(&convID); err != nil {
		return err
	}
	return s.TouchConversation(convID)
}

func (s *Store) MessageExists(id int64) bool {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM messages WHERE id = ?`, id).Scan(&n)
	return n > 0
}

// FailStale marks pending/streaming rows older than staleAfter as failed
// and returns them for broadcast.
func (s *Store) FailStale(olderThan time.Time) ([]*Message, error) {
	rows, err := s.db.Query(`SELECT id FROM messages
		WHERE role = 'assistant' AND status IN ('pending', 'streaming')
		AND updated_at < ?`, formatTime(olderThan))
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var stale []*Message
	for _, id := range ids {
		if err := s.FailAssistant(id, "stale"); err != nil {
			return nil, err
		}
		m, err := s.GetMessage(id)
		if err != nil {
			return nil, err
		}
		stale = append(stale, m)
	}
	return stale, nil
}

// AddUsageCost adds amount to one numeric key inside the token_usage blob,
// creating the blob when absent. Voice turns use it for stt_cost_usd (once,
// at send time) and tts_cost_usd (accumulating across replays).
func (s *Store) AddUsageCost(id int64, key string, amount float64) error {
	m, err := s.GetMessage(id)
	if err != nil {
		return err
	}
	usage := m.UsageMap()
	if usage == nil {
		usage = map[string]any{}
	}
	total := amount
	if cur, ok := usage[key]; ok {
		if f, ok := cur.(float64); ok {
			total += f
		}
	}
	usage[key] = total
	raw, err := json.Marshal(usage)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`UPDATE messages SET token_usage = ?, updated_at = ? WHERE id = ?`,
		string(raw), now(), id)
	return err
}

// UsageMap parses the token_usage blob (nil when absent).
func (m *Message) UsageMap() map[string]any {
	if m.TokenUsage == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(m.TokenUsage), &out); err != nil {
		return nil
	}
	return out
}

// RawMap parses the raw blob (nil when absent).
func (m *Message) RawMap() map[string]any {
	if m.Raw == "" {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(m.Raw), &out); err != nil {
		return nil
	}
	return out
}

func (m *Message) Inflight() bool {
	return m.Role == RoleAssistant && (m.Status == StatusPending || m.Status == StatusStreaming)
}

func (m *Message) Failed() bool {
	return m.Role == RoleAssistant && m.Status == StatusFailed
}
