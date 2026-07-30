# Engine and Adapter Contract

**Version**: 022.v1

**Scope**: Evidence Ledger、projection lineage、Semantic Episode、显式 ingest 与 lifecycle。
Evidence Compiler 的独立合同见 [compiler-contract.md](./compiler-contract.md)。

## Compatibility Promise

- 现有 `memory.Entry` 字段、`EntryStore.Upsert/Get/List/Delete`、`Retriever.Search` 签名与
  零配置行为保持兼容。
- `memory_write`、`memory_search`、`memory_list`、`memory_get`、`memory_delete` 的现有
  MCP input/output schema 和错误语义保持不变。
- `memory_delete(name)` 仍表示删除 Atomic Fact projection；它不隐式 tombstone/purge
  支持该 fact 的原始 Evidence。
- 003 graph 的 v3 schema、公开 Go API、已有数据和默认开关均不变。
- 新合同只通过 additive Go types/methods 和带新名称的 MCP tools 暴露。

## Public Go Types

以下是合同形状，不要求实现文件逐字采用相同内部 helper。

```go
package memory

type EvidenceSourceType string

const (
    EvidenceMessage     EvidenceSourceType = "message"
    EvidenceDirectWrite EvidenceSourceType = "direct_write"
    EvidenceLegacyEntry EvidenceSourceType = "legacy_entry"
)

type EvidenceState string

const (
    EvidenceActive      EvidenceState = "active"
    EvidenceTombstoned  EvidenceState = "tombstoned"
    EvidencePurged      EvidenceState = "purged"
)

type EvidenceInput struct {
    ExternalSourceID string
    SourceType       EvidenceSourceType
    SourceSessionID  string
    Speaker          string
    Ordinal          int
    Content          string
    OccurredAt       *time.Time
    RecordedAt       time.Time
}

type Evidence struct {
    ID               string
    ExternalSourceID string
    SourceType       EvidenceSourceType
    SourceSessionID  string
    Speaker          string
    Ordinal          int
    Content          string
    OccurredAt       *time.Time
    RecordedAt       time.Time
    ContentDigest    string
    State            EvidenceState
    Revision         int64
}

type EvidenceRef struct {
    EvidenceID string
    SourceOrder int
    FullSource bool
    StartChar  *int
    EndChar    *int
    SpanDigest string
}

type LifecycleRequest struct {
    EvidenceID string
    RequestID  string
    ReasonCode string
}

type PurgeResult struct {
    EvidenceID        string
    Purged            bool
    CheckpointPending bool
}
```

### LedgerStore

```go
type LedgerStore struct { /* opaque */ }

func NewLedgerStore(db *sql.DB) *LedgerStore

func (s *LedgerStore) AppendBatch(
    ctx context.Context,
    inputs []EvidenceInput,
) ([]Evidence, error)

func (s *LedgerStore) Get(
    ctx context.Context,
    evidenceID string,
) (*Evidence, error)

func (s *LedgerStore) GetMany(
    ctx context.Context,
    evidenceIDs []string,
) (map[string]Evidence, error)

func (s *LedgerStore) ListSession(
    ctx context.Context,
    sourceSessionID string,
    includeTombstoned bool,
) ([]Evidence, error)

func (s *LedgerStore) Tombstone(
    ctx context.Context,
    req LifecycleRequest,
) error

func (s *LedgerStore) Restore(
    ctx context.Context,
    req LifecycleRequest,
) error

func (s *LedgerStore) Purge(
    ctx context.Context,
    req LifecycleRequest,
) (PurgeResult, error)
```

行为：

- `AppendBatch` 先校验全部 input，再在一个事务中 append/reuse；任一 invalid/conflict
  导致整批无写入。
- `Get`/`GetMany` 默认只返回 active Evidence。tombstoned 返回 `ErrEvidenceUnavailable`，
  purged 返回 `ErrEvidencePurged`，未知返回 `store.ErrNotFound`。
- `ListSession` 按 ordinal、recorded_at、ID 稳定排序；从不跨当前 DB。
- `Tombstone`/`Restore`/`Purge` 执行 [data-model.md](../data-model.md) 定义的状态机和
  projection 闭包。
- `GetMany` 必须一次 SQL 批量读取并返回缺失/不可用 source 的结构化 error；Compiler
  不可把部分成功误作完整来源。

### Projection and Lineage API

