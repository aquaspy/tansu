# Tansu mobile design audit — 30 Sep 2026

Ground truth: eight production screenshots at roughly 390×844 (phone).

| # | File | Surface |
|---|---|---|
| 1 | `01-www-landing.png` | https://www.gettansu.com |
| 2 | `02-account-home.png` | https://account.gettansu.com |
| 3 | `03-assistant-home.png` | https://assistant.gettansu.com |
| 4 | `04-calendar-home.png` | https://calendar.gettansu.com |
| 5 | `05-email-home.png` | https://email.gettansu.com |
| 6 | `06-spend-home.png` | https://spend.gettansu.com |
| 7 | `07-notes-home.png` | https://notes.gettansu.com |
| 8 | `08-people-home.png` | https://people.gettansu.com |

Code references are this repo (`apps/*/web/static/css/input.css`, `apps/*/internal/views/*.templ`, `apps/*/internal/i18n/i18n.go`). The marketing site is not in this repo. This pass is an audit only: no CSS or template edits.

The signed-in chrome in shots 2–8 matches the **English** catalog (`"The tansu holds the suite"`, `"Left this month"`, `"New chat"`, `"Coming up"`, `"New message"`). Portuguese strings already exist in each `i18n.go` (`hub.title` → "O tansu guarda a suíte", `app.leftover` → "Sobra neste mês", `mail.compose` → "Nova mensagem"). Do not treat this audit as a translation project. A `pt` browser with no `kura_locale` cookie already receives Portuguese (`FromHeader`). These shots are an explicit English session, which is the wrong first impression next to a Portuguese landing page.

---

## Executive summary

- The six apps already look like one family: same cool paper (`--bg0: #f4f6f5`), same lagoon accent (`--accent: #0e7490`), same serif wordmark, same account pill, same kebab menu, same teal FAB. The work is to stop that shared chrome from collapsing on a 390px screen, not to invent a second visual language.
- The landing page is a different product. Warm cream, a forest-green pill, a high-contrast serif headline, and a "T" monogram. The suite is cool gray, cyan buttons, system sans, and a wooden cabinet bitmap. A person who taps "Entrar na lista" and later opens the apps will not feel they stayed in the same house.
- On the phone the current app's name loses. `.account-link` is `flex: none; white-space: nowrap` ("Tansu Account" in full) while `.mobile-top .wordmark span` ellipsizes. Shots show `TansuCale…`, `TansuSpe…`, `Tansu…`, and on People `Tans…`.
- Every home screen has two ways to create something, and they occupy the same corner. Email has a full-width "New message" plus a FAB. Spend's FAB covers "Add subscription". Notes' always-visible trash sits under the FAB. People has a header "+" and a FAB that both open the person composer.
- Spend tells the truth and then shouts a false number. Salary is missing (`SalaryMissing()` renders `app.salary_hint`) but the hero is still a 3rem `R$0.00` plus three more zeros. That reads as a broken ledger, not an empty one.
- Assistant is the product the landing sells, and its home is the flattest list in the suite: one line per chat, no time, no snippet, a permanent "Connect apps" banner even though Account shows 6/6 connected.
- No user-visible "Kura" or "K" wordmark appears in these eight shots. Internal `kura` names are documented in the README and should stay. The brand gap is the mark itself: marketing "T" versus suite cabinet PNG versus Account's flat teal chest versus emoji on the hub cards.
- What is already good should stay: Monday-first month, BRL, one accent inside the suite, People avatars and relationship chips, Notes title-plus-snippet, Calendar's dashed imported events, the labeled Assistant FAB ("+ New chat"), and safe-area math that already exists on the FAB.

---

## How the front-end is actually built

There is no shared design package. Each app copies the same token block into `apps/<app>/web/static/css/input.css` under the comment `Kura<App> "Abyss + Lagoon"`. Docker builds that file alone (`tailwindcss --input web/static/css/input.css`), and app Docker contexts do not see the repo root. A later implementation has to edit all seven `:root` / `[data-theme="dark"]` / `prefers-color-scheme` blocks in lockstep, or add a generate step that copies one `tokens.css` into each app before Tailwind runs. Do not add a cross-app Go import.

Tokens that matter (light, identical in all seven files):

| Token | Value | Role today |
|---|---|---|
| `--bg0` | `#f4f6f5` | page |
| `--sidebar` | `#e9edeb` | mobile header bar |
| `--surface` | `#ffffff` | cards, inputs |
| `--line` / `--line-soft` | `#dde3e0` / `#e8edeb` | borders |
| `--ink` | `#0f1512` | text |
| `--muted` | `#5c6862` | secondary text |
| `--accent` | `#0e7490` | buttons, links, today, unread bar |
| `--accent-2` | `#0b647d` | wordmark, pressed accent |
| `--accent-soft` | `#e2f1f5` | account pill, selected chip, selected day |
| `--accent-ink` | `#ffffff` | label on accent |
| `--danger` | `#d92d20` | destructive |
| `--shadow-sm` | `0 4px 16px rgba(15, 25, 22, 0.08)` | cards |

Dark accent is a different personality: `#2dd4bf` on `#0a0d12`. The theme toggle does not preserve hue, it swaps cyan-for-mint. These shots are all light, so this is a code finding, not a screenshot finding.

Type:

- UI: `-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, "Helvetica Neue", Arial, sans-serif` on `.app-body`.
- Wordmark, Account `h1`, Calendar month and day heading: `ui-serif, "Iowan Old Style", Palatino, Georgia, serif`.
- Spend's month title (`.spend-nav h1`) does **not** set the serif. "September / 2026" in shot 6 is the system sans, wrapped, next to Calendar's serif "September 2026" in shot 4.

Chrome classes to reuse, not reinvent:

