#!/usr/bin/env python3
"""023 synthetic conversation generator (V1 training data bootstrap).

Generates fictional multi-session memory conversations with a LOCAL Qwen
sidecar (OpenAI-compatible; Ollama or vLLM). The conversations are the seed
for the Go planner-build tool, which ingests them into engram (offline),
builds the index, retrieves frozen candidates per query, and labels them via
the fixed-gold oracle + rules (see specs/023/data-model.md §3).

Offline-only, zero user/namespace data, zero benchmark contamination:
everything here is invented fiction by a local model. Paid teachers are never
used (FR-013).

Usage:
  python3 data_build.py \
      --base-url http://localhost:8000/v1 \
      --model Qwen2.5-7B-Instruct \
      --convos 200 --sessions 4 --out data/raw/convos.jsonl

Output: one JSON object per line in data/raw/convos.jsonl:
  {"conversation_id": "...", "persona": "...", "sessions": [
     {"session_id": "...", "date": "2026-..", "turns": [
        {"turn_id": "<conv_id>-<sess_idx>-<turn_idx>", "speaker":"user|assistant","text":"..."}]}],
   "queries": [
     {"question_id": "<conv_id>-q<j>", "query": "...", "type": "direct|time|multi_hop|update",
      "gold_answer": "...", "gold_source_turn_ids": ["<turn_id>", ...]}]}

Every turn carries a stable turn_id; query generation annotates each recall
question with the concise gold answer and the turn ids that contain its facts,
so the Go planner-build tool can run oracle coverage over the frozen candidates.
"""

import argparse
import json
import os
import random
import re
import sys
import urllib.request

# ---------------------------------------------------------------- generation

SYSTEM = (
    "You write fictional multi-session memory conversations for a memory-system "
    "benchmark. The user (human) and assistant (AI assistant) talk about a "
    "consistent set of facts across sessions: people, projects, dates, "
    "preferences, updates. Later sessions must reference or update facts from "
    "earlier ones (e.g. 'you told me last month that X, is that still true?'). "
    "Output ONLY a JSON array of turns: "
    '[{"speaker":"user|assistant","text":"..."}, ...] '
    "Natural spoken text, 3-10 turns per session, each turn 1-4 sentences. "
    "Include concrete dates, names, and numbers. Never use markdown fences."
)

def gen_sessions(client_cfg, persona, conv_id, n_sessions, seed):
    """Ask the local Qwen to write n_sessions coherent sessions for a persona."""
    rng = random.Random(f"{conv_id}:{seed}")
    sessions = []
    for i in range(n_sessions):
        date = f"2026-{rng.randint(1, 6):02d}-{rng.randint(1, 28):02d}"
        prompt = (
            f"Fictional persona: {persona}. "
            f"Session {i + 1} of {n_sessions}, dated {date}. "
            + ("This is the FIRST session: establish the core facts."
               if i == 0 else
               "Reference or update facts from earlier sessions. Keep continuity.")
        )
        user_msg = {"role": "user", "content": prompt}
        text = _chat_completion(client_cfg, SYSTEM, user_msg)
        turns = _parse_turns(text)
        if not turns:
            turns = [{"speaker": "user", "text": prompt},
                     {"speaker": "assistant", "text": "(generation failed)"}]
        # Stable per-turn id the Go planner-build tool can trace through engram.
        for j, t in enumerate(turns):
            t["turn_id"] = f"{conv_id}-{i}-{j}"
        sessions.append({"session_id": f"s{conv_id}-{i}", "date": date, "turns": turns})
    return sessions

def _chat_completion(cfg, system, user_msg):
    body = {
        "model": cfg["model"],
        "messages": [{"role": "system", "content": system}, user_msg],
        "temperature": 0.9,
        "max_tokens": 2048,
    }
    req = urllib.request.Request(
        cfg["base_url"].rstrip("/") + "/chat/completions",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=120) as resp:
        data = json.loads(resp.read().decode())
    return data["choices"][0]["message"]["content"]

def _parse_turns(text):
    """Extract a JSON array of turns from the model reply (tolerates fences)."""
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```[a-zA-Z]*\s*", "", text)
        text = re.sub(r"\s*```$", "", text)
    m = re.search(r"\[.*\]", text, re.S)
    if not m:
        return None
    try:
        turns = json.loads(m.group(0))
    except json.JSONDecodeError:
        return None
    if not isinstance(turns, list):
        return None
    out = []
    for t in turns:
        if not isinstance(t, dict) or t.get("speaker") not in ("user", "assistant"):
            continue
        txt = str(t.get("text", "")).strip()
        if txt:
            out.append({"speaker": t["speaker"], "text": txt})
    return out or None

# ------------------------------------------------------------------- queries

PERSONAS = [
    "a software engineer named Dana who maintains a home-lab project",
    "a restaurant owner named Mei managing bookings and suppliers",
    "a graduate student named Sam writing a thesis on distributed systems",
    "a product manager named Priya tracking a launch plan and team preferences",
    "a family coordinator named Alex planning trips and remembering birthdays",
]

