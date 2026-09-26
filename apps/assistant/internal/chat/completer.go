package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
	"github.com/aquasp/kurachat/internal/views"
)

const (
	heartbeatEvery = 60 * time.Second
	flushEvery     = 250 * time.Millisecond
	flushChars     = 80
	staleAfter     = 5 * time.Minute
	// startDelay lets the POST response (with the SSE subscriptions) reach
	// the browser before the first status event, mirroring the de-facto
	// Solid Queue pickup delay.
	startDelay = 500 * time.Millisecond
)

// defaultRetryDelays spaces automatic retries of transient failures
// (rate limits, flapping providers). Worst case adds ~50s before the
// turn fails for real; the stale sweeper (5min) is far above that.
var defaultRetryDelays = []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}

// SearchConfig mirrors the SEARCH_* environment.
type SearchConfig struct {
	Enabled        bool
	Engine         string
	Mode           string
	MaxResults     int
	DeepMode       string
	DeepMaxResults int
	FeeIncluded    bool // search fee already folded into usage.cost
}

// CompleterConfig mirrors the CHAT_*/OPENROUTER_*/SEARCH_* environment.
type CompleterConfig struct {
	Model            string // default model (Models[0])
	Models           []string
	Effort           string
	WindowTokens     int
	KeepRecentTokens int
	ReplyMaxTokens   int // 0 = no cap
	Search           SearchConfig
	PDFEngine        string // file-parser engine for PDFs
}

// LLMClient is the provider surface the completer needs (stubbed in tests).
type LLMClient interface {
	StreamChat(ctx context.Context, messages []any, maxCompletionTokens *int, reasoningEffort, sessionID string, search *openrouter.SearchOptions, files *openrouter.FileOptions, yield func(map[string]any) error) error
	Complete(ctx context.Context, messages []any, maxCompletionTokens *int, reasoningEffort string) (map[string]any, error)
}

var _ LLMClient = (*openrouter.Client)(nil)

// Service runs streaming completions and publishes SSE fragment events.
type Service struct {
	Store   *store.Store
	Hub     *Hub
	DataDir string
	Config  CompleterConfig
	APIKey  string
	// Catalog answers vision-capability questions; nil fails open.
	Catalog *openrouter.Catalog

	// NewClient builds the provider client; override in tests.
	NewClient func(model string) (LLMClient, error)

	// retryDelays overrides the backoff between automatic retries in
	// tests (nil = defaultRetryDelays).
	retryDelays []time.Duration
}

func (s *Service) client(model string) (LLMClient, error) {
	if s.NewClient != nil {
		return s.NewClient(model)
	}
	return openrouter.New(s.APIKey, model)
}

// resolveModel pins the turn to the conversation's model when it is still
// configured, else the default.
func (s *Service) resolveModel(conv *store.Conversation) string {
	for _, m := range s.Config.Models {
		if m != "" && m == conv.Model {
			return m
		}
	}
	return s.Config.Model
}

// resolveSearch reports whether this turn searches and whether deep:
// globally enabled and the answered user row asked. Deep implies search.
func (s *Service) resolveSearch(conv *store.Conversation, assistant *store.Message) (search, deep bool) {
	if !s.Config.Search.Enabled {
		return false, false
	}
	user, err := s.Store.LastUserMessageBefore(conv.ID, assistant.ID)
	if err != nil {
		return false, false
	}
	return user.Web || user.Deep, user.Deep
}

// resolveEffort pins the turn to the conversation's effort when valid,
// else the server default.
func (s *Service) resolveEffort(conv *store.Conversation) string {
	if conv.Effort != "" && openrouter.ValidEffort(conv.Effort) {
		return conv.Effort
	}
	return s.Config.Effort
}

// RunAsync starts Run in the background after startDelay.
func (s *Service) RunAsync(assistantID int64, locale i18n.Locale) {
	go func() {
		time.Sleep(startDelay)
		s.Run(assistantID, locale)
	}()
}

