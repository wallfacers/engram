---
title: 当前能力边界
summary: 本文说明 engram 当前已交付能力及明确未实现的边界；不提供完整评测分数或未来路线承诺。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [current-capabilities]
tags: [product, capabilities, lifecycle]
---

# 当前能力边界

本文是当前产品能力的唯一正本。实现结构以[记忆系统架构](../architecture/memory-system.md)为准，当前结果只在[评测结果](../evaluation/results.md)维护。

## 已交付

- `shipped-default`：每个 namespace 独立本地 SQLite（schema v7）、`memory_entries`、FTS5、provenance/event/supersession/revision，以及 keyword 检索。
- `shipped-default`：在依赖可用时，semantic 与 entity 信号可参与 RRF 检索；embedding 缺席时保留 keyword 离线降级。
- `shipped-default`：CLI 与 MCP 的显式 `ingest` 可抽取记忆；`write` 与 `add` 直接写入显式内容。
- `shipped-default`：Evidence Ledger 保存不可变原始 Evidence、完整 projection lineage 和 tombstone/restore/privacy-purge 生命周期；`memory_ingest_v2` 在 extractor 缺席时仍先保存原文。
- `shipped-opt-in`：curation 已交付，但仅在显式开启或调用时执行。

## 明确未实现

记忆新鲜度与状态一致性尚未实现为完整能力；现有 provenance、supersession 或 side table 不能替代这项保证。问题定义和验收边界见[新鲜度 backlog](backlog/memory-freshness.md)，该页面是未实现的 proposed 工作项。

SaaS 习惯记忆未立项，也未实现；它不是当前产品能力。探索假设与停止条件见[习惯记忆探索](explorations/habit-memory.md)，该页面同样是未实现的 proposed 探索。

Semantic Episode、Query-time Evidence Compiler、Grounded Trace、Event/Scene/Profile
影子视图和一次 structured-gap refetch 已有 engine 或 benchmark 实验代码，但尚未获得
正式双基准配对证据，也没有进入默认 MCP/CLI 请求路径。它们不是已交付产品能力或
预定层级；边界、负结果和当前 `HOLD` 状态见
[查询期证据编译架构探索](explorations/benchmark-parity-memory-architecture.md)。

## 已验证规模边界

Evidence/projection lineage 的 100,000 条夹具在单次批量读取中使用固定 500-ID 分批，
实测恰好 200 次 SQL query；2026-07-31 的一次性基准为约 857 ms、53.0 MB 和 1.41M
allocations。这个结果证明没有 per-candidate N+1，并支持“单用户约 100k 条级”的诚实
功能边界；它不是延迟 SLO，也不构成 ANN、百万 token 或并发服务承诺。Privacy purge
在 WAL reader 阻塞 checkpoint 时先逻辑失效并返回可重试状态，reader 释放后可完成
truncate checkpoint。

## 不应由实现痕迹推断的能力

实验代码、embedding side table、entity alias 或评测开关不自动表示 associative、temporal、multi-query、Doc2Query 或自动新鲜度机制已经出货。若能力状态变化，应先更新本页，再更新相关指南、路线和证据。
