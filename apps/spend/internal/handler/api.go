package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/i18n"
	"github.com/aquasp/kuraspend/internal/spend"
	"github.com/aquasp/kuraspend/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	apiUserKey  ctxKey = "api_user"
	apiTokenKey ctxKey = "api_token"
)

func apiUserOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(apiUserKey).(*store.User)
	return u
}

func apiTokenOf(r *http.Request) *store.APIToken {
	t, _ := r.Context().Value(apiTokenKey).(*store.APIToken)
	return t
}

// requireAPIToken authenticates the Bearer [REDACTED] It works while the app is
// locked, like the Rails API.
func (s *Server) requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mirrors the Rails sub(/\ABearer\s+/i, ""): the prefix is
		// optional, so a bare token still authenticates.
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(h) > 6 && strings.EqualFold(h[:6], "bearer") && (h[6] == ' ' || h[6] == '\t') {
			h = strings.TrimSpace(h[6:])
		}
		tok, err := s.Store.AuthenticateToken(h)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := s.Store.FindUser(tok.UserID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		_ = s.Store.TouchTokenLastUsed(tok.ID)
		ctx := context.WithValue(r.Context(), apiTokenKey, tok)
		ctx = context.WithValue(ctx, apiUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAPIError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeAPIErrors(w http.ResponseWriter, status int, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string][]string{"errors": errs})
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func apiWriteLimited(s *Server, w http.ResponseWriter, r *http.Request) bool {
	tok := apiTokenOf(r)
	if !s.Limiter.Allow("api:"+strconv.FormatInt(tok.ID, 10), 60, time.Minute) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

func apiFailures(w http.ResponseWriter, r *http.Request, model string, fails []store.FieldError) {
	writeAPIErrors(w, http.StatusUnprocessableEntity,
		validationMessages(LocaleOf(r), model, fails))
}

// --- param parsing ---

// apiParams accepts nested ({"expense": {...}}) or flat ({"title": ...})
// params, like the Rails controllers. Form bodies work too.
func apiParams(r *http.Request, nest string) map[string]any {
	var payload map[string]any
	_ = json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&payload)
	if payload != nil {
		if nested, ok := payload[nest].(map[string]any); ok {
			return nested
		}
		return payload
	}
	_ = r.ParseForm()
	out := map[string]any{}
	for key, values := range r.Form {
		if len(values) == 0 {
			continue
		}
		if inner, ok := strings.CutPrefix(key, nest+"["); ok {
			if field, _, ok := strings.Cut(inner, "]"); ok && field != "" {
				out[field] = values[0]
			}
			continue
		}
		if _, taken := out[key]; !taken {
			out[key] = values[0]
		}
	}
	return out
}

func apiString(src map[string]any, key string) (string, bool) {
	v, ok := src[key]
	if !ok || v == nil {
		return "", false
	}
	switch t := v.(type) {
	case string:
		return t, true
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(t), true
	}
	return "", false
}

// apiInt mirrors the ActiveRecord integer cast: numbers truncate, numeric
// strings parse, anything else reads as 0 but stays present.
func apiInt(src map[string]any, key string) (int64, bool) {
	v, ok := src[key]
	if !ok || v == nil {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return int64(t), true
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(t), 10, 64); err == nil {
			return n, true
		}
		return 0, true
	case bool:
		if t {
			return 1, true
		}
		return 0, true
	}
	return 0, true
}

// apiBool mirrors the ActiveModel boolean cast.
func apiBool(src map[string]any, key string) (bool, bool) {
	v, ok := src[key]
	if !ok || v == nil {
		return false, false
	}
	switch t := v.(type) {
	case bool:
		return t, true
	case float64:
		return t != 0, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "", "0", "f", "false", "off":
			return false, true
		}
		return true, true
	}
	return true, true
}

// --- JSON shapes ---

