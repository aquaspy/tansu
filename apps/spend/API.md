# TansuSpend API (v1)

A small JSON API so AI agents (OpenClaw, Hermes Agent, …) and other apps can
log and review your spend. Same validations and money rules as the web UI.

## Authentication

Create a token in the app: **More → API tokens**. Name it after the agent
(e.g. `Hermes Agent`), confirm with your password, and copy the token — it is
shown **once**.

Send it on every request:

```http
Authorization: Bearer kura_xxx…
```

Notes:

- A token has **full access** to one user's expenses, subscriptions, and
  payment days. Salary, home currency, and exchange rates stay manual in the
  web settings — the API never changes them.
- Tokens keep working while the app is locked. Losing one means revoking it
  under **More → API tokens** and generating a new one.
- Only the token digest is stored; the raw token cannot be recovered.

## Conventions

- Base path: `/api/v1` (e.g. `https://spend.gettansu.com/api/v1/expenses`).
- Dates are `YYYY-MM-DD`; month filters are `YYYY-MM`.
- Money is stored as integer cents. Writes accept either `amount` (a decimal
  string like `"25.50"` — both `.` and `,` separators work) or `amount_cents`
  (an integer, unambiguous). If both are sent, `amount_cents` wins.
- Currencies: `BRL`, `USD`, `EUR` (default `BRL`).
- Expense categories: `food`, `transport`, `home`, `health`, `leisure`, `other`.
- `POST`/`PATCH` accept fields nested (`{"expense": {...}}`) or flat
  (`{"title": ...}`). Same for `subscription` and `payment_day`.
- Success: `200 OK` (`201 Created` on create, `204 No content` on delete).
- Errors: `401 {"error":"unauthorized"}`, `404 {"error":"not_found"}`,
  `422 {"errors":[...]}` (human-readable messages),
  `429 {"error":"rate_limited"}` (60 writes/minute per token).

## Expenses

An expense: `title` (required), `amount`/`amount_cents` (required, > 0),
`currency`, `spent_on` date (required), `category`, `notes`.

```bash
# This month's expenses (default), optionally filtered by category
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/expenses?month=2026-09&category=food"

# Log a spend in the right category
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expense":{"title":"Lunch","amount":"25.50","currency":"BRL","spent_on":"2026-09-19","category":"food"}}' \
  "$KURA_URL/api/v1/expenses"

# Same with integer cents and flat params
curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"title":"Bus","amount_cents":600,"currency":"BRL","spent_on":"2026-09-19","category":"transport"}' \
  "$KURA_URL/api/v1/expenses"

# Read / recategorize / delete one expense
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/expenses/42"
curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"expense":{"category":"leisure"}}' \
  "$KURA_URL/api/v1/expenses/42"
curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/expenses/42"
```

## Subscriptions

A subscription: `title`, `amount`/`amount_cents`, `currency`,
`interval` (`monthly`/`yearly`, default `monthly`), `active`, `notes`.
There is no day of month and no billing month. An active subscription
counts toward leftover every month: a monthly amount in full, a yearly
amount as one twelfth (December keeps the remainder so the year adds up).
`due_day` and `billing_month` on a write are ignored. The date of a bill
is a payment day, not a field on the subscription.

```bash
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/subscriptions?active=true"

curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"subscription":{"title":"Music","amount":"19.90","currency":"BRL","interval":"monthly"}}' \
  "$KURA_URL/api/v1/subscriptions"

curl -X PATCH -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"subscription":{"active":false}}' \
  "$KURA_URL/api/v1/subscriptions/7"

curl -X DELETE -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/subscriptions/7"
```

## Payment days

Reminders only — they never change the leftover. A payment day: `title`,
`due_day` (1–31, required), `active`, `notes`. Log an expense when you pay.
When Spend has `KURA_CALENDAR_URL` and `KURA_SYNC_SECRET`, each create,
update, delete, or deactivation is mirrored onto Tansu Calendar as a
read-only monthly marker. Subscriptions are not.

```bash
curl -H "Authorization: Bearer $KURA_TOKEN" "$KURA_URL/api/v1/payment_days"

curl -X POST -H "Authorization: Bearer $KURA_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"payment_day":{"title":"Water","due_day":10}}' \
  "$KURA_URL/api/v1/payment_days"
```

`GET` one, `PATCH`, and `DELETE` follow the same shape as expenses.

## Month summary

Totals plus every row, converted to the home currency with the user's rates
— the same numbers the web month view shows. Rows whose currency has no rate
are marked `skipped: true` and left out of the totals (never assumed 1:1).

```bash
curl -H "Authorization: Bearer $KURA_TOKEN" \
  "$KURA_URL/api/v1/months/2026/9"
```

```json
{
  "month": {
    "year": 2026, "month": 9, "home_currency": "BRL",
    "income_cents": 500000,
    "subscriptions_cents": 2000,
    "expenses_cents": 3000,
    "leftover_cents": 495000,
    "missing_rate_currencies": [],
    "salary_missing": false,
    "subscriptions": [ { "id": 7, "title": "Music", "amount_cents": 1990, "home_cents": 1990, "skipped": false } ],
    "expenses": [ { "id": 42, "title": "Lunch", "category": "food", "spent_on": "2026-09-19", "amount_cents": 2550, "home_cents": 2550, "skipped": false } ],
    "payment_days": [ { "id": 3, "title": "Water", "due_day": 10, "due_on": "2026-09-10", "overdue": false, "due_today": false } ]
  }
}
```

## Agent setup snippet

Give the agent three values: the base URL, the token, and these rules:

```text
You manage my TansuSpend at https://spend.gettansu.com via its JSON API.
Authenticate every request with: Authorization: Bearer <token>
- Month overview: GET /api/v1/months/YYYY/M (totals + rows in home cents)
- Expenses: GET /api/v1/expenses?month=YYYY-MM&category=<name>;
  POST /api/v1/expenses with {"expense":{"title","amount_cents","currency","spent_on":"YYYY-MM-DD","category"}};
  PATCH /api/v1/expenses/:id; DELETE /api/v1/expenses/:id
- Subscriptions: same shape under /api/v1/subscriptions with {"subscription":{"title","amount_cents","currency","interval":"monthly|yearly","active","notes"}}. No due_day or billing_month. Yearly amounts count as one twelfth each month.
- Payment days: same shape under /api/v1/payment_days with {"payment_day":{"title","due_day"}}
Prefer amount_cents (integer) over amount. Categories: food, transport, home,
health, leisure, other. On 422, read the "errors" array and fix the input.
```

One token per app: TansuCalendar, TansuNotes, and the others each have their own.
