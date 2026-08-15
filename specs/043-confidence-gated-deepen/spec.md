# Feature Specification: confidence-gated gap-guided deepening

**Feature Branch**: `043-confidence-gated-deepen`

**Created**: 2026-08-15

**Status**: Draft

**Input**: User description: "confidence-gated gap-guided deepening:LoCoMo k30 unified clean 87.9% → 90pp+ 的通用机制。默认 k30 + unified answer contract 字节不动;answerer 犹豫信号(040 验证:93% 信息不足时表达犹豫)低置信时触发一轮结构化 gap 补检(S2G-RAG 式通用 schema:category/target/slot,领域无关),确定性映射为补检查询,追加证据重答一次。round-0 chunk-quota 冻结。硬门:自带 2-conv 信号质量 pilot(AUC<0.65 即 NO-GO);008 铁律配对(同批+3-rep+store 复用+clean 口径);LME 零重调迁移门。与已证伪 021(slot 挤压 round-0)/029(自由导航)的机制区分必须写明。禁止:category 路由/测试集示例/固定拒答措辞/云端 reranker。文献依据 docs/research/low-topk-recall-context-survey-2026-08-15.md(S2G-RAG arXiv:2604.23783);040 verdict 理论上限 91.75% @ 4.8× 预算。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 犹豫信号质量 pilot(止损门) (Priority: P1)

维护者在小规模(2 个对话)上先验证"answerer 的犹豫表现能否区分'证据足够'与'证据不足'"——040 已观测到 93% 信息不足时 answerer 表达犹豫、仅 7% 自信地错。本故事产出该信号的检出质量指标(AUC),**不通过(AUC<0.65)则整个 feature NO-GO 关闭**,不进入任何后续开发。

**Why this priority**: 042 counterfactual 信号已因 5/5958 NO-GO 关闭,证明"信号质量前置门"是这条路线最大的风险点。先花最小成本验信号,避免在坏信号上盖机制。

**Independent Test**: 在 2-conv pilot 上计算犹豫信号对"最终答错/证据不足"的 AUC;单独交付一份 go/no-go 判定,不含任何机制代码。

**Acceptance Scenarios**:

1. **Given** 2 个对话的 k30 unified 基线 run 与逐题对错标签, **When** 计算犹豫信号(文本犹豫标记或答案 token 置信)对错题的判别力, **Then** 产出 AUC + 置信区间;AUC≥0.65 → GO 进入 Story 2,AUC<0.65 → NO-GO 并记录关闭理由。
2. **Given** 信号解析在部分题目上缺字段/不可解析, **When** 统计解析覆盖率, **Then** 覆盖率与 AUC 一并报告;解析失败题按"自信"(不触发加深)处理并在报告中单列。

---

### User Story 2 - 单轮 gap 导向加深机制(默认关) (Priority: P2)

在评测 harness 中实现核心机制:默认配方(k30 + unified 契约 + round-0 chunk 配额)**字节不变**;当犹豫信号低于阈值,由 answerer 输出**结构化缺口**(领域无关 schema:category ∈ {bridge_entity, attribute, relation, evidence_span, other} + target + slot + description),系统把缺口**确定性拼接**为一次补检查询,追加证据后重答一次,以重答为最终答案。全程一轮,无多轮导航。

**Why this priority**: 这是把 040 的"79% 是上下文量问题"诊断转化为机制的唯一主杠杆;对需要佐证的题按需拿到高预算效果,平均检索量仍≈k30。

**Independent Test**: harness 旗标(默认 off)开启后与关闭臂同批配对跑 LoCoMo 全量 3-rep;单独交付机制臂 vs 对照臂的配对差值。

**Acceptance Scenarios**:

1. **Given** 旗标关闭, **When** 运行评测, **Then** 逐 token 与现行 k30 unified 配方完全一致(默认路径零扰动)。
2. **Given** 旗标开启且某题犹豫信号低于阈值, **When** 加深轮执行, **Then** round-0 证据在最终上下文中**原样保留**(补检证据只追加、不替换、不重排 round-0),且最多执行一轮加深。
3. **Given** 旗标开启但信号高于阈值, **When** 处理该题, **Then** 行为与对照臂一致(不额外检索、不重答)。
4. **Given** 加深轮补检返回空结果或查询构造失败, **When** 处理该题, **Then** 回退为 round-0 答案,不报错、不重试。

---

### User Story 3 - 零重调迁移验证(LME) (Priority: P3)

用 LoCoMo 上定的全部阈值/参数,**不做任何重新调整**,直接在 LongMemEval-S(k30, unified, clean, 3-rep)上配对验证同一机制:不回退基线(90.2% 锚)即通过迁移门。

**Why this priority**: 维护者红线——机制须换场景零重调成立(042 先例);不过迁移门的机制不得声称可移植,不得设默认。

**Independent Test**: 单独跑 LME 配对批;交付迁移门判定(非回退 vs 90.2% 锚,噪声带内或以上)。

**Acceptance Scenarios**:

1. **Given** LoCoMo 定稿的阈值与 schema, **When** 在 LME 同配方运行机制臂, **Then** 机制臂 clean 3-rep 不显著低于 90.2% 锚(非回退)。
2. **Given** LME 迁移通过, **When** 汇总双数据集结果, **Then** 产出可入 result-matrix 的配对行(含 p 值、context parity、每题翻转清单)。

---

### Edge Cases

