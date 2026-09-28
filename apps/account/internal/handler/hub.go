package handler

import (
	"net/http"

	"github.com/aquasp/kuraaccount/internal/i18n"
	"github.com/aquasp/kuraaccount/internal/store"
	"github.com/aquasp/kuraaccount/internal/views"
)

func (s *Server) handleHubIndex(w http.ResponseWriter, r *http.Request) {
	s.renderHub(w, r, http.StatusOK, "", "")
}

func (s *Server) handleTimezone(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	raw := r.FormValue("timezone")
	zone, err := store.NormalizeTimezone(raw)
	if err != nil {
		s.renderHub(w, r, http.StatusUnprocessableEntity, raw, "hub.timezone_invalid")
		return
	}
	if err := s.Store.UpdateUserTimezone(user.ID, zone); err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	flashNotice(s, r, i18n.T(LocaleOf(r), "hub.timezone_saved"))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) renderHub(w http.ResponseWriter, r *http.Request, status int, timezone, timezoneError string) {
	user := UserOf(r)
	clients, err := s.Store.ListClients()
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	linkedIDs, err := s.Store.LinkedClientIDs(user.ID)
	if err != nil {
		http.Error(w, "unavailable", http.StatusInternalServerError)
		return
	}
	linked := map[string]bool{}
	for _, id := range linkedIDs {
		linked[id] = true
	}
	if timezone == "" && timezoneError == "" {
		timezone = user.Timezone
	}
	d := views.HubData{
		Email:         user.Email,
		Clients:       clients,
		Linked:        linked,
		AutoLock:      AutoLockEnabled(r),
		Timezone:      timezone,
		TimezoneError: timezoneError,
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, status, views.Layout(p, views.NoHead(), views.HubPage(p, d)))
}
