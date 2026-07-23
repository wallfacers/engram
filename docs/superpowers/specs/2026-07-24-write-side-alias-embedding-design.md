# 写入侧表示 —— dual-index alias 向量(提质型深召回,第二枪)

**Date**: 2026-07-24 | **Status**: Design(brainstorm 定稿,待 SDD) | **前序**: 009 归因门控 · 010 多查询检索(NO-GO)

## 背景与动机

009/010 三处独立印证同一瓶颈:**深层召回** —— 真靶心题的 gold fact 在宽池里中位 rank 71–90、无一进 top-30。010 把「query 侧分解 + RRF-of-RRF」做实并在门②证伪(gold 各子查询均弱命中,融合顶不动反挤出,net −8 掉出 top-30)。010 收口据此把提质方向**收窄到写入侧**:瓶颈是 gold fact 的**向量可发现性**(写入侧表示),而非 query 侧改写。

本设计是收窄后的第二枪:让一条 fact 从**多个语义角度**可发现,攻「单向量覆盖不了多样问法」这一根因。守 maintainer 杠杆哲学——**提质(同 top-k、不加量)、纯离线可移植、无付费杠杆**。

## 探明的链路事实(设计地基)

- **fact 抽取产出单句自包含句子**(已消解代词、显式主语),字段 `Fact`→`Entry.Content`(`memory/pipeline/pipeline.go:70-79,134-166`)。
- **被嵌入的文本 = `embedText(entry)` = `Trigger + "\n" + Content`**(`memory/embedder.go:83-90`);Trigger 只是 Content 截断。**抽取与嵌入之间零语义变换层**。
- **`aliases` 已在抽取时免费产出**("future question might use 的 paraphrase",`memory/prompt/memory_extraction.go:31`)、**已落库**(`memory_event_aliases`),但**从不参与嵌入**——只喂 keyword/temporal 通道(`retriever.go:668 aliasNames` FTS + `:756-758` LIKE join)。**语义(向量)通道从未见过 aliases。**
- **覆盖度探针(009 店 conv0)**:213 facts,**111 有 ≥1 alias(52%)**,143 alias rows;alias 是**短概念标签**(`self-acceptance`、`painting`、`LGBTQ+ advocacy`、`pride event`),非完整问法句。
- **`memory_embeddings.entry_name TEXT PRIMARY KEY`**(`store/migrations.go:101-105`)——**一 entry 一向量**;semantic 信号按 name 算 cosine(`retriever.go:405-445`)。

## 文献依据(方向坐实 + 关键修正)

- **Document Expansion by Query Prediction / doc2query**(Nogueira et al. 2019, arXiv:1904.08375):文档扩展用"预测该文档能回答的 query"追加到 doc 再索引,BM25 **+15%**;增益来自 term reweighting + synonym expansion。**警示(Appendix B)**:追加过多"reduce the contributions of the original text to the document representation"(稀释原文表示)。
- **Doc2Query++**(Kuo et al. 2025, arXiv:2510.09557):**"direct query appending injects semantic noise that degrades dense retrieval performance, as stronger encoders become sensitive to such distortions"** —— naive 拼接到**强 dense encoder(bge-large 正是)会掉分**(Table 5:Append 列在 dense 上几乎不涨甚至倒退)。解法 **Dual-Index Fusion**:原文向量与扩展向量**分开索引、max-pool 聚合 + 加权融合**,才把"有害噪声"转成"互补信号"(dense +9~12% N@10)。结论:**coverage not quantity**。

**修正**:因此本设计**不走单向量 append**(会稀释 bge-large 向量、大概率门②假阴性),而走 **dual-index alias 向量**——engram 化的 Doc2Query++ Dual-Index,融合用现成 RRF/max-pool(**无 α 调参**,守 tuning-free)。

## 机制:dual-index alias 影子向量

对**有 alias 的 fact**,把其 aliases 合并嵌入成一条**独立 name 的"影子向量"**(name 约定如源 fact name + `#alias` 后缀),存入 `memory_embeddings`(独立 PK row,无 schema 变更)。检索侧 semantic 信号命中影子向量时,**归并回源 fact**:同一源 fact 的 text-cosine 与 alias-cosine **取 max** 作为该 fact 的单一 semantic 相似度(**max-pool,不双重计票**),semantic 通道每 fact 仍只贡献一个排名,再进现有三信号 RRF。**源 fact 的 text 向量完全不动。**

- 无 alias 的 fact / 所有 chunk:无影子向量,semantic 逐字节不变(退化保真)。
- 有 alias 的 fact:text 向量不动 → **无稀释**;只**新增**一条"从概念/别名角度"的召回通道 → 纯增益或中性。
- 融合守 tuning-free:semantic 通道内 **max-pool**(text/alias cosine 取 max,无参数),信号间复用引擎现有 **RRF(k=60)**,**无新可调权重、无 α**。

## US 划分(与 010 同构,归因分离)

### US1 — 引擎 dual-index alias 向量(free 单测验,ENGINE)

