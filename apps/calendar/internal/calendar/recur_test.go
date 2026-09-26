package calendar

import (
	"testing"
	"time"

	"github.com/aquasp/kuracalendar/internal/store"
)

func recurEvent(startsOn, endsOn, repeat, until string) *store.Event {
	return &store.Event{ID: 7, Title: "Gym", StartsOn: startsOn, EndsOn: endsOn,
		Repeat: repeat, RepeatUntil: until}
}

func starts(t *testing.T, occs []Occurrence) []string {
	t.Helper()
	var out []string
	for _, o := range occs {
		out = append(out, o.Start.Format("2006-01-02"))
		if o.Event == nil || o.End.Before(o.Start) {
			t.Fatalf("bad occurrence: %+v", o)
		}
	}
	return out
}

func expandRange(e *store.Event, from, to string) []Occurrence {
	f, _ := store.ParseDate(from)
	t, _ := store.ParseDate(to)
	return Expand(e, f, t)
}

func equalStarts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExpandOneShot(t *testing.T) {
	e := recurEvent("2026-09-10", "2026-09-12", "none", "")
	got := starts(t, expandRange(e, "2026-09-01", "2026-09-30"))
	if !equalStarts(got, []string{"2026-09-10"}) {
		t.Fatalf("overlap: %v", got)
	}
	if got := expandRange(e, "2026-10-01", "2026-10-31"); len(got) != 0 {
		t.Fatalf("no overlap: %+v", got)
	}
	// Partial overlap still yields the template span.
	occs := expandRange(e, "2026-09-12", "2026-09-30")
	if len(occs) != 1 || occs[0].End.Format("2006-01-02") != "2026-09-12" {
		t.Fatalf("partial: %+v", occs)
	}
}

func TestExpandDailyWeekly(t *testing.T) {
	daily := recurEvent("2026-09-01", "2026-09-01", "daily", "")
	got := starts(t, expandRange(daily, "2026-09-01", "2026-09-03"))
	if !equalStarts(got, []string{"2026-09-01", "2026-09-02", "2026-09-03"}) {
		t.Fatalf("daily: %v", got)
	}
	weekly := recurEvent("2026-09-07", "2026-09-07", "weekly", "")
	got = starts(t, expandRange(weekly, "2026-09-01", "2026-09-30"))
	if !equalStarts(got, []string{"2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28"}) {
		t.Fatalf("weekly: %v", got)
	}
}

func TestExpandUntilBoundsStarts(t *testing.T) {
	e := recurEvent("2026-09-01", "2026-09-01", "daily", "2026-09-02")
	got := starts(t, expandRange(e, "2026-09-01", "2026-09-30"))
	if !equalStarts(got, []string{"2026-09-01", "2026-09-02"}) {
		t.Fatalf("until: %v", got)
	}
	// A multi-day tail may end past until; nothing starts after it.
	long := recurEvent("2026-09-01", "2026-09-03", "daily", "2026-09-02")
	occs := expandRange(long, "2026-09-01", "2026-09-30")
	if len(occs) != 2 || occs[1].End.Format("2006-01-02") != "2026-09-04" {
		t.Fatalf("tail: %+v", occs)
	}
}

func TestExpandMonthlySkipsShortMonths(t *testing.T) {
	e := recurEvent("2026-01-31", "2026-01-31", "monthly", "")
	got := starts(t, expandRange(e, "2026-01-01", "2026-05-31"))
	// February and April have no 31st; no drift to shorter months.
	if !equalStarts(got, []string{"2026-01-31", "2026-03-31", "2026-05-31"}) {
		t.Fatalf("monthly: %v", got)
	}
}

func TestExpandYearlyFeb29(t *testing.T) {
	e := recurEvent("2024-02-29", "2024-02-29", "yearly", "")
	got := starts(t, expandRange(e, "2024-01-01", "2026-12-31"))
	if !equalStarts(got, []string{"2024-02-29", "2025-02-28", "2026-02-28"}) {
		t.Fatalf("yearly: %v", got)
	}
}

func TestExpandMultiDayDuration(t *testing.T) {
	e := recurEvent("2026-09-04", "2026-09-06", "weekly", "")
	occs := expandRange(e, "2026-09-01", "2026-09-30")
	if len(occs) != 4 {
		t.Fatalf("count: %d", len(occs))
	}
	for _, o := range occs {
		if int(o.End.Sub(o.Start).Hours()/24) != 2 {
			t.Fatalf("duration: %+v", o)
		}
	}
	// Occurrences starting before the range still overlap it.
	occs = expandRange(e, "2026-09-06", "2026-09-06")
	if len(occs) != 1 || occs[0].Start.Format("2006-01-02") != "2026-09-04" {
		t.Fatalf("leading overlap: %+v", occs)
	}
}

func TestExpandFarPastFastForward(t *testing.T) {
	e := recurEvent("2020-01-06", "2020-01-06", "weekly", "")
	got := starts(t, expandRange(e, "2026-09-01", "2026-09-30"))
	if !equalStarts(got, []string{"2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28"}) {
		t.Fatalf("fast-forward: %v", got)
	}
	daily := recurEvent("2020-01-01", "2020-01-01", "daily", "")
	if got := expandRange(daily, "2026-09-01", "2026-09-30"); len(got) != 30 {
		t.Fatalf("daily count: %d", len(got))
	}
}

func TestExpandUnknownRepeatFallsBackToOneShot(t *testing.T) {
	e := recurEvent("2026-09-01", "2026-09-01", "fortnightly", "")
	got := starts(t, expandRange(e, "2026-09-01", "2026-09-30"))
	if !equalStarts(got, []string{"2026-09-01"}) {
		t.Fatalf("unknown repeat: %v", got)
	}
}

func TestExpandEmptyRange(t *testing.T) {
	e := recurEvent("2026-09-01", "2026-09-01", "daily", "")
	f, _ := store.ParseDate("2026-09-30")
	to, _ := store.ParseDate("2026-09-01")
	if got := Expand(e, f, to); len(got) != 0 {
		t.Fatalf("empty range: %+v", got)
	}
	_ = time.UTC
}