```go
type ProjectionKind string

const (
    ProjectionAtomicFact     ProjectionKind = "atomic_fact"
    ProjectionSemanticEpisode ProjectionKind = "semantic_episode"
)

type Projection struct {
    ID             string
    Kind           ProjectionKind
    ObjectKey      string
    State          string
    Builder        string
    BuilderVersion string
    ConfigHash     string
    BuiltAt        time.Time
    Revision       int64
}

type ProjectionStore struct { /* opaque */ }

func NewProjectionStore(db *sql.DB) *ProjectionStore

func (s *ProjectionStore) SourcesByProjectionIDs(
    ctx context.Context,
    projectionIDs []string,
) (map[string][]EvidenceRef, error)

func (s *ProjectionStore) MarkStaleByEvidenceIDs(
    ctx context.Context,
    evidenceIDs []string,
) error
```

Projection builder 的 create/replace/delete 操作必须与 payload 和完整 lineage 同事务，
但不作为 adapter 可自由写入的通用 CRUD。Event/Scene/Profile/graph 不能通过传任意
字符串绕过 kind allowlist。

### EntryStore Additions

```go
func (s *EntryStore) UpsertWithSources(
    ctx context.Context,
    entry *Entry,
    sources []EvidenceRef,
) error

func (s *EntryStore) SourceRefs(
    ctx context.Context,
    entryID string,
) ([]EvidenceRef, error)
```

- 现有 `Upsert` 内部等价于 `UpsertWithSources` 加自动 direct-write self Evidence；签名
  和调用方观察到的 entry upsert 语义不变。
- `UpsertWithSources` 要求非空、active、当前 DB 内 sources；未知、跨 DB 不可见、purged、
  invalid span 都导致整个 entry/projection transaction 回滚。
- 同内容 dedup 获得新实际支持时，不静默丢弃来源；现有 projection lineage 与新来源
  取并集。
- `Delete` 删除 projection 及其所有可重建 side data，不删除 source Evidence。

### Retriever Result Additions

`memory.Result` 可以添加以下零值安全字段；现有字段和排序不变：

```go
type Result struct {
    // existing fields unchanged
    ID             string
    ProjectionID   string
    ProjectionKind ProjectionKind
}
```

lineage 不逐 hit 塞入 `Result`，调用方用 `SourcesByProjectionIDs` 批量获取，以避免 N+1。
`Retriever.Search` 不返回 Bundle、不调用 Compiler，也不自动展开 raw source。

## Pipeline Contract

### Message Shape

现有 `pipeline.Message` additive 扩展：

```go
type Message struct {
    Role             string
    Text             string
    ExternalSourceID string
    Ordinal          int
    OccurredAt       *time.Time
}

type IngestResult struct {
    Evidence []memory.Evidence
    Entries  []*memory.Entry
    Degraded []string
}
```

新增：

```go
func (p *Pipeline) IngestDetailed(
    ctx context.Context,
    sessionDate time.Time,
    sourceSessionID string,
    messages []Message,
) (IngestResult, error)
```

现有 `Ingest(...)(int,error)` 保留，作为 `IngestDetailed` 的兼容 wrapper，仅返回投影数。

### Ordering and Failure Semantics

1. 验证并 append 所有非空 message Evidence。
2. Evidence transaction 成功后，向 extractor prompt 注入稳定 Evidence ID。
3. Extractor 每个 fact 输出 `source_ids`；ID 必须来自本次 batch 且非空。
4. 每个 fact 与其 Atomic Fact projection/lineage 同事务写入。
5. 模型、parse 或单 fact validation 失败时，Ledger 仍成功；`IngestDetailed` 在
   `Degraded` 记录原因，失败 fact 数为零或减少。
6. Ledger append 自身失败时不调用 extractor，返回 error。

Pipeline 的 ADD-only 语义指“不会由 extractor 更新/删除已有规范 Evidence”，不允许
extractor 创建新 Evidence 或无来源 fact。

### Curation and Embedding

- curation merge 的输出 projection 直接引用所有输入 projection 的 Evidence 并集。
- 近重复 dedup 发现同 fact 的新支持时 union lineage，而非只返回 existed。
- embedder、entity、fact query、event alias 和 003 graph 只索引 active projection。
- tombstone 导致 projection stale 时，对应 side indexes 必须同步退出服务。
- purge 与异步 embedder race 以 projection revision/head state 拒绝过期回写。

## Semantic Episode Builder

