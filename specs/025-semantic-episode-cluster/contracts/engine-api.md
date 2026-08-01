# Engine API Contract: 跨消息语义聚类 episode 表示

**Feature**: 025-semantic-episode-cluster

**Date**: 2026-08-01

**Status**: 冻结于实现前(additive,向后兼容;不改动 022 已交付契约)

**Scope**: 本契约定义 025 新增的引擎公开 API。只新增,不修改 022 的
`EpisodeStore.RebuildSession` / `EpisodeSegmenter` / `ProjectionStore` / `LedgerStore`
既有契约。MCP adapter 不涉及本 feature(研究实验)。

## 1. EpisodeCluster(新增类型)

```go
// EpisodeCluster 是跨 session 语义聚类的输出:一个证据簇。
// EvidenceIDs 是确定性排序(ordinal 升序)的跨 session Evidence ID 列表。
type EpisodeCluster struct {
    EvidenceIDs []string
    Signal      string // 触发信号:"entity" | "keyword" | "embedding";审计用
}
```

## 2. SemanticClusterer(新增接口)

```go
// SemanticClusterer 对 namespace 内全部 active Evidence 做跨 session 语义聚类。
// 输入为同 namespace 的全部 active Evidence;输出为证据簇(有界、确定性)。
// 实现必须:
//   - 默认离线(共享实体 + 关键词重叠,任一达成即聚);
//   - 可选 embedding overlay(nil 时纯离线,不依赖在线端点);
//   - 每簇证据数不超过配置上限;
//   - 输出确定性可重建。
type SemanticClusterer interface {
    Cluster(ctx context.Context, evidence []memory.Evidence) ([]EpisodeCluster, error)
}
```

实现:本 feature 交付 `memory.NewOfflineClusterer(opts)`(实体+关键词)与可选
`memory.NewHybridClusterer(opts, embedder)`(叠加 embedding cosine)。两者满足同一接口;
`opts` 含阈值与上限(research.md R3/R4)。

## 3. EpisodeStore.RebuildAll(新增方法)

```go
// RebuildAll 遍历 namespace 内全部 active Evidence(跨 session),用 clusterer 聚类,
// 复用 buildEpisodeTx 写 semantic_episode projection + lineage。
// 与 RebuildSession 的差异:不做"同一 session、连续 ordinal"校验;允许跨 session。
// config-hash 幂等:同 config 重建删旧建新,digest 稳定。
// 返回新建/更新的 Projection 列表。
func (s *EpisodeStore) RebuildAll(
    ctx context.Context,
    clusterer SemanticClusterer,
    builderVersion string,
    configHash string,
) ([]Projection, error)
```

语义:

- `clusterer == nil` → 返回 `ErrEpisodeClustererRequired`(新增错误值)。
- 聚类域 = 同 namespace DB 内全部 **active**(非 tombstoned/purged)Evidence,跨
  `source_session_id`。
- 空 Evidence → 空结果(no-op)。
- 每簇写入一个 `semantic_episode` projection;lineage 的 source_order 从 0 连续编号。
- 同 config 重建:删除旧 episode projections(该 config_hash)再建,revision 递增。

### 新增错误值

```go
var ErrEpisodeClustererRequired = errors.New("memory: episode clusterer is required")
```

## 4. EpisodesForEvidence(新增辅助查询)

```go
// EpisodesForEvidence 按 evidence_id 反查引用它的 active semantic_episode projections。
// 供渲染路由:anchor(fact/chunk)经 lineage 拿到 evidence IDs 后,定位所属 episode。
// 无命中返回 nil(渲染器退化为直读 anchor source,零行为变化)。
func (s *EpisodeStore) EpisodesForEvidence(
    ctx context.Context,
    evidenceIDs []string,
) (map[string][]Projection, error)
```

返回 `evidence_id → 引用它的 episode projections`(确定性排序)。批量化,无 N+1。

## 5. 兼容性承诺

- 022 的 `RebuildSession` / `EpisodeSegmenter` / `buildEpisodeTx` / `DeleteByConfig` /
  `EpisodeConfigHash` 签名与语义不变(仅 `RebuildSession` 保留用于验证补全)。
- `semanticEpisodeRenderer` 不改签名;其渲染路由扩展(anchor → lineage → episode)由 eval
  侧实现,不改变 renderer 公开行为。
- 既有 `memory_semantic_episodes` / `memory_projections` / `memory_projection_sources`
  schema 不变(零新 migration)。

## 6. 离线降级保证(宪法 I/V)

- 无 embedding 端点:`NewOfflineClusterer` 是默认,`NewHybridClusterer` 传 nil embedder
  退化为离线。不存在"必须在线端点才能聚类"的路径。
- 聚类构建失败:只影响 `semantic_episode` 视图;fact/chunk 检索与 answerer 路径不受影响。
- 关闭 `--episode-cluster`(默认):eval 不调用 RebuildAll,semantic_episode 臂走 fallback,
  与现状逐字节一致(FR-003)。
