# 设计：写入侧 Doc2Query 伪查询影子向量（Feature 012）

**日期**：2026-07-24
**状态**：brainstorm 收敛，待 SDD 形式化（specify → plan → tasks）
**承接**：010 多查询（query 侧，门② NO-GO）、011 dual-index alias 影子（写入侧，门② NO-GO）。二者共同把提质方向收窄到「gold fact 的**向量可发现性（写入侧表示）**」。

## 1. 问题与动机

009 归因诊断坐实：端到端瓶颈**不是** top-K 排序，而是**深层召回**——gold fact 在宽候选池里中位 rank 71-90，`--top-k 30` 根本捞不到。010 证伪了 query 侧再拆分（gold 对每个子查询都弱命中，融合顶不动）。011 证伪了 alias 影子（短名词标签对称抬噪，净零）。

剩下的唯一未证伪方向：**改善 gold fact 自身的向量可发现性**，且必须**不加量**（不撑 top-k / 不扩 context —— maintainer 杠杆哲学：只认提质，反感加量）。

**核心假设**：dense 双编码器（bge-large）在 fact 上的可发现性缺口，主要来自**陈述句↔提问句的嵌入不对称**。fact 以陈述句存储（"Jon lost his banking job on 2023-01-19"），真实提问是疑问句（"When did Jon lose his job?"）。若在写入侧为每条 fact 生成「它能回答的问句」并作为**查询形状**的影子向量，检索时 max-pool 归并，可闭合这一不对称。

## 2. 文献依据（alphaXiv，硬规矩）

**Doc2Query++（arXiv 2510.09557，Kuo et al. 2025）**，dense/Contriever 结果直接支撑本设计，并解释 011 为何失败：

1. **dense 双编码器吃 query-generation 的红利，但只有 dual-index max-pool 能拿到**——naive 拼接注入语义噪声、反伤强编码器（"stronger encoders become sensitive to such distortions"）。→ 背书 011 引擎已选的 max-pool 机制；单向量 append 是死路。
2. **dense 偏爱 LLM 生成的 human-like 完整问句，而非关键词式扩展**——"dense retrievers benefit more from the semantically richer, human-like queries produced by LLMs over the lexical keyword focus of Doc2Query series"。→ **011 的 aliases 是关键词式短标签（Doc2Query 系那一类），伪查询是 human-like 问句——这就是 011 该失败、012 该赢的文献级分界线。** Dual-Index vs 次优基线 N@10 +2.6~12.1%（Table 5）。
3. **coverage > quantity，过量饱和后反降**（sparse 峰值 ~100 条后掉）。但 BEIR 是多句 passage 需 BERTopic/KeyBERT 保主题覆盖；**LoCoMo 的 fact 是单命题句、单 topic，跳过主题建模机器，每 fact 2-3 条多样化问句足矣**。

**诚实风险（写进 spec 的 SC）**：文献增益在 BEIR 长 passage（词汇失配大）上取得；LoCoMo 短 fact 本就接近答案句，失配更小 → 增益可能更薄。这正是**门② 作为 kill-switch** 存在的理由。

## 3. 架构（沿用 011 的 US1/US2 双 commit 切分）

引擎/适配器分离硬约束：`git diff --name-only -- memory embedding provider store internal` 在 US2 提交须为空；US1/US2 分属不同 commit。

### US1 — 引擎（`memory/`，独立 commit）

- **migration v5**（现有已到 v4）：新表 `memory_fact_queries(entry_name TEXT, query TEXT, PRIMARY KEY(entry_name, query))`。伪查询存这里，**不**塞进 `memory_event_aliases`——保护 011 shipped 的 alias 语义，不动已发布迁移。
- **新 `#query` 影子**：embedder 为有伪查询的 fact 产一条 `<name>#query` 影子向量，内容=该 fact 全部伪查询**原样 join 嵌入**。**不**走 `aliasEmbedText` 的「与 content 重叠即丢词」滤器——查询要保留 content 词（问句本就和 fact 共享实体词，这正是我们要的信号）。
- **retriever 源码零改（关键复用）**：011 的 `vectorRankContext`(retriever.go:821) max-pool 归并**已是内容无关的**——对任何 `resolveShadow` 识别的影子折回源 fact 取 `max`、去重单票、进 RRF k=60。**只需把 `#query` 后缀加进 `resolveShadow`（embedder.go）**，retriever 自动 max-pool `max(text_cosine, alias_cosine, query_cosine)`，源码不动。**无 α**——守 011 无调参先例 + 宪法 tuning-free。
- **inert-by-default 退化保真**：无 `memory_fact_queries` 行的店 → 无 `#query` 影子 → `!hasShadows` 快路径逐字节不变，parity golden 不破。**US1 引擎能力常开但默认惰性**；伪查询的**写入**才是被 gate 的动作。

### US2 — 适配器（`cmd/locomo-bench/`，独立 commit）

