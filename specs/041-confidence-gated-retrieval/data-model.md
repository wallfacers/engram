# Data Model: Confidence-Gated Iterative Retrieval（041 Phase 1）

本 feature 不新增持久化存储（无新表/无新库）。所有实体均为**运行时结构体**或 **run-dir 审计 JSONL**。引擎（`memory/`）零改动。

## Entities

### 1. 犹豫信号 `HesitationSignal`

从单次 answerer 生成（thinking + 最终答案）确定性提取的信号，是迭代决策的唯一输入。

| 字段 | 类型 | 说明 |
|---|---|---|
| `Decision` | `enum {Confident, Hesitant}` | 判定：自信 / 犹豫 |
| `Score` | `float64` | 犹豫强度分（规则加权和；阈值 `ConfidenceThreshold` 之上的 → `Hesitant`） |
| `Hits` | `[]signalHit` | 命中的具体信号（信号名 + 权重 + 片段），用于审计与阈值调试 |

`signalHit{Signal string, Weight int, Snippet string}`。

**确定性契约（FR-002）**：同一 predicted 文本 → 完全相同的 `Decision` + `Score`。无随机、无模型调用。

### 2. 检索预算阶梯 `BudgetLadder`

定义「读到多少」的档位。两轮（Decision 3）。

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `ShallowTopK` | `int` | 30 | 第一轮检索深度（= 现有 `--top-k` 默认） |
| `DeepTopK` | `int` | 150 | 第二轮检索深度（= 91.10% 高分线） |
| `ChunkQuota` | `int` | 复用 `--chunk-quota` | 传给 `retrieveWithQuotaDiagnostics` 的 quota |

### 3. 迭代决策记录 `IterationDecisionRecord`

单题的迭代审计快照，落 run-dir `conf_gate_decisions.jsonl`（每行一条，JSON）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `QuestionID` | `string` | 题 ID |
| `Question` | `string` | 题面 |
| `ShallowHits` | `int` | 第一轮检索命中数 |
| `ShallowAnswer` | `string` | 第一轮 answerer 生成（含 thinking，完整保留供审计） |
| `ShallowSignal` | `HesitationSignal` | 第一轮犹豫信号 |
| `DeepHits` | `int` | 第二轮检索命中数（若加深；未加深 = 0） |
| `DeepAnswer` | `string` | 第二轮 answerer 生成（若加深；未加深 = ""） |
| `DeepSignal` | `HesitationSignal` | 第二轮犹豫信号（若加深） |
| `FinalAnswer` | `string` | 最终采用答案（提取 thinking 后的 final，供 judge） |
| `FinalFromRound` | `int` | 1 或 2（最终答案来源轮次） |
| `Deepened` | `bool` | 是否触发加深 |

**用途**：① 复现审计（FR-008 可归因）；② US3 阈值校准的标注集；③ 平均预算/加深率统计。

### 4. 校准阈值 `CalibrationThreshold`

把犹豫强度 `Score` 映射为「加深/停」的标定参数。

| 字段 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `ConfidenceThreshold` | `float64` | 见 contract（CLI flag，初版经验值，US1 诊断后用真实数据校准） | `Score >= Threshold` → 加深 |
| `MaxRounds` | `int` | `2` | 迭代轮数上限（FR-004，超限即停） |

## Relationships

```
CalibrationThreshold.ConfidenceThreshold
        └── 决定 HesitationSignal.Score → Decision（加深/停）
BudgetLadder.ShallowTopK/DeepTopK
        └── 决定每题两轮的检索深度
IterationDecisionRecord（每题一条，run-dir audit）
        └── 记录 HesitationSignal × 2（浅/深轮）+ BudgetLadder 实际消耗
```

## 无新持久化（宪法「单一存储真相」）

- 迭代不写 `memory/`、不新增 SQLite 表、不改 schema。
- 审计仅落 run-dir JSONL（`conf_gate_decisions.jsonl`，随 run-dir 一起 gitignore），与 `results-hybrid.jsonl` 平行。
- 评测输入输出仍走现有 `locomoQA` / `results-hybrid.jsonl` 契约。
