#!/usr/bin/env python3
"""Unit tests for build_training_data.py validate_sample (contract
training-data-schema.md 5 rules)."""
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from build_training_data import validate_sample


def mk(**over):
    s = {
        "id": "train-0001",
        "conv_id": "conv-0",
        "source_msg_id": "D1:3",
        "event_json": {
            "conversation_id": "conv-0", "source_ledger_ids": ["D1:3"], "speaker": "Caroline",
            "fact_entries": [{"text": "Caroline went to a support group on 2023-05-07", "grounded": True}],
            "relation_entries": [], "absolute_ts": "2023-05-07", "relative_ref": "yesterday",
        },
        "abs_time_label": "2023-05-07",
        "source": "teacher",
        "revised": False,
        "revision_notes": "",
    }
    s.update(over)
    return s


def test_ok():
    ok, reason = validate_sample(mk(), set())
    assert ok and reason == "ok", (ok, reason)
    print("ok valid")


def test_dup_id():
    seen = {"train-0001"}
    ok, reason = validate_sample(mk(), seen)
    assert not ok and reason == "dup_id"
    print("ok dup_id")


def test_time_semantic_no_label():
    s = mk(abs_time_label="")
    ok, reason = validate_sample(s, set())
    assert not ok and reason == "time_semantic_no_label", reason
    # no time semantics -> empty label is fine
    s2 = mk(abs_time_label="", event_json={"conversation_id": "conv-0", "source_ledger_ids": ["D1:3"],
                                           "speaker": "A", "fact_entries": [{"text": "A likes coffee", "grounded": True}],
                                           "relation_entries": [], "absolute_ts": "", "relative_ref": ""})
    ok2, reason2 = validate_sample(s2, set())
    assert ok2, reason2
    print("ok time_semantic_no_label")


def test_bad_abs_time():
    s = mk(abs_time_label="2023/05/07", event_json={**mk()["event_json"], "absolute_ts": "2023/05/07"})
    ok, reason = validate_sample(s, set())
    assert not ok and reason == "bad_abs_time"
    print("ok bad_abs_time")


def test_human_requires_revision():
    s = mk(source="human_refined", revised=True, revision_notes="")
    ok, reason = validate_sample(s, set())
    assert not ok and reason == "human_no_revision"
    s2 = mk(source="human_refined", revision_notes="fixed date")
    ok2, _ = validate_sample(s2, set())
    assert ok2
    print("ok human_requires_revision")


def test_unknown_relation_type():
    ev = mk()["event_json"]
    ev["relation_entries"] = [{"relation_type": "praise", "subject": "A", "object": "B", "text": "x"}]
    ok, reason = validate_sample(mk(event_json=ev), set())
    assert not ok and reason == "unknown_relation_type"
    print("ok unknown_relation_type")


if __name__ == "__main__":
    test_ok()
    test_dup_id()
    test_time_semantic_no_label()
    test_bad_abs_time()
    test_human_requires_revision()
    test_unknown_relation_type()
    print("ALL PASS")
