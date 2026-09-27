package store

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// EventTitleMax mirrors Event::TITLE_MAX.
	EventTitleMax = 200
	// EventBodyMax mirrors Event::BODY_MAX.
	EventBodyMax = 8000
	// EventPerUserCap mirrors Event::PER_USER_CAP.
	EventPerUserCap = 2000
	// EventRangeMax mirrors Event::RANGE_MAX.
	EventRangeMax = 366

	// BirthdayNameMax mirrors Birthday::NAME_MAX.
	BirthdayNameMax = 200
	// BirthdayBodyMax mirrors Birthday::BODY_MAX.
	BirthdayBodyMax = 8000
	// BirthdayPerUserCap mirrors Birthday::PER_USER_CAP.
	BirthdayPerUserCap = 500

	// EmojiMax caps the emoji field in runes (a ZWJ sequence is several).
	EmojiMax = 12
)

// Repeats lists the valid event repeat presets in display order.
var Repeats = []string{"none", "daily", "weekly", "monthly", "yearly"}

// ValidRepeat reports whether code is a known repeat preset.
func ValidRepeat(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "none", "daily", "weekly", "monthly", "yearly":
		return true
	}
	return false
}

// FieldError is one validation failure. Field+Code map to an i18n message
// ("errors.<model>.<field>.<code>"), mirroring full_messages.
type FieldError struct {
	Model string
	Field string
	Code  string
}

// MessageKey returns the i18n lookup key for this failure.
func (e FieldError) MessageKey() string {
	return "errors." + e.Model + "." + e.Field + "." + e.Code
}

var (
	dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	timeRe = regexp.MustCompile(`^(\d{1,2}):(\d{2})$`)
)

