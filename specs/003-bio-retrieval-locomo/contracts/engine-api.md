# Contract: 引擎新增 Go API（feature 003）

> 原则：`Retriever.Search(ctx, query, k)` 公开签名**不变**；新机制经选项结构启用，
> 默认全关（行为与 002 之前完全一致）。破坏性变更为零，无需 MAJOR。

## 1. Retriever 选项（memory/retriever.go）

```go
// RetrieverOptions 控制可选检索信号；零值 = 全关（现状三路 RRF）。
type RetrieverOptions struct {
    // Associative 启用共现图联想信号（Strike 1）。
    Associative bool
    // AssocDepth 游走深度，默认 2；仅 Associative=true 时生效。
    AssocDepth int
    // TemporalScore 启用检索侧时间相关性（Strike 2）。
    TemporalScore bool
    // TemporalTau T_score 衰减常数（秒），默认 30 天。
    TemporalTau float64
    // TemporalHardFilter 命中时间意图时硬过滤窗外候选（默认 false=软乘）。
    TemporalHardFilter bool
    // SupersededPenalty 被取代条目的降权系数 [0,1]，默认 0.3；1=不降权。
    SupersededPenalty float64
    // Now 查询锚定时间（评测注入会话时间；零值=time.Now）。
    Now time.Time
}

// NewRetrieverWithOptions 构造带选项的 Retriever；NewRetriever 保持原签名并
// 等价于零值选项。
func NewRetrieverWithOptions(entries *EntryStore, vectors *VectorStore,
    client embedding.Client, reranker embedding.Reranker, opts RetrieverOptions) *Retriever
```

**行为契约**：
- `Search` 返回语义不变：`[]Result` 按融合分降序；新信号只增/调 RRF 输入路。
- 任一新信号内部失败（SQL 错、解析失败）→ 该信号返回空并记日志，**不返回 error**
  （宪法 V：独立降级）。
- `Associative` 产出的候选必须经原 query 重排后进 RRF（FR-006 防 topic drift）。
- superseded 条目：默认乘 `SupersededPenalty`；查询带时间意图时不惩罚。

## 2. 图访问器（memory/graph.go 新增）

```go
// EdgeStore 读写实体边；all methods 内嵌于 EntryStore（同一 *sql.DB）。
func (s *EntryStore) UpsertEdges(ctx context.Context, pairs []EntityEdge) error
func (s *EntryStore) NeighborsOf(ctx context.Context, entities []string, kinds []string) ([]EntityEdge, error)
func (s *EntryStore) EntityDocFreq(ctx context.Context) (map[string]int, error)

type EntityEdge struct {
    A, B   string  // EntityNorm 归一化，A < B
    Kind   string  // "co" | "syn"
    Weight float64
}
```

## 3. 时间解析（memory/temporal.go 新增）

```go
// ParseTemporalIntent 从 query 提取时间窗；anchor 为提问时刻锚点。
// 无时间意图返回 ok=false。纯规则实现，无 LLM/网络调用。
func ParseTemporalIntent(query string, anchor time.Time) (win TimeWindow, ok bool)

type TimeWindow struct{ Start, End time.Time }
```

## 4. Entry 与写入（memory/entrystore.go）

```go
type Entry struct {
    // ... 既有字段不变 ...
    EventStart   *time.Time // nil=无事件时间
    EventEnd     *time.Time
    SupersededBy string     // ""=未被取代
}

// Supersede 非破坏压制：old.superseded_by = newName；校验存在性/非自指/非 pinned。
func (s *EntryStore) Supersede(ctx context.Context, oldName, newName string) error
// Unsupersede 误判回退。
func (s *EntryStore) Unsupersede(ctx context.Context, name string) error
// PutAliases 写事件别名（随抽取落库）。
func (s *EntryStore) PutAliases(ctx context.Context, name string, aliases []string) error
```

## 5. 抽取输出扩展（memory/pipeline + memory/prompt）

抽取 JSON 每条 fact 新增可选字段（缺省不破坏旧 prompt 解析）：

```json
{
  "content": "...", "entities": ["..."],
  "event_start": "2023-05-07", "event_end": "2023-05-07",
  "aliases": ["got a step counter", "bought a fitness tracker"]
}
```

同一次抽取调用顺带产出（调用数不变，预算门禁）。解析失败的字段按缺省处理。

## 6. Curation 四分类（memory/curation/judge.go）

```go
// JudgeDecision 扩展；旧字段语义不变。
type JudgeDecision struct {
    Evict []string
    Merge []MergeDecision
    // Conflicts 新增：Contradictory 对 → 非破坏压制。
    Conflicts []ConflictDecision
}
type ConflictDecision struct {
    Loser, Winner string // Loser 被 Winner 压制（写 superseded_by）
}
```

- judge prompt 输出四分类；`Worker.apply` 顺序：merge → conflicts(Supersede) → evict。
- Subsumes/Subsumed 映射到既有 Merge 路径，不新增 apply 分支。
- 解析容错：无 `conflicts` 字段的旧格式输出照常工作。

## 7. 测试契约

- `TestRetrievalParity`：零值选项下逐字节等价现基线（保真门禁）。
- `TestSignalDegradation`：扩展覆盖降级矩阵（data-model §5）全行。
- 新增 `TestAssociativeNoRegression`：构造单跳 fixture，开 Associative 后原 top-1 不变。
- 新增 `TestSupersedeLifecycle`：压制/回退/pinned 保护/时间查询不过滤。
- 全部离线可跑（宪法 I、工作流门禁）。
