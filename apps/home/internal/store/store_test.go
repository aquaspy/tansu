package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestProfileValidation(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")

	if errs := st.ValidateProfile(u.ID, 0, "  ", true); len(errs) != 1 || errs[0].Code != "blank" {
		t.Fatalf("blank errs = %+v", errs)
	}
	long := strings.Repeat("x", 41)
	if errs := st.ValidateProfile(u.ID, 0, long, true); len(errs) != 1 || errs[0].Code != "too_long" {
		t.Fatalf("long errs = %+v", errs)
	}
	if _, err := st.CreateProfile(u.ID, "Work", 0); err != nil {
		t.Fatal(err)
	}
	if errs := st.ValidateProfile(u.ID, 0, "work", true); len(errs) != 1 || errs[0].Code != "taken" {
		t.Fatalf("taken errs = %+v", errs)
	}
	// The same name on another user is fine.
	other, _ := st.CreateUser("bob@example.com", "digest")
	if errs := st.ValidateProfile(other.ID, 0, "work", true); len(errs) != 0 {
		t.Fatalf("other user errs = %+v", errs)
	}
	// Updating the same record keeps its name.
	p, _ := st.CreateProfile(other.ID, "Personal", 0)
	if errs := st.ValidateProfile(other.ID, p.ID, "personal", false); len(errs) != 0 {
		t.Fatalf("self update errs = %+v", errs)
	}
}

func TestProfileCapAndDestroyable(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	first, _ := st.CreateProfile(u.ID, "P0", 0)
	if st.ProfileDestroyable(u.ID, first.ID) {
		t.Fatal("last profile destroyable")
	}
	second, _ := st.CreateProfile(u.ID, "P1", 1)
	if !st.ProfileDestroyable(u.ID, second.ID) {
		t.Fatal("second profile not destroyable")
	}
	// Fill to the cap with distinct names for the cap check.
	st2 := openTest(t)
	u2, _ := st2.CreateUser("zed@example.com", "digest")
	for i := 0; i < MaxProfilesPerUser; i++ {
		name := string(rune('A' + i))
		if _, err := st2.CreateProfile(u2.ID, "Profile-"+name, i); err != nil {
			t.Fatal(err)
		}
	}
	errs := st2.ValidateProfile(u2.ID, 0, "One more", true)
	if len(errs) != 1 || errs[0].Code != "too_many" {
		t.Fatalf("cap errs = %+v", errs)
	}
	profiles, _ := st2.ListProfiles(u2.ID)
	if !st2.ProfileDestroyable(u2.ID, profiles[0].ID) {
		t.Fatal("second profile not destroyable")
	}
}

func TestSiteNormalizeAndValidate(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	p, _ := st.CreateProfile(u.ID, "Personal", 0)

	site := &Site{ProfileID: p.ID, Title: "", URL: "faculdade.edu.br", Hint: " Portal "}
	NormalizeSite(site)
	if site.URL != "https://faculdade.edu.br" {
		t.Fatalf("url = %q", site.URL)
	}
	if site.Title != "faculdade.edu.br" {
		t.Fatalf("title = %q", site.Title)
	}
	if site.Hint != "Portal" {
		t.Fatalf("hint = %q", site.Hint)
	}
	if errs := st.ValidateSite(p.ID, site, true); len(errs) != 0 {
		t.Fatalf("errs = %+v", errs)
	}

	bad := &Site{ProfileID: p.ID, Title: "X", URL: "javascript:alert(1)"}
	NormalizeSite(bad)
	if errs := ValidateSiteFields(bad); len(errs) != 1 || errs[0].Code != "invalid" {
		t.Fatalf("js url errs = %+v", errs)
	}
	empty := &Site{ProfileID: p.ID}
	NormalizeSite(empty)
	errs := ValidateSiteFields(empty)
	if len(errs) != 2 {
		t.Fatalf("empty errs = %+v", errs)
	}
}

