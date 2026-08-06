# Feature Specification: Agentic 多步记忆导航

**Feature Branch**: `029-agentic-memory-navigation`

**Created**: 2026-08-06

**Status**: Closed

**Input**: 用户描述："走 A 方向——Agentic 多步记忆导航"。承接 028 收口后的新方向调研（docs + alphaXiv）：检索侧单次被动召回（top-k 一次出）的表示/排序改进已 021 六次证伪、写侧 event 结构已 027/028 三次证伪；本次转向**推理驱动多步导航**——让模型像人一样"先查一层、看结果、再决定查哪一层/跟哪条线索、够了就停"，参考 [MemCog](https://www.alphaxiv.org/abs/2605.28046)（LoCoMo 92.98 / LongMemEval 95.80）与 [NapMem](https://www.alphaxiv.org/abs/2607.05794)（记忆金字塔 + RL 导航）。

## 背景（为什么是这条线）

- **已证伪的不能重复**：检索侧单次表示/排序改进（reranker、doc2query、assoc graph、时间窗、IRIS 等 021 六次）全 NO-GO；写侧 event 结构替换原文（027 7B / 028 教师 / 028 训练）三次 NO-GO。两者共同结论：**换个表示或换个排序，救不了"深度召回 + 跨证据组装"**。
- **本 feature 的假设**：短板可能在"检索是一次性的、推理不能介入"。MemCog 证明多步导航 + 推理交错在被动 QA 上不降分且提升（LoCoMo 92.98 vs 单次 HyperMem 89-92，且小 context backbone 下差距更大——低预算时推理介入检索的收益更明显）；NapMem 消融显示**主动导航本身**（非 RL）就值 +6.7pp（w/o navigation 54.08 vs 带工具 60.77 量级），RL 再叠 +6-14pp。这与 engram "预算下提质"（不大力出奇迹）的偏好吻合——多步导航的目标是**同预算内让推理救回单次检索漏掉的证据**，不是扩大召回广度。
- **与已证伪写侧 event 的关键区别**：MemCog/NapMem 的结构化导航**保留原文**（raw 层级始终在），层级/链接只是导航的 affordance；027/028 证伪的是"用结构化事件**替换**原文"。本条保留原文不替换。

## 阶段化（门禁驱动，不盲烧）

- **阶段 0（US1，零成本诊断）**：现有 store 上量化"单次检索漏掉、但多步导航理论上能救回"的题占比与机制（gold 是否在池、单次 top-k 是否够、漏的是"换查询能救"还是"表示缺失救不了"）。决定本线是否有救回空间。
- **阶段 1（US2，最小先导）**：复用 027/028 的 84 题配对纪律，本地强模型（现有 35B answerer）做**推理驱动多步检索**（不改存储结构、不训练），固定步数上限 + token 记账，配对比较"多步导航 vs 单次检索基线"。008 铁律：多步 ≥ 单次才 GO。
- **阶段 2（US3，可选）**：若平铺多步已转化，再验证 MemCog 式层级导航（保留原文 + 中间总结 + 类型化链接）是否进一步提分/省 token。
- **阶段 3（US4，SaaS 可选）**：若零训练多步已转化，再考虑 NapMem 式 RL 训练导航策略（SaaS 线，单独口径）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 零成本诊断：多步导航的救回空间（Priority: P1）

先不实现任何导航，对现有 store 的 temporal + multi-hop 错题做分诊：单次检索 top-k 捞不到 gold 的题里，有多少是"换查询词/换粒度/跟线索能救回"（→ 多步导航有救），有多少是"表示里根本没有"（→ 救不了，直接 STOP 省掉全部导航成本）。

**Why this priority**: 根除 021「没查 gold 在不在池就硬上」的教训。多步导航是加推理成本的机制；若单次检索已覆盖绝大部分 gold，导航增量空间为零，不投。已有先验：coverage 诊断显示 199/200 gold 由 chunk 承载、023 residual 显示 gold 中位 rank 71-90（深层召回不足）——深层是导航可能救回的点，但必须实测。

**Independent Test**: 对冻结 store 的 84 题（temporal 59 + multi-hop 25），分别记录：①gold 是否在候选池（全对话 oracle）；②单次 top-k=30 是否捞到 gold；③捞不到时，用「换查询改写 / 扩大粒度 / 实体线索跟链」这三类多步动作的**模拟**能否把 gold 捞进池。产出三分类计数 + 抽样人审。

**Acceptance Scenarios**:

1. **Given** 冻结 store 与 84 题,**When** 逐题记录 gold 在池 / 单次 top-k 捞到 / 模拟多步可救,**Then** 报告三分类计数；若"多步可救"占比 ≥20% → 进入阶段 1，否则记录负结论 STOP。
2. **Given** "多步可救"题,**When** 抽样人审,**Then** 确认是可救回的真实证据（非 judge/标注伪影），并归因到「换查询」/「换粒度」/「线索跟链」三类机制。
3. **Given** gold 根本不在池的题占比高,**When** 判定,**Then** 确认导航救不了表示缺失，STOP 记录负结论（008 纪律，不硬投）。

---

### User Story 2 - 最小先导：推理驱动多步检索（Priority: P1）

在现有 chunk store 上（不改存储、不训练），让本地强模型做 ReAct 式多步检索：`搜索 → 评估中间证据 → 决定下一步（换查询/跟实体/深挖/停止）→ 组装证据 → 作答`。与 027/028 同一批 84 题、同一 answerer/judge、固定步数上限配对，比较端到端正确率。

**Why this priority**: 这是「推理介入检索」的最小可证实验，也是唯一 GO 门（008 铁律）。MemCog 的导航是 prompt/ReAct 驱动的（不训练），NapMem 消融证明导航本身（非 RL）就有独立价值——所以零训练先导是成立的、也最省钱。

**Independent Test**: 同一 store/子集/answerer/judge 下，跑「多步导航」与「单次检索基线」配对，repeats≥3，McNemar；同时报告步数分布与 token 记账（导航消耗单独记账，最终答案证据在预算内）。

**Acceptance Scenarios**:

1. **Given** 同一 84 题子集,**When** 只开多步导航,**Then** 端到端 majority ≥ 单次基线（008 铁律）→ GO 进阶段 2/3；若负收益或噪声内，STOP 记录（不进入默认路径）。
2. **Given** 多步导航,**When** 检查 token,**Then** 最终喂给 answerer 的证据在既有 answer-context 预算内（多步是"推理救回证据"，不是"加预算大力出奇迹"）；若必须超预算才转化，单独口径登记并说明。
3. **Given** 导航失败/超步数,**When** 触发,**Then** fail-closed 退回单次检索结果作答（宪法 V 优雅降级），不产生空答案。

---

### User Story 3 - 结构化层级导航（Priority: P2）

若平铺多步已转化，构建 MemCog 式层级导航：**保留原文**（raw 层级）+ 可导航的中间总结（page/section）+ 类型化关联链接（related_to / temporal_next / caused_by / contrasts_with），暴露浏览/读取/跟链工具。验证结构导航相对平铺多步是提分还是省 token。

**Why this priority**: 这是「省 token / 提精度」的工程增益，不是涨点承诺；必须在平铺多步已验证转化后才值得投入（门禁驱动）。与已证伪 event 结构的关键区别：**不替换原文**，层级是导航索引，检索最终回到原文。

**Independent Test**: 同一导航 agent 下，「结构化导航 vs 平铺多步」配对，同预算比较正确率与 token 消耗；层级投影可重建（config-hash 幂等）。

**Acceptance Scenarios**:

1. **Given** 阶段 2 已 GO,**When** 只变导航结构（平铺 → 层级+链接),**Then** 同预算正确率不降，且 token 记账显示节流或等价。
2. **Given** 层级导航,**When** 关闭结构投影,**Then** 逐字节回到平铺多步行为（零行为变化，宪法 I/V）。
3. **Given** 层级导航无增益或更差,**When** 报告,**Then** 记录并维持平铺多步路径，不强行上线。

