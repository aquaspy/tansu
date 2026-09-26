package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// FolderMaxLength mirrors Note::FOLDER_MAX_LENGTH.
	FolderMaxLength = 80
	// TitleMaxLength mirrors the title truncate (omission "").
	TitleMaxLength = 80
	// PreviewMaxLength mirrors the preview truncate (omission "").
	PreviewMaxLength = 72
)

type Note struct {
	ID         int64
	UserID     int64
	Body       string
	Folder     string
	Title      string
	Preview    string
	ShareToken string // "" when unshared
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

func (n *Note) Shared() bool { return n.ShareToken != "" }

// NoteRow is one list row (mirrors the list_row scope columns).
type NoteRow struct {
	ID        int64
	Title     string
	Preview   string
	Folder    string
	UpdatedAt time.Time
}

// ReservedFolder mirrors Note.reserved_folder? (blank, all, inbox,
// case-insensitive).
func ReservedFolder(name string) bool {
	key := strings.TrimSpace(name)
	if key == "" {
		return true
	}
	return strings.EqualFold(key, "all") || strings.EqualFold(key, "inbox")
}

// NormalizeFolder mirrors Note.normalize_folder.
func NormalizeFolder(name string) string {
	value := truncate(strings.TrimSpace(name), FolderMaxLength)
	if ReservedFolder(value) {
		return ""
	}
	return value
}

func truncate(s string, max int) string {
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max])
}

// TitleAndPreview mirrors assign_title_and_preview: first non-blank line is
// the title, the second (or the first again) is the preview.
func TitleAndPreview(body string) (title, preview string) {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	if len(lines) > 0 {
		title = truncate(lines[0], TitleMaxLength)
		preview = truncate(lines[0], PreviewMaxLength)
	}
	if len(lines) > 1 {
		preview = truncate(lines[1], PreviewMaxLength)
	}
	return title, preview
}

// BodyAfterTitle mirrors Note#body_after_title: leading blank lines and the
// first line are stripped for the shared page.
func BodyAfterTitle(body string) string {
	rest := body
	for {
		line, after, found := strings.Cut(rest, "\n")
		if strings.TrimSpace(strings.Trim(line, " \t\r")) == "" {
			if !found {
				return ""
			}
			rest = after
			continue
		}
		// First non-blank line: drop it and one line break.
		if !found {
			return ""
		}
		return after
	}
}

func scanNote(row interface{ Scan(...any) error }) (*Note, error) {
	n := &Note{}
	var token, created, updated sql.NullString
	err := row.Scan(&n.ID, &n.UserID, &n.Body, &n.Folder, &n.Preview, &token,
		&n.Title, &created, &updated)
	if err != nil {
		return nil, err
	}
	n.ShareToken = token.String
	n.CreatedAt, _ = parseTime(created.String)
	n.UpdatedAt, _ = parseTime(updated.String)
	return n, nil
}

const noteCols = `id, user_id, body, folder, preview, share_token, title,
	created_at, updated_at`

