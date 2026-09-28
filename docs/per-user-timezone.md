# Per-user timezone

Account owns the IANA timezone. Each SSO app caches it and formats with that location.

Account and Calendar are implemented: the hub stores `timezone`, `/userinfo` returns `zoneinfo`, and Calendar caches it at SSO (including `/login/kura?sync=1`), projects ICS absolute times into that zone, and treats "today" as the user's civil date. Email, Notes, People, Spend, and Assistant are still planned. The rollout below describes the original sequence; Account and Calendar landed together because the ICS bug is the proof.

## What is broken today

Docker images (`debian:12-slim`) install certificates and curl. They do not install `tzdata` and they do not set `TZ`. Go's `time.Local` is therefore UTC.

`TimeShort` in every app does `t.Local()` before formatting (`apps/*/internal/i18n/i18n.go`). Absolute instants stored as UTC — email `Date`, token `LastUsedAt`, ICS `LastFetchedAt` — render as UTC. A message sent at 09:00 in `America/Sao_Paulo` (12:00Z) shows as 12:00.

Calendar event clocks are a second bug, and `TimeShort` does not cause it. Events are stored as civil strings (`starts_on` `YYYY-MM-DD`, `starts_at` `HH:MM`) and the grid prints those strings. The shift happens when an ICS feed is parsed:

- `ics.Parse` converts `Z` times with the `loc` argument (`t.In(loc)`, then `Format("15:04")`).
- Manual refresh and the 15-minute sync both pass `time.Local` (`apps/calendar/internal/handler/feeds.go`, `apps/calendar/internal/ics/sync.go`).
- A feed that says `DTSTART:20260928T120000Z` for a 09:00 São Paulo meeting is stored as `12:00`. The user sees +3h.

`TZID=America/Sao_Paulo` events already keep that wall clock, because `parseWhen` parses them in the named zone and formats them there. Floating times (no `Z`, no `TZID`) also keep the digits, because `time.Parse` plus `Format` does not convert. Those two forms are not the +3h bug. Treating them as UTC and then converting them would create a new shift.

"Today" on the calendar uses `time.Now()` in the process zone (`handleCalendarShow`). Between 21:00 and midnight in São Paulo the highlighted day is already tomorrow in UTC. Spend payment days are a day-of-month checked against that date.

Assistant does not use `TimeShort`. Its "Today is …" prompt uses a process-wide zone from `KURA_TIMEZONE`, and an empty value silently becomes `America/Sao_Paulo` (`apps/assistant/internal/config/config.go`, `chat/agent.go`).

Setting `TZ` on the container is not the fix. One process serves every user.

## 1. Account data model

Add a column on Account `users`:

```text
timezone TEXT NOT NULL DEFAULT 'UTC'
```

- Name: `timezone`. Value: an IANA name (`America/Sao_Paulo`, `UTC`).
- Validation: `strings.TrimSpace`, then `time.LoadLocation`. Reject empty-after-trim by saving `UTC`. Reject abbreviations (`BRT`, `EST`), numeric offsets (`-03:00`), and `Local`. Cap the raw input at 64 bytes.
- `UTC` and `Etc/UTC` both load; store `UTC` when the loaded name is `Etc/UTC` or `UTC`.
- Account's binary must `import _ "time/tzdata"`. The runtime image has no zoneinfo files. Calendar's ICS parser and Assistant's chat window already do this; Account does not.
- Existing rows: `ALTER TABLE` in `Store.migrate`, same shape as `pending_signups.attempts`. Default `UTC`. Do not backfill `America/Sao_Paulo`. Until a person sets a zone, display stays UTC, which matches the containers today.
- New signups store `UTC` unless the hub form submits a valid name.
- Editing: a form on the Account hub (there is no settings page today). Plain text input, placeholder `America/Sao_Paulo`, server-side error if `LoadLocation` fails. CSRF like the other hub posts.

`users` today is `id, email, password_digest, created_at, updated_at`. `/userinfo` returns `sub` and `email` only. Access tokens last 5 minutes. App sessions last 30 days and do not keep the token (`apps/account/ACCOUNT.md`).

