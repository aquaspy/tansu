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
	NotesSearch             = "notes_search"
	NotesRead               = "notes_read"
	NotesCreate             = "notes_create"
	NotesUpdate             = "notes_update"
	NotesDelete             = "notes_delete"
	NotesFolders            = "notes_folders"
	NotesFolderRename       = "notes_folder_rename"
	NotesFolderDelete       = "notes_folder_delete"
	CalList                 = "calendar_list"
	CalRead                 = "calendar_read"
	CalCreate               = "calendar_create"
	CalUpdate               = "calendar_update"
	CalDelete               = "calendar_delete"
	PeopleSearch            = "people_search"
	PeopleRead              = "people_read"
	PeopleCreate            = "people_create"
	PeopleUpdate            = "people_update"
	PeopleDelete            = "people_delete"
	SpendMonth              = "spend_month"
	SpendList               = "spend_list"
	SpendRead               = "spend_read"
	SpendCreate             = "spend_create"
	SpendUpdate             = "spend_update"
	SpendDelete             = "spend_delete"
	SpendSubscriptionList   = "spend_subscription_list"
	SpendSubscriptionRead   = "spend_subscription_read"
	SpendSubscriptionCreate = "spend_subscription_create"
	SpendSubscriptionUpdate = "spend_subscription_update"
	SpendSubscriptionDelete = "spend_subscription_delete"
	SpendPaymentDayList     = "spend_payment_day_list"
	SpendPaymentDayRead     = "spend_payment_day_read"
	SpendPaymentDayCreate   = "spend_payment_day_create"
	SpendPaymentDayUpdate   = "spend_payment_day_update"
	SpendPaymentDayDelete   = "spend_payment_day_delete"
)

// IsDelete reports a tool that waits for the confirm button.
func IsDelete(name string) bool {
	switch name {
	case NotesDelete, NotesFolderDelete, CalDelete, PeopleDelete, SpendDelete, SpendSubscriptionDelete, SpendPaymentDayDelete:
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
func boolProp(desc string) map[string]any {
	return map[string]any{"type": "boolean", "description": desc}
}

func sizesSchema() map[string]any {
	return map[string]any{
		"type":        "object",
		"description": "Ring, shoe, shirt, and pants. Only keys you include are written. Omit sizes to leave them unchanged.",
		"properties": map[string]any{
			"ring":  strProp("ring size"),
			"shoe":  strProp("shoe size"),
			"shirt": strProp("shirt size"),
			"pants": strProp("pants size"),
		},
		"additionalProperties": false,
	}
}

func attrsSchema() map[string]any {
	return map[string]any{
		"type":        "array",
		"description": "Free rows such as CPF, Pix, or an extra email. On update, sending attrs replaces the whole list. Omit attrs to leave existing rows unchanged.",
		"items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"label": strProp("short label, e.g. CPF"),
				"value": strProp("value"),
			},
			"required":             []string{"label"},
			"additionalProperties": false,
		},
	}
}

// peopleFields are the writable person properties shared by create and update.
func peopleFields() map[string]any {
	return map[string]any{
		"name":           strProp("required on create"),
		"nickname":       strProp(""),
		"relationship":   strProp(""),
		"emoji":          strProp("single emoji"),
		"phone":          strProp(""),
		"email":          strProp("primary email; extra emails go in attrs"),
		"address":        strProp("street address"),
		"height":         strProp("height, e.g. 1.65m"),
		"birthday":       strProp("YYYY-MM-DD or MM-DD"),
		"birthday_month": intProp("1-12"),
		"birthday_day":   intProp("1-31"),
		"birthday_year":  intProp("optional year"),
		"sizes":          sizesSchema(),
		"ring_size":      strProp("changes only the ring size"),
		"shoe_size":      strProp("changes only the shoe size"),
		"shirt_size":     strProp("changes only the shirt size"),
		"pants_size":     strProp("changes only the pants size"),
		"favorites":      strProp("On update, an empty string clears favorites. Omit the key to leave them unchanged."),
		"notes":          strProp("Free text that does not fit a field above. On update, an empty string clears notes, including a lone dash. Omit the key to leave notes unchanged. A dash is text, not a blank."),
		"attrs":          attrsSchema(),
	}
}

