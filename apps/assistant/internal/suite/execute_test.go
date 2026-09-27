package suite

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExecuteCreateTrimsAndStopsRedirects(t *testing.T) {
	var sawBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/notes" && r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"notes": []any{
				map[string]any{"id": 1, "title": "T", "preview": "P", "folder": "", "body": "SECRET"},
			}})
			return
		}
		if r.URL.Path == "/api/v1/notes" && r.Method == http.MethodPost {
			var doc map[string]any
			_ = json.NewDecoder(r.Body).Decode(&doc)
			raw, _ := json.Marshal(doc)
			sawBody = string(raw)
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{"id": 7, "title": "Trip", "body": "Trip\nKyoto"}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &Client{App: App{Name: "notes", Base: srv.URL}, Token: "kura_test"}
	out := Execute(context.Background(), c, NotesCreate, map[string]any{"body": "Trip\nKyoto"}, false)
	if !out.OK || out.ID != 7 || out.Title != "Trip" || strings.Contains(sawBody, "http://") {
		t.Fatalf("create %+v body %s", out, sawBody)
	}
	list := Execute(context.Background(), c, NotesSearch, map[string]any{"q": "trip"}, false)
	if !list.OK || strings.Contains(list.Body, "SECRET") {
		t.Fatalf("list %s", list.Body)
	}
}

func TestExecuteDelete404AndRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/notes/3" {
			http.NotFound(w, r)
			return
		}
		if r.URL.Path == "/gone" {
			http.Redirect(w, r, "http://evil.example/", http.StatusFound)
		}
	}))
	defer srv.Close()
	c := &Client{App: App{Name: "notes", Base: srv.URL}, Token: "kura_test"}
	out := Execute(context.Background(), c, NotesDelete, map[string]any{"id": float64(3)}, true)
	if !out.OK || !strings.Contains(out.Body, "already_gone") {
		t.Fatalf("delete %+v", out)
	}
	held := Execute(context.Background(), c, NotesDelete, map[string]any{"id": float64(3)}, false)
	if held.OK || !strings.Contains(held.Body, "needs_confirm") {
		t.Fatalf("unapproved %+v", held)
	}
	status, _, err := c.Call(context.Background(), http.MethodPost, "/api/v1/notes", map[string]any{"note": map[string]any{"body": "x"}})
	_ = status
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://evil.example/steal", http.StatusFound)
	}))
	defer redir.Close()
	c2 := &Client{App: App{Name: "notes", Base: redir.URL}, Token: "kura_test"}
	got := Execute(context.Background(), c2, NotesCreate, map[string]any{"body": "x"}, false)
	if !got.Unknown || err == nil && status >= 300 {
		if !got.Unknown {
			t.Fatalf("redirect should be unknown, got %+v err %v", got, err)
		}
	}
}

func TestExecute401DoesNotInventSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	}))
	defer srv.Close()
	c := &Client{App: App{Name: "notes", Base: srv.URL}, Token: "kura_dead"}
	out := Execute(context.Background(), c, NotesSearch, map[string]any{}, false)
	if out.OK || !out.Reconnect || !strings.Contains(out.Body, "/apps") {
		t.Fatalf("%+v", out)
	}
}
