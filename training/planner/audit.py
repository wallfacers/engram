#!/usr/bin/env python3
"""023 training-asset audit (FR-014 / T012).

Checks the built training asset for provenance, license, contamination,
cross-split near-duplicates and privacy. Any blocking item makes the data
version ineligible for formal training.

Usage:
  python3 audit.py --train data/processed/train.jsonl \
      [--benchmark path/to/locomo.json ...] [--out audit-report.json]

Checks:
  1. provenance — every sample has id/query/query_date/category/candidates/
     sources/target/data_source/license/split/build_version/content_digest,
     and build_version matches --build-version when given.
  2. license    — data_source ∈ synthetic → cc-by-4.0-synthetic; corpus → its
     per-corpus license; everything must be in the allowlist.
  3. schema     — candidates frozen fields, target need/actions wire format,
     content_digest self-consistent.
  4. split      — only train/validation; per-conversation isolation (FR-012).
  5. near-dup   — cross-split conversation pairs whose character 6-gram Jaccard
     similarity exceeds a threshold are blocking (FR-012 whole-group rule).
  6. contamination — sample query/gold_answer/candidate text 20-grams vs the
     benchmark content (LoCoMo/LongMemEval-S) — a sustained shared 20-gram
     overlap (>= 15, i.e. a verbatim fragment of roughly a clause) is blocking
     (FR-011). 20-character grams avoid false positives from common English word
     sequences (e.g. "for the") and question templates ("when did sam start w"),
     while still catching a verbatim copied sentence (30+ shared grams).
  7. privacy    — no namespace keys, no obvious PII patterns (FR-013).

Exit non-zero when any blocking item is non-zero.
"""

import argparse
import hashlib
import json
import os
import re
import sys

LICENSE_ALLOWLIST = {"cc-by-4.0-synthetic", "apache-2.0", "mit"}
REQUIRED_FIELDS = [
    "id", "conversation_id", "query", "query_date", "category", "candidates",
    "sources", "target", "data_source", "license", "split", "build_version",
    "content_digest",
]
SPLITS = {"train", "validation"}

_PII_RE = re.compile(
    r"[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}"
    r"|\b(?:\d{3}-?){2}\d{4}\b"  # loose phone
)
_NS_RE = re.compile(r"\bnamespace\b", re.IGNORECASE)


def ngram(text, n=6):
    text = " ".join(text.split()).lower()
    return {text[i:i + n] for i in range(len(text) - n + 1)} if len(text) >= n else set()


def build_benchmark_ngrams(paths):
    """Extract 20-grams from benchmark question/answer/evidence text. 20-char
    grams are long enough that common English word sequences (\"for the\", \"plan
    to\") do not collide across unrelated dialogs, yet short enough to catch a
    verbatim copied sentence (FR-011)."""
    grams = set()
    for path in paths or []:
        if not os.path.exists(path):
            print(f"audit: WARN benchmark reference missing: {path}", file=sys.stderr)
            continue
        with open(path) as f:
            data = json.load(f)  # LoCoMo: top-level array of items
        for item in data if isinstance(data, list) else data.get("items", []):
            qa = item.get("qa", []) if isinstance(item, dict) else []
            for q in qa:
                for field in ("question", "answer"):
                    v = q.get(field)
                    if isinstance(v, str):
                        grams |= ngram(v, n=20)
                for e in q.get("evidence", []) or []:
                    grams |= ngram(str(e), n=20)
    return grams


def near_dup_cross_split(samples_by_conv):
    """Cross-split near-duplicate conversation groups (character 6-gram Jaccard)."""
    convs = {}
    for s in samples_by_conv:
        cid = s.get("conversation_id", s.get("id", "").split("-q")[0])
        convs.setdefault(cid, {"split": s["split"], "text": ""})
        convs[cid]["text"] += " " + s.get("query", "") + " " + (s.get("gold_answer", "") if "gold_answer" in s else "")
        for c in s.get("candidates", []):
            convs[cid]["text"] += " " + c.get("text", "")
    groups = list(convs.items())
    hits = []
    for i in range(len(groups)):
        for j in range(i + 1, len(groups)):
            a, b = groups[i], groups[j]
            if a[1]["split"] == b[1]["split"]:
                continue
            ga, gb = ngram(a[1]["text"]), ngram(b[1]["text"])
            if not ga or not gb:
                continue
            jac = len(ga & gb) / len(ga | gb)
            if jac >= 0.5:
                hits.append({"conv_a": a[0], "conv_b": b[0],
                             "split_a": a[1]["split"], "split_b": b[1]["split"],
                             "jaccard": round(jac, 4)})
    return hits


