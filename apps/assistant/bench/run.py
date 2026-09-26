"""KuraChat bench: compare chat models on billed cost (tokens/latency supporting).

No quality scoring; blind sheets are for human sanity checks only.
Official suite: Grok models. OpenRouter arms optional.

Cost oracle: xAI cost_in_usd_ticks; OpenRouter /generation total_cost
(fallback: computed from /models pricing x tokens). Hard spend cap enforced.
"""
import base64
import datetime
import json
import os
import random
import sys
import time
import urllib.request

BASE = os.path.dirname(os.path.abspath(__file__))
RESULTS_DIR = os.path.join(BASE, "results")
SPEND_CAP_USD = 2.00
TICKS_PER_USD = 10_000_000_000.0

ARM_SETS = {
    "grok": [
        {"id": "grok-build", "kind": "xai", "model": "grok-build-0.1"},
        {"id": "grok43", "kind": "xai", "model": "grok-4.3"},
        {"id": "grok420", "kind": "xai", "model": "grok-4.20-0309-reasoning"},
        {"id": "grok420m", "kind": "xai", "model": "grok-4.20-multi-agent-0309"},
        {"id": "grok420n", "kind": "xai", "model": "grok-4.20-0309-non-reasoning"},
        {"id": "grok45", "kind": "xai", "model": "grok-4.5"},
        {"id": "grok46", "kind": "xai", "model": "grok-4.6"},
        {"id": "grok47", "kind": "xai", "model": "grok-4.7"},
    ],
    "or": [
        {"id": "ds41f", "kind": "or", "model": "deepseek/deepseek-v4.1-flash"},
        {"id": "ds0731", "kind": "or", "model": "deepseek/deepseek-v4-flash-0731"},
        {"id": "glm53f", "kind": "or", "model": "z-ai/glm-5.3-flash"},
    ],
}
# Default effort; override with --effort. xAI models that reject the
# parameter (grok-build-0.1, grok-4.20 snapshots) auto-retry without it.
# (KuraChat itself defaults to medium effort on web turns.)
BENCH_XAI_EFFORT = "low"

# (prompt_id, category, search_on, text)
PROMPTS = [
    ("search-news", "search", True,
     "What are the 3 biggest AI industry news stories this week? One line each, with sources."),
    ("search-fact", "search", True,
     "What is the current population of São Paulo city according to the latest available data? Just the number and year."),
    ("search-greet", "search", True, "Hi there! How are you today?"),
    ("reason-logic", "reason", False,
     "Three boxes labeled Apples, Oranges, and Mixed are all mislabeled. You may take one fruit from one box. Which box do you pick from to relabel all three correctly, and why?"),
    ("reason-math", "reason", False,
     "A train leaves São Paulo at 8:00 at 90 km/h toward Rio (430 km away). A second train leaves Rio at 8:30 at 110 km/h toward São Paulo. At what time do they meet? Show the math."),
    ("reason-compare", "reason", False,
     "Compare SQLite vs PostgreSQL for a self-hosted single-user web app. When is each the right call? Keep it under 150 words."),
    ("write-email-pt", "write", False,
     "Escreva um email curto e educado (máximo 100 palavras) para um cliente avisando que a fatura deste mês terá um reajuste de 5% por causa da inflação."),
    ("write-summary", "write", False,
     "Explain prompt caching in LLMs in two short paragraphs for a non-technical reader."),
    ("write-tone", "write", False,
     "Rewrite this to sound warmer without changing the meaning: 'Your request was denied. Refer to the policy document for details.'"),
    ("code-ruby", "code", False,
     "Write a small Ruby method that takes an array of hashes with :name and :age and returns the names of adults (18+) sorted alphabetically. Include a one-line usage example."),
    ("code-debug", "code", False,
     "This Python prints the wrong total. What's the bug and the fix?\n\ntotal = 0\nfor i in range(1, 10):\n    total += i\nprint(total / 10)"),
    ("explain-concept", "explain", False,
     "What is the difference between a VPN and a reverse proxy? When would a self-hoster use each?"),
    ("explain-history", "explain", False,
     "Why did the QWERTY keyboard layout become standard even though it wasn't designed for speed?"),
]

CACHE_CONTEXT = (
    "The Transatlantic Telegraph Cable Project of 1858 was the first attempt to lay a telegraph cable "
    "across the Atlantic Ocean. Cyrus West Field led the effort after years of fundraising on both sides "
    "of the ocean. The cable was laid by HMS Agamemnon and USS Niagara meeting mid-ocean. Queen Victoria "
    "and President James Buchanan exchanged ceremonial messages. The cable failed after only 732 messages "
    "over three weeks, but it proved the concept and a permanent cable followed in 1866. "
) * 40  # ~6k tokens of stable context

