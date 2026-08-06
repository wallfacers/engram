#!/usr/bin/env python3
"""030 US3 conditional-consolidation analysis (specs/030 US3, quickstart.md).

Consumes a paired run dir (keep vs consolidate arm) and reports:
  - over-cap question share (budget crossover probe, Retain or Consolidate)
  - consolidation output tokens ≤ cap (contracts/consolidation.md)
  - paired e2e: consolidate arm must not significantly regress vs keep

Implementation is filled in by T023. This file is the argparse skeleton from
T001.
"""

import argparse
import json
import statistics
import sys
from pathlib import Path


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_dir", type=Path, help="paired run dir containing keep/ and cons/ subdirs")
    parser.add_argument("--out", type=Path, default=None, help="write JSON report here (default: stdout)")
    return parser.parse_args(argv)


def _find_paired(run_dir):
    candidates = [run_dir / "paired.json", run_dir / "compare.json"]
    if (run_dir / "cons").exists():
        candidates.append(run_dir / "cons" / "paired.json")
        candidates.append(run_dir / "cons" / "compare.json")
    if (run_dir / "keep").exists():
        candidates.append(run_dir / "keep" / "compare.json")
        candidates.append(run_dir / "keep" / "paired.json")
    for p in candidates:
        if p.exists():
            return p
    return None


def main(argv=None):
    args = parse_args(argv if argv is not None else sys.argv[1:])
    if not args.run_dir.exists():
        print(f"error: {args.run_dir} not found", file=sys.stderr)
        return 1

    report = {"run_dir": str(args.run_dir)}

    # Over-cap share comes from the assembly audit in the keep arm (or cons arm).
    asm_glob = list(args.run_dir.glob("assembly-diagnose.jsonl"))
    asm_glob += list(args.run_dir.glob("**/assembly-diagnose.jsonl"))
    if asm_glob:
        records = []
        for path in sorted(set(asm_glob)):
            with open(path, encoding="utf-8") as fh:
                for line in fh:
                    line = line.strip()
                    if line:
                        records.append(json.loads(line))
        if records:
            over_cap = sum(1 for r in records if r.get("total_tokens", 0) > r.get("cap", 0))
            report["assembly"] = {
                "questions": len(records),
                "over_cap": over_cap,
                "over_cap_share": round(over_cap / len(records), 4),
                "total_tokens_mean": statistics.mean(r.get("total_tokens", 0) for r in records),
            }

    paired_path = _find_paired(args.run_dir)
    if paired_path:
        paired = json.loads(paired_path.read_text(encoding="utf-8"))
        report["paired"] = {
            "verdict": paired.get("verdict"),
            "flips_a_to_b": paired.get("flips_a_to_b"),
            "flips_b_to_a": paired.get("flips_b_to_a"),
            "mcnemar_p": paired.get("mcnemar_p"),
        }
    else:
        report["paired"] = None

    if args.out:
        args.out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    else:
        print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
