# Tansu

**One login for every app.** Tansu is a suite of quiet self-hosted apps — one
static Go binary and one SQLite file each. No Redis, no SaaS account, no sync
cloud you did not choose.

| App | Dir | Default port | Production host |
|---|---|---|---|
| Tansu Account (SSO + hub) | `apps/account` | 3006 | `account.gettansu.com` |
| Tansu Notes | `apps/notes` | 3000 | `notes.gettansu.com` |
| Tansu Assistant | `apps/assistant` | 3000 | `assistant.gettansu.com` |
| Tansu Calendar | `apps/calendar` | 3003 | `calendar.gettansu.com` |
| Tansu Spend | `apps/spend` | 3004 | `spend.gettansu.com` |
| Tansu People | `apps/people` | 3005 | `people.gettansu.com` |

The 3000 default is shared by Notes and Assistant — run one at a time, or
override `BIND` (the E2E script below uses scratch ports for the full fleet).

## Run one app

```sh
cd apps/notes
cp .env.example .env   # then edit KURA_HOST
go run ./cmd/kuranotes serve
```

Or with Docker — each app has a `docker-compose.yml`; see its README.

## Run the whole suite with single sign-on

[`scripts/e2e.sh`](scripts/e2e.sh) builds all six apps, starts them on
scratch ports (3200–3206), and walks one account signup through all five
app logins:

```sh
./scripts/e2e.sh start   # build + boot the fleet
./scripts/e2e.sh test    # SSO into every app, hub drawers, standalone check
./scripts/e2e.sh stop
```

The protocol is documented in [`apps/account/ACCOUNT.md`](apps/account/ACCOUNT.md):
first-party OAuth2 code flow with mandatory PKCE, static client registry,
opaque tokens. Each app keeps working standalone — the Account only adds the
"Entrar com Tansu" door next to the local login.

## Deploy the suite

One VPS, Docker, and six DNS `A` records pointing at it
(`account`, `notes`, `assistant`, `calendar`, `spend`, `people` under
your domain). Each app is its own Compose project with its own volume — there
is no shared database and no orchestrator to learn.

**1. Clone and give each app a localhost port.** Notes and Assistant default to 3000,
so pick distinct `BIND`s:

```sh
git clone https://github.com/aquaspy/tansu.git
cd tansu
for a in account notes assistant calendar spend people; do
  cp apps/$a/.env.example apps/$a/.env
done
```

| App | `.env` `BIND` | `KURA_HOST` |
|---|---|---|
| account | `127.0.0.1:3006` | `account.gettansu.com` |
| notes | `127.0.0.1:3000` | `notes.gettansu.com` |
| assistant | `127.0.0.1:3001` | `assistant.gettansu.com` |
| calendar | `127.0.0.1:3003` | `calendar.gettansu.com` |
| spend | `127.0.0.1:3004` | `spend.gettansu.com` |
| people | `127.0.0.1:3005` | `people.gettansu.com` |

Set `FORCE_SSL=true` in every `.env` (Caddy terminates HTTPS below).
Keep `SIGNUP_ENABLED=true` until the first accounts exist, then flip to
`false` everywhere.

On a hosted Account, also set `RESEND_API_KEY` and `RESEND_FROM` (a sender
verified at Resend). Signup then creates the account only after the person
opens the link and re-enters the password. The link uses the first
`KURA_HOST`. Leave both empty for a family self-host: Account signup stays
immediate, and the other apps are unchanged.

**2. Mint one client secret per app** (16+ chars each; they never travel
except over your own HTTPS):

```sh
for a in notes assistant calendar spend people; do
  echo "$a: $(openssl rand -hex 24)"
done
```

**3. Register the five apps on the Account.** In `apps/account/.env`,
`KURA_CLIENTS_JSON` is one JSON array — ids must match each app's
`KURA_CLIENT_ID`, secrets the matching `KURA_CLIENT_SECRET`, and each
`redirect_uris` entry must be exactly `https://<host>/login/kura/callback`:

