#!/usr/bin/env python3
"""030 US2 grounded-trace mediation analysis (specs/030 US2, quickstart.md).

Consumes a paired run dir (base vs trace arm) plus the trace gate-state journal
and reports:
  - majority correctness + exact McNemar (paired_eval.go parity, 008 discipline)
  - category non-regression (L0-3: temporal / multi-hop must not collapse)
  - fail-closed gate-state distribution (valid / invalid_citation /
    parse_failed / fallback, contracts/grounded-trace.md)

Implementation is filled in by T017. This file is the argparse skeleton from
T001.
"""

import argparse
import json
import statistics
import sys
from pathlib import Path


def parse_args(argv):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("run_dir", type=Path, help="paired run dir containing base/ and trace/ subdirs")
    parser.add_argument("--out", type=Path, default=None, help="write JSON report here (default: stdout)")
    return parser.parse_args(argv)


def _find_paired(run_dir):
    """paired.json may live at run_dir/ or inside an arm subdir."""
    candidates = [run_dir / "paired.json"]
    if (run_dir / "trace").exists():
        candidates.append(run_dir / "trace" / "paired.json")
    for p in candidates:
        if p.exists():
            return p
    return None


def _gate_records(run_dir):
    """Collect gate records from all trace-gate.jsonl files (repeat subdirs)."""
    records = []
    globs = list(run_dir.glob("trace-gate.jsonl"))
    globs += list(run_dir.glob("**/trace-gate.jsonl"))
    for path in sorted(set(globs)):
        with open(path, encoding="utf-8") as fh:
            for line in fh:
                line = line.strip()
                if not line:
                    continue
                records.append(json.loads(line))
    return records


def main(argv=None):
    args = parse_args(argv if argv is not None else sys.argv[1:])
    if not args.run_dir.exists():
        print(f"error: {args.run_dir} not found", file=sys.stderr)
        return 1

    paired_path = _find_paired(args.run_dir)
    report = {"run_dir": str(args.run_dir)}

    if paired_path:
        paired = json.loads(paired_path.read_text(encoding="utf-8"))
        cat_flips = {}
        for q in paired.get("questions", []):
            if q.get("flip"):
                cat = q.get("category", "unknown")
                cat_flips.setdefault(cat, {"a_to_b": 0, "b_to_a": 0})
                if q["flip"] == "a_to_b":
                    cat_flips[cat]["a_to_b"] += 1
                elif q["flip"] == "b_to_a":
                    cat_flips[cat]["b_to_a"] += 1
        report["paired"] = {
            "verdict": paired.get("verdict"),
            "flips_a_to_b": paired.get("flips_a_to_b"),
            "flips_b_to_a": paired.get("flips_b_to_a"),
            "mcnemar_p": paired.get("mcnemar_p"),
            "n_a": paired.get("n_a"),
            "n_b": paired.get("n_b"),
            "category_flips": cat_flips,
        }
    else:
        report["paired"] = None

    gates = _gate_records(args.run_dir)
    if gates:
        status_counts = {}
        retried = 0
        evidence_counts = []
        for r in gates:
            status_counts[r.get("status", "unknown")] = status_counts.get(r.get("status", "unknown"), 0) + 1
            retried += int(r.get("retried", False))
            evidence_counts.append(r.get("evidence_count", 0))
        report["gate"] = {
            "records": len(gates),
            "status_distribution": status_counts,
            "retried": retried,
            "evidence_count_mean": statistics.mean(evidence_counts) if evidence_counts else 0.0,
        }
    else:
        report["gate"] = None

    if args.out:
        args.out.write_text(json.dumps(report, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")
    else:
        print(json.dumps(report, indent=2, ensure_ascii=False))
    return 0


if __name__ == "__main__":
    sys.exit(main())
