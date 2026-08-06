# Feature Specification: 读侧证据关联装配（Evidence Relation Assembly）

**Feature Branch**: `031-evidence-relation-assembly`

**Created**: 2026-08-06

**Status**: Draft

**Input**: User description: "落地 MemCog（2605.28046）读侧结构上下文证据：检索后按类别组织证据——multi-hop 题把共享实体/因果链接的证据聚成链、temporal 题按 event_date 排序——作为结构上下文喂给 answerer，不引入 agent 决策、不写侧重构。纯 Go、离线、确定性。"

## Decision and Relationship to Feature 030

030（读侧证据装配 / evidence-mediation）已收口：chunk-first 装配 + trace 引用链中介在全量
1540 题以 **85.91%（3 次多数）@ 468 tok** 站住，是「预算下提质」的落地。030 的装配器负责
「证据先按结构装配」（chunk 保底、cap 截断、类别条件排序）与「引用链精选」，但**不携带证据间
的显式关系**——answerer 拿到的是排序后的证据列表，不知道证据 A 与证据 B 通过什么关联、谁是谁
的时间后继、谁导致谁。

本 feature 承接 030 的装配地基，只增加一个读侧增量：在装配好的证据上**附加证据间显式关系
（structural context）**。实证锚点 MemCog（`2605.28046`，腾讯 2026-05）的固定变量消融：
**w/o Graph Overlay（页面级关联链接）↓6.79pp、w/o Hierarchy（维度层级）↓6.53pp**，且
`structured-content-directions.md` §三 C4（类别条件装配）「multi-hop 按链 / temporal 按时序」的
契约已定型、未测——本 feature 把该契约用 MemCog 的建链规则（entity co-occurrence + temporal
proximity + causal indicators）实例化，全部纯 Go、离线、确定性。

| 能力面 | 030 所有权 | 031 所有权 | 并行结论 |
|---|---|---|---|
| 装配地基（chunk-quota 固化、cap 截断、类别条件排序） | 定义、实现并收口 | 只消费装配输出，不修改装配器排序 | 不冲突 |
| trace 引用链精选 + fail-closed 门 | 定义、实现并收口 | 不复制、不修改；031 的结构上下文可与 trace 叠加或独立关闭 | 不冲突 |
| 类别条件排序（temporal→时间序 / multi-hop→实体） | 030 US1 已实现排序 | **新增**：显式关系边标注 + 结构上下文块注入（不只是排序） | **031 独占** |
| 检索、store、embedding、answerer、judge、cap 预算 | 定义并冻结 | 全部复用冻结栈，不改调用次数 | 不冲突 |
| 正式涨点报告 | 发布 030 verdict（85.91% @ 468 tok） | 独立配对报告 + verdict，008 铁律 | 串行依赖 |

**必须避免的已证伪路径**（本 feature 的设计红线）：

1. **不做 agent 导航**（029 NO-GO，−17.9pp）：本 feature 不提供 list/browse/read/follow_link 工具、
   不让模型决定下一步搜什么。结构上下文是**静态注入给 answerer 的装配产物**，不是 agent 的决策空间。
2. **不写侧重构**（027/028/014 三次证伪）：不把内容重写成 event/graph/页面层级。关系是**读侧在候选
   证据上临时计算**的，不改 store 里的内容表示。
3. **不做实体图遍历**（014 e2e NO-GO；Mem0g multi-hop 47.19 < base 51.15）：不是跟随实体边跳转
   检索，而是把证据间关系作为上下文提示。
4. **不用付费云 rerank/recall 涨点**（DEATH RULE）：关系计算用 store 已有的实体/日期/词典，零外部调用。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 确定性证据关系计算（P1）

检索命中候选证据后（030 装配地基已就位），系统在候选证据之间计算并标注显式关系：共享实体关联
（related_to）、时间邻近/后继（temporal_next）、因果指示（caused_by）。这是 MemCog「关联链接」
规则在 engram store 数据上的纯 Go 实例化——不引入新抽取、不依赖模型、不改 store。

**Why this priority**: MemCog 消融证明「证据间显式关联」在完整系统内值 ↓6.79pp，是本 feature 的
唯一核心增量；先把它做成确定性的离线能力，才有后续一切。它也是本 feature 里唯一需要实现的新算法。

**Independent Test**: 完全离线（零模型调用）——对固定候选证据集断言：(1) 关系计算确定性（同输入
同输出）；(2) 三类关系（共享实体 / 时间邻近 / 因果指示词）按 store 已有字段正确标注；(3) 关系标注
不改变证据内容与排序（纯附加元数据）。

