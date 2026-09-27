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

// peopleWriteServer records the last write and echoes a saved person.
func peopleWriteServer(t *testing.T) (*httptest.Server, *string, *string, *string, *int) {
	t.Helper()
	var method, path, raw string
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		method = r.Method
		path = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		raw = string(body)
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		person, _ := doc["person"].(map[string]any)
		name, _ := person["name"].(string)
		if name == "" {
			name = "Ada Lovelace"
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]any{"person": map[string]any{
			"id": 7, "name": name,
		}})
	}))
	return srv, &method, &path, &raw, &hits
}

func personPayload(t *testing.T, raw string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("body %s: %v", raw, err)
	}
	person, _ := doc["person"].(map[string]any)
	if person == nil {
		t.Fatalf("no person in %s", raw)
	}
	return person
}

func TestPeopleCreateForwardsStructuredFields(t *testing.T) {
	srv, method, path, raw, hits := peopleWriteServer(t)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: "kura_people"}

	out := Execute(context.Background(), c, PeopleCreate, map[string]any{
		"name": "Ada Lovelace", "nickname": "Ada", "relationship": "friend",
		"emoji": "🎂", "phone": "+55 11 99999", "email": "ada@example.com",
		"address": "Rua 1, São Paulo", "height": "1.65m",
		"favorites": "mechanical keyboards", "notes": "met at a talk",
		"birthday": "1990-08-11",
		"sizes":    map[string]any{"ring": "16", "shoe": float64(37), "shirt": "M", "pants": "40"},
		"attrs": []any{
			map[string]any{"label": "CPF", "value": "123.456.789-00"},
			map[string]any{"label": "Pix", "value": "ada@pix"},
			map[string]any{"label": "Email", "value": "ada.extra@example.com"},
		},
	}, false)
	if !out.OK || *hits != 1 || *method != http.MethodPost || *path != "/api/v1/people" {
		t.Fatalf("out=%+v hits=%d %s %s body=%s", out, *hits, *method, *path, *raw)
	}
	person := personPayload(t, *raw)
	for key, want := range map[string]string{
		"name": "Ada Lovelace", "nickname": "Ada", "relationship": "friend",
		"emoji": "🎂", "phone": "+55 11 99999", "email": "ada@example.com",
		"address": "Rua 1, São Paulo", "height": "1.65m",
		"favorites": "mechanical keyboards", "notes": "met at a talk",
	} {
		if person[key] != want {
			t.Fatalf("%s = %#v in %s", key, person[key], *raw)
		}
	}
	bday, _ := person["birthday"].(map[string]any)
	if bday["month"] != float64(8) || bday["day"] != float64(11) || bday["year"] != float64(1990) {
		t.Fatalf("birthday %#v", bday)
	}
	sizes, _ := person["sizes"].(map[string]any)
	if sizes["ring"] != "16" || sizes["shoe"] != "37" || sizes["shirt"] != "M" || sizes["pants"] != "40" {
		t.Fatalf("sizes %#v", sizes)
	}
	attrs, _ := person["attrs"].([]any)
	if len(attrs) != 3 {
		t.Fatalf("attrs %#v", person["attrs"])
	}
	pix, _ := attrs[1].(map[string]any)
	if pix["label"] != "Pix" || pix["value"] != "ada@pix" {
		t.Fatalf("pix %#v", pix)
	}
}

