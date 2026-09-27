package suite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/aquasp/kurachat/internal/openrouter"
)

// Names the model can call. Deletes are held for a confirm button.
const (
	NotesSearch  = "notes_search"
	NotesRead    = "notes_read"
	NotesCreate  = "notes_create"
	NotesUpdate  = "notes_update"
	NotesDelete  = "notes_delete"
	CalList      = "calendar_list"
	CalCreate    = "calendar_create"
	CalUpdate    = "calendar_update"
	CalDelete    = "calendar_delete"
	PeopleSearch = "people_search"
	PeopleRead   = "people_read"
	PeopleCreate = "people_create"
	PeopleUpdate = "people_update"
	PeopleDelete = "people_delete"
	SpendMonth   = "spend_month"
	SpendList    = "spend_list"
	SpendCreate  = "spend_create"
	SpendUpdate  = "spend_update"
	SpendDelete  = "spend_delete"
)

// IsDelete reports a tool that waits for the confirm button.
func IsDelete(name string) bool {
	switch name {
	case NotesDelete, CalDelete, PeopleDelete, SpendDelete:
		return true
	}
	return false
}

// ToolsFor returns the function tools for the connected app names.
func ToolsFor(apps []string) []openrouter.Tool {
	var out []openrouter.Tool
	for _, app := range apps {
		out = append(out, toolset(app)...)
	}
	return out
}

func obj(props map[string]any, req []string) map[string]any {
	schema := map[string]any{"type": "object", "properties": props, "additionalProperties": false}
	// A null required is invalid JSON Schema and gets the whole tool list rejected.
	if len(req) > 0 {
		schema["required"] = req
	}
	return schema
}

func strProp(desc string) map[string]any {
	return map[string]any{"type": "string", "description": desc}
}
func intProp(desc string) map[string]any {
	return map[string]any{"type": "integer", "description": desc}
}

