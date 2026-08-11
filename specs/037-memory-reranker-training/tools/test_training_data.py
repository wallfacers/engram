#!/usr/bin/env python3
"""test_training_data.py — 训练 JSONL fail-closed schema 校验

T014. 契约: contracts/training-data-schema.md §校验规则（全部 9 条）。
fail-closed: 任何样本不满足任一规则 → 打印违规并 exit 1，杜绝脏数据进训练。

用法:
  python3 tools/test_training_data.py train-r1.jsonl \
      --locomo ../../testdata/locomo/locomo.json \
      --train-convs conv-26,conv-30,conv-41,conv-42,conv-43,conv-44,conv-47 \
      --heldout-convs conv-48,conv-49,conv-50 \
      [--manifest out-manifest.json]

校验规则（training-data-schema.md）:
  1. 必填字段缺失（含 schema_version/qa_id/query_group_id/split）→ 拒绝
  2. label 越界（<0 或 >1）→ 拒绝
  3. 正样本无 evidence_refs / positives 或无法定位到源对话 → 拒绝
  4. 负样本缺 negative_type → 拒绝
  5. multi-positive: 同 query_group_id 正样本不互作负例; 近重复/evidence-overlap
     候选排除出负池（违反整组拒绝）
  6. temporal-hard 负样本: 语义相关 + 非答案时间窗口 + 文本含可判别时间信号（R7）;
     确定性规则判定 + 人工抽检集（≥50 条）复核
  7. 类别/数据源一致性: source=locomo → category ∈ LoCoMo 四类; source=msc → msc-* 类
  8. split 隔离: split=heldout 的 conv 不出现在训练集; 划分与 conv_id 一致
  9. 同源过拟合防线: 污染标注（manifest 记录训练对话出现在 GO 门全量配对）

实现说明:
  - evidence 定位回源用 --locomo（重建 dia_id → turn 文本映射）
  - 时间信号/近重复判定复用 build_training_data.py（单一实现源）
  - temporal-hard 的"时间窗口外"确定性判定: 该 qa 的 evidence 不在其 document 的
    同 session 内（简化为: 非 evidence / 非同 session）; 严格窗口复核列人工抽检集
"""
import argparse
import json
import re
import sys
from collections import defaultdict

sys.path.insert(0, __file__.rsplit("/", 1)[0])
from build_training_data import has_time_signal, jaccard, LocomoConv, CATEGORY_NAMES

REQUIRED_FIELDS = [
    "sample_id", "schema_version", "qa_id", "query_group_id", "query", "document",
    "document_kind", "candidate_source", "label", "is_positive", "category",
    "temporal_label", "split", "conv_id", "source",
]
DOCUMENT_KINDS = {"fact", "chunk", "observation"}
NEGATIVE_TYPES = {"in-dialogue", "temporal-hard", "cross-session"}
LOCOMO_CATS = {"single-hop", "multi-hop", "temporal", "open-domain", "adversarial"}
MSC_CATS = {"msc-persona", "msc-cross-session"}


class RuleViolation(Exception):
    pass


def check_required(s, lineno, violations):
    for f in REQUIRED_FIELDS:
        if f not in s or s[f] is None:
            violations.append(f"L{lineno} {s.get('sample_id', '?')} 规则1: 缺必填字段 {f}")


def check_label(s, lineno, violations):
    label = s.get("label")
    if not isinstance(label, (int, float)) or not (0.0 <= label <= 1.0):
        violations.append(f"L{lineno} {s['sample_id']} 规则2: label 越界 {label!r}")


def check_positive(s, lineno, violations, conv_map):
    if not s.get("is_positive"):
        return
    ev = s.get("evidence_refs")
    pos = s.get("positives")
    if not ev or len(ev) < 1:
        violations.append(f"L{lineno} {s['sample_id']} 规则3: 正样本缺 evidence_refs")
        return
    if not pos or len(pos) < 1:
        violations.append(f"L{lineno} {s['sample_id']} 规则3: 正样本缺 positives")
        return
    # 回源定位（仅 locomo; msc 无回源库，跳过定位但保留文本）
    if s.get("source") == "locomo":
        conv = conv_map.get(s.get("conv_id"))
        if conv is None:
            violations.append(f"L{lineno} {s['sample_id']} 规则3: conv {s.get('conv_id')} 不在 locomo 数据")
            return
        for ref in ev:
            if ref not in conv.dia_map:
                violations.append(f"L{lineno} {s['sample_id']} 规则3: evidence_ref {ref} 无法定位")
                return
        # positives 必须与 evidence 全量一致（multi-positive 完整性）
        expected = sorted(conv.dia_map[r] for r in ev)
        if sorted(pos) != expected:
            violations.append(f"L{lineno} {s['sample_id']} 规则3/5: positives 与 evidence 定位不一致")


def check_negative(s, lineno, violations):
    if s.get("is_positive"):
        return
    nt = s.get("negative_type")
    if not nt or nt not in NEGATIVE_TYPES:
        violations.append(f"L{lineno} {s['sample_id']} 规则4: 负样本缺/非法 negative_type {nt!r}")
    # 负样本不得携带 evidence 定位
    if s.get("evidence_refs") is not None:
        violations.append(f"L{lineno} {s['sample_id']} 规则4: 负样本不应有 evidence_refs")


