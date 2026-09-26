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

	"github.com/aquasp/kurapeople/internal/store"
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
// locked, like the sibling apps' APIs.
func (s *Server) requireAPIToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// The Bearer prefix is optional, so a bare token still
		// authenticates.
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

// decodeAPIPayload accepts nested ({"person": {...}}) or flat ({"name":
// ...}) params, like the sibling apps. Form posts work too.
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

// apiMonthDay renders an optional month/day pair (nulls when unknown).
func apiMonthDay(month, day int) map[string]any {
	var m, d any
	if month != 0 {
		m = month
	}
	if day != 0 {
		d = day
	}
	return map[string]any{"month": m, "day": d}
}

func apiPerson(p *store.Person, attrs []*store.Attr, today time.Time) map[string]any {
	birthday := apiMonthDay(p.BirthMonth, p.BirthDay)
	var year any
	if p.BirthYear != 0 {
		year = p.BirthYear
	}
	birthday["year"] = year
	outAttrs := make([]any, 0, len(attrs))
	for _, a := range attrs {
		outAttrs = append(outAttrs, map[string]any{"label": a.Label, "value": a.Value})
	}
	var countdown any
	if next, ok := p.NextBirthday(today); ok {
		days, _ := p.DaysUntilBirthday(today)
		cd := map[string]any{
			"days_until_birthday": days,
			"next_birthday":       next.Format("2006-01-02"),
		}
		if age, ok := p.AgeIn(next.Year()); ok {
			cd["turns"] = age
		} else {
			cd["turns"] = nil
		}
		countdown = cd
	}
	return map[string]any{
		"id":           p.ID,
		"name":         p.Name,
		"nickname":     p.Nickname,
		"relationship": p.Relationship,
		"birthday":     birthday,
		"emoji":        p.Emoji,
		"phone":        p.Phone,
		"email":        p.Email,
		"address":      p.Address,
		"sizes": map[string]any{
			"ring": p.RingSize, "shoe": p.ShoeSize,
			"shirt": p.ShirtSize, "pants": p.PantsSize,
		},
		"height":     p.Height,
		"favorites":  p.Favorites,
		"notes":      p.Notes,
		"attrs":      outAttrs,
		"countdown":  countdown,
		"created_at": p.CreatedAt.UTC().Format(time.RFC3339),
		"updated_at": p.UpdatedAt.UTC().Format(time.RFC3339),
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

func apiLimit(r *http.Request) int {
	n, err := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
	if err != nil || n <= 0 {
		return 50
	}
	if n > 200 {
		return 200
	}
	return n
}

func (s *Server) handleAPIPeopleIndex(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	month, _ := strconv.Atoi(strings.TrimSpace(q.Get("birthday_month")))
	people, err := s.Store.SearchPeople(apiUserOf(r).ID,
		q.Get("q"), q.Get("relationship"), month, apiLimit(r))
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	today := time.Now()
	out := make([]any, 0, len(people))
	for _, p := range people {
		attrs, err := s.Store.ListAttrs(p.ID)
		if err != nil {
			writeAPIError(w, http.StatusInternalServerError, "unavailable")
			return
		}
		out = append(out, apiPerson(p, attrs, today))
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"people": out})
}

func (s *Server) handleAPIPeopleShow(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	p, err := s.Store.FindPerson(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	attrs, err := s.Store.ListAttrs(p.ID)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	writeAPIJSON(w, http.StatusOK, map[string]any{"person": apiPerson(p, attrs, time.Now())})
}

func personInputFromPayload(p map[string]any, base store.PersonInput) store.PersonInput {
	in := base
	if v, ok := p["name"]; ok {
		in.Name = apiString(v)
	}
	if v, ok := p["nickname"]; ok {
		in.Nickname = apiString(v)
	}
	if v, ok := p["relationship"]; ok {
		in.Relationship = apiString(v)
	}
	if v, ok := p["emoji"]; ok {
		in.Emoji = apiString(v)
	}
	if v, ok := p["phone"]; ok {
		in.Phone = apiString(v)
	}
	if v, ok := p["email"]; ok {
		in.Email = apiString(v)
	}
	if v, ok := p["address"]; ok {
		in.Address = apiString(v)
	}
	if v, ok := p["favorites"]; ok {
		in.Favorites = apiString(v)
	}
	if v, ok := p["notes"]; ok {
		in.Notes = apiString(v)
	}
	// Birthday accepts a nested {"month","day","year"} object or
	// flat birthday_month/birthday_day/birthday_year keys.
	if obj, ok := p["birthday"].(map[string]any); ok {
		in.BirthMonth = apiString(obj["month"])
		in.BirthDay = apiString(obj["day"])
		in.BirthYear = apiString(obj["year"])
	} else {
		if v, ok := p["birthday_month"]; ok {
			in.BirthMonth = apiString(v)
		}
		if v, ok := p["birthday_day"]; ok {
			in.BirthDay = apiString(v)
		}
		if v, ok := p["birthday_year"]; ok {
			in.BirthYear = apiString(v)
		}
	}
	// Sizes accept a nested object or flat ring_size/shoe_size/... keys.
	if obj, ok := p["sizes"].(map[string]any); ok {
		in.RingSize = apiString(obj["ring"])
		in.ShoeSize = apiString(obj["shoe"])
		in.ShirtSize = apiString(obj["shirt"])
		in.PantsSize = apiString(obj["pants"])
	} else {
		if v, ok := p["ring_size"]; ok {
			in.RingSize = apiString(v)
		}
		if v, ok := p["shoe_size"]; ok {
			in.ShoeSize = apiString(v)
		}
		if v, ok := p["shirt_size"]; ok {
			in.ShirtSize = apiString(v)
		}
		if v, ok := p["pants_size"]; ok {
			in.PantsSize = apiString(v)
		}
	}
	if v, ok := p["height"]; ok {
		in.Height = apiString(v)
	}
	return in
}

// attrsFromPayload reads the optional attrs array. present is false
// when the key is absent (leave rows untouched on update).
func attrsFromPayload(p map[string]any) (attrs []store.AttrInput, present bool) {
	raw, ok := p["attrs"]
	if !ok {
		return nil, false
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, true
	}
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		attrs = append(attrs, store.AttrInput{
			Label: apiString(obj["label"]),
			Value: apiString(obj["value"]),
		})
	}
	return attrs, true
}

func personInputFromRecord(p *store.Person) store.PersonInput {
	in := store.PersonInput{
		Name: p.Name, Nickname: p.Nickname, Relationship: p.Relationship,
		Emoji: p.Emoji, Phone: p.Phone, Email: p.Email, Address: p.Address,
		RingSize: p.RingSize, ShoeSize: p.ShoeSize,
		ShirtSize: p.ShirtSize, PantsSize: p.PantsSize, Height: p.Height,
		Favorites: p.Favorites, Notes: p.Notes,
	}
	if p.BirthMonth != 0 {
		in.BirthMonth = strconv.Itoa(p.BirthMonth)
	}
	if p.BirthDay != 0 {
		in.BirthDay = strconv.Itoa(p.BirthDay)
	}
	if p.BirthYear != 0 {
		in.BirthYear = strconv.Itoa(p.BirthYear)
	}
	return in
}

func (s *Server) handleAPIPeopleCreate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	payload := decodeAPIPayload(r, "person")
	in := personInputFromPayload(payload, store.PersonInput{})
	person, errs, err := s.Store.CreatePerson(apiUserOf(r).ID, in)
	if err != nil {
		writeAPIError(w, http.StatusInternalServerError, "unavailable")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	if attrs, present := attrsFromPayload(payload); present {
		if attrErrs, err := s.Store.ReplaceAttrs(person.ID, attrs); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "unavailable")
			return
		} else if len(attrErrs) > 0 {
			writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, attrErrs))
			return
		}
	}
	attrs, _ := s.Store.ListAttrs(person.ID)
	writeAPIJSON(w, http.StatusCreated, map[string]any{"person": apiPerson(person, attrs, time.Now())})
}