- Header: `.mobile-top` (Notes, Email, Assistant), `.people-top`, `.hub-top`, `.cal-top`, `.spend-top`.
- Account jump: `.account-link` in each `shared.templ`.
- Create: `.btn-new` (in-flow, full width) and `.fab` / `.fab-form` (fixed, ≤860px).
- Rows: `.note-item` / `.note-item-link` / `.note-delete`.
- Chips: `.folder-strip` / `.folder-item` (Notes, Email) and `.chips` / `.chip` (People).
- Sheets: `.mobile-menu` already slides up from the bottom.

Wordmarks as rendered (`Wordmark()` in each `shared.templ`):

| App | Literal string | Shot at 390px |
|---|---|---|
| Account | `TansuAccount` | full (no account pill, no lock) |
| Assistant | `Tansu Assistant` | full in the shot; lock + kebab are next in `shell.templ` and are not visible |
| Calendar | `TansuCalendar` | `TansuCale…` |
| Email | `Tansu Email` | `Tansu …` |
| Spend | `TansuSpend` | `TansuSpe…` |
| Notes | `TansuNotes` | `Tansu…` |
| People | `TansuPeople` | `Tans…` |

`letter-spacing: 0.04em` on `.wordmark` makes the concatenated names longer than they need to be.

---

## Cross-app consistency scorecard

Scores are 1 (broken on the phone) to 5 (would sit next to the landing without apology). They describe the eight shots plus the shared CSS, not a hypothetical redesign.

| Area | Score | What the shots and the CSS actually do |
|---|---|---|
| Typography | 3 | One sans and one serif exist, but only some titles use the serif. There is no documented scale. Body, chips, and list titles are all ~0.9–1rem system sans, so hierarchy comes from weight alone. Marketing is a third face. |
| Color | 3 | Light tokens match across all seven apps. Marketing cream + forest green does not. Dark `#2dd4bf` does not match light `#0e7490`. Emoji on hub cards introduce uncontrolled color. |
| Spacing | 3 | Account, Calendar, and Spend cards breathe (16px radius, `--shadow-sm`). Email stacks four bands (header, chips, New message, search) before the first message. Values are ad hoc (`0.2rem`, `0.35rem`, `0.55rem`, `0.7rem`, `1.15rem`), not a scale. |
| Nav / chrome | 2 | Same ingredients, different counts. Account pill never shrinks. Theme, lock, apps grid, and "+" compete with the product name. Only Assistant has a suite grid (`/apps`). Everyone else must leave via the pill. |
| Cards / lists | 3 | Three patterns: flat `.note-item` rows (Notes, Email, Assistant), white cards (Account, Spend, Calendar day), and People cards with avatars. Notes' title + muted snippet is the best row. Assistant is title only. Email is three stacked lines. |
| Empty / zero states | 3 | Spend's hint sentence is the right idea and is then undermined by `R$0.00`. Account's `6/6` has no noun (`hub.drawers` is `"%{count}/%{max}"`). Assistant's empty copy exists (`"No chats yet."`) but this shot is a populated list with no recency. |
| Icons | 2 | Header: `/icon.png` cabinet. Hub: inline SVG chest plus emoji from `KURA_CLIENTS_JSON` (sparkles, calendar). Actions: one-off 16×16 strokes. People: hashed initials. No legend for Calendar dots. No wink / "T" mark anywhere in the suite. |
| Hierarchy | 2 | Email unread is a teal inset bar **and** an "Unread" pill. Spend's hero number, the three sub-lines, and the section titles are all "zero". Assistant rows are the same weight top to bottom, including "Untitled". Hub leads with timezone, not the six apps. |

---

## Per-app findings

### 1. Marketing — `www.gettansu.com`

Separate codebase. Comment only, so the suite can move toward it on purpose.

**Works.** The page is already the "personal OS" pitch: eyebrow `LIFE OS · EARLY ACCESS · BRASIL`, a serif headline with one italic green word ("conhece"), a full-width forest pill, a quiet secondary pill ("Ver como funciona"), and a single product mock (a green chat bubble: "Quanto gastei com jantares com a Ana este mês?"). Restraint is the brand.

**Hurts.** Nothing on this screen is reused as a token. The suite's user bubble is `--user-bubble: #dceef3` (pale cyan), not the forest bubble in the mock. The mark is a "T" in a rounded square; the apps use a photo of a cabinet. Copy is Portuguese; the apps in the other shots are English.

**Do later, not in the suite PR that fixes chrome.** Sample the landing's paper, forest, and serif pairing (values below are eyed from the screenshot, not from the marketing repo — measure before coding). Do not paste the landing layout into the apps.

### 2. Account — hub

**Works.** The chest illustration is the one moment that explains the name. Cards are calm: white, 16px radius, hairline border, one "Open" affordance in accent. "Connected" in `--accent-2` is a clear status. Header fits: wordmark, theme, kebab. Lock is correctly `desktop-only` here.

**Hurts.**

- Headline "The tansu holds the suite" is poetry in the wrong language for the landing's audience, and it does not say what to do. The email under it is useful. `6/6` under that is not: `hub.drawers` renders only the fraction.
- The timezone card is the first task, above the apps, even though `America/Sao_Paulo` is already saved. Returning people came for the suite.
- App icons are emoji (`ClientIcon`). Shot shows ✨ and a calendar glyph. They will not match across iOS and Android, and they do not match the cabinet.
- "Save timezone" is `.btn-primary` at `border-radius: 0.5rem`. The landing CTA and the FABs are pills. Primary actions do not share a shape.
- Wordmark is the concatenated `TansuAccount`, colored `--accent-2`, tracked out. Fine at this width only because the pill is absent.

### 3. Assistant — chat list

**Works.** This is the only app whose name fits and whose create button says what it does: the FAB is a pill, `+` plus `chat.new` ("New chat"), with `padding: 0.85rem 1.2rem` and `border-radius: 999px`. Search is present. "Connect apps" (`.apps-entry`) is visually a soft accent banner, which is the right weight for a setup task. List bottom padding is `calc(6rem + env(safe-area-inset-bottom))`, so the end of the list can clear the FAB.

**Hurts.**

