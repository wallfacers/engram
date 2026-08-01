# Feature Specification: 查询期 verbatim 证据编译

**Feature Branch**: `026-verbatim-evidence-compile`

**Created**: 2026-08-01

**Status**: Draft

**Input**: User description: "查询期 verbatim 证据编译:在 022 Evidence Ledger + Compiler 契约基础上,固定候选池内按 query 选择原始 turn/span 组装 answer bundle,原文优先、装不下才有来源压缩(EXTRACT/MERGE 逐句绑 source),不重复 024 write_dedup/025 episode 聚合。与 chunk_900 同 store 同预算配对消融验证信息密度差距。"

## Clarifications

### Session 2026-08-01(方向决策,承接密度假说收口)

022/024/025 三轮收口为一个决策命题:"同预算信息密度差距不来自候选密度,而来自**命中后的原始证据覆盖**"。026 承接该结论,聚焦**查询期(query-time)verbatim 证据编译**:

- **✅ 差距锚点 = 命中后的原始证据覆盖**:MemOS 的真实优势是命中 fact 后沿血缘**回收原始 span** 喂 answerer(比 022 少 3 倍 token,见 `022-original-span-recovery-vs-memos`)。外部文献有强受控实证支撑——**Fidelity-Before-Structure**(arXiv:2601.00821)固定 pipeline 内只换存储表示:verbatim chunks 比 LLM-extracted artifacts 高 **15.9pp(LoCoMo)/ 22.0pp(LongMemEval-S)**,机制是 lossy distillation(write-time 丢弃的信息 retrieval 救不回),69% 失败是 extractor 从未写下的 write-time loss。
- **❌ 不重复已证伪机制**:write_dedup、neighbor_extend(024)、跨消息 episode 聚合(025)三连证伪,均默认关。026 不做 write-time 提取/聚合,做 **query-time 对原始证据的选择与组装**。
- **❌ 不引入付费云 reranker/recall model**(DEATH RULE):任一杠杆必须是纯 Go、离线可退化。
- **⚠️ 022 仍未收口**:022 verdict 仍 HOLD(B1 双基准、LongMemEval-S、双 reviewer audit 待完成)。026 承接 022 的 Evidence Ledger + Compiler 引擎 + formal protocol 资产;026 若改变引擎检索/生成路径,须先使 022 具备可引用的 accepted baseline,否则 026 无对照可配。

### 与 022 US3(Query-time Evidence Compiler)的关系

022 已交付**Compiler 引擎层**(`memory/evidencecompiler/`,contracts/need/extract/validate/render/resolve 全包,**测试全绿**,含 compiler_test/extract_test/need_test/validate_test/render_test/resolve_test)与 **exact-token relevance arm**(`cmd/locomo-bench/compiler_eval.go` 的 `compileExactTokenArm`)。**但 T069 的其余 arms(legacy-count、deterministic extractive、optional local Planner)未实现**,且 022 从未在正式 B1 协议下跑过完整 arms 配对。026 的增量是:

1. **补齐并冻结 Compiler arms 集合**(legacy-count / exact-token / deterministic extractive / verbatim-first),在 022 冻结协议下做**同 store、候选逐字节一致**的配对消融(与 025 的配对纪律一致)。
2. **核心新臂 = verbatim-first(原文优先双态)**:原文装得下 → `KEEP`/`FETCH_SOURCE` 保留原始 span;装不下 → 才 `EXTRACT(span)` 或有来源 `MERGE`(逐句绑 source ID)。这直接落地 Fidelity-Before-Structure 的"结构增强 verbatim 而非替换"与 Retain-or-Consolidate 的"原文装得下优先保留、MERGE 宽松预算下显著负"(MERGE −0.107 [−0.204, −0.013])。
3. **承接但显著区别**——022 的 Compiler 是"把候选内已有证据编译成更可回答的 bundle"的**通用机制契约**;026 是把它**落到 verbatim-first 具体策略 + 正式配对验证**。026 不做引擎层大改(引擎契约已冻结),主要改动在 `cmd/locomo-bench/`(eval harness 臂 + 配对)与必要的确定性编译策略代码。

### 文献证据基础(alphaXiv 核实 2026-08-01)

