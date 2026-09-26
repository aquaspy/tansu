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
| Tansu Home | `apps/home` | 3000 | `home.gettansu.com` |

The 3000 default is shared by Notes, Chat, and Home — run one at a time, or
override `BIND` (the E2E script below uses scratch ports for the full fleet).

## Run one app

```sh
cd apps/notes
cp .env.example .env   # then edit KURA_HOST
go run ./cmd/kuranotes serve
```

Or with Docker — each app has a `docker-compose.yml`; see its README.

## Run the whole suite with single sign-on

[`scripts/e2e.sh`](scripts/e2e.sh) builds all seven apps, starts them on
scratch ports (3200–3207), and walks one account signup through all six
app logins:

```sh
./scripts/e2e.sh start   # build + boot the fleet
./scripts/e2e.sh test    # SSO into every app, hub tails, standalone check
./scripts/e2e.sh stop
```

The protocol is documented in [`apps/account/ACCOUNT.md`](apps/account/ACCOUNT.md):
first-party OAuth2 code flow with mandatory PKCE, static client registry,
opaque tokens. Each app keeps working standalone — the Account only adds the
"Entrar com Tansu" door next to the local login.

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
