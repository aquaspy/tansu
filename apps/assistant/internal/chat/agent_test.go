package chat

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/suite"
)

type toolLLM struct {
	fakeLLM
	rounds   [][]map[string]any
	n        int
	tools    int
	failLeft int
	failErr  error
}

func (f *toolLLM) StreamChatTools(_ context.Context, _ []any, _ *int, _, _ string, _ *openrouter.SearchOptions, _ *openrouter.FileOptions, tools []openrouter.Tool, yield func(map[string]any) error) error {
	f.tools = len(tools)
	if f.failLeft > 0 {
		f.failLeft--
		return f.failErr
	}
	if f.n >= len(f.rounds) {
		return nil
	}
	events := f.rounds[f.n]
	f.n++
	for _, e := range events {
		if err := yield(e); err != nil {
			return err
		}
	}
	return nil
}

func toolDelta(id, name, args string) map[string]any {
	return map[string]any{"choices": []any{map[string]any{"delta": map[string]any{
		"tool_calls": []any{map[string]any{
			"index": float64(0), "id": id,
			"function": map[string]any{"name": name, "arguments": args},
		}},
	}}}}
}

func TestAgentCreateThenConfirmDelete(t *testing.T) {
	var posts, deletes int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/notes":
			posts++
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{"id": 4, "title": "Trip", "body": "Trip"}})
		case r.Method == http.MethodDelete:
			deletes++
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"notes":[]}`))
		}
	}))
	defer srv.Close()
	st := openStore(t)
	u := seedUser(t, st)
	key := make([]byte, 32)
	blob, err := suite.Encrypt(key, []byte("kura_live"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertAppLink(store.AppLink{UserID: u.ID, App: "notes", Token: blob, TokenPrefix: "live", Email: u.Email}); err != nil {
		t.Fatal(err)
	}
	raw, err := st.ListAppLinks(u.ID)
	if err != nil || strings.Contains(raw[0].Token, "kura_live") {
		t.Fatalf("token stored in the clear: %+v", raw)
	}
	conv, err := st.CreateConversation(u.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, asst, err := st.CreateTurn(conv.ID, "anota a viagem", false, false)
	if err != nil {
		t.Fatal(err)
	}
	fx := &toolLLM{rounds: [][]map[string]any{
		{toolDelta("c1", suite.NotesCreate, `{"body":"Trip"}`), usageChunk("m", map[string]any{"prompt_tokens": 10, "completion_tokens": 1, "cost": 0.01})},
		{chunk("Pronto."), usageChunk("m", map[string]any{"prompt_tokens": 4, "completion_tokens": 1, "cost": 0.02})},
	}}
	svc := testService(t, st, &fx.fakeLLM)
	svc.NewClient = func(string) (LLMClient, error) { return fx, nil }
	svc.SuiteKey = key
	svc.SuiteApps = []suite.App{{Name: "notes", Base: srv.URL}}
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete || posts != 1 || !strings.Contains(done.Content, "notes_create #4") {
		t.Fatalf("status %s posts %d content %q", done.Status, posts, done.Content)
	}
	if cost := asFloat(done.UsageMap()["cost_usd"]); cost < 0.029 || cost > 0.031 {
		t.Fatalf("cost %v", done.UsageMap()["cost_usd"])
	}

	_, asst2, err := st.CreateTurn(conv.ID, "apaga", false, false)
	if err != nil {
		t.Fatal(err)
	}
	fx.rounds = [][]map[string]any{
		{toolDelta("c2", suite.NotesDelete, `{"id":4}`)},
	}
	fx.n = 0
	svc.Run(asst2.ID, i18n.EN)
	pend, _ := st.GetMessage(asst2.ID)
	if pend.Status != store.StatusConfirming || deletes != 0 {
		t.Fatalf("confirm status %s deletes %d", pend.Status, deletes)
	}
	var doc struct {
		Calls []map[string]any `json:"calls"`
	}
	_ = json.Unmarshal([]byte(pend.ToolTrace), &doc)
	doc.Calls[0]["status"] = "approved"
	b, _ := json.Marshal(doc)
	_ = st.SetToolTrace(pend.ID, string(b))
	ok, err := st.ClaimConfirm(pend.ID)
	if err != nil || !ok {
		t.Fatal(err)
	}
	again, _ := st.ClaimConfirm(pend.ID)
	if again {
		t.Fatal("second claim")
	}
	fx.rounds = [][]map[string]any{{chunk("Apaguei."), usageChunk("m", map[string]any{"cost": 0.001})}}
	fx.n = 0
	svc.Run(pend.ID, i18n.EN)
	if deletes != 1 {
		t.Fatalf("deletes %d", deletes)
	}
	final, _ := st.GetMessage(pend.ID)
	if !strings.Contains(final.Content, "notes_delete") {
		t.Fatalf("final %q", final.Content)
	}
}

func TestVisibleContentHidesActionsWhenShared(t *testing.T) {
	body := "Pronto.\n\n[[actions]]\nnotes_create #4 Trip\n[[/actions]]"
	if got := visibleContent(body, false); strings.Contains(got, "[[") || !strings.Contains(got, "notes_create #4") || !strings.Contains(got, "Pronto.") {
		t.Fatalf("owner %q", got)
	}
	if got := visibleContent(body, true); strings.Contains(got, "notes_create") || strings.Contains(got, "#4") || got != "Pronto." {
		t.Fatalf("shared %q", got)
	}
	reads := "Oi.\n\n[[actions]]\n[[/actions]]"
	if strings.Contains(digest([]toolCall{{Name: "notes_search", Status: "done", RecID: 0}}), "notes_search") {
		t.Fatal("search leaked into the digest")
	}
	_ = reads
}

func TestAgentRetriesTransientBeforeAnyCall(t *testing.T) {
	var posts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"note": map[string]any{"id": 1, "title": "T", "body": "T"}})
	}))
	defer srv.Close()
	st := openStore(t)
	u := seedUser(t, st)
	key := make([]byte, 32)
	blob, _ := suite.Encrypt(key, []byte("kura_live"))
	_ = st.UpsertAppLink(store.AppLink{UserID: u.ID, App: "notes", Token: blob, TokenPrefix: "live", Email: u.Email})
	conv, _ := st.CreateConversation(u.ID)
	_, asst, _ := st.CreateTurn(conv.ID, "anota", false, false)
	fx := &toolLLM{
		failLeft: 1,
		failErr:  &openrouter.Error{Msg: "http_429", Code: 429},
		rounds: [][]map[string]any{
			{toolDelta("c1", suite.NotesCreate, `{"body":"T"}`)},
			{chunk("ok"), usageChunk("m", map[string]any{"cost": 0.001})},
		},
	}
	svc := testService(t, st, &fx.fakeLLM)
	svc.NewClient = func(string) (LLMClient, error) { return fx, nil }
	svc.retryDelays = []time.Duration{0}
	svc.SuiteKey = key
	svc.SuiteApps = []suite.App{{Name: "notes", Base: srv.URL}}
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete || posts != 1 {
		t.Fatalf("status %s posts %d err %s", done.Status, posts, done.Error)
	}
}

func TestCancelWritesVisibleLine(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, asst, _ := st.CreateTurn(conv.ID, "apaga", false, false)
	_ = st.SetAssistantStreaming(asst.ID)
	_, err := st.SetMessageStatus(asst.ID, store.StatusStreaming, store.StatusConfirming, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.CancelConfirming(conv.ID, "Action cancelled."); err != nil {
		t.Fatal(err)
	}
	got, _ := st.GetMessage(asst.ID)
	if got.Status != store.StatusComplete || !strings.Contains(got.Content, "Action cancelled.") {
		t.Fatalf("%s %q", got.Status, got.Content)
	}
}
