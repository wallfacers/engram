#!/usr/bin/env python3
"""Fail-closed paired analyzer for the 033 three-arm LoCoMo probe.

Gold-derived files are accepted only here, after answer results exist. The run
driver intentionally has no corresponding arguments, keeping private labels
out of retrieval, ordering, prompt construction, and arm selection.
"""

from __future__ import annotations

import argparse
import json
import math
from pathlib import Path
from typing import Any, Iterable


class AnalysisError(ValueError):
    """Raised when artifacts cannot support a comparable verdict."""


def read_ids(path: Path) -> list[str]:
    values = [line.strip() for line in path.read_text(encoding="utf-8").splitlines() if line.strip()]
    if len(values) != len(set(values)):
        raise AnalysisError(f"duplicate question ID in {path}")
    if not values:
        raise AnalysisError(f"empty cohort {path}")
    return values


def load_jsonl(path: Path) -> dict[str, dict[str, Any]]:
    records: dict[str, dict[str, Any]] = {}
    try:
        handle = path.open("r", encoding="utf-8")
    except OSError as exc:
        raise AnalysisError(f"missing or unreadable artifact {path}: {exc}") from exc
    with handle:
        for line_number, raw in enumerate(handle, 1):
            if not raw.strip():
                continue
            try:
                record = json.loads(raw)
            except json.JSONDecodeError as exc:
                raise AnalysisError(f"{path}:{line_number}: invalid JSON: {exc}") from exc
            question_id = record.get("question_id")
            if not isinstance(question_id, str) or not question_id:
                raise AnalysisError(f"{path}:{line_number}: missing question_id")
            if question_id in records:
                raise AnalysisError(f"{path}:{line_number}: duplicate question_id {question_id}")
            records[question_id] = record
    return records


def _require_coverage(path: Path, records: dict[str, dict[str, Any]], expected: set[str]) -> None:
    actual = set(records)
    if actual != expected:
        raise AnalysisError(
            f"{path}: coverage mismatch: missing={sorted(expected - actual)} extra={sorted(actual - expected)}"
        )


def _validate_result(path: Path, question_id: str, record: dict[str, Any]) -> None:
    if not isinstance(record.get("correct"), bool):
        raise AnalysisError(f"{path}: {question_id}: correct must be boolean")
    if not isinstance(record.get("category_name"), str) or not record["category_name"]:
        raise AnalysisError(f"{path}: {question_id}: missing category_name")
    context_tokens = record.get("answer_context_tokens")
    if not isinstance(context_tokens, int) or context_tokens <= 0:
        raise AnalysisError(f"{path}: {question_id}: missing positive provider answer_context_tokens")


def _validate_audit(path: Path, question_id: str, record: dict[str, Any]) -> None:
    units = record.get("units")
    candidates = record.get("input_candidate_count")
    if not isinstance(units, list):
        raise AnalysisError(f"{path}: {question_id}: assembly units must be a list")
    if not isinstance(candidates, int) or candidates < len(units):
        raise AnalysisError(f"{path}: {question_id}: invalid input/admitted candidate counts")
    if not isinstance(record.get("cap"), int) or record["cap"] <= 0:
        raise AnalysisError(f"{path}: {question_id}: invalid assembly cap")
    if not isinstance(record.get("total_tokens"), int) or record["total_tokens"] < 0:
        raise AnalysisError(f"{path}: {question_id}: invalid assembly total_tokens")
    if not isinstance(record.get("tokens_estimated"), bool):
        raise AnalysisError(f"{path}: {question_id}: missing tokens_estimated")
    if record.get("prompt_order_matches_units") is not True:
        raise AnalysisError(f"{path}: {question_id}: prompt order does not match assembly units")


