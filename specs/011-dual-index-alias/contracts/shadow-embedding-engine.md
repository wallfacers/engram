# Contract: dual-index alias 影子向量(引擎,US1)

冻结 US1 引擎对外/对内行为契约。实现照此,测试断言此。

## 影子 name 约定

- 影子 name = `<源 fact name> + "#alias"`。`#` 不在真 entry name 字符集,可无歧义 strip 回源。
- helper(引擎内)：`aliasShadowName(factName) string`、`resolveShadow(name) (source string, isShadow bool)`。

## 写入契约(`embedder.go`)

- `embedOne(ctx, name)`:
  - `name` 非影子 → **现有行为逐字节不变**(`GetByName`→`embedText`→`Put`)。
  - `name` 为影子(`resolveShadow` 命中)→ 取源 fact 的 `memory_event_aliases`;合并去掉源 Content 已含词得嵌入文本;文本为空 → 不 Put(no-op);否则 `client.Embed` → `vectors.Put(影子 name, model, vec)`。**不调 `GetByName(影子)`**。
- 影子枚举(供 `Backfill` / US2 re-embed):引擎导出 `AliasShadowNames(ctx) ([]string, error)` = `memory_event_aliases` distinct `entry_name` 各映射 `#alias`。
- `pipeline.storeFact`:`PutAliases` 之后,若该 fact 有非空 aliases → `Enqueue(aliasShadowName(entry.Name))`。

## 检索归并契约(`retriever.go: vectorRankContext`)

- 对 `LoadAllForModel` 全 candidates 算 cosine;**截断前**按 `resolveShadow` 归并:每源 fact 的 semantic 相似度 = `max` over {其 text 向量 cosine, 其影子向量 cosine}。
- 影子 name resolve 回源;结果 `ranks`/最终 `[]Result` 中**只含源 fact 真实 name**,影子 name 绝不出现;同源双命中去重为一条,semantic 只计一票。
- 归并后排序取 topK → `ranksFromOrder` → 进 `fuseRRF`(k=60)。**无 α、无新可调权重。**

## 退化契约(宪法 V)

- 无 alias 的 fact、所有 chunk、有 alias fact 的 **text 向量**:semantic 相似度与最终排序**逐字节等于现状**。
- `embedding client == nil`:`vectorRankContext` 返回空(现有行为),semantic 缺席,aliases 仍走 keyword 通道(`aliasNames`/LIKE)。
- 影子命中但源 fact `GetByName` 失败(supersede/删):丢弃该命中,不产悬空结果、不 panic。
- alias 去冗后为空:不产影子,等价无 alias。

## 半 entry 一致性核查(M2,承 analyze)

影子是「有向量、`memory_entries` 无 row」的半 entry,**只应被 `vectorRankContext` semantic 归并消费**。实现 MUST 核查其它 `memory_embeddings` 消费者不把影子当真 entry:
- `Backfill.NamesMissingModel`:不因影子缺 entry row 而反复 enqueue 失败(影子的「应有」由 `AliasShadowNames` 定义,已有向量即不缺)。
- export/snapshot:不把影子导出为条目。
- curation:不评审影子。
- 任何「遍历向量 → `GetByName`」的路径遇影子 name(`resolveShadow` 命中)MUST resolve 回源或跳过。

## 测试契约(`memory/*_test.go`,离线 stub embedder,确定性)

- `TestAliasShadow_NoAliasParity`:无 alias fact + chunk 的 semantic 结果与排序逐字节等于现状。
- `TestAliasShadow_MergeLiftsSource`:gold fact text cosine 弱、影子 cosine 强 → max-pool 后源 fact semantic 排名上升进入结果。
- `TestAliasShadow_DedupSingleVote`:同源 text+影子双命中 → 结果一条、semantic 一票。
- `TestAliasShadow_ShadowNameNeverLeaks`:任何影子命中 → 结果只含源 fact name。
- `TestAliasShadow_EmbedOneShadowBranch`:影子 name embedOne → 嵌入 aliases 合并去冗文本、Put 影子向量;空文本 no-op。
- `TestAliasShadow_Degenerate`:client nil / 孤儿影子 / 空 alias 不 panic、per-signal 降级。
- 全绿:`CGO_ENABLED=0 go test ./memory`;`git diff --name-only` 仅 `memory/`。
