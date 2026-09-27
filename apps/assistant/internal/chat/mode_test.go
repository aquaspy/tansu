package chat

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/suite"
)

func setMode(t *testing.T, st *store.Store, id int64, mode string) {
	t.Helper()
	if _, err := st.DB().Exec(`UPDATE conversations SET mode = ? WHERE id = ?`, mode, id); err != nil {
		t.Fatal(err)
	}
}

func TestHiddenControlsIgnoreStickyModel(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_ = st.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Model: "x-ai/grok-4.7", Effort: "low"})
	conv, _ = st.GetConversation(conv.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: []map[string]any{chunk("Yo")}, title: "T"}
	svc := testService(t, st, fx)
	svc.Config.ShowModelControls = false
	svc.Config.Models = []string{"openai/gpt-6-luna", "x-ai/grok-4.7"}
	svc.Run(asst.ID, i18n.EN)
	if len(fx.models) == 0 || fx.models[0] != "openai/gpt-6-luna" {
		t.Fatalf("models = %v", fx.models)
	}
	if len(fx.streams) == 0 || fx.streams[0].effort != "high" {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestChatModeDoesNotCallTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"notes":[]}`))
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
	conv, _ := st.CreateConversation(u.ID)
	setMode(t, st, conv.ID, store.ModeChat)
	_, asst, err := st.CreateTurn(conv.ID, "anota a viagem", false, false)
	if err != nil {
		t.Fatal(err)
	}
	fx := &toolLLM{}
	fx.events = []map[string]any{chunk("Só conversa.")}
	svc := testService(t, st, &fx.fakeLLM)
	svc.NewClient = func(string) (LLMClient, error) { return fx, nil }
	svc.SuiteKey = key
	svc.SuiteApps = []suite.App{{Name: "notes", Base: srv.URL}}
	svc.Run(asst.ID, i18n.PT)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete || fx.tools != 0 || !strings.Contains(done.Content, "Só conversa.") {
		t.Fatalf("status %s tools %d content %q", done.Status, fx.tools, done.Content)
	}
	if strings.Contains(done.Content, "notes_create") {
		t.Fatalf("chat mode wrote a tool trace: %q", done.Content)
	}
}

func TestAssistantSkipsUnhealthyApp(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	st := openStore(t)
	u := seedUser(t, st)
	key := make([]byte, 32)
	blob, _ := suite.Encrypt(key, []byte("kura_live"))
	if err := st.UpsertAppLink(store.AppLink{UserID: u.ID, App: "spend", Token: blob, TokenPrefix: "live", Email: u.Email}); err != nil {
		t.Fatal(err)
	}
	conv, _ := st.CreateConversation(u.ID)
	_, asst, _ := st.CreateTurn(conv.ID, "registra o almoço", false, false)
	cap := &captureLLM{}
	cap.events = []map[string]any{chunk("O Spend não respondeu.")}
	svc := testService(t, st, &cap.fakeLLM)
	svc.NewClient = func(string) (LLMClient, error) { return cap, nil }
	svc.SuiteKey = key
	svc.SuiteApps = []suite.App{{Name: "spend", Base: srv.URL}}
	svc.Run(asst.ID, i18n.PT)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete {
		t.Fatalf("status %s err %q", done.Status, done.Error)
	}
	joined := strings.Join(cap.saw, "\n")
	if !strings.Contains(joined, "indisponível") || !strings.Contains(joined, "unauthorized") {
		t.Fatalf("prompt missing connection status:\n%s", joined)
	}
	if strings.Contains(joined, "spend_create") {
		t.Fatal("unhealthy app was offered as a tool")
	}
}

type captureLLM struct {
	fakeLLM
	saw []string
}

func (c *captureLLM) StreamChat(ctx context.Context, input []any, max *int, effort, session string, search *openrouter.SearchOptions, files *openrouter.FileOptions, yield func(map[string]any) error) error {
	for _, item := range input {
		m, _ := item.(map[string]any)
		if s, ok := m["content"].(string); ok {
			c.saw = append(c.saw, s)
		}
	}
	return c.fakeLLM.StreamChat(ctx, input, max, effort, session, search, files, yield)
}
