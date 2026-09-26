package views

import (
	"encoding/json"
	"hash/fnv"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/a-h/templ"
	"github.com/aquasp/kurapeople/internal/i18n"
	"github.com/aquasp/kurapeople/internal/store"
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

// Upcoming is one birthday on the horizon.
type Upcoming struct {
	Person *store.Person
	Date   time.Time
	Days   int
}

// IndexData drives the card grid.
type IndexData struct {
	People        []*store.Person
	Upcoming      []Upcoming
	Relationships []string
	Q             string
	Relationship  string
	AutoLock      bool
	Today         time.Time
}

// DialogData drives the person form dialog (blank for new, filled for
// edit — the form posts back to Action).
type DialogData struct {
	Action    string
	Method    string // post (create) or patch (update)
	Heading   string
	Person    store.Person // zero value for a new card
	Attrs     []*store.Attr
	Rels      []string // existing relationships for the datalist
	DeleteURL string
	DeleteMsg string
}

// ShowData drives the detail page.
type ShowData struct {
	Person   *store.Person
	Attrs    []*store.Attr
	AutoLock bool
	Today    time.Time
}

// PersonURL is the detail path for a card.
func PersonURL(p *store.Person) string {
	return "/people/" + strconv.FormatInt(p.ID, 10)
}

// PersonID renders the id for data attributes and URLs.
func PersonID(p *store.Person) string { return strconv.FormatInt(p.ID, 10) }

// Initials returns up to two uppercase initials (first + last word).
func Initials(name string) string {
	words := strings.Fields(name)
	if len(words) == 0 {
		return "?"
	}
	first, _ := utf8.DecodeRuneInString(words[0])
	last, _ := utf8.DecodeRuneInString(words[len(words)-1])
	out := string(first)
	if len(words) > 1 {
		out += string(last)
	}
	return strings.ToUpper(out)
}

// AvatarHue hashes the name onto the color wheel so every card keeps a
// stable identity tint.
func AvatarHue(name string) int {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(name))))
	return int(h.Sum32() % 360)
}

// BirthdayLabel renders the Localized month/day ("August 11"), or ""
// when unknown.
func BirthdayLabel(p Page, person *store.Person) string {
	if !person.HasBirthday() {
		return ""
	}
	return i18n.MonthDayLabel(p.L, person.BirthMonth, person.BirthDay)
}

// CountdownLabel renders the strip badge ("Today", "Tomorrow",
// "in 12 days").
func CountdownLabel(p Page, days int) string {
	switch days {
	case 0:
		return p.T("app.today")
	case 1:
		return p.T("app.tomorrow")
	default:
		return p.T("app.in_days", "count", strconv.Itoa(days))
	}
}

// TurnsLabel renders the age reached on date ("turns 26"), or "" when
// the birth year is unknown.
func TurnsLabel(p Page, person *store.Person, date time.Time) string {
	if age, ok := person.AgeIn(date.Year()); ok {
		return p.T("app.turns", "age", strconv.Itoa(age))
	}
	return ""
}

// PersonTitle prefixes the name with its emoji when set.
func PersonTitle(person *store.Person) string {
	if person.Emoji == "" {
		return person.Name
	}
	return person.Emoji + " " + person.Name
}

// MonthOption is one month-picker row.
type MonthOption struct {
	Value string
	Label string
}

// MonthOptions returns the 1..12 picker rows with localized labels.
func MonthOptions(p Page) []MonthOption {
	names := i18n.MonthNames(p.L)
	out := make([]MonthOption, 0, 12)
	for i, name := range names {
		out = append(out, MonthOption{Value: strconv.Itoa(i + 1), Label: name})
	}
	return out
}

// Date parts render the optional month/day/year for selects and inputs.
func BirthMonth(person store.Person) string {
	if person.BirthMonth == 0 {
		return ""
	}
	return strconv.Itoa(person.BirthMonth)
}

func BirthDay(person store.Person) string {
	if person.BirthDay == 0 {
		return ""
	}
	return strconv.Itoa(person.BirthDay)
}

func BirthYear(person store.Person) string {
	if person.BirthYear == 0 {
		return ""
	}
	return strconv.Itoa(person.BirthYear)
}

// AvatarStyle pins the card's identity hue as a CSS variable; the
// gradient itself lives in the stylesheet so it stays theme-aware.
func AvatarStyle(name string) string {
	return "--hue: " + strconv.Itoa(AvatarHue(name))
}

// CountdownInfo is the card badge: empty Label means no badge.
type CountdownInfo struct {
	Label string
	Soon  bool // within 30 days: highlighted
	Show  bool
}

