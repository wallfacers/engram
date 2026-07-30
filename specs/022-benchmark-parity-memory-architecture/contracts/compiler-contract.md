# Evidence Compiler Contract

**Version**: 022.v1

**Package**: `github.com/wallfacers/engram/memory/evidencecompiler`

## Boundary

Compiler 位于 retrieval 之后、answerer 之前。它：

- 接受调用方已经冻结且有稳定 ID 的 `[]Candidate`；
- 可以按这些 candidate 已声明的 lineage 精确读取 source；
- 生成 Evidence Need、受限 actions、Grounded Trace 和 Evidence Bundle；
- 用实际 answerer integration 提供的 tokenizer 对完整最终 input 计数；
- 不拥有 Retriever，不执行 query/search，不调用 answerer，不知道 benchmark/category。

若需要一次结构化 gap retrieval，由上层 orchestration 在两次 Compile 之间执行；Compiler
本身不破坏 fixed-candidate 实验。

## Core Types

```go
package evidencecompiler

type CandidateKind string

const (
    CandidateChunk           CandidateKind = "chunk"
    CandidateRawTurn         CandidateKind = "raw_turn"
    CandidateSemanticEpisode CandidateKind = "semantic_episode"
    CandidateAtomicFact      CandidateKind = "atomic_fact"
)

type Candidate struct {
    ID         string
    Kind       CandidateKind
    Rank       int
    Score      float64
    Text       string
    TextDigest string
    SourceIDs  []string
    Metadata   map[string]string
}

type Source struct {
    ID              string
    SourceSessionID string
    Speaker         string
    Ordinal         int
    Content         string
    ContentDigest   string
    OccurredAt      *time.Time
}

type SourceSpan struct {
    SourceID  string
    StartChar int
    EndChar   int
    SpanDigest string
}
```

`Candidate.ID`、`TextDigest`、rank 和 source IDs 是 run 内冻结输入。Compiler 不得修改它们。
`Source.Content` 必须来自 active Ledger，合法 UTF-8；offset 是 Unicode code-point
`[StartChar,EndChar)`。

### Evidence Need

```go
type Cardinality struct {
    Known bool
    Count int
}

type Operand struct {
    Name      string
    Satisfied bool
}

type GapKind string

const (
    GapEntity        GapKind = "entity"
    GapTimeRange     GapKind = "time_range"
    GapSecondOperand GapKind = "second_operand"
)

type StructuredGap struct {
    Kind       GapKind
    Entity     string
    Start      *time.Time
    End        *time.Time
    Operand    string
    SourceNeed string
}

type RelationKind string

const (
    RelationBefore          RelationKind = "before"
    RelationAfter           RelationKind = "after"
    RelationConflicts       RelationKind = "conflicts"
    RelationSupportsOperand RelationKind = "supports_operand"
)

type EvidenceRelation struct {
    Kind          RelationKind
    LeftSourceID  string
    RightSourceID string
    Operand       string
}

type EvidenceNeed struct {
    Entities        []string
    TimeConstraints []string
    Operands        []Operand
    ListCardinality Cardinality
    UpdateState     string
    Gap             *StructuredGap
}
```

`Cardinality{Known:false}` 必须被保留为未知，Planner/validator 不能猜数。`Gap` 最多一个；
低置信度、泛化 “need more context” 或自由文本 query 不是有效 gap。

### Closed Action Union

```go
type ActionKind string

const (
    ActionKeep        ActionKind = "KEEP"
    ActionExtract     ActionKind = "EXTRACT"
    ActionDrop        ActionKind = "DROP"
    ActionMerge       ActionKind = "MERGE"
    ActionFetchSource ActionKind = "FETCH_SOURCE"
)

type GroundedSentence struct {
    Text    string
    Sources []SourceSpan
}

type Action struct {
    Kind       ActionKind
    CandidateID string
    SourceID   string
    Span       *SourceSpan
    Sentences  []GroundedSentence
    ReasonCode string
}

type Proposal struct {
    Need    EvidenceNeed
    Actions []Action
}
```

各 kind 的合法字段：

