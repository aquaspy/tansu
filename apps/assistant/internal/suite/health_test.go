package suite

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProbeAuthAndOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/folders" && r.Header.Get("Authorization") == "Bearer good" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"folders":[]}`))
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	ok := Probe(context.Background(), Client{App: App{Name: "notes", Base: srv.URL}, Token: "good"})
	if ok.State != HealthOK || ok.Detail != "" {
		t.Fatalf("ok = %+v", ok)
	}
	bad := Probe(context.Background(), Client{App: App{Name: "notes", Base: srv.URL}, Token: "nope"})
	if bad.State != HealthAuth || bad.Detail != "unauthorized" {
		t.Fatalf("auth = %+v", bad)
	}
	down := Probe(context.Background(), Client{App: App{Name: "notes", Base: "http://127.0.0.1:1"}, Token: "good"})
	if down.State != HealthDown {
		t.Fatalf("down = %+v", down)
	}
}
