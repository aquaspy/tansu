package suite

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func writeServer(t *testing.T, nest string) (*httptest.Server, *string, *string, *string, *int) {
	t.Helper()
	var method, path, raw string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		method = r.Method
		path = r.URL.RequestURI()
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		if r.Method == http.MethodDelete {
			if strings.Contains(r.URL.Path, "/folders") {
				_ = json.NewEncoder(w).Encode(map[string]any{"folder": r.URL.Query().Get("folder"), "deleted": 2})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/notes/") {
			_ = json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{
				"id": 4, "title": "Old", "body": "Old title\nkeep this\n", "folder": "travel",
			}})
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		w.WriteHeader(status)
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		inner, _ := doc[nest].(map[string]any)
		title, _ := inner["title"].(string)
		if title == "" {
			title, _ = inner["body"].(string)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{nest: map[string]any{"id": 9, "title": title}})
	}))
	return srv, &method, &path, &raw, &hits
}

func nested(t *testing.T, raw, key string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("body %s: %v", raw, err)
	}
	obj, _ := doc[key].(map[string]any)
	if obj == nil {
		t.Fatalf("no %s in %s", key, raw)
	}
	return obj
}

func TestSpendCreateForwardsMoneyCategoryAndDates(t *testing.T) {
	srv, method, path, raw, hits := writeServer(t, "expense")
	defer srv.Close()
	c := &Client{App: App{Name: "spend", Base: srv.URL}, Token: "kura_spend"}

	out := Execute(context.Background(), c, SpendCreate, map[string]any{
		"expense": map[string]any{
			"title": "Lunch", "amount": "25,50", "currency": "reais",
			"spent_on": "19/09/2026", "category": "comida",
			"notes": "with Ada", "tags": []any{"lunch", "team"},
		},
	}, false)
	if !out.OK || *hits != 1 || *method != http.MethodPost || *path != "/api/v1/expenses" {
		t.Fatalf("%+v %s %s hits %d body %s", out, *method, *path, *hits, *raw)
	}
	exp := nested(t, *raw, "expense")
	if exp["title"] != "Lunch" || exp["amount"] != "25,50" || exp["currency"] != "BRL" {
		t.Fatalf("money %#v", exp)
	}
	if exp["spent_on"] != "2026-09-19" || exp["category"] != "food" {
		t.Fatalf("date/category %#v", exp)
	}
	notes, _ := exp["notes"].(string)
	if !strings.Contains(notes, "with Ada") || !strings.Contains(notes, "tags: lunch, team") {
		t.Fatalf("notes %q", notes)
	}
	if _, ok := exp["tags"]; ok {
		t.Fatalf("tags leaked into the expense object: %s", *raw)
	}

	*hits = 0
	out = Execute(context.Background(), c, SpendCreate, map[string]any{
		"title": "Bus", "amount_cents": 25.5, "spent_on": "September 19, 2026", "category": "Transport",
	}, false)
	exp = nested(t, *raw, "expense")
	if !out.OK || exp["amount"] != "25.5" || exp["amount_cents"] != nil || exp["spent_on"] != "2026-09-19" || exp["category"] != "transport" {
		t.Fatalf("fractional cents %+v body %s", out, *raw)
	}

	out = Execute(context.Background(), c, SpendCreate, map[string]any{
		"title": "Rent", "amount_cents": float64(150000), "spent_on": "2026-09-01", "currency": "usd",
	}, false)
	exp = nested(t, *raw, "expense")
	if exp["amount_cents"] != float64(150000) || exp["currency"] != "USD" {
		t.Fatalf("cents %#v", exp)
	}
	if _, ok := exp["amount"]; ok {
		t.Fatalf("integer cents also sent amount: %s", *raw)
	}
}

func TestSpendUpdateIsPartial(t *testing.T) {
	srv, method, path, raw, hits := writeServer(t, "expense")
	defer srv.Close()
	c := &Client{App: App{Name: "spend", Base: srv.URL}, Token: "kura_spend"}
	out := Execute(context.Background(), c, SpendUpdate, map[string]any{
		"id": float64(9), "category": "leisure",
	}, false)
	if !out.OK || *method != http.MethodPatch || *path != "/api/v1/expenses/9" || *hits != 1 {
		t.Fatalf("%+v %s %s", out, *method, *path)
	}
	exp := nested(t, *raw, "expense")
	if exp["category"] != "leisure" || len(exp) != 1 {
		t.Fatalf("partial %#v", exp)
	}
}

