package store

import (
	"testing"

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

func seedClient(t *testing.T, st *Store) {
	t.Helper()
	err := st.SeedClients([]SeedClient{{
		ID: "kurapeople", Secret: "test-secret-16-chars",
		Name: "TansuPeople", Home: "http://127.0.0.1:3005/", Icon: "🧑",
		RedirectURIs: []string{"http://127.0.0.1:3005/login/kura/callback"},
	}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestClientSeedAndAuth(t *testing.T) {
	st := openTest(t)
	seedClient(t, st)
	c, err := st.FindClient("kurapeople")
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "TansuPeople" || len(c.RedirectURIs) != 1 {
		t.Fatalf("client: %+v", c)
	}
	if !c.AllowsRedirect("http://127.0.0.1:3005/login/kura/callback") {
		t.Fatal("exact redirect rejected")
	}
	if c.AllowsRedirect("http://127.0.0.1:3005/login/kura/callback/evil") ||
		c.AllowsRedirect("http://evil.example.com/") || c.AllowsRedirect("") {
		t.Fatal("non-exact redirect accepted")
	}
	if _, err := st.AuthenticateClient("kurapeople", "test-secret-16-chars"); err != nil {
		t.Fatalf("auth: %v", err)
	}
	if _, err := st.AuthenticateClient("kurapeople", "wrong"); err != ErrNotFound {
		t.Fatalf("bad secret: %v", err)
	}
	if _, err := st.AuthenticateClient("nope", "test-secret-16-chars"); err != ErrNotFound {
		t.Fatalf("unknown client: %v", err)
	}
	// Re-seed is a no-op; removal cascades.
	seedClient(t, st)
	if err := st.SeedClients(nil); err != nil {
		t.Fatal(err)
	}
	if _, err := st.FindClient("kurapeople"); err != ErrNotFound {
		t.Fatalf("removed client still there: %v", err)
	}
}

func TestAuthCodeSingleUse(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	seedClient(t, st)
	redir := "http://127.0.0.1:3005/login/kura/callback"
	raw, err := st.CreateAuthCode("kurapeople", u.ID, redir, "challenge")
	if err != nil {
		t.Fatal(err)
	}
	got, err := st.RedeemAuthCode(raw, "kurapeople", redir)
	if err != nil || got.UserID != u.ID || got.Challenge != "challenge" {
		t.Fatalf("redeem: %+v %v", got, err)
	}
	if _, err := st.RedeemAuthCode(raw, "kurapeople", redir); err != ErrNotFound {
		t.Fatalf("replay accepted: %v", err)
	}
	// Wrong client or redirect burns the code.
	raw2, _ := st.CreateAuthCode("kurapeople", u.ID, redir, "c")
	if _, err := st.RedeemAuthCode(raw2, "other", redir); err != ErrNotFound {
		t.Fatalf("wrong client: %v", err)
	}
	if _, err := st.RedeemAuthCode(raw2, "kurapeople", redir); err != ErrNotFound {
		t.Fatalf("burned code reusable: %v", err)
	}
	// Redemption links the app.
	ids, err := st.LinkedClientIDs(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != "kurapeople" {
		t.Fatalf("links: %+v", ids)
	}
}

func TestAccessTokenRoundTrip(t *testing.T) {
	st := openTest(t)
	u := seedUser(t, st, "ada@example.com")
	seedClient(t, st)
	raw, err := st.CreateAccessToken("kurapeople", u.ID)
	if err != nil {
		t.Fatal(err)
	}
	id, client, err := st.FindAccessToken("  " + raw + " ")
	if err != nil || id != u.ID || client != "kurapeople" {
		t.Fatalf("lookup: %d %s %v", id, client, err)
	}
	if _, _, err := st.FindAccessToken("nope"); err != ErrNotFound {
		t.Fatalf("bogus: %v", err)
	}
}

func TestEmailUnique(t *testing.T) {
	st := openTest(t)
	seedUser(t, st, "ada@example.com")
	if _, err := st.CreateUser("Ada@Example.com", "digest"); !IsUniqueViolation(err) {
		t.Fatalf("expected unique violation, got %v", err)
	}
}

func TestReclaimSpaceNeverBreaks(t *testing.T) {
	st := openTest(t)
	st.ReclaimSpace()
	st2, err := Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	st2.Close()
	st2.ReclaimSpace()
}
