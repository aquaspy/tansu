# TansuHome (Go)

A quiet homepage for the sites you actually open. Go port of the Rails app (which lives on in the archived KuraHome repo): chi + templ + htmx + SQLite, no build step beyond `templ generate` and the Tailwind CLI.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

## Run

```sh
cp .env.example .env   # then edit KURA_HOST
go run ./cmd/kurahome serve
```

Or with Docker:

```sh
docker compose up -d --build
```

Environment: `BIND` (default `127.0.0.1:3000`), `DATA_DIR` (default `storage`), `KURA_HOST` (comma-separated, empty only while testing), `SIGNUP_ENABLED` (default true), `FORCE_SSL` (default false). SSO: `KURA_ACCOUNT_URL` (empty = standalone), `KURA_CLIENT_ID` / `KURA_CLIENT_SECRET` — see [Deploy the suite](../../README.md#deploy-the-suite).

## Self-host (Docker Compose)

You need Docker on a VPS (or a home box). The app binds to localhost only — port 80/443 stay free for your proxy.

```bash
git clone https://github.com/aquaspy/tansu.git
cd tansu/apps/home
cp .env.example .env
```

Edit `.env`. At minimum:

```bash
KURA_HOST=home.gettansu.com
SIGNUP_ENABLED=true       # first account, then flip to false
FORCE_SSL=false           # true once HTTPS terminates in front
BIND=127.0.0.1:3000       # change the port if another Tansu app already took 3000
```

Then:

```bash
docker compose up -d --build
```

Open the app (e.g. `http://127.0.0.1:3000`), create the first account in the browser — **or** from the shell:

```bash
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kurahome create
```

Lock public signup so the internet cannot mint accounts on your box:

```bash
# in .env
SIGNUP_ENABLED=false
docker compose up -d
```

> **Important:** `docker compose restart` does **not** reload `.env`. Always use `docker compose up -d` after changing environment variables.

### Reverse proxy (Caddy or nginx)

Nothing is bundled. Point your proxy at whatever `BIND` you chose, set `FORCE_SSL=true`, then `docker compose up -d`.

**Caddy:**

```
home.gettansu.com {
  reverse_proxy 127.0.0.1:3000
}
```

**nginx:**

```
location / {
  proxy_pass http://127.0.0.1:3000;
  proxy_http_version 1.1;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
}
```

### Users on the server

There is no email recovery. Reset passwords from the box:

```bash
docker compose exec web ./kurahome users
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kurahome create
docker compose exec -e EMAIL=you@example.com -e PASSWORD='new-secret' web ./kurahome password
```

### Backup

Everything lives in the `kura_home_data` volume (`/data/kurahome.sqlite3`). Back that up:

```bash
docker compose exec web tar -C /data -cf - . > kurahome-backup.tar
```

## Develop

```sh
templ generate
tailwindcss --input web/static/css/input.css --output web/static/css/app.css
go run ./cmd/kurahome serve
```

Subcommands: `serve` (default), `users`, `create` (`EMAIL=`/`PASSWORD=`), `password` (`EMAIL=`/`PASSWORD=`).

## Test

```sh
gofmt -l cmd internal
go vet ./...
go test ./...
```
