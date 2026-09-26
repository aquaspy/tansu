"""Aggregates + blind quality sheet for a bench results file.

Usage: python3 run.py --report results/<file>.json
Writes <file>.md (aggregates) and <file>.blind.md (blind sheet).
The letter->model mapping prints ONLY a sealed notice: it is written to
<file>.mapping.json, which must stay out of git until rankings are in.
"""
import json
import os
import random
import sys
from collections import defaultdict

QTEXT = {
    "search-news": "3 biggest AI news stories this week? One line each, with sources.",
    "search-fact": "Current population of São Paulo city per latest data? Number and year.",
    "search-greet": "Hi there! How are you today? (web search was ON)",
    "reason-logic": "Mislabeled Apples/Oranges/Mixed boxes; one fruit pick to fix all labels?",
    "reason-math": "Two trains SP↔Rio (430 km), 90 km/h @8:00 vs 110 km/h @8:30 — when meet?",
    "reason-compare": "SQLite vs PostgreSQL for self-hosted single-user app, <150 words.",
    "write-email-pt": "Email curto em PT: reajuste de 5% na fatura (máx 100 palavras).",
    "write-summary": "Explain prompt caching in two short paragraphs, non-technical.",
    "write-tone": "Rewrite warmer: 'Your request was denied. Refer to the policy document.'",
    "code-ruby": "Ruby: names of adults (18+) sorted, from array of hashes.",
    "code-debug": "Python bug: sum 1..9 divided by 10 — what's wrong?",
    "explain-concept": "VPN vs reverse proxy for a self-hoster?",
    "explain-history": "Why did QWERTY become standard?",
    "vision-dot": "Image: red circle, blue rectangle, text '42 Bananas'. One sentence.",
    "cache-t2": "Follow-up on long 1858-cable context: message count + project leader?",
}
ORDER = list(QTEXT)


def write_report(path):
    rs = json.load(open(path))
    arms = sorted({r["arm"] for r in rs})
    agg = defaultdict(lambda: {"usd": 0.0, "tin": 0, "tout": 0, "trsn": 0,
                               "tcached": 0, "lat": [], "n": 0, "err": 0})
    for r in rs:
        a = agg[r["arm"]]
        a["n"] += 1
        a["usd"] += r.get("billed_usd") or 0
        a["tin"] += r["tokens_in"]
        a["tout"] += r["tokens_out"]
        a["trsn"] += r["tokens_reason"]
        a["tcached"] += r["tokens_cached"]
        a["lat"].append(r["latency_s"])
        if str(r.get("finish", "")).startswith("ERROR"):
            a["err"] += 1
    base = agg[arms[0]]["usd"] or 1e-9
    lines = ["# Bench aggregates", "", f"source: `{os.path.basename(path)}`", "",
             "| Model | Billed | vs first | Output tok | Reasoning | Cached in | Lat med/max |",
             "|---|---|---|---|---|---|---|"]
    for arm in arms:
        a = agg[arm]
        lat = sorted(a["lat"])
        lines.append(
            f"| {arm} | ${a['usd']:.4f} | {a['usd']/base:.1f}x | {a['tout']} "
            f"| {100*a['trsn']/max(a['tout'],1):.0f}% | {a['tcached']} "
            f"| {lat[len(lat)//2]:.0f}s / {lat[-1]:.0f}s |")
    lines += ["", f"errors: {sum(a['err'] for a in agg.values())}"]
    md_path = os.path.splitext(path)[0] + ".md"
    open(md_path, "w").write("\n".join(lines) + "\n")
    print("\n".join(lines[4:]))
    print(f"wrote {md_path}")

    # blind sheet (skip cache-t1: bare context dump)
    texts = {(r["prompt"], r["arm"]): r["text"] for r in rs}
    pids = [p for p in ORDER if any((p, a) in texts for a in arms)]
    random.seed(77)
    mapping = {}
    out = ["# Blind quality ranking sheet", "",
           f"{len(pids)} scenarios × {len(arms)} models (A/B/C/... shuffled per scenario).",
           "Reply with your ranking per scenario, e.g. `search-news: B > A > D > C`.",
           "Skip any you don't care about.", ""]
    letters = "ABCDEFGH"
    for pid in pids:
        order = list(arms)
        random.shuffle(order)
        mapping[pid] = {L: a for L, a in zip(letters, order)}
        out += [f"## {pid}", f"*{QTEXT.get(pid, pid)}*", ""]
        for L, arm in zip(letters, order):
            body = (texts.get((pid, arm)) or "").strip() or "*N/A (model error)*"
            if len(body) > 2200:
                body = body[:2200] + "\n\n*[... truncated for length]*"
            out += [f"### {L}", "", body, ""]
        out += ["---", ""]
    blind_path = os.path.splitext(path)[0] + ".blind.md"
    open(blind_path, "w").write("\n".join(out))
    map_path = os.path.splitext(path)[0] + ".mapping.json"
    json.dump(mapping, open(map_path, "w"), indent=1)
    print(f"wrote {blind_path} (mapping sealed in {os.path.basename(map_path)} — keep uncommitted until ranked)")


if __name__ == "__main__":
    write_report(sys.argv[1])