func TestSpendSubscriptionAndPaymentDay(t *testing.T) {
	srv, method, path, raw, hits := writeServer(t, "subscription")
	defer srv.Close()
	c := &Client{App: App{Name: "spend", Base: srv.URL}, Token: "kura_spend"}

	out := Execute(context.Background(), c, SpendSubscriptionCreate, map[string]any{
		"subscription": map[string]any{
			"title": "Music", "amount": float64(19.9), "currency": "eur",
			"interval": "anual", "due_day": float64(10), "billing_month": "3", "active": "false",
		},
	}, false)
	if !out.OK || *method != http.MethodPost || *path != "/api/v1/subscriptions" {
		t.Fatalf("create %+v %s %s body %s", out, *method, *path, *raw)
	}
	sub := nested(t, *raw, "subscription")
	if sub["title"] != "Music" || sub["amount"] != "19.9" || sub["currency"] != "EUR" {
		t.Fatalf("money %#v", sub)
	}
	if sub["interval"] != "yearly" || sub["active"] != false {
		t.Fatalf("schedule %#v", sub)
	}
	if _, ok := sub["due_day"]; ok {
		t.Fatalf("due_day still sent: %#v", sub)
	}
	if _, ok := sub["billing_month"]; ok {
		t.Fatalf("billing_month still sent: %#v", sub)
	}

	out = Execute(context.Background(), c, SpendSubscriptionUpdate, map[string]any{
		"id": json.Number("9"), "active": false,
	}, false)
	sub = nested(t, *raw, "subscription")
	if !out.OK || *method != http.MethodPatch || *path != "/api/v1/subscriptions/9" || len(sub) != 1 || sub["active"] != false {
		t.Fatalf("pause %#v %s %s", sub, *method, *path)
	}

	if !IsDelete(SpendSubscriptionDelete) {
		t.Fatal("subscription delete is not gated")
	}
	held := Execute(context.Background(), c, SpendSubscriptionDelete, map[string]any{"id": float64(9)}, false)
	if held.OK || !strings.Contains(held.Body, "needs_confirm") {
		t.Fatalf("unapproved %+v", held)
	}
	before := *hits
	out = Execute(context.Background(), c, SpendSubscriptionDelete, map[string]any{"id": float64(9)}, true)
	if !out.OK || *hits != before+1 || *method != http.MethodDelete || !strings.Contains(out.Body, `"ok":true`) {
		t.Fatalf("delete %+v %s hits %d body %s", out, *method, *hits, out.Body)
	}

	out = Execute(context.Background(), c, SpendPaymentDayCreate, map[string]any{
		"payment_day": map[string]any{"title": "Water", "due_day": "10th", "notes": "bill"},
	}, false)
	if !out.OK || *path != "/api/v1/payment_days" {
		t.Fatalf("payment %+v %s body %s", out, *path, *raw)
	}
	day := nested(t, *raw, "payment_day")
	if day["title"] != "Water" || day["due_day"] != float64(10) || day["notes"] != "bill" {
		t.Fatalf("payment day %#v", day)
	}
	if !IsDelete(SpendPaymentDayDelete) {
		t.Fatal("payment day delete is not gated")
	}
}

func TestSpendListNormalizesFilters(t *testing.T) {
	srv, _, path, _, _ := writeServer(t, "expense")
	defer srv.Close()
	c := &Client{App: App{Name: "spend", Base: srv.URL}, Token: "kura_spend"}
	_ = Execute(context.Background(), c, SpendList, map[string]any{"month": "September 2026", "category": "Comida"}, false)
	if !strings.Contains(*path, "month=2026-09") || !strings.Contains(*path, "category=food") {
		t.Fatalf("query %s", *path)
	}
	_ = Execute(context.Background(), c, SpendSubscriptionList, map[string]any{"active": true}, false)
	if !strings.Contains(*path, "/api/v1/subscriptions?active=true") {
		t.Fatalf("subs %s", *path)
	}
	_ = Execute(context.Background(), c, SpendMonth, map[string]any{"year": "2026", "month": json.Number("9")}, false)
	if !strings.HasSuffix(*path, "/api/v1/months/2026/9") {
		t.Fatalf("month %s", *path)
	}
}

