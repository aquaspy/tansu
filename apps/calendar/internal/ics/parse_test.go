package ics

import (
	"strings"
	"testing"
	"time"
)

func window(from, to string) (time.Time, time.Time) {
	a, _ := time.Parse("2006-01-02", from)
	b, _ := time.Parse("2006-01-02", to)
	return a, b
}

func titles(events []Event) map[string]Event {
	out := map[string]Event{}
	for _, e := range events {
		out[e.StartsOn+" "+e.Title] = e
	}
	return out
}

func TestParseAllDayExclusiveEnd(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	body := "BEGIN:VCALENDAR\r\nBEGIN:VEVENT\r\nUID:trip\r\nSUMMARY:Trip\r\nDTSTART;VALUE=DATE:20260928\r\nDTEND;VALUE=DATE:20260930\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"
	events, err := Parse([]byte(body), time.UTC, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	e := events[0]
	if !e.AllDay || e.StartsOn != "2026-09-28" || e.EndsOn != "2026-09-29" || e.UID != "trip" {
		t.Fatalf("event = %+v", e)
	}
}

func TestParseUTCAndTZID(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	loc := time.FixedZone("UTC-3", -3*3600)
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:utc
SUMMARY:Standup
DTSTART:20260928T150000Z
DTEND:20260928T160000Z
END:VEVENT
BEGIN:VEVENT
UID:wall
SUMMARY:Shift
DTSTART;TZID=America/Sao_Paulo:20260928T090000
DTEND;TZID=America/Sao_Paulo:20260928T170000
END:VEVENT
END:VCALENDAR`
	events, err := Parse([]byte(body), loc, from, to)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Event{}
	for _, e := range events {
		got[e.UID] = e
	}
	if e := got["utc"]; e.StartsOn != "2026-09-28" || e.StartsAt != "12:00" || e.EndsAt != "13:00" || e.AllDay {
		t.Fatalf("utc = %+v", e)
	}
	if e := got["wall"]; e.StartsAt != "09:00" || e.EndsAt != "17:00" || e.StartsOn != "2026-09-28" {
		t.Fatalf("tzid = %+v", e)
	}
}

func TestParseFoldedEscapesAndAlarm(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	body := "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:note\nSUMMARY:Hello\\, world\nDESCRIPTION:Line one\\nNext\n  line\nLOCATION:Cafe\nDTSTART;VALUE=DATE:20260928\nBEGIN:VALARM\nSUMMARY:ignore me\nEND:VALARM\nEND:VEVENT\nEND:VCALENDAR\n"
	events, err := Parse([]byte(body), time.UTC, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	e := events[0]
	if e.Title != "Hello, world" || e.Body != "Line one\nNext line\nCafe" {
		t.Fatalf("event = %+v", e)
	}
}

func TestParseWeeklyRuleAndExotic(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:gym
SUMMARY:Gym
DTSTART;VALUE=DATE:20260901
DTEND;VALUE=DATE:20260902
RRULE:FREQ=WEEKLY;COUNT=5
EXDATE;VALUE=DATE:20260908
END:VEVENT
BEGIN:VEVENT
UID:monday
SUMMARY:Mondays
DTSTART;VALUE=DATE:20260901
RRULE:FREQ=WEEKLY;BYDAY=MO
END:VEVENT
BEGIN:VEVENT
UID:gone
SUMMARY:Cancelled
STATUS:CANCELLED
DTSTART;VALUE=DATE:20260903
END:VEVENT
END:VCALENDAR`
	events, err := Parse([]byte(body), time.UTC, from, to)
	if err != nil {
		t.Fatal(err)
	}
	var gym []string
	var mondays int
	for _, e := range events {
		switch {
		case strings.HasPrefix(e.UID, "gym#"):
			gym = append(gym, e.StartsOn)
		case e.Title == "Mondays":
			mondays++
			if e.StartsOn != "2026-09-01" {
				t.Fatalf("exotic expanded: %+v", e)
			}
		case e.Title == "Cancelled":
			t.Fatalf("cancelled kept: %+v", e)
		}
	}
	want := []string{"2026-09-01", "2026-09-15", "2026-09-22", "2026-09-29"}
	if len(gym) != len(want) {
		t.Fatalf("gym = %v", gym)
	}
	for i := range want {
		if gym[i] != want[i] {
			t.Fatalf("gym = %v", gym)
		}
	}
	if mondays != 1 {
		t.Fatalf("mondays = %d", mondays)
	}
}

func TestParseYearlyFromThePast(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:labor
SUMMARY:Labor Day
DTSTART;VALUE=DATE:20100907
RRULE:FREQ=YEARLY
END:VEVENT
END:VCALENDAR`
	events, err := Parse([]byte(body), time.UTC, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].StartsOn != "2026-09-07" || !events[0].AllDay {
		t.Fatalf("events = %+v", events)
	}
}

func TestParseRejectsNonCalendar(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	if _, err := Parse([]byte("hello"), time.UTC, from, to); err != ErrParse {
		t.Fatalf("err = %v", err)
	}
}

func TestParseOutsideWindowDropped(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:old
SUMMARY:Old
DTSTART;VALUE=DATE:20200101
END:VEVENT
END:VCALENDAR`
	events, err := Parse([]byte(body), time.UTC, from, to)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("events = %+v", events)
	}
}