func TestPeopleWriteNestedAndPartialFields(t *testing.T) {
	srv, method, path, raw, hits := peopleWriteServer(t)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: "kura_people"}

	out := Execute(context.Background(), c, PeopleCreate, map[string]any{
		"person": map[string]any{
			"name": "Ada Lovelace", "address": "Rua 2", "height": float64(165),
			"sizes":    map[string]any{"shoe": "38", "hat": "nope"},
			"attrs":    []any{map[string]any{"label": "CPF", "value": "1", "extra": "x"}},
			"unknown":  map[string]any{"nested": true},
			"birthday": map[string]any{"month": "8", "day": "11", "year": "1990"},
		},
		"favorites": "tea",
	}, false)
	if !out.OK || *hits != 1 {
		t.Fatalf("nested %+v hits %d body %s", out, *hits, *raw)
	}
	person := personPayload(t, *raw)
	if person["address"] != "Rua 2" || person["favorites"] != "tea" || person["height"] != float64(165) {
		t.Fatalf("forwarded %#v", person)
	}
	if _, ok := person["unknown"]; ok {
		t.Fatalf("unknown key leaked: %s", *raw)
	}
	sizes, _ := person["sizes"].(map[string]any)
	if len(sizes) != 1 || sizes["shoe"] != "38" {
		t.Fatalf("sizes %#v", sizes)
	}
	attrs, _ := person["attrs"].([]any)
	row, _ := attrs[0].(map[string]any)
	if len(attrs) != 1 || row["label"] != "CPF" || row["extra"] != nil {
		t.Fatalf("attrs %#v", attrs)
	}
	bday, _ := person["birthday"].(map[string]any)
	if bday["month"] != float64(8) || bday["year"] != float64(1990) {
		t.Fatalf("birthday %#v", bday)
	}

	*hits = 0
	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7), "shoe_size": "39", "address": "Rua 3",
	}, false)
	if !out.OK || *hits != 1 || *method != http.MethodPatch || *path != "/api/v1/people/7" {
		t.Fatalf("update %+v %s %s body %s", out, *method, *path, *raw)
	}
	person = personPayload(t, *raw)
	sizes, _ = person["sizes"].(map[string]any)
	if person["address"] != "Rua 3" || sizes["shoe"] != "39" || len(sizes) != 1 {
		t.Fatalf("partial sizes %#v", person)
	}
	for _, key := range []string{"name", "notes", "attrs", "favorites", "birthday", "height"} {
		if _, ok := person[key]; ok {
			t.Fatalf("update sent unchanged %s: %s", key, *raw)
		}
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7),
		"attrs": []any{
			map[string]any{"label": "Pix", "value": "new"},
			map[string]any{"label": "  ", "value": "skip"},
		},
	}, false)
	person = personPayload(t, *raw)
	if _, ok := person["address"]; ok {
		t.Fatalf("attrs update included address: %s", *raw)
	}
	attrs, _ = person["attrs"].([]any)
	row, _ = attrs[0].(map[string]any)
	if len(attrs) != 1 || row["label"] != "Pix" || row["value"] != "new" {
		t.Fatalf("attrs replace %#v", person["attrs"])
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id":    float64(7),
		"attrs": []any{map[string]any{"label": "  ", "value": "no label"}},
	}, false)
	person = personPayload(t, *raw)
	if _, ok := person["attrs"]; ok {
		t.Fatalf("blank labels must not replace attrs: %s", *raw)
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": json.Number("7"), "attrs": []any{},
	}, false)
	person = personPayload(t, *raw)
	attrs, ok := person["attrs"].([]any)
	if !ok || len(attrs) != 0 {
		t.Fatalf("empty attrs should be sent to clear: %#v", person)
	}
	if !out.OK {
		t.Fatalf("clear %+v", out)
	}
}

