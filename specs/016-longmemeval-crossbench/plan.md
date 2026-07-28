# Implementation Plan: LongMemEval 子集先行 · 跨 benchmark 复现 coverage≠answer

**Branch**: `016-longmemeval-crossbench` | **Date**: 2026-07-26 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/016-longmemeval-crossbench/spec.md`

**上游事实源**: [归档设计](../../docs/archive/designs/2026-07-26-longmemeval-subset-design.md)（brainstorming 逐段确认）

## Summary

补齐评测工具读取 LongMemEval 真实数据集的四处缺口（题型名、逐会话日期、证据标记、
无证据题处置），使该 benchmark 的证据形式与 LoCoMo **同构**，从而让既有的覆盖计量与
归因追踪逻辑**原样复用**；随后跑 ORACLE（完美证据，全 500）与 S（真实检索，分层
抽样 100）两臂，按逐题证据覆盖率分桶做检索侧/答题侧分账，对照**测量前固化**的判据
给出「复现 / 证伪 / 无法判定」。

目标是填论文 RQ6 的空白，**不追分**；证伪与复现同等有价值。

技术路径：纯 adapter 改动（只动 `cmd/locomo-bench/`），核心是给每条消息合成
`DiaID = "D<会话序>:<消息序>"` —— 该格式被 `^D(\d+):(\d+)$` 锚定，一旦同构，
`evidenceRecallAt` / `chunkTurns` / `buildAttributionTrace` / 会话级召回全部自动成立，
**无需修改 `coverage.go`、`attribution.go`、`evidence.go` 中的任何一行**。

**执行纪律**：必须先过 G-尺子门（oracle 30 题覆盖 ≥ 0.95，零答题/判分调用），
不过即判死；每次建库后必须过 G-向量门。

## Technical Context

**Language/Version**: Go 1.25.0

**Primary Dependencies**: 标准库。**本特性不引入任何新的第三方依赖**。

**Storage**: 不涉及 schema 变更 —— **无 migration**，不动 `store/`。评测产生的每题
一个 SQLite 库沿用既有 `conv%d.db` 结构（ORACLE 500 个、S 100 个，两臂分目录）。

**Testing**: `go test`，全部离线、零模型调用。新增手写夹具（真实数组形状 +
`haystack_dates` + `has_answer`），不拷贝数据集内容。

**Target Platform**: 跨平台纯 Go（**CGO_ENABLED=0 硬门禁**）。

**Project Type**: 单体 Go 库（引擎）+ 评测工具。本特性**只动评测工具**。

**Performance Goals**: 无。评测是离线批处理，不在任何请求热路径上。

**Constraints**:
- 引擎零改动（宪法 II）—— `git diff --name-only -- memory embedding provider store internal` 必须为空
- LoCoMo 路径零行为变更 —— 唯一触及的共享符号是 `categoryLabel(12)`，而 12 只属
  LongMemEval 题型（LoCoMo 用 1–5）
- 覆盖/归因逻辑零改动 —— 靠证据同构而非改计量代码
- 判据测量前固化，事后不得调整（FR-019 / SC-008）

**Scale/Scope**: ORACLE 全 500 题（10,960 条消息，≈1.86× LoCoMo 全量）；
S 分层抽样 100 题（≈49,350 条消息，≈8.4× LoCoMo）。LoCoMo 基准实测为
272 session / 5,882 turn。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 门禁判定 | 依据 |
|---|---|---|
| **I. 本地优先，默认离线** | ✅ PASS | 纯评测工具改动，引擎的离线能力与逐信号降级语义不受影响。P0 完全离线；P1 的覆盖诊断零答题/判分调用 |
| **II. 引擎与适配层分离** | ✅ PASS | 改动严格限于 `cmd/locomo-bench/`。research R4/R5 明确**拒绝**为一次性评测新增命令行开关或在 bench 内塞入运行纪律断言，正是为了不让适配层表面因宿主特定需求而膨胀 |
| **III. 契约优先与命名空间隔离** | ✅ PASS | 契约先冻结于 [contracts/loader-contract.md](./contracts/loader-contract.md)。仅**新增** struct 字段与题型条目，不改任何既有导出符号的签名或语义；无 schema 变更 |
| **IV. 评测回归门禁（不可协商）** | ✅ PASS | 本特性**不触碰**检索/抽取/策展/存储/嵌入 ⇒ LoCoMo 基线 **invariant by construction**，以「引擎零 diff + 全量测试全绿 + LoCoMo 路径零行为变更」证明，**不重跑 LoCoMo**。LongMemEval 首跑声明为**独立新基线**，不替代 LoCoMo 85.71% |
| **V. 优雅降级与规模诚实** | ✅ PASS | 报告明示 LongMemEval-S (cleaned) / oracle 与子集规模，不简称全量；官方废弃旧版本的事实如实记录；样本不足 20 的桶标记「不可判」而非硬报点估计 |

**技术约束核对**：无 CGO ✅（零新依赖）；依赖最小化 ✅；单一存储真相 ✅（不建平行副本）。

**Post-Phase-1 复查**: 设计产出（research / data-model / contracts / quickstart）后
重新核对上表，**结论不变，无新增违规**。Complexity Tracking 为空。

## Project Structure

### Documentation (this feature)

```text
specs/016-longmemeval-crossbench/
├── plan.md              # 本文件
├── spec.md              # 需求（26 FR / 10 SC / 3 用户故事）
├── research.md          # Phase 0：R1–R10，含一处对自身初稿的事实修正
├── data-model.md        # Phase 1：解析结构增量 + 转换规则 + 产物结构 + 门禁流转
├── quickstart.md        # Phase 1：P0→P4 执行手册
├── contracts/
│   └── loader-contract.md   # Phase 1：读取/计量/门禁契约 + 零改动承诺
├── checklists/
│   └── requirements.md  # spec 质量清单（已通过）
└── tasks.md             # Phase 2 输出（由 speckit-tasks 生成，非本命令）
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── longmemeval.go              # 【改】loader：题型名 / haystack_dates / has_answer / DiaID 合成
├── longmemeval_test.go         # 【改】保留既有对象形式用例，新增真实形状用例
├── dataset.go                  # 【仅一行】categoryLabel(12) 标签改名（research R8）—— 初稿误写为 main.go
├── coverage.go                 # 【不改】证据同构后原样复用
├── attribution.go              # 【不改】
├── evidence.go                 # 【不改】DiaID 格式反过来被它约束
└── chunks.go                   # 【不改】SourceSessionID 已是 conv%d-sess%d

