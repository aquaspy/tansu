# Tansu Assistant

**A calm place to talk to your model — on a machine you own.**

Tansu Assistant is a self-hosted chat PWA. One SQLite file, no Redis, no third-party chat UI logging your prompts into someone else's product. You bring an [OpenRouter](https://openrouter.ai/settings/keys) API key. Keys stay on the server. Conversations sync across your devices because they live in *your* database.

One static Go binary (~14 MB, ~20 MB RAM) serves the whole app: pages, streaming replies, uploads, and the PWA shell.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

---

## Philosophy

Public chat products are optimized for engagement and billing. Tansu Assistant is optimized for **quality replies** and **obvious cost**.

- **Any model, one env var.** Replies come from OpenRouter. Out of the box you get a short menu of cheap-but-capable models (luna, deepseek flash, opus 5.5, muse glimmer, grok). Hosted installs hide the model and effort pickers (`ASSISTANT_SHOW_MODEL_CONTROLS` defaults to false) and always use `OPENROUTER_MODEL` (or the first `OPENROUTER_MODELS` entry) plus `OPENROUTER_REASONING_EFFORT`. Set the flag true to show the pickers; each chat then remembers its own model and effort. No code changes either way.
- **Explicit web search.** The model answers from its own knowledge unless the globe toggle in the composer is on for that turn. Search runs through OpenRouter's web plugin (pinned to the Exa engine, so results don't change when you switch models) and comes back with a Sources fold under the reply. A second toggle switches that turn to deep search (deeper Exa mode, more results) for research questions. Titles and compaction never search.
- **Voice mode.** One mic button: it transcribes, polishes the grammar, then either sends right away or stages the draft for review (per-chat toggle). A second per-chat toggle reads every reply aloud in the voice matching its language. Any finished reply can also be replayed from its Listen button. Every leg runs through OpenRouter on the same key and meters into the chat cost like any other usage.
- **Honest threat model.** Messages are plaintext SQLite on this server. They are sent to OpenRouter, which routes them to a provider to generate replies. When Apps is linked, a note, person, event, or expense the Assistant reads is sent the same way. The sibling token is encrypted in this database with `KURA_APPS_KEY`; a copy of the file without that key does not reveal it, and a host that has both does. The token keeps working while that app is locked. On search turns the question also reaches the search engine (Exa by default) via OpenRouter. Attached photos and PDFs live on this server's disk and are **re-sent** on later turns while that message is still in the model window. Deleting a chat purges its attachments. Word and PowerPoint uploads are converted to PDF locally first (headless LibreOffice, never leaves this server) and enter the same pipeline. PDFs are parsed by OpenRouter's file-parser plugin (mistral-ocr engine): the file leaves OpenRouter for Mistral's OCR API under OpenRouter's own key — Mistral does not train on it but retains it 30 days — and it is re-parsed on every turn it is attached to, so per-page OCR fees repeat per turn. Every request enforces Zero Data Retention provider routing (`provider.zdr`), on top of whatever ZDR you set on the OpenRouter account — but that covers *model provider* routing only: per OpenRouter's docs ZDR does not extend to plugins, so neither the search queries (Exa offers ZDR on Enterprise plans only) nor the Mistral OCR step are under ZDR. Chat turns send a `session_id` (the conversation id) so OpenRouter keeps one conversation on a warm provider cache — that is billing/latency, not storing the transcript. Share links let anyone with the URL read that chat (including photos). This is **not** end-to-end encryption.
- **Same calm shell as the rest of Tansu.** Cookie auth, idle lock (per device), PWA offline *reads*, Compose bound to localhost, signup you can shut off.

It sits next to [TansuNotes](../notes), [TansuCalendar](../calendar), [TansuSpend](../spend), and [TansuPeople](../people) — same family, **separate** volume and database. Chat is sent to OpenRouter. A note, a person, or a spend the Assistant was asked to read goes out the same way; everything it was not asked to read stays in that app's database.

---

## What you get

