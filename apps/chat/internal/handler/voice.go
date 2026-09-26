package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/abadojack/whatlanggo"
	"github.com/aquasp/kurachat/internal/chat"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
)

// VoiceClient is the audio surface the voice endpoints need (stubbed in
// tests); *openrouter.Client implements it.
type VoiceClient interface {
	Transcribe(ctx context.Context, audio []byte, filename, language string) (openrouter.Transcript, error)
	Speak(ctx context.Context, text, voice, format string) (io.ReadCloser, string, error)
	GenerationCost(ctx context.Context, genID string) (float64, error)
}

var _ VoiceClient = (*openrouter.Client)(nil)

const (
	// voiceAudioMax caps one recording (STT providers time out past ~60s
	// of processing anyway; 10 MB holds minutes of Opus).
	voiceAudioMax = 10 << 20
	// voiceTextMax caps one TTS request; the client chunks replies by
	// sentence so playback starts before the whole reply is spoken.
	voiceTextMax = 2000
)

func (s *Server) voiceClient(model string) (VoiceClient, error) {
	if s.NewVoiceClient != nil {
		return s.NewVoiceClient(model)
	}
	return openrouter.New(s.Config.OpenRouterAPIKey, model)
}

// voiceLang passes the UI locale to STT ("en"/"pt" are valid BCP-47).
func voiceLang(r *http.Request) string {
	return string(LocaleOf(r))
}

// handleVoiceTranscribe turns one recording into chat text:
// POST /conversations/{id}/voice/transcribe (multipart, field "audio")
// → {"text","seconds","cost_usd"}. Empty text (silence) is a 200 with
// ""; the client decides how to surface it.
func (s *Server) handleVoiceTranscribe(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, convID); err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("voice:"+itoa64(user.ID), 30, 5*time.Minute) {
		voiceJSON(w, http.StatusTooManyRequests, map[string]any{"error": "voice.too_many"})
		return
	}
	if err := r.ParseMultipartForm(voiceAudioMax + (1 << 20)); err != nil {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_audio"})
		return
	}
	f, hdr, err := r.FormFile("audio")
	if err != nil {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.no_audio"})
		return
	}
	defer f.Close()
	if !voiceAudioOK(hdr.Filename, hdr.Header.Get("Content-Type")) {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_audio"})
		return
	}
	raw, err := io.ReadAll(io.LimitReader(f, voiceAudioMax+1))
	if err != nil || len(raw) == 0 || len(raw) > voiceAudioMax {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_audio"})
		return
	}
	vc, err := s.voiceClient(s.Config.VoiceSTTModel)
	if err != nil {
		voiceJSON(w, http.StatusBadGateway, map[string]any{"error": "voice.unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	tr, err := vc.Transcribe(ctx, raw, hdr.Filename, voiceLang(r))
	if err != nil {
		voiceJSON(w, http.StatusBadGateway, map[string]any{"error": "voice.unavailable"})
		return
	}
	voiceJSON(w, http.StatusOK, map[string]any{
		"text": tr.Text, "seconds": tr.Seconds, "cost_usd": tr.CostUSD,
	})
}

// handleVoiceSpeak streams one reply chunk as audio:
// POST /conversations/{id}/voice/speak (text, message_id) → audio/mpeg.
// The billed total posts asynchronously, so metering runs after the
// stream finishes: poll the generation, then fold the cost into the
// message usage (replays accumulate under tts_cost_usd).
func (s *Server) handleVoiceSpeak(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, convID); err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("voice:"+itoa64(user.ID), 30, 5*time.Minute) {
		voiceJSON(w, http.StatusTooManyRequests, map[string]any{"error": "voice.too_many"})
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > voiceTextMax {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_text"})
		return
	}
	mid, err := strconv.ParseInt(r.FormValue("message_id"), 10, 64)
	if err != nil || mid <= 0 {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_message"})
		return
	}
	msg, err := s.Store.GetMessage(mid)
	if err != nil || msg.ConversationID != convID || msg.Role != "assistant" {
		s.notFound(w, r)
		return
	}
	vc, err := s.voiceClient(s.Config.VoiceTTSModel)
	if err != nil {
		voiceJSON(w, http.StatusBadGateway, map[string]any{"error": "voice.unavailable"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), openrouter.SpeakTimeout)
	defer cancel()
	voice := voiceForText(text, LocaleOf(r), s.Config.VoiceTTSVoice, s.Config.VoiceTTSVoiceEN)
	body, genID, err := vc.Speak(ctx, text, voice, "mp3")
	if err != nil {
		voiceJSON(w, http.StatusBadGateway, map[string]any{"error": "voice.unavailable"})
		return
	}
	defer body.Close()
	w.Header().Set("X-TTS-Voice", voice)
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Disposition", `inline; filename="reply.mp3"`)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	n, copyErr := io.Copy(&flushWriter{w: w, flusher: flusher}, body)
	if copyErr != nil || n == 0 {
		return // client gone or empty stream: nothing billable to record
	}
	_ = s.Store.AddUsageCost(mid, "tts_chars", float64(len(text)))
	if genID != "" {
		go s.pollVoiceCost(vc, mid, genID)
	}
}

// pollVoiceCost resolves one TTS generation's billed total (404 = not
// posted yet) and folds it into the message usage. Best effort: the
// tts_chars key already records that speech happened.
func (s *Server) pollVoiceCost(vc VoiceClient, mid int64, genID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	for i, wait := range append([]time.Duration{0}, openrouter.PollDelays...) {
		if i > 0 {
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			}
		}
		cost, err := vc.GenerationCost(ctx, genID)
		if err == nil {
			_ = s.Store.AddUsageCost(mid, "tts_cost_usd", cost)
			return
		}
		var oerr *openrouter.Error
		if !errors.As(err, &oerr) || oerr.Code != http.StatusNotFound {
			return // fatal: malformed id, bad key, provider error
		}
	}
}

// voiceForText picks the TTS voice by language: a confident detection on
// enough text wins, otherwise the UI locale, otherwise the default voice.
// Only configured languages map; anything else falls back to default.
func voiceForText(text string, locale i18n.Locale, ptVoice, enVoice string) string {
	if utf8.RuneCountInString(strings.TrimSpace(text)) >= 24 {
		if info := whatlanggo.Detect(text); info.IsReliable() {
			if v := voiceForLang(info.Lang.Iso6391(), ptVoice, enVoice); v != "" {
				return v
			}
		}
	}
	if v := voiceForLang(string(locale), ptVoice, enVoice); v != "" {
		return v
	}
	return ptVoice
}

func voiceForLang(code, ptVoice, enVoice string) string {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "pt":
		return ptVoice
	case "en":
		return enVoice
	}
	return ""
}

// voiceAudioOK accepts recorded audio by content type or extension.
func voiceAudioOK(filename, contentType string) bool {
	if ct := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0])); ct != "" {
		if strings.HasPrefix(ct, "audio/") || ct == "video/webm" {
			return true
		}
	}
	switch strings.ToLower(strings.TrimPrefix(path.Ext(filename), ".")) {
	case "webm", "mp3", "wav", "m4a", "ogg", "oga", "opus", "mp4", "flac", "aac":
		return true
	}
	return false
}

