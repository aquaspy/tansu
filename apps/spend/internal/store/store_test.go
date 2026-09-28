package store

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func seedUser(t *testing.T, st *Store, email string) *User {
	t.Helper()
	digest, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	u, err := st.CreateUser(email, string(digest))
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func strptr(s string) *string { return &s }
func intptr(n int64) *int64   { return &n }

func TestUserDefaultsAndNormalize(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "  Ada@Example.com ")
	if u.Email != "ada@example.com" {
		t.Fatalf("email: %q", u.Email)
	}
	if u.HomeCurrency != "BRL" || u.MonthlyIncomeCents != 0 || u.IncomeCurrency != "BRL" {
		t.Fatalf("defaults: %+v", u)
	}
	if len(FXHash(u)) != 0 {
		t.Fatalf("fx: %v", FXHash(u))
	}
}

func TestFXHashIgnoresHomeAndBlanks(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "lin@example.com")
	updated, fails, err := st.UpdateSettings(u.ID, "BRL", "10.000,00", "BRL",
		map[string]string{"USD": "5.45", "EUR": " ", "BRL": "1", "GBP": "7"})
	if err != nil || len(fails) > 0 {
		t.Fatalf("settings: %v %v", fails, err)
	}
	if got := FXHash(updated); len(got) != 1 || got["USD"] != "5.45" {
		t.Fatalf("fx: %v", got)
	}
	if updated.MonthlyIncomeCents != 1_000_000 {
		t.Fatalf("income: %d", updated.MonthlyIncomeCents)
	}
	if RateFor(updated, "BRL") != "1" || RateFor(updated, "USD") != "5.45" || RateFor(updated, "EUR") != "" {
		t.Fatal("rate_for drifted")
	}
}

func TestSettingsRejectBadRates(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	for _, rate := range []string{"0", "-2", "abc", "5,45"} {
		if _, fails, err := st.UpdateSettings(u.ID, "BRL", "", "BRL",
			map[string]string{"USD": rate}); err != nil || len(fails) == 0 {
			t.Errorf("rate %q accepted: %v %v", rate, fails, err)
		}
	}
	if _, fails, _ := st.UpdateSettings(u.ID, "BRL", "-5", "BRL", nil); len(fails) == 0 {
		t.Error("negative income accepted")
	}
}

func TestExpenseParsesAmountAndRequiresTitle(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	e, fails, err := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("Coffee"), Amount: strptr("12,50"),
		Currency: strptr("BRL"), SpentOn: strptr("2026-08-25")})
	if err != nil || len(fails) > 0 {
		t.Fatalf("create: %v %v", fails, err)
	}
	if e.AmountCents != 1250 || e.SpentOn != "2026-08-25" {
		t.Fatalf("row: %+v", e)
	}
	if _, fails, _ := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("  "), Amount: strptr("1"), SpentOn: strptr("2026-08-25")}); len(fails) == 0 {
		t.Fatal("blank title accepted")
	}
	if _, fails, _ := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("X"), Amount: strptr("nope"), SpentOn: strptr("2026-08-25")}); len(fails) == 0 {
		t.Fatal("bad amount accepted")
	}
	if _, fails, _ := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("X"), Amount: strptr("1"), SpentOn: strptr("yesterday")}); len(fails) == 0 {
		t.Fatal("bad date accepted")
	}
	badCat := "planes"
	if _, fails, _ := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("X"), Amount: strptr("1"), SpentOn: strptr("2026-08-25"),
		Category: &badCat}); len(fails) == 0 {
		t.Fatal("bad category accepted")
	}
}

func TestExpenseCentsWinAndUpdateKeeps(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	e, fails, err := st.CreateExpense(u.ID, ExpensePatch{
		Title: strptr("X"), Amount: strptr("99"), AmountCents: intptr(1999),
		SpentOn: strptr("2026-08-25")})
	if err != nil || len(fails) > 0 || e.AmountCents != 1999 {
		t.Fatalf("cents win: %+v %v %v", e, fails, err)
	}
	leisure := "leisure"
	updated, fails, err := st.UpdateExpense(u.ID, e.ID, ExpensePatch{Category: &leisure})
	if err != nil || len(fails) > 0 {
		t.Fatalf("update: %v %v", fails, err)
	}
	if updated.Category != "leisure" || updated.AmountCents != 1999 {
		t.Fatalf("patch: %+v", updated)
	}
}

func TestExpenseScopedToUser(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	a := seedUser(t, st, "a@example.com")
	b := seedUser(t, st, "b@example.com")
	e, _, _ := st.CreateExpense(a.ID, ExpensePatch{
		Title: strptr("Secret"), Amount: strptr("5"), SpentOn: strptr("2026-08-25")})
	if _, err := st.FindExpense(b.ID, e.ID); err != ErrNotFound {
		t.Fatalf("cross-user read: %v", err)
	}
}

