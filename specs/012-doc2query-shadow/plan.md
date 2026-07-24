# Implementation Plan: 写入侧 Doc2Query 伪查询影子向量

**Branch**: `012-doc2query-shadow` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

## Summary

给引擎一个 `#query` 伪查询影子能力：有伪查询的 fact 产一条 `<name>#query` 影子向量（伪查询原样嵌入，**不**丢词）；检索复用 011 已有的**内容无关 max-pool 归并**（`vectorRankContext`，retriever.go:821）——只需把 `#query` 后缀加进 `resolveShadow`，retriever 源码零改即 `max(text,alias,query)` 折回源 fact、去重单票、进 RRF k=60（**无 α**）。US1 引擎能力对无 `memory_fact_queries` 行的店惰性零改（退化保真、单测即验、free）。US2 在 `cmd/locomo-bench` 对 009 固化店 **LLM backfill 补伪查询影子（不重抽取）**，过三道门。US3（改抽取 prompt 的 shipped 写入路径）**deferred，仅门③ GO 后做**。

## Technical Context

- **Language**: Go 1.25.0（CGO 禁用，纯 Go）
- **复用面（US1 注入点）**：`retriever.go:821 vectorRankContext`（已内容无关 max-pool，**源码零改**）、`embedder.go:88 embedOne`（加 `#query` 分支）+ `resolveShadow`(embedder.go:28，泛化认 `#query`)、`entrystore.go:476 PutAliases`（`PutFactQueries` 仿此）、`store/migrations.go`（追加 v5）、`Backfill`/`Enqueue`（补 `#query`）。
- **Storage**: 复用 009 固化 SQLite（`009-bge-chunks-store`，bge-large 1024d + chunks）；**migration v5** 新增 `memory_fact_queries`（现有已到 v4，不改 v1-v4）。baseline/treatment 复制到 run-local `doc2query-store` 后只 backfill 不重抽取。产物 gitignored `.locomo-run/012-*/`。
- **Testing**: `CGO_ENABLED=0 go test ./store ./memory ./memory/pipeline`；US1 parity + 归并正确性 golden（stub embedder，无 LLM）；US2 backfill/分层诊断单测（mock caller）+ 端到端配对 McNemar。
- **Target**: 本地 WSL2 开发；US2 决胜门在 box vllm Qwen 栈 + box bge-large 8001 + deepseek mem0-aligned judge。
- **Constraints**: 无 query 行的店 text 向量逐字节不变；无 α、无付费 reranker（死规矩）；**final top-k=30 不变、answer-context 不涨**（提质硬约束，涨即加量判负）；US2 期间 `git diff -- memory embedding provider store internal` 空。

## Constitution Check

| 原则 | 判定 | 依据 |
|---|---|---|
| **I 本地优先/离线** | ✅ | 检索路径无 query-时 LLM；`#query` 影子与 fact 同源 embedder、纯 Go/offline；无 query 行的店惰性零改 |
| **II 引擎/适配器分离** | ✅ | max-pool 归并已在引擎（011）；`#query` 影子生成入引擎，backfill 编排 + 三道门留 adapter；US2 引擎零改 |
| **III 契约优先/命名空间** | ✅ | migration v5 非破坏新增、不改 v1-v4；无 query 行退化保真；`#query` 影子名不泄漏 |
| **IV 评测回归门** | ✅ | US2 三道门，分层召回诊断止损 + 端到端配对 McNemar + context-parity；US1/US2/US3 提交分离 |
| **V 优雅降级/诚实规模** | ✅ | 空 queries/缺向量/离线 per-signal 降级；越不过 NO-GO 保留能力；短 fact 增益薄的风险诚实标注 |
| **死规则（无付费 rerank）** | ✅ | 全程无云/付费 reranker；不撑 top-k、不扩池、不加 context（表示提质非加量） |

## 关键设计决定（brainstorm 已定）

1. **无 α**：文献用 α=0.5 调参涨点，本设计守 011/宪法 tuning-free，赢在机制不在旋钮。
2. **每 fact 2-3 问句**：fact 单 topic，跳过 BERTopic 主题建模机器。
3. **新 `memory_fact_queries` 表 + `#query` 影子**（非复用 011 alias 表）：保护 011 shipped 语义。
4. **retriever 源码零改**：011 max-pool 已内容无关，`resolveShadow` 泛化即扩展。
5. **US3 deferred**：改抽取 prompt 的默认写入行为，仅门③ GO 后接线（default-off）。

## 风险与止损

**首要风险**：LoCoMo 短 fact 词汇失配比 BEIR 长 passage 小 → doc2query 增益可能薄、甚至复现 011 对称抬噪。**止损**：门② near-free retrieval-only，子层不前移或 coverage@30 delta ≤ 0 即断，不启动门③（省 box 答题窗口）。与 008/010/011 同族诚实处理。
