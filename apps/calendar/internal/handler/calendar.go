package handler

import (
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/aquasp/kuracalendar/internal/calendar"
	"github.com/aquasp/kuracalendar/internal/i18n"
	"github.com/aquasp/kuracalendar/internal/store"
	"github.com/aquasp/kuracalendar/internal/views"
	"github.com/go-chi/chi/v5"
)

// resolveMonth mirrors CalendarController#month_date: the requested month,
// falling back to the current month.
func resolveMonth(yearS, monthS string, today time.Time) time.Time {
	year, errY := strconv.Atoi(strings.TrimSpace(yearS))
	month, errM := strconv.Atoi(strings.TrimSpace(monthS))
	if errY != nil || errM != nil || year < 1 || year > 9999 || month < 1 || month > 12 {
		return time.Date(today.Year(), today.Month(), 1, 0, 0, 0, 0, time.UTC)
	}
	return time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
}

// resolveSelected mirrors CalendarController#selected_date: the requested
// day, today when viewing the current month, else the month start.
func resolveSelected(month time.Time, dayS string, today time.Time) time.Time {
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
	if strings.TrimSpace(dayS) != "" {
		if day, err := strconv.Atoi(strings.TrimSpace(dayS)); err == nil &&
			day >= 1 && day <= store.DaysInMonth(month.Year(), int(month.Month())) {
			return time.Date(month.Year(), month.Month(), day, 0, 0, 0, 0, time.UTC)
		}
		return month
	}
	if today.Year() == month.Year() && today.Month() == month.Month() {
		return today
	}
	return month
}

func (s *Server) handleCalendarShow(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	today := time.Now()
	month := resolveMonth(chi.URLParam(r, "year"), chi.URLParam(r, "month"), today)
	selected := resolveSelected(month, chi.URLParam(r, "day"), today)
	grid, err := calendar.BuildGrid(s.Store, user.ID, user.HolidayCountryCodes(), month, selected, today)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	cell := grid.SelectedCell()
	if cell == nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	d := views.CalData{Grid: grid, Cell: cell, AutoLock: AutoLockEnabled(r), Codes: user.HolidayCountryCodes()}
	p := s.page(w, r, pTitle(r, "titles.app"), "app-body")
	render(w, r, http.StatusOK, views.Layout(p, views.NoHead(), views.CalPage(p, d)))
}

func (s *Server) handleExport(w http.ResponseWriter, r *http.Request) {
	payload, err := calendar.BuildExport(s.Store, UserOf(r).ID)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	data, err := calendar.MarshalExport(payload)
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	filename := "kuracalendar-" + time.Now().Format("2006-01-02") + ".json"
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename="+strconv.Quote(filename))
	_, _ = w.Write(data)
}

func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	l := LocaleOf(r)
	user := UserOf(r)
	fail := func() {
		flashAlert(s, r, i18n.T(l, "app.import_invalid"))
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
	r.Body = http.MaxBytesReader(w, r.Body, 16<<20)
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		fail()
		return
	}
	files := r.MultipartForm.File["file"]
	if len(files) == 0 {
		fail()
		return
	}
	f, err := files[0].Open()
	if err != nil {
		fail()
		return
	}
	data, err := io.ReadAll(io.LimitReader(f, 16<<20))
	f.Close()
	if err != nil {
		fail()
		return
	}
	imported, err := calendar.ParseImport(data)
	if err != nil {
		fail()
		return
	}
	count := 0
	for _, in := range imported.Events {
		if _, errs, err := s.Store.CreateEvent(user.ID, in); err == nil && len(errs) == 0 {
			count++
		}
	}
	for _, in := range imported.Birthdays {
		ev, errs := store.EventInputFromBirthday(in)
		if len(errs) > 0 {
			continue
		}
		if _, errs, err := s.Store.CreateEvent(user.ID, ev); err == nil && len(errs) == 0 {
			count++
		}
	}
	flashNotice(s, r, i18n.T(l, "app.import_done", "count", strconv.Itoa(count)))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *Server) handleUpdateHolidays(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	_ = r.ParseForm()
	user.SetHolidayCountryCodes(r.Form["countries"])
	if err := s.Store.UpdateUserHolidayCountries(user.ID, user.HolidayCountries); err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	today := time.Now()
	month := resolveMonth(r.FormValue("year"), r.FormValue("month"), today)
	selected := resolveSelected(month, r.FormValue("day"), today)
	http.Redirect(w, r, views.CalPath(selected), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Events
// ---------------------------------------------------------------------------

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

func eventInput(r *http.Request) store.EventInput {
	return store.EventInput{
		Title:       r.FormValue("title"),
		Body:        r.FormValue("body"),
		AllDay:      r.FormValue("all_day") == "1",
		StartsOn:    r.FormValue("starts_on"),
		EndsOn:      r.FormValue("ends_on"),
		StartsAt:    r.FormValue("starts_at"),
		EndsAt:      r.FormValue("ends_at"),
		Emoji:       r.FormValue("emoji"),
		Repeat:      r.FormValue("repeat"),
		RepeatUntil: r.FormValue("repeat_until"),
	}
}

func eventFallback(r *http.Request) time.Time {
	if d, ok := store.ParseDate(strings.TrimSpace(r.FormValue("starts_on"))); ok {
		return d
	}
	today := time.Now()
	return time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)
}

// webCreateLimited mirrors the Rails create rate limit (60/minute per
// user): flash + back, like redirect_back fallback root.
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

func (s *Server) handleEventsCreate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	if s.webCreateLimited(w, r, "event:"+itoa64(user.ID)) {
		return
	}
	s.saveEvent(w, r, user.ID, 0, true)
}

