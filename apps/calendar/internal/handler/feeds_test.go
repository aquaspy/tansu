package handler

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func TestICSFeedFlow(t *testing.T) {
	f := newFlow(t, nil)
	day := time.Now().UTC()
	day = time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, time.UTC)
	stamp := day.Format("20060102")
	iso := day.Format("2006-01-02")
	path := "/" + day.Format("2006/1/2")
	var body string
	body = "BEGIN:VCALENDAR\n" +
		"BEGIN:VEVENT\nUID:shift-1\nSUMMARY:Morning shift\nDESCRIPTION:Floor notes\nDTSTART;VALUE=DATE:" + stamp + "\nEND:VEVENT\n" +
		"BEGIN:VEVENT\nUID:shift-2\nSUMMARY:Evening shift\nDTSTART;VALUE=DATE:" + stamp + "\nEND:VEVENT\n" +
		"END:VCALENDAR\n"
	f.srv.FetchICS = func(context.Context, string) ([]byte, error) {
		return []byte(body), nil
	}
	f.seedUser("ada@example.com", "password-pass")
	f.login("ada@example.com", "password-pass")

	code, page, _ := f.get("/feeds", nil)
	if code != http.StatusOK {
		t.Fatalf("feeds status = %d", code)
	}
	mustContain(t, page, `name="url"`)
	mustContain(t, page, "No feeds yet.")

	code, page, _ = f.post("/feeds", url.Values{
		"name": {"Work rota"},
		"url":  {"http://feeds.example/cal.ics"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("http url status = %d", code)
	}
	mustContain(t, page, "Use an https:// link.")

	code, page, _ = f.post("/feeds", url.Values{
		"name": {"Work rota"},
		"url":  {"https://127.0.0.1/secret-token/cal.ics"},
	}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("loopback status = %d", code)
	}
	mustContain(t, page, "That address is not allowed.")
	mustNotContain(t, page, "secret-token")

	code, _, _ = f.post("/feeds", url.Values{
		"name": {"Work rota"},
		"url":  {"https://feeds.example/secret-token/cal.ics"},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("add status = %d", code)
	}
	code, page, _ = f.get("/feeds", nil)
	if code != http.StatusOK {
		t.Fatalf("list status = %d", code)
	}
	mustContain(t, page, "Work rota")
	mustContain(t, page, "feeds.example")
	mustContain(t, page, "Feed added.")
	mustNotContain(t, page, "secret-token")

	code, page, _ = f.get(path, nil)
	if code != http.StatusOK {
		t.Fatalf("calendar status = %d", code)
	}
	mustContain(t, page, "Morning shift")
	mustContain(t, page, "Evening shift")
	mustContain(t, page, `class="day-item is-ics"`)
	mustContain(t, page, `data-action="ics#open"`)
	mustContain(t, page, `class="pill ics"`)
	mustContain(t, page, "Read-only.")
	mustContain(t, page, `data-feed="Work rota"`)
	mustNotContain(t, page, "secret-token")

	code, _, _ = f.post("/events", url.Values{
		"title": {"Dentist"}, "all_day": {"1"}, "starts_on": {iso}, "ends_on": {iso},
	}, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("native create = %d", code)
	}
	_, page, _ = f.get(path, nil)
	mustContain(t, page, "Dentist")
	mustContain(t, page, `data-action="composer#newEvent"`)
	mustContain(t, page, "Morning shift")

	body = "BEGIN:VCALENDAR\nBEGIN:VEVENT\nUID:shift-2\nSUMMARY:Evening shift\nDTSTART;VALUE=DATE:" + stamp + "\nEND:VEVENT\nEND:VCALENDAR\n"
	code, _, _ = f.post("/feeds/1/refresh", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("refresh = %d", code)
	}
	_, page, _ = f.get(path, nil)
	mustNotContain(t, page, "Morning shift")
	mustContain(t, page, "Evening shift")
	mustContain(t, page, "Dentist")

	code, _, _ = f.post("/feeds/1/pause", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("pause = %d", code)
	}
	_, page, _ = f.get("/feeds", nil)
	mustContain(t, page, "Paused")

	code, _, _ = f.post("/feeds/1/delete", nil, nil)
	if code != http.StatusSeeOther {
		t.Fatalf("delete = %d", code)
	}
	_, page, _ = f.get(path, nil)
	mustNotContain(t, page, "Evening shift")
	mustContain(t, page, "Dentist")
}
