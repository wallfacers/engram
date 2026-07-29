---
title: MemOS LoCoMo 同栈复现报告
summary: 本文保留 MemOS 在 engram 同栈上的 LoCoMo 复现方法、结果与限制；不将该单次对照外推为通用机制排名。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memos-locomo-reproduction]
tags: [evaluation, memos, reproduction, report]
---

# MemOS LoCoMo 同栈复现报告

本报告保存 2026-07-26 的同栈复现证据。当前分数矩阵见[当前评测结果](../results.md)，竞品解释规则见[竞品与基准口径](../competitors.md)。

## 复现设置

MemOS 使用其自家代码与 LoCoMo cat 1–4（1540 题），答题模型固定为 Qwen3.6-35B-A3B-FP8，embedding 固定 bge-large-en-v1.5，judge 固定 deepseek-v4-flash 与相同 prompt。MemOS 继续使用其默认检索形态；因此这不是等上下文预算实验。

## 结果与可声明范围

在原始 1540 行汇总中，MemOS 得到 1269/1540（82.40%），engram 同口径参考为 85.71%，差为 +3.31 个百分点。MemOS 公开 88.83 的数字不能与本结果直接混用；两者的 answerer 与 judge regime 不同。

### 逐题配对检验

原始数据的 1540 行含 11 组重复问题。以 `(conv, question)` 对齐，每个 engram run 对重复组保留首条后再取三次运行多数，MemOS 侧同样折叠，得到 1529 个唯一配对：

| 类别 | n | engram | MemOS | b | c | 双侧 exact p |
|---|---:|---:|---:|---:|---:|---:|
| single-hop | 830 | 737/830（88.80%） | 687/830（82.77%） | 90 | 40 | 0.000014 |
| open-domain | 96 | 63/96（65.62%） | 57/96（59.38%） | 16 | 10 | 0.326940 |
| multi-hop | 282 | 247/282（87.59%） | 252/282（89.36%） | 15 | 20 | 0.499560 |
| temporal | 321 | 263/321（81.93%） | 265/321（82.55%） | 34 | 36 | 0.904975 |
| **overall** | **1529** | **1310/1529（85.68%）** | **1261/1529（82.47%）** | **155** | **106** | **0.002895** |

overall 的连续性校正 McNemar `χ²=8.828`，双侧 exact `p=0.002895`。因此结果从“两个点估计之差”升级为“固定 v4-flash 同栈下总体领先具有配对统计证据”；显著差异由 single-hop 驱动。

### 解释边界

该结论仍有三条硬边界：

- MemOS 侧只有一次答题运行，engram 侧是三次运行多数，前者没有 answer-run 误差带。
- 上下文预算差异（engram ~3614 tok/次 vs MemOS ~1059 tok/次）已由[预算剥离实验](budget-ablation.md) 量化并消除：扫 answerer 预算对齐 MemOS（1083 tok ≈ 1059）后，engram 极显著落后（−5.62pp，exact p=0.000006），领先随预算下降单调消失并反转。即原始 +3.20pp 完全由上下文预算驱动，不是记忆机制优势；engram 需约 2.1 倍 MemOS 预算才持平。
- deepseek-v4-pro 重判只保存了 MemOS overall 80.26%，没有逐题 verdict；其相对 engram 83.77% 的 +3.51pp 只是原始汇总差，不能声称配对显著。

因而可以声明 v4-flash 受控栈下的总体统计领先，不可将差异归因于记忆机制本身，也不可外推为通用系统排名。

## 复现资产

配对统计入口为 [`scripts/mcnemar.py`](../../../scripts/mcnemar.py)。从受控评测资产取得 `009-eval-runs/009-full-A-base/run-*/results-hybrid.jsonl` 与 `memos-parity/memos_judged_detail.json` 后运行：

```bash
python3 scripts/mcnemar.py \
  '009-eval-runs/009-full-A-base/run-*/results-hybrid.jsonl' \
  memos-parity/memos_judged_detail.json
```

可重建但代价高的逐题产物与 manifest 存放在受控评测资产位置。资产必须在使用前核对数据版本、模型 revision 和凭据清理状态；不得将私有数据、令牌或远端连接信息复制到本仓库。
