package calendar

import (
	"testing"
	"time"

	"github.com/aquasp/kuracalendar/internal/holidays"
	"github.com/aquasp/kuracalendar/internal/store"
)

func openTest(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func TestGridMondayFirst(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	// September 2026 starts on a Tuesday; the grid starts Monday Aug 31.
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, []string{"BR"}, month, month, month)
	if err != nil {
		t.Fatal(err)
	}
	if g.StartDate.Weekday() != time.Monday || g.EndDate.Weekday() != time.Sunday {
		t.Fatalf("week = %s..%s", g.StartDate.Format("2006-01-02"), g.EndDate.Format("2006-01-02"))
	}
	if len(g.Cells)%7 != 0 || len(g.Cells) < 28 {
		t.Fatalf("cells = %d", len(g.Cells))
	}
	if g.PrevMonth.Month() != time.August || g.NextMonth.Month() != time.October {
		t.Fatalf("nav = %s / %s", g.PrevMonth.Format("2006-01"), g.NextMonth.Format("2006-01"))
	}
}

func TestGridExpandsSeries(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	e, errs, err := st.CreateEvent(u.ID, store.EventInput{Title: "Gym",
		AllDay: true, StartsOn: "2026-09-07", EndsOn: "2026-09-07",
		Emoji: "🏋️", Repeat: "weekly"})
	if err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, month, month)
	if err != nil {
		t.Fatal(err)
	}
	var hits []string
	byDate := map[string][]*store.Event{}
	for _, c := range g.Cells {
		key := c.Date.Format("2006-01-02")
		byDate[key] = c.Events
		for _, ev := range c.Events {
			if ev.ID == e.ID {
				hits = append(hits, key)
			}
		}
	}
	want := []string{"2026-09-07", "2026-09-14", "2026-09-21", "2026-09-28"}
	if len(hits) != len(want) {
		t.Fatalf("hits = %v, want %v", hits, want)
	}
	for i := range want {
		if hits[i] != want[i] {
			t.Fatalf("hits = %v, want %v", hits, want)
		}
	}
	// Cells carry the series template (edit/delete stay series-based).
	if ev := byDate["2026-09-14"][0]; ev.StartsOn != "2026-09-07" || ev.Emoji != "🏋️" {
		t.Fatalf("template: %+v", ev)
	}
	// Pills prefix the emoji.
	for _, c := range g.Cells {
		if c.Date.Format("2006-01-02") != "2026-09-14" {
			continue
		}
		marks, _ := c.Marks(func(h holidays.Holiday) string { return "" })
		if len(marks) != 1 || marks[0].Label != "🏋️ Gym" {
			t.Fatalf("marks: %+v", marks)
		}
	}
}

func TestGridSelectedAndToday(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	selected := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	today := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, selected, today)
	if err != nil {
		t.Fatal(err)
	}
	cell := g.SelectedCell()
	if cell == nil || !cell.Date.Equal(selected) || !cell.Selected {
		t.Fatal("selected cell missing")
	}
	foundToday := false
	for _, c := range g.Cells {
		if c.Today {
			foundToday = true
			if !c.Date.Equal(today) {
				t.Fatalf("today = %s", c.Date.Format("2006-01-02"))
			}
		}
	}
	if !foundToday {
		t.Fatal("today flag missing")
	}
}

func TestGridTodayIsUserCivilDate(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.UpsertSyncedPaymentDay(u.ID, "spend:1", store.PaymentDayInput{
		Title: "Rent", DueDay: 28,
	}); err != nil || len(errs) > 0 {
		t.Fatalf("upsert: %v %+v", err, errs)
	}
	sp, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	// 22:00 on the 28th in São Paulo is already the 29th in UTC.
	now := time.Date(2026, 9, 29, 1, 0, 0, 0, time.UTC)
	today := CivilToday(now, sp)
	if today.Format("2006-01-02") != "2026-09-28" {
		t.Fatalf("civil today = %s", today.Format("2006-01-02"))
	}
	if CivilToday(now, time.UTC).Format("2006-01-02") != "2026-09-29" {
		t.Fatal("utc civil date drifted")
	}
	month := time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, today, today)
	if err != nil {
		t.Fatal(err)
	}
	var cell *Cell
	for _, c := range g.Cells {
		if c.Today {
			cell = c
		}
	}
	if cell == nil || cell.Date.Format("2006-01-02") != "2026-09-28" {
		t.Fatalf("today cell = %+v", cell)
	}
	if len(cell.PaymentDays) != 1 || cell.PaymentDays[0].DueDay != 28 {
		t.Fatalf("payment = %+v", cell.PaymentDays)
	}
	start, end := MonthContaining(time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC), sp)
	if start.Format("2006-01-02") != "2026-09-01" || end.Format("2006-01-02") != "2026-09-30" {
		t.Fatalf("sp month = %s %s", start.Format("2006-01-02"), end.Format("2006-01-02"))
	}
	start, end = MonthContaining(time.Date(2026, 10, 1, 2, 0, 0, 0, time.UTC), time.UTC)
	if start.Format("2006-01-02") != "2026-10-01" {
		t.Fatalf("utc month = %s", start.Format("2006-01-02"))
	}
}

func TestGridSpanningEventOnBothDays(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Trip", AllDay: true, StartsOn: "2026-08-31", EndsOn: "2026-09-01",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, month, month)
	if err != nil {
		t.Fatal(err)
	}
	hits := 0
	for _, c := range g.Cells {
		for _, e := range c.Events {
			if e.Title == "Trip" {
				hits++
			}
		}
	}
	if hits != 2 {
		t.Fatalf("spanning hits = %d", hits)
	}
}

