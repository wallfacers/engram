# Data Model: 跨消息语义聚类 episode 表示

**Feature**: 025-semantic-episode-cluster

**Date**: 2026-08-01

**Base**: 复用 022 的 v7 Evidence Ledger / projection registry / lineage 模型,详见
`specs/022-benchmark-parity-memory-architecture/data-model.md`。本文只记录 025 的增量
(跨 session 语义聚类)。

## Design Rules

1. **零新 migration(预期)**:`memory_semantic_episodes` 表无 session 约束;
   `memory_projection_sources` lineage 天然支持跨 session(evidence_id 直连,无 session 列)。
   跨 session 聚类的差异只在构建代码(`RebuildAll` 跳过 RebuildSession 的同 session 连续校验),
   不在 schema。若后续确需持久化聚类判定审计,走 additive migration。
2. Evidence 是调用方实际提供的规范原文;episode 是可丢弃、可重建的投影视图。
3. 聚类域 = 同 namespace DB 内全部 active Evidence;跨 session 默认允许、有界;跨 namespace 隔离。
4. 所有时间使用 Unix microseconds;所有 source span 使用 Unicode code-point `[start_char,end_char)`。

## Relationship Overview

```text
Evidence (immutable content, 跨 session)
  │
  └── N:M ProjectionSource <────── Projection (kind='semantic_episode')
                                        │
                                        └── memory_semantic_episodes (narrative)
```

与 022 的关系图差异:**semantic_episode 的 lineage 不再要求同一 source_session_id**;
一个 episode 可引用多个 session 的 Evidence。

## Persistent Entities

### Semantic Episode(跨 session 版本)

`memory_semantic_episodes` 表结构不变(projection_id PK、narrative、started_at、ended_at、
char_count)。**边界约束放宽**:

| 约束 | 022(同 session) | 025(跨 session) |
|------|------------------|------------------|
| sources 必须来自同一 `source_session_id` | 是 | **否**(可跨 session) |
| 连续 ordinal | 是 | **否**(语义相关即可,不要求时间连续) |
| 至少一条 Evidence | 是 | 是 |
| 同一 builder/config/source digest 重建产生相同 narrative | 是 | 是(确定性截断保证) |

`memory_projections` registry 不变(kind='semantic_episode'、config_hash、revision 递增)。
`memory_projection_sources` lineage 不变,但 source_order 跨越多个 session 依然从 0 开始
连续编号(渲染顺序)。

## v8 Migration(若有)

预期不需要。若需要把聚类判定审计统计持久化到 DB(而非仅 run 侧 artifact),才新增 additive
v8:

```text
memory_episode_cluster_audit(
  run_id      TEXT,        -- 构建运行身份(可对齐 config_hash)
  decisions   INTEGER,     -- 判定总数
  clusters    INTEGER,     -- 聚类数
  suspected   INTEGER      -- 疑似误聚类数
)
```

**默认不建**。审计统计先走 run 侧 JSON artifact(对齐 024 suppression-audit)。

## Query-time Entities

### EpisodeCluster(内存对象,不进产品 SQLite)

| 字段 | 含义 |
|------|------|
| `EvidenceIDs` | 有序跨 session Evidence ID 列表(确定性顺序:ordinal 升序) |
| `ClusterKey` | 判定依据摘要(共享实体/关键词),供审计 |

### SemanticClusterer(引擎接口)

```go
type EpisodeCluster struct {
    EvidenceIDs []string
    Signal      string // "entity" | "keyword" | "embedding" — 触发信号,审计用
}

type SemanticClusterer interface {
    Cluster(ctx context.Context, evidence []memory.Evidence) ([]EpisodeCluster, error)
}
```

输入为同 namespace 全部 active Evidence;输出为证据簇。embedding overlay 是可选依赖,
nil 时纯离线。

## Evaluation Entities

### 配对协议

复用 022 冻结协议(cap 3600)。新增 eval-config 字段:

- `episode_cluster: bool`(默认 false;true 时渲染前 RebuildAll)
- `cluster_signals: {min_keyword_jaccard: 0.25, embed_thresh: 0.9}`(阈值,eval-config,
  独立提交)
- `max_evidence_per_episode: 8`(默认)

`protocol_hash` 计算包含上述字段;改动属 eval-config 变更,与算法改动分开提交。

### 判定统计 artifact

对齐 024 suppression-audit:`decisions / clusters / suspected_mis` → run 侧 JSON。
