// Package spend holds the spend-domain logic shared by the web and API
// handlers: the month summary, the JSON exporter, and the importer.
package spend

import (
	"time"

	"github.com/aquasp/kuraspend/internal/fx"
	"github.com/aquasp/kuraspend/internal/money"
	"github.com/aquasp/kuraspend/internal/store"
)

// Line kinds mirror MonthSummary::Line kinds.
const (
	KindIncome       = "income"
	KindSubscription = "subscription"
	KindExpense      = "expense"
	KindPaymentDay   = "payment_day"
)

// Line is one summary row. HomeCents is nil when Skipped (missing FX rate),
// like the Rails Line with a nil home_cents.
type Line struct {
	ID           *int64
	Kind         string
	Title        string
	Notes        string
	AmountCents  int64
	Currency     string
	HomeCents    *int64
	Skipped      bool
	DueDay       *int
	DueOn        *time.Time
	Overdue      bool
	DueToday     bool
	Category     string
	SpentOn      *time.Time
	Interval     *string
	Counts       bool
	BillingMonth *int
}

// DayGroup is one day's expenses, days in spent_on desc order.
type DayGroup struct {
	Date  time.Time
	Lines []Line
}

// Summary mirrors MonthSummary.
type Summary struct {
	User   *store.User
	Year   int
	Month  int
	Start  time.Time
	End    time.Time
	Today  time.Time
	home   string
	rates  map[string]string
	income Line

	subRows []Line
	subs    []Line
	days    []Line
	exp     []Line
	byDay   []DayGroup

	loaded bool
	err    error
}

// NewSummary builds the summary for year/month. today injects the clock
// (Date.current in Rails); pass time.Now() in production.
func NewSummary(st *store.Store, user *store.User, year, month int, today time.Time) *Summary {
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	s := &Summary{
		User:  user,
		Year:  year,
		Month: month,
		Start: time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC),
		Today: today,
		home:  user.HomeCurrency,
		rates: fx.EffectiveRates(st, user),
	}
	s.End = time.Date(year, time.Month(month)+1, 0, 0, 0, 0, 0, time.UTC)
	s.load(st)
	return s
}

func (s *Summary) load(st *store.Store) {
	s.income = s.convert(nil, KindIncome, "salary", "", s.User.MonthlyIncomeCents, s.User.IncomeCurrency)

	subs, err := st.ListSubscriptions(s.User.ID, true)
	if err != nil {
		s.err = err
		return
	}
	for _, su := range subs {
		counts := su.AppliesIn(s.Year, s.Month)
		line := s.convert(&su.ID, KindSubscription, su.Title, su.Notes,
			su.AmountCents, su.Currency)
		line.DueDay = su.DueDay
		line.Interval = &su.Interval
		line.BillingMonth = su.BillingMonth
		line.Counts = counts
		s.subRows = append(s.subRows, line)
		if counts {
			s.subs = append(s.subs, line)
		}
	}

	days, err := st.ListActivePaymentDays(s.User.ID)
	if err != nil {
		s.err = err
		return
	}
	current := s.CurrentMonth()
	for _, d := range days {
		due := d.DueOn(s.Year, s.Month)
		line := s.convert(&d.ID, KindPaymentDay, d.Title, d.Notes, 0, s.home)
		dd := d.DueDay
		line.DueDay = &dd
		line.DueOn = &due
		line.Overdue = current && due.Before(s.Today)
		line.DueToday = current && due.Equal(s.Today)
		s.days = append(s.days, line)
	}

	expenses, err := st.ListExpensesMonth(s.User.ID,
		s.Start.Format("2006-01-02"), s.End.Format("2006-01-02"), "")
	if err != nil {
		s.err = err
		return
	}
	for _, e := range expenses {
		spent, _ := time.Parse("2006-01-02", e.SpentOn)
		line := s.convert(&e.ID, KindExpense, e.Title, e.Notes, e.AmountCents, e.Currency)
		line.Category = e.Category
		line.SpentOn = &spent
		s.exp = append(s.exp, line)
	}
	s.byDay = groupByDay(s.exp)
	s.loaded = true
}