func TestGridPaymentDayClampsAndMarks(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.UpsertSyncedPaymentDay(u.ID, "spend:1", store.PaymentDayInput{
		Title: "Card", DueDay: 31, Notes: "limit",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("upsert: %v %+v", err, errs)
	}
	month := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	day := time.Date(2026, 2, 28, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, day, month)
	if err != nil {
		t.Fatal(err)
	}
	cell := g.SelectedCell()
	if cell.Empty() || len(cell.PaymentDays) != 1 || cell.PaymentDays[0].Title != "Card" {
		t.Fatalf("feb 28: %+v", cell.PaymentDays)
	}
	marks, _ := cell.Marks(func(holidays.Holiday) string { return "" })
	if len(marks) != 1 || marks[0].Kind != "payment" || marks[0].Label != "Card" {
		t.Fatalf("marks: %+v", marks)
	}
	// The 27th is not the clamped day.
	var earlier *Cell
	for _, c := range g.Cells {
		if c.Date.Day() == 27 && c.Date.Month() == time.February {
			earlier = c
		}
	}
	if earlier == nil || len(earlier.PaymentDays) != 0 {
		t.Fatal("payment day leaked onto the 27th")
	}
}

func TestGridFeb29Birthday(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.CreateBirthday(u.ID, store.BirthdayInput{
		Name: "Ada", Month: "2", Day: "29",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("create: %v %+v", err, errs)
	}
	month := time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, month, month)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range g.Cells {
		if len(c.Birthdays) > 0 {
			seen[c.Date.Format("2006-01-02")] = true
		}
	}
	if len(seen) != 1 || !seen["2025-02-28"] {
		t.Fatalf("observed = %+v", seen)
	}
}

func TestMarksFirstThreePlusOverflow(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	for _, title := range []string{"A", "B", "C", "D"} {
		if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
			Title: title, AllDay: true, StartsOn: "2026-09-07", EndsOn: "2026-09-07",
		}); err != nil || len(errs) > 0 {
			t.Fatalf("create: %v %+v", err, errs)
		}
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	day := time.Date(2026, 9, 7, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, []string{"BR"}, month, day, month)
	if err != nil {
		t.Fatal(err)
	}
	cell := g.SelectedCell()
	marks, extra := cell.Marks(func(h holidays.Holiday) string { return h.Key })
	// 1 holiday + 4 events: 3 pills + 2 overflow.
	if len(marks) != 3 || extra != 2 {
		t.Fatalf("marks = %d extra = %d", len(marks), extra)
	}
	if marks[0].Kind != "holiday" || marks[1].Kind != "event" {
		t.Fatalf("order = %+v", marks)
	}
	// Single pack: no country tag.
	if cell.TagCountries {
		t.Fatal("single pack should not tag countries")
	}

	g2, err := BuildGrid(st, u.ID, []string{"BR", "US"}, month, day, month)
	if err != nil {
		t.Fatal(err)
	}
	if !g2.SelectedCell().TagCountries {
		t.Fatal("two packs should tag countries")
	}
}

func TestGridICSEventsAreSeparate(t *testing.T) {
	st := openTest(t)
	u, _ := st.CreateUser("ada@example.com", "digest")
	if _, errs, err := st.CreateEvent(u.ID, store.EventInput{
		Title: "Dentist", AllDay: true, StartsOn: "2026-09-28", EndsOn: "2026-09-28",
	}); err != nil || len(errs) > 0 {
		t.Fatalf("native: %v %+v", err, errs)
	}
	feed, err := st.CreateICSFeed(u.ID, "Work rota", "https://feeds.example/secret/cal.ics")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := st.ReplaceICSEvents(u.ID, feed.ID, []store.ICSEventInput{
		{UID: "shift", Title: "Morning shift", Body: "Floor", AllDay: true, StartsOn: "2026-09-28", EndsOn: "2026-09-29"},
	}); err != nil {
		t.Fatal(err)
	}
	month := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	day := time.Date(2026, 9, 28, 0, 0, 0, 0, time.UTC)
	g, err := BuildGrid(st, u.ID, nil, month, day, month)
	if err != nil {
		t.Fatal(err)
	}
	cell := g.SelectedCell()
	if cell.Empty() || len(cell.Events) != 1 || len(cell.ICSEvents) != 1 {
		t.Fatalf("cell events=%d ics=%d", len(cell.Events), len(cell.ICSEvents))
	}
	if cell.ICSEvents[0].FeedName != "Work rota" || cell.ICSEvents[0].Title != "Morning shift" {
		t.Fatalf("ics = %+v", cell.ICSEvents[0])
	}
	// Spans the next day too. Native events stay a different slice.
	var next *Cell
	for _, c := range g.Cells {
		if c.Date.Format("2006-01-02") == "2026-09-29" {
			next = c
		}
	}
	if next == nil || len(next.ICSEvents) != 1 || len(next.Events) != 0 {
		t.Fatalf("next = %+v", next)
	}
	marks, _ := cell.Marks(func(holidays.Holiday) string { return "" })
	var sawEvent, sawICS bool
	for _, m := range marks {
		if m.Kind == "event" && m.Label == "Dentist" {
			sawEvent = true
		}
		if m.Kind == "ics" && m.Label == "Morning shift" {
			sawICS = true
		}
	}
	if !sawEvent || !sawICS {
		t.Fatalf("marks = %+v", marks)
	}
	if st.CountEvents(u.ID) != 1 {
		t.Fatal("ics event counted against the native cap")
	}
}
