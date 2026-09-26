package handler

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuracalendar/internal/calendar"
	"github.com/aquasp/kuracalendar/internal/i18n"
	"github.com/aquasp/kuracalendar/internal/store"
	"github.com/go-chi/chi/v5"
)

const (
	apiUserKey  ctxKey = "api_user"
	apiTokenKey ctxKey = "api_token"
)

func apiUserOf(r *http.Request) *store.User {
	u, _ := r.Context().Value(apiUserKey).(*store.User)
	return u
}

func apiTokenOf(r *http.Request) *store.APIToken {
	t, _ := r.Context().Value(apiTokenKey).(*store.APIToken)
	return t
}

// requireAPIToken authenticates the Bearer [REDACTED] It works while the app is
// locked, like the Rails API.
func (s *Server) requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Mirrors the Rails sub(/\ABearer\s+/i, ""): the prefix is
		// optional, so a bare token still authenticates.
		h := strings.TrimSpace(r.Header.Get("Authorization"))
		if len(h) > 6 && strings.EqualFold(h[:6], "bearer") && (h[6] == ' ' || h[6] == '\t') {
			h = strings.TrimSpace(h[6:])
		}
		tok, err := s.Store.AuthenticateToken(h)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		user, err := s.Store.FindUser(tok.UserID)
		if err != nil {
			writeAPIError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		_ = s.Store.TouchTokenLastUsed(tok.ID)
		ctx := context.WithValue(r.Context(), apiTokenKey, tok)
		ctx = context.WithValue(ctx, apiUserKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func writeAPIError(w http.ResponseWriter, status int, code string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code})
}

func writeAPIErrors(w http.ResponseWriter, status int, errs []string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string][]string{"errors": errs})
}

func writeAPIJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeAPIPayload accepts nested ({"event": {...}}) or flat ({"title":
// ...}) params, like the Rails controllers. Form posts work too.
func decodeAPIPayload(r *http.Request, nested string) map[string]any {
	var payload map[string]any
	_ = json.NewDecoder(io.LimitReader(r.Body, 2<<20)).Decode(&payload)
	if payload == nil {
		_ = r.ParseForm()
		out := map[string]any{}
		for k, v := range r.PostForm {
			if len(v) > 0 {
				out[k] = v[0]
			}
		}
		return out
	}
	if inner, ok := payload[nested].(map[string]any); ok {
		return inner
	}
	return payload
}

func apiString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == math.Trunc(t) && math.Abs(t) < 1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return ""
	}
}

// apiAllDay mirrors the Rails boolean cast ("0"/"false"/"no"/"off" are
// false, everything else true).
func apiAllDay(v any) bool {
	switch t := v.(type) {
	case nil:
		return true
	case bool:
		return t
	case float64:
		return t != 0
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "0", "f", "false", "n", "no", "off":
			return false
		}
		return true
	default:
		return true
	}
}

func apiEvent(n *store.Event, occurrenceOn string) map[string]any {
	var startsAt, endsAt, repeatUntil any
	if n.StartsAt != "" {
		startsAt = n.StartsAt
	}
	if n.EndsAt != "" {
		endsAt = n.EndsAt
	}
	if n.RepeatUntil != "" {
		repeatUntil = n.RepeatUntil
	}
	return map[string]any{
		"id":            n.ID,
		"title":         n.Title,
		"body":          n.Body,
		"all_day":       n.AllDay,
		"starts_on":     n.StartsOn,
		"ends_on":       n.EndsOn,
		"starts_at":     startsAt,
		"ends_at":       endsAt,
		"emoji":         n.Emoji,
		"repeat":        n.Repeat,
		"repeat_until":  repeatUntil,
		"occurrence_on": occurrenceOn,
		"created_at":    n.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at":    n.UpdatedAt.UTC().Format(time.RFC3339),
	}
}

func apiBirthday(b *store.Birthday) map[string]any {
	var year any
	if b.Year != 0 {
		year = b.Year
	}
	return map[string]any{
		"id":    b.ID,
		"name":  b.Name,
		"month": b.Month,
		"day":   b.Day,
		"year":  year,
		"body":  b.Body,
		"emoji": b.Emoji,
	}
}

func (s *Server) apiWriteLimited(w http.ResponseWriter, r *http.Request) bool {
	tok := apiTokenOf(r)
	if !s.Limiter.Allow("api:"+strconv.FormatInt(tok.ID, 10), 60, time.Minute) {
		writeAPIError(w, http.StatusTooManyRequests, "rate_limited")
		return false
	}
	return true
}

// dateRangeParams mirrors the Rails date_range_params: explicit from/to or
// the current month, clamped to RANGE_MAX days.
func dateRangeParams(r *http.Request) (string, string, bool) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	monthEnd := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	fromS := strings.TrimSpace(r.URL.Query().Get("from"))
	toS := strings.TrimSpace(r.URL.Query().Get("to"))
	if fromS == "" {
		fromS = monthStart.Format("2006-01-02")
	}
	if toS == "" {
		toS = monthEnd.Format("2006-01-02")
	}
	from, ok := store.ParseDate(fromS)
	if !ok {
		return "", "", false
	}
	to, ok := store.ParseDate(toS)
	if !ok {
		return "", "", false
	}
	if int(to.Sub(from).Hours()/24) > store.EventRangeMax {
		to = from.AddDate(0, 0, store.EventRangeMax)
	}
	return from.Format("2006-01-02"), to.Format("2006-01-02"), true
}

