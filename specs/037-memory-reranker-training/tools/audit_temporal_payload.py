#!/usr/bin/env python3
"""audit_temporal_payload.py — R7 temporal 可判别性审计（research.md R7）

T012. 前提: retriever 只传 Entry.Content（无结构化 EventDate, retriever.go:701），
故时序 hard negative 只能依赖文本中可见的时间信号。本脚本从 LoCoMo temporal 类
QA 的答案文档（evidence）与同对话窗口外候选文档判定:
  1) 信号存在性: 答案文档含时间信号的比例（答案能否从文本定位时间）
  2) 候选判别性: 窗口外候选（不同 session 文档）的日期可提取率 + 时间信号率
     （候选池是否提供"时间值不同"的文档）
  3) 结论: 文本可见时间信号是否足以区分"答案窗口内 vs 外"

本地零 GPU 可跑（规则判定，无 embedding）。真实 baseline top-pool 增强
（--run-dir locomo-bench 产物）为后续 T012b。

冻结项（写进报告）: 时间信号模式集、时间窗口定义（±days）、判别性阈值、
结论与适用边界。

用法:
  python3 tools/audit_temporal_payload.py \
      --data ../../testdata/locomo/locomo.json \
      --out reports/r7-temporal-audit.md \
      [--time-window-days 7]
"""
import argparse
import json
import re
from collections import defaultdict

import sys
sys.path.insert(0, __file__.rsplit("/", 1)[0])
from build_training_data import (has_time_signal, token_overlap, LocomoConv,
                                 TIME_SIGNAL_PATTERNS)