testdata/longmemeval/
├── sample.json                 # 【不改】对象形式夹具，覆盖既有分支
└── sample_array.json           # 【新增】手写真实形状夹具（数组套数组 + dates + has_answer）

memory/ embedding/ provider/ store/ internal/    # 【一行不动】
```

**Structure Decision**: 改动集中在 `longmemeval.go` 单文件，加一处 `dataset.go` 的标签
改名与一份新夹具。选此结构的理由：本特性的全部难点是**让新 benchmark 的证据形式与
既有 benchmark 同构**，一旦同构成立，计量与归因是纯复用。任何「为 LongMemEval 另写
一套覆盖计量」的方案都会产生两把不可比的尺子，直接摧毁本特性的目的（跨 benchmark
比较自身结论）。

## Phase 0 摘要（详见 research.md）

| 编号 | 结论 |
|---|---|
| **R1** | `evidenceReferencePattern` 是锚定完全匹配，不匹配项被**静默丢弃**而非报错 ⇒ DiaID 格式无自由度 |
| **R2** | 会话级召回**无需新代码** —— 摄入侧 `conv%d-sess%d` 与计量侧 `sess(\d+)` 已同源 |
| **R3** | 实测 500 题：三数组等长、含证据会话全部 ∈ `answer_session_ids`，违例均为 0 ⇒ 长度校验是**护栏**不是补丁 |
| **R4** | 分层抽样用**一次性脚本产出子集文件**，不新增 bench 开关（既有筛选只支持单题型/取前 N，无法分层；子集文件本身即最强可复现产物） |
| **R5** | 向量完整性门用**独立脚本**，不进 bench（bench 内硬断言会把 `--retrieval fts` 这条本就无向量的合法路径变成错误） |
| **R6** | 时间锚语义分歧（`max(session date)` vs `question_date`）本次**不受影响** —— canonical 配方不含任何时间机制；记录为未来裁定项 |
| **R7** | 每题一库沿用既有结构，500 库无压力；两臂**必须分目录** |
| **R8** | `single-session-preference` **复用 id 12**，并同步把 `categoryLabel(12)` 改名 —— 否则答题侧与覆盖侧两份产物对同一批题用不同名字 |
| **R9** | 既有测试断言的是**现实中不存在的 schema**（对象形式），这正是 G2 长期未被发现的原因；保留旧夹具，新增真实形状夹具 |
| **R10** | `has_answer` / `haystack_dates` / `haystack_session_ids` 三字段当前**完全不存在**于解析结构，是纯新增 |

**过程记录**：R8 初稿曾断言「报告分组按类别名字符串」，实读代码后发现**答题侧按类别号
分组、覆盖侧按名字符串分组**，两套并存，已就地修正并补上 `categoryLabel` 改名的裁定。
此类「先断言机制再验证」的自查是本特性的既定纪律。

**未解决项**：无。

## Complexity Tracking

> 仅在 Constitution Check 有需要证成的违规时填写。

**本特性无宪法违规，本节为空。**
