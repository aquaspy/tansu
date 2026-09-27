# Plan: polish the Assistant conversation modes UI

Status: plan only. Nothing here is implemented yet.
Scope: `apps/assistant` UI for the modes shipped in `c94f0e6` (Assistente, Conversa, Anônimo).
Audience: the agent implementing the polish. Every decision below is final unless marked "optional".

---

## 1. Where things are today

| Piece | File | Component / selector |
| --- | --- | --- |
| Mode switcher | `internal/views/modes.templ` | `ModeSwitcher`, `modeChoice` → `.mode-switch`, `.mode-option`, `.mode-form` |
| App connection badges | `internal/views/modes.templ`, `internal/handler/anonymous.go` (`appBadges`) | `AppBadges` → `.app-badges`, `.app-badge.is-ok / .is-down` |
| Example prompts (empty state) | `internal/views/modes.templ`, `internal/views/examples.go` | `ExampleBoard` → `.examples`, `.example-group`, `.example-chip` |
| "O que posso fazer?" | `internal/views/modes.templ` | `ExampleDrawer` → `<details class="can-do">` inside the composer dock |
| Anonymous page | `internal/views/modes.templ` | `AnonThread`, `AnonComposer` → `.anon`, `.anon-local-wrap`, `.anon-banner`, `.anon-pill`, `.anon-close` |
| Placement in saved threads | `internal/views/thread.templ` | `Thread`: switcher + badges + drawer are stacked at the top of `.composer-dock` |
| Styles | `web/static/css/input.css` | "conversation modes" block at the end of the file (about line 1515 onward) |
| JS | `web/static/js/app.js` | `controllers.examples`, `controllers.anon` (+ `anon*` helpers) |
| Copy | `internal/i18n/i18n.go` | `mode.*`, `chat.examples_hint`, `chat.what_can_i_do`, `chat.badge_*` |

Screenshots of the current state (dark and light, desktop 1360×860, mobile 390×844) are attached to the PR that adds this file.

### The design language this UI must match

Taken from `input.css` and the existing shell:

- **One accent, used sparingly.** A solid `--accent` fill is only used for primary actions: `.btn-new` ("Novo chat"), `.btn-primary` ("Enviar"), the mobile `.fab`. Selected state in segmented controls uses either
  - `--accent-soft` background with `--accent-2` text (`.theme-choice.is-on`, `.note-item.is-on`), or
  - small solid pills *inside a bordered track* (`.effort-picker`, `.search-picker`, `.voice-pills`: 2px padding, `--line-soft` border, `--bg0` track, radius 999px).
- **Quiet chrome.** `.icon-btn` is transparent `--muted` and turns `--accent-soft` on hover. `.editor-bar` is one 1px `--line-soft` row. Status uses `.chat-cost`: a tiny 0.7rem pill with a `--surface` background and a `--line-soft` border.
- **Radii.** Buttons and menu items use 10–12px. Pills and tracks use 999px. Cards and dialogs use 16–20px. The composer dock uses 18px.
- **Type.** System stack. Body text is about 0.9–1rem. Labels in the settings row are 0.7rem, uppercase, letter-spacing 0.05em, `--muted`, weight 600–650.
- **Motion.** 140ms color/background transitions, gated by `prefers-reduced-motion`.
- **Mobile (≤860px).** Touch targets are at least 2.6rem, the dock is full width with an 0.6rem margin, the editor bar is at least 3.2rem tall, and sheets slide up (`.mobile-menu`, `sheet-up`).

---

## 2. What is wrong (concrete)

Ordered roughly by how much each issue hurts.

1. **Anônimo layout is broken: the composer floats at the top of the page.**
   `AnonThread` wraps everything in `<div class="anon">`, but `.anon` has no flex rules. Inside `.col-editor` (a flex column) the wrapper collapses to its content, so `.transcript { flex: 1 }` never grows and `.composer-dock` sits right under the header. You can see it in both the desktop and mobile screenshots. This alone makes the mode look unfinished.

2. **The mode switcher is the loudest thing on the screen.**
   `.mode-option.is-on` is a solid `--accent` pill (bright teal on dark, deep teal on light). It competes with "Enviar" and "Novo chat", which are the only other solid-accent elements. It also doesn't match any other segmented control in the app: effort, search, voice and theme all sit in a bordered track, and this one floats bare with a 0.25rem gap.

3. **The composer dock has turned into a stacked control panel.**
   In an Assistente thread the dock is now: mode row → badge row → "O que posso fazer?" disclosure (once there are messages) → VOZ row → textarea → tool row. That is 4–5 rows of chrome above the textarea, each with its own padding (`0.55rem 0.75rem 0.15rem`, `0.2rem 0.9rem 0.15rem`, `0 0.85rem 0.35rem`…) and none sharing the `.chat-settings` rhythm. On mobile the dock takes about a third of the screen before you type anything.

