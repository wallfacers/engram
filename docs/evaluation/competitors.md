---
title: 竞品与基准口径
summary: 本文说明竞品自报、论文高分和同栈复现的比较边界；不维护 engram 当前分数矩阵的副本。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [competitors]
tags: [evaluation, competitors, methodology]
---

# 竞品与基准口径

engram 的当前完整结果只在[当前评测结果](results.md)维护。本页用于说明哪些外部数字能比较，哪些只能作为厂商自报或复现目标。

## 比较原则

跨系统比较至少要记录数据版本与分母、answerer、judge、prompt、检索预算、上下文预算和聚合规则。仅当关键轴对齐时，差值才可解释为该受控栈下的比较；不同栈的 leaderboard 数字不能横向相减。

## 已完成的同栈复现

MemOS 已在 engram 的同款 answerer、embedding 与 judge 条件下完成 LoCoMo 复现。该复现说明其公开 leaderboard 与同栈表现存在显著 regime 差异，同时保留 answer rep 与上下文预算不对称等限制。完整过程、原始产物定位与诚实项见[MemOS LoCoMo 复现报告](reports/memos-locomo-reproduction.md)。

## 厂商自报边界

Mem0、MemOS 或其他厂商的公开数字可作为外部来源记录，但必须标为自报并保留原始口径链接。它们不能被改写为 engram 的相对排名，也不能取代当前结果正本。

Mem0 2025 论文只报告 LoCoMo，其结果与后续托管平台 New Memory Algorithm 的
LoCoMo/LongMemEval 自报不是同一套系统和口径。后者不可由开源 SDK 同栈复现，
因此只能作为数值方向锚。

022 在 2026-07-31 的单次 LoCoMo 命令未带 `--eval-protocol`，所以请求的 compiler flag
被静默忽略；同时没有使用与当前基线相同的 category-top-k/三次多数协议，并发生额外
answer/rewrite。它既不是 Compiler 证据、正式双基准结果，也不是 Mem0/MemOS 对照。
Pure-fact 与 recall×budget 扫描只支持内部负结论：压低上下文会同步损失 gold-source
survival，不能据此声明竞品优劣。数值与 artifact hashes 只在
[当前评测结果](results.md)维护。

## 论文成绩与机制记录

LoCoMo/LongMemEval 的完整系统分数、受控机制实验、工程依赖和口径差异统一记录在
[长期记忆系统成绩与机制证据登记](../research/high-scoring-memory-systems.md)。该记录
允许未跨过 90 目标线的论文提供机制证据，但不把跨论文 leaderboard 数字或完整 bundle
写成受控竞品结论。
