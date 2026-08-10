#!/usr/bin/env python3

import importlib.util
import json
from pathlib import Path
import tempfile
import unittest


MODULE_PATH = Path(__file__).with_name("offline_order_analyze.py")
SPEC = importlib.util.spec_from_file_location("offline_order_analyze", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(MODULE)


def record(question_id, mode, units, digest="a" * 64, **private):
    return {
        "question_id": question_id,
        "category": 1,
        "entity_order": mode,
        "input_candidate_count": len(units),
        "input_closure_sha256": digest,
        "prompt_order_matches_units": True,
        "units": units,
        **private,
    }


CHUNK = {"source_id": "chunk-1", "text": "raw", "kind": "chunk"}
FACT = {"source_id": "fact-1", "text": "derived", "kind": "fact"}


class OfflineOrderAnalyzeTest(unittest.TestCase):
    def test_paired_closure_and_kind_layer(self):
        legacy = {"q1": record("q1", "legacy_grouped", [FACT, CHUNK])}
        treatment = {"q1": record("q1", "kind_layered", [CHUNK, FACT])}
        got = MODULE.analyze_records(legacy, treatment)
        self.assertTrue(got["valid"])
        self.assertEqual(got["input_closure_equal"], 1)
        self.assertEqual(got["admitted_multiset_equal"], 1)
        self.assertEqual(got["legacy_chunk_first"], 0)
        self.assertEqual(got["treatment_chunk_first"], 1)
        self.assertEqual(got["prompt_order_matches_units"], 1)
        self.assertEqual(got["treatment_multi_hop_chunk_rank_bands"]["1-5"], 1)

    def test_private_labels_cannot_change_analysis(self):
        legacy = {
            "q1": record(
                "q1", "legacy_grouped", [FACT, CHUNK], gold="secret-a", correct=False,
                judge_verdict="wrong", gold_rank=29,
            )
        }
        treatment = {
            "q1": record(
                "q1", "kind_layered", [CHUNK, FACT], gold="secret-b", correct=True,
                judge_verdict="right", gold_rank=1,
            )
        }
        with_private = MODULE.analyze_records(legacy, treatment)
        for item in (*legacy.values(), *treatment.values()):
            for key in ("gold", "correct", "judge_verdict", "gold_rank"):
                item.pop(key)
        self.assertEqual(with_private, MODULE.analyze_records(legacy, treatment))

    def test_rejects_mismatched_coverage_and_closure(self):
        with self.assertRaises(MODULE.AnalysisError):
            MODULE.analyze_records(
                {"q1": record("q1", "legacy_grouped", [FACT])},
                {"q2": record("q2", "kind_layered", [FACT])},
            )
        got = MODULE.analyze_records(
            {"q1": record("q1", "legacy_grouped", [FACT], digest="a" * 64)},
            {"q1": record("q1", "kind_layered", [FACT], digest="b" * 64)},
        )
        self.assertFalse(got["valid"])
        self.assertEqual(got["mismatched_input_closures"], ["q1"])

    def test_prompt_order_mismatch_invalidates_receipt(self):
        legacy_record = record("q1", "legacy_grouped", [FACT, CHUNK])
        treatment_record = record("q1", "kind_layered", [CHUNK, FACT])
        treatment_record["prompt_order_matches_units"] = False
        got = MODULE.analyze_records({"q1": legacy_record}, {"q1": treatment_record})
        self.assertFalse(got["valid"])
        self.assertEqual(got["prompt_order_matches_units"], 0)

    def test_jsonl_loader_rejects_duplicate_question(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory, "dup.jsonl")
            payload = record("q1", "kind_layered", [CHUNK])
            path.write_text(json.dumps(payload) + "\n" + json.dumps(payload) + "\n", encoding="utf-8")
            with self.assertRaises(MODULE.AnalysisError):
                MODULE.load_jsonl(path)


if __name__ == "__main__":
    unittest.main()
