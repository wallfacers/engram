# Feature Specification: Counterfactual Evidence Utility Gate（反事实证据效用门控）

**Feature Branch**: `worktree-042-counterfactual-evidence-utility`

**Created**: 2026-08-14

**Status**: Draft

**Input**: User description: "040 和 041 的 top-k 缩减已失败；下一步把目标改为预测深检索相对浅检索的反事实效用，先用真实生成概率信号做低成本诊断，只有证明能在不掉分的前提下降低完整决策路径预算，才进入门控验证。"

## Context and Scope

040 已证明检索分数形状不能可靠判断上下文是否足够：在同口径的浅检索与深检索配对中，深检索救回 56 题、反害 31 题，且多数救回题的关键答案信息在浅上下文中已经可见。041 又证明 answerer 文本中的“犹豫”不能可靠触发深检索：自动检测对错题的召回仅约 50%–63%，真实迭代出现救回少于反害。

本 feature 不再预测“浅答案是否错误”或“模型是否犹豫”，而是预测对同一个问题执行深检索的反事实效用：

- **BENEFIT（+1）**：浅检索答错、深检索答对；
- **NEUTRAL（0）**：两种预算结果相同；
- **HARM（−1）**：浅检索答对、深检索答错。

首轮只评估浅答案正常生成时同步产生的概率信号是否足以预测该三值效用。若诊断不通过，本 feature 必须以 NO-GO 收口，不得改换文本规则、自主判断、检索分数阈值或额外生成探针来追逐结果。

**In scope**:

- 构造可审计的浅/深配对效用标签与概率信号数据；
- 按 conversation 隔离校准与验证，测量信号对 BENEFIT/HARM 的真实区分能力；
- 诊断通过后，验证一次浅检索到一次深检索的两档门控策略；
- 同时衡量答案质量、完整决策路径 token、调用次数与延迟；
- 仅在 LoCoMo primary GO 后执行 LongMemEval non-regression 迁移门，区分单 benchmark 机制成立与可移植能力；
- 产出 GO/NO-GO verdict，并保留可复现审计记录；
- 全量 collect 前先执行 2-conversation 信号存在性 pilot：仅用前两条 conversation 的浅/深配对，以固定 ridge 得分对 BENEFIT 类别测 AUC；AUC<0.65 或类别缺失致 AUC 不可定义时立即 valid NO-GO，避免把 8/10 采集预算花在无信号方向上（来源：042 对抗式评审可行性结论，冻结于 research.md）；
- 用显式 harm 上限取代"只看净效用"：门控路由须满足冻结精度前沿 `56c−31h≥25`（c=BENEFIT 捕获率、h=HARM 触发率，56/31 为历史锚计数），写成可逐题复算的判据。

**Out of scope**:

- MCP、CLI、SDK 或其他产品适配面的生产接线；
- 修改公开 provider/engine 契约，或把概率信号设为默认依赖；
- 文本犹豫规则、LLM 自主“是否够用”判断、gap-knee/分数阈值；
- generator-aligned passage 选择、语义分段、答案验证器；这些是独立后续 feature；
- 多于“浅→深”两档的循环、额外 closed-book answer pass；
- 任何付费云 reranker/recall 模型。

## Clarifications

### Session 2026-08-14