func noteFields() map[string]any {
	return map[string]any{
		"body":   strProp("full text. The first non-blank line is the title. There is no separate title column and there are no tags."),
		"title":  strProp("optional. Placed on the first line. On update without body, the rest of the note is kept."),
		"folder": strProp("folder name. inbox or blank moves the note to the inbox. all is reserved."),
	}
}

func eventFields() map[string]any {
	return map[string]any{
		"title":        strProp("required on create"),
		"starts_on":    strProp("YYYY-MM-DD. 19/09/2026 is day/month/year."),
		"ends_on":      strProp("YYYY-MM-DD. Defaults to starts_on."),
		"all_day":      boolProp("true for an all-day event. Timed events need false plus starts_at and ends_at. Omit this when you send a time and it is treated as false."),
		"starts_at":    strProp("HH:MM. 2:30 PM and 14h30 are accepted."),
		"ends_at":      strProp("HH:MM"),
		"body":         strProp("notes. There is no location or attendee field; put a place or guests here."),
		"emoji":        strProp("single emoji"),
		"repeat":       strProp("none, daily, weekly, monthly, or yearly"),
		"repeat_until": strProp("YYYY-MM-DD. Omit to repeat forever. Ignored when repeat is none."),
	}
}

func expenseFields() map[string]any {
	return map[string]any{
		"title":        strProp("short name of the expense"),
		"amount":       strProp("decimal money such as 25.50 or 25,50. Use this or amount_cents."),
		"amount_cents": intProp("integer cents, such as 2550 for 25.50. If both are sent, amount_cents wins."),
		"currency":     strProp("BRL, USD, or EUR. Default BRL."),
		"spent_on":     strProp("YYYY-MM-DD. 19/09/2026 is day/month/year."),
		"category":     strProp("food, transport, home, health, leisure, or other"),
		"notes":        strProp("free text that is not a category, amount, or currency. There are no tags or splits."),
	}
}

func subscriptionFields() map[string]any {
	return map[string]any{
		"title":        strProp("name of the recurring bill"),
		"amount":       strProp("decimal money such as 19.90. Use this or amount_cents."),
		"amount_cents": intProp("integer cents. If both are sent, amount_cents wins."),
		"currency":     strProp("BRL, USD, or EUR. Default BRL."),
		"interval":     strProp("monthly (default) or yearly. Yearly is the amount for the whole year; leftover counts one twelfth each month."),
		"active":       boolProp("defaults to true. false pauses the subscription."),
		"notes":        strProp("free text that does not fit a field above"),
	}
}

func paymentDayFields() map[string]any {
	return map[string]any{
		"title":   strProp("what the reminder is for"),
		"due_day": intProp("1-31, required on create"),
		"active":  boolProp("defaults to true"),
		"notes":   strProp("free text. A payment day does not log an expense."),
	}
}

