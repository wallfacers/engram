# Phase 0 Research: 跨消息语义聚类 episode 表示

**Feature**: 025-semantic-episode-cluster

**Date**: 2026-08-01

**Status**: Complete — no unresolved items

本文把 [spec.md](./spec.md) 与仓库既有研究记录(022/024 已合并)转化为可实施决策。
新的事实核查结论(承接资产实际状态)记录于 R0,它修正 spec 的 US2 前提。

## R0 — 承接资产已实现且单测通过,真实缺口是"接线"而非"补测试"

**Decision**: 025 的 US2 承接验证不再从"写缺失测试"开始——022 已交付的
`memory/episode.go` 与 `cmd/locomo-bench/representation_eval.go` 的单测已存在且通过。
真实缺口是**正式 eval 从未构建过 episode 投影**。

**Evidence**:

- `memory/episode_test.go` 13 tests:同 session 连续边界、deterministic narrative、删除不删
  Evidence、segmenter 降级、config-hash 隔离、rebuild 幂等、char_count/timestamps、
  tombstoned 排除等,已覆盖 022 spec 的 T046 范围。
- `cmd/locomo-bench/representation_eval_test.go` 14 tests:shared anchors、byte digest、
  token-cap rendering、source expansion、三渲染器、digest identity、shared budget 等,
  已覆盖 022 spec 的 T047 范围。
- `representation_integration_test.go` 6 tests:bake-off flow、episode delete preserves
  evidence、candidate budget equality、episode fault degradation、shadow index 隔离、
  digest stability。
- 实测 `CGO_ENABLED=0 go test -count=1 ./memory/ ./cmd/locomo-bench/` 全绿(plan 编写时)。
- **但 `RebuildSession` 全仓库零调用**(grep 仅定义处);`representation_integration_test.go`
  不调用 RebuildSession;`main.go` 创建 `EpisodeStore` 后从未构建 → semantic_episode 渲染臂
  永远走 fallback(直读 anchor source),**从未渲染过真实 episode 投影**。

**Rationale**: 承接 022 资产的风险不是"代码没测试"(已测),而是"从未在真实 pipeline 里
验证 episode 构建 + 渲染链路"。025 的 US2 焦点 = 用跨 session 能力测试钉住构建链路,再在
eval 接线。

**Alternatives considered**:

- 按 spec 原句"补写 T046/T047 失败测试":拒绝,测试已存在,重复补写浪费;真实缺口是接线。
- 跳过 US2 直接实现跨 session:拒绝,承接资产未经端到端验证就扩展,风险不可控(违反
  FR-007)。

## R1 — 聚类对象:原始 Evidence 级(跨 session 语义相关证据簇)

**Decision**: SemanticClusterer 对**原始 Evidence**(`memory_evidence`)聚类,输出跨
`source_session_id` 的证据簇;每簇的渲染文本是证据叙事(`speaker: content` 按 source_order
拼接),复用 022 `EpisodeStore.buildEpisodeTx`。anchor 是检索命中的 fact/chunk,命中后经
lineage 定位所属 episode projection,整体带入。

**Rationale**: 见 spec 聚类决策(证据级而非 fact 级)。fact 级=语义化 neighbor_extend(024
已证伪);原文叙事保留措辞,与 022 三渲染器同范式,可直接复用 `semanticEpisodeRenderer` +
`EpisodeStore`。

**Alternatives considered**:

- fact 投影级聚类:拒绝,压缩文本丢失原始措辞,近似 024 已证伪的 neighbor_extend。
- 消息级原文渲染 + fact 级聚类:拒绝,引入第二套候选单元,渲染与检索归因混乱。

## R2 — 聚类构建时机:全量可重建投影(不做查询时在线聚类)

**Decision**: 扩展 `EpisodeStore` 新增 `RebuildAll(ctx, clusterer, builderVersion,
configHash)`:遍历 namespace 内全部 active Evidence(跨 session),用 SemanticClusterer
聚类,复用 `buildEpisodeTx` 写 `semantic_episode` projection + lineage。config-hash 幂等
(同 config 删旧建新)。不做查询时在线聚类,不做写入时增量。

**Rationale**: ① 全量重建可配置可冻结,是 bake-off 需要的确定性;② 022 的 EpisodeStore
已经是"重建式投影"哲学(config-hash 幂等、可丢弃可重建),扩展为跨 session 一致;③ 在线聚类
每个查询都要算 embedding/重叠,不确定、难冻结、难归因。

**Alternatives considered**:

- 查询时在线聚类:拒绝,不确定、难冻结、难 bake-off;每次查询重复计算。
- 写入时增量聚类:拒绝,破坏可丢弃可重建的投影哲学,增量维护复杂且易漂移。

**Schema 影响**: `memory_semantic_episodes` 表无 session 约束(约束在 `RebuildSession`
代码里对同一 session 连续 ordinal 校验);`memory_projection_sources` 的 lineage 天然支持
跨 session(evidence_id 直连)。因此跨 session 聚类**不需要新 migration**,只需在
`RebuildAll` 中跳过 RebuildSession 的同 session 连续校验。

## R3 — 离线聚类信号:共享实体 + 共享关键词,任一达成即聚

**Decision** (spec 的 research.md Decision 1):