- Q: 042 的最终 GO 质量门采用哪种？ → A: 严格不掉分：同批配对中正确题数不低于固定 k150，且绝对分 ≥90%。
- Q: 最终 GO 的成本门采用哪种？ → A: 完整决策路径 token ≤固定 k150 的 60%；计入浅/深作答及信号开销，另报调用次数与延迟。
- Q: 042 的交付边界是什么？ → A: 分阶段同 feature：概率信号诊断通过后继续实现并验证浅→深门控；有效诊断未过硬门立即 NO-GO，协议/基础设施错误保持 INVALID。
- Q: 042 的 benchmark 范围如何定？ → A: LoCoMo 为 primary；通过后跑 LME non-regression 迁移门。LME 失败阻止可移植/产品晋级，但保留 LoCoMo 机制 verdict。
- Q: 运行时效用决策器允许多复杂？ → A: 预注册低容量校准规则；特征族、模型族和复杂度在 held-out 前冻结。
- Q: 历史 040 是否承担 042 的诊断门？ → A: 不承担。历史结果只验证 label constructor；诊断门只使用 fresh collect 的 LOCO cross-fitted held-out 合并结果。
- Q: fresh 批次的净效用门如何处理批次内 deep-vs-shallow 差值 `D`？ → A: 保留独立的硬 `+25` 效应量门，并同时要求不低于同批 deep；若 `D<25`，有意从严地要求 policy 比 deep 多至少 `25-D` 题。
- Q: 新 caller 如何对齐现有 answer sampling recipe？ → A: 与现有 OpenAI adapter 的实际 wire shape 一样省略 `temperature` 字段；不把 Go 零值误写成有效温度 0，并冻结 sidecar 配置摘要。
- Q: 长批次是否允许 provider retry？ → A: 每个逻辑模型调用最多 3 次 attempt，只重试预注册的瞬时错误；每次 attempt 都写 journal 并计入对应臂成本，耗尽或非瞬时失败使 stage INVALID。

### Session 2026-08-14 (external review closure — 补两个已评审确认缺失的约束)

- Q: 全量 collect 前是否需要先验证概率信号存在？ → A: 需要。新增 2-conversation 信号存在性 pilot stage：只在前两条 conversation（`conversation_ids[0..1]`）上采集浅/深配对，用固定 ridge 得分对 BENEFIT 类别测 AUC。AUC<0.65 或 BENEFIT/HARM 缺类致 AUC 不可定义时，立即 valid NO-GO，省下其余 8 条 conversation 的采集预算。pilot AUC 是 in-sample 存在性 kill-gate，本身不授权任何后续 stage；完整 10-conversation collect + LOCO 诊断仍是唯一 GO authority。pilot 的来源是 042 对抗式评审的可行性结论，见 research.md。
- Q: +25 净效用门是否足以约束路由精度？ → A: 不够。新增显式精度前沿 `56c−31h≥25`：c=BENEFIT 捕获率、h=HARM 触发率（分母用 held-out 实际计数），56/31 为历史锚。等价 harm 上限 `h≤(56c−25)/31`；c=0.70 时要求 h≤0.46。仅净效用达标但精度前沿不满足的 held-out 结果判 NO-GO，不得只看净效用。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 建立反事实效用真值集 (Priority: P1)

评测维护者希望把既有浅检索与深检索的逐题配对结果转成 BENEFIT、NEUTRAL、HARM 三类效用标签，从而直接测量“继续读取是否有用”，而不是继续拿浅答案错误或文本犹豫作为替代目标。

**Why this priority**: 没有正确的目标标签，任何门控信号即使能预测“答错”，也无法区分深检索会救回、无效还是反害；041 的根因正是这两件事被混为一谈。

**Independent Test**: 对一组逐题对齐、口径一致的浅/深结果执行标签构造，两次运行必须生成完全相同的三类标签、计数与输入来源摘要；历史 040 配对应能复现 56 个 BENEFIT 与 31 个 HARM。

**Acceptance Scenarios**:

1. **Given** 同一问题的浅检索和深检索结果均存在且判题口径一致，**When** 维护者构造效用标签，**Then** 每题恰好得到 BENEFIT、NEUTRAL、HARM 之一。
2. **Given** 任一问题缺少一侧结果、问题身份不一致或评测口径漂移，**When** 构造标签，**Then** 该输入被明确拒绝，不产生猜测标签。
3. **Given** 同一冻结输入重复处理，**When** 比较两次产物，**Then** 逐题标签、汇总计数与来源摘要完全一致。

---

