package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kurapeople/internal/i18n"
	"github.com/aquasp/kurapeople/internal/store"
	"github.com/aquasp/kurapeople/internal/views"
	"github.com/go-chi/chi/v5"
)

func (s *Server) handlePeopleIndex(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	rel := strings.TrimSpace(r.URL.Query().Get("relationship"))
	people, err := s.Store.SearchPeople(user.ID, q, rel, 0, 0)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	rels, err := s.Store.ListRelationships(user.ID)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	today := time.Now()
	var upcoming []views.Upcoming
	if q == "" && rel == "" {
		all, err := s.Store.ListPeople(user.ID)
		if err != nil {
			http.Error(w, "people unavailable", http.StatusInternalServerError)
			return
		}
		upcoming = views.UpcomingBirthdays(all, today, 60, 8)
	}
	d := views.IndexData{
		People: people, Upcoming: upcoming, Relationships: rels,
		Q: q, Relationship: rel, AutoLock: AutoLockEnabled(r), Today: today,
	}
	dlg := views.DialogData{
		Action: "/people", Method: "post",
		Heading: pTitle(r, "app.new_person"), Rels: rels,
	}
	p := s.page(w, r, pTitle(r, "titles.app"), "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.IndexPage(p, d, dlg)))
}

func (s *Server) handlePeopleShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	person, err := s.Store.FindPerson(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	attrs, err := s.Store.ListAttrs(person.ID)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	rels, err := s.Store.ListRelationships(user.ID)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	today := time.Now()
	d := views.ShowData{Person: person, Attrs: attrs, AutoLock: AutoLockEnabled(r), Today: today}
	dlg := views.DialogData{
		Action: views.PersonURL(person), Method: "patch",
		Heading: pTitle(r, "app.edit_person"),
		Person:  *person, Attrs: attrs, Rels: rels,
		DeleteURL: views.PersonURL(person),
		DeleteMsg: i18n.T(LocaleOf(r), "js.delete_person_confirm", "name", person.Name),
	}
	p := s.page(w, r, person.Name+" · TansuPeople", "")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.ShowPage(p, d, dlg)))
}

// webCreateLimited mirrors the sibling apps' create rate limit
// (60/minute per user): flash + back, like redirect_back fallback root.
func (s *Server) webCreateLimited(w http.ResponseWriter, r *http.Request, key string) bool {
	if s.Limiter.Allow(key, 60, time.Minute) {
		return false
	}
	flashAlert(s, r, i18n.T(LocaleOf(r), "auth.too_many"))
	back := r.Referer()
	if back == "" {
		back = "/"
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
	return true
}

func (s *Server) handlePeopleCreate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	if s.webCreateLimited(w, r, "person:"+itoa64(user.ID)) {
		return
	}
	s.savePerson(w, r, user.ID, 0, true)
}

func (s *Server) handlePeopleUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindPerson(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	s.savePerson(w, r, user.ID, id, false)
}

func (s *Server) savePerson(w http.ResponseWriter, r *http.Request, userID, id int64, create bool) {
	l := LocaleOf(r)
	in := personInput(r)
	attrs := attrsInput(r)
	var (
		person *store.Person
		errs   []store.FieldError
		err    error
	)
	if create {
		person, errs, err = s.Store.CreatePerson(userID, in)
	} else {
		person, errs, err = s.Store.UpdatePerson(userID, id, in)
	}
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	back := "/"
	if !create {
		back = "/people/" + strconv.FormatInt(id, 10)
	}
	if len(errs) > 0 {
		flashAlert(s, r, errorSentence(l, errs))
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	if attrErrs, err := s.Store.ReplaceAttrs(person.ID, attrs); err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	} else if len(attrErrs) > 0 {
		flashAlert(s, r, errorSentence(l, attrErrs))
		http.Redirect(w, r, views.PersonURL(person), http.StatusSeeOther)
		return
	}
	s.pushBirthdayFor(userID, person, false)
	http.Redirect(w, r, views.PersonURL(person), http.StatusSeeOther)
}

