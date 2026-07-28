---
title: 基准扩展路线
summary: 本文列出仍有效的评测基准扩展方向与进入条件；不把历史计划或未验证栈描述为当前结果。
status: proposed
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [benchmark-roadmap]
tags: [evaluation, roadmap, benchmarks]
---

# 基准扩展路线

这是未实现的 proposed 评测路线，不代表当前已完成的 benchmark 覆盖。已验证结果见[当前评测结果](results.md)。

## 优先方向

- 在冻结 recipe、answerer 与 judge 的前提下扩展 LongMemEval 与 LoCoMo 的交叉验证。
- 为新基准先建立数据版本、样本数、类别、写入策略、检索预算和聚合方式的可审计记录。
- 优先补足多次重复、冻结 transcript 重判和低成本对照，而不是直接追逐单一最高分。

## 进入条件

任何新 benchmark 在成为当前结论前，都必须具备可重跑 recipe、明确分母、产物保留位置和结果正本条目。旧计划与本地模型栈设置仅作为[历史计划](../archive/README.md)保留，不能绕过这些门。
