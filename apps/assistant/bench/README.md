# Tansu Assistant bench

A small, reproducible harness that compares chat models on **billed cost**
(tokens and latency as supporting signals) — the numbers behind Tansu Assistant's
"obvious cost" claim. It is a dev tool, not app code: nothing here is
loaded by the Rails app.

It deliberately scores **no quality metric**. `--report` also emits blind
output sheets so a human can sanity-check that cheap answers are
acceptable — but those never collapse into a number.

## Official suite

Grok models via the xAI API, all on low reasoning effort (effort is pinned
so the bench isolates *model* differences; Tansu Assistant itself defaults to
medium effort on web turns):

- `grok-build-0.1`, `grok-4.3`, `grok-4.20-0309-reasoning`,
  `grok-4.20-multi-agent-0309`, `grok-4.20-0309-non-reasoning`,
  `grok-4.5`, `grok-4.6`, `grok-4.7`

16 scenarios: 3 web-search (incl. a greeting-with-search-on skip check),
3 reasoning, 3 writing (one in PT), 2 code, 2 explainers, 1 vision turn,
and a 2-turn shared-context cache probe. Cost is the API's own billed
figure (xAI `cost_in_usd_ticks`, OpenRouter generation cost), never an
estimate — except as a labeled fallback when a generation record is late.

## Usage

```bash
cd bench
export XAI_API_KEY=xai-...            # grok set
export OPENROUTER_API_KEY=sk-or-...   # only for --set or|all

python3 run.py --smoke               # offline self-check, no API calls
python3 run.py --list                # show corpus + arms
python3 run.py --set grok            # official suite, low effort (~$0.50, ~20 min)
python3 run.py --set grok --effort medium   # same suite, medium effort
python3 run.py --set grok --effort high     # same suite, high effort
python3 run.py --set or              # Flash comparison (needs OR key)
python3 run.py --set grok search-news vision-dot --into results/<file>.json  # re-run prompts
python3 run.py --report results/<file>.json   # aggregates + blind sheet
```

Note: `grok-build-0.1`, `grok-4.20-0309-reasoning`, and
`grok-4.20-0309-non-reasoning` reject the `reasoningEffort` parameter
(verified 2026-09-17), so `--effort` is a no-op for them — the harness
retries without it and records `effort_param: false`.
`grok-4.20-multi-agent-0309` does accept it. Tansu Assistant itself omits the
parameter for the rejecting models (see `Xai::Client::NO_EFFORT_PREFIXES`).

Options: `--cap USD` (default 2.00, hard stop), `--out FILE`,
`--add-arm xai:<model>` / `--add-arm or:<slug>` for extra arms.

`--report` writes `<file>.md` (aggregates) and `<file>.blind.md`
(letter-labeled outputs, shuffled per scenario). The letter→model map
goes to `<file>.mapping.json` — **keep it uncommitted until rankings
are in**, or the blind is spoiled.

## Keys

Keys come from environment variables only. This folder never reads or
writes key files. Results files contain model outputs and token counts,
never keys — but re-run them yourself before publishing; model behavior
and pricing drift over time.
