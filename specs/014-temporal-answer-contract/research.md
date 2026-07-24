# Phase 0 Research: 答题侧时序推理契约

## R1: 强化现有 `forceTemporalAnswerPrompt` vs 新增独立开关/常量

**Decision**: 就地重写现有 `forceTemporalAnswerPrompt` 常量,复用现有 `--temporal-answer-prompt` 开关 + `answerPromptForRegime(category, forceAnswer, temporalAnswer, abstain)` 的 category==2 路由(`runner.go:281-290`)。

**Rationale**:
- 路由机制已存在且正确(temporalAnswer && category==2 → force 时返回 `forceTemporalAnswerPrompt`);不需要新增 flag/路由,减少表面积,合"signal not volume"。
- 现有契约**从未在被诊断口径生效**(canonical recipe 不带 `--temporal-answer-prompt`),故重写它不改变任何已评测基线的默认行为——默认仍走 `forceAnswerSystemPrompt`。
- 三臂 e2e 天然可测:`base`=不带开关、`new-tplan`=带开关+新常量、`old-tplan`=带开关+旧常量(git stash / 临时常量),同一路由。

**Alternatives rejected**:
- 新增 `--temporal-answer-prompt-v2` 开关:徒增 flag 与路由分支,无收益。
- 改 `buildAnswerPrompt` 注入独立会话 date 锚:记忆行 `[event:]` 已是绝对锚,足够相对→绝对解析;注入独立锚是 Option B 脚手架的一部分,YAGNI。

## R2: 契约措辞如何精确压三失败模式

**Decision**: 四条推理锚 + 终局约束,一一映射诊断模式:

| 诊断模式(占答错 top-30 内) | 契约锚 |
|---|---|
| ±1 月/年误归属去歧 55% | **EXACT MATCH**:锁问题时间约束精确匹配,显式声明"April/June 是不同事件,绝不 close enough" |
| 相对→绝对未解析 26% | **RELATIVE→ABSOLUTE**:以记忆自身 `[event:]` 为锚解析相对短语成绝对,绝不回显相对短语 |
| 时长/区间算术 18% | **DURATION ARITHMETIC**:认定 START/END 两端点 `[event:]` 相减,输出 duration 非日期 |
| (共性:不先看日期就答) | **ENUMERATE**:判定前逐候选列 `[event:]` 日期 |

终局约束沿用现有:绝对日期自然语言(非 ISO)、最短短语、不复述、force 下永不拒答。

**Rationale**: 诊断给了每模式的**具体病征**(回显相对短语 / 挑 ±1 邻近 / 端点不相减),契约用**祈使 + 反例**(如"April or June is a DIFFERENT event")比泛化"normalize/compare"更能改变模型行为。现有弱契约只有"normalize/compare/interval",未点名任一病征——这正是设 `old-tplan` 归因锚要区分的:是否**点名病征**在涨点。

**Alternatives rejected**:
- few-shot 示例块:增 token/上下文,且 LoCoMo 日期格式多样,示例易过拟合;先试零示例祈使式。
- 让模型输出中间推理再抽取:改答题协议 + 判题,超范围;契约要求"silently reason, output only final"。

## R3: 三臂 e2e 门设计与归因

**Decision**: 一次 box run 三臂 × repeats=3,canonical recipe + `--cat-top-k 1=150`:
- `base`:不带 `--temporal-answer-prompt`(cat-2 走 `forceAnswerSystemPrompt`)。
- `old-tplan`:带开关 + 旧契约常量。
- `new-tplan`:带开关 + 新强化常量。

判据:category-2(n=321)逐题正误多数投票(3 rep),配对 McNemar `new-tplan` vs `base` → SC-001;`new-tplan` vs `old-tplan` → SC-004 归因;overall 不回退核对。

**Rationale**:
- `old-tplan` 归因锚区分"强化本身涨"vs"打开任意 temporal 契约就涨"——避免把开关效应误报成契约设计的功劳(与 013「先诊断避免建错方向」同源纪律)。
- `--cat-top-k 1=150` 锚定现 ship 候选栈(bge-large + cattopk ~86%),使 verdict 相对**当前最强基线**而非裸 bge-large,避免叠加口径漂移。

**Alternatives rejected**:
- 只跑 base vs new-tplan 两臂:省一臂但丢归因,无法回答"是不是措辞强化在起作用"。
- 只跑 category-2 子集:省 token 但丢 overall 不回退核对(FR-007 cross-category 无害需 overall 佐证);canonical 全跑同时给 cat-2 delta 与 overall,一次到位。

## R4: 冷启动首臂偏低 gotcha 规程

**Decision**: box 冷启后第一个 arm 作 warm-up 丢弃,或必复跑一次基线锚;配对 McNemar 只对干净复跑基线,不对冷首臂。

**Rationale**: 已在 014-assoc 评测实证:同配置 base 冷首臂 82.92% vs 复跑 base2 85.17%,差 2.25pp(KV 冷/共卡竞争),险酿假 GO。正本 `docs/locomo-e2e-eval-reproduction.md` 踩坑#10。三臂里把 base 复跑一次(或首臂丢弃)是硬规程。

**Alternatives rejected**: 信任冷首臂——已证会凭空造 +2pp 假显著。

## R5: 引擎零改验证

**Decision**: 实现后 `git diff --name-only -- memory embedding provider store internal` 必空(FR-008/SC-003)。答题契约 100% 在 `cmd/locomo-bench`。

**Rationale**: 答题是 host 职责,引擎不做答题;本 feature 不需要任何引擎侧新入口。这是与检索侧 P0(013/014-assoc)的关键定性差异——那些也未改引擎但需引擎能力评估,本 feature 连评估都不涉及引擎。