### User Story 2 - 验证低成本概率信号能否预测效用 (Priority: P1)

评测维护者希望从浅答案正常生成过程中同步取得概率信号，并在完全隔离的 conversation 上验证这些信号能否识别“深检索会带来净收益”的问题，同时避开“深检索会反害”的问题。

**Why this priority**: 这是本方向的生死门。真实概率信号是 041 收口后唯一尚未验证的低成本置信来源，但它仍可能只反映浅答案置信度，而不反映深检索的反事实效用。

**Independent Test**: 使用冻结的效用标签与概率信号记录，以预注册的低容量校准规则按 conversation 留一验证；报告每一折及合并后的 BENEFIT 捕获、HARM 触发、NEUTRAL 触发、净效用、预算和校准稳定性。任何问题级随机切分或 held-out 后更换规则族都应被验证器拒绝。

**Acceptance Scenarios**:

1. **Given** 浅答案生成成功且概率信号可得，**When** 记录诊断样本，**Then** 样本只包含运行时可观察的信号、问题身份和成本数据，不包含 gold、judge verdict 或效用标签作为门控输入。
2. **Given** 全部诊断样本，**When** 执行校准与验证，**Then** 每个 conversation 只出现在训练侧或验证侧之一，且最终报告覆盖全部 conversation。
3. **Given** 概率信号缺失、部分缺失或无法对应最终答案，**When** 执行诊断，**Then** 该问题被标为 signal-unavailable 并计入覆盖率，不得用文本犹豫或自我判断填补。
4. **Given** 信号存在性 pilot 在前两条 conversation 上完成采集，**When** 评估固定 ridge 得分对 BENEFIT 的 AUC，**Then** AUC<0.65 或 BENEFIT/HARM 缺失致 AUC 不可定义时立即 valid NO-GO 且不启动全量 collect；AUC≥0.65 且类别齐全才授权全量 collect。pilot 本身不构成任何 held-out 成绩。
5. **Given** held-out 结果未达到预注册门槛，**When** 维护者形成 verdict，**Then** feature 以 NO-GO 收口，不启动完整门控评测。
6. **Given** held-out 净效用达标但精度前沿 `56c−31h≥25` 不满足，**When** 维护者形成 verdict，**Then** 判 NO-GO；不得只凭净效用晋级。c=0.70 时的 harm 上限为 h≤0.46。
7. **Given** 特征族、校准规则族和复杂度上限已冻结，**When** 查看 held-out 结果，**Then** 不得再增加特征、切换高容量分类器或引入 LLM router 来改善结果。

---

### User Story 3 - 以反事实效用门控浅→深检索 (Priority: P2)

只有在概率信号诊断通过后，评测维护者才验证两档策略：每题先用浅预算作答，预测深检索为 BENEFIT 时再使用深预算重答，否则保留浅答案。策略必须比固定深预算节省完整决策路径成本，同时保持深预算的答案质量。LoCoMo primary 通过后，维护者再以同一冻结策略验证 LongMemEval non-regression，区分单 benchmark 机制成立与跨 benchmark 可移植性。

**Why this priority**: 该步骤验证可部署的最终价值，但它不能先于信号诊断；否则会重复 041 在全量运行中才发现触发器无效的成本。迁移门同样不能先于 LoCoMo primary，否则会为已失败的机制继续消耗资源。

**Independent Test**: 在固定记忆库、问题集、answerer、judge、聚合口径和重复次数下，配对比较固定深预算与效用门控策略，报告逐题翻转、类别结果、总 token、调用次数和延迟。关闭门控时控制路径必须保持不变。LoCoMo 通过后，冻结信号家族、规则和质量/成本口径，在 LongMemEval 上重复 non-regression 配对，不使用 LongMemEval 标签重新选择策略。

**Acceptance Scenarios**:

1. **Given** 门控关闭，**When** 运行固定深预算控制，**Then** 行为和产物与当前控制路径一致。
2. **Given** 门控开启且某题被预测为 BENEFIT，**When** 处理该题，**Then** 系统最多执行一次浅作答和一次深作答，并记录触发依据与两次成本。
3. **Given** 门控开启且某题未被预测为 BENEFIT，**When** 处理该题，**Then** 系统保留浅答案且不执行深作答。
4. **Given** 运行时概率信号不可得，**When** 处理该题，**Then** 系统采用质量优先的固定深预算结果，并明确记录降级原因。
5. **Given** 完整配对评测完成，**When** 形成 verdict，**Then** 门控策略的正确题数不得低于同批固定 k150、绝对分必须达到 90%、平均完整决策路径 token 必须不超过固定 k150 的 60%，并由 BENEFIT/HARM 翻转、各类别表现、调用次数与延迟共同决定 GO/NO-GO；不能只凭上下文缩短或“不显著回退”晋级。
6. **Given** LoCoMo primary 未通过，**When** 执行 feature 流程，**Then** LongMemEval 迁移评测不启动。
7. **Given** LoCoMo primary 已通过且策略已冻结，**When** 运行 LongMemEval 迁移评测，**Then** 不得用 LongMemEval 结果重新选择信号、阈值或规则。
8. **Given** LongMemEval 未通过 non-regression 门，**When** 形成最终报告，**Then** 保留 LoCoMo 机制 verdict，但明确标记“不可移植、不得产品晋级”。
9. **Given** LongMemEval 通过 non-regression 门，**When** 形成最终报告，**Then** 可将结果标记为跨 benchmark 迁移验证通过；生产接线仍需独立 feature。

### Edge Cases