**Acceptance Scenarios**:

1. **Given** 一个含多条 chunk/fact 的候选证据集（store 含 memory_entities 与 event_date），**When**
   关系计算运行，**Then** 输出证据间关系边集合：共享实体的 related_to、日期邻近/后继的 temporal_next、
   含因果指示词的 caused_by，全部离线确定性生成。
2. **Given** 同一候选证据集，**When** 关系计算运行两次，**Then** 两次输出逐字节一致（确定性）。
3. **Given** 证据间不存在任何共享实体/日期邻近/因果指示词，**When** 关系计算运行，**Then** 输出空关系
   集（不臆造关系，fail-soft 降级为空上下文）。
4. **Given** 关系计算未启用（默认），**When** 检索完成，**Then** 现有装配路径逐字节不变（parity 门）。

---

### User Story 2 - 结构上下文装配与类别条件注入（P1）

把 US1 算出的关系边组织成一段**结构上下文块**，按问题类别注入 answerer 上下文：multi-hop 题把
共享实体/因果链的证据按链序组织，temporal 题按时间后继链（temporal_next）组织。这是
`structured-content-directions.md` C4「类别条件装配」的实例化——但比 030 的排序更进一步：不只排序，
还显式告诉 answerer「这些证据通过什么关系相连」。结构上下文可独立于 trace 开关，也可叠加。

**Why this priority**: 021 IRIS 教训「temporal≠graph，须 category-conditional」已在 030 排序层落地，
本 US 把它升级为「排序 + 显式关系提示」，是 031 的装配产物；MemCog 的 Graph Overlay 消融正对这条。

**Independent Test**: 完全离线——对固定候选证据集断言：(1) multi-hop 类别问题注入的上下文含共享
实体/因果链组织；(2) temporal 类别问题注入的上下文按时间后继序；(3) 未启用时逐字节不变。

**Acceptance Scenarios**:

1. **Given** 一个 multi-hop 类别问题及其候选证据，**When** 结构上下文装配，**Then** 注入的上下文把
   共享实体/因果链的证据组织为链式段落，每条关系注明类型与依据。
2. **Given** 一个 temporal 类别问题及其候选证据，**When** 结构上下文装配，**Then** 注入的上下文按
   时间后继顺序组织证据，注明 temporal_next 关系。
3. **Given** 证据关系为空（US1 场景 3），**When** 结构上下文装配，**Then** 不注入结构上下文块，走
   030 现有装配路径（fail-soft）。
4. **Given** 结构上下文未启用（默认），**When** 检索完成，**Then** 现有装配路径逐字节不变（SC-004 parity）。

---

### User Story 3 - 配对评测与 GO 门（P2）

在 030 已收口的配对纪律下验证 031 增量：同一 store、同一子集、同一 answerer/judge/cap 预算下，
结构上下文 arm 相对 030 基线做配对评测；majority ≥ 基线且类别不回归为 GO（008 铁律）。先 84 题
子集 × 3 reps，GO 后全量 1540 复跑。

**Why this priority**: 008 铁律与「028/029/030 全部以配对 majority 收口」的纪律要求端到端配对为唯一
GO 门；US1/US2 只证明实现正确，不证明涨点。

**Independent Test**: 配对评测——84 题子集 × 3 reps majority，结构上下文 arm ≥ 030 基线（不回归），
且 multi-hop/temporal 类别不回归（L0-3 category non-regression）。

**Acceptance Scenarios**:

1. **Given** 030 已收口的 84 题子集与基线记录，**When** 结构上下文 arm 跑 3 次重复，**Then** majority
   ≥ 基线（008 铁律 GO 门）。
2. **Given** 子集 GO，**When** 全量 1540 题复跑，**Then** 正确率 majority 相对 030 基线不回归，且类别
   全正向无回落（L0-3）。
3. **Given** 结构上下文 arm 与 trace arm 都启用，**When** 配对，**Then** 报告两者叠加效应（结构上下文
   在 trace 之上是否还有增量），不把叠加与独立混报。
4. **Given** 配对发现某类别显著回退，**When** 裁决，**Then** 判定 NO-GO 并记录机制归因（不硬推）。

---

### Edge Cases

- **证据间关系为空**（无共享实体/日期邻近/因果词）：不注入结构上下文，fail-soft 走 030 路径（绝不让
  answerer 上下文劣化）。
- **关系过密**（证据高度重叠导致关系边爆炸）：对关系边做 cap（如单证据最多 K 条边），防止上下文被
  关系提示淹没（token 预算纪律）。
