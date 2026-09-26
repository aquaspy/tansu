# TansuNotes API (v1)

A small JSON API so AI agents (OpenClaw, Hermes Agent, …) and other apps can
read and manage your notes. Same validations and folder rules as the web UI.

## Authentication

Create a token in the app: **More → API tokens**. Name it after the agent
(e.g. `Hermes Agent`), confirm with your password, and copy the token — it is
shown **once**.

Send it on every request:

```http
Authorization: Bearer kura_xxx…
```

Notes:

- A token has **full access** to one user's notes and folders.
- Tokens keep working while the app is locked. Losing one means revoking it
  under **More → API tokens** and generating a new one.
- Only the token digest is stored; the raw token cannot be recovered.

## Conventions

- Base path: `/api/v1` (e.g. `https://notes.gettansu.com/api/v1/notes`).
- `POST`/`PATCH` accept fields nested (`{"note": {...}}`) or flat
  (`{"body": ...}`). Both work for `note`; folder endpoints take flat params.
- Success: `200 OK` (`201 Created` on create, `204 No content` on delete).
- Errors: `401 {"error":"unauthorized"}`, `404 {"error":"not_found"}`,
  `422 {"errors":[...]}` (human-readable messages),
  `429 {"error":"rate_limited"}` (60 writes/minute per token).
- Share links stay web-only: the API reports whether a note is shared but
  never mints or reveals share tokens.

## Notes

A note is just `body` (plain text, first line becomes the title) plus an
optional `folder` (≤80 chars). `all` and `inbox` are reserved names and land
in the inbox (`""`).

```bash
# Latest 50 notes (newest first)
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/notes"

# Filter by folder or search title + body (?limit= up to 200)
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/notes?folder=work&q=deploy&limit=20"

# Create a note in the right folder
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"note":{"body":"Trip ideas\nKyoto in April","folder":"travel"}}' \
  "$KURA_URL/api/v1/notes"

# Read / edit / move / delete one note
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/notes/42"
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"note":{"body":"Trip ideas\nKyoto in April\nOsaka too"}}' \
  "$KURA_URL/api/v1/notes/42"
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"folder":"done"}' \
  "$KURA_URL/api/v1/notes/42"
curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/notes/42"
```

`GET /api/v1/notes` returns full bodies:

```json
{
  "notes": [
    {
      "id": 42, "title": "Trip ideas",
      "body": "Trip ideas\nKyoto in April",
      "preview": "Kyoto in April",
      "folder": "travel", "shared": false,
      "created_at": "2026-09-19T10:00:00Z",
      "updated_at": "2026-09-19T10:00:00Z"
    }
  ]
}
```

Single-note endpoints wrap the same object as `{"note": {...}}`.

## Folders

```bash
# List folders with note counts ("" is the inbox)
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/folders"

# Rename a folder (moves every note in it)
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from":"work","to":"job"}' \
  "$KURA_URL/api/v1/folders"

# Clear a folder (deletes its notes; inbox via "inbox", never "all")
curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/folders?folder=home"
```

Reserved names (`all`, `inbox`, blank) are rejected with `422`.

## Agent setup snippet

Give the agent three values: the base URL, the token, and these rules:

```text
You manage my TansuNotes at https://notes.gettansu.com via its JSON API.
Authenticate every request with: Authorization: Bearer <token>
- Read notes: GET /api/v1/notes?folder=<name>&q=<search>&limit=50
- Create: POST /api/v1/notes with {"note":{"body","folder"}}
- Update: PATCH /api/v1/notes/:id — Delete: DELETE /api/v1/notes/:id
- Folders: GET /api/v1/folders lists names + counts;
  PATCH /api/v1/folders with {"from","to"} renames;
  DELETE /api/v1/folders?folder=<name> clears one folder.
First line of the body is the title. Put notes in the folder the user asks
for; "inbox" means folder "". On 422, read the "errors" array and fix the input.
```

One token per app: TansuCalendar, TansuSpend, and the others each have their own.
