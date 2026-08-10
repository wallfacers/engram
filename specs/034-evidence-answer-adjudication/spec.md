# Feature Specification: Evidence-Grounded Answer Adjudication

**Feature Branch**: `worktree-034-evidence-answer-adjudication`

**Created**: 2026-08-10

**Status**: Draft

**Input**: User description: "在 033 chunk-first 排序 NO-GO 后继续冲击 LoCoMo 严格超过 90%；不使用付费云 reranker/recall，利用历史 deepseek-v4-pro 三次答案中已存在的候选空间，构建 gold-blind、evidence-grounded 的候选答案裁决，并先用冻结 Stage-0 止损门验证。"

## Decision and Scope

033 已证明在当前 top-30、约 3.3k answer context 与强 answerer 下，仅调整 chunk/fact 顺序不能转成
端到端答分。历史 89.03 三次结果却存在明确的答案侧空间：三个旧 judge verdict 的多数为 1371/1540，
而每题三个候选中至少一个已判对的诊断上界为 1411/1540（91.62%）。1371 是 verdict 聚合，不对应一个
可执行的“多数答案”。本 feature 只验证一件事：在不读取 gold、judge 标签或 correctness 的前提下，
裁决器能否依据同配方冻结的 canonical evidence，从三个既有答案中选出更可靠的一个。

范围内：

- 从冻结的三次候选答案与同配方 canonical evidence 构造 label-blind 裁决输入；历史结果未保存每个
  candidate 的完整 prompt，故不得声称 canonical evidence 与三次原始 context 逐字相同；
- 仅以候选文本本身决定是否点火，并对候选顺序做可复算的盲化；
- 让显式可选的 answer-side verifier 引用证据后选择候选；
- 输出封存后才连接隐藏 verdict，执行 Stage-0 上限验证与正式晋级门；
- 完整记录输入、输出、模型、调用量、费用、失败回退和污染审计。

范围外：

- 修改检索、召回、embedding、reranker、store、抽取、curation 或证据排序；
- 把 judge/correctness/gold answer/gold evidence 用于点火、候选排列、提示或运行时选择；
- 生成新的 caption/event-ledger 候选来挽救本轮；这些属于后继 feature；
- 把候选 oracle 或旧 verdict 映射结果直接登记为新的正式 LoCoMo 分数；
- 把托管 answer-side verifier 描述为本地默认产品能力。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 冻结无标签裁决输入 (Priority: P1)

评测维护者能够从三份冻结候选结果中得到一个完全不依赖 gold/judge/correctness 的执行清单。候选
文本经固定规范化后不全相同才点火；证据、问题和三个候选被封装为可复算 packet，重复候选本身是
合法的 label-blind 自一致性信号，但原始 run 身份与隐藏评测字段不进入 packet 或执行 manifest。

**Why this priority**: 96 道 mixed-verdict 题是事后标签分层，若直接拿它选题就是泄漏。只有先冻结
label-blind 点火与 packet，后续任何涨点才具有证据意义。

**Independent Test**: 在离线环境中构造 packet；任意修改 gold、judge verdict、correctness、历史多数
标签或 run 顺序标识，执行清单、packet bytes 与调用数保持不变；修改候选文本时才允许变化。

**Acceptance Scenarios**:

1. **Given** 三份完整候选结果，**When** 生成执行清单，**Then** 点火只由三个候选的规范化文本是否
   不全相同决定，不读取任何评测标签。
2. **Given** 同一输入与冻结随机种子，**When** 重复生成 packet，**Then** question、evidence、候选排列、
   packet digest 与 manifest 逐字节一致。
3. **Given** gold、correctness、judge verdict 或历史多数标签被任意替换，**When** 重新生成，**Then**
   执行 cohort、候选排列、packet 与调用计数完全不变。
4. **Given** 任一题缺少候选、证据、问题或来源收据，**When** 生成或验证 packet，**Then** fail closed，
   整个 decision set 不得发起 verifier 调用，并在有效性报告中明确缺口。

---

### User Story 2 - 基于证据选择答案并安全回退 (Priority: P1)

对 label-blind 点火题，裁决器只看到问题、冻结证据和盲化后的候选答案，必须给出一个候选选择及证据
依据。无法解析、越界、缺少依据、调用失败或置信不足时，系统回退冻结的 label-blind 文本规则：规范化
文本支持数最高者优先，三方并列时按候选原文的固定词典序选择；不得读取 verdict，也不得产生第四个答案。