## 2. How apps learn the zone

**Primary: a `zoneinfo` field on the existing `/userinfo` JSON, copied onto the app user at SSO.**

`/userinfo` becomes:

```json
{"sub":"7","email":"ada@example.com","zoneinfo":"America/Sao_Paulo"}
```

`zoneinfo` is the standard OIDC claim name. This is still the opaque userinfo document. There is no JWT, no JWKS, and no new scope. `scope` stays a subset of `openid email`. Old apps decode `sub` and `email` and ignore the extra field.

Each app adds `users.timezone TEXT NOT NULL DEFAULT 'UTC'` (migrate after the schema string, with a `TestMigrateOldUsers*` like `account_sub`). The SSO callback (`kura.go`, copied per app) writes `profile.Zoneinfo` when `LoadLocation` accepts it, and writes `UTC` when the field is missing or invalid.

Why this and not the alternatives:

| Approach | Why it loses |
|---|---|
| `id_token` | Not built. `ACCOUNT.md` defers JWKS and discovery. |
| New Account preferences API | Apps throw away the access token after userinfo. A later pull needs a new credential. Refresh tokens are an explicit non-goal. |
| Shared cookie | Apps are different sites (`calendar.gettansu.com`, `account.gettansu.com`). A parent-domain cookie fails on self-host domains and is invisible to the ICS timer and to Assistant tool calls. `kura_locale` is a cookie because language is a browser preference. Timezone is an account fact. |

**Cache lifetime.** A logged-in app does not call userinfo again. `handleKuraStart` returns home when the local session is usable, so a hub click does not refresh the zone. A change on Account would otherwise sit for up to 30 days.

Close that gap without a new protocol:

- `/login/kura?sync=1` skips the "already logged in" short-circuit and runs the normal PKCE flow.
- The callback, when `account_sub` already matches, updates `timezone` and does not mint a second session.
- Account authorize already skips consent when the Account session is unlocked, so this is redirects only.
- Hub links become `{home}/login/kura?sync=1` once an app understands the query. Until then the app ignores it and short-circuits, which is safe.
- Background work (ICS timer, email display, Assistant "today") reads the cached column. It does not call Account.

Standalone (empty `KURA_ACCOUNT_URL`, or a local user with empty `account_sub`): the app owns the column. Default `UTC`. A small `POST /timezone` form edits it. When `account_sub` is set, that form is read-only and points at the Account hub; SSO overwrites the cache.

Do not guess on the server. The standalone form may offer "use this browser's timezone" (`Intl.DateTimeFormat().resolvedOptions().timeZone`) as a value the person submits. The stored row is still the source of truth.

`KURA_TIMEZONE` stays an Assistant fallback only: user row, then the env var, then `UTC`. Remove the silent `America/Sao_Paulo` default in `loadZone` and `Service.zone`. A deploy that relied on that default sets `KURA_TIMEZONE=America/Sao_Paulo` or sets the Account zone. Do not add `KURA_TIMEZONE` to the other apps.

## 3. Formatting helper

There is no shared i18n module. The same function is copied in Account, Email, Calendar, Notes, People, and Spend. Change each copy the same way. Do not extract a module in this work.

```go
func TimeShort(l Locale, t time.Time, loc *time.Location) string {
    if loc == nil {
        loc = time.UTC
    }
    t = t.In(loc)
    // existing en / pt layouts
}
```

Rules:

- A nil location means UTC, never `time.Local`.
- Do not call `time.Local` and do not set the process zone.
- Request middleware puts the logged-in user's `*time.Location` on the context (`ZoneOf`). Templates and handlers pass it in.
- Call sites today: token last-used in every app, calendar feed `LastFetchedAt`, email `FormatWhen` (list and read). Account defines `TimeShort` and does not call it; update the signature anyway so the copies do not drift.
- JSON APIs stay UTC `RFC3339` (`email` `rfc()`, notes/people `created_at`). Display strings change. Agent payloads do not.
- SQLite timestamps stay UTC. This work does not rewrite stored instants.
- Every binary that calls `LoadLocation` embeds `time/tzdata`.