func TestPeopleUpdateClearsBlankNotes(t *testing.T) {
	srv, method, path, raw, hits := peopleWriteServer(t)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: "kura_people"}

	// A lone dash is stored text. Clearing it means sending an empty notes
	// string, which must appear on the PATCH so the API overwrites "-".
	out := Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7), "notes": "",
	}, false)
	if !out.OK || *hits != 1 || *method != http.MethodPatch || *path != "/api/v1/people/7" {
		t.Fatalf("clear %+v %s %s body %s", out, *method, *path, *raw)
	}
	person := personPayload(t, *raw)
	notes, ok := person["notes"].(string)
	if !ok || notes != "" {
		t.Fatalf("empty notes dropped: %#v body %s", person["notes"], *raw)
	}
	for _, key := range []string{"name", "favorites", "address", "attrs", "sizes", "birthday"} {
		if _, present := person[key]; present {
			t.Fatalf("clear notes also sent %s: %s", key, *raw)
		}
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7), "notes": "  \n\t", "favorites": "",
	}, false)
	person = personPayload(t, *raw)
	if person["notes"] != "" || person["favorites"] != "" {
		t.Fatalf("whitespace should clear: %#v", person)
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"person": map[string]any{"notes": ""}, "id": float64(7),
	}, false)
	person = personPayload(t, *raw)
	if person["notes"] != "" {
		t.Fatalf("nested empty notes dropped: %#v", person)
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7), "notes": "-",
	}, false)
	person = personPayload(t, *raw)
	if person["notes"] != "-" {
		t.Fatalf("a dash is text, got %#v", person["notes"])
	}

	out = Execute(context.Background(), c, PeopleUpdate, map[string]any{
		"id": float64(7), "shoe_size": "", "nickname": "  ",
	}, false)
	person = personPayload(t, *raw)
	sizes, _ := person["sizes"].(map[string]any)
	if person["nickname"] != "" || sizes["shoe"] != "" || len(sizes) != 1 {
		t.Fatalf("blank size/nickname %#v", person)
	}
	if !out.OK {
		t.Fatalf("last clear %+v", out)
	}
}

func TestPeopleDeleteRequiresConfirm(t *testing.T) {
	srv, method, path, _, hits := peopleWriteServer(t)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: "kura_people"}

	if !IsDelete(PeopleDelete) {
		t.Fatal("people_delete is not a confirm tool")
	}
	held := Execute(context.Background(), c, PeopleDelete, map[string]any{"id": float64(7)}, false)
	if held.OK || *hits != 0 || !strings.Contains(held.Body, "needs_confirm") {
		t.Fatalf("unapproved %+v hits %d", held, *hits)
	}
	out := Execute(context.Background(), c, PeopleDelete, map[string]any{"id": float64(7)}, true)
	if !out.OK || *hits != 1 || *method != http.MethodDelete || *path != "/api/v1/people/7" {
		t.Fatalf("approved %+v %s %s hits %d", out, *method, *path, *hits)
	}
	if !strings.Contains(out.Body, `"ok":true`) {
		t.Fatalf("delete result %s", out.Body)
	}
}

func TestPeopleToolSchemaCoversWritableFields(t *testing.T) {
	byName := map[string]struct {
		desc  string
		props map[string]any
	}{}
	for _, tool := range ToolsFor([]string{"people"}) {
		props, _ := tool.Parameters["properties"].(map[string]any)
		byName[tool.Name] = struct {
			desc  string
			props map[string]any
		}{desc: tool.Description, props: props}
	}
	create, okCreate := byName[PeopleCreate]
	update, okUpdate := byName[PeopleUpdate]
	if !okCreate || !okUpdate {
		t.Fatal("missing create or update tool")
	}
	want := []string{
		"name", "nickname", "relationship", "emoji", "phone", "email", "address", "height",
		"birthday", "birthday_month", "birthday_day", "birthday_year",
		"sizes", "ring_size", "shoe_size", "shirt_size", "pants_size",
		"favorites", "notes", "attrs",
	}
	for _, key := range want {
		if create.props[key] == nil {
			t.Fatalf("create missing %s", key)
		}
		if update.props[key] == nil {
			t.Fatalf("update missing %s", key)
		}
	}
	if update.props["id"] == nil {
		t.Fatal("update missing id")
	}
	desc := strings.ToLower(update.desc)
	if !strings.Contains(desc, "only fields that change") || !strings.Contains(desc, "attrs") || !strings.Contains(desc, "omit attrs") || !strings.Contains(desc, "empty string") {
		t.Fatalf("update description: %s", update.desc)
	}
	if !strings.Contains(strings.ToLower(create.desc), "attrs") || !strings.Contains(strings.ToLower(create.desc), "notes") {
		t.Fatalf("create description: %s", create.desc)
	}
	sizes, _ := create.props["sizes"].(map[string]any)
	sizeProps, _ := sizes["properties"].(map[string]any)
	if sizeProps["ring"] == nil || sizeProps["shoe"] == nil || sizeProps["shirt"] == nil || sizeProps["pants"] == nil {
		t.Fatalf("sizes schema %#v", sizes)
	}
}
