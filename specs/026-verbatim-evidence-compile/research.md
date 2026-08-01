# Research: 查询期 verbatim 证据编译

**Date**: 2026-08-01 | **Branch**: `026-verbatim-evidence-compile`

本文件记录 026 的 Phase 0 决策。所有文献证据均经 alphaXiv 核实(2026-08-01)。

## D1: verbatim-first 编译策略的具体形式

**决策**: 原文装得下 → `KEEP`/`FETCH_SOURCE` 保留原始 span;装不下 → `EXTRACT(span)`(按 relevance 排序)仍不够才 `MERGE`(逐句验证 source)。**复用 022 `internal/extract/extract.go` 已有的 raw-fit / over-cap / EXTRACT-sufficient 双条件 gate**,不重写。

**Rationale**:
- Fidelity-Before-Structure(2601.00821):固定 pipeline 内 verbatim chunks 比 LLM-extracted artifacts 高 15.9pp(LoCoMo)/22.0pp(LongMemEval-S),机制是 lossy distillation(write-time 丢弃的信息 retrieval 救不回),69% 失败是 extractor 从未写下的 write-time loss。**query-time 选原文 > write-time 提取**。
- Retain-or-Consolidate(2607.17545):原文装得下优先保留;宽松预算下 MERGE 显著负(−0.107 [−0.204, −0.013])。MERGE 仅当原文装不下且 EXTRACT 仍不够时才允许。
- 022 的 extract.go 已实现 raw-fits→KEEP、over-cap→EXTRACT、EXTRACT 充分→拒绝 MERGE、EXTRACT 不充分→逐句验证 MERGE 的 gate——026 直接承接,不重写。

**Alternatives considered**:
- write-time 提取/压缩(022 pure-fact 已 NO-GO,73.70%,single-hop −16.0pp)→ 排除
- 全 EXTRACT 无原文优先 → 违背 Fidelity 证据,排除
- LLM planner 主导编译 → 违反纯 Go deterministic 约束,留作 023 独立工作,排除

## D2: 需要哪些 arms

**决策**: 四个编译 arms——`legacy-count`(按条数装填,现状基线)、`exact-token relevance`(022 已实现)、`deterministic extractive`、`verbatim-first`(026 核心新臂)。

**Rationale**:
- 022 T069 定义了四臂集合但只落地 exact-token;其余三臂是 026 的增量。
- 每臂固定候选池 byte-replay,输出确定性 bundle+trace(FR-003)。
- legacy-count = 对照(现状按条数装填);exact-token = 022 已交付;extractive = 确定性 span 选择;verbatim-first = 原文优先双态。
- arm 集合必须完整才能做"编译策略差异"的干净配对(只差策略,不差检索/表示)。

**Alternatives considered**:
- optional local Planner arm → 依赖训练/模型,属 023 范围,026 不做(记为边界)

## D3: verbatim-first 是否需要 source-span 索引增量

**决策**: **不需要新增 schema 或索引**。复用 022 的 `SourceResolver.Resolve(ids)` 批量读取 active Ledger(022 已实现,禁 Search/query)。verbatim-first 的"原始 span 回收"= 沿候选 lineage 的 source ID 批量 Resolve,不新增 N+1。

**Rationale**:
- 022 T068 已通过窄 `Resolve(ids)` bridge 批量读取,验证禁 Search/query。verbatim 回收沿已有 lineage 走同路径,不新增存储。
- 若配对显示 span-level 选择精度不足(如需要源内子跨度定位),再评估增量索引——作为显式 contract increment(宪法 II),不预建。

**Alternatives considered**:
- 预建 source-span 索引 → 无必要性证据,违背"不预建 projection"纪律,排除

## D4: 配对基线如何建立

**决策**: 复用 022 冻结协议(cap 3600、hybrid retrieval、Qwen3.6-35B-A3B-FP8 answerer + deepseek-v4-flash judge、3 次答题多数)。**若 022 accepted baseline 未收口(HOLD),先建立当前 chunk_900 可引用分数作为对照**,再在**同一 store、候选逐字节一致**下配对(025 纪律)。

**Rationale**:
- 宪法 IV 回归门要求 comparable slice 确认不回归。
- 025 教训:不同 store = 候选漂移 = 不可配对。026 复用 024/025 的 `025-control-v2`(chunk_900,evidence `01KYYK…`)与 treatment 同 store 配对协议。
- LoCoMo 6.4% 答案键噪声(Penfield audit:99/1,540 改分错误,multi-hop 9.9%):小 delta 不单独作 promotion 依据,优先看 multi-hop/temporal 大 delta 方向;必要时用 audit 的 errors.json 做敏感性核对。

**Alternatives considered**:
- 等待 022 完全收口再启动 026 → 保守但阻塞;026 与 022 共享资产,可在 022 baseline 可引用后立即配对,先行实现 arms(不阻塞)。

## 文献证据小结(alphaXiv 核实)

| 证据 | 论文 | 对 026 的含义 |
|---|---|---|
| verbatim > artifact 15.9/22.0pp,write-time loss 69% | Fidelity-Before-Structure(2601.00821) | 原文优先是受控实证的最优表示 |
| MERGE 宽松预算显著负 | Retain-or-Consolidate(2607.17545) | 原文装得下优先保留 |
| query-conditioned 构造;recall 分界 | LazyMem(2607.22690) | compiler 效力上界由 candidate oracle 决定 |
| 6.4% 答案键错误,完美上限 93.6% | Penfield Labs audit(dial481/locomo-audit) | 高分局分不可信;配对须记录噪声 |
