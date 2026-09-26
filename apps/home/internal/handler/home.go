package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurahome/internal/i18n"
	"github.com/aquasp/kurahome/internal/iconfetch"
	"github.com/aquasp/kurahome/internal/store"
	"github.com/aquasp/kurahome/internal/views"
	"github.com/go-chi/chi/v5"
)

// fullSentence renders validation errors like Rails full_messages.to_sentence.
func fullSentence(l i18n.Locale, model string, errs []*store.ValError) string {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		if e.Attr == "" {
			msgs = append(msgs, i18n.T(l, "err."+model+".base."+e.Code))
			continue
		}
		key := "err." + model + "." + e.Attr + "." + e.Code
		msg := i18n.T(l, key, "count", strconv.Itoa(e.Count))
		if msg == key { // fall back to the generic message table
			msg = i18n.T(l, "err."+e.Code, "count", strconv.Itoa(e.Count))
		}
		msgs = append(msgs, i18n.T(l, "attr."+model+"."+e.Attr)+" "+msg)
	}
	return i18n.Sentence(l, msgs)
}

// currentProfile mirrors the CurrentProfile concern: ?profile_id wins,
// then the stored session profile, then the first profile. It backfills
// a default profile for users that have none.
func (s *Server) currentProfile(w http.ResponseWriter, r *http.Request) ([]*store.Profile, *store.Profile, error) {
	user := UserOf(r)
	l := LocaleOf(r)
	if err := s.ensureHomeProfile(user.ID, l); err != nil {
		return nil, nil, err
	}
	profiles, err := s.Store.ListProfiles(user.ID)
	if err != nil || len(profiles) == 0 {
		return nil, nil, err
	}
	want := r.URL.Query().Get("profile_id")
	if want == "" {
		if sess := SessionOf(r); sess != nil && sess.ProfileID != 0 {
			want = strconv.FormatInt(sess.ProfileID, 10)
		}
	}
	profile := profiles[0]
	if want != "" {
		for _, p := range profiles {
			if strconv.FormatInt(p.ID, 10) == want {
				profile = p
				break
			}
		}
	}
	if sess := SessionOf(r); sess != nil && sess.ProfileID != profile.ID {
		_ = s.Store.SetSessionProfile(sess.ID, profile.ID)
		sess.ProfileID = profile.ID
	}
	return profiles, profile, nil
}

// afterProfilePath mirrors CurrentProfile#after_profile_path.
func afterProfilePath(r *http.Request, profileID int64) string {
	ref := r.Referer()
	if i := strings.Index(ref, "/stack"); i >= 0 {
		rest := ref[i+len("/stack"):]
		if rest == "" || strings.HasPrefix(rest, "?") || strings.HasPrefix(rest, "#") || strings.HasPrefix(rest, "/") {
			return "/stack?profile_id=" + strconv.FormatInt(profileID, 10)
		}
	}
	return "/?profile_id=" + strconv.FormatInt(profileID, 10)
}

func (s *Server) redirectAlert(w http.ResponseWriter, r *http.Request, to, alert string) {
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetFlash(sess.ID, "", alert)
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

func pathID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}

// --- home ---

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	profiles, profile, err := s.currentProfile(w, r)
	if err != nil {
		http.Error(w, "home unavailable", http.StatusInternalServerError)
		return
	}
	sites, err := s.Store.ListSites(profile.ID)
	if err != nil {
		http.Error(w, "home unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.IndexPage(p, views.HomeData{
		Profiles:   profiles,
		Profile:    profile,
		Sites:      sites,
		AutoLock:   AutoLockEnabled(r),
		OnStack:    false,
		ReorderURL: "/sites/reorder",
	})))
}

func (s *Server) handleStack(w http.ResponseWriter, r *http.Request) {
	profiles, profile, err := s.currentProfile(w, r)
	if err != nil {
		http.Error(w, "home unavailable", http.StatusInternalServerError)
		return
	}
	items, err := s.Store.ListStackItems(profile.ID)
	if err != nil {
		http.Error(w, "home unavailable", http.StatusInternalServerError)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.stack"), "app-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.StackPage(p, views.HomeData{
		Profiles:   profiles,
		Profile:    profile,
		Items:      items,
		AutoLock:   AutoLockEnabled(r),
		OnStack:    true,
		ReorderURL: "/stack_items/reorder",
	})))
}

// --- profiles ---

