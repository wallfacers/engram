# Phase 0 Research: 归因门控的检索排序

grounding 基于对 `cmd/locomo-bench`(evidence.go/coverage.go/chunks.go/journal.go/main.go)与 `memory/retriever.go` 的实读。全部 NEEDS CLARIFICATION 已解。

## D1 — US1 复用现有 gold 解析 + chunk→turn 映射,不重造

- **Decision**:US1 归因直接复用 harness 现有 `evidenceReferences(qa.Evidence)`/`evidenceSessions`/`exactTurnRecall`(evidence.go)与 `chunkTurns map[string][]string`(chunks.go,chunk entry name → `D<session>:<turn>` 列表)、`coveredGoldTurns`(coverage.go)。
- **Rationale**:gold 解析与 fact/chunk→turn 映射已在库内成熟运行(coverage/sweep 路径在用),口径与现有 turn recall 一致,避免二次实现漂移。US1 只需在其上加"逐 hit rank + 归因判定"。
- **Alternatives**:重写独立映射器——被拒(口径漂移风险 + 重复代码)。

## D2 — per-signal rank 归属:延到 US2 引擎增量,US1 严格零引擎改动(关键决策)

- **Decision**:US1 的 trace **只记录 adapter 侧可导出的字段**——fused `rank`、`rrf_score`(= `Result.Score`)、`gold_rank`、`outranked_by`、mapped-gold-turn 标志。**per-signal rank(sem/kw/entity)延到 US2**,由 US2 的引擎增量在 `SearchDiagnostics` 上新增 `PerSignalRanks` 暴露(contract-first,默认行为不变)。
- **Rationale**:per-signal rank 是 `fuseRRF` 内部量,**不在 `memory.Result` 公共契约上**。US1 要保持"引擎零改"(FR-006/SC-005),就不能拿到它。硬性现在暴露 = 违反"US1 adapter 不碰引擎"。而四象限 + `gold_rank` + `outranked_by`(判断"gold 在池内但被谁压")**不依赖** per-signal rank 即可成立——它们全部可从引擎返回的有序 `[]Result` + `Score` + chunk→turn 映射 adapter 侧导出。per-signal rank 是"哪一路失败"的加诊断,自然与 US2 引擎改动同批做(那时也需要它挑机制)。
- **Alternatives**:
  - (A) US1 就地给引擎加 diagnostics 暴露 per-signal rank —— 被拒:破坏 US1 引擎零改,且违反"adapter 缺引擎入口须 STOP 显式立增量"应走 US2。
  - (B) adapter 侧重算 per-signal rank(自己跑 BM25/cosine/entity)—— 被拒:重实现引擎算法 = 明令禁止,且必漂移。
- **对 spec 的细化**:spec FR-001 列了 per-signal rank;此处细化为"US1 记 fused rank + rrf_score;per-signal rank 属 US2"。不缩小 scope(四象限证据完整),只把一个诊断字段移到它本就该在的引擎增量批次。data-model 与 contract 按此定稿。

## D3 — `gold_rank` / `outranked_by` / 四象限判定算法(adapter 侧,确定性)

- **Decision**:对每题取有序 `hits []Result`(retrieval-only,top-N,N 用与答题一致的 top-k,默认 30,可配)。
  - `gold_hit_indices` = 满足 `chunkTurns[hit.Name] ∩ goldTurns ≠ ∅` 的 hit 下标集合。
  - `gold_in_pool` = 在更宽候选池(widePool,复用 chunks.go 的 wide 检索)里存在任一 gold-covering hit。
  - `gold_rank` = `gold_hit_indices` 的最小值 + 1(1-indexed);无则记 `-1`。
  - `outranked_by` = 排在 `gold_rank` 之前(下标更小)的非 gold-covering hit 的 `{name, rank, rrf_score}` 列表(截断前 M,M 默认 5)。
  - **四象限**(join 答题对错):`gold_rank∈[1,k] & correct` → Q1 正常;`gold_rank∈[1,k] & !correct` → Q2 答题侧;`gold_in_pool & gold_rank>k(或不在top-k)` → Q3 US2 靶心;`!gold_in_pool` → Q4 抽取/召回侧。
