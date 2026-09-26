package spend

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aquasp/kuraspend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func seedImportUser(t *testing.T, st *store.Store, email string) *store.User {
	t.Helper()
	digest, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	u, err := st.CreateUser(email, string(digest))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustCreate(t *testing.T, st *store.Store, u *store.User) {
	t.Helper()
	cents := int64(3400)
	title, cur, interval := "Spotify", "BRL", "monthly"
	if _, fails, err := st.CreateSubscription(u.ID, store.SubscriptionPatch{
		Title: &title, AmountCents: &cents, Currency: &cur, Interval: &interval}); err != nil || len(fails) > 0 {
		t.Fatalf("sub: %v %v", fails, err)
	}
	day, ptitle := "10", "Water"
	if _, fails, err := st.CreatePaymentDay(u.ID,
		store.PaymentDayPatch{Title: &ptitle, DueDay: &day}); err != nil || len(fails) > 0 {
		t.Fatalf("day: %v %v", fails, err)
	}
	lunch, on := "Lunch", "2026-08-20"
	lcents := int64(2000)
	if _, fails, err := st.CreateExpense(u.ID, store.ExpensePatch{
		Title: &lunch, AmountCents: &lcents, Currency: &cur,
		SpentOn: &on}); err != nil || len(fails) > 0 {
		t.Fatalf("exp: %v %v", fails, err)
	}
}

func TestExportShape(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedImportUser(t, st, "ada@example.com")
	mustCreate(t, st, u)

	raw, err := Export(st, u)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["app"] != "TansuSpend" {
		t.Fatalf("app: %v", payload["app"])
	}
	subs := payload["subscriptions"].([]any)
	if len(subs) != 1 {
		t.Fatalf("subs: %d", len(subs))
	}
	sub := subs[0].(map[string]any)
	if sub["title"] != "Spotify" || sub["amount_cents"] != 3400.0 || sub["active"] != true {
		t.Fatalf("sub row: %+v", sub)
	}
	// Key order matches the Rails as_export hashes.
	text := string(raw)
	for _, want := range []string{`"title": "Spotify"`, `"due_day": 10`, `"spent_on": "2026-08-20"`} {
		if !strings.Contains(text, want) {
			t.Errorf("export missing %s", want)
		}
	}
	if i := strings.Index(text, `"title"`); i < 0 || strings.Index(text, `"amount_cents"`) < i {
		t.Error("export key order drifted")
	}
}

func TestImportExportRoundTrip(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedImportUser(t, st, "ada@example.com")
	mustCreate(t, st, u)
	raw, err := Export(st, u)
	if err != nil {
		t.Fatal(err)
	}

	other := seedImportUser(t, st, "other@example.com")
	count, err := Import(st, other.ID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count: %d", count)
	}
	if st.CountSubscriptions(other.ID) != 1 || st.CountPaymentDays(other.ID) != 1 || st.CountExpenses(other.ID) != 1 {
		t.Fatal("rows did not round-trip")
	}
	// A second import appends again (imports never upsert).
	if _, err := Import(st, other.ID, raw); err != nil {
		t.Fatal(err)
	}
	if st.CountExpenses(other.ID) != 2 {
		t.Fatal("re-import should append")
	}
}

func TestImportRejectsJunk(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedImportUser(t, st, "ada@example.com")
	for _, raw := range []string{`not json`, `[1,2]`, `{"app":"KuraNotes"}`, `{"app":"Nope"}`} {
		if _, err := Import(st, u.ID, []byte(raw)); err == nil {
			t.Errorf("Import(%q) should fail", raw)
		}
	}
	// Rows without the app marker still import.
	count, err := Import(st, u.ID, []byte(`{"expenses":[{"title":"X","amount_cents":100,"spent_on":"2026-08-01"}]}`))
	if err != nil || count != 1 {
		t.Fatalf("bare rows: %d %v", count, err)
	}
}

func TestImportLegacyBillsAndDueOn(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedImportUser(t, st, "ada@example.com")
	count, err := Import(st, u.ID, []byte(`{"app":"KuraSpend",
		"bills":[{"title":"Legacy","due_on":"2026-09-10"}],
		"payment_days":[],
		"subscriptions":[{"title":"Bad","amount_cents":0}],
		"expenses":[{"title":"No date","amount_cents":100}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count: %d (invalid rows must skip silently)", count)
	}
	days, _ := st.ListPaymentDays(u.ID)
	if len(days) != 1 || days[0].DueDay != 10 {
		t.Fatalf("due_on parse: %+v", days)
	}
}

func TestImportActiveFalsyStrings(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedImportUser(t, st, "ada@example.com")
	count, err := Import(st, u.ID, []byte(`{"app":"TansuSpend","subscriptions":[
		{"title":"A","amount_cents":100,"active":"off"},
		{"title":"B","amount_cents":100,"active":"0"},
		{"title":"C","amount_cents":100}
	]}`))
	if err != nil || count != 3 {
		t.Fatalf("count: %d %v", count, err)
	}
	subs, _ := st.ListSubscriptions(u.ID, false)
	byTitle := map[string]bool{}
	for _, s := range subs {
		byTitle[s.Title] = s.Active
	}
	if byTitle["A"] || byTitle["B"] || !byTitle["C"] {
		t.Fatalf("active flags: %+v", byTitle)
	}
}