- 离线信号(确定性、默认路径、无 embedding/LLM 依赖):
  - **共享实体重叠**:两条 Evidence 共享 ≥1 个归一化实体(复用 engine 现有 entity 机制
    `memory_entities` 的 entity_norm 归一化;对 Evidence 内容做同样的归一化 token 提取),
    满足即聚。
  - **共享关键词/主题重叠**:去除停用词后,两条 Evidence 的显著 token 集合 Jaccard 相似度
    ≥ 阈值 `minKeywordJaccard`(默认 0.25,宽松于 024 write_dedup 的 0.7——聚类是分组动作
    不是抑制动作,过度保守会漏掉跨消息相关证据)。
  - 任一达成即可聚(OR),不是两者都要。
- 可选 embedding overlay(默认关,本地 sidecar):embedding cosine ≥ `embedThresh`(默认 0.9)
  可作为额外的聚信号;无 embedding 端点时自动跳过,纯离线路径完整。

**Rationale**: 024 教训是 trigram Jaccard 0.7 抑制面过窄(21,860 次仅 20 触发,0.09%),
因为"抑制"要求近似重复;聚类是"分组",应更宽松。实体重叠捕捉"同一事件/人物",关键词重叠
捕捉"同一话题",OR 保证跨消息相关证据能被聚拢。离线确定性信号保证宪法 V 的降级路径
(SC-005)。

**Alternatives considered**:

- 复用 024 trigram Jaccard 0.7:拒绝,抑制语义过窄,LoCoMo 上几乎不触发(0.09%)。
- 只用实体重叠:拒绝,消息级证据未必都提取了实体(提取是 projection 级),关键词重叠兜底。
- embedding 作主信号:拒绝,违反默认离线;embedding 只能作可选 overlay。

## R4 — 聚类有界:每 episode 证据数上限 + 跨 session 允许

**Decision** (spec 的 research.md Decision 2):

- 每 episode 证据数上限 `maxEvidencePerEpisode` 默认 **8**(对齐 024 SiblingFacts 默认
  maxSiblings=8 与 022 rawTurnWindow 的合理窗口量级)。可配。
- 超上限时的截断顺序**确定性**:按 evidence ordinal(会话内顺序)升序取前 N,保证重建稳定。
- 跨 session 聚类默认允许(FR-011);聚类域 = 同 namespace DB 内全部 active Evidence,
  不跨 namespace(宪法 III)。

**Rationale**: cap 3600 token 下,8 条证据的叙事量级与现有候选上下文一致;确定性截断保证
digest 可核对。LoCoMo 单条消息常被抽取成多个 fact,8 条 evidence 的簇在 multi-hop 上下文
内既不无界也不过窄。

**Alternatives considered**:

- 不设上限:拒绝,高频话题聚类规模爆炸,违反 FR-004。
- 按 token 数而非证据数设限:拒绝,需真实 tokenizer 才能定,回归到 eval 层才可判;证据数
  是构建期可确定的确定性单位。

## R5 — anchor 路由:fact → lineage → episode projection

**Decision**: 渲染复用 022 `semanticEpisodeRenderer`。anchor 是检索命中的 fact/chunk;
渲染前 eval 用 `SourcesByProjectionIDs` 解析 anchor 的 evidence lineage,再用
`EpisodesForEvidence`(按 evidence_id 反查 `semantic_episode` projection,新增辅助查询)定位
所属 episode;命中则渲染 episode narrative,未命中则退化为直读 anchor source(与现状一致)。

**Rationale**: anchor 是 fact(检索结果),episode 是跨消息簇;二者通过 lineage 关联。
022 的 renderer 假设 anchor.CandidateID 即 episode projection ID,这在 022 的 navigation
bake-off 下成立;025 的 rendering bake-off 下 anchor 是 fact,需按 evidence lineage 反查。
复用 renderer 的 fallback 语义保证未命中时零行为变化。

**Alternatives considered**:

- 让检索直接命中 episode projection:拒绝,改变检索契约与候选组成,混入检索差异。
- 新增独立 renderer:拒绝,022 已有 semanticEpisodeRenderer,扩展其路由即可。

## R6 — 判定统计审计

**Decision**: SemanticClusterer 每次重建输出判定统计:决策总数、聚类数、疑似误聚类数
(重叠但被有界截断 / 边缘阈值),对齐 024 suppression-audit 的 JSON 输出到 run 侧 artifact。
不持久化到 DB(避免新 migration;config_hash 已提供重建身份)。

**Rationale**: FR-006 要求可审计;024 的 suppression-audit(decisions/suppressed/
suspected_mis)是已验证的先例。

## R7 — 配对统计口径复用 022(不自创)

**Decision**: 配对验证的统计方法显式复用 022 的 `cmd/locomo-bench/paired_eval.go`:
exact McNemar(p<0.05)+ primary cohort majority accuracy Δ≥+2.0pp promotion rule + 另一
benchmark overall Δ≥−0.5pp 且无显著负向(022 research R14)。025 只在"目标 cohort =
multi-hop 类别"上聚焦报告,不更改统计方法本身。

**Rationale**: 025 的结果必须与 022/024 的配对口径直接可比;若自创统计或阈值,破坏
宪法 IV 的可比性与归因纪律。

**Alternatives considered**:

- 只用点估计对比:拒绝,无法区分噪声与机制收益。
- 用不同 McNemar 实现/阈值:拒绝,口径漂移,与 022/024 不可比。

## Unresolved Items

无。聚类阈值的具体数值(0.25 / 0.9 / 8)是默认值,可在配对实验前按 research.md 记录调整
(调整属 eval-config 变更,独立提交,归因不混入算法改动)。
