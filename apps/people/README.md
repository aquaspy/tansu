# TansuPeople

**The people in your life, on cards — birthdays, sizes, favorites, and the little things worth remembering.**

TansuPeople is a quiet personal CRM PWA. Keep a card per person: who they are, when to celebrate them, what size ring they wear, and what would make a good gift. Several people can share one server with separate accounts. One static Go binary (~14 MB, ~20 MB RAM) serves the whole app on a single SQLite file. No Redis. No contacts sync. No social graph.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

---

## Philosophy

Contact apps want to be your address book: sync protocols, duplicate mergers, vCard arcana. That is a real product for phones. It is the wrong product for “what size does she wear” and “don’t forget mom’s birthday.”

TansuPeople is the second kind.

- **Cards, not contacts.** A person is a card with a name, a birthday, sizes, favorites, and free rows for the specifics.
- **Birthdays with a countdown.** The upcoming strip tells you what is near; every birthday exports to TansuCalendar (or any calendar) as `.ics`.
- **Gifts that land.** Ring, shoe, shirt, pants, height, address, favorites — the practical half of remembering someone.
- **Your data stays a file.** People live in one SQLite file. Back that up.
- **No sync cosplay.** If you need CardDAV and phone sync, use something built for that. This app is for *you*, on a VPS you trust.
- **Same Tansu shell.** Auth, idle lock (per device), PWA offline card views, Compose on localhost.

Sister apps: [TansuNotes](../notes), [Tansu Assistant](../assistant), [TansuSpend](../spend), [TansuCalendar](../calendar). Each keeps its own volume — your people should not share a database with chat history.

---

## What you get

- Multi-user accounts on one instance
- Person cards with nickname, relationship, emoji, birthday
- Sizes (height, ring, shoe, shirt, pants), contact, address, favorites, notes
- Free label/value rows per card for the specifics (coffee order, allergies, …)
- Search + relationship filter chips + upcoming-birthdays strip with countdowns
- Birthday export: one card or all birthdays as `.ics` (imports into TansuCalendar)
- API tokens + JSON API for AI agents (see API.md)
- Offline: reopen cards you already opened; edits wait until you are back
- Long-lived sessions with an optional idle lock (**per device**, not synced in the account DB); sign-out wipes the offline cache

**What you do not get (on purpose):** CardDAV/phone sync, shared address books, photo uploads, E2E encryption, outbound email password reset, or a bundled reverse proxy. You bring your own Caddy or nginx.

---

## Self-host (Docker Compose)

You need Docker on a VPS (or a home box). The app binds to localhost only — port 80/443 stay free for your proxy.

```bash
git clone https://github.com/aquaspy/tansu.git
cd tansu/apps/people
cp .env.example .env
```

Edit `.env`. At minimum:

```bash
KURA_HOST=people.gettansu.com
SIGNUP_ENABLED=true       # first account, then flip to false
FORCE_SSL=false           # true once HTTPS terminates in front
BIND=127.0.0.1:3005       # 3005 if the other apps already took the lower ports
```

Then:

```bash
docker compose up -d --build
```

Create the first account in the browser (`http://127.0.0.1:3005`) — **or** from the shell:

```bash
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kurapeople create
```

Lock public signup so the internet cannot mint accounts on your box:

```bash
# in .env
SIGNUP_ENABLED=false
docker compose up -d
```

> **Important:** `docker compose restart` does **not** reload `.env`. Always use `docker compose up -d` after changing environment variables.

There are no cookie-signing secrets to manage: sessions are opaque random ids in SQLite. Losing the database loses everything; losing anything else loses nothing.

### Reverse proxy (Caddy or nginx)

Nothing is bundled. Point your proxy at whatever `BIND` you chose, set `FORCE_SSL=true`, then `docker compose up -d`.

**Caddy:**

```
people.gettansu.com {
  reverse_proxy 127.0.0.1:3005
}
```

**nginx:**

```
location / {
  proxy_pass http://127.0.0.1:3005;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
}
```

### Users on the server

There is no email recovery. Reset passwords from the box:

```bash
docker compose exec web ./kurapeople users
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kurapeople create
docker compose exec -e EMAIL=you@example.com -e PASSWORD='new-secret' web ./kurapeople password
```

### Backup

People live in the `kura_people_data` volume (`/data/kurapeople.sqlite3`). Back that up.

```bash
docker compose exec web tar -C /data -cf - . > kurapeople-backup.tar
```

### Shared browsers

Sign out **and** wait for the cache wipe. Until then, another person who opens the PWA offline can see cached pages from the previous user.

---

## Calendar bridge

Each Tansu app keeps its own database, so birthdays travel as files:

- **TansuCalendar:** “Send to calendar” on the detail page (or **More → TansuCalendar (.json)** for everyone) downloads birthdays in the TansuCalendar import format — bring the file to **More → Import** over there.
- **Any other calendar:** **More → Birthdays (.ics)** downloads all birthdays as `.ics` (Google, Apple, Outlook, …). UIDs are stable per person, so re-importing updates instead of duplicating.

---

## AI agents (API)

TansuPeople is ready for the agentic era: mint a token under **More → API tokens**, hand it to OpenClaw, Hermes Agent, or any HTTP client, and it can read and manage your people — even while the app is locked. See [API.md](API.md) for endpoints, curl examples, and a setup snippet.

---

## Local development

You need Go 1.27+, plus the `templ` and `tailwindcss` binaries:

```bash
go install github.com/a-h/templ/cmd/templ@latest
# tailwindcss: https://github.com/tailwindlabs/tailwindcss/releases
```

Then:

```bash
templ generate
tailwindcss --input web/static/css/input.css --output web/static/css/app.css
go run ./cmd/kurapeople serve
```

Open http://127.0.0.1:3005

```bash
go test ./...   # suite: store, HTTP flows, API
go vet ./...
```

---

## Environment

| Variable | What it does |
| --- | --- |
| `SIGNUP_ENABLED` | Public password signup. Off still lets a completed Account SSO create the local user |
| `FORCE_SSL` | `true` when Caddy/nginx terminates HTTPS |
| `KURA_HOST` | Public hostname (comma-separated if several) |
| `BIND` | Default `127.0.0.1:3005` |
| `DATA_DIR` | Where `kurapeople.sqlite3` lives. Default `storage` (`/data` in Docker) |
| `KURA_ACCOUNT_URL` | Tansu Account base URL. Empty = standalone, no SSO button |
| `KURA_CLIENT_ID` / `KURA_CLIENT_SECRET` | This app's OAuth credentials (must match its registry entry). See [Deploy the suite](../../README.md#deploy-the-suite) |

---

## Sister apps

| App | Role |
| --- | --- |
| [TansuNotes](../notes) | Private notes |
| [Tansu Assistant](../assistant) | Private chat with your model |
| [TansuSpend](../spend) | Subscriptions & daily spend |
| [TansuCalendar](../calendar) | Personal calendar & birthdays |

Same spirit. Separate databases. Your stack, your rules.