def load_arm(
    root: Path,
    expected_ids: set[str],
    repeats: int,
    result_arm: str,
    require_audit: bool,
) -> tuple[list[dict[str, dict[str, Any]]], list[dict[str, dict[str, Any]]]]:
    results_by_repeat: list[dict[str, dict[str, Any]]] = []
    audits_by_repeat: list[dict[str, dict[str, Any]]] = []
    categories: dict[str, str] = {}
    for repeat in range(1, repeats + 1):
        run = root / f"run-{repeat}" if repeats > 1 else root
        result_path = run / f"results-{result_arm}.jsonl"
        results = load_jsonl(result_path)
        _require_coverage(result_path, results, expected_ids)
        for question_id, record in results.items():
            _validate_result(result_path, question_id, record)
            category = record["category_name"]
            if question_id in categories and categories[question_id] != category:
                raise AnalysisError(f"{question_id}: category drift across repeats")
            categories[question_id] = category
        results_by_repeat.append(results)
        if require_audit:
            audit_path = run / "assembly-audit.jsonl"
            try:
                audits = load_jsonl(audit_path)
            except AnalysisError as exc:
                raise AnalysisError(f"assembly audit unavailable: {exc}") from exc
            _require_coverage(audit_path, audits, expected_ids)
            for question_id, record in audits.items():
                _validate_audit(audit_path, question_id, record)
            audits_by_repeat.append(audits)
    return results_by_repeat, audits_by_repeat


def majority(runs: list[dict[str, dict[str, Any]]]) -> dict[str, bool]:
    if not runs or len(runs) % 2 == 0:
        raise AnalysisError("majority aggregation requires a positive odd repeat count")
    threshold = len(runs) // 2 + 1
    ids = set(runs[0])
    if any(set(run) != ids for run in runs[1:]):
        raise AnalysisError("repeat coverage drift")
    return {question_id: sum(bool(run[question_id]["correct"]) for run in runs) >= threshold for question_id in ids}


def exact_mcnemar(left_only: int, right_only: int) -> float:
    discordant = left_only + right_only
    if discordant == 0:
        return 1.0
    tail = sum(math.comb(discordant, k) for k in range(min(left_only, right_only) + 1)) / (2**discordant)
    return min(1.0, 2.0 * tail)


def holm_adjust(p_values: dict[str, float]) -> dict[str, float]:
    ordered = sorted(p_values.items(), key=lambda item: (item[1], item[0]))
    adjusted: dict[str, float] = {}
    running = 0.0
    total = len(ordered)
    for index, (name, value) in enumerate(ordered):
        running = max(running, min(1.0, value * (total - index)))
        adjusted[name] = running
    return adjusted


def paired_summary(
    left: dict[str, bool], right: dict[str, bool], ids: set[str], left_label: str, right_label: str
) -> dict[str, Any]:
    if not ids <= set(left) or not ids <= set(right):
        raise AnalysisError(f"paired {left_label}/{right_label} cohort is outside result coverage")
    left_only_ids = sorted(qid for qid in ids if left[qid] and not right[qid])
    right_only_ids = sorted(qid for qid in ids if right[qid] and not left[qid])
    both_correct = sum(left[qid] and right[qid] for qid in ids)
    both_wrong = sum(not left[qid] and not right[qid] for qid in ids)
    return {
        "questions": len(ids),
        "both_correct": both_correct,
        "both_wrong": both_wrong,
        f"{left_label}_only": len(left_only_ids),
        f"{right_label}_only": len(right_only_ids),
        "net_right_minus_left": len(right_only_ids) - len(left_only_ids),
        "mcnemar_exact_p": exact_mcnemar(len(left_only_ids), len(right_only_ids)),
        f"{left_label}_only_ids": left_only_ids,
        f"{right_label}_only_ids": right_only_ids,
    }


def load_chunk_gold_map(path: Path, expected_ids: set[str], high_rank_ids: set[str]) -> tuple[str, dict[str, dict[str, Any]]]:
    payload = json.loads(path.read_text(encoding="utf-8"))
    trace_digest = payload.get("source_trace_sha256")
    if not isinstance(trace_digest, str) or len(trace_digest) != 64:
        raise AnalysisError(f"{path}: invalid source trace digest")
    records: dict[str, dict[str, Any]] = {}
    for item in payload.get("records", []):
        question_id = item.get("question_id")
        source_id = item.get("gold_source_id")
        rank = item.get("gold_rank_topk")
        if not isinstance(question_id, str) or question_id in records:
            raise AnalysisError(f"{path}: invalid or duplicate gold-map question ID")
        if not isinstance(source_id, str) or not source_id.startswith("chunk-"):
            raise AnalysisError(f"{path}: {question_id}: invalid chunk source")
        if not isinstance(rank, int) or rank < 1:
            raise AnalysisError(f"{path}: {question_id}: invalid gold rank")
        records[question_id] = item
    if set(records) != expected_ids:
        raise AnalysisError(f"{path}: chunk-gold map coverage mismatch")
    derived_high = {qid for qid, item in records.items() if item["gold_rank_topk"] >= 19}
    if derived_high != high_rank_ids:
        raise AnalysisError(f"{path}: high-rank cohort does not match literal gold_rank_topk >= 19")
    return trace_digest, records