func (s *Server) handleEventsUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindEvent(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	s.saveEvent(w, r, user.ID, id, false)
}

func (s *Server) saveEvent(w http.ResponseWriter, r *http.Request, userID, id int64, create bool) {
	l := LocaleOf(r)
	in := eventInput(r)
	var (
		event *store.Event
		errs  []store.FieldError
		err   error
	)
	if create {
		event, errs, err = s.Store.CreateEvent(userID, in)
	} else {
		event, errs, err = s.Store.UpdateEvent(userID, id, in)
	}
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	if len(errs) > 0 {
		flashAlert(s, r, errorSentence(l, errs))
		http.Redirect(w, r, views.CalPath(eventFallback(r)), http.StatusSeeOther)
		return
	}
	startsOn, _ := store.ParseDate(event.StartsOn)
	http.Redirect(w, r, views.CalPath(startsOn), http.StatusSeeOther)
}

func (s *Server) handleEventsDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	event, err := s.Store.FindEvent(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	startsOn, _ := store.ParseDate(event.StartsOn)
	_ = s.Store.DeleteEvent(user.ID, id)
	s.Store.ReclaimSpace()
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, views.CalPath(startsOn), http.StatusSeeOther)
}

// ---------------------------------------------------------------------------
// Birthdays
// ---------------------------------------------------------------------------

func birthdayInput(r *http.Request) store.BirthdayInput {
	return store.BirthdayInput{
		Name:  r.FormValue("name"),
		Month: r.FormValue("month"),
		Day:   r.FormValue("day"),
		Year:  r.FormValue("year"),
		Body:  r.FormValue("body"),
		Emoji: r.FormValue("emoji"),
	}
}

// landingDate mirrors BirthdaysController#landing_date.
func landingDate(r *http.Request, month, day int) time.Time {
	today := time.Now()
	year := today.Year()
	if y, err := strconv.Atoi(strings.TrimSpace(r.FormValue("return_year"))); err == nil && y >= 1900 && y <= 2100 {
		year = y
	}
	if month < 1 || month > 12 {
		month = int(today.Month())
	}
	if day < 1 {
		day = 1
	}
	if max := store.DaysInMonth(year, month); day > max {
		day = max
	}
	if month == 2 && day == 29 && store.DaysInMonth(year, 2) == 28 {
		day = 28
	}
	return time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
}

func formMonthDay(r *http.Request) (int, int) {
	month, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("month")))
	day, _ := strconv.Atoi(strings.TrimSpace(r.FormValue("day")))
	return month, day
}

func (s *Server) handleBirthdaysCreate(w http.ResponseWriter, r *http.Request) {
	flashAlert(s, r, i18n.T(LocaleOf(r), "app.birthday_retired"))
	month, day := formMonthDay(r)
	http.Redirect(w, r, views.CalPath(landingDate(r, month, day)), http.StatusSeeOther)
}

func (s *Server) handleBirthdaysUpdate(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.FindBirthday(user.ID, id); err != nil {
		s.notFound(w, r)
		return
	}
	s.saveBirthday(w, r, user.ID, id, false)
}

func (s *Server) saveBirthday(w http.ResponseWriter, r *http.Request, userID, id int64, create bool) {
	l := LocaleOf(r)
	in := birthdayInput(r)
	var (
		birthday *store.Birthday
		errs     []store.FieldError
		err      error
	)
	if create {
		birthday, errs, err = s.Store.CreateBirthday(userID, in)
	} else {
		birthday, errs, err = s.Store.UpdateBirthday(userID, id, in)
	}
	if err != nil {
		http.Error(w, "calendar unavailable", http.StatusInternalServerError)
		return
	}
	month, day := formMonthDay(r)
	if len(errs) > 0 {
		flashAlert(s, r, errorSentence(l, errs))
		http.Redirect(w, r, views.CalPath(landingDate(r, month, day)), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, views.CalPath(landingDate(r, birthday.Month, birthday.Day)), http.StatusSeeOther)
}

func (s *Server) handleBirthdaysDestroy(w http.ResponseWriter, r *http.Request) {
	user := UserOf(r)
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	birthday, err := s.Store.FindBirthday(user.ID, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	landing := landingDate(r, birthday.Month, birthday.Day)
	_ = s.Store.DeleteBirthday(user.ID, id)
	s.Store.ReclaimSpace()
	if r.Header.Get("HX-Request") == "true" || r.Header.Get("X-Requested-With") == "fetch" {
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, views.CalPath(landing), http.StatusSeeOther)
}
