# Contract: 确定性日期脚手架

**Feature**: 017-temporal-date-scaffold | **Date**: 2026-07-27 | **Status**: 冻结(实现前)

宪法 III 要求契约先于实现冻结。本文定义**四个对外面**:CLI 开关、脚手架块文本格式、
构造函数签名、口径指纹。实现 MUST 匹配本文;若实现期发现契约有误,
**先改本文再改代码**,不得反向漂移。

---

## C-1:CLI 开关

```
--temporal-date-scaffold    (bool, 默认 false)
```

| 项 | 约定 |
|---|---|
| 默认值 | **false**(默认关 —— 宪法 I/IV:未过 e2e 门前不影响 canonical recipe) |
| 作用域 | 仅 LoCoMo category 2(temporal);其他类别**逐字节无影响** |
| 与 `--temporal-answer-prompt` 的关系 | **正交,可独立开关**。前者改 system prompt(014 遗留,默认关),后者改 user message。两者同时开时互不覆盖 —— 但 e2e 门中 MUST 固定 `--temporal-answer-prompt` 状态,否则两个变量混在一起无法归因 |
| 与 `--force-answer` 的关系 | 正交,无交互 |
| 与 abstain regime 的关系 | 正交。脚手架不改变拒答与否的决策,只改上下文内容 |

---

## C-2:脚手架块文本格式

块整体格式(注入位置:`RETRIEVED MEMORIES` 段之后、`QUESTION:` 之前):

```
TIMELINE (computed from the [event:] markers above, chronological):
T1. <date> — memory <n>
T2. <date> — memory <n> (derived from "<relative phrase>")
...
SPAN: T1 → T<k> = <duration>
```

### 逐项约定

| 元素 | 约定 |
|---|---|
| 块标题 | 固定字面量,明示这是**由上文标记算出来的**(而非新证据) |
| 条目前缀 | `T` + 序号,从 1 连续递增 |
| `<date>` | 自然语言形式,**非 ISO**(与既有答题 prompt 的日期约定一致:`21 July 2023` / `May 2023`) |
| `memory <n>` | 指回 `RETRIEVED MEMORIES` 中的编号,**必须**存在且一一对应 |
| 推导标记 | 推导得来的日期 MUST 带 `(derived from "...")` 后缀,原生日期 MUST NOT 带 |
| `SPAN` 行 | 条目 ≥2 时输出;`<duration>` 按 data-model I-4 定精度(粒度不足→"about N months" 式约略表述) |
| 空块 | 条目数 = 0 时**整块不输出**(连标题都没有) |

### 硬约束

1. **块内不得出现任何不在输入记忆中的事实**——只有日期、序号、跨度、以及被引用的相对表达原文。
2. **日期格式与既有 prompt 约定一致**(自然语言、非 ISO)。既有 system prompt 反复要求
   "never ISO format";脚手架若输出 ISO,等于在上下文里和 system prompt 打架。
3. **块位置固定**,不因记忆条数或类别浮动。
4. 块**追加**,不改写 `RETRIEVED MEMORIES` 段的任何字节。

---

## C-3:构造函数签名(Go)

```go
// buildTimelineBlock renders the deterministic date scaffold for one question.
// Returns "" when the scaffold is disabled, the category is not temporal, or no
// memory carries a resolvable event date — in all three cases the caller's
// prompt stays byte-identical to the pre-feature path.
func buildTimelineBlock(memories []retrievedMemory, category int, enabled bool) string
```

| 契约条款 | 约定 |
|---|---|
| **纯函数** | 无 I/O、无全局状态、无 `time.Now()`、无随机源。相同入参 → 逐字节相同返回值 |
| **只读入参** | MUST NOT 修改 `memories` 切片或其元素 |
| **空串语义** | 返回 `""` 表示"无脚手架",调用方据此走与今天完全相同的构造路径 |
| **不 panic** | 任何异常输入(空切片、全空日期、畸形日期串)MUST 降级返回 `""` 或省略该条,不得 panic |
| **参数顺序** | 冻结如上;`enabled` 显式传入而非读全局 options —— 保证可单测 |