CACHE_TURNS = [
    ("cache-t1", "cache", False, CACHE_CONTEXT + "\n\nSummarize the above in exactly two sentences."),
    ("cache-t2", "cache", False, "How many messages did the 1858 cable transmit before failing, and who led the project?"),
]


def load_keys(need):
    keys = {"XAI_API_KEY": os.environ.get("XAI_API_KEY", ""),
            "OPENROUTER_API_KEY": os.environ.get("OPENROUTER_API_KEY", "")}
    missing = [k for k in need if not keys.get(k)]
    if missing:
        raise SystemExit(f"missing env keys: {', '.join(missing)} (export them; never commit keys)")
    return keys


KEYS = {}
OR_PRICING = {}  # model -> {prompt, completion, input_cache_read} per token


def post(url, headers, body):
    req = urllib.request.Request(
        url, data=json.dumps(body).encode(),
        headers={**headers, "Content-Type": "application/json"})
    t0 = time.monotonic()
    with urllib.request.urlopen(req, timeout=300) as r:
        raw = r.read().decode()
    return json.loads(raw), time.monotonic() - t0


def get(url, headers):
    req = urllib.request.Request(url, headers=headers)
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.loads(r.read().decode())


def load_or_pricing():
    data = get("https://openrouter.ai/api/v1/models", {})["data"]
    for m in data:
        p = m.get("pricing", {})
        try:
            OR_PRICING[m["id"]] = {
                "prompt": float(p.get("prompt") or 0),
                "completion": float(p.get("completion") or 0),
                "cached": float(p.get("input_cache_read") or 0),
            }
        except (TypeError, ValueError):
            pass


def xai_output_text(resp):
    parts = []
    for item in resp.get("output", []) or []:
        if not isinstance(item, dict):
            continue
        for c in item.get("content", []) or []:
            if isinstance(c, dict) and c.get("text"):
                parts.append(c["text"])
    return "".join(parts)


def xai_search_count(resp):
    usage = resp.get("usage", {}) or {}
    ssu = usage.get("server_side_tool_usage")
    if isinstance(ssu, dict):
        n = sum(v for k, v in ssu.items() if "web_search" in str(k).lower())
        if n:
            return int(n)
    return sum(1 for item in resp.get("output", []) or []
               if isinstance(item, dict) and "web_search" in str(item.get("type", "")))


def err_detail(e):
    try:
        body = e.read().decode() if hasattr(e, "read") else ""
    except Exception:  # noqa: BLE001
        body = ""
    return f"{type(e).__name__}: {str(e)[:120]} | {body[:300]}"


def call_xai(model, messages, search_on, cache_key=None):
    body = {
        "model": model,
        "input": messages,
        "stream": False,
        "store": False,
        "reasoning_effort": BENCH_XAI_EFFORT,
    }
    if search_on:
        body["tools"] = [{"type": "web_search"}]
    if cache_key:
        body["prompt_cache_key"] = cache_key
    try:
        resp, dt = post("https://api.x.ai/v1/responses",
                        {"Authorization": f"Bearer {KEYS['XAI_API_KEY']}"}, body)
    except urllib.error.HTTPError as e:
        detail = ""
        try:
            detail = e.read().decode()
        except Exception:  # noqa: BLE001
            pass
        if "reasoningEffort" in detail and "does not support" in detail:
            del body["reasoning_effort"]  # e.g. grok-build, grok-4.20 snapshots
            resp, dt = post("https://api.x.ai/v1/responses",
                            {"Authorization": f"Bearer {KEYS['XAI_API_KEY']}"}, body)
            resp["_no_effort_param"] = True
        else:
            raise
    usage = resp.get("usage", {}) or {}
    ind = usage.get("input_tokens_details", {}) or {}
    outd = usage.get("output_tokens_details", {}) or {}
    ticks = usage.get("cost_in_usd_ticks")
    return {
        "text": xai_output_text(resp),
        "latency_s": round(dt, 2),
        "tokens_in": usage.get("input_tokens", 0) or 0,
        "tokens_out": usage.get("output_tokens", 0) or 0,
        "tokens_reason": outd.get("reasoning_tokens", 0) or 0,
        "tokens_cached": ind.get("cached_tokens", 0) or 0,
        "searches": xai_search_count(resp),
        "annotations": 0,
        "provider": "xai",
        "model_routed": resp.get("model", model),
        "billed_usd": (ticks / TICKS_PER_USD) if ticks is not None else None,
        "billed_src": "ticks" if ticks is not None else None,
        "finish": "ok",
        "effort_param": not resp.pop("_no_effort_param", False),
    }


