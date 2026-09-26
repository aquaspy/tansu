package views

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/aquasp/kuraspend/internal/fx"
	"github.com/aquasp/kuraspend/internal/i18n"
	"github.com/aquasp/kuraspend/internal/money"
	"github.com/aquasp/kuraspend/internal/spend"
	"github.com/aquasp/kuraspend/internal/store"
)

// Page carries the data every full page needs.
type Page struct {
	L         i18n.Locale
	Title     string
	BodyClass string
	CSRF      string
	Notice    string
	Alert     string
	KuraLogin bool
}

// T translates key with optional %{name}, value pairs.
func (p Page) T(key string, pairs ...string) string { return i18n.T(p.L, key, pairs...) }

func (p Page) Lang() string { return i18n.HTMLLang(p.L) }

// OtherLocale is the language a single click switches to.
func (p Page) OtherLocale() string {
	if p.L == i18n.PT {
		return "en"
	}
	return "pt"
}

// OtherLocaleName is the label of that switch, in the target language.
func (p Page) OtherLocaleName() string {
	if p.L == i18n.PT {
		return "English"
	}
	return "Português"
}

// I18nJSON serializes the js.* table for the #i18n script blob.
func (p Page) I18nJSON() string {
	b, _ := json.Marshal(i18n.JS(p.L))
	return string(b)
}

// Raw renders pre-sanitized HTML (JSON blob).
func Raw(s string) templ.Component { return templ.Raw(s) }

// SpendData drives the month page.
type SpendData struct {
	Summary  *spend.Summary
	User     *store.User
	AutoLock bool
	Year     int
	Month    int
	// FXAuto carries the cached live quotes (fresh or stale) for settings.
	FXAuto map[string]fx.Quote
}

// Money formats cents in the user's home currency, like the money helper.
func Money(p Page, d SpendData, cents int64) string {
	return money.Format(cents, d.User.HomeCurrency, p.L)
}

// MoneyPair shows "home · original", or just the original when it already
// is home, like the money_pair helper.
func MoneyPair(p Page, d SpendData, line spend.Line) string {
	original := money.Format(line.AmountCents, line.Currency, p.L)
	if line.Skipped || line.HomeCents == nil || line.Currency == d.User.HomeCurrency {
		return original
	}
	return money.Format(*line.HomeCents, d.User.HomeCurrency, p.L) + " · " + original
}

// AmountInput formats cents for form inputs, like the amount_input helper.
func AmountInput(p Page, cents int64) string {
	return money.InputAmount(cents, p.L)
}

// CategoryLabel translates a category key, "" when blank.
func CategoryLabel(p Page, category string) string {
	if category == "" {
		return ""
	}
	return p.T("categories." + category)
}

// MonthPath builds /year/month links.
func MonthPath(year, month int) string {
	return fmt.Sprintf("/%d/%d", year, month)
}

// LineID dereferences a line id (always set outside the income line).
func LineID(line spend.Line) int64 {
	if line.ID == nil {
		return 0
	}
	return *line.ID
}

// HomeCents dereferences home cents (0 when skipped).
func HomeCents(line spend.Line) int64 {
	if line.HomeCents == nil {
		return 0
	}
	return *line.HomeCents
}

func intPtrString(v *int) string {
	if v == nil {
		return ""
	}
	return strconv.Itoa(*v)
}

// DueDayString renders an optional due day for data attributes and inputs.
func DueDayString(v *int) string { return intPtrString(v) }

// BillingMonthString renders an optional billing month.
func BillingMonthString(v *int) string { return intPtrString(v) }

// IntervalString renders an optional interval.
func IntervalString(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

// DateString renders an optional date as YYYY-MM-DD.
func DateString(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02")
}

// DueOnDay renders the day-of-month of a due date.
func DueOnDay(t *time.Time) string {
	if t == nil {
		return ""
	}
	return strconv.Itoa(t.Day())
}

// IsToday reports whether date is the summary's today.
func IsToday(d SpendData, t time.Time) bool {
	return t.Format("2006-01-02") == d.Summary.Today.Format("2006-01-02")
}

// MonthTitle renders the month heading in locale format.
func MonthTitle(p Page, d SpendData) string {
	return i18n.MonthTitle(p.L, d.Summary.Start)
}

// DayLabel renders an expenses day heading in locale format.
func DayLabel(p Page, t time.Time) string {
	return i18n.DayLabel(p.L, t)
}

// Itoa renders an int for attributes and inputs.
func Itoa(n int) string { return strconv.Itoa(n) }

// Itoa64 renders an int64 for attributes.
func Itoa64(n int64) string { return strconv.FormatInt(n, 10) }

// BoolString renders a stimulus-style boolean value.
func BoolString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// FXAutoStatus renders the live-quote line for the settings dialog. Both
// quotes must be fresh; anything else reads as unavailable and the manual
// values stand in. The age shown is the oldest of the two fetches.
func FXAutoStatus(p Page, d SpendData) string {
	usd, okU := d.FXAuto["USD"]
	eur, okE := d.FXAuto["EUR"]
	if !okU || !okE || !usd.Fresh || !eur.Fresh {
		return p.T("app.fx_auto_unavailable")
	}
	ago := usd.FetchedAt
	if eur.FetchedAt.Before(ago) {
		ago = eur.FetchedAt
	}
	return p.T("app.fx_auto", "usd", usd.Rate, "eur", eur.Rate, "ago", i18n.Ago(p.L, ago))
}

// FXCodes lists the non-home currencies for the settings form.
func FXCodes(d SpendData) []string {
	var out []string
	for _, code := range money.Currencies {
		if code != d.User.HomeCurrency {
			out = append(out, code)
		}
	}
	return out
}