func TestSiteOwnershipAndReorder(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	p, _ := st.CreateProfile(u.ID, "Personal", 0)
	a, _ := st.CreateSite(&Site{ProfileID: p.ID, Title: "A", URL: "https://a.example", Position: 0})
	b, _ := st.CreateSite(&Site{ProfileID: p.ID, Title: "B", URL: "https://b.example", Position: 1})

	other, _ := st.CreateUser("bob@example.com", "digest")
	if _, err := st.FindOwnedSite(other.ID, a.ID); err != ErrNotFound {
		t.Fatalf("foreign lookup err = %v", err)
	}
	// Foreign ids in reorder are ignored, not fatal.
	if err := st.ReorderSites(other.ID, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	sites, _ := st.ListSites(p.ID)
	if sites[0].ID != a.ID {
		t.Fatal("foreign reorder moved rows")
	}
	if err := st.ReorderSites(u.ID, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	sites, _ = st.ListSites(p.ID)
	if sites[0].ID != b.ID || sites[1].ID != a.ID {
		t.Fatalf("order = %d,%d", sites[0].ID, sites[1].ID)
	}
}

func TestStackItemValidateAndReorder(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	p, _ := st.CreateProfile(u.ID, "Personal", 0)

	item := &StackItem{ProfileID: p.ID, Category: " Music ", Choice: "Tidal", URL: "tidal.com"}
	NormalizeStackItem(item)
	if item.Category != "Music" || item.URL != "https://tidal.com" {
		t.Fatalf("normalized = %+v", item)
	}
	if !item.Linked() {
		t.Fatal("linked = false")
	}
	if errs := st.ValidateStackItem(p.ID, item, true); len(errs) != 0 {
		t.Fatalf("errs = %+v", errs)
	}
	blank := &StackItem{ProfileID: p.ID}
	NormalizeStackItem(blank)
	if errs := ValidateStackItemFields(blank); len(errs) != 2 {
		t.Fatalf("blank errs = %+v", errs)
	}
	if blank.Linked() {
		t.Fatal("blank item linked")
	}

	a, _ := st.CreateStackItem(&StackItem{ProfileID: p.ID, Category: "A", Choice: "a", Position: 0})
	b, _ := st.CreateStackItem(&StackItem{ProfileID: p.ID, Category: "B", Choice: "b", Position: 1})
	if err := st.ReorderStackItems(u.ID, []int64{b.ID, a.ID}); err != nil {
		t.Fatal(err)
	}
	items, _ := st.ListStackItems(p.ID)
	if items[0].ID != b.ID || items[1].ID != a.ID {
		t.Fatalf("order = %d,%d", items[0].ID, items[1].ID)
	}
	other, _ := st.CreateUser("bob@example.com", "digest")
	if _, err := st.FindOwnedStackItem(other.ID, a.ID); err != ErrNotFound {
		t.Fatalf("foreign lookup err = %v", err)
	}
}

func TestIconHelpers(t *testing.T) {
	if got := Host("https://www.example.com/x"); got != "example.com" {
		t.Fatalf("host = %q", got)
	}
	if got := Letter("Tidal", "https://tidal.com"); got != "T" {
		t.Fatalf("letter = %q", got)
	}
	if got := Letter("123 abc", "https://x.example"); got != "1" {
		t.Fatalf("letter = %q", got)
	}
	if got := Letter("", "https://tidal.com"); got != "T" {
		t.Fatalf("host letter = %q", got)
	}
	if got := Letter("", ""); got != "?" {
		t.Fatalf("empty letter = %q", got)
	}
	// Color is stable for the same host.
	if Color("https://a.example", "A") != Color("https://a.example", "B") {
		t.Fatal("color not host-stable")
	}
	found := false
	for _, c := range Palette {
		if Color("https://a.example", "A") == c {
			found = true
		}
	}
	if !found {
		t.Fatal("color outside palette")
	}
	if got := IconSrc("", "https://example.com"); got != "https://icons.duckduckgo.com/ip3/example.com.ico" {
		t.Fatalf("icon = %q", got)
	}
	if got := IconSrc("https://cdn.example/i.png", "https://example.com"); got != "https://cdn.example/i.png" {
		t.Fatalf("custom icon = %q", got)
	}
	if got := IconSrc("", ""); got != "" {
		t.Fatalf("empty icon = %q", got)
	}
}

func TestSessionLockUnlock(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	sess, err := st.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.UnlockedAt == nil {
		t.Fatal("fresh session locked")
	}
	if err := st.LockSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	sess, _ = st.GetSession(sess.ID)
	if sess.UnlockedAt != nil {
		t.Fatal("session still unlocked")
	}
	if err := st.UnlockSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	sess, _ = st.GetSession(sess.ID)
	if sess.UnlockedAt == nil {
		t.Fatal("session still locked")
	}
	if err := st.SetSessionProfile(sess.ID, 42); err != nil {
		t.Fatal(err)
	}
	sess, _ = st.GetSession(sess.ID)
	if sess.ProfileID != 42 {
		t.Fatalf("profile = %d", sess.ProfileID)
	}
	// The stale sweep only drops sessions unseen past the window.
	old := time.Now().Add(-31 * 24 * time.Hour).UTC().Format(timeLayout)
	if _, err := st.DB().Exec(`UPDATE sessions SET last_seen_at = ? WHERE id = ?`, old, sess.ID); err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteStaleSessions(SessionMaxAge); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetSession(sess.ID); err != ErrNotFound {
		t.Fatalf("stale session kept: %v", err)
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
