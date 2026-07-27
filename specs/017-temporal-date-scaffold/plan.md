# Implementation Plan: 确定性日期脚手架(TIMELINE 块)

**Branch**: `017-temporal-date-scaffold` | **Date**: 2026-07-27 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/017-temporal-date-scaffold/spec.md`

## Summary

把 temporal 题答题上下文里的**日期机械劳动**从答题模型手上拿走,交给确定性代码:
扫描本题要喂给模型的检索记忆,读出各自的结构化事件日期,按时间升序排序编号,
把记忆正文里的相对时间表达按该条自己的日期锚解析成绝对日期,再预先算好候选之间的
时间跨度,组装成一个 `TIMELINE` 块附在答题上下文中。模型只需**选**,不需要**算**。

技术路径的核心发现(见 [research.md](./research.md)):`memory.Result.EventDate` 已经是
**结构化 `*time.Time`**,不是需要正则抽取的文本——脚手架的"日期抽取"退化为读字段 + 排序,
不确定性来源只剩"相对表达解析"一处,且该处失败时静默降级。这让 FR-002/FR-003/FR-005
的正确性几乎可以由类型系统和单测完全钉死。

变更范围纯 `cmd/locomo-bench`(adapter),引擎零改。开关默认关,关时逐字节不变。

## Technical Context

**Language/Version**: Go 1.25.0,`CGO_ENABLED=0` 硬门(纯 Go,可交叉编译)

**Primary Dependencies**: 仅标准库(`strings` / `sort` / `time` / `fmt`)。**不新增任何第三方依赖**
——脚手架是纯字符串与日期算术,引入 NLP/日期解析库既违背依赖最小化,也会把确定性交给外部实现。

**Storage**: N/A。脚手架只读**已经检索出来的**结果,不查库、不写库、不碰 schema。

**Testing**: `go test`,离线确定性单测(表驱动,零网络/零 box/零 LLM);
e2e 门用 `cmd/locomo-bench` 全本地栈(US2,需授权)。

**Target Platform**: 开发与单测在 WSL2 Linux;e2e 门在租用 GPU box(vllm,OpenAI 兼容)。

**Project Type**: 评测 harness 的 adapter 增量(CLI 二进制 `cmd/locomo-bench`),非引擎能力。

**Performance Goals**: 脚手架对每题 O(n log n),n = top-k(canonical recipe 为 30)。
相对单次 LLM 答题调用可忽略不计;不设专门性能指标。

**Constraints**:
- 引擎目录零改(`git diff --name-only -- memory embedding provider store internal` 必须为空);
- **完全确定性**:不得使用随机数、`time.Now()`、map 迭代顺序或任何外部状态;
- 开关关闭 / 非 temporal 类 → 答题上下文**逐字节**不变;
- 不臆造日期:无锚、无日期、粒度不足时一律降级而非补值。

**Scale/Scope**: 每题 ≤30 条记忆;LoCoMo 全量 1540 题 × 3 rep;temporal 类 n=321。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| **I. 本地优先,默认离线** | ✅ PASS | 脚手架是纯本地确定性代码,零网络、零模型调用。US1 全部验收可在断网环境完成。它**降低**而非增加对外部能力的依赖(把日期算术从 LLM 手里拿回代码)。 |
| **II. 引擎/适配层分离** | ✅ PASS | 改动全部在 `cmd/locomo-bench`。答题步本就是 host 侧职责,不是引擎能力——引擎只负责存储/检索/抽取。**不需要引擎新增任何公开入口**:所需的事件日期已由 `memory.Result.EventDate` 提供(既有公开字段)。硬验证:`git diff --name-only -- memory embedding provider store internal` 为空。 |
| **III. 契约优先与命名空间隔离** | ✅ PASS | 契约先冻结于 [contracts/](./contracts/)(脚手架块的文本格式 + 构造函数签名 + 开关语义 + fingerprint 标记)再实现。命名空间不涉及(评测 harness 单库单跑)。 |
| **IV. 评测回归门禁** | ⚠️ **PASS,但强门控** | 本 feature **触及答题上下文**,故 US2 的端到端门是**强制**的,且 GO 判据写死为「temporal 类配对显著抬升 **且** overall 不回退」。三条附加纪律已进 FR:必须有同配置重跑臂作噪声标尺(FR-012)、禁止对冷启动首臂做配对检验(FR-012)、必须实测 token 增量(FR-013)。若 GO,eval 结果与实现改动**分开提交**(FR-016)。**开关默认关**意味着未过门之前 canonical recipe 不受任何影响。 |
| **V. 优雅降级与规模诚实** | ✅ PASS | 降级是本设计的核心不变量,逐条落到 FR-002/FR-004 与 Edge Cases:无日期→跳过该条但不丢弃记忆;无锚→不解析;粒度不足→降级为粗粒度且不补零;全无日期→整块省略。规模诚实:名义上限 **2.47pp** 已在 SC-007 写死且标注"实际远低",不承诺达到。 |

**门禁结论**:无违背,`Complexity Tracking` 留空。

### 死规则核对(项目级 HARD)

- **不使用任何付费云 reranker / 云 recall 模型**:本 feature 全程零模型调用(除 US2 的答题/判题本身走既有全本地栈 + 小额 judge),不引入任何新的模型依赖。✅
- **signal not volume**:TIMELINE **不新增检索条目、不提高 top-k、不扩候选池**,只重组已在上下文中的信息。但它**确实增加 prompt token**,故 FR-013 强制实测该增量——使"这是提质还是变相加量"成为**可被数据回答**的问题,而不是靠声称。✅

## Project Structure

### Documentation (this feature)

```text
specs/017-temporal-date-scaffold/
├── plan.md              # 本文件
├── research.md          # Phase 0:技术决策与被否方案
├── data-model.md        # Phase 1:实体与不变量
├── quickstart.md        # Phase 1:验证怎么跑
├── contracts/
│   └── scaffold-contract.md   # Phase 1:冻结的对外契约
├── checklists/
│   └── requirements.md  # /speckit-specify 产出
└── tasks.md             # Phase 2(/speckit-tasks,本命令不产出)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── timeline.go            # 新增:脚手架全部逻辑(纯函数,无 I/O)
│                          #   buildTimelineBlock / 日期规范化 / 相对表达解析 / 跨度计算
├── timeline_test.go       # 新增:表驱动确定性单测(离线,零 LLM)
├── runner.go              # 改:buildAnswerPrompt / buildSweepAnswerPrompt / buildAnswerContextPrompt
│                          #   接受 scaffold 文本;为空时逐字节不变
├── main.go                # 改:新增 --temporal-date-scaffold flag;
│                          #   answerRegimeFingerprint 追加标记;答题调用点传 category
└── bench_test.go          # 改:补「开关关闭 / 非 temporal 类逐字节不变」回归断言

