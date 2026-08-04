#!/usr/bin/env python3
"""023 LLM-assisted annotator (r3): select KEEP candidates by reference answer.

Rule-based oracle (label.py) tops out far below the T011 gate: "which candidate
carries the answer" is a semantic judgement that source-intersection + token
matching cannot make (r2: 62% sufficiency, 76 fails). This annotator asks a
LOCAL Qwen sidecar (vLLM, OpenAI-compatible; no paid teacher, FR-013) to pick
every candidate that directly carries the reference answer, and to gap when none
does. The model sees the gold_answer (a teacher), the same way any SFT label
does; the trained Planner will NOT see it at inference.

Output schema matches label.py's Training Example (data-model.md §1) so the rest
of the pipeline (audit / rebuild_check / review / train_lora) is unchanged.
Need parsing stays deterministic (label.parse_need); only the actions come from
the model. Split is per-conversation and deterministic (assign_split).

Usage:
  python3 llm_label.py \
      --candidates data/processed/candidates.jsonl \
      --out data/processed/train-r3.jsonl \
      --build-version 023-b20260803-r3 \
      --base-url http://localhost:8000/v1 --model Qwen2.5-7B-Instruct \
      --seed 0
"""

import argparse
import hashlib
import json
import os
import re
import sys
import urllib.request

from label import assign_split, parse_need, STOPWORDS_A

SYSTEM_PROMPT = (
    "You are a training-data annotator for an evidence-selection model. Given a "
    "question, the reference answer, and a ranked list of candidate evidence, "
    "select EVERY candidate that DIRECTLY contains the information needed to "
    "support the reference answer.\n\n"
    "Rules:\n"
    "- Select a candidate only if its text explicitly carries the answer's key "
    "facts (entity, date, number, name, event). Partial relevance is NOT enough.\n"
    "- If several candidates together cover the answer, select all of them.\n"
    "- If NO candidate carries the answer's key facts, set \"gap\": true.\n"
    "- If a candidate's dates or numbers CONFLICT with the reference answer, do "
    "NOT select it.\n"
    "- Composite / multi-hop answers: every sub-answer must be covered by the "
    "selected set; leave gaps for the missing parts.\n"
    "Output ONLY strict JSON with EXACTLY this shape:\n"
    '{"gap": true|false, "actions": [{"kind":"KEEP","candidate_id":"...","source_id":"..."}]}\n'
    '"source_id" must be one of that candidate\'s listed sources. Only reference '
    "given candidate/source ids; never invent ids."
)


def render_user(query, gold_answer, candidates):
    lines = [f"Question: {query}", f"Reference answer: {gold_answer}", "", "Candidates:"]
    for i, c in enumerate(candidates):
        srcs = ",".join(c.get("source_ids", []))
        lines.append(f"[{i}] id={c['id']} kind={c.get('kind', '')} rank={c.get('rank', i)} sources={srcs}")
        lines.append(c.get("text", ""))
    lines.append("")
    lines.append("Emit the annotation JSON now.")
    return "\n".join(lines)


def chat_completion(base_url, model, user_text, max_tokens=2048):
    body = {
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM_PROMPT},
            {"role": "user", "content": user_text},
        ],
        "temperature": 0.0,
        "max_tokens": max_tokens,
    }
    req = urllib.request.Request(
        base_url.rstrip("/") + "/chat/completions",
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json"},
    )
    with urllib.request.urlopen(req, timeout=180) as resp:
        data = json.loads(resp.read().decode())
    return data["choices"][0]["message"]["content"]


def parse_annotation(text):
    """Tolerate fences/prose and extract {gap, actions}."""
    t = text.strip()
    if t.startswith("```"):
        t = re.sub(r"^```[a-zA-Z]*\s*", "", t)
        t = re.sub(r"\s*```$", "", t)
    candidates = []
    try:
        obj = json.loads(t)
        if isinstance(obj, dict):
            candidates.append(obj)
    except json.JSONDecodeError:
        pass
    for m in re.finditer(r"\{.*?\}", t, re.S):
        try:
            obj = json.loads(m.group(0))
            if isinstance(obj, dict) and ("actions" in obj or "gap" in obj):
                candidates.append(obj)
        except json.JSONDecodeError:
            continue
    for obj in candidates:
        if not isinstance(obj, dict):
            continue
        actions = []
        ok = True
        for a in obj.get("actions", []) or []:
            if a.get("kind") != "KEEP" or not a.get("candidate_id"):
                ok = False
                break
            actions.append({"kind": "KEEP", "candidate_id": a["candidate_id"],
                            "source_id": a.get("source_id", "")})
        if not ok:
            continue
        return {"gap": bool(obj.get("gap", False)), "actions": actions}
    return None


