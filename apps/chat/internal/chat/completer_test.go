package chat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
)

type streamCall struct {
	effort  string
	max     *int
	session string
	nInput  int
	search  *openrouter.SearchOptions
	files   *openrouter.FileOptions
	sys0    string
	tail    string
}

type fakeLLM struct {
	events      []map[string]any
	title       string
	completeErr error
	// failFirst/failEvents script transient failures: the first failFirst
	// streams yield failEvents instead of events.
	failFirst  int
	failEvents []map[string]any
	streams    []streamCall
	completes  int
	models     []string
}

func (f *fakeLLM) StreamChat(_ context.Context, input []any, max *int, effort, session string, search *openrouter.SearchOptions, files *openrouter.FileOptions, yield func(map[string]any) error) error {
	sys0, tail := "", ""
	if len(input) > 0 {
		if m, _ := input[0].(map[string]any); m != nil {
			sys0, _ = m["content"].(string)
		}
		if m, _ := input[len(input)-1].(map[string]any); m != nil {
			tail, _ = m["content"].(string)
		}
	}
	f.streams = append(f.streams, streamCall{effort, max, session, len(input), search, files, sys0, tail})
	events := f.events
	if f.failFirst > 0 {
		f.failFirst--
		events = f.failEvents
	}
	for _, e := range events {
		if err := yield(e); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeLLM) Complete(_ context.Context, _ []any, _ *int, _ string) (map[string]any, error) {
	f.completes++
	if f.completeErr != nil {
		return nil, f.completeErr
	}
	return map[string]any{"choices": []any{map[string]any{"message": map[string]any{
		"role": "assistant", "content": f.title}}}}, nil
}

func testService(t *testing.T, st *store.Store, fx *fakeLLM) *Service {
	t.Helper()
	return &Service{
		Store:   st,
		Hub:     NewHub(),
		DataDir: t.TempDir(),
		Config: CompleterConfig{
			Model: "openai/gpt-6-luna", Effort: "high",
			WindowTokens: 150000, KeepRecentTokens: 32000,
		},
		APIKey: "x",
		NewClient: func(model string) (LLMClient, error) {
			fx.models = append(fx.models, model)
			return fx, nil
		},
	}
}

func openStore(t *testing.T) *store.Store {
	t.Helper()
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

func seedUser(t *testing.T, st *store.Store) *store.User {
	t.Helper()
	u, err := st.CreateUser("you@x.com", "digest")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func chunk(content string) map[string]any {
	return map[string]any{"choices": []any{map[string]any{
		"delta": map[string]any{"content": content}}}}
}

func usageChunk(model string, usage map[string]any) map[string]any {
	return map[string]any{"model": model, "choices": []any{map[string]any{
		"delta": map[string]any{}, "finish_reason": "stop"}}, "usage": usage}
}

func textEvents(parts ...string) []map[string]any {
	var out []map[string]any
	for _, p := range parts {
		out = append(out, chunk(p))
	}
	out = append(out, usageChunk("openai/gpt-6-luna", map[string]any{
		"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.0000036,
	}))
	return out
}

func TestPlainTurn(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hello there friend", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Hi", "!"), title: "Short title"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete || done.Content != "Hi!" {
		t.Fatalf("row = %+v", done)
	}
	conv, _ = st.GetConversation(conv.ID)
	if conv.Title != "Short title" {
		t.Fatalf("title = %q", conv.Title)
	}
	if len(fx.streams) != 1 || fx.streams[0].effort != "high" {
		t.Fatalf("stream = %+v", fx.streams)
	}
	if fx.streams[0].max != nil || fx.streams[0].session != "kura-1" {
		t.Fatalf("stream = %+v", fx.streams)
	}
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["cost_usd"] != 0.0000036 || usage["model"] != "openai/gpt-6-luna" {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
}

func TestTitleFallback(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hello there friend", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Hi"), completeErr: &openrouter.Error{Msg: "nope"}}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	conv, _ = st.GetConversation(conv.ID)
	if conv.Title != "Hello there friend" {
		t.Fatalf("title = %q", conv.Title)
	}
}

// Model output is stored verbatim: no citation stripping, no glue fixes.
func TestRawTextPreserved(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	raw := "See [[1]](https://x.test) Mr.Smith, de15 anos, GPT4."
	fx := &fakeLLM{events: textEvents(raw), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Content != raw {
		t.Fatalf("content = %q", done.Content)
	}
}

func TestClosingLoopAbort(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	loop := "Analysis done.\n\n" + strings.Repeat("Fim.\n", 8) + strings.Repeat("Resumo\n", 4)
	fx := &fakeLLM{events: textEvents(loop), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Error != "truncated_repetition" {
		t.Fatalf("row = %+v", done)
	}
	if strings.Contains(done.Content, "Fim.\nFim.\nFim.\nFim") {
		t.Fatalf("loop remains: %q", done.Content)
	}
}

func TestStreamErrorChunkFails(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: []map[string]any{
		{"choices": []any{}, "error": map[string]any{"code": 429, "message": "rate-limited"}},
	}, title: "T"}
	svc := testService(t, st, fx)
	svc.retryDelays = []time.Duration{0, 0, 0}
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusFailed {
		t.Fatalf("row = %+v", done)
	}
	if len(fx.streams) != 4 {
		t.Fatalf("streams = %d, want 1 initial + 3 retries", len(fx.streams))
	}
}

func TestRateLimitAutoRetry(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{
		events:    textEvents("Hi!"),
		title:     "T",
		failFirst: 2,
		failEvents: []map[string]any{
			{"choices": []any{}, "error": map[string]any{"code": 429, "message": "temporarily rate-limited upstream"}},
		},
	}
	svc := testService(t, st, fx)
	svc.retryDelays = []time.Duration{0, 0, 0}
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusComplete || done.Content != "Hi!" {
		t.Fatalf("row = %+v", done)
	}
	if len(fx.streams) != 3 {
		t.Fatalf("streams = %d, want 2 failures + 1 success", len(fx.streams))
	}
}

func TestNonRetryableFailsFast(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: []map[string]any{
		{"choices": []any{}, "error": map[string]any{"code": 400, "message": "bad request"}},
	}, title: "T"}
	svc := testService(t, st, fx)
	svc.retryDelays = []time.Duration{0, 0, 0}
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusFailed {
		t.Fatalf("row = %+v", done)
	}
	if len(fx.streams) != 1 {
		t.Fatalf("streams = %d, want no retries", len(fx.streams))
	}
}

func TestNoCompactShortThread(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	conv, _ = st.GetConversation(conv.ID)
	if conv.Summary != "" || conv.SummarizedThroughID != 0 {
		t.Fatalf("conv = %+v", conv)
	}
}

func TestCompactOnOverflow(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	big := strings.Repeat("word ", 20000) // ~100k chars ≈ 25k tokens each
	for range 8 {
		_, _ = st.CreateUserMessage(conv.ID, big, false, false)
		a, _ := st.CreateAssistantMessage(conv.ID)
		_ = st.CompleteAssistant(a.ID, store.Completion{Content: "ok"})
	}
	_, _ = st.CreateUserMessage(conv.ID, "latest question", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("answer"), title: "Summary words here"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	conv, _ = st.GetConversation(conv.ID)
	if conv.Summary == "" || conv.SummarizedThroughID == 0 {
		t.Fatalf("conv = %+v", conv)
	}
	// Full transcript intact.
	rows, _ := st.Transcript(conv.ID)
	if len(rows) < 10 {
		t.Fatalf("rows = %d", len(rows))
	}
}

func TestUsageStoredWithoutReplay(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: []map[string]any{
		chunk("Yo"),
		usageChunk("openai/gpt-6-luna", map[string]any{
			"prompt_tokens": 10, "completion_tokens": 5, "cost": 0.00001,
			"prompt_tokens_details":     map[string]any{"cached_tokens": 4},
			"completion_tokens_details": map[string]any{"reasoning_tokens": 2},
		}),
	}, title: "T"}
	svc := testService(t, st, fx)
	svc.Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["cost_usd"] != 0.00001 || usage["model"] != "openai/gpt-6-luna" ||
		usage["input_tokens"] != 10.0 || usage["cached_tokens"] != 4.0 ||
		usage["output_tokens"] != 5.0 || usage["reasoning_tokens"] != 2.0 {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	// Next turn sends the assistant row as one plain message.
	_, _ = st.CreateUserMessage(conv.ID, "Again", false, false)
	items, _, _, _ := svc.messageInput(done, i18n.EN, true)
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
}

var tinyPDF = []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n")

func storePDF(t *testing.T, svc *Service, msgID int64, name string) {
	t.Helper()
	sum := fmt.Sprintf("%x", sha256.Sum256(tinyPDF))
	sub := filepath.Join(svc.DataDir, "uploads", sum[:2])
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(docs.OriginalPath(svc.DataDir, sum), tinyPDF, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.CreateDocument(msgID, name, docs.PDFMime, int64(len(tinyPDF)), sum); err != nil {
		t.Fatal(err)
	}
}

func TestMessageInputPDF(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	um, _ := st.CreateUserMessage(conv.ID, "", false, false)
	svc := testService(t, st, &fakeLLM{})
	storePDF(t, svc, um.ID, "r.pdf")
	items, cost, pdf, _ := svc.messageInput(um, i18n.EN, true)
	if !pdf {
		t.Fatal("pdf = false")
	}
	if len(items) != 1 {
		t.Fatalf("items = %v", items)
	}
	msg, _ := items[0].(map[string]any)
	parts, _ := msg["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %v", msg["content"])
	}
	text, _ := parts[0].(map[string]any)
	if text["type"] != "text" || text["text"] != "Summarize this document." {
		t.Fatalf("text part = %v", parts[0])
	}
	file, _ := parts[1].(map[string]any)
	inner, _ := file["file"].(map[string]any)
	want := "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(tinyPDF)
	if file["type"] != "file" || inner["filename"] != "r.pdf" || inner["file_data"] != want {
		t.Fatalf("file part = %v", parts[1])
	}
	if cost <= 0 {
		t.Fatalf("cost = %d", cost)
	}
}

func TestRunAttachesFilePlugin(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	um, _ := st.CreateUserMessage(conv.ID, "Read", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("ok"), title: "T"}
	svc := testService(t, st, fx)
	svc.Config.PDFEngine = "mistral-ocr"
	storePDF(t, svc, um.ID, "r.pdf")
	svc.Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].files == nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
	if fx.streams[0].files.Engine != "mistral-ocr" {
		t.Fatalf("files = %+v", fx.streams[0].files)
	}
}

func TestRunOmitsFilePluginWithoutPDF(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Hi!"), title: "T"}
	svc := testService(t, st, fx)
	svc.Config.PDFEngine = "mistral-ocr"
	svc.Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].files != nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestToolRowsOmittedFromWindow(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, err := st.DB().Exec(`INSERT INTO messages
		(conversation_id, role, content, created_at, updated_at)
		VALUES (?, 'tool', 'kagi blob', '2026-01-01 00:00:00', '2026-01-01 00:00:00')`, conv.ID)
	if err != nil {
		t.Fatal(err)
	}
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	if fx.streams[0].nInput != 3 { // persona + date + trailing turn note only
		t.Fatalf("input rows = %d", fx.streams[0].nInput)
	}
}

func TestTokenUsageParsing(t *testing.T) {
	out := tokenUsageFrom(map[string]any{
		"model": "openai/gpt-6-luna",
		"usage": map[string]any{
			"prompt_tokens": 100, "completion_tokens": 10, "cost": 0.00002,
			"prompt_tokens_details":     map[string]any{"cached_tokens": 40},
			"completion_tokens_details": map[string]any{"reasoning_tokens": 3},
		},
	})
	if out["input_tokens"] != 100.0 || out["output_tokens"] != 10.0 ||
		out["cached_tokens"] != 40.0 || out["reasoning_tokens"] != 3.0 ||
		out["cost_usd"] != 0.00002 || out["model"] != "openai/gpt-6-luna" {
		t.Fatalf("usage = %v", out)
	}
	if len(tokenUsageFrom(map[string]any{})) != 0 {
		t.Fatal("empty response should yield empty usage")
	}
}

func TestStoredCitationsValidates(t *testing.T) {
	rows := StoredCitations(`[{"title":"News","url":"https://b.example/x"},{"title":"","url":"notaurl"}]`)
	if len(rows) != 1 || rows[0].URL != "https://b.example/x" || rows[0].Title != "News" {
		t.Fatalf("rows = %+v", rows)
	}
	if StoredCitations("bogus") != nil || StoredCitations("") != nil {
		t.Fatal("bad blobs should yield nil")
	}
}

func TestStaleSweep(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	_, err := st.DB().Exec(`UPDATE messages SET updated_at = '2020-01-01 00:00:00' WHERE id = ?`, asst.ID)
	if err != nil {
		t.Fatal(err)
	}
	testService(t, st, &fakeLLM{}).SweepStale()
	done, _ := st.GetMessage(asst.ID)
	if done.Status != store.StatusFailed || done.Error != "stale" {
		t.Fatalf("row = %+v", done)
	}
}

func searchEvents() []map[string]any {
	return []map[string]any{
		{"choices": []any{map[string]any{"delta": map[string]any{
			"content": "News!",
			"annotations": []any{map[string]any{
				"type":         "url_citation",
				"url_citation": map[string]any{"url": "https://n.test/a", "title": "News A"},
			}},
		}}}},
		usageChunk("openai/gpt-6-luna", map[string]any{
			"prompt_tokens": 100, "completion_tokens": 5, "cost": 0.001,
		}),
	}
}

func searchService(t *testing.T, st *store.Store, fx *fakeLLM, feeIncluded bool) *Service {
	t.Helper()
	svc := testService(t, st, fx)
	svc.Config.Search = SearchConfig{
		Enabled: true, Engine: "exa", Mode: "auto", MaxResults: 5,
		DeepMode: "deep-lite", DeepMaxResults: 10, FeeIncluded: feeIncluded,
	}
	return svc
}

func TestSearchTurn(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Latest news?", true, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: searchEvents(), title: "T"}
	searchService(t, st, fx, true).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].search == nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
	got := fx.streams[0].search
	if got.Engine != "exa" || got.Mode != "auto" || got.MaxResults != 5 {
		t.Fatalf("search = %+v", got)
	}
	if !strings.Contains(fx.streams[0].tail, "fresh web search results") {
		t.Fatalf("tail = %q", fx.streams[0].tail)
	}
	if strings.Contains(fx.streams[0].sys0, "fresh web search results") {
		t.Fatalf("sys0 must stay toggle-free, got %q", fx.streams[0].sys0)
	}
	done, _ := st.GetMessage(asst.ID)
	if done.Citations != `[{"title":"News A","url":"https://n.test/a"}]` {
		t.Fatalf("citations = %q", done.Citations)
	}
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["search_engine"] != "exa" || usage["search_mode"] != "auto" || usage["search_cost_usd"] != 0.007 {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	if _, ok := usage["search_cost_separate"]; ok {
		t.Fatalf("included fee should not be separate: %q", done.TokenUsage)
	}
	if total := USDFor(usage); total != 0.001 {
		t.Fatalf("total = %v (fee must already be inside cost_usd)", total)
	}
}

func TestSearchTurnSeparateFee(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Latest news?", true, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: searchEvents(), title: "T"}
	searchService(t, st, fx, false).Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["search_cost_separate"] != true {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	if total := USDFor(usage); total < 0.00799 || total > 0.00801 {
		t.Fatalf("total = %v", total)
	}
}

func storeImage(t *testing.T, svc *Service, msgID int64) {
	t.Helper()
	raw := []byte("fake-jpeg-bytes")
	sum := fmt.Sprintf("%x", sha256.Sum256(raw))
	p := images.ModelPath(svc.DataDir, sum)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Store.CreateImage(msgID, "pic.jpg", "image/jpeg", int64(len(raw)), sum); err != nil {
		t.Fatal(err)
	}
}

func catalogStub(t *testing.T) *openrouter.Catalog {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"data":[
			{"id":"vis/model","architecture":{"modality":"text+image->text"}},
			{"id":"txt/model","architecture":{"modality":"text->text"}}
		]}`))
	}))
	t.Cleanup(srv.Close)
	return openrouter.NewCatalog(srv.URL, "")
}

func windowParts(t *testing.T, input []any) (images int, notice bool) {
	t.Helper()
	for _, item := range input {
		m, _ := item.(map[string]any)
		if m == nil {
			continue
		}
		switch content := m["content"].(type) {
		case []any:
			for _, p := range content {
				pm, _ := p.(map[string]any)
				if pm != nil && pm["type"] == "image_url" {
					images++
				}
			}
		case string:
			if m["role"] == "system" && strings.Contains(content, "can't see them") {
				notice = true
			}
		}
	}
	return images, notice
}

func TestVisionGuardDropsImages(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	um, _ := st.CreateUserMessage(conv.ID, "Look", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	svc := testService(t, st, &fakeLLM{})
	storeImage(t, svc, um.ID)

	svc.Config.Model = "txt/model"
	svc.Catalog = catalogStub(t)
	input, _, err := svc.windowedMessages(conv, asst, i18n.EN, false)
	if err != nil {
		t.Fatal(err)
	}
	if n, notice := windowParts(t, input); n != 0 || !notice {
		t.Fatalf("text-only: images=%d notice=%v", n, notice)
	}

	svc.Config.Model = "vis/model"
	input, _, err = svc.windowedMessages(conv, asst, i18n.EN, false)
	if err != nil {
		t.Fatal(err)
	}
	if n, notice := windowParts(t, input); n != 1 || notice {
		t.Fatalf("vision: images=%d notice=%v", n, notice)
	}
}

func TestVoiceSTTCarryOver(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	if err := st.AddUsageCost(asst.ID, "stt_cost_usd", 0.0001); err != nil {
		t.Fatal(err)
	}
	if err := st.AddUsageCost(asst.ID, "stt_seconds", 1.5); err != nil {
		t.Fatal(err)
	}
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	done, _ := st.GetMessage(asst.ID)
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["stt_cost_usd"] != 0.0001 || usage["stt_seconds"] != 1.5 {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
	if _, ok := usage["cost_usd"]; !ok {
		t.Fatalf("fresh usage missing: %q", done.TokenUsage)
	}
}

func TestSearchOffWithoutWebFlag(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	searchService(t, st, fx, true).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].search != nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
	if !strings.Contains(fx.streams[0].tail, "cannot browse") {
		t.Fatalf("tail = %q", fx.streams[0].tail)
	}
	done, _ := st.GetMessage(asst.ID)
	if done.Citations != "" {
		t.Fatalf("citations = %q", done.Citations)
	}
}

func TestSearchOffWhenDisabled(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "News?", true, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN) // Search zero value: disabled
	if len(fx.streams) != 1 || fx.streams[0].search != nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestModelRouting(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_ = st.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Model: "x-ai/grok-4.7"})
	conv, _ = st.GetConversation(conv.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	svc := testService(t, st, fx)
	svc.Config.Models = []string{"openai/gpt-6-luna", "x-ai/grok-4.7"}
	svc.Run(asst.ID, i18n.EN)
	// Turn runs on the sticky model; the title still uses the default.
	if len(fx.models) != 2 || fx.models[0] != "x-ai/grok-4.7" || fx.models[1] != "openai/gpt-6-luna" {
		t.Fatalf("models = %v", fx.models)
	}
}

func TestModelFallbackUnknown(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_ = st.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Model: "ghost/model"})
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	svc := testService(t, st, fx)
	svc.Config.Models = []string{"openai/gpt-6-luna", "x-ai/grok-4.7"}
	svc.Run(asst.ID, i18n.EN)
	if len(fx.models) == 0 || fx.models[0] != "openai/gpt-6-luna" {
		t.Fatalf("models = %v", fx.models)
	}
}

func TestRetryPreservesSearch(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "News?", true, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	_ = st.FailAssistant(asst.ID, "generation_failed")
	_ = st.ResetForRetry(asst.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	searchService(t, st, fx, true).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].search == nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestDeepTurn(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Deep dive?", true, true)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: searchEvents(), title: "T"}
	searchService(t, st, fx, true).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].search == nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
	got := fx.streams[0].search
	if got.Engine != "exa" || got.Mode != "deep-lite" || got.MaxResults != 10 {
		t.Fatalf("search = %+v", got)
	}
	done, _ := st.GetMessage(asst.ID)
	var usage map[string]any
	_ = json.Unmarshal([]byte(done.TokenUsage), &usage)
	if usage["search_mode"] != "deep-lite" || usage["search_deep"] != true ||
		usage["search_cost_usd"] != 0.012 {
		t.Fatalf("usage = %q", done.TokenUsage)
	}
}

func TestDeepImpliesSearch(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_, _ = st.CreateUserMessage(conv.ID, "Deep dive?", false, true)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	searchService(t, st, fx, true).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].search == nil {
		t.Fatalf("streams = %+v", fx.streams)
	}
	if fx.streams[0].search.Mode != "deep-lite" {
		t.Fatalf("search = %+v", fx.streams[0].search)
	}
}

func TestEffortRouting(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_ = st.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Effort: "low"})
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].effort != "low" {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestEffortFallbackUnknown(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	_ = st.UpdateConversationSettings(u.ID, conv.ID, store.ConversationSettings{Effort: "ultra"})
	_, _ = st.CreateUserMessage(conv.ID, "Hi", false, false)
	asst, _ := st.CreateAssistantMessage(conv.ID)
	fx := &fakeLLM{events: textEvents("Yo"), title: "T"}
	testService(t, st, fx).Run(asst.ID, i18n.EN)
	if len(fx.streams) != 1 || fx.streams[0].effort != "high" {
		t.Fatalf("streams = %+v", fx.streams)
	}
}

func TestMessageViewMeta(t *testing.T) {
	st := openStore(t)
	u := seedUser(t, st)
	conv, _ := st.CreateConversation(u.ID)
	a, _ := st.CreateAssistantMessage(conv.ID)
	_ = st.CompleteAssistant(a.ID, store.Completion{Content: "Hi",
		TokenUsage: `{"cost_usd":0.001,"model":"x-ai/grok-4.7","search_engine":"exa","search_deep":true}`})
	done, _ := st.GetMessage(a.ID)
	svc := testService(t, st, &fakeLLM{})
	mv := svc.TranscriptViewsFor(i18n.EN, []*store.Message{done}, conv.ID, false, "")[0]
	if mv.ModelLabel != "x-ai/grok-4.7" || !mv.Searched || !mv.Deep {
		t.Fatalf("view = %+v", mv)
	}
}
