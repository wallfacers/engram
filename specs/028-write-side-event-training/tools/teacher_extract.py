#!/usr/bin/env python3
"""028 US1 teacher event extraction (contracts/teacher-extract-prompt.md).

Reads LoCoMo conversations, calls a hosted teacher model (DeepSeek-v4-pro) with
the time-anchoring prompt, and writes one 027 Project JSON (memory/eventstore
project.go serialization) whose events the harness can load via
`--representation event --event-project`.

Conversation ids are harness indices (conv-0..conv-9, array order in locomo.json),
matching 027's build-event so renderEventHitsForQuery can locate events.
Fail-closed: a schema-invalid or model-error message is skipped (stats counted);
the raw-chunk path remains the fallback. Secret (API key) flows via env only.
"""
import json
import os
import re
import sys
import time
import urllib.request
from concurrent.futures import ThreadPoolExecutor, as_completed
from datetime import datetime, timezone

BASE_URL = os.environ.get("TEACHER_BASE_URL", "https://api.deepseek.com/chat/completions")
DEFAULT_MODEL = "deepseek-v4-pro"

SYSTEM_PROMPT = """You extract structured events from a conversation message for a long-term memory system.

Respond with ONLY JSON, no prose, no markdown:
{
  "fact_entries": [{"text": "self-contained fact", "grounded": true}],
  "relation_entries": [{"relation_type": "interpersonal|causal|co_participation|temporal_order|preference", "subject": "...", "object": "...", "text": "..."}],
  "absolute_ts": "YYYY-MM-DD",
  "relative_ref": "original relative phrase"
}

RULES:
1. fact_entries = what happened. Make each fact self-contained (resolve pronouns, drop noise). Each fact text <= 500 runes.
2. relation_entries = relations between entities: causal (A led to B), co_participation (A and B did X together), temporal_order (A happened before/after B), interpersonal, preference. Each subject/object/text non-empty.
3. TIME ANCHORING (MANDATORY): the current session date is given as "[session date: YYYY-MM-DD]". Convert EVERY relative time expression to an ABSOLUTE date relative to that session date.
   Example: [session date: 2023-05-08], message says "last Saturday" -> absolute_ts "2023-05-06", relative_ref "last Saturday"; "yesterday" -> "2023-05-07".
   - When the message has ANY time semantics (yesterday/today/last week/ago/etc.), absolute_ts is REQUIRED and MUST NOT be empty.
   - Put the absolute date in absolute_ts (YYYY-MM-DD); keep the original phrase in relative_ref as a trace.
   - A fact about WHEN something happened MUST carry the absolute date in its text.
   - Only leave absolute_ts empty when the date is truly unresolvable from the context AND session date.
4. Total event JSON payload <= 2000 runes.
5. absolute_ts format MUST be YYYY-MM-DD."""


def build_user_prompt(source_id, session_date, speaker, context_turns, msg_text):
    lines = [f"[source_id={source_id}]"]
    if session_date:
        lines.append(f"[session date: {session_date}]")
    lines.append(f"Speaker: {speaker}")
    if context_turns:
        lines.append("Previous context:")
        for s in context_turns[-4:]:
            lines.append(f"  {s}")
    lines.append(f"Message: {speaker}: {msg_text}")
    return "\n".join(lines)


def call_teacher(key, model, msg_text, max_tokens=2000, temperature=0.2, retries=2):
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": msg_text},
        ],
        "max_tokens": max_tokens,
        "temperature": temperature,
    }
    last_err = None
    for attempt in range(retries + 1):
        req = urllib.request.Request(
            BASE_URL,
            data=json.dumps(body).encode(),
            headers={"Authorization": "Bearer " + key, "Content-Type": "application/json"},
        )
        try:
            with urllib.request.urlopen(req, timeout=180) as r:
                data = json.loads(r.read().decode())
            return data["choices"][0]["message"]["content"]
        except Exception as e:
            last_err = e
            time.sleep(1 + attempt * 2)
    raise last_err


_SESSION_DATE = re.compile(r"on\s+(\d{1,2})\s+(\w+)\s*,\s*(\d{4})", re.I)
_MONTHS = {m: i for i, m in enumerate(["january", "february", "march", "april", "may", "june",
                                       "july", "august", "september", "october", "november", "december"], 1)}


def parse_session_date(raw):
    """'10:04 am on 19 December, 2023' -> '2023-12-19' (or None)."""
    if not raw:
        return None
    m = _SESSION_DATE.search(raw)
    if not m:
        return None
    day, mon, year = m.group(1), _MONTHS.get(m.group(2).lower()), m.group(3)
    if not mon:
        return None
    return f"{year}-{mon:02d}-{int(day):02d}"


def clean_json(raw):
    raw = raw.strip()
    m = re.search(r"\{.*\}", raw, re.S)
    if not m:
        return None
    return m.group(0)