- Multi-user instance; each person owns many conversations
- Streaming replies over server-sent events + htmx fragment swaps
- Three chat modes: **Assistente** (tools on, saved), **Conversa** (no tools, saved), and **Anônimo** (no tools, kept in this browser only — the text still goes to the model)
- Model and effort pickers hidden on the hosted default (`ASSISTANT_SHOW_MODEL_CONTROLS=false`); set it true to show the per-chat picker with price-tier dots. Titles and compaction always use `none`
- Explicit per-turn web search with a Sources fold (Exa engine via OpenRouter; off unless toggled), plus a deep-search toggle for research questions
- Attach up to 4 files per message (images JPEG/PNG/WebP/GIF, or PDF, up to 8 MB each; the model sees them; follow-ups keep seeing them while that turn is in context). PDFs go through OpenRouter's file-parser plugin (`OPENROUTER_PDF_ENGINE`, default `mistral-ocr`)
- Optional read-only share links (`/s/...`)
- Automatic context compaction on very long threads (full transcript stays in SQLite)
- Offline: reopen chats you already opened; sending stays disabled until you are back

**What you do not get (on purpose):** generating images, a media library, per-user API keys, RAG over your notes, Redis, or a bundled reverse proxy.

---

## Self-host (Docker Compose)

```bash
git clone https://github.com/aquaspy/tansu.git
cd tansu/apps/assistant
cp .env.example .env
```

Edit `.env`. At minimum:

```bash
KURA_HOST=assistant.gettansu.com
OPENROUTER_API_KEY=sk-or-...  # from https://openrouter.ai/settings/keys
SIGNUP_ENABLED=true       # first account, then false
FORCE_SSL=false           # true once HTTPS terminates in front
BIND=127.0.0.1:3000
```

Then:

```bash
docker compose up -d --build
```

Create the first account in the browser (`http://127.0.0.1:3000`), or:

```bash
docker compose exec web ./kurachat create EMAIL=you@example.com PASSWORD='at-least-8'
```

**Lock signup** so strangers cannot burn your API credits:

```bash
# in .env
SIGNUP_ENABLED=false
docker compose up -d
```

> **Important:** `docker compose restart` does **not** reload `.env`. Use `docker compose up -d`.

There are no cookie-signing secrets to manage: sessions are opaque random ids in SQLite. Losing the database loses everything; losing anything else loses nothing.

### Coming from the Rails version

The Go app reads its own `kurachat.sqlite3`, so the Rails database is imported once:

```bash
# 1. Back up the old volume (SQLite + Active Storage blobs).
docker compose exec web tar -C /rails/storage -cf - . > kurachat-rails-backup.tar

# 2. Deploy the Go image (same kura_chat_data volume, now mounted at /data).
docker compose up -d --build

# 3. Import the old database + uploads into the new layout.
docker compose exec web ./kurachat import /data/production.sqlite3 /data

# 4. Verify in the browser, then delete the legacy files:
#    /data/production.sqlite3* and the two-letter blob directories.
```

Users keep their passwords. Everyone signs in again (sessions are not imported). Uploads that predate the rewrite keep their original bytes; HEIC originals stay downloadable but get no new thumbnails.

### Reverse proxy (Caddy or nginx)

The app listens on `BIND` (default `127.0.0.1:3000`) and does not claim 80/443. Point your proxy there, set `FORCE_SSL=true`, then `docker compose up -d`.

Live replies stream over **plain SSE** (`/conversations/:id/events`) — no WebSocket upgrade needed. If your proxy buffers responses, disable buffering for that route or the UI sticks on “Thinking…”.

**Caddy:**

```
assistant.gettansu.com {
  reverse_proxy 127.0.0.1:3000
}
```

**nginx:**

```
location / {
  proxy_pass http://127.0.0.1:3000;
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
  proxy_read_timeout 3600;
  proxy_buffering off;
}
```

### Users on the server

No email recovery — reset from the box:

```bash
docker compose exec web ./kurachat users
docker compose exec web ./kurachat create EMAIL=you@example.com PASSWORD='at-least-8'
docker compose exec web ./kurachat password EMAIL=you@example.com PASSWORD='new-secret'
```

### Backup

Chats live in the `kura_chat_data` volume: SQLite (`kurachat.sqlite3`) plus uploads under `uploads/`. The tar below copies both.

```bash
docker compose exec web tar -C /data -cf - kurachat.sqlite3 uploads > kurachat-backup.tar
```

### Shared browsers

Sign out **and** wait for the cache wipe. Until then, another person opening the PWA offline can see the previous user’s cached conversation HTML.

---

## Cost

