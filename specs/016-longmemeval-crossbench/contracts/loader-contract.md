# 契约：LongMemEval 读取与计量

**日期**: 2026-07-26 | **稳定性**: 契约优先（宪法 III）—— 本文件先于实现冻结。

这是一个评测工具（adapter）特性，没有网络 API。「契约」即本次改动对外的**行为承诺**，
以及对既有表面的**零改动**承诺。

## 0. 契约级承诺

| 承诺 | 内容 |
|---|---|
| **引擎零改动** | MUST NOT 修改 `memory/`、`embedding/`、`provider/`、`store/`、`internal/` 下任何文件。验证：`git diff --name-only -- memory embedding provider store internal` 为空 |
| **LoCoMo 路径零行为变更** | 对 `--dataset-format locomo` 的任何一次运行，本次改动 MUST NOT 改变其结果。唯一触及的共享符号是 `categoryLabel(12)`，而 12 只属 LongMemEval 题型（LoCoMo 用 1–5） |
| **覆盖/归因逻辑零改动** | MUST NOT 修改 `coverage.go`、`attribution.go`、`evidence.go`。LongMemEval 的证据经 DiaID 合成后与 LoCoMo 同形，下游原样复用 |
| **无 schema 变更** | 不新增 migration，不改任何表 |

## 1. 读取契约

```go
// loadLongMemEval 读取 LongMemEval 数据集。
//
// 行为承诺:
//  1. 支持全部 6 种 cleaned 版题型, 含 single-session-preference。
//  2. 每个会话被赋予 haystack_dates 中同下标的日期。
//  3. len(haystack_dates) != len(haystack_sessions) 时返回错误 —— 绝不回落到
//     question_date 或任何默认值。
//  4. 每条写入的消息获得形如 "D<会话序>:<消息序>" 的轮次标识, 两序号均从 1 起,
//     且严格匹配 ^D(\d+):(\d+)$。
//  5. has_answer 为 true 的消息、且仅这些消息, 其轮次标识进入该题 Evidence。
//  6. 无 has_answer 的题, Evidence 为空 —— 下游据此判定不可评分。
//  7. 未知题型返回错误(既有行为, 不改为静默跳过)。
func loadLongMemEval(path string) ([]longMemEvalItem, error)
```

## 2. 计量契约（不改代码，仅声明依赖）

| 既有函数 | 本次依赖的性质 |
|---|---|
| `evidenceReferences` | 仅接受 `^D(\d+):(\d+)$`；不匹配项被**静默丢弃** ⇒ 合成格式无自由度 |
| `parsedGoldTurns` | 以 `D%d:%d` 重新格式化 ⇒ 合成值须与之逐字一致 |
| `evidenceSessions` | 从证据标识取会话号 ⇒ 会话级黄金集合自动得到 |
| `sourceSessionNumber` | 从 `conv<N>-sess<M>` 取会话号 ⇒ 与 `session.Index` 同源 |
| `evidenceRecallAt` | 精确轮次召回**只认 chunk 的轮次血缘**，fact 不计 ⇒ LongMemEval 与 LoCoMo 同尺 |

## 3. 门禁契约

| 门 | 判据 | 载体 | 失败后果 |
|---|---|---|---|
| **G-尺子** | oracle 30 题精确证据覆盖 ≥ 0.95，且答题/判分调用数均为 0 | 既有 `--coverage-only` | 本特性作废 |
| **G-向量** | 每库 `count(memory_embeddings WHERE model=<当前>) == 应有行数` | 独立只读脚本 | 补齐后重测，不得继续 |

**G-向量 不进 bench 的理由**（research R5）：bench 内硬断言会把 `--retrieval fts`
这条本就无向量的合法路径变成错误。

## 4. 不在契约内（明确排除）

- 官方分题型判分口径
- 弃答（abstention）子集
- `longmemeval_m` 与 LongMemEval-V2
- 强制注入黄金证据的干预臂
- 任何新增命令行开关（分层抽样与向量校验均由一次性脚本承担，见 research R4/R5）
- 时间机制在 LongMemEval 下的 "now" 语义（research R6，已记录为未来裁定项）