---

### User Story 4 - RL 导航策略训练（SaaS 线，Priority: P2）

若零训练多步已转化，参照 NapMem 训练导航策略（记忆多粒度金字塔 + GRPO，reward = 正确作答 + 工具使用），让模型学会「何时查、查哪层、何时停」。SaaS 线允许训练算力，分数单独口径登记，不回填本地。

**Why this priority**: NapMem 消融显示 RL 是跨 benchmark 平均的主要贡献（62.74 vs 48.39），但 LoCoMo 类别增益需本地口径实测；且训练成本高（Qwen3.5-9B 级 GRPO），必须门禁驱动、SaaS 单独口径。

**Independent Test**: 训练前后同一配对（或跨 benchmark 泛化），报告端到端转化 + 工具调用行为（步数、命中率）；分数写入单独口径（SaaS/训练标记）。

**Acceptance Scenarios**:

1. **Given** 训练完成,**When** 跑配对,**Then** 端到端相对零训练多步基线有可测增益且归因干净（RL 训练为单一机制）；增益主要来自导航策略而非单纯增大预算。
2. **Given** 训练模型,**When** 非记忆任务（推理/工具调用）评估,**Then** 不显著退化（NapMem 非记忆任务验证），且不需要时不再无谓调用记忆工具。
3. **Given** SaaS 分数,**When** 登记,**Then** 单独口径标注（不回填本地涨点，死亡规则不变）。