- **`--doc2query off|baseline|treatment` 臂**；**方案 A 两店隔离**（照搬 011）：两臂都把 009 canonical 店复制到各自 `<run-dir>/doc2query-store`，baseline 副本剥离 `#query` 影子（assert 0），treatment 保留；canonical 从不被写。
- **伪查询来源 = 对 009 店已存 fact 做 LLM backfill**（doc2query over stored passages）：遍历 store 内 fact，LLM 生成 2-3 问句 → 写 `memory_fact_queries` → 嵌 `#query` 影子。**不重跑整段会话抽取**（省钱，且从自足 fact 句生成问句本就是 doc2query 的标准形态）。

### US3 — shipped 写入路径（独立 story，**仅门③ GO 后才做**）

把伪查询生成**折进现有抽取 LLM 调用**：抽取 prompt 增 `"queries":[…]` 字段（每 fact 2-3 问句），`pipeline.storeFact` 存入 `memory_fact_queries` + `Enqueue(QueryShadowName)`。**零额外写入 LLM 调用**（只是更富的 JSON 输出）。**此 story 改的是默认写入行为，故 default-off（config 开关）且 deferred**——门③ 未 GO 前不接线，避免默认栈携带 query 影子。US1 引擎能力对无 query 行的店惰性零改，与此解耦。

## 4. 三道门（成本感知，止损纪律焊死）

| 门 | 成本 | 内容 | 判据 |
|---|---|---|---|
| ① 纯 Go 契约 | free | `#query` 影子内容=查询、不泄漏、parity 逐字节、去重单票、退化——照搬 011 测试形状 | 先失败→实现→全绿；`git diff -- memory…` 分离 |
| ② 离线分层召回诊断 | near-free（box bge-large，retrieval-only 不调答题） | backfill 伪查询→嵌影子→同 011 分层诊断；主看「gold」子层 | **gold 子层 gold 净升 top-30（entered>left，mean rank 前移，coverage@30 ↑）= 非对称抬升坐实**；否则 **NO-GO 止损，不启动门③** |
| ③ 端到端配对 McNemar | box 答题窗口，repeats=3 | 两臂 recipe 逐字一致，唯一变量=`#query` 影子 | GO 须 above-noise + 非目标类不回退 + context 不涨 + top-k 恒等；超越 010/011 反证基线 |

**判据的反面**：若门② 复现 011 的「对称抬噪、coverage@30 delta 恒 0」，则伪查询在 LoCoMo 短 fact 上同样顶不动 → NO-GO，与 011 同族诚实处理（引擎 `#query` 机制作纯 Go/退化保真/可移植能力保留，adapter flag 默认关，不进默认栈、不报为赢）。

## 5. Success Criteria（供 SDD 细化）

- **SC-001** 退化保真：无伪查询 fact + chunk semantic 逐字节等于现状；parity golden 不破。
- **SC-002** max-pool 归并正确 + 去重单票 + `#query` 影子名不泄漏进结果；确定性单测（无 LLM）。
- **SC-003** `CGO_ENABLED=0` 构建+测试通过；无云 reranker 依赖；无 α；migration v5 在独立 tx 内 bump schema_version（不改已发布 v1-v4）。
- **SC-004** 提质非加量：两臂 `final_top_k=30` 恒等，treatment `answer_context_tokens` 不显著 > baseline。
- **SC-005** 分层召回（门②诊断）：目标类「gold」子层 gold 升进 top-30 / coverage@30 ↑。
- **SC-006** 判定诚实：GO/NO-GO 由配对 McNemar 决定，超越 010/011 反证基线；门② NO-GO 即止损不耗门③。
- **SC-007** 引擎/适配器分离：US2 提交 `git diff -- memory embedding provider store internal` 空；US1/US2 分属不同 commit。

## 6. 我替 maintainer 拍的 3 个设计选择（已获批，可后续推翻）

1. **无 α**：文献用 α=0.5 调参涨点，本设计不引入调参旋钮，守 011/宪法 tuning-free；机制赢或不赢，不靠调 α 掩盖。
2. **每 fact 2-3 问句**：fact 单 topic，跳过 BERTopic/KeyBERT 主题建模机器（那是为多句 passage 保覆盖用的）。
3. **新 `memory_fact_queries` 表 + `#query` 影子**，而非复用 011 alias 表：保护 011 shipped 语义，代价=少量重复代码。

## 7. 非目标（YAGNI）

- **不做** BERTopic/HDBSCAN/KeyBERT 主题建模（fact 单 topic，无必要）。
- **不做** α 调参 / query 数量扫描（守 tuning-free；量少即够）。
- **不做** query 侧扩展（HyDE 等）——受同一「gold 表示弱」上限约束，010 已证伪 query 侧。
- **不引入**任何付费云 reranker/recall 模型作评分杠杆（死规矩）。

## 8. 收口

结论（GO/NO-GO + 子层/全局 delta、p 值、context 对比、coverage 诊断）写入 `docs/locomo-score-levers.md` 台账 Feature 012；越不过则保留 `--doc2query` 默认关（与 008 reranker / 010 分解 / 011 alias 同族诚实处理）。**box 空闲必停。凭据只走 env/隧道。WSL2 长跑 setsid 分离。**
