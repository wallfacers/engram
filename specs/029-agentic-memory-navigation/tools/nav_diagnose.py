#!/usr/bin/env python3
"""029 US1 zero-cost navigation rescue-space diagnostic (specs/029 US1,
SC-001). Consumes the harness-produced per-question retrieval diagnosis
(run-dir/nav-diagnose.jsonl, emitted by `locomo-bench --nav-diagnose`) and
classifies each paired question into one of:

  gold_unresolved - the question carries no parseable gold-turn reference
  in_pool         - the gold turns are covered by chunks that exist in the store
  topk_hit        - a single-shot top-k retrieval already surfaced the gold
  rescueable      - single-shot missed it, but a simulated multi-step action
                    (rewrite / follow_entity / deep wide-pool) can rescue it
  not_in_pool     - the gold is simply not retrievable; navigation cannot help

Verification gate (SC-001): rescueable share >= 20% of the paired subset →
GO to US2; otherwise record a negative verdict and STOP. Attribution of rescue
mechanism is deterministic (rewrite > follow_entity > deep, matching the Go
simulation's priority order).

Pure offline; no answerer/judge/embedding calls.
"""
import argparse
import json
import sys
from collections import Counter

CLASS_ORDER = ["in_pool", "topk_hit", "rescueable", "not_in_pool", "gold_unresolved"]


def classify(rec):
    """One deterministic three-way classification per record. Returns
    (class, rescue_mechanism_or_None)."""
    gold_resolved = bool(rec.get("gold_resolved"))
    if not gold_resolved:
        return "gold_unresolved", None
    in_pool = bool(rec.get("in_pool"))
    single = rec.get("single_topk") or {}
    if single.get("gold_hit"):
        return "topk_hit", None
    if not in_pool:
        return "not_in_pool", None
    # rescueable: any simulated mechanism surfaced the gold.
    for sim in rec.get("simulated") or []:
        if sim.get("gold_hit"):
            return "rescueable", sim.get("action", "unknown")
    wide = rec.get("wide_pool") or {}
    if wide.get("gold_hit"):
        return "rescueable", "deep"
    return "rescueable", None  # in pool but no simulated mechanism caught it


def parse_args(argv):
    p = argparse.ArgumentParser(
        description="029 US1 navigation rescue-space classification (offline)"
    )
    p.add_argument("--hits-jsonl", required=True,
                   help="path to run-dir/nav-diagnose.jsonl emitted by locomo-bench --nav-diagnose")
    p.add_argument("--questions", default=None,
                   help="optional question-id whitelist (phase0-ids.txt, one conv-N-q-M per line; # = comment)")
    p.add_argument("--out", default="diagnosis-report.json", help="output JSON path")
    # Compatibility with the quickstart US1 command shape; kept optional because
    # the retrieval itself is produced by the harness, not this script.
    p.add_argument("--store-dir", default=None, help="(informational) chunk store directory")
    p.add_argument("--data", default=None, help="(informational) locomo.json path")
    p.add_argument("--top-k", type=int, default=30, help="(informational) single-shot budget")
    return p.parse_args(argv)


def load_whitelist(path):
    ids = set()
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            ids.add(line)
    return ids


def main(argv=None):
    args = parse_args(argv if argv is not None else sys.argv[1:])
    whitelist = load_whitelist(args.questions) if args.questions else None

    records = []
    with open(args.hits_jsonl) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            rec = json.loads(line)
            if whitelist and rec.get("question_id") not in whitelist:
                continue
            records.append(rec)

    if not records:
        print(f"[nav_diagnose] no records in {args.hits_jsonl}", file=sys.stderr)
        return 1

    counts = Counter()
    mechanisms = Counter()
    per_question = {}
    for rec in records:
        cls, mech = classify(rec)
        counts[cls] += 1
        if cls == "rescueable" and mech:
            mechanisms[mech] += 1
        per_question[rec.get("question_id") or f"conv-{rec.get('conv')}-q-{rec.get('q')}"] = cls

    total = len(records)
    rescueable = counts.get("rescueable", 0)
    denominator = max(1, total - counts.get("gold_unresolved", 0))
    rescueable_share = rescueable / denominator
    gate_pass = rescueable_share >= 0.20

    # Attribution distribution over the rescueable subset.
    mechanism_share = {k: mechanisms.get(k, 0) / max(1, rescueable)
                       for k in ("rewrite", "follow_entity", "deep")}
    mechanisms["unattributed"] = rescueable - sum(mechanisms.values())

    report = {
        "input": {"hits_jsonl": args.hits_jsonl, "questions": total,
                  "whitelist": bool(whitelist)},
        "class_counts": {k: counts.get(k, 0) for k in CLASS_ORDER},
        "rescueable_share": round(rescueable_share, 4),
        "rescue_mechanisms": dict(mechanisms),
        "rescue_mechanism_share": mechanism_share,
        "verdict_gate": {
            "threshold": 0.20,
            "rescueable_share": round(rescueable_share, 4),
            "pass": gate_pass,
        },
        "per_question": per_question,
        "sample_rescueable": [
            {"question_id": rec.get("question_id"), "question": rec.get("question"),
             "single_gold_rank": (rec.get("single_topk") or {}).get("gold_rank"),
             "wide_gold_rank": (rec.get("wide_pool") or {}).get("gold_rank"),
             "simulated": [s.get("action") for s in (rec.get("simulated") or [])]}
            for rec in records if classify(rec)[0] == "rescueable"
        ][:20],
    }
    with open(args.out, "w") as f:
        json.dump(report, f, indent=2)

    print(f"[nav_diagnose] n={total} rescueable_share={rescueable_share:.3f} "
          f"(gate {'PASS >= 0.20' if gate_pass else 'FAIL < 0.20'})")
    for k in CLASS_ORDER:
        print(f"  {k:16s} {counts.get(k, 0):4d}")
    print(f"  mechanisms: {dict(mechanisms)}")
    print(f"  wrote {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
