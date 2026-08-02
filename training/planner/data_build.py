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
     {"session_id": "...", "date": "2026-..", "turns": [{"speaker":"user|assistant","text":"..."}]}]}
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
    """Generate recall queries for a conversation (direct/time/multi-hop/update)."""
    qtypes = ["direct", "time", "multi_hop", "update"]
    prompts = {
        "direct": "Ask a direct factual question answerable from one session.",
        "time": "Ask a time-bound question ('when did X happen?').",
        "multi_hop": "Ask a question requiring connecting facts across two sessions.",
        "update": "Ask about the current/latest state of something.",
    }
    queries = []
    for qt in qtypes:
        prompt = (f"From this conversation, write one {qt} recall question "
                  f"({prompts[qt]}) answerable ONLY from the given sessions. "
                  "Output a single JSON string: \"the question\".")
        user_msg = {"role": "user", "content": prompt + "\n\n" + json.dumps(conv["sessions"], ensure_ascii=False)}
        text = _chat_completion(client_cfg, SYSTEM, user_msg)
        text = text.strip().strip('"').strip()
        if text:
            queries.append({"question": text, "type": qt})
    return queries

# --------------------------------------------------------------------- main

def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--base-url", default=os.environ.get("PLANNER_BASE_URL", "http://localhost:8000/v1"))
    ap.add_argument("--model", default=os.environ.get("PLANNER_MODEL", "Qwen2.5-7B-Instruct"))
    ap.add_argument("--convos", type=int, default=200)
    ap.add_argument("--sessions", type=int, default=4)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--out", default="data/raw/convos.jsonl")
    args = ap.parse_args()

    os.makedirs(os.path.dirname(args.out), exist_ok=True)
    cfg = {"base_url": args.base_url, "model": args.model}
    rng = random.Random(args.seed)

    seen = set()
    with open(args.out, "w") as f:
        for i in range(args.convos):
            conv_id = f"{args.seed}-{i:05d}"
            persona = rng.choice(PERSONAS)
            sessions = gen_sessions(cfg, persona, conv_id, args.sessions, args.seed)
            conv = {"conversation_id": conv_id, "persona": persona, "sessions": sessions}
            f.write(json.dumps(conv, ensure_ascii=False) + "\n")
            if i % 10 == 0:
                print(f"generated {i}/{args.convos}", file=sys.stderr)
    print(f"wrote {args.convos} conversations to {args.out}", file=sys.stderr)

    # Query generation is a separate pass (optional at build time); the Go
    # planner-build tool consumes convos.jsonl and emits candidates.jsonl.
    print("next: run the Go planner-build tool on this file "
          "(see specs/023/data-model.md §3)", file=sys.stderr)

if __name__ == "__main__":
    main()