## 4. Calendar

Two kinds of time, kept distinct.

**Wall clocks the person typed** (`starts_on`, `starts_at`, `ends_on`, `ends_at`, all-day dates, repeat-until). The form is already "clock in my zone". Keep storing the strings. Keep printing `TimeLabel()` as stored. Do not parse them as UTC and run them through `TimeShort`. Changing timezone does not rewrite these rows: a 09:00 dentist stays 09:00. Export JSON stays these strings.

**ICS feeds** are mixed. `Parse` must take the user's location and apply it only to absolute times:

| ICS form | Meaning | What to store |
|---|---|---|
| `DTSTART:…Z` | Absolute UTC | Wall clock in the user zone (`t.In(userLoc)`) |
| `DTSTART;TZID=Zone:…` | Absolute in that zone | Expand in that zone, then store each instance in the user zone |
| No `Z` and no `TZID` | Floating (RFC 5545) | The digits as written. No `In` |
| `VALUE=DATE` | All-day civil date | Dates only. Exclusive `DTEND` stays as it is |

`TZID` is the gap in the current parser. It returns the time in the event zone, so a 09:00 `America/New_York` event is stored as 09:00 for a São Paulo user. After this work it becomes 10:00 on a September date (EDT is UTC−4, São Paulo is UTC−3). A `TZID` that `LoadLocation` cannot resolve stays floating digits, as the fallback branch does now. Do not assume UTC for an unknown `TZID`. The parser uses Go's IANA data, not a `VTIMEZONE` block in the file.

Recurrence expands on the original instant, then each start/end is projected. Projecting before expand would apply the wrong DST.

`Window` and "today" use `time.Now().In(userLoc)` so the civil date is the user's. `BuildGrid` can keep comparing UTC-midnight date keys; the year-month-day passed in must already be the user's date. The calendar API default month (`dateRangeParams`) uses the same date, so Assistant's "this month" matches the grid.

Sync:

- `SyncFeed` callers pass the feed owner's location, never `time.Local`.
- The periodic loop loads that user. A missing or invalid zone is UTC.
- When SSO `sync=1` or a standalone save changes the zone string, re-sync that user's feeds. Absolute events were baked into the old wall clock; the next successful fetch rewrites them. If the fetch fails, the old rows remain until a later success.

Manual create/edit is unchanged aside from "today" when the form's fallback date is computed (`eventFallback`).

### Spend payment days

Spend pushes `due_day` (1–31), title, notes, and `source_key` (`apps/spend/internal/handler/sync.go`). Calendar stores that day and clamps 31 to the last day of the month (`ObservedOn`). There is no timestamp.

Do not add a timezone to the sync body. Do not convert `due_day`. The only change is the calendar's notion of today, so a reminder due on the 28th highlights on the 28th in the user's zone after 21:00 São Paulo. Spend's own `spent_on` dates are values the person typed; leave them.

## 5. Email

`parseRFC822` already stores `mail.ParseDate` as UTC (`apps/email/internal/mail/mime.go`). Keep that.

`FormatWhen` passes the user location into `TimeShort`. List and read views pick it up. A `Date` of 12:00 −0300 displays as 12:00 for `America/Sao_Paulo` and as 15:00 for `UTC`.

The API `date` field stays RFC3339 UTC. Compose still writes `time.Now().UTC()` as `RFC1123Z`. Do not emit floating `Date` headers.

## 6. Rollout

Ship in this order. Later apps keep working while earlier ones are deployed, because extra userinfo fields are ignored and `?sync=1` is ignored until that app's `kura.go` learns it. Account and Calendar shipped in one change. Each remaining step is its own PR.

