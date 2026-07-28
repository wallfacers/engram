---
title: LoCoMo Single 与 Multi-hop 失败诊断（2026-07）
summary: 本文冻结 LoCoMo single 与 multi-hop 的历史失败诊断；不描述当前默认检索机制或路线决策。
status: archived
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [archive-locomo-failure-diagnosis]
tags: [archive, evaluation, diagnosis]
outcome: historical-evidence
superseded_by: docs/evaluation/experiment-verdicts.md
---

# LoCoMo Single 与 Multi-hop 失败诊断（2026-07）

> **历史归档，不描述当前状态。** 当前实验结论请查看[实验裁决索引](../../evaluation/experiment-verdicts.md)。

## 保留原因

本诊断保留了 single-hop 与 multi-hop 错误的分类方法、候选池与 top-k 区分，以及“gold 是否可得”和“答案是否兑现”的分析边界。它曾为排序、深召回和多查询实验提供假设，但不替代后来实际的 GO/NO-GO verdict。
