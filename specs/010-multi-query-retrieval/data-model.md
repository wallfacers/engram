# Phase 1 Data Model: 多查询检索(multi-query retrieval)

无持久化 schema 变更(宪法 III:`SearchMulti` 非破坏、不碰 SQLite migrations)。以下是运行时/契约层实体。

## SubQueries(子查询数组)

调用方分解出、传给引擎的 `[]string`。

| 字段 | 类型 | 说明 |
|---|---|---|
| (元素) | `string` | 一个子查询;规范化时 `TrimSpace` + 丢空串 + 精确去重 |

- **不变式**:含原 query 兜底;adapter 侧 `len ≤ 4`(策略上限);引擎侧对任意 `len` 有定义。
- **退化**:规范化后 `len==0` → 空结果;`len==1` → 短路等价 `Search`。

## FusedRanking(融合排序,引擎内部)

`SearchMulti` 二级融合的中间态,不对外导出,决定返回顺序。

| 字段 | 类型 | 说明 |
|---|---|---|
| `perSubqueryLists` | `[][]string`(有序 doc name) | 每子查询 `SearchWithDiagnostics` 精检得到的 L_i(深度 = 内部候选池 `k*candidateMultiplier`) |
| `ranks` | `[]map[string]int` | 每 L_i 经 `ranksFromOrder` 转成的 rank map(1-based) |
| `fused` | `[]embedding.Scored` | `fuseRRF(ranks...)` 结果;`Score = Σ_i 1/(60+rank_i(d))`(未命中记 0) |

- **输出**:`fused` 截断到 top-k → 逐条 `GetByName` 装成 `[]Result`(与 `Search` 同一装配路径)。
- **`Result.Score` 语义**:`len>1` 时为 RRF-of-RRF 分,**不与单查询 `Search` 的分可比**(契约明示);`len==1` 短路,Score 与 `Search` 逐字节相同。
- **共同命中优先**:被多个 L_i 命中的 doc 因累加,`Score` 高于同 rank 单命中 doc → 排名更前。

## ContextParityCheck(提质约束记录,adapter 侧)

端到端配对里证明「提质不加量」的记账,写入 `.locomo-run/010-*/`。

| 字段 | 类型 | 说明 |
|---|---|---|
| `arm` | `string` | `single`(基线)/ `multi`(分解) |
| `final_top_k` | `int` | 喂答题的最终条数,两臂须相等(=30) |
| `answer_context_tokens` | `int`(逐题) | 喂答题器的 context token 数;两臂分布对比 |
| `subquery_count` | `int`(逐题,multi 臂) | 实际分解出的子查询数(≤4;=1 即退化) |

- **判负条件**:`multi` 臂 `answer_context_tokens` 相对 `single` **显著上升** → 隐性加量,端到端门 NO-GO。
- **判 GO 前提**:`final_top_k` 两臂恒等且 context 不显著涨(SC-004)。

## DecompositionPolicy(分解策略,adapter 侧)

`cmd/locomo-bench/decompose.go` 的纯调用方逻辑,不入引擎。

| 属性 | 说明 |
|---|---|
| 输入 | 原 question、答题 LLM provider |
| 输出 | `[]string`(≤4,含原 query 兜底) |
| 退化 | LLM 失败/超时/返回 >4/全同质 → `[]string{question}`(端到端等价现基线) |
| 确定性 | 分解本身走 LLM(temp 依 provider);退化路径确定。离线单测用 mock provider 断言退化与上限 |

## 关系图

```
question ──decompose(adapter,LLM)──▶ SubQueries []string
                                          │
                        SearchMulti(engine, 只收 []string)
                                          │
              len==1 ? ──yes──▶ SearchWithDiagnostics(parity 短路)
                 │no
                 ▼
        每子查询 SearchWithDiagnostics(pool 深度) ─▶ perSubqueryLists
                 │  ranksFromOrder × N
                 ▼
        fuseRRF(ranks...) ─▶ fused ─top-k─▶ []Result ─▶ 答题(top-k=30, context 不涨)
```