The chat bar shows a running USD total for that conversation. Every turn stores the amount OpenRouter actually billed (`usage.cost`: model, cache, and reasoning tokens), so the total is exact — there are no price tables to go stale. Pre-migration xAI turns keep their billed totals too. Turns with token counts but no billed total contribute nothing and flip the pill to `est.`, with the hint saying the sum is incomplete.

Web search costs one plugin fee per searched turn (Exa `auto`: $0.007 for up to 10 results) plus the input tokens of the injected excerpts. OpenRouter folds the fee into `usage.cost`, so the pill already includes it; each searched turn also itemizes the fee (`search_engine`, `search_cost_usd`) in its stored usage for transparency. If your OpenRouter Activity page ever shows the fee billed separately from the turn, set `SEARCH_FEE_INCLUDED=false` and the itemized fee is added on top instead.

`OPENROUTER_REASONING_EFFORT` (default **`xhigh`**) is how hard the model thinks (`none` / `low` / `medium` / `high` / `xhigh` / `max`, model permitting), overridable per chat in the settings row. Reasoning tokens are billed as output, so `xhigh` trades money and latency for harder thinking on every turn. Titles and compaction summaries always use `none`.

Cached input is cheaper than a full prompt when the conversation prefix is unchanged. gpt-6-luna raises its rates past **272k** prompt tokens; compaction exists to stay under that, not because the model’s window is small (it is ~1M). The model sees the thread until about **150k** estimated tokens. Past that, a short rolling summary plus about **32k** of recent raw messages (`CHAT_KEEP_RECENT_TOKENS`). The full transcript stays in SQLite. Attached images are resized to JPEG before the model sees them; image tokens bill as input (and should cache on follow-ups since the prefix is stable). Tansu Assistant does not generate pictures.

---

## Model cost bench

Historical: measured against xAI models before the OpenRouter migration; kept for reference until `bench/` is re-pointed.

This bench measures **cost only, not quality**: billed USD for the same 16 scenarios (search, reasoning, writing, code, explainers, one image, one cache probe) per model and effort level. Billed cost blends both drivers — price per token *and* verbosity — so the cheapest list price does not always win. Quality is ranked separately on blind sheets in `bench/results/`.

| Model | low | medium | high |
| --- | --- | --- | --- |
| grok-4.3 | $0.10 | $0.10 | $0.14 |
| grok-4.20-0309-non-reasoning \* | $0.11 | $0.09 | $0.13 |
| grok-4.20-0309-reasoning \* | $0.12 | $0.15 | $0.12 |
| grok-4.5 | $0.13 | $0.20 | $0.21 |
| grok-4.7 ‡ | $0.15 | $0.28 | $0.23 |
| grok-4.6 | $0.15 | $0.25 | $0.19 |
| grok-build-0.1 \* | $0.20 | $0.22 | $0.18 |
| grok-4.20-multi-agent-0309 | $0.77 | $0.66 | >$2 † |

Run 2026-09-17, billed `cost_in_usd_ticks`, single run per cell. \* `grok-build-0.1`, `grok-4.20-0309-reasoning`, and `grok-4.20-0309-non-reasoning` reject the effort parameter, so their columns differ only by run variance (Grok decides search counts itself). † Multi-agent at high effort blew the $2/run cap in 4 calls — one search turn billed $1.27 on 1.3M input tokens. Not a chat model. ‡ `grok-4.7` ran separately on 2026-09-22 (launched 2026-09-21), same 16 scenarios. Reproduce with `bench/` — see [bench/README.md](bench/README.md).

Speed (total request time, low effort, same 16 scenarios):

| Model | p50 | pmax |
| --- | --- | --- |
| grok-4.20-0309-non-reasoning | 3s | 10s |
| grok-4.7 | 4s | 12s |
| grok-4.3 | 5s | 14s |
| grok-4.20-0309-reasoning | 7s | 21s |
| grok-4.5 | 7s | 24s |
| grok-4.6 | 7s | 22s |
| grok-4.20-multi-agent-0309 | 11s | 36s |
| grok-build-0.1 | 17s | 62s |

---

## Local development

Needs: Go (1.24+), the `templ` CLI, and the standalone `tailwindcss` v4 binary.

```bash
go install github.com/a-h/templ/cmd/templ@latest
# tailwindcss: https://github.com/tailwindlabs/tailwindcss/releases (v4 standalone)

export OPENROUTER_API_KEY=...   # required to generate replies
bin/dev
```

Open http://127.0.0.1:3000 (`bin/dev` also serves a live-reload proxy on :7331).