def validate_lenient(ev):
    """Validate the MODEL OUTPUT portion only (facts/relations/timing).

    conversation_id/source_ledger_ids/speaker are injected by this script after
    validation (the model is not told to emit them). Relation entries with an
    unknown relation_type are dropped (027 ValidateLenient behavior).
    """
    if not isinstance(ev, dict):
        return None
    facts = ev.get("fact_entries") or []
    if not facts or not all(isinstance(f, dict) and f.get("text") for f in facts):
        return None
    total = sum(len(f.get("text", "")) for f in facts)
    kept = []
    for r in ev.get("relation_entries") or []:
        if not isinstance(r, dict):
            continue
        if r.get("relation_type") not in ("interpersonal", "causal", "co_participation", "temporal_order", "preference"):
            continue
        if not (r.get("subject") and r.get("object") and r.get("text")):
            continue
        kept.append(r)
        total += len(r["text"]) + len(r["subject"]) + len(r["object"])
    ev["relation_entries"] = kept
    if total > 2000:
        return None
    return ev


def extract_one(key, model, job):
    """Returns (event_dict or None, error_str or None).

    model_call / parse / json failures are retried once (hosted-model output
    instability); a schema failure (no events in a chit-chat message) is a
    legitimate fail-closed skip and is NOT retried.
    """
    prompt = build_user_prompt(job["source_id"], job["session_date"], job["speaker"], job["context"], job["text"])
    for attempt in range(2):
        try:
            raw = call_teacher(key, model, prompt)
        except Exception as e:
            if attempt == 0:
                continue
            return None, f"model_call:{e}"
        parsed = clean_json(raw)
        if not parsed:
            if attempt == 0:
                continue
            return None, "parse"
        try:
            ev = json.loads(parsed)
        except Exception:
            if attempt == 0:
                continue
            return None, "json"
        ev = validate_lenient(ev)
        if ev is None:
            return None, "schema"  # no-event message: fail-closed, don't retry
        ev["event_id"] = f"evt-{job['idx']}"
        ev["conversation_id"] = job["conv_id"]
        ev["source_ledger_ids"] = [job["source_id"]]
        ev["speaker"] = job["speaker"]
        return ev, None
    return None, "parse"


def load_jobs(data_path):
    convs = json.load(open(data_path))
    jobs = []
    for idx, conv in enumerate(convs):
        conv_id = f"conv-{idx}"
        conv_data = conv["conversation"]
        sessions = sorted(
            (k for k in conv_data.keys() if re.fullmatch(r"session_\d+", k)),
            key=lambda k: int(k.split("_")[1]),
        )
        # one session date per session (session_N_date_time)
        for sk in sessions:
            n = int(sk.split("_")[1])
            date_key = f"session_{n}_date_time"
            sdate = parse_session_date(conv_data.get(date_key) or "")
            turns = conv_data[sk]
            for t in turns:
                text = (t.get("text") or "").strip()
                if not text:
                    continue
                jobs.append({
                    "idx": len(jobs),
                    "conv_id": conv_id,
                    "source_id": t.get("dia_id", f"D{n}:?"),
                    "session_date": sdate,
                    "speaker": t.get("speaker", "?"),
                    "text": text,
                    "context": [],
                })
    return jobs


def main():
    import argparse
    ap = argparse.ArgumentParser()
    ap.add_argument("--data", required=True, help="locomo.json")
    ap.add_argument("--out", required=True, help="output Project JSON")
    ap.add_argument("--model", default=DEFAULT_MODEL)
    ap.add_argument("--limit", type=int, default=0, help="only first N messages (0 = all)")
    ap.add_argument("--workers", type=int, default=4)
    args = ap.parse_args()

    key = os.environ.get("TEACHER_API_KEY") or os.environ.get("JUDGE_API_KEY")
    if not key:
        sys.exit("TEACHER_API_KEY or JUDGE_API_KEY env required (source ~/.config/engram/judge.env)")

    jobs = load_jobs(args.data)
    if args.limit:
        jobs = jobs[: args.limit]
    print(f"jobs={len(jobs)} model={args.model} workers={args.workers}")

    events, stats = [], {"attempts": 0, "successes": 0, "failures": 0, "schema": 0, "model_call": 0, "parse": 0, "json": 0}
    t0 = time.time()
    with ThreadPoolExecutor(max_workers=args.workers) as ex:
        futs = {ex.submit(extract_one, key, args.model, j): j for j in jobs}
        for f in as_completed(futs):
            ev, err = f.result()
            stats["attempts"] += 1
            if ev is not None:
                events.append(ev)
                stats["successes"] += 1
            else:
                stats["failures"] += 1
                stats[err.split(":")[0]] += 1
            if stats["attempts"] % 50 == 0:
                print(f"  {stats['attempts']}/{len(jobs)} elapsed={time.time()-t0:.0f}s", flush=True)

    # deterministic order by source id for a stable render
    events.sort(key=lambda e: ",".join(e["source_ledger_ids"]))
    project = {
        "config_hash": "028-teacher-v1",
        "conversation_id": "all",
        "built_at": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "events": events,
        "summaries": [],
    }
    os.makedirs(os.path.dirname(os.path.abspath(args.out)), exist_ok=True)
    with open(args.out, "w") as f:
        json.dump(project, f, ensure_ascii=False, indent=2)

    anchored = sum(1 for e in events if e.get("absolute_ts"))
    print(json.dumps({"events": len(events), "attempts": stats["attempts"], "successes": stats["successes"],
                      "failures": stats["failures"], "schema_fail": stats["schema"], "model_fail": stats["model_call"],
                      "time_anchor_rate": round(anchored / len(events), 4) if events else 0,
                      "elapsed_s": round(time.time() - t0, 1)}, indent=2))


if __name__ == "__main__":
    main()
