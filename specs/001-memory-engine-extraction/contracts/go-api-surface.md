# Phase 1 Contract: 公开 Go API 面

**说明**:本特性无 REST/GraphQL 对外接口——engram 此阶段是一个 **Go 库**,其"契约"是
公开包的 API 面。本特性冻结该面为"与抽离前语义等价"(FR-013),仅改包路径。namespace 隔离、
错误语义规整、Mem0 兼容 HTTP API 等**新契约**留待后续「契约」spec,不在此定义。

**契约稳定性**:v0。方法签名与字段与 workhorse-agent 抽离前**逐一致**。任何签名/字段的
破坏性变更须走后续 spec + 版本 MAJOR(宪法原则 III)。

## 包路径映射(唯一允许的变化)

| 抽离前 | 抽离后 |
|--------|--------|
| `…/internal/memory` | `github.com/wallfacers/engram/memory` |
| `…/internal/memory/curation` | `github.com/wallfacers/engram/memory/curation` |
| `…/internal/memory/pipeline` | `github.com/wallfacers/engram/memory/pipeline` |
| `…/internal/embedding` | `github.com/wallfacers/engram/embedding` |
| `…/internal/provider`(+anthropic/openai) | `github.com/wallfacers/engram/provider` |
| `…/internal/store`(记忆接口/类型切片:Store/ErrNotFound/Upsert/BumpUsage) | `github.com/wallfacers/engram/store` |
| `…/internal/store/sqlite`(记忆切片) | `github.com/wallfacers/engram/store` |
| `…/internal/idgen` | `github.com/wallfacers/engram/internal/idgen`(不对外) |
| `…/internal/tools/sessionsearch` 的 `BuildPlan`/`LikeFragments` | 内化为 `memory` 包内未导出的 `buildPlan`/`likeFragments`(`queryplan.go`) |

## 公开入口(`memory` 包,签名不变)

存储与检索:
- `NewEntryStore(db *sql.DB) *EntryStore` — 记忆条目 CRUD/预算
- `NewVectorStore(db *sql.DB) *VectorStore` — 向量存取
- `NewEmbedder(entries *EntryStore, vectors *VectorStore, client embedding.Client, buf int) *Embedder` — 异步补向量
- `NewRetriever(entries *EntryStore, vectors *VectorStore, client embedding.Client) *Retriever` — 三路混合检索
- `NewUsageLogger(store *EntryStore, buf int) *UsageLogger` — 命中/使用记账

类型:`Entry`、`Result`、`EntryStore`、`VectorStore`、`Embedder`、`Retriever`、`Budgets`、
`UsageLogger`、`Snapshot`、`Loader`。

错误:`ErrMemoryTooLarge`、`ErrTriggerInvalid`、`ErrPinnedBudgetExceeded`。

子包:`curation`(确定性 scorer + 近重复聚类 + LLM judge + leader-lease)、`pipeline`
(ADD-only 抽取)——公开面同样保持同形。

## store 包契约(记忆切片)

`store` 包合并宿主两个 store 包的**记忆闭包**:`internal/store`(接口/类型)+
`internal/store/sqlite`(SQLite 实现)。

- `store.Open(opts store.Options) (*store.Store, error)` — 打开只含记忆 schema 的 SQLite
- `store.Options` — 打开选项(路径等,字段同源)
- `store.Store` — 持有 `*sql.DB`;建链只跑记忆迁移
- `store.ErrNotFound` — 记忆条目未找到错误(切自 internal/store)
- `store.Upsert(...)` / `store.BumpUsage(...)` — entrystore 依赖的记忆存取契约(切自 internal/store)
- `ProbeFTS5(db *sql.DB) error` — FTS5 可用性探测

**不纳入**:`Session` / `SessionState` / `SessionSummary` 等会话类型留宿主。

## 契约级验收

- **C-1**:引用 engram 公开包即可完成 workhorse 抽离前对 `internal/memory` 的等价用法,
  无需引用任何宿主包。
- **C-2**:抽离后编译期不出现指向 `workhorse-agent/internal/{store之外的宿主包}` 的引用。
- **C-3**:公开类型的字段与方法集与抽离前一致(以既有单测编译通过为机器验证)。
