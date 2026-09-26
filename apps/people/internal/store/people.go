package store

import (
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// PersonNameMax mirrors Birthday::NAME_MAX.
	PersonNameMax         = 200
	PersonNicknameMax     = 80
	PersonRelationshipMax = 40
	PersonPhoneMax        = 40
	PersonEmailMax        = 160
	PersonAddressMax      = 500
	PersonSizeMax         = 20
	PersonHeightMax       = 20
	PersonFavoritesMax    = 2000
	// PersonNotesMax mirrors Birthday::BODY_MAX.
	PersonNotesMax = 8000
	// PersonPerUserCap mirrors Birthday::PER_USER_CAP.
	PersonPerUserCap = 500

	AttrLabelMax     = 80
	AttrValueMax     = 500
	AttrPerPersonCap = 50

	// EmojiMax caps the emoji field in runes (a ZWJ sequence is several).
	EmojiMax = 12
)

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

// daysInMonth mirrors Birthday::DAYS_IN_MONTH (Feb allows 29; leap
// birthdays are observed on Feb 28 in common years).
var daysInMonth = [13]int{0, 31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

// DaysInMonth mirrors Time.days_in_month.
func DaysInMonth(year, month int) int {
	return time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC).Day()
}

// ---------------------------------------------------------------------------
// People
// ---------------------------------------------------------------------------

