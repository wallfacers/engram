#!/usr/bin/env python3
"""023 deterministic-rebuild verification (FR-010 / T013).

Two independent builds of the same input + frozen build config MUST produce
byte-identical samples, split assignment and content digests (100%). This tool
compares two build outputs (candidates.jsonl or train.jsonl) line-by-line and
reports the consistency rate, or emits a build summary for a single output.

Usage:
  # compare two builds of the same input (must be 100%)
  python3 rebuild_check.py --baseline data/processed/train.jsonl \
      --rebuild data/processed/rebuild/train.jsonl

  # summarize one build
  python3 rebuild_check.py --summary data/processed/train.jsonl

Output: per-stage consistency rates; exits non-zero when any stage < 100%.
"""

import argparse
import hashlib
import json
import os
import sys


def load_lines(path):
    rows = []
    with open(path) as f:
        for i, raw in enumerate(f, start=1):
            raw = raw.strip()
            if not raw:
                continue
            try:
                rows.append((i, json.loads(raw)))
            except json.JSONDecodeError as e:
                raise SystemExit(f"{path}:{i}: invalid JSON: {e}")
    return rows


def canonical(obj):
    return json.dumps(obj, sort_keys=True, ensure_ascii=False, separators=(",", ":"))


def sha256hex(s):
    return hashlib.sha256(s.encode()).hexdigest()


def summary_of(rows):
    """Build-wide summary: counts by split/category/source and content digest."""
    n = len(rows)
    split = {}
    cat = {}
    source = {}
    digests = []
    ids = []
    for _, r in rows:
        ids.append(r.get("id"))
        split[r.get("split", "(none)")] = split.get(r.get("split", "(none)"), 0) + 1
        cat[r.get("category", "(none)")] = cat.get(r.get("category", "(none)"), 0) + 1
        src = r.get("data_source", "(none)")
        source[src] = source.get(src, 0) + 1
        digests.append(r.get("content_digest", ""))
    ids_sorted = sorted(ids)
    return {
        "samples": n,
        "split": split,
        "category": cat,
        "data_source": source,
        "id_set_digest": sha256hex("\n".join(ids_sorted)),
        "content_digest_set_digest": sha256hex("\n".join(sorted(digests))),
    }


def compare(baseline, rebuild):
    """Return (ok, report) comparing two builds; None for a skipped stage."""
    a = {r.get("id"): (line, r) for line, r in baseline}
    b = {r.get("id"): (line, r) for line, r in rebuild}
    ids_a, ids_b = set(a), set(b)

    report = {}
    # Stage 1: sample set
    only_a = sorted(ids_a - ids_b)
    only_b = sorted(ids_b - ids_a)
    sample_set_rate = 100.0 if not only_a and not only_b else \
        (len(ids_a & ids_b) / max(1, len(ids_a | ids_b)) * 100.0)
    report["sample_set"] = {
        "rate": round(sample_set_rate, 4),
        "only_baseline": only_a[:20],
        "only_rebuild": only_b[:20],
        "only_baseline_count": len(only_a),
        "only_rebuild_count": len(only_b),
    }

    # Stage 2: split assignment + content digest per shared id
    split_mismatch = []
    digest_mismatch = []
    shared = sorted(ids_a & ids_b)
    for i in shared:
        if a[i][1].get("split") != b[i][1].get("split"):
            split_mismatch.append(i)
        if a[i][1].get("content_digest") != b[i][1].get("content_digest"):
            digest_mismatch.append(i)
    base_n = max(1, len(shared))
    report["split_assignment"] = {
        "rate": round((1 - len(split_mismatch) / base_n) * 100.0, 4),
        "mismatch_count": len(split_mismatch),
        "mismatch": split_mismatch[:20],
    }
    report["content_digest"] = {
        "rate": round((1 - len(digest_mismatch) / base_n) * 100.0, 4),
        "mismatch_count": len(digest_mismatch),
        "mismatch": digest_mismatch[:20],
    }

    # Stage 3: whole-build digest (samples + split + canonical content)
    sa = summary_of(baseline)
    sb = summary_of(rebuild)
    overall = (
        sa["samples"] == sb["samples"]
        and sa["id_set_digest"] == sb["id_set_digest"]
        and sa["content_digest_set_digest"] == sb["content_digest_set_digest"]
    )
    report["overall"] = {
        "consistent": overall,
        "baseline": {"samples": sa["samples"], "id_set_digest": sa["id_set_digest"][:16], "content_digest_set_digest": sa["content_digest_set_digest"][:16]},
        "rebuild": {"samples": sb["samples"], "id_set_digest": sb["id_set_digest"][:16], "content_digest_set_digest": sb["content_digest_set_digest"][:16]},
    }

    ok = (
        report["sample_set"]["rate"] == 100.0
        and report["split_assignment"]["rate"] == 100.0
        and report["content_digest"]["rate"] == 100.0
        and overall
    )
    return ok, report


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--baseline", help="first build output")
    ap.add_argument("--rebuild", help="second build output")
    ap.add_argument("--summary", help="single output to summarize")
    args = ap.parse_args()

    if args.summary:
        rows = load_lines(args.summary)
        print(json.dumps({"summary": summary_of(rows)}, indent=2, ensure_ascii=False))
        return 0
    if not args.baseline or not args.rebuild:
        ap.error("--summary <file> OR (--baseline and --rebuild) is required")
    if os.path.abspath(args.baseline) == os.path.abspath(args.rebuild):
        raise SystemExit("baseline and rebuild must be distinct files (independent builds)")

    ok, report = compare(load_lines(args.baseline), load_lines(args.rebuild))
    print(json.dumps(report, indent=2, ensure_ascii=False))
    if not ok:
        print("rebuild_check: FAIL — consistency below 100% (FR-010 violated)", file=sys.stderr)
        return 1
    print("rebuild_check: OK — sample set, split and content digests 100% consistent")
    return 0


if __name__ == "__main__":
    sys.exit(main())