// Run streams the reply for a pending assistant row.
func (s *Service) Run(assistantID int64, locale i18n.Locale) {
	assistant, err := s.Store.GetMessage(assistantID)
	if err != nil {
		return // gone
	}
	if assistant.Status != store.StatusPending && assistant.Status != store.StatusStreaming {
		return
	}
	conv, err := s.Store.GetConversation(assistant.ConversationID)
	if err != nil {
		return
	}

	if err := s.Store.SetAssistantStreaming(assistant.ID); err != nil {
		return
	}
	s.publishStatus(conv.ID, assistant.ID, locale)

	var acc *accumulator
	var tokenUsage map[string]any
	truncated := false
	delays := s.retryDelays
	if delays == nil {
		delays = defaultRetryDelays
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stopBeat := make(chan struct{})
	defer close(stopBeat)
	go func() {
		t := time.NewTicker(heartbeatEvery)
		defer t.Stop()
		for {
			select {
			case <-t.C:
				_ = s.Store.Heartbeat(assistantID)
			case <-stopBeat:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	model := s.resolveModel(conv)
	effort := s.resolveEffort(conv)
	search, deep := s.resolveSearch(conv, assistant)
	input, hasPDF, err := s.windowedMessages(conv, assistant, locale, search)
	if err != nil {
		s.fail(conv.ID, assistant, err, locale)
		return
	}
	client, err := s.client(model)
	if err != nil {
		s.fail(conv.ID, assistant, err, locale)
		return
	}
	var maxOut *int
	if s.Config.ReplyMaxTokens > 0 {
		maxOut = &s.Config.ReplyMaxTokens
	}
	mode, maxResults := s.Config.Search.Mode, s.Config.Search.MaxResults
	if deep {
		mode, maxResults = s.Config.Search.DeepMode, s.Config.Search.DeepMaxResults
	}
	var searchOpts *openrouter.SearchOptions
	if search {
		searchOpts = &openrouter.SearchOptions{
			Engine:     s.Config.Search.Engine,
			Mode:       mode,
			MaxResults: maxResults,
		}
	}
	var fileOpts *openrouter.FileOptions
	if hasPDF {
		fileOpts = &openrouter.FileOptions{Engine: s.Config.PDFEngine}
	}
	var streamErr error
	for attempt := 0; ; attempt++ {
		acc = &accumulator{lastFlush: time.Now()}
		tokenUsage = map[string]any{}
		streamErr = client.StreamChat(ctx, input, maxOut, effort,
			fmt.Sprintf("kura-%d", conv.ID), searchOpts, fileOpts, func(event map[string]any) error {
				if failErr := failedEvent(event); failErr != nil {
					return failErr
				}
				if abort := s.applyEvent(event, acc, tokenUsage, conv); abort {
					truncated = true
					cancel()
					return &repetitionAbort{reason: acc.reason}
				}
				if acc.flushDue() {
					s.flush(conv.ID, assistant.ID, acc, locale)
				}
				return nil
			})
		if streamErr == nil {
			break
		}
		if !s.Store.MessageExists(assistantID) {
			return
		}
		// A repetition abort ends the stream cleanly (truncated, not failed).
		if _, ok := streamErr.(*repetitionAbort); ok {
			truncated = true
			break
		}
		// Retry transient failures that produced nothing yet; a partial
		// reply fails as before (manual Retry regenerates it whole).
		if acc.text != "" || !openrouter.Retryable(streamErr) || attempt >= len(delays) {
			s.fail(conv.ID, assistant, streamErr, locale)
			return
		}
		log.Printf("[chat] message_id=%d transient %v, retry %d/%d in %v",
			assistant.ID, streamErr, attempt+1, len(delays), delays[attempt])
		s.publishRetrying(conv.ID, assistant.ID, locale)
		time.Sleep(delays[attempt])
	}
	s.flush(conv.ID, assistant.ID, acc, locale)
	if search {
		s.stampSearchUsage(tokenUsage, mode, maxResults, deep)
	}
	log.Printf("[chat] message_id=%d model=%s effort=%s search=%v deep=%v chars=%d input=%v cached=%v cost=%v",
		assistant.ID, model, effort, search, deep, len(acc.text),
		tokenUsage["input_tokens"], tokenUsage["cached_tokens"], tokenUsage["cost_usd"])
	s.htmlComplete(conv, assistant, acc, tokenUsage, truncated || acc.aborted, locale)
	s.autoTitle(conv, locale)
	s.maybeCompact(conv, assistant, locale)
}

type repetitionAbort struct{ reason string }

func (e *repetitionAbort) Error() string { return e.reason }

// applyEvent folds one chat chunk into the accumulator. It reports
// whether the stream must abort (repetition guard tripped).
func (s *Service) applyEvent(event map[string]any, acc *accumulator, tokenUsage map[string]any, conv *store.Conversation) bool {
	if delta := chatTextDelta(event); delta != "" {
		acc.addText(delta)
		if reason := Check(acc.text); reason != "" {
			acc.abort(reason)
			return true
		}
	}
	if usage, ok := event["usage"].(map[string]any); ok && usage != nil {
		for k, v := range tokenUsageFrom(event) {
			tokenUsage[k] = v
		}
		if len(tokenUsage) > 0 {
			if _, ok := tokenUsage["model"]; !ok {
				tokenUsage["model"] = s.resolveModel(conv)
			}
		}
	}
	for _, c := range openrouter.AnnotationCitations(event) {
		acc.addCitation(c)
	}
	return false
}

// stampSearchUsage records how the turn searched. The fee is itemized but
// only added to totals when billed separately (see USDFor); by default
// OpenRouter already folds it into usage.cost.
func (s *Service) stampSearchUsage(tokenUsage map[string]any, mode string, maxResults int, deep bool) {
	cfg := s.Config.Search
	if cfg.Engine != "" {
		tokenUsage["search_engine"] = cfg.Engine
	}
	if mode != "" {
		tokenUsage["search_mode"] = mode
	}
	if deep {
		tokenUsage["search_deep"] = true
	}
	if fee, ok := openrouter.SearchFeeUSD(cfg.Engine, mode, maxResults); ok {
		tokenUsage["search_cost_usd"] = fee
		if !cfg.FeeIncluded {
			tokenUsage["search_cost_separate"] = true
		}
	}
}

func chatTextDelta(event map[string]any) string {
	choices, _ := event["choices"].([]any)
	if len(choices) == 0 {
		return ""
	}
	first, _ := choices[0].(map[string]any)
	delta, _ := first["delta"].(map[string]any)
	if delta == nil {
		return ""
	}
	text, _ := delta["content"].(string)
	return text
}

// failedEvent extracts a provider failure from the event, if any.
func failedEvent(event map[string]any) error {
	if e, ok := event["error"].(map[string]any); ok && e != nil {
		msg, _ := e["message"].(string)
		if msg == "" {
			msg = "generation_failed"
		}
		return &openrouter.Error{Msg: msg, Code: errorCode(e["code"])}
	}
	choices, _ := event["choices"].([]any)
	if len(choices) > 0 {
		if first, _ := choices[0].(map[string]any); first != nil {
			if fr, _ := first["finish_reason"].(string); fr == "error" {
				return &openrouter.Error{Msg: "generation_failed"}
			}
		}
	}
	return nil
}

// errorCode reads a provider error code in whatever shape JSON decoding
// left it (float64 over the wire, int in hand-built maps).
func errorCode(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}

func (s *Service) flush(convID, assistantID int64, acc *accumulator, locale i18n.Locale) {
	if !acc.textChanged() {
		return
	}
	if !s.Store.MessageExists(assistantID) {
		return
	}
	_ = s.Store.WriteStreamContent(assistantID, acc.text)
	acc.markFlushed()
	s.Hub.Publish(convID, Event{
		Name: views.BodyEvent(assistantID),
		HTML: Render(orEllipsis(acc.text)),
	})
}

func orEllipsis(s string) string {
	if s == "" {
		return "…"
	}
	return s
}

func (s *Service) publishStatus(convID, assistantID int64, locale i18n.Locale) {
	s.Hub.Publish(convID, Event{Name: views.StatusEvent(assistantID), HTML: escapeText(i18n.T(locale, "chat.thinking"))})
}

func (s *Service) publishRetrying(convID, assistantID int64, locale i18n.Locale) {
	s.Hub.Publish(convID, Event{Name: views.StatusEvent(assistantID), HTML: escapeText(i18n.T(locale, "chat.retrying"))})
}

func (s *Service) htmlComplete(conv *store.Conversation, assistant *store.Message, acc *accumulator,
	tokenUsage map[string]any, truncated bool, locale i18n.Locale) {
	if !s.Store.MessageExists(assistant.ID) {
		return
	}
	c := store.Completion{Content: strings.TrimSpace(acc.text), Citations: acc.citationsJSON()}
	if truncated {
		c.Error = "truncated_repetition"
	}
	// Voice STT/polish legs stamp the row before the stream runs; carry
	// those keys into the final blob (fresh usage wins every collision).
	for _, key := range []string{"stt_cost_usd", "stt_seconds", "polish_cost_usd"} {
		if _, ok := tokenUsage[key]; !ok {
			if v, ok := assistant.UsageMap()[key]; ok && v != nil {
				tokenUsage[key] = v
			}
		}
	}
	if len(tokenUsage) > 0 {
		if b, err := json.Marshal(tokenUsage); err == nil {
			c.TokenUsage = string(b)
		}
	}
	_ = s.Store.CompleteAssistant(assistant.ID, c)
	done, err := s.Store.GetMessage(assistant.ID)
	if err != nil {
		return
	}
	mv := s.messageView(locale, done, conv.ID, false, "")
	s.Hub.Publish(conv.ID, Event{Name: views.BodyEvent(done.ID), HTML: mv.BodyHTML})
	s.Hub.Publish(conv.ID, Event{Name: views.MessageEvent(done.ID), HTML: renderHTML(views.MessageArticle(pageFor(locale), mv, conv.ID, false))})
	s.Hub.Publish(conv.ID, Event{Name: views.CostEvent(conv.ID), HTML: renderHTML(views.ChatCost(pageFor(locale), conv.ID, s.CostView(locale, conv.ID)))})
}

func (s *Service) fail(convID int64, assistant *store.Message, err error, locale i18n.Locale) {
	if !s.Store.MessageExists(assistant.ID) {
		return
	}
	log.Printf("[chat] complete failed: %v", err)
	code := "generation_failed"
	if msg := err.Error(); msg == "missing_key" {
		code = "missing_key"
	}
	_ = s.Store.FailAssistant(assistant.ID, code)
	s.BroadcastFailed(convID, assistant.ID, locale)
}

// BroadcastFailed re-renders a failed row everywhere it is open.
func (s *Service) BroadcastFailed(convID, assistantID int64, locale i18n.Locale) {
	done, err := s.Store.GetMessage(assistantID)
	if err != nil {
		return
	}
	mv := s.messageView(locale, done, convID, false, "")
	s.Hub.Publish(convID, Event{Name: views.MessageEvent(done.ID),
		HTML: renderHTML(views.MessageArticle(pageFor(locale), mv, convID, false))})
}

// SweepStale fails completions idle past the threshold (boot + ticker).
// Rails runs this job under the default locale, so failed text may render
// in English until reload; same here.
func (s *Service) SweepStale() {
	stale, err := s.Store.FailStale(time.Now().Add(-staleAfter))
	if err != nil {
		return
	}
	for _, m := range stale {
		s.BroadcastFailed(m.ConversationID, m.ID, i18n.EN)
	}
}

func renderHTML(c templ.Component) string {
	var b strings.Builder
	_ = c.Render(context.Background(), &b)
	return b.String()
}

func escapeText(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

type accumulator struct {
	text       string
	lastFlush  time.Time
	flushedLen int
	aborted    bool
	reason     string
	citations  []openrouter.Citation
	cited      map[string]bool
}

func (a *accumulator) addCitation(c openrouter.Citation) {
	if a.cited == nil {
		a.cited = map[string]bool{}
	}
	if a.cited[c.URL] {
		return
	}
	a.cited[c.URL] = true
	a.citations = append(a.citations, c)
}

// citationsJSON renders the stored shape StoredCitations parses.
func (a *accumulator) citationsJSON() string {
	if len(a.citations) == 0 {
		return ""
	}
	arr := make([]map[string]string, 0, len(a.citations))
	for _, c := range a.citations {
		arr = append(arr, map[string]string{"url": c.URL, "title": c.Title})
	}
	b, err := json.Marshal(arr)
	if err != nil {
		return ""
	}
	return string(b)
}

func (a *accumulator) addText(chunk string) {
	if a.aborted {
		return
	}
	a.text += chunk
}

func (a *accumulator) abort(reason string) {
	a.aborted = true
	a.reason = reason
	a.text = Truncate(a.text, reason)
}

func (a *accumulator) flushDue() bool {
	grown := len(a.text) - a.flushedLen
	return grown >= flushChars || (grown > 0 && time.Since(a.lastFlush) >= flushEvery)
}

func (a *accumulator) textChanged() bool { return len(a.text) != a.flushedLen }

func (a *accumulator) markFlushed() {
	a.flushedLen = len(a.text)
	a.lastFlush = time.Now()
}
