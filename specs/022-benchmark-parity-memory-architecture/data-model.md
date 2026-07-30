# Data Model: Evidence Ledger、Projection 与 Evidence Compiler

**Feature**: 022-benchmark-parity-memory-architecture

**Date**: 2026-07-30

## Design Rules

1. Evidence 是调用方实际提供的规范原文；projection 是可清空、可重建的查询视图。
2. 每个 projection 直接引用其全部 Evidence，不能仅通过另一 projection 间接引用。
3. namespace 不进入 engine schema；MCP 继续用“一 namespace 一 DB”物理隔离。
4. `memory_entries` 继续保存 Atomic Fact/search payload，不改造成多 kind 通用表。
5. V1 不预建 Event、Scene、Profile 或新 graph 表；只有对应实验 GO 后再增加 migration。
6. 所有时间使用 Unix microseconds；所有 source span 使用 Unicode code-point
   `[start_char,end_char)`。

## Relationship Overview

```text
Evidence (immutable content)
  │
  ├── 1:N EvidenceLifecycleEvent ──> EvidenceHead (current-state cache)
  │
  └── N:M ProjectionSource <────── Projection
                                  ├── atomic_fact ──> memory_entries
                                  └── semantic_episode ──> memory_semantic_episodes

Frozen Candidate ──> Candidate lineage ──> Evidence
       │
       └── Compiler Action ──> Grounded Trace ──> Evidence Bundle
                                                       │
                                                       └── one Answerer call
```

## Persistent Entities

### Evidence Record

`memory_evidence` 保存不可变、仍可读取的规范原文。

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `id` | TEXT | 新写入由 Engine 生成 ULID；v7 backfill 使用保留前缀 `legacy:<entry-id>`；主键且 purge 后不得复用 |
| `source_type` | TEXT | `message \| direct_write \| legacy_entry` |
| `external_source_id` | TEXT | 调用方幂等 ID；可空 |
| `source_session_id` | TEXT | 会话/摄入批次分组；direct write 可空 |
| `speaker` | TEXT | 原始 speaker；direct/legacy 可空 |
| `ordinal` | INTEGER | session 内显式顺序；不得从 ULID 或时间推断 |
| `content` | TEXT | 非空、合法 UTF-8；写入后不可 UPDATE |
| `occurred_at` | INTEGER | 事件/发言发生时间；可空 |
| `recorded_at` | INTEGER | 首次落库时间，非空 |
| `content_digest` | TEXT | 规范 UTF-8 bytes 的 SHA-256，用于幂等和 span 校验 |

约束：

- 若 `external_source_id` 非空，则
  `(source_type, source_session_id, external_source_id)` 唯一。
- 同一个幂等键与完全相同 payload 重试返回原 Evidence；payload 不同返回
  `ErrEvidenceConflict`，不覆盖旧内容。
- `source_type=message` 要求非空 session、speaker 和非负 ordinal。
- `source_type=direct_write` 表示没有上游 turn 的 self Evidence。
- `source_type=legacy_entry` 只由 v7 backfill 生成，不宣称 message provenance。

### Evidence Lifecycle Event

`memory_evidence_events` 是 append-only 处置日志。

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `seq` | INTEGER | 自增主键，定义全序 |
| `event_id` | TEXT | ULID，唯一 |
| `evidence_id` | TEXT | 被处置 Evidence ID；不设 FK，使 purge 后审计仍在 |
| `source_type` | TEXT | 非内容型审计元数据 |
| `action` | TEXT | `append \| tombstone \| restore \| purge` |
| `recorded_at` | INTEGER | 处置时间 |
| `reason_code` | TEXT | 枚举型、不得包含原文 |
| `request_id` | TEXT | 调用幂等/审计 ID；可空但不得含 secret |

事件表禁止保存 content、speaker、session、digest、span 或 projection text。

### Evidence Head

`memory_evidence_heads` 是 lifecycle 的可重建当前状态缓存，同时阻止 purge 后 ID 复用。

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `evidence_id` | TEXT | 主键 |
| `state` | TEXT | `active \| tombstoned \| purged` |
| `last_seq` | INTEGER | 最后 lifecycle event |
| `revision` | INTEGER | 每次状态变化递增 |
| `changed_at` | INTEGER | 最近状态变化时间 |