def sample_dict(line, need, actions, gap, split, build_version):
    cand_by_id = {c["id"]: c for c in line["candidates"]}
    kept_actions = []
    for a in actions:
        c = cand_by_id.get(a["candidate_id"])
        if not c or not c.get("source_ids"):
            continue
        sid = a["source_id"] if a["source_id"] in c["source_ids"] else c["source_ids"][0]
        kept_actions.append({"kind": "KEEP", "candidate_id": c["id"], "source_id": sid})
    if gap and not kept_actions:
        # Fail-closed gap with the 022 auditable source_need (validateStructuredGap
        # requires it non-empty); mirrors label._gap_kind.
        if need["time_constraints"]:
            need["gap"] = {"kind": "time_range", "entity": "", "start": None, "end": None,
                           "operand": "", "source_need": "time:" + ",".join(need["time_constraints"])}
        elif need["entities"]:
            ent = need["entities"][0]
            need["gap"] = {"kind": "entity", "entity": ent, "start": None, "end": None,
                           "operand": "", "source_need": "entity:" + ent}
        else:
            need["gap"] = {"kind": "entity", "entity": "", "start": None, "end": None,
                           "operand": "", "source_need": ""}
    sample = {
        "id": f"{build_version}-{line['id']}",
        "conversation_id": line["conversation_id"],
        "query": line["query"],
        "query_date": line["query_date"],
        "category": line["category"],
        "gold_answer": line.get("gold_answer", ""),
        "candidates": line["candidates"],
        "sources": line["sources"],
        "target": {"need": need, "actions": kept_actions},
        "data_source": "synthetic",
        "license": "cc-by-4.0-synthetic",
        "split": split,
        "build_version": build_version,
    }
    digest = hashlib.sha256(
        json.dumps(sample, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
    ).hexdigest()
    sample["content_digest"] = digest
    return sample


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--candidates", required=True)
    ap.add_argument("--out", default="data/processed/train-r3.jsonl")
    ap.add_argument("--build-version", required=True)
    ap.add_argument("--base-url", default=os.environ.get("LLM_LABEL_BASE_URL", "http://localhost:8000/v1"))
    ap.add_argument("--model", default=os.environ.get("LLM_LABEL_MODEL", "Qwen2.5-7B-Instruct"))
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--val-ratio", type=float, default=0.15)
    ap.add_argument("--max-lines", type=int, default=0, help="0 = all; >0 = first N (smoke)")
    args = ap.parse_args()

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    lines = []
    with open(args.candidates) as f:
        for raw in f:
            raw = raw.strip()
            if raw:
                lines.append(json.loads(raw))
    if args.max_lines:
        lines = lines[: args.max_lines]

    counts = {"train": 0, "validation": 0}
    gaps = 0
    total_kept = 0
    failed = 0
    with open(args.out, "w") as f:
        for i, ln in enumerate(lines):
            if not ln.get("candidates"):
                continue  # nothing for the Planner to decide on (same as label.py)
            need = parse_need(ln["query"], ln["gold_answer"], STOPWORDS_A)
            user_text = render_user(ln["query"], ln["gold_answer"], ln["candidates"])
            try:
                raw = chat_completion(args.base_url, args.model, user_text)
                ann = parse_annotation(raw)
            except Exception as e:  # network/parse — surface and continue
                print(f"llm_label: line {i} ({ln['id']}) failed: {e}", file=sys.stderr)
                failed += 1
                ann = None
            if ann is None:
                # fail-closed: no usable model output → gap (conservative)
                actions, gap = [], True
            else:
                actions, gap = ann["actions"], ann["gap"]
            split = assign_split(ln["conversation_id"], args.seed, args.val_ratio)
            sample = sample_dict(ln, need, actions, gap, split, args.build_version)
            counts[split] += 1
            gaps += 1 if gap else 0
            total_kept += len(sample["target"]["actions"])
            f.write(json.dumps(sample, ensure_ascii=False) + "\n")
            if i % 50 == 0:
                print(f"annotated {i}/{len(lines)}", file=sys.stderr)
    print(
        f"llm_label: {len(lines)} -> {counts['train'] + counts['validation']} samples "
        f"(train {counts['train']} / validation {counts['validation']}); "
        f"gap {gaps}; kept {total_kept}; failed {failed} -> {args.out}",
        file=sys.stderr,
    )


if __name__ == "__main__":
    main()
