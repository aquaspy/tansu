# TansuAccount

**One login for every Tansu app.**

TansuAccount is the suite's identity service: one account, one password, and a
hub that opens every app. Each app keeps working standalone — the Account only
adds the "Entrar com Tansu" door next to the local login. Same deal as the
siblings: one static Go binary on a single SQLite file. No Redis. No external
IdP. No JWT.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

---

## Philosophy

A central login must earn its place: it may never become a single point of
failure for people who never asked for it.

- **Progressive.** No `KURA_ACCOUNT_URL` on an app, no Account anything: local
  login, exactly like before. The Account is an addition, never a requirement.
- **Boring protocol.** First-party OAuth2 code flow with mandatory PKCE, static
  client registry, opaque tokens, no JWT, no refresh tokens, no consent screen.
  Small enough to audit, strict enough to trust.
- **Apps own their sessions.** The Account says "this email authenticated"; each
  app keeps its own 30-day local session. The Account can go down without
  logging anyone out of their apps.
- **Same Tansu shell.** Auth, idle lock (per device), PWA, Compose on localhost.

Sister apps: [TansuNotes](../notes), [Tansu Assistant](../assistant), [TansuHome](../home), [TansuSpend](../spend), [TansuCalendar](../calendar), [TansuPeople](../people). Each keeps its own volume.

---

## What you get

- Central signup/login for the suite
- OAuth2 authorize + code + userinfo for first-party apps (see ACCOUNT.md)
- Hub: launcher grid, per-app linked state, and a fox that grows tails
- Multi-user accounts on one instance
- Offline hub shell; apps need a connection to open
- Long-lived sessions with an optional idle lock (**per device**); sign-out
  wipes the offline cache

**What you do not get (on purpose):** Single logout, refresh tokens,
`id_token`/JWKS, dynamic client registration, consent screens, central API
tokens, passkeys, or a bundled reverse proxy. See ACCOUNT.md for the roadmap
order if any of those earns its place.

---

## Self-host (Docker Compose)

```bash
git clone ../account.git
cd KuraAccount
cp .env.example .env
```

Edit `.env`. At minimum:

```bash
KURA_HOST=account.gettansu.com
SIGNUP_ENABLED=true       # first account, then flip to false
FORCE_SSL=false           # true once HTTPS terminates in front
BIND=127.0.0.1:3006
KURA_CLIENTS_JSON=[...]   # one entry per suite app (see .env.example)
```

Then:

```bash
docker compose up -d --build
```

Create the first account in the browser (`http://127.0.0.1:3006`) — **or** from the shell:

```bash
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kuraaccount create
```

Lock public signup so the internet cannot mint accounts on your box:

```bash
# in .env
SIGNUP_ENABLED=false
docker compose up -d
```

> **Important:** `docker compose restart` does **not** reload `.env`. Always use `docker compose up -d` after changing environment variables.

Point each suite app at the Account with `KURA_ACCOUNT_URL=http://127.0.0.1:3006`
(or its public URL) plus the matching `KURA_CLIENT_ID` / `KURA_CLIENT_SECRET`
from the registry entry. Then its login page grows the "Entrar com Tansu" button.
Leave `KURA_ACCOUNT_URL` empty to keep an app standalone.

### Reverse proxy (Caddy or nginx)

Nothing is bundled. Point your proxy at whatever `BIND` you chose, set `FORCE_SSL=true`, then `docker compose up -d`.

**Caddy:**

```
account.gettansu.com {
  reverse_proxy 127.0.0.1:3006
}
```

### Users on the server

There is no email recovery. Reset passwords from the box:

```bash
docker compose exec web ./kuraaccount users
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kuraaccount create
docker compose exec -e EMAIL=you@example.com -e PASSWORD='new-secret' web ./kuraaccount password
```

### Backup

Accounts live in the `kura_account_data` volume (`/data/kuraaccount.sqlite3`). Back that up.

```bash
docker compose exec web tar -C /data -cf - . > kuraaccount-backup.tar
```

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
KURA_CLIENTS_JSON='[{"id":"demo","secret":"0123456789abcdef","name":"Demo","home":"http://127.0.0.1:3006/","icon":"🧪","redirect_uris":["http://127.0.0.1:3006/"]}]' \
  go run ./cmd/kuraaccount serve
```

Open http://127.0.0.1:3006

```bash
go test ./...   # suite: store, hub flows, OAuth2 negatives
go vet ./...
```

---

## Environment

| Variable | What it does |
| --- | --- |
| `SIGNUP_ENABLED` | Public signup form. Turn off after the first account |
| `FORCE_SSL` | `true` when Caddy/nginx terminates HTTPS |
| `KURA_HOST` | Public hostname (comma-separated if several) |
| `BIND` | Default `127.0.0.1:3006` |
| `DATA_DIR` | Where `kuraaccount.sqlite3` lives. Default `storage` (`/data` in Docker) |
| `KURA_CLIENTS_JSON` | Static first-party client registry (see `.env.example`) |