| 证据 | 论文/来源 | 对 026 的含义 |
|---|---|---|
| verbatim chunks > extracted artifacts 15.9/22.0pp,机制 = lossy distillation(write-time loss 69%) | [Fidelity-Before-Structure](https://www.alphaxiv.org/abs/2601.00821) | 原文优先(query-time 选原始 span)是受控实证的最优表示;write-time 提取/压缩是反的 |
| 宽松预算下 MERGE 显著负(−0.107 [−0.204, −0.013]) | [Retain-or-Consolidate](https://www.alphaxiv.org/abs/2607.17545) | 原文装得下优先保留;MERGE 仅当原文装不下且 EXTRACT 不够时 |
| query-conditioned 构造;recall@50=0.99(LME)构造可超 oracle,0.89(LoCoMo)救不了缺失证据 | [LazyMem](https://www.alphaxiv.org/abs/2607.22690) | query-time compiler 的效力上界由 candidate oracle 决定;gold 不在池的题归补检/写入侧,不归 Compiler |
| LoCoMo-10 有 6.4% 答案键错误(99/1,540 改分错误),完美系统理论上限 ≈93.6%,category 1(multi-hop)错误率最高 9.9% | [Penfield Labs audit](https://github.com/dial481/locomo-audit)(被 [CogniFold](https://www.alphaxiv.org/abs/2605.13438) 引用) | 高分局分不可信(ByteRover 96.1% > 93.6% 天花板=不可比口径);engram 配对必须记录答案键噪声,small-delta 结论须谨慎 |

## Background and Scope

### 问题

budget-ablation 证明 engram 同预算(1059 tok)对 MemOS 落后 −5.62pp(1083 vs 1059 tok),差距由信息密度驱动。022/024/025 三连证伪"加证据/聚证据"机械密度杠杆。决策摘要(density-mechanism-hypothesis-closed)把差距锚点定为**命中后的原始证据覆盖**。Fidelity-Before-Structure 给出受控实证:query-time 用 verbatim 原文比 write-time 提取 artifact 高 15.9/22.0pp。但 engram 当前 chunk_900 表示是 **write-time 固定 900 字符块**,不带 query 条件选择;022 Compiler 引擎已实现但只落地了 exact-token 一个 arm,从未在正式 B1 配对下验证"query-time verbatim 编译是否比静态 chunk_900 更密"。

### 目标

在 022 冻结协议(cap 3600、LoCoMo 1,540 answerable + LongMemEval-S 500)下,验证 query-time verbatim 证据编译(原文优先 + 有来源压缩双态)是否在同 token 预算内比 chunk_900 基线带出更有效的原始证据覆盖,从而缩小同预算对 MemOS 的信息密度差距。核心假设:**query-time 按问题选择原始 span > write-time 固定 chunk**。

### 范围边界

- **在范围内**:补齐 Compiler arms(legacy-count / exact-token / deterministic extractive / verbatim-first);verbatim-first 双态策略(原文装得下 KEEP/FETCH_SOURCE,装不下才 EXTRACT/MERGE);同 store 配对消融验证;纯 Go deterministic 编译路径;fail-closed 校验(无来源 ADD 拒绝、无效 citation 丢弃)。
- **不在范围内**:write_dedup/neighbor_extend/episode 聚合(024/025 已证伪);write-time 提取/压缩/摘要;付费云 reranker;LLM 融合写回;engine 层契约改动(引擎已冻结);optional local Planner(022 S-1 方向,独立工作);补检(022 阶段 4,独立 feature)。
- 机制默认关(宪法 I/V);无 LLM 端点时须有纯离线判定路径(宪法 V);不得用付费云模型涨点(DEATH RULE)。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 查询期 verbatim 证据编译,同预算下原始证据更密 (Priority: P1)

用户查询命中候选后,系统在**固定候选池内**按 query 选择原始 turn/span 组装 answer bundle:原文装得下就保留原文,装不下才做有来源的 EXTRACT/MERGE。相比 write-time 固定 chunk_900,同一个 token cap 内带出更多逐证据原始覆盖。

**Why this priority**: 这是 026 的信息密度核心假设(query-time 选原文 > write-time 固定块)。Fidelity-Before-Structure 受控消融给出 15.9/22.0pp 的方向性证据;022 Compiler 引擎已就绪,只差 arms 补齐 + 配对验证。

**Independent Test**: 构造固定候选池,同一 query 下断言:verbatim-first arm 的 bundle 在 token cap 内优先含原始 span(而非压缩 fact),且与 chunk_900 基线候选逐字节一致(只差编译策略)。

**Acceptance Scenarios**:

1. **Given** 固定候选池与 query,**When** verbatim-first 编译,**Then** 原文能装入 cap 时 bundle 内容为原始 turn/span(KEEP/FETCH_SOURCE),不主动压缩;候选 ID 与 chunk_900 基线逐字节一致。
2. **Given** 原文超 cap,**When** verbatim-first 编译,**Then** 按 relevance 顺序 EXTRACT 有来源 span;EXTRACT 仍不够才逐句验证 MERGE,每句绑 source ID;无来源 ADD 被拒绝。
3. **Given** 编译策略关闭(默认),**When** 查询,**Then** 行为与 chunk_900 基线完全一致(零行为变化)。

---

### User Story 2 - Compiler arms 补齐并冻结,引擎契约保持可信 (Priority: P1)

在 022 已交付的 Compiler 引擎上补齐 arms(legacy-count / exact-token / deterministic extractive / verbatim-first),每个 arm 在固定候选池上 byte-replay 可复现,并复用 022 的 fail-closed 校验(无效 citation / 无来源 ADD → 丢弃或退回 extractive)。

**Why this priority**: 022 T069 只落地了 exact-token 一个 arm;无完整 arms 集合则无法做"编译策略差异"的干净配对。补齐必须基于 022 引擎契约(不重写引擎)。

**Independent Test**: 对每个 arm 断言:同一 query+候选池输出确定性的 bundle+trace,合法;无效 citation / 无来源 ADD 被 fail-closed;退化路径(无 LLM)仍产出 extractive bundle。

**Acceptance Scenarios**:

1. **Given** 同一 query + 同一候选池,**When** 重复运行任一 arm,**Then** 输出逐字节一致(deterministic)。
2. **Given** arm 想无来源 ADD,**When** 编译,**Then** 拒绝并退回 extractive(不调 answerer)。
3. **Given** arm 输出含无效 citation,**When** 编译,**Then** 丢弃不可验证句子,fail-closed。
4. **Given** 无 LLM 端点,**When** 查询,**Then** deterministic extractive 路径接管,查询不失败(宪法 V)。

---

### User Story 3 - 同 store 配对消融,verbatim 编译不回归基线 (Priority: P1)

维护者在 LoCoMo(1,540 answerable)与 LongMemEval-S(500)上、在 022 冻结协议(cap 3600)下,对"各 Compiler arm vs chunk_900 基线"做**同 store、候选逐字节一致**的配对消融,确认 query-time verbatim 编译是否带来信息密度增益且不回归基线。

**Why this priority**: 宪法 IV 回归门 + 022/024/025 的配对纪律。负结果可接受,但必须归因干净(只差编译策略,不差检索/表示)。

**Independent Test**: 在 022 冻结协议下,同一 store 跑 chunk_900 对照 vs 各 arm,repeats≥3,对比 overall 与分类别配对统计,重点看 multi-hop 与 temporal。

**Acceptance Scenarios**:

1. **Given** 022 冻结协议与同一 store,**When** 只切换编译 arm,**Then** LoCoMo overall 不显著回归基线,且 multi-hop/temporal 分类别报告明细(预期有增益,负则记录负结果)。
2. **Given** 配对有效(候选逐字节一致),**When** 报告差异,**Then** 归因到单一编译策略变量;报告 candidate oracle(gold 是否在池)以区分 compiler miss 与 candidate miss。
3. **Given** 任一 arm 相对基线负收益,**When** 报告,**Then** 按 FR-011 记录 verdict 并保持默认关,不进入默认路径。

---

### Edge Cases

- **原文装不下**:超 cap 时按 relevance 顺序 EXTRACT;EXTRACT 仍不够才 MERGE,每句逐句验证 source IDs。不得静默丢来源。
- **无来源 ADD / 无效 citation**:fail-closed——不调 answerer、退回 extractive(022 引擎已实现该校验)。
- **gold 不在候选池(candidate miss)**:compiler 救不了缺失证据(LazyMem recall 分界)。配对报告须用 candidate oracle 区分 compiler miss 与 candidate miss,不把 candidate miss 归因于 Compiler(022 阶段 0 纪律)。
- **答案键噪声**:LoCoMo 6.4% 答案键错误(99/1,540),multi-hop 9.9%。配对的小 delta 须记录噪声并谨慎解读;优先看 multi-hop/temporal 的大 delta 方向。
- **候选一致性**:两臂必须同一 store、候选逐字节一致(025 教训:不同 store = 不可配对)。
- **无 embedding / 无 LLM 端点**:offline 降级下 deterministic extractive 路径可完整运行(宪法 V)。
- **与 022 三表示的关系**:026 不重做表示层 bake-off(022 已定义 chunk_900/raw_turn_window/semantic_episode);026 固定表示(默认 chunk_900)只变**编译策略**。若 verbatim 编译在 chunk_900 表示上有效,再考虑与表示层组合(独立后续)。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 支持在固定候选池内按 query 执行 verbatim 证据编译:原文能装入 token cap 时优先 `KEEP`/`FETCH_SOURCE` 保留原始 turn/span(命中后沿 lineage 回收原文),只有装不下才 `EXTRACT(span)` 或有来源 `MERGE`(逐句绑 source ID)。
- **FR-002**: 编译 MUST 基于 022 已交付的 Compiler 引擎契约(`memory/evidencecompiler/`),不重写引擎;引擎契约改动 MUST 走显式 increment(宪法 II)。
- **FR-003**: 编译 arms MUST 至少包含 legacy-count、exact-token relevance(已实现)、deterministic extractive、verbatim-first 四臂;每臂对同一 query+候选池 MUST 输出确定性、byte-replay 可复现的 bundle+trace。
- **FR-004**: 编译 MUST 默认关闭;关闭时 MUST 与 chunk_900 基线行为完全一致(零行为变化)。
- **FR-005**: 编译 MUST fail-closed:无来源 `ADD` 拒绝、无效 citation 丢弃、退回 deterministic extractive,不调 answerer(022 引擎校验已实现)。
- **FR-006**: 无 LLM/embedding 端点时 MUST 退化到 deterministic extractive 路径,查询不失败(宪法 V 离线可退化)。
- **FR-007**: 编译 MUST NOT 依赖付费云 reranker/recall model 或强制 LLM 调用;任一可选 LLM 判定 MUST 默认关(DEATH RULE + 宪法 I/V)。
- **FR-008**: 配对消融 MUST 同 store、候选逐字节一致,只差编译策略;报告 MUST 含 candidate oracle(gold 是否在池)以区分 compiler miss 与 candidate miss。
- **FR-009**: 编译 MUST 保持 append-only Evidence Ledger 无损;EXTRACT/MERGE 的产物 MUST 绑回 source ID,不得凭空生成新事实。
- **FR-010**: 编译 MUST 记录可审计的 Trace(Evidence Need、候选 ID、action、span、source IDs、token 取舍、未满足 gap),供归因。
- **FR-011**: 若配对证明 verbatim-first 无收益或负收益,该机制 MUST 保持默认关并记录 verdict,不得进入默认路径(宪法 I/V)。
- **FR-012**: 结果报告 MUST 记录 LoCoMo 答案键噪声(6.4%/multi-hop 9.9%,Penfield audit),小 delta 不单独作 promotion 依据。

### Key Entities

- **Evidence Ledger(022 承接)**:append-only 消息级原文;编译的 source recovery 只读它,不删改。
- **Compiler Bundle(022 承接)**:编译产物(answerer 输入),token cap 内、每项绑 source ID。
- **Grounded Trace(022 承接)**:编译审计记录(Evidence Need、action、span、source IDs)。
- **Compile Arm**:编译策略的具体实现(legacy-count / exact-token / extractive / verbatim-first),026 的新候选单元。
- **Candidate Oracle**:每题 gold 证据是否已在固定候选池的诊断记录(区分 compiler miss vs candidate miss)。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: verbatim-first arm 在 LoCoMo 上相对 chunk_900 基线有可测提升(配对统计;预期集中在 multi-hop 与 temporal——query-time 原文覆盖),且 overall 不显著回归。
- **SC-002**: LoCoMo 与 LongMemEval-S 的 overall 在相同 token cap 下不显著回归基线(宪法 IV);若 negative,以负结果记录而不进入默认路径。
- **SC-003**: 配对实验有分类别明细、配对统计、token 记账与 candidate oracle;候选逐字节一致性有抽查证据(025 配对纪律)。
- **SC-004**: 关闭 verbatim-first 时,双基准在 022 冻结协议下的结果与基线一致(回归门对照)。
- **SC-005**: 无 embedding、无 LLM 的离线环境下,deterministic extractive 编译路径可完整运行并产出合法 bundle(宪法 V)。
- **SC-006**: 任一 arm 的无效 citation / 无来源 ADD 均被 fail-closed 拒绝,不产生无来源答案上下文。

## Assumptions

- **数据与协议**:复用 022 的 formal protocol、lossless chunks、LoCoMo(1,540 answerable)/LongMemEval-S(500 题)双基准与现有 store 资产;协议 cap 3600、hybrid retrieval、Qwen3.6-35B-A3B-FP8 answerer + deepseek-v4-flash judge、3 次答题多数(与 024/025 配对协议一致)。
- **承接资产**:022 的 `memory/evidencecompiler/`(引擎层,测试全绿)与 `cmd/locomo-bench/compiler_eval.go`(exact-token arm)可直接承接;026 补 arms + 配对,不重写引擎。
- **判断基准**:信息密度的成功以"同预算下的端到端正确率"为准,不以 coverage/recall 等代理指标作 verdict(008 教训)。
- **范围**:026 是研究实验(默认关),不承诺 productization;只有双基准配对通过才讨论进入默认路径。
- **对标**:对 MemOS 的"效果更好"定义为"同预算下达到或接近其信息密度";verbatim 编译需证明"query-time 选原文 > write-time 固定 chunk"才成立。
- **依赖**:依赖 022 已交付的 Evidence Ledger、Compiler 引擎、formal protocol 与 accepted baseline。若 022 baseline 未收口(仍 HOLD),026 须先建立可引用的 chunk_900 baseline 再配对,否则无对照可配。