- `.apps-entry` is unconditional in `shell.templ`. Account on the previous shot says 6/6 connected. The banner still says "Connect apps". That is a false chore on every visit.
- `ConversationItem` is `{ID, Title, Active}` only. `ConversationList` renders `ConvTitle` and a delete button. No snippet, no time. The shot is a stack of first-line titles ("Altura da Triz: informação indisponível", "Untitled") with equal weight. The landing promised a conversation; the home is a log file.
- The shot does **not** show trash icons. The template still mounts `.note-delete`, and the ≤860px rule sets `.note-delete { opacity: 0.85 }`. Reconcile toward the shot (keep delete hidden until confirm-from-menu), not toward Notes.
- Header in `shell.templ` order: wordmark, account pill, `/apps` grid, theme, lock, kebab. The shot stops at theme. `.app-shell { overflow: hidden }` and a nowrap header will clip the last 2.6rem buttons when the wordmark's min-content plus the pill exceed 390px. Those controls are one thumb slip away from being unreachable.
- Rows have no timestamp, so "Untitled" at the bottom may be newest or oldest. The list cannot answer "where was I?".

### 4. Calendar — month

**Works.** Monday-first grid matches Brasil. Month title is the serif, centered, with real prev/next targets. Selected day and the day card agree ("Wednesday, September 30"). Imported feed events are visually distinct: dashed `.day-item.is-ics` and a `feeds.badge` ("SHIFT") on "12:00–20:00 Support". Hollow dots (`.dot.ics`, transparent with an inset ring) versus a filled dot are a real encoding. "Add" in the day header is contextual. Lock is `desktop-only`. The FAB uses safe-area insets.

**Hurts.**

- Wordmark `TansuCalendar` ellipsizes to `TansuCale…` beside a full "Tansu Account" pill.
- Mobile cells are `min-height: 3.4rem` and `.cal-pills { display: none }` so only dots remain. There is no legend. `--holiday: #5d7a4f`, `--birthday: #b55220`, `--payment: #1d4f91`, `--accent` for events, hollow for ICS. In the shot, the 7th is a filled dot and almost every other day is a hollow ring. A person cannot tell those apart without opening the day.
- Today versus selected is muddy at this size. `.cal-cell.is-today .cal-num` paints a filled accent disc; `.is-selected` paints an accent border and `--accent-soft` fill. On 30 Sep 2026 those are the same cell. The shot reads as a teal outline, not as "today". Two treatments in a 3.4rem cell cancel each other.
- "Add" in the day header and the anonymous FAB both create an event (`composer#newEvent`). Two primaries.
- Weekday labels (`MON`…) are 0.72rem uppercase muted. Fine. The month nav and the day card are good; do not restyle the grid geometry.

### 5. Email — inbox

**Works.** Unread is at least detectable (weight 700 plus `box-shadow: inset 3px 0 0 var(--accent)`). Subject is the strongest line. Folder chips are real navigation (Inbox, Sent, Drafts, Archive, Junk) instead of a hidden menu. Search can clear (`search#clear`). The bottom FAB rule exists and list padding reserves `calc(5.5rem + env(safe-area-inset-bottom))`.

**Hurts.**

- Densest screen in the set. Before the first message: header, a horizontally scrolling chip row, a full-width `.btn-new` ("New message"), and search. The chip row hides its scrollbar (`scrollbar-width: none`) and the shot clips the next chip after Junk. Nothing says the row scrolls.
- Unread is signaled twice. `.unread-badge` ("Unread") sits on the sender line and eats the width the sender needs, and the inset bar already marks the row. Shot: badge, then "Loja Electrolux <noreply@…>", then the date on its own third line.
- Header is the most crowded: wordmark ellipsized to `Tansu …`, full account pill, theme, **lock** (not `desktop-only` in `shell.templ`, unlike Calendar/Spend/People), kebab.
- Compose is duplicated: in-flow `.btn-new` linking to `d.ComposeHref`, plus `<a class="fab">` with the same href.
- The shot also shows a teal "+" disc in the **top-left**, above the header. Current `.fab` is `position: fixed; right: …; bottom: …` inside the 860px query. That disc is not explained by this tree. Re-capture before moving the FAB. The duplicate that *is* in the tree is `.btn-new` + `.fab`.
- Dates sit on a third line (`<span>{ m.When }</span>` under subject and from). A 390px inbox should be two lines: subject + time, sender underneath.

### 6. Spend — month overview

**Works.** The hero card is the right object: kicker, one big figure, a hint, a three-line breakdown, `font-variant-numeric: tabular-nums`. "Cruzeiro do Sul", day `10`, "Already passed this month", and a remove `×` is a clear row. Section headers with a text action ("Add expense", "Add day", "Add subscription") name the verb. BRL formatting is correct for the audience. Empty copy exists and is specific (`app.empty_expenses`, `app.salary_hint`).

**Hurts.**

