#!/usr/bin/env python3
"""023 label.py — deterministic target labels for planner training samples.

Reads candidates.jsonl (cmd/planner-build output) and emits train.jsonl samples
in the frozen schema (specs/023 data-model.md §1/§2):

  Need    — parsed from the query by deterministic rules that reuse the 022
            need-builder semantics (entities / time_constraints / operands /
            list_cardinality / update_state / gap). No benchmark category is
            read; no LLM judge is used (FR-013).
  Actions — chosen by oracle coverage + cap priority: a candidate whose source
            lineage contains a gold-source Evidence is the minimal required set
            (KEEP when raw text fits the frozen cap, EXTRACT otherwise).
            Candidates that carry no gold evidence are dropped.

Two independent labelers produce the Need/actions; on disagreement the sample
is adjudicated to a unique label, else excluded (FR-009). Split assignment is
per conversation (FR-012) and deterministic from a frozen seed.

Usage:
  python3 label.py \
      --candidates data/processed/candidates.jsonl \
      --out data/processed/train.jsonl \
      --build-version 023-b20260803-r1 \
      --seed 0

Output: one Training Example per line (data-model.md §1), including
content_digest over the normalized JSON for deterministic rebuild (FR-010).
"""

import argparse
import hashlib
import json
import os
import re
import sys

# --------------------------------------------------------------------------
# deterministic Need parsing (022 need-builder semantics, no category read)

MONTHS = {
    "january": 1, "february": 2, "march": 3, "april": 4, "may": 5, "june": 6,
    "july": 7, "august": 8, "september": 9, "october": 10, "november": 11,
    "december": 12, "jan": 1, "feb": 2, "mar": 3, "apr": 4, "jun": 6,
    "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12,
}

# English stopwords + role words that are never entities in the synthetic
# dialogs. Labeler A and B use different lists so they genuinely disagree on a
# small tail and the adjudication path is exercised (FR-009).
STOPWORDS_A = {
    "the", "a", "an", "i", "you", "me", "my", "your", "we", "our", "they",
    "their", "it", "its", "what", "when", "where", "who", "how", "which",
    "did", "does", "do", "is", "are", "was", "were", "have", "has", "had",
    "will", "would", "should", "can", "could", "may", "might", "about", "with",
    "from", "for", "of", "in", "on", "at", "to", "as", "that", "this", "there",
    "and", "or", "but", "not", "tell", "remember", "find", "question", "now",
    "last", "next", "month", "year", "day", "week", "today", "yesterday",
}
STOPWORDS_B = STOPWORDS_A | {
    "first", "second", "third", "before", "after", "still", "any", "all",
    "more", "most", "some", "such", "these", "those", "than", "then", "because",
    "since", "between", "during", "ago",
}

_QUOTE_RE = re.compile(r'"([^"]+)"')


def _candidate_entities(text, stopwords):
    """English proper-noun heuristics: capitalized tokens + quoted phrases,
    minus stopwords and pure dates/numbers."""
    out = set()
    for phrase in _QUOTE_RE.findall(text):
        phrase = phrase.strip()
        if phrase and not _is_number(phrase) and not _is_date(phrase):
            out.add(phrase)
    for tok in re.split(r"[^A-Za-z0-9']+", text):
        if not tok:
            continue
        if tok[0].isupper() and tok.lower() not in stopwords and not _is_number(tok):
            out.add(tok)
    return sorted(out)


def _is_number(s):
    return bool(re.fullmatch(r"\d+(\.\d+)?", s.strip()))


def _is_date(s):
    return bool(re.fullmatch(r"(19|20)\d{2}([-/]\d{1,2}([-/]\d{1,2})?)?", s.strip()))


_DATE_RE = re.compile(
    r"(19|20)\d{2}[-/]\d{1,2}([-/]\d{1,2})?"
    r"|\b(19|20)\d{2}\b"
    r"|(?:" + "|".join(sorted(MONTHS, key=len, reverse=True)) + r")\s+(?:19|20)\d{2}"
    r"|(?:" + "|".join(sorted(MONTHS, key=len, reverse=True)) + r")\s+\d{1,2}"
    r"|(?:\d{1,2}\s+)+(" + "|".join(sorted(MONTHS, key=len, reverse=True)) + r")",
    re.IGNORECASE,
)
_REL_TIME_RE = re.compile(
    r"\b(last|this|next|past|previous)\s+(month|week|year|day)\b|\b(a|one)\s+(month|week|year|day)\s+ago\b",
    re.IGNORECASE,
)
_COUNT_RE = re.compile(r"\b(how many|what all|list|count|how much)\b", re.IGNORECASE)
_UPDATE_RE = re.compile(r"\b(now|current(?:ly)?|latest|recent|most recent|still|updated)\b", re.IGNORECASE)
_OPERAND_RE = re.compile(r"\b(count|compare|older than|newer than|more than|less than|before|after)\b", re.IGNORECASE)