- 犹豫信号解析失败/字段缺失 → 按自信处理(不加深),解析覆盖率单独统计上报(Story 1 场景 2)。
- 加深查询构造不出有效短语(target/slot 皆空)→ 回退用原问题作为补检查询;仍失败则不加深。
- 补检证据与 round-0 重复 → 按条目去重后追加,不占用 round-0 配额。
- 加深轮重答与 round-0 答案冲突 → 以重答为最终答案(机制定义),逐题记录两答以供审计。
- 犹豫阈值边界(恰好等于阈值)→ 归入"不加深"(保守侧,降低误触发)。
- 加深触发率过高(如 >50% 题触发)→ 视为信号校准失败,pilot 门应拦截;机制侧硬性不做自适应调整(避免拟合数据集)。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 提供犹豫信号质量的独立评估路径(2-conv pilot),产出信号对错题判别力的 AUC 与解析覆盖率,且该评估 MUST NOT 依赖任何机制代码。
- **FR-002**: 犹豫信号 pilot 的 GO 阈值 MUST 为 AUC≥0.65;低于阈值 MUST 产生 NO-GO 判定并阻止后续机制开发(硬门)。
- **FR-003**: 加深机制 MUST 以默认关闭的旗标提供;旗标关闭时评测路径 MUST 与现行 k30 unified 配方逐字节一致。
- **FR-004**: unified answer contract 的文本 MUST 保持字节不变(契约 digest `1d8a8d0f` 不变);加深轮 MUST NOT 引入任何新的答题 prompt 措辞(缺口 schema 是输出格式契约,不是答题 prompt)。
- **FR-005**: 缺口 schema MUST 为领域无关的通用槽位(category/target/slot/description),MUST NOT 含数据集、类别(categorical)路由、或任何看过测试集后写定的示例。
- **FR-006**: 缺口到补检查询的映射 MUST 是确定性拼接(无模型调用、无随机性),任何维护者都可复算同一条映射。
- **FR-007**: round-0 证据集在加深后 MUST 原样保留于最终上下文(只追加、不删除、不重排)——与已证伪 021(slot 合并挤掉 round-0 好证据)的机制区分。
- **FR-008**: 每题最多执行一轮加深(一次补检 + 一次重答)——与已证伪 029(多轮自由导航)的机制区分;MUST NOT 出现模型自主决定继续/更换查询的循环。
- **FR-009**: 评测 MUST 遵守 008 铁律:同批配对、repeats≥3、store 复用、clean 判题口径;结果行 MUST 含 p 值与 context parity 校验。
- **FR-010**: 机制参数(犹豫阈值等)MUST 只在 LoCoMo 上定稿;LME 验证 MUST 零重调,且 MUST NOT 因 LME 表现回改参数(回改即视为 in-sample 特调,feature 失败)。
- **FR-011**: 系统 MUST 记录每题的加深决策(触发与否、缺口内容、补检查询、追加条数、两轮答案),供审计与失败归因。
- **FR-012**: 机制 MUST NOT 依赖任何托管 reranker/recall 模型(宪法死亡规则);补检索 MUST 复用现行本地 hybrid 检索。
- **FR-013**: 平均每题检索条数(含加深追加)MUST 显著低于 top-k 150 的水平(预算哲学:不加量换分);该均值 MUST 随结果一并报告。

### Key Entities

- **HesitationSignal**: answerer 在证据不足时的可观测表现(文本犹豫标记/答案置信),带解析覆盖率属性;是唯一触发加深的信号源。
- **GapItem**: 单条结构化缺口,字段为 category(枚举:bridge_entity/attribute/relation/evidence_span/other)、target、slot、description;领域无关。
- **DeepenDecision**: 每题的加深记录——是否触发、触发依据的信号值、GapItem 列表、映射出的补检查询、追加的证据条目、round-0 答案与重答。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 2-conv 信号 pilot 产出 AUC≥0.65(GO);否则 feature 关闭并留下关闭报告。
- **SC-002**: LoCoMo(1,540 题)k30 unified clean 配对 3-rep majority 达到 **≥90.0%**(相对 87.9% 锚净增 ≥2.1pp,above-noise)。
- **SC-003**: 平均每题检索条数(含加深追加)≤ 60 条(远低于 k150 水平),证明收益来自"按需加深"而非全局加量。
- **SC-004**: LME 零重调迁移:同参数机制臂 clean 3-rep 不显著低于 90.2% 锚。
- **SC-005**: 全部评测结果可复算:配对臂同批同 judge、context parity 通过、逐题翻转清单入库 result-matrix。

## Assumptions

- 犹豫信号在 Qwen3.6-35B-A3B-FP8(vllm, eval box)上可观测且跨批稳定;若文本犹豫与 logprob 置信二选一,pilot 两者都测,取 AUC 高者作为机制信号(选择依据只看 pilot,不看最终分数)。
- 加深轮的补检索复用现行 hybrid 检索与既有 store;不需要新索引或新 embedding。
- 040 的归因(79% 上下文量 / 21% 召回)在 unified 契约下仍大体成立;若 pilot 显示犹豫信号与"上下文量"题不对应,按 NO-GO 处理而非改机制。
- 本 feature 只动评测 harness(cmd/locomo-bench)与其读侧装配路径;引擎(memory/ 等)零改动——若实现中发现需要引擎新入口,按宪法 II 显式提出契约增量,不得绕过。
- eval-config 改动与算法改动分开 commit(宪法 IV 归因纪律)。
- box 侧全部实验(042 残余、Step A unified×trace 臂、本 feature 的 pilot 与配对批)合并为一次开机,跑完即关。
