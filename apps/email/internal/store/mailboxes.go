package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Mailbox is one connected IMAP/SMTP account. PasswordEnc is ciphertext;
// callers decrypt it only at the moment of a server call.
type Mailbox struct {
	ID          int64
	UserID      int64
	DisplayName string
	FromAddress string
	Username    string
	PasswordEnc string
	IMAPHost    string
	IMAPPort    int
	IMAPTLS     string
	SMTPHost    string
	SMTPPort    int
	SMTPTLS     string
	LastOK      *time.Time
	LastError   string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// MailboxInput is a validated connect form. Password empty on update
// means "keep the stored secret".
type MailboxInput struct {
	DisplayName string
	FromAddress string
	Username    string
	Password    string
	IMAPHost    string
	IMAPPort    int
	IMAPTLS     string
	SMTPHost    string
	SMTPPort    int
	SMTPTLS     string
}

const mailboxCols = `id, user_id, display_name, from_address, username, password_enc,
	imap_host, imap_port, imap_tls, smtp_host, smtp_port, smtp_tls,
	last_ok, last_error, created_at, updated_at`

func scanMailbox(row interface{ Scan(...any) error }) (*Mailbox, error) {
	m := &Mailbox{}
	var lastOK, created, updated sql.NullString
	err := row.Scan(&m.ID, &m.UserID, &m.DisplayName, &m.FromAddress, &m.Username, &m.PasswordEnc,
		&m.IMAPHost, &m.IMAPPort, &m.IMAPTLS, &m.SMTPHost, &m.SMTPPort, &m.SMTPTLS,
		&lastOK, &m.LastError, &created, &updated)
	if err != nil {
		return nil, err
	}
	if lastOK.Valid {
		if t, err := parseTime(lastOK.String); err == nil {
			m.LastOK = &t
		}
	}
	m.CreatedAt, _ = parseTime(created.String)
	m.UpdatedAt, _ = parseTime(updated.String)
	return m, nil
}

func (s *Store) ListMailboxes(userID int64) ([]*Mailbox, error) {
	rows, err := s.db.Query(`SELECT `+mailboxCols+` FROM mailboxes WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Mailbox
	for rows.Next() {
		m, err := scanMailbox(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) CountMailboxes(userID int64) int {
	var n int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM mailboxes WHERE user_id = ?`, userID).Scan(&n)
	return n
}

func (s *Store) FindMailbox(userID, id int64) (*Mailbox, error) {
	m, err := scanMailbox(s.db.QueryRow(`SELECT `+mailboxCols+` FROM mailboxes WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return m, err
}

func (s *Store) CreateMailbox(userID int64, in MailboxInput, passwordEnc string) (*Mailbox, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO mailboxes (
		user_id, display_name, from_address, username, password_enc,
		imap_host, imap_port, imap_tls, smtp_host, smtp_port, smtp_tls,
		last_error, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)`,
		userID, in.DisplayName, in.FromAddress, in.Username, passwordEnc,
		in.IMAPHost, in.IMAPPort, in.IMAPTLS, in.SMTPHost, in.SMTPPort, in.SMTPTLS,
		ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.FindMailbox(userID, id)
}

func (s *Store) UpdateMailbox(userID, id int64, in MailboxInput, passwordEnc string) (*Mailbox, error) {
	if passwordEnc == "" {
		cur, err := s.FindMailbox(userID, id)
		if err != nil {
			return nil, err
		}
		passwordEnc = cur.PasswordEnc
	}
	res, err := s.db.Exec(`UPDATE mailboxes SET
		display_name = ?, from_address = ?, username = ?, password_enc = ?,
		imap_host = ?, imap_port = ?, imap_tls = ?, smtp_host = ?, smtp_port = ?, smtp_tls = ?,
		updated_at = ? WHERE id = ? AND user_id = ?`,
		in.DisplayName, in.FromAddress, in.Username, passwordEnc,
		in.IMAPHost, in.IMAPPort, in.IMAPTLS, in.SMTPHost, in.SMTPPort, in.SMTPTLS,
		now(), id, userID)
	if err != nil {
		return nil, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return s.FindMailbox(userID, id)
}

func (s *Store) SetMailboxStatus(userID, id int64, ok bool, errText string) error {
	if ok {
		_, err := s.db.Exec(`UPDATE mailboxes SET last_ok = ?, last_error = '', updated_at = ? WHERE id = ? AND user_id = ?`,
			now(), now(), id, userID)
		return err
	}
	_, err := s.db.Exec(`UPDATE mailboxes SET last_error = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		errText, now(), id, userID)
	return err
}

func (s *Store) DeleteMailbox(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM mailboxes WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// NormalizeTLS accepts tls, starttls, and none.
func NormalizeTLS(raw string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "tls", "ssl", "implicit":
		return "tls", true
	case "starttls":
		return "starttls", true
	case "none", "plain":
		return "none", true
	default:
		return "", false
	}
}