func toolset(app string) []openrouter.Tool {
	switch app {
	case "notes":
		updateNote := noteFields()
		updateNote["id"] = intProp("note id")
		return []openrouter.Tool{
			{Name: NotesSearch, Description: "Search notes. Returns id, title, preview, folder. No full body.", Parameters: obj(map[string]any{"q": strProp("text"), "folder": strProp("folder or empty for every folder")}, nil)},
			{Name: NotesRead, Description: "Read one note by id, including the full body and folder.", Parameters: obj(map[string]any{"id": intProp("note id")}, []string{"id"})},
			{Name: NotesCreate, Description: "Create a note. The first non-blank line of body is the title. Optional title is placed on that first line in front of body. folder is optional; inbox means the inbox. There are no tags. Do not wrap fields in a note object.", Parameters: obj(noteFields(), nil)},
			{Name: NotesUpdate, Description: "Change a note by id. Partial: send body to replace the text, title to rewrite the first line and keep the rest, folder to move it, or any combination. inbox moves it to the inbox. There are no tags.", Parameters: obj(updateNote, []string{"id"})},
			{Name: NotesDelete, Description: "Ask to delete one note. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("note id")}, []string{"id"})},
			{Name: NotesFolders, Description: "List folders and how many notes are in each. An empty name is the inbox.", Parameters: obj(map[string]any{}, nil)},
			{Name: NotesFolderRename, Description: "Rename a folder and move every note in it. inbox and all are reserved.", Parameters: obj(map[string]any{"from": strProp("current folder name"), "to": strProp("new folder name")}, []string{"from", "to"})},
			{Name: NotesFolderDelete, Description: "Ask to delete every note in one folder. It does not run until the person confirms. Never pass all. inbox clears the inbox.", Parameters: obj(map[string]any{"folder": strProp("folder name")}, []string{"folder"})},
		}
	case "calendar":
		updateEvent := eventFields()
		updateEvent["id"] = intProp("event id")
		return []openrouter.Tool{
			{Name: CalList, Description: "List events from a date to a date (YYYY-MM-DD), including times, repeat, and repeat_until. Series are expanded, so id can repeat.", Parameters: obj(map[string]any{"from": strProp("YYYY-MM-DD"), "to": strProp("YYYY-MM-DD")}, []string{"from", "to"})},
			{Name: CalRead, Description: "Read one event by id, including body, times, repeat, and repeat_until.", Parameters: obj(map[string]any{"id": intProp("event id")}, []string{"id"})},
			{Name: CalCreate, Description: "Create an event. title and starts_on are required. All-day is the default. Timed events need all_day false plus starts_at and ends_at (HH:MM; 2:30 PM and 14h30 work). repeat is none, daily, weekly, monthly, or yearly. repeat_until bounds a series; omit it to repeat forever. There is no location or attendee field; put a place or guests in body. Do not wrap fields in an event object. Birthdays belong on a person in People. Payment-day reminders belong in Spend.", Parameters: obj(eventFields(), []string{"title", "starts_on"})},
			{Name: CalUpdate, Description: "Update an event by id. Partial merge: send only fields that change. Timed events need all_day false plus starts_at and ends_at. repeat_until bounds a series.", Parameters: obj(updateEvent, []string{"id"})},
			{Name: CalDelete, Description: "Ask to delete one event. It does not run until the person confirms. Deleting a series deletes every occurrence.", Parameters: obj(map[string]any{"id": intProp("event id")}, []string{"id"})},
		}
	case "people":
		updateFields := peopleFields()
		updateFields["id"] = intProp("person id")
		return []openrouter.Tool{
			{Name: PeopleSearch, Description: "Search people. Returns id, name, nickname, relationship, a short notes clip.", Parameters: obj(map[string]any{"q": strProp("text")}, nil)},
			{Name: PeopleRead, Description: "Read one person by id, including address, height, sizes, favorites, notes, and attrs.", Parameters: obj(map[string]any{"id": intProp("person id")}, []string{"id"})},
			{Name: PeopleCreate, Description: "Create a person. Only name is required. Store address, height, sizes, favorites, and attrs (label/value rows such as CPF, Pix, or an extra email) in those fields, not only in notes. Birthday is YYYY-MM-DD (or MM-DD if the year is unknown), or birthday_month, birthday_day, and optional birthday_year. Sizes are {ring, shoe, shirt, pants} or flat ring_size, shoe_size, shirt_size, pants_size. Do not wrap fields in a person object. Birthdays sync to Calendar from People.", Parameters: obj(peopleFields(), []string{"name"})},
			{Name: PeopleUpdate, Description: "Update a person by id. Partial merge: send only fields that change and omit the rest. Birthday is YYYY-MM-DD (or MM-DD), or birthday_month, birthday_day, and optional birthday_year. Sizes are {ring, shoe, shirt, pants} or flat ring_size, shoe_size, shirt_size, pants_size; only sizes you include are written. An empty string clears that size, and clears notes, favorites, address, phone, email, nickname, relationship, emoji, or height. A dash in notes is text; send an empty string to remove it. attrs replaces the whole list when the key is present — omit attrs to keep existing rows, and do not send attrs unless those rows should change. Use address, sizes, and attrs instead of parking those facts only in notes.", Parameters: obj(updateFields, []string{"id"})},
			{Name: PeopleDelete, Description: "Ask to delete one person. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("")}, []string{"id"})},
		}
	case "spend":
		updateExpense := expenseFields()
		updateExpense["id"] = intProp("expense id")
		updateSub := subscriptionFields()
		updateSub["id"] = intProp("subscription id")
		updatePay := paymentDayFields()
		updatePay["id"] = intProp("payment day id")
		return []openrouter.Tool{
			{Name: SpendMonth, Description: "Month totals in home currency, plus the expenses, subscriptions, and payment days counted that month.", Parameters: obj(map[string]any{"year": intProp("year, such as 2026"), "month": intProp("1-12")}, []string{"year", "month"})},
			{Name: SpendList, Description: "List expenses for YYYY-MM. Includes category, amount_cents, currency, and a short notes clip.", Parameters: obj(map[string]any{"month": strProp("YYYY-MM"), "category": strProp("food, transport, home, health, leisure, or other")}, nil)},
			{Name: SpendRead, Description: "Read one expense by id, including notes.", Parameters: obj(map[string]any{"id": intProp("expense id")}, []string{"id"})},
			{Name: SpendCreate, Description: "Log one expense, not a subscription. Send amount_cents (integer cents: 2550 is 25.50) or amount (a decimal such as 25.50 or 25,50). currency is BRL, USD, or EUR (default BRL). category is food, transport, home, health, leisure, or other. Put a category, amount, or currency in those fields, not only in notes. There are no tags or splits. Do not wrap fields in an expense object.", Parameters: obj(expenseFields(), []string{"title", "spent_on"})},
			{Name: SpendUpdate, Description: "Update an expense by id. Partial merge: send only fields that change. amount_cents is integer cents; amount is a decimal. category is food, transport, home, health, leisure, or other.", Parameters: obj(updateExpense, []string{"id"})},
			{Name: SpendDelete, Description: "Ask to delete one expense. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("expense id")}, []string{"id"})},
			{Name: SpendSubscriptionList, Description: "List subscriptions (recurring bills). Pass active true to hide paused ones.", Parameters: obj(map[string]any{"active": boolProp("true lists only active subscriptions")}, nil)},
			{Name: SpendSubscriptionRead, Description: "Read one subscription by id, including interval, active, and notes. There is no due day or billing month.", Parameters: obj(map[string]any{"id": intProp("subscription id")}, []string{"id"})},
			{Name: SpendSubscriptionCreate, Description: "Create a subscription, which is a recurring bill and not a one-off expense. title and an amount are required. interval is monthly (default) or yearly. A yearly amount is the charge for the whole year and leftover counts one twelfth each month. active defaults to true. There is no due day or billing month; use a payment day for the reminder. Use amount_cents or amount the same way as an expense. Do not wrap fields in a subscription object.", Parameters: obj(subscriptionFields(), []string{"title"})},
			{Name: SpendSubscriptionUpdate, Description: "Update a subscription by id. Partial merge: send only fields that change. active false pauses it. There is no due day or billing month.", Parameters: obj(updateSub, []string{"id"})},
			{Name: SpendSubscriptionDelete, Description: "Ask to delete one subscription. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("subscription id")}, []string{"id"})},
			{Name: SpendPaymentDayList, Description: "List payment-day reminders. They do not change the leftover and they are not expenses.", Parameters: obj(map[string]any{}, nil)},
			{Name: SpendPaymentDayRead, Description: "Read one payment day by id.", Parameters: obj(map[string]any{"id": intProp("payment day id")}, []string{"id"})},
			{Name: SpendPaymentDayCreate, Description: "Create a payment-day reminder. title and due_day (1-31) are required. It does not log an expense and it does not change the leftover. active defaults to true. Do not wrap fields in a payment_day object.", Parameters: obj(paymentDayFields(), []string{"title", "due_day"})},
			{Name: SpendPaymentDayUpdate, Description: "Update a payment day by id. Partial merge: send only fields that change.", Parameters: obj(updatePay, []string{"id"})},
			{Name: SpendPaymentDayDelete, Description: "Ask to delete one payment day. It does not run until the person confirms.", Parameters: obj(map[string]any{"id": intProp("payment day id")}, []string{"id"})},
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
	args = unwrapArgs(name, args)
	if name == NotesUpdate {
		merged, err := mergeNoteTitle(ctx, c, args)
		if err != nil {
			return Outcome{Body: `{"ok":false,"error":"unavailable"}`, App: app}
		}
		args = merged
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
	case isListTool(name):
		shaped, serr = ShapeList(listKey(name), raw)
	case strings.HasSuffix(name, "_read") || strings.HasSuffix(name, "_create") || strings.HasSuffix(name, "_update"):
		shaped, serr = ShapeOne(objectKey(name), raw)
	}
	if serr != nil {
		shaped = raw
	}
	out.OK = true
	out.Body = stampOK(string(shaped), true)
	// Deletes answer 204 with an empty body. The model is told not to claim
	// a delete unless the tool result says ok true.
	if IsDelete(name) && strings.TrimSpace(out.Body) == "" {
		out.Body = `{"ok":true}`
	}
	out.Title = titleFrom(out.Body, objectKey(name))
	if out.OK && out.Title == "" {
		out.Title = firstArgString(args, "title", "name", "folder", "to")
	}
	if out.ID == 0 {
		out.ID = idFrom(out.Body, objectKey(name))
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

func isListTool(name string) bool {
	switch name {
	case NotesSearch, NotesFolders, PeopleSearch, CalList, SpendList, SpendSubscriptionList, SpendPaymentDayList:
		return true
	}
	return false
}

func listKey(name string) string {
	switch name {
	case NotesSearch:
		return "notes"
	case NotesFolders:
		return "folders"
	case PeopleSearch:
		return "people"
	case CalList:
		return "events"
	case SpendList:
		return "expenses"
	case SpendSubscriptionList:
		return "subscriptions"
	case SpendPaymentDayList:
		return "payment_days"
	}
	return ""
}

func objectKey(name string) string {
	switch {
	case strings.Contains(name, "subscription"):
		return "subscription"
	case strings.Contains(name, "payment_day"):
		return "payment_day"
	case strings.HasPrefix(name, "notes_folder"):
		return ""
	case strings.HasPrefix(name, "notes"):
		return "note"
	case strings.HasPrefix(name, "people"):
		return "person"
	case strings.HasPrefix(name, "calendar"):
		return "event"
	case strings.HasPrefix(name, "spend"):
		return "expense"
	}
	return ""
}

func firstArgString(args map[string]any, keys ...string) string {
	for _, key := range keys {
		if s := strings.TrimSpace(strArg(args, key)); s != "" {
			return s
		}
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

func titleFrom(payload, key string) string {
	var doc map[string]any
	if key == "" || json.Unmarshal([]byte(payload), &doc) != nil {
		return ""
	}
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

func idFrom(payload, key string) int64 {
	var doc map[string]any
	if key == "" || json.Unmarshal([]byte(payload), &doc) != nil {
		return 0
	}
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
		if s := strArg(args, "q"); s != "" {
			q.Set("q", s)
		}
		if s := strings.TrimSpace(strArg(args, "folder")); s != "" {
			if strings.EqualFold(s, "inbox") {
				s = "inbox"
			}
			q.Set("folder", s)
		}
		return http.MethodGet, "/api/v1/notes?" + q.Encode(), nil, "notes", false
	case NotesRead:
		return http.MethodGet, "/api/v1/notes/" + strconv.FormatInt(id, 10), nil, "notes", false
	case NotesCreate:
		return http.MethodPost, "/api/v1/notes", map[string]any{"note": noteBody(args)}, "notes", true
	case NotesUpdate:
		return http.MethodPatch, "/api/v1/notes/" + strconv.FormatInt(id, 10), map[string]any{"note": noteBody(args)}, "notes", true
	case NotesDelete:
		return http.MethodDelete, "/api/v1/notes/" + strconv.FormatInt(id, 10), nil, "notes", true
	case NotesFolders:
		return http.MethodGet, "/api/v1/folders", nil, "folders", false
	case NotesFolderRename:
		return http.MethodPatch, "/api/v1/folders", map[string]any{
			"from": strings.TrimSpace(strArg(args, "from")),
			"to":   strings.TrimSpace(strArg(args, "to")),
		}, "folders", true
	case NotesFolderDelete:
		q.Set("folder", strings.TrimSpace(strArg(args, "folder")))
		return http.MethodDelete, "/api/v1/folders?" + q.Encode(), nil, "folders", true
	case CalList:
		q.Set("from", normalizeDateOrRaw(strArg(args, "from")))
		q.Set("to", normalizeDateOrRaw(strArg(args, "to")))
		return http.MethodGet, "/api/v1/events?" + q.Encode(), nil, "events", false
	case CalRead:
		return http.MethodGet, "/api/v1/events/" + strconv.FormatInt(id, 10), nil, "events", false
	case CalCreate:
		return http.MethodPost, "/api/v1/events", map[string]any{"event": eventBody(args)}, "events", true
	case CalUpdate:
		return http.MethodPatch, "/api/v1/events/" + strconv.FormatInt(id, 10), map[string]any{"event": eventBody(args)}, "events", true
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
		return http.MethodGet, fmt.Sprintf("/api/v1/months/%d/%d", intField(args, "year"), intField(args, "month")), nil, "month", false
	case SpendList:
		q.Set("limit", "20")
		if s := strArg(args, "month"); s != "" {
			q.Set("month", normalizeMonth(s))
		}
		if s := strArg(args, "category"); s != "" {
			q.Set("category", normalizeCategory(s))
		}
		return http.MethodGet, "/api/v1/expenses?" + q.Encode(), nil, "expenses", false
	case SpendRead:
		return http.MethodGet, "/api/v1/expenses/" + strconv.FormatInt(id, 10), nil, "expenses", false
	case SpendCreate:
		return http.MethodPost, "/api/v1/expenses", map[string]any{"expense": expenseBody(args)}, "expenses", true
	case SpendUpdate:
		return http.MethodPatch, "/api/v1/expenses/" + strconv.FormatInt(id, 10), map[string]any{"expense": expenseBody(args)}, "expenses", true
	case SpendDelete:
		return http.MethodDelete, "/api/v1/expenses/" + strconv.FormatInt(id, 10), nil, "expenses", true
	case SpendSubscriptionList:
		if b, ok := boolArg(args["active"]); ok && b {
			q.Set("active", "true")
		}
		return http.MethodGet, withQuery("/api/v1/subscriptions", q), nil, "subscriptions", false
	case SpendSubscriptionRead:
		return http.MethodGet, "/api/v1/subscriptions/" + strconv.FormatInt(id, 10), nil, "subscriptions", false
	case SpendSubscriptionCreate:
		return http.MethodPost, "/api/v1/subscriptions", map[string]any{"subscription": subscriptionBody(args)}, "subscriptions", true
	case SpendSubscriptionUpdate:
		return http.MethodPatch, "/api/v1/subscriptions/" + strconv.FormatInt(id, 10), map[string]any{"subscription": subscriptionBody(args)}, "subscriptions", true
	case SpendSubscriptionDelete:
		return http.MethodDelete, "/api/v1/subscriptions/" + strconv.FormatInt(id, 10), nil, "subscriptions", true
	case SpendPaymentDayList:
		return http.MethodGet, "/api/v1/payment_days", nil, "payment_days", false
	case SpendPaymentDayRead:
		return http.MethodGet, "/api/v1/payment_days/" + strconv.FormatInt(id, 10), nil, "payment_days", false
	case SpendPaymentDayCreate:
		return http.MethodPost, "/api/v1/payment_days", map[string]any{"payment_day": paymentDayBody(args)}, "payment_days", true
	case SpendPaymentDayUpdate:
		return http.MethodPatch, "/api/v1/payment_days/" + strconv.FormatInt(id, 10), map[string]any{"payment_day": paymentDayBody(args)}, "payment_days", true
	case SpendPaymentDayDelete:
		return http.MethodDelete, "/api/v1/payment_days/" + strconv.FormatInt(id, 10), nil, "payment_days", true
	}
	return "", "", nil, "", false
}

func withQuery(path string, q url.Values) string {
	if enc := q.Encode(); enc != "" {
		return path + "?" + enc
	}
	return path
}

func strArg(args map[string]any, k string) string {
	s, _ := args[k].(string)
	return s
}

// putClearable copies a field the caller sent. Absent and null stay
// omitted so a partial update does not change it. A blank or
// whitespace-only string is sent as "" and clears the field. "-" is text.
// Other types, such as a numeric height, are forwarded as given.
func putClearable(args, out map[string]any, key string) {
	v, ok := args[key]
	if !ok || v == nil {
		return
	}
	if s, isStr := v.(string); isStr {
		out[key] = strings.TrimSpace(s)
		return
	}
	out[key] = v
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
// wraps the fields in "person". Address, height, favorites, sizes, and
// attrs are forwarded the same way. attrs is included only when the
// caller sent an array, because the API replaces attrs when the key is
// present.
func personBody(args map[string]any) map[string]any {
	args = flattenPerson(args)
	out := map[string]any{}
	// A blank name is a 422 and would reject the whole update, so omit it.
	if s, ok := nonemptyString(args, "name"); ok {
		out["name"] = s
	}
	for _, key := range []string{"nickname", "relationship", "emoji", "phone", "email", "address", "height", "favorites", "notes"} {
		putClearable(args, out, key)
	}
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
	if sizes := sizesBody(args); len(sizes) > 0 {
		out["sizes"] = sizes
	}
	if attrs, ok := attrsBody(args["attrs"]); ok {
		out["attrs"] = attrs
	}
	return out
}

// sizesBody accepts a nested sizes object or flat ring_size/shoe_size keys.
// Only sizes that were sent are included. The People API writes just those
// keys, so a shoe-only update does not clear ring, shirt, or pants.
// An empty string is sent and clears that one size.
func sizesBody(args map[string]any) map[string]any {
	out := map[string]any{}
	if obj, ok := args["sizes"].(map[string]any); ok {
		for _, key := range []string{"ring", "shoe", "shirt", "pants"} {
			if v, ok := obj[key]; ok && v != nil {
				out[key] = scalarString(v)
			}
		}
	}
	for _, pair := range []struct{ flat, key string }{
		{"ring_size", "ring"},
		{"shoe_size", "shoe"},
		{"shirt_size", "shirt"},
		{"pants_size", "pants"},
	} {
		if v, ok := args[pair.flat]; ok && v != nil {
			out[pair.key] = scalarString(v)
		}
	}
	return out
}

// attrsBody reports whether the attrs key was an array. present is false
// when the key is absent, not an array, or every row lacked a label, so an
// update does not wipe rows the model did not mean to replace. An empty
// array is kept and clears attrs.
func attrsBody(v any) (rows []any, present bool) {
	list, ok := v.([]any)
	if !ok {
		return nil, false
	}
	rows = make([]any, 0, len(list))
	for _, item := range list {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := scalarString(obj["label"])
		if label == "" {
			continue
		}
		rows = append(rows, map[string]any{"label": label, "value": scalarString(obj["value"])})
	}
	if len(list) > 0 && len(rows) == 0 {
		return nil, false
	}
	return rows, true
}

func scalarString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(t)
	case float64:
		if t == float64(int64(t)) && t < 1e15 && t > -1e15 {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case int:
		return strconv.Itoa(t)
	case int64:
		return strconv.FormatInt(t, 10)
	case json.Number:
		return strings.TrimSpace(t.String())
	default:
		return ""
	}
}

func flattenPerson(args map[string]any) map[string]any {
	return flattenRecord(args, "person")
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
