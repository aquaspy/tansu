package ics

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aquasp/kuracalendar/internal/store"
)

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

const calA = `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:shift-1
SUMMARY:Morning shift
DESCRIPTION:Floor
DTSTART;VALUE=DATE:20260928
DTEND;VALUE=DATE:20260929
END:VEVENT
BEGIN:VEVENT
UID:shift-2
SUMMARY:Evening shift
DTSTART:20260928T180000Z
DTEND:20260928T220000Z
END:VEVENT
END:VCALENDAR`

const calB = `BEGIN:VCALENDAR
BEGIN:VEVENT
UID:shift-2
SUMMARY:Evening shift renamed
DTSTART:20260928T180000Z
DTEND:20260928T230000Z
END:VEVENT
END:VCALENDAR`

func TestSyncUpsertAndDelete(t *testing.T) {
	st := openStore(t)
	u, err := st.CreateUser("ada@example.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Dentist", AllDay: true, StartsOn: "2026-09-28", EndsOn: "2026-09-28",
	}); err != nil {
		t.Fatal(err)
	}
	feed, err := st.CreateICSFeed(u.ID, "Rota", "https://feeds.example/secret-token/cal.ics")
	if err != nil {
		t.Fatal(err)
	}
	other, err := st.CreateICSFeed(u.ID, "Other", "https://feeds.example/other.ics")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	body := calA
	get := func(context.Context, string) ([]byte, error) { return []byte(body), nil }
	if err := SyncFeed(context.Background(), st, feed, get, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReplaceICSEvents(u.ID, other.ID, []store.ICSEventInput{{
		UID: "keep", Title: "Keep me", AllDay: true, StartsOn: "2026-09-28", EndsOn: "2026-09-28",
	}}); err != nil {
		t.Fatal(err)
	}

	rows, err := st.ICSEventsInRange(u.ID, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, e := range rows {
		if e.FeedID == feed.ID {
			got[e.UID] = e.Title
		}
	}
	if got["shift-1"] != "Morning shift" || got["shift-2"] != "Evening shift" || len(got) != 2 {
		t.Fatalf("rows = %+v", rows)
	}
	if st.CountEvents(u.ID) != 1 {
		t.Fatalf("native events = %d", st.CountEvents(u.ID))
	}

	body = calB
	if err := SyncFeed(context.Background(), st, feed, get, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err = st.ICSEventsInRange(u.ID, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	got = map[string]string{}
	var evening *store.ICSEvent
	keptOther := false
	for _, e := range rows {
		if e.FeedID == feed.ID {
			got[e.UID] = e.Title
			if e.UID == "shift-2" {
				evening = e
			}
		}
		if e.UID == "keep" && e.Title == "Keep me" {
			keptOther = true
		}
	}
	if _, ok := got["shift-1"]; ok || got["shift-2"] != "Evening shift renamed" || !keptOther {
		t.Fatalf("after delete = %+v", rows)
	}
	if evening == nil || evening.EndsAt != "23:00" || evening.AllDay {
		t.Fatalf("updated = %+v", evening)
	}
	reloaded, err := st.FindICSFeed(u.ID, feed.ID)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.EventCount != 1 || reloaded.LastError != "" || reloaded.LastFetchedAt == nil {
		t.Fatalf("feed = %+v", reloaded)
	}
	if reloaded.Host() != "feeds.example" {
		t.Fatalf("host = %q", reloaded.Host())
	}
}

func TestSyncFeedUsesUserZoneNotProcessLocal(t *testing.T) {
	prev := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = prev })

	st := openStore(t)
	u, err := st.CreateUser("ada@example.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateUserTimezone(u.ID, "America/Sao_Paulo"); err != nil {
		t.Fatal(err)
	}
	manual, _, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Dentist", AllDay: false, StartsOn: "2026-09-28", EndsOn: "2026-09-28",
		StartsAt: "09:00", EndsAt: "09:30",
	})
	if err != nil || manual == nil {
		t.Fatal(err)
	}
	feed, err := st.CreateICSFeed(u.ID, "Rota", "https://feeds.example/cal.ics")
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
END:VCALENDAR`
	get := func(context.Context, string) ([]byte, error) { return []byte(body), nil }
	now := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC) // 22:00 on the 28th in São Paulo
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	if err := SyncFeed(context.Background(), st, feed, get, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ICSEventsInRange(u.ID, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].StartsAt != "12:00" {
		t.Fatalf("utc sync = %+v", rows)
	}
	if err := SyncUserFeeds(context.Background(), st, u.ID, get, now, sp); err != nil {
		t.Fatal(err)
	}
	rows, err = st.ICSEventsInRange(u.ID, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].StartsAt != "09:00" || rows[0].StartsOn != "2026-09-28" {
		t.Fatalf("sp resync = %+v", rows)
	}
	again, err := st.FindEvent(u.ID, manual.ID)
	if err != nil || again.StartsAt != "09:00" || again.Title != "Dentist" {
		t.Fatalf("manual event = %+v err=%v", again, err)
	}
}

func TestSyncFetchErrorKeepsEvents(t *testing.T) {
	st := openStore(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	feed, err := st.CreateICSFeed(u.ID, "Rota", "https://feeds.example/cal.ics")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	get := func(context.Context, string) ([]byte, error) { return []byte(calA), nil }
	if err := SyncFeed(context.Background(), st, feed, get, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	get = func(context.Context, string) ([]byte, error) { return nil, ErrFetch }
	if err := SyncFeed(context.Background(), st, feed, get, now, time.UTC); err != nil {
		t.Fatal(err)
	}
	rows, err := st.ICSEventsInRange(u.ID, "2026-09-01", "2026-09-30")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("kept = %+v", rows)
	}
	reloaded, _ := st.FindICSFeed(u.ID, feed.ID)
	if reloaded.LastError != "fetch" || reloaded.EventCount != 2 {
		t.Fatalf("feed = %+v", reloaded)
	}
}

func TestSyncCapsPerFeed(t *testing.T) {
	st := openStore(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	feed, err := st.CreateICSFeed(u.ID, "Busy", "https://feeds.example/cal.ics")
	if err != nil {
		t.Fatal(err)
	}
	inputs := make([]store.ICSEventInput, 0, store.ICSEventsPerFeedCap+5)
	day := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < store.ICSEventsPerFeedCap+5; i++ {
		d := day.AddDate(0, 0, i).Format("2006-01-02")
		inputs = append(inputs, store.ICSEventInput{
			UID: "u" + d, Title: "E", AllDay: true, StartsOn: d, EndsOn: d,
		})
	}
	n, capped, err := st.ReplaceICSEvents(u.ID, feed.ID, inputs)
	if err != nil {
		t.Fatal(err)
	}
	if n != store.ICSEventsPerFeedCap || !capped {
		t.Fatalf("n=%d capped=%v", n, capped)
	}
	reloaded, _ := st.FindICSFeed(u.ID, feed.ID)
	if reloaded.LastError != "capped" || reloaded.ErrorKey() != "feeds.capped" {
		t.Fatalf("feed = %+v", reloaded)
	}
	if st.CountEvents(u.ID) != 0 {
		t.Fatal("ics rows counted as native events")
	}
}

func TestSyncBlockedCode(t *testing.T) {
	st := openStore(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	feed, err := st.CreateICSFeed(u.ID, "Nope", "https://feeds.example/secret/cal.ics")
	if err != nil {
		t.Fatal(err)
	}
	get := func(context.Context, string) ([]byte, error) {
		return nil, errors.Join(ErrBlocked, errors.New("dial 10.0.0.1"))
	}
	if err := SyncFeed(context.Background(), st, feed, get, time.Now(), time.UTC); err != nil {
		t.Fatal(err)
	}
	reloaded, _ := st.FindICSFeed(u.ID, feed.ID)
	if reloaded.LastError != "blocked" {
		t.Fatalf("code = %q", reloaded.LastError)
	}
}
