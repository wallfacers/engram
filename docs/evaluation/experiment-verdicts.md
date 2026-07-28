---
title: 实验裁决索引
summary: 本文汇总已收口实验的可执行 verdict 与证据入口；不提供当前完整分数矩阵或未来功能承诺。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [experiment-verdicts]
tags: [evaluation, verdicts, evidence]
---

# 实验裁决索引

本文是已收口实验的唯一裁决入口；完整当前分数见[当前评测结果](results.md)，历史过程见下列证据链接。

## 已收口的 LoCoMo 杠杆

| 方向 | Verdict | 当前处理 | 证据 |
|---|---|---|---|
| 本地 reranker | closed-no-go | 不进入默认检索；coverage 增益没有兑现为端到端回答收益 | [历史实验台账](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 时间窗检索打分 | closed-no-go | 默认关闭；没有可行动的端到端收益 | [历史实验台账](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 强制答题 | protocol-only | 仅用于与同口径实验对齐，不能称为算法提升 | [历史实验台账](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| CURRENT DATE 答题侧锚点 | evidence-backed | 仅在数据集提供题目日期时注入；不改变 LoCoMo prompt | [时序分析](../archive/evaluation/temporal-t4-analysis-2026-07.md) |

## Feature 013

Feature 013 的最终状态是 `closed-no-go`：其探索结论不进入当前路线，也不代表已出货能力。保留原因、适用范围和原始分析见[Feature 013 历史证据](../archive/evaluation/temporal-t4-analysis-2026-07.md)。

## 判定规则

coverage、离线代理指标或单次差值都不能单独成为出货依据。对默认行为有影响的实验必须至少说明端到端结果、对照条件、噪声标尺、适用范围与明确 verdict；尚未收口的研究不得写入本索引。