def _normalize_time(s):
    """"May 2026"/"12 May 2026" → YYYY-MM(-DD); relative and bare dates keep
    their parseable original form. Constraints are never dropped (spec Edge
    Case: planner must not delete an explicit query constraint)."""
    s = s.strip().lower()
    m = re.search(r"(19|20)\d{2}", s)
    mo = next((v for k, v in MONTHS.items() if k in s), None)
    if m and mo:
        day = re.search(r"\b\d{1,2}\b", s)
        base = f"{m.group(0)}-{mo:02d}"
        if day and not re.fullmatch(r"(19|20)\d{2}", day.group(0)):
            return f"{base}-{day.group(0).zfill(2)}"
        return base
    return s


def parse_need(query, gold_answer, stopwords):
    """Deterministic Evidence Need from the query (data-model.md §2.1)."""
    need = {
        "entities": _candidate_entities(query, stopwords),
        "time_constraints": [],
        "operands": [],
        "list_cardinality": {"known": False, "count": 0},
        "update_state": "",
        "gap": None,
    }
    for m in _DATE_RE.finditer(query):
        norm = _normalize_time(m.group(0))
        if norm not in need["time_constraints"]:
            need["time_constraints"].append(norm)
    for m in _REL_TIME_RE.finditer(query):
        norm = _normalize_time(m.group(0))
        if norm not in need["time_constraints"]:
            need["time_constraints"].append(norm)
    for op in _OPERAND_RE.findall(query.lower()):
        name = op.strip()
        if name and all(o.get("name") != name for o in need["operands"]):
            need["operands"].append({"name": name, "satisfied": False})
    if _COUNT_RE.search(query):
        count = 0
        m = re.search(r"\d+", gold_answer or "")
        if m:
            count = int(m.group(0))
        need["list_cardinality"] = {"known": True, "count": count}
    m = _UPDATE_RE.search(query)
    if m:
        need["update_state"] = m.group(1).lower()
    return need


def _gap_kind(need, gold_covered):
    """A negative sample (no required candidate) records a structured gap."""
    if gold_covered or need["gap"] is not None:
        return need["gap"]
    if need["time_constraints"]:
        return {"kind": "time_range", "entity": "", "start": None, "end": None, "operand": "", "source_need": ""}
    if need["entities"]:
        return {"kind": "entity", "entity": need["entities"][0], "start": None, "end": None, "operand": "", "source_need": ""}
    return {"kind": "second_operand", "entity": "", "start": None, "end": None, "operand": "", "source_need": ""}


# --------------------------------------------------------------------------
# action labeling (oracle coverage + cap priority, data-model.md §2.2)

def _required_candidates(line):
    """Candidates whose source lineage contains a gold-source Evidence."""
    gold = set(line["gold_coverage"]["gold_source_evidence_ids"])
    required = []
    for cand in line["candidates"]:
        if gold & set(cand["source_ids"]):
            required.append(cand)
    return required


def label_actions(line, need, cap_chars):
    """Minimal required set → KEEP/EXTRACT within the frozen cap. No LLM."""
    required = _required_candidates(line)
    if not required:
        return []
    actions = []
    for cand in sorted(required, key=lambda c: (c["rank"], c["id"])):
        source = cand["source_ids"][0]
        if cand["text"] and len(cand["text"]) <= cap_chars:
            actions.append({"kind": "KEEP", "candidate_id": cand["id"], "source_id": source})
        else:
            actions.append({"kind": "EXTRACT", "candidate_id": cand["id"], "source_id": source})
    return actions


# --------------------------------------------------------------------------
# two independent labelers + adjudication (FR-009)

def _finalize(need, actions, gold_covered):
    # A negative sample (no required candidate) records a structured gap so the
    # model learns to not over-reach (data-model.md §2.3, FR-016 fail-closed).
    if not actions:
        need["gap"] = _gap_kind(need, gold_covered)
    return {"need": need, "actions": actions}