```json
[
  {"id": "kuranotes", "secret": "<notes-secret>", "name": "Tansu Notes", "home": "https://notes.gettansu.com/", "icon": "📝", "redirect_uris": ["https://notes.gettansu.com/login/kura/callback"]},
  {"id": "kurachat", "secret": "<assistant-secret>", "name": "Tansu Assistant", "home": "https://assistant.gettansu.com/", "icon": "✨", "redirect_uris": ["https://assistant.gettansu.com/login/kura/callback"]},
  {"id": "kuracalendar", "secret": "<calendar-secret>", "name": "Tansu Calendar", "home": "https://calendar.gettansu.com/", "icon": "📅", "redirect_uris": ["https://calendar.gettansu.com/login/kura/callback"]},
  {"id": "kuraspend", "secret": "<spend-secret>", "name": "Tansu Spend", "home": "https://spend.gettansu.com/", "icon": "💸", "redirect_uris": ["https://spend.gettansu.com/login/kura/callback"]},
  {"id": "kurapeople", "secret": "<people-secret>", "name": "Tansu People", "home": "https://people.gettansu.com/", "icon": "🧑", "redirect_uris": ["https://people.gettansu.com/login/kura/callback"]}
]
```

**4. Point each app at the Account.** In every app `.env` except the
Account's own:

```bash
KURA_ACCOUNT_URL=https://account.gettansu.com
KURA_CLIENT_ID=kuranotes            # kurachat, kuracalendar, ...
KURA_CLIENT_SECRET=<matching-secret>
```

Leave `KURA_ACCOUNT_URL` empty on any app to keep it standalone — its local
login keeps working exactly as before, Account or no Account.

Birthdays typed in People show up in Calendar when both apps share one
secret. On People set `KURA_CALENDAR_URL=https://calendar.example.com` and
`KURA_SYNC_SECRET` (16+ chars). Set the same `KURA_SYNC_SECRET` on Calendar.
The Calendar user is the one with the same Account subject, or the same
email if that app is still standalone. Theme, language, and the lock travel
with the browser across the suite: on a shared parent domain
(`notes.example.com` and `calendar.example.com`) and, on one machine,
across ports of `127.0.0.1`.

**5. Boot everything, Account first:**

```sh
for a in account notes assistant calendar spend people; do
  (cd apps/$a && docker compose up -d --build)
done
```

**6. Front it with Caddy** (automatic HTTPS for all six hosts):

```
account.gettansu.com {
  reverse_proxy 127.0.0.1:3006
}
notes.gettansu.com {
  reverse_proxy 127.0.0.1:3000
}
assistant.gettansu.com {
  reverse_proxy 127.0.0.1:3001
}
calendar.gettansu.com {
  reverse_proxy 127.0.0.1:3003
}
spend.gettansu.com {
  reverse_proxy 127.0.0.1:3004
}
people.gettansu.com {
  reverse_proxy 127.0.0.1:3005
}
```

Then create the first Account user in the browser (or
`docker compose exec web ./kuraaccount create` with `EMAIL`/`PASSWORD`),
open each app once from the Account hub (the tile runs "Entrar com Tansu"
for you), and flip `SIGNUP_ENABLED=false`
in all six `.env` files followed by `docker compose up -d` per app.
(`restart` does **not** reload `.env`.)

**Updates** are per app, in any order — the SSO protocol is backwards
compatible and apps never go down together unless you take them down:

```sh
cd tansu && git pull
(cd apps/notes && docker compose up -d --build)
```

**Backups** are the six volumes (one SQLite file each, plus Assistant
uploads). Any consistent copy works; per app:

```sh
(cd apps/notes && docker compose exec web tar -C /data -cf - . > notes-backup.tar)
```

Volumes: `kura_account_data`, `kura_notes_data`, `kura_chat_data`,
`kura_calendar_data`, `kura_spend_data`, `kura_people_data`.

## Layout

Each app is independent: its own `go.mod`, Dockerfile, CI job, and SQLite
file. Nothing imports across apps; the only cross-app contract is the SSO
protocol plus the JSON export markers (`TansuCalendar`, `TansuSpend`).

> **Why does the code say `kura`?** The suite started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

## History

The Tansu suite was imported from the `go` branches of the `Kura*` repos
(Rails lived on their `master`). Those repos are archived; per-app import
commits reference the source SHAs.
