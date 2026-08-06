# Research: 读侧证据关联装配（Evidence Relation Assembly）

**Phase 0 输出** | 2026-08-06 | 决策记录，解决 plan.md 的 Technical Context 未知点。

## R-1: 实证锚点（MemCog 消融核实，alphaXiv）

来源：**MemCog: From Memory-as-Tool to Memory-as-Cognition**（`2605.28046`，arXiv 2026-05-27）。
固定变量消融（GLM-5.1 骨干，LoCoMo）：

| 配置 | LoCoMo Overall | Δ |
|---|---|---|
| MemCog (full) | 89.83 | — |
| w/o Proactive Protocol | 89.48 | **↓0.35**（只服务主动触发基准，被动 QA 几乎无影响） |
| w/o Graph Overlay（跨维度关联链接） | 83.04 | **↓6.79** |
| w/o Hierarchy（Dimension→Page→Section 层级） | 83.30 | **↓6.53** |

**Decision**: 采纳「证据间显式关联（Graph Overlay）」为 031 的核心增量；「层级（Hierarchy）」依赖写侧构建（dimension/page 分类），撞 027/028/014 写侧证伪线，不纳入。Proactive Protocol 超出 LoCoMo 被动 QA 评测，不纳入。

**Rationale**: Graph Overlay 是唯一「读侧可计算、不写侧重构」的消融赢——MemCog 的建链规则（entity co-occurrence + temporal proximity + causal indicators）可在 engram 候选证据上临时计算，不持久化结构。

**诚实边界**（写入 quickstart 与 verdict 纪律）：
- 消融 delta 是「MemCog 完整系统内移除组件」的差异，不是「engram 栈上叠加」的差异；MemCog 走 GLM-5.1 + agent 导航，engram 走 Qwen3.6 + 静态装配。
- 008 铁律：以 engram 配对 majority 为准，预期是「试一把」而非「必然涨点」。029 的模拟高估教训在前（US1 理论空间 0.655 → US2 实际 −17.9pp）。

## R-2: 实体提取方法

**Decision**: harness 内确定性正则提取（复用 029 `nav_diagnose_cli.go:extractEntitiesFromHits` 模式：title-case 多词 + quoted 短语，去重、cap 每证据 5 个）。

**Rationale**: 
- 保持离线（零模型、零 store 依赖）；对 chunk 与 fact 统一处理（store `memory_entities` 只索引 fact，chunk 没有实体索引）。
- 避免触碰 engine API 面——FR-008 引擎零改动是硬门，harness 内自算最稳。
- 029 已证明该提取在 `memory.Result.Content` 上可复用（`quotedRe` / `titleRe`）。

**Alternatives considered**: 
- store `memory_entities` 表（schema v2 已有，`EntityCues`/`EntityDocFreqFor` 等 engine API）：被否——需打开 store + 依赖 engine API + 不覆盖 chunk。
- LLM 抽取实体：被否——付费/延迟 + 非确定性，违背 I/V。

## R-3: 因果指示词词典

**Decision**: 内置确定性小词典（纯 Go `map[string]bool`，~12 词）：
`because`, `due to`, `led to`, `caused by`, `resulted in`, `therefore`, `as a result`, `consequently`, `since`, `thus`, `triggered`, `in response to`。匹配规则：证据文本（小写化）含任一指示词，且两证据共享该句子上下文中的同一核心实体（共享实体才成边，防止整段文本误连）。

**Rationale**: MemCog 用 "causal indicators" 建 caused_by 链；词典是最简确定性实现，可维护、可审计。要求「共享实体 + 指示词」双条件，避免虚假因果边（对应 FR-001 的可靠计算）。

**Alternatives considered**: LLM 因果判断——被否（非确定性 + 付费）；纯共现——过度连接，不采纳。

## R-4: 关系边容量上限

**Decision**: 单证据关系边 cap K=4：`related_to` ≤ 4 条（按共享实体共享度降序）、`temporal_next` ≤ 1 条（最近时间后继）、`caused_by` ≤ 2 条（原因 1 + 结果 1）。整块结构上下文 token 计入 030 exact-token 记账（FR-005 + token 预算纪律）。

**Rationale**: 防止证据高度重叠时的关系爆炸淹没 answerer 上下文；关系是附加元数据，随被截断证据一起消失（不改变 030 的截断语义）。

## R-5: trace 叠加顺序与引用门

**Decision**: 结构上下文可独立启用，也可与 `--trace-mediation` 叠加。叠加时，结构上下文块引用的证据必须在 trace 的 closed candidate boundary 内——复用 `trace_gate.go` 的 `idsInside`/`overlapsAny` 校验，越界证据不注入（fail-closed，FR-008 复用 030 门）。

**Rationale**: 030 trace 的 fail-closed 门是已收口的确定性组件，复用保证叠加不破坏引用纪律；结构上下文是 trace 精选证据上的附加关系提示，两者正交（spec US3 acceptance 3 要求叠加效应单独报告）。

## R-6: 类别条件优先级

**Decision**: multi-hop 题取 `related_to` + `caused_by` 链（按链序组织）；temporal 题取 `temporal_next` 链（按时间后继序）；同一证据同时满足两者时按类别取舍，不重复注入。generic/single-hop 不注入关系上下文（无链可循时 fail-soft，031 只在 multi-hop/temporal 有效）。

**Rationale**: 021 IRIS 教训 `temporal≠graph`——类别条件装配必须区分；030 `orderHitsForAssembly` 已按此排序，031 在排序之上附加显式关系标注，不改变排序本身。

## 决策汇总

| # | 决策 | 备选（被否） |
|---|---|---|
| R-1 | 采纳 Graph Overlay 关联链接；弃 Hierarchy/Proactive | Hierarchy=写侧证伪线；Proactive=超评测范围 |
| R-2 | harness 内正则实体提取 | store memory_entities（依赖 engine API+不覆盖 chunk）；LLM 抽取（付费+非确定） |
| R-3 | 内置因果词典 + 共享实体双条件 | LLM 判断；纯共现 |
| R-4 | 关系边 cap K=4 + token 计入 cap | 无边限 |
| R-5 | 复用 trace fail-closed 门做叠加校验 | 独立重复实现 |
| R-6 | 类别条件取舍（temporal 时序 / multi-hop 链） | 全类别统一注入 |