- `SpendHero` always prints `.hero-figure` at `clamp(2.1rem, 5vw, 3.1rem)`. When `d.Summary.SalaryMissing()` it also prints the hint, then still prints Salary `R$0.00`, Subscriptions `R$0.00`, Spent `R$0.00`. The shot is that state. The largest text on the screen is a number that means "not set up".
- Month title wraps to two lines ("September" / "2026") because `.spend-nav h1` is `flex: 1` beside chevrons and `.today-btn` ("This month", longer than Calendar's "Today") and the h1 is **not** serif. Calendar's identical string stays one line.
- Wordmark `TansuSpend` → `TansuSpe…`.
- The FAB (`composer#newExpense`, aria-label "Add expense") covers the "Add subscription" text button in the shot. The same action already exists as "Add expense" on the Spent card. The FAB is the duplicate, and it hides a different action.
- "LEFT THIS MONTH" is `text-transform: uppercase` with `letter-spacing: 0.08em`. At this size it is louder than the hint that explains the zero.

### 7. Notes — list

**Works.** Best list in the suite. Title in `--ink` at 1rem / 600-ish, snippet in muted at 0.82rem, one row. Folder chips carry counts (All 71, Inbox 61, Studies 1), which makes the information architecture visible. Search is in the right place. FAB safe-area and list bottom padding match Email.

**Hurts.**

- `.note-delete` is `opacity: 0` until hover, then the 860px query sets `.note-delete { opacity: 1; width: 2.4rem; height: 2.4rem }`. The shot shows a trash can on every row. Delete is a rare, destructive action sitting in the same corner as the FAB, which covers the trash on "July 27th 2025".
- Chip row clips `rando…` with no scroll affordance (same `.folder-strip` as Email).
- Wordmark `TansuNotes` → `Tansu…`. `shell.templ` also puts lock in the mobile header (not `desktop-only`); the shot shows theme + kebab and no lock, so either the lock is clipped or production differs. Specify a max of two trailing icon buttons so this cannot depend on clip.
- Titles that are stored in all caps ("GUIA PRÁTICO DE GUERRA CONTRA EMF…", "PÃO DE QUEIJO 1") dominate the muted snippets. That is content, not CSS. Do not auto-rewrite user titles. Do keep the snippet, which is what makes the row scannable.

### 8. People — overview

**Works.** Strongest information design after Notes. "COMING UP" kicker, avatar initials colored by name hash (`--hue`), countdown in accent ("in 23 days · turns 66"), search **with** a magnifying-glass icon (the only one), relationship chips that are real data (Everyone / Amiga / Pai) with `.chip.is-on` on Everyone. Directory rows repeat avatar, relationship, birthday, and a countdown pill. The FAB sits in empty canvas here, so it does not cover a row.

**Hurts.**

- Worst truncation in the set: `Tans…`. Cause is the extra header button. `PeopleHeader` mounts wordmark, account pill, theme, lock (`desktop-only`), a "+" (`composer#openPerson`), and kebab. The FAB at the bottom calls the same `composer#openPerson`.
- Upcoming name is `white-space: nowrap` inside `.upcoming-item { max-width: 16rem }`. "Marco Aurélio Pereira do N…" ellipsizes even though the card has a second line free. The same person then appears again in the directory (correct) with the same ellipsis, because the countdown pill (`in 23 days`) shares the row.
- Search placeholder is English ("Search people, notes, relationships...") while the chips are Portuguese relationship words the user typed. Mixed language inside one control cluster.
- Header "+" is 2.6rem and sits where the product name should be.

---

## Prioritized recommendations

Each item is something a later agent can implement without re-deciding the design. File paths are the ones that exist today.

### P0 — trust and clarity on the phone

Do these before any palette change. Stay on the current `#0e7490` tokens.

**P0.1 Header budget (all apps except the marketing site).**

Problem. At 390px the account pill (~148px, `flex: none`, `white-space: nowrap`, label `app.account` = "Tansu Account") plus two to five `2.6rem` icon buttons leave the product name to ellipsize.

Why it matters. The person cannot tell which app they are in. "Tans…" does not build trust.

Proposal.

1. Add `app.short_name` in every `i18n.go` (en / pt): Notes `Notes`/`Notas`, Assistant `Assistant`/`Assistente`, Calendar `Calendar`/`Agenda`, Email `Email`/`Email`, Spend `Spend`/`Gastos`, People `People`/`Pessoas`, Account `Account`/`Conta`. Point `Wordmark()`'s `<span>` at `p.T("app.short_name")`. Keep the `<img src="/icon.png">` until P2 replaces the mark. Drop `letter-spacing` on the mobile wordmark (override inside the 860px query).
2. In the same 860px query, hide `.account-link span` and keep the SVG. The control already has `aria-label={ p.T("app.account_label") }` ("Open Tansu Account"). Size it `2.75rem` square, radius `999px`, so it matches `.icon-btn`.
3. Mobile header trailing controls, in order, and only these: the account icon, then the existing kebab. Move theme and lock out of the header. Both already exist inside `.mobile-menu` (lock as a form, theme can move next to `LanguageSwitch`). People: remove the header "+" (`composer#openPerson`); the FAB already does it. Assistant: move the `/apps` grid into the menu as well, unless P1 ships a real switcher.
4. CSS, in each mobile header rule:

```css
.mobile-top, .people-top, .hub-top, .cal-top, .spend-top {
  display: flex;
  align-items: center;
  gap: 0.25rem;
  min-width: 0;
}
.wordmark { flex: 1; min-width: 0; }
.wordmark span { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.account-link { flex: none; }
```

The span needs its own `min-width: 0`. Setting it only on `.wordmark` does not shrink a nowrap child whose min-content is "TansuCalendar".

Acceptance. At 390px every short name is fully visible, including People. No header control is clipped. Account still opens from the icon.

**P0.2 One create control per screen.**

Problem. Two accent creates, and the fixed one covers the other.

| Screen | Keep | Remove on ≤860px |
|---|---|---|
| Assistant | Pill FAB, verb visible (`chat.new`) | none (desktop `.btn-new` is already `desktop-only`) |
| Email | Bottom `.fab` → `d.ComposeHref` | `.list-head .btn-new` (`display: none` in the 860px query) |
| Calendar | Day-header "Add" (`day-add`, already next to the date) | `.fab` (`display: none` even in the 860px query) |
| Spend | Section text buttons (`composer#newExpense`, `#newPaymentDay`, `#newSubscription`) | `.fab` (it only creates an expense, which the Spent header already does, and it covers "Add subscription") |
| Notes | Bottom `.fab` | nothing else creates; see P0.3 for the trash |
| People | Bottom `.fab` `composer#openPerson` | header "+" button |

Why it matters. Two primaries read as an unfinished screen, and the covered "Add subscription" is a control the person can see but cannot hit.

Acceptance. Spend's "Add subscription" is fully visible with the thumb at rest. Email's first message starts higher by roughly one button (the full-width `.btn-new` is gone). Calendar still creates from the day header.

**P0.3 Stop showing delete on every row.**

Problem. Notes' 860px rule forces `.note-delete { opacity: 1 }`. The trash is under the FAB in the shot. Assistant's matching rule sets opacity `0.85` even though the shot looks title-only.

Why it matters. A destructive icon on every row is noise, and it shares the FAB's hit area. Mis-taps delete a note or a chat.

Proposal. In both `apps/notes/web/static/css/input.css` and `apps/assistant/web/static/css/input.css`, delete the mobile `.note-delete { opacity: 0.85 or 1 }` override so the base rule stands: `opacity: 0`, visible on `:focus-visible` only. Do not add swipe in this step. Deleting from inside the open note / open chat can stay as it is.

Acceptance. Notes rows match the current Assistant *screenshot*: title, snippet, no trash. The last row is not pinned under a trash+FAB stack.

**P0.4 Spend: a missing salary is not R$0.00.**

Problem. `SpendHero` in `apps/spend/internal/views/spend.templ` renders the figure and `.hero-lines` even when `SalaryMissing()` is true. The hint is already there (`app.salary_hint`).

Why it matters. The biggest number on a money app is a lie of precision. People will not trust later months.

Proposal. When `SalaryMissing()`:

- Do not render `.hero-figure` or `.hero-lines`.
- Render the hint as the card body (16px / 1.45, `--ink`, not muted uppercase).
- Render one `.btn-primary` labeled with a new key `app.set_salary` ("Set salary" / "Definir salário") whose `data-action` is the existing `settings#open` used by the menu's settings button.
- When salary is set, keep today's hero, including the negative state (`app.leftover_negative`).

Acceptance. The shot's screen shows the sentence and one button, not four zeros.

**P0.5 Email unread: one signal.**

Problem. `messageClass` adds `is-unread`, which draws the inset bar, and the template also prints `<em class="unread-badge">`.

Why it matters. The badge pushes the sender off the row and makes "Unread" look like part of the address.

Proposal. Remove the `<em class="unread-badge">` node from `MailList` in `apps/email/internal/views/shell.templ`. Keep `.note-item.is-unread strong { font-weight: 700 }` and the inset bar. Leave the CSS class in place so it does not become a dangling rule people are afraid to touch; just stop emitting the element.

Acceptance. An unread row is subject (semibold) + sender, with a 3px accent bar. No "Unread" chip.

**P0.6 Account fraction needs a noun.**

Problem. `hub.drawers` is `"%{count}/%{max}"` in both locales. The shot shows a teal `6/6` under the email with no label.

Why it matters. It looks like a battery, a score, or an error.

Proposal. Change the strings only:

- en: `"%{count} of %{max} apps connected"`
- pt: `"%{count} de %{max} apps conectados"`

Style `.chest-count` as the meta line (0.85rem, `--muted`), not as a second headline. When `LinkedCount == AppCount`, that sentence is enough; do not add a celebration.

**P0.7 Assistant "Connect apps" only when something is disconnected.**

Problem. `apps-entry` always renders. The hub shot is 6/6.

Why it matters. A setup banner on a finished account trains people to ignore banners.

Proposal. Pass a bool (true when any registered app is unlinked — the same notion as Account's `IsLinked`). Render `.apps-entry` only then. When everything is linked, do not replace it with a different banner.

**P0.8 Chip rows must look scrollable.**

Problem. `.folder-strip` and People `.chips` set `scrollbar-width: none`. Email clips the folder after Junk. Notes clips `rando…`.

Why it matters. Hidden folders are hidden mail and hidden notes.

Proposal. On the scroller:

```css
.folder-strip, .chips {
  mask-image: linear-gradient(to right, #000 calc(100% - 1.25rem), transparent);
}
```

And add `scroll-padding-inline: 0.75rem`. Do not turn the scrollbar back on. Do not wrap chips to a second line (that brings back Email's wall of chrome).

**P0.9 PWA chrome matches the light header.**

Problem. Every `layout.templ` sets `<meta name="theme-color" content="#0a0d12"/>` and `apple-mobile-web-app-status-bar-style` = `black-translucent`. `#0a0d12` is the dark `--bg0`. These shots are light. `viewport-fit=cover` is already correct.

Why it matters. Installed to the home screen, the status bar and browser chrome read as a dark app wrapped around a light page. Translucent black over `#e9edeb` also risks low-contrast status glyphs.

Proposal. One `theme-color` meta, default `#e9edeb` (light `--sidebar`). The existing theme controller (`theme#cycle` / the script that sets `data-theme`) also sets `meta[name=theme-color].content` to `#e9edeb` for light and `#0a0d12` for dark, including the system setting. Change `apple-mobile-web-app-status-bar-style` to `default` unless a later pass deliberately draws the header under the status bar. Keep the shell's `padding-top: env(safe-area-inset-top)`.

### P1 — one mobile system, still the current palette

**P1.1 Token ownership.**

Problem. Seven hand-copied blocks. They match today and will drift the first time someone "fixes" one app.

Proposal. Add `design/tokens.css` at the repo root containing only the custom properties (light, dark, and both `prefers-color-scheme` blocks) plus a one-line comment that it is the source. A small script (or the existing per-app Docker `RUN`) copies it to `apps/<app>/web/static/css/tokens.css`, and each `input.css` replaces its inlined blocks with `@import "./tokens.css";`. Tailwind v4 already uses `@import`. Do this as its own PR, snapshot-tested by diffing the seven files so they stay identical. This audit's "Proposed tokens" section is the content of that file; do not invent extra tokens during the move.

**P1.2 Type scale, expressed as custom properties, not new fonts.**

No remote webfonts. The PWA stays offline. Use the system sans and `ui-serif` already in the CSS.

| Token | Size / line | Face | Use |
|---|---|---|---|
| `--text-display` | `1.75rem / 2.1rem`, weight 500, tracking `-0.03em` | serif | month title, hub `h1`, day `h2` |
| `--text-title` | `1.25rem / 1.6rem`, weight 600, tracking `-0.02em` | sans | hero figure when it is a real amount; dialog titles |
| `--text-body` | `1rem / 1.45rem`, weight 400 | sans | list title, buttons, inputs |
| `--text-secondary` | `0.875rem / 1.25rem`, weight 400, color `--muted` | sans | snippets, hints, sender |
| `--text-meta` | `0.75rem / 1rem`, weight 600, tracking `0.06em`, uppercase, color `--muted` | sans | kickers: "LEFT THIS MONTH", "COMING UP", weekday labels |

Apply `--text-display` to `.spend-nav h1` so it matches `.cal-nav h1`. Put `.today-btn` on the next row when the title would wrap: `flex-wrap: wrap` is the wrong fix (it wraps the chevrons). Instead give the h1 `white-space: nowrap` and move `.today-btn` to `position` on its own, or shorten Spend's key the way Calendar did (`app.today` is "This month" vs Calendar's "Today"). Concrete choice: change Spend `app.today` to "Today" / "Hoje" **or** place the link under the title, full width, right-aligned, 13px. Prefer the second so "this month" stays accurate for a money view. Calendar can keep "Today" in the same nav row because that string fits.

**P1.3 Space scale.**

Use only these paddings and gaps in mobile chrome and cards: 4, 8, 12, 16, 24, 32 px (`0.25rem` … `2rem` at the default root).

| Surface | Value |
|---|---|
| Header padding | `8px 12px 8px 16px` |
| Gap between stacked cards | `12px` |
| Card padding | `16px` |
| Card radius | `16px` (already `.hero` and `.cal-day`) |
| List row padding | `12px 16px` |
| Row gap (title to snippet) | `2px` |
| Section gap | `24px` |
| Screen inline inset | `16px` |
| FAB inset from the safe area | `16px` (already `max(1rem, env(safe-area-inset-*))`) |

**P1.4 List row.**

One structure for Notes, Email, Assistant, and People directory rows. Keep People avatars and Email's unread bar as optional slots.

```html
<a class="row">
  <!-- optional: .avatar or nothing -->
  <span class="row-main">
    <span class="row-title">…</span>
    <span class="row-meta">…</span>
  </span>
  <span class="row-trailing">…</span> <!-- time, countdown pill, or unread dot. Not a trash icon. -->
</a>
```

```css
.row { display: flex; align-items: center; gap: 12px; min-height: 4.25rem; padding: 12px 16px; }
.row-main { flex: 1; min-width: 0; display: grid; gap: 2px; }
.row-title { font-size: 1rem; font-weight: 600; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row-meta { font-size: 0.875rem; color: var(--muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.row-trailing { flex: none; font-size: 0.75rem; color: var(--muted); font-variant-numeric: tabular-nums; }
.row.is-unread { box-shadow: inset 3px 0 0 var(--accent); }
.row.is-unread .row-title { font-weight: 700; }
```

Email mapping: `row-title` = subject, `row-trailing` = `m.When` (same line as the title), `row-meta` = from. That deletes the third line visible in the shot.

Assistant mapping: extend `ConversationItem` with `UpdatedAt time.Time` and a one-line preview (first user message, trimmed, already what several titles duplicate). `row-trailing` = locale-aware relative time (`i18n` already formats clock times; add a `RelTime` helper, "3d" / "3 d", not a sentence). `row-meta` = preview, hidden if it equals the title.

People directory: `row-trailing` is the existing countdown pill; `row-title` gets `min-width: 0` so the pill cannot force the name to a single clipped glyph. Allow the title 2 lines only in the upcoming card (`-webkit-line-clamp: 2; white-space: normal`) and raise `.upcoming-item` `max-width` from `16rem` to `min(22rem, 86vw)`.

**P1.5 Search.**

People's search is the pattern: 16px magnifying-glass SVG, placeholder, `type="search"`. Copy that markup into Notes, Email, Assistant, and Spend. Shared rules:

```css
.search-form, form.search { position: relative; }
.search-form svg, form.search svg { position: absolute; left: 12px; top: 50%; transform: translateY(-50%); width: 16px; height: 16px; color: var(--muted); }
input.search, form.search input {
  width: 100%;
  height: 2.75rem;
  border-radius: 12px;
  border: 1px solid var(--line);
  background: var(--surface);
  padding: 0 12px 0 2.4rem;
  font-size: 1rem;
}
```

Placeholder stays the existing i18n key (`app.search` / `mail.search`). Do not use placeholder as the only accessible name; People already sets `aria-label`. Do the same on the others.

**P1.6 Touch size.**

`.icon-btn` at ≤860px is `2.6rem` (41.6px) in Notes, Email, and Assistant, and `2.75rem` in Calendar. Chips are `min-height: 2.15rem` (~34px). Set all of these to `2.75rem` minimum on the short side. FAB stays `3.5rem` for the icon-only variant and `min-height: 3rem` for Assistant's pill.

**P1.7 Calendar dots need a one-line legend.**

Under `.cal-weekdays`, a single horizontally scrolling row of the five kinds already in CSS, only the kinds that appear in the current month: filled accent "Event", olive "Holiday", rust "Birthday", blue "Payment", hollow "Imported". Use the existing i18n keys where they exist (`feeds.badge`, holiday/birthday/payment labels) rather than new marketing copy. On mobile, today = filled `.cal-num` only; selected = `box-shadow: inset 0 0 0 2px var(--accent)` and no extra fill. Do not stack `background: var(--accent-soft)` on top of the today disc.

**P1.8 Suite switcher, after the header is calm.**

Assistant's 4-square icon (`/apps`) is the right idea and the wrong singleton. P0 hides it in the menu. P1 replaces the account pill's job of "leave this app" with a sheet:

- Trigger: the account icon in the header (P0) opens the sheet on a long-press, **or** a dedicated item at the top of `.mobile-menu` titled with `app.account` .
- Sheet contents: the six apps plus Account, each row = mark, short name, "Connected" / "Open". Data already exists on Account (`d.Clients`). Other apps do not have that list in-process. Pragmatic v1: the menu item is just the current `.account-link` (go to the hub). v2 (only if you are willing to duplicate the client list into each app's env, which `KURA_CLIENTS_JSON` already is on Account): a local sheet. Do not block P0 on v2.
- Do not add a bottom tab bar. These are separate origins. A tab bar that full-reloads six hosts will feel slower than the sheet, and it will fight the FAB.

**P1.9 Hosted default locale.**

Problem. Landing is Portuguese. These shots are English because the cookie or `Accept-Language` said en, and the language control is inside the kebab (`LanguageSwitch`).

Proposal. Add `DEFAULT_LOCALE` (default `en`, so self-hosters do not change). On gettansu.com set `DEFAULT_LOCALE=pt`. `FromCookie` uses it only when the cookie is empty **and** `Accept-Language` has neither `pt` nor `en`. Do not override an explicit cookie. Do not bury a new language banner in the header; P0 already needs that slot for the name. People who picked English on purpose stay in English.

**P1.10 Primary button shape.**

`.btn-primary` is `border-radius: 0.5rem`. `.btn-new`, the landing CTA, and the FAB are pills. Set `.btn-primary` and `.btn-new` to `border-radius: 999px; min-height: 2.75rem; font-weight: 650`. Cards stay 16px. Inputs stay 12px. Icon buttons stay circles.

### Landed differently (P0 + P1)

- **P1.2.** A real Spend balance uses `--text-display` (1.75rem), so the leftover stays the primary number. `--text-title` stays available for dialog titles.
- **P1.3.** The space scale is on chrome, cards, rows, and search. Auth screens keep their existing padding.
- **P1.5.** Notes, Email, and Assistant gained the magnifier. People already had it. Spend has no search backend, so there is no Spend field.
- **P1.8.** Pragmatic v1 only: the first item in each app's overflow menu is the existing account link (hub). No sheet, and no duplicated client list.
- **P0.9.** The theme script already painted `theme-color`. Light is now `#e9edeb` (the header), dark stays `#0a0d12`, and the status-bar style is `default`.
- **P0.1 / P0.3.** Theme and lock left the mobile header. The Notes editor bar still has both, because that header is hidden while a note is open. Row delete stays in the DOM for keyboard focus and is clipped on small screens so it is not a visible hit target.
- **P1.1.** Color, type, and space tokens share `design/tokens.css`. `scripts/sync-design-css.sh` copies that file into each app (Docker only sees the app directory) and refreshes the mobile rules.
- **P1.9.** `DEFAULT_LOCALE` defaults to English. Hosted gettansu.com should set `DEFAULT_LOCALE=pt`. This change does not edit deploy scripts.

### P2 — the landing and the suite in one family

Do P2 only after P0 and P1 are in production. A recolor on top of `Tans…` and two FABs will still look unfinished.

**P2.1 Light tokens closer to the landing.**

Eyed from `01-www-landing.png`, to be measured with a picker before implementation. Direction, not a hex contract:

| Token | Current | Target direction |
|---|---|---|
| `--bg0` | `#f4f6f5` cool gray | warm paper, near `#f6f4ef` |
| `--sidebar` | `#e9edeb` | slightly warmer, near `#efeae2` |
| `--surface` | `#ffffff` | warm white, near `#fffcf7` |
| `--ink` | `#0f1512` | warm near-black, near `#1c1915` |
| `--muted` | `#5c6862` | warm gray, near `#5e584f` |
| `--accent` | `#0e7490` cyan | forest, the landing pill, near `#1f6b4a` |
| `--accent-2` | `#0b647d` | deeper forest, near `#164e36` |
| `--accent-soft` | `#e2f1f5` | pale green, near `#e5f0ea` |
| `--accent-ink` | `#ffffff` | warm paper if contrast holds, else `#f7f4ee` |
| `--user-bubble` | `#dceef3` | the landing's green bubble, a step lighter than `--accent` so user text in `--accent-ink` still clears 4.5:1 |

Check contrast after the picker: body `--ink` on `--bg0` ≥ 4.5:1, `--muted` on `--surface` ≥ 4.5:1, `--accent-ink` on `--accent` ≥ 4.5:1. If forest-on-paper fails muted text, darken `--muted` before lightening the paper.

**P2.2 Dark accent, same hue.**

Replace dark `--accent: #2dd4bf` with a forest tint that is lighter than the light-theme accent but not neon, around `#8fcead` on `#0a0d12`, `--accent-ink: #062117`. Re-check the unread bar, the FAB, and the today disc. The theme toggle should feel like the same ink at night.

**P2.3 One mark.**

- 20–24px header and PWA icon: a minimal "T" (or a one-stroke wink) in a 6px-radius square, one color (`currentColor` or `--accent-2`). Replace `/icon.png` in `Wordmark()` once the asset exists. Do not draw the cabinet at 22px; the photo turns to mud at that size, which is what the headers show now.
- Account hub hero: keep the flat SVG chest (`cabinet` in `hub.templ`). It is an illustration, not the favicon. Color its drawers with `--accent`, not a second teal.
- Hub app rows: stop using emoji from `KURA_CLIENTS_JSON` for `ClientIcon`. Use the same small mark plus the short name. Emoji can stay in the env for clients that are not the six first-party apps, if that path still exists.

**P2.4 Assistant home should resemble the landing mock.**

Not a fake phone inside the app. The list from P1.4 (title, one muted line, relative time) plus the forest user bubble from P2.1 inside the thread. The landing bubble is the promise; the thread is where it has to be true. Empty state, when there are no chats, replaces "No chats yet." with one sentence and the existing pill FAB: pt "Pergunte sobre a sua agenda, os seus gastos ou as suas notas." / en "Ask about your calendar, spend, or notes." No illustration.

**P2.5 Hub order.**

Once timezone is non-empty and valid, do not lead with the form. Render the app list first (it is why the hub exists). Move the timezone card below the apps, collapsed to a single meta row: "Timezone · America/Sao_Paulo" with a disclosure that reveals the current form. An invalid or empty zone still leads, with the P0 salary-style treatment: one sentence, one button.

---

## Proposed mobile design system (sketch)

This is the contract for the P1 PR. Implement it with the tokens and classes above. Do not introduce a component library.

**Type scale.** The five tokens in P1.2. Serif only for `--text-display`. Everything else is the system sans. No third family.

**Spacing scale.** 4 / 8 / 12 / 16 / 24 / 32. See the table in P1.3.

**Color roles (current hexes until P2).**

| Role | Token | Used for |
|---|---|---|
| Canvas | `--bg0` | page behind cards and rows |
| Chrome | `--sidebar` | header bar, chip track |
| Raised | `--surface` | cards, search, menus |
| Hairline | `--line`, `--line-soft` | card border, row separator if a row is not a card |
| Text | `--ink` | titles, amounts |
| Secondary text | `--muted` | snippets, kickers, timestamps |
| Action | `--accent` | one primary per screen, unread bar, today numeral, links |
| Action label | `--accent-ink` | text on `--accent` |
| Action wash | `--accent-soft` | account chip, selected chip, press state |
| Danger | `--danger` | confirm-delete only, never a resting icon |

**Header.** Left: mark + short name, both allowed to shrink, name ellipsizes only after the short name itself cannot fit (it will fit). Right: circular account button (SVG only, existing aria-label), circular kebab. Height 44px content + safe-area padding. Background `--sidebar`. Bottom border `1px solid var(--line-soft)`. No theme icon, no lock icon, no second "+".

**Nav.** No bottom tab bar. App-to-app is the account button → hub (P0/P1) and, later, a sheet (P1.8). In-app navigation stays what it is: Email/Notes chips, Calendar month chevrons, People relationship chips.

**List row.** P1.4. Pressed state: background `--accent-soft`. Do not use a card border on every row; reserve cards for a single object (Spend hero, Calendar day, Account app, People upcoming).

**FAB / primary.**

- Shape: pill, `border-radius: 999px`, background `--accent`, label `--accent-ink`, shadow `--shadow`.
- Icon-only (Notes, Email, People): 56×56, bottom trailing, `right/bottom: max(16px, safe-area)`.
- With verb (Assistant, and any create whose verb is not obvious): height 48px, inline padding 16px 20px, icon then short label.
- Never two of them, and never one plus a full-width `.btn-new` on the same mobile screen (P0.2).
- Lists keep bottom padding `calc(5.5rem + env(safe-area-inset-bottom))` so the last row clears the FAB when scrolled to the end.

**Safe area / PWA.**

- Keep `viewport-fit=cover` on every layout.
- Keep `padding-top: env(safe-area-inset-top)` on the fixed shell and `padding-bottom` likewise.
- FAB and `.mobile-menu` already use `env(safe-area-inset-*)`. Do not switch them to raw `1rem` or the home indicator will cover them.
- `theme-color` follows the active theme (P0.9). Light value is the header color `#e9edeb`, not the page and not the dark background.
- `black-translucent` goes away until the header is intentionally full-bleed under the status bar.
- Inputs stay `font-size: 1rem` or iOS will zoom on focus. The search spec above is 1rem on purpose.
- People and Account layouts omit `interactive-widget=resizes-content`, which Email, Spend, and Assistant set. Add it to all seven viewport metas so the keyboard shrinks the shell instead of covering the composer.

---

## Do not change yet

These are fine. A later PR that "cleans them up" will make the suite worse.

- Monday-first calendar. Correct for Brasil. Do not switch to Sunday.
- BRL formatting (`R$0.00` with the tabular figures). Change the zero-salary *state*, not the currency formatter.
- The internal `kura` identifiers (module paths, `KURA_*`, `kura_locale`, OAuth client ids, CSS comments, `.btn-kura`). The README is right: renaming them is a migration, and none of the eight shots show the word "Kura" or a "K" mark. No user-visible K branding to remove.
- Account's SVG chest as a hero illustration.
- People avatar hues derived from the name, and relationship chips driven by stored relationships (Amiga, Pai). Those chips are data, not decoration.
- Notes' title + muted snippet structure. It is the row to copy. Only the always-on trash goes.
- Calendar's dashed ICS treatment and the "SHIFT" badge. Add a legend (P1.7); do not make imported events look like native ones.
- Assistant's labeled pill FAB. It is the create control the others should learn from.
- Serif on the hub title, the calendar month, and the day heading. Extend it to Spend's month (P1.2). Do not replace it with the system sans.
- The single light accent `#0e7490` shared by all seven apps, until the P2 recolor ships as one token change across all seven files.
- Dark mode as a feature. Do not remove the theme control; move it into the existing menu (P0.1).
- Safe-area insets already on the FAB, the shell, and the mobile menu. Adjust what overlaps them; do not delete the `env()` math.
- Offline banner, lock, auto-lock, and the language switch. They are buried on purpose once the header is quiet. Leave their behavior alone.
- Email folder set (Inbox / Sent / Drafts / Archive / Junk / …) and Notes folder counts. The pattern is right; the missing piece is the scroll mask.
- User-authored all-caps note titles. Do not title-case people's notes.

---

## Suggested implementation order

1. P0.1–P0.3 and P0.5 (CSS and one email template). One PR. Re-shoot the eight URLs at 390px.
2. P0.4, P0.6, P0.7 (templates + two strings). Re-shoot Account, Assistant, Spend.
3. P0.8, P0.9 (CSS mask + theme-color script). Check an installed PWA, not only Safari chrome.
4. P1.4–P1.6 as the row/search/touch PR.
5. P1.1–P1.3, P1.10 as the token PR, still on the current hexes.
6. P1.7, P1.9, then P1.8 if the hub round-trip still feels slow.
7. P2 only with a fresh set of screenshots next to `01-www-landing.png`.

Out of scope for all of the above: marketing-site code, new features, and any rename of `kura` identifiers.