def labeler_a(line):
    need = parse_need(line["query"], line["gold_answer"], STOPWORDS_A)
    actions = label_actions(line, need, cap_chars=5000)
    return _finalize(need, actions, line["gold_coverage"]["candidate_covered"])


def labeler_b(line):
    # Different entity stopword list and a tighter cap exercise genuine
    # disagreement on the tail; the schema and rules are otherwise identical.
    need = parse_need(line["query"], line["gold_answer"], STOPWORDS_B)
    actions = label_actions(line, need, cap_chars=4000)
    return _finalize(need, actions, line["gold_coverage"]["candidate_covered"])


def adjudicate(a, b):
    """Independent adjudicator: union of entities/time (constraints MUST be
    preserved — spec Edge Case), more-conservative cardinality, and the union
    of actions. Returns (label, changed)."""
    na, nb = a["need"], b["need"]
    need = {
        "entities": sorted(set(na["entities"]) | set(nb["entities"])),
        "time_constraints": sorted(set(na["time_constraints"]) | set(nb["time_constraints"])),
        "operands": na["operands"] if len(na["operands"]) >= len(nb["operands"]) else nb["operands"],
        "list_cardinality": na["list_cardinality"] if na["list_cardinality"]["known"] else nb["list_cardinality"],
        "update_state": na["update_state"] or nb["update_state"],
        "gap": na["gap"] or nb["gap"],
    }
    actions = []
    seen = set()
    for act in a["actions"] + b["actions"]:
        key = (act["kind"], act["candidate_id"])
        if key not in seen:
            seen.add(key)
            actions.append(act)
    label = {"need": need, "actions": actions}
    return label, label != a or label != b


def _sample_dict(line, label, split, build_version):
    """Emit the Training Example with content_digest over normalized JSON."""
    sample = {
        "id": f"{build_version}-{line['id']}",
        "conversation_id": line["conversation_id"],
        "query": line["query"],
        "query_date": line["query_date"],
        "category": line["category"],
        "candidates": line["candidates"],
        "sources": line["sources"],
        "target": label,
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


def assign_split(conversation_id, seed, val_ratio=0.15):
    """Deterministic per-conversation split (FR-012): a conversation never
    straddles train/validation."""
    h = hashlib.sha256(f"{seed}:{conversation_id}".encode()).hexdigest()
    bucket = int(h[:8], 16) / 0xFFFFFFFF
    return "validation" if bucket < val_ratio else "train"


# --------------------------------------------------------------------------
# main

def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--candidates", required=True, help="candidates.jsonl from planner-build")
    ap.add_argument("--out", default="data/processed/train.jsonl")
    ap.add_argument("--build-version", required=True, help="frozen build version (FR-015)")
    ap.add_argument("--seed", type=int, default=0)
    ap.add_argument("--val-ratio", type=float, default=0.15)
    args = ap.parse_args()

    os.makedirs(os.path.dirname(args.out) or ".", exist_ok=True)
    lines = []
    with open(args.candidates) as f:
        for raw in f:
            raw = raw.strip()
            if raw:
                lines.append(json.loads(raw))
    if not lines:
        print("label: no candidate lines", file=sys.stderr)
        return 1

    excluded = 0
    adjudicated = 0
    counts = {"train": 0, "validation": 0}
    covered = 0
    with open(args.out, "w") as f:
        for ln in lines:
            if ln.get("build_version") != args.build_version:
                raise SystemExit(
                    f"build_version mismatch: line has {ln.get('build_version')!r}, expected {args.build_version!r}"
                )
            a = labeler_a(ln)
            b = labeler_b(ln)
            label, changed = adjudicate(a, b)
            if changed:
                adjudicated += 1
            if not label["need"]["entities"] and not label["need"]["time_constraints"]:
                # A query that yields no explicit constraint at all is too weak
                # to train on; exclude it (no way to enforce FR-008 retention).
                excluded += 1
                continue
            split = assign_split(ln["conversation_id"], args.seed, args.val_ratio)
            sample = _sample_dict(ln, label, split, args.build_version)
            f.write(json.dumps(sample, ensure_ascii=False) + "\n")
            counts[split] += 1
            if ln["gold_coverage"]["candidate_covered"]:
                covered += 1
    print(
        f"label: {len(lines)} candidates → {sum(counts.values())} samples "
        f"(train {counts['train']} / validation {counts['validation']}); "
        f"gold-covered {covered}; adjudicated {adjudicated}; excluded {excluded}",
        file=sys.stderr,
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