- 浅答案正确但置信度低，而深答案反而错误：必须计为 HARM，不得当作“合理加深”。
- 浅答案错误且深答案仍错误：必须计为 NEUTRAL；触发它只增加成本，不增加质量。
- 浅答案和深答案文本不同但都正确，或文本相同却 judge 标签波动：必须依赖冻结的配对判定规则并显式报告标签不稳定性。
- 最终答案极短、包含数字或专名时，概率信号可能受长度影响；报告必须包含长度分层，不能让长度成为隐藏路由器。
- answerer 含长 thinking 时，最终答案概率与完整生成概率可能方向相反；二者必须分开报告，不得事后挑选有利口径。
- 某 conversation 内 BENEFIT/HARM 极少或为零时，单折类别 rate 可能不可定义；报告必须保留该折，以 `null + reason` 标明不可定义项，不能静默丢弃。训练侧缺某一效用类别本身不使固定 ridge 失效；只有零可用训练行、数值失败、覆盖/来源不完整等协议错误才使 stage INVALID。
- 信号采集或门控本身引入的 token、调用与延迟必须计入处理成本，不能只计算最终答案上下文。
- 任一检索信号缺失时，实验必须保持现有逐信号降级语义，不得因门控新增整体失败。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 以 BENEFIT、NEUTRAL、HARM 三值标签表达深检索相对浅检索的逐题反事实效用。
- **FR-002**: 标签构造 MUST 要求浅/深结果逐题对齐且评测口径一致；缺失、重复、身份漂移或口径漂移 MUST fail closed。
- **FR-003**: 门控的预测目标 MUST 是反事实效用，而不是浅答案正确性、文本犹豫或模型自报置信度。
- **FR-004**: 首轮候选信号 MUST 来自浅答案正常生成时同步产生的真实概率信息，不得增加独立 closed-book answer pass。
- **FR-005**: 运行时门控输入 MUST label-blind，不得包含 gold、judge 结果、浅/深 correctness 或由它们派生的字段。
- **FR-006**: 校准和验证 MUST 按 conversation 隔离；同一 conversation 的问题不得跨训练侧与验证侧。
- **FR-007**: 诊断报告 MUST 分别给出 BENEFIT 捕获数、HARM 触发数、NEUTRAL 触发数、净效用和 signal availability，不得只报告准确率或 AUROC。
- **FR-008**: 概率信号不可用时 MUST 显式记录并采用固定深预算的质量优先降级，不得改用 041 已证伪的文本规则。
- **FR-009**: 诊断门未通过时 MUST 立即以 NO-GO 停止，不得实现或评测浅→深门控，也不得扩大到其他信号家族；诊断门通过时，MUST 在同一 feature 内继续完成门控实现与端到端验证。
- **FR-010**: 门控策略 MUST 限定为一次浅预算与至多一次深预算，不得无限循环或超过固定深预算上限。
- **FR-011**: 每个决策记录 MUST 包含问题身份、信号可用性、预注册信号摘要、决策、所用预算、调用次数、token、延迟和降级原因；若决策只保存信号 digest，MUST 能以 `shallow_attempt_id` 唯一连接到 public answer-attempt 中的原始三元组并复算。
- **FR-012**: 成本比较 MUST 覆盖完整决策路径，包括信号采集、浅作答与条件深作答的全部可计量输入/输出 token，以及任何额外门控调用；不得只报告最终上下文大小。固定本地 query-embedding sidecar 不贡献 generation-token ratio，但其调用次数与延迟 MUST 按臂单列，不能伪造成 provider 返回了 0 token；评测 judge 的固定开销不计入两臂运行时成本但 MUST 单列。
- **FR-013**: 完整评测 MUST 使用同记忆库、同问题集、同 answerer request shape、同与 store 相容的 embedder、同 judge regime（含 mem0-aligned 与 clean-final-answer mode）、同聚合口径进行控制/处理配对，并报告逐题翻转；任一冻结身份或模式不匹配 MUST fail closed。
- **FR-014**: 任一答案类别的实质性回退 MUST 单独报告并参与否决，不得由总体均值掩盖。
- **FR-015**: 新能力 MUST 默认关闭；关闭时不得改变现有产品或评测默认行为。
- **FR-016**: 本 feature MUST 保持为评测验证能力；生产接线与任何公共契约扩展均需独立 feature。
- **FR-017**: 所有模型侧能力 MUST 可由本地、可替换的 sidecar 提供；不得依赖付费云 reranker/recall 或默认联网能力。
- **FR-018**: feature verdict MUST 同时说明准确率、统计结论、类别结果、资源消耗与迁移边界；中间信号改善不得被报告为产品涨点。
- **FR-019**: 最终 GO MUST 同时满足同批配对正确题数不低于固定 k150 与绝对正确率不低于 90%；仅“未观察到统计显著回退”不足以晋级。
- **FR-020**: 最终 GO MUST 同时满足平均完整决策路径 token 不超过同批固定 k150 的 60%；调用次数与端到端延迟必须并列报告，不能用 token 达标掩盖调用或延迟失控。
- **FR-021**: LoCoMo MUST 是 primary 机制裁决；仅在 LoCoMo GO 后执行 LongMemEval non-regression 迁移门，且迁移阶段不得根据 LongMemEval 标签重新选择信号或规则。
- **FR-022**: LongMemEval 回退 MUST 阻止可移植能力声明与后续产品晋级，但不得改写已经冻结的 LoCoMo 机制 verdict。
- **FR-023**: 效用决策器 MUST 使用预注册的低容量校准规则；允许的信号特征族、规则族和复杂度上限 MUST 在 held-out 评估前冻结，禁止任意高容量分类器或 LLM/router 自主决策。
- **FR-024**: 每个 answer/judge 逻辑调用 MUST 最多执行 3 次 provider attempt；只有 timeout、network error、HTTP 429 和 HTTP 5xx 可重试，每次 attempt 的调用、usage 与延迟 MUST 写入 journal 并计入所属实验臂。其他 4xx、上下文超限、response/decode/schema/empty-answer 与 judge-parse 错误不得重试；重试耗尽或非可重试失败 MUST 使 stage INVALID。
- **FR-025**: 全量 collect 前 MUST 执行 2-conversation 信号存在性 pilot；pilot 以固定 ridge 得分对 BENEFIT 类别测 AUC，AUC<0.65 或 BENEFIT/HARM 缺类致 AUC 不可定义时 MUST 以 valid NO-GO 收口，不得进入全量 collect。
- **FR-026**: 门控路由 MUST 满足冻结精度前沿 `56c−31h≥25`（c=BENEFIT 捕获率、h=HARM 触发率，56/31 为历史锚，等价 harm 上限 `h≤(56c−25)/31`）；诊断与确认 verdict 不得只报告净效用而不校验 harm 上限。

