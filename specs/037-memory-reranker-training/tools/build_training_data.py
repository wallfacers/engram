#!/usr/bin/env python3
"""build_training_data.py — 重排训练数据构建（LoCoMo + MSC → 训练 JSONL）

T013. 契约: contracts/training-data-schema.md; 派生规则: research.md R3/R3b, data-model.md
确定性派生: 同一输入 + 同一 --seed → 完全相同的输出。

输出: NDJSON，每行一个 RerankTrainingSample，行字段含
schema_version/qa_id/query_group_id/positives/split/source/evidence_refs/category/
temporal_label/negative_type/conv_id 等。旁出 `{out}.manifest.json`（seed/计数/
拒绝 ledger/映射）与 `{out}.rejected.json`（拒绝 ledger）。

LoCoMo 结构（已实测确认, testdata/locomo/locomo.json）:
  - 顶层 list[10], 每项 sample_id=conv-XX; qa[list]; conversation dict
  - conversation: session_N = turns list; 每 turn 有 dia_id（如 "D1:3"）+ text
    （图像 turn: dia_id + blip_caption + query, 无 text）
  - evidence ref = dia_id; 多 evidence = multi-positive（实测 423/1986）
  - category: 1=single-hop 2=temporal 3=multi-hop 4=open-domain 5=adversarial(默认排除)
  - 无 evidence 题实测 4 个; 畸形 ref 实测 9 个（分号/空格分隔、D、D:11:26、D30:05）

负采样协议（research R3）:
  - in-dialogue : 同对话非 evidence / 非近重复 turn（随机）
  - cross-session: 其他对话 turn（随机）
  - temporal-hard: 仅 category==2; 同对话**时间窗口外**（与答案 session 日期差
    > --time-window-days）且文本含时间信号（R7）的 turn; 语义相关性本地用
    token-overlap 近似（manifest 标注 method=overlap），AutoDL/embedding 端点
    就绪后以 baseline top-pool 增强（T012/T013b）——严格 embedding 相似度判定
    在 T014 规则 6 的语义相关性抽检中复核。
  - 同一 query_group_id 的正例、近重复与 evidence-overlap 候选不得互作负例。

MSC（--msc，可选）: HF 镜像无显式 QA，query 由 persona/cross-session 回指启发式
派生（research R3b，噪声派生监督）。数据结构待 T002 探查后填充，当前仅接口占位。

用法:
  python3 tools/build_training_data.py \
      --locomo ../../testdata/locomo/locomo.json \
      --out train-r1.jsonl \
      --train-convs conv-26,conv-30,conv-41,conv-42,conv-43,conv-44,conv-47 \
      --heldout-convs conv-48,conv-49,conv-50 \
      --seed 42
"""
import argparse
import hashlib
import json
import random
import re
import sys
from collections import defaultdict

SCHEMA_VERSION = 1
CATEGORY_NAMES = {1: "single-hop", 2: "temporal", 3: "multi-hop", 4: "open-domain", 5: "adversarial"}

# R7: 文本可见时间信号（temporal_label / temporal-hard 判定的依据）
TIME_SIGNAL_PATTERNS = [
    re.compile(r"\b\d{1,2}\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*(\s+\d{2,4})?\b", re.I),
    re.compile(r"\b(january|february|march|april|june|july|august|september|october|november|december)\s+\d{1,2}(,\s+\d{4})?\b", re.I),
    re.compile(r"\b\d{4}\b"),
    re.compile(r"\b\d{1,2}/\d{1,2}/\d{2,4}\b"),
    re.compile(r"\b(yesterday|today|tomorrow|tonight)\b", re.I),
    re.compile(r"\b(last|this|next|past|previous|upcoming)\s+(week|month|year|weekend|friday|saturday|sunday|monday|tuesday|wednesday|thursday)\b", re.I),
    re.compile(r"\b\d+\s+(days?|weeks?|months?|years?)\s+ago\b", re.I),
    re.compile(r"\b(monday|tuesday|wednesday|thursday|friday|saturday|sunday)\b", re.I),
    re.compile(r"\b(ago|since|until|after|before|back then|recently|last night|this morning)\b", re.I),
]


def has_time_signal(text: str) -> bool:
    return any(p.search(text) for p in TIME_SIGNAL_PATTERNS)


def _norm_doc(text: str) -> str:
    return re.sub(r"[^a-z0-9]+", " ", text.lower()).strip()


def jaccard(a: str, b: str) -> float:
    """归一化 token 集合 Jaccard 相似度（近重复排除用）。"""
    sa, sb = set(_norm_doc(a).split()), set(_norm_doc(b).split())
    if not sa or not sb:
        return 0.0
    return len(sa & sb) / len(sa | sb)


