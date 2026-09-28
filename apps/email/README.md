# Tansu Email

**Your mailboxes. Your VPS. IMAP and SMTP, nothing else in the middle.**

Tansu Email is a private mail client you run yourself. It is a Go PWA. A signed-in user connects personal mailboxes with IMAP and SMTP. Folder lists, search, and message bodies are read from the mail server when you ask for them. SQLite stores the account, the session, and the encrypted mailbox password — not a copy of the mail.

> **Why does the code say `kura`?** This project started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name. Everything user-visible is Tansu.

It is part of the **Tansu** family: the same calm shell as [Tansu Notes](../notes), [Tansu Calendar](../calendar), [Tansu Spend](../spend), [Tansu People](../people), and [Tansu Assistant](../assistant). Each app keeps its own database.

## What you get

- Several mailboxes on one Tansu user (IMAP host, port, TLS, SMTP host, port, TLS, username, password, From address)
- A connection test on save, with `last_ok` / `last_error`
- Inbox, Sent, Trash, and the other folders `LIST` returns
- Paginated message lists and server-side search (`from:`, `subject:`, `since:YYYY-MM-DD`, or text)
- Read, reply, send, move to Trash, delete
- Attachments downloaded when opened
- API tokens for Assistant (see [API.md](API.md))
- Account SSO, idle lock, English and Portuguese

**Not in this version:** Google or Microsoft OAuth, push, a full offline copy of every message, Gmail-style labels.

## Self-host (Docker Compose)

```bash
git clone https://github.com/aquaspy/tansu.git
cd tansu/apps/email
cp .env.example .env
```

```bash
KURA_HOST=mail.gettansu.com
SIGNUP_ENABLED=true
FORCE_SSL=false
BIND=127.0.0.1:3002
KURA_SECRETS_KEY=$(openssl rand -hex 32)
```

`KURA_SECRETS_KEY` is required before a mailbox password can be saved. It is 32 bytes, hex-encoded. Losing it means the saved passwords cannot be opened.

```bash
docker compose up -d --build
```

Open `http://127.0.0.1:3002` and create the first account, or:

```bash
docker compose exec -e EMAIL=you@example.com -e PASSWORD='at-least-8' web ./kuraemail create
```

Point Caddy at the app:

```
mail.gettansu.com {
  reverse_proxy 127.0.0.1:3002
}
```

Account SSO uses the same three variables as the other apps (`KURA_ACCOUNT_URL`, `KURA_CLIENT_ID=kuraemail`, `KURA_CLIENT_SECRET`). Register `https://mail.gettansu.com/login/kura/callback` in the Account's `KURA_CLIENTS_JSON`.

Assistant linking needs `KURA_ASSISTANT_URL` here and `KURA_EMAIL_URL` on Assistant.

## Develop

```bash
cd apps/email
go install github.com/a-h/templ/cmd/templ@v0.3.1020
templ generate
go test ./...
```

CSS is built in CI and in the Docker image (`tailwindcss` v4.3.3, input `web/static/css/input.css`).
