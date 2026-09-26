package views

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/aquasp/kuracalendar/internal/calendar"
	"github.com/aquasp/kuracalendar/internal/holidays"
	"github.com/aquasp/kuracalendar/internal/i18n"
	"github.com/aquasp/kuracalendar/internal/store"
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

// Raw renders pre-sanitized HTML.
func Raw(s string) templ.Component { return templ.Raw(s) }

// I18nScript renders the js.* string table for scripts. It is built in
// Go because templ treats <script> bodies as opaque text: an @-component
// inside one would render literally. encoding/json escapes <, >, and &,
// so the blob is safe to embed raw.
func (p Page) I18nScript() templ.Component {
	return templ.Raw(`<script type="application/json" id="i18n">` + p.I18nJSON() + `</script>`)
}

// CalData drives the calendar page.
type CalData struct {
	Grid     *calendar.Grid
	Cell     *calendar.Cell // selected day (never nil)
	AutoLock bool
	Codes    []string // the user's selected holiday packs
}

// CalPath mirrors the cal_path helper: /2026/8/25.
func CalPath(t time.Time) string {
	return "/" + strconv.Itoa(t.Year()) + "/" + strconv.Itoa(int(t.Month())) + "/" + strconv.Itoa(t.Day())
}

// MonthNavPath mirrors month_nav_path: keep the day when it exists in the
// target month, else clamp to the last day.
func MonthNavPath(month time.Time, keepDay int) string {
	day := keepDay
	if max := store.DaysInMonth(month.Year(), int(month.Month())); day > max {
		day = max
	}
	return "/" + strconv.Itoa(month.Year()) + "/" + strconv.Itoa(int(month.Month())) + "/" + strconv.Itoa(day)
}

// HolidayName mirrors ApplicationHelper#holiday_name.
func HolidayName(p Page, h holidays.Holiday, tagCountries bool) string {
	name := p.T("holidays." + lowerCountry(h.Country) + "." + h.Key)
	if tagCountries {
		return name + " · " + h.Country
	}
	return name
}

func lowerCountry(code string) string {
	switch code {
	case "BR":
		return "br"
	case "US":
		return "us"
	case "SI":
		return "si"
	case "CZ":
		return "cz"
	default:
		return "br"
	}
}

// MonthLabel formats the month heading ("September 2026").
func MonthLabel(p Page, t time.Time) string { return i18n.MonthLabel(p.L, t) }

// DayLabel formats the day heading ("Monday, September 7").
func DayLabel(p Page, t time.Time) string { return i18n.DayLabel(p.L, t) }

// WeekdayLabels returns the Monday-first weekday header labels.
func WeekdayLabels(p Page) [7]string {
	names := i18n.AbbrDayNames(p.L)
	var out [7]string
	for i, idx := range calendar.WeekdayIndexes() {
		out[i] = names[idx]
	}
	return out
}

// MonthNames returns the January..December picker labels.
func MonthNames(p Page) [12]string { return i18n.MonthNames(p.L) }

// CellClass mirrors the _month.html.erb cell classes.
func CellClass(c *calendar.Cell) string {
	class := "cal-cell"
	if !c.InMonth {
		class += " is-other"
	}
	if c.Today {
		class += " is-today"
	}
	if c.Selected {
		class += " is-selected"
	}
	if len(c.Events) > 0 || len(c.Birthdays) > 0 || len(c.Holidays) > 0 {
		class += " has-items"
	}
	return class
}

// Marks resolves the cell pills with localized holiday names.
func Marks(p Page, c *calendar.Cell) ([]calendar.Mark, int) {
	return c.Marks(func(h holidays.Holiday) string {
		return HolidayName(p, h, c.TagCountries)
	})
}

// OverflowLabel renders app.overflow ("+2").
func OverflowLabel(p Page, extra int) string {
	return p.T("app.overflow", "count", strconv.Itoa(extra))
}

// BirthdayYear renders the optional year for data attributes.
func BirthdayYear(b *store.Birthday) string {
	if b.Year == 0 {
		return ""
	}
	return strconv.Itoa(b.Year)
}

// BirthdayMonth / BirthdayDay render the birthday's own month/day for the
// edit dialog.
func BirthdayMonth(b *store.Birthday) string { return strconv.Itoa(b.Month) }
func BirthdayDay(b *store.Birthday) string   { return strconv.Itoa(b.Day) }

