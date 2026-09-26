package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
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

func seedUser(t *testing.T, st *Store) *User {
	t.Helper()
	u, err := st.CreateUser("you@example.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestTitleAndPreview(t *testing.T) {
	title, preview := TitleAndPreview("Trip ideas\nKyoto in April\nOsaka too")
	if title != "Trip ideas" || preview != "Kyoto in April" {
		t.Fatalf("got %q/%q", title, preview)
	}
	title, preview = TitleAndPreview("\n\n  Solo  \n")
	if title != "Solo" || preview != "Solo" {
		t.Fatalf("single line: got %q/%q", title, preview)
	}
	title, preview = TitleAndPreview("   \n\t\n")
	if title != "" || preview != "" {
		t.Fatalf("blank: got %q/%q", title, preview)
	}
	long := strings.Repeat("x", 200)
	title, preview = TitleAndPreview(long + "\n" + long)
	if len([]rune(title)) != TitleMaxLength || len([]rune(preview)) != PreviewMaxLength {
		t.Fatalf("truncate: got %d/%d", len(title), len(preview))
	}
}

func TestNormalizeFolder(t *testing.T) {
	for in, want := range map[string]string{
		"":        "",
		"  ":      "",
		"all":     "",
		"ALL":     "",
		"inbox":   "",
		"InBox":   "",
		"work":    "work",
		"  work ": "work",
	} {
		if got := NormalizeFolder(in); got != want {
			t.Errorf("NormalizeFolder(%q) = %q, want %q", in, got, want)
		}
	}
	if got := NormalizeFolder(strings.Repeat("n", 200)); len([]rune(got)) != FolderMaxLength {
		t.Errorf("long folder kept %d runes", len([]rune(got)))
	}
}

func TestNoteCRUD(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)

	n, err := st.CreateNote(u.ID, "Title\nBody here", "work")
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Title" || n.Preview != "Body here" || n.Folder != "work" {
		t.Fatalf("create: %+v", n)
	}

	updated, err := st.UpdateNote(u.ID, n.ID, "New title\nNew body", "Inbox")
	if err != nil {
		t.Fatal(err)
	}
	if updated.Title != "New title" || updated.Folder != "" {
		t.Fatalf("update: %+v", updated)
	}

	if _, err := st.UpdateNote(u.ID, n.ID+999, "x", ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing: %v", err)
	}
	if _, err := st.FindNote(u.ID, n.ID+999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find missing: %v", err)
	}
	if err := st.DeleteNote(u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindNote(u.ID, n.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("find deleted: %v", err)
	}
}

func TestOpenDraftReusesBlank(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)

	a, err := st.OpenDraft(u.ID, "work")
	if err != nil {
		t.Fatal(err)
	}
	b, err := st.OpenDraft(u.ID, "work")
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID {
		t.Fatalf("draft not reused: %d vs %d", a.ID, b.ID)
	}
	rows, _ := st.ListNotes(u.ID, "all", "")
	if len(rows) != 1 {
		t.Fatalf("expected 1 draft, got %d", len(rows))
	}
	// A draft in another folder is separate.
	c, err := st.OpenDraft(u.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == a.ID {
		t.Fatal("inbox draft collided with work draft")
	}
	// Non-blank notes are never treated as drafts.
	if _, err := st.UpdateNote(u.ID, a.ID, "real", "work"); err != nil {
		t.Fatal(err)
	}
	d, err := st.OpenDraft(u.ID, "work")
	if err != nil {
		t.Fatal(err)
	}
	if d.ID == a.ID {
		t.Fatal("filled note reused as draft")
	}
	if err := st.DeleteBlankDraftsExcept(u.ID, d.ID); err != nil {
		t.Fatal(err)
	}
	rows, _ = st.ListNotes(u.ID, "all", "")
	if len(rows) != 2 { // real note + kept draft
		t.Fatalf("cleanup left %d rows", len(rows))
	}
}

