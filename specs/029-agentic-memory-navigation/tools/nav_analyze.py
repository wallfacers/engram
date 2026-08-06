#!/usr/bin/env python3
"""029 US2 paired navigation-vs-baseline analysis (contracts/navigation-trajectory.md
Pair-Gate + spec US2). Consumes the harness result journals
(`results-<arm>.jsonl`, per-repeat under `run-N/` when --repeats>1) and the
navigation arm's trajectory audit (`nav-trajectories.jsonl`) and reports:

  - majority correctness per arm (correct >= ceil(repeats/2) -> question correct)
  - paired McNemar exact binomial p (nav✓/base✗ vs nav✗/base✓)
  - per-category regression (temporal / multi-hop / other), 008 rule
  - navigation step-count and token accounting (budget_usage) + fallback rate
  - trajectory schema validation replay (contracts/navigation-trajectory.md)

Verification gate (008 rule): navigation majority >= baseline majority → GO;
otherwise NO-GO. Pure offline; consumes already-written artifacts.
"""
import argparse
import json
import math
import os
import sys
from collections import Counter


def parse_args(argv):
    p = argparse.ArgumentParser(description="029 US2 paired navigation analysis (offline)")
    p.add_argument("--base-dir", required=True, help="baseline run dir (contains results-<arm>.jsonl or run-N/)")
    p.add_argument("--nav-dir", required=True, help="navigation run dir")
    p.add_argument("--arm", default="hybrid", help="result arm name (default hybrid)")
    p.add_argument("--repeats", type=int, default=3, help="number of repeats (majority vote)")
    p.add_argument("--nav-trajectories", default=None, help="path to nav-trajectories.jsonl for step/token accounting")
    p.add_argument("--answer-context-cap", type=int, default=1800, help="answer-context token cap for budget validation")
    p.add_argument("--out", default="us2-pair-report.json", help="output JSON path")
    return p.parse_args(argv)


