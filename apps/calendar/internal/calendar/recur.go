package calendar

import (
	"time"

	"github.com/aquasp/kuracalendar/internal/store"
)

// Occurrence is one appearance of an event on the calendar: the series it
// belongs to plus this appearance's dates (the template span shifted).
// One-shots yield at most one occurrence (their template span).
type Occurrence struct {
	Event *store.Event
	Start time.Time
	End   time.Time
}

// MaxOccurrences caps one expansion as misuse insurance. Real callers pass
// bounded ranges (grid ~42 days, API at most 366), which stay far below it.
const MaxOccurrences = 1000

func atDate(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// Occurrences returns one-shots plus expanded series occurrences
// overlapping [from, to], ordered by (start, id) like EventsInRange.
func Occurrences(st *store.Store, userID int64, from, to time.Time) ([]Occurrence, error) {
	rows, err := st.EventsInRange(userID,
		atDate(from).Format("2006-01-02"), atDate(to).Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	var out []Occurrence
	for _, e := range rows {
		if e.Repeating() {
			continue
		}
		out = append(out, Expand(e, from, to)...)
	}
	series, err := st.ListRepeatingEvents(userID)
	if err != nil {
		return nil, err
	}
	for _, e := range series {
		out = append(out, Expand(e, from, to)...)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && lessOccurrence(out[j], out[j-1]); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

func lessOccurrence(a, b Occurrence) bool {
	if !a.Start.Equal(b.Start) {
		return a.Start.Before(b.Start)
	}
	return a.Event.ID < b.Event.ID
}

func maxDate(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

func minDate(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

// Expand returns the occurrences of e overlapping [from, to] (inclusive,
// date precision), oldest first. repeat_until bounds occurrence starts: an
// appearance may end past it, but none starts after it.
func Expand(e *store.Event, from, to time.Time) []Occurrence {
	from, to = atDate(from), atDate(to)
	if to.Before(from) {
		return nil
	}
	start, ok := store.ParseDate(e.StartsOn)
	if !ok {
		return nil
	}
	end, ok := store.ParseDate(e.EndsOn)
	if !ok || end.Before(start) {
		end = start
	}
	dur := int(end.Sub(start).Hours() / 24)

	var until time.Time
	var bounded bool
	if e.RepeatUntil != "" {
		if u, ok := store.ParseDate(e.RepeatUntil); ok {
			until, bounded = u, true
		}
	}
	pastUntil := func(s time.Time) bool { return bounded && s.After(until) }

	switch e.Repeat {
	case "daily", "weekly", "monthly", "yearly":
		// Series path below. Anything else (including hand-edited
		// garbage, which validation normally blocks) falls back to the
		// template span so the event stays visible.
	default:
		if start.After(to) || end.Before(from) {
			return nil
		}
		return []Occurrence{{Event: e, Start: start, End: end}}
	}

	// Fast-forward daily/weekly series whose template starts far behind the
	// range; monthly/yearly steps are coarse enough to iterate directly.
	s := start
	if (e.Repeat == "daily" || e.Repeat == "weekly") && dur >= 0 {
		need := from.AddDate(0, 0, -dur)
		if need.After(s) {
			step := 1
			if e.Repeat == "weekly" {
				step = 7
			}
			days := int(need.Sub(s).Hours() / 24)
			s = s.AddDate(0, 0, (days+step-1)/step*step)
		}
	}

	var out []Occurrence
	for i := 0; i < MaxOccurrences; i++ {
		if s.After(to) || pastUntil(s) {
			break
		}
		occEnd := s.AddDate(0, 0, dur)
		if !occEnd.Before(from) {
			out = append(out, Occurrence{Event: e, Start: s, End: occEnd})
		}
		s = nextStart(e.Repeat, start, s)
		if s.IsZero() {
			break
		}
	}
	return out
}

// nextStart advances one repeat step from s. Monthly steps anchor on the
// template day-of-month and skip months lacking it; yearly steps clamp
// Feb 29 to Feb 28 in common years (like Birthday.ObservedOn).
func nextStart(repeat string, template, s time.Time) time.Time {
	switch repeat {
	case "daily":
		return s.AddDate(0, 0, 1)
	case "weekly":
		return s.AddDate(0, 0, 7)
	case "monthly":
		day := template.Day()
		y, m, _ := s.Date()
		for range 24 {
			m++
			if m > 12 {
				m = 1
				y++
			}
			if day <= store.DaysInMonth(y, int(m)) {
				return time.Date(y, m, day, 0, 0, 0, 0, time.UTC)
			}
		}
		return time.Time{}
	case "yearly":
		y, _, _ := s.Date()
		_, tm, _ := template.Date()
		day := template.Day()
		if max := store.DaysInMonth(y+1, int(tm)); day > max {
			day = max
		}
		return time.Date(y+1, tm, day, 0, 0, 0, 0, time.UTC)
	default:
		return time.Time{}
	}
}
