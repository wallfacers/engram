#!/usr/bin/env python3
"""023 human review sampling + semantic-sufficiency statistics (FR-009 / T011).

Draws a stratified random sample (by category × split) of at least N training
samples into a review sheet, and later computes the semantic-sufficiency rate
with a 95% Wilson interval. The gate is: rate >= 95% AND Wilson lower bound
>= 90% over >= 200 samples (all samples when the asset is smaller).

Usage:
  # draw the review sheet
  python3 review.py --train data/processed/train.jsonl --out review.csv --n 200 --seed 0

  # score the filled-in sheet (semantic_sufficiency column: pass|fail)
  python3 review.py --results review.csv

Output review.csv: id, category, split, query, gold_answer, candidates (top 5
texts), target_need, target_actions, semantic_sufficiency, notes.
"""

import argparse
import csv
import json
import math
import random
import sys


def wilson_ci(k, n, z=1.96):
    """Two-sided Wilson score interval for k/n."""
    if n == 0:
        return (0.0, 0.0)
    p = k / n
    denom = 1 + z * z / n
    center = (p + z * z / (2 * n)) / denom
    half = z * math.sqrt(p * (1 - p) / n + z * z / (4 * n * n)) / denom
    return (center - half, center + half)


def draw_sample(rows, n, seed):
    """Stratified random sample by (category, split), then fill to n uniformly."""
    rng = random.Random(seed)
    strata = {}
    order = []
    for r in rows:
        key = (r.get("category", "?"), r.get("split", "?"))
        if key not in strata:
            strata[key] = []
            order.append(key)
        strata[key].append(r)
    out = []
    if n <= len(rows):
        per_stratum = max(1, n // len(strata))
        for key in order:
            pool = strata[key]
            k = min(len(pool), per_stratum)
            out.extend(rng.sample(pool, k))
        # top up to n from whatever remains
        remaining = [r for r in rows if id(r) not in {id(x) for x in out}]
        need = n - len(out)
        if need > 0 and remaining:
            out.extend(rng.sample(remaining, min(need, len(remaining))))
    else:
        out = rows  # fewer than n samples → review all (FR-009)
    return out


def sheet_rows(samples):
    rows = []
    for s in samples:
        cands = " || ".join(c.get("text", "") for c in s.get("candidates", [])[:5])
        need = json.dumps(s.get("target", {}).get("need", {}), ensure_ascii=False)
        actions = json.dumps(s.get("target", {}).get("actions", []), ensure_ascii=False)
        rows.append({
            "id": s.get("id", ""), "category": s.get("category", ""),
            "split": s.get("split", ""), "query": s.get("query", ""),
            "gold_answer": s.get("gold_answer", ""), "candidates": cands,
            "target_need": need, "target_actions": actions,
            "semantic_sufficiency": "", "notes": "",
        })
    return rows


def write_sheet(path, rows):
    with open(path, "w", newline="") as f:
        w = csv.DictWriter(f, fieldnames=list(rows[0].keys()))
        w.writeheader()
        w.writerows(rows)


def read_results(path):
    with open(path, newline="") as f:
        return list(csv.DictReader(f))


def summarize(results):
    scored = [r for r in results if r.get("semantic_sufficiency", "").strip().lower() in ("pass", "fail")]
    n = len(scored)
    k = sum(1 for r in scored if r["semantic_sufficiency"].strip().lower() == "pass")
    rate = (k / n) if n else 0.0
    lo, hi = wilson_ci(k, n)
    return {
        "reviewed": len(results),
        "scored": n,
        "pass": k,
        "rate": round(rate, 4),
        "wilson_95_ci": [round(lo, 4), round(hi, 4)],
        "gate_rate_ge_95": rate >= 0.95 if n else False,
        "gate_ci_lb_ge_90": (lo >= 0.90) if n else False,
    }


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--train", help="train.jsonl to sample from")
    ap.add_argument("--out", default="data/processed/review.csv", help="review sheet output")
    ap.add_argument("--n", type=int, default=200, help="target sample size (>= 200)")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--results", help="filled-in review sheet to score")
    args = ap.parse_args()

    if args.results:
        r = summarize(read_results(args.results))
        print(json.dumps(r, indent=2, ensure_ascii=False))
        if r["scored"] < 200:
            print(f"review: WARN only {r['scored']} scored (< 200; FR-009 wants >= 200 unless the asset is smaller)", file=sys.stderr)
        if not (r["gate_rate_ge_95"] and r["gate_ci_lb_ge_90"]):
            print("review: FAIL — semantic-sufficiency gate not met (>=95% and Wilson lower bound >=90%)", file=sys.stderr)
            return 1
        print("review: OK — semantic-sufficiency gate met")
        return 0

    if not args.train:
        ap.error("--train (to sample) or --results (to score) is required")
    if args.n < 200:
        ap.error("--n must be >= 200 (FR-009); set --n to total size when the asset is smaller")
    rows = []
    with open(args.train) as f:
        for line in f:
            line = line.strip()
            if line:
                rows.append(json.loads(line))
    sampled = draw_sample(rows, args.n, args.seed)
    out = sheet_rows(sampled)
    write_sheet(args.out, out)
    print(f"review: wrote {len(out)} rows to {args.out} "
          f"(asset={len(rows)}, categories={sorted({s.get('category') for s in sampled})})", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