状态机：

```text
missing ──append──> active ──tombstone──> tombstoned
                       ^                     │
                       └──────restore────────┘

active | tombstoned ──purge──> purged
purged ──X──> any other state
```

非法重复处理：

- 对 active 再 tombstone、对 tombstoned 再 restore：以 request ID 幂等，否则返回明确
  state error。
- purged 永远不能 restore，也不能以相同 Evidence ID append。

### Projection Registry

`memory_projections` 保存派生对象身份和重建状态，不保存规范原文。

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `id` | TEXT | Projection ULID，主键 |
| `kind` | TEXT | V1 只允许 `atomic_fact \| semantic_episode` |
| `object_key` | TEXT | 对应 payload 稳定键；atomic fact 使用 entry ID |
| `state` | TEXT | `active \| stale \| disabled` |
| `builder` | TEXT | 构建器名称 |
| `builder_version` | TEXT | 代码/模型版本 |
| `config_hash` | TEXT | 构建配置 fingerprint |
| `built_at` | INTEGER | 构建时间 |
| `revision` | INTEGER | 重建递增 |

`(kind, object_key)` 唯一。`event`、`scene`、`profile` 和 graph 不在 v7 的允许枚举中；
对应阶段若 GO，先通过新的 contract/migration 扩展。

Projection 状态机：

```text
absent ──build──> active ──source tombstone/config change──> stale
                       │                         │
                       ├──disable────────> disabled
                       └──purged source──> deleted

stale | disabled ──validated rebuild──> active
```

只有 `active` 且至少有一条 active source 的 projection 可服务。`restore` 不会自动把
旧 projection 变 active；它必须经 builder 重建。

### Projection Source

`memory_projection_sources` 实现直接、完整的 N:M lineage。

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `projection_id` | TEXT | Projection FK，删除 projection 时级联 |
| `source_order` | INTEGER | 渲染顺序，从 0 开始且 projection 内唯一 |
| `evidence_id` | TEXT | Evidence ID |
| `full_source` | INTEGER | 1 表示引用整条 Evidence |
| `start_char` | INTEGER | 可空；code-point 起点 |
| `end_char` | INTEGER | 可空；code-point 终点，不含 |
| `span_digest` | TEXT | 可空；复原 span 的 SHA-256 |
| `relation` | TEXT | `supports \| derived_from`；V1 fact/episode 使用 `supports` |

主键为 `(projection_id, source_order, evidence_id)`。另建 `evidence_id` 索引以使 purge
依赖闭包为一跳查询。

校验：

- `full_source=1` 时 span 三字段为空。
- `full_source=0` 时 `0 <= start_char < end_char <= rune_count(content)` 且 digest 匹配。
- source 必须 active；purged/tombstoned/未知 source 不可建立 active lineage。
- 一个 active projection 不得使用空 source set。

### Atomic Fact Payload

现有 `memory_entries` 保持 schema 语义和 outward behavior；它不再承担 Evidence 真相。
每个 entry 对应一个 `kind=atomic_fact` registry row，entry ID 是 `object_key`。

写入规则：

- 既有 `EntryStore.Upsert` 签名不变。没有显式 sources 时，事务内创建或复用
  `direct_write` self Evidence，并建立 lineage。
- 新的 `UpsertWithSources` 只接受当前 DB 内 active Evidence IDs。
- 同名同内容 direct write 不新增 Evidence；同名改内容 append 新 self Evidence，将
  projection lineage 切到新 Evidence，旧 Evidence 保留。
- pipeline/curation merge 必须把所有实际支持 source 的集合直接挂到结果 fact。
- 删除 Atomic Fact 只删除 projection payload、embedding、entity/query/event alias 和
  003 已有关联数据；不删除 Evidence。

### Semantic Episode Payload

`memory_semantic_episodes` 只保存已验证边界的确定性渲染：

