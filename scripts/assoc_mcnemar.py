#!/usr/bin/env python3
"""Paired exact McNemar between two engram LoCoMo runs (each a 3-run majority).

Measures --assoc (hybrid+assoc) vs the flat hybrid baseline at matched
top-k / chunk-quota on the 009-bge-chunks-store, isolating the graph-traversal
contribution under a fixed context budget.

Usage:
    python3 scripts/assoc_mcnemar.py \
      '<assoc-run-dir>/run-*/results-hybrid.jsonl' \
      '<baseline-run-dir>/run-*/results-hybrid.jsonl' [labelA labelB]
"""
import glob
import os
import sys
from collections import defaultdict

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from mcnemar import load_engrams, mcnemar_statistics  # noqa: E402


def summarize(pairs):
    s = defaultdict(int)
    for a, b in pairs:
        s["n"] += 1
        s["a_correct"] += a["correct"]
        s["b_correct"] += b["correct"]
        if a["correct"] and not b["correct"]:
            s["b_disc"] += 1  # A right, B wrong
        elif b["correct"] and not a["correct"]:
            s["c_disc"] += 1  # B right, A wrong
    return s


def main(argv):
    if len(argv) < 3:
        print("usage: assoc_mcnemar.py '<a-glob>' '<b-glob>' [labelA labelB]", file=sys.stderr)
        return 2
    a_paths = sorted(glob.glob(argv[1]))
    b_paths = sorted(glob.glob(argv[2]))
    label_a = argv[3] if len(argv) > 3 else "assoc"
    label_b = argv[4] if len(argv) > 4 else "baseline"
    if not a_paths or not b_paths:
        print("no matches: a=%s b=%s" % (a_paths, b_paths), file=sys.stderr)
        return 2
    a = load_engrams(a_paths)
    b = load_engrams(b_paths)
    keys = sorted(set(a) & set(b))
    for k in keys:
        if a[k]["category"] != b[k]["category"]:
            raise ValueError("category mismatch %r: %s vs %s" % (k, a[k]["category"], b[k]["category"]))
    pairs = [(a[k], b[k]) for k in keys]
    cats = {}
    for cat in sorted({p[0]["category"] for p in pairs}):
        cats[cat] = summarize([p for p in pairs if p[0]["category"] == cat])
    overall = summarize(pairs)

    def fmt(label, s):
        n = s["n"]
        st = mcnemar_statistics(s["b_disc"], s["c_disc"])
        acc_a = 100.0 * s["a_correct"] / n
        acc_b = 100.0 * s["b_correct"] / n
        return ("%-12s n=%4d  %s=%6.2f%%  %s=%6.2f%%  Δ=%+5.2fpp  "
                "b=%d c=%d  χ²(cc)=%.3f  exact p=%.6f" % (
                    label, n, label_a, acc_a, label_b, acc_b, acc_a - acc_b,
                    s["b_disc"], s["c_disc"], st["chi_square_cc"], st["p_exact"]))

    print("Paired items: %d  (%s-only:%d  %s-only:%d)" % (
        overall["n"], label_a, len(set(a) - set(b)), label_b, len(set(b) - set(a))))
    print(fmt("OVERALL", overall))
    for cat, s in cats.items():
        print(fmt(cat, s))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