def or_generation_cost(gen_id):
    for _ in range(4):
        try:
            g = get(f"https://openrouter.ai/api/v1/generation?id={gen_id}",
                    {"Authorization": f"Bearer {KEYS['OPENROUTER_API_KEY']}"})["data"]
            if g.get("total_cost") is not None:
                return float(g["total_cost"])
        except Exception:  # noqa: BLE001
            pass
        time.sleep(3)
    return None


def call_or(model, messages, search_on):
    body = {"model": model, "messages": messages, "stream": False}
    if search_on:
        body["tools"] = [{"type": "openrouter:web_search"}]
    resp, dt = post("https://openrouter.ai/api/v1/chat/completions",
                    {"Authorization": f"Bearer {KEYS['OPENROUTER_API_KEY']}",
                     "HTTP-Referer": "https://localhost/kurachat-eval",
                     "X-Title": "KuraChat eval"}, body)
    gen_id = resp.get("id", "")
    if resp.get("error"):
        raise RuntimeError(f"OR error: {resp['error'].get('message', resp['error'])}")
    choice = (resp.get("choices") or [{}])[0]
    msg = choice.get("message", {}) or {}
    usage = resp.get("usage", {}) or {}
    ctd = usage.get("completion_tokens_details", {}) or {}
    ptd = usage.get("prompt_tokens_details", {}) or {}
    anns = msg.get("annotations", []) or []
    cost = usage.get("cost") or None
    src = "usage" if cost else None
    if not cost:
        cost = or_generation_cost(gen_id)
        src = "generation" if cost else None
    if not cost:
        pr = OR_PRICING.get(model, {})
        cached = ptd.get("cached_tokens", 0) or 0
        cost = ((usage.get("prompt_tokens", 0) or 0) - cached) * pr.get("prompt", 0) \
            + cached * pr.get("cached", 0) \
            + (usage.get("completion_tokens", 0) or 0) * pr.get("completion", 0)
        src = "computed+deferred"
    return {
        "text": msg.get("content", "") or "",
        "latency_s": round(dt, 2),
        "tokens_in": usage.get("prompt_tokens", 0) or 0,
        "tokens_out": usage.get("completion_tokens", 0) or 0,
        "tokens_reason": ctd.get("reasoning_tokens", 0) or 0,
        "tokens_cached": ptd.get("cached_tokens", 0) or 0,
        "searches": None,  # server-side; see annotations + cost delta
        "annotations": sum(1 for a in anns if isinstance(a, dict) and a.get("type") == "url_citation"),
        "provider": resp.get("provider", "?"),
        "model_routed": resp.get("model", model),
        "billed_usd": cost,
        "billed_src": src,
        "finish": choice.get("finish_reason", "?"),
        "gen_id": gen_id,
    }


def settle_deferred(results):
    """Re-query generation costs that settled late; keep token-computed floor."""
    for r in results:
        if r.get("billed_src") != "computed+deferred" or not r.get("gen_id"):
            continue
        try:
            g = get(f"https://openrouter.ai/api/v1/generation?id={r['gen_id']}",
                    {"Authorization": f"Bearer {KEYS['OPENROUTER_API_KEY']}"})["data"]
            if g.get("total_cost"):
                r["billed_usd"] = float(g["total_cost"])
                r["billed_src"] = "generation-settled"
                print(f"settled {r['prompt']:14s} {r['arm']:7s} ${r['billed_usd']:.5f}")
        except Exception:  # noqa: BLE001
            pass


def to_xai_input(role, content):
    return {"role": role, "content": content}


def run_call(arm, prompt_id, category, search_on, user_text, history, image_b64=None):
    """history: list of (role, text) prior turns (for cache probe)."""
    if arm["kind"] == "xai":
        msgs = [to_xai_input(r, t) for r, t in history]
        if image_b64:
            msgs.append({"role": "user", "content": [
                {"type": "input_text", "text": user_text},
                {"type": "input_image", "image_url": f"data:image/png;base64,{image_b64}",
                 "detail": "high"},
            ]})
        else:
            msgs.append(to_xai_input("user", user_text))
        ck = f"eval-{prompt_id.split('-')[0]}-{arm['id']}" if category == "cache" else None
        return call_xai(arm["model"], msgs, search_on, cache_key=ck)
    msgs = [{"role": r, "content": t} for r, t in history]
    if image_b64:
        msgs.append({"role": "user", "content": [
            {"type": "text", "text": user_text},
            {"type": "image_url", "image_url": {"url": f"data:image/png;base64,{image_b64}"}},
        ]})
    else:
        msgs.append({"role": "user", "content": user_text})
    return call_or(arm["model"], msgs, search_on)


