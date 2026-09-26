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

func personIn(name string) PersonInput {
	return PersonInput{Name: name, BirthMonth: "8", BirthDay: "11", BirthYear: "2000"}
}

func findErr(errs []FieldError, field, code string) bool {
	for _, e := range errs {
		if e.Field == field && e.Code == code {
			return true
		}
	}
	return false
}

func TestPersonRoundTrip(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	in := personIn("Ada Lovelace")
	in.Nickname = "Ada"
	in.Relationship = "friend"
	in.Emoji = "🎂"
	in.Phone = "+55 11 90000-0000"
	in.Email = "ada@example.com"
	in.Address = "Rua A, 123"
	in.RingSize = "16"
	in.ShoeSize = "37"
	in.ShirtSize = "M"
	in.PantsSize = "40"
	in.Height = "1.65m"
	in.Favorites = "tea, sci-fi"
	in.Notes = "met at conf"
	p, errs, err := st.CreatePerson(u.ID, in)
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if p.Nickname != "Ada" || p.Relationship != "friend" || p.RingSize != "16" ||
		p.Height != "1.65m" ||
		p.BirthMonth != 8 || p.BirthDay != 11 || p.BirthYear != 2000 {
		t.Fatalf("round trip: %+v", p)
	}
	if _, err := st.ReplaceAttrs(p.ID, []AttrInput{
		{Label: "Bike frame", Value: "M"},
		{Label: "Coffee", Value: "flat white"},
		{Label: "", Value: ""}, // empty form row: dropped
	}); err != nil {
		t.Fatal(err)
	}
	attrs, err := st.ListAttrs(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 2 || attrs[0].Label != "Bike frame" || attrs[1].Value != "flat white" {
		t.Fatalf("attrs: %+v", attrs)
	}
	in.Nickname = "A."
	p, errs, err = st.UpdatePerson(u.ID, p.ID, in)
	if err != nil || len(errs) > 0 {
		t.Fatalf("update: %v %+v", err, errs)
	}
	if p.Nickname != "A." {
		t.Fatalf("update: %+v", p)
	}
	if got := st.CountPeople(u.ID); got != 1 {
		t.Fatalf("count = %d", got)
	}
}

func TestPersonValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*PersonInput)
		field  string
		code   string
	}{
		{"blank name", func(in *PersonInput) { in.Name = "  " }, "name", "blank"},
		{"bad month", func(in *PersonInput) { in.BirthMonth = "13" }, "birthday", "range"},
		{"31 april", func(in *PersonInput) { in.BirthMonth = "4"; in.BirthDay = "31" }, "birthday", "invalid"},
		{"year without date", func(in *PersonInput) { in.BirthMonth = ""; in.BirthDay = "" }, "birthday", "needs_date"},
		{"year range", func(in *PersonInput) { in.BirthYear = "1800" }, "birth_year", "range"},
		{"emoji too long", func(in *PersonInput) { in.Emoji = "🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂🎂" }, "emoji", "too_long"},
	}
	for _, c := range cases {
		in := personIn("Ada")
		c.mutate(&in)
		if errs := ValidatePerson(in); !findErr(errs, c.field, c.code) {
			t.Errorf("%s: missing %s.%s in %+v", c.name, c.field, c.code, errs)
		}
	}
	// Feb 29 and blank birthdays are fine.
	if errs := ValidatePerson(PersonInput{Name: "Leap", BirthMonth: "2", BirthDay: "29"}); len(errs) > 0 {
		t.Fatalf("feb29: %+v", errs)
	}
	if errs := ValidatePerson(PersonInput{Name: "No date"}); len(errs) > 0 {
		t.Fatalf("dateless: %+v", errs)
	}
}

func TestPersonFeb29Observed(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	p, errs, err := st.CreatePerson(u.ID, PersonInput{Name: "Ada", BirthMonth: "2", BirthDay: "29", BirthYear: "2000"})
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
		if got := p.ObservedOn(d); got != want {
			t.Errorf("observed %s = %v, want %v", day, got, want)
		}
	}
	from, _ := time.Parse("2006-01-02", "2025-02-27")
	if n, _ := p.DaysUntilBirthday(from); n != 1 {
		t.Errorf("days until = %d, want 1", n)
	}
	from, _ = time.Parse("2006-01-02", "2025-03-01")
	if n, _ := p.DaysUntilBirthday(from); n != 364 {
		t.Errorf("days until = %d, want 364", n)
	}
}

func TestPersonAgeAndLabel(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	p, errs, err := st.CreatePerson(u.ID, personIn("Lin"))
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	if age, ok := p.AgeIn(2026); !ok || age != 26 {
		t.Fatalf("age = %d, %v", age, ok)
	}
	d, _ := time.Parse("2006-01-02", "2026-08-11")
	if got := p.LabelOn(d); got != "Lin (26)" {
		t.Fatalf("label = %q", got)
	}
}

func TestPersonScopedToOwner(t *testing.T) {
	st := openTest(t)
	a := seedUser(t, st, "a@example.com")
	b := seedUser(t, st, "b@example.com")
	p, _, err := st.CreatePerson(a.ID, personIn("Secret"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindPerson(b.ID, p.ID); err != ErrNotFound {
		t.Fatalf("stranger read: %v", err)
	}
	if _, _, err := st.UpdatePerson(b.ID, p.ID, personIn("Stolen")); err != ErrNotFound {
		t.Fatalf("stranger update: %v", err)
	}
}

func TestPersonSearch(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	mustCreate := func(in PersonInput) {
		t.Helper()
		if _, errs, err := st.CreatePerson(u.ID, in); err != nil || len(errs) > 0 {
			t.Fatalf("create: %v %+v", err, errs)
		}
	}
	ada := personIn("Ada Lovelace")
	ada.Relationship = "friend"
	ada.Notes = "first programmer"
	mustCreate(ada)
	lin := personIn("Lin")
	lin.Relationship = "family"
	lin.BirthMonth = "12"
	mustCreate(lin)

	if got, _ := st.SearchPeople(u.ID, "programmer", "", 0, 0); len(got) != 1 || got[0].Name != "Ada Lovelace" {
		t.Fatalf("text search: %+v", got)
	}
	if got, _ := st.SearchPeople(u.ID, "", "family", 0, 0); len(got) != 1 || got[0].Name != "Lin" {
		t.Fatalf("relationship filter: %+v", got)
	}
	if got, _ := st.SearchPeople(u.ID, "", "", 12, 0); len(got) != 1 || got[0].Name != "Lin" {
		t.Fatalf("month filter: %+v", got)
	}
	if rels, _ := st.ListRelationships(u.ID); len(rels) != 2 || rels[0] != "family" {
		t.Fatalf("relationships: %+v", rels)
	}
}

func TestAttrsReplace(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	p, _, err := st.CreatePerson(u.ID, personIn("Ada"))
	if err != nil {
		t.Fatal(err)
	}
	if errs, _ := st.ReplaceAttrs(p.ID, []AttrInput{{Label: "", Value: "orphan"}}); !findErr(errs, "label", "blank") {
		t.Fatalf("blank label: %+v", errs)
	}
	if _, err := st.ReplaceAttrs(p.ID, []AttrInput{{Label: "A", Value: "1"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.ReplaceAttrs(p.ID, []AttrInput{{Label: "B", Value: "2"}}); err != nil {
		t.Fatal(err)
	}
	attrs, _ := st.ListAttrs(p.ID)
	if len(attrs) != 1 || attrs[0].Label != "B" {
		t.Fatalf("replace: %+v", attrs)
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
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_digest TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
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