func toolset(app string) []openrouter.Tool {
	switch app {
	case "notes":
		return []openrouter.Tool{
			{Name: NotesSearch, Description: "Search notes. Returns id, title, preview, folder. No full body.", Parameters: obj(map[string]any{"q": strProp("text"), "folder": strProp("folder or empty")}, nil)},
			{Name: NotesRead, Description: "Read one note body by id.", Parameters: obj(map[string]any{"id": intProp("note id")}, []string{"id"})},
			{Name: NotesCreate, Description: "Create a note. First line of body is the title.", Parameters: obj(map[string]any{"body": strProp("full text"), "folder": strProp("optional folder")}, []string{"body"})},
			{Name: NotesUpdate, Description: "Change a note. Send body to replace the text, folder to move it, or both.", Parameters: obj(map[string]any{"id": intProp("note id"), "body": strProp("new text"), "folder": strProp("folder name; inbox moves it to the inbox")}, []string{"id"})},
			{Name: NotesDelete, Description: "Ask to delete one note. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("note id")}, []string{"id"})},
		}
	case "calendar":
		return []openrouter.Tool{
			{Name: CalList, Description: "List events from a date to a date (YYYY-MM-DD).", Parameters: obj(map[string]any{"from": strProp("YYYY-MM-DD"), "to": strProp("YYYY-MM-DD")}, []string{"from", "to"})},
			{Name: CalCreate, Description: "Create an event. Timed events need all_day false plus starts_at and ends_at (HH:MM).", Parameters: obj(map[string]any{"title": strProp(""), "starts_on": strProp("YYYY-MM-DD"), "ends_on": strProp(""), "all_day": map[string]any{"type": "boolean"}, "starts_at": strProp("HH:MM"), "ends_at": strProp("HH:MM"), "body": strProp(""), "emoji": strProp(""), "repeat": strProp("none|daily|weekly|monthly|yearly")}, []string{"title", "starts_on"})},
			{Name: CalUpdate, Description: "Update an event by id. Send only fields that change. Timed events need all_day false plus starts_at and ends_at.", Parameters: obj(map[string]any{"id": intProp(""), "title": strProp(""), "starts_on": strProp("YYYY-MM-DD"), "ends_on": strProp(""), "all_day": map[string]any{"type": "boolean"}, "starts_at": strProp("HH:MM"), "ends_at": strProp("HH:MM"), "body": strProp(""), "emoji": strProp(""), "repeat": strProp("none|daily|weekly|monthly|yearly"), "repeat_until": strProp("YYYY-MM-DD")}, []string{"id"})},
			{Name: CalDelete, Description: "Ask to delete one event. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("")}, []string{"id"})},
		}
	case "people":
		return []openrouter.Tool{
			{Name: PeopleSearch, Description: "Search people. Returns id, name, nickname, relationship, a short notes clip.", Parameters: obj(map[string]any{"q": strProp("text")}, nil)},
			{Name: PeopleRead, Description: "Read one person by id.", Parameters: obj(map[string]any{"id": intProp("person id")}, []string{"id"})},
			{Name: PeopleCreate, Description: "Create a person. Only name is required. Birthday is YYYY-MM-DD (or MM-DD if the year is unknown), or birthday_month, birthday_day, and optional birthday_year. Do not wrap fields in a person object. Birthdays sync to Calendar from People.", Parameters: obj(map[string]any{"name": strProp("required"), "nickname": strProp(""), "relationship": strProp(""), "birthday": strProp("YYYY-MM-DD or MM-DD"), "birthday_month": intProp("1-12"), "birthday_day": intProp("1-31"), "birthday_year": intProp("optional year"), "phone": strProp(""), "email": strProp(""), "notes": strProp("")}, []string{"name"})},
			{Name: PeopleUpdate, Description: "Update a person by id. Send only fields that change. Birthday is YYYY-MM-DD (or MM-DD), or birthday_month, birthday_day, and optional birthday_year.", Parameters: obj(map[string]any{"id": intProp("person id"), "name": strProp(""), "nickname": strProp(""), "relationship": strProp(""), "birthday": strProp("YYYY-MM-DD or MM-DD"), "birthday_month": intProp("1-12"), "birthday_day": intProp("1-31"), "birthday_year": intProp("optional year"), "phone": strProp(""), "email": strProp(""), "notes": strProp("")}, []string{"id"})},
			{Name: PeopleDelete, Description: "Ask to delete one person. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("")}, []string{"id"})},
		}
	case "spend":
		return []openrouter.Tool{
			{Name: SpendMonth, Description: "Month totals in home currency.", Parameters: obj(map[string]any{"year": intProp(""), "month": intProp("1-12")}, []string{"year", "month"})},
			{Name: SpendList, Description: "List expenses for YYYY-MM. No note text.", Parameters: obj(map[string]any{"month": strProp("YYYY-MM"), "category": strProp("food|transport|home|health|leisure|other")}, nil)},
			{Name: SpendCreate, Description: "Log one expense. amount_cents is an integer.", Parameters: obj(map[string]any{"title": strProp(""), "amount_cents": intProp(""), "currency": strProp("BRL|USD|EUR"), "spent_on": strProp("YYYY-MM-DD"), "category": strProp(""), "notes": strProp("")}, []string{"title", "amount_cents", "spent_on"})},
			{Name: SpendUpdate, Description: "Update an expense by id. Send only fields that change.", Parameters: obj(map[string]any{"id": intProp(""), "title": strProp(""), "amount_cents": intProp(""), "currency": strProp("BRL|USD|EUR"), "spent_on": strProp("YYYY-MM-DD"), "category": strProp(""), "notes": strProp("")}, []string{"id"})},
			{Name: SpendDelete, Description: "Ask to delete one expense. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("")}, []string{"id"})},
		}
	}
	return nil
}

// Outcome is what the model sees after a call.
type Outcome struct {
	OK        bool
	Unknown   bool
	Status    int
	Body      string
	Reconnect bool
	App       string
	Title     string
	ID        int64
}

// Execute runs one non-delete tool, or an already-approved delete.
func Execute(ctx context.Context, c *Client, name string, args map[string]any, approvedDelete bool) Outcome {
	app := appOf(name)
	if IsDelete(name) && !approvedDelete {
		return Outcome{Body: `{"error":"needs_confirm"}`, App: app}
	}
	method, path, body, kind, write := route(name, args)
	if method == "" {
		return Outcome{Body: `{"error":"bad_tool"}`, App: app}
	}
	status, raw, err := c.Call(ctx, method, path, body)
	out := Outcome{Status: status, App: app, ID: idArg(args)}
	if errors.Is(err, ErrRedirect) || (write && Lost(err)) {
		out.Unknown = true
		out.Body = `{"error":"unknown_outcome","hint":"the write was sent and the response was lost; search before creating another"}`
		return out
	}
	if err != nil {
		out.Body = `{"ok":false,"error":"unavailable"}`
		return out
	}
	if status == http.StatusUnauthorized {
		out.Reconnect = true
		out.App = app
		out.Body = fmt.Sprintf(`{"ok":false,"error":"unauthorized","reconnect":true,"app":%q,"settings_path":"/apps"}`, app)
		return out
	}
	if IsDelete(name) && status == http.StatusNotFound {
		out.OK = true
		out.Body = `{"ok":true,"already_gone":true}`
		return out
	}
	if status < 200 || status >= 300 {
		out.Body = stampOK(string(raw), false)
		if strings.TrimSpace(out.Body) == "" {
			out.Body = fmt.Sprintf(`{"ok":false,"error":"http_%d"}`, status)
		}
		return out
	}
	shaped := raw
	var serr error
	switch {
	case kind == "month":
		shaped = raw
	case strings.HasSuffix(name, "_search") || name == CalList || name == SpendList:
		shaped, serr = ShapeList(listKind(name), raw)
	case strings.HasSuffix(name, "_read") || strings.HasSuffix(name, "_create") || strings.HasSuffix(name, "_update"):
		shaped, serr = ShapeOne(oneKind(name), raw)
	}
	if serr != nil {
		shaped = raw
	}
	out.OK = true
	out.Body = stampOK(string(shaped), true)
	out.Title = titleFrom(out.Body, oneKind(name))
	if out.ID == 0 {
		out.ID = idFrom(out.Body, oneKind(name))
	}
	return out
}

func appOf(name string) string {
	switch {
	case strings.HasPrefix(name, "notes"):
		return "notes"
	case strings.HasPrefix(name, "calendar"):
		return "calendar"
	case strings.HasPrefix(name, "people"):
		return "people"
	case strings.HasPrefix(name, "spend"):
		return "spend"
	}
	return ""
}

func listKind(name string) string {
	switch name {
	case NotesSearch:
		return "notes"
	case PeopleSearch:
		return "people"
	case CalList:
		return "events"
	case SpendList:
		return "expenses"
	}
	return ""
}

func oneKind(name string) string {
	switch appOf(name) {
	case "notes":
		return "notes"
	case "people":
		return "people"
	case "calendar":
		return "events"
	case "spend":
		return "expenses"
	}
	return ""
}

func idArg(args map[string]any) int64 {
	switch n := args["id"].(type) {
	case float64:
		return int64(n)
	case json.Number:
		i, _ := n.Int64()
		return i
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

func titleFrom(payload, kind string) string {
	var doc map[string]any
	if json.Unmarshal([]byte(payload), &doc) != nil {
		return ""
	}
	key := map[string]string{"notes": "note", "people": "person", "events": "event", "expenses": "expense"}[kind]
	m, _ := doc[key].(map[string]any)
	if m == nil {
		return ""
	}
	if s, _ := m["title"].(string); s != "" {
		return s
	}
	s, _ := m["name"].(string)
	return s
}

func idFrom(payload, kind string) int64 {
	var doc map[string]any
	if json.Unmarshal([]byte(payload), &doc) != nil {
		return 0
	}
	key := map[string]string{"notes": "note", "people": "person", "events": "event", "expenses": "expense"}[kind]
	m, _ := doc[key].(map[string]any)
	if m == nil {
		return 0
	}
	return idArg(m)
}

func route(name string, args map[string]any) (method, path string, body any, kind string, write bool) {
	id := idArg(args)
	q := url.Values{}
	switch name {
	case NotesSearch:
		q.Set("limit", "20")
		if s, _ := args["q"].(string); s != "" {
			q.Set("q", s)
		}
		if s, _ := args["folder"].(string); s != "" {
			q.Set("folder", s)
		}
		return http.MethodGet, "/api/v1/notes?" + q.Encode(), nil, "notes", false
	case NotesRead:
		return http.MethodGet, "/api/v1/notes/" + strconv.FormatInt(id, 10), nil, "notes", false
	case NotesCreate:
		return http.MethodPost, "/api/v1/notes", map[string]any{"note": pick(args, "body", "folder")}, "notes", true
	case NotesUpdate:
		return http.MethodPatch, "/api/v1/notes/" + strconv.FormatInt(id, 10), map[string]any{"note": pick(args, "body", "folder")}, "notes", true
	case NotesDelete:
		return http.MethodDelete, "/api/v1/notes/" + strconv.FormatInt(id, 10), nil, "notes", true
	case CalList:
		q.Set("from", strArg(args, "from"))
		q.Set("to", strArg(args, "to"))
		return http.MethodGet, "/api/v1/events?" + q.Encode(), nil, "events", false
	case CalCreate:
		return http.MethodPost, "/api/v1/events", map[string]any{"event": pick(args, "title", "starts_on", "ends_on", "all_day", "starts_at", "ends_at", "body", "emoji", "repeat", "repeat_until")}, "events", true
	case CalUpdate:
		return http.MethodPatch, "/api/v1/events/" + strconv.FormatInt(id, 10), map[string]any{"event": pick(args, "title", "starts_on", "ends_on", "all_day", "starts_at", "ends_at", "body", "emoji", "repeat", "repeat_until")}, "events", true
	case CalDelete:
		return http.MethodDelete, "/api/v1/events/" + strconv.FormatInt(id, 10), nil, "events", true
	case PeopleSearch:
		q.Set("limit", "20")
		if s := strArg(args, "q"); s != "" {
			q.Set("q", s)
		}
		return http.MethodGet, "/api/v1/people?" + q.Encode(), nil, "people", false
	case PeopleRead:
		return http.MethodGet, "/api/v1/people/" + strconv.FormatInt(id, 10), nil, "people", false
	case PeopleCreate:
		return http.MethodPost, "/api/v1/people", map[string]any{"person": personBody(args)}, "people", true
	case PeopleUpdate:
		return http.MethodPatch, "/api/v1/people/" + strconv.FormatInt(id, 10), map[string]any{"person": personBody(args)}, "people", true
	case PeopleDelete:
		return http.MethodDelete, "/api/v1/people/" + strconv.FormatInt(id, 10), nil, "people", true
	case SpendMonth:
		y, _ := args["year"].(float64)
		m, _ := args["month"].(float64)
		return http.MethodGet, fmt.Sprintf("/api/v1/months/%d/%d", int(y), int(m)), nil, "month", false
	case SpendList:
		q.Set("limit", "20")
		if s := strArg(args, "month"); s != "" {
			q.Set("month", s)
		}
		if s := strArg(args, "category"); s != "" {
			q.Set("category", s)
		}
		return http.MethodGet, "/api/v1/expenses?" + q.Encode(), nil, "expenses", false
	case SpendCreate:
		return http.MethodPost, "/api/v1/expenses", map[string]any{"expense": pick(args, "title", "amount_cents", "currency", "spent_on", "category", "notes")}, "expenses", true
	case SpendUpdate:
		return http.MethodPatch, "/api/v1/expenses/" + strconv.FormatInt(id, 10), map[string]any{"expense": pick(args, "title", "amount_cents", "currency", "spent_on", "category", "notes")}, "expenses", true
	case SpendDelete:
		return http.MethodDelete, "/api/v1/expenses/" + strconv.FormatInt(id, 10), nil, "expenses", true
	}
	return "", "", nil, "", false
}

func strArg(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return s
}

func pick(args map[string]any, keys ...string) map[string]any {
	out := map[string]any{}
	for _, k := range keys {
		if v, ok := args[k]; ok && v != nil && v != "" {
			out[k] = v
		}
	}
	return out
}

// stampOK records whether the tool result is a saved record. The model is
// told not to claim a save unless the result says ok, and not to retry when
// it says false.
func stampOK(payload string, ok bool) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return ""
	}
	var doc map[string]any
	if json.Unmarshal([]byte(payload), &doc) != nil || doc == nil {
		return payload
	}
	doc["ok"] = ok
	b, err := json.Marshal(doc)
	if err != nil {
		return payload
	}
	return string(b)
}

// personBody maps tool arguments onto the People {"person": ...} body.
// A missing name or a year without a month and day is a 422 and inserts
// nothing, so birthday parts are accepted as numbers, numeric strings,
// a nested birthday object, or a date string — including when the model
// wraps the fields in "person".
func personBody(args map[string]any) map[string]any {
	args = flattenPerson(args)
	out := pick(args, "name", "nickname", "relationship", "phone", "email", "notes", "emoji")
	if month, day, year, present := birthdayParts(args); present {
		bday := map[string]any{}
		if month != 0 {
			bday["month"] = month
		}
		if day != 0 {
			bday["day"] = day
		}
		if year != 0 {
			bday["year"] = year
		}
		if len(bday) > 0 {
			out["birthday"] = bday
		}
	}
	return out
}

func flattenPerson(args map[string]any) map[string]any {
	inner, ok := args["person"].(map[string]any)
	if !ok {
		return args
	}
	out := make(map[string]any, len(inner)+len(args))
	for k, v := range inner {
		out[k] = v
	}
	for k, v := range args {
		if k == "person" || v == nil || v == "" {
			continue
		}
		out[k] = v
	}
	return out
}

func birthdayParts(args map[string]any) (month, day, year int, present bool) {
	fm, fd, fy := intField(args, "birthday_month"), intField(args, "birthday_day"), intField(args, "birthday_year")
	if fm != 0 && fd != 0 {
		return fm, fd, fy, true
	}
	if s, ok := args["birthday"].(string); ok {
		if m, d, y, ok := parseBirthdayString(s); ok {
			if y == 0 {
				y = fy
			}
			return m, d, y, true
		}
	}
	if obj, ok := args["birthday"].(map[string]any); ok {
		m, d, y := intField(obj, "month"), intField(obj, "day"), intField(obj, "year")
		if m != 0 || d != 0 || y != 0 {
			if m == 0 {
				m = fm
			}
			if d == 0 {
				d = fd
			}
			if y == 0 {
				y = fy
			}
			return m, d, y, true
		}
	}
	if fm != 0 || fd != 0 || fy != 0 {
		return fm, fd, fy, true
	}
	return 0, 0, 0, false
}

func intField(args map[string]any, key string) int {
	v, ok := args[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		f, err := n.Float64()
		if err != nil {
			return 0
		}
		return int(f)
	case string:
		if i, ok := leadingInt(n); ok {
			return i
		}
		return monthIndex(n)
	default:
		return 0
	}
}

func leadingInt(s string) (int, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:i])
	if err != nil {
		return 0, false
	}
	return n, true
}

func parseBirthdayString(s string) (month, day, year int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, 0, false
	}
	norm := strings.ReplaceAll(s, "/", "-")
	parts := strings.Split(norm, "-")
	if len(parts) == 2 || len(parts) == 3 {
		if m, d, y, ok := numericDate(parts); ok {
			return m, d, y, true
		}
	}
	return proseDate(s)
}

func numericDate(parts []string) (month, day, year int, ok bool) {
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return 0, 0, 0, false
		}
		nums = append(nums, n)
	}
	switch len(nums) {
	case 3:
		if nums[0] >= 1900 && nums[0] <= 2100 {
			return nums[1], nums[2], nums[0], true
		}
	case 2:
		return nums[0], nums[1], 0, true
	}
	return 0, 0, 0, false
}

