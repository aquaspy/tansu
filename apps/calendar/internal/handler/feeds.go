package handler

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aquasp/kuracalendar/internal/i18n"
	"github.com/aquasp/kuracalendar/internal/ics"
	"github.com/aquasp/kuracalendar/internal/store"
	"github.com/aquasp/kuracalendar/internal/views"
	"github.com/go-chi/chi/v5"
)

func (s *Server) fetchFeed(ctx context.Context, raw string) ([]byte, error) {
	if s.FetchICS != nil {
		return s.FetchICS(ctx, raw)
	}
	return ics.Fetch(ctx, raw)
}

func (s *Server) checkFeedURL(ctx context.Context, raw string) error {
	if s.FetchICS != nil {
		return ics.CheckURLShape(raw)
	}
	return ics.CheckURL(ctx, raw, nil)
}

func (s *Server) handleFeedsIndex(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	list, err := s.Store.ListICSFeeds(user.ID)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.feeds"), "auth-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.FeedsPage(p, list)))
}

func (s *Server) handleFeedsCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("ics:"+itoa64(user.ID), 20, time.Minute) {
		s.feedsFail(w, r, i18n.T(l, "auth.too_many"), http.StatusTooManyRequests)
		return
	}
	name := r.FormValue("name")
	rawURL := r.FormValue("url")
	if err := s.checkFeedURL(r.Context(), rawURL); err != nil {
		msg := i18n.T(l, "feeds.url_invalid")
		if errors.Is(err, ics.ErrBlocked) {
			msg = i18n.T(l, "feeds.err_blocked")
		} else if errors.Is(err, ics.ErrFetch) {
			msg = i18n.T(l, "feeds.err_fetch")
		}
		s.feedsFail(w, r, msg, http.StatusUnprocessableEntity)
		return
	}
	feed, err := s.Store.CreateICSFeed(user.ID, name, rawURL)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrICSNameBlank):
			s.feedsFail(w, r, i18n.T(l, "feeds.name_blank"), http.StatusUnprocessableEntity)
		case errors.Is(err, store.ErrICSTooMany):
			s.feedsFail(w, r, i18n.T(l, "feeds.too_many"), http.StatusUnprocessableEntity)
		default:
			s.feedsFail(w, r, i18n.T(l, "feeds.url_invalid"), http.StatusUnprocessableEntity)
		}
		return
	}
	if err := ics.SyncFeed(r.Context(), s.Store, feed, s.fetchFeed, time.Now(), time.Local); err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	s.flashFeed(w, r, feed.ID, i18n.T(l, "feeds.added"))
}

func (s *Server) handleFeedsRefresh(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	feed, ok := s.ownedFeed(w, r, user.ID)
	if !ok {
		return
	}
	if !s.Limiter.Allow("ics:"+itoa64(user.ID), 20, time.Minute) {
		flashAlert(s, r, i18n.T(l, "auth.too_many"))
		http.Redirect(w, r, "/feeds", http.StatusSeeOther)
		return
	}
	if err := ics.SyncFeed(r.Context(), s.Store, feed, s.fetchFeed, time.Now(), time.Local); err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	s.flashFeed(w, r, feed.ID, i18n.T(l, "feeds.refreshed"))
}

func (s *Server) handleFeedsPause(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	feed, ok := s.ownedFeed(w, r, user.ID)
	if !ok {
		return
	}
	if err := s.Store.SetICSFeedPaused(user.ID, feed.ID, !feed.Paused); err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	msg := i18n.T(l, "feeds.paused_done")
	if feed.Paused {
		msg = i18n.T(l, "feeds.resumed")
	}
	flashNotice(s, r, msg)
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}

func (s *Server) handleFeedsDestroy(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	feed, ok := s.ownedFeed(w, r, user.ID)
	if !ok {
		return
	}
	if err := s.Store.DeleteICSFeed(user.ID, feed.ID); err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	s.Store.ReclaimSpace()
	flashNotice(s, r, i18n.T(l, "feeds.removed"))
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}

func (s *Server) ownedFeed(w http.ResponseWriter, r *http.Request, userID int64) (*store.ICSFeed, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return nil, false
	}
	feed, err := s.Store.FindICSFeed(userID, id)
	if err != nil {
		s.notFound(w, r)
		return nil, false
	}
	return feed, true
}

func (s *Server) feedsFail(w http.ResponseWriter, r *http.Request, alert string, status int) {
	list, err := s.Store.ListICSFeeds(UserOf(r).ID)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.feeds"), "auth-body")
	p.Alert = alert
	render(w, r, status, views.Layout(p, views.NoHead(), views.FeedsPage(p, list)))
}

func (s *Server) flashFeed(w http.ResponseWriter, r *http.Request, id int64, okMsg string) {
	feed, err := s.Store.FindICSFeed(UserOf(r).ID, id)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	if feed.LastError != "" && feed.LastError != "capped" {
		flashAlert(s, r, i18n.T(LocaleOf(r), feed.ErrorKey()))
	} else {
		flashNotice(s, r, okMsg)
	}
	http.Redirect(w, r, "/feeds", http.StatusSeeOther)
}