def _check_category_parity(
    arm_a: list[dict[str, dict[str, Any]]], arm_b: list[dict[str, dict[str, Any]]], arm_c: list[dict[str, dict[str, Any]]]
) -> dict[str, str]:
    categories = {qid: record["category_name"] for qid, record in arm_a[0].items()}
    for runs in (arm_a, arm_c):
        for run in runs:
            for qid, record in run.items():
                if categories.get(qid) != record["category_name"]:
                    raise AnalysisError(f"{qid}: A/C category mismatch")
    for run in arm_b:
        for qid, record in run.items():
            if categories.get(qid) != record["category_name"]:
                raise AnalysisError(f"{qid}: B/C category mismatch")
    return categories


def analyze_probe(
    *,
    arm_a: Path,
    arm_b: Path,
    arm_c: Path,
    target_path: Path,
    guard_path: Path,
    multi_hop_path: Path,
    chunk_gold_path: Path,
    high_rank_path: Path,
    chunk_gold_map_path: Path,
    repeats: int,
    result_arm: str,
) -> dict[str, Any]:
    if repeats < 1 or repeats % 2 == 0:
        raise AnalysisError("repeats must be a positive odd number")
    target = set(read_ids(target_path))
    guard = set(read_ids(guard_path))
    if target & guard:
        raise AnalysisError("target and guard cohorts overlap")
    probe = target | guard
    multi_hop = set(read_ids(multi_hop_path))
    chunk_gold = set(read_ids(chunk_gold_path))
    high_rank = set(read_ids(high_rank_path))
    if not multi_hop <= probe:
        raise AnalysisError("multi-hop cohort is outside probe")
    if not chunk_gold <= target:
        raise AnalysisError("chunk-gold cohort is outside target")
    if not high_rank <= chunk_gold:
        raise AnalysisError("high-rank cohort must be an analysis-only subset of chunk-gold")
    trace_digest, gold_map = load_chunk_gold_map(chunk_gold_map_path, chunk_gold, high_rank)

    a_runs, _ = load_arm(arm_a, probe, repeats, result_arm, require_audit=False)
    b_runs, b_audits = load_arm(arm_b, multi_hop, repeats, result_arm, require_audit=True)
    c_runs, c_audits = load_arm(arm_c, probe, repeats, result_arm, require_audit=True)
    categories = _check_category_parity(a_runs, b_runs, c_runs)
    a_majority, b_majority, c_majority = majority(a_runs), majority(b_runs), majority(c_runs)

    target_summary = paired_summary(a_majority, c_majority, target, "a", "c")
    guard_summary = paired_summary(a_majority, c_majority, guard, "a", "c")
    all_summary = paired_summary(a_majority, c_majority, probe, "a", "c")
    isolated = paired_summary(b_majority, c_majority, multi_hop, "b", "c")
    high_summary = paired_summary(a_majority, c_majority, high_rank, "a", "c")
    remainder_summary = paired_summary(a_majority, c_majority, probe - high_rank, "a", "c")

    category_summaries: dict[str, dict[str, Any]] = {}
    for category in sorted(set(categories.values())):
        ids = {qid for qid in probe if categories[qid] == category}
        category_summaries[category] = paired_summary(a_majority, c_majority, ids, "a", "c")
    adjusted = holm_adjust({name: value["mcnemar_exact_p"] for name, value in category_summaries.items()})
    negative_regressions: list[str] = []
    for name, value in category_summaries.items():
        value["holm_adjusted_p"] = adjusted[name]
        value["significant_net_negative"] = value["net_right_minus_left"] < 0 and adjusted[name] < 0.05
        if value["significant_net_negative"]:
            negative_regressions.append(name)

    context_audit: list[dict[str, Any]] = []
    for repeat in range(repeats):
        for qid in sorted(chunk_gold):
            audit = c_audits[repeat][qid]
            unit_ids = {unit.get("source_id") for unit in audit["units"]}
            gold = gold_map[qid]
            admitted = len(audit["units"])
            candidates = audit["input_candidate_count"]
            truncated = admitted < candidates
            gold_admitted = gold["gold_source_id"] in unit_ids
            context_audit.append(
                {
                    "question_id": qid,
                    "repeat": repeat + 1,
                    "gold_source_id": gold["gold_source_id"],
                    "gold_rank_topk": gold["gold_rank_topk"],
                    "a_provider_answer_context_tokens": a_runs[repeat][qid]["answer_context_tokens"],
                    "c_provider_answer_context_tokens": c_runs[repeat][qid]["answer_context_tokens"],
                    "assembly_total_tokens": audit["total_tokens"],
                    "assembly_cap": audit["cap"],
                    "tokens_estimated": audit["tokens_estimated"],
                    "input_candidates": candidates,
                    "admitted_units": admitted,
                    "truncated": truncated,
                    "gold_admitted": gold_admitted,
                    "gold_excluded_by_cap": truncated and not gold_admitted,
                }
            )

    exact_rows = sum(not row["tokens_estimated"] for row in context_audit)
    truncated_rows = sum(row["truncated"] for row in context_audit)
    gold_excluded_rows = sum(row["gold_excluded_by_cap"] for row in context_audit)
    guard_loss = guard_summary["a_only"] - guard_summary["c_only"]
    target_net = target_summary["c_only"] - target_summary["a_only"]
    go = target_net >= 8 and guard_loss <= 1 and not negative_regressions
    return {
        "valid": True,
        "repeats": repeats,
        "coverage": {"target": len(target), "guard": len(guard), "probe": len(probe), "multi_hop_b": len(multi_hop)},
        "source_trace_sha256": trace_digest,
        "primary": {"target": target_summary, "guard": guard_summary, "all": all_summary},
        "isolated_b_to_c": isolated,
        "strata": {"chunk_gold_rank_ge_19": high_summary, "remainder": remainder_summary},
        "categories": category_summaries,
        "chunk_gold_context_audit": context_audit,
        "audit_summary": {
            "chunk_gold_questions": len(chunk_gold),
            "chunk_gold_question_repeats": len(context_audit),
            "exact_counter_rows": exact_rows,
            "estimated_counter_rows": len(context_audit) - exact_rows,
            "truncated_rows": truncated_rows,
            "gold_excluded_by_cap_rows": gold_excluded_rows,
            "all_token_counts_exact": exact_rows == len(context_audit),
        },
        "gate": {
            "target_net": target_net,
            "target_required": 8,
            "guard_net_loss": guard_loss,
            "guard_max_loss": 1,
            "significant_net_negative_categories": negative_regressions,
            "go": go,
        },
    }


