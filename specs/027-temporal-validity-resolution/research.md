# Research: 查询时时间有效性解析

**Branch**: `027-temporal-validity-resolution` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

## 决策摘要

### D1: 027 的机制边界 —— 查侧确定性时间有效性解析（与已证伪机制划清）

**Decision**: 027 在**固定候选池内、查询时、确定性**组织已命中证据的时间结构：当前值解析
（选最新 valid）、演化链组装（按 OccurredAt 全序）、时间窗约束（按 query 显式时间过滤）。
不改变候选选择、不改变检索打分、不改变写侧。

**Rationale**: 代码盘点确认 013/014/017/024 遗留的 temporal 机制全部是**别的面**，且被
formal B1 明确拒绝（`eval_runner.go` `validateFormalLegacyMechanismOptions`）：

| 遗留 flag | 机制面 | 归属 | 状态 |
|---|---|---|---|
| `temporalScore` | 检索侧 soft temporal 打分重排 | 013/017 | 已证伪，formal B1 拒绝 |
| `temporalHardFilter` | 检索侧 hard temporal 候选过滤 | 013/017 | 已证伪，formal B1 拒绝 |
| `temporalAnswerPrompt` / `temporalDateScaffold` | answer prompt 侧脚手架 | 014 | 已证伪（prompt 侧非机制性） |
| `conflictResolution` + `supersededPenalty` | **写侧**非破坏性 supersede + 检索时 downweight | 024 | 已证伪，formal B1 拒绝 |
| `temporalDiagnostic` | retrieval-only 四层时间召回诊断 | 013 US1 | 诊断性，非涨点杠杆 |

027 的机制面 = **"证据已命中但未按时间组织"**：候选池同时含 superseded 旧值与当前值时，
查询时用确定性规则把旧值/当前值组织成演化链或选当前值。这是 013/014/017（检索侧）、024
（写侧惩罚）都未覆盖的**查侧组装面**。

**Alternatives considered**:
- 复用 `conflictResolution` 的 superseded 标注 + downweight → 拒绝：024 已证伪，且是写侧
  标注 + 检索打分层，不是查询时组织。
- 检索侧时间窗召回（021 IRIS 式）→ 拒绝：六次 NO-GO，不重复。

### D2: 时间信息来源 —— 复用已存在的 OccurredAt，不新增写侧 schema

**Decision**: 时间解析的输入 = `memory/evidencecompiler.Source.OccurredAt`（harness 侧
`compileFormalSources` 构建 source 时已可访问）+ 只读消费 022 的 append-only 生命周期
（`memory_evidence_events` / `memory_evidence_heads`）。V1 不新增写侧 schema。

**Rationale**: 代码盘点确认时间基建已充分：
- `memory_evidence.occurred_at`：证据发生时刻（已建索引）。
- `memory_evidence_events`：append-only 生命周期事件（append/tombstone/restore/purge +
  reason_code）——事实演化的审计链已存在。
- `memory_evidence_heads`：每 evidence 当前状态（active/tombstoned/purged）+ revision。
- `memory_entries.event_date`（v2）：记忆条目的事实发生时刻。

migration 历史里 `event_start`/`event_end`/`superseded_by`/`revision` 曾被 ADD 又 DROP
（014 temporal contract 试过写侧时间 schema 后放弃）；027 不重复写侧 schema 路线。

**Alternatives considered**:
- 新增 `superseded_by` 关系列 → 拒绝：写侧改动违反引擎零改动 + 014 已试过又放弃。
- 从 `memory_evidence_events.tombstone + reason_code` 推断 supersede → 可选增强，V1 不依赖
  （tombstone 的 reason_code 不保证指向替代者）。

### D3: supersede 判定 —— V1 确定性主题判定，不引入 LLM

**Decision**: V1 用确定性规则识别"同一事实主题的多个版本"：在候选内按实体/属性提及聚类，
同一主题的多条 source 按 `OccurredAt` 全序；查询时按语义（当前值/演化/时间窗）选取。冲突时
以时间序判定 superseded（最新 = 当前值）。不调用 LLM 判定主题一致性。