// ParseDate strictly parses YYYY-MM-DD (like Date.iso8601).
func ParseDate(s string) (time.Time, bool) {
	if !dateRe.MatchString(s) {
		return time.Time{}, false
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// NormalizeClock parses H:MM / HH:MM and returns zero-padded HH:MM.
// Invalid input returns "", mirroring the Rails time column type-cast
// (garbage becomes nil, then fails presence).
func NormalizeClock(s string) string {
	m := timeRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return ""
	}
	h, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	if h > 23 || min > 59 {
		return ""
	}
	return time.Date(0, 1, 1, h, min, 0, 0, time.UTC).Format("15:04")
}

// DaysInMonth mirrors Time.days_in_month.
func DaysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

// Event dates are YYYY-MM-DD strings so range comparisons stay lexical.
// StartsAt/EndsAt are HH:MM or "" (NULL).
type Event struct {
	ID          int64
	UserID      int64
	Title       string
	Body        string
	AllDay      bool
	StartsOn    string
	EndsOn      string
	StartsAt    string
	EndsAt      string
	Emoji       string
	Repeat      string // one of Repeats; "none" means one-shot
	RepeatUntil string // YYYY-MM-DD or "" (unbounded); bounds starts
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Repeating reports whether the event is a series rather than one-shot.
func (e *Event) Repeating() bool { return e.Repeat != "" && e.Repeat != "none" }

// EventInput is the raw form/API payload, mirroring event_params.
type EventInput struct {
	Title       string
	Body        string
	AllDay      bool
	StartsOn    string
	EndsOn      string
	StartsAt    string
	EndsAt      string
	Emoji       string
	Repeat      string
	RepeatUntil string
}

// normalize mirrors Event#normalize: strip the title, default ends_on,
// drop times for all-day events.
func (in EventInput) normalize() EventInput {
	in.Title = strings.TrimSpace(in.Title)
	in.StartsOn = strings.TrimSpace(in.StartsOn)
	in.EndsOn = strings.TrimSpace(in.EndsOn)
	if _, ok := ParseDate(in.StartsOn); !ok {
		in.StartsOn = ""
	}
	if _, ok := ParseDate(in.EndsOn); !ok {
		in.EndsOn = ""
	}
	if in.EndsOn == "" && in.StartsOn != "" {
		in.EndsOn = in.StartsOn
	}
	if in.AllDay {
		in.StartsAt, in.EndsAt = "", ""
	} else {
		in.StartsAt = NormalizeClock(in.StartsAt)
		in.EndsAt = NormalizeClock(in.EndsAt)
	}
	in.Emoji = strings.TrimSpace(in.Emoji)
	in.Repeat = strings.ToLower(strings.TrimSpace(in.Repeat))
	if in.Repeat == "" {
		in.Repeat = "none"
	}
	in.RepeatUntil = strings.TrimSpace(in.RepeatUntil)
	if in.Repeat == "none" {
		in.RepeatUntil = ""
	}
	return in
}

// ValidateEvent mirrors the Event model validations (without the per-user
// cap, which needs the database). It returns the normalized input.
func ValidateEvent(in EventInput) (EventInput, []FieldError) {
	in = in.normalize()
	var errs []FieldError
	fail := func(field, code string) {
		errs = append(errs, FieldError{Model: "event", Field: field, Code: code})
	}
	if in.Title == "" {
		fail("title", "blank")
	} else if runeLen(in.Title) > EventTitleMax {
		fail("title", "too_long")
	}
	if runeLen(in.Body) > EventBodyMax {
		fail("body", "too_long")
	}
	if in.StartsOn == "" {
		fail("starts_on", "blank")
	}
	if in.EndsOn == "" {
		fail("ends_on", "blank")
	}
	if in.StartsOn != "" && in.EndsOn != "" {
		if in.EndsOn < in.StartsOn {
			fail("ends_on", "before_start")
		} else {
			from, _ := ParseDate(in.StartsOn)
			to, _ := ParseDate(in.EndsOn)
			if int(to.Sub(from).Hours()/24) > EventRangeMax {
				fail("ends_on", "too_long")
			}
		}
	}
	if !in.AllDay {
		if in.StartsAt == "" {
			fail("starts_at", "blank")
		} else if in.EndsAt != "" && in.EndsOn == in.StartsOn && in.EndsAt < in.StartsAt {
			fail("ends_at", "before_start")
		}
	}
	if runeLen(in.Emoji) > EmojiMax {
		fail("emoji", "too_long")
	}
	if !ValidRepeat(in.Repeat) {
		fail("repeat", "invalid")
	}
	if in.RepeatUntil != "" {
		if _, ok := ParseDate(in.RepeatUntil); !ok {
			fail("repeat_until", "invalid")
		} else if in.StartsOn != "" && in.RepeatUntil < in.StartsOn {
			fail("repeat_until", "before_start")
		}
	}
	return in, errs
}

// TimeLabel mirrors Event#time_label: "14:30" or "14:30–15:00", "" for
// all-day events.
func (e *Event) TimeLabel() string {
	if e.AllDay || e.StartsAt == "" {
		return ""
	}
	if e.EndsAt == "" || e.EndsAt == e.StartsAt {
		return e.StartsAt
	}
	return e.StartsAt + "–" + e.EndsAt
}

const eventCols = `id, user_id, title, body, all_day, starts_on, ends_on,
	starts_at, ends_at, emoji, repeat, repeat_until, created_at, updated_at`

func scanEvent(row interface{ Scan(...any) error }) (*Event, error) {
	e := &Event{}
	var allDay int
	var startsAt, endsAt, until, created, updated sql.NullString
	err := row.Scan(&e.ID, &e.UserID, &e.Title, &e.Body, &allDay, &e.StartsOn,
		&e.EndsOn, &startsAt, &endsAt, &e.Emoji, &e.Repeat, &until, &created, &updated)
	if err != nil {
		return nil, err
	}
	e.AllDay = allDay != 0
	e.StartsAt, e.EndsAt = startsAt.String, endsAt.String
	e.RepeatUntil = until.String
	e.CreatedAt, _ = parseTime(created.String)
	e.UpdatedAt, _ = parseTime(updated.String)
	return e, nil
}

func (s *Store) countEvents(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// CountEvents reports the user's event total (CLI users listing).
func (s *Store) CountEvents(userID int64) int {
	n, _ := s.countEvents(userID)
	return n
}

// CreateEvent validates (including the per-user cap) and inserts.
func (s *Store) CreateEvent(userID int64, in EventInput) (*Event, []FieldError, error) {
	in, errs := ValidateEvent(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	n, err := s.countEvents(userID)
	if err != nil {
		return nil, nil, err
	}
	if n >= EventPerUserCap {
		return nil, []FieldError{{Model: "event", Field: "base", Code: "too_many"}}, nil
	}
	allDay := 0
	if in.AllDay {
		allDay = 1
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO events
		(user_id, title, body, all_day, starts_on, ends_on, starts_at, ends_at,
		emoji, repeat, repeat_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, in.Title, in.Body, allDay, in.StartsOn, in.EndsOn,
		nullIfEmpty(in.StartsAt), nullIfEmpty(in.EndsAt),
		in.Emoji, in.Repeat, nullIfEmpty(in.RepeatUntil), ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	e, err := s.FindEvent(userID, id)
	return e, nil, err
}

// UpdateEvent validates and updates a record owned by the user.
func (s *Store) UpdateEvent(userID, id int64, in EventInput) (*Event, []FieldError, error) {
	in, errs := ValidateEvent(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if _, err := s.FindEvent(userID, id); err != nil {
		return nil, nil, err
	}
	allDay := 0
	if in.AllDay {
		allDay = 1
	}
	_, err := s.db.Exec(`UPDATE events SET title = ?, body = ?, all_day = ?,
		starts_on = ?, ends_on = ?, starts_at = ?, ends_at = ?,
		emoji = ?, repeat = ?, repeat_until = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		in.Title, in.Body, allDay, in.StartsOn, in.EndsOn,
		nullIfEmpty(in.StartsAt), nullIfEmpty(in.EndsAt),
		in.Emoji, in.Repeat, nullIfEmpty(in.RepeatUntil), now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	e, err := s.FindEvent(userID, id)
	return e, nil, err
}

// FindEvent scopes the lookup to the owner (a stranger's id is a 404,
// like current_user.events.find).
func (s *Store) FindEvent(userID, id int64) (*Event, error) {
	e, err := scanEvent(s.db.QueryRow(`SELECT `+eventCols+` FROM events
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return e, err
}

// EventsInRange mirrors the in_range scope (overlap ordering included).
func (s *Store) EventsInRange(userID int64, from, to string) ([]*Event, error) {
	rows, err := s.db.Query(`SELECT `+eventCols+` FROM events
		WHERE user_id = ? AND starts_on <= ? AND ends_on >= ?
		ORDER BY starts_on, id`, userID, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// ListRepeatingEvents returns the user's series (repeat != 'none') for
// occurrence expansion. One-shots keep using EventsInRange.
func (s *Store) ListRepeatingEvents(userID int64) ([]*Event, error) {
	rows, err := s.db.Query(`SELECT `+eventCols+` FROM events
		WHERE user_id = ? AND repeat <> 'none' ORDER BY starts_on, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// EventsForExport mirrors the export ordering (starts_on, id).
func (s *Store) EventsForExport(userID int64) ([]*Event, error) {
	rows, err := s.db.Query(`SELECT `+eventCols+` FROM events
		WHERE user_id = ? ORDER BY starts_on, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) DeleteEvent(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM events WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ---------------------------------------------------------------------------
// Birthdays
// ---------------------------------------------------------------------------

// daysInMonth mirrors Birthday::DAYS_IN_MONTH (Feb allows 29; leap
// birthdays are observed on Feb 28 in common years).
var daysInMonth = [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

type Birthday struct {
	ID        int64
	UserID    int64
	Name      string
	Month     int
	Day       int
	Year      int // 0 when unknown
	Body      string
	Emoji     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// BirthdayInput is the raw form/API payload, mirroring birthday_params.
type BirthdayInput struct {
	Name  string
	Month string
	Day   string
	Year  string
	Body  string
	Emoji string
}

type birthdayParsed struct {
	Name       string
	Month, Day int
	Year       int // 0 when unknown
	Body       string
	Emoji      string
}

func parseBirthdayInput(in BirthdayInput) (birthdayParsed, []FieldError) {
	var errs []FieldError
	fail := func(field, code string) {
		errs = append(errs, FieldError{Model: "birthday", Field: field, Code: code})
	}
	p := birthdayParsed{Name: strings.TrimSpace(in.Name), Body: in.Body,
		Emoji: strings.TrimSpace(in.Emoji)}
	if p.Name == "" {
		fail("name", "blank")
	} else if runeLen(p.Name) > BirthdayNameMax {
		fail("name", "too_long")
	}
	if runeLen(p.Body) > BirthdayBodyMax {
		fail("body", "too_long")
	}
	if runeLen(p.Emoji) > EmojiMax {
		fail("emoji", "too_long")
	}
	month, err := strconv.Atoi(strings.TrimSpace(in.Month))
	if err != nil || month < 1 || month > 12 {
		fail("month", "range")
	} else {
		p.Month = month
	}
	day, err := strconv.Atoi(strings.TrimSpace(in.Day))
	if err != nil || day < 1 || day > 31 {
		fail("day", "range")
	} else {
		p.Day = day
	}
	if strings.TrimSpace(in.Year) != "" {
		year, err := strconv.Atoi(strings.TrimSpace(in.Year))
		if err != nil || year < 1900 || year > 2100 {
			fail("year", "range")
		} else {
			p.Year = year
		}
	}
	// day_exists: the day must exist in that month (Feb 29 allowed).
	if p.Month >= 1 && p.Month <= 12 && p.Day >= 1 && p.Day <= daysInMonth[p.Month] {
		// ok
	} else if p.Month != 0 && p.Day != 0 {
		fail("day", "invalid")
	}
	return p, errs
}

// ValidateBirthday mirrors the Birthday model validations (without the
// per-user cap).
func ValidateBirthday(in BirthdayInput) []FieldError {
	_, errs := parseBirthdayInput(in)
	return errs
}

// EventInputFromBirthday turns a calendar-typed birthday into a yearly
// all-day event so the date, name, emoji, notes, and birth year stay.
// Feb 29 in a non-leap year anchors on 2000 (a leap year); the original
// year is kept in the notes. People-synced rows are not passed here.
func EventInputFromBirthday(in BirthdayInput) (EventInput, []FieldError) {
	p, errs := parseBirthdayInput(in)
	if len(errs) > 0 {
		return EventInput{}, errs
	}
	year := p.Year
	if year == 0 {
		year = 2000
	}
	day := p.Day
	if day > DaysInMonth(year, p.Month) {
		if p.Month == 2 && p.Day == 29 {
			year = 2000
			day = 29
		} else {
			day = DaysInMonth(year, p.Month)
		}
	}
	date := fmt.Sprintf("%04d-%02d-%02d", year, p.Month, day)
	body := p.Body
	if p.Year != 0 && p.Year != year {
		note := strconv.Itoa(p.Year)
		switch {
		case body == "":
			body = note
		case runeLen(note)+1+runeLen(body) <= EventBodyMax:
			body = note + "\n" + body
		}
	}
	return EventInput{
		Title: p.Name, Body: body, AllDay: true,
		StartsOn: date, EndsOn: date,
		Emoji: p.Emoji, Repeat: "yearly",
	}, nil
}

// migrateLocalBirthdays copies birthdays that were typed in Calendar
// (no People source key) onto yearly events, then deletes those rows.
// Synced rows stay so People can keep updating them.
func migrateLocalBirthdays(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, user_id, name, month, day, IFNULL(year, 0), body, emoji, created_at, updated_at
		FROM birthdays WHERE source_key = ''`)
	if err != nil {
		return err
	}
	type row struct {
		id, userID       int64
		name             string
		month, day, yr   int
		body, emoji      string
		created, updated string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userID, &r.name, &r.month, &r.day, &r.yr, &r.body, &r.emoji, &r.created, &r.updated); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()
	if len(list) == 0 {
		return nil
	}
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range list {
		in, errs := EventInputFromBirthday(BirthdayInput{
			Name: r.name, Month: strconv.Itoa(r.month), Day: strconv.Itoa(r.day),
			Year: zeroYear(r.yr), Body: r.body, Emoji: r.emoji,
		})
		if len(errs) > 0 {
			continue
		}
		if _, err := tx.Exec(`INSERT INTO events
			(user_id, title, body, all_day, starts_on, ends_on, emoji, repeat, created_at, updated_at)
			VALUES (?, ?, ?, 1, ?, ?, ?, 'yearly', ?, ?)`,
			r.userID, in.Title, in.Body, in.StartsOn, in.EndsOn, in.Emoji, r.created, r.updated); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM birthdays WHERE id = ?`, r.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func zeroYear(year int) string {
	if year == 0 {
		return ""
	}
	return strconv.Itoa(year)
}

// ObservedOn mirrors Birthday#observed_on? (Feb 29 falls on Feb 28 in
// common years).
func (b *Birthday) ObservedOn(date time.Time) bool {
	if b.Month == 2 && b.Day == 29 {
		if date.Month() == time.February && date.Day() == 29 {
			return true
		}
		return date.Month() == time.February && date.Day() == 28 &&
			DaysInMonth(date.Year(), 2) == 28
	}
	return int(date.Month()) == b.Month && date.Day() == b.Day
}

// AgeIn mirrors Birthday#age_in (ok == false when the year is unknown).
func (b *Birthday) AgeIn(year int) (int, bool) {
	if b.Year == 0 {
		return 0, false
	}
	return year - b.Year, true
}

// LabelOn mirrors Birthday#label_on ("Eben (36)").
func (b *Birthday) LabelOn(date time.Time) string {
	if age, ok := b.AgeIn(date.Year()); ok && age >= 0 {
		return b.Name + " (" + strconv.Itoa(age) + ")"
	}
	return b.Name
}

const birthdayCols = `id, user_id, name, month, day, year, body, emoji, created_at, updated_at`

func scanBirthday(row interface{ Scan(...any) error }) (*Birthday, error) {
	b := &Birthday{}
	var year sql.NullInt64
	var created, updated sql.NullString
	err := row.Scan(&b.ID, &b.UserID, &b.Name, &b.Month, &b.Day, &year,
		&b.Body, &b.Emoji, &created, &updated)
	if err != nil {
		return nil, err
	}
	if year.Valid {
		b.Year = int(year.Int64)
	}
	b.CreatedAt, _ = parseTime(created.String)
	b.UpdatedAt, _ = parseTime(updated.String)
	return b, nil
}

func nullIfZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func (s *Store) countBirthdays(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM birthdays WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// CountBirthdays reports the user's birthday total (CLI users listing).
func (s *Store) CountBirthdays(userID int64) int {
	n, _ := s.countBirthdays(userID)
	return n
}

// UpsertSyncedBirthday creates or updates the birthday imported from
// another Tansu app. sourceKey is stable ("people:12") and is not shown.
func (s *Store) UpsertSyncedBirthday(userID int64, sourceKey string, in BirthdayInput) (*Birthday, []FieldError, error) {
	p, errs := parseBirthdayInput(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	var id int64
	err := s.db.QueryRow(`SELECT id FROM birthdays WHERE user_id = ? AND source_key = ?`,
		userID, sourceKey).Scan(&id)
	ts := now()
	if errors.Is(err, sql.ErrNoRows) {
		res, err := s.db.Exec(`INSERT INTO birthdays
			(user_id, name, month, day, year, body, emoji, source_key, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			userID, p.Name, p.Month, p.Day, nullIfZero(p.Year), p.Body, p.Emoji, sourceKey, ts, ts)
		if err != nil {
			return nil, nil, err
		}
		id, _ = res.LastInsertId()
		b, err := s.FindBirthday(userID, id)
		return b, nil, err
	}
	if err != nil {
		return nil, nil, err
	}
	_, err = s.db.Exec(`UPDATE birthdays SET name = ?, month = ?, day = ?, year = ?,
		emoji = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		p.Name, p.Month, p.Day, nullIfZero(p.Year), p.Emoji, ts, id, userID)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.FindBirthday(userID, id)
	return b, nil, err
}

// DeleteSyncedBirthday removes one imported birthday. A missing row is fine.
func (s *Store) DeleteSyncedBirthday(userID int64, sourceKey string) error {
	_, err := s.db.Exec(`DELETE FROM birthdays WHERE user_id = ? AND source_key = ?`, userID, sourceKey)
	return err
}

// CreateBirthday validates (including the per-user cap) and inserts.
func (s *Store) CreateBirthday(userID int64, in BirthdayInput) (*Birthday, []FieldError, error) {
	p, errs := parseBirthdayInput(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	n, err := s.countBirthdays(userID)
	if err != nil {
		return nil, nil, err
	}
	if n >= BirthdayPerUserCap {
		return nil, []FieldError{{Model: "birthday", Field: "base", Code: "too_many"}}, nil
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO birthdays
		(user_id, name, month, day, year, body, emoji, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, p.Name, p.Month, p.Day, nullIfZero(p.Year), p.Body, p.Emoji, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	b, err := s.FindBirthday(userID, id)
	return b, nil, err
}

// UpdateBirthday validates and updates a record owned by the user.
func (s *Store) UpdateBirthday(userID, id int64, in BirthdayInput) (*Birthday, []FieldError, error) {
	p, errs := parseBirthdayInput(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if _, err := s.FindBirthday(userID, id); err != nil {
		return nil, nil, err
	}
	_, err := s.db.Exec(`UPDATE birthdays SET name = ?, month = ?, day = ?,
		year = ?, body = ?, emoji = ?, updated_at = ? WHERE id = ? AND user_id = ?`,
		p.Name, p.Month, p.Day, nullIfZero(p.Year), p.Body, p.Emoji, now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	b, err := s.FindBirthday(userID, id)
	return b, nil, err
}

// FindBirthday scopes the lookup to the owner (a stranger's id is a 404).
func (s *Store) FindBirthday(userID, id int64) (*Birthday, error) {
	b, err := scanBirthday(s.db.QueryRow(`SELECT `+birthdayCols+` FROM birthdays
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return b, err
}

// ListBirthdays mirrors the API/export ordering (month, day, id).
func (s *Store) ListBirthdays(userID int64) ([]*Birthday, error) {
	rows, err := s.db.Query(`SELECT `+birthdayCols+` FROM birthdays
		WHERE user_id = ? ORDER BY month, day, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Birthday
	for rows.Next() {
		b, err := scanBirthday(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) DeleteBirthday(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM birthdays WHERE id = ? AND user_id = ?`, id, userID)
	return err
}
