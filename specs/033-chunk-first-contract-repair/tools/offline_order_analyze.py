#!/usr/bin/env python3
"""Compare legacy and kind-layered assembly journals without private labels.

The analyzer intentionally consumes only public assembly receipt fields. Extra
fields (including private benchmark labels) are ignored, so they cannot alter
cohort eligibility, ordering metrics, or the verdict.
"""

from __future__ import annotations

import argparse
import collections
import json
from pathlib import Path
from typing import Any, Iterable


class AnalysisError(ValueError):
    """Raised when journals cannot form a comparable paired diagnostic."""


def load_jsonl(path: Path) -> dict[str, dict[str, Any]]:
    records: dict[str, dict[str, Any]] = {}
    with path.open("r", encoding="utf-8") as handle:
        for line_number, raw in enumerate(handle, 1):
            if not raw.strip():
                continue
            record = json.loads(raw)
            question_id = record.get("question_id")
            if not isinstance(question_id, str) or not question_id:
                raise AnalysisError(f"{path}:{line_number}: missing question_id")
            if question_id in records:
                raise AnalysisError(f"{path}:{line_number}: duplicate question_id {question_id}")
            records[question_id] = record
    return records


def _closure_receipt(record: dict[str, Any]) -> tuple[int, str]:
    count = record.get("input_candidate_count")
    digest = record.get("input_closure_sha256")
    if not isinstance(count, int) or count < 0:
        raise AnalysisError(f"{record.get('question_id')}: missing input_candidate_count")
    if not isinstance(digest, str) or len(digest) != 64:
        raise AnalysisError(f"{record.get('question_id')}: missing input_closure_sha256")
    return count, digest


def _unit_multiset(record: dict[str, Any]) -> collections.Counter[tuple[str, str]]:
    units = record.get("units")
    if not isinstance(units, list):
        raise AnalysisError(f"{record.get('question_id')}: units must be a list")
    return collections.Counter((str(unit.get("source_id", "")), str(unit.get("text", ""))) for unit in units)


def _chunk_first(record: dict[str, Any]) -> bool:
    seen_fact = False
    for unit in record.get("units", []):
        kind = unit.get("kind")
        if kind == "fact":
            seen_fact = True
        elif kind == "chunk" and seen_fact:
            return False
    return True


def _chunk_rank_bands(record: dict[str, Any]) -> dict[str, int]:
    bands = {"1-5": 0, "6-10": 0, "11-15": 0, "16-20": 0, "21-30": 0, ">30": 0}
    for rank, unit in enumerate(record.get("units", []), 1):
        if unit.get("kind") != "chunk":
            continue
        if rank <= 5:
            bands["1-5"] += 1
        elif rank <= 10:
            bands["6-10"] += 1
        elif rank <= 15:
            bands["11-15"] += 1
        elif rank <= 20:
            bands["16-20"] += 1
        elif rank <= 30:
            bands["21-30"] += 1
        else:
            bands[">30"] += 1
    return bands


def analyze_records(
    legacy: dict[str, dict[str, Any]], treatment: dict[str, dict[str, Any]]
) -> dict[str, Any]:
    legacy_ids = set(legacy)
    treatment_ids = set(treatment)
    if legacy_ids != treatment_ids:
        missing_legacy = sorted(treatment_ids - legacy_ids)
        missing_treatment = sorted(legacy_ids - treatment_ids)
        raise AnalysisError(
            f"question coverage mismatch: missing_legacy={missing_legacy} "
            f"missing_treatment={missing_treatment}"
        )

    closure_equal = 0
    admitted_equal = 0
    treatment_chunk_first = 0
    legacy_chunk_first = 0
    legacy_mode_ok = 0
    treatment_mode_ok = 0
    non_multi_mode_ok = 0
    multi_hop_questions = 0
    prompt_order_ok = 0
    legacy_chunk_rank_bands = collections.Counter()
    treatment_chunk_rank_bands = collections.Counter()
    mismatched_closures: list[str] = []

    for question_id in sorted(legacy_ids):
        legacy_record = legacy[question_id]
        treatment_record = treatment[question_id]
        if legacy_record.get("category") != treatment_record.get("category"):
            raise AnalysisError(f"{question_id}: category mismatch")
        if _closure_receipt(legacy_record) == _closure_receipt(treatment_record):
            closure_equal += 1
        else:
            mismatched_closures.append(question_id)
        if _unit_multiset(legacy_record) == _unit_multiset(treatment_record):
            admitted_equal += 1
        if _chunk_first(legacy_record):
            legacy_chunk_first += 1
        if _chunk_first(treatment_record):
            treatment_chunk_first += 1
        if (
            legacy_record.get("prompt_order_matches_units") is True
            and treatment_record.get("prompt_order_matches_units") is True
        ):
            prompt_order_ok += 1
        if treatment_record.get("category") == 1:
            multi_hop_questions += 1
            legacy_chunk_rank_bands.update(_chunk_rank_bands(legacy_record))
            treatment_chunk_rank_bands.update(_chunk_rank_bands(treatment_record))
            if legacy_record.get("entity_order") == "legacy_grouped":
                legacy_mode_ok += 1
            if treatment_record.get("entity_order") == "kind_layered":
                treatment_mode_ok += 1
        elif (
            legacy_record.get("entity_order") == "not_applicable"
            and treatment_record.get("entity_order") == "not_applicable"
        ):
            non_multi_mode_ok += 1

    total = len(legacy_ids)
    return {
        "questions": total,
        "input_closure_equal": closure_equal,
        "input_closure_equal_rate": 1.0 if total == 0 else closure_equal / total,
        "mismatched_input_closures": mismatched_closures,
        "admitted_multiset_equal": admitted_equal,
        "legacy_chunk_first": legacy_chunk_first,
        "treatment_chunk_first": treatment_chunk_first,
        "legacy_mode_receipts": legacy_mode_ok,
        "treatment_mode_receipts": treatment_mode_ok,
        "multi_hop_questions": multi_hop_questions,
        "non_multi_mode_receipts": non_multi_mode_ok,
        "prompt_order_matches_units": prompt_order_ok,
        "legacy_multi_hop_chunk_rank_bands": dict(sorted(legacy_chunk_rank_bands.items())),
        "treatment_multi_hop_chunk_rank_bands": dict(sorted(treatment_chunk_rank_bands.items())),
        "valid": (
            closure_equal == total
            and legacy_mode_ok == multi_hop_questions
            and treatment_mode_ok == multi_hop_questions
            and non_multi_mode_ok == total - multi_hop_questions
            and treatment_chunk_first == total
            and prompt_order_ok == total
        ),
    }


def analyze_paths(legacy_path: Path, treatment_path: Path) -> dict[str, Any]:
    return analyze_records(load_jsonl(legacy_path), load_jsonl(treatment_path))


def main(argv: Iterable[str] | None = None) -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--legacy", required=True, type=Path)
    parser.add_argument("--treatment", required=True, type=Path)
    parser.add_argument("--json-out", type=Path)
    args = parser.parse_args(argv)

    summary = analyze_paths(args.legacy, args.treatment)
    payload = json.dumps(summary, indent=2, sort_keys=True) + "\n"
    if args.json_out:
        args.json_out.write_text(payload, encoding="utf-8")
    else:
        print(payload, end="")
    return 0 if summary["valid"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
