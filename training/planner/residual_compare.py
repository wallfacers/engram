#!/usr/bin/env python3
"""T003 residual cohort comparison: G (fixed-gold oracle) vs D (B1 deterministic).

computes  compiler-eligible residual = { q : q ∈ G and q ∉ D }
(G = questions the oracle answered correctly with all gold evidence,
 D = questions the deterministic B1 pipeline answered correctly).

Usage:
  python3 residual_compare.py \
      --b1-classification <run-dir>/classification.jsonl \
      --oracle-artifact <run-dir>/fixed_gold_oracle.jsonl \
      --out <run-dir>/residual_cohort.json [--verbose]

Both inputs are 022.v1 artifacts produced by cmd/locomo-bench.
"""

import argparse
import json
import sys


def load_b1_correct(classification_path):
    """B1 D set: question_id -> majority_correct for VALID classifications."""
    d = {}
    with open(classification_path) as f:
        for line in f:
            rec = json.loads(line)
            qid = rec.get("question_id")
            if not qid:
                continue
            # Only valid classifications define D: invalid questions have no
            # trustworthy deterministic answer.
            if rec.get("valid"):
                d[qid] = bool(rec.get("majority_correct"))
    return d


def load_oracle_correct(oracle_path):
    """Oracle G set: question_id -> majority_correct for valid diagnostics.

    Invalid diagnostics (answer_failed, gold_evidence_unresolved, ...) and
    skipped empty-evidence questions are excluded from G.
    """
    g = {}
    with open(oracle_path) as f:
        for line in f:
            rec = json.loads(line)
            qid = rec.get("question_id")
            if not qid:
                continue
            if not rec.get("valid"):
                continue
            outcome = rec.get("oracle_diagnostic") or {}
            g[qid] = bool(outcome.get("majority_correct"))
    return g


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--b1-classification", required=True)
    ap.add_argument("--oracle-artifact", required=True)
    ap.add_argument("--out", required=True)
    ap.add_argument("--verbose", action="store_true")
    args = ap.parse_args()

    d = load_b1_correct(args.b1_classification)
    g = load_oracle_correct(args.oracle_artifact)
    print(f"D (valid B1 classifications): {len(d)}")
    print(f"G (valid oracle diagnostics): {len(g)}", flush=True)

    # Denominator = oracle-resolvable AND B1-evaluable questions.
    common = [q for q in g if q in d]
    print(f"common (both sides present): {len(common)}", flush=True)

    # residual = oracle-correct AND NOT deterministic-correct.
    residual = [q for q in common if g[q] and not d[q]]
    # oracle-only-correct & determinist-correct-but-oracle-wrong for context.
    both = [q for q in common if g[q] and d[q]]
    d_only = [q for q in common if not g[q] and d[q]]

    print(f"oracle AND deterministic correct (G∩D): {len(both)}")
    print(f"deterministic only, oracle wrong: {len(d_only)}")
    print(f"*** residual |G∖D| = {len(residual)} ***", flush=True)

    # Category distribution of residual.
    by_cat = {}
    with open(args.b1_classification) as f:
        cat_of = {json.loads(l)["question_id"]: json.loads(l).get("category") for l in f}
    for q in residual:
        c = cat_of.get(q, "unknown")
        by_cat[c] = by_cat.get(c, 0) + 1

    cohort = {
        "schema": "023.v1",
        "stage": "primary_cohort_residual",
        "b1_classification": args.b1_classification,
        "oracle_artifact": args.oracle_artifact,
        "denominator_common": len(common),
        "g_oracle_correct": sum(1 for q in common if g[q]),
        "d_deterministic_correct": sum(1 for q in common if d[q]),
        "residual_count": len(residual),
        "residual_questions": sorted(residual),
        "residual_by_category": by_cat,
        "verdict": "NOT_NEEDED" if len(residual) == 0 else "READY",
    }
    with open(args.out, "w") as f:
        json.dump(cohort, f, indent=2)
    print(f"wrote {args.out}")
    print(f"verdict: {cohort['verdict']} (residual {'empty → NOT_NEEDED' if len(residual)==0 else 'non-empty → READY'})")

    if args.verbose:
        print("\nresidual questions:")
        for q in sorted(residual):
            print(f"  {q}")
        print(f"\ncategory distribution: {by_cat}")


if __name__ == "__main__":
    sys.exit(main())
