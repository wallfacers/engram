#!/usr/bin/env python3
"""Regression tests for the reproducible LoCoMo McNemar report."""

import importlib.util
import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


SCRIPT = pathlib.Path(__file__).with_name("mcnemar.py")


def load_script():
    spec = importlib.util.spec_from_file_location("mcnemar", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class McNemarArtifactTest(unittest.TestCase):
    def test_pairs_majority_engrams_with_deduplicated_memos_records(self):
        script = load_script()
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            run_paths = []
            votes = ((True, False, True), (True, True, True), (True, False, True))
            questions = ("same", "memos-only", "engram-only")
            for run_number, run_votes in enumerate(votes, start=1):
                path = root / ("run-%d.jsonl" % run_number)
                rows = [
                    {
                        "conv": 0,
                        "q": index,
                        "question": question,
                        "category_name": "single-hop" if index == 0 else "multi-hop",
                        "correct": correct,
                    }
                    for index, (question, correct) in enumerate(zip(questions, run_votes))
                ]
                path.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")
                run_paths.append(path)

            memos = root / "memos.json"
            memos.write_text(json.dumps([
                {"group": "locomo_exp_user_0", "question": "same", "cat": 4, "correct": True},
                {"group": "locomo_exp_user_0", "question": "memos-only", "cat": 1, "correct": True},
                {"group": "locomo_exp_user_0", "question": "engram-only", "cat": 1, "correct": False},
                {"group": "locomo_exp_user_0", "question": "engram-only", "cat": 1, "correct": False},
            ]), encoding="utf-8")

            report = script.analyze(run_paths, memos)

        overall = report["overall"]
        self.assertEqual(overall["n"], 3)
        self.assertEqual(overall["engram_correct"], 2)
        self.assertEqual(overall["memos_correct"], 2)
        self.assertEqual((overall["b"], overall["c"]), (1, 1))
        self.assertEqual(report["deduplicated_memos"], 1)

    def test_reports_exact_two_sided_p_and_continuity_corrected_chi_square(self):
        script = load_script()

        statistics = script.mcnemar_statistics(155, 106)

        self.assertAlmostEqual(statistics["chi_square_cc"], 8.8275862069, places=10)
        self.assertAlmostEqual(statistics["p_exact"], 0.00289529547966, places=14)

    def test_cli_accepts_an_engrams_glob_and_prints_a_report(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            (root / "run-1.jsonl").write_text(json.dumps({
                "conv": 0,
                "q": 0,
                "question": "engram wins",
                "category_name": "single-hop",
                "correct": True,
            }) + "\n", encoding="utf-8")
            memos = root / "memos.json"
            memos.write_text(json.dumps([{
                "group": "locomo_exp_user_0",
                "question": "engram wins",
                "cat": 4,
                "correct": False,
            }]), encoding="utf-8")

            completed = subprocess.run(
                [sys.executable, str(SCRIPT), str(root / "run-*.jsonl"), str(memos)],
                check=True,
                capture_output=True,
                text=True,
            )

        self.assertIn("OVERALL", completed.stdout)
        self.assertIn("b=1 c=0", completed.stdout)

    def test_rejects_a_category_mismatch_for_an_aligned_question(self):
        script = load_script()
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            run = root / "run.jsonl"
            run.write_text(json.dumps({
                "conv": 0,
                "q": 0,
                "question": "same question",
                "category_name": "single-hop",
                "correct": True,
            }) + "\n", encoding="utf-8")
            memos = root / "memos.json"
            memos.write_text(json.dumps([{
                "group": "locomo_exp_user_0",
                "question": "same question",
                "cat": 1,
                "correct": True,
            }]), encoding="utf-8")

            with self.assertRaisesRegex(ValueError, "category mismatch"):
                script.analyze([run], memos)

    def test_keeps_the_first_duplicate_question_from_each_engrams_run(self):
        script = load_script()
        with tempfile.TemporaryDirectory() as tmp:
            root = pathlib.Path(tmp)
            run_paths = []
            for run_number in range(1, 4):
                path = root / ("run-%d.jsonl" % run_number)
                rows = [
                    {
                        "conv": 0,
                        "q": 10,
                        "question": "duplicated LoCoMo question",
                        "category_name": "single-hop",
                        "correct": False,
                    },
                    {
                        "conv": 0,
                        "q": 11,
                        "question": "duplicated LoCoMo question",
                        "category_name": "single-hop",
                        "correct": True,
                    },
                ]
                path.write_text("".join(json.dumps(row) + "\n" for row in rows), encoding="utf-8")
                run_paths.append(path)
            memos = root / "memos.json"
            memos.write_text(json.dumps([{
                "group": "locomo_exp_user_0",
                "question": "duplicated LoCoMo question",
                "cat": 4,
                "correct": False,
            }]), encoding="utf-8")

            report = script.analyze(run_paths, memos)

        self.assertEqual(report["overall"]["n"], 1)
        self.assertFalse(report["overall"]["engram_correct"])


if __name__ == "__main__":
    unittest.main()
