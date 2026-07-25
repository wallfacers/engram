# 契约：`memory/consolidation` 包公开 API

**日期**: 2026-07-25 | **稳定性**: 契约优先（宪法 III）——本文件先于实现冻结；
破坏性变更需 MAJOR + 迁移说明。

这是一个 Go 库特性，没有网络 API。「契约」即本包的公开 Go 表面，加上它对既有
引擎表面的**零改动**承诺。

## 0. 契约级承诺

| 承诺 | 内容 |
|---|---|
| **引擎既有表面零改动** | 本特性 MUST NOT 修改 `memory/`、`embedding/`、`provider/`、`store/` 中任何**既有**导出符号的签名或语义。唯一的既有文件改动是 `store/migrations.go` **追加** v6（新增不改旧） |
| **检索侧零改动** | `memory/retriever.go` 与三路信号 MUST NOT 有任何改动。产物同构即可被命中（FR-016） |
| **适配器零改动** | `mcpserver/` MUST NOT 改动。本特性不新增任何 MCP 工具 |
| **默认关闭** | 未配置语言模型时完全 inert（FR-020，宪法 I） |

## 1. 类型

```go
package consolidation

// ModelCaller 与 curation/pipeline 同形（research R7）。nil 表示未配置模型，
// 此时 Worker 完全 inert。
type ModelCaller func(ctx context.Context, system, user string) (string, error)

// Config 是固结 worker 的可调项。
type Config struct {
	// TopKPerPass 是单趟送入模型的候选对硬上限（FR-023）。初始值 2000。
	TopKPerPass int

	// MaxBucketSize 是单个实体桶的规模上限；超过即跳过该实体桶，
	// 用于消除高 df 实体导致的 O(df²) 组合爆炸。
	MaxBucketSize int

	// MinInterval 是两趟固结之间的时间下限。
	MinInterval time.Duration

	// LeaseTTL 是领导租约时长。
	LeaseTTL time.Duration

	// EntryCountLow 是水位线：非桥接记忆数低于此值时不触发固结
	// （小库不产生告警噪声，见 Edge Case）。
	EntryCountLow int
}

// CandidatePair 是一个跨 session、共享实体的候选证据对。
// 不变式：A < B（字典序）；len(Shared) >= 1；A、B 分属不同 session；
// A、B 均非桥接产物。
type CandidatePair struct {
	A      string
	B      string
	Shared []string
	Score  float64
}

// PairKey 返回该对的确定性幂等键，与枚举顺序无关。
func (p CandidatePair) PairKey() string

// BridgeVerdict 是模型对一个候选对的合成裁决。
// Bridged 为 false 表示模型判定两者之间不存在可推出的连接（FR-013）。
type BridgeVerdict struct {
	Bridged bool
	Content string
	SourceA string
	SourceB string
}

// PassStats 是一趟固结的结果统计（供日志与评测归因）。
type PassStats struct {
	CandidatesEnumerated int // 枚举出的候选对总数（截断前）
	CandidatesConsidered int // 实际送模型的对数（截断后、去幂等后）
	BridgesCreated       int // 成功落库的桥接记忆数
	ModelDeclined        int // 模型返回「无连接」的对数
	ValidationRejected   int // 校验未通过被拒的对数
	Errors               int // 失败并跳过的对数
}

// Worker 是后台固结循环。它持有领导租约，在水位线满足时运行一趟有界的固结：
// 枚举候选 → 模型合成 → 校验 → 落库。所有失败均 fail-safe（WARN + 跳过）。
type Worker struct { /* 未导出字段 */ }
```

## 2. 构造与驱动

```go
// NewWorker 在共享 DB 上构造固结 worker。
//
// call 为 nil 时 worker 完全 inert —— Notify/Start/RunPass 均为安全空操作，
// 固结根本不运行（FR-020，宪法 I）。
//
// embedder 可为 nil（向量化未配置）；此时产物仍会写入并被关键词/实体信号命中，
// 只是没有语义向量（宪法 V 逐信号降级）。
func NewWorker(
	store *memory.EntryStore,
	db *sql.DB,
	embedder *memory.Embedder,
	call ModelCaller,
	cfg Config,
	logger *slog.Logger,
) *Worker

// Notify 是压力触发信号：写入成功后在请求热路径之外调用。
// 非阻塞、去抖（已有待处理唤醒时直接吸收）。对 inert worker 安全。
func (w *Worker) Notify()

// Start 启动后台循环直到 ctx 取消。对 inert worker 是空操作。
func (w *Worker) Start(ctx context.Context)

// RunPass 评估水位线，赢得租约后运行恰好一趟有界固结。
// 导出以便测试与评测确定性驱动（照 curation.ResolveConflictsPass 先例，FR-021）。
// 错误被吸收（fail-safe）——RunPass 永不 panic、永不向外传播错误。
func (w *Worker) RunPass(ctx context.Context) PassStats
```

## 3. 内部单元（包内可测，不导出给引擎外）

```go
// EnumerateCandidates 是纯确定性的候选枚举，零模型调用（FR-012）。
// 同一输入永远产出同一序列：Score 降序，同分按 (A, B) 字典序升序。
func EnumerateCandidates(ctx context.Context, db *sql.DB, cfg Config) ([]CandidatePair, error)

// ParseVerdict 解析模型输出为裁决。无法解析或显式 NONE 均返回 Bridged=false，
// 不返回错误 —— 「模型说没有」是正常路径，不是故障（FR-013）。
func ParseVerdict(raw string) BridgeVerdict

// ValidateVerdict 施加 data-model §2.2 的全部校验规则。
// 返回 nil 表示可落库；返回错误表示拒绝（调用方告警并跳过，FR-014）。
func ValidateVerdict(v BridgeVerdict, pair CandidatePair, srcA, srcB *memory.Entry) error
```

## 4. 对 `store` 的唯一改动

```go
// store/migrations.go —— 仅追加，不改既有任何一行
var migrationsByVersion = []Migration{
	// ... v1..v5 原样不动 ...
	{Version: 6, Up: v6ConsolidationBridges, Down: v6ConsolidationBridgesDown},
}
```

## 5. 提示词

新增 `memory/prompt/` 下的固结提示词常量（照既有 extraction/curation 提示词的
组织方式）。契约要求：

- 系统提示 MUST 明确授予模型**拒绝权**，并规定拒绝时的确切输出记号（FR-013）。
- 输出格式 MUST 要求模型回述两个源标识，供 `ValidateVerdict` 校验（FR-014）。
- 提示 MUST 要求「非冗余」——桥接内容不得只是复述任一源事实（Edge Case）。

## 6. 不在契约内（明确排除）

- 抽象合成、去重合并、二阶固结
- 任何多租户/配额/计费实现（仅要求数据模型不堵死）
- 任何 MCP/CLI 工具暴露
- 任何检索侧改动