func (s *Store) CreateNote(userID int64, body, folder string) (*Note, error) {
	folder = NormalizeFolder(folder)
	title, preview := TitleAndPreview(body)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO notes
		(user_id, body, folder, preview, title, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, body, folder, preview, title, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.FindNote(userID, id)
}

func (s *Store) FindNote(userID, id int64) (*Note, error) {
	n, err := scanNote(s.db.QueryRow(
		`SELECT `+noteCols+` FROM notes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) FindNoteByShareToken(token string) (*Note, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	n, err := scanNote(s.db.QueryRow(
		`SELECT `+noteCols+` FROM notes WHERE share_token = ?`, token))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return n, err
}

func (s *Store) UpdateNote(userID, id int64, body, folder string) (*Note, error) {
	folder = NormalizeFolder(folder)
	title, preview := TitleAndPreview(body)
	res, err := s.db.Exec(`UPDATE notes SET body = ?, folder = ?, preview = ?,
		title = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		body, folder, preview, title, now(), id, userID)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, ErrNotFound
	}
	return s.FindNote(userID, id)
}

func (s *Store) DeleteNote(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM notes WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// OpenDraft mirrors Note.open_draft_for: reuse the newest blank draft in the
// folder (deleting stray duplicates) or create one.
func (s *Store) OpenDraft(userID int64, folder string) (*Note, error) {
	folder = NormalizeFolder(folder)
	var id int64
	err := s.db.QueryRow(`SELECT id FROM notes WHERE user_id = ? AND folder = ?
		AND title = '' AND share_token IS NULL AND TRIM(body) = ''
		ORDER BY updated_at DESC LIMIT 1`, userID, folder).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return s.CreateNote(userID, "", folder)
	case err != nil:
		return nil, err
	}
	_, _ = s.db.Exec(`DELETE FROM notes WHERE user_id = ? AND folder = ?
		AND title = '' AND share_token IS NULL AND TRIM(body) = '' AND id != ?`,
		userID, folder, id)
	return s.FindNote(userID, id)
}

// DeleteBlankDraftsExcept clears abandoned blank drafts (keeps keepID, or
// none when keepID is 0).
func (s *Store) DeleteBlankDraftsExcept(userID, keepID int64) error {
	_, err := s.db.Exec(`DELETE FROM notes WHERE user_id = ?
		AND title = '' AND share_token IS NULL AND TRIM(body) = '' AND id != ?`,
		userID, keepID)
	return err
}

// ListNotes returns newest-first list rows filtered by folder ("all"/"" for
// everything, "inbox" for the inbox, else the exact name) and query.
func (s *Store) ListNotes(userID int64, folder, query string) ([]*NoteRow, error) {
	return s.ListNotesLimit(userID, folder, query, 0)
}

// ListNotesLimit is ListNotes with a LIMIT (0 means no limit).
func (s *Store) ListNotesLimit(userID int64, folder, query string, limit int) ([]*NoteRow, error) {
	var where strings.Builder
	var args []any
	where.WriteString(`user_id = ?`)
	args = append(args, userID)
	switch folder {
	case "", "all":
	case "inbox":
		where.WriteString(` AND folder = ''`)
	default:
		where.WriteString(` AND folder = ?`)
		args = append(args, folder)
	}
	if q := strings.TrimSpace(query); q != "" {
		like := "%" + escapeLike(q) + "%"
		where.WriteString(` AND (title LIKE ? ESCAPE '\' OR body LIKE ? ESCAPE '\')`)
		args = append(args, like, like)
	}
	sql := `SELECT id, title, preview, folder, updated_at FROM notes
		WHERE ` + where.String() + ` ORDER BY updated_at DESC`
	if limit > 0 {
		sql += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*NoteRow
	for rows.Next() {
		r := &NoteRow{}
		var updated string
		if err := rows.Scan(&r.ID, &r.Title, &r.Preview, &r.Folder, &updated); err != nil {
			return nil, err
		}
		r.UpdatedAt, _ = parseTime(updated)
		out = append(out, r)
	}
	return out, rows.Err()
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	return strings.ReplaceAll(s, `_`, `\_`)
}

// FolderCounts mirrors notes.group(:folder).count ("" is the inbox).
func (s *Store) FolderCounts(userID int64) (map[string]int, error) {
	rows, err := s.db.Query(`SELECT folder, COUNT(*) FROM notes
		WHERE user_id = ? GROUP BY folder`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var name string
		var count int
		if err := rows.Scan(&name, &count); err != nil {
			return nil, err
		}
		out[name] = count
	}
	return out, rows.Err()
}

// RenameFolder mirrors Note.rename_folder. It returns the new name, or
// allowed=false when the rename is not permitted.
func (s *Store) RenameFolder(userID int64, from, to string) (newName string, allowed bool, err error) {
	from = strings.TrimSpace(from)
	to = NormalizeFolder(to)
	if ReservedFolder(from) || ReservedFolder(to) {
		return "", false, nil
	}
	if from != to {
		if _, err := s.db.Exec(`UPDATE notes SET folder = ?, updated_at = ?
			WHERE user_id = ? AND folder = ?`, to, now(), userID, from); err != nil {
			return "", false, err
		}
	}
	return to, true, nil
}

// DeleteFolder deletes every note in key ("inbox" clears the inbox) and
// returns the count. "all"/blank must be rejected by the caller.
func (s *Store) DeleteFolder(userID int64, key string) (int64, error) {
	folder := key
	if key == "inbox" {
		folder = ""
	}
	res, err := s.db.Exec(`DELETE FROM notes WHERE user_id = ? AND folder = ?`,
		userID, folder)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// GenerateShareToken mints (or rotates) the share token, retrying on
// collision like the Rails model.
func (s *Store) GenerateShareToken(userID, id int64) (string, error) {
	for range 5 {
		token, err := randomHex(18)
		if err != nil {
			return "", err
		}
		res, err := s.db.Exec(`UPDATE notes SET share_token = ?, updated_at = ?
			WHERE id = ? AND user_id = ?`, token, now(), id, userID)
		if err != nil {
			if IsUniqueViolation(err) {
				continue
			}
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return "", ErrNotFound
		}
		return token, nil
	}
	return "", errors.New("could not generate a share token")
}

func (s *Store) RevokeShareToken(userID, id int64) error {
	_, err := s.db.Exec(`UPDATE notes SET share_token = NULL, updated_at = ?
		WHERE id = ? AND user_id = ?`, now(), id, userID)
	return err
}

// ExportNotes returns every note oldest-first for the JSON export.
func (s *Store) ExportNotes(userID int64) ([]*Note, error) {
	rows, err := s.db.Query(`SELECT `+noteCols+` FROM notes
		WHERE user_id = ? ORDER BY updated_at`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Note
	for rows.Next() {
		n, err := scanNote(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) CountNotes(userID int64) int64 {
	var n int64
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE user_id = ?`, userID).Scan(&n)
	return n
}
