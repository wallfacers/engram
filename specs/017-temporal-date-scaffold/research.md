# Phase 0 Research: 确定性日期脚手架

**Feature**: 017-temporal-date-scaffold | **Date**: 2026-07-27

spec.md 未留任何 `[NEEDS CLARIFICATION]`。本文记录实现前对**现有代码的实证探查**、
由此确定的技术决策,以及**被否掉的方案**——尤其是那些看起来更"完整"但会踩到既有教训的。

---

## D-1:日期从哪来 —— 读结构化字段,不做文本抽取

**Decision**: 脚手架的日期来源是 `memory.Result.EventDate`(`*time.Time`),
经 `toMemories`(`cmd/locomo-bench/main.go:1732-1745`)格式化为 `retrievedMemory.EventDate`
(`"2006-01-02"` 字符串)。脚手架把该字符串解析回 `time.Time` 参与排序与算术。

**Rationale**: 探查证实事件日期**本来就是结构化的**:

```go
// main.go:1735-1738
rm := retrievedMemory{Name: h.Name, Content: h.Content, SourceSessionID: h.SourceSessionID}
if h.EventDate != nil && !h.EventDate.IsZero() {
    rm.EventDate = h.EventDate.Format("2006-01-02")
}
```

且 `retrievedMemory.Line()`(`runner.go:480-490`)就是用它渲染出模型看到的 `[event: ...]` 标记。
这意味着 spec FR-002 的"抽取绝对事件日期"在实现上**退化为读一个字段**——
没有正则、没有歧义、没有解析失败分支。脚手架唯一的不确定性来源因此被压缩到 D-3 一处。

**Alternatives considered**:

- ❌ **从 `Line()` 渲染出的文本里正则抽 `[event: YYYY-MM-DD]`**。是同一份数据绕一圈,
  凭空引入一个可能与上游渲染逻辑漂移的解析器。上游一改格式,脚手架静默失效。
- ❌ **要求引擎新增"日期范围"公开字段**(即 `temporal-t4-design.md` 的 T-2 设想:
  把 range 暴露给 reader)。这会把一个 adapter feature 变成引擎契约增量,
  触发宪法 II/III 的重门槛,而**收益为零**——现有单点日期已足够做排序与跨度计算。
  这正是 017 相对 T-2 的关键简化:**不依赖引擎暴露任何新东西**。

---

## D-2:类别门控怎么接 —— 显式传参,不做隐式推断

**Decision**: 给答题上下文构造路径显式传入 category 与开关状态。
`buildAnswerContextPrompt` 增加参数,四个调用点(`main.go:1509,1644,1680,1715`)一并更新。

**Rationale**: 探查发现 `buildAnswerContextPrompt(question, hits, currentDate)`
**当前不接收 category**,而 category 在同一作用域是拿得到的(`qa.Category`,
见 `main.go:1504` 构造 system prompt 时就在用)。FR-007 要求脚手架仅对 temporal 类生效,
故必须把它传下去。显式参数是唯一诚实的做法。

**Alternatives considered**:

- ❌ **从问题文本猜是不是 temporal 题**(关键词匹配 "when/how long/before/after")。
  这会引入一个**分类器**——一个新的错误来源、一个新的需要调的东西,且与数据集自带的
  category 标签冲突时无人裁决。spec Assumptions 已明确"沿用 014 已确立的类别门控方式,
  不新增分类器"。
- ❌ **对所有类别都开脚手架**。违反 FR-007,且直接踩 `opinion-pass` 的坑:
  台账已记载"全局对称抬升"是那次净负的根因;`locomo-score-levers.md` 把
  "只对特定类别开信号"列为 #2 方向存活的理由。全类别开还会让 NO-GO 时无法归因
  ——分不清是脚手架没用,还是伤了别的类。

---

## D-3:相对表达解析 —— 窄集合 + 静默降级,唯一的不确定性来源