def load_results(dir_path, arm, repeats):
    """Return per-repeat {question_id: row}. Raises if a repeat file is missing."""
    all_reps = []
    for r in range(1, repeats + 1):
        sub = dir_path if repeats == 1 else os.path.join(dir_path, f"run-{r}")
        path = os.path.join(sub, f"results-{arm}.jsonl")
        rows = []
        with open(path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                rows.append(json.loads(line))
        all_reps.append({row.get("question_id") or f"conv-{row.get('conv')}-q-{row.get('q')}": row for row in rows})
    return all_reps


def majority_correct(rows, key):
    """rows: per-repeat {qid: row}. key: the result id used for pairing."""
    votes = {}
    for rep in rows:
        for qid in key:
            row = rep.get(qid)
            votes.setdefault(qid, []).append(bool(row.get("correct")) if row else False)
    out = {}
    for qid, corrs in votes.items():
        out[qid] = sum(corrs) >= (len(corrs) + 1) // 2
    return out


def mcnemar_p(b, n):
    """Two-sided exact binomial p for discordant pairs (b = nav✓/base✗, n = nav✗/base✓)."""
    k = b + n
    if k == 0:
        return 1.0
    x = max(b, n)
    p = 0.0
    for i in range(x, k + 1):
        p += math.comb(k, i) * (0.5 ** k)
    return min(1.0, 2.0 * p)


def load_trajectories(path):
    if not path or not os.path.exists(path):
        return None
    out = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            out.append(json.loads(line))
    return out


def validate_trajectory(t, cap, max_steps=None):
    """Replay contracts/navigation-trajectory.md validation. Returns (ok, reason)."""
    if not isinstance(t.get("steps"), list) or len(t["steps"]) < 1:
        return False, "steps missing/empty"
    if max_steps and len(t["steps"]) > max_steps:
        return False, f"steps {len(t['steps'])} > cap {max_steps}"
    last = t["steps"][-1]
    fallback = bool(t.get("fallback_triggered"))
    if not fallback and last.get("tool") != "stop":
        return False, f"last step tool={last.get('tool')} not stop"
    budget = t.get("budget_usage") or {}
    if budget.get("answer_context_tokens", 0) > cap:
        return False, f"answer_context_tokens {budget.get('answer_context_tokens')} > cap {cap}"
    fe = t.get("final_evidence") or {}
    if not fallback and (not fe.get("evidence") or len(fe["evidence"]) == 0):
        return False, "final_evidence empty without fallback"
    allowed = {"search", "expand_query", "follow_entity", "stop"}
    for s in t["steps"]:
        if s.get("tool") not in allowed:
            return False, f"unknown tool {s.get('tool')}"
    return True, ""


def main(argv=None):
    args = parse_args(argv if argv is not None else sys.argv[1:])
    base_reps = load_results(args.base_dir, args.arm, args.repeats)
    nav_reps = load_results(args.nav_dir, args.arm, args.repeats)

    # Pair on the union of question ids present in both arms.
    base_ids = set(base_reps[0].keys())
    nav_ids = set(nav_reps[0].keys())
    key = sorted(base_ids & nav_ids)
    if not key:
        print("[nav_analyze] no shared question ids between arms", file=sys.stderr)
        return 1

    base_correct = majority_correct(base_reps, key)
    nav_correct = majority_correct(nav_reps, key)

    b = n = 0
    for qid in key:
        if nav_correct[qid] and not base_correct[qid]:
            b += 1
        elif base_correct[qid] and not nav_correct[qid]:
            n += 1
    p = mcnemar_p(b, n)

    base_acc = sum(base_correct.values()) / len(key)
    nav_acc = sum(nav_correct.values()) / len(key)

    # Per-category regression (008 rule): no category may drop significantly.
    cat_regression = {}
    cats = {}
    for rep in nav_reps:
        for qid, row in rep.items():
            if qid in key:
                cats.setdefault(row.get("category_name") or f"cat{row.get('category')}", set()).add(qid)
    for cat, ids in sorted(cats.items()):
        ids = [i for i in ids if i in key]
        if not ids:
            continue
        bc = sum(base_correct[i] for i in ids) / len(ids)
        nc = sum(nav_correct[i] for i in ids) / len(ids)
        cat_regression[cat] = {"n": len(ids), "baseline": round(bc, 4), "navigation": round(nc, 4),
                               "delta": round(nc - bc, 4)}

    # Step / token accounting from the trajectory audit.
    budget = {"steps": {}, "nav_tokens_total": None, "answer_context_tokens_total": None,
              "fallback_count": None, "over_cap": 0, "invalid": 0}
    trajectories = load_trajectories(args.nav_trajectories)
    if trajectories is not None:
        step_hist = Counter()
        nav_tok = ans_tok = 0
        fallback = 0
        invalid = 0
        for t in trajectories:
            ok, reason = validate_trajectory(t, args.answer_context_cap)
            if not ok:
                invalid += 1
            budget["over_cap"] += int((t.get("budget_usage") or {}).get("answer_context_tokens", 0) > args.answer_context_cap)
            step_hist[len(t.get("steps") or [])] += 1
            nav_tok += (t.get("budget_usage") or {}).get("nav_tokens", 0)
            ans_tok += (t.get("budget_usage") or {}).get("answer_context_tokens", 0)
            fallback += int(bool(t.get("fallback_triggered")))
        budget = {
            "steps": dict(sorted(step_hist.items())),
            "nav_tokens_total": nav_tok,
            "answer_context_tokens_total": ans_tok,
            "fallback_count": fallback,
            "fallback_rate": round(fallback / len(trajectories), 4),
            "invalid": invalid,
            "over_cap": budget["over_cap"],
        }

    go = nav_acc >= base_acc and p < 0.05 and all(r["delta"] >= -0.0 for r in cat_regression.values())
    report = {
        "n": len(key),
        "baseline": {"majority_accuracy": round(base_acc, 4)},
        "navigation": {"majority_accuracy": round(nav_acc, 4)},
        "delta_pp": round((nav_acc - base_acc) * 100, 2),
        "mcnemar": {"p": round(p, 4), "nav_win_base_loss": b, "nav_loss_base_win": n},
        "category_regression": cat_regression,
        "budget": budget,
        "verdict_gate": {
            "rule": "008: navigation majority >= baseline majority",
            "navigation": round(nav_acc, 4),
            "baseline": round(base_acc, 4),
            "pass": go,
        },
    }
    with open(args.out, "w") as f:
        json.dump(report, f, indent=2)

    print(f"[nav_analyze] n={len(key)} base={base_acc:.3f} nav={nav_acc:.3f} "
          f"delta={report['delta_pp']:+.2f}pp mcnemar_p={p:.4f}")
    if cat_regression:
        print("  category:", {k: f"{v['baseline']:.2f}->{v['navigation']:.2f} ({v['delta']:+.2f})"
                              for k, v in cat_regression.items()})
    if trajectories is not None:
        print(f"  budget: fallback_rate={budget['fallback_rate']} steps={budget['steps']} "
              f"invalid={budget['invalid']} over_cap={budget['over_cap']}")
    print(f"  gate: {'GO' if go else 'NO-GO'} (008 rule)")
    print(f"  wrote {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