func TestSubscriptionYearlyIsATwelfthAndLegacyColumnsStay(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	yearly, fails, err := st.CreateSubscription(u.ID, SubscriptionPatch{
		Title: strptr("Domain"), Amount: strptr("120"), Interval: strptr("yearly")})
	if err != nil || len(fails) > 0 {
		t.Fatalf("create: %v %v", fails, err)
	}
	if !yearly.AppliesIn(2026, 3) || !yearly.AppliesIn(2026, 8) {
		t.Fatal("active yearly should count every month")
	}
	if yearly.MonthCents(8) != 1000 || yearly.MonthCents(12) != 1000 {
		t.Fatalf("even twelfth: %d %d", yearly.MonthCents(8), yearly.MonthCents(12))
	}
	monthly, _, _ := st.CreateSubscription(u.ID, SubscriptionPatch{
		Title: strptr("Netflix"), Amount: strptr("50")})
	if !monthly.AppliesIn(2026, 1) || monthly.MonthCents(8) != monthly.AmountCents {
		t.Fatal("monthly should count the full amount")
	}
	off := false
	paused, _, _ := st.UpdateSubscription(u.ID, monthly.ID, SubscriptionPatch{Active: &off})
	if paused.AppliesIn(2026, 8) {
		t.Fatal("inactive should not count")
	}

	// 200.01 a year: eleven months get 1666 cents, December keeps the rest.
	odd, _, err := st.CreateSubscription(u.ID, SubscriptionPatch{
		Title: strptr("Odd"), AmountCents: intptr(20001), Interval: strptr("yearly")})
	if err != nil {
		t.Fatal(err)
	}
	if odd.MonthCents(1) != 1666 || odd.MonthCents(12) != 20001-1666*11 {
		t.Fatalf("remainder: %d %d", odd.MonthCents(1), odd.MonthCents(12))
	}

	var due, month sql.NullInt64
	if err := st.DB().QueryRow(`SELECT due_day, billing_month FROM subscriptions WHERE id = ?`, yearly.ID).
		Scan(&due, &month); err != nil {
		t.Fatal(err)
	}
	if due.Valid || month.Valid {
		t.Fatalf("new row wrote legacy columns: due=%v month=%v", due, month)
	}
	if _, err := st.DB().Exec(`UPDATE subscriptions SET due_day = 10, billing_month = 3 WHERE id = ?`, yearly.ID); err != nil {
		t.Fatal(err)
	}
	renamed := "Domain renamed"
	updated, fails, err := st.UpdateSubscription(u.ID, yearly.ID, SubscriptionPatch{Title: &renamed})
	if err != nil || len(fails) > 0 || updated.Title != renamed {
		t.Fatalf("rename: %+v %v %v", updated, fails, err)
	}
	if err := st.DB().QueryRow(`SELECT due_day, billing_month FROM subscriptions WHERE id = ?`, yearly.ID).
		Scan(&due, &month); err != nil {
		t.Fatal(err)
	}
	if !due.Valid || due.Int64 != 10 || !month.Valid || month.Int64 != 3 {
		t.Fatalf("update wiped legacy columns: due=%v month=%v", due, month)
	}
	if !updated.AppliesIn(2026, 8) {
		t.Fatal("stored billing month must not gate leftover")
	}
}

func TestPaymentDayClampsToMonthEnd(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	due := "31"
	d, fails, err := st.CreatePaymentDay(u.ID,
		PaymentDayPatch{Title: strptr("Card"), DueDay: &due})
	if err != nil || len(fails) > 0 {
		t.Fatalf("create: %v %v", fails, err)
	}
	if got := d.DueOn(2026, 2).Format("2006-01-02"); got != "2026-02-28" {
		t.Fatalf("feb: %s", got)
	}
	if got := d.DueOn(2026, 8).Format("2006-01-02"); got != "2026-08-31" {
		t.Fatalf("aug: %s", got)
	}
	if _, fails, _ := st.CreatePaymentDay(u.ID,
		PaymentDayPatch{Title: strptr("X")}); len(fails) == 0 {
		t.Fatal("missing due_day accepted")
	}
	bad := "32"
	if _, fails, _ := st.CreatePaymentDay(u.ID,
		PaymentDayPatch{Title: strptr("X"), DueDay: &bad}); len(fails) == 0 {
		t.Fatal("due_day 32 accepted")
	}
}

func TestTokenLifecycle(t *testing.T) {
	st, _ := Open(":memory:")
	defer st.Close()
	u := seedUser(t, st, "ada@example.com")
	tok, raw, err := st.CreateToken(u.ID, "hermes")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, "kura_") || !strings.HasPrefix(tok.Prefix, "kura_") {
		t.Fatalf("prefix: %q %q", raw, tok.Prefix)
	}
	found, err := st.AuthenticateToken(raw)
	if err != nil || found.ID != tok.ID {
		t.Fatalf("auth: %v", err)
	}
	if _, err := st.AuthenticateToken("kura_bogus"); err == nil {
		t.Fatal("bogus token accepted")
	}
	if _, _, err := st.CreateToken(u.ID, "  "); err != ErrTokenNameBlank {
		t.Fatalf("blank name: %v", err)
	}
	for i := 1; i < TokenCap; i++ {
		if _, _, err := st.CreateToken(u.ID, "t"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := st.CreateToken(u.ID, "one more"); err != ErrTokenTooMany {
		t.Fatalf("cap: %v", err)
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
		`CREATE TABLE users (id INTEGER PRIMARY KEY AUTOINCREMENT, email TEXT NOT NULL UNIQUE, password_digest TEXT NOT NULL, fx TEXT NOT NULL DEFAULT '{}', home_currency TEXT NOT NULL DEFAULT 'BRL', income_currency TEXT NOT NULL DEFAULT 'BRL', monthly_income_cents INTEGER NOT NULL DEFAULT 0, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
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