def parse_cli(argv):
    o = {"set": "grok", "out": None, "into": None, "cap": SPEND_CAP_USD,
         "smoke": False, "list": False, "report": None, "add_arm": [], "arms_only": None, "effort": "low", "only": []}
    i = 0
    while i < len(argv):
        a = argv[i]
        if a == "--set":
            i += 1; o["set"] = argv[i]
        elif a == "--out":
            i += 1; o["out"] = argv[i]
        elif a == "--into":
            i += 1; o["into"] = argv[i]
        elif a == "--cap":
            i += 1; o["cap"] = float(argv[i])
        elif a == "--add-arm":
            i += 1; o["add_arm"].append(argv[i])
        elif a == "--arms":
            i += 1; o["arms_only"] = argv[i].split(",")
        elif a == "--effort":
            i += 1; o["effort"] = argv[i]
        elif a == "--smoke":
            o["smoke"] = True
        elif a == "--list":
            o["list"] = True
        elif a == "--report":
            i += 1; o["report"] = argv[i]
        elif a.startswith("-"):
            raise SystemExit(f"unknown flag {a}")
        else:
            o["only"].append(a)
        i += 1
    return o


def cmd_smoke(arms):
    pids = [p[0] for p in PROMPTS] + ["vision-dot", "cache-t1", "cache-t2"]
    assert len(pids) == len(set(pids)), "duplicate prompt ids"
    assert all(p[1] in ("search", "reason", "write", "code", "explain") for p in PROMPTS)
    assert os.path.isfile(os.path.join(BASE, "vision.png")), "vision.png missing"
    assert arms, "empty arm set"
    print(f"smoke OK: {len(pids)} prompts, {len(arms)} arms ({', '.join(a['id'] for a in arms)})")


def cmd_list(arms):
    for pid, cat, so, txt in PROMPTS:
        print(f"{pid:15s} {cat:7s} search={'on' if so else 'off'}  {txt[:60]}")
    print(f"{'vision-dot':15s} vision  search=off  What is in this image? [...]")
    print(f"{'cache-t1/t2':15s} cache   search=off  2-turn shared-context probe")
    print(f"arms: {', '.join(a['id'] + '=' + a['model'] for a in arms)}")


def cmd_report(path):
    from report import write_report
    write_report(path)


def select_arms(opts):
    if opts["set"] == "all":
        arms = ARM_SETS["grok"] + ARM_SETS["or"]
    else:
        arms = list(ARM_SETS.get(opts["set"], []))
    if not arms:
        raise SystemExit(f"unknown --set {opts['set']} (grok|or|all)")
    if opts["arms_only"]:
        arms = [a for a in arms if a["id"] in opts["arms_only"]]
    for spec in opts["add_arm"]:
        kind, _, model = spec.partition(":")
        if kind not in ("xai", "or") or not model:
            raise SystemExit(f"--add-arm expects kind:model, got {spec!r}")
        arms.append({"id": model.split("/")[-1].replace(".", ""), "kind": kind, "model": model})
    return arms


