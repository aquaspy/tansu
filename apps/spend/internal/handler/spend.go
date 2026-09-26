package handler

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuraspend/internal/fx"
	"github.com/aquasp/kuraspend/internal/i18n"
	"github.com/aquasp/kuraspend/internal/money"
	"github.com/aquasp/kuraspend/internal/spend"
	"github.com/aquasp/kuraspend/internal/store"
	"github.com/aquasp/kuraspend/internal/views"
	"github.com/go-chi/chi/v5"
)

func isHX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func flashNotice(s *Server, r *http.Request, notice string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, notice, "")
	}
}

func flashAlert(s *Server, r *http.Request, alert string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, "", alert)
	}
}

// validationMessages renders store failures through the errors.* table.
func validationMessages(l i18n.Locale, model string, fails []store.FieldError) []string {
	out := make([]string, 0, len(fails))
	for _, f := range fails {
		out = append(out, i18n.T(l, "errors."+model+"."+f.Field+"."+f.Code))
	}
	return out
}

func joinMessages(msgs []string) string { return strings.Join(msgs, " ") }

// monthParams mirrors MonthsController#month_date: URL year/month, falling
// back to the current month when invalid.
func monthParams(r *http.Request) (int, int) {
	y, yerr := strconv.Atoi(chi.URLParam(r, "year"))
	m, merr := strconv.Atoi(chi.URLParam(r, "month"))
	if yerr != nil || merr != nil || y < 1 || y > 9999 || m < 1 || m > 12 {
		now := time.Now()
		return now.Year(), int(now.Month())
	}
	return y, m
}

// monthPathFor mirrors month_url_for: the dialogs' hidden year/month,
// falling back to the current month.
func monthPathFor(r *http.Request) string {
	y, yerr := strconv.Atoi(r.FormValue("year"))
	m, merr := strconv.Atoi(r.FormValue("month"))
	if yerr != nil || merr != nil || y < 1 || m < 1 || m > 12 {
		now := time.Now()
		return views.MonthPath(now.Year(), int(now.Month()))
	}
	return views.MonthPath(y, m)
}

func (s *Server) handleMonthShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	year, month := monthParams(r)
	summary := spend.NewSummary(s.Store, user, year, month, time.Now())
	if err := summary.Err(); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	d := views.SpendData{Summary: summary, User: user,
		AutoLock: AutoLockEnabled(r), Year: year, Month: month,
		FXAuto: fx.Quotes(s.Store)}
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.SpendPage(p, d)))
}

// --- web CRUD ---

func formPtr(r *http.Request, key string) *string {
	v := r.FormValue(key)
	return &v
}

func (s *Server) createLimited(w http.ResponseWriter, r *http.Request, scope string) bool {
	user := UserOf(r)
	if !s.Limiter.Allow(scope+":"+itoa64(user.ID), 60, time.Minute) {
		flashAlert(s, r, i18n.T(LocaleOf(r), "auth.too_many"))
		http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
		return false
	}
	return true
}

