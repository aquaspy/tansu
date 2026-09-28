# Tansu Email API (v1)

JSON API for a signed-in user's mailboxes. Listing, search, and reads go to
the IMAP server when the request arrives. Message bodies are not stored in
SQLite. Sending uses SMTP.

## Authentication

Create a token in the app: **More → API tokens**. Copy it once.

```http
Authorization: Bearer kura_xxx…
```

The `Bearer` prefix is optional. Tokens keep working while the app is locked.
A token can read and change every mailbox that belongs to that user.

## Conventions

- Base path: `/api/v1` (e.g. `https://email.gettansu.com/api/v1/accounts`).
- Mailbox creates accept a nested `mailbox` object or the same fields flat.
- Success: `200`, `201` on create, `204` on delete, trash, and token revoke.
- Errors: `401 {"error":"unauthorized"}`, `404 {"error":"not_found"}`,
  `422 {"error":"secrets_key"}` or `{"errors":["from_address", ...]}`,
  `422 {"error":"no_trash"}`, `502 {"error":"mail_unavailable"}`.
- Passwords are never returned. `password_set` is `true` when one is stored.
- `last_error` is scrubbed of the password text.

## Mailboxes

`GET /api/v1/accounts` — `{ "accounts": [ ... ] }`

`POST /api/v1/accounts` — create, then test IMAP login and SMTP AUTH.
The row is saved even when the test fails (`last_ok` null, `last_error` set).

```json
{
  "display_name": "Me",
  "from_address": "me@example.com",
  "username": "me",
  "password": "app-password",
  "imap_host": "imap.example.com",
  "imap_port": 993,
  "imap_tls": "tls",
  "smtp_host": "smtp.example.com",
  "smtp_port": 587,
  "smtp_tls": "starttls"
}
```

`imap_tls` and `smtp_tls`: `tls` (implicit), `starttls`, or `none`.

`GET /api/v1/accounts/{id}`

`PATCH /api/v1/accounts/{id}` — same fields. An empty `password` keeps the saved one and retests.

`DELETE /api/v1/accounts/{id}` — `204`. Removes the saved mailbox, not the mail on the server.

`DELETE /api/v1/token` — revokes the bearer token. `204`.

## Mail

Folder and message paths take the mailbox id. UIDs are per folder. Pass
`folder` as a query parameter (default `INBOX`).

`GET /api/v1/accounts/{id}/folders`

```json
{ "folders": [ { "name": "INBOX", "special": "inbox", "selectable": true } ] }
```

`special` is `inbox`, `sent`, `trash`, `drafts`, `junk`, `archive`, or empty.

`GET /api/v1/accounts/{id}/messages?folder=INBOX&q=&page=1`

Search is sent to IMAP. Empty `q` loads one page of headers by sequence
number (newest first) and does not download every UID. `from:ada`,
`subject:hello`, and `since:2026-09-01` use those criteria. Other words
match From, To, Cc, or Subject. `text:` and `body:` search the newest 200
messages, not the whole mailbox: a full body scan does not finish on a
large Dovecot inbox. Page size is 30. At most 1000 newest matches are paged.
`capped` is true when the result is that newest slice, or when a body
search did not cover the whole mailbox.

```json
{
  "messages": [
    { "uid": 11, "from": "Ada <ada@example.com>", "to": "Bob <bob@example.com>", "subject": "Hello", "date": "2026-09-28T12:00:00Z", "seen": true }
  ],
  "total": 1,
  "page": 1,
  "page_size": 30,
  "folder": "INBOX",
  "query": "hello"
}
```

`GET /api/v1/accounts/{id}/messages/{uid}?folder=INBOX`

```json
{ "message": { "uid": 11, "from": "", "to": "", "cc": "", "subject": "", "date": "", "seen": true, "text": "", "attachments": [ { "index": 0, "name": "a.pdf", "mime": "application/pdf", "size": 12 } ] } }
```

HTML parts are reduced to text. The raw HTML is not returned.

`GET /api/v1/accounts/{id}/messages/{uid}/attachments/{part}?folder=INBOX` — file bytes.

`POST /api/v1/accounts/{id}/messages/{uid}/trash?folder=INBOX` — move to Trash. `204`.
If the folder is already Trash, the message is deleted. `422 {"error":"no_trash"}`
when the server has no Trash folder.

`DELETE /api/v1/accounts/{id}/messages/{uid}?folder=INBOX` — mark deleted and expunge. `204`.

## Compose

`POST /api/v1/accounts/{id}/preview` does not touch SMTP.

```json
{ "to": ["bob@example.com"], "cc": [], "bcc": [], "subject": "Hi", "body": "There", "in_reply_to": "", "references": "" }
```

`text` is accepted as an alias of `body`. Response:

```json
{ "preview": { "from": "Me <me@example.com>", "to": ["bob@example.com"], "subject": "Hi", "body": "There" }, "sent": false }
```

`POST /api/v1/accounts/{id}/send` — same body, then SMTP. `{ "sent": true, "subject": "Hi" }`.

Assistant must call preview first and wait for the person to confirm before send.
