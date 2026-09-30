package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func openTest(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustUser(t *testing.T, s *Store, email string) *User {
	t.Helper()
	u, err := s.CreateUser(email, "digest")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestCreateUserNormalizesEmail(t *testing.T) {
	s := openTest(t)
	u, err := s.CreateUser("  ALICE@Example.COM ", "d")
	if err != nil {
		t.Fatal(err)
	}
	if u.Email != "alice@example.com" {
		t.Fatalf("email = %q", u.Email)
	}
	if _, err := s.CreateUser("alice@example.com", "d"); !IsUniqueViolation(err) {
		t.Fatalf("duplicate email err = %v", err)
	}
}

func TestOpenDraftFor(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	d1, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	d2, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d1.ID != d2.ID {
		t.Fatalf("expected same draft, got %d and %d", d1.ID, d2.ID)
	}
	if _, err := s.CreateUserMessage(d1.ID, "hi", false, false); err != nil {
		t.Fatal(err)
	}
	d3, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d3.ID == d1.ID {
		t.Fatal("expected a new draft after the old one gained a message")
	}
}

func TestOneInflightPerConversation(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	if _, err := s.CreateAssistantMessage(c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAssistantMessage(c.ID); !IsUniqueViolation(err) {
		t.Fatalf("second inflight err = %v", err)
	}
}

func TestTranscriptVisibility(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	if _, err := s.CreateUserMessage(c.ID, "hi", false, false); err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateAssistantMessage(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Transcript(c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("pending assistant should be visible, got %d rows", len(rows))
	}
	if err := s.CompleteAssistant(a.ID, Completion{Content: ""}); err != nil {
		t.Fatal(err)
	}
	rows, _ = s.Transcript(c.ID)
	if len(rows) != 1 {
		t.Fatalf("empty complete assistant should be hidden, got %d rows", len(rows))
	}
}

func TestFailStale(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	a, _ := s.CreateAssistantMessage(c.ID)
	stale, err := s.FailStale(time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].ID != a.ID {
		t.Fatalf("stale = %+v", stale)
	}
	m, _ := s.GetMessage(a.ID)
	if m.Status != StatusFailed || m.Error != "stale" {
		t.Fatalf("row = %+v", m)
	}
}

func TestDeleteConversationCascades(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	m, _ := s.CreateUserMessage(c.ID, "hi", false, false)
	if _, err := s.CreateImage(m.ID, "a.jpg", "image/jpeg", 10, "abc"); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteConversation(u.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetMessage(m.ID); err != ErrNotFound {
		t.Fatalf("message err = %v", err)
	}
	imgs, _ := s.ListImages(m.ID)
	if len(imgs) != 0 {
		t.Fatalf("images = %+v", imgs)
	}
}

func TestShareTokenRoundtrip(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	token, err := s.GenerateShareToken(u.ID, c.ID)
	if err != nil || token == "" {
		t.Fatal(err)
	}
	found, err := s.FindConversationByShareToken(token)
	if err != nil || found.ID != c.ID {
		t.Fatalf("found = %+v, err = %v", found, err)
	}
	if err := s.RevokeShareToken(u.ID, c.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FindConversationByShareToken(token); err != ErrNotFound {
		t.Fatalf("revoked err = %v", err)
	}
}

func TestSessionLockCycle(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	sess, err := s.CreateSession(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if sess.UnlockedAt == nil {
		t.Fatal("new session should be unlocked")
	}
	if err := s.LockSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	sess, _ = s.GetSession(sess.ID)
	if sess.UnlockedAt != nil {
		t.Fatal("locked session should have nil UnlockedAt")
	}
	if err := s.UnlockSession(sess.ID); err != nil {
		t.Fatal(err)
	}
	sess, _ = s.GetSession(sess.ID)
	if sess.UnlockedAt == nil {
		t.Fatal("unlocked session should have UnlockedAt")
	}
}

func TestConversationSettings(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	c, _ := s.CreateConversation(u.ID)
	if c.Model != "" || c.WebSearch || c.DeepSearch || c.Effort != "" {
		t.Fatalf("defaults = %+v", c)
	}
	if err := s.UpdateConversationSettings(u.ID, c.ID, ConversationSettings{
		Model: "x-ai/grok-4.7", Web: true, Deep: true, Effort: "low",
	}); err != nil {
		t.Fatal(err)
	}
	c, _ = s.FindConversation(u.ID, c.ID)
	if c.Model != "x-ai/grok-4.7" || !c.WebSearch || !c.DeepSearch || c.Effort != "low" {
		t.Fatalf("settings = %+v", c)
	}
}

// TestMigrateOldConversations opens a database written before the sticky
// model/search columns existed; Open must add them with defaults.
func TestMigrateOldConversations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.sqlite3")
	db, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_digest TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE conversations (id INTEGER PRIMARY KEY AUTOINCREMENT, user_id INTEGER NOT NULL, title TEXT NOT NULL DEFAULT '', summary TEXT, summarized_through_id INTEGER, share_token TEXT UNIQUE, archived_at TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE TABLE messages (id INTEGER PRIMARY KEY AUTOINCREMENT, conversation_id INTEGER NOT NULL, role TEXT NOT NULL, status TEXT, content TEXT, error TEXT, web INTEGER NOT NULL DEFAULT 0, citations TEXT, raw TEXT, token_usage TEXT, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`INSERT INTO users (email, password_digest, created_at, updated_at) VALUES ('a@x.com', 'd', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO conversations (user_id, title, created_at, updated_at) VALUES (1, 'Old', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
		`INSERT INTO messages (conversation_id, role, content, web, created_at, updated_at) VALUES (1, 'user', 'hi', 1, '2026-01-01 00:00:00', '2026-01-01 00:00:00')`,
	} {
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
	c, err := s.FindConversation(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Old" || c.Model != "" || c.WebSearch || c.DeepSearch || c.Effort != "" {
		t.Fatalf("migrated = %+v", c)
	}
	if err := s.UpdateConversationSettings(1, 1, ConversationSettings{Model: "m", Web: true}); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMessage(1)
	if err != nil || !m.Web || m.Deep {
		t.Fatalf("migrated message = %+v, err = %v", m, err)
	}
}

func TestAddUsageCost(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "v@example.com")
	c, err := s.CreateConversation(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, asst, err := s.CreateTurn(c.ID, "hi", false, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddUsageCost(asst.ID, "stt_cost_usd", 0.0001); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUsageCost(asst.ID, "tts_cost_usd", 0.0003); err != nil {
		t.Fatal(err)
	}
	if err := s.AddUsageCost(asst.ID, "tts_cost_usd", 0.0003); err != nil {
		t.Fatal(err)
	}
	m, err := s.GetMessage(asst.ID)
	if err != nil {
		t.Fatal(err)
	}
	usage := m.UsageMap()
	if usage["stt_cost_usd"] != 0.0001 || usage["tts_cost_usd"] != 0.0006 {
		t.Fatalf("usage = %v", usage)
	}
}

func TestOpenDraftForInheritsEffort(t *testing.T) {
	s := openTest(t)
	u := mustUser(t, s, "a@x.com")
	d1, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d1.Effort != "" {
		t.Fatalf("first draft effort = %q", d1.Effort)
	}
	if err := s.UpdateConversationSettings(u.ID, d1.ID, ConversationSettings{Effort: "low"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateUserMessage(d1.ID, "hi", false, false); err != nil {
		t.Fatal(err)
	}
	d2, err := s.OpenDraftFor(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if d2.ID == d1.ID {
		t.Fatal("expected a new draft after the old one gained a message")
	}
	if d2.Effort != "low" {
		t.Fatalf("new draft effort = %q", d2.Effort)
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