func (s *Server) handlePeopleDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	person, err := s.Store.FindPerson(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	s.pushBirthdayFor(user.ID, person, true)
	_ = s.Store.DeletePerson(user.ID, id)
	s.Store.ReclaimSpace()
	if r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func errorMessages(l i18n.Locale, errs []store.FieldError) []string {
	out := make([]string, 0, len(errs))
	for _, e := range errs {
		out = append(out, i18n.T(l, e.MessageKey()))
	}
	return out
}

func errorSentence(l i18n.Locale, errs []store.FieldError) string {
	return strings.Join(errorMessages(l, errs), " ")
}

func personInput(r *http.Request) store.PersonInput {
	return store.PersonInput{
		Name:         r.FormValue("name"),
		Nickname:     r.FormValue("nickname"),
		Relationship: r.FormValue("relationship"),
		BirthMonth:   r.FormValue("birth_month"),
		BirthDay:     r.FormValue("birth_day"),
		BirthYear:    r.FormValue("birth_year"),
		Emoji:        r.FormValue("emoji"),
		Phone:        r.FormValue("phone"),
		Email:        r.FormValue("email"),
		Address:      r.FormValue("address"),
		RingSize:     r.FormValue("ring_size"),
		ShoeSize:     r.FormValue("shoe_size"),
		ShirtSize:    r.FormValue("shirt_size"),
		PantsSize:    r.FormValue("pants_size"),
		Height:       r.FormValue("height"),
		Favorites:    r.FormValue("favorites"),
		Notes:        r.FormValue("notes"),
	}
}

// attrsInput pairs the repeated attr_label/attr_value form rows.
func attrsInput(r *http.Request) []store.AttrInput {
	_ = r.ParseForm()
	labels := r.Form["attr_label"]
	values := r.Form["attr_value"]
	var out []store.AttrInput
	for i := range labels {
		var value string
		if i < len(values) {
			value = values[i]
		}
		out = append(out, store.AttrInput{Label: labels[i], Value: value})
	}
	return out
}

// ---------------------------------------------------------------------------
// Birthday export (KuraCalendar bridge)
// ---------------------------------------------------------------------------

func (s *Server) handlePersonCalendarJSON(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	person, err := s.Store.FindPerson(user.ID, id)
	if err != nil || !person.HasBirthday() {
		s.notFound(w, r)
		return
	}
	serveJSON(w, slug(person.Name)+"-birthday.json", calendarExportJSON([]*store.Person{person}))
}

func (s *Server) handleBirthdaysJSON(w http.ResponseWriter, r *http.Request) {
	people, err := s.Store.ListPeople(UserOf(r).ID)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	serveJSON(w, "birthdays.json", calendarExportJSON(people))
}

func serveJSON(w http.ResponseWriter, filename, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write([]byte(body))
}

// calendarExportBirthday mirrors KuraCalendar's ExportBirthday (key
// order included) so the file imports under More → Import there.
type calendarExportBirthday struct {
	Name  string `json:"name"`
	Month int    `json:"month"`
	Day   int    `json:"day"`
	Year  *int   `json:"year"`
	Body  string `json:"body"`
	Emoji string `json:"emoji"`
}

type calendarExportPayload struct {
	App        string                   `json:"app"`
	ExportedAt string                   `json:"exported_at"`
	Events     []any                    `json:"events"`
	Birthdays  []calendarExportBirthday `json:"birthdays"`
}

// calendarExportJSON renders birthdays in the KuraCalendar import
// format (dateless cards are skipped).
func calendarExportJSON(people []*store.Person) string {
	payload := calendarExportPayload{
		App:        "TansuCalendar",
		ExportedAt: time.Now().UTC().Format(time.RFC3339),
		Events:     []any{},
		Birthdays:  []calendarExportBirthday{},
	}
	for _, p := range people {
		if !p.HasBirthday() {
			continue
		}
		var year *int
		if p.BirthYear != 0 {
			y := p.BirthYear
			year = &y
		}
		var body []string
		if strings.TrimSpace(p.Nickname) != "" {
			body = append(body, strings.TrimSpace(p.Nickname))
		}
		if strings.TrimSpace(p.Relationship) != "" {
			body = append(body, strings.TrimSpace(p.Relationship))
		}
		payload.Birthdays = append(payload.Birthdays, calendarExportBirthday{
			Name: p.Name, Month: p.BirthMonth, Day: p.BirthDay,
			Year: year, Body: strings.Join(body, " · "), Emoji: p.Emoji,
		})
	}
	data, _ := json.MarshalIndent(payload, "", "  ")
	return string(data) + "\n"
}

func (s *Server) handlePersonICS(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	person, err := s.Store.FindPerson(user.ID, id)
	if err != nil || !person.HasBirthday() {
		s.notFound(w, r)
		return
	}
	serveICS(w, slug(person.Name)+"-birthday.ics", birthdaysICS(r.Host, []*store.Person{person}))
}

func (s *Server) handleBirthdaysICS(w http.ResponseWriter, r *http.Request) {
	people, err := s.Store.ListPeople(UserOf(r).ID)
	if err != nil {
		http.Error(w, "people unavailable", http.StatusInternalServerError)
		return
	}
	serveICS(w, "birthdays.ics", birthdaysICS(r.Host, people))
}

func serveICS(w http.ResponseWriter, filename, body string) {
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filename+`"`)
	_, _ = w.Write([]byte(body))
}

