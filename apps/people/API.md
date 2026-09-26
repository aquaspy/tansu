# TansuPeople API (v1)

A small JSON API so AI agents (OpenClaw, Hermes Agent, …) and other apps can
read and manage your people. Same validations as the web UI.

## Authentication

Create a token in the app: **More → API tokens**. Name it after the agent
(e.g. `Hermes Agent`), confirm with your password, and copy the token — it is
shown **once**.

Send it on every request:

```http
Authorization: Bearer [REDACTED]…
```

Notes:

- A token has **full access** to one user's people.
- Tokens keep working while the app is locked. Losing one means revoking it
  under **More → API tokens** and generating a new one.
- Only the token digest is stored; the raw token cannot be recovered.

## Conventions

- Base path: `/api/v1` (e.g. `https://people.gettansu.com/api/v1/people`).
- `POST`/`PATCH` accept fields nested (`{"person": {...}}`) or flat
  (`{"name": ...}`).
- Birthday accepts a nested object
  (`{"birthday": {"month": 8, "day": 11, "year": 1990}}`) or flat keys
  (`birthday_month`, `birthday_day`, `birthday_year`).
- Sizes accept a nested object
  (`{"sizes": {"ring": "16", "shoe": "37"}}`) or flat keys
  (`ring_size`, `shoe_size`, `shirt_size`, `pants_size`).
- `PATCH` merges onto the record; `attrs` is replaced only when the key is
  present.
- Success: `200 OK` (`201 Created` on create, `204 No content` on delete).
- Errors: `401 {"error":"unauthorized"}`, `404 {"error":"not_found"}`,
  `422 {"errors":[...]}` (human-readable messages),
  `429 {"error":"rate_limited"}` (60 writes/minute per token).

## People

A person: `name` (required, ≤200 chars), `nickname`, `relationship`,
`emoji`, `phone`, `email`, `address`, `height`, sizes, `favorites`,
`notes`, plus `attrs` (free `[{"label","value"}]` rows).

```bash
# Latest 50 people (set ?limit= up to 200)
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/people"

# Filter by text, relationship, or birth month
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/people?q=ada&relationship=friend&birthday_month=8"

# Add someone
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"person":{
    "name": "Ada Lovelace", "nickname": "Ada", "relationship": "friend",
    "birthday": {"month": 8, "day": 11, "year": 2000},
    "sizes": {"ring": "16", "shoe": "37"}, "height": "1.65m",
    "favorites": "mechanical keyboards",
    "attrs": [{"label": "Coffee", "value": "flat white"}]
  }}' \
  "$KURA_URL/api/v1/people"

# Read / rename / delete
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/people/42"
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"person": {"nickname": "A."}}' \
  "$KURA_URL/api/v1/people/42"
curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/people/42"
```

`GET /api/v1/people` returns full cards:

```json
{
  "people": [
    {
      "id": 42, "name": "Ada Lovelace", "nickname": "Ada",
      "relationship": "friend",
      "birthday": {"month": 8, "day": 11, "year": 2000},
      "emoji": "🎂", "phone": "", "email": "", "address": "",
      "sizes": {"ring": "16", "shoe": "37", "shirt": "", "pants": ""},
      "height": "1.65m",
      "favorites": "", "favorites": "mechanical keyboards", "notes": "",
      "attrs": [{"label": "Coffee", "value": "flat white"}],
      "countdown": {
        "days_until_birthday": 12,
        "next_birthday": "2026-08-11",
        "turns": 26
      },
      "created_at": "2026-09-19T10:00:00Z",
      "updated_at": "2026-09-19T10:00:00Z"
    }
  ]
}
```

Single-person endpoints wrap the same object as `{"person": {...}}`.
`countdown` is `null` when no birthday is set; `turns` is `null` when the
birth year is unknown.

## Agent setup snippet

Give the agent three values: the base URL, the token, and these rules:

```text
You manage my TansuPeople at https://people.gettansu.com via its JSON API.
Authenticate every request with: Authorization: Bearer <token>
- Read people: GET /api/v1/people?q=&relationship=&birthday_month=&limit=
- Create: POST /api/v1/people with {"person":{"name","nickname","relationship","birthday":{"month","day","year"},"emoji","phone","email","address","height","sizes":{"ring","shoe","shirt","pants"},"favorites","notes","attrs":[{"label","value"}]}}
- Update: PATCH /api/v1/people/:id (merges; attrs replaced only when present) — Delete: DELETE /api/v1/people/:id
Only "name" is required. Use "countdown" for upcoming birthdays. On 422, read the "errors" array and fix the input.
```

One token per app: TansuCalendar, TansuSpend, and TansuNotes each have their own.