func isoTime(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func apiExpense(e *store.Expense) map[string]any {
	return map[string]any{
		"id": e.ID, "title": e.Title, "amount_cents": e.AmountCents,
		"currency": e.Currency, "spent_on": e.SpentOn, "category": e.Category,
		"notes": e.Notes, "created_at": isoTime(e.CreatedAt), "updated_at": isoTime(e.UpdatedAt),
	}
}

func apiSubscription(su *store.Subscription) map[string]any {
	return map[string]any{
		"id": su.ID, "title": su.Title, "amount_cents": su.AmountCents,
		"currency": su.Currency, "interval": su.Interval,
		"due_day": su.DueDay, "billing_month": su.BillingMonth,
		"active": su.Active, "notes": su.Notes,
		"created_at": isoTime(su.CreatedAt), "updated_at": isoTime(su.UpdatedAt),
	}
}

func apiPaymentDay(d *store.PaymentDay) map[string]any {
	return map[string]any{
		"id": d.ID, "title": d.Title, "due_day": d.DueDay,
		"active": d.Active, "notes": d.Notes,
		"created_at": isoTime(d.CreatedAt), "updated_at": isoTime(d.UpdatedAt),
	}
}

// --- expenses ---

// monthRange mirrors month_range_param: YYYY-MM with to_i tolerance,
// defaulting to the current month.
func monthRange(param string) (start, end string, ok bool) {
	if strings.TrimSpace(param) == "" {
		param = time.Now().Format("2006-01")
	}
	parts := strings.SplitN(param, "-", 2)
	year := tolerantInt(parts[0])
	month := 0
	if len(parts) == 2 {
		month = tolerantInt(parts[1])
	}
	if year < 1 || year > 9999 || month < 1 || month > 12 {
		return "", "", false
	}
	first := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	last := time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	return first.Format("2006-01-02"), last.Format("2006-01-02"), true
}

// tolerantInt mirrors String#to_i: leading digits win, junk reads as 0.
func tolerantInt(s string) int {
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg = true
		s = s[1:]
	} else if strings.HasPrefix(s, "+") {
		s = s[1:]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	if neg {
		n = -n
	}
	return n
}

func (s *Server) handleAPIExpensesIndex(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	start, end, ok := monthRange(r.URL.Query().Get("month"))
	if !ok {
		writeAPIErrors(w, http.StatusUnprocessableEntity,
			[]string{i18n.T(LocaleOf(r), "api.invalid_month")})
		return
	}
	list, err := s.Store.ListExpensesMonth(user.ID, start, end, r.URL.Query().Get("category"))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(list))
	for _, e := range list {
		out = append(out, apiExpense(e))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"expenses": out})
}

func (s *Server) handleAPIExpensesShow(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	e, err := s.Store.FindExpense(user.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"expense": apiExpense(e)})
}

func expensePatchFromAPI(src map[string]any) store.ExpensePatch {
	var p store.ExpensePatch
	if v, ok := apiString(src, "title"); ok {
		p.Title = &v
	}
	if v, ok := apiString(src, "amount"); ok {
		p.Amount = &v
	}
	if v, ok := apiInt(src, "amount_cents"); ok {
		p.AmountCents = &v
	}
	if v, ok := apiString(src, "currency"); ok {
		p.Currency = &v
	}
	if v, ok := apiString(src, "spent_on"); ok {
		p.SpentOn = &v
	}
	if v, ok := apiString(src, "category"); ok {
		p.Category = &v
	}
	if v, ok := apiString(src, "notes"); ok {
		p.Notes = &v
	}
	return p
}

func (s *Server) handleAPIExpensesCreate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	e, fails, err := s.Store.CreateExpense(user.ID, expensePatchFromAPI(apiParams(r, "expense")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "expense", fails)
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"expense": apiExpense(e)})
}

func (s *Server) handleAPIExpensesUpdate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindExpense(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	e, fails, err := s.Store.UpdateExpense(user.ID, id, expensePatchFromAPI(apiParams(r, "expense")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "expense", fails)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"expense": apiExpense(e)})
}