| 字段 | 类型 | 约束与含义 |
|------|------|------------|
| `projection_id` | TEXT | 主键/FK 到 registry |
| `narrative` | TEXT | 按 source_order 拼接原文与 speaker label，不做生成式摘要 |
| `started_at` | INTEGER | sources 中最早 occurred_at；可空 |
| `ended_at` | INTEGER | sources 中最晚 occurred_at；可空 |
| `char_count` | INTEGER | Unicode code-point 数 |

边界约束：

- sources 必须来自同一 `source_session_id`、连续 ordinal、至少一条。
- segmenter 只输出 `[first_source_id,last_source_id]` 边界，不得改写内容。
- 相同 builder/config/source digest 重建必须产生相同 narrative。
- 删除 Episode 只删除 registry/payload/lineage，不删除 Evidence。

Raw-turn window 不持久化为 projection；查询或 bake-off 根据 anchor Evidence 的 session
和 ordinal，从 active Ledger 读取前后窗口并记录展开的 source IDs。

## v7 Migration

v7 是在现有 v1–v6 之后追加的一次 migration；不得编辑已发布 migration。

新增表与索引：

```text
memory_evidence
memory_evidence_events
memory_evidence_heads
memory_projections
memory_projection_sources
memory_semantic_episodes

idx_evidence_session_ordinal(source_session_id, ordinal)
uq_evidence_external(source_type, source_session_id, external_source_id)
  WHERE external_source_id <> ''
idx_evidence_heads_state(state)
idx_projection_kind_state(kind, state)
idx_projection_sources_evidence(evidence_id)
```

Backfill 在同一个 v7 transaction 内：

1. 按 `memory_entries.id` 排序扫描现有 entry。
2. 生成确定性 Evidence ID `legacy:<entry.id>`，source type 为 `legacy_entry`，content
   为迁移时 entry 的规范 snapshot。
3. 插入 append event/head。
4. 创建 `atomic_fact` projection，`object_key=entry.id`，并建立 full-source lineage。
5. 任一步失败则 v7 schema、数据和 `schema_version` 全部回滚。

Backfill 重跑必须幂等。它不改变 entry、FTS、embedding、entity、fact query 或 003 graph
数据，因而不能被描述为 message-level lineage。

## Lifecycle Transactions

### Explicit Conversation Ingest

```text
BEGIN
  validate the complete message batch
  append/reuse all Evidence + lifecycle heads
COMMIT
run optional extraction
BEGIN
  upsert facts with exact source IDs
  write projection registry + lineage
COMMIT
```

Evidence batch 本身全有或全无。Extractor/parse/model 失败发生在 Evidence commit 之后，
因此允许 “Evidence 已保存、0 projection” 的诚实降级。事实事务失败不回滚 Ledger。

### Tombstone

一个事务内：

1. append tombstone event；
2. 更新 head；
3. 找出直接引用该 Evidence 的 projection；
4. 若 projection 已无 active support，标为 stale 并从可服务索引移除。

不得删除原文。raw-turn reader 默认排除 tombstoned Evidence。

### Restore

一个事务内 append restore event、更新 head；依赖 projection 保持 stale。重建器可以
随后按 kind/config 重建，不允许沿用未重新验证的旧派生文本。

### Privacy Purge

在 `PRAGMA secure_delete=ON` 的连接上，一个事务内：

1. 获取并锁定 head revision；
2. 找出所有直接依赖 projection；
3. 复用各 projection store 的完整删除逻辑，清除 payload、FTS、embedding、entity、
   query、alias 和 003 边；
4. 删除 projection registry/lineage；
5. 删除 `memory_evidence` 内容行；
6. append 无内容 purge event，把 head 标为 purged；
7. commit。

提交后执行 `PRAGMA wal_checkpoint(TRUNCATE)`。失败返回可重试 `ErrPurgeIncomplete`；
状态保持 purged，后续重试只完成物理 checkpoint。保证边界仅覆盖当前 engine 管理的
SQLite DB、WAL 和 projection，不覆盖外部备份、export 或调用方副本。

## Query-time Entities

这些实体是内存对象和评测 artifact，不进入产品 SQLite。

### Candidate