**Rationale**: 宪法 I/V（离线、确定性）+ spec Assumptions。APEX-MEM 的 GraphSQL 用结构化
查询做时间推理，但依赖 property graph + SQL agent；027 的 V1 用纯规则在候选内实现，规避
图存储与多轮 agent 的工程与 token 成本。确定性主题判定在配对中暴露的误判记录为已知边界，
不阻塞 V1（spec Edge Case 已声明）。

**Alternatives considered**:
- LLM 判定主题/演化 → 拒绝：违反宪法 V 的确定性原则 + 与 026（query-time 剪枝负结果）
  教训冲突——引入 LLM 判定的收益未证。
- 引入图存储 + SQL graph 遍历（复刻 APEX-MEM GraphSQL）→ 记录为独立研究对照，不进 V1
  （spec Out of Scope 已声明）。

### D4: 接入方式 —— 新 mechanism flag 进 formal B1（复刻 026 compiler 接线）

**Decision**: 新增 `--temporal-resolution` 作为 b1 阶段加性机制，进 `densityMechanismFlagsForOptions`
→ `mechanism_flags{temporal_resolution:true}`，与 026 的 `{compiler:true}`、025 的
`{episode_cluster:true}` 同构。候选仍走 `compileFormalSources` 同一 flat source list（候选逐字节
一致），解析器在 bundle 组装前对 source 做时间组织。

**Rationale**: 026 已验证该接线模式（`eval_runner.go` `formalTreatmentFreeze` +
`mergeMechanismFlags` + protocol hash 归因），027 复用同一协议路径，保证配对可归因
（protocol hash 稳定、只差机制 flag）。`validateFormalLegacyMechanismOptions` 拒绝的是 legacy
temporal flag；027 的新 flag 是 additive mechanism，走 `densityMechanismFlagsForOptions`
分支，不触发拒绝。

**Alternatives considered**:
- 独立 treatment stage（`formalTreatmentFreeze{Stage: "temporal_resolution"}`）→ 可选；
  additive mechanism 更贴近 026 的已验证形态，V1 采用 additive。

### D5: 评测协议 —— 全量 paired，同 store 候选逐字节一致

**Decision**: 在 022 冻结协议（LoCoMo 1,540 answerable + LongMemEval-S 500，cap 沿用 022
当前值）下，chunk_900 对照 vs `--temporal-resolution` 臂，同 store、候选逐字节一致，repeats≥3，
分类别配对统计（temporal/knowledge-update/演化类 multi-hop），candidate-oracle 归因
（superseded/current 是否在池）。负收益 → FR-010/FR-011 默认关。

**Rationale**: 宪法 IV + 026/025 配对纪律。APEX-MEM 的 +14~25pp 是跨栈证据（GPT5 answerer），
engram 固定栈增益必须独立配对验证（spec Assumptions 已声明不外推）。

## 文献证据

### APEX-MEM（2604.14362，ACL 2026，Amazon AGI）—— 027 的方向锚点

- **分数**：LoCoMo 88.88%（GPT5 QnA）/ 86.35%（GPT4o），LongMemEval 86.2%（Claude 4.5
  Sonnet）。judge = LLM-as-a-Judge（Mem0 协议，与 engram 同 LLM-judge 口径，但 answer model
  不同——GPT5/GPT4o vs engram 35B-A3B，需同栈复现归因）。
- **核心机制**：property graph（domain-agnostic ontology，entity + event 一视同仁）+ **append-only
  event storage**（保留完整演化，不做 consolidation/overwrite）+ **retrieval-time resolution**
  （查询时按 validity interval 解析冲突/演化）。
- **关键机制证据（Appendix F）**：append-only vs eager update 在 temporal 上是
  **+14~25pp**（APEX-MEM 90.63 vs Mem0 75.71 / MIRIX 65.62 / Zep 76.60）。GraphSQL 结构化
  时间查询单加一项 +9.37pp（temporal 72.92→82.29）。
- **消融**：SchemaViewer+EntityLookup 77.19 → +GraphSQL 79.45（multi-hop/temporal 增益大）→
  +Search 87.00（open-domain 增益大）。工具组合必要。
- **对 027 的含义**：写侧 append-only 已由 022 Ledger 满足；027 复刻其**查侧 resolution**（按
  validity 解析演化），用纯规则替代 GraphSQL/Search 的多工具 agent。

### 其它相关证据（不直接依赖，作为边界）