func (s *Server) handleAPIExpensesDestroy(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindExpense(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeleteExpense(user.ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}

// --- subscriptions ---

func subscriptionPatchFromAPI(src map[string]any) store.SubscriptionPatch {
	var p store.SubscriptionPatch
	if v, ok := apiString(src, "title"); ok {
		p.Title = &v
	}
	if v, ok := apiString(src, "amount"); ok {
		p.Amount = &v
	}
	if v, ok := apiInt(src, "amount_cents"); ok {
		p.AmountCents = &v
	}
	if v, ok := apiString(src, "currency"); ok {
		p.Currency = &v
	}
	if v, ok := apiString(src, "interval"); ok {
		p.Interval = &v
	}
	if v, ok := apiString(src, "due_day"); ok {
		p.DueDay = &v
	}
	if v, ok := apiString(src, "billing_month"); ok {
		p.BillingMonth = &v
	}
	if v, ok := apiBool(src, "active"); ok {
		p.Active = &v
	}
	if v, ok := apiString(src, "notes"); ok {
		p.Notes = &v
	}
	return p
}

func (s *Server) handleAPISubscriptionsIndex(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	list, err := s.Store.ListSubscriptions(user.ID, r.URL.Query().Get("active") == "true")
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(list))
	for _, su := range list {
		out = append(out, apiSubscription(su))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"subscriptions": out})
}

func (s *Server) handleAPISubscriptionsShow(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	su, err := s.Store.FindSubscription(user.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"subscription": apiSubscription(su)})
}

func (s *Server) handleAPISubscriptionsCreate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	su, fails, err := s.Store.CreateSubscription(user.ID, subscriptionPatchFromAPI(apiParams(r, "subscription")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "subscription", fails)
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"subscription": apiSubscription(su)})
}

func (s *Server) handleAPISubscriptionsUpdate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindSubscription(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	su, fails, err := s.Store.UpdateSubscription(user.ID, id, subscriptionPatchFromAPI(apiParams(r, "subscription")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "subscription", fails)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"subscription": apiSubscription(su)})
}

func (s *Server) handleAPISubscriptionsDestroy(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindSubscription(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeleteSubscription(user.ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}

// --- payment days ---

func paymentDayPatchFromAPI(src map[string]any) store.PaymentDayPatch {
	var p store.PaymentDayPatch
	if v, ok := apiString(src, "title"); ok {
		p.Title = &v
	}
	if v, ok := apiString(src, "due_day"); ok {
		p.DueDay = &v
	}
	if v, ok := apiBool(src, "active"); ok {
		p.Active = &v
	}
	if v, ok := apiString(src, "notes"); ok {
		p.Notes = &v
	}
	return p
}

func (s *Server) handleAPIPaymentDaysIndex(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	list, err := s.Store.ListPaymentDays(user.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(list))
	for _, d := range list {
		out = append(out, apiPaymentDay(d))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"payment_days": out})
}

func (s *Server) handleAPIPaymentDaysShow(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	d, err := s.Store.FindPaymentDay(user.ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"payment_day": apiPaymentDay(d)})
}

func (s *Server) handleAPIPaymentDaysCreate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	d, fails, err := s.Store.CreatePaymentDay(user.ID, paymentDayPatchFromAPI(apiParams(r, "payment_day")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "payment_day", fails)
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"payment_day": apiPaymentDay(d)})
}

func (s *Server) handleAPIPaymentDaysUpdate(w http.ResponseWriter, r *http.Request) {
	if !apiWriteLimited(s, w, r) {
		return
	}
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindPaymentDay(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	d, fails, err := s.Store.UpdatePaymentDay(user.ID, id, paymentDayPatchFromAPI(apiParams(r, "payment_day")))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(fails) > 0 {
		apiFailures(w, r, "payment_day", fails)
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"payment_day": apiPaymentDay(d)})
}

