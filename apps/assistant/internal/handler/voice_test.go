package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
)

type stubVoice struct {
	text    string
	seconds float64
	cost    float64
	audio   []byte
	genID   string
	genCost float64

	gotModel string
	gotVoice string
	gotText  string
	gotFile  string
}

func (s *stubVoice) Transcribe(_ context.Context, _ []byte, filename, _ string) (openrouter.Transcript, error) {
	s.gotFile = filename
	return openrouter.Transcript{Text: s.text, Seconds: s.seconds, CostUSD: s.cost}, nil
}

func (s *stubVoice) Speak(_ context.Context, text, voice, _ string) (io.ReadCloser, string, error) {
	s.gotText, s.gotVoice = text, voice
	return io.NopCloser(bytes.NewReader(s.audio)), s.genID, nil
}

func (s *stubVoice) GenerationCost(_ context.Context, _ string) (float64, error) {
	return s.genCost, nil
}

func voiceFlow(t *testing.T, sv *stubVoice) (*flow, string) {
	t.Helper()
	f := newFlow(t, nil)
	f.srv.NewVoiceClient = func(model string) (VoiceClient, error) {
		sv.gotModel = model
		return sv, nil
	}
	f.seedUser("v@x.com", "secret-ok")
	f.login("v@x.com", "secret-ok")
	code, _, h := f.post("/conversations/", nil, nil)
	if code != 303 {
		t.Fatalf("create = %d", code)
	}
	return f, strings.TrimPrefix(h.Get("Location"), "/conversations/")
}

// postAudio uploads one recording. CreateFormFile always marks parts
// application/octet-stream, so the handler accepts by extension here.
func postAudio(t *testing.T, f *flow, convID, field, filename string, raw []byte) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("csrf_token", f.csrf())
	w, err := mw.CreateFormFile(field, filename)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(raw)
	_ = mw.Close()
	req, _ := http.NewRequest(http.MethodPost, f.server.URL+"/conversations/"+convID+"/voice/transcribe", &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := f.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
}