func TestCalendarCreateNormalizesTimesAndRepeat(t *testing.T) {
	srv, method, path, raw, hits := writeServer(t, "event")
	defer srv.Close()
	c := &Client{App: App{Name: "calendar", Base: srv.URL}, Token: "kura_cal"}

	out := Execute(context.Background(), c, CalCreate, map[string]any{
		"event": map[string]any{
			"title": "Call", "starts_on": "26/09/2026", "ends_on": "September 26, 2026",
			"starts_at": "2:30 PM", "ends_at": "15h00",
			"repeat": "semanal", "repeat_until": "2026/12/26",
			"emoji": "📞", "location": "kitchen", "attendees": []any{"Ada"},
		},
	}, false)
	if !out.OK || *hits != 1 || *method != http.MethodPost || *path != "/api/v1/events" {
		t.Fatalf("%+v %s %s body %s", out, *method, *path, *raw)
	}
	ev := nested(t, *raw, "event")
	if ev["starts_on"] != "2026-09-26" || ev["ends_on"] != "2026-09-26" {
		t.Fatalf("dates %#v", ev)
	}
	if ev["all_day"] != false || ev["starts_at"] != "14:30" || ev["ends_at"] != "15:00" {
		t.Fatalf("times %#v", ev)
	}
	if ev["repeat"] != "weekly" || ev["repeat_until"] != "2026-12-26" || ev["emoji"] != "📞" {
		t.Fatalf("repeat %#v", ev)
	}
	body, _ := ev["body"].(string)
	if !strings.Contains(body, "location: kitchen") || !strings.Contains(body, "attendees: Ada") {
		t.Fatalf("body %q", body)
	}
	if _, ok := ev["location"]; ok {
		t.Fatalf("location leaked: %s", *raw)
	}

	out = Execute(context.Background(), c, CalUpdate, map[string]any{
		"id": float64(9), "title": "Call moved",
	}, false)
	ev = nested(t, *raw, "event")
	if !out.OK || *method != http.MethodPatch || *path != "/api/v1/events/9" || len(ev) != 1 || ev["title"] != "Call moved" {
		t.Fatalf("partial %#v %s", ev, *path)
	}

	_ = Execute(context.Background(), c, CalList, map[string]any{"from": "01/09/2026", "to": "30/09/2026"}, false)
	if !strings.Contains(*path, "from=2026-09-01") || !strings.Contains(*path, "to=2026-09-30") {
		t.Fatalf("range %s", *path)
	}
	if !IsDelete(CalDelete) {
		t.Fatal("calendar delete is not gated")
	}
}

func TestCalendarListKeepsRepeat(t *testing.T) {
	raw, _ := json.Marshal(map[string]any{"events": []any{
		map[string]any{
			"id": 1, "title": "Gym", "starts_on": "2026-09-07", "ends_on": "2026-09-07",
			"all_day": true, "body": "SECRET", "repeat": "weekly", "repeat_until": "2026-12-07",
			"occurrence_on": "2026-09-07",
		},
	}})
	out, err := ShapeList("events", raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "SECRET") || !strings.Contains(string(out), `"repeat":"weekly"`) || !strings.Contains(string(out), "2026-12-07") {
		t.Fatalf("shaped %s", out)
	}
}

func TestNotesTitleFolderAndConfirm(t *testing.T) {
	srv, method, path, raw, hits := writeServer(t, "note")
	defer srv.Close()
	c := &Client{App: App{Name: "notes", Base: srv.URL}, Token: "kura_notes"}

	out := Execute(context.Background(), c, NotesCreate, map[string]any{
		"note": map[string]any{"title": "Trip", "body": "Kyoto in April", "folder": "inbox"},
	}, false)
	if !out.OK || *method != http.MethodPost || *path != "/api/v1/notes" {
		t.Fatalf("create %+v %s %s body %s", out, *method, *path, *raw)
	}
	note := nested(t, *raw, "note")
	if note["body"] != "Trip\nKyoto in April" || note["folder"] != "inbox" {
		t.Fatalf("composed %#v", note)
	}
	if _, ok := note["title"]; ok {
		t.Fatalf("title must be the first line, not its own field: %s", *raw)
	}

	*hits = 0
	out = Execute(context.Background(), c, NotesUpdate, map[string]any{
		"id": float64(4), "title": "New title",
	}, false)
	if !out.OK || *hits != 2 || *method != http.MethodPatch || *path != "/api/v1/notes/4" {
		t.Fatalf("rename %+v hits %d %s %s body %s", out, *hits, *method, *path, *raw)
	}
	note = nested(t, *raw, "note")
	if note["body"] != "New title\nkeep this\n" {
		t.Fatalf("kept the rest: %#v", note)
	}
	if _, ok := note["folder"]; ok {
		t.Fatalf("title rewrite also sent folder: %s", *raw)
	}

	out = Execute(context.Background(), c, NotesUpdate, map[string]any{
		"id": float64(4), "folder": "",
	}, false)
	note = nested(t, *raw, "note")
	if note["folder"] != "inbox" || note["body"] != nil {
		t.Fatalf("inbox move %#v", note)
	}

	if !IsDelete(NotesFolderDelete) {
		t.Fatal("folder delete is not gated")
	}
	before := *hits
	held := Execute(context.Background(), c, NotesFolderDelete, map[string]any{"folder": "travel"}, false)
	if held.OK || *hits != before || !strings.Contains(held.Body, "needs_confirm") {
		t.Fatalf("unapproved %+v hits %d", held, *hits)
	}
	out = Execute(context.Background(), c, NotesFolderDelete, map[string]any{"folder": "travel"}, true)
	if !out.OK || *hits != before+1 || *method != http.MethodDelete || !strings.Contains(*path, "/api/v1/folders?folder=travel") {
		t.Fatalf("clear %+v %s %s", out, *method, *path)
	}
	if !strings.Contains(out.Body, `"ok":true`) || !strings.Contains(out.Body, `"deleted"`) {
		t.Fatalf("result %s", out.Body)
	}

	out = Execute(context.Background(), c, NotesFolderRename, map[string]any{"from": "travel", "to": "trips"}, false)
	if !out.OK || *method != http.MethodPatch || *path != "/api/v1/folders" {
		t.Fatalf("rename %+v %s %s body %s", out, *method, *path, *raw)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(*raw), &doc); err != nil || doc["from"] != "travel" || doc["to"] != "trips" {
		t.Fatalf("rename body %s", *raw)
	}
}

