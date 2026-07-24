# Feature 012: 写入侧 Doc2Query 伪查询影子向量

**Branch**: `012-doc2query-shadow` | **Date**: 2026-07-24
**Brainstorm 设计**: [../../docs/superpowers/specs/2026-07-24-doc2query-pseudo-query-shadow-design.md](../../docs/superpowers/specs/2026-07-24-doc2query-pseudo-query-shadow-design.md)
**承接**: 010 多查询（query 侧，门② NO-GO）、011 dual-index alias 影子（写入侧，门② NO-GO）

## 问题

009 归因诊断坐实端到端瓶颈是**深层召回**——gold fact 在宽候选池里中位 rank 71-90，`--top-k 30` 捞不到。010 证伪 query 侧再拆分；011 证伪 alias 影子（关键词式短标签对称抬噪、净零）。剩余唯一未证伪方向：**改善 gold fact 自身向量可发现性**，且**不加量**（不撑 top-k / 不扩 context）。

## 假设

dense 双编码器（bge-large）在 fact 上的可发现性缺口，主要来自**陈述句↔提问句的嵌入不对称**。为每条 fact 生成「它能回答的问句」作**查询形状**影子向量、检索时 max-pool 归并，可闭合这一不对称。**非对称抬升机理**：gold fact 的伪查询 Q'≈真实提问 Q（cosine 高），非 gold fact 的伪查询与 Q 无关（cosine 低）→ max-pool 大抬 gold、小抬噪声。

## 文献依据（alphaXiv）

Doc2Query++（arXiv 2510.09557）dense/Contriever：(1) dense 只吃 **dual-index max-pool** 红利，naive 拼接反伤强编码器；(2) dense 偏爱 **LLM human-like 完整问句 > 关键词式扩展**——这正是 011 aliases 失败、012 伪查询该赢的分界；(3) coverage>quantity，但 LoCoMo fact 是单命题句、单 topic，**每 fact 2-3 问句足矣**，跳过 BERTopic 主题建模机器。**诚实风险**：增益在 BEIR 长 passage 上取得，LoCoMo 短 fact 失配更小 → 增益可能更薄 → 门② 作 kill-switch。

## User Stories

### US1 — 引擎 `#query` 伪查询影子向量（P1，FREE，ENGINE，inert-by-default）
有伪查询的 fact 产 `<name>#query` 影子向量（伪查询原样 join 嵌入，不丢词）；检索时 `vectorRankContext` **复用 011 已有的内容无关 max-pool 归并**（只需 `resolveShadow` 认 `#query` 后缀，retriever 源码零改），`max(text_cosine, query_cosine)` 折回源 fact、去重单票、进 RRF k=60。无 `memory_fact_queries` 行的店逐字节不变。**独立 commit（engine）。**

### US2 — adapter backfill + 三道门 gated（P2，ADAPTER，GATED）
`cmd/locomo-bench` 对 009 固化店做 **LLM backfill**（遍历已存 fact 生成 2-3 问句 → `PutFactQueries` → 嵌 `#query` 影子，**不重抽取**）；`--doc2query off|baseline|treatment` 三臂 + 方案 A 两店隔离（照搬 011）。过三道门（纯 Go 契约 / 分层召回诊断止损 / 端到端配对 McNemar），证 top-k=30 与 answer-context 不涨。**引擎零改。独立 commit（adapter）。**

### US3 — shipped 写入路径（P3，DEFERRED——仅门③ GO 后才做）
抽取 prompt 增 `"queries":[…]` 字段（每 fact 2-3 问句，零额外 LLM 调用），`pipeline.storeFact` 存 `memory_fact_queries` + `Enqueue(QueryShadowName)`。改默认写入行为，故 **default-off（config 开关）+ deferred**。门③ 未 GO 不接线。

## Success Criteria

- **SC-001** 退化保真：无 `memory_fact_queries` 行的店 + chunk semantic 逐字节等于现状；parity golden 不破。
- **SC-002** max-pool 归并正确 + 去重单票 + `#query` 影子名不泄漏进结果；确定性单测（无 LLM）。
- **SC-003** `CGO_ENABLED=0` 构建+测试通过；无云 reranker；无 α；migration v5 独立 tx bump schema_version（不改 v1-v4）。
- **SC-004** 提质非加量：两臂 `final_top_k=30` 恒等，treatment `answer_context_tokens` 不显著 > baseline。
- **SC-005** 分层召回（门②诊断）：目标类 gold 子层 gold 升进 top-30 / coverage@30 ↑。
- **SC-006** 判定诚实：GO/NO-GO 由配对 McNemar 决定，超越 010/011 反证基线；门② NO-GO 即止损不耗门③。
- **SC-007** 引擎/适配器分离：US2 提交 `git diff --name-only -- memory embedding provider store internal` 空；US1/US2/US3 分属不同 commit。

## 非目标（YAGNI）

不做 BERTopic/HDBSCAN/KeyBERT 主题建模；不做 α 调参 / query 数量扫描；不做 query 侧扩展（HyDE，受同一上限约束）；不引入任何付费云 reranker（死规矩）。

## 收口

结论写入 `docs/locomo-score-levers.md` 台账 Feature 012；越不过保留 `--doc2query` 默认关（与 008/010/011 同族诚实处理）。**box 空闲必停。凭据只走 env/隧道。WSL2 长跑 setsid 分离。**