func TestVoiceTranscribe(t *testing.T) {
	sv := &stubVoice{text: "ola mundo", seconds: 1.5, cost: 0.00002}
	f, convID := voiceFlow(t, sv)
	code, body := postAudio(t, f, convID, "audio", "rec.webm", []byte("fakeopus"))
	if code != 200 {
		t.Fatalf("status = %d (%s)", code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out["text"] != "ola mundo" || out["seconds"] != 1.5 || out["cost_usd"] != 0.00002 {
		t.Fatalf("json = %s", body)
	}
	if sv.gotFile != "rec.webm" {
		t.Fatalf("file = %q", sv.gotFile)
	}
}

func TestVoiceTranscribeRejectsNonAudio(t *testing.T) {
	sv := &stubVoice{}
	f, convID := voiceFlow(t, sv)
	code, body := postAudio(t, f, convID, "audio", "evil.exe", []byte("MZ"))
	if code != 400 || !strings.Contains(body, "voice.bad_audio") {
		t.Fatalf("status = %d (%s)", code, body)
	}
}

func TestVoiceForText(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		locale i18n.Locale
		want   string
	}{
		{"pt long", "O relatório trimestral registra uma receita de quarenta e dois milhões de reais.", i18n.EN, "pt-BR-FranciscaNeural"},
		{"en long", "The quarterly report shows revenue of forty-two million dollars this year.", i18n.PT, "en-US-AvaNeural"},
		{"pt short falls to locale", "Oi, tudo bem?", i18n.PT, "pt-BR-FranciscaNeural"},
		{"en short falls to locale", "Hi there friend", i18n.EN, "en-US-AvaNeural"},
		{"other language falls to default", "Este informe trimestral muestra ingresos millonarios este año fiscal.", i18n.PT, "pt-BR-FranciscaNeural"},
	}
	for _, tc := range cases {
		if got := voiceForText(tc.text, tc.locale, "pt-BR-FranciscaNeural", "en-US-AvaNeural"); got != tc.want {
			t.Errorf("%s = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestVoiceTranscribeRequiresAuth(t *testing.T) {
	f := newFlow(t, nil)
	code, _, _ := f.post("/conversations/1/voice/transcribe", nil, nil)
	if code != 303 {
		t.Fatalf("status = %d, want login redirect", code)
	}
}

func TestVoiceSpeak(t *testing.T) {
	sv := &stubVoice{audio: []byte("ID3fake"), genID: "gen-1", genCost: 0.00033}
	f, convID := voiceFlow(t, sv)
	u, _ := f.store.FindUserByEmail("v@x.com")
	cid, _ := strconv.ParseInt(convID, 10, 64)
	if _, err := f.store.CreateUserMessage(cid, "Hi", false, false); err != nil {
		t.Fatal(err)
	}
	asst, err := f.store.CreateAssistantMessage(cid)
	if err != nil {
		t.Fatal(err)
	}
	_ = u
	code, body, hdr := f.post("/conversations/"+convID+"/voice/speak",
		url.Values{"text": {"Ola mundo"}, "message_id": {strconv.FormatInt(asst.ID, 10)}}, nil)
	if code != 200 {
		t.Fatalf("status = %d (%s)", code, body)
	}
	if ct := hdr.Get("Content-Type"); ct != "audio/mpeg" {
		t.Fatalf("content-type = %q", ct)
	}
	if body != "ID3fake" {
		t.Fatalf("audio = %q", body)
	}
	if sv.gotText != "Ola mundo" {
		t.Fatalf("text = %q", sv.gotText)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		m, _ := f.store.GetMessage(asst.ID)
		usage := m.UsageMap()
		if usage["tts_cost_usd"] == 0.00033 && usage["tts_chars"] == float64(len("Ola mundo")) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("usage = %v", usage)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestVoicePolish(t *testing.T) {
	sv := &stubVoice{}
	f, convID := voiceFlow(t, sv)
	code, body, _ := f.post("/conversations/"+convID+"/voice/polish",
		url.Values{"text": {"oi tudo bem"}}, nil)
	if code != 200 {
		t.Fatalf("status = %d (%s)", code, body)
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		t.Fatal(err)
	}
	if out["text"] != "Stub title" || out["cost_usd"] != 0.0 {
		t.Fatalf("json = %s", body)
	}
}

func TestVoiceSettingsPersist(t *testing.T) {
	f := newFlow(t, nil)
	u := f.seedUser("you@x.com", "secret-ok")
	conv, _ := f.store.CreateConversation(u.ID)
	f.login("you@x.com", "secret-ok")
	if conv.VoiceReadAloud || !conv.VoiceAutoSend {
		t.Fatalf("defaults = %+v", conv)
	}
	hx := map[string]string{"HX-Request": "true"}
	flip := func(form url.Values) {
		t.Helper()
		if code, _, _ := f.methodCall(http.MethodPatch, "/conversations/1/settings", form, hx); code != 200 {
			t.Fatalf("settings = %d", code)
		}
	}
	// Checked box submits ["1","0"] (hidden fallback last); FormValue takes "1".
	flip(url.Values{"voice_read_aloud": {"1", "0"}})
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if !conv.VoiceReadAloud {
		t.Fatal("read-aloud did not stick")
	}
	// Unchecked submits ["0"] only.
	flip(url.Values{"voice_read_aloud": {"0"}})
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.VoiceReadAloud {
		t.Fatal("read-aloud did not clear")
	}
	// Auto-send flips the same way, defaulting on.
	flip(url.Values{"voice_auto_send": {"0"}})
	conv, _ = f.store.FindConversation(u.ID, conv.ID)
	if conv.VoiceAutoSend {
		t.Fatal("auto-send did not clear")
	}
}

func TestVoicePolishRejectsBlank(t *testing.T) {
	sv := &stubVoice{}
	f, convID := voiceFlow(t, sv)
	code, body, _ := f.post("/conversations/"+convID+"/voice/polish",
		url.Values{"text": {"  "}}, nil)
	if code != 400 || !strings.Contains(body, "voice.bad_text") {
		t.Fatalf("status = %d (%s)", code, body)
	}
}

func TestVoiceSpeakRejectsForeignMessage(t *testing.T) {
	sv := &stubVoice{audio: []byte("x")}
	f, convID := voiceFlow(t, sv)
	u, _ := f.store.FindUserByEmail("v@x.com")
	other, err := f.store.CreateConversation(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	asst, err := f.store.CreateAssistantMessage(other.ID)
	if err != nil {
		t.Fatal(err)
	}
	code, _, _ := f.post("/conversations/"+convID+"/voice/speak",
		url.Values{"text": {"Hi"}, "message_id": {strconv.FormatInt(asst.ID, 10)}}, nil)
	if code != 404 {
		t.Fatalf("status = %d, want 404", code)
	}
}

func TestVoiceMessageCarriesSTTCost(t *testing.T) {
	sv := &stubVoice{}
	f, convID := voiceFlow(t, sv)
	code, _, _ := f.post("/conversations/"+convID+"/messages",
		url.Values{"content": {"ditado"}, "stt_cost_usd": {"0.0001"}, "stt_seconds": {"2.5"}},
		map[string]string{"HX-Request": "true"})
	if code != 200 {
		t.Fatalf("status = %d", code)
	}
	cid, _ := strconv.ParseInt(convID, 10, 64)
	m := f.pollStatus(cid, "complete")
	var usage map[string]any
	if err := json.Unmarshal([]byte(m.TokenUsage), &usage); err != nil {
		t.Fatal(err)
	}
	if usage["stt_cost_usd"] != 0.0001 || usage["stt_seconds"] != 2.5 {
		t.Fatalf("usage = %s", m.TokenUsage)
	}
}
