# TansuCalendar API (v1)

A small JSON API so AI agents (OpenClaw, Hermes Agent, …) and other apps can
read and manage your calendar. Same validations as the web UI.

## Authentication

Create a token in the app: **More → API tokens**. Name it after the agent
(e.g. `Hermes Agent`), confirm with your password, and copy the token — it is
shown **once**.

Send it on every request:

```http
Authorization: Bearer [REDACTED]…
```

Notes:

- A token has **full access** to one user's events and birthdays.
- Tokens keep working while the app is locked. Losing one means revoking it
  under **More → API tokens** and generating a new one.
- Only the token digest is stored; the raw token cannot be recovered.

## Conventions

- Base path: `/api/v1` (e.g. `https://calendar.gettansu.com/api/v1/events`).
- Dates are `YYYY-MM-DD`, times are `HH:MM` (24h).
- `POST`/`PATCH` accept fields nested (`{"event": {...}}`) or flat
  (`{"title": ...}`). Both work for `event` and `birthday`.
- Success: `200 OK` (`201 Created` on create, `204 No content` on delete).
- Errors: `401 {"error":"unauthorized"}`, `404 {"error":"not_found"}`,
  `422 {"errors":[...]}` (human-readable messages),
  `429 {"error":"rate_limited"}` (60 writes/minute per token).

## Events

An event: `title` (required, ≤200 chars), `body` notes (optional),
`all_day` (default `true`), `starts_on` / `ends_on` dates, and for timed
events (`all_day: false`) `starts_at` / `ends_at` times. `ends_on` defaults
to `starts_on` when omitted. Optional extras: `emoji` (≤12 runes),
`repeat` (`none`/`daily`/`weekly`/`monthly`/`yearly`, default `none`), and
`repeat_until` (YYYY-MM-DD, bounds occurrence starts; blank repeats forever).

The index expands series: one entry per occurrence in range, with the
occurrence's `starts_on` / `ends_on` plus `occurrence_on` (so `id` repeats
and `(id, occurrence_on)` is unique). Monthly skips months lacking the day;
yearly maps Feb 29 to Feb 28 in common years. Show/create/update return the
series row (template dates plus the `repeat` fields).

```bash
# What's on September 2026? (defaults to the current month)
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/events?from=2026-09-01&to=2026-09-30"

# Range is clamped to 366 days. Bad dates return 422.

# Add an all-day event
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"event":{"title":"Dentist","starts_on":"2026-09-25"}}' \
  "$KURA_URL/api/v1/events"

# Add a timed event (flat params also accepted)
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Call","starts_on":"2026-09-26","all_day":false,"starts_at":"14:30","ends_at":"15:00"}' \
  "$KURA_URL/api/v1/events"

# Read / rename / delete one event
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/events/42"
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"event":{"title":"Dentist (rescheduled)"}}' \
  "$KURA_URL/api/v1/events/42"
curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/events/42"
```

`GET /api/v1/events` returns events overlapping `[from, to]`:

```json
{
  "events": [
    {
      "id": 42, "title": "Dentist", "body": "",
      "all_day": true,
      "starts_on": "2026-09-25", "ends_on": "2026-09-25",
      "starts_at": null, "ends_at": null,
      "emoji": "", "repeat": "none", "repeat_until": null,
      "occurrence_on": "2026-09-25",
      "created_at": "2026-09-19T10:00:00Z",
      "updated_at": "2026-09-19T10:00:00Z"
    }
  ]
}
```

Single-event endpoints wrap the same object as `{"event": {...}}`.

## Birthdays

Birthdays are created in Tansu People and synced here. `POST /api/v1/birthdays`
returns `422` with `{"error":"birthdays_retired"}`. Existing rows (including
ones People syncs) can still be listed, updated, and deleted. A birthday:
`name` (required), `month` (1–12), `day` (1–31, must exist in that month),
optional `year` (shown as age), optional `body` notes and optional `emoji`
(≤12 runes).

```bash
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/birthdays"

curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"birthday":{"year":1991}}' \
  "$KURA_URL/api/v1/birthdays/7"

curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/birthdays/7"
```

## Agent setup snippet

Give the agent three values: the base URL, the token, and these rules:

```text
You manage my TansuCalendar at https://calendar.gettansu.com via its JSON API.
Authenticate every request with: Authorization: Bearer <token>
- Read events: GET /api/v1/events?from=YYYY-MM-DD&to=YYYY-MM-DD
- Create: POST /api/v1/events with {"event":{"title","starts_on","ends_on","all_day","starts_at","ends_at","body","emoji","repeat","repeat_until"}}
- Update: PATCH /api/v1/events/:id — Delete: DELETE /api/v1/events/:id
- Birthdays belong in Tansu People. POST /api/v1/birthdays is retired. GET/PATCH/DELETE still work for rows already on the calendar.
Dates are YYYY-MM-DD, times HH:MM. Timed events need all_day=false plus
starts_at/ends_at. On 422, read the "errors" array and fix the input.
```

One token per app: TansuSpend and TansuNotes will get their own tokens later.