1. **Account.** Column, migration test, hub form, `zoneinfo` on `/userinfo`, `time/tzdata`, `ACCOUNT.md` note. No app changes. Existing sessions keep showing UTC until step 2 copies the field.
2. **Calendar.** Local column, callback copy, `?sync=1`, `TimeShort` location, ICS rules above, today and API default month, re-sync when the zone string changes. This is the proof. The +3h feed bug is fixed here, not by the Email PR.
3. **Email.** Cache plus `FormatWhen`. Small, and it is the other user-visible clock.
4. **Notes, People, Spend.** Same cache and `TimeShort` pass-through. Token timestamps move. Spend's push payload stays as it is. People birthdays stay month/day.
5. **Assistant.** "Today is" uses the chatting user's zone, then `KURA_TIMEZONE`, then UTC. Drop the implicit São Paulo default in the same PR.
6. **Hub links.** Point every app at `/login/kura?sync=1` after that app's PR is in production. Doing it earlier is harmless (old binaries short-circuit).

Hosted user action after step 1: set `America/Sao_Paulo` once on the hub. Nothing else backfills it.

## 7. Out of scope

- A browser-only timezone that is not stored on Account or on the local user row.
- Mailcow full-text search.
- OAuth Gmail (or any external mailbox) timezone.
- `id_token`, JWKS, discovery, refresh tokens, consent, single logout.
- A shared Go module for the six `i18n` copies.
- Rewriting manual event wall clocks when the zone changes.
- Per-event display zones (a New York meeting shown as New York while the rest of the day is São Paulo). v1 shows one zone, the user's.
- Container `TZ` or `time.Local` as a stand-in.
- Parsing `VTIMEZONE` bodies. Unknown `TZID` stays floating.

## 8. Success criteria and tests

**Account**

- Migration: an old SQLite file opens, existing users read back as `UTC`, new users can be updated.
- `LoadLocation` accepts `America/Sao_Paulo` and `UTC`; rejects `BRT`, `-03:00`, `Local`, and a 65-byte string.
- `/userinfo` includes `zoneinfo`. A client that only decodes `sub` and `email` still works.
- Hub save round-trips the value. Invalid input re-renders the form and does not write.

**Calendar (the bug)**

- With user zone `America/Sao_Paulo`, `DTSTART:20260928T120000Z` stores `09:00`, not `12:00`. The same feed with user zone `UTC` stores `12:00`.
- `DTSTART;TZID=America/New_York:20260928T090000` stores `10:00` for São Paulo.
- `DTSTART;TZID=America/Sao_Paulo:20260928T090000` stores `09:00` for São Paulo (the existing test, still true).
- Floating `DTSTART:20260928T090000` stores `09:00` for both São Paulo and UTC.
- `VALUE=DATE` all-day range is unchanged (28th through the 29th when `DTEND` is the 30th).
- Unknown `TZID` stores the written clock and does not shift it.
- `SyncFeed` / periodic sync never receives `time.Local`. A test with `time.Local` forced to UTC still formats with the user location argument.
- Changing the cached zone from `UTC` to `America/Sao_Paulo` re-syncs and replaces the `Z` wall clock.
- At 2026-09-28 22:00 in São Paulo (2026-09-29 01:00Z), the grid's today cell is the 28th, and a payment day with `due_day` 28 is on that cell.
- A manual event saved as 09:00 is still 09:00 after the zone change.
- `TimeShort` of a UTC `LastFetchedAt` renders in the user zone. A nil location renders UTC even if the test process is not UTC.

**Email**

- A message `Date` of `Mon, 28 Sep 2026 12:00:00 -0300` renders `12:00` for `America/Sao_Paulo` and `15:00` for `UTC`.
- The API `date` value is unchanged (`…Z`).

**Other apps**

- SSO callback persists `zoneinfo`. `?sync=1` updates it without a new session row. A normal `/login/kura` with a live session still returns home.
- Standalone `POST /timezone` updates the row; the same post is ignored when `account_sub` is set.
- Assistant prompt date at 22:00 São Paulo is that civil date, and a user whose zone is `UTC` does not inherit São Paulo when `KURA_TIMEZONE` is unset.

**Not success**

- All users automatically showing São Paulo without setting it.
- Feed times that were floating shifting by three hours.
- Payment-day sync payloads gaining a timezone field.