func TestListNotesFilters(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)
	if _, err := st.CreateNote(u.ID, "inbox one", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNote(u.ID, "work deploy", "work"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNote(u.ID, "100% done_thing", "work"); err != nil {
		t.Fatal(err)
	}

	rows, _ := st.ListNotes(u.ID, "all", "")
	if len(rows) != 3 {
		t.Fatalf("all: %d", len(rows))
	}
	rows, _ = st.ListNotes(u.ID, "inbox", "")
	if len(rows) != 1 || rows[0].Title != "inbox one" {
		t.Fatalf("inbox: %+v", rows)
	}
	rows, _ = st.ListNotes(u.ID, "work", "deploy")
	if len(rows) != 1 {
		t.Fatalf("folder+q: %+v", rows)
	}
	// LIKE metacharacters are escaped: they match literally, not as wildcards.
	rows, _ = st.ListNotes(u.ID, "all", "100% done_thing")
	if len(rows) != 1 {
		t.Fatalf("escaped query: %+v", rows)
	}
	rows, _ = st.ListNotes(u.ID, "all", "100_")
	if len(rows) != 0 {
		t.Fatalf("underscore must not wildcard: %+v", rows)
	}

	counts, err := st.FolderCounts(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts[""] != 1 || counts["work"] != 2 {
		t.Fatalf("counts: %+v", counts)
	}
}

func TestRenameFolder(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)
	if _, err := st.CreateNote(u.ID, "a", "work"); err != nil {
		t.Fatal(err)
	}

	to, ok, err := st.RenameFolder(u.ID, "work", "job")
	if err != nil || !ok || to != "job" {
		t.Fatalf("rename: %q %v %v", to, ok, err)
	}
	rows, _ := st.ListNotes(u.ID, "job", "")
	if len(rows) != 1 {
		t.Fatalf("renamed notes missing: %+v", rows)
	}
	for _, tc := range [][2]string{{"all", "x"}, {"", "x"}, {"job", "inbox"}, {"job", "all"}, {"job", ""}} {
		if _, ok, _ := st.RenameFolder(u.ID, tc[0], tc[1]); ok {
			t.Errorf("reserved rename %q->%q allowed", tc[0], tc[1])
		}
	}
	// Same-name rename is a no-op success.
	if to, ok, _ := st.RenameFolder(u.ID, "job", "job"); !ok || to != "job" {
		t.Fatalf("same-name rename: %q %v", to, ok)
	}
}

func TestDeleteFolder(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)
	if _, err := st.CreateNote(u.ID, "a", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateNote(u.ID, "b", "work"); err != nil {
		t.Fatal(err)
	}
	n, err := st.DeleteFolder(u.ID, "inbox")
	if err != nil || n != 1 {
		t.Fatalf("clear inbox: %d %v", n, err)
	}
	n, err = st.DeleteFolder(u.ID, "work")
	if err != nil || n != 1 {
		t.Fatalf("clear work: %d %v", n, err)
	}
	if got := st.CountNotes(u.ID); got != 0 {
		t.Fatalf("notes left: %d", got)
	}
}

func TestShareTokenRoundTrip(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)
	n, _ := st.CreateNote(u.ID, "shared", "")

	token, err := st.GenerateShareToken(u.ID, n.ID)
	if err != nil || token == "" {
		t.Fatal(err)
	}
	found, err := st.FindNoteByShareToken(token)
	if err != nil || found.ID != n.ID {
		t.Fatalf("lookup: %+v %v", found, err)
	}
	rotated, err := st.GenerateShareToken(u.ID, n.ID)
	if err != nil || rotated == token {
		t.Fatalf("rotate: %q %v", rotated, err)
	}
	if err := st.RevokeShareToken(u.ID, n.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindNoteByShareToken(rotated); !errors.Is(err, ErrNotFound) {
		t.Fatalf("revoked token still resolves: %v", err)
	}
}

func TestBodyAfterTitle(t *testing.T) {
	if got := BodyAfterTitle("Title\n\nBody line"); got != "\nBody line" {
		t.Fatalf("got %q", got)
	}
	if got := BodyAfterTitle("\n\nTitle\nRest"); got != "Rest" {
		t.Fatalf("leading blanks: %q", got)
	}
	if got := BodyAfterTitle("Only"); got != "" {
		t.Fatalf("single line: %q", got)
	}
}

func TestTokens(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st)

	if _, _, err := st.CreateToken(u.ID, "  "); !errors.Is(err, ErrTokenNameBlank) {
		t.Fatalf("blank name: %v", err)
	}
	tok, raw, err := st.CreateToken(u.ID, "Hermes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, TokenPrefix) || len(tok.Prefix) != 12 {
		t.Fatalf("raw shape: %q prefix %q", raw, tok.Prefix)
	}
	found, err := st.AuthenticateToken(raw)
	if err != nil || found.ID != tok.ID {
		t.Fatalf("auth: %+v %v", found, err)
	}
	if _, err := st.AuthenticateToken("kura_nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("bad token: %v", err)
	}
	if _, err := st.AuthenticateToken(""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty token: %v", err)
	}

	for i := 0; i < TokenCap-1; i++ {
		if _, _, err := st.CreateToken(u.ID, "extra"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateToken(u.ID, "overflow"); !errors.Is(err, ErrTokenTooMany) {
		t.Fatalf("cap: %v", err)
	}
	list, err := st.ListTokens(u.ID)
	if err != nil || len(list) != TokenCap {
		t.Fatalf("list: %d %v", len(list), err)
	}
	if err := st.DeleteToken(u.ID, tok.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AuthenticateToken(raw); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted token still works: %v", err)
	}
	if err := st.TouchTokenLastUsed(list[1].ID); err != nil {
		t.Fatal(err)
	}
	list, _ = st.ListTokens(u.ID)
	if list[0].LastUsedAt == nil && list[1].LastUsedAt == nil {
		t.Fatal("last_used_at never set")
	}
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
