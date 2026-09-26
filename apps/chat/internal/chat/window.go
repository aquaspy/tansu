package chat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/docs"
	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/images"
	"github.com/aquasp/kurachat/internal/store"
	_ "time/tzdata" // embedded zones for the container image
)

// windowedMessages builds the model input: system prefix plus the newest
// rows that fit the token window. It also reports whether any picked row
// carries a PDF, so the caller can attach the file-parser plugin.
// Images are dropped (with a system notice) when the turn's model is
// known text-only, so switching models mid-chat degrades instead of
// erroring at the provider.
func (s *Service) windowedMessages(conv *store.Conversation, assistant *store.Message, locale i18n.Locale, search bool) ([]any, bool, error) {
	rows, err := s.Store.WindowRows(conv.ID, assistant.ID, conv.SummarizedThroughID)
	if err != nil {
		return nil, false, err
	}
	vision := true
	if s.rowsHaveImages(rows) {
		vision = s.supportsImage(s.resolveModel(conv))
	}
	prefix := s.prefixMessages(conv, locale)
	suffix := []any{s.turnNote(locale, search)}
	prefixJSON, _ := json.Marshal(prefix)
	suffixJSON, _ := json.Marshal(suffix)
	est := tokenEstimate(string(prefixJSON)) + tokenEstimate(string(suffixJSON))
	var picked []any
	hasPDF := false
	dropped := 0
	for i := len(rows) - 1; i >= 0; i-- {
		items, cost, pdf, drop := s.messageInput(rows[i], locale, vision)
		if len(items) == 0 {
			continue
		}
		if est+cost > s.Config.WindowTokens {
			break
		}
		picked = append(items, picked...)
		est += cost
		hasPDF = hasPDF || pdf
		dropped += drop
	}
	if dropped > 0 {
		// Unbudgeted (~60 tokens): the notice only exists because the
		// loop above dropped images for a text-only model.
		suffix = append(suffix, s.visionNotice(locale, dropped))
	}
	return append(append(prefix, picked...), suffix...), hasPDF, nil
}

// rowsHaveImages peeks for attached images so the capability lookup only
// runs when it can change the outcome (never on text-only turns).
func (s *Service) rowsHaveImages(rows []*store.Message) bool {
	for _, m := range rows {
		if m.Role != store.RoleUser {
			continue
		}
		if imgs, _ := s.Store.ListImages(m.ID); len(imgs) > 0 {
			return true
		}
	}
	return false
}

// supportsImage consults the model catalog (nil/unknown/failure fails open).
func (s *Service) supportsImage(model string) bool {
	if s.Catalog == nil {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return s.Catalog.SupportsImage(ctx, model)
}

func (s *Service) visionNotice(locale i18n.Locale, dropped int) any {
	return map[string]any{
		"role":    "system",
		"content": i18n.T(locale, "chat.no_vision_note", "n", strconv.Itoa(dropped)),
	}
}

// prefixMessages builds the stable head of the model input: base prompt,
// date, and rolling summary. It never varies with per-turn flags, so the
// cached prefix survives web-search toggles; conditional notes ride at
// the end instead (turnNote, visionNotice).
func (s *Service) prefixMessages(conv *store.Conversation, locale i18n.Locale) []any {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		loc = time.UTC
	}
	now := time.Now().In(loc)
	date := "Current date: " + now.Format("2006-01-02 Monday") + " (America/Sao_Paulo)."
	out := []any{
		map[string]any{"role": "system", "content": i18n.T(locale, "chat.system_prompt")},
		map[string]any{"role": "system", "content": date},
	}
	if strings.TrimSpace(conv.Summary) != "" {
		out = append(out, map[string]any{"role": "system", "content": "Earlier conversation summary:\n" + conv.Summary})
	}
	return out
}

// turnNote carries the per-turn browse/search instruction. It lives after
// the newest user message so toggling web search rewrites the tail only,
// never the cached prefix.
func (s *Service) turnNote(locale i18n.Locale, search bool) any {
	key := "chat.system_note_plain"
	if search {
		key = "chat.system_note_search"
	}
	return map[string]any{"role": "system", "content": i18n.T(locale, key)}
}