func (s *Server) handleAPIPaymentDaysDestroy(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindPaymentDay(user.ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeletePaymentDay(user.ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}

// --- month summary ---

type apiLine struct {
	ID           *int64  `json:"id"`
	Kind         string  `json:"kind"`
	Title        string  `json:"title"`
	Notes        string  `json:"notes"`
	AmountCents  int64   `json:"amount_cents"`
	Currency     string  `json:"currency"`
	HomeCents    *int64  `json:"home_cents"`
	Skipped      bool    `json:"skipped"`
	DueDay       *int    `json:"due_day"`
	DueOn        *string `json:"due_on"`
	Overdue      bool    `json:"overdue"`
	DueToday     bool    `json:"due_today"`
	Category     string  `json:"category"`
	SpentOn      *string `json:"spent_on"`
	Interval     *string `json:"interval"`
	BillingMonth *int    `json:"billing_month"`
}

func apiLineFrom(line spend.Line) apiLine {
	out := apiLine{
		ID: line.ID, Kind: line.Kind, Title: line.Title, Notes: line.Notes,
		AmountCents: line.AmountCents, Currency: line.Currency,
		HomeCents: line.HomeCents, Skipped: line.Skipped,
		DueDay: line.DueDay, Overdue: line.Overdue, DueToday: line.DueToday,
		Category: line.Category, Interval: line.Interval, BillingMonth: line.BillingMonth,
	}
	if line.DueOn != nil {
		s := line.DueOn.Format("2006-01-02")
		out.DueOn = &s
	}
	if line.SpentOn != nil {
		s := line.SpentOn.Format("2006-01-02")
		out.SpentOn = &s
	}
	return out
}

type apiMonth struct {
	Year               int       `json:"year"`
	Month              int       `json:"month"`
	HomeCurrency       string    `json:"home_currency"`
	IncomeCents        int64     `json:"income_cents"`
	SubscriptionsCents int64     `json:"subscriptions_cents"`
	ExpensesCents      int64     `json:"expenses_cents"`
	LeftoverCents      int64     `json:"leftover_cents"`
	Missing            []string  `json:"missing_rate_currencies"`
	SalaryMissing      bool      `json:"salary_missing"`
	Subscriptions      []apiLine `json:"subscriptions"`
	Expenses           []apiLine `json:"expenses"`
	PaymentDays        []apiLine `json:"payment_days"`
}

func (s *Server) handleAPIMonthShow(w http.ResponseWriter, r *http.Request) {
	user := apiUserOf(r)
	year, yerr := strconv.Atoi(chi.URLParam(r, "year"))
	month, merr := strconv.Atoi(chi.URLParam(r, "month"))
	if yerr != nil || merr != nil || year < 1 || year > 9999 || month < 1 || month > 12 {
		writeAPIErrors(w, http.StatusUnprocessableEntity,
			[]string{i18n.T(LocaleOf(r), "api.invalid_month")})
		return
	}
	summary := spend.NewSummary(s.Store, user, year, month, time.Now())
	if err := summary.Err(); err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := apiMonth{
		Year: year, Month: month, HomeCurrency: user.HomeCurrency,
		IncomeCents:        summary.IncomeHomeCents(),
		SubscriptionsCents: summary.SubscriptionsHomeCents(),
		ExpensesCents:      summary.ExpensesHomeCents(),
		LeftoverCents:      summary.LeftoverCents(),
		Missing:            summary.MissingRateCurrencies(),
		SalaryMissing:      summary.SalaryMissing(),
		Subscriptions:      make([]apiLine, 0, len(summary.Subscriptions())),
		Expenses:           make([]apiLine, 0, len(summary.Expenses())),
		PaymentDays:        make([]apiLine, 0, len(summary.PaymentDays())),
	}
	if out.Missing == nil {
		out.Missing = []string{}
	}
	for _, line := range summary.Subscriptions() {
		out.Subscriptions = append(out.Subscriptions, apiLineFrom(line))
	}
	for _, line := range summary.Expenses() {
		out.Expenses = append(out.Expenses, apiLineFrom(line))
	}
	for _, line := range summary.PaymentDays() {
		out.PaymentDays = append(out.PaymentDays, apiLineFrom(line))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"month": out})
}
