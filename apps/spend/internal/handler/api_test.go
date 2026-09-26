package handler

import (
	"net/http"
	"strconv"
	"testing"

	"github.com/aquasp/kuraspend/internal/store"
)

func storePatch(title, amount, currency, spentOn, category string) store.ExpensePatch {
	return store.ExpensePatch{
		Title: &title, Amount: &amount, Currency: &currency,
		SpentOn: &spentOn, Category: &category,
	}
}

func subscriptionSeed(title, amount, currency, interval string) store.SubscriptionPatch {
	return store.SubscriptionPatch{
		Title: &title, Amount: &amount, Currency: &currency, Interval: &interval,
	}
}

func TestAPIUnauthorized(t *testing.T) {
	f := newFlow(t, nil)
	if code, out := f.apiCall(http.MethodGet, "/api/v1/expenses", "", ""); code != http.StatusUnauthorized || out["error"] != "unauthorized" {
		t.Fatalf("no token: %d %+v", code, out)
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", "kura_bogus", ""); code != http.StatusUnauthorized {
		t.Fatalf("bad token: %d", code)
	}
}

func TestAPIExpensesMonthFilter(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	lunch, snack := "Lunch", "Bus"
	food, transport := "food", "transport"
	amt1, amt2 := "25.50", "6.00"
	day1, day2 := "2026-09-10", "2026-09-11"
	cur := "BRL"
	for _, args := range [][5]string{{lunch, amt1, day1, food, cur}, {snack, amt2, day2, transport, cur}} {
		title, amount, spent, cat, c := args[0], args[1], args[2], args[3], args[4]
		if _, fails, err := f.store.CreateExpense(u.ID, storePatch(title, amount, c, spent, cat)); err != nil || len(fails) > 0 {
			t.Fatalf("seed: %v %v", fails, err)
		}
	}

	code, out := f.apiCall(http.MethodGet, "/api/v1/expenses?month=2026-09&category=food", raw, "")
	if code != http.StatusOK {
		t.Fatalf("index: %d %+v", code, out)
	}
	expenses := out["expenses"].([]any)
	if len(expenses) != 1 {
		t.Fatalf("filtered: %+v", out)
	}
	row := expenses[0].(map[string]any)
	if row["title"] != "Lunch" || row["amount_cents"] != 2550.0 {
		t.Fatalf("row: %+v", row)
	}
}

func TestAPIExpenseCRUD(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/expenses", raw,
		`{"expense":{"title":"Coffee","amount":"4.50","currency":"BRL","spent_on":"2026-09-19","category":"food"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	created := out["expense"].(map[string]any)
	if created["amount_cents"] != 450.0 {
		t.Fatalf("cents: %+v", created)
	}
	id := strconv.FormatFloat(created["id"].(float64), 'f', 0, 64)

	code, out = f.apiCall(http.MethodGet, "/api/v1/expenses/"+id, raw, "")
	if code != http.StatusOK || out["expense"].(map[string]any)["title"] != "Coffee" {
		t.Fatalf("show: %d %+v", code, out)
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/expenses/"+id, raw, `{"expense":{"category":"leisure"}}`)
	if code != http.StatusOK || out["expense"].(map[string]any)["category"] != "leisure" {
		t.Fatalf("update: %d %+v", code, out)
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/expenses/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := f.store.FindExpense(u.ID, mustID(id)); err == nil {
		t.Fatal("row survived delete")
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/expenses/"+id, raw, "")
	if code != http.StatusNotFound || out["error"] != "not_found" {
		t.Fatalf("deleted show: %d %+v", code, out)
	}
}

func TestAPIExpensesCentsAndFlat(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	code, out := f.apiCall(http.MethodPost, "/api/v1/expenses", raw,
		`{"title":"Flat","amount_cents":1999,"currency":"USD","spent_on":"2026-09-19"}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	if out["expense"].(map[string]any)["amount_cents"] != 1999.0 {
		t.Fatalf("cents: %+v", out)
	}
}

