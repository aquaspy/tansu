package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	st, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedUser(t *testing.T, st *Store, email string) *User {
	t.Helper()
	u, err := st.CreateUser(email, "digest")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func eventIn(title, from, to string) EventInput {
	return EventInput{Title: title, AllDay: true, StartsOn: from, EndsOn: to}
}

func TestEventEmojiRepeatRoundTrip(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	in := eventIn("Gym", "2026-09-01", "2026-09-01")
	in.Emoji = "🏋️"
	in.Repeat = "weekly"
	in.RepeatUntil = "2026-12-01"
	e, errs, err := st.CreateEvent(u.ID, in)
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if e.Emoji != "🏋️" || e.Repeat != "weekly" || e.RepeatUntil != "2026-12-01" || !e.Repeating() {
		t.Fatalf("round trip: %+v", e)
	}
	in.Repeat = "none"
	in.RepeatUntil = "2026-12-01" // cleared when the series ends
	e, errs, err = st.UpdateEvent(u.ID, e.ID, in)
	if err != nil || len(errs) > 0 {
		t.Fatalf("update: %v %+v", err, errs)
	}
	if e.Repeat != "none" || e.RepeatUntil != "" || e.Repeating() {
		t.Fatalf("series end: %+v", e)
	}
}

func TestEventRepeatValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*EventInput)
		field  string
		code   string
	}{
		{"bad repeat", func(in *EventInput) { in.Repeat = "fortnightly" }, "repeat", "invalid"},
		{"until garbage", func(in *EventInput) { in.Repeat = "daily"; in.RepeatUntil = "soon" }, "repeat_until", "invalid"},
		{"until before start", func(in *EventInput) { in.Repeat = "daily"; in.RepeatUntil = "2026-08-01" }, "repeat_until", "before_start"},
		{"emoji too long", func(in *EventInput) { in.Emoji = "🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂" }, "emoji", "too_long"},
	}
	for _, c := range cases {
		in := eventIn("Gym", "2026-09-01", "2026-09-01")
		c.mutate(&in)
		_, errs := ValidateEvent(in)
		found := false
		for _, e := range errs {
			if e.Field == c.field && e.Code == c.code {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing %s.%s in %+v", c.name, c.field, c.code, errs)
		}
	}
	// Blank repeat normalizes to none; repeat names are case-insensitive.
	in := eventIn("Gym", "2026-09-01", "2026-09-01")
	in.Repeat = "Weekly"
	norm, errs := ValidateEvent(in)
	if len(errs) > 0 || norm.Repeat != "weekly" {
		t.Fatalf("normalize: %+v %+v", norm, errs)
	}
}

func TestBirthdayEmojiRoundTrip(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	b, errs, err := st.CreateBirthday(u.ID,
		BirthdayInput{Name: "Ada", Month: "12", Day: "10", Emoji: "🎂"})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if b.Emoji != "🎂" {
		t.Fatalf("emoji: %+v", b)
	}
	if errs := ValidateBirthday(BirthdayInput{Name: "Bo", Month: "1", Day: "1",
		Emoji: "🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂"}); len(errs) == 0 {
		t.Fatal("expected emoji error")
	}
}