4. **The switcher looks like a toggle but acts as navigation.**
   Picking another mode starts a *new* thread (POST `/conversations/` with `mode`, or GET `/anonymous?new=1`). In a thread that already has messages, the control reads as "change this chat's mode", and clicking it silently moves you somewhere else. The markup also says `role="radiogroup"` while containing buttons with `aria-pressed` and a link.

5. **Connection badges are noisy and hard to read.**
   `AppBadges` prints one pill per linked app, on every Assistente thread, forever. Healthy pills (`--ink` text, `--line` border) and failed ones (`--danger` text and border) have the same weight, the 0.72rem pills don't match `.chat-cost` or the settings labels, and the only detail is in `title` (invisible on touch). When nothing is linked, nothing renders, so the user never learns that Assistente needs apps.

6. **The Assistente empty state looks like a debug dump.**
   `ExampleBoard` is left-aligned at the top of the transcript: a small muted hint, four uppercase `<h2>` group labels, and eight pill buttons. There is no heading and no sense of "you are in Assistente". On mobile, the 999px-radius pills wrap to two lines and become lumpy capsules (`.example-chip` has `border-radius: 999px` and `text-align: left`). The group labels are `<h2>`s inside the transcript, which adds headings that mean nothing to screen readers.

7. **Conversa's empty state is a blank void.**
   With no messages, Conversa shows an empty transcript and the dock. Nothing says what the mode is for, so it feels broken next to Assistente.

8. **Anônimo's chrome is scattered and looks bolted on.**
   - `.anon-local-wrap` ("NESTE NAVEGADOR" + horizontally scrolling thread buttons) is a free-floating strip between the header and the transcript. It uses its own uppercase micro-label and 8px buttons that match nothing else.
   - `.anon-banner` adds yet another dock row: a bordered `.anon-pill` "Não salvo na conta", a paragraph, and a **native checkbox** "Apagar ao fechar". It is the only native checkbox in the chat UI; every other toggle is a pill.
   - There is no persistent, quiet "you are anonymous" signal once the empty state scrolls away. The only signal is the switcher.
   - The pending reply shows a literal `…` (`status.textContent = "…"`) where saved threads show the localized `chat.thinking` status with its animated `::after`.

9. **"O que posso fazer?" pushes the composer around.**
   `<details class="can-do">` sits inside the dock. Opening it inserts the whole `ExampleBoard` above the textarea, so the dock grows upward over the transcript, and it can't be closed by picking a prompt.

10. **Small inconsistencies.**
    - `.mode-option` is `min-height: 2.4rem` on desktop, while neighbouring `.voice-pill` and `.search-pill` are about 1.9rem. The rows look mismatched.
    - `.app-badges` and `.anon-banner` use `0.9rem` side padding, `.mode-switch` uses `0.75rem`, `.can-do` uses a `0.85rem` margin, and `.chat-settings` uses `0.9rem`. Left edges don't line up.
    - `.mode-option:hover` only changes the text color. There is no hover background, unlike `.icon-btn`, `.btn-io` and `.theme-choice`.

---

## 3. Goals and non-goals

### Goals
- Make the modes feel native to Tansu Assistant: quiet, elegant, simple. The chat stays the hero.
- Keep at most **one** row of chrome above the textarea in every mode. Everything else lives in the empty state, the editor bar, or an on-demand sheet.
- Make it obvious which mode a thread is in, and that choosing another mode starts a new chat.
- Show app connection problems when they matter, and stay silent when everything works.
- Fix the Anônimo layout bug.
- Mobile first-class: nothing wraps into lumpy pills, and all targets are at least 2.6rem.

### Non-goals (do not touch)
- Mode semantics: switching still starts a new thread. Assistente uses tools, Conversa and Anônimo never do.
- Server-side tool gating, health checks (`internal/chat/health.go`, `internal/suite/health.go`), system prompts, `AllowsTools()`.
- Anonymous storage: `kura.anon.pref` / `kura.anon.v1`, `localStorage` vs `sessionStorage`, the `/anonymous/complete` protocol, the `anon-seed` no-JS path.
- Env flags and defaults: `ASSISTANT_SHOW_MODEL_CONTROLS` (default false, still hides model and effort), `SEARCH_ENABLED`, and so on.
- Persistence, routes, handlers' behavior, store code.
- The sidebar, the auth pages, message rendering, tool action cards, the share dialog, the voice feature.
- Tooling: no new build steps, no new JS/CSS dependencies, no Tailwind utility classes in templates (the app styles through `input.css`).
- No new design tokens unless listed in §6.

---

## 4. Visual direction

> "A calm chat window that happens to know what mode it is in."

