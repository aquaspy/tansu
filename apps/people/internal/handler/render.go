package handler

import (
	"net/http"
	"os"

	"github.com/a-h/templ"
	"github.com/aquasp/kurapeople/internal/views"
)

// page builds the shared page model with flash + CSRF.
func (s *Server) page(w http.ResponseWriter, r *http.Request, title, bodyClass string) views.Page {
	flash := FlashOf(r)
	return views.Page{
		L:          LocaleOf(r),
		Title:      title,
		BodyClass:  bodyClass,
		CSRF:       s.CSRFToken(w, r),
		Notice:     flash.Notice,
		Alert:      flash.Alert,
		KuraLogin:  s.Config.AccountEnabled(),
		AccountURL: s.Config.KuraAccountURL,
	}
}

func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(r.Context(), w)
}

// notFound serves the static 404 page (plain fallback when absent).
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	if data, err := os.ReadFile(s.WebDir + "/404.html"); err == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write(data)
		return
	}
	http.NotFound(w, r)
}

// hxRedirect navigates htmx-driven requests; plain requests get a 303.
func hxRedirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func flashNotice(s *Server, r *http.Request, notice string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, notice, "")
	}
}

func flashAlert(s *Server, r *http.Request, alert string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, "", alert)
	}
}