def main():
    ap = argparse.ArgumentParser(description=__doc__)
    ap.add_argument("--data", required=True, help="locomo.json")
    ap.add_argument("--out", default="reports/r7-temporal-audit.md")
    ap.add_argument("--time-window-days", type=int, default=7)
    ap.add_argument("--overlap-threshold", type=int, default=2,
                    help="hard 池语义相关判定: query↔doc 共享实词阈值（本地近似）")
    ap.add_argument("--run-dir", default=None,
                    help="（可选）locomo-bench run 产物: 真实 baseline top-pool 增强")
    args = ap.parse_args()

    with open(args.data) as f:
        raw = json.load(f)
    convs = [LocomoConv(c) for c in raw]

    # 会话日期表: conv_id -> [(session_key, date)]
    # 答案文档时间信号判定 + 窗口外候选收集
    answer_signal = 0
    answer_total = 0
    no_signal_answers = []        # (conv, qa, doc前80) —— 答案无时间信号的案例
    window_out_total = 0
    window_out_dated = 0          # 窗口外候选可提取 session 日期数
    window_out_signal = 0         # 窗口外候选含时间信号数
    hard_total = 0                # hard-negative 池: 窗口外 + 语义相关（token_overlap）
    hard_signal = 0               # hard 池含时间信号数
    hard_dated = 0                # hard 池可提取 session 日期数
    groups = 0                    # 审计组数（≥50 目标）

    for conv in convs:
        session_dates = {k: dt for k, dt in conv.session_dates}
        for qi, qa in enumerate(conv.qa_list):
            if int(qa.get("category", 0)) != 2:
                continue
            query = str(qa.get("question", "")).strip()
            refs = qa.get("evidence") or []
            resolved, valid_refs, _ = conv.evidence_docs(refs)
            if not resolved:
                continue
            groups += 1
            # 答案文档: evidence 所在 session
            ans_session = None
            for ref in valid_refs:
                if ref in conv.dia_to_session:
                    ans_session = conv.dia_to_session[ref]
                    break
            ans_date = session_dates.get(ans_session)
            # 1) 答案文档时间信号
            for doc in resolved:
                answer_total += 1
                if has_time_signal(doc):
                    answer_signal += 1
                else:
                    no_signal_answers.append((conv.conv_id, qi, query[:50], doc[:100]))
            # 2) 窗口外候选: 同对话不同 session 的文档（全池 + 语义相关 hard 池）
            for did, doc in conv.all_docs:
                s = conv.dia_to_session.get(did)
                sdate = session_dates.get(s)
                if ans_date is None or sdate is None:
                    continue
                diff = abs((sdate[0] - ans_date[0]) * 365 + (sdate[1] - ans_date[1]) * 30 + (sdate[2] - ans_date[2]))
                if diff <= args.time_window_days:
                    continue
                window_out_total += 1
                if sdate is not None:
                    window_out_dated += 1
                if has_time_signal(doc):
                    window_out_signal += 1
                # hard 池: 语义相关（query 与 doc 共享实词 ≥ --overlap 阈值）
                if token_overlap(query, doc) >= args.overlap_threshold:
                    hard_total += 1
                    if has_time_signal(doc):
                        hard_signal += 1
                    if sdate is not None:
                        hard_dated += 1

    ans_rate = answer_signal / answer_total if answer_total else 0
    wout_dated_rate = window_out_dated / window_out_total if window_out_total else 0
    wout_signal_rate = window_out_signal / window_out_total if window_out_total else 0
    hard_signal_rate = hard_signal / hard_total if hard_total else 0
    hard_dated_rate = hard_dated / hard_total if hard_total else 0

    # 判别性判定（阈值冻结）:
    #   A. 答案文档时间信号率 ≥ 0.7（答案可从文本定位时间）
    #   B. hard 池（窗口外+语义相关）日期可提取率 ≥ 0.5（负样本时间值可区分）
    #   C. hard 池时间信号率 ≥ 0.5（负样本有信号可学）
    verdict_ok = (ans_rate >= 0.7 and hard_dated_rate >= 0.5 and hard_signal_rate >= 0.5)
    verdict = "PASS — 文本可见时间信号足以区分答案窗口内外" if verdict_ok else \
        "FAIL — 文本时间信号不足/无判别性，temporal-hard 训练需谨慎"

    lines = [
        "# R7 Temporal Payload 可判别性审计",
        "",
        f"**Date**: 2026-08-11 | **T012** | **data**: {args.data}",
        f"**时间窗口**: ±{args.time_window_days} 天（答案 session 日期）",
        "",
        "## 审计统计",
        "",
        f"- 审计组数: **{groups}**（目标 ≥50）",
        f"- 答案文档: {answer_signal}/{answer_total} 含时间信号 (**{ans_rate:.1%}**)",
        f"- 窗口外全池候选: {window_out_total} 个; 含时间信号 {window_out_signal} (**{wout_signal_rate:.1%}**)",
        f"- **hard 池**（窗口外 + 语义相关, overlap≥{args.overlap_threshold}）: {hard_total} 个;",
        f"  日期可提取 {hard_dated} (**{hard_dated_rate:.1%}**); 含时间信号 {hard_signal} (**{hard_signal_rate:.1%}**)",
        "",
        "## 判定阈值（冻结）",
        "",
        f"- A. 答案时间信号率 ≥ 0.7 → {'通过' if ans_rate >= 0.7 else '不通过'} ({ans_rate:.1%})",
        f"- B. hard 池日期可提取率 ≥ 0.5 → {'通过' if hard_dated_rate >= 0.5 else '不通过'} ({hard_dated_rate:.1%})",
        f"- C. hard 池时间信号率 ≥ 0.5 → {'通过' if hard_signal_rate >= 0.5 else '不通过'} ({hard_signal_rate:.1%})",
        "",
        f"## 结论: **{verdict}**",
        "",
        "## 冻结项",
        "",
        "- 时间信号模式集: 见 build_training_data.TIME_SIGNAL_PATTERNS（绝对日期/相对时间词/星期/顺序词）",
        f"- 时间窗口: ±{args.time_window_days} 天",
        "- 判别性阈值: A≥0.7 / B≥0.5 / C≥0.5",
        "- 适用边界: 判定基于 LoCoMo 文本; 未过 audit 的样本不得标 temporal_label=true",
        "- 增强路径: 真实 baseline top-pool（--run-dir）待 US1 后补（T012b）",
        "",
        "## 无时间信号答案案例（抽样）",
        "",
    ]
    for cid, qi, q, d in no_signal_answers[:10]:
        lines.append(f"- {cid}-q{qi} `{q}` → `{d}`")
    lines.append("")

    report = "\n".join(lines)
    with open(args.out, "w") as f:
        f.write(report)
    print(report)
    print(f"\n报告: {args.out}")


if __name__ == "__main__":
    main()
