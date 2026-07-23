# Data Model: 写入侧表示 —— dual-index alias 向量

**Phase 1** | 无 schema 变更;下列为逻辑实体(复用现有表 + name 约定)。

## AliasShadowVector(alias 影子向量)

- **载体**: `memory_embeddings` 的一行,`entry_name = <源 fact name>#alias`(独立 PK row),`model`/`dims`/`vector` 同 text 向量列。
- **来源**: 源 fact 的 `memory_event_aliases` 全部 aliases 合并、去掉 Content 已含词后的短文本,经与 fact 同一 embedder 嵌入。
- **约束**: 每有 alias 的 fact 至多一条;`memory_entries` 无对应 row(影子只有向量);源 fact 无 alias / alias 去冗后为空 → 不产生。
- **生命周期**: `storeFact` 在 `PutAliases` 后 enqueue;aliases 变更即重嵌入;源 fact 删除后成孤儿,检索归并时因源 `GetByName` 失败被丢弃。
- **评测侧隔离(H1/research D7)**: US2 影子只写入 009 店的**副本**(treatment 店);**原店(baseline 用)绝不写影子**——物理两店保证 baseline parity 与决胜门唯一变量=影子向量。
- **半 entry 一致性(M2)**: 影子有向量、`memory_entries` 无 row,只应被 `vectorRankContext` semantic 归并消费;其它 `memory_embeddings` 消费者(Backfill/export/snapshot/curation)不得把它当真 entry。

## MaxPooledSemantic(归并 semantic 分)

- **定义**: 检索时同一源 fact 的 `text_cosine` 与 `alias_cosine`(其影子向量对 query 的 cosine)取 `max`,作为该 fact 单一 semantic 相似度。
- **约束**: 每 fact 在 semantic 通道只贡献一个排名(不双重计票);在 cosine 全集、topK 截断**之前**归并;影子 name resolve 回源、不进 ranks/结果、同源双命中去重为一条。
- **下游**: 进现有三信号 `fuseRRF`(k=60),与 keyword/entity 融合;无 α、无新可调权重。

## ContextParityCheck(提质约束记录)

- **字段**: 配对端到端里 baseline(无影子)vs treatment(有影子)的 `answer_context_tokens`、`final_top_k`、`subquery_count`(=1,本 feature 不分解)。
- **判定**: `final_top_k` 两臂恒等 = 30;treatment `answer_context_tokens` 不显著 > baseline(涨即加量 NO-GO)。落 `.locomo-run/011-*/context_parity.jsonl`。

## StratifiedRecallDiagnostic(分层召回诊断)

- **字段**: 每题 `gold_has_alias`(该题 gold fact 是否有 alias)、`single_gold_rank`/`treatment_gold_rank`、`gold_entered/left_top_30`、`coverage_at_30_delta`;按 `gold_has_alias` 分层汇总。
- **判定**: **主看「gold 有 alias」子层** gold 是否净升 top-30(全局会被 48% 无 alias 层稀释);coverage 仅诊断不作 verdict;子层不升即 NO-GO 止损。落 `.locomo-run/011-recall/`。
- **符号口径**(承 010 勘误):明确记录 delta 方向(建议 `treatment − baseline` 的 rank 为负=前移=变好),避免 010 式符号歧义。
