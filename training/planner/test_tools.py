#!/usr/bin/env python3
"""Unit tests for the 023 training-pipeline tooling (data_build helpers, label,
rebuild_check, audit). Run from training/planner:

    python3 -m unittest test_tools -v

Offline and deterministic; no LLM/network endpoints required.
"""

import hashlib
import json
import os
import tempfile
import unittest

import audit as audit_mod
import corpus_adapter as ca
import data_build as db
import rebuild_check as rc
import review as rv
from label import (STOPWORDS_A, adjudicate, assign_split, label_actions,
                   labeler_a, labeler_b, parse_need)


def digest_of(obj):
    return hashlib.sha256(
        json.dumps(obj, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
    ).hexdigest()


def sample(cid, query, gold, cat="single-hop", split="train", lic="cc-by-4.0-synthetic",
           covered=True, missing=None):
    s = {
        "id": f"023-btest-r1-{cid}-q0", "conversation_id": cid, "query": query,
        "query_date": "2026-01-01", "category": cat,
        "candidates": [{"id": "e1", "kind": "atomic_fact", "rank": 0, "score": 0.5,
                        "text": query, "text_digest": "t", "source_ids": ["ev1"]}],
        "sources": {"ev1": {"session_id": cid + "-s0", "ordinal": 0,
                            "content_digest": "cd", "occurred_at": "2026-01-01T00:00:00Z"}},
        "target": {"need": {"entities": ["Dana"], "time_constraints": [], "operands": [],
                            "list_cardinality": {"known": False, "count": 0},
                            "update_state": "", "gap": None},
                   "actions": [{"kind": "KEEP", "candidate_id": "e1", "source_id": "ev1"}]},
        "data_source": "synthetic", "license": lic, "split": split,
        "build_version": "023-btest-r1",
        "gold_answer": gold,
    }
    if covered:
        s["gold_coverage"] = {"gold_source_evidence_ids": ["ev1"],
                              "candidate_evidence_union": ["ev1"],
                              "covered_source_count": 1, "candidate_covered": True}
    else:
        s["gold_coverage"] = {"gold_source_evidence_ids": ["gx"],
                              "candidate_evidence_union": ["ev1"],
                              "covered_source_count": 0, "candidate_covered": False}
    if missing:
        del s[missing]
    s["content_digest"] = digest_of(s)
    return s


def write_jsonl(path, rows):
    with open(path, "w") as f:
        for r in rows:
            f.write(json.dumps(r) + "\n")


class TestParseNeed(unittest.TestCase):
    def test_entities_and_time(self):
        need = parse_need("When did Dana buy a server in May 2026?",
                          "May 2026.", STOPWORDS_A)
        self.assertEqual(need["entities"], ["Dana"])
        self.assertIn("2026-05", need["time_constraints"])
        self.assertEqual(need["update_state"], "")

    def test_count_and_update(self):
        need = parse_need("How many servers does Dana currently own?", "2", STOPWORDS_A)
        self.assertTrue(need["list_cardinality"]["known"])
        self.assertEqual(need["list_cardinality"]["count"], 2)
        self.assertEqual(need["update_state"], "currently")


class TestLabelActions(unittest.TestCase):
    def test_keep_required_only(self):
        line = {"query": "What project does Dana maintain?", "gold_answer": "homelab",
                "candidates": [
                    {"id": "e1", "rank": 0, "text": "Dana runs homelab.",
                     "source_ids": ["ev1"]},
                    {"id": "e2", "rank": 1, "text": "Dana bought a server.",
                     "source_ids": ["ev2"]}],
                "gold_coverage": {"gold_source_evidence_ids": ["ev1"]}}
        need = parse_need(line["query"], line["gold_answer"], STOPWORDS_A)
        actions = label_actions(line, need, cap_chars=5000)
        self.assertEqual(actions, [{"kind": "KEEP", "candidate_id": "e1", "source_id": "ev1"}])

    def test_uncovered_records_gap(self):
        line = {"query": "What restaurant does Mei own?", "gold_answer": "Lan",
                "candidates": [{"id": "e9", "rank": 0, "text": "Mei opened a bakery.",
                                "source_ids": ["ev9"]}],
                "gold_coverage": {"gold_source_evidence_ids": ["gx"],
                                  "candidate_evidence_union": ["ev9"],
                                  "covered_source_count": 0,
                                  "candidate_covered": False}}
        label = labeler_a(line)
        self.assertEqual(label["actions"], [])
        self.assertIsNotNone(label["need"]["gap"])
        self.assertEqual(label["need"]["gap"]["kind"], "entity")


class TestSplit(unittest.TestCase):
    def test_conversation_isolation(self):
        self.assertEqual(assign_split("c0", 0), assign_split("c0", 0))
        # different conversations may still collide by chance; the guarantee is
        # determinism within a conversation.
        self.assertEqual(assign_split("c7", 42), assign_split("c7", 42))


class TestDualLabelerAdjudicate(unittest.TestCase):
    def test_adjudication_union(self):
        line = sample("c0", "What project does Dana maintain?", "homelab")
        a = labeler_a(line)
        b = labeler_b(line)
        label, changed = adjudicate(a, b)
        # Every entity both labelers found must survive the adjudicator.
        for src in (a, b):
            for e in src["need"]["entities"]:
                self.assertIn(e, label["need"]["entities"])
        self.assertIn("Dana", label["need"]["entities"])


class TestRebuildCheck(unittest.TestCase):
    def test_identical_and_mutated(self):
        with tempfile.TemporaryDirectory() as d:
            p1 = os.path.join(d, "a.jsonl")
            p2 = os.path.join(d, "b.jsonl")
            rows = [sample(f"c{i}", "What project does Dana maintain?", "homelab",
                           split="train" if i % 5 else "validation") for i in range(10)]
            write_jsonl(p1, rows)
            write_jsonl(p2, rows)
            ok, _ = rc.compare(rc.load_lines(p1), rc.load_lines(p2))
            self.assertTrue(ok, "identical builds must be 100% consistent")

            bad = [dict(r) for r in rows]
            bad[3]["split"] = "validation" if bad[3]["split"] == "train" else "train"
            write_jsonl(p2, bad)
            ok, report = rc.compare(rc.load_lines(p1), rc.load_lines(p2))
            self.assertFalse(ok)
            self.assertEqual(report["split_assignment"]["mismatch_count"], 1)


class TestDataBuildParse(unittest.TestCase):
    """data_build.py model-output parsing must tolerate JSON arrays, JSONL (the
    Qwen2.5-7B behavior under the full prompt), fences, and surrounding prose."""

    def test_parse_turns_jsonl_and_array(self):
        t = db._parse_turns('{"speaker":"assistant","text":"Hi"}\n'
                            '{"speaker":"user","text":"Sure"}')
        self.assertEqual([x["speaker"] for x in t], ["assistant", "user"])
        t2 = db._parse_turns('[{"speaker":"user","text":"a"},{"speaker":"assistant","text":"b"}]')
        self.assertEqual([x["text"] for x in t2], ["a", "b"])

    def test_parse_turns_fenced(self):
        t = db._parse_turns('```json\n[{"speaker":"user","text":"x"}]\n```')
        self.assertEqual(t[0]["text"], "x")

    def test_parse_query_embedded_and_jsonl(self):
        q = db._parse_query('here is my answer {"question":"q","answer":"a",'
                            '"source_turn_ids":["t0-0-0"]} hope that helps')
        self.assertEqual(q["query"], "q")
        q2 = db._parse_query('{"question":"bad","answer":"","source_turn_ids":[]}\n'
                             '{"question":"q3","answer":"a3","source_turn_ids":["t1"]}')
        self.assertEqual(q2["query"], "q3")  # skips incomplete object


class TestCorpusAdapter(unittest.TestCase):
    def _tmp(self, name, text):
        path = os.path.join(tempfile.mkdtemp(), name)
        with open(path, "w") as f:
            f.write(text)
        return path

    def test_ultrachat_convert(self):
        fixture = (
            '{"messages":[{"from":"user","value":"hi"},{'
            '"from":"assistant","value":"hello there"}]}\n'
            '{"messages":[{"from":"user","value":""}]}\n'
        )
        convos = ca.convert_ultrachat(self._tmp("u.jsonl", fixture))
        self.assertEqual(len(convos), 1)  # empty-message line yields no convo
        turns = convos[0]["sessions"][0]["turns"]
        self.assertEqual([t["speaker"] for t in turns], ["user", "assistant"])
        self.assertEqual([t["text"] for t in turns], ["hi", "hello there"])
        self.assertTrue(all(t["turn_id"].startswith("ultrachat-") for t in turns))

    def test_oasst_thread_linearization(self):
        fixture = "\n".join([
            '{"message_id":"m1","parent_id":null,"role":"user","text":"root"}',
            '{"message_id":"m2","parent_id":"m1","role":"assistant","text":"reply"}',
            '{"message_id":"m3","parent_id":"m2","role":"user","text":"follow"}',
            '{"message_id":"m4","parent_id":null,"role":"assistant","text":"second-root"}',
        ])
        convos = ca.convert_oasst(self._tmp("o.jsonl", fixture))
        self.assertEqual(len(convos), 1)  # only root user threads; m4 is assistant root
        turns = convos[0]["sessions"][0]["turns"]
        self.assertEqual([t["speaker"] for t in turns], ["user", "assistant", "user"])
        self.assertEqual([t["text"] for t in turns], ["root", "reply", "follow"])


class TestReview(unittest.TestCase):
    def test_wilson_ci(self):
        lo, hi = rv.wilson_ci(190, 200)
        self.assertLess(lo, 0.95)  # CI lower bound below point estimate
        self.assertLess(hi, 1.0)

    def test_gate_met_at_95(self):
        rows = [{"semantic_sufficiency": "pass"} for _ in range(190)] + \
               [{"semantic_sufficiency": "fail"} for _ in range(10)]
        s = rv.summarize(rows)
        self.assertEqual(s["scored"], 200)
        self.assertGreaterEqual(s["rate"], 0.95)
        self.assertTrue(s["gate_rate_ge_95"])

    def test_draw_sample_stratified(self):
        rows = [sample(f"c{i}", "q", "a",
                       cat="temporal" if i % 2 else "single-hop",
                       split="validation" if i % 5 == 0 else "train")
                for i in range(50)]
        out = rv.draw_sample(rows, 20, 7)
        self.assertEqual(len(out), 20)
        self.assertLessEqual({r["category"] for r in out}, {"temporal", "single-hop"})


class TestAudit(unittest.TestCase):
    def _audit(self, rows, bench_rows=None):
        with tempfile.TemporaryDirectory() as d:
            train = os.path.join(d, "train.jsonl")
            write_jsonl(train, rows)
            bench = os.path.join(d, "bench.json")
            if bench_rows is not None:
                with open(bench, "w") as f:
                    json.dump(bench_rows, f)
            return audit_mod.audit(train, [bench] if bench_rows is not None else [])

    def test_clean_passes(self):
        rows = [sample("c0", "What project does Dana maintain?", "homelab"),
                sample("c1", "When did Dana buy a server?", "May 2026", cat="temporal", split="validation")]
        r = self._audit(rows)
        self.assertFalse(r["blocking"])
        self.assertEqual(r["schema"]["digest_mismatch_count"], 0)

    def test_blocking_classes(self):
        sent = "Dana's favorite book is The Moon is a Harsh Mistress."
        rows = [
            sample("c0", "What project does Dana maintain?", "homelab"),
            sample("c1", "When did Dana buy a server?", "May 2026", cat="temporal", split="validation"),
            sample("c2", "What is Dana's favorite book?", sent, cat="open-domain", split="train"),
            # c3 tests the license-missing blocker only; its query deliberately
            # avoids the benchmark sentence shape so contamination stays isolated.
            sample("c3", "What plan is Dana working on?", "plan", missing="license"),
            sample("c4", "Dana runs the homelab server stack every weekend morning.", "stack", split="train"),
            sample("c5", "Dana runs the homelab server stack every weekend morning.", "stack", split="validation"),
        ]
        r = self._audit(rows, bench_rows=[{"qa": [{"question": "What is Dana's favorite book?",
                                                   "answer": sent, "evidence": []}]}])
        self.assertTrue(r["blocking"])
        self.assertEqual(r["provenance"]["missing_field_count"], 1)
        self.assertTrue(r["license"]["invalid"])
        self.assertEqual(len(r["near_dup"]["hits"]), 1)
        contam = [h["id"] for h in r["contamination"]["hits"]]
        self.assertEqual(contam, ["023-btest-r1-c2-q0"], "only the verbatim copy is contaminated")


if __name__ == "__main__":
    unittest.main()