# 引擎目录 memory/ embedding/ provider/ store/ internal/ —— 零改动(硬门)
```

**Structure Decision**: 脚手架逻辑独立成 `timeline.go` 而非塞进已 1700+ 行的 `main.go` 或
已承载全部 prompt 常量的 `runner.go`。理由:(1) 它是**纯函数集合**,无 I/O、无 options 依赖,
独立文件让"确定性"这一核心属性在文件级别可见且易于单测;(2) 与既有 prompt 常量物理分离,
降低误改 canonical 路径的风险;(3) 若 US2 判 NO-GO,整个文件可原子删除,回滚面清晰。

## Constitution Re-Check(Phase 1 设计完成后)

设计产出 [research.md](./research.md) / [data-model.md](./data-model.md) /
[contracts/scaffold-contract.md](./contracts/scaffold-contract.md) / [quickstart.md](./quickstart.md)
后重新逐条核对,**五项全部仍 PASS**,且有两处比 Phase 0 时更强:

| 原则 | 复核结论 | 设计带来的变化 |
|---|---|---|
| I. 本地优先 | ✅ 更强 | research D-3 否掉了"用 LLM 做相对表达归一化",脚手架确定为**全程零模型调用** |
| II. 引擎分离 | ✅ 更强 | research D-1 证实 `memory.Result.EventDate` 已是结构化 `*time.Time`,**不需要引擎新增任何字段**;T-2 设想的"引擎暴露 range"被明确否掉 |
| III. 契约优先 | ✅ PASS | 四个对外面(CLI 开关 / 块文本格式 / 函数签名 / fingerprint)已在 contracts 冻结,含 17 条**必须先红**的契约测试 |
| IV. 评测门禁 | ✅ PASS | 三臂设计(含 `ref` 噪声标尺)、五项必产数字、冷启动纪律已落 quickstart;开关默认关,过门前 canonical recipe 零影响 |
| V. 降级与诚实 | ✅ PASS | 降级不变量形式化为 data-model 的 I-1~I-10;I-4(精度不得高于较粗端点)把"不补零假装精确"变成可断言的规则 |

新增依赖:**无**(仅标准库)。新增持久化实体:**无**。
`Complexity Tracking` 仍留空。

## Complexity Tracking

> Constitution Check 与 Re-Check 均无违背,本节留空。
