# Data Model: 记忆密度杠杆的数据契约

**Date**: 2026-07-31 | **Feature**: [spec.md](spec.md) | **Branch**: `024-memory-density`

## Design Rules

- **无新表、无新迁移**：两机制复用 022 已交付的 schema v7（`memory_evidence` / `memory_projections` / `memory_projection_sources` / `memory_entries`），不新增持久化表，不 bump schema version（宪法 III：非破坏性）。
- **抑制即不写**：写入时冗余抑制表现为"不创建新的 atomic fact 投影"，而非删除/覆盖任何已有行——evidence ledger 保持 append-only。
- **邻居离线推导**：兄弟 fact 由 `memory_projection_sources` 的共享 evidence 关系在查询时推导，不落任何派生表。
- **可审计**：抑制判定计数（判定/抑制/疑似误伤）作为运行时统计输出（非持久化事实表），供配对消融评估误伤率。

## Relationship Overview

```text
Evidence (memory_evidence) ──< projection_source >── Projection (memory_projections)
        ▲                                             │
        │                                             │ object_key
        │                                             ▼
        └────────── shared-evidence 兄弟关系 ────── memory_entries (fact)
                     (查询时离线推导, depth-1 有界)
```

- **兄弟 fact**：两个 atomic-fact 投影共享至少一条 evidence（`memory_projection_sources.evidence_id` 相同），即为兄弟；命中其一可在候选冻结后扩展另一。
- **冗余对**：新 fact 投影与既有投影被冗余判定器判为近重复时，**不创建新投影**（可选的 relation 标记见下）。

## Persistent Entities（复用，无新增）

### Evidence Record（复用 `memory_evidence`）

- 语义不变：append-only 消息级原文。冗余抑制、邻居扩展均**不得删改**任何 evidence 行（spec FR-002 / FR-006）。
- 本 feature 不新增字段。

### Projection Registry（复用 `memory_projections`）

- 现有字段 `kind`（`atomic_fact`）、`object_key`、`state`、`builder` 语义不变。
- 写入时抑制 = 对判定为冗余的新 fact **不执行 `replaceAtomicFactProjectionTx` 的 create 分支**；既有的第一个投影保持不变。

### Projection Source（复用 `memory_projection_sources`）

- 现有 `relation` 字段（如 `supports`）保留；本 feature **新增一个 relation 值 `redundant`**（可选，仅用于审计标记"被抑制的冗余对"），不改变既有值的语义。
- 邻居扩展直接读 `evidence_id` 共现：`SELECT DISTINCT p.object_key FROM memory_projection_sources p WHERE p.evidence_id IN (命中 fact 的 evidence_ids) AND p.object_key != 命中fact`，depth-1，有界（上限可配）。

### Atomic Fact Entry（复用 `memory_entries`）

- 冗余抑制的目标对象：被抑制的新 fact 不产生新 `memory_entries` 行、不产生新 `memory_projections` 行。
- 冲突事实（非冗余）**不被抑制**（spec FR-003 / Edge Case：日期纠正须保留）。

## 状态/生命周期

- **冗余抑制**：无持久化状态转移（抑制 = 不写入，无后续生命周期）。
- **邻居扩展**：查询期一次性推导，无状态；与检索候选的合并是纯函数（命中集 + 有界兄弟集 → 扩展候选），确定性顺序（evidence 顺序 → fact name）。
- 两机制的 on/off 状态由 protocol `mechanism_flags` 绑定（见 [contracts/mechanism-bindings.md](contracts/mechanism-bindings.md)），默认 off；off 时零行为变化（spec FR-004/FR-008）。