def token_overlap(query: str, doc: str) -> int:
    """query 与 doc 共享实词数（temporal-hard 语义相关性的本地近似）。"""
    q = set(w for w in _norm_doc(query).split() if len(w) > 2)
    d = set(w for w in _norm_doc(doc).split() if len(w) > 2)
    return len(q & d)


def parse_date(s: str):
    """解析 '1:56 pm on 8 May, 2023' 格式的 session 日期 → (y, m, d) 或 None。"""
    m = re.search(r"(\d{1,2})\s+(jan|feb|mar|apr|may|jun|jul|aug|sep|sept|oct|nov|dec)[a-z]*\s*,?\s*(\d{4})", s, re.I)
    if not m:
        return None
    months = {"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
              "jul": 7, "aug": 8, "sep": 9, "sept": 9, "oct": 10, "nov": 11, "dec": 12}
    return (int(m.group(3)), months[m.group(2).lower()[:3]], int(m.group(1)))


def date_diff_days(a, b):
    """两个 (y,m,d) 的粗略天数差（近似, 不计闰年精度——仅窗口判定用）。"""
    import datetime
    if a is None or b is None:
        return None
    da = datetime.date(*a)
    db = datetime.date(*b)
    return abs((da - db).days)


class LocomoConv:
    def __init__(self, raw):
        self.raw = raw
        self.conv_id = raw["sample_id"]
        self.qa_list = raw["qa"]
        self.dia_map = {}          # dia_id -> serialized document
        self.dia_to_session = {}   # dia_id -> session date string
        self.all_docs = []         # list of (dia_id, document)
        self.session_dates = []    # list of (session_key, date)
        self._index()

    def _serialize_turn(self, t):
        speaker = t.get("speaker", "")
        if "text" in t and t.get("text"):
            return f"{speaker}: {t['text']}"
        # 图像 turn: blip_caption + query（与 runtime 候选同源序列化）
        parts = []
        if t.get("blip_caption"):
            parts.append(f"[image] {t['blip_caption']}")
        if t.get("query"):
            parts.append(f"query: {t['query']}")
        return f"{speaker}: {' '.join(parts)}"

    def _index(self):
        conv_data = self.raw["conversation"]
        for k, v in conv_data.items():
            if k.endswith("_date_time") or not isinstance(v, list):
                continue
            session_key = k
            sdate = parse_date(conv_data.get(f"{k}_date_time", ""))
            if sdate:
                self.session_dates.append((session_key, sdate))
            for t in v:
                did = t.get("dia_id")
                if not did:
                    continue
                doc = self._serialize_turn(t)
                self.dia_map[did] = doc
                self.dia_to_session[did] = session_key
                self.all_docs.append((did, doc))

    def evidence_docs(self, refs):
        """解析 evidence refs → (resolved_docs, valid_refs, invalid_refs)。"""
        resolved, valid, invalid = [], [], []
        for ref in refs:
            for piece in re.split(r"[;,\s]+", ref.strip()):
                if not piece:
                    continue
                norm = re.sub(r":0+(\d)", r":\1", piece)  # D30:05 → D30:5
                if norm.count(":") != 1 or not re.match(r"^D\d+:\d+$", norm):
                    invalid.append(piece)
                    continue
                if norm in self.dia_map:
                    resolved.append(self.dia_map[norm])
                    valid.append(norm)
                else:
                    invalid.append(piece)
        return resolved, valid, invalid