def gen_queries(client_cfg, conv):
    """Generate recall queries for a conversation (direct/time/multi-hop/update).

    Each query carries the concise gold answer and the exact source turn ids so
    the Go planner-build tool can run oracle coverage (which candidates contain
    the answer's evidence) over the frozen retrieval output.
    """
    qtypes = ["direct", "time", "multi_hop", "update"]
    prompts = {
        "direct": "Ask a direct factual question answerable from one session.",
        "time": "Ask a time-bound question ('when did X happen?').",
        "multi_hop": "Ask a question requiring connecting facts across two sessions.",
        "update": "Ask about the current/latest state of something.",
    }
    queries = []
    for j, qt in enumerate(qtypes):
        prompt = (
            f"From this conversation, write one {qt} recall question "
            f"({prompts[qt]}) answerable ONLY from the given sessions.\n"
            "Output a single JSON object with EXACTLY these keys:\n"
            '{"question": "...", "answer": "<concise factual answer>", '
            '"source_turn_ids": ["<exact turn id>", ...]}\n'
            "Use ONLY the turn ids present in the conversation below. Never invent "
            "a turn id; the answer must be a plain fact stated in the source turns."
        )
        user_msg = {"role": "user", "content": prompt + "\n\n" + json.dumps(conv["sessions"], ensure_ascii=False)}
        text = _chat_completion(client_cfg, SYSTEM, user_msg)
        parsed = _parse_query(text)
        if parsed:
            parsed["type"] = qt
            parsed["question_id"] = f"{conv['conversation_id']}-q{j}"
            queries.append(parsed)
    return queries

def _parse_query(text):
    """Extract a {question, answer, source_turn_ids} object, tolerating fences."""
    text = text.strip()
    if text.startswith("```"):
        text = re.sub(r"^```[a-zA-Z]*\s*", "", text)
        text = re.sub(r"\s*```$", "", text)
    try:
        obj = json.loads(text)
    except json.JSONDecodeError:
        m = re.search(r"\{.*\}", text, re.S)
        if not m:
            return None
        try:
            obj = json.loads(m.group(0))
        except json.JSONDecodeError:
            return None
    if not isinstance(obj, dict):
        return None
    q = str(obj.get("question", "")).strip()
    a = str(obj.get("answer", "")).strip()
    src = obj.get("source_turn_ids")
    if not q or not a or not isinstance(src, list) or not src:
        return None
    src = [str(s).strip() for s in src if str(s).strip()]
    if not src:
        return None
    return {"query": q, "gold_answer": a, "gold_source_turn_ids": src}

# --------------------------------------------------------------------- main

def annotate_conv(conv, cfg):
    """Attach annotated queries to a conversation (shared by the synthetic path
    and --gen-queries-only). Returns (conversation, kept, dropped)."""
    valid_turn_ids = {t["turn_id"] for s in conv["sessions"] for t in s["turns"]}
    queries = gen_queries(cfg, conv)
    kept = []
    dropped = 0
    for q in queries:
        if set(q["gold_source_turn_ids"]) <= valid_turn_ids:
            kept.append(q)
        else:
            dropped += 1
    if kept:
        conv["queries"] = kept
    return conv, len(kept), dropped


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--base-url", default=os.environ.get("PLANNER_BASE_URL", "http://localhost:8000/v1"))
    ap.add_argument("--model", default=os.environ.get("PLANNER_MODEL", "Qwen2.5-7B-Instruct"))
    ap.add_argument("--convos", type=int, default=200)
    ap.add_argument("--sessions", type=int, default=4)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--out", default="data/raw/convos.jsonl")
    # Read an existing convos.jsonl (e.g. from corpus_adapter.py) and annotate
    # queries in place — same generation path as the synthetic build (T009).
    ap.add_argument("--gen-queries-only", default=None, metavar="CONVOS.JSONL")
    args = ap.parse_args()

    cfg = {"base_url": args.base_url, "model": args.model}

    if args.gen_queries_only:
        in_path = args.gen_queries_only
        out_path = args.out if args.out != "data/raw/convos.jsonl" else in_path
        total = kept = dropped = 0
        with open(in_path) as f:
            convos = [json.loads(line) for line in f if line.strip()]
        for conv in convos:
            _, k, dr = annotate_conv(conv, cfg)
            kept += k
            dropped += dr
        os.makedirs(os.path.dirname(out_path) or ".", exist_ok=True)
        with open(out_path, "w") as f:
            for c in convos:
                f.write(json.dumps(c, ensure_ascii=False) + "\n")
        print(f"annotated {kept} queries over {len(convos)} conversations "
              f"({dropped} dropped: bad turn ids) → {out_path}", file=sys.stderr)
        return 0

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    rng = random.Random(args.seed)

    n_queries = 0
    n_dropped = 0
    with open(args.out, "w") as f:
        for i in range(args.convos):
            conv_id = f"{args.seed}-{i:05d}"
            persona = rng.choice(PERSONAS)
            sessions = gen_sessions(cfg, persona, conv_id, args.sessions, args.seed)
            conv, kept, dropped = annotate_conv(
                {"conversation_id": conv_id, "persona": persona, "sessions": sessions}, cfg)
            n_queries += kept
            n_dropped += dropped
            f.write(json.dumps(conv, ensure_ascii=False) + "\n")
            if i % 10 == 0:
                print(f"generated {i}/{args.convos}", file=sys.stderr)
    print(f"wrote {args.convos} conversations to {args.out}; "
          f"{n_queries} queries kept, {n_dropped} dropped (bad turn ids)", file=sys.stderr)

    # The Go planner-build tool consumes convos.jsonl and emits candidates.jsonl.
    print("next: run the Go planner-build tool on this file "
          "(see specs/023/data-model.md §3)", file=sys.stderr)

if __name__ == "__main__":
    main()
