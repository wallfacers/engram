# 记忆会变（Memory Mutability）— 评测面诊断

**日期**: 2026-08-07 · **性质**: 零成本离线诊断（不跑模型，仅扫数据集）· **动机**: 评估"写侧演化检测 + supersede 状态 + 时间有效性"方向（HiGram/MemCog 启发）在 engram 现有评测上是否有测试面，避免 013/IRIS 式"机制有效但评测点不着火"。

## 核心问题

LoCoMo / LongMemEval 的 temporal / knowledge-update 题，有多少真正依赖**对象状态随时间演变**（同一对象在不同时间有不同状态，需要区分旧/新值）？

## 诊断方法

对 `testdata/locomo/locomo.json`（1986 题）与 `testdata/longmemeval/longmemeval_s_cleaned.json`（500 题）做三类统计：
1. **答案形态分类**（LoCoMo temporal 321 题）：答案是否本身就是时间/时长表达。
2. **演变/更新语义关键词命中**（now/currently/before/used to/moved/switched/updated/old/new…），再人工核验抽样是否**真**需要旧新区分。
3. **题面证据是否跨 session**（是否需要跨时间点拼状态）。

## 结果

### LoCoMo — temporal 321 题（category=2）

| 判据 | 数量 | 说明 |
|---|---|---|
| 答案即时间/时长表达 | 223（69.5%） | "7 May 2023"、"4 years"、"the week before 25 May" |
| 答案非时间 | 98 | 多为"某时间某人做了什么/是谁"（时间锚定的事件内容） |
| evidence 跨 ≥2 session | 28（8.7%） | 少数需要跨时间点 |
| **真正状态演变题** | **≈1/321** | 仅 "Which city was John in **before** traveling to Chicago?" → Seattle |

**关键形态**：LoCoMo temporal 题 = 事件→时间定位 + 相对时间推算（"the week/friday before X"、时长算术）。**几乎没有"同一对象不同时间不同状态"的题**。"before" 在题面里 99% 是相对时间介词，不是演变。

### LongMemEval — 500 题

| 类别 | 数 | 形态 | 真演变题 |
|---|---|---|---|
| temporal-reasoning | 133 | 时间差（"days passed between"）、事件排序（"earliest to latest"）、"most recent" | ~2-4 |
| knowledge-update | 78 | **多数是属性当前值**（"How many bikes do I currently own?"）；少数真更新 | ~5-8 |
| 其余 | 289 | single/multi-session 属性 | ~0 |

**真更新样本**（需要旧/新区分）："How many engineers do I lead when I just started…? How many do I lead now?"（4→5）、"coffee-to-water ratio, did I switch to more or less?"、Rachel relocation。**这类题在全 500 题中约 5-10 题（1-2%）。**

## 结论

1. **PASS — 评测无测试面。** "记忆会变"（写侧演化检测、supersede 链、时间有效性状态机）在 LoCoMo 321 个 temporal 题上测试面 ≈0，LongMemEval 500 题上 ≈1-2%。实现它无法用现有基准验证，极可能重蹈 013/IRIS："机制真实但评测点不着火"。
2. **这解释时间域 7 次证伪的根源**：LoCoMo/LongMemEval 的 temporal 主形态是**事件-时间锚定 + 相对时间推算 + 时间差/排序**，测的是"时间定位"能力，**不是"状态一致性/演变"能力**。真瓶颈（013/017/028 已定位）在答题侧时间推理契约（相对→绝对解析、时长算术），不在存储/检索结构。
3. **"记忆会变"的定位应为产品正确性，而非评测涨点**：真实记忆必须能更新/纠错/过期（用户改地址、改偏好时旧事实不得继续作为当前事实返回）。这值得做，但验证需 MemConflict 类专门 bench（HiGram `2608.05095` 用）或自建更新场景，**不能依赖 LoCoMo/LongMemEval**。

## 行动建议

- **不立项**写侧演化检测作为评测方向。
- 若做"记忆更新"，按产品正确性功能走：用户/系统显式写入更新时对旧 entry 置 `superseded_by`（schema 已有），读侧把时间有效性暴露给 answerer；作为生产质量项而非分数杠杆。
- 评测缺口是真实的：需要一个覆盖"状态演变/知识更新"的 bench 才能验证该类能力（MemConflict 或自建）。

## 关联

- [[031-evidence-relation-sdd-ready]]（031 verdict：related_to 无区分度，temporal_next 可靠 → 时间信息在 store 是可靠的，缺的是答题侧用法）
- [[027-write-side-event-verdict]] / [[028-us2-verdict-nogo]]（写侧结构化两次证伪）
- [[013-temporal-window-verdict]] / [[IRIS-us1-verdict]]（temporal 检索侧杠杆全 NO-GO）
- [[temporal-bottleneck-diagnosis]]（temporal 主瓶颈在答题侧：答错题 69% gold 已进 top-30）
