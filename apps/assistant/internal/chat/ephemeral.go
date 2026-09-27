package chat

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/openrouter"
	"github.com/aquasp/kurachat/internal/store"
)

// AnonTurn is one client-held anonymous message. Nothing here is stored.
type AnonTurn struct {
	Role    string
	Content string
}

const (
	anonMaxTurns = 40
	anonMaxChars = 16384
)

// StreamAnonymous completes one anonymous turn. It never reads or writes
// conversations, messages, or app tools. onDelta may be nil.
func (s *Service) StreamAnonymous(ctx context.Context, locale i18n.Locale, history []AnonTurn, web, deep bool, model, effort string, onDelta func(string) error) (string, error) {
	conv := &store.Conversation{Mode: store.ModeAnonymous}
	if s.Config.ShowModelControls {
		conv.Model = model
		conv.Effort = effort
	}
	model = s.resolveModel(conv)
	effort = s.resolveEffort(conv)
	if !s.Config.Search.Enabled {
		web, deep = false, false
	}
	if deep {
		web = true
	}
	client, err := s.client(model)
	if err != nil {
		return "", err
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
	if web {
		searchOpts = &openrouter.SearchOptions{Engine: s.Config.Search.Engine, Mode: mode, MaxResults: maxResults}
	}
	var text strings.Builder
	err = client.StreamChat(ctx, s.anonInput(locale, history, web), maxOut, effort, anonSession(), searchOpts, nil, func(event map[string]any) error {
		if failErr := failedEvent(event); failErr != nil {
			return failErr
		}
		delta := chatTextDelta(event)
		if delta == "" {
			return nil
		}
		text.WriteString(delta)
		if reason := Check(text.String()); reason != "" {
			return &repetitionAbort{reason: reason}
		}
		if onDelta != nil {
			return onDelta(delta)
		}
		return nil
	})
	out := strings.TrimSpace(text.String())
	if _, abort := err.(*repetitionAbort); abort && out != "" {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	return out, nil
}

func (s *Service) anonInput(locale i18n.Locale, history []AnonTurn, search bool) []any {
	loc := s.zone()
	now := time.Now().In(loc)
	date := "Current date: " + now.Format("2006-01-02 Monday") + " (" + loc.String() + ")."
	out := []any{
		map[string]any{"role": "system", "content": i18n.T(locale, "chat.system_prompt_anon")},
		map[string]any{"role": "system", "content": date},
	}
	var turns []any
	for _, m := range history {
		role := m.Role
		if role != store.RoleUser && role != store.RoleAssistant {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		if len(text) > anonMaxChars {
			text = text[:anonMaxChars]
		}
		turns = append(turns, map[string]any{"role": role, "content": text})
	}
	if len(turns) > anonMaxTurns {
		turns = turns[len(turns)-anonMaxTurns:]
	}
	out = append(out, turns...)
	out = append(out, s.turnNote(locale, search))
	return out
}

func anonSession() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "kura-anon"
	}
	return "kura-anon-" + hex.EncodeToString(buf)
}