// Err reports a load failure (database unavailable).
func (s *Summary) Err() error { return s.err }

// PrevMonth mirrors start_on << 1.
func (s *Summary) PrevMonth() time.Time { return s.Start.AddDate(0, -1, 0) }

// NextMonth mirrors start_on >> 1.
func (s *Summary) NextMonth() time.Time { return s.Start.AddDate(0, 1, 0) }

// CurrentMonth mirrors current_month?.
func (s *Summary) CurrentMonth() bool {
	return s.Today.Year() == s.Year && int(s.Today.Month()) == s.Month
}

// Income is the salary line.
func (s *Summary) Income() Line { return s.income }

// SubscriptionRows lists every active subscription, counting or not.
func (s *Summary) SubscriptionRows() []Line { return s.subRows }

// Subscriptions lists the rows counting this month.
func (s *Summary) Subscriptions() []Line { return s.subs }

// PaymentDays lists the active payment days.
func (s *Summary) PaymentDays() []Line { return s.days }

// Expenses lists this month's expenses.
func (s *Summary) Expenses() []Line { return s.exp }

// ExpensesByDay groups expenses by spent_on, days descending.
func (s *Summary) ExpensesByDay() []DayGroup { return s.byDay }

// IncomeHomeCents mirrors totaled(income).
func (s *Summary) IncomeHomeCents() int64 { return totaled(s.income) }

// SubscriptionsHomeCents mirrors sum(subscriptions).
func (s *Summary) SubscriptionsHomeCents() int64 { return sumLines(s.subs) }

// ExpensesHomeCents mirrors sum(expenses).
func (s *Summary) ExpensesHomeCents() int64 { return sumLines(s.exp) }

// LeftoverCents mirrors income - subscriptions - expenses.
func (s *Summary) LeftoverCents() int64 {
	return s.IncomeHomeCents() - s.SubscriptionsHomeCents() - s.ExpensesHomeCents()
}

// MissingRateCurrencies mirrors the Rails method: income first, then
// subscriptions, then expenses, deduplicated.
func (s *Summary) MissingRateCurrencies() []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range append(append([]Line{s.income}, s.subs...), s.exp...) {
		if line.Skipped && !seen[line.Currency] {
			seen[line.Currency] = true
			out = append(out, line.Currency)
		}
	}
	return out
}

// SalaryMissing mirrors salary_missing?.
func (s *Summary) SalaryMissing() bool { return s.User.MonthlyIncomeCents == 0 }

// convert mirrors MonthSummary#convert: a MissingRate marks the line skipped,
// leaving it out of the totals (never assumed 1:1).
func (s *Summary) convert(id *int64, kind, title, notes string, amountCents int64, currency string) Line {
	line := Line{ID: id, Kind: kind, Title: title, Notes: notes,
		AmountCents: amountCents, Currency: currency, Counts: true}
	home, err := money.ToHomeCents(amountCents, currency, s.home, s.rates)
	if err != nil {
		line.Skipped = true
		return line
	}
	line.HomeCents = &home
	return line
}

func totaled(line Line) int64 {
	if line.Skipped || line.HomeCents == nil {
		return 0
	}
	return *line.HomeCents
}

func sumLines(lines []Line) int64 {
	var total int64
	for _, line := range lines {
		total += totaled(line)
	}
	return total
}

func groupByDay(lines []Line) []DayGroup {
	var out []DayGroup
	index := map[string]int{}
	for _, line := range lines {
		if line.SpentOn == nil {
			continue
		}
		key := line.SpentOn.Format("2006-01-02")
		if i, ok := index[key]; ok {
			out[i].Lines = append(out[i].Lines, line)
			continue
		}
		index[key] = len(out)
		out = append(out, DayGroup{Date: *line.SpentOn, Lines: []Line{line}})
	}
	return out
}