**Decision**: 只解析诊断中**实际出现过**的相对表达族,用**该条记忆自己的 `[event:]` 日期**
作锚,解析结果在 TIMELINE 中**标注为推导值**(与原生日期可区分)。任一环节不成立
(无锚 / 表达不在窄集合内 / 解析有歧义)→ **静默跳过**,原文仍在正文记忆列表里。

**Rationale**: 这是脚手架里唯一"可能算错"的部分,占诊断错题的 26%(10/38)。
把它做窄、做可降级,是为了让"脚手架算错"这个失败模式尽可能不发生——
一旦发生,它比"模型自己算错"更糟:模型算错还有别的线索可救,**代码算错是把错误答案
以权威格式喂给模型**。标注推导值也是同一考虑:让模型知道哪些是原始事实、哪些是推算,
保留它自行否决的余地。

**Alternatives considered**:

- ❌ **引入通用自然语言时间解析库**。违反"依赖最小化",且把确定性交给外部实现;
  通用解析器的宽容度恰恰是风险来源(它会对模糊表达给出自信的错误答案)。
- ❌ **用 LLM 做相对表达归一化**。直接违背本 feature 的立论——014 已证明
  "让模型做时序推理"这条路走不通;用模型去修模型的毛病是循环论证,且引入 token 成本与不确定性。
- ❌ **无锚时用会话时间戳兜底**。`retrievedMemory` 另有 `Recorded`(记录时间)字段,
  技术上可当锚。但"记录时间 ≠ 事件时间"——`temporal-t4-design.md` 的风险清单里
  "旧 chunk 拿 mention time 当 event time"就是列明的坑。宁可不解析。

---

## D-4:TIMELINE 块放哪 —— 记忆列表之后、问题之前,单点注入

**Decision**: 脚手架块作为一段独立文本,插在 `RETRIEVED MEMORIES` 之后、`QUESTION` 之前。
实现上由独立纯函数产出字符串,`buildAnswerPrompt` / `buildSweepAnswerPrompt` 接受该字符串,
**为空时走与今天逐字节相同的路径**。

**Rationale**: 位置选择让模型先看到原始证据、再看到归纳出的时间线、最后看到问题——
时间线是对上文的**索引**而非替代品。单点注入 + 空字符串短路,是让 FR-006
("关闭时逐字节一致")可以由**构造本身**保证而非靠测试碰运气的最省事做法。

**Alternatives considered**:

- ❌ **把 TIMELINE 塞进 system prompt**。system prompt 是按 category 选的常量,
  与每题的检索结果无关;塞进去会把常量变成动态串,破坏现有 prompt 常量的可测性,
  也让 `answerPromptForRegime` 的纯函数性质丢失。
- ❌ **用 TIMELINE 替换原有记忆列表**。那会**移除**信息(记忆正文),
  把"重组"变成"筛选"——一旦脚手架漏掉某条,该条就对模型彻底不可见。
  Edge Case "无日期的记忆不进 TIMELINE 也不被丢弃"正是为堵这个。
- ⚠️ **只在标准路径注入,不管 cluster-sweep 路径**。已否:`buildAnswerContextPrompt`
  会在 `hasClusterSweepHit(hits)` 时分流到 `buildSweepAnswerPrompt`(`runner.go:367-373`)。
  两条路径都要接脚手架,否则开了开关却在部分题上静默失效——那会污染 US2 的归因。

---

## D-5:跨度算什么 —— 首尾必算,粒度不足则降级表述

**Decision**: 候选 ≥2 时至少给出**首尾跨度**;相邻跨度按契约给出。
当端点日期粒度不足以支撑精确天数时,降级为粗粒度表述,**绝不补零假装精确**。

**Rationale**: 诊断中"时长/区间算术"占 7/38(18%),失败形态是"两端点都在上下文里但没相减"
——即**需要的正是首尾差**。补零假装精确是 spec Edge Cases 明令禁止的:
一个"精确到天"的错误跨度比一个"约两个月"的正确跨度危险得多。