// EventID / BirthdayID render ids for data attributes and URLs.
func EventID(e *store.Event) string       { return strconv.FormatInt(e.ID, 10) }
func BirthdayID(b *store.Birthday) string { return strconv.FormatInt(b.ID, 10) }

func EventURL(e *store.Event) string {
	return "/events/" + strconv.FormatInt(e.ID, 10)
}

func BirthdayURL(b *store.Birthday) string {
	return "/birthdays/" + strconv.FormatInt(b.ID, 10)
}

// AllDayValue renders the all_day flag for data attributes.
func AllDayValue(e *store.Event) string {
	if e.AllDay {
		return "true"
	}
	return "false"
}

// LockEnabledValue renders the lock controller's enabled value.
func LockEnabledValue(d CalData) string {
	if d.AutoLock {
		return "true"
	}
	return "false"
}

// PackChecked reports whether a holiday pack is selected.
func PackChecked(d CalData, code string) bool {
	for _, c := range d.Codes {
		if c == code {
			return true
		}
	}
	return false
}

// PackCodes returns the holiday pack codes in display order.
func PackCodes() []string { return store.HolidayPacks }

// MonthOption is one birthday month-picker row.
type MonthOption struct {
	Value string
	Label string
}

// MonthOptions returns the 1..12 picker rows with localized labels.
func MonthOptions(p Page) []MonthOption {
	names := MonthNames(p)
	out := make([]MonthOption, 0, 12)
	for i, name := range names {
		out = append(out, MonthOption{Value: strconv.Itoa(i + 1), Label: name})
	}
	return out
}

// RepeatOptions returns the repeat-preset picker rows with localized labels.
func RepeatOptions(p Page) []MonthOption {
	out := make([]MonthOption, 0, len(store.Repeats))
	for _, code := range store.Repeats {
		out = append(out, MonthOption{Value: code, Label: p.T("app.repeat_" + code)})
	}
	return out
}

// EmojiPresets suggests common emoji in the picker datalist; any emoji (or
// short text) stays valid input.
func EmojiPresets() []string {
	return []string{"🎂", "✈️", "🏖️", "💼", "🎉", "⚽", "🍽️", "📞", "🎓", "🏥", "🎁", "❤️"}
}

// DeleteEventMessage picks the delete confirmation: series warn that every
// occurrence goes away, since there are no per-occurrence edits.
func DeleteEventMessage(p Page, e *store.Event) string {
	if e.Repeating() {
		return p.T("js.delete_event_series_confirm")
	}
	return p.T("js.delete_event_confirm")
}

// EventTitle prefixes the title with its emoji when set.
func EventTitle(e *store.Event) string {
	if e.Emoji == "" {
		return e.Title
	}
	return e.Emoji + " " + e.Title
}

// BirthdayLabel prefixes the age label with its emoji when set.
func BirthdayLabel(b *store.Birthday, date time.Time) string {
	if b.Emoji == "" {
		return b.LabelOn(date)
	}
	return b.Emoji + " " + b.LabelOn(date)
}

// CellMarks bundles one cell's pills + overflow for templates.
type CellMarks struct {
	Pills []calendar.Mark
	Extra int
}

func MarksOf(p Page, c *calendar.Cell) CellMarks {
	pills, extra := Marks(p, c)
	return CellMarks{Pills: pills, Extra: extra}
}

// Cell date parts for data attributes and hidden fields.
func CellISO(c *calendar.Cell) string   { return c.Date.Format("2006-01-02") }
func CellYear(c *calendar.Cell) string  { return strconv.Itoa(c.Date.Year()) }
func CellMonth(c *calendar.Cell) string { return strconv.Itoa(int(c.Date.Month())) }
func CellDay(c *calendar.Cell) string   { return strconv.Itoa(c.Date.Day()) }

func GridYear(d CalData) string  { return strconv.Itoa(d.Grid.Month.Year()) }
func GridMonth(d CalData) string { return strconv.Itoa(int(d.Grid.Month.Month())) }

// SortedDayEvents mirrors the _day sort: all-day first, then start
// time, then id.
func SortedDayEvents(c *calendar.Cell) []*store.Event {
	out := append([]*store.Event(nil), c.Events...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessEvent(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func lessEvent(a, b *store.Event) bool {
	aa, bb := 1, 1
	if a.AllDay {
		aa = 0
	}
	if b.AllDay {
		bb = 0
	}
	if aa != bb {
		return aa < bb
	}
	if a.StartsAt != b.StartsAt {
		return a.StartsAt < b.StartsAt
	}
	return a.ID < b.ID
}