**Why this priority**: 候选 oracle 只说明空间存在；要兑现空间，选择必须由证据支持，同时不破坏
未点火或不确定题。它是本 feature 唯一会产生模型调用的处理路径。

**Independent Test**: 使用完全离线的固定 verifier stub 覆盖合法选择、候选置换、无效索引、空引用、
超时与失败；断言选择映射正确、回退确定、未点火题逐字节不变且无额外调用。

**Acceptance Scenarios**:

1. **Given** 一个合法 packet，**When** verifier 返回有效候选索引与可定位证据依据，**Then** 选择对应
   候选，并记录盲化索引到原候选的映射和完整收据。
2. **Given** 候选排列发生可复算置换，**When** verifier 选择同一语义候选，**Then** 最终候选内容不因
   槽位变化而改变，固定槽位不能代表原 run；重复候选的出现次数可以作为自一致性信号，但不能还原
   其 run 来源。
3. **Given** verifier 返回非法索引、自由生成答案、空依据、低置信或调用错误，**When** 处理结果，
   **Then** 100% 回退冻结的确定性文本 control 并记录原因。
4. **Given** 三个规范化候选相同，**When** 执行 adjudication，**Then** 不调用 verifier，保持既有结果。

---

### User Story 3 - 封存后评分与严格晋级 (Priority: P2)

评测维护者在全部选择输出封存并计算 digest 后，才将其与隐藏 verdict 连接。Stage-0 明确区分候选
上界、旧标签映射诊断和可登记的正式结果；未过门立即停止，过门后才允许设计同时间窗口的正式配对。

**Why this priority**: 旧 judge 对相同规范化答案存在不一致标签，直接映射旧 verdict 只能止损，不能
注册新分。封存后评分与独立重判边界必须先写死，避免把后验选择包装成 >90。

**Independent Test**: 对固定隐藏标签 fixture，验证输出 digest 先于 join 产生；缺失输出、额外输出、
重复题、packet digest 不匹配或提前加载标签都会使 Stage-0 无效；合法输入准确复算 overall、类别、
paired flips、回退和调用量。

**Acceptance Scenarios**:

1. **Given** 所有执行输出已封存，**When** 分析器连接隐藏 verdict，**Then** 分开报告 1540 题历史
   verdict-majority（1371）、可执行确定性文本 control、candidate oracle、点火覆盖、mixed-verdict 分层、
   处理后总分、类别与逐题翻转。
2. **Given** 历史冻结输入，**When** 运行 Stage-0，**Then** 只有在处理后至少 1387/1540，且 label-blind
   点火覆盖的 88 个 informative mixed-verdict rows 中至少选对 69 个时才判 GO；否则 NO-GO。
3. **Given** Stage-0 GO，**When** 申请正式结果，**Then** 必须用同一候选生成、同一时间窗口与独立重判
   的 control/treatment 协议；旧 verdict 映射结果不得进入当前结果正本。
4. **Given** 任一类别出现统计可信的净回退或任一污染/完整性门失败，**When** 裁决，**Then** 停止晋级，
   不以总体分掩盖回退。

### Edge Cases

- 三个候选大小写、标点或空白不同但规范化后相同：不点火；旧 judge 标签若不同，只记 judge instability。
- 两个候选规范化相同、第三个不同：可以点火；三个盲化候选会自然暴露 2:1 的文本自一致性，但不得
  暴露重复候选来自哪些 run，也不得把多数状态当作隐藏 correctness 标签。
- 三个候选均为空、IDK 或格式损坏：build/validation 无效，禁止任何调用；不得用 fallback 掩盖损坏输入。
- 证据中不存在支持任一候选的内容：verifier 必须选择不确定/回退，不能生成新答案。
- 三个候选规范化后仍两两不同：文本 control 按候选原文固定词典序选择，不得以输入文件顺序打破平局。
- verifier 输出自由文本答案而非候选索引：视为非法，不接受相似度猜测。
- API 限流、超时、部分完成或中断恢复：已完成结果不可覆盖；恢复必须校验 packet/model/prompt digest。
- 相同规范化答案对应不同旧 judge verdict：无论整题是否点火，都进入 judge-instability 敏感性；不把
  verifier 在同文槽位间的任意选择当作真实能力。冻结 artifacts 中共有 13 题，其中 5 题处于点火 cohort。