### Key Entities

- **Paired Retrieval Outcome（配对检索结果）**：同一问题在浅预算和深预算下的冻结答案、判定及可比性元数据。
- **Counterfactual Utility Label（反事实效用标签）**：由配对结果唯一导出的 BENEFIT、NEUTRAL 或 HARM，不作为运行时输入。
- **Probability Signal Record（概率信号记录）**：浅答案正常生成时可观察的预注册概率特征、覆盖状态和生成成本，不含结果标签。
- **Signal-Existence Pilot（信号存在性 pilot）**：全量 collect 前在前两条 conversation 上执行的浅/深配对小批采集，输出固定 ridge 得分对 BENEFIT 的 in-sample AUC、类别计数与覆盖率；AUC≥0.65 且类别齐全才授权全量 collect。
- **Calibration Fold（校准折）**：以完整 conversation 为隔离单元的训练/验证划分及其冻结参数。
- **Calibrated Utility Rule（校准效用规则）**：从预注册概率特征映射到“保留浅答案/执行深检索”的低容量规则；其特征族、规则族和复杂度上限在 held-out 前冻结。
- **Utility Decision（效用决策）**：运行时对“保留浅答案”或“执行深检索”的一次决定及可审计依据。
- **Evaluation Receipt（评测回执）**：记录控制/处理口径一致性、逐题决策、翻转、类别指标、统计结论和完整成本。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 对所有可比的浅/深配对问题，100% 生成且仅生成一个效用标签；冻结输入重复处理时标签与汇总逐字节一致。
- **SC-002**: 诊断覆盖全部 conversation，并在每一折保持 conversation 零泄漏；signal-unavailable 问题占比和原因覆盖 100% 问题。
- **SC-003**: 在 fresh collect 的同批 k30/k150 配对语料上，10 个 LOCO folds 的 cross-fitted held-out 合并决策相对同批固定浅预算取得至少 +25 题净效用，且正确题数不低于同批固定深预算；任何 HARM 触发均从净效用中扣除。`+25` 是独立、刻意从严的效应量下限：若同批 deep 相对 shallow 的净值 `D<25`，policy 仍须比 deep 多至少 `25-D` 题；历史 040 不参与该门。同时 MUST 满足显式精度前沿 `56c−31h≥25`（c=BENEFIT 捕获率、h=HARM 触发率，56/31 为历史锚）；仅净效用达标而精度前沿不满足判 NO-GO。
- **SC-004**: 只有 SC-003 达标才允许在同一 feature 内实现并执行完整门控评测；未达标时立即以 NO-GO 收口、停止后续 story/tasks，且默认行为零变化。
- **SC-005**: 完整门控策略的答案质量达到 90% 以上，且同批配对正确题数不低于固定深预算；任一类别不得出现超出预注册容差的实质性回退。统计检验仍须报告，但不能替代正确题数硬门。
- **SC-006**: 完整门控策略的全部 generation 输入/输出 token（含浅作答、条件深作答、概率信号与任何额外模型门控开销，不含评测 judge）不超过同批固定深预算的 60%。固定本地 query embedding 的 ratio contribution 按协议为不适用，但 policy/control 的 embedding 调用次数、失败与延迟，以及全部 answer/judge/provider attempts，均须完整分臂报告。
- **SC-007**: 当概率信号不可用时，100% 的问题采用固定深预算降级并留下可审计原因，不产生静默浅答。
- **SC-008**: 门控关闭时，控制路径的逐题输入、答案与评测结果相对冻结基线无差异。
- **SC-009**: 最终 verdict 明确区分历史 label-constructor audit、fresh held-out 预测结果与 fresh 全量端到端结果，不把历史结果、oracle、训练折或中间指标表述为可部署成绩。
- **SC-010**: LoCoMo GO 后，LongMemEval 使用冻结策略完成 100% 同批可比问题的 non-regression 配对；若正确题数低于同批固定深预算，则迁移门失败并阻止可移植/产品声明。
- **SC-011**: 全部 held-out folds 使用同一预注册特征族、规则族与复杂度上限；评估开始后发生 0 次事后扩特征、换规则或引入 LLM/router 的变更。
- **SC-012**: 全量 collect 前，2-conversation pilot 在前两条 conversation 上得到固定 ridge 得分对 BENEFIT 的 AUC≥0.65 且 BENEFIT/HARM 类别齐全，才允许全量 collect；AUC<0.65 或不可定义时 pilot 产出 valid NO-GO，8/10 采集预算不支出，且 pilot 不作为任何 held-out 成绩。