func (s *Server) handleProfilesCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("profiles:"+itoa64(user.ID), 30, time.Minute) {
		s.redirectAlert(w, r, "/", i18n.T(l, "auth.too_many"))
		return
	}
	name := r.FormValue("profile[name]")
	if errs := s.Store.ValidateProfile(user.ID, 0, name, true); len(errs) > 0 {
		first, _ := s.Store.ListProfiles(user.ID)
		fallback := int64(0)
		if len(first) > 0 {
			fallback = first[0].ID
		}
		s.redirectAlert(w, r, afterProfilePath(r, fallback), fullSentence(l, "profile", errs))
		return
	}
	profile, err := s.Store.CreateProfile(user.ID, name, s.Store.MaxProfilePosition(user.ID)+1)
	if err != nil {
		s.redirectAlert(w, r, "/", i18n.T(l, "auth.too_many"))
		return
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetSessionProfile(sess.ID, profile.ID)
	}
	http.Redirect(w, r, afterProfilePath(r, profile.ID), http.StatusSeeOther)
}

func (s *Server) handleProfilesUpdate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("profiles:"+itoa64(user.ID), 30, time.Minute) {
		s.redirectAlert(w, r, afterProfilePath(r, id), i18n.T(l, "auth.too_many"))
		return
	}
	profile, err := s.Store.FindProfile(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	name := r.FormValue("profile[name]")
	if errs := s.Store.ValidateProfile(user.ID, id, name, false); len(errs) > 0 {
		s.redirectAlert(w, r, afterProfilePath(r, id), fullSentence(l, "profile", errs))
		return
	}
	_ = s.Store.UpdateProfile(profile.ID, name)
	http.Redirect(w, r, afterProfilePath(r, profile.ID), http.StatusSeeOther)
}

// handleProfilesPost serves plain HTML forms, which can only POST: the
// dialog sets _method=patch, delete forms set _method=delete.
func (s *Server) handleProfilesPost(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.FormValue("_method"), "delete") {
		s.handleProfilesDestroy(w, r)
		return
	}
	s.handleProfilesUpdate(w, r)
}

func (s *Server) handleProfilesDestroy(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	profile, err := s.Store.FindProfile(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Store.ProfileDestroyable(user.ID, id) {
		s.redirectAlert(w, r, afterProfilePath(r, profile.ID), i18n.T(l, "app.last_profile"))
		return
	}
	_ = s.Store.DeleteProfile(profile.ID)
	rest, _ := s.Store.ListProfiles(user.ID)
	fallback := profile.ID
	if len(rest) > 0 {
		fallback = rest[0].ID
	}
	if sess := SessionOf(r); sess != nil {
		_ = s.Store.SetSessionProfile(sess.ID, fallback)
	}
	http.Redirect(w, r, afterProfilePath(r, fallback), http.StatusSeeOther)
}

// --- sites ---

func siteFromForm(r *http.Request) *store.Site {
	return &store.Site{
		Title:   r.FormValue("site[title]"),
		URL:     r.FormValue("site[url]"),
		Hint:    r.FormValue("site[hint]"),
		IconURL: r.FormValue("site[icon_url]"),
	}
}

func (s *Server) handleSitesCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("sites:"+itoa64(user.ID), 60, time.Minute) {
		s.redirectAlert(w, r, "/", i18n.T(l, "auth.too_many"))
		return
	}
	profileID, _ := strconv.ParseInt(r.FormValue("site[profile_id]"), 10, 64)
	profile, err := s.Store.FindProfile(user.ID, profileID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	site := siteFromForm(r)
	site.ProfileID = profile.ID
	store.NormalizeSite(site)
	if errs := s.Store.ValidateSite(profile.ID, site, true); len(errs) > 0 {
		s.redirectAlert(w, r, "/?profile_id="+itoa64(profile.ID), fullSentence(l, "site", errs))
		return
	}
	site.Position = s.Store.MaxSitePosition(profile.ID) + 1
	if _, err := s.Store.CreateSite(site); err != nil {
		s.redirectAlert(w, r, "/?profile_id="+itoa64(profile.ID), i18n.T(l, "auth.too_many"))
		return
	}
	http.Redirect(w, r, "/?profile_id="+itoa64(profile.ID), http.StatusSeeOther)
}

func (s *Server) handleSitesUpdate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("sites:"+itoa64(user.ID), 60, time.Minute) {
		s.redirectAlert(w, r, "/", i18n.T(l, "auth.too_many"))
		return
	}
	site, err := s.Store.FindOwnedSite(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	form := siteFromForm(r)
	site.Title, site.URL, site.Hint, site.IconURL = form.Title, form.URL, form.Hint, form.IconURL
	store.NormalizeSite(site)
	if errs := s.Store.ValidateSite(site.ProfileID, site, false); len(errs) > 0 {
		s.redirectAlert(w, r, "/?profile_id="+itoa64(site.ProfileID), fullSentence(l, "site", errs))
		return
	}
	_ = s.Store.UpdateSite(site)
	http.Redirect(w, r, "/?profile_id="+itoa64(site.ProfileID), http.StatusSeeOther)
}

func (s *Server) handleSitesPost(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.FormValue("_method"), "delete") {
		s.handleSitesDestroy(w, r)
		return
	}
	s.handleSitesUpdate(w, r)
}