func (s *Server) handleAPIPeopleUpdate(w http.ResponseWriter, r *http.Request) {
	if !s.apiWriteLimited(w, r) {
		return
	}
	l := LocaleOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	current, err := s.Store.FindPerson(apiUserOf(r).ID, id)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	payload := decodeAPIPayload(r, "person")
	in := personInputFromPayload(payload, personInputFromRecord(current))
	person, errs, err := s.Store.UpdatePerson(apiUserOf(r).ID, id, in)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if len(errs) > 0 {
		writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, errs))
		return
	}
	if attrs, present := attrsFromPayload(payload); present {
		if attrErrs, err := s.Store.ReplaceAttrs(person.ID, attrs); err != nil {
			writeAPIError(w, http.StatusInternalServerError, "unavailable")
			return
		} else if len(attrErrs) > 0 {
			writeAPIErrors(w, http.StatusUnprocessableEntity, errorMessages(l, attrErrs))
			return
		}
	}
	attrs, _ := s.Store.ListAttrs(person.ID)
	writeAPIJSON(w, http.StatusOK, map[string]any{"person": apiPerson(person, attrs, time.Now())})
}

func (s *Server) handleAPIPeopleDestroy(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	if _, err := s.Store.FindPerson(apiUserOf(r).ID, id); err != nil {
		writeAPIError(w, http.StatusNotFound, "not_found")
		return
	}
	_ = s.Store.DeletePerson(apiUserOf(r).ID, id)
	s.Store.ReclaimSpace()
	w.WriteHeader(http.StatusNoContent)
}