- **写入**:embedder 为有 alias 的 fact 额外产一条 alias 影子向量(aliases 合并成嵌入文本),存 `memory_embeddings`(影子 name)。
- **检索**:retriever semantic 信号识别影子 name → resolve 回源 fact name;同一源 fact 的 text-cosine 与 alias-cosine **取 max**(max-pool,semantic 每 fact 单一分,不双重计票),再进三信号 RRF(k=60);**影子 name 绝不出现在最终 `[]Result`**。
- **契约边界(宪法)**:无 schema 变更(`memory_embeddings` 复用,影子为独立 name row);tuning-free(RRF/max,无 α);纯 Go/offline;无付费 reranker。这是**预期的算法改动**(有 alias fact 的 semantic 召回变强)→ 按宪法 IV 声明意图变更 + 新 baseline;无 alias fact 与所有 chunk 保 parity。
- **退化(宪法 V)**:embedder 未配置(离线)→ 无任何向量,keyword+entity 照常,aliases 仍走原 keyword 通道;影子向量缺失 → semantic 少一路,per-signal 降级不影响全局。

### US2 — adapter re-embed + 三道门(ADAPTER,引擎零改,GATED)

- **re-embed**:在 009 固化店上产 alias 影子向量(**只重嵌入不重抽取**,near-free,需 box 8001 bge-large endpoint)。
- **门① 纯 Go 契约(free)**:US1 单测 + `git diff -- memory embedding provider store internal` 空。
- **门② 分层召回诊断(near-free,retrieval-only,止损)**:baseline(无影子)vs treatment(有影子),同 query single 检索,复用 `buildAttributionTrace`/`evidenceRecallAt`。**分层判据**——按"该题 gold fact 是否有 alias"分层,**主看"gold 有 alias"子层**的 gold rank delta(全局会被 48% 无 alias 层稀释)。子层净升 top-30 才算机制有信号;否则 NO-GO,不烧门③。
- **门③ 端到端配对(box 窗口,repeats=3)**:baseline 店 vs treatment 店,**唯一变量 = alias 影子向量**,canonical recipe + deepseek mem0-aligned judge,配对 McNemar。**提质硬约束天然成立**:top-k=30 与 answer-context 结构上不变(仅向量新增,无扩池),**无 010 的加量伪装可能**,仍记账 `answer_context_tokens` 坐实。GO = 目标类(open-domain / multi-hop)above-noise ↑ + 非目标类不回退 + context 不涨;基线 = bge-large 后 85.45%。

## 验证形状(TDD)

**US1 引擎单测(free,确定性,离线)**:
- `TestAliasVector_NoAliasParity`:无 alias entry(chunk + 48% 无 alias fact)semantic 结果逐字节等于现状。
- `TestAliasVector_ShadowMergesToSource`:构造 alias 命中影子向量 → 源 fact 被 semantic 召回、取 max、**影子 name 不泄漏到结果**;确定性可复算(stub embedder)。
- `TestAliasVector_ShadowResolvesUnique`:同源 fact 的 text + alias 双命中只产一条源 fact 结果(去重归并)。
- `CGO_ENABLED=0 go test ./memory` 全绿 + 既有 parity golden 不破。

**门②分层判据**:`gold_entered_top_30 > gold_left_top_30`(有 alias 子层净正)、mean gold rank 前移、coverage@30 delta;子层正向即机制有信号。

## 边界 / YAGNI / 升级路径

- **YAGNI(第一枪不做)**:不改抽取 prompt(只消费现有 aliases);不做 LLM propositional 改写;不碰 chunk 表示;融合不引入可调 α。
- **升级路径(future,数据驱动,不承诺)**:门②有信号但**覆盖是瓶颈(52%/短标签)**→ 增强抽取产**问法式 paraphrase**(Doc2Query++ 的 coverage-guided QG:topic 覆盖 + 每 fact 多问法,需重抽取);α 加权融合(Doc2Query++ α≈0.3–0.6)作为 tuning-free RRF 之外的可选项,仅在证明有据时。

## 诚实局限(不夸大)

- 天花板受 **52% 覆盖 + 短标签**限——第一枪定位是 **near-free 探针 / 可能小赢**,先回答"多问法语义锚点撬不撬得动 gold 可发现性",非大跳分。
- dual-index **消除了单向量 append 的稀释风险**(文献实证 + 源向量不动),但覆盖/标签质量的天花板要靠升级路径(增强抽取)才能抬高。
- 越不过门③ = 诚实 NO-GO,保留为默认关能力(与 008 reranker / 009 cluster-sweep / 010 分解同族处理)。

## 宪法对齐

- **I 本地优先/离线**:检索路径无 query-时 LLM;alias 向量写入时嵌入(与 fact 同源 embedder),纯 Go/offline。✅
- **II 引擎/适配器分离**:dual-index 融合机制(不可重造)入引擎;re-embed 编排 + 三道门在 adapter,US2 引擎零改。✅
- **III 契约优先/命名空间**:无 schema 变更;无 alias/chunk 退化保真;影子 name 不泄漏契约。✅
- **IV 评测回归门**:三道门,门②分层召回诊断止损 + 门③配对 McNemar 决胜;US1/US2 提交分离;有 alias fact 声明意图变更 + 新 baseline。✅
- **V 优雅降级/诚实规模**:离线/缺向量 per-signal 降级;越不过 NO-GO 保留诊断;coverage 不作 verdict。✅
- **死规则**:全程无付费/云 reranker;不撑 top-k、不扩池(dual-index 是表示提质,非加量)。✅

## 参考文献

- Nogueira, Yang, Lin, Cho. *Document Expansion by Query Prediction*. arXiv:1904.08375 (2019).
- Kuo, Chiu, Ma, Cheng. *Doc2Query++: Topic-Coverage based Document Expansion and its Application to Dense Retrieval via Dual-Index Fusion*. arXiv:2510.09557 (2025).