func (s *Server) handleSitesDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	site, err := s.Store.FindOwnedSite(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteSite(site.ID)
	http.Redirect(w, r, "/?profile_id="+itoa64(site.ProfileID), http.StatusSeeOther)
}

func (s *Server) handleSitesReorder(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var ids []int64
	for _, raw := range r.Form["ids[]"] {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	// Also accept ids[]=1&ids[]=2 submitted as ids=1,2 by tests.
	for _, raw := range r.Form["ids"] {
		for _, part := range strings.Split(raw, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	_ = s.Store.ReorderSites(UserOf(r).ID, ids)
	w.WriteHeader(http.StatusOK)
}

// --- stack items ---

func stackItemFromForm(r *http.Request) *store.StackItem {
	return &store.StackItem{
		Category: r.FormValue("stack_item[category]"),
		Choice:   r.FormValue("stack_item[choice]"),
		Origin:   r.FormValue("stack_item[origin]"),
		Note:     r.FormValue("stack_item[note]"),
		URL:      r.FormValue("stack_item[url]"),
		IconURL:  r.FormValue("stack_item[icon_url]"),
	}
}

func (s *Server) handleStackItemsCreate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	if !s.Limiter.Allow("stack:"+itoa64(user.ID), 60, time.Minute) {
		s.redirectAlert(w, r, "/stack", i18n.T(l, "auth.too_many"))
		return
	}
	profileID, _ := strconv.ParseInt(r.FormValue("stack_item[profile_id]"), 10, 64)
	profile, err := s.Store.FindProfile(user.ID, profileID)
	if err != nil {
		s.notFound(w, r)
		return
	}
	item := stackItemFromForm(r)
	item.ProfileID = profile.ID
	store.NormalizeStackItem(item)
	if errs := s.Store.ValidateStackItem(profile.ID, item, true); len(errs) > 0 {
		s.redirectAlert(w, r, "/stack?profile_id="+itoa64(profile.ID), fullSentence(l, "stack", errs))
		return
	}
	item.Position = s.Store.MaxStackItemPosition(profile.ID) + 1
	if _, err := s.Store.CreateStackItem(item); err != nil {
		s.redirectAlert(w, r, "/stack?profile_id="+itoa64(profile.ID), i18n.T(l, "auth.too_many"))
		return
	}
	http.Redirect(w, r, "/stack?profile_id="+itoa64(profile.ID), http.StatusSeeOther)
}

func (s *Server) handleStackItemsUpdate(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if !s.Limiter.Allow("stack:"+itoa64(user.ID), 60, time.Minute) {
		s.redirectAlert(w, r, "/stack", i18n.T(l, "auth.too_many"))
		return
	}
	item, err := s.Store.FindOwnedStackItem(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	form := stackItemFromForm(r)
	item.Category, item.Choice, item.Origin = form.Category, form.Choice, form.Origin
	item.Note, item.URL, item.IconURL = form.Note, form.URL, form.IconURL
	store.NormalizeStackItem(item)
	if errs := s.Store.ValidateStackItem(item.ProfileID, item, false); len(errs) > 0 {
		s.redirectAlert(w, r, "/stack?profile_id="+itoa64(item.ProfileID), fullSentence(l, "stack", errs))
		return
	}
	_ = s.Store.UpdateStackItem(item)
	http.Redirect(w, r, "/stack?profile_id="+itoa64(item.ProfileID), http.StatusSeeOther)
}

func (s *Server) handleStackItemsPost(w http.ResponseWriter, r *http.Request) {
	if strings.EqualFold(r.FormValue("_method"), "delete") {
		s.handleStackItemsDestroy(w, r)
		return
	}
	s.handleStackItemsUpdate(w, r)
}

func (s *Server) handleStackItemsDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	item, err := s.Store.FindOwnedStackItem(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteStackItem(item.ID)
	http.Redirect(w, r, "/stack?profile_id="+itoa64(item.ProfileID), http.StatusSeeOther)
}

func (s *Server) handleStackItemIcon(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := pathID(r)
	if err != nil {
		s.notFound(w, r)
		return
	}
	item, err := s.Store.FindOwnedStackItem(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	src := item.IconSrc()
	if src == "" {
		s.notFound(w, r)
		return
	}
	body, typ, ok := iconfetch.Call(src)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%d", 24*60*60))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func (s *Server) handleStackItemsReorder(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	var ids []int64
	for _, raw := range r.Form["ids[]"] {
		if id, err := strconv.ParseInt(raw, 10, 64); err == nil {
			ids = append(ids, id)
		}
	}
	for _, raw := range r.Form["ids"] {
		for _, part := range strings.Split(raw, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64); err == nil {
				ids = append(ids, id)
			}
		}
	}
	_ = s.Store.ReorderStackItems(UserOf(r).ID, ids)
	w.WriteHeader(http.StatusOK)
}