- The **empty state carries the mode**: a centered heading, one short line saying what the mode does, and (Assistente only) a few example cards. The switcher lives there, centered above the heading.
- Once the conversation starts, the **mode becomes a small label in the editor bar**, next to the title, in the same visual family as `.chat-cost`. Clicking it opens a tiny menu: "Novo chat em: Assistente / Conversa / Anônimo".
- **Connection health** is a single quiet line (dot + text). It is green-dot silent when everything is fine, and it only speaks up in the editor bar when something is down.
- **Anônimo** is recognizable by the eye-off label in the editor bar and the composer placeholder "Mensagem anônima…". There are no banners.
- No new colors. Selection = `--accent-soft` + `--accent-2`. Solid `--accent` stays reserved for "Novo chat" and "Enviar".

Target sketch (desktop, empty Assistente thread):

```
┌ editor-bar ───────────────────────────────────────────────────────────┐
│ Sem título                                          [💡] [share] [🗑] │
└───────────────────────────────────────────────────────────────────────┘

                 ╭──────────────────────────────────────╮
                 │ Assistente │ Conversa │  Anônimo    │   ← .mode-switch (track)
                 ╰──────────────────────────────────────╯
                          Como posso ajudar?                ← .mode-hero-title
       Leio e altero Pessoas, Gastos, Calendário e Notas por você.
                  ● Apps conectados · Calendário indisponível  ← .app-status

        ┌────────────────────────┐  ┌────────────────────────┐
        │ PESSOAS                │  │ GASTOS                 │  ← .example-card
        │ Cria a Ana, irmã, …    │  │ Registra um almoço …   │
        └────────────────────────┘  └────────────────────────┘
        ┌────────────────────────┐  ┌────────────────────────┐
        │ CALENDÁRIO             │  │ NOTAS                  │
        │ Marca dentista amanhã… │  │ Cria uma nota na pasta…│
        └────────────────────────┘  └────────────────────────┘
                        Mais exemplos                          ← opens the sheet

   ╭ composer-dock ──────────────────────────────────────────────────╮
   │ VOZ [LER EM VOZ ALTA][ENVIO AUTOMÁTICO]                          │  ← unchanged
   │ Mensagem…                                                        │
   │ [WEB][DEEP] 📎 🎙                                        [Enviar] │
   ╰──────────────────────────────────────────────────────────────────╯
```

Thread with messages:

```
│ ‹  Viagem pra Floripa   [✦ Assistente ▾]   ● Calendário indisponível   R$0,02  [💡] [share] [🗑] │
```

---

## 5. Prioritized changes

Each item lists the files, what to do, and acceptance criteria (AC). Do P0 first and ship it as its own commit. P1 and P2 can each be one commit per item.

### P0-1 — Fix the Anônimo layout
- **Files:** `web/static/css/input.css`
- **Do:** add `.anon { display: flex; flex-direction: column; flex: 1; min-height: 0; }`.
- **AC:** on `/anonymous` (desktop and 390px mobile) the composer dock sits at the bottom of the editor column and the transcript scrolls above it, exactly like a saved thread. Sending a message keeps the dock pinned.

### P0-2 — Restyle the mode switcher as a quiet segmented track
- **Files:** `input.css` ("conversation modes" block), `modes.templ` (`ModeSwitcher`, `modeChoice`)
- **Do:**
  - `.mode-switch`: `display: inline-grid; grid-auto-flow: column; grid-auto-columns: 1fr; gap: 2px; padding: 2px; border: 1px solid var(--line-soft); border-radius: 999px; background: var(--bg0);`. Remove the old padding and wrap.
  - `.mode-form { margin: 0; display: flex; }` so the form's button fills the grid cell.
  - `.mode-option`: `justify-content: center; width: 100%; min-height: 2rem; padding: 0.3rem 0.9rem; font-size: 0.82rem; font-weight: 600; border-radius: 999px; color: var(--muted); transition: color 140ms ease, background 140ms ease;`
  - Hover (not selected): `color: var(--ink); background: color-mix(in srgb, var(--accent-soft) 60%, transparent);`
  - Selected (`.is-on`): `background: var(--accent-soft); color: var(--accent-2); cursor: default;`. **No solid accent.**
  - Accessibility: change the wrapper to `role="group"` (keep `aria-label={ p.T("mode.label") }`). Selected item: `aria-current="true"` (drop `aria-pressed` on all items). Keep the `title` tooltips.
  - Keep the class names `mode-switch` and `mode-option` (tests and JS rely on them).
- **AC:** in both themes, the selected mode is readable but visibly calmer than "Enviar". All three segments are equal width and the same height whether they render as `<button>` or `<a>`. Focus rings use `--ring`.

