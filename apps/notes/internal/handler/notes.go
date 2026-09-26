package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"time"

	"github.com/aquasp/kuranotes/internal/i18n"
	"github.com/aquasp/kuranotes/internal/notes"
	"github.com/aquasp/kuranotes/internal/store"
	"github.com/aquasp/kuranotes/internal/views"
	"github.com/go-chi/chi/v5"
)

// shellData loads everything the app shell needs. currentID selects the open
// note (0 for none). hx skips the blank-draft cleanup for fragment swaps,
// mirroring the turbo_frame_request? guard. folder/q are the list filters,
// passed explicitly because the editor form reuses those names for the note.
func (s *Server) shellData(r *http.Request, userID, currentID int64, hx bool, folder, q string) (views.ShellData, *store.Note, error) {
	l := LocaleOf(r)
	if !hx {
		if err := s.Store.DeleteBlankDraftsExcept(userID, currentID); err != nil {
			return views.ShellData{}, nil, err
		}
	}
	var current *store.Note
	if currentID > 0 {
		n, err := s.Store.FindNote(userID, currentID)
		if err != nil {
			return views.ShellData{}, nil, err
		}
		current = n
	}

	rows, err := s.Store.ListNotes(userID, filterFolder(folder), q)
	if err != nil {
		return views.ShellData{}, nil, err
	}
	counts, err := s.Store.FolderCounts(userID)
	if err != nil {
		return views.ShellData{}, nil, err
	}

	d := views.ShellData{
		Query:    q,
		Folder:   folder,
		AutoLock: AutoLockEnabled(r),
		Filters:  filtersValue(folder, q),
	}
	total := 0
	for _, c := range counts {
		total += c
	}
	d.Total = total
	d.Folders = append(d.Folders, &views.FolderItem{
		Name: "all", Label: i18n.T(l, "js.all"), Count: total,
		Active: folder == "" || folder == "all",
	})
	d.Folders = append(d.Folders, &views.FolderItem{
		Name: "", Label: i18n.T(l, "js.inbox"), Count: counts[""],
		Active: folder == "inbox",
	})
	var names []string
	for name := range counts {
		if name != "" {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	d.FolderNames = names
	for _, name := range names {
		d.Folders = append(d.Folders, &views.FolderItem{
			Name: name, Label: name, Count: counts[name], Active: folder == name,
		})
	}

	untitled := i18n.T(l, "js.untitled")
	for _, row := range rows {
		title := row.Title
		if title == "" {
			title = untitled
		}
		d.Notes = append(d.Notes, &views.NoteItem{
			ID: row.ID, Title: title, Preview: row.Preview,
			Active: current != nil && current.ID == row.ID,
		})
	}

	if current != nil {
		detail := &views.NoteDetail{Note: current, Shared: current.Shared()}
		if current.Shared() {
			detail.ShareURL = shareURL(r, current.ShareToken)
		}
		d.Current = detail
	}
	return d, current, nil
}

// filterFolder maps the raw filter param to the store filter.
func filterFolder(folder string) string {
	if folder == "" {
		return "all"
	}
	return folder
}

// filtersValue mirrors note_filters: non-blank folder+q encoded for links.
func filtersValue(folder, q string) string {
	v := url.Values{}
	if folder != "" {
		v.Set("folder", folder)
	}
	if q != "" {
		v.Set("q", q)
	}
	return v.Encode()
}

func isHX(r *http.Request) bool { return r.Header.Get("HX-Request") == "true" }

func (s *Server) handleNotesIndex(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	d, _, err := s.shellData(r, user.ID, 0, isHX(r), r.FormValue("folder"), r.FormValue("q"))
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	if isHX(r) {
		// htmx search: swap the list only, like the Rails search frame.
		p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
		render(w, r, http.StatusOK, views.NoteList(p, d))
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) handleNotesShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	d, _, err := s.shellData(r, user.ID, id, false, r.FormValue("folder"), r.FormValue("q"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.Shell(p, d)))
}

func (s *Server) handleNotesCreate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	folder := r.FormValue("folder")
	switch folder {
	case "", "all", "inbox":
		folder = ""
	}
	note, err := s.Store.OpenDraft(user.ID, folder)
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, views.NoteHref(note.ID, filtersValue(r.FormValue("folder"), r.FormValue("q"))), http.StatusSeeOther)
}