// birthdaysICS renders one yearly VEVENT per birthday, stable across
// exports (same UID) so re-imports update instead of duplicating.
func birthdaysICS(host string, people []*store.Person) string {
	host = strings.TrimSpace(host)
	if i := strings.LastIndex(host, ":"); i >= 0 {
		host = host[:i]
	}
	if host == "" {
		host = "kurapeople"
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//TansuPeople//Birthdays//EN\r\n")
	stamp := time.Now().UTC().Format("20060102T150405Z")
	year := time.Now().Year()
	for _, p := range people {
		if !p.HasBirthday() {
			continue
		}
		startYear := year
		if p.BirthYear != 0 {
			startYear = p.BirthYear
		}
		summary := "🎂 " + p.Name
		if strings.TrimSpace(p.Emoji) != "" {
			summary = strings.TrimSpace(p.Emoji) + " " + p.Name
		}
		var desc []string
		if strings.TrimSpace(p.Relationship) != "" {
			desc = append(desc, strings.TrimSpace(p.Relationship))
		}
		if strings.TrimSpace(p.Nickname) != "" {
			desc = append(desc, strings.TrimSpace(p.Nickname))
		}
		b.WriteString("BEGIN:VEVENT\r\n")
		b.WriteString("UID:kurapeople-person-" + strconv.FormatInt(p.ID, 10) + "@" + host + "\r\n")
		b.WriteString("DTSTAMP:" + stamp + "\r\n")
		b.WriteString("DTSTART;VALUE=DATE:" + icsDate(startYear, p.BirthMonth, p.BirthDay) + "\r\n")
		b.WriteString("RRULE:FREQ=YEARLY\r\n")
		b.WriteString("SUMMARY:" + icsEscape(summary) + "\r\n")
		if len(desc) > 0 {
			b.WriteString("DESCRIPTION:" + icsEscape(strings.Join(desc, " · ")) + "\r\n")
		}
		b.WriteString("END:VEVENT\r\n")
	}
	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

func icsDate(year, month, day int) string {
	return pad4(year) + pad2(month) + pad2(day)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

func pad4(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 4 {
		s = "0" + s
	}
	return s
}

// icsEscape escapes TEXT values per RFC 5545.
func icsEscape(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, ";", "\\;")
	s = strings.ReplaceAll(s, ",", "\\,")
	s = strings.ReplaceAll(s, "\r\n", "\\n")
	s = strings.ReplaceAll(s, "\n", "\\n")
	return s
}

// slug renders a filename-safe stem from a name.
func slug(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '_':
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-")
	for strings.Contains(out, "--") {
		out = strings.ReplaceAll(out, "--", "-")
	}
	if out == "" {
		return "person"
	}
	return out
}
