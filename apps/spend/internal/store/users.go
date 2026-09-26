package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/money"
)

var ErrNotFound = errors.New("not found")

// FieldError is one validation failure; the handler renders it through the
// errors.<model>.<field>.<code> i18n key.
type FieldError struct {
	Field string
	Code  string
}

type User struct {
	ID             int64
	Email          string
	PasswordDigest string
	// AccountSub is the Kura Account subject ("" = standalone-only).
	// Stable across email changes; enforced unique when non-empty.
	AccountSub         string
	FX                 string // raw JSON object of currency -> rate
	HomeCurrency       string
	IncomeCurrency     string
	MonthlyIncomeCents int64
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

// NormalizeEmail mirrors the Rails normalizes (strip + downcase).
func NormalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// ValidEmail is a light format check (the Rails app uses the mailto regexp).
func ValidEmail(email string) bool {
	email = strings.TrimSpace(email)
	at := strings.Index(email, "@")
	if at <= 0 || strings.Contains(email[at+1:], "@") {
		return false
	}
	dot := strings.LastIndex(email, ".")
	return dot > at+1 && dot < len(email)-1 && !strings.Contains(email, " ")
}

func (s *Store) CreateUser(email, passwordDigest string) (*User, error) {
	email = NormalizeEmail(email)
	ts := now()
	res, err := s.db.Exec(`INSERT INTO users
		(email, password_digest, fx, home_currency, income_currency, monthly_income_cents, created_at, updated_at)
		VALUES (?, ?, '{}', 'BRL', 'BRL', 0, ?, ?)`, email, passwordDigest, ts, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &User{ID: id, Email: email, PasswordDigest: passwordDigest,
		FX: "{}", HomeCurrency: "BRL", IncomeCurrency: "BRL",
		CreatedAt: t, UpdatedAt: t}, nil
}

const userCols = `id, email, password_digest, account_sub, fx, home_currency, income_currency,
	monthly_income_cents, created_at, updated_at`

func scanUser(row interface{ Scan(...any) error }) (*User, error) {
	u := &User{}
	var created, updated string
	err := row.Scan(&u.ID, &u.Email, &u.PasswordDigest, &u.AccountSub, &u.FX,
		&u.HomeCurrency, &u.IncomeCurrency, &u.MonthlyIncomeCents, &created, &updated)
	if err != nil {
		return nil, err
	}
	u.CreatedAt, _ = parseTime(created)
	u.UpdatedAt, _ = parseTime(updated)
	return u, nil
}

func (s *Store) FindUser(id int64) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) FindUserByEmail(email string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE email = ?`,
		NormalizeEmail(email)))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) UpdateUserPassword(id int64, digest string) error {
	_, err := s.db.Exec(`UPDATE users SET password_digest = ?, updated_at = ? WHERE id = ?`,
		digest, now(), id)
	return err
}

// FindUserBySub locates a user by Kura Account subject.
func (s *Store) FindUserBySub(sub string) (*User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE account_sub = ?`, sub))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// SetUserSub links an existing user to a Kura Account subject.
func (s *Store) SetUserSub(id int64, sub string) error {
	_, err := s.db.Exec(`UPDATE users SET account_sub = ?, updated_at = ? WHERE id = ?`,
		sub, now(), id)
	return err
}

func (s *Store) ListUsers() ([]*User, error) {
	rows, err := s.db.Query(`SELECT ` + userCols + ` FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// FXHash mirrors User#fx_hash: parsed rates minus the home currency and
// blanks. A corrupt fx column reads as empty.
func FXHash(u *User) map[string]string {
	out := map[string]string{}
	var raw map[string]any
	if err := json.Unmarshal([]byte(u.FX), &raw); err != nil {
		return out
	}
	for key, value := range raw {
		code := strings.ToUpper(key)
		if !money.ValidCurrency(code) || code == u.HomeCurrency {
			continue
		}
		v, _ := value.(string)
		if strings.TrimSpace(v) == "" {
			continue
		}
		out[code] = strings.TrimSpace(v)
	}
	return out
}

// RateFor mirrors User#rate_for: "1" for the home currency, the stored rate
// otherwise, "" when unset.
func RateFor(u *User, code string) string {
	code = strings.ToUpper(code)
	if code == u.HomeCurrency {
		return "1"
	}
	return FXHash(u)[code]
}

// setFX mirrors User#fx_hash=: keep only supported, non-home, non-blank
// pairs. Callers assign the home currency first, like the Rails controller.
func setFX(u *User, incoming map[string]string) {
	pairs := map[string]string{}
	for key, value := range incoming {
		code := strings.ToUpper(key)
		if !money.ValidCurrency(code) || code == strings.ToUpper(u.HomeCurrency) {
			continue
		}
		if strings.TrimSpace(value) == "" {
			continue
		}
		pairs[code] = strings.TrimSpace(value)
	}
	raw, _ := json.Marshal(pairs)
	u.FX = string(raw)
}

// UpdateSettings mirrors SettingsController#update + User validations.
// incomeRaw parses like the Rails monthly_income= writer (blank stays 0).
func (s *Store) UpdateSettings(userID int64, home, incomeRaw, incomeCurrency string, fx map[string]string) (*User, []FieldError, error) {
	u, err := s.FindUser(userID)
	if err != nil {
		return nil, nil, err
	}
	// fx_rates_make_sense normalization first.
	u.HomeCurrency = strings.ToUpper(strings.TrimSpace(home))
	if u.HomeCurrency == "" {
		u.HomeCurrency = "BRL"
	}
	u.IncomeCurrency = strings.ToUpper(strings.TrimSpace(incomeCurrency))
	if u.IncomeCurrency == "" {
		u.IncomeCurrency = u.HomeCurrency
	}
	setFX(u, fx)

	var fails []FieldError
	if !money.ValidCurrency(u.HomeCurrency) {
		fails = append(fails, FieldError{"home_currency", "invalid"})
	}
	if !money.ValidCurrency(u.IncomeCurrency) {
		fails = append(fails, FieldError{"income_currency", "invalid"})
	}
	if income, ok := money.ParseCents(incomeRaw); ok {
		if income < 0 {
			fails = append(fails, FieldError{"income", "negative"})
		} else {
			u.MonthlyIncomeCents = income
		}
	} else {
		u.MonthlyIncomeCents = 0
	}
	for _, value := range FXHash(u) {
		if !money.ValidRate(value) {
			fails = append(fails, FieldError{"fx", "invalid"})
			break
		}
	}
	if len(fails) > 0 {
		return nil, fails, nil
	}
	_, err = s.db.Exec(`UPDATE users SET home_currency = ?, income_currency = ?,
		monthly_income_cents = ?, fx = ?, updated_at = ? WHERE id = ?`,
		u.HomeCurrency, u.IncomeCurrency, u.MonthlyIncomeCents, u.FX, now(), u.ID)
	if err != nil {
		return nil, nil, err
	}
	u, err = s.FindUser(u.ID)
	return u, nil, err
}

// IsUniqueViolation reports UNIQUE constraint failures (email, token
// digest) across driver error shapes.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	type coder interface{ Code() int }
	var ce coder
	if errors.As(err, &ce) && ce.Code() == 2067 { // SQLITE_CONSTRAINT_UNIQUE
		return true
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