func tokenEstimate(s string) int {
	return int(math.Ceil(float64(len(s)) / 4.0))
}

// messageInput ports Message#as_input + #input_cost. It also reports
// whether the row carries a PDF part (file-parser plugin signal) and how
// many images were dropped for a text-only model (vision=false).
func (s *Service) messageInput(m *store.Message, locale i18n.Locale, vision bool) ([]any, int, bool, int) {
	if m.Role != store.RoleUser && m.Role != store.RoleAssistant {
		return nil, 0, false, 0
	}
	if m.Role == store.RoleAssistant && (m.Status != store.StatusComplete || m.Content == "") {
		return nil, 0, false, 0
	}
	var items []any
	cost := 0
	content, textCost, attachCost, pdf, dropped := s.inputContent(m, locale, vision)
	cost += textCost + attachCost
	items = append(items, map[string]any{"role": m.Role, "content": content})
	return items, cost, pdf, dropped
}

func (s *Service) inputContent(m *store.Message, locale i18n.Locale, vision bool) (any, int, int, bool, int) {
	if m.Role != store.RoleUser {
		return m.Content, tokenEstimate(m.Content), 0, false, 0
	}
	imgs, _ := s.Store.ListImages(m.ID)
	docs, _ := s.Store.ListDocuments(m.ID)
	text := strings.TrimSpace(m.Content)
	if text == "" && len(docs) > 0 {
		text = i18n.T(locale, "chat.doc_prompt")
	} else if text == "" && len(imgs) > 0 {
		text = i18n.T(locale, "chat.image_prompt")
	}
	if len(imgs) == 0 && len(docs) == 0 {
		return m.Content, tokenEstimate(text), 0, false, 0
	}
	parts := []any{map[string]any{"type": "text", "text": text}}
	attachCost := 0
	dropped := 0
	if !vision {
		dropped = len(imgs)
		imgs = nil
	}
	for _, img := range imgs {
		if uri := s.imageDataURI(img); uri != "" {
			parts = append(parts, map[string]any{
				"type": "image_url", "image_url": map[string]any{"url": uri, "detail": "high"},
			})
			attachCost += images.ImageTokens
		}
	}
	hasPDF := false
	for _, doc := range docs {
		if part, cost := s.docFilePart(doc); part != nil {
			parts = append(parts, part)
			attachCost += cost
			hasPDF = true
		}
	}
	if len(parts) == 1 {
		return m.Content, tokenEstimate(text), 0, false, dropped
	}
	return parts, tokenEstimate(text), attachCost, hasPDF, dropped
}

// docFilePart builds the OpenRouter file part for one PDF plus its rough
// window cost. Missing or unreadable bytes are skipped, not fatal.
func (s *Service) docFilePart(doc *store.Document) (any, int) {
	raw, err := os.ReadFile(docs.OriginalPath(s.DataDir, doc.SHA256))
	if err != nil || len(raw) == 0 {
		return nil, 0
	}
	return map[string]any{
		"type": "file",
		"file": map[string]any{
			"filename":  doc.Filename,
			"file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(raw),
		},
	}, tokenEstimate(string(raw))
}

func (s *Service) imageDataURI(img *store.Image) string {
	if raw, err := os.ReadFile(images.ModelPath(s.DataDir, img.SHA256)); err == nil {
		return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(raw)
	}
	// Fallback to the original bytes when they decode (imported originals
	// without derivatives); undecodable files are skipped, not fatal.
	raw, err := os.ReadFile(images.OriginalPath(s.DataDir, img.SHA256, images.ExtFor(img.ContentType)))
	if err != nil || !images.Decodable(raw) {
		return ""
	}
	mime := img.ContentType
	if mime == "" {
		mime = "image/png"
	}
	return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw)
}

// inputCostOf estimates a row's window cost without building input.
// Titles budget with images included (unchanged).
func (s *Service) inputCostOf(m *store.Message, locale i18n.Locale) int {
	_, cost, _, _ := s.messageInput(m, locale, true)
	return cost
}
