#!/usr/bin/env python3
from __future__ import annotations

import json
import tempfile
import unittest
from pathlib import Path

import probe_analyze


class ProbeAnalyzeTest(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.target = self._ids("target.txt", ["q1", "q2"])
        self.guard = self._ids("guard.txt", ["q3", "q4"])
        self.multi = self._ids("multi.txt", ["q1", "q3"])
        self.chunk = self._ids("chunk.txt", ["q1", "q2"])
        self.high = self._ids("high.txt", ["q1"])
        self.chunk_map = self.root / "chunk-map.json"
        self.chunk_map.write_text(
            json.dumps(
                {
                    "source_trace_sha256": "a" * 64,
                    "records": [
                        {"question_id": "q1", "gold_source_id": "chunk-q1-0", "gold_rank_topk": 19},
                        {"question_id": "q2", "gold_source_id": "chunk-q2-0", "gold_rank_topk": 2},
                    ],
                }
            ),
            encoding="utf-8",
        )
        self.a = self.root / "a"
        self.b = self.root / "b"
        self.c = self.root / "c"
        categories = {"q1": "multi-hop", "q2": "single-hop", "q3": "multi-hop", "q4": "temporal"}
        # Majority: A q1/q3/q4; C q1/q2/q4. Target net +1, guard loss 1.
        a_correct = {"q1": [True, True, False], "q2": [False, False, True], "q3": [True, True, False], "q4": [True, True, True]}
        c_correct = {"q1": [True, True, True], "q2": [True, True, False], "q3": [False, False, True], "q4": [True, True, True]}
        b_correct = {"q1": [False, False, True], "q3": [False, True, False]}
        self._arm(self.a, a_correct, categories, audit=False)
        self._arm(self.c, c_correct, categories, audit=True)
        self._arm(self.b, b_correct, categories, audit=True)

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def _ids(self, name: str, values: list[str]) -> Path:
        path = self.root / name
        path.write_text("\n".join(values) + "\n", encoding="utf-8")
        return path

    def _arm(self, root: Path, outcomes: dict[str, list[bool]], categories: dict[str, str], audit: bool) -> None:
        for repeat in range(1, 4):
            run = root / f"run-{repeat}"
            run.mkdir(parents=True)
            results = []
            audits = []
            for index, question_id in enumerate(sorted(outcomes)):
                results.append(
                    {
                        "question_id": question_id,
                        "category_name": categories[question_id],
                        "correct": outcomes[question_id][repeat - 1],
                        "answer_context_tokens": 3000 + repeat * 10 + index,
                        "answer_regime": "fixture",
                    }
                )
                if audit:
                    # q1 truncates from 3 to 2; all other questions admit all 3.
                    admitted = 2 if question_id == "q1" else 3
                    audits.append(
                        {
                            "question_id": question_id,
                            "category": 1 if categories[question_id] == "multi-hop" else 4,
                            "input_candidate_count": 3,
                            "input_closure_sha256": "a" * 64,
                            "entity_order": "kind_layered",
                            "prompt_order_matches_units": True,
                            "units": [
                                {"source_id": f"chunk-{question_id}-{i}", "text": "e", "kind": "chunk"}
                                for i in range(admitted)
                            ],
                            "total_tokens": 2500 + repeat,
                            "cap": 3600,
                            "tokens_estimated": repeat == 3,
                        }
                    )
            self._jsonl(run / "results-hybrid.jsonl", results)
            if audit:
                self._jsonl(run / "assembly-audit.jsonl", audits)

    @staticmethod
    def _jsonl(path: Path, rows: list[dict[str, object]]) -> None:
        path.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")

    def analyze(self) -> dict[str, object]:
        return probe_analyze.analyze_probe(
            arm_a=self.a,
            arm_b=self.b,
            arm_c=self.c,
            target_path=self.target,
            guard_path=self.guard,
            multi_hop_path=self.multi,
            chunk_gold_path=self.chunk,
            high_rank_path=self.high,
            chunk_gold_map_path=self.chunk_map,
            repeats=3,
            result_arm="hybrid",
        )

    def test_majority_flips_strata_and_context_audit(self) -> None:
        summary = self.analyze()
        self.assertEqual(summary["primary"]["target"]["c_only"], 1)
        self.assertEqual(summary["primary"]["target"]["a_only"], 0)
        self.assertEqual(summary["primary"]["guard"]["a_only"], 1)
        self.assertEqual(summary["strata"]["chunk_gold_rank_ge_19"]["questions"], 1)
        self.assertEqual(summary["strata"]["remainder"]["questions"], 3)
        self.assertEqual(summary["isolated_b_to_c"]["b_only"], 0)
        self.assertEqual(summary["isolated_b_to_c"]["c_only"], 1)
        self.assertFalse(summary["gate"]["go"])
        audits = summary["chunk_gold_context_audit"]
        self.assertEqual(len(audits), 6)
        q1 = [row for row in audits if row["question_id"] == "q1"]
        self.assertTrue(all(row["truncated"] for row in q1))
        self.assertTrue(all(row["gold_admitted"] for row in q1))
        self.assertTrue(any(row["tokens_estimated"] for row in q1))
        self.assertEqual(summary["audit_summary"]["chunk_gold_question_repeats"], 6)

    def test_missing_runtime_audit_fails_closed(self) -> None:
        (self.c / "run-2" / "assembly-audit.jsonl").unlink()
        with self.assertRaisesRegex(probe_analyze.AnalysisError, "assembly audit"):
            self.analyze()

    def test_misaligned_audit_fails_closed(self) -> None:
        path = self.c / "run-1" / "assembly-audit.jsonl"
        rows = [json.loads(line) for line in path.read_text(encoding="utf-8").splitlines()]
        rows.pop()
        self._jsonl(path, rows)
        with self.assertRaisesRegex(probe_analyze.AnalysisError, "coverage mismatch"):
            self.analyze()

    def test_high_rank_must_be_analysis_only_subset(self) -> None:
        self.high.write_text("q4\n", encoding="utf-8")
        with self.assertRaisesRegex(probe_analyze.AnalysisError, "high-rank cohort"):
            self.analyze()

    def test_exact_mcnemar_and_holm(self) -> None:
        self.assertEqual(probe_analyze.exact_mcnemar(0, 0), 1.0)
        self.assertAlmostEqual(probe_analyze.exact_mcnemar(0, 6), 0.03125)
        adjusted = probe_analyze.holm_adjust({"a": 0.01, "b": 0.04, "c": 0.2})
        self.assertAlmostEqual(adjusted["a"], 0.03)
        self.assertAlmostEqual(adjusted["b"], 0.08)
        self.assertAlmostEqual(adjusted["c"], 0.2)


if __name__ == "__main__":
    unittest.main()
