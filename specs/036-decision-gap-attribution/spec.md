# Feature Specification: Decision-Gap Attribution(决策缺口归因)

**Feature Branch**: `036-decision-gap-attribution`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "基于 034/035 的 NO-GO 结论,冲击 LoCoMo 严格超过 90% 的下一步不是第二次更保守的证据裁决(035 已证明改选轴关闭:1539/1540 保留、唯一 1 次改选中性)。回到缺口本身:候选空间上界 1411/1540 而裁决只选对 1378/1540,33 题 '候选里有对的却没选对'。先做一个零模型调用的逐题归因诊断,按类别与失败模式把缺口拆开,作为后续喂原始 wording 或生成前置机制的、可审计的数据前提。"

## Decision and Scope

Feature 035 用 954 次双视图独立审计证明:在统一重建证据(E01–E30)下,父答案被严格反驳的比例只有 ~2%(10/473 + 16/467),满足"双视图一致反驳父答案 + 唯一替代被支持"的只有 1 个 packet,所以改选几乎不发生、发生也净零。**结论是:问题不在裁决的保守度,而在裁决输入的证据形态与候选生成质量。**

在投入任何新机制之前,本 feature 先把已 seal 的历史产物逐题展开,归因那 33 题缺口(1411 oracle − 1378 selected)。它回答三个问题:

1. **证据不足**:证据 E01–E30 下裁决器无法区分正确候选与强父答案 —— 指向后续喂血缘原始 wording(022/MemOS 方向)。
2. **证据自洽但候选事实错**:被选中的候选在证据下站得住但事实错 —— 指向生成前置(035 failure analysis 的 "answer generation before adjudication")。
3. **语义等价混淆**:正确候选与选中的候选规范化后语义等价,裁决选哪个都像 —— 指向候选生成多样性与答案规范化。

本 feature 是 **benchmark-only 的纯诊断**:零模型调用、零决策变更、零新基线。它只读 034 的 seal/manifest/packets/decisions/custody + 三份候选结果 + 035 的 audit 诊断,输出可审计的逐题归因清单与聚合分布。它不发起 rejudge,不改变任何既有 artifact,归因结果仅作为后续机制 feature(独立 spec)的数据决策输入。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 逐题缺口重建与归因清单 (Priority: P1)

评测维护者从 034 的有效 public artifacts + seal + sealed decisions + 三份候选结果中,逐题重建三方状态:文本确定性 control(`adjudicationTextControlSlot`)、candidate oracle(任一候选判对)、034 裁决选择。输出全部 1540 题的逐行明细,并标记缺口(候选空间有对的但裁决没选对)。缺口进一步拆为 control-only loss 与 both-wrong/third-candidate。

**Why this priority**: 归因的第一性要求是把"33 题缺口"从聚合数字变成可逐行审计的清单。没有逐题明细,任何"证据不足/事实错/语义等价"的归类都是拍脑袋。

**Independent Test**: 完全离线的固定 fixture 上,脚本逐题重建的三方状态与手工判定一致;33 题缺口精确复现 1411 − 1378;13 个 control-only loss 与 9 个 both-wrong 的归属与 034 `verdict.md` 报告的分解一致;改乱 hidden verdict 字段不改变任何归因行。

**Acceptance Scenarios**:

1. **Given** 完整且有效的 034 public artifacts、seal、decisions 与三份候选结果,**When** 运行归因,**Then** 输出 1540 行逐题明细,含 question_id / category / control对错 / selected对错 / oracle / 正确候选slot / evidence_ids / confidence,缺口行被标记。
2. **Given** 任一题 candidate oracle=1 且 selected=0,**When** 归因,**Then** 该题进入缺口集,并按 control 对错分为 control-only loss 或 both-wrong/third-candidate;总缺口数 = 1411 − 1378 = 33。
3. **Given** 034 的 seal/decision/packet 任一 digest 被篡改,**When** 运行归因,**Then** 在读取 hidden verdict 之前 fail closed,零归因输出。
4. **Given** 原始候选正确性/gold/historical score 被修改,**When** 重建归因,**Then** 逐行明细与缺口集不变,归因过程没有读取这些字段来驱动任何决策。

---

