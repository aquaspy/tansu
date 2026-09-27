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

// peopleCreateServer is the People create contract the assistant calls:
// Bearer auth, {"person":{name, birthday:{month,day,year}}}, 201 or 422.
func peopleCreateServer(t *testing.T, token string) (*httptest.Server, *int, *string) {
	t.Helper()
	var posts int
	var rawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/people" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		posts++
		if r.Header.Get("Authorization") != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		body, _ := io.ReadAll(r.Body)
		rawBody = string(body)
		var doc map[string]any
		_ = json.Unmarshal(body, &doc)
		person, _ := doc["person"].(map[string]any)
		name, _ := person["name"].(string)
		if strings.TrimSpace(name) == "" {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"errors":["Name can't be blank"]}`))
			return
		}
		if bday, ok := person["birthday"].(map[string]any); ok {
			month, _ := bday["month"].(float64)
			day, _ := bday["day"].(float64)
			if month == 4 && day == 31 {
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"errors":["Birthday isn't a real day in that month"]}`))
				return
			}
			if _, hasYear := bday["year"]; hasYear {
				if _, hasMonth := bday["month"]; !hasMonth {
					w.WriteHeader(http.StatusUnprocessableEntity)
					_, _ = w.Write([]byte(`{"errors":["A birth year needs a month and day"]}`))
					return
				}
			}
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"person": map[string]any{
			"id": 7, "name": name,
		}})
	}))
	return srv, &posts, &rawBody
}

func TestPeopleCreateSucceedsFromModelArgs(t *testing.T) {
	const token = "kura_people"
	srv, posts, rawBody := peopleCreateServer(t, token)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: token}

	cases := []map[string]any{
		{"name": "Ada Lovelace", "birthday_month": "8", "birthday_day": json.Number("11"), "birthday_year": float64(1990)},
		{"person": map[string]any{"name": "Ada Lovelace", "birthday": map[string]any{"month": "8", "day": "11", "year": "1990"}}},
		{"name": "Ada Lovelace", "birthday": "1990-08-11"},
		{"name": "Ada Lovelace", "birthday": "August 11, 1990"},
		{"name": "Ada Lovelace", "birthday": "11 de agosto de 1990"},
	}
	for i, args := range cases {
		*posts = 0
		out := Execute(context.Background(), c, PeopleCreate, args, false)
		if !out.OK || out.ID != 7 || out.Title != "Ada Lovelace" || *posts != 1 {
			t.Fatalf("case %d out=%+v posts=%d body=%s", i, out, *posts, *rawBody)
		}
		if !strings.Contains(out.Body, `"ok":true`) {
			t.Fatalf("case %d result missing ok: %s", i, out.Body)
		}
		if !strings.Contains(*rawBody, `"month":8`) || !strings.Contains(*rawBody, `"day":11`) || !strings.Contains(*rawBody, `"year":1990`) {
			t.Fatalf("case %d payload %s", i, *rawBody)
		}
	}
}

func TestPeopleCreateFailureIsOneError(t *testing.T) {
	const token = "kura_people"
	srv, posts, rawBody := peopleCreateServer(t, token)
	defer srv.Close()
	c := &Client{App: App{Name: "people", Base: srv.URL}, Token: token}

	out := Execute(context.Background(), c, PeopleCreate, map[string]any{
		"name": "Bad", "birthday_month": "4", "birthday_day": "31",
	}, false)
	if out.OK || out.Reconnect || *posts != 1 {
		t.Fatalf("bad birthday %+v posts %d", out, *posts)
	}
	if !strings.Contains(out.Body, `"ok":false`) || !strings.Contains(out.Body, "Birthday isn't a real day") {
		t.Fatalf("error body %s", out.Body)
	}
	if !strings.Contains(*rawBody, `"month":4`) || !strings.Contains(*rawBody, `"day":31`) {
		t.Fatalf("payload dropped the birthday: %s", *rawBody)
	}

	blank := Execute(context.Background(), c, PeopleCreate, map[string]any{
		"person": map[string]any{"nickname": "no name"},
	}, false)
	if blank.OK || *posts != 2 || !strings.Contains(blank.Body, "Name can't be blank") {
		t.Fatalf("blank %+v posts %d", blank, *posts)
	}

	denied := Execute(context.Background(), &Client{App: App{Name: "people", Base: srv.URL}, Token: "kura_dead"}, PeopleCreate, map[string]any{"name": "Ada"}, false)
	if denied.OK || !denied.Reconnect || !strings.Contains(denied.Body, `"ok":false`) || *posts != 3 {
		t.Fatalf("auth %+v posts %d", denied, *posts)
	}
}

func TestPeopleToolSchemaOmitsNullRequired(t *testing.T) {
	for _, tool := range ToolsFor([]string{"people"}) {
		raw, err := json.Marshal(tool.Wire())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), `"required":null`) {
			t.Fatalf("%s has null required: %s", tool.Name, raw)
		}
	}
}