// Countdown builds the badge for a card (shown when a birthday is
// known).
func Countdown(p Page, person *store.Person, today time.Time) CountdownInfo {
	days, ok := person.DaysUntilBirthday(today)
	if !ok {
		return CountdownInfo{}
	}
	return CountdownInfo{Label: CountdownLabel(p, days), Soon: days <= 30, Show: true}
}

// ChipURL keeps the search text while switching relationship filter.
func ChipURL(q, rel string) string {
	if rel == "" {
		if q == "" {
			return "/"
		}
		return "/?q=" + url.QueryEscape(q)
	}
	out := "/?relationship=" + url.QueryEscape(rel)
	if q != "" {
		out += "&q=" + url.QueryEscape(q)
	}
	return out
}

// CountdownText renders the badge label, or "" when no birthday is
// known.
func CountdownText(p Page, person *store.Person, today time.Time) string {
	c := Countdown(p, person, today)
	if !c.Show {
		return ""
	}
	return c.Label
}

// CountdownClass renders the badge class ("badge is-soon" within 30
// days), or "" when no birthday is known.
func CountdownClass(p Page, person *store.Person, today time.Time) string {
	c := Countdown(p, person, today)
	if !c.Show {
		return ""
	}
	if c.Soon {
		return "badge is-soon"
	}
	return "badge"
}

// HeroSub joins nickname and relationship for the detail hero.
func HeroSub(person *store.Person) string {
	nick := strings.TrimSpace(person.Nickname)
	rel := strings.TrimSpace(person.Relationship)
	switch {
	case nick != "" && rel != "":
		return nick + " · " + rel
	case nick != "":
		return nick
	default:
		return rel
	}
}

// HeroBirthday renders the detail birthday line ("August 11 ·
// in 12 days · turns 26"), or "" when unknown.
func HeroBirthday(p Page, person *store.Person, today time.Time) string {
	if !person.HasBirthday() {
		return ""
	}
	parts := []string{BirthdayLabel(p, person)}
	if days, ok := person.DaysUntilBirthday(today); ok {
		parts = append(parts, CountdownLabel(p, days))
		if next, ok := person.NextBirthday(today); ok {
			if turns := TurnsLabel(p, person, next); turns != "" {
				parts = append(parts, turns)
			}
		}
	}
	return strings.Join(parts, " · ")
}

// MapsURL links an address to a map search, or "" when blank.
func MapsURL(address string) string {
	if strings.TrimSpace(address) == "" {
		return ""
	}
	return "https://www.google.com/maps/search/?api=1&query=" + url.QueryEscape(address)
}

// CardSub joins the relationship and birthday into the card subtitle.
func CardSub(p Page, person *store.Person) string {
	rel := strings.TrimSpace(person.Relationship)
	bday := BirthdayLabel(p, person)
	switch {
	case rel != "" && bday != "":
		return rel + " · " + bday
	case rel != "":
		return rel
	default:
		return bday
	}
}

// Size is one labeled measurement for the detail page.
type Size struct {
	Label string
	Value string
}

// Sizes returns the set measurements in display order.
func Sizes(p Page, person *store.Person) []Size {
	pairs := []struct {
		key, value string
	}{
		{"app.height", person.Height},
		{"app.ring_size", person.RingSize},
		{"app.shoe_size", person.ShoeSize},
		{"app.shirt_size", person.ShirtSize},
		{"app.pants_size", person.PantsSize},
	}
	var out []Size
	for _, pair := range pairs {
		if strings.TrimSpace(pair.value) == "" {
			continue
		}
		out = append(out, Size{Label: p.T(pair.key), Value: pair.value})
	}
	return out
}

// LockEnabledValue renders the lock controller's enabled value.
func LockEnabledValue(autoLock bool) string {
	if autoLock {
		return "true"
	}
	return "false"
}

// EmojiPresets suggests common emoji in the picker datalist; any emoji
// (or short text) stays valid input.
func EmojiPresets() []string {
	return []string{"🎂", "❤️", "🎁", "⭐", "🎉", "✈️", "🍽️", "📞", "🏠", "⚽", "🎓", "🌟"}
}

// UpcomingBirthdays picks the nearest birthdays within days, soonest
// first (ties by name).
func UpcomingBirthdays(people []*store.Person, today time.Time, days, max int) []Upcoming {
	var out []Upcoming
	for _, person := range people {
		next, ok := person.NextBirthday(today)
		if !ok {
			continue
		}
		n, _ := person.DaysUntilBirthday(today)
		if n > days {
			continue
		}
		out = append(out, Upcoming{Person: person, Date: next, Days: n})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessUpcoming(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) > max {
		out = out[:max]
	}
	return out
}

func lessUpcoming(a, b Upcoming) bool {
	if a.Days != b.Days {
		return a.Days < b.Days
	}
	return strings.ToLower(a.Person.Name) < strings.ToLower(b.Person.Name)
}