- **LazyMem（2607.22690）**：recall 高（LongMemEval recall@50=0.99）时 query-time 构造可超
  oracle；recall 低（LoCoMo 0.89）救不了缺失证据。→ 027 必须用 candidate-oracle 区分 resolution
  miss（版本在池未解析，027 可救）与 candidate miss（版本不在池，027 救不了）。
- **Fidelity-Before-Structure（2601.00821）**：verbatim > extracted（write-time loss 69%）——
  027 不改变候选表示（保留 chunk_900 原文），只做时间组织，不违背该证据。
- **026 负结果（2026-08-02 配对）**：query-time verbatim 剪枝编译是负结果（compiler −4.5pp
  extractive / −3.6pp exact-token），机制是 need 剪枝压 token 丢逐证据覆盖。→ 027 的时间组织
  **不做剪枝**（保留证据），只做排序/过滤/当前值选择，避免重蹈 026 覆辙。

## 代码资产盘点（2026-08-06 核实）

| 资产 | 路径 | 对 027 的含义 |
|---|---|---|
| Source.OccurredAt | `memory/evidencecompiler/compiler.go` | 时间解析的时间戳来源（只读） |
| `selectedHasTimeEvidence` | `memory/evidencecompiler/orchestrate.go:318` | 已有"是否有时间证据"判定，027 可参考其 OccurredAt 访问模式 |
| `GapTimeRange` / `TimeConstraints` | `memory/evidencecompiler/orchestrate.go:304` | Evidence Need 已支持显式时间约束 |
| `densityMechanismFlagsForOptions` + `mergeMechanismFlags` | `cmd/locomo-bench/eval_runner.go:359/378` | 027 mechanism flag 接入点 |
| `formalTreatmentFreeze` | `cmd/locomo-bench/eval_runner.go:399` | additive mechanism 冻结契约 |
| `compileFormalSources` | `cmd/locomo-bench/eval_compile_bridge.go:244` | 候选→source 构建，027 解析器的前置 |
| `memory_evidence_events` / `heads` | `store/migrations.go:244/254` | append-only 生命周期（只读对照，V1 可选） |
| `validateFormalLegacyMechanismOptions` | `cmd/locomo-bench/eval_runner.go:325` | 确认 legacy temporal flag 被拒（027 不碰它们） |

**引擎零改动边界**：027 增量限定在 `cmd/locomo-bench/`（harness 侧 adapter）。`memory/
evidencecompiler` 只读消费（Source.OccurredAt、EvidenceNeed.TimeConstraints），不改契约。
若解析需要引擎未提供的公开入口（如按 source 批量拉取 occurred_at 到 harness），作为显式
contract increment 提出，不绕过（宪法 II）。

## 实现核实（T001/T002，2026-08-06 代码确证）

- **T001 确证**：`memory.Evidence.OccurredAt *time.Time`（`memory/evidence.go:82`）经
  `formalExpandedSource.Evidence`（`cmd/locomo-bench/eval_source_bundle.go:20`）在 harness 层
  **直接可读**——解析器无需引擎增量即可消费时间戳。接入点 = `compileFormalSources`
  （`eval_compile_bridge.go:244`）内 `formalCompileSourceList`（flat source list）之后、
  `buildCompileCandidates` 之前：对 `[]formalExpandedSource` 做时间组织，再构建候选传给
  `evidencecompiler.Compile`。引擎侧 `Source.OccurredAt` 由 `LedgerResolver` 从
  `record.OccurredAt` 克隆（`internal/resolve/resolve.go:108`）。
- **T002 确证**：`validateFormalLegacyMechanismOptions`（`eval_runner.go:325`）明确拒绝
  `temporalScore`/`temporalHardFilter`/`conflictResolution`/`supersededPenalty` 等 legacy
  temporal flag 进 formal B1；027 的 additive 分支 =
  `densityMechanismFlagsForOptions`（`eval_runner.go:359`）→ `formalTreatmentFreeze` +
  `mergeMechanismFlags` → `mechanism_flags{temporal_resolution:true}`，契约形态对齐
  [024 mechanism-bindings](../024-memory-density/contracts/mechanism-bindings.md)。
- **T003 baseline（2026-08-06）**：`CGO_ENABLED=0 go build ./...` OK；
  `./memory/evidencecompiler/...` + `./cmd/locomo-bench` 测试全绿（10.4s）。健康状态可追溯。
