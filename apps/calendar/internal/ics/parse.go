// Package ics parses a public iCalendar feed into concrete events and
// fetches it without following a link into a private network.
//
// The parser is an MVP: VEVENT with SUMMARY, DESCRIPTION, UID, LOCATION,
// DTSTART/DTEND (UTC, floating, or TZID) and VALUE=DATE. Simple RRULE
// values (daily/weekly/monthly/yearly, INTERVAL, COUNT, UNTIL, EXDATE)
// expand inside a window. BYDAY and the other BY* parts are not expanded;
// the first date is kept. Anything else is skipped rather than guessed.
package ics

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
	"unicode/utf8"

	_ "time/tzdata"
)

const (
	// PastDays is how far behind today a subscribed event is kept.
	PastDays = 31
	// FutureMonths is how far ahead a subscribed event is kept.
	FutureMonths = 14
	// maxOccurrences caps one RRULE so a daily rule cannot fill the feed alone.
	maxOccurrences = 500
)

// Event is one concrete instance ready to store. UID is stable across syncs.
type Event struct {
	UID      string
	Title    string
	Body     string
	AllDay   bool
	StartsOn string // YYYY-MM-DD
	EndsOn   string
	StartsAt string // HH:MM or empty
	EndsAt   string
}

// Window is the inclusive date span a sync keeps: about a month back
// through FutureMonths ahead, using now's calendar date.
func Window(now time.Time) (from, to time.Time) {
	y, m, d := now.Date()
	day := time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
	return day.AddDate(0, 0, -PastDays), day.AddDate(0, FutureMonths, 0)
}

// Parse reads one calendar. loc is the zone used for UTC (Z) times; nil
// means UTC. from/to bound expanded repeats (inclusive dates). A body
// without VCALENDAR is ErrParse. A valid calendar with no events returns
// an empty slice.
func Parse(data []byte, loc *time.Location, from, to time.Time) ([]Event, error) {
	if loc == nil {
		loc = time.UTC
	}
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	text := string(data)
	if !utf8.ValidString(text) {
		text = strings.ToValidUTF8(text, "")
	}
	lines := unfold(text)
	if !hasCalendar(lines) {
		return nil, ErrParse
	}
	fromS := from.UTC().Format("2006-01-02")
	toS := to.UTC().Format("2006-01-02")
	var out []Event
	for _, raw := range vevents(lines) {
		ev, ok := buildEvent(raw, loc, fromS, toS)
		if !ok {
			continue
		}
		out = append(out, ev...)
	}
	return out, nil
}

func unfold(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\n ", "")
	text = strings.ReplaceAll(text, "\n\t", "")
	return strings.Split(text, "\n")
}

func hasCalendar(lines []string) bool {
	for _, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), "BEGIN:VCALENDAR") {
			return true
		}
	}
	return false
}

type prop struct {
	name   string
	params map[string]string
	value  string
}

func vevents(lines []string) [][]prop {
	var blocks [][]prop
	in := false
	nested := 0
	var cur []prop
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			continue
		}
		upper := strings.ToUpper(line)
		if !in {
			if upper == "BEGIN:VEVENT" {
				in = true
				nested = 0
				cur = nil
			}
			continue
		}
		if strings.HasPrefix(upper, "BEGIN:") {
			nested++
			continue
		}
		if strings.HasPrefix(upper, "END:") {
			if nested > 0 {
				nested--
				continue
			}
			if upper == "END:VEVENT" {
				blocks = append(blocks, cur)
				in = false
			}
			continue
		}
		if nested > 0 {
			continue
		}
		if p, ok := parseProp(line); ok {
			cur = append(cur, p)
		}
	}
	return blocks
}

func parseProp(line string) (prop, bool) {
	colon := -1
	inQ := false
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '"':
			inQ = !inQ
		case ':':
			if !inQ {
				colon = i
			}
		}
		if colon >= 0 {
			break
		}
	}
	if colon <= 0 {
		return prop{}, false
	}
	left := splitSemi(line[:colon])
	if len(left) == 0 || left[0] == "" {
		return prop{}, false
	}
	params := map[string]string{}
	for _, part := range left[1:] {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		params[strings.ToUpper(strings.TrimSpace(k))] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return prop{name: strings.ToUpper(left[0]), params: params, value: line[colon+1:]}, true
}

