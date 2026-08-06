#!/usr/bin/env python3
"""030 US1 evidence-assembly diagnostic (specs/030 US1, quickstart.md).

Consumes run-dir/assembly-diagnose.jsonl emitted by --assembly-diagnose and
reports the evidence-assembly health of the read-side pipeline:
  - chunk_fraction (SC-002: fix 029's ~1% fact-dominated context)
  - total_tokens vs cap (FR-002 exact token accounting)
  - structure distribution (temporal / entity / generic, FR-004)
  - tokens_estimated share (fallback degradation rate, constitution V)

Implementation is filled in by T010. This file is the argparse skeleton from
T001.
"""

import argparse
import json
import statistics
import sys
from pathlib import Path


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_dir", type=Path, help="run dir containing assembly-diagnose.jsonl")
    parser.add_argument("--out", type=Path, default=None, help="write JSON report here (default: stdout)")
    return parser.parse_args(argv)


def _percentile(values, p):
    if not values:
        return 0.0
    ordered = sorted(values)
    idx = min(len(ordered) - 1, max(0, int(round(p / 100.0 * (len(ordered) - 1)))))
    return ordered[idx]


def main(argv=None):
    args = parse_args(argv if argv is not None else sys.argv[1:])
    path = args.run_dir / "assembly-diagnose.jsonl"
    if not path.exists():
        print(f"error: {path} not found", file=sys.stderr)
        return 1
    records = []
    with open(path, encoding="utf-8") as fh:
        for line in fh:
            line = line.strip()
            if not line:
                continue
            records.append(json.loads(line))
    if not records:
        print(f"error: {path} contains no records", file=sys.stderr)
        return 1

    chunk_frac = [r["chunk_fraction"] for r in records if "chunk_fraction" in r]
    total_tokens = [r["total_tokens"] for r in records if "total_tokens" in r]
    caps = [r.get("cap", 0) for r in records]
    over_cap = sum(1 for r in records if r.get("total_tokens", 0) > r.get("cap", 0))
    structures = {}
    for r in records:
        structures[r.get("structure", "unknown")] = structures.get(r.get("structure", "unknown"), 0) + 1
    estimated = sum(1 for r in records if r.get("tokens_estimated"))
    units_count = [len(r.get("units", [])) for r in records]

    report = {
        "questions": len(records),
        "chunk_fraction": {
            "median": _percentile(chunk_frac, 50),
            "mean": statistics.mean(chunk_frac) if chunk_frac else 0.0,
            "p10": _percentile(chunk_frac, 10),
            "p90": _percentile(chunk_frac, 90),
        },
        "total_tokens": {
            "median": _percentile(total_tokens, 50),
            "mean": statistics.mean(total_tokens) if total_tokens else 0.0,
            "max": max(total_tokens) if total_tokens else 0,
            "cap_median": _percentile(caps, 50),
        },
        "over_cap": over_cap,
        "over_cap_share": round(over_cap / len(records), 4) if records else 0.0,
        "structures": structures,
        "tokens_estimated": estimated,
        "tokens_estimated_share": round(estimated / len(records), 4) if records else 0.0,
        "units_per_question": {
            "median": _percentile(units_count, 50),
            "mean": statistics.mean(units_count) if units_count else 0.0,
        },
    }
    if args.out:
        args.out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    else:
        print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