func TestAPIInvalidExpenses(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	code, out := f.apiCall(http.MethodPost, "/api/v1/expenses", raw, `{"expense":{"title":""}}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid: %d %+v", code, out)
	}
	if len(out["errors"].([]any)) == 0 {
		t.Fatalf("errors empty: %+v", out)
	}
	code, out = f.apiCall(http.MethodGet, "/api/v1/expenses?month=september", raw, "")
	if code != http.StatusUnprocessableEntity || len(out["errors"].([]any)) == 0 {
		t.Fatalf("bad month: %d %+v", code, out)
	}
}

func TestAPISubscriptions(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/subscriptions", raw,
		`{"subscription":{"title":"Music","amount":"19.90","currency":"BRL","interval":"monthly"}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	id := strconv.FormatFloat(out["subscription"].(map[string]any)["id"].(float64), 'f', 0, 64)

	code, out = f.apiCall(http.MethodGet, "/api/v1/subscriptions", raw, "")
	if code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	rows := out["subscriptions"].([]any)
	if len(rows) != 1 || rows[0].(map[string]any)["title"] != "Music" {
		t.Fatalf("index rows: %+v", out)
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/subscriptions/"+id, raw, `{"subscription":{"active":false}}`)
	if code != http.StatusOK || out["subscription"].(map[string]any)["active"] != false {
		t.Fatalf("deactivate: %d %+v", code, out)
	}
	_, out = f.apiCall(http.MethodGet, "/api/v1/subscriptions?active=true", raw, "")
	if len(out["subscriptions"].([]any)) != 0 {
		t.Fatalf("active filter: %+v", out)
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/subscriptions/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := f.store.FindSubscription(u.ID, mustID(id)); err == nil {
		t.Fatal("row survived delete")
	}
}

func TestAPIPaymentDays(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")

	code, out := f.apiCall(http.MethodPost, "/api/v1/payment_days", raw,
		`{"payment_day":{"title":"Water","due_day":10}}`)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %+v", code, out)
	}
	id := strconv.FormatFloat(out["payment_day"].(map[string]any)["id"].(float64), 'f', 0, 64)

	code, out = f.apiCall(http.MethodGet, "/api/v1/payment_days", raw, "")
	rows := out["payment_days"].([]any)
	if code != http.StatusOK || len(rows) != 1 || rows[0].(map[string]any)["title"] != "Water" {
		t.Fatalf("index: %d %+v", code, out)
	}

	code, out = f.apiCall(http.MethodPatch, "/api/v1/payment_days/"+id, raw, `{"payment_day":{"due_day":12}}`)
	if code != http.StatusOK || out["payment_day"].(map[string]any)["due_day"] != 12.0 {
		t.Fatalf("update: %d %+v", code, out)
	}

	code, _ = f.apiCall(http.MethodDelete, "/api/v1/payment_days/"+id, raw, "")
	if code != http.StatusNoContent {
		t.Fatalf("delete: %d", code)
	}
	if _, err := f.store.FindPaymentDay(u.ID, mustID(id)); err == nil {
		t.Fatal("row survived delete")
	}
}

func TestAPIMonthSummary(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	_, raw, _ := f.store.CreateToken(u.ID, "hermes")
	if _, fails, err := f.store.UpdateSettings(u.ID, "BRL", "5000", "BRL", nil); err != nil || len(fails) > 0 {
		t.Fatalf("settings: %v %v", fails, err)
	}
	title, amount, cur, interval := "Music", "20.00", "BRL", "monthly"
	if _, fails, err := f.store.CreateSubscription(u.ID, subscriptionSeed(title, amount, cur, interval)); err != nil || len(fails) > 0 {
		t.Fatalf("sub: %v %v", fails, err)
	}
	lunch, spent, food := "Lunch", "2026-09-10", "food"
	lunchAmount, lunchCur := "30.00", "BRL"
	if _, fails, err := f.store.CreateExpense(u.ID, storePatch(lunch, lunchAmount, lunchCur, spent, food)); err != nil || len(fails) > 0 {
		t.Fatalf("exp: %v %v", fails, err)
	}

	code, out := f.apiCall(http.MethodGet, "/api/v1/months/2026/9", raw, "")
	if code != http.StatusOK {
		t.Fatalf("month: %d %+v", code, out)
	}
	month := out["month"].(map[string]any)
	if month["home_currency"] != "BRL" || month["leftover_cents"] != 495000.0 {
		t.Fatalf("totals: %+v", month)
	}
	expenses := month["expenses"].([]any)
	if len(expenses) != 1 || expenses[0].(map[string]any)["title"] != "Lunch" {
		t.Fatalf("rows: %+v", month["expenses"])
	}
	if month["missing_rate_currencies"] == nil {
		t.Fatal("missing rates must be [] not null")
	}

	code, out = f.apiCall(http.MethodGet, "/api/v1/months/2026/13", raw, "")
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("bad month: %d %+v", code, out)
	}
}

func TestAPICrossUserIsolation(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	title, amount, cur, spent := "Secret", "1.00", "BRL", "2026-09-10"
	e, _, _ := f.store.CreateExpense(u.ID, storePatch(title, amount, cur, spent, ""))
	other := f.seedUser("other@example.com", "secret-password")
	_, otherRaw, _ := f.store.CreateToken(other.ID, "other")

	code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses/"+strconv.FormatInt(e.ID, 10), otherRaw, "")
	if code != http.StatusNotFound {
		t.Fatalf("cross-user read: %d", code)
	}
}

func TestAPIRevokedAndLockedAndLastUsed(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("ada@example.com", "secret-password")
	tok, raw, _ := f.store.CreateToken(u.ID, "hermes")
	if tok.LastUsedAt != nil {
		t.Fatal("fresh token has last_used")
	}
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", raw, ""); code != http.StatusOK {
		t.Fatalf("index: %d", code)
	}
	refreshed, _ := f.store.FindToken(u.ID, tok.ID)
	if refreshed.LastUsedAt == nil {
		t.Fatal("last_used not recorded")
	}

	// Tokens keep working while the app is locked.
	f.login(u.Email, "secret-password")
	code, _, h := f.post("/lock", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("lock: %d", code)
	}
	_ = h
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", raw, ""); code != http.StatusOK {
		t.Fatalf("locked api: %d", code)
	}

	_ = f.store.DeleteToken(u.ID, tok.ID)
	if code, _ := f.apiCall(http.MethodGet, "/api/v1/expenses", raw, ""); code != http.StatusUnauthorized {
		t.Fatalf("revoked api: %d", code)
	}
}

func mustID(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}
