# Research: 同预算记忆密度杠杆

**Date**: 2026-07-31 | **Feature**: [spec.md](spec.md) | **Branch**: `024-memory-density`

## Scope

解决 spec 的未知项：冗余判定用什么信号、邻居扩展用什么 relation、机制如何绑定到 022.v1 协议、误伤如何测量、MemOS 参数如何落为 engram 参数。所有决策以"默认关、纯本地、可离线、不破坏 append-only、可归因"为约束。

## Decision 1 — 冗余判定信号：离线 Jaccard 为主，embedding 语义可选叠加

- **Decision**: 主路径复用 `memory/curation` 的字符 trigram Jaccard（`DefaultJaccardThreshold = 0.7`，可配），用 FTS5 pre-filter 限定 candidate pairs（O(pairs)）。embedding 语义相似度作为**可选叠加层**（默认关，需本地 sidecar），与离线结果做 OR/加权。
- **Rationale**: 现有 `curation/dedup.go` 已实现纯 Go、离线、无 LLM 的 near-dup 判定，符合宪法 V 的降级路径；trigram Jaccard 对"同事件近似转述"（字符级重合）有效且确定性强。embedding 能捕捉"措辞完全不同但语义相同"（MemOS 用 0.92 余弦），但依赖 sidecar，故默认关。
- **Alternatives considered**:
  - 仅 embedding（MemOS 式 0.92 余弦）：无 embedding 端点时无法判定，违宪法 V 离线；拒绝。
  - LLM 判定（MemOS `MEMORY_RELATION_DETECTOR_PROMPT`）：每写入一次 LLM 调用，贵且非本地；只作未来可选，默认关，本 feature 不实现。
  - 仅 exact-token 重叠：对转述召回不足，漏检率高；拒绝。

## Decision 2 — 邻居扩展的 relation：共享 evidence 的兄弟 fact（depth-1 有界）

- **Decision**: 命中 fact 后，沿 `memory_projection_sources` 推导"共享同一 evidence 的其他 atomic-fact 投影"（兄弟 fact），depth-1，数量上限可配（默认小值，不无界放大 token）。无邻居时行为与关闭一致。
- **Rationale**: 共享 evidence 的兄弟 fact 天然是同一事件/话题的互补视角（如 MemOS 树中同 concept 下的兄弟 fact），且**完全离线**可从现有 lineage 表推导，无需图数据库或新表。depth-1 对应 MemOS depth-2 的保守子集，先验证最小有效步。
- **Alternatives considered**:
  - 语义邻居（embedding 近邻 fact）：与检索本身重叠，无增量信息且依赖 sidecar；拒绝。
  - 父主题节点（MemOS 的 topic/concept 总结节点）：engram 无总结父节点，引入需 LLM 建树，违宪法；列为后续（若 depth-1 兄弟有效再做）。

## Decision 3 — 机制绑定：022.v1 protocol `mechanism_flags` 向后兼容新 key

- **Decision**: 在 022.v1 的 `experiment.mechanism_flags` 增加两个新 key：`write_dedup`（写入时冗余抑制）、`neighbor_extend`（命中后邻居扩展）。新 key 是向后兼容新增字段，不改既有 key 语义（`idk_retry`/`iris`/`rerank` 保持）。新机制 protocol 独立冻结，不动已冻结的 022 资产。机制 flag 在 formal context 外 fail closed（复用 `validateMechanismArms` 框架）。
- **Rationale**: `mechanism_flags` 已是 022 的机制开关载体（`freezeFormalProtocol` / `validateFormalMechanismBinding` 现有绑定路径）；新增 key 是字段级向后兼容，不 bump MAJOR（宪法 III）。
- **Alternatives considered**: 新增独立协议字段/顶层 schema 版本 → 破坏性变更，需迁移说明；拒绝（不必要）。

## Decision 4 — 误伤测量：审计统计 + 配对消融兜底

- **Decision**: 冗余抑制记录每批判定统计（判定数、抑制数、疑似误伤数）。误伤的最终裁决靠配对消融：若抑制开启后 overall 不回归且候选密度上升，误伤在可接受范围；若分类别显著回退（如 multi-hop 因抑制细节被删），判定为误伤过高。
- **Rationale**: 离线 Jaccard 会误判"相似但不等价"（spec Edge Case：同一事件的不同细节）。无法离线百分百判定语义等价，故用"可观测审计 + 端到端配对"双层兜底（008 教训：不以 coverage 作 verdict，以端到端为准）。
- **Alternatives considered**: 仅靠覆盖率指标判断 → 违 008 教训；拒绝。

## Decision 5 — MemOS 参数落为 engram 参数

- **Decision**（源码核对，AutoDL `memos-repro/MemOS`）:
  - MemOS 写入去重 `merged_threshold=0.92`（embedding 余弦）→ engram 离线主路径用 Jaccard 0.7（现有 curation 值，字符级信号阈值域不同，不作为直接换算）；embedding 可选层默认阈值 ~0.9（校准后定）。
  - MemOS depth-2 图邻居 → engram depth-1 兄弟（先验证最小有效步）。
  - MemOS 写入时 LLM 融合（`NodeHandler.resolve`）→ **engram 明确不做**（破坏 append-only，且 Retain-or-Consolidate 证明 merge 显著为负 −0.107）。
- **Rationale**: 参数直接从 MemOS 代码取数（非记忆）；engram 只借鉴"去重 + 邻居"两个机制方向，不做其 LLM 依赖部分。
- **Alternatives considered**: 全套复刻 MemOS 写入管线 → 违宪法 I/V；拒绝。

## Residual Risks

1. **Jaccard 对跨措辞转述漏检**：同一事件完全换词表述可能漏检（字符 trigram 不重合）→ 用 embedding 可选层补，或接受漏检（配对消融定）。
2. **邻居扩展放大 token**：高关联 fact 兄弟多 → 数量上限强制，SC-002 用同 cap 验证。
3. **两机制交互**：同开时可能抵消（如抑制减少的兄弟 fact 又由扩展找回）→ SC-003 显式报告交互，不假设可加。
4. **022 资产未冻结**：依赖 022 协议/store 资产；若 022 未冻结则本 feature 实验不启动（spec Assumptions）。
