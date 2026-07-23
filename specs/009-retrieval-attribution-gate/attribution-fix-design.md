# 009 US1 归因 adapter 修复设计(fact 感知覆盖 + wide-pool outranked)

**日期**:2026-07-23 · **范围**:`cmd/locomo-bench/`(adapter,引擎零改,零答题成本) · **状态**:设计通过,待实现

## 背景:为什么首版归因无效

首版 US1 全量归因(commit 87400f8)跑出 **Q3=1458/1540(94.7%)**、`outranked_by` 恒空(0/1458),codex 判 US2 证据门 **STOP**。真实 trace 数据核实出两个根因:

- **根因 A(结构性)**:`outranked_by` 只在 `goldRank>0`(narrow top-K)时填,但 Q3 定义要求 `goldRank≤0`(gold 不在 top-K)。二者互斥 → Q3 的 `outranked_by` **永不可能非空** → SC-002 结构性不可满足。契约本意(SC-002「若在池内 outranked_by」)是 **pool 级**,实现却只算 narrow。
- **根因 B(架构不匹配)**:gold 覆盖判定 `chunkTurns[hit.Name]` 只认 chunk 名实体,但 008 store 是**混合库**(436 条 = 309 fact + 127 chunk),检索命中 **99.2% 是 fact**(45808:392)。fact 命中全被判 `covers_gold=false` → `goldRank=-1` 假性成立 → 94.7% 的题误扔进 Q3。例:conv2/q111 答案 fact `maria-is-trying-kundalini-yoga` 就在 rank 4,却因是 fact 被判非 gold。

**更深的约束**:fact 只有 **session 级**溯源(`source_session_id='conv2-sess19'`,`fact_source='extraction'`),**无 turn 级**;而 LoCoMo gold 是 turn 级(`D19:3`)。这正是 memory `coverage-diagnosis-2026-07-22` 记过的「turn@k 对 fact 级 assoc 失明」。归因复用了为 chunk 检索写的 turn-based coverage 机器,继承了同一失明。

## 决策:fact↔gold turn 用**确定性词法内容匹配**桥接

engram 真实答题检索单位是 fact,gold 是 turn,provenance 只到 session。三种桥接里选**内容匹配**(而非 session 级过粗、答案串不可得)。

**硬约束**:SC-004 要求重跑逐字节一致,而 embedding probe 报 `unstable` → 覆盖判定**必须词法确定性**,禁用 embedding cosine。

## 修复设计

### 1. fact 感知的 gold 覆盖(治根因 B)

`covers_gold` 改双路径,取 rank 最靠前者:

- **chunk 命中**(`chunk-c*`):沿用 `chunkTurns[name] ∩ gold_turns`(真实 DiaIDs,turn 级)。
- **fact 命中**:**session 门 + 方向包含**
  - session 门:`fact.source_session` 对应 session ∈ gold turns 的 session 集(用**可靠的** session 溯源防跨-session 假阳);
  - 方向包含:fact 实义词(去停用词)**≥ τ=0.8** 出现在该 gold turn 文本中(文本从已加载 LoCoMo dataset 取)。
  - fact 由源 turn 抽出 → 其实义词本应出现在源 turn,方向包含比对称 Jaccard 稳。
- 新增 `factCoversGoldTurn(factContent, factSession, goldTurnText, goldTurnSession, τ) bool`;`mappedGoldTurns` 合并两路。

### 2. wide-pool 排名 + outranked_by(治根因 A)

拆两个显式 rank,消除契约内部矛盾:

- **`gold_rank_topk`**:gold 在**答题器实际消费的 narrow top-K `hits`** 里名次;-1=不在。→ **仅用于象限分类**(gold 是否被答题器看到)。
- **`gold_rank_pool`**:gold 在 **wide pool `wideHits`** 里名次;-1=不在池。→ 用于 `outranked_by`。
- **`outranked_by`**:wide pool 中排在 `gold_rank_pool` 前的**非 gold 命中**,截前 `--outrank-cap`(默认 5)。Q3 现在能列出「谁把 gold 挤下 top-K」。

### 3. 象限分类(逻辑不变,喂修正信号)

```
gold 不可解析            → gold_unresolved
correct==nil            → retrieval_only
gold_rank_topk ∈ [1,K]  → correct? q1_ok : q2_answer_side   # 答题器看到 gold
否则 gold_rank_pool>0    → q3_us2_target                     # 排序挤出 top-K ← US2 靶心
否则                    → q4_extraction_side                # 池里都没有 = 抽取/召回缺
```

### 4. 契约同步(constitution III)

- `data-model.md`:`gold_rank` 一字段 → `gold_rank_topk` + `gold_rank_pool`;`covers_gold` 定义补 fact 路径(session 门 + 方向包含 + τ)。
- SC-002 措辞已含「(若在池内)outranked_by」,意图本就是 pool 级,**不改 SC**,实现补齐即可。

### 5. 测试(TDD,先失败)——补原测试盲区

原 golden 手喂匹配 chunkTurns 且只测 gold-in-topK,漏了真 Q3 与 fact 路径。新增:

- **fact 覆盖单测**:Kundalini fixture(fact content + source_session + 合成 gold turn 文本)→ 断言判真;实义词 <80% → 判假;跨 session → 判假。
- **真 Q3 单测**:gold 仅在 wide pool(rank>topK)→ 断言 `gold_rank_topk==-1`、`gold_rank_pool==池名次`、`outranked_by` 列池内非 gold 前几、`quadrant==q3_us2_target`。
- **τ 边界表驱动**:79% vs 81% 命中 → 判假/真。
- SC-004:改动全词法无 embedding 依赖 → 重跑逐字节一致。

## 成功判据

1. `CGO_ENABLED=0 go build ./... && go test ./cmd/locomo-bench -run 'Attribution|EmbedProbe'` 全绿;`git diff --name-only -- memory embedding provider store internal` 空。
2. 免费重跑(retrieval-only,`answer_calls=0`):**Q3 从 94.7% 塌到合理量级**、Q1 显著升(答案 fact 命中可见)、conv2/q111 归 Q1。
3. SC-002:答错 Q3 的 `outranked_by` 非空率 ≥90%。
4. 重跑两份 trace 逐字节一致(SC-004)。
5. τ、象限前后对比、方法学写入 `eval-log.md`。

## 范围边界

- 全在 adapter,引擎零改,零答题成本。
- **本版不做 US2 机制选择**——先让 Q3 可信,再看证据是否支持 US2(仍 gated:门①②③ + 授权)。
- 禁云/付费 reranker(死规则)。
