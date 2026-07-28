---
title: 竞品与基准口径
summary: 本文说明竞品自报与同栈复现的比较边界；不维护 engram 当前分数矩阵的副本。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
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
