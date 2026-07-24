# Feature Specification: 答题侧时序推理契约 (Answer-Side Temporal Reasoning Contract)

**Feature Branch**: `014-temporal-answer-contract`

**Created**: 2026-07-24

**Status**: Draft

**Input**: 已批准 brainstorm 设计正本 `docs/superpowers/specs/2026-07-24-answer-side-temporal-reasoning-contract-design.md`。LoCoMo temporal(category 2)瓶颈经近免费分诊定位在**答题侧**(69% 答错题 gold 已进 top-30 上下文却答错),三失败模式:±1 月/年误归属去歧 55% · 相对表达未解析成绝对 26% · 时长/区间算术 18%。纯 client-side 零成本提质型契约,引擎零改。

## 背景 *(context)*

engram 的 LoCoMo temporal 类是当前诚实参考点(~86%)下的次弱类。检索侧结构 P0 两条(实体图遍历 014-assoc、时间窗召回 013)均 NO-GO。近免费离线分诊把 temporal 失败切成召回侧 vs 答题侧,坐实**主瓶颈在答题侧**:日期已在答题上下文里,答题 LLM 的时序推理没做对。因此下一杠杆是**答题侧推理契约**,而非任何检索改动。

**架构定性(诚实点)**:答题契约活在 host 侧答题步(eval harness `cmd/locomo-bench`),**不是 engram 引擎能力**。引擎只做存储/检索/抽取;答题 LLM 是 host 的。故本 feature 引擎(`memory/ embedding/ provider/ store/ internal/`)**零改**,交付形态是一份**可移植的答题契约 pattern**,供任何 engram 集成方在自己 host 的答题步复用。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 强化时序推理契约 (Priority: P1)

作为在 host 侧用 engram 记忆答 temporal 问题的集成方(评测中由 harness 扮演),当检索到的记忆已含正确日期信息时,答题步应遵循一份显式的时序推理契约,把三种已诊断的失败模式各个击破,给出正确的绝对日期 / 时长答案,而不是回显相对短语、挑错 ±1 邻近日期、或漏做端点相减。

**Why this priority**: 这是本 feature 的 MVP 与全部提质来源。它可**完全离线**、零 box/token 单元测试(纯字符串契约断言 + 路由断言),独立交付价值。没有它后面的 e2e 门无对象。

**Independent Test**: 离线单测断言:temporal(category==2)+ force + temporal-answer 路径返回的契约文本含四条锚(枚举候选日期 / 相对→绝对解析 / 精确匹配反 ±1 / 时长端点相减),且非 category-2 路径、契约开关关闭时行为逐字节不变。无需 box、无 token。

**Acceptance Scenarios**:

1. **Given** 一道 category-2 temporal 问题走 force + temporal-answer 路径, **When** 选择答题契约, **Then** 返回的契约同时包含:相对→绝对解析指令、精确日期匹配(反 ±1)指令、时长两端点相减指令、逐候选枚举 `[event:]` 日期指令。
2. **Given** 一道 category≠2 问题(multi-hop / open-domain / single-hop), **When** 选择答题契约, **Then** 返回各自原有契约,逐字节不受本 feature 影响。
3. **Given** 时序契约开关关闭(默认), **When** 选择 category-2 答题契约, **Then** 行为与本 feature 前完全一致(回退通用 force 契约)。
4. **Given** abstention regime 生效, **When** 选择答题契约, **Then** 仍优先返回 abstain 契约(时序契约不越权)。

---

### User Story 2 - 三臂配对 e2e 验证门 (Priority: P2)

作为要判定该契约是否**真涨点**的维护者,需要在 box 全本地栈上以 canonical recipe 跑一次三臂配对 e2e,用配对统计判 GO/NO-GO,并带一个归因锚臂,区分"强化契约本身涨点"与"打开任意 temporal 契约就涨点"。

**Why this priority**: 这是 Constitution IV 评测回归门的兑现,也是唯一能坐实提质的证据(008 铁律:最终 verdict 必须端到端答分)。依赖 US1 的契约已实现。

**Independent Test**: 一次 box run 产出三臂 category-2 逐题正误,配对 McNemar `new-tplan` vs `base`,并对比 `new-tplan` vs `old-tplan`;附 overall 不回退核对。

**Acceptance Scenarios**:

1. **Given** box 全本地栈 + canonical recipe(`--top-k 30`,**无 cat-top-k**,干净 bge-large 基线), **When** 跑 base / old-tplan / new-tplan 三臂 repeats=3, **Then** 得到每臂 category-2(n=321)准确率与逐题正误,用于配对检验。
2. **Given** 三臂结果, **When** 判定, **Then** GO 当且仅当 `new-tplan` 相对 `base` 在 category-2 上**显著抬升**(配对 McNemar)**且** overall 不回退。
3. **Given** GO 成立, **When** 归因, **Then** 报告 `new-tplan` vs `old-tplan` 的差分,声明提质是否归于"强化本身"。
4. **Given** box 冷启动, **When** 采集基线, **Then** 首臂作 warm-up 丢弃或复跑一次干净基线锚;配对检验只对干净复跑基线,不对冷首臂。

---

### User Story 3 - 可移植契约 pattern 文档化 (Priority: P3)

作为 engram 集成方,若该契约被证 GO,需要一份不依赖评测 harness 的、可直接抄进自己 host 答题步的契约 pattern 文档。

**Why this priority**: 兑现"好用/可移植"承诺,把 eval 内的赢转成产品可用资产。条件性——仅当 US2 判 GO 才交付。