func splitSemi(s string) []string {
	var out []string
	start := 0
	inQ := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '"':
			inQ = !inQ
		case ';':
			if !inQ {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func buildEvent(props []prop, loc *time.Location, from, to string) ([]Event, bool) {
	var (
		summary, desc, location, uid, status, recur, duration string
		start                                                 time.Time
		end                                                   time.Time
		hasStart, hasEnd, allDay                              bool
		rule                                                  *rrule
		exdates                                               = map[string]struct{}{}
	)
	for _, p := range props {
		switch p.name {
		case "SUMMARY":
			if summary == "" {
				summary = unescape(p.value)
			}
		case "DESCRIPTION":
			if desc == "" {
				desc = unescape(p.value)
			}
		case "LOCATION":
			if location == "" {
				location = unescape(p.value)
			}
		case "UID":
			if uid == "" {
				uid = strings.TrimSpace(p.value)
			}
		case "STATUS":
			status = strings.ToUpper(strings.TrimSpace(p.value))
		case "RECURRENCE-ID":
			recur = strings.TrimSpace(p.value)
		case "DTSTART":
			if t, day, ok := parseWhen(p.value, p.params, loc); ok {
				start, allDay, hasStart = t, day, true
			}
		case "DTEND":
			if t, _, ok := parseWhen(p.value, p.params, loc); ok {
				end, hasEnd = t, true
			}
		case "DURATION":
			if duration == "" {
				duration = p.value
			}
		case "RRULE":
			if rule == nil {
				rule = parseRRule(p.value)
			}
		case "EXDATE":
			for _, part := range strings.Split(p.value, ",") {
				if t, _, ok := parseWhen(strings.TrimSpace(part), p.params, loc); ok {
					exdates[t.Format("2006-01-02")] = struct{}{}
				}
			}
		}
	}
	if !hasStart || status == "CANCELLED" {
		return nil, false
	}
	if !hasEnd && duration != "" {
		if t, ok := applyDuration(start, allDay, duration); ok {
			end, hasEnd = t, true
		}
	}
	if !hasEnd {
		if allDay {
			end = start.AddDate(0, 0, 1)
		} else {
			end = start
		}
	}
	if allDay && !end.After(start) {
		end = start.AddDate(0, 0, 1)
	}
	if uid == "" {
		sum := sha256.Sum256([]byte(summary + "\n" + start.Format(time.RFC3339) + "\n" + end.Format(time.RFC3339)))
		uid = "synth:" + hex.EncodeToString(sum[:16])
	}
	title := strings.Join(strings.Fields(summary), " ")
	if title == "" {
		title = "ICS"
	}
	body := strings.TrimSpace(desc)
	if locLine := strings.TrimSpace(location); locLine != "" {
		if body != "" {
			body += "\n"
		}
		body += locLine
	}

	// An override instance is one row. A master with a simple rule expands.
	expanded := rule != nil && !rule.exotic && recur == ""
	var spans []span
	if expanded {
		spans = expandRule(start, end, allDay, rule, exdates, from, to)
	} else if overlaps(start, end, allDay, from, to) && !excluded(start, exdates) {
		spans = []span{{start: start, end: end}}
	}
	if len(spans) == 0 {
		return nil, false
	}
	out := make([]Event, 0, len(spans))
	for _, sp := range spans {
		key := uid
		if expanded || recur != "" {
			key = uid + "#" + sp.start.Format("20060102")
			if !allDay {
				key += "T" + sp.start.Format("1504")
			}
			if recur != "" {
				key = uid + "#" + recur
			}
		}
		ev := Event{UID: key, Title: title, Body: body, AllDay: allDay}
		ev.StartsOn = sp.start.Format("2006-01-02")
		if allDay {
			incl := sp.end.AddDate(0, 0, -1)
			if incl.Before(sp.start) {
				incl = sp.start
			}
			ev.EndsOn = incl.Format("2006-01-02")
		} else {
			ev.EndsOn = sp.end.Format("2006-01-02")
			ev.StartsAt = sp.start.Format("15:04")
			ev.EndsAt = sp.end.Format("15:04")
		}
		out = append(out, ev)
	}
	return out, true
}

func excluded(start time.Time, ex map[string]struct{}) bool {
	_, ok := ex[start.Format("2006-01-02")]
	return ok
}

type span struct {
	start time.Time
	end   time.Time
}

func overlaps(start, end time.Time, allDay bool, from, to string) bool {
	sDate := start.Format("2006-01-02")
	eDate := inclusiveEnd(start, end, allDay)
	return sDate <= to && eDate >= from
}

func inclusiveEnd(start, end time.Time, allDay bool) string {
	if allDay {
		incl := end.AddDate(0, 0, -1)
		if incl.Before(start) {
			incl = start
		}
		return incl.Format("2006-01-02")
	}
	return end.Format("2006-01-02")
}

type rrule struct {
	freq     string
	interval int
	count    int
	until    time.Time
	exotic   bool
}

func parseRRule(value string) *rrule {
	r := &rrule{interval: 1}
	for _, part := range strings.Split(value, ";") {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.ToUpper(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		switch k {
		case "FREQ":
			switch strings.ToUpper(v) {
			case "DAILY", "WEEKLY", "MONTHLY", "YEARLY":
				r.freq = strings.ToLower(v)
			default:
				r.exotic = true
			}
		case "INTERVAL":
			n := atoi(v)
			if n < 1 || n > 366 {
				r.exotic = true
			} else {
				r.interval = n
			}
		case "COUNT":
			n := atoi(v)
			if n < 1 {
				r.exotic = true
			} else {
				r.count = n
			}
		case "UNTIL":
			if t, _, ok := parseWhen(v, nil, time.UTC); ok {
				r.until = t
			} else {
				r.exotic = true
			}
		case "WKST":
			// Ignored. We do not expand BYDAY, so the week start does not matter.
		default:
			if strings.HasPrefix(k, "BY") {
				r.exotic = true
			}
		}
	}
	if r.freq == "" {
		r.exotic = true
	}
	return r
}

func atoi(s string) int {
	n := 0
	if s == "" {
		return 0
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
		if n > 1000000 {
			return n
		}
	}
	return n
}

func expandRule(start, end time.Time, allDay bool, r *rrule, ex map[string]struct{}, from, to string) []span {
	dur := end.Sub(start)
	if dur < 0 {
		dur = 0
	}
	durDays := 1
	if allDay {
		durDays = int(end.Sub(start).Hours() / 24)
		if durDays < 1 {
			durDays = 1
		}
	}
	hi := ruleLimit(start, r, to)
	lo := 0
	right := hi
	for lo < right {
		mid := (lo + right) / 2
		s := step(start, r.freq, r.interval, mid)
		if inclusiveEnd(s, occEnd(s, allDay, durDays, dur), allDay) < from {
			lo = mid + 1
		} else {
			right = mid
		}
	}
	var out []span
	for n := lo; n < hi && len(out) < maxOccurrences; n++ {
		s := step(start, r.freq, r.interval, n)
		if !r.until.IsZero() && s.After(r.until) {
			break
		}
		sDate := s.Format("2006-01-02")
		if sDate > to {
			break
		}
		e := occEnd(s, allDay, durDays, dur)
		if inclusiveEnd(s, e, allDay) < from {
			continue
		}
		if _, skip := ex[sDate]; skip {
			continue
		}
		out = append(out, span{start: s, end: e})
	}
	return out
}

func occEnd(start time.Time, allDay bool, durDays int, dur time.Duration) time.Time {
	if allDay {
		return start.AddDate(0, 0, durDays)
	}
	return start.Add(dur)
}

func ruleLimit(anchor time.Time, r *rrule, to string) int {
	if r.count > 0 {
		return r.count
	}
	limit, err := time.Parse("2006-01-02", to)
	if err != nil {
		return 1
	}
	if !r.until.IsZero() {
		u := time.Date(r.until.Year(), r.until.Month(), r.until.Day(), 0, 0, 0, 0, time.UTC)
		if u.Before(limit) {
			limit = u
		}
	}
	days := int(limit.Sub(time.Date(anchor.Year(), anchor.Month(), anchor.Day(), 0, 0, 0, 0, time.UTC)).Hours()/24) + 3
	if days < 1 {
		days = 1
	}
	stepDays := r.interval
	switch r.freq {
	case "weekly":
		stepDays = 7 * r.interval
	case "monthly":
		stepDays = 28 * r.interval
	case "yearly":
		stepDays = 365 * r.interval
	}
	if stepDays < 1 {
		stepDays = 1
	}
	n := days/stepDays + 3
	if n > 20000 {
		n = 20000
	}
	return n
}

func step(anchor time.Time, freq string, interval, n int) time.Time {
	if interval < 1 {
		interval = 1
	}
	switch freq {
	case "weekly":
		return anchor.AddDate(0, 0, n*7*interval)
	case "monthly":
		return addMonths(anchor, n*interval)
	case "yearly":
		return addMonths(anchor, n*interval*12)
	default:
		return anchor.AddDate(0, 0, n*interval)
	}
}

func addMonths(anchor time.Time, months int) time.Time {
	y, m, d := anchor.Date()
	first := time.Date(y, m+time.Month(months), 1, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
	dim := time.Date(first.Year(), first.Month()+1, 0, 0, 0, 0, 0, first.Location()).Day()
	if d > dim {
		d = dim
	}
	return time.Date(first.Year(), first.Month(), d, anchor.Hour(), anchor.Minute(), anchor.Second(), anchor.Nanosecond(), anchor.Location())
}

func applyDuration(start time.Time, allDay bool, value string) (time.Time, bool) {
	days, hours, mins, ok := parseDuration(value)
	if !ok {
		return time.Time{}, false
	}
	if allDay {
		if days < 1 {
			days = 1
		}
		return start.AddDate(0, 0, days), true
	}
	return start.Add(time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour + time.Duration(mins)*time.Minute), true
}

func parseDuration(s string) (days, hours, mins int, ok bool) {
	s = strings.ToUpper(strings.TrimSpace(s))
	if !strings.HasPrefix(s, "P") || len(s) < 2 {
		return 0, 0, 0, false
	}
	s = s[1:]
	var rest string
	if n, r, found := takeNum(s, 'W'); found {
		days += n * 7
		s = r
	}
	if n, r, found := takeNum(s, 'D'); found {
		days += n
		s = r
	}
	if strings.HasPrefix(s, "T") {
		s = s[1:]
		if n, r, found := takeNum(s, 'H'); found {
			hours = n
			s = r
		}
		if n, r, found := takeNum(s, 'M'); found {
			mins = n
			s = r
		}
		if n, r, found := takeNum(s, 'S'); found {
			_ = n
			s = r
		}
	}
	rest = s
	if rest != "" {
		return 0, 0, 0, false
	}
	return days, hours, mins, true
}

func takeNum(s string, unit byte) (int, string, bool) {
	if s == "" || s[0] < '0' || s[0] > '9' {
		return 0, s, false
	}
	i := 0
	n := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		n = n*10 + int(s[i]-'0')
		i++
	}
	if i >= len(s) || s[i] != unit {
		return 0, s, false
	}
	return n, s[i+1:], true
}

func parseWhen(value string, params map[string]string, loc *time.Location) (time.Time, bool, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false, false
	}
	dateOnly := len(value) == 8 && !strings.Contains(value, "T")
	if params != nil && strings.EqualFold(params["VALUE"], "DATE") {
		dateOnly = true
	}
	if dateOnly {
		t, err := time.Parse("20060102", value[:8])
		if err != nil {
			return time.Time{}, false, false
		}
		return t, true, true
	}
	utc := strings.HasSuffix(value, "Z")
	v := strings.TrimSuffix(value, "Z")
	layout := "20060102T150405"
	if len(v) == 13 {
		layout = "20060102T1504"
	}
	if utc {
		t, err := time.Parse(layout, v)
		if err != nil {
			return time.Time{}, false, false
		}
		if loc == nil {
			loc = time.UTC
		}
		return t.In(loc), false, true
	}
	if params != nil {
		if tz := params["TZID"]; tz != "" {
			zone, err := time.LoadLocation(tz)
			if err != nil {
				t, err := time.Parse(layout, v)
				if err != nil {
					return time.Time{}, false, false
				}
				return t, false, true
			}
			t, err := time.ParseInLocation(layout, v, zone)
			if err != nil {
				return time.Time{}, false, false
			}
			return t, false, true
		}
	}
	t, err := time.Parse(layout, v)
	if err != nil {
		return time.Time{}, false, false
	}
	return t, false, true
}

func unescape(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n', 'N':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i+1])
			}
			i++
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
