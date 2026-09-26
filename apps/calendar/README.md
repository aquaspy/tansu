# TansuCalendar

**A calendar that remembers people and days — not every meeting protocol on earth.**

TansuCalendar is a quiet personal calendar PWA. Mark days, keep yearly birthdays, turn on public holidays for the countries you care about. Several people can share one server with separate accounts. One static Go binary (~14 MB, ~20 MB RAM) serves the whole app on a single SQLite file. No Redis. No CalDAV. No ICS-as-the-database.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

---

## Philosophy

Calendar software tends to become infrastructure: invite RSVPs, free/busy, timezone hell, sync conflicts across three devices and a watch. That is a real product for workplaces. It is the wrong product for “when is mom’s birthday” and “don’t forget the long weekend.”

TansuCalendar is the second kind.

- **Days you mark, birthdays that repeat.** Simple events with times when you need them. Birthdays that come back every year without ceremony.
- **Holidays as packs, not plugins.** Flip on Brazil, the United States, Slovenia, and/or Czechia. Enough for a life that spans places — not a marketplace of calendar feeds.
- **Your data stays a file.** Export JSON when you want a copy. Import adds; it does not overwrite your life by accident.
- **No protocol cosplay.** If you need CalDAV and shared free/busy, use something built for that. This app is for *you*, on a VPS you trust.
- **Same Tansu shell.** Auth, idle lock (per device), PWA offline month views, Compose on localhost.

Sister apps: [TansuNotes](https://github.com/aquaspy/TansuNotes), [TansuChat](https://github.com/aquaspy/TansuChat), [TansuHome](https://github.com/aquaspy/TansuHome), [TansuSpend](https://github.com/aquaspy/TansuSpend). Each keeps its own volume — a calendar should not share a database with chat history.

---

## What you get

- Multi-user accounts on one instance
- Month (and day) views with events and birthdays
- Recurring events (daily, weekly, monthly, yearly, optional end date) and emoji on events and birthdays
- Holiday packs: **BR**, **US**, **SI**, **CZ**
- JSON export / import (import adds rows; it does not replace)
- API tokens + JSON API for AI agents (see API.md)
- Offline: reopen months you already opened; edits wait until you are back
- Long-lived sessions with an optional idle lock (**per device**, not synced in the account DB); sign-out wipes the offline cache

**What you do not get (on purpose):** CalDAV, shared calendars, invites, E2E encryption, outbound email password reset, or a bundled reverse proxy. You bring your own Caddy or nginx.

---

## Self-host (Docker Compose)

You need Docker on a VPS (or a home box). The app binds to localhost only — port 80/443 stay free for your proxy.

```bash
git clone https://github.com/aquaspy/TansuCalendar.git
cd KuraCalendar
cp .env.example .env
```

Edit `.env`. At minimum:

```bash
KURA_HOST=calendar.gettansu.com
SIGNUP_ENABLED=true       # first account, then flip to false
FORCE_SSL=false           # true once HTTPS terminates in front
BIND=127.0.0.1:3003       # 3003 if Notes/Chat/Home already took 3000+
```

Then:

```bash
docker compose up -d --build
```

Create the first account in the browser (`http://127.0.0.1:3003`) — **or** from the shell:

```bash
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kuracalendar create
```

Lock public signup so the internet cannot mint accounts on your box:

```bash
# in .env
SIGNUP_ENABLED=false
docker compose up -d
```

> **Important:** `docker compose restart` does **not** reload `.env`. Always use `docker compose up -d` after changing environment variables.

There are no cookie-signing secrets to manage: sessions are opaque random ids in SQLite. Losing the database loses everything; losing anything else loses nothing.

### Coming from the Rails version

The Go app reads its own `kuracalendar.sqlite3`, so the Rails database is imported once:

```bash
# 1. Back up the old volume.
docker compose exec web tar -C /rails/storage -cf - . > kuracalendar-rails-backup.tar

# 2. Deploy the Go image (same kura_calendar_data volume, now mounted at /data).
docker compose up -d --build

# 3. Import the old database into the new layout.
docker compose exec web ./kuracalendar import /data/production.sqlite3

# 4. Verify in the browser, then delete the legacy file:
#    /data/production.sqlite3*.
```

Users keep their passwords, holiday packs, events, birthdays, and API tokens (same `kura_…` values — agents keep working). Everyone signs in again (sessions are not imported).

### Reverse proxy (Caddy or nginx)

Nothing is bundled. Point your proxy at whatever `BIND` you chose, set `FORCE_SSL=true`, then `docker compose up -d`.

**Caddy:**

```
calendar.gettansu.com {
  reverse_proxy 127.0.0.1:3003
}
```

**nginx:**

```
location / {
  proxy_pass http://127.0.0.1:3003;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
}
```

### Users on the server

There is no email recovery. Reset passwords from the box:

```bash
docker compose exec web ./kuracalendar users
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kuracalendar create
docker compose exec -e EMAIL=you@example.com -e PASSWORD='new-secret' web ./kuracalendar password
```

### Backup

Events and birthdays live in the `kura_calendar_data` volume (`/data/kuracalendar.sqlite3`). Back that up.

```bash
docker compose exec web tar -C /data -cf - . > kuracalendar-backup.tar
```

### Shared browsers

Sign out **and** wait for the cache wipe. Until then, another person who opens the PWA offline can see cached pages from the previous user.

---

## Import / export

**Export** downloads JSON of events and birthdays.

**Import** accepts that same JSON (a Rails-era export works too). It **adds** rows; it does not replace existing ones. Cap is 500 events + 500 birthdays per import; rows that fail validation are skipped and not counted.

---

## AI agents (API)

TansuCalendar is ready for the agentic era: mint a token under **More → API tokens**, hand it to OpenClaw, Hermes Agent, or any HTTP client, and it can read and manage events and birthdays — even while the app is locked. See [API.md](API.md) for endpoints, curl examples, and a setup snippet.

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
go run ./cmd/kuracalendar serve
```

Open http://127.0.0.1:3003

```bash
go test ./...   # suite: store, holidays, grid, importer, HTTP flows, API
go vet ./...
```

---

## Environment

| Variable | What it does |
| --- | --- |
| `SIGNUP_ENABLED` | Public signup form. Turn off after the first account |
| `FORCE_SSL` | `true` when Caddy/nginx terminates HTTPS |
| `KURA_HOST` | Public hostname (comma-separated if several) |
| `BIND` | Default `127.0.0.1:3003` |
| `DATA_DIR` | Where `kuracalendar.sqlite3` lives. Default `storage` (`/data` in Docker) |

---

## Sister apps

| App | Role |
| --- | --- |
| [TansuNotes](https://github.com/aquaspy/TansuNotes) | Private notes |
| [TansuChat](https://github.com/aquaspy/TansuChat) | Private chat with your model |
| [TansuHome](https://github.com/aquaspy/TansuHome) | Quiet start-page / homepage |
| [TansuSpend](https://github.com/aquaspy/TansuSpend) | Subscriptions & daily spend |

Same spirit. Separate databases. Your stack, your rules.
