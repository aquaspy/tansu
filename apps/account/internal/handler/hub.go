package handler

import (
	"net/http"

	"github.com/aquasp/kuraaccount/internal/views"
)

func (s *Server) handleHubIndex(w http.ResponseWriter, r *http.Request) {
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
	d := views.HubData{
		Email:    user.Email,
		Clients:  clients,
		Linked:   linked,
		AutoLock: AutoLockEnabled(r),
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.HubPage(p, d)))
}