func TestMigrateAddsEmojiRepeatColumns(t *testing.T) {
	path := t.TempDir() + "/old.sqlite3"
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-feature schema: the same tables minus the new columns.
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_digest TEXT NOT NULL, holiday_countries TEXT NOT NULL DEFAULT 'BR', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, title TEXT NOT NULL, body TEXT NOT NULL DEFAULT '', all_day INTEGER NOT NULL DEFAULT 1, starts_on TEXT NOT NULL, ends_on TEXT NOT NULL, starts_at TEXT, ends_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE birthdays (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE, name TEXT NOT NULL, month INTEGER NOT NULL, day INTEGER NOT NULL, year INTEGER, body TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO users (email, password_digest, created_at, updated_at) VALUES ('ada@example.com', 'digest', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO events (user_id, title, starts_on, ends_on, created_at, updated_at) VALUES (1, 'Old', '2026-09-01', '2026-09-01', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()

	st, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	e, err := st.FindEvent(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if e.Title != "Old" || e.Repeat != "none" || e.Emoji != "" || e.RepeatUntil != "" {
		t.Fatalf("migrated row: %+v", e)
	}
	// Second open is idempotent.
	st2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
}

func TestEventsInRangeIncludesSpanning(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	e, errs, err := st.CreateEvent(u.ID, eventIn("Trip", "2026-08-30", "2026-09-02"))
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	found, err := st.EventsInRange(u.ID, "2026-09-01", "2026-09-07")
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].ID != e.ID {
		t.Fatalf("spanning event missing: %+v", found)
	}
	miss, err := st.EventsInRange(u.ID, "2026-08-01", "2026-08-29")
	if err != nil {
		t.Fatal(err)
	}
	if len(miss) != 0 {
		t.Fatalf("out-of-range event returned: %+v", miss)
	}
}

func TestAllDayEventsDropTimes(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	e, errs, err := st.CreateEvent(u.ID, EventInput{
		Title: "Park", AllDay: true, StartsOn: "2026-08-25", StartsAt: "14:00",
	})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if !e.AllDay || e.StartsAt != "" {
		t.Fatalf("times not dropped: %+v", e)
	}
}

func TestTimedEventsNeedStartTime(t *testing.T) {
	_, errs := ValidateEvent(EventInput{Title: "Call", StartsOn: "2026-08-25"})
	found := false
	for _, e := range errs {
		if e.Field == "starts_at" && e.Code == "blank" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing starts_at error: %+v", errs)
	}
}

func TestTimedEventEndBeforeStart(t *testing.T) {
	_, errs := ValidateEvent(EventInput{Title: "Call", AllDay: false,
		StartsOn: "2026-08-25", EndsOn: "2026-08-25", StartsAt: "15:00", EndsAt: "14:00"})
	found := false
	for _, e := range errs {
		if e.Field == "ends_at" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing ends_at error: %+v", errs)
	}
}

func TestRejectsInvertedRange(t *testing.T) {
	_, errs := ValidateEvent(eventIn("Nope", "2026-08-25", "2026-08-20"))
	found := false
	for _, e := range errs {
		if e.Field == "ends_on" {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing ends_on error: %+v", errs)
	}
}

func TestEndsOnDefaultsToStartsOn(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	e, errs, err := st.CreateEvent(u.ID, EventInput{Title: "One", AllDay: true, StartsOn: "2026-08-25"})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if e.EndsOn != "2026-08-25" {
		t.Fatalf("ends_on = %q", e.EndsOn)
	}
}

func TestEventScopedToOwner(t *testing.T) {
	st := openTest(t)
	a := seedUser(t, st, "a@example.com")
	b := seedUser(t, st, "b@example.com")
	e, _, err := st.CreateEvent(a.ID, eventIn("Secret", "2026-08-25", "2026-08-25"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindEvent(b.ID, e.ID); err != ErrNotFound {
		t.Fatalf("stranger read: %v", err)
	}
	if _, _, err := st.UpdateEvent(b.ID, e.ID, eventIn("Stolen", "2026-08-25", "2026-08-25")); err != ErrNotFound {
		t.Fatalf("stranger update: %v", err)
	}
}

func TestBirthdayFeb29Observed(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	b, errs, err := st.CreateBirthday(u.ID, BirthdayInput{Name: "Ada", Month: "2", Day: "29", Year: "2000"})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	cases := map[string]bool{
		"2025-02-28": true,
		"2025-03-01": false,
		"2024-02-29": true,
		"2024-02-28": false,
	}
	for day, want := range cases {
		d, _ := time.Parse("2006-01-02", day)
		if got := b.ObservedOn(d); got != want {
			t.Errorf("observed %s = %v, want %v", day, got, want)
		}
	}
}

func TestBirthdayAgeAndLabel(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	b, errs, err := st.CreateBirthday(u.ID, BirthdayInput{Name: "Lin", Month: "8", Day: "11", Year: "2000"})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if age, ok := b.AgeIn(2026); !ok || age != 26 {
		t.Fatalf("age = %d, %v", age, ok)
	}
	d, _ := time.Parse("2006-01-02", "2026-08-11")
	if got := b.LabelOn(d); got != "Lin (26)" {
		t.Fatalf("label = %q", got)
	}
}

func TestBirthdayRejects31April(t *testing.T) {
	if errs := ValidateBirthday(BirthdayInput{Name: "No", Month: "4", Day: "31"}); len(errs) == 0 {
		t.Fatal("expected day error")
	} else {
		found := false
		for _, e := range errs {
			if e.Field == "day" {
				found = true
			}
		}
		if !found {
			t.Fatalf("missing day error: %+v", errs)
		}
	}
}

func TestUserHolidayCodes(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	u, _ = st.FindUser(u.ID)
	if got := u.HolidayCountryCodes(); len(got) != 1 || got[0] != "BR" {
		t.Fatalf("default codes = %+v", got)
	}
	u.SetHolidayCountryCodes([]string{"br", "US", "XX", "BR"})
	if u.HolidayCountries != "BR,US" {
		t.Fatalf("joined = %q", u.HolidayCountries)
	}
	if err := st.UpdateUserHolidayCountries(u.ID, u.HolidayCountries); err != nil {
		t.Fatal(err)
	}
	u, _ = st.FindUser(u.ID)
	if got := u.HolidayCountryCodes(); len(got) != 2 {
		t.Fatalf("saved codes = %+v", got)
	}
}

func TestEmailUnique(t *testing.T) {
	st := openTest(t)
	seedUser(t, st, "ada@example.com")
	if _, err := st.CreateUser("Ada@Example.com", "digest"); !IsUniqueViolation(err) {
		t.Fatalf("expected unique violation, got %v", err)
	}
}

func TestTokensAuthenticate(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	tok, raw, err := st.CreateToken(u.ID, "hermes")
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) < 20 || raw[:5] != "kura_" {
		t.Fatalf("raw shape: %q", raw)
	}
	got, err := st.AuthenticateToken("  " + raw + " ")
	if err != nil || got.ID != tok.ID {
		t.Fatalf("auth: %+v %v", got, err)
	}
	if _, err := st.AuthenticateToken("kura_nope"); err != ErrNotFound {
		t.Fatalf("bogus: %v", err)
	}
	if _, _, err := st.CreateToken(u.ID, "  "); err != ErrTokenNameBlank {
		t.Fatalf("blank name: %v", err)
	}
}

func TestReclaimSpaceNeverBreaks(t *testing.T) {
	st := openTest(t)
	st.ReclaimSpace()
	// A broken pool errors; ReclaimSpace must swallow it, never panic.
	st2, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
	st2.ReclaimSpace()
}

// TestMigrateOldUsersSSO opens a database written before the SSO column
// existed: Open must add account_sub plus its unique index, and the
// pre-existing user must stay readable and linkable.
func TestMigrateOldUsersSSO(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	stmts := []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_digest TEXT NOT NULL, holiday_countries TEXT NOT NULL DEFAULT 'BR', created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO users (email, password_digest, created_at, updated_at) VALUES ('old@x.com', 'd', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatal(err)
		}
	}
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	u, err := s.FindUserByEmail("old@x.com")
	if err != nil || u.AccountSub != "" {
		t.Fatalf("old user = %+v, err = %v", u, err)
	}
	if err := s.SetUserSub(u.ID, "acct-old"); err != nil {
		t.Fatal(err)
	}
	linked, err := s.FindUserBySub("acct-old")
	if err != nil || linked.ID != u.ID {
		t.Fatalf("linked = %+v, err = %v", linked, err)
	}
	// The partial unique index: a second user reusing the sub fails.
	u2, err := s.CreateUser("new@x.com", "d")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetUserSub(u2.ID, "acct-old"); !IsUniqueViolation(err) {
		t.Fatalf("duplicate sub err = %v, want unique violation", err)
	}
}