def main(argv: Iterable[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--arm-a", required=True, type=Path)
    parser.add_argument("--arm-b", required=True, type=Path)
    parser.add_argument("--arm-c", required=True, type=Path)
    parser.add_argument("--target", required=True, type=Path)
    parser.add_argument("--guard", required=True, type=Path)
    parser.add_argument("--multi-hop", required=True, type=Path)
    parser.add_argument("--chunk-gold", required=True, type=Path)
    parser.add_argument("--high-rank", required=True, type=Path)
    parser.add_argument("--chunk-gold-map", required=True, type=Path)
    parser.add_argument("--repeats", type=int, default=3)
    parser.add_argument("--result-arm", default="hybrid")
    parser.add_argument("--json-out", type=Path)
    args = parser.parse_args(argv)
    try:
        summary = analyze_probe(
            arm_a=args.arm_a,
            arm_b=args.arm_b,
            arm_c=args.arm_c,
            target_path=args.target,
            guard_path=args.guard,
            multi_hop_path=args.multi_hop,
            chunk_gold_path=args.chunk_gold,
            high_rank_path=args.high_rank,
            chunk_gold_map_path=args.chunk_gold_map,
            repeats=args.repeats,
            result_arm=args.result_arm,
        )
    except (AnalysisError, OSError, json.JSONDecodeError) as exc:
        parser.error(str(exc))
    payload = json.dumps(summary, indent=2, sort_keys=True) + "\n"
    if args.json_out:
        args.json_out.write_text(payload, encoding="utf-8")
    else:
        print(payload, end="")
    return 0 if summary["gate"]["go"] else 2


if __name__ == "__main__":
    raise SystemExit(main())