func voiceJSON(w http.ResponseWriter, code int, v map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// handleVoicePolish cleans a dictation transcript:
// POST /conversations/{id}/voice/polish (text) → {"text","cost_usd"}.
// Grammar/punctuation/paragraphs fixed, meaning kept, no commentary.
// The cost rides back as a hidden field on the next send.
func (s *Server) handleVoicePolish(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	convID, err := convID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindConversation(user.ID, convID); err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("voice:"+itoa64(user.ID), 30, 5*time.Minute) {
		voiceJSON(w, http.StatusTooManyRequests, map[string]any{"error": "voice.too_many"})
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > 4000 {
		voiceJSON(w, http.StatusBadRequest, map[string]any{"error": "voice.bad_text"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	cleaned, cost, err := s.polishText(ctx, LocaleOf(r), text)
	if err != nil || cleaned == "" {
		voiceJSON(w, http.StatusBadGateway, map[string]any{"error": "voice.unavailable"})
		return
	}
	voiceJSON(w, http.StatusOK, map[string]any{"text": cleaned, "cost_usd": cost})
}

// polishText runs the cleanup pass on the default model (quality is
// model-insensitive here; effort none keeps it fast and cheap).
func (s *Server) polishText(ctx context.Context, locale i18n.Locale, text string) (string, float64, error) {
	model := s.Config.OpenRouterModel
	var lc chat.LLMClient
	var err error
	if s.Chat.NewClient != nil {
		lc, err = s.Chat.NewClient(model)
	} else {
		lc, err = openrouter.New(s.Config.OpenRouterAPIKey, model)
	}
	if err != nil {
		return "", 0, err
	}
	maxTokens := 1500
	out, err := lc.Complete(ctx, []any{
		map[string]any{"role": "system", "content": i18n.T(locale, "chat.polish_prompt")},
		map[string]any{"role": "user", "content": text},
	}, &maxTokens, "none")
	if err != nil {
		return "", 0, err
	}
	return strings.TrimSpace(openrouter.MessageText(out)), voiceUsageCost(out), nil
}

// voiceUsageCost pulls the billed total out of a completion response.
func voiceUsageCost(out map[string]any) float64 {
	usage, _ := out["usage"].(map[string]any)
	switch n := usage["cost"].(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

// flushWriter flushes each chunk so playback starts before TTS finishes.
type flushWriter struct {
	w       io.Writer
	flusher http.Flusher
}

func (f *flushWriter) Write(p []byte) (int, error) {
	n, err := f.w.Write(p)
	if f.flusher != nil {
		f.flusher.Flush()
	}
	return n, err
}