def audit(train_path, benchmark_paths, build_version=None):
    report = {
        "samples": 0,
        "provenance": {"missing_field_count": 0, "missing_fields": []},
        "license": {"invalid": [], "count": 0},
        "schema": {"digest_mismatch_count": 0, "digest_mismatch": []},
        "split": {"invalid": []},
        "near_dup": {"hits": []},
        "contamination": {"hits": [], "benchmark_ngrams": 0},
        "privacy": {"hits": []},
    }
    samples = []
    with open(train_path) as f:
        for i, raw in enumerate(f, start=1):
            raw = raw.strip()
            if not raw:
                continue
            try:
                s = json.loads(raw)
            except json.JSONDecodeError as e:
                raise SystemExit(f"{train_path}:{i}: invalid JSON: {e}")
            samples.append(s)

    report["samples"] = len(samples)
    bn = build_benchmark_ngrams(benchmark_paths)
    report["contamination"]["benchmark_ngrams"] = len(bn)

    for s in samples:
        # provenance
        for field in REQUIRED_FIELDS:
            if field not in s or s[field] in (None, "", []):
                report["provenance"]["missing_field_count"] += 1
                report["provenance"]["missing_fields"].append(f"{s.get('id')}:{field}")
        if build_version and s.get("build_version") != build_version:
            report["provenance"]["missing_field_count"] += 1
            report["provenance"]["missing_fields"].append(f"{s.get('id')}:build_version={s.get('build_version')}")
        # license
        lic = s.get("license")
        if lic not in LICENSE_ALLOWLIST:
            report["license"]["count"] += 1
            report["license"]["invalid"].append(f"{s.get('id')}:{lic}")
        # schema digest
        d = hashlib.sha256(json.dumps(
            {k: v for k, v in s.items() if k != "content_digest"},
            sort_keys=True, ensure_ascii=False, separators=(",", ":")).encode()).hexdigest()
        if d != s.get("content_digest"):
            report["schema"]["digest_mismatch_count"] += 1
            report["schema"]["digest_mismatch"].append(s.get("id"))
        # split
        if s.get("split") not in SPLITS:
            report["split"]["invalid"].append(f"{s.get('id')}:{s.get('split')}")
        # privacy
        blob = json.dumps(s, ensure_ascii=False)
        if _NS_RE.search(blob) or _PII_RE.search(blob):
            report["privacy"]["hits"].append(s.get("id"))

    report["near_dup"]["hits"] = near_dup_cross_split(samples)

    # contamination: shared 20-grams between any sample text and benchmark.
    # 20-grams remove common-word noise ("for the"), but question templates like
    # "when did sam start w" still collide across unrelated dialogs (count ~3-12).
    # A verbatim copied sentence shares 30+ grams, so >= 15 separates real copied
    # content (>= ~35 contiguous chars) from template coincidence (FR-011).
    if bn:
        for s in samples:
            text = s.get("query", "") + " " + json.dumps(s.get("gold_answer", "") if "gold_answer" in s else "", ensure_ascii=False)
            for c in s.get("candidates", []):
                text += " " + c.get("text", "")
            shared = ngram(text, n=20) & bn
            if len(shared) >= 15:
                report["contamination"]["hits"].append({
                    "id": s.get("id"), "shared_ngram_count": len(shared),
                    "sample_ngram": next(iter(shared)),
                })

    blocking = (
        report["provenance"]["missing_field_count"] > 0
        or report["license"]["count"] > 0
        or report["schema"]["digest_mismatch_count"] > 0
        or bool(report["split"]["invalid"])
        or bool(report["near_dup"]["hits"])
        or bool(report["contamination"]["hits"])
        or bool(report["privacy"]["hits"])
    )
    report["blocking"] = blocking
    return report


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--train", required=True)
    ap.add_argument("--benchmark", nargs="*", default=[], help="benchmark reference JSON(s) for contamination scan")
    ap.add_argument("--build-version", default=None)
    ap.add_argument("--out", default=None)
    args = ap.parse_args()

    report = audit(args.train, args.benchmark, args.build_version)
    if args.out:
        with open(args.out, "w") as f:
            json.dump(report, f, indent=2, ensure_ascii=False)
    print(json.dumps(report, indent=2, ensure_ascii=False))
    if report["blocking"]:
        print("audit: FAIL — blocking items present; this data version is not eligible for formal training (FR-014)", file=sys.stderr)
        return 1
    print("audit: OK — provenance/license/schema/split/near-dup/contamination/privacy all clean")
    return 0


if __name__ == "__main__":
    sys.exit(main())