### P0-3 — Move the switcher out of the composer dock; add a mode hero for empty threads
- **Files:** `thread.templ` (`Thread`), `modes.templ` (new `ModeHero`, `AnonThread`), `input.css`, `i18n.go`
- **Do:**
  - New component `ModeHero(p Page, mode string, badges []AppBadge, appsTotal int)` rendered **inside `.transcript`** when the thread has no messages (saved threads: `len(d.Current.Messages) == 0`; Anônimo: always rendered in the markup, and `controllers.anon` hides it with `hidden` once the active local thread has messages and shows it again after "Apagar agora" or when switching to an empty local thread).
  - Hero structure (centered column, `max-width: 34rem`, `margin: auto` vertically inside the transcript, `gap: 0.9rem`, `text-align: center`):
    1. `@ModeSwitcher(p, mode)`
    2. `<h1 class="mode-hero-title">`: Assistente "Como posso ajudar?" / Conversa "Sobre o que vamos conversar?" / Anônimo "Chat anônimo". Style: 1.35rem, weight 700, letter-spacing -0.02em, `--ink` (same as `.share-card h1`, a touch smaller).
    3. `<p class="mode-hero-lede">`: 0.92rem, `--muted`, line-height 1.55, max 30rem. Copy in §7.
    4. Assistente only: `@AppStatus(...)` (P1-1). Until P1-1 lands, keep `@AppBadges` here instead of in the dock.
    5. Assistente only: example cards (P0-4).
    6. Anônimo only: the privacy line and the "Apagar ao fechar" pill (P1-3).
  - Remove `@ModeSwitcher`, `@AppBadges` and `@ExampleDrawer` from `.composer-dock` in `Thread` and `AnonThread`. **Remove the `.anon-banner` from the dock** (its content moves into the hero, see P1-3; the `mode.not_saved` text must still be on the page, since `TestAnonymousIsNotAccountHistory` asserts it).
  - To make the hero center vertically, the transcript needs to allow it: add `.transcript:has(> .mode-hero:only-child), .transcript:has(> .mode-hero:not([hidden]):first-child) { align-content: center; }`. A simpler equivalent is fine, for example a modifier class `.transcript.is-empty` set by the template, toggled by `controllers.anon` for Anônimo.
- **AC:**
  - Empty Assistente / Conversa / Anônimo each show a centered hero with switcher, heading and lede. The dock above the textarea only contains the existing VOZ row (saved threads) or nothing (Anônimo, unless `ASSISTANT_SHOW_MODEL_CONTROLS=true`, in which case the existing model/effort row stays).
  - The dock height of an empty thread is the same as a thread with messages.
  - `go test ./...` still passes: `mode-switch` and "Assistente" remain in thread HTML (see P1-2 for threads with messages), `example-chip` still appears in empty Assistente threads and still does not appear in Conversa or Anônimo.

### P0-4 — Replace example pills with example cards
- **Files:** `modes.templ` (`ExampleBoard`), `examples.go` (optional helper), `input.css`
- **Do:**
  - `ExampleBoard` in the hero renders **one card per app** (the first item of each `ExampleGroup`) in a grid: `display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.6rem; width: 100%; text-align: left;`
  - Each card is the existing button, **keeping the class `example-chip`** plus a new `example-card` (tests assert `example-chip`; JS action `examples#fill` is unchanged):
    ```
    <button type="button" class="example-chip example-card" data-action="examples#fill" data-examples-text-param={ item }>
      <span class="example-app">{ g.Label }</span>
      <span class="example-text">{ item }</span>
    </button>
    ```
  - `.example-card`: `display: grid; gap: 0.3rem; padding: 0.75rem 0.85rem; border: 1px solid var(--line-soft); border-radius: 12px; background: var(--surface); color: var(--ink); font-size: 0.9rem; line-height: 1.4; transition: border-color 140ms ease, background 140ms ease;` Hover: `border-color: var(--line); background: color-mix(in srgb, var(--accent-soft) 45%, var(--surface));`
  - `.example-app`: the same micro-label as `.setting` (0.68rem, 650, uppercase, 0.05em, `--accent-2`).
  - `.example-text`: clamp to 2 lines (`display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden;`).
  - Replace the `<h2>` group labels (no headings inside the transcript).
  - Drop `chat.examples_hint` from the hero, since the hero lede already sets context. Keep the i18n key; it is reused in the sheet (P1-4).
  - Below the grid: a text button "Mais exemplos" (`.link-btn`: transparent, `--muted`, 0.84rem, 600, hover `--ink`) that opens the examples sheet (P1-4). Until P1-4 lands, it can be omitted.
  - The English test string "Log lunch for R$ 42.50 today under food" is the first Spend item, so it stays in the default render.
