#!/usr/bin/env python3
"""Pair LoCoMo engram and MemOS correctness artifacts for McNemar analysis.

Usage (after downloading the private HF artifact paths):
    python3 scripts/mcnemar.py \
      '009-eval-runs/009-full-A-base/run-*/results-hybrid.jsonl' \
      memos-parity/memos_judged_detail.json

The LoCoMo source repeats 11 `(conv, question)` groups.  To reproduce the
bench's majority aggregator, this script keeps the first occurrence in each
engram run before taking the three-run majority.  Identical MemOS rows are
collapsed; conflicting duplicate MemOS rows and aligned category mismatches
fail closed.
"""

import json
import math
import re
import glob
import sys
from collections import defaultdict


MEMOS_CATEGORY = {
    1: "multi-hop",
    2: "temporal",
    3: "open-domain",
    4: "single-hop",
}
MEMOS_GROUP = re.compile(r"^locomo_exp_user_(\d+)$")


def outcome_key(conv, question):
    return int(conv), str(question)


def mcnemar_statistics(b, c):
    """Return continuity-corrected chi-square and exact two-sided binomial p."""
    discordant = b + c
    if discordant == 0:
        return {"chi_square_cc": 0.0, "p_exact": 1.0}
    chi_square_cc = max(abs(b - c) - 1, 0) ** 2 / discordant
    lower_tail = sum(math.comb(discordant, i) for i in range(min(b, c) + 1))
    p_exact = min(1.0, 2.0 * lower_tail / 2 ** discordant)
    return {"chi_square_cc": chi_square_cc, "p_exact": p_exact}


def load_engrams(paths):
    """Return majority-vote outcomes from one or more engram JSONL runs."""
    votes = defaultdict(list)
    categories = {}
    for path in paths:
        seen = {}
        with open(path, encoding="utf-8") as handle:
            for line in handle:
                if not line.strip():
                    continue
                row = json.loads(line)
                key = outcome_key(row["conv"], row["question"])
                value = bool(row["correct"])
                category = row["category_name"]
                # Match cmd/locomo-bench/stats.go: each run keeps the first
                # occurrence of a duplicated `(conv, question)` pair.
                if key in seen:
                    continue
                seen[key] = value
                prior_category = categories.setdefault(key, category)
                if prior_category != category:
                    raise ValueError("conflicting engram category for %r" % (key,))
        for key, value in seen.items():
            votes[key].append(value)
    return {
        key: {"correct": sum(values) * 2 >= len(values), "category": categories[key]}
        for key, values in votes.items()
    }


def load_memos(path):
    """Return unique MemOS outcomes and count exact duplicate records."""
    with open(path, encoding="utf-8") as handle:
        rows = json.load(handle)
    outcomes = {}
    deduplicated = 0
    for row_number, row in enumerate(rows, start=1):
        match = MEMOS_GROUP.fullmatch(row["group"])
        if not match:
            raise ValueError("unexpected MemOS group at row %d: %r" % (row_number, row["group"]))
        key = outcome_key(match.group(1), row["question"])
        current = {"correct": bool(row["correct"]), "category": MEMOS_CATEGORY[row["cat"]]}
        prior = outcomes.get(key)
        if prior is None:
            outcomes[key] = current
        elif prior != current:
            raise ValueError("conflicting MemOS duplicate for %r" % (key,))
        else:
            deduplicated += 1
    return outcomes, deduplicated


def summarize(pairs):
    """Count correctness and discordant pairs for an iterable of paired outcomes."""
    summary = {"n": 0, "engram_correct": 0, "memos_correct": 0, "b": 0, "c": 0}
    for engram, memos in pairs:
        summary["n"] += 1
        summary["engram_correct"] += engram["correct"]
        summary["memos_correct"] += memos["correct"]
        if engram["correct"] and not memos["correct"]:
            summary["b"] += 1
        elif memos["correct"] and not engram["correct"]:
            summary["c"] += 1
    return summary


def analyze(engram_paths, memos_path):
    """Pair raw artifacts by `(conv, question)` and report overall and categories."""
    engrams = load_engrams(engram_paths)
    memos, deduplicated = load_memos(memos_path)
    keys = sorted(set(engrams) & set(memos))
    for key in keys:
        if engrams[key]["category"] != memos[key]["category"]:
            raise ValueError(
                "category mismatch for %r: engram=%s MemOS=%s"
                % (key, engrams[key]["category"], memos[key]["category"])
            )
    pairs = [(engrams[key], memos[key]) for key in keys]
    categories = {}
    for category in sorted({engram["category"] for engram, _ in pairs}):
        categories[category] = summarize(
            (engram, memos) for engram, memos in pairs if engram["category"] == category
        )
    return {
        "overall": summarize(pairs),
        "categories": categories,
        "deduplicated_memos": deduplicated,
        "engram_unpaired": len(set(engrams) - set(memos)),
        "memos_unpaired": len(set(memos) - set(engrams)),
    }


def format_summary(label, summary):
    """Format one overall or category row for a human-readable report."""
    n = summary["n"]
    statistics = mcnemar_statistics(summary["b"], summary["c"])
    engram_accuracy = 100.0 * summary["engram_correct"] / n
    memos_accuracy = 100.0 * summary["memos_correct"] / n
    return (
        "%s n=%d  engram=%.2f%%(%d/%d)  MemOS=%.2f%%(%d/%d)  "
        "Δ=%+.2fpp  b=%d c=%d  χ²(cc)=%.3f  exact p=%.6f"
        % (
            label,
            n,
            engram_accuracy,
            summary["engram_correct"],
            n,
            memos_accuracy,
            summary["memos_correct"],
            n,
            engram_accuracy - memos_accuracy,
            summary["b"],
            summary["c"],
            statistics["chi_square_cc"],
            statistics["p_exact"],
        )
    )


def main(argv):
    if len(argv) != 3:
        print("usage: mcnemar.py '<engram-results-glob>' <memos_judged_detail.json>", file=sys.stderr)
        return 2
    engram_paths = sorted(glob.glob(argv[1]))
    if not engram_paths:
        print("no engram results matched: %s" % argv[1], file=sys.stderr)
        return 2
    report = analyze(engram_paths, argv[2])
    print("Paired LoCoMo items: %d (MemOS duplicate rows removed: %d; engram-only: %d; MemOS-only: %d)" % (
        report["overall"]["n"],
        report["deduplicated_memos"],
        report["engram_unpaired"],
        report["memos_unpaired"],
    ))
    print(format_summary("OVERALL", report["overall"]))
    for category, summary in report["categories"].items():
        print(format_summary(category, summary))
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))