### User Story 2 - 类别 × 失败模式聚合 (Priority: P1)

维护者按 packet 的 Category(1–4:fact_lookup / multi_hop / temporal_state_tracking / preference_recall)对缺口集聚合,并叠加失败模式(证据不足 / 事实错 / 语义等价),得到类别分布表。

**Why this priority**: 缺口只有 33 题,需要先看它们是否集中在某一类别(例如 multi-hop 或 temporal)——这直接决定后续机制落在哪个检索类别上,避免对全类别做无差别改动。

**Independent Test**: fixture 上类别分布表逐格与手工统计一致;总行数 = 缺口数;失败模式归类可复核(每行给出判定依据字段)。

**Acceptance Scenarios**:

1. **Given** 缺口清单,**When** 按 Category 聚合,**Then** 输出 4 类各自的缺口数、control-only loss 数、both-wrong 数,四类之和等于 33。
2. **Given** 每个缺口行,**When** 归类失败模式,**Then** 依据可复核:evidence 是否覆盖正确候选的引用、正确候选与 selected 的规范化答案是否等价、035 审计下父答案是否被反驳。
3. **Given** 任一类别缺口为 0,**When** 聚合,**Then** 该类别在表中为 0 行且明确标注,不参与后续机制决策。

---

### User Story 3 - 035 审计交叉验证 (Priority: P2)

维护者把 035 的 audit 诊断(477 风险题、父答案被反驳的 10/473 + 16/467、唯一替代的 4/5)与缺口清单交叉:每个缺口题在 035 双视图下是"父答案被反驳"、"父答案未被反驳"还是"不在 477 风险队列"。

**Why this priority**: 035 已经证明"即使双视图独立审查,父答案也极少被反驳"。交叉表能直接看出:33 个缺口里有多少个其实 035 审计已经认为父答案有问题(只是没到双收敛) —— 若这类很多,说明问题在证据形态;若极少,说明父答案在证据下完全自洽(候选本身或证据问题)。

**Independent Test**: 交叉表行数 = 缺口数,每行带 035 审计状态;fixture 上状态判定与 035 审计 seal 一致。

**Acceptance Scenarios**:

1. **Given** 有效 035 audit seal,**When** 交叉,**Then** 每个缺口题标注 035 状态(风险队列内外 / 父答案是否被反驳 / 是否有唯一替代),总数与缺口数一致。
2. **Given** 035 seal 无效或缺失,**When** 交叉,**Then** 该列为空并明确标注,不阻塞 US1/US2 输出,也不假装有 035 审计结论。
3. **Given** 035 审计存在,**When** 归因完成,**Then** 输出三类占比结论(证据不足主导 / 事实错主导 / 语义等价主导),供后续机制 spec 决策。

### Edge Cases