def build_lo_como_samples(conv: LocomoConv, qa_index: int, qa: dict, args, rng, ledger):
    """一个 qa → 样本列表（正样本 + 负样本）。"""
    qa_id = f"{conv.conv_id}-q{qa_index}"
    query = str(qa.get("question", "")).strip()
    cat_int = int(qa.get("category", 0))
    category = CATEGORY_NAMES.get(cat_int)
    if category is None:
        ledger.append({"qa_id": qa_id, "reason": f"unknown category {cat_int}"})
        return []
    if category == "adversarial" and not args.include_adversarial:
        return []  # 默认排除 cat5 adversarial（评测也默认跳过，1986-446=1540 对账）

    raw_refs = qa.get("evidence") or []
    if not raw_refs:
        ledger.append({"qa_id": qa_id, "reason": "no evidence", "question": query[:80]})
        return []
    resolved_docs, valid_refs, invalid_refs = conv.evidence_docs(raw_refs)
    if invalid_refs:
        ledger.append({"qa_id": qa_id, "reason": "malformed/unresolved evidence",
                       "refs": invalid_refs, "question": query[:80]})
    if not resolved_docs:
        ledger.append({"qa_id": qa_id, "reason": "all evidence unresolvable"})
        return []

    split = args.split_for(conv.conv_id)
    is_temporal = category == "temporal"
    samples = []
    positives_texts = resolved_docs

    # --- 正样本（multi-positive：同 question 全量 positives） ---
    for i, doc in enumerate(resolved_docs):
        sid = f"locomo-{conv.conv_id}-{qa_index}-p{i}"
        samples.append({
            "sample_id": sid, "schema_version": SCHEMA_VERSION,
            "qa_id": qa_id, "query_group_id": qa_id,
            "query": query, "document": doc,
            "document_kind": "chunk", "candidate_source": "evidence-locomo",
            "label": 1.0, "is_positive": True,
            "positives": positives_texts, "category": category,
            "temporal_label": is_temporal and has_time_signal(doc),
            "negative_type": None, "evidence_refs": valid_refs,
            "split": split, "source": "locomo", "conv_id": conv.conv_id,
        })

    # --- 负样本 ---
    neg_count = args.negatives_per_positive
    excluded = set(valid_refs)  # evidence refs 排除出负池（同 group 正例不互作负例）
    # evidence-overlap 候选排除: 与任一 positive Jaccard > 0.7 视为近重复/overlap
    neg_pool = []
    for did, doc in conv.all_docs:
        if did in excluded:
            continue
        if any(jaccard(doc, p) > 0.7 for p in positives_texts):
            continue
        neg_pool.append((did, doc))

    # 1) in-dialogue 随机负样本
    if neg_pool:
        picks = rng.sample(neg_pool, min(neg_count, len(neg_pool)))
        for j, (did, doc) in enumerate(picks):
            samples.append(_neg_sample(conv, qa_id, qa_index, query, doc, "in-dialogue",
                                       category, is_temporal, split, j))

    # 2) temporal-hard（仅 temporal 类）：时间窗口外 + 时间信号 + 词 overlap 近似语义相关
    if is_temporal:
        answer_session = None
        for ref in valid_refs:
            if ref in conv.dia_to_session:
                answer_session = conv.dia_to_session[ref]
                break
        answer_date = None
        for k, dt in conv.session_dates:
            if k == answer_session:
                answer_date = dt
                break
        th_pool = []
        for did, doc in conv.all_docs:
            if did in excluded:
                continue
            if any(jaccard(doc, p) > 0.7 for p in positives_texts):
                continue
            if not has_time_signal(doc):
                continue
            # 时间窗口外：与答案 session 日期差 > 阈值（窗口内排除）
            s = conv.dia_to_session.get(did)
            sdate = None
            for k, dt in conv.session_dates:
                if k == s:
                    sdate = dt
                    break
            if answer_date is not None and sdate is not None:
                diff = date_diff_days(answer_date, sdate)
                if diff is not None and diff <= args.time_window_days:
                    continue
            # 语义相关性：token-overlap 近似（严格 embedding 判定 T014 复核）
            if token_overlap(query, doc) < args.overlap_threshold:
                continue
            th_pool.append((did, doc))
        picks = rng.sample(th_pool, min(neg_count, len(th_pool)))
        for j, (did, doc) in enumerate(picks):
            samples.append(_neg_sample(conv, qa_id, qa_index, query, doc, "temporal-hard",
                                       category, True, split, 50 + j))

    # 3) cross-session：其他对话（由调用方传入其它对话池？——简化：本对话内跨 session 亦可，
    #    但契约 cross-session 指跨对话。此处留 pool 注入，main 层处理跨对话负样本）

    return samples