调用侧签名扩展(既有函数,加参数):

```go
func buildAnswerContextPrompt(question string, hits []memory.Result, currentDate string, category int, scaffold bool) string
func buildAnswerPrompt(question string, memories []retrievedMemory, currentDate, timeline string) string
func buildSweepAnswerPrompt(question string, memories []retrievedMemory, currentDate, timeline string) string
```

> **两条派生要求**:
> 1. `timeline == ""` 时,后两个函数 MUST 产出与本 feature 之前**逐字节相同**的字符串。
>    这是 FR-006 的实现级保证点,MUST 有专门的回归断言。
> 2. **cluster-sweep 路径同样接脚手架**(`buildSweepAnswerPrompt`)。
>    只接标准路径会让开关在部分题上静默失效,污染 US2 归因(research D-4)。

---

## C-4:口径指纹

`answerRegimeFingerprint`(`cmd/locomo-bench/main.go:1218`)在开关开启时追加:

```
;temporal_date_scaffold=true
```

| 契约条款 | 约定 |
|---|---|
| 关闭时 | fingerprint **逐字节不变**(不追加任何内容,包括 `=false`) |
| 开启时 | **必然**与关闭时不同 |
| 生效路径 | 复用既有 `regime.json` 校验(`main.go:1196-1215`):同一 `--run-dir` 下口径不一致会**直接报错拒跑**,不会静默混用 |
| 追加位置 | 现有字段之后,与既有 `;temporal_answer_prompt=true` / `;judge=mem0-aligned` 风格一致 |

---

## 契约测试清单(实现前必须先失败)

按 TDD,以下断言 MUST 在实现之前写好并**先红**:

| # | 断言 | 对应 |
|---|---|---|
| CT-1 | `enabled=false` → 返回 `""` | C-3 空串语义 / FR-006 |
| CT-2 | `category≠2` 且 `enabled=true` → 返回 `""` | FR-007 |
| CT-3 | 全部记忆无 `EventDate` → 返回 `""` | I-7 / Edge Case |
| CT-4 | 5 条有日期 → 按升序、连续编号 `T1..T5`、日期为自然语言非 ISO | C-2 / FR-003 |
| CT-5 | 无日期的记忆不进块,但**不影响**正文记忆列表 | I-1 / FR-002 |
| CT-6 | 相对表达 + 本条有锚 → 推导出绝对日期且带 `(derived from ...)` | FR-004 |
| CT-7 | 相对表达但**无锚** → 不推导、不标注、不臆造 | I-3 / FR-004 |
| CT-8 | 条目 ≥2 → 输出 `SPAN`,数值精确可断言 | FR-005 |
| CT-9 | 条目 = 1 → **不输出** `SPAN` | I-5 / Edge Case |
| CT-10 | 端点粒度不足 → 跨度降级为约略,**不出现**精确天数 | I-4 / Edge Case |
| CT-11 | 同一输入调用两次 → 逐字节相同 | I-6 / SC-002 |
| CT-12 | 同日多条 → 保持输入顺序(稳定排序) | I-10 / Edge Case |
| CT-13 | `timeline=""` 时 `buildAnswerPrompt` 输出 == 本 feature 前的输出 | C-3 派生要求 1 / SC-003 |
| CT-14 | `buildSweepAnswerPrompt` 同样接受并注入 timeline | C-3 派生要求 2 |
| CT-15 | 开关关 → fingerprint 不变;开关开 → fingerprint 含 `;temporal_date_scaffold=true` | C-4 / FR-008 |
| CT-16 | 跨年日期正确排序与计算 | Edge Case |
| CT-17 | 畸形/空输入不 panic | C-3 不 panic |

> 全部 17 条 **零 LLM 调用、零网络、零 box**,可在 `CGO_ENABLED=0 go test` 下跑完。
