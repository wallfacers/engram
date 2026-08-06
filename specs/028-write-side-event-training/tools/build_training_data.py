#!/usr/bin/env python3
"""028 US2 training-data builder (contracts/training-data-schema.md).

Combines teacher-extracted events (teacher_extract.py Project JSON) with the
source messages (locomo.json) into event-level training samples: each sample is
(message + context -> time-anchored dual-perspective event JSON). Abs-time is
the supervision label. Output JSONL + audit.json (FR-002 auditable).
"""
import json
import os
import re
import sys

ISO_DATE = re.compile(r"^\d{4}-\d{2}-\d{2}$")
VALID_RT = ("interpersonal", "causal", "co_participation", "temporal_order", "preference")


def validate_sample(s, samples):
    """Replay of contracts/training-data-schema.md 5 rules. Returns (ok, reason)."""
    if s.get("id") in samples:
        return False, "dup_id"
    if not s.get("conv_id") or not s.get("source_msg_id"):
        return False, "missing_ref"
    ev = s.get("event_json")
    if not isinstance(ev, dict) or not ev.get("fact_entries"):
        return False, "bad_event_json"
    ts = ev.get("absolute_ts") or ""
    if ts and not ISO_DATE.match(ts):
        return False, "bad_abs_time"
    has_time = bool(ev.get("relative_ref")) or any(
        re.search(r"(yesterday|today|tomorrow|last\s+\w+|ago|recently|earlier|later)", f.get("text", ""), re.I)
        for f in ev.get("fact_entries", []))
    if has_time and not s.get("abs_time_label"):
        return False, "time_semantic_no_label"
    if s.get("source") == "human_refined" and not s.get("revision_notes"):
        return False, "human_no_revision"
    for r in ev.get("relation_entries", []):
        if r.get("relation_type") not in VALID_RT:
            return False, "unknown_relation_type"
    return True, "ok"


_SDATE_RE = re.compile(r"on\s+(\d{1,2})\s+(\w+)\s*,\s*(\d{4})", re.I)
_MONTHS = {m: i for i, m in enumerate(["january", "february", "march", "april", "may", "june",
                                       "july", "august", "september", "october", "november", "december"], 1)}


def _parse_sdate(raw):
    """'8:56 pm on 20 July, 2023' -> '2023-07-20' (ISO; empty if unresolvable)."""
    if not raw:
        return ""
    m = _SDATE_RE.search(raw)
    if not m:
        return ""
    mon = _MONTHS.get(m.group(2).lower())
    if not mon:
        return ""
    return f"{m.group(3)}-{mon:02d}-{int(m.group(1)):02d}"


def load_message_index(data_path):
    """'<conv_id>:<dia_id>' -> (speaker, session_date, text, context_prev3).

    LoCoMo re-numbers dia_ids per conversation (each conv has its own D1:1..),
    so the key MUST include the conv_id or later convs would shadow earlier ones.
    """
    idx = {}
    convs = json.load(open(data_path))
    for ci, conv in enumerate(convs):
        conv_id = f"conv-{ci}"
        conv_data = conv["conversation"]
        sessions = sorted((k for k in conv_data if re.fullmatch(r"session_\d+", k)),
                          key=lambda k: int(k.split("_")[1]))
        for sk in sessions:
            n = int(sk.split("_")[1])
            sdate = _parse_sdate(conv_data.get(f"session_{n}_date_time") or "")
            turns = conv_data[sk]
            for i, t in enumerate(turns):
                text = (t.get("text") or "").strip()
                if not text:
                    continue
                ctx = [f"{u.get('speaker','?')}: {u.get('text','')}" for u in turns[max(0, i - 3):i] if u.get("text")]
                idx[f"{conv_id}:{t.get('dia_id')}"] = {
                    "conv_id": conv_id,
                    "speaker": t.get("speaker", "?"),
                    "session_date": sdate,
                    "text": text,
                    "context": ctx,
                }
    return idx


def build(project_path, data_path, out_jsonl, out_audit, human_refined_path=None):
    proj = json.load(open(project_path))
    events = proj.get("events", [])
    msg_idx = load_message_index(data_path)
    seen, samples = set(), []
    for i, ev in enumerate(events):
        sid = (ev.get("source_ledger_ids") or [""])[0]
        m = msg_idx.get(f"{ev.get('conversation_id')}:{sid}")
        if not m:
            continue
        sample = {
            "id": f"train-{i:05d}",
            "conv_id": m["conv_id"],
            "source_msg_id": sid,
            "session_date": m["session_date"],
            "input_text": f"{m['speaker']}: {m['text']}",
            "context_turns": m["context"],
            "event_json": ev,
            "abs_time_label": ev.get("absolute_ts") or "",
            "source": "teacher",
            "revised": False,
            "revision_notes": "",
        }
        ok, reason = validate_sample(sample, seen)
        if not ok:
            continue
        seen.add(sample["id"])
        samples.append(sample)

    # merge human-refined overrides (same source_msg_id)
    if human_refined_path and os.path.exists(human_refined_path):
        by_sid = {}
        for line in open(human_refined_path):
            r = json.loads(line)
            by_sid[r["source_msg_id"]] = r
        for s in samples:
            r = by_sid.get(s["source_msg_id"])
            if r:
                s["event_json"] = r["event_json"]
                s["abs_time_label"] = r.get("abs_time_label", s["abs_time_label"])
                s["source"] = "human_refined"
                s["revised"] = True
                s["revision_notes"] = r.get("revision_notes", "")

    with open(out_jsonl, "w") as f:
        for s in samples:
            f.write(json.dumps(s, ensure_ascii=False) + "\n")

    has_time = [s for s in samples if s["abs_time_label"] or s["event_json"].get("relative_ref")]
    anchored = [s for s in samples if s["abs_time_label"]]
    refined = [s for s in samples if s["revised"]]
    audit = {
        "n_samples": len(samples),
        "n_time_semantic": len(has_time),
        "time_anchor_rate": round(len(anchored) / len(has_time), 4) if has_time else 0,
        "n_human_refined": len(refined),
        "refine_rate": round(len(refined) / len(samples), 4) if samples else 0,
        "by_conv": {},
    }
    for s in samples:
        audit["by_conv"][s["conv_id"]] = audit["by_conv"].get(s["conv_id"], 0) + 1
    with open(out_audit, "w") as f:
        json.dump(audit, f, ensure_ascii=False, indent=2)
    print(json.dumps(audit, ensure_ascii=False, indent=2))


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--project", required=True, help="teacher_extract.py Project JSON")
    ap.add_argument("--data", required=True, help="locomo.json")
    ap.add_argument("--out", required=True, help="training JSONL")
    ap.add_argument("--human-refined", help="optional human-refined JSONL (overrides by source_msg_id)")
    args = ap.parse_args()
    build(args.project, args.data, args.out, args.out + ".audit.json", args.human_refined)


if __name__ == "__main__":
    main()
