#!/usr/bin/env python3
"""Unit tests for audit_anchoring.py — synthetic event arrays verify counting."""
import sys
import os
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from audit_anchoring import audit, has_time_semantics, schema_legal, ISO_DATE


def mk(ts=None, rel=None, fact="plain fact", relations=None, source="D1:1", conv="conv-0"):
    return {
        "event_id": "e1", "conversation_id": conv, "source_ledger_ids": [source], "speaker": "A",
        "fact_entries": [{"text": fact, "grounded": True}],
        "relation_entries": relations or [],
        "absolute_ts": ts, "relative_ref": rel,
    }


def test_anchor_rate_raw_and_semantic():
    evs = [
        mk(ts="2023-01-06", rel="last Friday", fact="A went to the agency last Friday"),
        mk(ts=None, rel="yesterday", fact="A did X yesterday"),           # time semantic, no abs -> miss
        mk(ts=None, fact="A likes coffee"),                                # no time semantics
        mk(ts="2023-05-07", fact="A ran a race"),                          # anchored but no relative ref
    ]
    a = audit(evs)
    assert a["n_events"] == 4
    assert a["n_anchored"] == 2 and a["time_anchor_rate_raw"] == 0.5
    assert a["n_time_semantic"] == 2
    assert a["time_anchor_rate_semantic"] == 0.5  # 1 of 2 semantic events anchored
    assert a["schema_legal_rate"] == 1.0
    print("ok anchor_rate")


def test_has_time_semantics():
    assert has_time_semantics(mk(rel="yesterday"))
    assert has_time_semantics(mk(fact="last week we hiked"))
    assert not has_time_semantics(mk(fact="A likes coffee"))
    print("ok time_semantics")


def test_schema_legal_invalid():
    bad = mk(relations=[{"relation_type": "praise", "subject": "A", "object": "B", "text": "x"}])
    assert not schema_legal(bad)          # unknown relation_type
    no_facts = {"conversation_id": "conv-0", "source_ledger_ids": ["D1:1"], "speaker": "A",
                "fact_entries": [], "relation_entries": []}
    assert not schema_legal(no_facts)     # empty fact_entries
    no_src = mk(); del no_src["source_ledger_ids"]
    assert not schema_legal(no_src)       # missing source
    print("ok schema_legal")


def test_bad_ts_format_detected():
    evs = [mk(ts="2023/01/06"), mk(ts="2023-01-06")]
    a = audit(evs)
    assert a["n_bad_ts_format"] == 1
    assert ISO_DATE.match("2023-01-06")
    assert not ISO_DATE.match("2023/01/06")
    print("ok bad_ts")


if __name__ == "__main__":
    test_anchor_rate_raw_and_semantic()
    test_has_time_semantics()
    test_schema_legal_invalid()
    test_bad_ts_format_detected()
    print("ALL PASS")