Checks:

```bash
templ generate && gofmt -l cmd internal && go vet ./... && go test ./...
```

Layout: `cmd/kurachat` (serve/users/create/password/import), `internal/config`, `internal/store` (SQLite), `internal/i18n`, `internal/handler` (Chi routes), `internal/views` (templ), `internal/chat` (completer, markdown, cost), `internal/openrouter`, `internal/images`, `internal/docs`, `web/static` (CSS source, JS, PWA).

---

## Branches

- **`master`** — development. Land and iterate here first.
- **`stable`** — tested code only. Promote from `master` once a change has been run and verified.
- **`go`** — this rewrite. Merge into `master` after verification, then promote as usual.

They sit on the same commit until the next change is under test.

---

## Environment

| Variable | What it does |
| --- | --- |
| `OPENROUTER_API_KEY` | Required to generate replies |
| `OPENROUTER_MODEL` | Single OpenRouter slug (date-pinned slug also works). Hides the picker; unset = default menu |
| `OPENROUTER_MODELS` | Comma-separated slugs for the model picker. First is the default; unset = `OPENROUTER_MODEL` when set, else the cheap default menu (luna, deepseek-v4.1-flash, claude-opus-5.5, muse-glimmer-30b, grok-4.7) |
| `OPENROUTER_TIER_CHEAP_MAX` | Blended $/1M at/below = green dot. Default `1` |
| `OPENROUTER_TIER_EXPENSIVE_MIN` | Blended $/1M at/above = red dot (between = amber). Default `10` |
| `OPENROUTER_TIER_PINS` | Force tiers: `id:tier,...` (e.g. `x-ai/grok-4.3:cheap`). Unset = all from live pricing |
| `OPENROUTER_REASONING_EFFORT` | Default thinking effort. Default `xhigh` (each chat can override it in the UI) |
| `OPENROUTER_PDF_ENGINE` | PDF parser for attached documents: `mistral-ocr` (default), `cloudflare-ai`, `native` |
| `SEARCH_ENABLED` | Web search toggle. Default `true` (per-turn opt-in; nothing searches unless toggled) |
| `SEARCH_ENGINE` | Plugin engine: `exa` (default), `native`, `parallel`, `perplexity`, `firecrawl` |
| `SEARCH_MODE` | Engine mode. Default `auto` (Exa keyword+neural hybrid); `fast` trades depth for latency at the same price |
| `SEARCH_MAX_RESULTS` | Results per search. Default `5` (1–25; past 10 Exa/Parallel add $0.001/result) |
| `SEARCH_DEEP_MODE` | Deep toggle mode. Default `deep-lite` ($0.012; `deep` and `deep-reasoning` go further and slower) |
| `SEARCH_DEEP_MAX_RESULTS` | Results per deep search. Default `10` |
| `SEARCH_FEE_INCLUDED` | Search fee already inside `usage.cost`. Default `true`; set `false` only if Activity shows separate billing |
| `CHAT_REPLY_MAX_TOKENS` | Optional hard cap on reply length (reasoning shares the budget). Unset = no cap |
| `CHAT_WINDOW_TOKENS` | Max estimated tokens sent as the model’s prompt. Default `150000` (under the 272k price step) |
| `CHAT_KEEP_RECENT_TOKENS` | After compaction, how much recent raw text to keep. Default `32000` |
| `SIGNUP_ENABLED` | Public password signup. Off still lets a completed Account SSO create the local user |
| `FORCE_SSL` | `true` when Caddy/nginx terminates HTTPS |
| `KURA_HOST` | Public hostname allowlist. Share links use the request host |
| `BIND` | Default `127.0.0.1:3000` |
| `DATA_DIR` | SQLite + uploads. Default `storage` (Compose: `/data`) |
| `KURA_ACCOUNT_URL` | Tansu Account base URL. Empty = standalone, no SSO button |
| `KURA_CLIENT_ID` / `KURA_CLIENT_SECRET` | This app's OAuth credentials (must match its registry entry). See [Deploy the suite](../../README.md#deploy-the-suite) |

---

## Sister apps

| App | Role |
| --- | --- |
| [TansuNotes](../notes) | Private notes |
| [TansuCalendar](../calendar) | Personal calendar & birthdays |
| [TansuSpend](../spend) | Subscriptions & daily spend |
| [TansuPeople](../people) | People you care about |