- **AC:** 4 calm cards in 2 columns on desktop and 1 column below 520px. No capsule wrapping. Clicking a card fills the composer and focuses it (the current behavior).

### P1-1 — One quiet connection indicator instead of badge pills
- **Files:** `modes.templ` (replace `AppBadges` with `AppStatus`; keep the `AppBadge` model), `thread.templ`, `models.go` (optional `AppsDown()` helper on `ConversationDetail` or a free func), `input.css`, `i18n.go`
- **Do:**
  - `AppStatus(p Page, badges []AppBadge)` renders one inline element:
    - `badges` empty → `<a href="/apps" class="app-status is-none">` with a hollow dot, "Nenhum app conectado · Conectar apps".
    - all `ok` → `<span class="app-status is-ok">` with a `--tier-cheap` dot and "Apps conectados". The `title` lists them ("Pessoas, Gastos e Notas conectados").
    - some `down` → `<a href="/apps" class="app-status is-down">` with a `--danger` dot. Text: one down → "%{app} indisponível" (existing `chat.badge_down`). Several down → "%{n} apps indisponíveis". `title` = the joined `AppBadge.Title`s, which include the detail.
  - Style: like `.chat-cost` minus the border. `display: inline-flex; align-items: center; gap: 0.4rem; font-size: 0.78rem; font-weight: 600; color: var(--muted); text-decoration: none; border-radius: 999px; padding: 0.2rem 0.55rem;`. Dot `0.45rem` circle. `a.app-status:hover { background: var(--accent-soft); color: var(--ink); }`. `.is-down` text uses `--ink` (the dot carries the danger color; red text is too loud).
  - Placement:
    - Empty Assistente → in the hero under the lede (all three states).
    - Assistente with messages → in the `.editor-bar` right after the title form, **only when `is-down` or `is-none`**. Silent when OK.
    - Conversa / Anônimo → never.
  - Delete the `.app-badges` / `.app-badge` CSS once unused.
- **AC:** a healthy Assistente thread with messages shows no connection chrome at all. A broken app shows one small dot + text in the header that links to "Conectar apps". Touch users can reach the detail via the link target (the `/apps` page) instead of a tooltip.

### P1-2 — Mode label + "Novo chat em…" menu in threads with messages
- **Files:** `modes.templ` (new `ModeMenu`), `thread.templ`, `modes.templ` (`AnonThread` editor bar), `input.css`, `app.js` (tiny generic close-on-outside handler, see below), `i18n.go`
- **Do:**
  - When a saved thread has messages, and in Anônimo once the active local thread has messages, the `.editor-bar` shows, right after the title: `ModeMenu(p, mode)`, a `<details class="mode-menu">`:
    - `<summary class="mode-tag">` shows the mode icon + the name ("Assistente" / "Conversa" / "Anônimo") + a chevron. Style like `.chat-cost` but interactive: 0.75rem, 600, `--muted`, `--surface` bg, `--line-soft` border, radius 999px, padding `0.2rem 0.6rem`. Hover `--accent-soft`. Remove the default marker.
    - The panel reuses `.model-menu` visuals (surface, `--line` border, 12px radius, `--shadow`, 4px padding), but opens **downward** (`top: calc(100% + 6px)`) and is right- or left-aligned to fit.
    - Panel content: a heading line `mode.new_in` "Novo chat em" (0.7rem uppercase muted), then **the same `@ModeSwitcher(p, mode)` markup rendered vertically** (add modifier `.mode-switch.is-list`: `grid-auto-flow: row; border: 0; background: transparent; padding: 0;` with items left-aligned and radius 8px, like `.model-option`). The current mode shows a check icon (reuse the `.model-option` check SVG) and is `aria-current="true"`. This keeps the `mode-switch` class in thread HTML for `TestWebParamIgnoredWhenDisabled` (flow_test line ~582).
  - Mode icons (16×16, stroke 1.3, `currentColor`, same style as the existing SVGs):
    - Assistente: a 4-square grid (reuse the Apps icon from `shell.templ`).
    - Conversa: a speech bubble.
    - Anônimo: an eye with a slash.
  - JS: add a small generic behavior in `app.js`: clicking outside an open `details.mode-menu`, or pressing Escape, closes it (remove the `open` attribute). No new controller framework. Follow the existing `controllers` pattern (for example `controllers.disclosure` with `data-controller="disclosure"` on the `<details>`).
  - Hide `.mode-tag` text on very narrow screens only if the bar overflows: at ≤380px show the icon only and keep `aria-label` on the summary.
- **AC:** in a thread with messages, no mode UI appears in the dock. The header shows e.g. "✦ Conversa ▾". Opening it offers "Novo chat em" with three options, and picking one behaves exactly as the old switcher (same POST / link). Keyboard: Tab to the summary, Enter opens, Tab through options, Escape closes.

