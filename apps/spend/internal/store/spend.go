package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/aquasp/kuraspend/internal/money"
)

const (
	// TitleMax mirrors the TITLE_MAX constants.
	TitleMax = 200
	// NotesMax mirrors the NOTES_MAX constants.
	NotesMax = 4000
	// ExpenseCap mirrors Expense::PER_USER_CAP.
	ExpenseCap = 10_000
	// SubscriptionCap mirrors Subscription::PER_USER_CAP.
	SubscriptionCap = 200
	// PaymentDayCap mirrors PaymentDay::PER_USER_CAP.
	PaymentDayCap = 200
)

// Categories mirrors Expense::CATEGORIES.
var Categories = []string{"food", "transport", "home", "health", "leisure", "other"}

// ValidCategory reports whether c is blank or a known category.
func ValidCategory(c string) bool {
	if c == "" {
		return true
	}
	for _, k := range Categories {
		if k == c {
			return true
		}
	}
	return false
}

// --- expenses ---

type Expense struct {
	ID          int64
	UserID      int64
	Title       string
	AmountCents int64
	Currency    string
	SpentOn     string // YYYY-MM-DD
	Category    string
	Notes       string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// ExpensePatch carries writes; nil fields are absent (PATCH semantics) while
// Create treats absent required fields as blank. AmountCents wins over
// Amount when both are present, like the Rails API.
type ExpensePatch struct {
	Title       *string
	Amount      *string // decimal string, parsed with money.ParseCents
	AmountCents *int64
	Currency    *string
	SpentOn     *string
	Category    *string
	Notes       *string
}

const expenseCols = `id, user_id, title, amount_cents, currency, spent_on,
	category, notes, created_at, updated_at`

func scanExpense(row interface{ Scan(...any) error }) (*Expense, error) {
	e := &Expense{}
	var created, updated string
	err := row.Scan(&e.ID, &e.UserID, &e.Title, &e.AmountCents, &e.Currency,
		&e.SpentOn, &e.Category, &e.Notes, &created, &updated)
	if err != nil {
		return nil, err
	}
	e.CreatedAt, _ = parseTime(created)
	e.UpdatedAt, _ = parseTime(updated)
	return e, nil
}

func (s *Store) FindExpense(userID, id int64) (*Expense, error) {
	e, err := scanExpense(s.db.QueryRow(`SELECT `+expenseCols+
		` FROM expenses WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// normalizeExpense applies the patch onto e (Rails before_validation) and
// returns validation failures. On create the zero Expense fails required
// checks, mirroring blank attributes.
func normalizeExpense(e *Expense, p ExpensePatch, amount *int64, amountSet bool) []FieldError {
	var fails []FieldError
	if p.Title != nil {
		e.Title = strings.TrimSpace(*p.Title)
	}
	if p.Currency != nil {
		e.Currency = strings.ToUpper(strings.TrimSpace(*p.Currency))
		if e.Currency == "" {
			e.Currency = "BRL"
		}
	}
	if p.Category != nil {
		e.Category = strings.TrimSpace(*p.Category)
	}
	if p.Notes != nil {
		e.Notes = *p.Notes
	}
	if p.SpentOn != nil {
		e.SpentOn = strings.TrimSpace(*p.SpentOn)
	}
	if amountSet {
		e.AmountCents = *amount
	}

	if e.Title == "" {
		fails = append(fails, FieldError{"title", "blank"})
	} else if utf8.RuneCountInString(e.Title) > TitleMax {
		fails = append(fails, FieldError{"title", "too_long"})
	}
	if e.AmountCents <= 0 {
		fails = append(fails, FieldError{"amount", "greater_than"})
	}
	if !money.ValidCurrency(e.Currency) {
		fails = append(fails, FieldError{"currency", "invalid"})
	}
	if e.SpentOn == "" {
		fails = append(fails, FieldError{"spent_on", "blank"})
	} else if t, err := time.Parse("2006-01-02", e.SpentOn); err != nil {
		fails = append(fails, FieldError{"spent_on", "invalid"})
	} else {
		e.SpentOn = t.Format("2006-01-02")
	}
	if !ValidCategory(e.Category) {
		fails = append(fails, FieldError{"category", "invalid"})
	}
	if utf8.RuneCountInString(e.Notes) > NotesMax {
		fails = append(fails, FieldError{"notes", "too_long"})
	}
	return fails
}

// resolveAmount mirrors the amount/amount_cents writer pair: explicit cents
// win, otherwise the decimal string parses, otherwise absent.
func resolveAmount(pAmount *string, pCents *int64) (cents int64, set bool, invalid bool) {
	if pCents != nil {
		return *pCents, true, false
	}
	if pAmount == nil {
		return 0, false, false
	}
	cents, ok := money.ParseCents(*pAmount)
	if !ok {
		return 0, true, true // unparseable reads as invalid (<= 0 fails)
	}
	return cents, true, false
}

func (s *Store) CreateExpense(userID int64, p ExpensePatch) (*Expense, []FieldError, error) {
	e := &Expense{UserID: userID, Currency: "BRL"}
	cents, set, invalid := resolveAmount(p.Amount, p.AmountCents)
	if invalid {
		cents, set = 0, true
	}
	fails := normalizeExpense(e, p, &cents, set)
	if n, err := s.count("expenses", userID); err != nil {
		return nil, nil, err
	} else if n >= ExpenseCap {
		fails = append(fails, FieldError{"base", "too_many"})
	}
	if len(fails) > 0 {
		return nil, fails, nil
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO expenses
		(user_id, title, amount_cents, currency, spent_on, category, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, e.Title, e.AmountCents, e.Currency, e.SpentOn, e.Category, e.Notes, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	e, err = s.FindExpense(userID, id)
	return e, nil, err
}

func (s *Store) UpdateExpense(userID, id int64, p ExpensePatch) (*Expense, []FieldError, error) {
	e, err := s.FindExpense(userID, id)
	if err != nil {
		return nil, nil, err
	}
	cents, set, invalid := resolveAmount(p.Amount, p.AmountCents)
	if invalid {
		cents, set = 0, true
	}
	if fails := normalizeExpense(e, p, &cents, set); len(fails) > 0 {
		return nil, fails, nil
	}
	_, err = s.db.Exec(`UPDATE expenses SET title = ?, amount_cents = ?, currency = ?,
		spent_on = ?, category = ?, notes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		e.Title, e.AmountCents, e.Currency, e.SpentOn, e.Category, e.Notes, now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	e, err = s.FindExpense(userID, id)
	return e, nil, err
}

func (s *Store) DeleteExpense(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM expenses WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ListExpensesMonth mirrors the API index ordering (spent_on desc, id desc).
func (s *Store) ListExpensesMonth(userID int64, start, end, category string) ([]*Expense, error) {
	query := `SELECT ` + expenseCols + ` FROM expenses
		WHERE user_id = ? AND spent_on >= ? AND spent_on <= ?`
	args := []any{userID, start, end}
	if category != "" {
		query += ` AND category = ?`
		args = append(args, category)
	}
	query += ` ORDER BY spent_on DESC, id DESC`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Expense
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ExportExpenses orders by spent_on, id like MonthsController#export.
func (s *Store) ExportExpenses(userID int64) ([]*Expense, error) {
	rows, err := s.db.Query(`SELECT `+expenseCols+` FROM expenses
		WHERE user_id = ? ORDER BY spent_on, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Expense
	for rows.Next() {
		e, err := scanExpense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CountExpenses(userID int64) int64 {
	n, _ := s.count("expenses", userID)
	return n
}

// --- subscriptions ---

type Subscription struct {
	ID           int64
	UserID       int64
	Title        string
	AmountCents  int64
	Currency     string
	Interval     string // monthly | yearly
	DueDay       *int   // 1-31, optional
	BillingMonth *int   // 1-12, yearly only
	Active       bool
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type SubscriptionPatch struct {
	Title        *string
	Amount       *string
	AmountCents  *int64
	Currency     *string
	Interval     *string
	DueDay       *string // blank clears
	BillingMonth *string // blank clears (yearly normalizes to 1)
	Active       *bool
	Notes        *string
}

const subscriptionCols = `id, user_id, title, amount_cents, currency, interval,
	due_day, billing_month, active, notes, created_at, updated_at`

func scanSubscription(row interface{ Scan(...any) error }) (*Subscription, error) {
	su := &Subscription{}
	var dueDay, billingMonth sql.NullInt64
	var active int64
	var created, updated string
	err := row.Scan(&su.ID, &su.UserID, &su.Title, &su.AmountCents, &su.Currency,
		&su.Interval, &dueDay, &billingMonth, &active, &su.Notes, &created, &updated)
	if err != nil {
		return nil, err
	}
	if dueDay.Valid {
		d := int(dueDay.Int64)
		su.DueDay = &d
	}
	if billingMonth.Valid {
		m := int(billingMonth.Int64)
		su.BillingMonth = &m
	}
	su.Active = active != 0
	su.CreatedAt, _ = parseTime(created)
	su.UpdatedAt, _ = parseTime(updated)
	return su, nil
}

// AppliesIn mirrors Subscription#applies_in?.
func (su *Subscription) AppliesIn(year, month int) bool {
	if !su.Active {
		return false
	}
	if su.Interval == "monthly" {
		return true
	}
	billing := 1
	if su.BillingMonth != nil {
		billing = *su.BillingMonth
	}
	return billing == month
}

// parseDayOfMonth parses an optional 1-31 day; blank means absent.
func parseDayOfMonth(raw string, lo, hi int) (val *int, blank, invalid bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil, true, false
	}
	n, err := strconv.Atoi(trimmed)
	if err != nil || n < lo || n > hi {
		return nil, false, true
	}
	return &n, false, false
}

func normalizeSubscription(su *Subscription, p SubscriptionPatch, amount *int64, amountSet bool) []FieldError {
	var fails []FieldError
	var dueInvalid, billingInvalid bool
	if p.Title != nil {
		su.Title = strings.TrimSpace(*p.Title)
	}
	if p.Currency != nil {
		su.Currency = strings.ToUpper(strings.TrimSpace(*p.Currency))
		if su.Currency == "" {
			su.Currency = "BRL"
		}
	}
	if p.Interval != nil {
		su.Interval = strings.TrimSpace(*p.Interval)
		if su.Interval == "" {
			su.Interval = "monthly"
		}
	}
	if p.DueDay != nil {
		v, _, invalid := parseDayOfMonth(*p.DueDay, 1, 31)
		dueInvalid = invalid
		su.DueDay = v
	}
	if p.BillingMonth != nil {
		v, _, invalid := parseDayOfMonth(*p.BillingMonth, 1, 12)
		billingInvalid = invalid
		su.BillingMonth = v
	}
	// Yearly without a month bills in January; monthly never keeps one.
	if su.Interval == "monthly" {
		su.BillingMonth = nil
	} else if su.Interval == "yearly" && su.BillingMonth == nil && !billingInvalid {
		one := 1
		su.BillingMonth = &one
	}
	if p.Active != nil {
		su.Active = *p.Active
	}
	if p.Notes != nil {
		su.Notes = *p.Notes
	}
	if amountSet {
		su.AmountCents = *amount
	}

	if su.Title == "" {
		fails = append(fails, FieldError{"title", "blank"})
	} else if utf8.RuneCountInString(su.Title) > TitleMax {
		fails = append(fails, FieldError{"title", "too_long"})
	}
	if su.AmountCents <= 0 {
		fails = append(fails, FieldError{"amount", "greater_than"})
	}
	if !money.ValidCurrency(su.Currency) {
		fails = append(fails, FieldError{"currency", "invalid"})
	}
	if su.Interval != "monthly" && su.Interval != "yearly" {
		fails = append(fails, FieldError{"interval", "invalid"})
	}
	if dueInvalid {
		fails = append(fails, FieldError{"due_day", "invalid"})
	}
	if billingInvalid {
		fails = append(fails, FieldError{"billing_month", "invalid"})
	}
	if utf8.RuneCountInString(su.Notes) > NotesMax {
		fails = append(fails, FieldError{"notes", "too_long"})
	}
	return fails
}

func (s *Store) FindSubscription(userID, id int64) (*Subscription, error) {
	su, err := scanSubscription(s.db.QueryRow(`SELECT `+subscriptionCols+
		` FROM subscriptions WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return su, err
}

func (s *Store) CreateSubscription(userID int64, p SubscriptionPatch) (*Subscription, []FieldError, error) {
	su := &Subscription{UserID: userID, Currency: "BRL", Interval: "monthly", Active: true}
	cents, set, invalid := resolveAmount(p.Amount, p.AmountCents)
	if invalid {
		cents, set = 0, true
	}
	fails := normalizeSubscription(su, p, &cents, set)
	if n, err := s.count("subscriptions", userID); err != nil {
		return nil, nil, err
	} else if n >= SubscriptionCap {
		fails = append(fails, FieldError{"base", "too_many"})
	}
	if len(fails) > 0 {
		return nil, fails, nil
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO subscriptions
		(user_id, title, amount_cents, currency, interval, due_day, billing_month, active, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, su.Title, su.AmountCents, su.Currency, su.Interval,
		nullInt(su.DueDay), nullInt(su.BillingMonth), boolInt(su.Active), su.Notes, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	su, err = s.FindSubscription(userID, id)
	return su, nil, err
}

func (s *Store) UpdateSubscription(userID, id int64, p SubscriptionPatch) (*Subscription, []FieldError, error) {
	su, err := s.FindSubscription(userID, id)
	if err != nil {
		return nil, nil, err
	}
	// Switching to monthly clears the stored billing month, like normalize.
	if p.Interval != nil && strings.TrimSpace(*p.Interval) == "monthly" && p.BillingMonth == nil {
		su.BillingMonth = nil
	}
	cents, set, invalid := resolveAmount(p.Amount, p.AmountCents)
	if invalid {
		cents, set = 0, true
	}
	if fails := normalizeSubscription(su, p, &cents, set); len(fails) > 0 {
		return nil, fails, nil
	}
	_, err = s.db.Exec(`UPDATE subscriptions SET title = ?, amount_cents = ?, currency = ?,
		interval = ?, due_day = ?, billing_month = ?, active = ?, notes = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		su.Title, su.AmountCents, su.Currency, su.Interval,
		nullInt(su.DueDay), nullInt(su.BillingMonth), boolInt(su.Active), su.Notes, now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	su, err = s.FindSubscription(userID, id)
	return su, nil, err
}

func (s *Store) DeleteSubscription(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM subscriptions WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ListSubscriptions orders by title, id; activeOnly mirrors ?active=true.
func (s *Store) ListSubscriptions(userID int64, activeOnly bool) ([]*Subscription, error) {
	query := `SELECT ` + subscriptionCols + ` FROM subscriptions WHERE user_id = ?`
	if activeOnly {
		query += ` AND active = 1`
	}
	query += ` ORDER BY title, id`
	rows, err := s.db.Query(query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Subscription
	for rows.Next() {
		su, err := scanSubscription(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, su)
	}
	return out, rows.Err()
}

func (s *Store) CountSubscriptions(userID int64) int64 {
	n, _ := s.count("subscriptions", userID)
	return n
}

// --- payment days ---

type PaymentDay struct {
	ID        int64
	UserID    int64
	Title     string
	DueDay    int
	Active    bool
	Notes     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DueOn mirrors PaymentDay#due_on: the day clamped to the month length.
func (d *PaymentDay) DueOn(year, month int) time.Time {
	day := d.DueDay
	if last := daysInMonth(year, month); day > last {
		day = last
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func daysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

type PaymentDayPatch struct {
	Title  *string
	DueDay *string // blank clears (fails required on create)
	Active *bool
	Notes  *string
}

const paymentDayCols = `id, user_id, title, due_day, active, notes, created_at, updated_at`

func scanPaymentDay(row interface{ Scan(...any) error }) (*PaymentDay, error) {
	d := &PaymentDay{}
	var active int64
	var created, updated string
	err := row.Scan(&d.ID, &d.UserID, &d.Title, &d.DueDay, &active, &d.Notes, &created, &updated)
	if err != nil {
		return nil, err
	}
	d.Active = active != 0
	d.CreatedAt, _ = parseTime(created)
	d.UpdatedAt, _ = parseTime(updated)
	return d, nil
}

func (s *Store) FindPaymentDay(userID, id int64) (*PaymentDay, error) {
	d, err := scanPaymentDay(s.db.QueryRow(`SELECT `+paymentDayCols+
		` FROM payment_days WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return d, err
}

func normalizePaymentDay(d *PaymentDay, p PaymentDayPatch, isCreate bool) []FieldError {
	var fails []FieldError
	if p.Title != nil {
		d.Title = strings.TrimSpace(*p.Title)
	}
	if p.DueDay != nil {
		v, blank, invalid := parseDayOfMonth(*p.DueDay, 1, 31)
		switch {
		case blank:
			d.DueDay = 0
		case invalid:
			d.DueDay = -1
		default:
			d.DueDay = *v
		}
	} else if isCreate {
		d.DueDay = 0
	}
	if p.Active != nil {
		d.Active = *p.Active
	}
	if p.Notes != nil {
		d.Notes = *p.Notes
	}

	if d.Title == "" {
		fails = append(fails, FieldError{"title", "blank"})
	} else if utf8.RuneCountInString(d.Title) > TitleMax {
		fails = append(fails, FieldError{"title", "too_long"})
	}
	switch {
	case d.DueDay == 0:
		fails = append(fails, FieldError{"due_day", "blank"})
	case d.DueDay < 1 || d.DueDay > 31:
		fails = append(fails, FieldError{"due_day", "invalid"})
	}
	if utf8.RuneCountInString(d.Notes) > NotesMax {
		fails = append(fails, FieldError{"notes", "too_long"})
	}
	return fails
}

func (s *Store) CreatePaymentDay(userID int64, p PaymentDayPatch) (*PaymentDay, []FieldError, error) {
	d := &PaymentDay{UserID: userID, Active: true}
	fails := normalizePaymentDay(d, p, true)
	if n, err := s.count("payment_days", userID); err != nil {
		return nil, nil, err
	} else if n >= PaymentDayCap {
		fails = append(fails, FieldError{"base", "too_many"})
	}
	if len(fails) > 0 {
		return nil, fails, nil
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO payment_days
		(user_id, title, due_day, active, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, d.Title, d.DueDay, boolInt(d.Active), d.Notes, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	d, err = s.FindPaymentDay(userID, id)
	return d, nil, err
}

func (s *Store) UpdatePaymentDay(userID, id int64, p PaymentDayPatch) (*PaymentDay, []FieldError, error) {
	d, err := s.FindPaymentDay(userID, id)
	if err != nil {
		return nil, nil, err
	}
	if fails := normalizePaymentDay(d, p, false); len(fails) > 0 {
		return nil, fails, nil
	}
	_, err = s.db.Exec(`UPDATE payment_days SET title = ?, due_day = ?, active = ?,
		notes = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		d.Title, d.DueDay, boolInt(d.Active), d.Notes, now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	d, err = s.FindPaymentDay(userID, id)
	return d, nil, err
}

func (s *Store) DeletePaymentDay(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM payment_days WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ListPaymentDays orders by due_day, id like the Rails export and API.
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

// ListActivePaymentDays orders by due_day, title, id like MonthSummary.
func (s *Store) ListActivePaymentDays(userID int64) ([]*PaymentDay, error) {
	rows, err := s.db.Query(`SELECT `+paymentDayCols+` FROM payment_days
		WHERE user_id = ? AND active = 1 ORDER BY due_day, title, id`, userID)
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

func (s *Store) CountPaymentDays(userID int64) int64 {
	n, _ := s.count("payment_days", userID)
	return n
}

func (s *Store) count(table string, userID int64) (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COUNT(*) FROM `+table+` WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