func (s *Server) handleExpensesCreate(w http.ResponseWriter, r *http.Request) {
	if !s.createLimited(w, r, "expense") {
		return
	}
	user := UserOf(r)
	p := store.ExpensePatch{
		Title:    formPtr(r, "expense[title]"),
		Amount:   formPtr(r, "expense[amount]"),
		Currency: formPtr(r, "expense[currency]"),
		SpentOn:  formPtr(r, "expense[spent_on]"),
		Category: formPtr(r, "expense[category]"),
		Notes:    formPtr(r, "expense[notes]"),
	}
	if _, fails, err := s.Store.CreateExpense(user.ID, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "expense", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handleExpensesUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindExpense(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	p := store.ExpensePatch{
		Title:    formPtr(r, "expense[title]"),
		Amount:   formPtr(r, "expense[amount]"),
		Currency: formPtr(r, "expense[currency]"),
		SpentOn:  formPtr(r, "expense[spent_on]"),
		Category: formPtr(r, "expense[category]"),
		Notes:    formPtr(r, "expense[notes]"),
	}
	if _, fails, err := s.Store.UpdateExpense(user.ID, id, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "expense", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handleExpensesDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindExpense(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteExpense(user.ID, id)
	s.Store.ReclaimSpace()
	if isHX(r) || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handleSubscriptionsCreate(w http.ResponseWriter, r *http.Request) {
	if !s.createLimited(w, r, "subscription") {
		return
	}
	user := UserOf(r)
	p := store.SubscriptionPatch{
		Title:        formPtr(r, "subscription[title]"),
		Amount:       formPtr(r, "subscription[amount]"),
		Currency:     formPtr(r, "subscription[currency]"),
		Interval:     formPtr(r, "subscription[interval]"),
		DueDay:       formPtr(r, "subscription[due_day]"),
		BillingMonth: formPtr(r, "subscription[billing_month]"),
		Notes:        formPtr(r, "subscription[notes]"),
	}
	if _, fails, err := s.Store.CreateSubscription(user.ID, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "subscription", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handleSubscriptionsUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindSubscription(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	p := store.SubscriptionPatch{
		Title:        formPtr(r, "subscription[title]"),
		Amount:       formPtr(r, "subscription[amount]"),
		Currency:     formPtr(r, "subscription[currency]"),
		Interval:     formPtr(r, "subscription[interval]"),
		DueDay:       formPtr(r, "subscription[due_day]"),
		BillingMonth: formPtr(r, "subscription[billing_month]"),
		Notes:        formPtr(r, "subscription[notes]"),
	}
	if _, fails, err := s.Store.UpdateSubscription(user.ID, id, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "subscription", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handleSubscriptionsDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindSubscription(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteSubscription(user.ID, id)
	s.Store.ReclaimSpace()
	if isHX(r) || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handlePaymentDaysCreate(w http.ResponseWriter, r *http.Request) {
	if !s.createLimited(w, r, "payment_day") {
		return
	}
	user := UserOf(r)
	p := store.PaymentDayPatch{
		Title:  formPtr(r, "payment_day[title]"),
		DueDay: formPtr(r, "payment_day[due_day]"),
		Notes:  formPtr(r, "payment_day[notes]"),
	}
	if _, fails, err := s.Store.CreatePaymentDay(user.ID, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "payment_day", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handlePaymentDaysUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindPaymentDay(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	p := store.PaymentDayPatch{
		Title:  formPtr(r, "payment_day[title]"),
		DueDay: formPtr(r, "payment_day[due_day]"),
		Notes:  formPtr(r, "payment_day[notes]"),
	}
	if _, fails, err := s.Store.UpdatePaymentDay(user.ID, id, p); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(LocaleOf(r), "payment_day", fails)))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

func (s *Server) handlePaymentDaysDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindPaymentDay(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeletePaymentDay(user.ID, id)
	s.Store.ReclaimSpace()
	if isHX(r) || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

// --- settings ---

func (s *Server) handleSettingsUpdate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	_ = r.ParseForm()
	fx := map[string]string{}
	for _, code := range money.Currencies {
		if r.Form.Has("fx[" + code + "]") {
			fx[code] = r.FormValue("fx[" + code + "]")
		}
	}
	if _, fails, err := s.Store.UpdateSettings(user.ID,
		r.FormValue("user[home_currency]"),
		r.FormValue("user[monthly_income]"),
		r.FormValue("user[income_currency]"), fx); err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	} else if len(fails) > 0 {
		flashAlert(s, r, joinMessages(validationMessages(l, "user", fails)))
	} else {
		flashNotice(s, r, i18n.T(l, "app.settings_saved"))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

// handleFXRefresh refetches the live USD/EUR quotes on demand. The ticker
// in serve.go does this periodically; the button covers "right now".
func (s *Server) handleFXRefresh(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("fx:"+itoa64(user.ID), 12, time.Hour) {
		flashAlert(s, r, i18n.T(l, "auth.too_many"))
		http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
		return
	}
	switch n, err := fx.Refresh(r.Context(), s.Store, s.FXClient); {
	case err != nil:
		flashAlert(s, r, i18n.T(l, "app.fx_refresh_failed"))
	case n < 2:
		flashNotice(s, r, i18n.T(l, "app.fx_refreshed_partial"))
	default:
		flashNotice(s, r, i18n.T(l, "app.fx_refreshed"))
	}
	http.Redirect(w, r, monthPathFor(r), http.StatusSeeOther)
}

// --- export / import ---

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	raw, err := spend.Export(s.Store, user)
	if err != nil {
		http.Error(w, "spend unavailable", http.StatusInternalServerError)
		return
	}
	filename := fmt.Sprintf("kuraspend-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(raw)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	fail := func() {
		flashAlert(s, r, i18n.T(l, "app.import_invalid"))
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail()
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		fail()
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail()
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 64<<20))
	f.Close()
	if err != nil {
		fail()
		return
	}
	count, err := spend.Import(s.Store, user.ID, data)
	if err != nil {
		fail()
		return
	}
	flashNotice(s, r, i18n.T(l, "app.import_done", "count", strconv.Itoa(count)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