// Person is one card: who they are, when to celebrate them, and the
// practical details (sizes, address, favorites) that make a gift land.
type Person struct {
	ID           int64
	UserID       int64
	Name         string
	Nickname     string
	Relationship string
	BirthMonth   int // 0 when unknown
	BirthDay     int // 0 when unknown
	BirthYear    int // 0 when unknown
	Emoji        string
	Phone        string
	Email        string
	Address      string
	RingSize     string
	ShoeSize     string
	ShirtSize    string
	PantsSize    string
	Height       string
	Favorites    string
	Notes        string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PersonInput is the raw form/API payload.
type PersonInput struct {
	Name         string
	Nickname     string
	Relationship string
	BirthMonth   string
	BirthDay     string
	BirthYear    string
	Emoji        string
	Phone        string
	Email        string
	Address      string
	RingSize     string
	ShoeSize     string
	ShirtSize    string
	PantsSize    string
	Height       string
	Favorites    string
	Notes        string
}

type personParsed struct {
	Name         string
	Nickname     string
	Relationship string
	BirthMonth   int
	BirthDay     int
	BirthYear    int
	Emoji        string
	Phone        string
	Email        string
	Address      string
	RingSize     string
	ShoeSize     string
	ShirtSize    string
	PantsSize    string
	Height       string
	Favorites    string
	Notes        string
}

// parseMonthDay reads an optional month/day pair: blank means unknown,
// a half-filled pair is an error, and the day must exist in the month.
func parseMonthDay(monthS, dayS, field string, fail func(field, code string)) (int, int) {
	monthS = strings.TrimSpace(monthS)
	dayS = strings.TrimSpace(dayS)
	if monthS == "" && dayS == "" {
		return 0, 0
	}
	month, merr := strconv.Atoi(monthS)
	day, derr := strconv.Atoi(dayS)
	if merr != nil || month < 1 || month > 12 {
		fail(field, "range")
		return 0, 0
	}
	if derr != nil || day < 1 || day > 31 {
		fail(field, "range")
		return 0, 0
	}
	if day > daysInMonth[month] {
		fail(field, "invalid")
		return 0, 0
	}
	return month, day
}

func parsePersonInput(in PersonInput) (personParsed, []FieldError) {
	var errs []FieldError
	fail := func(field, code string) {
		errs = append(errs, FieldError{Model: "person", Field: field, Code: code})
	}
	tooLong := func(value string, max int, field string) string {
		value = strings.TrimSpace(value)
		if runeLen(value) > max {
			fail(field, "too_long")
		}
		return value
	}
	p := personParsed{
		Name:         strings.TrimSpace(in.Name),
		Nickname:     tooLong(in.Nickname, PersonNicknameMax, "nickname"),
		Relationship: tooLong(in.Relationship, PersonRelationshipMax, "relationship"),
		Emoji:        tooLong(in.Emoji, EmojiMax, "emoji"),
		Phone:        tooLong(in.Phone, PersonPhoneMax, "phone"),
		Email:        tooLong(in.Email, PersonEmailMax, "email"),
		Address:      tooLong(in.Address, PersonAddressMax, "address"),
		RingSize:     tooLong(in.RingSize, PersonSizeMax, "ring_size"),
		ShoeSize:     tooLong(in.ShoeSize, PersonSizeMax, "shoe_size"),
		ShirtSize:    tooLong(in.ShirtSize, PersonSizeMax, "shirt_size"),
		PantsSize:    tooLong(in.PantsSize, PersonSizeMax, "pants_size"),
		Height:       tooLong(in.Height, PersonHeightMax, "height"),
		Favorites:    strings.TrimSpace(in.Favorites),
		Notes:        strings.TrimSpace(in.Notes),
	}
	if p.Name == "" {
		fail("name", "blank")
	} else if runeLen(p.Name) > PersonNameMax {
		fail("name", "too_long")
	}
	if runeLen(p.Favorites) > PersonFavoritesMax {
		fail("favorites", "too_long")
	}
	if runeLen(p.Notes) > PersonNotesMax {
		fail("notes", "too_long")
	}
	p.BirthMonth, p.BirthDay = parseMonthDay(in.BirthMonth, in.BirthDay, "birthday", fail)
	if strings.TrimSpace(in.BirthYear) != "" {
		year, err := strconv.Atoi(strings.TrimSpace(in.BirthYear))
		if err != nil || year < 1900 || year > 2100 {
			fail("birth_year", "range")
		} else {
			p.BirthYear = year
		}
	}
	if p.BirthYear != 0 && p.BirthMonth == 0 {
		fail("birthday", "needs_date")
	}
	return p, errs
}

// ValidatePerson mirrors the model validations (without the per-user
// cap, which needs the database).
func ValidatePerson(in PersonInput) []FieldError {
	_, errs := parsePersonInput(in)
	return errs
}

// HasBirthday reports whether the birth month/day are known.
func (p *Person) HasBirthday() bool { return p.BirthMonth != 0 && p.BirthDay != 0 }

// observedDay mirrors Birthday#observed_on?: Feb 29 falls on Feb 28 in
// common years.
func observedDay(month, day, year int) (int, int) {
	if month == 2 && day == 29 && DaysInMonth(year, 2) == 28 {
		return 2, 28
	}
	return month, day
}

// ObservedOn reports whether date is this person's birthday.
func (p *Person) ObservedOn(date time.Time) bool {
	if !p.HasBirthday() {
		return false
	}
	m, d := observedDay(p.BirthMonth, p.BirthDay, date.Year())
	return int(date.Month()) == m && date.Day() == d
}

// NextBirthday returns the next date (from, inclusive) the birthday
// lands on. ok is false when no birthday is set.
func (p *Person) NextBirthday(from time.Time) (next time.Time, ok bool) {
	if !p.HasBirthday() {
		return time.Time{}, false
	}
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	m, d := observedDay(p.BirthMonth, p.BirthDay, from.Year())
	next = time.Date(from.Year(), time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if next.Before(from) {
		m, d := observedDay(p.BirthMonth, p.BirthDay, from.Year()+1)
		next = time.Date(from.Year()+1, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	}
	return next, true
}

// DaysUntilBirthday counts whole days from (exclusive of time of day)
// to the next birthday. ok is false when no birthday is set.
func (p *Person) DaysUntilBirthday(from time.Time) (int, bool) {
	next, ok := p.NextBirthday(from)
	if !ok {
		return 0, false
	}
	from = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, time.UTC)
	return int(next.Sub(from).Hours() / 24), true
}

// AgeIn mirrors Birthday#age_in: the age reached in year, or false
// when the birth year is unknown.
func (p *Person) AgeIn(year int) (int, bool) {
	if p.BirthYear == 0 || !p.HasBirthday() {
		return 0, false
	}
	return year - p.BirthYear, true
}

// LabelOn mirrors Birthday#label_on: "Ada (26)" with a known year,
// else the bare name.
func (p *Person) LabelOn(date time.Time) string {
	if age, ok := p.AgeIn(date.Year()); ok {
		return p.Name + " (" + strconv.Itoa(age) + ")"
	}
	return p.Name
}

const personCols = `id, user_id, name, nickname, relationship,
	birth_month, birth_day, birth_year,
	emoji, phone, email, address, ring_size, shoe_size, shirt_size, pants_size,
	height, favorites, notes, created_at, updated_at`

func scanPerson(row interface{ Scan(...any) error }) (*Person, error) {
	p := &Person{}
	var birthMonth, birthDay, birthYear sql.NullInt64
	var created, updated string
	err := row.Scan(&p.ID, &p.UserID, &p.Name, &p.Nickname, &p.Relationship,
		&birthMonth, &birthDay, &birthYear,
		&p.Emoji, &p.Phone, &p.Email, &p.Address,
		&p.RingSize, &p.ShoeSize, &p.ShirtSize, &p.PantsSize,
		&p.Height, &p.Favorites, &p.Notes, &created, &updated)
	if err != nil {
		return nil, err
	}
	p.BirthMonth, p.BirthDay, p.BirthYear = int(birthMonth.Int64), int(birthDay.Int64), int(birthYear.Int64)
	p.CreatedAt, _ = parseTime(created)
	p.UpdatedAt, _ = parseTime(updated)
	return p, nil
}

func (s *Store) countPeople(userID int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM people WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// CountPeople reports the user's person total (CLI users listing).
func (s *Store) CountPeople(userID int64) int {
	n, _ := s.countPeople(userID)
	return n
}

// CreatePerson validates (including the per-user cap) and inserts.
func (s *Store) CreatePerson(userID int64, in PersonInput) (*Person, []FieldError, error) {
	p, errs := parsePersonInput(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	n, err := s.countPeople(userID)
	if err != nil {
		return nil, nil, err
	}
	if n >= PersonPerUserCap {
		return nil, []FieldError{{Model: "person", Field: "base", Code: "too_many"}}, nil
	}
	ts := now()
	res, err := s.db.Exec(`INSERT INTO people
		(user_id, name, nickname, relationship,
		birth_month, birth_day, birth_year,
		emoji, phone, email, address, ring_size, shoe_size, shirt_size, pants_size,
		height, favorites, notes, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		userID, p.Name, p.Nickname, p.Relationship,
		nullIfZero(p.BirthMonth), nullIfZero(p.BirthDay), nullIfZero(p.BirthYear),
		p.Emoji, p.Phone, p.Email, p.Address,
		p.RingSize, p.ShoeSize, p.ShirtSize, p.PantsSize,
		p.Height, p.Favorites, p.Notes, ts, ts)
	if err != nil {
		return nil, nil, err
	}
	id, _ := res.LastInsertId()
	person, err := s.FindPerson(userID, id)
	return person, nil, err
}

// UpdatePerson validates and updates a record owned by the user.
func (s *Store) UpdatePerson(userID, id int64, in PersonInput) (*Person, []FieldError, error) {
	p, errs := parsePersonInput(in)
	if len(errs) > 0 {
		return nil, errs, nil
	}
	if _, err := s.FindPerson(userID, id); err != nil {
		return nil, nil, err
	}
	_, err := s.db.Exec(`UPDATE people SET name = ?, nickname = ?, relationship = ?,
		birth_month = ?, birth_day = ?, birth_year = ?,
		emoji = ?, phone = ?, email = ?, address = ?,
		ring_size = ?, shoe_size = ?, shirt_size = ?, pants_size = ?,
		height = ?, favorites = ?, notes = ?, updated_at = ?
		WHERE id = ? AND user_id = ?`,
		p.Name, p.Nickname, p.Relationship,
		nullIfZero(p.BirthMonth), nullIfZero(p.BirthDay), nullIfZero(p.BirthYear),
		p.Emoji, p.Phone, p.Email, p.Address,
		p.RingSize, p.ShoeSize, p.ShirtSize, p.PantsSize,
		p.Height, p.Favorites, p.Notes, now(), id, userID)
	if err != nil {
		return nil, nil, err
	}
	person, err := s.FindPerson(userID, id)
	return person, nil, err
}

// FindPerson scopes the lookup to the owner (a stranger's id is a 404,
// like current_user.people.find).
func (s *Store) FindPerson(userID, id int64) (*Person, error) {
	p, err := scanPerson(s.db.QueryRow(`SELECT `+personCols+` FROM people
		WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return p, err
}

// ListPeople returns every card, alphabetically.
func (s *Store) ListPeople(userID int64) ([]*Person, error) {
	return s.SearchPeople(userID, "", "", 0, 0)
}

// SearchPeople filters by free text (name, nickname, relationship,
// notes), exact relationship, and birth month. limit <= 0 means all.
func (s *Store) SearchPeople(userID int64, q, relationship string, birthMonth, limit int) ([]*Person, error) {
	var conds []string
	var args []any
	args = append(args, userID)
	if q = strings.TrimSpace(q); q != "" {
		like := "%" + strings.ReplaceAll(strings.ReplaceAll(q, "%", ""), "_", "") + "%"
		conds = append(conds, `(name LIKE ? ESCAPE '\' OR nickname LIKE ? ESCAPE '\' OR relationship LIKE ? ESCAPE '\' OR notes LIKE ? ESCAPE '\')`)
		args = append(args, like, like, like, like)
	}
	if relationship = strings.TrimSpace(relationship); relationship != "" {
		conds = append(conds, `relationship = ?`)
		args = append(args, relationship)
	}
	if birthMonth >= 1 && birthMonth <= 12 {
		conds = append(conds, `birth_month = ?`)
		args = append(args, birthMonth)
	}
	query := `SELECT ` + personCols + ` FROM people WHERE user_id = ?`
	if len(conds) > 0 {
		query += ` AND ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY name COLLATE NOCASE, id`
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Person
	for rows.Next() {
		p, err := scanPerson(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRelationships returns the distinct non-blank relationships for
// the filter chips, alphabetically.
func (s *Store) ListRelationships(userID int64) ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT relationship FROM people
		WHERE user_id = ? AND relationship <> '' ORDER BY relationship COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var rel string
		if err := rows.Scan(&rel); err != nil {
			return nil, err
		}
		out = append(out, rel)
	}
	return out, rows.Err()
}

func (s *Store) DeletePerson(userID, id int64) error {
	_, err := s.db.Exec(`DELETE FROM people WHERE id = ? AND user_id = ?`, id, userID)
	return err
}

// ---------------------------------------------------------------------------
// Custom attributes
// ---------------------------------------------------------------------------

// Attr is one free label/value row on a card ("Bike frame", "M").
type Attr struct {
	ID       int64
	PersonID int64
	Label    string
	Value    string
	Position int
}

// AttrInput is the raw label/value payload.
type AttrInput struct {
	Label string
	Value string
}

// ValidateAttrInput checks one row; fully blank rows are skipped by the
// caller (empty form rows), never an error.
func ValidateAttrInput(in AttrInput) []FieldError {
	label := strings.TrimSpace(in.Label)
	value := strings.TrimSpace(in.Value)
	if label == "" && value == "" {
		return nil
	}
	var errs []FieldError
	fail := func(field, code string) {
		errs = append(errs, FieldError{Model: "attr", Field: field, Code: code})
	}
	if label == "" {
		fail("label", "blank")
	} else if runeLen(label) > AttrLabelMax {
		fail("label", "too_long")
	}
	if runeLen(value) > AttrValueMax {
		fail("value", "too_long")
	}
	return errs
}

const attrCols = `id, person_id, label, value, position`

func scanAttr(row interface{ Scan(...any) error }) (*Attr, error) {
	a := &Attr{}
	err := row.Scan(&a.ID, &a.PersonID, &a.Label, &a.Value, &a.Position)
	return a, err
}

// ListAttrs returns the person's rows in position order.
func (s *Store) ListAttrs(personID int64) ([]*Attr, error) {
	rows, err := s.db.Query(`SELECT `+attrCols+` FROM person_attrs
		WHERE person_id = ? ORDER BY position, id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Attr
	for rows.Next() {
		a, err := scanAttr(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ReplaceAttrs swaps the whole row set (form semantics: the posted
// rows are the truth). Fully blank rows are dropped.
func (s *Store) ReplaceAttrs(personID int64, ins []AttrInput) ([]FieldError, error) {
	var kept []AttrInput
	for _, in := range ins {
		if errs := ValidateAttrInput(in); len(errs) > 0 {
			return errs, nil
		}
		if strings.TrimSpace(in.Label) == "" && strings.TrimSpace(in.Value) == "" {
			continue
		}
		kept = append(kept, AttrInput{
			Label: strings.TrimSpace(in.Label),
			Value: strings.TrimSpace(in.Value),
		})
	}
	if len(kept) > AttrPerPersonCap {
		return []FieldError{{Model: "attr", Field: "base", Code: "too_many"}}, nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM person_attrs WHERE person_id = ?`, personID); err != nil {
		return nil, err
	}
	ts := now()
	for i, in := range kept {
		if _, err := tx.Exec(`INSERT INTO person_attrs
			(person_id, label, value, position, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`,
			personID, in.Label, in.Value, i, ts, ts); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}
