package chat

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aquasp/kurachat/internal/i18n"
	"github.com/aquasp/kurachat/internal/suite"
)

// healthTTL dedupes the probe that builds the prompt and the probe that
// decides which tools to attach. A later turn probes again.
const healthTTL = 2 * time.Second

type healthSnap struct {
	at   time.Time
	rows []suite.Health
}

type healthCache struct {
	mu sync.Mutex
	by map[int64]healthSnap
}

// AppHealth reports People, Spend, Calendar, and Notes for this user.
// Only HealthOK apps may be offered as tools.
func (s *Service) AppHealth(ctx context.Context, userID int64) []suite.Health {
	if rows, ok := s.healthFresh(userID); ok {
		return rows
	}
	rows := s.probeApps(ctx, userID)
	s.healthPut(userID, rows)
	return rows
}

func (s *Service) healthFresh(userID int64) ([]suite.Health, bool) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	snap, ok := s.health.by[userID]
	if !ok || time.Since(snap.at) > healthTTL {
		return nil, false
	}
	return append([]suite.Health(nil), snap.rows...), true
}

func (s *Service) healthPut(userID int64, rows []suite.Health) {
	s.health.mu.Lock()
	defer s.health.mu.Unlock()
	if s.health.by == nil {
		s.health.by = map[int64]healthSnap{}
	}
	s.health.by[userID] = healthSnap{at: time.Now(), rows: append([]suite.Health(nil), rows...)}
}

func (s *Service) probeApps(ctx context.Context, userID int64) []suite.Health {
	byName := map[string]suite.App{}
	for _, a := range s.SuiteApps {
		byName[a.Name] = a
	}
	const (
		linkMissing = iota
		linkOK
		linkBroken
	)
	links := map[string]int{}
	tokens := map[string]string{}
	if len(s.SuiteKey) == 32 && s.Store != nil {
		if rows, err := s.Store.ListAppLinks(userID); err == nil {
			for _, row := range rows {
				raw, err := suite.Decrypt(s.SuiteKey, row.Token)
				if err != nil || len(raw) == 0 {
					links[row.App] = linkBroken
					continue
				}
				links[row.App] = linkOK
				tokens[row.App] = string(raw)
			}
		}
	}
	out := make([]suite.Health, len(suite.AppOrder))
	var wg sync.WaitGroup
	for i, name := range suite.AppOrder {
		app, configured := byName[name]
		if !configured || app.Base == "" {
			out[i] = suite.Health{App: name, State: suite.HealthUnconfigured}
			continue
		}
		switch links[name] {
		case linkBroken:
			out[i] = suite.Health{App: name, State: suite.HealthBroken}
			continue
		case linkMissing:
			out[i] = suite.Health{App: name, State: suite.HealthOff}
			continue
		}
		token := tokens[name]
		wg.Add(1)
		go func(i int, name, token string, app suite.App) {
			defer wg.Done()
			out[i] = suite.Probe(ctx, suite.Client{App: app, Token: token})
			out[i].App = name
		}(i, name, token, app)
	}
	wg.Wait()
	return out
}

// connectionNote is the per-turn status the model must trust.
func (s *Service) connectionNote(userID int64, locale i18n.Locale) any {
	return map[string]any{
		"role":    "system",
		"content": HealthNote(locale, s.AppHealth(context.Background(), userID)),
	}
}

// HealthNote renders the runtime connection list for the model.
func HealthNote(locale i18n.Locale, rows []suite.Health) string {
	var b strings.Builder
	b.WriteString(i18n.T(locale, "chat.health_intro"))
	b.WriteString("\n")
	for _, row := range rows {
		label := i18n.T(locale, "apps."+row.App)
		var line string
		switch row.State {
		case suite.HealthOK:
			line = i18n.T(locale, "chat.health_ok")
		case suite.HealthAuth:
			line = i18n.T(locale, "chat.health_auth", "detail", row.Detail)
		case suite.HealthDown:
			line = i18n.T(locale, "chat.health_down", "detail", row.Detail)
		case suite.HealthBroken:
			line = i18n.T(locale, "chat.health_broken")
		case suite.HealthUnconfigured:
			line = i18n.T(locale, "chat.health_unconfigured")
		default:
			line = i18n.T(locale, "chat.health_off")
		}
		b.WriteString("- " + label + ": " + line + "\n")
	}
	b.WriteString(i18n.T(locale, "chat.health_rule"))
	return b.String()
}

// HealthyApps returns the names whose probe succeeded.
func HealthyApps(rows []suite.Health) []string {
	var out []string
	for _, row := range rows {
		if row.State == suite.HealthOK {
			out = append(out, row.App)
		}
	}
	return out
}