func TestSuiteToolSchemasCoverWritableFields(t *testing.T) {
	byName := map[string]map[string]any{}
	for _, tool := range ToolsFor([]string{"spend", "calendar", "notes", "people"}) {
		raw, err := json.Marshal(tool.Wire())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"required":null`) {
			t.Fatalf("%s has null required: %s", tool.Name, raw)
		}
		props, _ := tool.Parameters["properties"].(map[string]any)
		byName[tool.Name] = props
	}
	for _, key := range []string{"title", "amount", "amount_cents", "currency", "spent_on", "category", "notes"} {
		if byName[SpendCreate][key] == nil || byName[SpendUpdate][key] == nil {
			t.Fatalf("expense missing %s", key)
		}
	}
	for _, key := range []string{"interval", "active", "amount", "amount_cents"} {
		if byName[SpendSubscriptionCreate][key] == nil || byName[SpendSubscriptionUpdate][key] == nil {
			t.Fatalf("subscription missing %s", key)
		}
	}
	for _, key := range []string{"title", "due_day", "active", "notes"} {
		if byName[SpendPaymentDayCreate][key] == nil {
			t.Fatalf("payment day missing %s", key)
		}
	}
	for _, key := range []string{"title", "starts_on", "ends_on", "all_day", "starts_at", "ends_at", "body", "emoji", "repeat", "repeat_until"} {
		if byName[CalCreate][key] == nil || byName[CalUpdate][key] == nil {
			t.Fatalf("event missing %s", key)
		}
	}
	for _, key := range []string{"body", "title", "folder"} {
		if byName[NotesCreate][key] == nil || byName[NotesUpdate][key] == nil {
			t.Fatalf("note missing %s", key)
		}
	}
	if byName[CalRead] == nil || byName[SpendRead] == nil || byName[NotesFolders] == nil {
		t.Fatal("missing read or folder tools")
	}
	if !strings.Contains(toolDesc(SpendCreate), "category") || !strings.Contains(toolDesc(CalCreate), "repeat_until") || !strings.Contains(toolDesc(NotesCreate), "title") {
		t.Fatal("descriptions do not mention the structured fields")
	}
}

func toolDesc(name string) string {
	for _, tool := range ToolsFor([]string{"spend", "calendar", "notes"}) {
		if tool.Name == name {
			return tool.Description
		}
	}
	return ""
}

func TestNormalizeDatesAndClocks(t *testing.T) {
	cases := []struct{ in, want string }{
		{"2026-09-19", "2026-09-19"},
		{"19/09/2026", "2026-09-19"},
		{"09/19/2026", "2026-09-19"},
		{"2026/9/19", "2026-09-19"},
		{"19 September 2026", "2026-09-19"},
		{"19 de setembro de 2026", "2026-09-19"},
	}
	for _, tc := range cases {
		if got := normalizeDateOrRaw(tc.in); got != tc.want {
			t.Fatalf("%q -> %q, want %q", tc.in, got, tc.want)
		}
	}
	if got := normalizeDateOrRaw("31/02/2026"); got != "31/02/2026" {
		t.Fatalf("invalid date rewritten to %q", got)
	}
	clocks := []struct{ in, want string }{
		{"14:30", "14:30"},
		{"2:30 PM", "14:30"},
		{"2pm", "14:00"},
		{"14h30", "14:30"},
		{"15:00:00", "15:00"},
		{"12:05 AM", "00:05"},
	}
	for _, tc := range clocks {
		got, ok := normalizeClock(tc.in)
		if !ok || got != tc.want {
			t.Fatalf("%q -> %q ok %v", tc.in, got, ok)
		}
	}
	if got := normalizeMonth("September 2026"); got != "2026-09" {
		t.Fatalf("month %q", got)
	}
}