**Alternatives considered**:

- ❌ **枚举所有候选两两之间的跨度**。n=30 时是 435 个数字,把上下文淹没,
  且与 signal-not-volume 直接冲突;真正被问到的几乎总是首尾或相邻。
- ❌ **不算跨度,只给排序**。会漏掉 18% 那一族——而它恰恰是"确定性代码最擅长、
  模型最不擅长"的部分,放弃它等于放弃本 feature 最有把握的收益来源。

---

## D-6:验证策略 —— 离线钉正确性,e2e 只回答"有没有用"

**Decision**: 严格两层。**US1 层**用表驱动单测把脚手架的正确性、确定性、降级行为、
逐字节不变性全部钉死,零成本、零 box。**US2 层**只回答一个问题:开了它端到端有没有涨。
US2 的 verdict **不看**任何中间信号。

**Rationale**: 这是对 008 铁律("覆盖率增益 ≠ 答题增益",本项目已连续成立 N 次)
和 014 教训的直接编码。分层的实际价值在归因:如果 US2 判 NO-GO,US1 的绿测让我们能说
"脚手架**实现是对的**,是这个**思路**在端到端上不转化",而不是留下"可能只是有 bug"的悬案。
014 的 NO-GO 之所以有说服力,正因为它的契约内容有单测断言。

**Alternatives considered**:

- ❌ **用"TIMELINE 里 gold 日期出现率"之类的中间指标当门**。这就是 008 铁律反复
  惩罚的模式(reranker coverage +15.457pp → e2e −0.06pp;assoc cov@30 +2.6pp → e2e NO-GO)。
  spec FR-011 已明令禁止。
- ❌ **先跑 e2e 再补单测**。会让 NO-GO 无法归因,且浪费 box 时间在可能有 bug 的实现上。

---

## 既有教训清单(实现期必须随时对照)

| 教训 | 出处 | 对本 feature 的约束 |
|---|---|---|
| **008 铁律**:中间信号增益不转化为答题增益 | 008 US1/US3、014 assoc | verdict 只认 temporal 类端到端答分(FR-011) |
| **给模型更多结构可能反而更差** | 014 强化契约翻车(显著差于旧简单契约) | TIMELINE 可能稀释上下文;NO-GO 归因必须能识别这一种(Edge Cases 末条) |
| **冷启动首臂偏低 ~2.25pp** | 014 assoc 诊断,险酿假 GO | 首臂丢弃/复跑,配对检验只对干净复跑基线(FR-012) |
| **同配置重跑就差 2 分,per-rep 带宽 9–10 分** | LongMemEval 消融 `ref` 臂 | 必须有噪声标尺臂,否则单臂对单臂结论不可信(FR-012) |
| **全局对称抬升是净负的根因** | opinion-pass NO-GO | 严格 category 门控(FR-007) |
| **"加量"型涨点不被接受** | maintainer 的 signal-not-volume 哲学 | 不动 top-k/候选池;token 增量强制实测(FR-013) |
| **口径改动须单独 commit + 声明新基线** | 宪法 IV、force-answer 那次 | GO 时 eval 结果与实现分开提交(FR-016) |
| **实验结论必须落 tracked docs** | 本项目纪律(换环境会丢) | US3 强制回填 `locomo-score-levers.md`(FR-015) |

---

## 未决但不阻塞的问题

以下留到实现/tasks 阶段按契约定,不影响 plan 的结构决策:

1. **TIMELINE 块的长度上界具体取值**。契约要求"有明确上界策略且可断言"(spec Edge Cases),
   具体阈值在 tasks 阶段随 token 预算实测定;默认取 top-k 全量(30 条上限本就不大)。
2. **相对表达窄集合的确切成员**。以诊断中实际出现的 10 题形态为准,
   在写单测时逐条固化为表驱动用例。
3. **推导值的标注符号**。契约层面只要求"可区分",具体记号在 contracts 定稿时固定。
