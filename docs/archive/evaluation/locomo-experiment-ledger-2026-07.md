---
title: LoCoMo 实验台账（2026-07）
summary: 本文冻结已收口 LoCoMo 杠杆的原始结论和方法学边界；不提供当前默认路线或完整分数正本。
status: archived
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [archive-locomo-ledger]
tags: [archive, evaluation, locomo]
outcome: closed-no-go
superseded_by: docs/evaluation/experiment-verdicts.md
---

# LoCoMo 实验台账（2026-07）

> **历史归档，不描述当前状态。** 已收口杠杆的现行结论请查看[实验裁决索引](../../evaluation/experiment-verdicts.md)。

## 关键实验记录

本地 reranker 的 exact-turn coverage 从 77.012% 提高到 92.468%，但端到端回答从 83.70% 变为 83.64%，McNemar p=1.0，temporal 类净损 9 题。该实验确立了“coverage 增益不是 answer 增益”的证据边界。

强制作答在 LoCoMo 1540 题上把记录值从 83.70% 变为 84.22%，但同时改变拒答策略和 prompt，因此只可作为 protocol 对齐观察，不是算法改进。时间窗检索、assoc 及其他单次变化落在噪声地板或为负，未进入默认路径。

## 方法学遗产

每个候选杠杆都应保留同配置复跑、端到端结果、分母、answerer、judge、recipe 与判定门。代理覆盖率只能帮助诊断，不能替代出货 verdict。