### P1-3 — Anônimo chrome cleanup
- **Files:** `modes.templ` (`AnonThread`, `AnonComposer`), `app.js` (`controllers.anon`, `anonRenderLocal`, `send`), `input.css`, `i18n.go`
- **Do:**
  - **Local threads:** remove the `.anon-local-wrap` strip. Add an `.icon-btn` (clock/history icon) in the editor bar, before the delete button, with `title`/`aria-label` `mode.local_menu` "Chats neste navegador". It opens a `<details class="mode-menu anon-history">` panel (same visuals as P1-2) that `anonRenderLocal` fills with `.model-option`-styled buttons (active thread gets the check and `aria-current`). Keep `data-anon-target="local"` on the panel list so the JS keeps working. Hide the button when there is ≤1 local thread (toggle `hidden` in `anonRenderLocal`).
  - **Privacy copy:** the hero lede for Anônimo is `mode.anonymous_lede` (see §7). Directly below it, render `<p class="mode-hero-note">` with `mode.not_saved` + " · " + `mode.not_saved_detail` in 0.8rem `--muted`. This keeps "Not saved to your account" in the page for the test.
  - **Clear on close:** replace the native checkbox with the existing pill toggle pattern: `<label class="voice-option anon-close">` + hidden checkbox (keep `data-anon-target="close"` and `data-action="change->anon#close"`) + `<span class="voice-pill">` with a small timer icon + `mode.clear_on_close`. Put it in the hero under the note (centered). Also put it inside the history panel footer so it stays reachable after the hero hides.
  - **Persistent signal:** the `.mode-tag` in the editor bar (P1-2) always shows for Anônimo, even when empty (eye-off + "Anônimo"). Add i18n `chat.placeholder_anon` "Mensagem anônima…" and use it as the `AnonComposer` textarea placeholder.
  - **Pending status:** in `controllers.anon.send`, use the localized thinking text instead of `"…"`. Pass it via a new `data-anon-thinking-value={ p.T("chat.thinking") }` on `.anon` and set `status.textContent = scope.dataset.anonThinkingValue`. Remove the dead `${scope.dataset.anonUntitledValue ? "" : ""}` template expression.
  - Delete the CSS for `.anon-banner`, `.anon-pill`, `.anon-local-wrap`, `.anon-local-label`, `.anon-local`, `.anon-thread` once unused (replace `.anon-thread` with `.model-option` styling).
- **AC:** Anônimo shows no strip under the header and no banner in the dock. Empty: centered hero with switcher, "Chat anônimo", lede, privacy note, "Apagar ao fechar" pill. With messages: a clean transcript, "👁̸ Anônimo ▾" in the header, a history icon when there are 2+ local threads, and the placeholder "Mensagem anônima…". All `localStorage`/`sessionStorage` behavior is identical (verify by toggling "Apagar ao fechar", reloading, and closing and reopening the tab).

### P1-4 — "O que posso fazer?" becomes an on-demand sheet
- **Files:** `modes.templ` (`ExampleDrawer` → `ExampleSheet`), `thread.templ`, `input.css`, `app.js` (`controllers.examples`)
- **Do:**
  - Remove the `<details class="can-do">` from the dock.
  - In Assistente threads (empty or not), add an `.icon-btn` to the `.editor-bar` (before share) with a lightbulb icon, `title`/`aria-label` `chat.what_can_i_do` "O que posso fazer?". It opens `<dialog class="confirm-dialog examples-dialog">` (reuse dialog styles: 16px radius, `--shadow`, backdrop). Width `min(34rem, calc(100vw - 2rem))`.
  - Dialog content: heading `chat.what_can_i_do` (style `.share-heading`), hint `chat.examples_hint` (0.85rem muted), then the **full** list: all items of every group, grouped under the `.example-app` micro-label, rendered as `.example-chip.example-card` in a single column (compact padding `0.6rem 0.75rem`), and a close `.btn-dialog` "Fechar" (`app.close`, add it if missing, otherwise reuse `app.cancel`).
  - "Mais exemplos" in the hero (P0-4) opens the same dialog.
  - `controllers.examples`: add `open()` (calls `showModal()` on the dialog target) and `backdrop()` (click on the dialog element itself closes it, same as `confirm#backdrop`). In `fill()`, after filling, call `closest("dialog")?.close()` on the clicked element.
  - Mobile: at ≤860px the dialog becomes a bottom sheet: `margin: auto 0.6rem calc(0.6rem + env(safe-area-inset-bottom)); width: auto; max-height: 75dvh; overflow: auto; animation: sheet-up 160ms ease-out;` (the keyframe already exists in the mobile block; move it out of the media query so it can be reused).
