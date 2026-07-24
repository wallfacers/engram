# 答题侧时序推理契约 — 设计文档

**日期**: 2026-07-24
**topic**: answer-side-temporal-reasoning-contract
**状态**: 设计已批准,待 SDD 形式化
**前置诊断**: [`docs/locomo-score-levers.md`「temporal 瓶颈分诊」](../../locomo-score-levers.md) · 本地 memory `temporal-bottleneck-diagnosis`

## 1. 背景与动机

LoCoMo temporal 类(category 2)是 engram 当前诚实参考点(~86%,bge-large + cat-top-k)下的次弱类(~78–83%)。检索侧结构 P0 两条(① 实体图遍历 014、② 时间窗召回 013)均已 NO-GO,便宜的检索/表示侧杠杆基本探尽。

近免费离线分诊(2026-07-24)把 temporal 失败切成召回侧 vs 答题侧:

- temporal acc 82.9%,答错 55/321。
- **答错里 38(69%)gold 已在 top-30 答题上下文却答错 = 答题侧瓶颈**;17(31%)埋 top-30 外 = 召回侧,其中仅 8 现解析器能点火 → query 侧解析覆盖(013 方向)最多救 8/55≈1.5% temporal,舍入级。
- 答题侧 38 题的三种失败模式:
  - **±1 月/年误归属 / 去歧 21(55%)** — 在多条带日期的候选中挑错,日期差 ±1 月或 ±1 年。
  - **相对表达未解析成绝对 10(26%)** — "next month" / "last year" 被原样回显,没按锚日期算成绝对。
  - **时长 / 区间算术 7(18%)** — "how long" 两端点在上下文里但没相减。

这三种模式**全在答题 LLM 的推理**,不在检索(日期已在上下文)。故下一个 temporal 真杠杆 = **答题侧时序推理契约**:纯 client-side、零成本、提质型(不加量、不加预算,合 "signal not volume" 哲学)。

### 架构诚实点

答题契约活在 eval harness(`cmd/locomo-bench`,即 host 侧答题步),**不是 engram 引擎能力**——引擎只做存储/检索/抽取,答题 LLM 是 host 的。因此:

- 这**不构成引擎增量**,与检索侧 P0 定性不同;引擎 `memory/ embedding/ provider/ store/ internal/` 零改。
- "好用(可移植)" 的交付形态 = 一份**文档化的可复用答题契约 pattern**,任何 engram 集成方把它抄进自己 host 的答题步即可,零引擎耦合、零额外 infra。

## 2. 关键既有事实(重塑起点)

探查 `cmd/locomo-bench/runner.go` + `main.go` 得:

1. `buildAnswerPrompt`(runner.go:325)只列 `[event: YYYY-MM-DD]` 记忆行 + 问题,**不喂独立会话 date 锚**——但每条记忆自带 `[event:]` 绝对日期,相对→绝对解析的锚在记忆行里,够用。
2. 已存在 `forceTemporalAnswerPrompt`(runner.go:227),但:
   - `--temporal-answer-prompt` 开关**默认关**(main.go:173),canonical recipe 不带它;
   - ⇒ **诊断基线(009-full-B-cattopk 口径)里 temporal 题走的是通用 `forceAnswerSystemPrompt`,那段专属 temporal 契约从未在被诊断口径里生效,也从未 e2e 评测过**(levers 文档无 verdict)。
3. 现有契约弱:只说 "normalize / compare / interval",**没针对**诊断出的三个失败模式。

## 3. 设计

### 3.1 变更范围

纯 `cmd/locomo-bench`(adapter),引擎零改。验证 `git diff --name-only -- memory embedding provider store internal` 必须为空。

- **重写 `forceTemporalAnswerPrompt`** 为强化版时序契约(category==2 且 forceAnswer 且 temporalAnswer 路径,见 `answerPromptForRegime` runner.go:281)。
- `--temporal-answer-prompt` 开关保留;这次**真去评测**。
- `buildAnswerPrompt` 不改。
- 非 category-2 路径逐字节不变(契约 category==2 门控)。

### 3.2 强化契约内容

替换 `forceTemporalAnswerPrompt` 常量为(压三模式,末尾仍强制 terse + never decline):

