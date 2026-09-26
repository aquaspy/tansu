package spend

import (
	"testing"
	"time"

	"github.com/aquasp/kuraspend/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func seedSummaryUser(t *testing.T, st *store.Store) *store.User {
	t.Helper()
	digest, _ := bcrypt.GenerateFromPassword([]byte("secret-password"), bcrypt.MinCost)
	u, err := st.CreateUser("ada@example.com", string(digest))
	if err != nil {
		t.Fatal(err)
	}
	u, _, err = st.UpdateSettings(u.ID, "BRL", "10000", "BRL", map[string]string{"USD": "5.45"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func strptr(s string) *string { return &s }

func TestLeftoverSubtractsSubsAndExpenses(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedSummaryUser(t, st)
	today := time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC)

	mustSub := func(title string, cents int64, interval, billing string) {
		t.Helper()
		p := store.SubscriptionPatch{Title: strptr(title), AmountCents: &cents,
			Currency: strptr("BRL"), Interval: strptr(interval)}
		if billing != "" {
			p.BillingMonth = &billing
		}
		if _, fails, err := st.CreateSubscription(u.ID, p); err != nil || len(fails) > 0 {
			t.Fatalf("sub %s: %v %v", title, fails, err)
		}
	}
	mustSub("Netflix", 5_000, "monthly", "")
	mustSub("Domain", 12_000, "yearly", "3")
	mustSub("Insurance", 20_000, "yearly", "8")

	for _, d := range []string{"10", "8"} {
		day, title := d, "Water"
		if d == "8" {
			title = "Card"
		}
		if _, fails, err := st.CreatePaymentDay(u.ID,
			store.PaymentDayPatch{Title: strptr(title), DueDay: &day}); err != nil || len(fails) > 0 {
			t.Fatalf("day: %v %v", fails, err)
		}
	}
	mustExp := func(title string, cents int64, cur, on string) {
		t.Helper()
		if _, fails, err := st.CreateExpense(u.ID, store.ExpensePatch{
			Title: strptr(title), AmountCents: &cents, Currency: strptr(cur),
			SpentOn: strptr(on)}); err != nil || len(fails) > 0 {
			t.Fatalf("exp %s: %v %v", title, fails, err)
		}
	}
	mustExp("Coffee", 1_500, "BRL", "2026-08-12")
	mustExp("USD snack", 1_000, "USD", "2026-08-13")

	s := NewSummary(st, u, 2026, 8, today)
	if s.Err() != nil {
		t.Fatal(s.Err())
	}
	if got := s.IncomeHomeCents(); got != 1_000_000 {
		t.Errorf("income: %d", got)
	}
	if got := s.SubscriptionsHomeCents(); got != 25_000 {
		t.Errorf("subs: %d", got)
	}
	if got := s.ExpensesHomeCents(); got != 6_950 {
		t.Errorf("expenses: %d (want 1500 + 5450)", got)
	}
	if got := s.LeftoverCents(); got != 968_050 {
		t.Errorf("leftover: %d", got)
	}
	if len(s.PaymentDays()) != 2 {
		t.Errorf("days: %d", len(s.PaymentDays()))
	}
	for _, d := range s.PaymentDays() {
		if !d.Overdue {
			t.Errorf("day %s should be overdue", d.Title)
		}
	}
	if len(s.MissingRateCurrencies()) != 0 {
		t.Errorf("missing: %v", s.MissingRateCurrencies())
	}
}

func TestYearlyOutOfMonthAndInactiveDoNotCount(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedSummaryUser(t, st)
	inactive := false
	billing := "1"
	if _, fails, err := st.CreateSubscription(u.ID, store.SubscriptionPatch{
		Title: strptr("Old"), AmountCents: int64ptr(9_000), Currency: strptr("BRL"),
		Interval: strptr("monthly"), Active: &inactive}); err != nil || len(fails) > 0 {
		t.Fatalf("inactive: %v %v", fails, err)
	}
	if _, fails, err := st.CreateSubscription(u.ID, store.SubscriptionPatch{
		Title: strptr("Domain"), AmountCents: int64ptr(12_000), Currency: strptr("BRL"),
		Interval: strptr("yearly"), BillingMonth: &billing}); err != nil || len(fails) > 0 {
		t.Fatalf("yearly: %v %v", fails, err)
	}
	s := NewSummary(st, u, 2026, 8, time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC))
	if got := s.SubscriptionsHomeCents(); got != 0 {
		t.Errorf("subs: %d", got)
	}
	if len(s.SubscriptionRows()) != 1 {
		t.Errorf("rows: %d", len(s.SubscriptionRows()))
	}
	if len(s.Subscriptions()) != 0 {
		t.Errorf("counting: %d", len(s.Subscriptions()))
	}
}

func TestMissingRatesSkippedAndTodayFlagged(t *testing.T) {
	st, _ := store.Open(":memory:")
	defer st.Close()
	u := seedSummaryUser(t, st)
	due := "25"
	if _, fails, err := st.CreatePaymentDay(u.ID,
		store.PaymentDayPatch{Title: strptr("Power"), DueDay: &due}); err != nil || len(fails) > 0 {
		t.Fatalf("day: %v %v", fails, err)
	}
	if _, fails, err := st.CreateExpense(u.ID, store.ExpensePatch{
		Title: strptr("Euro"), AmountCents: int64ptr(2_000), Currency: strptr("EUR"),
		SpentOn: strptr("2026-08-02")}); err != nil || len(fails) > 0 {
		t.Fatalf("exp: %v %v", fails, err)
	}
	s := NewSummary(st, u, 2026, 8, time.Date(2026, 8, 25, 0, 0, 0, 0, time.UTC))
	if got := s.ExpensesHomeCents(); got != 0 {
		t.Errorf("expenses: %d", got)
	}
	missing := s.MissingRateCurrencies()
	if len(missing) != 1 || missing[0] != "EUR" {
		t.Errorf("missing: %v", missing)
	}
	if !s.PaymentDays()[0].DueToday {
		t.Error("day should be due today")
	}
	if s.PaymentDays()[0].DueToday && s.PaymentDays()[0].Overdue {
		t.Error("day due today must not be overdue")
	}
}

func int64ptr(n int64) *int64 { return &n }