---

### Edge Cases

- **导航死循环 / 超步数**：模型反复检索不收敛 → 步数硬上限 + fail-closed 退回单次检索，记录导航失败率。
- **中间证据误导**：某一步检索结果把导航带偏 → 需要"评估-放弃"能力；US2 记录走偏分支的占比与最终影响。
- **换查询后漂移**：改写查询引入无关结果 → 与原始查询的 RRF 融合或实体锚定，防止漂移丢原始召回。
- **空检索结果**：某粒度/查询无命中 → 优雅跳过继续，或触发更宽查询，不崩整条导航。
- **结构投影缺失（US3）**：层级构建失败/被删 → 退回平铺多步（027 模式的可重建投影，不污染原文）。
- **预算越界**：导航消耗 + 最终证据超预算 → 单独记账；只有预算内转化才算 GO（防大力出奇迹）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 多步导航 MUST 以端到端配对为唯一 GO 门（008 铁律：多步 majority ≥ 单次基线）；中间信号（步数、命中率、召回覆盖）不得单独作为 GO 依据。
- **FR-002**: 多步导航 MUST 有明确的预算纪律：步数上限可配、token 记账（导航消耗与最终 answer-context 分开）、最终证据 MUST 在既有预算内；超预算才转化 MUST 单独口径登记并说明（防大力出奇迹）。
- **FR-003**: 本 feature MUST NOT 修改引擎（`memory/ embedding/ provider/ store/ internal/`）；导航编排 MUST 在 harness/评测层实现（复用引擎 public API）。若确需引擎公开入口增量（如多轮检索的证据状态传递），STOP 并显式提出合同增量，绝不绕过引擎。
- **FR-004**: 优雅降级（宪法 I/V）：无强模型 / 导航失败 / 超步数时 MUST 退回单次检索结果作答；关闭导航 MUST 与现状逐字节一致（零行为变化）。
- **FR-005**: 结构化层级导航（US3）MUST 默认关闭、可重建投影（config-hash 幂等，027 模式）；层级是原文之上的导航索引，MUST NOT 替换/删改原文（区别于已证伪的 event 替换）。
- **FR-006**: SaaS 线（US4 RL 训练）分数 MUST 单独口径登记，标注 SaaS/训练标记，不回填为本地涨点（028 模式，死亡规则不变）。
- **FR-007**: 每一阶段 MUST 记录可审计判定统计：导航步数分布、中间证据命中率、导航失败率、token 记账、类别明细 + McNemar，供归因与复现。

### Key Entities *(include if feature involves data)*