- **Rationale**:全部字段 adapter 可导出;判定互斥、确定性(同 store+同题→同结果,SC-004)。
- **Alternatives**:用 turn@k 聚合替代 fact 级——被拒(已知 turn@k 对 fact 级 assoc 失明,spec 明列坑)。

## D4 — 四象限与答题对错的 join 源

- **Decision**:join 现有 `.locomo-run/008-us4-e2e/results-hybrid.jsonl` 的 `{conv,q,correct}`(journal.go `result` 结构已有)。US1 的归因跑用**相同固化 store + 相同检索配置**(hybrid, top-k30, chunk-quota),保证 trace 的 hits 与产生 correct 的那次答题同分布。
- **Rationale**:near-free 的核心——不重跑答题,复用已判对错的结果。key 用 `(conv,q)`(resultKey 已存在)。
- **风险与缓解**:若答题非确定性(temp=1.0,见 locomo-answer-nondeterministic)使检索↔答题对不齐——US1 检索是确定的(retrieval-only 无采样),对错取自已归档结果;trace 明确标注"correct 来自 008 归档单次观测",不宣称因果,仅作分象限的先验。US2 决胜门才做同机配对差分。

## D5 — embedding 查询确定性探针

- **Decision**:新 `embed_probe.go`,对一组代表性 query 各调 `embedding.Client.Embed` 两次,比对返回向量:报告 bit-identical 比例 + 最大/均值 L2 δ。作为 US1 附带诊断,独立于 trace 主流程,可单独 flag 触发。
- **Rationale**:诊断所指"embedding 运行退化"若真会稳定复现;纯 Go 可验,是 US2 端到端判定必须先排除的噪声源。
- **Alternatives**:忽略——被拒(会把 embedding 抖动误读为排序增益)。
- **Note**:探针只读,调用现有 `embedding.Client` 公共接口,引擎零改。

## D6 — US2 机制候选与选择协议(留给 US1 证据)

- **Decision**:US2 机制在 US1 分布表(Q3 竞争事实象限主导模式)出炉后,从三候选择一:
  - **score-aware RRF**:融合带回 cosine/BM25 幅度边距(打破纯 rank,但保持 tuning-free——用相对边距非拟合权重)。
  - **近重去重/MMR**:候选池内近似 fact 去重,防副本挤占 top-k。
  - **实体/时间锚约束**:题含 anchor 时约束候选(与 T-3/T-4 正交)。
- **Rationale**:证据驱动 + tuning-free 守可移植;避免盲设机制(008 rerank 教训)。
- **本 plan 不收敛机制**:tasks 阶段在 US1 证据到位后据实选一;plan 只固定"三候选 + 选择协议 + 三道门",不预判赢家。

## D7 — 三道门与反证基线

- **Decision**:①纯 Go 契约门(`CGO_ENABLED=0` 构建 + parity golden 关时逐字节 + 新排序单测);②离线归因门(US1 trace 复跑,Q3 象限 gold 平均排名上升,无云/付费杠杆);③端到端决胜门(同机配对 hybrid vs hybrid+US2,唯一变量=排序机制,先目标类后全量 1540,McNemar above-noise + overall 及任一非目标类不显著回退)。
- **反证须超越**:legacy temporal Δ−0.3pp/p=1.000(003 eval-log:149)、008 US1 reranker 端到端 −0.06pp/p=1.0。coverage 增益不作 GO 依据。
- **Rationale**:宪法 IV + 死规则;与 [temporal-t4-design.md](../../docs/temporal-t4-design.md) 三道门同构,统一 engram 涨点判定纪律。

## 未决 / 显式外部依赖

- US2 决胜门需机器窗口(远端 vllm Qwen 栈)——非阻塞 US1。
- 008 固化 store 与 `results-hybrid.jsonl` 须可用(假设成立,见 spec Assumptions)。