func (s *Server) handleNotesUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	note, err := s.Store.UpdateNote(user.ID, id, r.FormValue("body"), r.FormValue("folder"))
	if err != nil {
		s.notFound(w, r)
		return
	}
	// The filter inputs are filter_* here: folder/body belong to the note.
	folder, q := r.FormValue("filter_folder"), r.FormValue("filter_q")
	if isHX(r) {
		// Autosave: refresh list + folders + picker out-of-band.
		d, _, err := s.shellData(r, user.ID, note.ID, true, folder, q)
		if err != nil {
			http.Error(w, "notes unavailable", http.StatusInternalServerError)
			return
		}
		p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
		render(w, r, http.StatusOK, views.NoteUpdateResponse(p, d))
		return
	}
	http.Redirect(w, r, views.NoteHref(note.ID, filtersValue(folder, q)), http.StatusSeeOther)
}

func (s *Server) handleNotesDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	_ = s.Store.DeleteNote(user.ID, id)
	s.Store.ReclaimSpace()
	if isHX(r) || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, views.NotesHref(r.FormValue("folder"), r.FormValue("q")), http.StatusSeeOther)
}

func (s *Server) handleFolderRename(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	from := r.FormValue("folder")
	to, allowed, err := s.Store.RenameFolder(user.ID, from, r.FormValue("name"))
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	keep := from
	if allowed {
		keep = to
		flashNotice(s, r, i18n.T(l, "app.folder_renamed"))
	} else {
		flashAlert(s, r, i18n.T(l, "app.folder_rename_invalid"))
	}
	hxRedirect(w, r, views.NotesHref(keep, r.FormValue("q")))
}

func (s *Server) handleFolderDestroy(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	key := r.FormValue("folder")
	if key == "" {
		key = r.URL.Query().Get("folder")
	}
	if key == "" || key == "all" {
		http.Redirect(w, r, "/notes/", http.StatusSeeOther)
		return
	}
	count, err := s.Store.DeleteFolder(user.ID, key)
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	s.Store.ReclaimSpace()
	flashNotice(s, r, i18n.T(l, "app.folder_cleared", "count", strconv.FormatInt(count, 10)))
	keep := ""
	if key == "inbox" {
		keep = "inbox"
	}
	if isHX(r) || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, views.NotesHref(keep, r.FormValue("q")), http.StatusSeeOther)
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

func (s *Server) handleNotesExport(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	list, err := s.Store.ExportNotes(user.ID)
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	payload := make([]map[string]string, 0, len(list))
	for _, n := range list {
		payload = append(payload, map[string]string{
			"title":      n.Title,
			"body":       n.Body,
			"folder":     n.Folder,
			"updated_at": n.UpdatedAt.UTC().Format(time.RFC3339),
		})
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		http.Error(w, "notes unavailable", http.StatusInternalServerError)
		return
	}
	filename := fmt.Sprintf("kuranotes-%s.json", time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
	_, _ = w.Write(data)
}

func (s *Server) handleNotesImport(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	fail := func() {
		flashAlert(s, r, i18n.T(l, "app.import_invalid"))
		http.Redirect(w, r, "/notes/", http.StatusSeeOther)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<20)
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		fail()
		return
	}
	var uploads []notes.Upload
	for _, fh := range r.MultipartForm.File["file"] {
		f, err := fh.Open()
		if err != nil {
			fail()
			return
		}
		data, err := io.ReadAll(io.LimitReader(f, notes.MaxUpload+1))
		f.Close()
		if err != nil {
			fail()
			return
		}
		uploads = append(uploads, notes.Upload{Name: fh.Filename, Data: data})
	}
	rows, err := notes.Rows(uploads)
	if err != nil {
		fail()
		return
	}
	count := 0
	for _, row := range rows {
		if _, err := s.Store.CreateNote(user.ID, row.Body, row.Folder); err != nil {
			fail()
			return
		}
		count++
	}
	flashNotice(s, r, i18n.T(l, "app.import_done", "count", strconv.Itoa(count)))
	http.Redirect(w, r, "/notes/", http.StatusSeeOther)
}
