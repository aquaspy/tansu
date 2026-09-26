package openrouter

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func audioClient(t *testing.T, mux *http.ServeMux) *Client {
	t.Helper()
	c, err := New("k", "model-x")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c.base = srv.URL
	return c
}

func TestTranscribe(t *testing.T) {
	var gotModel, gotLang, gotFile, gotProvider string
	mux := http.NewServeMux()
	mux.HandleFunc("/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Errorf("parse: %v", err)
		}
		gotModel = r.FormValue("model")
		gotLang = r.FormValue("language")
		gotProvider = r.FormValue("provider")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Errorf("file: %v", err)
		} else {
			gotFile = h.Filename
			f.Close()
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"text":"  ola mundo ","usage":{"seconds":1.5,"cost":0.00002}}`))
	})
	c := audioClient(t, mux)
	tr, err := c.Transcribe(context.Background(), []byte("RIFFfake"), "rec.webm", "pt")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Text != "ola mundo" || tr.Seconds != 1.5 || tr.CostUSD != 0.00002 {
		t.Fatalf("transcript = %+v", tr)
	}
	if gotModel != "model-x" || gotLang != "pt" || gotFile != "rec.webm" {
		t.Fatalf("multipart model=%q lang=%q file=%q", gotModel, gotLang, gotFile)
	}
	if gotProvider != `{"zdr":true}` {
		t.Fatalf("provider = %q", gotProvider)
	}
}

func TestTranscribeError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio/transcriptions", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	c := audioClient(t, mux)
	_, err := c.Transcribe(context.Background(), []byte("x"), "r.webm", "")
	if err == nil || !Retryable(err) {
		t.Fatalf("err = %v, want retryable 429", err)
	}
}

func TestSpeakAndGenerationCost(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		prov, _ := body["provider"].(map[string]any)
		if prov["zdr"] != true || body["voice"] != "pt-BR-FranciscaNeural" {
			t.Errorf("body = %v", body)
		}
		w.Header().Set("X-Generation-Id", "gen-1")
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Write([]byte("ID3fakeaudio"))
	})
	mux.HandleFunc("/generation", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("id") != "gen-1" {
			t.Errorf("id = %q", r.URL.Query().Get("id"))
		}
		w.Write([]byte(`{"data":{"total_cost":0.00033}}`))
	})
	c := audioClient(t, mux)
	body, gen, err := c.Speak(context.Background(), "Oi.", "pt-BR-FranciscaNeural", "mp3")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(body)
	body.Close()
	if string(raw) != "ID3fakeaudio" || gen != "gen-1" {
		t.Fatalf("speak = %q %q", raw, gen)
	}
	cost, err := c.GenerationCost(context.Background(), gen)
	if err != nil || cost != 0.00033 {
		t.Fatalf("cost = %v %v", cost, err)
	}
}

func TestSpeakVoiceError(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/audio/speech", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":{"message":"Unknown voice \"zz\"."}}`))
	})
	c := audioClient(t, mux)
	_, _, err := c.Speak(context.Background(), "Oi.", "zz", "mp3")
	if err == nil || !strings.Contains(err.Error(), "Unknown voice") || Retryable(err) {
		t.Fatalf("err = %v, want fatal voice error", err)
	}
}