```go
type EpisodeBoundary struct {
    FirstEvidenceID string
    LastEvidenceID  string
}

type EpisodeSegmenter interface {
    Segment(
        ctx context.Context,
        session []Evidence,
    ) ([]EpisodeBoundary, error)
}

type EpisodeStore struct { /* opaque */ }

func (s *EpisodeStore) RebuildSession(
    ctx context.Context,
    sourceSessionID string,
    segmenter EpisodeSegmenter,
    builderVersion string,
    configHash string,
) ([]Projection, error)

func (s *EpisodeStore) DeleteByConfig(
    ctx context.Context,
    configHash string,
) error
```

Segmenter 只能选择连续边界。Store 自行从 Ledger 读取 active sources、校验连续性、生成
确定性 narrative 并写完整 lineage。Segmenter nil/error 时 Episode capability unavailable，
不影响 Ledger、Atomic Fact 或 Search。

## Error Contract

可用 `errors.Is/As` 判断：

```go
var (
    ErrEvidenceConflict    = errors.New("memory: evidence idempotency conflict")
    ErrEvidenceUnavailable = errors.New("memory: evidence is tombstoned")
    ErrEvidencePurged      = errors.New("memory: evidence is purged")
    ErrInvalidEvidenceRef  = errors.New("memory: invalid evidence reference")
    ErrInvalidTransition   = errors.New("memory: invalid evidence transition")
    ErrPurgeIncomplete     = errors.New("memory: purge committed; checkpoint incomplete")
)
```

`ErrPurgeIncomplete` 表示逻辑 purge 已提交、只需重试 checkpoint；调用方不得重新写入
Evidence 或把它报告成 active。所有 error message 不含原文或 secret。

## Additive MCP Contract

### `memory_ingest_v2`

该 tool 始终注册，即使 LLM/extractor 未配置；它的最低承诺是保存 Evidence。

输入：

```json
{
  "namespace": "optional",
  "session_id": "required-non-empty",
  "messages": [
    {
      "source_id": "caller-stable-id",
      "role": "user|assistant",
      "text": "original text",
      "ordinal": 0,
      "occurred_at": "optional RFC3339"
    }
  ],
  "extract": true
}
```

输出：

```json
{
  "evidence": [
    {"source_id": "caller-stable-id", "evidence_id": "engine-ulid", "state": "active"}
  ],
  "extracted_count": 1,
  "entries": [{"name": "fact-name", "content": "fact"}],
  "degraded": []
}
```

`extract` 默认 true。Extractor 未配置/失败时 tool 仍成功保存 Evidence，
`extracted_count=0`，并返回 `degraded=["extraction_unavailable"]` 或受控原因。Ledger
append 失败则整个调用失败且 extractor 调用数为零。

### `memory_evidence_get`

输入 `{namespace?, evidence_ids:[...]}`；输出保持请求顺序的 active Evidence。任一 source
不可用时整体返回对应结构化 error，不部分拼接。

### `memory_evidence_tombstone` / `memory_evidence_restore`

输入 `{namespace?, evidence_id, request_id?, reason_code}`；分别调用 engine lifecycle API，
输出 `{evidence_id,state}`。

### `memory_evidence_purge`

输入 `{namespace?, evidence_id, request_id?, reason_code}`；输出
`{evidence_id,state:"purged",checkpoint_pending}`。`ErrPurgeIncomplete` 映射为可重试
tool error，同时不得泄露已清除内容。

## Adapter Boundaries

- Adapter 只负责 namespace 校验/handle 获取、JSON 映射与结构性 degraded 信息。
- Adapter 不执行 SQL、不展开 lineage、不实现 span 校验、不复制 projection 删除闭包。
- 本增量不新增 Evidence Compiler MCP tool。Compiler 首先作为 engine API 经 benchmark
验证；未来产品 tool 必须另行冻结真实 tokenizer 和一次 answerer orchestration 合同。
- 既有 `memory_ingest` 继续只在 LLM 配置时出现，保持旧调用方行为；新调用方使用
`memory_ingest_v2` 获得不可损失语义。

## Contract Tests

必须先于实现写出：

1. 现有 tools schema 与 direct-engine parity 不变；
2. `memory_ingest_v2` 无 LLM 仍保存 Evidence，0 projection；
3. batch conflict 原子回滚且 extractor 未调用；
4. 两 namespace 相同 external source ID 互不可见；
5. `memory_write` 自动建立且复用 self Evidence；
6. tombstone/restore/purge 状态、错误和 projection closure；
7. old DB v6→v7 backfill、fresh v7、重复 migrate 和失败 rollback；
8. 003 schema/API/data 未被 v7 改写；
9. 批量 lineage 没有逐 hit SQL；
10. purge 后 FTS/vector/entity/query/alias/graph/source recovery 都不可见。
