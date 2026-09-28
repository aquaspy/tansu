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

func TestParseProjectsAbsoluteTimes(t *testing.T) {
	from, to := window("2026-09-01", "2026-09-30")
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:z
SUMMARY:Standup
DTSTART:20260928T120000Z
DTEND:20260928T130000Z
END:VEVENT
BEGIN:VEVENT
UID:ny
SUMMARY:New York
DTSTART;TZID=America/New_York:20260928T090000
DTEND;TZID=America/New_York:20260928T100000
END:VEVENT
BEGIN:VEVENT
UID:sp
SUMMARY:Local
DTSTART;TZID=America/Sao_Paulo:20260928T090000
DTEND;TZID=America/Sao_Paulo:20260928T100000
END:VEVENT
BEGIN:VEVENT
UID:float
SUMMARY:Floating
DTSTART:20260928T090000
DTEND:20260928T100000
END:VEVENT
BEGIN:VEVENT
UID:unknown
SUMMARY:Unknown zone
DTSTART;TZID=Mars/Olympus:20260928T090000
DTEND;TZID=Mars/Olympus:20260928T100000
END:VEVENT
BEGIN:VEVENT
UID:day
SUMMARY:Trip
DTSTART;VALUE=DATE:20260928
DTEND;VALUE=DATE:20260930
END:VEVENT
END:VCALENDAR`
	for _, tc := range []struct {
		loc *time.Location
		uid string
		on  string
		at  string
		end string
		day bool
	}{
		{sp, "z", "2026-09-28", "09:00", "10:00", false},
		{time.UTC, "z", "2026-09-28", "12:00", "13:00", false},
		{sp, "ny", "2026-09-28", "10:00", "11:00", false},
		{sp, "sp", "2026-09-28", "09:00", "10:00", false},
		{sp, "float", "2026-09-28", "09:00", "10:00", false},
		{time.UTC, "float", "2026-09-28", "09:00", "10:00", false},
		{sp, "unknown", "2026-09-28", "09:00", "10:00", false},
		{time.UTC, "unknown", "2026-09-28", "09:00", "10:00", false},
		{sp, "day", "2026-09-28", "", "", true},
	} {
		events, err := Parse([]byte(body), tc.loc, from, to)
		if err != nil {
			t.Fatal(err)
		}
		var got Event
		found := false
		for _, e := range events {
			if e.UID == tc.uid {
				got = e
				found = true
			}
		}
		if !found {
			t.Fatalf("loc %s missing %s in %+v", tc.loc, tc.uid, events)
		}
		if got.StartsOn != tc.on || got.StartsAt != tc.at || got.EndsAt != tc.end || got.AllDay != tc.day {
			t.Fatalf("loc %s uid %s = %+v", tc.loc, tc.uid, got)
		}
		if tc.day && got.EndsOn != "2026-09-29" {
			t.Fatalf("all-day end = %+v", got)
		}
	}
}

func TestParseExpandsBeforeProjectingDST(t *testing.T) {
	from, to := window("2026-03-01", "2026-03-31")
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:utc-week
SUMMARY:UTC weekly
DTSTART:20260301T120000Z
DTEND:20260301T130000Z
RRULE:FREQ=WEEKLY;COUNT=2
END:VEVENT
END:VCALENDAR`
	events, err := Parse([]byte(body), ny, from, to)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range events {
		got[e.StartsOn] = e.StartsAt
	}
	if got["2026-03-01"] != "07:00" || got["2026-03-08"] != "08:00" {
		t.Fatalf("dst clocks = %+v", got)
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