- **同一证据既有 temporal_next 又 caused_by**：按类别优先级取舍（temporal 题取时序、multi-hop 题取
  因果/关联），不重复注入。
- **日期缺失**（fact 无 event_date）：temporal 链退化为仅按文本内年份/月份正则排序，缺失者置尾（014
  时间解析覆盖不足的教训）。
- **trace 叠加时引用门冲突**：结构上下文引用的证据必须在 trace 的 closed candidate boundary 内，否则
  不注入（复用 030 fail-closed 门）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 在候选证据上计算三类关系边——共享实体（related_to）、时间邻近/后继
  （temporal_next）、因果指示词（caused_by）——全部离线、确定性，仅用 store 已有字段
  （memory_entities / event_date）与内置因果指示词词典，零外部模型调用。
- **FR-002**: 系统 MUST 在装配输出上附加结构上下文块（关系类型 + 依据），作为附加段落注入 answerer
  上下文，不修改证据内容与排序（纯附加元数据）。
- **FR-003**: 系统 MUST 按问题类别组织结构上下文——multi-hop 题按关联/因果链组织，temporal 题按时间
  后继链组织（021 契约 temporal≠graph）。
- **FR-004**: 特征未启用时（默认关），系统 MUST 保持现有装配路径逐字节不变（SC-004 parity，同 030）。
- **FR-005**: 关系边 MUST 有容量上限（单证据关系边数 cap），超限截断，token 预算纪律（不大力出奇迹）。
- **FR-006**: 证据关系为空或无法可靠计算时，系统 MUST fail-soft 降级为 030 现有装配路径，绝不空答或
  劣化上下文（宪法 V 优雅降级）。
- **FR-007**: 配对评测 MUST 遵守 008 纪律（同 store/子集/answerer/judge/cap 预算），majority ≥ 基线
  为 GO 门，且类别不回归（L0-3）；结构上下文 arm 与 trace arm 的叠加效应 MUST 单独报告，不混报。
- **FR-008**: 本 feature MUST 不修改 engine（`memory/ embedding/ provider/ store/ internal/`）任何
  `.go`——只动评测 harness（cmd/locomo-bench）；用 `git diff --name-only` 校验必须为空。

### Key Entities

- **证据单元（Evidence Unit）**: 候选证据集合中的单个 chunk 或 fact，是关系计算的节点；含内容、来源、
  实体（memory_entities）、日期（event_date，若存在）。
- **关系边（Relation Edge）**: 两证据单元之间的显式关系，类型为 related_to / temporal_next /
  caused_by，含依据（共享实体名 / 日期对 / 命中因果指示词）；有容量上限。
- **结构上下文块（Structural Context Block）**: 装配器产出的附加段落，按类别组织关系边，注入
  answerer 上下文；独立于 trace 开关、可叠加。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 关系计算完全离线、零模型调用、确定性——同输入两次运行逐字节一致（单测 + 离线门）。
- **SC-002**: 特征默认关闭；未启用时现有装配路径逐字节不变（parity 门，git diff 与字节级断言）。
- **SC-003**: 84 题子集 × 3 reps majority：结构上下文 arm ≥ 030 基线，且 multi-hop/temporal 类别不
  回归（008 铁律 + L0-3）。
- **SC-004**: 全量 1540 复跑（子集 GO 后）：正确率 majority 相对 030 基线不回归；若与 trace 叠加，单独
  报告增量，不把叠加当独立赢。
- **SC-005**: 引擎零改动——`git diff --name-only -- memory embedding provider store internal` 为空
  （FR-008 硬门）。

## Assumptions

- **复用 030 装配地基**：chunk-quota 固化、cap 截断、类别条件排序均已在 030 收口，本 feature 只在其
  输出上附加结构上下文，不重做装配器。
- **关系计算只用 store 已有数据**：memory_entities（实体）与 event_date/fact_source（日期）已存在于
  schema v2，不需要新抽取或新列。
- **因果指示词用内置确定性词典**（because / due to / led to / caused by 等），不引入模型判断；词典是
  可维护的纯 Go 资源。
- **配对复用 030 已用的子集与基线**：84 题 = 029 实际子集（phase0-ids-029-84.txt），基线 = 030 配对
  记录；全量 = 1540 题。
- **默认关闭、SC-004 零行为变化**：本 feature 不改变默认 MCP/CLI/检索路径，只作为评测 harness 的
  opt-in 装配增量（与 030 trace 同纪律）。
- **不引入 agent 决策 / 不写侧重构 / 不用付费云 rerank**：见「必须避免的已证伪路径」红线，四者全部
  是本 feature 的设计约束而非可选优化。
