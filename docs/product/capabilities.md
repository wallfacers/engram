---
title: 当前能力边界
summary: 本文说明 engram 当前已交付能力及明确未实现的边界；不提供完整评测分数或未来路线承诺。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-29
canonical_for: [current-capabilities]
tags: [product, capabilities, lifecycle]
---

# 当前能力边界

本文是当前产品能力的唯一正本。实现结构以[记忆系统架构](../architecture/memory-system.md)为准，当前结果只在[评测结果](../evaluation/results.md)维护。

## 已交付

- `shipped-default`：每个 namespace 独立本地 SQLite（schema v6）、`memory_entries`、FTS5、provenance/event/supersession/revision，以及 keyword 检索。
- `shipped-default`：在依赖可用时，semantic 与 entity 信号可参与 RRF 检索；embedding 缺席时保留 keyword 离线降级。
- `shipped-default`：CLI 与 MCP 的显式 `ingest` 可抽取记忆；`write` 与 `add` 直接写入显式内容。
- `shipped-opt-in`：curation 已交付，但仅在显式开启或调用时执行。

## 明确未实现

记忆新鲜度与状态一致性尚未实现为完整能力；现有 provenance、supersession 或 side table 不能替代这项保证。问题定义和验收边界见[新鲜度 backlog](backlog/memory-freshness.md)，该页面是未实现的 proposed 工作项。

SaaS 习惯记忆未立项，也未实现；它不是当前产品能力。探索假设与停止条件见[习惯记忆探索](explorations/habit-memory.md)，该页面同样是未实现的 proposed 探索。

不可损失 Evidence Ledger、Semantic Episode View、Query-time Evidence Compiler
和 Grounded Trace 尚未实现，也没有批准进入实施。Event、Scene、Profile 与 graph
只是须独立消融的可重建 projection，不是当前能力或预定层级。边界和证伪顺序见
[查询期证据编译架构探索](explorations/benchmark-parity-memory-architecture.md)。

## 不应由实现痕迹推断的能力

实验代码、embedding side table、entity alias 或评测开关不自动表示 associative、temporal、multi-query、Doc2Query 或自动新鲜度机制已经出货。若能力状态变化，应先更新本页，再更新相关指南、路线和证据。