**Independent Test**: 文档存在且自包含(契约全文 + 三失败模式动机 + 适用条件:记忆行须带绝对 `[event:]` 日期锚),读者无需读 harness 代码即可复用。

**Acceptance Scenarios**:

1. **Given** US2 判 GO, **When** 交付, **Then** tracked 文档含契约全文、三模式动机、复用前置条件。
2. **Given** US2 判 NO-GO, **When** 收口, **Then** 落 NO-GO verdict(含归因)于 tracked docs,不产出移植文档,并记录欠转化时的升级路径(确定性日期脚手架 Option B)。

---

### Edge Cases

- **相对短语缺锚**:某条记忆用相对表达但自身无 `[event:]` 日期 → 契约要求以该记忆自身锚解析;无锚时不得臆造绝对日期,退回记忆所支持的最粗粒度。
- **时长问题只有单端点**:"how long" 但上下文只找到一个端点 → 不得编造另一端点;给最佳支持的部分答案而非拒答(force regime)。
- **多候选同一时间约束**:多条记忆命中同一月份 → 精确匹配指令要求锁问题约束,不得因"更显著"挑错事件。
- **契约让模型冗长**:强化 CoT 可能诱发长输出 → 契约末条强制最短短语、不复述问题。
- **cross-category 泄漏**:契约必须 category==2 门控,不得改变其它类答题行为。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 为 category-2(temporal)+ force + 时序契约开关开启的答题路径提供一份强化时序推理契约,取代现有弱契约。
- **FR-002**: 契约 MUST 指示答题者在判定前逐条枚举候选记忆的 `[event:]` 绝对日期。
- **FR-003**: 契约 MUST 指示把相对时间表达(如 "next month"/"last week"/"two days ago")以该记忆自身 `[event:]` 日期为锚解析成绝对日期,且不回显相对短语。
- **FR-004**: 契约 MUST 指示锁定问题的时间约束做精确匹配,显式拒绝 ±1 月/年的邻近事件"差不多算对"。
- **FR-005**: 契约 MUST 指示对时长/区间问题("how long / how many days/months/years")认定起止两端点并相减,输出 duration 而非某个日期。
- **FR-006**: 契约 MUST 保留终局约束:输出绝对日期自然语言(非 ISO)、最短短语、不复述问题、force regime 下永不拒答。
- **FR-007**: 系统 MUST 保持 category≠2 路径、时序契约开关关闭时、abstention regime 优先级三者的行为逐字节不变。
- **FR-008**: 引擎(`memory/ embedding/ provider/ store/ internal/`)MUST 零改动;`git diff --name-only -- memory embedding provider store internal` 为空。
- **FR-009**: 系统 MUST 提供三臂 e2e 评测能力(base / old-tplan / new-tplan),使 category-2 逐题正误可配对比较并做归因。
- **FR-010**: 若 e2e 判 GO,系统 MUST 产出可移植的契约 pattern 文档(自包含,声明记忆行须带绝对 `[event:]` 锚的前置条件);若 NO-GO,MUST 落带归因的 verdict 并记录升级路径。

### Key Entities *(include if feature involves data)*

- **时序推理契约 (temporal answer contract)**:一段答题系统提示文本,含四条推理锚(枚举 / 相对→绝对 / 精确匹配反±1 / 时长相减)+ 终局约束;绑定 category==2 且 force + 时序开关路径。
- **e2e 臂 (arm)**:一次评测配置,以 category-2 答题契约区分:`base`(通用 force 契约)、`old-tplan`(现有弱契约)、`new-tplan`(强化契约)。
- **配对判据 (paired verdict)**:category-2 逐题正误的配对 McNemar 结果 + overall 不回退核对 + 归因差分。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: category-2 答对率在 `new-tplan` 相对 `base` 上**统计显著抬升**(配对 McNemar,box 全本地栈 canonical recipe repeats=3),且 overall 答对率不回退。
- **SC-002**: 全部 US1 离线单测通过,证四条推理锚在位、非 category-2 与开关关闭路径逐字节不变——零 box、零 token。
- **SC-003**: 引擎零改动(diff 为空),证本 feature 是纯 host 侧适配器改动。
- **SC-004**: 提质可归因——报告 `new-tplan` vs `old-tplan` 的差分,明确说明涨点是否归于"强化本身"而非"打开任意 temporal 契约"。
- **SC-005**: 判定过程遵循冷启动首臂 warm-up 纪律(配对只对干净复跑基线);verdict(GO 或 NO-GO)落 tracked docs。

## Assumptions

- 检索到的记忆行已携带绝对 `[event: YYYY-MM-DD]` 日期锚(engram 现有格式);契约靠此做相对→绝对解析,无需另喂独立会话 date 锚。
- 分诊结论(69% 答错题 gold 已进 top-30)成立,故答题侧是主瓶颈;召回侧短板(31%,其中现解析器可及仅 8/55≈1.5% temporal)不在本 feature 范围。
- 现有 `--temporal-answer-prompt` 开关 + category==2 路由复用;本 feature 强化其目标契约文本,不新增路由机制。
- box 全本地栈可用(answer/extract = box vllm Qwen;embedder = box vllm bge-large;judge = deepseek 微付费),按 canonical recipe 与冷启动纪律运行。
- 纯 prompt 契约为首选路径;若欠转化,确定性日期脚手架(预解析 `[event:]` 注入 TIMELINE 块)为预设升级(Option B),不在本 feature 首版实现。
- 不碰 query 侧时间解析覆盖(013 方向,分诊证其舍入级)。