- 运行时输入意外包含 gold、evidence label、correctness 或 judge verdict：污染扫描必须在任何调用前
  fail closed。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 接受三份完整、同口径候选结果及其同配方 canonical evidence 收据，并在任何
  调用前验证题目集合、question/category、候选来源、answer regime、retrieval flags 和输入 digest 一致；
  不一致 MUST fail closed。旧 artifacts 未被正式 protocol 密码学绑定时，模型身份 MUST 标为 legacy
  operator claim 而非已验证 provenance；若原 candidate prompts 未被保存，系统 MUST 明示 evidence 仅为
  同配方重建，不得声明逐字等价。
- **FR-002**: 点火规则 MUST 只使用候选文本：lowercase 后移除非字母数字字符，三个规范化字符串不全
  相同才点火；规则 MUST NOT 读取 gold、judge verdict、correctness、历史多数或类别表现。
- **FR-003**: 每个执行 packet MUST 只包含 question、冻结 evidence、三个候选、盲化槽位和协议字段；
  重复候选文本可以保留为自一致性信号，但 MUST 排除 gold answer、gold evidence、judge/correctness、
  原 run id 与任何可还原隐藏标签的字段。
- **FR-004**: 候选排列 MUST 由与标签无关、可复算的冻结种子决定；同输入重复运行 bytes/digest 一致，
  任一固定槽位 MUST 不稳定对应原 run 或 majority 状态。
- **FR-005**: verifier MUST 只能选择 packet 内已有候选，并返回候选索引、证据依据和置信状态；系统
  MUST NOT 接受或合成第四个答案。
- **FR-006**: 非点火题、非法/低置信 verifier 输出和 provider 失败 MUST 100% 回退冻结的 label-blind 文本
  control（规范化支持数优先、并列按候选原文词典序），且未点火题不得产生 verifier 调用；packet/manifest
  完整性失败 MUST 阻断整个执行与 seal，不得降级成逐题 fallback。
- **FR-007**: 执行结果 MUST 在连接隐藏 verdict 前封存并生成不可变 digest；Stage-0 分析 MUST 拒绝
  缺失、重复、额外、digest 不匹配或先验标签可见的结果。
- **FR-008**: Stage-0 MUST 分开报告历史 verdict-majority、可执行文本 control、candidate oracle、旧
  verdict slot-mapping 诊断、judge-instability 敏感性和可登记正式结果；judge-instability MUST 包含所有
  “同一规范化答案对应多个旧 verdict”的题，并分别报告点火/未点火子集；旧 verdict 映射 MUST NOT 被
  称为新的 LoCoMo 分数。
- **FR-009**: 冻结历史口径的 Stage-0 GO 门 MUST 同时要求：处理后至少 1387/1540、informative
  mixed-verdict 分层至少 69/88 选择正确、类别无统计可信净回退、污染/完整性全绿；任一失败即停止。
- **FR-010**: Stage-0 GO 后的正式验证 MUST 固定候选生成、answerer、verifier、judge、prompt、随机种子、
  重试、并发、时间窗口和费用口径，并报告同一候选 triplet 上 control/treatment 的逐题配对结果。
- **FR-011**: 本 feature MUST 不改变检索候选、检索打分、上下文证据、召回或排序，不得调用或依赖
  付费云 reranker/recall。
- **FR-012**: 托管 answer-side verifier MUST 显式 opt-in、默认关闭；packet 生成、离线 stub、分析与
  所有合同测试 MUST 在无网络环境完整运行。
- **FR-013**: API key MUST 只经环境进入 provider；不得写入 packet、manifest、日志、结果、错误消息、
  tracked 文件或工具响应。
- **FR-014**: 本 feature MUST 不修改 `memory/`、`embedding/`、`provider/`、`store/`、`internal/`
  下的运行代码；只允许 benchmark/SaaS evaluation surface 与 feature 文档变化。
- **FR-015**: 每次执行 MUST 记录 packet/prompt/model/binary digest、计划/实际调用数、input/output token、
  retry、fallback、exit 和 provider 账单对账字段；未定价模型的本地 `0` 不得表述为免费。

