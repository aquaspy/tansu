package handler

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/aquasp/kuracalendar/internal/store"
)

func TestStandaloneTimezoneResyncsFeeds(t *testing.T) {
	f := newFlow(t, nil)
	body := `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:z
SUMMARY:Standup
DTSTART:20260928T120000Z
DTEND:20260928T130000Z
END:VEVENT
END:VCALENDAR`
	f.srv.FetchICS = func(context.Context, string) ([]byte, error) {
		return []byte(body), nil
	}
	f.seedUser("ada@example.com", "password-pass")
	f.login("ada@example.com", "password-pass")
	user, err := f.store.FindUserByEmail("ada@example.com")
	if err != nil {
		t.Fatal(err)
	}
	manual, _, err := f.store.CreateEvent(user.ID, store.EventInput{
		Title: "Dentist", AllDay: false, StartsOn: "2026-09-28", EndsOn: "2026-09-28",
		StartsAt: "09:00", EndsAt: "09:30",
	})
	if err != nil || manual == nil {
		t.Fatal(err)
	}
	if code, _, _ := f.post("/feeds", url.Values{
		"name": {"Work"},
		"url":  {"https://feeds.example/cal.ics"},
	}, nil); code != http.StatusSeeOther {
		t.Fatalf("add feed = %d", code)
	}
	rows, err := f.store.ICSEventsInRange(user.ID, "2026-09-01", "2026-10-31")
	if err != nil || len(rows) != 1 || rows[0].StartsAt != "12:00" {
		t.Fatalf("utc feed = %+v err=%v", rows, err)
	}

	code, page, _ := f.get("/feeds", nil)
	if code != http.StatusOK {
		t.Fatalf("feeds = %d", code)
	}
	mustContain(t, page, `name="timezone"`)
	mustContain(t, page, `value="UTC"`)

	code, _, hdr := f.post("/timezone", url.Values{"timezone": {"America/Sao_Paulo"}}, nil)
	if code != http.StatusSeeOther || hdr.Get("Location") != "/feeds" {
		t.Fatalf("save = %d %q", code, hdr.Get("Location"))
	}
	saved, err := f.store.FindUser(user.ID)
	if err != nil || saved.Timezone != "America/Sao_Paulo" {
		t.Fatalf("zone = %+v err=%v", saved, err)
	}
	rows, err = f.store.ICSEventsInRange(user.ID, "2026-09-01", "2026-10-31")
	if err != nil || len(rows) != 1 || rows[0].StartsAt != "09:00" || rows[0].Title != "Standup" {
		t.Fatalf("resynced = %+v err=%v", rows, err)
	}
	again, err := f.store.FindEvent(user.ID, manual.ID)
	if err != nil || again.StartsAt != "09:00" {
		t.Fatalf("manual = %+v err=%v", again, err)
	}
	_, page, _ = f.get("/feeds", nil)
	mustContain(t, page, "Timezone saved.")
	mustContain(t, page, `value="America/Sao_Paulo"`)

	code, page, _ = f.post("/timezone", url.Values{"timezone": {"BRT"}}, nil)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid = %d", code)
	}
	mustContain(t, page, "Use an IANA name")
	saved, _ = f.store.FindUser(user.ID)
	if saved.Timezone != "America/Sao_Paulo" {
		t.Fatalf("invalid wrote %q", saved.Timezone)
	}

	if err := f.store.SetUserSub(user.ID, "acct-1"); err != nil {
		t.Fatal(err)
	}
	if code, _, _ = f.post("/timezone", url.Values{"timezone": {"Europe/Lisbon"}}, nil); code != http.StatusSeeOther {
		t.Fatalf("locked post = %d", code)
	}
	saved, _ = f.store.FindUser(user.ID)
	if saved.Timezone != "America/Sao_Paulo" {
		t.Fatalf("locked write %q", saved.Timezone)
	}
	_, page, _ = f.get("/feeds", nil)
	mustContain(t, page, "Timezone is set on Tansu Account.")
	mustNotContain(t, page, `name="timezone"`)
}