- **导航轨迹（Navigation Trajectory）**：一次查询的多步动作序列（搜索/评估/跟链/停止），含每步的查询、返回证据、决策理由；US2 的可审计产物。
- **导航工具集（Navigation Tools）**：暴露给导航 agent 的记忆操作（搜索/精确读取/跟链接/浏览粒度），带返回的结构化上下文；MemCog 5 工具 / NapMem 金字塔粒度的本地化对应。
- **层级记忆投影（Hierarchical Projection，US3）**：原文之上可重建的「粒度 → 页面 → 区块 → 类型化链接」导航索引，证据最终回到原文。
- **导航策略（Navigation Policy，US4）**：训练出的「何时查 / 查哪层 / 何时停」决策模型（NapMem 式 GRPO），SaaS 资产。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: US1 诊断报告：84 题三分类计数（gold 在池 / 单次 top-k 捞到 / 模拟多步可救），"多步可救"占比 ≥20% 才继续；报告归因到换查询/换粒度/线索跟链三类。
- **SC-002**: US2 配对：多步导航 majority ≥ 单次基线（008 铁律，GO 门）；token 记账显示最终证据在既有 answer-context 预算内；temporal/multi-hop 类别明细 + McNemar 齐全。
- **SC-003**: 类别不回归门（L0-3）：整体若涨，任一类别不得显著崩（temporal/multi-hop 不重蹈 013/014 覆辙）。
- **SC-004**: 降级与回归：关闭导航时与现状逐字节一致（宪法 IV 回归门对照）；导航失败路径完整可跑。
- **SC-005**: US3 结构导航（若做）同预算正确率不降且 token 节流或等价；US4（SaaS）分数单独口径登记，非记忆任务不显著退化。

## Assumptions

- **导航 agent 复用现有本地 35B answerer**（Qwen3.6-35B-A3B-FP8 @ 8000），零训练先导、近零成本；这是 US2 的默认，不强依赖托管 API。
- **多步导航的目标是「同预算内推理救回证据」，不是「加预算扩大召回」**——最终 answer-context 预算不变量保持（008 纪律），导航消耗单独记账。
- **评测沿用 027/028 配对纪律**：84 题先导（temporal 59 + multi-hop 25）→ 门禁后全量（LoCoMo 1540 + LongMemEval-S 500）；同 store/answerer/judge、3 reps majority、McNemar。
- **本 feature 是研究实验（默认关），产出 verdict**：只有配对转化才讨论进入默认路径；不转化的负结果如实记录（008 铁律，负结果可接受）。
- **SaaS 线（US4）允许训练算力**（AutoDL，遵循"空闲必停"与盘位纪律）；分数单独口径声明，不回填本地。
- **范围边界**：不做写侧 event/图替换原文（027/028/014 已证伪）；不做单次检索的表示/排序改进（021 已穷尽）；不做 agent 融合写回（022 证伪）。本 feature 唯一改动点是**检索侧的推理多步化**（不改引擎、不改存储表示）。

## 实际收口（2026-08-06）

**US1 诊断 GO，US2 配对 NO-GO，feature 终止。**

- **US1（零成本诊断）**：`rescueable_share = 0.655`（55/84）≥ 20% → GO。归因：rewrite 换查询 42 / deep 换粒度 12 / follow_entity 1；`not_in_pool=0` 确认召回缺口纯在排序/查询质量。verdict: `diagnosis/us1-verdict.md`。
- **US2（配对，008 铁律）**：nav **29.8% vs 基线 47.6%**，**−17.9pp，McNemar p=0.0059（显著负）**。temporal −20pp / multi-hop −12pp 均崩。4 次机制归因重跑（enable_thinking / 证据补足 / chunk-first）全部显著负（25%→32.9%→34.5%→29.8%）。verdict: `diagnosis/us2-verdict.md`。
- **根因**：① 该 store 的裸混合检索以短 fact 为主，chunk 需 `chunk-quota` 机制强制保底——导航组装无 quota，answerer 上下文永远劣化（~500 vs 3654 tokens）；② 模型自主导航不转化 US1 理论空间（73% 不 stop、改写查询命中 gold 差于确定性模拟）。
- **US3（结构化导航）/ US4（RL 导航）按门禁不执行**。
- **引擎零改动**：`git diff --name-only -- memory embedding provider store internal` 为空。导航代码保留为评测 harness 基础设施（`--nav` 默认关，SC-004 零行为变化）。