## Assumptions

- “浅预算”与“深预算”首轮分别固定为 top-k 30 与 top-k 150；其他预算阶梯不属于本 feature。
- 历史 040 的 56 BENEFIT / 31 HARM 只用于验证标签构造并描述历史 prevalence，不作为 prior、训练行或 held-out 门输入；
  新的同批配对结果才是诊断与最终 verdict 的权威数据。
- 数据仅有少量完整 conversation，因此默认采用 leave-one-conversation-out，而非问题级随机划分。
- 概率信号以最终答案 token 的长度归一化概率、低概率尾部和候选概率间隔等预注册家族为起点；thinking 概率只作单独诊断，不与最终答案概率事后混选。
- 校准规则默认采用可审计的低容量模型族；具体模型形式、正则与阈值选择方法在 plan 中预注册，但不得扩大为任意分类器搜索。
- 评测 judge 的固定开销不属于运行时门控成本，但浅/深 answerer、信号采集及任何决策调用全部计入完整决策路径成本。
- 当前 answerer 的 Go 请求零值会被 OpenAI wire 的 `omitempty` 省略，因此冻结的 sampling recipe 是 `temperature_request_mode=omitted`，不是 `temperature=0`；有效服务端采样配置由 model revision 与 sidecar configuration digest 共同绑定。
- 正式 hybrid run 的 query embedder 必须与 store 的 embedding fingerprint/维度相容；endpoint、model revision 与 sidecar 参数只以非 secret 身份/digest 冻结，任何不匹配在首个 benchmark call 前拒绝。
- 若当前 answerer 端点不能返回可归因到最终答案的概率信号，本 feature 直接判为 signal-unavailable/NO-GO；不得为追求结果扩大为 provider 公共契约改造。
- 2-conversation pilot 使用 benchmark 顺序的前两条 conversation（`conversation_ids[0..1]`），该选择是预注册的确定性规则；pilot AUC 是 in-sample 存在性 kill-gate，本身不构成 held-out 成绩，也绝不授权除全量 collect 以外的任何 stage。
- `56c−31h≥25` 中的 56/31 是历史 label audit 必须复现的历史锚计数，只作为精度前沿的固定权重；历史行不进入训练或 held-out 行，pilot 与精度前沿的系数都不是训练参数。
- 本 feature 是研究与评测资产，不承诺进入产品默认路径。即使 verdict 为 GO，生产接线仍需新的契约优先 feature。
- LongMemEval 只承担迁移门，不参与 LoCoMo 阶段的信号家族、特征或阈值选择。