| Kind | 必需 | 禁止/限制 |
|------|------|-----------|
| KEEP | `CandidateID` 或 `SourceID` 恰好一个 | 内容必须是已验证原文；projection synthesis 不能只凭 lineage KEEP |
| EXTRACT | 一个 `Span` | span 必须位于 candidate lineage 且 digest 可复原 |
| DROP | `CandidateID`, `ReasonCode` | 不产生 Bundle item |
| MERGE | 非空 `Sentences` | 仅 raw over-cap 且 EXTRACT 仍不满足 Need 时允许；每句非空且有一个以上有效 span |
| FETCH_SOURCE | `SourceID` | source 必须在 candidate lineage allowlist；只调用 Resolve |

没有 `ADD`。未知 kind、多余互斥字段或空引用使整个 proposal invalid，触发确定性 fallback，
而不是部分采纳模型意图。

## Narrow Interfaces

### Source Resolver

```go
type SourceResolver interface {
    Resolve(
        ctx context.Context,
        sourceIDs []string,
    ) (map[string]Source, error)
}
```

接口故意没有 query、Search 或 list-all 方法。Compiler 构造所有 candidate source IDs 的
allowlist；`Resolve` 请求超出 allowlist 是内部 validation error。Resolver 任一 source
未知、tombstoned、purged、digest 变化或 storage error 时，Compile fail closed，不调用
answerer。

### Optional Planner

```go
type Planner interface {
    Propose(
        ctx context.Context,
        query string,
        candidates []Candidate,
    ) (Proposal, error)
}
```

Planner 只提议 Need/actions，不能写 Bundle、读 Store 或答题。nil、timeout、malformed、
unknown action、invalid span/citation 或越权 source 都记录 fallback reason，改用
deterministic extractive path。`context.Canceled` / `DeadlineExceeded` 由调用方取消时
直接传播，不伪装成功。

### Actual Token Counter

```go
type AnswerInput struct {
    Model  string
    System string
    User   string
}

type TokenCount struct {
    InputTokens int
    Fingerprint string
}

type TokenCounter interface {
    CountInput(
        ctx context.Context,
        input AnswerInput,
    ) (TokenCount, error)
}

type AnswerRenderer interface {
    RenderAnswerInput(
        query string,
        renderedEvidence string,
    ) AnswerInput
}
```

`AnswerRenderer` 必须与正式 answerer 使用同一 system/user prompt 代码路径。`TokenCounter`
必须使用同一 model tokenizer、chat template、special-token policy；fingerprint 至少覆盖：

```text
model ID
model/tokenizer revision
tokenizer vocabulary/merges digest
chat-template digest
special-token policy
answer-prompt digest
```

`TokenCap` 约束完整的 answerer input，而不是仅 evidence 字符串：

```text
CountInput(RenderAnswerInput(query, bundle_context)).InputTokens <= TokenCap
```

同时记录 `EvidenceTokens` 作为描述性指标，但不得用 item token 求和或估算替代硬门。
Compiler 在每次增删 Bundle item 后重渲染完整 prompt 并重计，以支持非加性 chat template。

Counter nil、失败、返回非正 token 或 fingerprint 与 frozen protocol 不同，使 run invalid，
answerer 调用为 0。不得降级为 `strings.Fields`、字符数、固定 tiktoken 或调用后 usage。
基础 `Retriever.Search` 不受此错误影响。

## Compile Request and Output

