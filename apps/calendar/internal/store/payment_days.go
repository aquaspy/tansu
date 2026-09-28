package store

import (
	"database/sql"
	"errors"
	"strings"
	"time"
)

const (
	// PaymentDayTitleMax matches Spend's title cap.
	PaymentDayTitleMax = 200
	// PaymentDayNotesMax matches Spend's notes cap.
	PaymentDayNotesMax = 4000
)

// PaymentDay is a monthly reminder mirrored from Tansu Spend. Calendar
// does not create these; Spend pushes them and they stay read-only.
type PaymentDay struct {
	ID        int64
	UserID    int64
	SourceKey string
	Title     string
	DueDay    int
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// PaymentDayInput is the sync payload after the handler has checked the key.
type PaymentDayInput struct {
	Title  string
	DueDay int
	Notes  string
}

// ObservedOn reports whether date is this reminder's day in that month.
// A due day past the month's length lands on the last day (31 in February
// is the 28th or 29th), matching Spend.
func (d *PaymentDay) ObservedOn(date time.Time) bool {
	if d.DueDay < 1 || d.DueDay > 31 {
		return false
	}
	last := DaysInMonth(date.Year(), int(date.Month()))
	day := d.DueDay
	if day > last {
		day = last
	}
	return date.Day() == day
}

func validatePaymentDay(in PaymentDayInput) (PaymentDayInput, []FieldError) {
	var errs []FieldError
	fail := func(field, code string) {
		errs = append(errs, FieldError{Model: "payment_day", Field: field, Code: code})
	}
	p := PaymentDayInput{
		Title:  strings.TrimSpace(in.Title),
		DueDay: in.DueDay,
		Notes:  in.Notes,
	}
	if p.Title == "" {
		fail("title", "blank")
	} else if runeLen(p.Title) > PaymentDayTitleMax {
		fail("title", "too_long")
	}
	if p.DueDay < 1 || p.DueDay > 31 {
		fail("due_day", "invalid")
	}
	if runeLen(p.Notes) > PaymentDayNotesMax {
		fail("notes", "too_long")
	}
	return p, errs
}

const paymentDayCols = `id, user_id, source_key, title, due_day, notes, created_at, updated_at`

func scanPaymentDay(row interface{ Scan(...any) error }) (*PaymentDay, error) {
	d := &PaymentDay{}
	var created, updated sql.NullString
	err := row.Scan(&d.ID, &d.UserID, &d.SourceKey, &d.Title, &d.DueDay, &d.Notes, &created, &updated)
	if err != nil {
		return nil, err
	}
	d.CreatedAt, _ = parseTime(created.String)
	d.UpdatedAt, _ = parseTime(updated.String)
	return d, nil
}

// UpsertSyncedPaymentDay creates or updates the reminder imported from
// Spend. sourceKey is stable ("spend:12") and is not shown.
func (s *Store) UpsertSyncedPaymentDay(userID int64, sourceKey string, in PaymentDayInput) (*PaymentDay, []FieldError, error) {
	p, errs := validatePaymentDay(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM payment_days WHERE user_id = ? AND source_key = ?`,
		userID, sourceKey).Scan(&id)
	ts := now()
	if errors.Is(err, sql.ErrNoRows) {
		res, err := s.db.Exec(`INSERT INTO payment_days
			(user_id, source_key, title, due_day, notes, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?)`,
			userID, sourceKey, p.Title, p.DueDay, p.Notes, ts, ts)
		if err != nil {
			return nil, nil, err
		}
		id, _ = res.LastInsertId()
		d, err := s.findPaymentDay(userID, id)
		return d, nil, err
	}
	if err != nil {
		return nil, nil, err
	}
	_, err = s.db.Exec(`UPDATE payment_days SET title = ?, due_day = ?, notes = ?,
		updated_at = ? WHERE id = ? AND user_id = ?`,
		p.Title, p.DueDay, p.Notes, ts, id, userID)
	if err != nil {
		return nil, nil, err
	}
	d, err := s.findPaymentDay(userID, id)
	return d, nil, err
}

// DeleteSyncedPaymentDay removes one imported reminder. A missing row is fine.
func (s *Store) DeleteSyncedPaymentDay(userID int64, sourceKey string) error {
	_, err := s.db.Exec(`DELETE FROM payment_days WHERE user_id = ? AND source_key = ?`, userID, sourceKey)
	return err
}

func (s *Store) findPaymentDay(userID, id int64) (*PaymentDay, error) {
	d, err := scanPaymentDay(s.db.QueryRow(`SELECT `+paymentDayCols+` FROM payment_days
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

// ListPaymentDays returns the user's mirrored reminders, due day then id.
func (s *Store) ListPaymentDays(userID int64) ([]*PaymentDay, error) {
	rows, err := s.db.Query(`SELECT `+paymentDayCols+` FROM payment_days
		WHERE user_id = ? ORDER BY due_day, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PaymentDay
	for rows.Next() {
		d, err := scanPaymentDay(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}
