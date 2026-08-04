#!/usr/bin/env python3
"""023: assemble externally-AI-labeled actions into a Training Example file.

The T011 gate needs >=95% semantic sufficiency, which neither the rule-based
oracle (label.py, 62%) nor the local 7B annotator (llm_label.py, 57.5%) reach.
This assembler takes the external AI's per-sample actions (one JSON object per
line: {"id","gap","actions":[candidate_id,...]}) and merges them with the
frozen candidates.jsonl rows to emit a train asset with the standard schema.

Need parsing stays deterministic (label.parse_need); only the actions come from
the external reviewer. Split is per-conversation and deterministic. Output is
identical in shape to llm_label.py so audit/rebuild_check/review/train_lora
work unchanged.

Usage:
  python3 assemble_external.py \
      --candidates data/processed/candidates.jsonl \
      --labels external-output.jsonl \
      --out data/processed/train-r4.jsonl \
      --build-version 023-b20260803-r4 \
      --seed 0
"""

import argparse
import hashlib
import json
import os

from label import assign_split, parse_need, STOPWORDS_A


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--candidates", required=True)
    ap.add_argument("--labels", required=True, help="external output JSONL: {id,gap,actions}")
    ap.add_argument("--out", default="data/processed/train-r4.jsonl")
    ap.add_argument("--build-version", required=True)
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--val-ratio", type=float, default=0.15)
    args = ap.parse_args()

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    cand_lines = {}
    for raw in open(args.candidates):
        raw = raw.strip()
        if not raw:
            continue
        c = json.loads(raw)
        if c.get("candidates"):
            cand_lines[c["id"]] = c

    labels = {}
    for i, raw in enumerate(open(args.labels), start=1):
        raw = raw.strip()
        if not raw:
            continue
        obj = json.loads(raw)
        if "id" not in obj or "actions" not in obj:
            raise SystemExit(f"labels:{i}: missing id/actions: {raw[:120]}")
        labels[obj["id"]] = obj

    counts = {"train": 0, "validation": 0}
    gaps = 0
    kept = 0
    missing = 0
    with open(args.out, "w") as f:
        for cid, c in cand_lines.items():
            lab = labels.get(cid)
            if lab is None:
                print(f"assemble: WARN no label for {cid}", file=__import__("sys").stderr)
                missing += 1
                continue
            need = parse_need(c["query"], c["gold_answer"], STOPWORDS_A)
            cand_by_id = {x["id"]: x for x in c["candidates"]}
            actions = []
            for aid in lab["actions"]:
                cand = cand_by_id.get(aid)
                if not cand or not cand.get("source_ids"):
                    continue
                actions.append({"kind": "KEEP", "candidate_id": cand["id"],
                                "source_id": cand["source_ids"][0]})
            gap = bool(lab.get("gap", False))
            if gap and not actions:
                if need["time_constraints"]:
                    need["gap"] = {"kind": "time_range", "entity": "", "start": None,
                                   "end": None, "operand": "",
                                   "source_need": "time:" + ",".join(need["time_constraints"])}
                elif need["entities"]:
                    ent = need["entities"][0]
                    need["gap"] = {"kind": "entity", "entity": ent, "start": None,
                                   "end": None, "operand": "", "source_need": "entity:" + ent}
                else:
                    need["gap"] = {"kind": "entity", "entity": "", "start": None,
                                   "end": None, "operand": "", "source_need": ""}
            split = assign_split(c["conversation_id"], args.seed, args.val_ratio)
            sample = {
                "id": f"{args.build_version}-{c['id']}",
                "conversation_id": c["conversation_id"],
                "query": c["query"],
                "query_date": c["query_date"],
                "category": c["category"],
                "gold_answer": c.get("gold_answer", ""),
                "candidates": c["candidates"],
                "sources": c["sources"],
                "target": {"need": need, "actions": actions},
                "data_source": "synthetic",
                "license": "cc-by-4.0-synthetic",
                "split": split,
                "build_version": args.build_version,
            }
            digest = hashlib.sha256(
                json.dumps(sample, sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()
            ).hexdigest()
            sample["content_digest"] = digest
            f.write(json.dumps(sample, ensure_ascii=False) + "\n")
            counts[split] += 1
            gaps += 1 if gap else 0
            kept += len(actions)
    print(
        f"assemble: {len(cand_lines)} candidates, {sum(counts.values())} samples "
        f"(train {counts['train']} / validation {counts['validation']}); "
        f"gap {gaps}; kept {kept}; missing labels {missing} -> {args.out}",
        file=__import__("sys").stderr,
    )


if __name__ == "__main__":
    main()