```go
type Config struct {
    TokenCap             int
    CounterFingerprint   string
    MaxCandidates        int
    MaxSources           int
    Planner              Planner
    Resolver             SourceResolver
    Counter              TokenCounter
    Renderer             AnswerRenderer
}

type CompileRequest struct {
    Query      string
    Candidates []Candidate
}

type BundleItem struct {
    Kind       ActionKind
    Text       string
    Sources    []SourceSpan
    CandidateIDs []string
}

type EvidenceBundle struct {
    Items              []BundleItem
    SourceIDs          []string
    RenderedContext    string
    EvidenceTokens     int
    InputTokens        int
    TokenCap           int
    CounterFingerprint string
    TraceDigest        string
}

type DropRecord struct {
    CandidateID string
    ReasonCode  string
}

type TokenStep struct {
    Operation             string
    ItemID                string
    FullAnswerInputTokens int
    TokenCap              int
}

type GroundedTrace struct {
    Need               EvidenceNeed
    CandidateDigest    string
    CandidateIDs       []string
    CandidateSourceIDs []string
    ProposedActions    []Action
    AppliedActions     []Action
    Relations          []EvidenceRelation
    Dropped            []DropRecord
    TokenSteps         []TokenStep
    FallbackReason     string
    RemainingGap       *StructuredGap
    Valid              bool
}

type Result struct {
    Bundle EvidenceBundle
    Trace  GroundedTrace
}

func New(cfg Config) (*Compiler, error)
func (c *Compiler) Compile(
    ctx context.Context,
    req CompileRequest,
) (Result, error)
```

构造时拒绝 nil Resolver/Counter/Renderer、非正 cap/limit 或空 frozen fingerprint。Planner
可 nil。

## Deterministic Algorithm

### Input Validation

在任何 Planner 调用前：

1. query 非空；candidate 数在上限内。
2. IDs 唯一、rank 正且唯一、按 rank 递增。
3. `TextDigest` 与 bytes 匹配。
4. `SourceIDs` 非空、去重后稳定排序。
5. 批量 Resolve allowlist，并验证 active、digest 和 UTF-8。
6. 对空 evidence context 渲染/计数；若静态 prompt 已超 cap，返回
   `ErrBudgetImpossible`。

### Deterministic Need

Compiler 总是先从 query 构造 deterministic Need，不依赖 benchmark category：

- 保留显式专名、代词已知指代和比较实体；
- 解析 query 内绝对/相对时间表达和 before/after 关系；
- 将比较、因果、集合或多事实问句拆成 operands；
- 只有 query 明示数量时设置 known cardinality，否则保持 unknown；
- 识别 latest/current/previous/change/conflict 等 update-state 要求。

Planner 可以在验证后补充 Need，但不得删除 query 中已经显式解析出的 constraint。Planner
缺失或 proposal invalid 时，Trace/Bundle 使用 deterministic Need。对选中 evidence 的
before/after/conflict/operand 支持关系写入 `Relations`，每个关系两端都必须是 allowlist
内 source；无法证实的关系不写入。

### Raw-fits Rule

Compiler 先尝试按候选 rank 保留所有与 Need 相关的可验证原文。若完整必要 source 原文
能装入 cap：

- 保留原文；
- 不做 EXTRACT 或 MERGE；
- 只 DROP 明确无关候选；
- Trace 记录 `compression_applied=false`。

不能因为 Planner 喜欢摘要而压缩能装下的必要原文。

### Over-cap Rule

只有 raw 必要原文超 cap 才允许：

1. 按 Need coverage、query/source 词法重合、原 rank、source ID 排序；
2. 选择句子或连续 span；
3. 对每个 span 做 code-point/digest 校验；
4. 每次增删后完整重计；
5. 如果最小合法 evidence item 仍装不下，返回 `ErrBudgetImpossible` 和有效 Trace，
   不超 cap、不调用 answerer。

Compiler 必须先完成一轮有来源的 EXTRACT 并重新计算 Need coverage。只有 EXTRACT
仍不能同时满足全部 required entities/time/operands/cardinality/update-state，才可验证
Planner 的 MERGE proposal；否则 MERGE proposal invalid 并 fallback。Deterministic
fallback 从不生成 MERGE。

### Partial MERGE

MERGE 每句独立验证。若 proposal 内任一句 invalid，则 proposal 整体 invalid 并 fallback；
不能仅删除坏句后把同一模型 proposal 当成有效 treatment。Trace 可保留逐句失败原因。

### Output Validation

成功 Result 必须同时满足：

- Candidate digest 与输入一致；
- 每个 Bundle item 至少一个有效 source span；
- Source IDs 全在冻结 lineage；
- EXTRACT/MERGE span 复原率 100%；
- `RenderedContext` 由 Items 确定性渲染；
- Counter fingerprint 等于 frozen value；
- 完整 `InputTokens <= TokenCap`；
- Trace digest 与 canonical Trace bytes 匹配；
- 没有 ADD/unknown action。