- **AC:** the dock never changes height because of examples. From any Assistente thread, the lightbulb opens the sheet, picking a prompt fills the composer, closes the sheet and focuses the textarea. Esc and a backdrop click close it. Conversa and Anônimo have no lightbulb.

### P1-5 — Spacing and alignment pass on the dock and hero
- **Files:** `input.css`
- **Do:**
  - Any remaining mode-related row inside `.composer-dock` uses the `.chat-settings` rhythm: `padding: 0.3rem 0.9rem`, `border-bottom: 1px solid var(--line-soft)`.
  - The hero's inner width matches the transcript column (`max-width: 34rem` centered). Cards align to the same left edge as the lede.
  - Vertical rhythm in the hero: switcher → 1.1rem → title → 0.4rem → lede → 0.7rem → status → 1.2rem → cards → 0.6rem → "Mais exemplos".
- **AC:** left edges of the dock rows line up at 0.9rem, and the hero looks centered and balanced at 1360×860 and 390×844 in both themes.

### P2-1 — Examples reflect connected apps (optional)
- **Files:** `thread.templ`, `modes.templ`, `examples.go`
- **Do:** pass the badges into `ExampleBoard`. Order connected apps first. Cards for apps that are down get `.is-off` (opacity 0.55, `title` = the badge title) but stay clickable. Apps that are not linked at all are omitted from the hero grid (they stay in the sheet). If no app is linked, show no cards and let the `AppStatus` "Conectar apps" link do the work.
- **AC:** with only Pessoas linked, the hero shows the Pessoas card first and does not pretend Calendar works.

### P2-2 — Mode icons in the switcher (optional)
- Add the P1-2 icons (0.9rem) before each label in `.mode-option`. On ≤380px, the hero switcher may hide labels only if it overflows. It must keep `aria-label`s.

### P2-3 — Micro-motion (optional)
- The hero fades in (`opacity` 0→1, 160ms) on first paint and when Anônimo returns to empty. Card hover uses a 1px lift only on `(hover: hover)`. Everything respects `prefers-reduced-motion`, which the global reduce rule already handles.

---

## 6. CSS and token guidance

- **No new color tokens.** Use `--accent-soft`/`--accent-2` for selection, `--line-soft` for borders at rest, `--line` for hover borders, `--muted` for secondary text, `--tier-cheap` for the OK dot, `--danger` for the down dot.
- **Solid `--accent` is reserved** for "Novo chat", "Enviar" and the FAB. Existing search, effort and voice pills keep their current solid-in-track look; do not restyle them in this pass.
- Put all new rules in the "conversation modes" block at the end of `input.css`. Replace the old rules there instead of layering overrides. Mobile overrides go inside the existing `@media (max-width: 860px)` block, next to the other editor rules.
- Reuse, don't clone: `.model-menu`/`.model-option` visuals for menus, `.confirm-dialog` for the sheet, `.voice-option`/`.voice-pill` for the clear-on-close toggle, `.icon-btn` for header buttons, `.chat-cost` proportions for the mode tag.
- New class names (keep them this short): `.mode-hero`, `.mode-hero-title`, `.mode-hero-lede`, `.mode-hero-note`, `.mode-menu`, `.mode-tag`, `.mode-switch.is-list`, `.app-status` (`.is-ok/.is-down/.is-none`), `.example-card`, `.example-app`, `.example-text`, `.examples-dialog`, `.link-btn`, `.anon-history`.
- Classes that must remain because tests or JS reference them: `mode-switch`, `mode-option`, `example-chip`, `composer`, `.composer textarea[name="content"]`, and all `data-anon-*` targets.

## 7. Copy (PT-BR first; add EN equivalents in `i18n.go`)

| Key | PT | EN |
| --- | --- | --- |
| `mode.hero_assistant` (new) | Como posso ajudar? | How can I help? |
| `mode.hero_chat` (new) | Sobre o que vamos conversar? | What should we talk about? |
| `mode.hero_anonymous` (new) | Chat anônimo | Anonymous chat |
| `mode.assistant_lede` (new) | Leio e altero Pessoas, Gastos, Calendário e Notas por você. | I can read and change People, Spend, Calendar, and Notes for you. |
| `mode.chat_lede` (new) | Conversa livre, salva na sua conta. Não mexe nos seus outros apps. | Plain chat, saved to your account. It doesn't touch your other apps. |
| `mode.anonymous_lede` (new) | Fica só neste navegador, fora do histórico da conta. | Stays in this browser only, out of your account history. |
| `mode.new_in` (new) | Novo chat em | New chat in |
| `mode.local_menu` (new) | Chats neste navegador | Chats in this browser |
| `chat.placeholder_anon` (new) | Mensagem anônima… | Anonymous message… |
| `chat.apps_ok` (new) | Apps conectados | Apps connected |
| `chat.apps_down_many` (new) | %{n} apps indisponíveis | %{n} apps unavailable |
| `chat.apps_none` (new) | Nenhum app conectado · Conectar apps | No apps connected · Connect apps |
| `chat.more_examples` (new) | Mais exemplos | More examples |
| `app.close` (new if missing) | Fechar | Close |

