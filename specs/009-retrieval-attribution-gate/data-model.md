# Phase 1 Data Model: 归因门控的检索排序

实体为**评测诊断产物**(非引擎持久 schema)。US1 实体全在 adapter 内,序列化为 gitignored JSONL;US2 只新增引擎侧一个默认关的选项 + 一个诊断暴露字段。

## AttributionTrace(US1,一题一条 → `trace.jsonl`)

| 字段 | 类型 | 说明 | 来源 |
|---|---|---|---|
| `conv` / `q` | int | 题标识(join key,复用 `resultKey`) | dataset |
| `category` / `category_name` | int / string | 类别(1..4) | dataset |
| `gold_evidence` | []string | gold 证据原始 `D<session>:<turn>` | `qa.Evidence` |
| `gold_turns` | []string | 解析后 gold turn id 集合 | `evidenceReferences` |
| `retrieved` | []RetrievedHit | 有序 top-N 命中(见下) | `SearchWithDiagnostics` |
| `gold_in_pool` | bool | 宽候选池内是否存在 gold-covering hit | widePool 检索 + `chunkTurns` |
| `gold_rank` | int | 最靠前 gold-covering hit 的 1-indexed 名次;无=-1 | 计算 |
| `outranked_by` | []RetrievedHit | 排在 gold_rank 之前的非 gold hit(截前 M=5) | 计算 |
| `quadrant` | string | `q1_ok`/`q2_answer_side`/`q3_us2_target`/`q4_extraction_side`/`gold_unresolved` | join correct |
| `correct` | bool | 该题答对与否(join 来源标注) | 008 `results-hybrid.jsonl` |
| `correct_source` | string | 固定 `"008-us4-e2e/results-hybrid.jsonl"`(诚实标注单次观测) | 常量 |

### RetrievedHit(嵌套)

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | entry name |
| `rank` | int | 1-indexed fused 名次 |
| `rrf_score` | float64 | = `Result.Score`(融合分) |
| `covers_gold` | bool | `chunkTurns[name] ∩ gold_turns ≠ ∅` |
| `mapped_gold_turns` | []string | 命中的 gold turn id(可空) |
| `per_signal_ranks` | map[string]int | **US2 才填**(sem/kw/entity);US1 阶段省略(引擎未暴露) |

**校验规则**:`quadrant` 五值互斥且穷尽;`gold_rank=-1 ⟺ 无 gold-covering hit 在 top-N`;`gold_unresolved` 题不进四象限分母(edge case)。

## QuadrantDistribution(US1,聚合 → 分布表)

| 字段 | 类型 | 说明 |
|---|---|---|
| `category` | string | single_hop / multi_hop / temporal / (open_domain 仅列不作靶心) |
| `q1_ok` … `q4_extraction_side` | int | 各象限计数 |
| `gold_unresolved` | int | 单独桶,不入分母 |
| `total_gradeable` | int | 分母(排除 unresolved) |

**用途**:把诊断的"排序题"精确切成"Q3 US2 靶心"的可归因子集(SC-002)。

## EmbeddingDeterminismProbe(US1 附带 → `embed_probe.json`)

| 字段 | 类型 | 说明 |
|---|---|---|
| `n_queries` | int | 抽样 query 数 |
| `bit_identical_ratio` | float64 | 两次嵌入完全一致的比例 |
| `max_l2_delta` / `mean_l2_delta` | float64 | 向量 L2 偏差 |
| `verdict` | string | `deterministic` / `bounded`(δ<阈) / `unstable` |

## RankingOption(US2,引擎侧,默认关)

- `RetrieverOptions` 新增一个 bool 字段(命名待 tasks 定,如 `RankingRefine`),**零值 = 现三信号等权 RRF 行为逐字节不变**。
- 关联新增 `SearchDiagnostics.PerSignalRanks map[string]map[string]int`(entry→signal→rank),默认 nil;仅当调用方请求诊断时填充。**contract-first**:字段可加(additive),零值调用者不受影响。
- **状态迁移**:无持久 schema 变更(纯内存选项 + 诊断);不动 `store/migrations.go`。

## 不变量(跨实体)

1. US1 全部实体序列化仅含 benchmark 内容,**绝不含任何凭据**(沿用 journal.go 纪律)。
2. US1 产物确定性:同 store + 同题集 + 同检索配置 → 逐字段一致(SC-004)。
3. US2 关闭时,检索输出与现基线逐字节一致(SC-006 parity)。