func proseDate(s string) (month, day, year int, ok bool) {
	chunks := strings.FieldsFunc(s, func(r rune) bool {
		return r == ' ' || r == ',' || r == '.'
	})
	for _, c := range chunks {
		if m := monthIndex(c); m != 0 && month == 0 {
			month = m
			continue
		}
		n, good := leadingInt(c)
		if !good {
			continue
		}
		if n >= 1900 && n <= 2100 && year == 0 {
			year = n
			continue
		}
		if n >= 1 && n <= 31 && day == 0 {
			day = n
		}
	}
	if month == 0 || day == 0 {
		return 0, 0, 0, false
	}
	return month, day, year, true
}

func monthIndex(s string) int {
	switch strings.ToLower(strings.Trim(strings.TrimSpace(s), ".")) {
	case "january", "jan", "janeiro":
		return 1
	case "february", "feb", "fevereiro":
		return 2
	case "march", "mar", "março", "marco":
		return 3
	case "april", "apr", "abril":
		return 4
	case "may", "maio":
		return 5
	case "june", "jun", "junho":
		return 6
	case "july", "jul", "julho":
		return 7
	case "august", "aug", "agosto":
		return 8
	case "september", "sep", "sept", "setembro":
		return 9
	case "october", "oct", "outubro":
		return 10
	case "november", "nov", "novembro":
		return 11
	case "december", "dec", "dezembro":
		return 12
	default:
		return 0
	}
}