| 字段 | 含义 |
|------|------|
| `ID` | 稳定 projection/anchor ID |
| `Kind` | `chunk \| raw_turn \| semantic_episode \| atomic_fact` |
| `Rank`, `Score` | 冻结检索结果 |
| `Text`, `TextDigest` | 本臂逐字节回放内容 |
| `SourceIDs` | 完整 lineage，稳定排序 |
| `Metadata` | 时间、speaker、session 等非内容渲染信息 |

### Evidence Need

| 字段 | 含义 |
|------|------|
| `Entities` | 必需实体集合 |
| `TimeConstraints` | absolute/relative time operands |
| `Operands` | multi-hop 操作数及满足状态 |
| `ListCardinality` | `known(n) \| unknown` |
| `UpdateState` | 需要 current/previous/conflict state |
| `Gap` | 最多一个 `entity \| time_range \| second_operand` 结构化缺口 |

低置信度不是 Gap 类型。

### Source Span

`SourceSpan{SourceID, StartChar, EndChar, SpanDigest}` 必须能从 active Evidence 的
code-point 切片精确复原。任何 action 都不能引用 candidate lineage 之外的 source。

### Compiler Action

封闭 union：

- `KEEP(candidate_id | source_id)`：保留已验证原文。
- `EXTRACT(source_span)`：保留可精确复原片段。
- `DROP(candidate_id, reason)`：显式丢弃。
- `MERGE([]GroundedSentence)`：逐生成句引用一个或多个有效 SourceSpan。
- `FETCH_SOURCE(source_id)`：只按 candidate lineage 的 ID 读取；不是 Search。

V1 不存在 `ADD`。未知 action 是 invalid proposal。

### Grounded Trace

记录 run/candidate/protocol ID、Evidence Need、proposal 与最终 action、所有 span/citation、
时间/冲突关系、每步完整 prompt token 数、drop reason、fallback reason 和剩余 Gap。
Trace 是审计，不是新的 Evidence。

### Evidence Bundle

| 字段 | 约束 |
|------|------|
| `Items` | 有序 KEEP/EXTRACT/MERGE 渲染结果 |
| `SourceIDs` | 所有 item lineage 的并集 |
| `RenderedContext` | 实际送入 answer prompt 的内容 |
| `InputTokens` | 对完整 model/system/user prompt 的实际 tokenizer 计数 |
| `TokenCap` | 冻结协议值；`InputTokens <= TokenCap` |
| `CounterFingerprint` | tokenizer/model/chat-template fingerprint |
| `TraceDigest` | 对应 Grounded Trace digest |

若 counter/source/cap 校验失败则 Bundle 不可服务，answerer 调用数必须为 0。

## Evaluation Entities

### Frozen Protocol

一份 `022.v1` manifest 冻结 dataset、denominator、answerer、judge、prompt、extractor、
embedding、candidate rules/budget、tokenizer/cap、repetition/majority、mechanism flags 与
git commit。其 canonical JSON SHA-256 是 `protocol_hash`。

### Ranked Anchor

表示实验的共同检索起点：`question_id`、stable candidate ID、rank、score、text digest、
全部 source IDs。不同 renderer 可以展开不同 source closure，但必须记录展开差异。

### Miss Classification

按顺序互斥：

```text
gold_unresolved
candidate_miss
compiler_miss
answerer_miss
success
```

所有题进入 accuracy 分母；只有 `gold_unresolved` 从 source-survival miss-rate 分母排除。

### Evaluation Summary

按 benchmark、category、cap、cohort、arm 记录 majority accuracy、CI、exact McNemar、
candidate/bundle gold-source survival、平均/p95 tokens、retrieval/answer calls、latency、
cost 和 GO/HOLD/STOP。完整 JSON schema 见
[evaluation-artifacts.md](./contracts/evaluation-artifacts.md)。

## Explicit Non-Entities in V1

以下结构在 v7 中不存在：通用 L0→L3 hierarchy、Event calendar、Scene store、Profile
store、current-state store、新 typed graph、projection-to-projection provenance。
它们只有在独立 residual-cohort 实验 GO 后才能获得新的 spec、migration 和默认合同。
