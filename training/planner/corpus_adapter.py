#!/usr/bin/env python3
"""023 public-corpus auxiliary path (T009): adapt a permissively-licensed
dialogue corpus into the same convos.jsonl shape data_build.py emits, so the
rest of the pipeline (query generation → planner-build → label) is unchanged.

Supported formats (V1, zero external deps — JSONL only; no parquet):
  ultrachat-jsonl  — one JSON object per line: {"messages":[{"from":"user|assistant","value":"..."}]}
  oasst-jsonl      — one JSON object per line: {"message_id","parent_id","role","text"};
                     threads are reconstructed by parent links.

Licenses are per-corpus and must be confirmed before use (FR-013 / T009):
  ultrachat_200k — MIT
  OASST1         — Apache-2.0
The adapter writes a corpus manifest recording name/license/source/version so
T012 audit can verify the allowlist.

Usage:
  python3 corpus_adapter.py --format ultrachat-jsonl \
      --input /data/ultrachat_200k.jsonl --corpus ultrachat_200k --license mit \
      --out data/raw/ultrachat-convos.jsonl --manifest data/processed/corpus-manifest.json

  # then generate annotated queries (same pass as synthetic path):
  python3 data_build.py --gen-queries-only data/raw/ultrachat-convos.jsonl
"""

import argparse
import json
import os
import sys


def turn_id(conv_id, sess_idx, turn_idx):
    return f"{conv_id}-{sess_idx}-{turn_idx}"


def convert_ultrachat(path):
    """ultrachat_200k jsonl: {"messages":[{"from","value"}...]} → convos."""
    convos = []
    with open(path) as f:
        for i, line in enumerate(f):
            line = line.strip()
            if not line:
                continue
            obj = json.loads(line)
            msgs = obj.get("messages", [])
            turns = []
            for j, m in enumerate(msgs):
                text = str(m.get("value", "")).strip()
                role = str(m.get("from", "")).strip()
                if not text:
                    continue
                if role not in ("user", "assistant"):
                    role = "assistant" if j % 2 else "user"
                turns.append({"turn_id": turn_id(f"ultrachat-{i:05d}", 0, j),
                              "speaker": role, "text": text})
            if turns:
                convos.append({
                    "conversation_id": f"ultrachat-{i:05d}",
                    "persona": "public-corpus",
                    "sessions": [{"session_id": f"ultrachat-{i:05d}-s0",
                                  "date": "2024-01-01", "turns": turns}],
                })
    return convos


def convert_oasst(path):
    """OASST1 jsonl: {message_id, parent_id, role, text} → threads → convos."""
    nodes = {}
    order = []
    with open(path) as f:
        for line in f:
            line = line.strip()
            if not line:
                continue
            o = json.loads(line)
            nodes[o.get("message_id")] = o
            order.append(o.get("message_id"))
    convos = []
    for i, mid in enumerate(order):
        node = nodes[mid]
        if node.get("parent_id") not in (None, "", "null", "root"):
            continue  # only thread roots start a conversation
        turns = []
        j = 0
        cur = mid
        visited = 0
        while cur and cur in nodes and visited < 200:
            n = nodes[cur]
            text = str(n.get("text", "")).strip()
            role = str(n.get("role", "")).strip()
            if text and role in ("user", "assistant"):
                turns.append({"turn_id": turn_id(f"oasst-{i:05d}", 0, j),
                              "speaker": role, "text": text})
                j += 1
            # advance to the first child (single-thread linearization)
            child = next((c for c in order if nodes[c].get("parent_id") == cur), None)
            if child is None:
                break
            cur = child
            visited += 1
        if len(turns) >= 2:
            convos.append({
                "conversation_id": f"oasst-{i:05d}",
                "persona": "public-corpus",
                "sessions": [{"session_id": f"oasst-{i:05d}-s0",
                              "date": "2024-01-01", "turns": turns}],
            })
    return convos


MANIFEST = {
    "ultrachat_200k": {"license": "mit", "source": "HuggingFace ultrachat_200k"},
    "OASST1": {"license": "apache-2.0", "source": "HuggingFace OpenAssistant/OASST1"},
}


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--format", choices=["ultrachat-jsonl", "oasst-jsonl"], required=True)
    ap.add_argument("--input", required=True)
    ap.add_argument("--corpus", required=True, help="corpus name; license looked up in manifest")
    ap.add_argument("--license", default=None, help="override license (must be in allowlist)")
    ap.add_argument("--out", required=True)
    ap.add_argument("--manifest", default=None, help="corpus manifest output (for T012 audit)")
    args = ap.parse_args()

    if args.license:
        lic = args.license.lower()
    else:
        info = MANIFEST.get(args.corpus)
        if not info:
            raise SystemExit(f"unknown corpus {args.corpus!r}; pass --license explicitly")
        lic = info["license"]
    if lic not in {"mit", "apache-2.0"}:
        raise SystemExit(f"license {lic!r} not in allowlist (mit/apache-2.0) — corpus cannot be used")

    if args.format == "ultrachat-jsonl":
        convos = convert_ultrachat(args.input)
    else:
        convos = convert_oasst(args.input)
    if not convos:
        raise SystemExit("no conversations produced; check input format")

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    with open(args.out, "w") as f:
        for c in convos:
            f.write(json.dumps(c, ensure_ascii=False) + "\n")

    if args.manifest:
        os.makedirs(os.path.dirname(args.manifest) or ".", exist_ok=True)
        with open(args.manifest, "w") as f:
            json.dump({"corpus": args.corpus, "license": lic, "source": args.input,
                       "conversations": len(convos), "format": args.format},
                      f, indent=2)
    print(f"corpus_adapter: {len(convos)} conversations from {args.corpus} "
          f"(license {lic}) → {args.out}", file=sys.stderr)
    print("next: python3 data_build.py --gen-queries-only <out>", file=sys.stderr)
    return 0


if __name__ == "__main__":
    sys.exit(main())