func (s *Server) handleAPIEventsIndex(w http.ResponseWriter, r *http.Request) {
	from, to, ok := dateRangeParams(r)
	if !ok {
		writeAPIErrors(w, http.StatusUnprocessableEntity,
			[]string{i18n.T(LocaleOf(r), "api.invalid_date")})
		return
	}
	fromT, _ := store.ParseDate(from)
	toT, _ := store.ParseDate(to)
	occs, err := calendar.Occurrences(s.Store, apiUserOf(r).ID, fromT, toT)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	// One entry per occurrence: series rows render with occurrence dates,
	// and (id, occurrence_on) is unique within the list.
	out := make([]any, 0, len(occs))
	for _, o := range occs {
		cp := *o.Event
		cp.StartsOn = o.Start.Format("2006-01-02")
		cp.EndsOn = o.End.Format("2006-01-02")
		out = append(out, apiEvent(&cp, cp.StartsOn))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"events": out})
}

func (s *Server) handleAPIEventsShow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	e, err := s.Store.FindEvent(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"event": apiEvent(e, e.StartsOn)})
}

func eventInputFromPayload(p map[string]any, base store.EventInput) store.EventInput {
	in := base
	if v, ok := p["title"]; ok {
		in.Title = apiString(v)
	}
	if v, ok := p["body"]; ok {
		in.Body = apiString(v)
	}
	if v, ok := p["all_day"]; ok {
		in.AllDay = apiAllDay(v)
	}
	if v, ok := p["starts_on"]; ok {
		in.StartsOn = apiString(v)
	}
	if v, ok := p["ends_on"]; ok {
		in.EndsOn = apiString(v)
	}
	if v, ok := p["starts_at"]; ok {
		in.StartsAt = apiString(v)
	}
	if v, ok := p["ends_at"]; ok {
		in.EndsAt = apiString(v)
	}
	if v, ok := p["emoji"]; ok {
		in.Emoji = apiString(v)
	}
	if v, ok := p["repeat"]; ok {
		in.Repeat = apiString(v)
	}
	if v, ok := p["repeat_until"]; ok {
		in.RepeatUntil = apiString(v)
	}
	return in
}

func (s *Server) handleAPIEventsCreate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	in := eventInputFromPayload(decodeAPIPayload(r, "event"), store.EventInput{AllDay: true})
	e, errs, err := s.Store.CreateEvent(apiUserOf(r).ID, in)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"event": apiEvent(e, e.StartsOn)})
}

func (s *Server) handleAPIEventsUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	current, err := s.Store.FindEvent(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	base := store.EventInput{Title: current.Title, Body: current.Body, AllDay: current.AllDay,
		StartsOn: current.StartsOn, EndsOn: current.EndsOn,
		StartsAt: current.StartsAt, EndsAt: current.EndsAt,
		Emoji: current.Emoji, Repeat: current.Repeat, RepeatUntil: current.RepeatUntil}
	in := eventInputFromPayload(decodeAPIPayload(r, "event"), base)
	e, errs, err := s.Store.UpdateEvent(apiUserOf(r).ID, id, in)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"event": apiEvent(e, e.StartsOn)})
}

func (s *Server) handleAPIEventsDestroy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindEvent(apiUserOf(r).ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeleteEvent(apiUserOf(r).ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAPIBirthdaysIndex(w http.ResponseWriter, r *http.Request) {
	birthdays, err := s.Store.ListBirthdays(apiUserOf(r).ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	out := make([]any, 0, len(birthdays))
	for _, b := range birthdays {
		out = append(out, apiBirthday(b))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"birthdays": out})
}

func (s *Server) handleAPIBirthdaysShow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	b, err := s.Store.FindBirthday(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"birthday": apiBirthday(b)})
}

func birthdayInputFromPayload(p map[string]any, base store.BirthdayInput) store.BirthdayInput {
	in := base
	if v, ok := p["name"]; ok {
		in.Name = apiString(v)
	}
	if v, ok := p["month"]; ok {
		in.Month = apiString(v)
	}
	if v, ok := p["day"]; ok {
		in.Day = apiString(v)
	}
	if v, ok := p["year"]; ok {
		in.Year = apiString(v)
	}
	if v, ok := p["body"]; ok {
		in.Body = apiString(v)
	}
	if v, ok := p["emoji"]; ok {
		in.Emoji = apiString(v)
	}
	return in
}

func (s *Server) handleAPIBirthdaysCreate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	in := birthdayInputFromPayload(decodeAPIPayload(r, "birthday"), store.BirthdayInput{})
	b, errs, err := s.Store.CreateBirthday(apiUserOf(r).ID, in)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	writeAPIJSON(w, http.StatusCreated, map[string]any{"birthday": apiBirthday(b)})
}

func (s *Server) handleAPIBirthdaysUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	current, err := s.Store.FindBirthday(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	base := store.BirthdayInput{Name: current.Name,
		Month: strconv.Itoa(current.Month), Day: strconv.Itoa(current.Day),
		Body: current.Body, Emoji: current.Emoji}
	if current.Year != 0 {
		base.Year = strconv.Itoa(current.Year)
	}
	in := birthdayInputFromPayload(decodeAPIPayload(r, "birthday"), base)
	b, errs, err := s.Store.UpdateBirthday(apiUserOf(r).ID, id, in)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"birthday": apiBirthday(b)})
}

func (s *Server) handleAPIBirthdaysDestroy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindBirthday(apiUserOf(r).ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeleteBirthday(apiUserOf(r).ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}
