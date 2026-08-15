# Data Model: 确定性次模证据装填(045)

**Date**: 2026-08-16 | 全部为 harness 内结构(不落引擎 schema;工件只写 run-dir,gitignored)

## EvidencePool(单题候选池)

| 字段 | 类型 | 说明/校验 |
|---|---|---|
| QuestionID | string | conv+qid 复合键 |
| Candidates | []PoolCandidate | 宽池全量(≥1;池空 → 整题回退现行装配) |
| WidePoolSize | int | 记录用(max(6×topK,300)) |

**PoolCandidate**

| 字段 | 类型 | 说明/校验 |
|---|---|---|
| ID | string | stable entry ID(tie-break 键,升序) |
| Kind | enum{fact,chunk} | `chunk-` name 前缀判定 |
| Content | string | 非空 |
| FusedScore | float64 | RRF 融合分原值 |
| NormScore | float64 | 池内 min-max ∈ [0,1] |
| Shingles | map[uint64]struct{} | 词级 5-shingle 集合(hash 稳定:FNV-1a,不依赖 Math.random) |
| EstTokens | int | packEstimateTokens(Content),≥1 |
| CoverTerms | int | 该条覆盖的 query 内容词数(set-cover 增量分子) |

## PackingObjective(四项目标,cost-scaled 贪心)

| 字段 | 类型 | 说明 |
|---|---|---|
| WRel, WCover, WFac, WDiv | float64 | 冻结起点 3:1:1:1;LoCoMo 定稿后零重调 |
| Budget | int | 逐题锚 B_q(对照臂该题真实 answer-context tokens;缺失 → 全局均值) |
| State | greedyState | 已选集 S、已覆盖 query 词、已选间相似度累计(增量维护) |

**贪心步**: `argmax_e [WRel·NormScore(e) + WCover·Δcover(e) + WFac·Δfac(e) − WDiv·Δdiv(e)] / EstTokens(e)`;预算耗尽停;并列 → ID 升序。**singleton fallback**: 若 S 为空且最小 EstTokens > Budget → 选 NormScore 最高一条(允许超预算,唯一例外,审计标记)。

**PackedContext**(产物)

| 字段 | 类型 | 说明 |
|---|---|---|
| Selected | []string | 选中 ID 有序集(贪心序,渲染按融合分稳定还原) |
| EstTokensUsed | int | Σ EstTokens ≤ Budget(除 singleton 标记) |
| SingletonFallback | bool | 审计位 |
| Dropped | []DroppedReason | 被弃条目(前 50 条)+ 弃因(budget/redundancy/score) |

## AnswerInContext(离线诊断,US1 门)

| 字段 | 类型 | 说明/校验 |
|---|---|---|
| QuestionID | string | — |
| GoldAliases | []string | dataset adjudicated answers(只用于诊断/判题,不进运行时装配) |
| MatchedAlias | string \| null | 命中的别名(归一化后子串匹配) |
| InContext | bool | 三口径各算一次:current-k30 / packed / top150-full |
| UnmatchableInPool | bool | 池内任何条目都不含 gold → 审计单列(入分母) |

**规范化(冻结,plan R5)**: `lower(collapse-whitespace(strip))`,连续子串匹配,无分词器依赖。

## ReverifyReport(042 重验 ride-along)

| 字段 | 类型 | 说明 |
|---|---|---|
| Slice | {Convs:[0,1], Questions:304} | 与 043 pilot2 同 slice(可比性) |
| ValidRate | float | final-span 映射有效采集率(对照 042 的 5/5958) |
| Features | per-question | mean/p10/top1-top2(final span) |
| Discrimination | AUC+CI | 同 043 deepenAUC 口径(WMW tie-mean + bootstrap seed 43) |
| ChannelFlipRate | float | logprob vs 流式双通道终答一致性 |
| Verdict | enum{measurement-artifact-confirmed, signal-still-invalid, inconclusive} | 只陈述测量事实,翻案权在维护者 |

## 状态转移(仅两处)

- 旗标: off(默认,现行路径)→ on(装填路径);无中间态。
- 装填: 池空 → fallback-to-current(整题,审计记 pool-empty);预算内无可放 → singleton → 继续。

## 校验规则(工件写盘前)

- PackedContext.EstTokensUsed ≤ Budget 或 SingletonFallback=true(硬断言)。
- AIC 门工件:三口径题数一致(1540),分母含 UnmatchableInPool 题且审计清单齐。
- manifest 全字段(含 QuestionCount/BudgetAnchor 摘要)填满后才算 digest + seal(工程铁律)。
