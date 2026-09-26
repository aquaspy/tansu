# TansuHome (Go)

A quiet homepage for the sites you actually open. Go port of the Rails app on `master`: chi + templ + htmx + SQLite, no build step beyond `templ generate` and the Tailwind CLI.

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

Environment: `BIND` (default `127.0.0.1:3000`), `DATA_DIR` (default `storage`), `KURA_HOST` (comma-separated, empty only while testing), `SIGNUP_ENABLED` (default true), `FORCE_SSL` (default false).

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