### Key Entities

- **Candidate Triplet**: 同一题在冻结协议下得到的三个候选答案及其来源收据；隐藏 verdict 不属于运行实体。
- **Deterministic Text Control**: 不读取标签的可执行候选选择；规范化文本支持数最高者优先，三方并列按
  原文词典序；它与历史 verdict-majority 是两个不同口径。
- **Trigger Decision**: 仅由候选规范化文本导出的点火/不点火决定，含可复算理由与 digest。
- **Adjudication Packet**: question、冻结 evidence、盲化候选槽位和协议元数据；不含任何评测标签。
- **Adjudication Decision**: verifier 对 packet 内候选的选择、证据依据、置信状态、调用/回退收据。
- **Sealed Decision Set**: 全部执行输出的不可变集合与 digest，是隐藏评分 join 的唯一前置输入。
- **Hidden Score Join**: 输出封存后执行的诊断评分，将选择映射到历史 verdict，并单独标记 judge instability。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 冻结 1540 题候选输入验证通过；label-blind 规范化点火数为 771/1540，历史 verdict-majority
  为 1371/1540，可执行确定性文本 control 复算为 1368/1540；重复构建 packet 与 manifest 逐字节一致，
  隐藏标签字段出现数为 0。
- **SC-002**: 对 gold、judge verdict、correctness、历史多数和原 run 标识进行任意变异后，点火 cohort、
  packet bytes/digest 与计划调用数保持 100% 不变。
- **SC-003**: 离线合同覆盖合法选择、候选置换、重复候选、非法索引、空引用、低置信、超时、部分恢复
  和污染输入；所有无效路径 100% 确定性回退，网络调用数为 0。
- **SC-004**: Stage-0 分析完整覆盖 1540/1540 题、771/771 点火决策与全部执行收据；冻结 artifacts 的
  judge-instability 必须复算为 13 题（其中 5 题点火）；缺失/重复/额外或 digest 不匹配均使结果无效。
- **SC-005**: Stage-0 只有在处理后至少 1387/1540 且 informative mixed-verdict 至少 69/88 正确、类别
  non-regression 和污染门全绿时判 GO；否则登记 NO-GO 并停止正式付费全量。
- **SC-006**: 若晋级正式验证，treatment 绝对结果严格超过 90.00%，同候选配对净正，且没有多重校正后
  `p < 0.05` 的净负类别；完整报告调用、token、费用和独立重判口径。
- **SC-007**: 引擎目录变更数为 0；默认 benchmark、MCP、CLI 和 SDK 行为变化数为 0。

## Assumptions

- 历史 deepseek-v4-pro 三跑、对应 top-30 证据与 store 可由维护者在本地提供，并能冻结 digest；路径不是
  对外契约，缺失时 Stage-0 不执行。
- 三跑结果只有 `answer_context_tokens`，没有逐题 hit/context journal；其中 8 题的 token 数跨 run 不同。
  因此 verifier 使用 attribution trace + 同配方 store 重建的 canonical evidence，不能声称复现每个候选
  的原始 prompt。该限制必须进入 receipt 与最终报告。
- 三跑是 legacy ordinary-result artifacts，没有正式 protocol manifest 将模型 revision 与逐题输出做
  密码学绑定；deepseek-v4-pro 身份来自同期归档说明，只能登记为 operator claim，不能升级为正式凭证。
- 1411/1540 是使用隐藏 verdict 计算的诊断 oracle，只证明候选空间，不是运行输入或可登记结果。
- 771 点火、88 informative mixed-verdict 与 69 正确门只对冻结 89.03 artifacts 成立；输入 digest 变化时
  必须重新预注册，不能硬套旧分母。
- answer-side verifier 可以使用用户显式授权的托管模型，但它是 benchmark/SaaS opt-in，engram 本地引擎
  与默认产品路径保持离线。
- Stage-0 首先复用旧 candidate verdict 止损；冻结输入中有 13 题出现同一规范化答案对应不同 verdict
  （5 题已点火），因此任何正式 >90 声明都需要新的、冻结的独立重判协议。
- caption late-binding、event ledger、新候选生成和 agent 检索均不在 034；若 034 NO-GO，必须另立 spec。
