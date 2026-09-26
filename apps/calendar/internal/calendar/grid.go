// Package calendar holds the calendar-domain logic shared by the web and
// API handlers: the month grid and the export importer.
package calendar

import (
	"time"

	"github.com/aquasp/kuracalendar/internal/holidays"
	"github.com/aquasp/kuracalendar/internal/store"
)

// Mark is one month-cell pill: its kind (holiday/birthday/event) and label.
type Mark struct {
	Kind  string
	Label string
}

// Cell is one month-grid day, mirroring CalendarGrid::Cell.
type Cell struct {
	Date         time.Time
	InMonth      bool
	Today        bool
	Selected     bool
	Events       []*store.Event
	Birthdays    []*store.Birthday
	Holidays     []holidays.Holiday
	TagCountries bool // >1 pack selected: suffix " · BR"
}

// HolidayLabel mirrors Cell#holiday_label (name resolved by the caller so
// the grid stays locale-free).
func (c *Cell) HolidayLabel(h holidays.Holiday, name string) string {
	if c.TagCountries {
		return name + " · " + h.Country
	}
	return name
}

// Marks mirrors Cell#marks: up to 3 pills plus an overflow count.
func (c *Cell) Marks(holidayName func(h holidays.Holiday) string) ([]Mark, int) {
	var list []Mark
	for _, h := range c.Holidays {
		list = append(list, Mark{Kind: "holiday", Label: c.HolidayLabel(h, holidayName(h))})
	}
	for _, b := range c.Birthdays {
		list = append(list, Mark{Kind: "birthday", Label: withEmoji(b.Emoji, b.Name)})
	}
	for _, e := range c.Events {
		list = append(list, Mark{Kind: "event", Label: withEmoji(e.Emoji, e.Title)})
	}
	extra := len(list) - 3
	if extra < 0 {
		extra = 0
	}
	if len(list) > 3 {
		list = list[:3]
	}
	return list, extra
}

// withEmoji prefixes "🎂 " when the row carries an emoji.
func withEmoji(emoji, label string) string {
	if emoji == "" {
		return label
	}
	return emoji + " " + label
}

func (c *Cell) Empty() bool {
	return len(c.Events) == 0 && len(c.Birthdays) == 0 && len(c.Holidays) == 0
}

// Grid is one rendered month, mirroring CalendarGrid.
type Grid struct {
	Month     time.Time
	Selected  time.Time
	Today     time.Time
	Cells     []*Cell
	StartDate time.Time
	EndDate   time.Time
	PrevMonth time.Time
	NextMonth time.Time
}

// WeekdayIndexes mirrors CalendarGrid#weekday_indexes (Monday-first,
// Sunday=0).
func WeekdayIndexes() [7]int { return [7]int{1, 2, 3, 4, 5, 6, 0} }

func mondayOffset(wd time.Weekday) int { return (int(wd) + 6) % 7 }

// SelectedCell mirrors CalendarGrid#selected_cell.
func (g *Grid) SelectedCell() *Cell {
	for _, c := range g.Cells {
		if c.Selected {
			return c
		}
	}
	for _, c := range g.Cells {
		if c.Date.Equal(g.Selected) {
			return c
		}
	}
	return nil
}

// BuildGrid mirrors CalendarGrid: the month's Monday-first cells with the
// user's events, birthdays (Feb 29 observed Feb 28 in common years), and
// the selected packs' holidays.
func BuildGrid(st *store.Store, userID int64, codes []string, month, selected, today time.Time) (*Grid, error) {
	month = time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	selected = time.Date(selected.Year(), selected.Month(), selected.Day(), 0, 0, 0, 0, time.UTC)
	today = time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, time.UTC)

	start := month.AddDate(0, 0, -mondayOffset(month.Weekday()))
	lastOfMonth := time.Date(month.Year(), month.Month()+1, 0, 0, 0, 0, 0, time.UTC)
	end := lastOfMonth.AddDate(0, 0, 6-mondayOffset(lastOfMonth.Weekday()))

	occs, err := Occurrences(st, userID, start, end)
	if err != nil {
		return nil, err
	}
	// Cells hold series pointers (template values): display, edit, and
	// delete are all series-based. The seen-set dedups multi-day series
	// whose duration covers one date twice (duration >= step).
	eventsByDate := map[string][]*store.Event{}
	seen := map[string]map[int64]bool{}
	for _, o := range occs {
		for d := maxDate(o.Start, start); !d.After(minDate(o.End, end)); d = d.AddDate(0, 0, 1) {
			key := d.Format("2006-01-02")
			if seen[key] == nil {
				seen[key] = map[int64]bool{}
			}
			if seen[key][o.Event.ID] {
				continue
			}
			seen[key][o.Event.ID] = true
			eventsByDate[key] = append(eventsByDate[key], o.Event)
		}
	}

	birthdays, err := st.ListBirthdays(userID)
	if err != nil {
		return nil, err
	}

	holidaysByDate := map[string][]holidays.Holiday{}
	for _, h := range holidays.InRange(codes, start, end) {
		key := h.Date.Format("2006-01-02")
		holidaysByDate[key] = append(holidaysByDate[key], h)
	}
	tag := len(codes) > 1

	var cells []*Cell
	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		var dayBirthdays []*store.Birthday
		for _, b := range birthdays {
			if b.ObservedOn(d) {
				dayBirthdays = append(dayBirthdays, b)
			}
		}
		key := d.Format("2006-01-02")
		cells = append(cells, &Cell{
			Date:         d,
			InMonth:      d.Month() == month.Month() && d.Year() == month.Year(),
			Today:        d.Equal(today),
			Selected:     d.Equal(selected),
			Events:       eventsByDate[key],
			Birthdays:    dayBirthdays,
			Holidays:     holidaysByDate[key],
			TagCountries: tag,
		})
	}

	return &Grid{
		Month:     month,
		Selected:  selected,
		Today:     today,
		Cells:     cells,
		StartDate: start,
		EndDate:   end,
		PrevMonth: month.AddDate(0, -1, 0),
		NextMonth: month.AddDate(0, 1, 0),
	}, nil
}