## One-answer Orchestration

Compiler 不调用 answerer。上层使用以下顺序：

```text
retrieve once
  -> compile
  -> optional one structured-gap retrieval
  -> compile final union
  -> validate Bundle + prompt digest again
  -> call answerer exactly once
```

固定候选 Compiler 实验停在第一次 Compile，`retrieval_calls_after_freeze=0`。

允许 gap 的产品/实验路径：

- 第一次 Trace 的 `RemainingGap` 必须是三种合法 kind 之一；
- 上层最多发起一次，且补检 query 由结构化字段确定性渲染；
- 累计候选不超过预注册上限；control 用首轮 `N` candidates，treatment 用首轮 `N-r`
  加最多 `r` supplemental，union 上限仍为 `N`；
- 两轮共享同一个 TokenCap/CounterFingerprint；
- 第二次 Compile 后即停止，不允许低置信度 retry；
- answerer 仍只在最终 Bundle 后调用一次，legacy IDK retry 必须关闭。

## Error and Degradation Contract

```go
var (
    ErrInvalidCandidate   = errors.New("evidencecompiler: invalid candidate")
    ErrSourceUnavailable  = errors.New("evidencecompiler: source unavailable")
    ErrInvalidAction      = errors.New("evidencecompiler: invalid action")
    ErrInvalidSpan        = errors.New("evidencecompiler: invalid source span")
    ErrCounterUnavailable = errors.New("evidencecompiler: token counter unavailable")
    ErrFingerprintMismatch = errors.New("evidencecompiler: counter fingerprint mismatch")
    ErrBudgetImpossible   = errors.New("evidencecompiler: evidence cannot fit token cap")
)
```

| Failure | Result |
|---------|--------|
| Planner nil/failure/invalid proposal | deterministic fallback；Trace 标原因 |
| Optional Episode/Event/Scene/Profile missing | 调用方省略该 candidate kind；基础 Compiler 可继续 |
| Resolver/storage/source integrity failure | Compile error；answerer 0 |
| Counter/fingerprint failure | invalid run；answerer 0 |
| Budget impossible | validated Trace + typed error；answerer 0 |
| Caller cancellation | propagate；answerer 0 |

不能把 source/counter 错误标为 graceful Planner fallback，因为那会伪造 provenance 或预算。

## Counter Calibration Contract

正式协议冻结前，用同一 local answerer runtime 跑至少包含以下项的 calibration fixture：

- 中文、日文、英文混合；
- emoji 与组合 code points；
- 长整数、小数、ISO/RFC3339 日期；
- 空 evidence、单/多 source；
- system/user role 边界、换行、JSON/XML-like delimiter；
- 刚好 cap、cap-1、cap+1。

对每个 fixture：

```text
preflight CountInput == runtime reported input usage
fingerprint == protocol counter fingerprint
absolute delta == 0
```

任一不等则正式 run 不启动。校准产物记录内容 digest 和 counts，不提交 secret。

## Required Tests

### Unit

- action union 字段矩阵、unknown/ADD 拒绝；
- 中英文/emoji code-point span 与 digest；
- candidate/source allowlist、tombstone/purge/missing；
- raw fits 不压缩、over-cap 才 EXTRACT；
- raw over-cap 但 EXTRACT 已充分时拒绝 MERGE，EXTRACT 不充分时才验证逐句 citation；
- nil/invalid Planner fallback 确定性；
- non-additive fake tokenizer 证明每步完整重计；
- fingerprint drift、counter error、static prompt over cap；
- 同输入多次输出 Trace/Bundle bytes 一致。

### Contract/Integration

- Compiler type 不引用 Retriever/answerer/benchmark package；
- mock Resolver 断言只收到 frozen lineage IDs；
- fixed-candidate 各 arm Candidate digest 100% 一致；
- answer orchestrator 成功/失败路径调用 answerer 分别为 1/0；
- gap retrieval 最大 1，无 gap 时 0；
- actual local runtime calibration delta 0；
- planner unavailable 时基础 Search/write parity 仍绿。
