---
title: 记忆系统架构
summary: 本文定义当前记忆写入、存储、检索与 curation 的实现边界；不承诺实验性 side table 已成为默认能力。
status: stable
audience: [maintainers, agents, users]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memory-architecture]
tags: [memory, architecture, retrieval, storage]
---

# 记忆系统架构

本文描述当前已实现的记忆路径。来源归属与宿主边界见[Provenance 架构](provenance.md)，用户可操作命令见[CLI 使用指南](../guides/cli.md)。

## 写入与抽取

只有显式 MCP 或 CLI `ingest` 才会触发记忆抽取。`write` 与 `add` 直接写入调用方提供的内容，不会把聊天或宿主上下文隐式转换为记忆；这一边界让调用方能够决定数据进入时机。

## 存储原语

每个 namespace 使用独立的本地 SQLite 文件，当前 schema 为 v6。`memory_entries` 与 FTS5 是主记录和全文检索原语；provenance、event、supersession、revision 用于保存来源、事件、替代与修订关系。存储表存在不等于每一种推理机制已经出货。

## 检索与降级

默认检索组合 keyword，存在 embedding 时可选 semantic，并可将 entity 信号用 RRF 合并。embedding 不可用时系统必须保留 keyword 的离线降级路径，而不是伪造 semantic 结果。associative、temporal、multi-query 和 Doc2Query 等实验机制不因 side table 或代码路径存在而自动成为默认能力。

## Curation

curation 是 `shipped-opt-in`：实现已交付，但不会在普通写入后默认运行，必须由调用方显式开启或调用。它不等同于完整的新鲜度或状态一致性解决方案；这些仍是[未实现 backlog](../product/backlog/memory-freshness.md)。

## 证据与维护

实现事实以 `store/migrations.go`、`memory/entrystore.go` 和 `memory/retriever.go` 为准。变更写入、检索或 curation 语义时，应同步更新本页与[当前能力边界](../product/capabilities.md)。