- 三个候选规范化后两两等价:缺口分类归入"语义等价混淆",并给出三者 digest。
- 正确候选与 selected 是同一规范化答案但不同 slot(judge-instability):不视为缺口,单列 instability 行。
- selected 为 fallback(triggered + fallback_reason 非空):单独标记,不与 override 缺口混计(verdict.md 已证明 fallback 全救也只 +5)。
- evidence_ids 为空或含未知 ID:标记为证据不可定位,归入"证据不足"候选。
- 034 seal 有效但 035 目录缺失:US3 输出空列并标注,不 fail 整体。
- 归因输出不得包含 gold 文本、原始 verdict 或 credential;只允许 frozen digest 与判定状态。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 只读复用 `loadAndValidateAdjudicationPublic` + `loadAdjudicationHiddenInputs`(`cmd/locomo-bench/answer_adjudication_artifact.go`),在任何 hidden verdict 读取前完整验证 034 的 manifest/packets/decisions/seal/custody;验证失败 MUST fail closed 且 hidden reads 为零。
- **FR-002**: 系统 MUST 逐题重建文本确定性 control(`adjudicationTextControlSlot`)、candidate oracle(任一候选判对)、034 裁决选择(`SelectedSlot`),并对每行输出 question_id/category/三方状态/evidence_ids/confidence/fallback_reason。
- **FR-003**: 缺口定义 MUST 为 oracle=1 ∧ selected=0;缺口 MUST 进一步拆为 control-only loss(control 对 selected 错)与 both-wrong/third-candidate(control 错 selected 错但第三候选对),总缺口 MUST 精确等于 1411 − 1378 = 33(fixture 或真实数据上可复算)。
- **FR-004**: 失败模式归类 MUST 可复核:证据不足(evidence 未覆盖正确候选引用或无法定位)、事实错(selected 证据自洽但非正确)、语义等价混淆(正确候选与 selected 规范化后等价);每行 MUST 携带判定依据字段。
- **FR-005**: 类别聚合 MUST 使用 packet.Category(1–4),输出 4 类分布表,四类之和等于缺口总数。
- **FR-006**: US3 交叉 MUST 读 035 audit seal(若有),标注每缺口题的 035 状态;035 目录无效/缺失时该列 MUST 为空并显式标注,不得阻塞 US1/US2。
- **FR-007**: 本 feature MUST 不发起任何 provider 调用、不修改任何 034/035 artifact、不改变默认 benchmark 或既有 CLI 模式(默认路径 byte-identical),MUST 不接触 `memory/ embedding/ provider/ store/ internal/` 任何 `.go`。
- **FR-008**: 归因 MUST 是追加式新文件(`cmd/locomo-bench/answer_adjudication_attribution*.go`)加 minimal `main.go` flag/dispatch;离线 fixture 测试 MUST 离线、确定性、可复算。
- **FR-009**: 归因结果 MUST 标记为诊断(`decision_gap_attribution`),不得作为正式 LoCoMo 分数,不得触发 rejudge,不得作为 tuned-threshold 的依据。
- **FR-010**: 输出 MUST 不包含 gold 文本、原始 verdict、credential、raw endpoint/provider response/error;provider-facing 字段 MUST 保持 frozen digest。

### Key Entities

- **Gap Row**: 一个缺口的逐题明细,含 question_id/category/control 状态/selected 状态/oracle 状态/正确候选 slot/evidence_ids/confidence/失败模式/035 审计状态。
- **Failure Mode**: 证据不足 / 事实错 / 语义等价混淆 三分类(可复核、逐行判定)。
- **Attribution Report**: 聚合产物:1540 行明细 + 33 缺口 + 类别×模式分布表 + 035 交叉表 + 结论(某类主导)。
- **034 Baseline Receipt**: 归因的合法输入基线(与 034/035 相同);hidden verdict 只在 seal 验证后读取。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 1540 行逐题明细在 fixture 上可复算,每行三方状态与手工判定一致;33 缺口精确复现。
- **SC-002**: control-only loss 与 both-wrong/third-candidate 的归属与 034 `verdict.md` 报告分解(13 / 9)一致。
- **SC-003**: 类别×失败模式分布表逐格可复核,四类之和 = 33;结论(证据不足/事实错/语义等价 主导)有逐行依据。
- **SC-004**: 035 交叉表每缺口题带 035 状态;035 缺失时列空并标注,不阻塞。
- **SC-005**: 全程零 provider 调用、引擎目录变更数为零、默认 benchmark/既有 CLI 模式 byte-identical;全部离线测试在无网络环境通过。
- **SC-006**: 归因输出不包含 gold/verdict/credential,缺口的逐行判定依据完整可审计。

## Assumptions

- 034 的 seal/manifest/packets/decisions/custody 与三份候选结果文件来自维护者指定的位置(本 feature 开发阶段可用离线 fixture 先完成脚本与测试,真实 034/035 数据后补);035 的 audit seal 可选。
- 归因是纯诊断,不改变任何决策;后续"喂血缘原始 wording"或"生成前置"是独立 feature spec,本 feature 只产出数据前提。
- 失败模式归类依赖自动化判定(evidence 引用覆盖 + 规范化等价 + 035 审计状态),在少量难判行上允许 fixture 标记为 "unclear" 并计入结论的置信度说明,不做人工逐题调参。
- 不引入任何 paid 调用;不把 hosted 裁决当作 shipped/default 检索杠杆(Constitution I & V)。
- 现有 E01–E30 是统一重建证据,不是三个历史 answerer 的精确原始上下文;8/1540 context parity 例外继续作为 provenance limitation,不作为归因条件。