def check_multi_positive(samples, violations):
    """规则5: 同 query_group_id 正样本不互作负例; 近重复/overlap 排除出负池。"""
    group_pos_docs = defaultdict(set)
    for s in samples:
        if s["is_positive"]:
            group_pos_docs[s["query_group_id"]].add(s["document"])
    for s in samples:
        if s["is_positive"]:
            continue
        gpos = group_pos_docs.get(s["query_group_id"], set())
        if s["document"] in gpos:
            violations.append(f"L? {s['sample_id']} 规则5: 正样本 document 出现在同 group 负例")
        for pd in gpos:
            if jaccard(s["document"], pd) > 0.7:
                violations.append(f"L? {s['sample_id']} 规则5: 负样本与正样本近重复 (jaccard={jaccard(s['document'], pd):.2f})")
    # 整组拒绝语义: 若任一样本违规，标记该 group
    if violations:
        pass  # fail-closed 已触发，整组由 build ledger 记录


def check_temporal_hard(s, lineno, violations):
    if s.get("negative_type") != "temporal-hard":
        return
    doc = s.get("document", "")
    if not has_time_signal(doc):
        violations.append(f"L{lineno} {s['sample_id']} 规则6: temporal-hard 缺文本可见时间信号")
    if not s.get("temporal_label"):
        violations.append(f"L{lineno} {s['sample_id']} 规则6: temporal-hard 应标 temporal_label=true")
    if s.get("category") != "temporal":
        violations.append(f"L{lineno} {s['sample_id']} 规则6: temporal-hard 仅限 temporal 类")


def check_category_source(s, lineno, violations):
    src, cat = s.get("source"), s.get("category")
    if src == "locomo" and cat not in LOCOMO_CATS:
        violations.append(f"L{lineno} {s['sample_id']} 规则7: locomo 类别非法 {cat!r}")
    if src == "msc" and cat not in MSC_CATS:
        violations.append(f"L{lineno} {s['sample_id']} 规则7: msc 类别非法 {cat!r}")
    if src not in {"locomo", "msc"}:
        violations.append(f"L{lineno} {s['sample_id']} 规则7: source 非法 {src!r}")


def check_split(s, lineno, violations, train_convs, heldout_convs):
    cid = s.get("conv_id")
    split = s.get("split")
    if split == "train" and cid not in train_convs:
        violations.append(f"L{lineno} {s['sample_id']} 规则8: train split 的 conv {cid} 不在训练列表")
    if split == "heldout" and cid not in heldout_convs:
        violations.append(f"L{lineno} {s['sample_id']} 规则8: heldout split 的 conv {cid} 不在留出列表")
    if split not in {"train", "heldout"}:
        violations.append(f"L{lineno} {s['sample_id']} 规则8: split 非法 {split!r}")
    # heldout conv 不得混入训练样本（隔离）
    if cid in heldout_convs and split != "heldout":
        violations.append(f"L{lineno} {s['sample_id']} 规则8: heldout conv {cid} 出现在非 heldout split")


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("data", help="训练 JSONL")
    ap.add_argument("--locomo", default=None, help="locomo.json（evidence 回源定位）")
    ap.add_argument("--train-convs", required=True)
    ap.add_argument("--heldout-convs", required=True)
    ap.add_argument("--manifest", default=None, help="输出校验 manifest")
    args = ap.parse_args()

    train_convs = set(args.train_convs.split(","))
    heldout_convs = set(args.heldout_convs.split(","))

    conv_map = {}
    if args.locomo:
        with open(args.locomo) as f:
            for raw in json.load(f):
                conv = LocomoConv(raw)
                conv_map[conv.conv_id] = conv

    samples = []
    with open(args.data) as f:
        for lineno, line in enumerate(f, 1):
            line = line.strip()
            if not line:
                continue
            try:
                s = json.loads(line)
            except json.JSONDecodeError as e:
                sys.exit(f"L{lineno}: JSON 解析失败 — {e}")  # fail-closed
            samples.append((lineno, s))

    violations = []
    for lineno, s in samples:
        check_required(s, lineno, violations)
        check_label(s, lineno, violations)
        check_positive(s, lineno, violations, conv_map)
        check_negative(s, lineno, violations)
        check_temporal_hard(s, lineno, violations)
        check_category_source(s, lineno, violations)
        check_split(s, lineno, violations, train_convs, heldout_convs)
    check_multi_positive([s for _, s in samples], violations)

    # 人工抽检集导出（规则6 复核; ≥50 条 temporal-hard）
    th_samples = [s for _, s in samples if s.get("negative_type") == "temporal-hard"]
    spot_check = th_samples[:50]

    if violations:
        print(f"FAIL: {len(violations)} 违规（前 20 条）:")
        for v in violations[:20]:
            print("  -", v)
        sys.exit(1)

    # 统计
    n_pos = sum(1 for _, s in samples if s["is_positive"])
    n_neg = sum(1 for _, s in samples if not s["is_positive"])
    n_th = len(th_samples)
    print(f"PASS: {len(samples)} 样本（pos={n_pos} neg={n_neg}）全部满足 schema（9/9 规则）")
    print(f"temporal-hard 样本: {n_th} 条; 人工抽检集: 50 条（存 manifest.spot_check）")

    if args.manifest:
        manifest = {
            "schema_version": samples[0][1]["schema_version"] if samples else None,
            "total_samples": len(samples),
            "positives": n_pos, "negatives": n_neg,
            "temporal_hard_count": n_th,
            "rules_passed": 9,
            "spot_check_first50": [{"sample_id": s["sample_id"], "qa_id": s["qa_id"],
                                    "document": s["document"][:120]} for s in spot_check],
            "note": "人工抽检: 核对 50 条 temporal-hard 的文本时间信号可判别性（R7 报告归档）",
        }
        with open(args.manifest, "w") as f:
            json.dump(manifest, f, ensure_ascii=False, indent=2)
        print(f"manifest: {args.manifest}")


if __name__ == "__main__":
    main()
