# Tansu Account protocol (v1)

How suite apps offer "Entrar com Tansu". First-party OAuth2 code flow with
mandatory PKCE. No JWT, no refresh tokens, no consent screen, no dynamic
registration — every client is known at boot from `KURA_CLIENTS_JSON`.

> **Why does the code say `kura`?** The suite started life as Kura; Tansu is
> the new public name. Internal identifiers — Go module paths, `KURA_*` env
> vars, cookie and CSS names, OAuth client IDs, i18n keys — intentionally
> keep the old name: renaming them would force database migrations and break
> existing deploys for zero user benefit. Everything user-visible is Tansu;
> `kura` in the code is history, not a bug.

## Registry

One entry per app:

```json
[
  {
    "id": "kurapeople",
    "secret": "at-least-16-chars",
    "name": "TansuPeople",
    "home": "https://people.gettansu.com/",
    "icon": "🧑",
    "redirect_uris": ["https://people.gettansu.com/login/kura/callback"]
  }
]
```

Secrets are bcrypt-hashed into SQLite at boot and never logged. Removing an
entry deletes its codes, tokens, and links (cascades).

## Flow

```text
App                       Browser                   Account
 |  GET /login/kura          |                         |
 |-------------------------->|                         |
 |                     302 authorize?client_id=...     |
 |                       &redirect_uri=...&state=...   |
 |                       &code_challenge=...&method=S256
 |                           |------------------------>|
 |                           |                   login?|
 |                           |<------------------------| (if anonymous)
 |  302 redirect_uri?code=..&state=..                 |
 |<--------------------------|                         |
 |  POST /token (Basic id:secret, grant_type, code,    |
 |   redirect_uri, code_verifier)                      |
 |---------------------------------------------------->|
 |  {"access_token","token_type":"Bearer",             |
 |<----------------------------------------------------|
 |  GET /userinfo (Bearer)                             |
 |---------------------------------------------------->|
 |  {"sub":"7","email":"ada@example.com"}              |
 |<----------------------------------------------------|
```

Rules the Account enforces (all fail closed):

- `response_type=code` only; `scope` must be a subset of `openid email`.
- `redirect_uri` must **exactly match** one registered URI. Unknown client
  or redirect → plain `400`, never a redirect.
- `code_challenge_method=S256` is mandatory; the token call must present the
  verifier that hashes to the challenge.
- Codes: single use, 60s TTL, bound to (client, redirect). Reuse, mismatch,
  or expiry burns the code.
- `/token` accepts `client_secret_basic` only (no secrets in the body) and
  is rate-limited per client.
- Access tokens: opaque, 5-minute TTL, good for `/userinfo` only. Apps keep
  their own 30-day local sessions afterwards.
- `sub` is the stable Account user id. Apps should bind it explicitly
  (`users.account_sub`) and only fall back to email for the first claim.

Error shape (token/userinfo): `{"error":"invalid_grant", ...}` with OAuth2
codes (`invalid_client`, `invalid_grant`, `invalid_token`, `rate_limited`).

## Client checklist (per app, ~120 lines with x/oauth2)

1. Env: `KURA_ACCOUNT_URL`, `KURA_CLIENT_ID`, `KURA_CLIENT_SECRET`.
   Empty account URL → hide the button, local login only.
2. `GET /login/kura`: build the authorize URL (random `state` + PKCE pair),
   stash both server-side (signed cookie or short session row), redirect.
3. `GET /login/kura/callback`: verify `state` against the `kura_sso_state`
   cookie set at step 2 (HttpOnly, SameSite=Lax), exchange the code, fetch
   userinfo, find-or-create the local user (`account_sub`, else email
   claim only when `account_sub` is still empty), mint the normal local
   session, redirect `/`. A different `account_sub` on that email fails
   closed — recreating the Account user does not take over the app.
4. Keep the local login form untouched below the button.
5. Tests: callback against an `httptest` Account double (happy + bad state),
   plus standalone regression (no env → old behavior).

## Rollout notes (proven across all 6 apps)

Interop details the checklist glosses over, all verified live:

- Endpoint paths are `/authorize`, `/token`, `/userinfo` (no `/oauth` prefix).
- Scopes accepted: `openid` and `email` only. Asking for `profile` fails
  the request — don't.
- `/token` takes `client_secret_basic` only. Pin
  `oauth2.Endpoint.AuthStyle = AuthStyleInHeader`; never send the secret
  in the body.
- `userinfo` returns exactly `{"sub","email"}`. `sub` is the account's
  numeric user id as a string — stable, use it as the join key.
- Apps join on `account_sub` (unique partial index, empty = standalone).
  First SSO with a known email links the existing row only while
  `account_sub` is empty. A row already linked to a different subject
  fails closed, so deleting and recreating the Account user (same email,
  new id) does not take over the app. Unknown email provisions a row
  with an unusable password digest when signups are open.
  Known hardening gap: that first link-by-email is automatic. With
  `RESEND_API_KEY` and `RESEND_FROM` set, Account creates the user only
  after the inbox is confirmed, so a new hosted account's email was
  proven. A self-host Account without the key still creates the user
  immediately, and an app user that already existed with the same email
  is still linked on the first SSO while `account_sub` is empty. The
  callback also requires the `kura_sso_state` cookie from the browser
  that started `/login/kura`.
- Migration ordering matters: create the `users_account_sub` index
  **after** the `ALTER TABLE ... ADD COLUMN`, not in the schema string,
  or old databases fail to open (`no such column`). Every app has a
  `TestMigrateOldUsersSSO` proving an old file opens, stays readable,
  and enforces sub uniqueness.
- Reference client: `KuraPeople/internal/handler/kura.go` (+ `kura_test.go`
  with an `httptest` Account double). The other five apps are the same
  file with the module path swapped.

## Roadmap order (when something earns its place)

1. Refresh tokens (only if apps need long-lived API access to each other).
2. `id_token` + JWKS + discovery (only if a standard OIDC client needs it).
3. Consent screen (only with third-party clients — not planned).
4. Single logout (last: the fiddliest, least-missed piece).