def _neg_sample(conv, qa_id, qa_index, query, doc, neg_type, category, temporal, split, n):
    return {
        "sample_id": f"locomo-{conv.conv_id}-{qa_index}-n{n}",
        "schema_version": SCHEMA_VERSION, "qa_id": qa_id, "query_group_id": qa_id,
        "query": query, "document": doc,
        "document_kind": "chunk", "candidate_source": f"neg-{neg_type}",
        "label": 0.0, "is_positive": False,
        "positives": None, "category": category,
        "temporal_label": temporal and has_time_signal(doc),
        "negative_type": neg_type, "evidence_refs": None,
        "split": split, "source": "locomo", "conv_id": conv.conv_id,
    }


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--locomo", required=True)
    ap.add_argument("--msc", default=None, help="MSC 派生子集 JSONL（T002 后填充）")
    ap.add_argument("--out", required=True)
    ap.add_argument("--train-convs", required=True)
    ap.add_argument("--heldout-convs", required=True)
    ap.add_argument("--seed", type=int, default=42)
    ap.add_argument("--negatives-per-positive", type=int, default=3)
    ap.add_argument("--time-window-days", type=int, default=7,
                    help="temporal-hard 答案时间窗口阈值（±N 天外才算窗口外）")
    ap.add_argument("--overlap-threshold", type=int, default=2,
                    help="temporal-hard 语义相关 token-overlap 阈值（本地近似）")
    ap.add_argument("--max-doc-len", type=int, default=4096)
    ap.add_argument("--max-query-len", type=int, default=512)
    ap.add_argument("--include-adversarial", action="store_true")
    args = ap.parse_args()

    train = set(args.train_convs.split(","))
    heldout = set(args.heldout_convs.split(","))
    if train & heldout:
        ap.error(f"train 与 heldout conv 重叠: {train & heldout}")
    known = train | heldout

    def split_for(cid):
        if cid in heldout:
            return "heldout"
        if cid in train:
            return "train"
        raise ValueError(f"conv {cid} 不在 train/heldout 列表")

    args.split_for = split_for

    rng = random.Random(args.seed)
    with open(args.locomo) as f:
        raw = json.load(f)

    convs = [LocomoConv(c) for c in raw]
    ledger = []
    samples = []
    for conv in convs:
        for qi, qa in enumerate(conv.qa_list):
            for s in build_lo_como_samples(conv, qi, qa, args, rng, ledger):
                if len(s["query"]) > args.max_query_len or len(s["document"]) > args.max_doc_len:
                    ledger.append({"qa_id": s["qa_id"], "reason": "len overflow",
                                   "query_len": len(s["query"]), "doc_len": len(s["document"])})
                    continue
                samples.append(s)

    # cross-session 负样本（跨对话；确定性：按 conv 顺序轮转采样）
    cross_idx = 0
    for conv in convs:
        for qi, qa in enumerate(conv.qa_list):
            qa_id = f"{conv.conv_id}-q{qi}"
            qs = [s for s in samples if s["qa_id"] == qa_id and s["is_positive"]]
            if not qs:
                continue
            other = [c for c in convs if c.conv_id != conv.conv_id]
            if not other:
                continue
            n_src = other[rng.randrange(len(other))]
            pool = [d for did, d in n_src.all_docs
                    if not any(jaccard(d, p["document"]) > 0.7 for p in qs)]
            if not pool:
                continue
            doc = rng.choice(pool)
            samples.append(_neg_sample(conv, qa_id, qi, qs[0]["query"], doc, "cross-session",
                                       qs[0]["category"], qs[0]["category"] == "temporal",
                                       qs[0]["split"], 90 + cross_idx))
            cross_idx += 1

    # 确定性：按 sample_id 排序后写出
    samples.sort(key=lambda s: s["sample_id"])
    with open(args.out, "w") as f:
        for s in samples:
            f.write(json.dumps(s, ensure_ascii=False, sort_keys=True) + "\n")

    # manifest + rejected ledger
    n_pos = sum(1 for s in samples if s["is_positive"])
    n_neg = sum(1 for s in samples if not s["is_positive"])
    cat_counts = defaultdict(int)
    split_counts = defaultdict(int)
    temporal_counts = {"temporal": 0, "non-temporal": 0}
    for s in samples:
        cat_counts[s["category"]] += 1
        split_counts[s["split"]] += 1
        temporal_counts["temporal" if s["temporal_label"] else "non-temporal"] += 1
    manifest = {
        "schema_version": SCHEMA_VERSION,
        "seed": args.seed,
        "train_convs": sorted(train), "heldout_convs": sorted(heldout),
        "total_samples": len(samples), "positives": n_pos, "negatives": n_neg,
        "category_counts": dict(cat_counts),
        "split_counts": dict(split_counts),
        "temporal_label_counts": dict(temporal_counts),
        "negative_type_counts": dict(defaultdict(int, **{})),
        "semantic_overlap_method": "token-overlap-approx",
        "rejected_ledger_count": len(ledger),
        "notes": ["temporal-hard 语义相关性为 token-overlap 近似；T012/T013b 用 baseline top-pool + embedding 增强后严格化"],
    }
    for s in samples:
        if s.get("negative_type"):
            manifest["negative_type_counts"][s["negative_type"]] = manifest["negative_type_counts"].get(s["negative_type"], 0) + 1
    with open(args.out + ".manifest.json", "w") as f:
        json.dump(manifest, f, ensure_ascii=False, indent=2)
    with open(args.out + ".rejected.json", "w") as f:
        json.dump(ledger, f, ensure_ascii=False, indent=2)

    print(f"samples={len(samples)} pos={n_pos} neg={n_neg}")
    print(f"categories: {dict(cat_counts)}")
    print(f"rejected: {len(ledger)}  (详见 {args.out}.rejected.json)")
    print(f"manifest: {args.out}.manifest.json")


if __name__ == "__main__":
    main()