Existing keys that stay in use: `mode.assistant`, `mode.chat`, `mode.anonymous`, `mode.*_tip` (tooltips), `mode.label`, `mode.not_saved`, `mode.not_saved_detail`, `mode.clear`, `mode.clear_on_close`, `mode.clear_on_close_tip`, `chat.badge_ok`, `chat.badge_down`, `chat.what_can_i_do`, `chat.examples_hint`, `chat.thinking`.
`mode.local` ("Neste navegador") becomes unused once the strip is gone; delete it.

## 8. Mobile notes (≤860px)

- The hero: the switcher is full width (`width: 100%`, segments at least 2.6rem tall, 0.9rem text). The title is 1.2rem. Cards go to 1 column below 520px, with at least 3rem tap height.
- The editor bar already holds back, title input, cost, share and delete. Adding the mode tag, the lightbulb, the status and (Anônimo) history must not overflow:
  - The title input keeps `flex: 1 1 auto; min-width: 5rem`.
  - `.mode-tag` shows icon only at ≤480px (label kept in `aria-label`).
  - `.app-status` in the header shows the dot only at ≤480px (text in `aria-label` and `title`); the full text stays in the hero.
  - `.chat-cost` already shrinks to 5.2rem; leave it.
- Menus (`.mode-menu` panels) are positioned so they don't clip off-screen: right-align them when they sit on the right half. `max-width: calc(100vw - 1.2rem)`.
- The examples dialog becomes a bottom sheet (P1-4).
- Check with the on-screen keyboard open (`--vvh` handling already exists): the dock must stay pinned in Anônimo after P0-1.

## 9. What NOT to change

- `internal/chat/*`, `internal/suite/*`, `internal/store/*`, handler logic (except passing extra view data if P2-1 needs it; no behavior change), routes, CSRF, rate limits.
- `ASSISTANT_SHOW_MODEL_CONTROLS` default (false) and its hide path: `ShowControls` still gates `ModelField`/`EffortField` in both `Thread` and `AnonComposer`. When true, the existing `.chat-settings` row renders as today.
- The anonymous JS storage model (`ANON_PREF`, `ANON_DATA`, `anonBucket`, `anonSave`, seeding from `#anon-seed`).
- Existing composer controls (web/deep, attach, mic, voice row), message rendering, action cards, share/delete dialogs, sidebar and "Novo chat" behavior.
- Tooling: templ version (`v0.3.1020` from `go.mod`), Tailwind v4 standalone, `app.css` stays gitignored.

## 10. Implementation checklist

From `apps/assistant`:

1. Edit `.templ`, `.go`, `input.css`, `app.js`, `i18n.go`.
2. `go run github.com/a-h/templ/cmd/templ@v0.3.1020 generate` and commit the regenerated `*_templ.go`.
3. `gofmt -l cmd internal && go vet ./... && go test ./...`, all green. If a test string assertion changes on purpose, update the test in the same commit and say why.
4. Build the CSS for manual checks: `tailwindcss --input web/static/css/input.css --output web/static/css/app.css` (do not commit `app.css`).
5. Run locally: `DATA_DIR=/tmp/kdata OPENROUTER_API_KEY=sk-test go run ./cmd/kurachat serve`, sign up at `http://127.0.0.1:3000/signup`, and switch to PT with the language toggle.
6. Screenshot before/after for: empty Assistente, empty Conversa, empty Anônimo, Anônimo with 2+ local threads, and an Assistente thread with messages. Each at 1360×860 dark and light, plus 390×844. Setting `KURA_PEOPLE_URL`/`KURA_SPEND_URL` to an unreachable URL plus `KURA_APPS_KEY` lets you see the "not linked" status. A linked-but-down app needs the e2e fleet (`scripts/e2e.sh`), or you can temporarily inject markup in DevTools.
7. One commit per P item; P0 items may share one commit.

## 11. Definition of done

- The Anônimo composer is pinned to the bottom.
- At most one chrome row above the textarea in any mode (VOZ, or the model/effort row when enabled).
- Empty threads show a centered, mode-specific hero. Threads with messages show only a small mode tag (+ status when broken) in the header.
- No solid-accent element other than "Novo chat", "Enviar" and existing selected pills in search/effort/voice.
- All PT copy from §7 is in place, and EN keys exist.
- Tests pass, and no behavior change in modes, gating, anonymous storage, or env defaults.
