#!/usr/bin/env python3
"""028 US2/Foundational audit (data-model.md E3): time-anchor rate, schema
legal rate, hallucination sample, category counts. Input is a 027 Project JSON
or a bare event array. Pure offline, no API calls.
"""
import json
import re
import sys

TIME_WORDS = re.compile(
    r"\b(yesterday|today|tomorrow|last\s+(week|month|year|night|friday|saturday|sunday|monday|tuesday|wednesday|thursday|weekend)|"
    r"this\s+(week|month|year|morning|afternoon|evening|night|weekend)|"
    r"next\s+(week|month|year|weekend|friday|saturday|sunday|monday|tuesday|wednesday|thursday)|"
    r"ago|recently|earlier|later|a few days|a couple of (weeks|days|months))\b",
    re.I,
)
VALID_RT = ("interpersonal", "causal", "co_participation", "temporal_order", "preference")
ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")


def has_time_semantics(ev):
    for f in ev.get("fact_entries") or []:
        if TIME_WORDS.search(f.get("text", "")):
            return True
    for r in ev.get("relation_entries") or []:
        if TIME_WORDS.search(r.get("text", "")):
            return True
    if ev.get("relative_ref"):
        return True
    return False


def schema_legal(ev):
    """Replay of 027 ValidateLenient (memory/eventstore/event.go)."""
    if not ev.get("conversation_id") or not ev.get("speaker"):
        return False
    if not ev.get("source_ledger_ids"):
        return False
    facts = ev.get("fact_entries") or []
    if not facts or not all(f.get("text") for f in facts):
        return False
    total = sum(len(f.get("text", "")) for f in facts)
    for r in ev.get("relation_entries") or []:
        if r.get("relation_type") not in VALID_RT:
            return False
        if not (r.get("subject") and r.get("object") and r.get("text")):
            return False
        total += len(r["text"]) + len(r["subject"]) + len(r["object"])
    if total > 2000:
        return False
    return True


def audit(events, sample_n=8):
    n = len(events)
    anchored = [e for e in events if e.get("absolute_ts")]
    sem = [e for e in events if has_time_semantics(e)]
    sem_anchored = [e for e in sem if e.get("absolute_ts")]
    legal = [e for e in events if schema_legal(e)]
    bad_ts = [e for e in events if e.get("absolute_ts") and not ISO_DATE.match(e["absolute_ts"])]
    cats = {}
    for e in events:
        c = e.get("conversation_id", "?")
        cats[c] = cats.get(c, 0) + 1
    return {
        "n_events": n,
        "n_anchored": len(anchored),
        "time_anchor_rate_raw": round(len(anchored) / n, 4) if n else 0,
        "n_time_semantic": len(sem),
        "time_anchor_rate_semantic": round(len(sem_anchored) / len(sem), 4) if sem else 0,
        "n_schema_legal": len(legal),
        "schema_legal_rate": round(len(legal) / n, 4) if n else 0,
        "n_bad_ts_format": len(bad_ts),
        "by_conv": cats,
        "sample": [{"event_id": e.get("event_id"), "conv": e.get("conversation_id"),
                     "absolute_ts": e.get("absolute_ts"), "relative_ref": e.get("relative_ref"),
                     "facts": [f["text"] for f in e.get("fact_entries", [])][:2]} for e in events[:sample_n]],
    }


def load_events(path):
    data = json.load(open(path))
    if isinstance(data, list):
        return data
    return data.get("events", [])


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else None
    if not path:
        sys.exit("usage: audit_anchoring.py <events.json or project.json>")
    events = load_events(path)
    print(json.dumps(audit(events), ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
