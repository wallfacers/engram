# Phase 1 Data Model: 答题侧时序推理契约

本 feature 无持久化数据/schema。实体是**评测与契约域**的逻辑对象。

## Entity: 时序推理契约 (temporal answer contract)

- **表示**:`cmd/locomo-bench/runner.go` 内 `forceTemporalAnswerPrompt` 常量(一段答题系统提示字符串)。
- **绑定路径**:`answerPromptForRegime(category=2, forceAnswer=true, temporalAnswer=true, abstain=false)`。
- **组成字段**(不变式,US1 单测断言其在位):
  - `ENUMERATE`:判定前逐候选列 `[event:]` 日期。
  - `RELATIVE→ABSOLUTE`:相对短语以记忆自身 `[event:]` 为锚解析成绝对,不回显相对短语。
  - `EXACT MATCH`:锁问题时间约束精确匹配,±1 邻近事件不算对。
  - `DURATION ARITHMETIC`:时长问题认定 START/END 端点相减输出 duration。
  - 终局约束:绝对日期自然语言(非 ISO)、最短短语、不复述、force 下永不拒答。
- **不变量**(US1 单测):category≠2 路径、`temporalAnswer=false`、`abstain=true` 三者行为逐字节不变(契约不越权,abstain 优先)。

## Entity: e2e 臂 (arm)

以 category-2 答题契约区分的评测配置,共三个:

| 臂 | flag 组合 | cat-2 契约 | 角色 |
|---|---|---|---|
| `base` | 无 `--temporal-answer-prompt` | `forceAnswerSystemPrompt` | 干净基线(需复跑锚,避冷启动) |
| `old-tplan` | `--temporal-answer-prompt` + 旧常量 | 旧弱契约 | 归因锚 |
| `new-tplan` | `--temporal-answer-prompt` + 新常量 | 强化契约 | 处理臂 |

- **公共配置**:canonical recipe(`--chunks --chunk-quota 12 --force-answer --judge-mem0-aligned --retrieval hybrid --top-k 30`)+ `--cat-top-k 1=150` + `--repeats 3`,box 全本地栈,固化 store `009-bge-chunks-store`。
- **产物**:每臂 category-2(n=321)逐题正误 + overall。

## Entity: 配对判据 (paired verdict)

- **输入**:三臂逐题正误(3 rep 多数投票)。
- **计算**:
  - `new-tplan` vs `base` 配对 McNemar → SC-001(GO 需显著抬 + overall 不回退)。
  - `new-tplan` vs `old-tplan` 差分 → SC-004 归因。
- **输出**:GO / NO-GO + 归因声明,落 `docs/locomo-score-levers.md`。
- **纪律**:配对只对干净复跑基线(冷启动首臂丢弃);跑完核 `regime.json` 四要素。

## 关系

契约(1)绑定路由(1);臂(3)各引用一个契约变体;判据(1)消费三臂产物,产出 verdict(1)。无跨命名空间、无存储、无引擎实体。