def main():
    global KEYS, BENCH_XAI_EFFORT
    opts = parse_cli(sys.argv[1:])
    if opts["effort"] not in ("low", "medium", "high", "xhigh", "none"):
        raise SystemExit(f"unknown --effort {opts['effort']}")
    BENCH_XAI_EFFORT = opts["effort"]
    arms = select_arms(opts)
    if opts["smoke"]:
        return cmd_smoke(arms)
    if opts["list"]:
        return cmd_list(arms)
    if opts["report"]:
        return cmd_report(opts["report"])
    only = opts["only"]
    cap = opts["cap"]
    need = set()
    if any(a["kind"] == "xai" for a in arms):
        need.add("XAI_API_KEY")
    if any(a["kind"] == "or" for a in arms):
        need.add("OPENROUTER_API_KEY")
    KEYS = load_keys(need)
    if any(a["kind"] == "or" for a in arms):
        load_or_pricing()
    with open(os.path.join(BASE, "vision.png"), "rb") as f:
        dot_b64 = base64.b64encode(f.read()).decode()

    jobs = [(pid, cat, so, txt, []) for pid, cat, so, txt in PROMPTS]
    jobs.append(("vision-dot", "vision", False, "What is in this image? Reply in one short sentence.", []))
    # cache probe runs as 2 sequential turns per arm (handled below)
    random.seed(20260917)
    results = []
    into = opts["into"]
    if into:
        try:
            with open(into) as f:
                prior = json.load(f)
        except (OSError, ValueError):
            prior = []
        arm_ids = [a["id"] for a in arms]
        if only:
            rerun = set(only)
            if rerun & {"cache-t1", "cache-t2"}:
                rerun |= {"cache-t1", "cache-t2"}
            results = [r for r in prior if r.get("prompt") not in rerun or r.get("arm") not in arm_ids]
        else:
            results = [r for r in prior if r.get("arm") not in arm_ids]
    if only and not into:
        print("note: prompt filter without --into writes a fresh file")
        rerun = set(only) | ({"cache-t1", "cache-t2"} if set(only) & {"cache-t1", "cache-t2"} else set())
        results = [r for r in prior if r.get("prompt") not in rerun]
    spent = sum(r.get("billed_usd") or 0 for r in results)

    def do(pid, cat, so, txt, hist, img=None):
        nonlocal spent
        for arm in arms:
            if spent >= cap:
                print(f"SPEND CAP ${cap:.2f} hit; stopping.")
                return None
            try:
                r = run_call(arm, pid, cat, so, txt, hist, image_b64=img)
            except Exception as e:  # noqa: BLE001
                r = {"text": "", "latency_s": 0, "tokens_in": 0, "tokens_out": 0,
                     "tokens_reason": 0, "tokens_cached": 0, "searches": None,
                     "annotations": 0, "provider": "?", "model_routed": arm["model"],
                     "billed_usd": 0.0, "billed_src": "error", "finish": "ERROR " + err_detail(e)}
            r.update({"arm": arm["id"], "prompt": pid, "category": cat,
                      "search_on": so, "ts_utc": datetime.datetime.now(datetime.timezone.utc).isoformat()})
            spent += r.get("billed_usd") or 0
            results.append(r)
            print(f"{pid:14s} {arm['id']:7s} ${r.get('billed_usd') or 0:.5f} "
                  f"in={r['tokens_in']} out={r['tokens_out']} rsn={r['tokens_reason']} "
                  f"cached={r['tokens_cached']} srch={r['searches']} ann={r['annotations']} "
                  f"t={r['latency_s']}s prov={r['provider']} fin={r['finish']}", flush=True)
            time.sleep(1)
        return True

    for pid, cat, so, txt, hist in jobs:
        if only and pid not in only:
            continue
        if do(pid, cat, so, txt, hist, img=dot_b64 if pid == "vision-dot" else None) is None:
            break

    # cache probe: sequential turns with history per arm
    if not only or "cache-t1" in only or "cache-t2" in only:
        for arm in arms:
            if spent >= cap:
                break
            hist = []
            for pid, cat, so, txt in CACHE_TURNS:
                try:
                    r = run_call(arm, pid, cat, so, txt, hist)
                except Exception as e:  # noqa: BLE001
                    r = {"text": "", "latency_s": 0, "tokens_in": 0, "tokens_out": 0,
                         "tokens_reason": 0, "tokens_cached": 0, "searches": None,
                         "annotations": 0, "provider": "?", "model_routed": arm["model"],
                         "billed_usd": 0.0, "billed_src": "error",
                         "finish": "ERROR " + err_detail(e)}
                r.update({"arm": arm["id"], "prompt": pid, "category": cat,
                          "search_on": so, "ts_utc": datetime.datetime.now(datetime.timezone.utc).isoformat()})
                spent += r.get("billed_usd") or 0
                results.append(r)
                hist.append(("user", txt))
                hist.append(("assistant", r["text"][:4000]))
                print(f"{pid:14s} {arm['id']:7s} ${r.get('billed_usd') or 0:.5f} "
                      f"in={r['tokens_in']} out={r['tokens_out']} cached={r['tokens_cached']} "
                      f"t={r['latency_s']}s fin={r['finish']}", flush=True)
                time.sleep(1)

    settle_deferred(results)
    spent = sum(r.get("billed_usd") or 0 for r in results)
    os.makedirs(RESULTS_DIR, exist_ok=True)
    out = opts["out"] or os.path.join(
        RESULTS_DIR,
        "run-%s-%s-%s.json" % (datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d-%H%M%S"), opts["set"], BENCH_XAI_EFFORT))
    with open(out, "w") as f:
        json.dump(results, f, indent=1)
    print(f"\nDONE calls={len(results)} billed_total=${spent:.5f} cap=${cap:.2f} file={out}")


if __name__ == "__main__":
    main()