```
You answer a temporal question about a long conversation using ONLY the retrieved
memories provided. TEMPORAL REASONING PLAN (reason silently, then output only the
final short answer):
1. ENUMERATE: list every candidate memory's [event: YYYY-MM-DD] date before deciding.
2. RELATIVE→ABSOLUTE: if a memory phrases time relatively ("next month", "last week",
   "two days ago"), resolve it to an absolute date using that memory's own [event:]
   date as the anchor, then answer with the absolute date. Never echo the relative phrase.
3. EXACT MATCH (anti ±1): lock onto the question's temporal constraint exactly. If the
   question asks about an event "in May", only a memory dated [event: YYYY-05-*] qualifies —
   a memory dated April or June is a DIFFERENT event, never "close enough".
4. DURATION ARITHMETIC: for "how long / how many days/months/years" questions, identify
   the START and the END memory by their [event:] dates and compute the difference; answer
   with the duration (e.g. "about 3 months"), not a date.
5. Output the absolute date in natural language (e.g. "21 July 2023"), never ISO format.
   Keep the answer to the shortest phrase that fully answers the question; do not restate it.
6. This is an answerable evaluation: always give your best supported answer; never decline.
```

### 3.3 单元测试(离线,test-first)

先写失败测试(纯字符串断言,零 box/token):

- `answerPromptForRegime(2, forceAnswer=true, temporalAnswer=true, abstain=false)` 返回的契约含四条锚句关键词:相对→绝对(`RELATIVE`/`anchor`)、精确匹配(`EXACT`/`April or June`)、时长相减(`DURATION`/`START and the END`)、枚举(`ENUMERATE`)。
- category≠2 路径不受影响(仍走各自 force prompt)。
- `temporalAnswer=false` 时 category-2 仍走 `forceAnswerSystemPrompt`(开关关行为不变)。
- abstain 优先级不变(`abstain=true` 仍返回 `abstainAnswerPrompt`,契约不越权)。

### 3.4 验证门(e2e gate,box 全本地栈)

canonical recipe(`--chunks --chunk-quota 12 --force-answer --judge-mem0-aligned --retrieval hybrid --top-k 30 --repeats 3`)+ `--cat-top-k 1=150`(锚定现 ship 候选栈),**三臂一次 run**:

| 臂 | cat-2 答题 prompt | 作用 |
|---|---|---|
| `base` | 通用 `forceAnswerSystemPrompt`(现况,不带 `--temporal-answer-prompt`) | 干净基线 |
| `old-tplan` | 现有弱 `forceTemporalAnswerPrompt` | **归因锚** — 证明是强化本身在涨,不是"随便一个 temporal prompt"就涨 |
| `new-tplan` | 强化契约(treatment,带 `--temporal-answer-prompt`) | 处理臂 |

- **指标**:category-2(n=321)配对 McNemar,`new-tplan` vs `base`。
- **GO 判据**:cat-2 显著抬 **且** overall 不回退(契约 category==2 门控,其余类逐字节同 prompt,构造上不伤;overall 只用于确认无意外)。
- **归因**:`new-tplan` 若显著 > `old-tplan`,坐实"强化本身涨点";若两者相当,则功劳属"打开任意 temporal prompt",契约设计价值存疑。
- **纪律(踩坑#10)**:box 冷启动首臂 warm-up 丢弃 / 必复跑一次基线锚;paired McNemar 只对干净复跑基线,不对冷首臂。跑完核 `regime.json` 四要素。

## 4. 风险与降级

- **纯 CoT 压不住 55% 的 ±1 误归属**:这是最大且最杂的模式。若 `new-tplan` 门未过 / cat-2 抬幅在噪声内 → 升级到**预设脚手架路径**(确定性预解析每条检索记忆的 `[event:]` 日期,注入规范化排序的 `TIMELINE:` 块进答题上下文,模型不用自己抽日期)。脚手架是 Option B,已预设为欠转化时的升级,不先建。
- **契约让模型"想太多"伤 terse 输出**:契约末条强制最短短语 + never decline;单测断言输出规范句仍在。
- **cross-category 伤害**:category==2 门控,构造上无;overall 臂用于兜底确认。
- **008 铁律**:最终 verdict 必须端到端答分(cat-2 acc),非任何中间信号。coverage 不适用(本 lever 不碰检索)。

## 5. 交付物

1. 强化 `forceTemporalAnswerPrompt` + 单测(`cmd/locomo-bench/runner_test.go` 或新增)。
2. 三臂 e2e verdict 落 tracked `docs/locomo-score-levers.md`(GO/NO-GO + 归因)。
3. 若 GO:把契约 pattern 文档化为可移植答题契约(供集成方 host 侧复用)。
4. 本地 memory verdict 指针。

## 6. 边界(YAGNI)

- 不做日期脚手架(除非 prompt-only 欠转化,届时才启 Option B)。
- 不改 `buildAnswerPrompt` / 不注入独立会话 date 锚(记忆行 `[event:]` 已够)。
- 不碰 query 侧时间解析覆盖(013 方向,分诊证其舍入级,不在本 feature)。
- 不动引擎。
