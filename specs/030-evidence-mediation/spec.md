# Feature Specification: 读侧证据装配结构（Evidence Mediation）

**Feature Branch**: `030-evidence-mediation`

**Created**: 2026-08-06

**Status**: Draft

> **修订注记 (2026-08-06)**: 下述 FR-005 与 SC-004 中「trace 生成默认关闭 / opt-in」的约束，经全量 1540 题 × 3 次 majority 验证（**85.91%** @ ~468 tok，类别全正向、token 省 7.7 倍）后更新：`--trace-mediation` **现为 eval harness 默认开启**。未配置 answerer sidecar 时仍优雅降级 legacy 路径（字节一致，SC-004 保持）；`--trace-mediation=false` 显式回 legacy。`--evidence-assembly` / `--consolidate` 维持默认关不变。改动仅在 `cmd/locomo-bench/`（引擎零改动，FR-001 保持）。

**Input**: User description: "推进吧" —— 落地 C1 引用链证据门（MemChain）+ D1 token 精确装填（Retain or Consolidate）+ 证据装配地基（chunk-quota 固化），即「检索之后、作答之前」的读侧证据装配结构。

## User Scenarios & Testing *(mandatory)*

**背景**: 029 US2 NO-GO 的根因 A 是「检索结果被 fact 碎片稀释（chunk fraction 1%，~500 vs 基线 3654 tokens）」；同时 MemChain（去 trace −13.96pp）与 Retain or Consolidate（宽松预算 Merge −0.107 显著为负）两篇论文给出了读侧装配的受控证据。本 feature 把「检索后、作答前」的证据装配结构固化、验证，只动评测 harness（引擎零改动），端到端配对为唯一 GO 门（008 铁律）。

### User Story 1 - 预算诚实的证据装配地基 (Priority: P1)

把检索结果装配成 answerer 上下文时：用真实 tokenizer 精确记账每个证据单元的 token 数；chunk 原文默认优先于 fact 碎片（保底，不靠配额 flag 事后补救）；按问题类别选择证据组织结构。这是 029 根因 A 的直接修复 + Retain or Consolidate「原文优先、不压缩」纪律的第一步，纯 Go、零模型、完全离线。

**Why this priority**: 029 的 ~500 vs 3654 tokens 失真说明「不知道上下文实际装了多少」是读侧最大的基础设施缺陷；先修它，后面所有结构（引用链/压缩）才有可靠的地基。

**Independent Test**: 完全离线（无模型调用）——对固定检索结果集断言：(1) 装配输出 token 数精确等于 tokenizer 计数（零估算误差）；(2) chunk 保底 fraction ≥ 阈值（修复 1% 现状）；(3) 类别条件排序生效（temporal → 时间序）。

**Acceptance Scenarios**:

1. **Given** 一个含 chunks 与 facts 的检索结果集，**When** 装配器运行，**Then** 输出的 token 数等于真实 tokenizer 精确计数（逐条记账，非字符/条数估算）。
2. **Given** 检索结果中 fact 碎片数量远多于 chunks，**When** 装配，**Then** chunk 原文优先进入上下文，chunk fraction ≥ 约定阈值（相对现状 ~1% 的硬修复）。
3. **Given** 一个 temporal 类别问题，**When** 装配，**Then** 证据按时间顺序组织；一个 multi-hop 类别问题则按实体组织（021 契约：temporal≠graph，category-conditional）。
4. **Given** 装配管线未启用（默认），**When** 检索完成，**Then** 现有路径逐字节不变（parity 门）。

---

### User Story 2 - 引用链证据中介（Grounded Evidence Mediation，C1）(Priority: P1)

检索后、作答前插入一层证据中介（MemChain 式）：sidecar（opt-in，默认关）把候选证据组织成「plan → grounded trace → 显式动作 → 最终证据 E」四层，E 只喂给答题模型；fail-closed 校验门（纯 Go 确定性）丢弃任何引用候选集之外 ID 的证据并回退。goal：答题模型收到带来源引用的紧凑证据链，不用自己判断谁新谁旧、谁支持谁（这正是 029 中 35B 判断不了的）。

**Why this priority**: MemChain 去 trace −13.96pp 是 post-retrieval 最强单结构消融；而 029 已证明「让弱模型自己组织证据」不行——把组织责任前移给中介是机制性修复。但 trace 生成需模型，故 opt-in 默认关，008 端到端配对为唯一 GO 门。

**Independent Test**: fail-closed 门纯离线可测（非法 ID → 丢弃 → 回退）；trace 生成开启后 84 题 × 3 reps 配对基线（008 铁律：中介 arm majority ≥ 单次基线，不回归）。

**Acceptance Scenarios**:

1. **Given** sidecar 未配置（默认），**When** 检索完成，**Then** 走现有装配路径，零行为变化（SC-004 parity）。
2. **Given** 中介产出一条引用候选集之外 ID 的证据，**When** fail-closed 门校验，**Then** 该证据被丢弃、不进入 answerer 上下文（确定性的、纯 Go）。
3. **Given** 中介产出合法 trace，**When** 答题，**Then** answerer 上下文 = 最终证据 E（每条带来源 ID），且 E 内每条证据都可在候选集回溯。
4. **Given** trace 解析失败，**When** 重试一次仍失败，**Then** 回退现有路径（宪法 V 优雅降级，绝不空答）。
5. **Given** 中介 arm 开启且完成配对评测，**When** 与基线比较，**Then** majority ≥ 基线（008 铁律 GO 门），且类别不回归（L0-3）。

---

### User Story 3 - 条件压缩操作符（Budget-Dependent Consolidation，D1 的 when/which）(Priority: P2)

仅当「证据确定超预算」且显式启用时，才允许压缩操作符（Abstract/Merge）替换原文；默认保留原文。配对验证压缩 arm 不使 e2e 显著回退。目标是把 Retain or Consolidate 的预算交叉结论（紧预算压缩 +35pp / 宽松预算 Merge −0.107 显著为负）落到 engram 的宽松预算（cap 3600）口径上验证。

**Why this priority**: engram 的 cap 3600 属于宽松预算，论文预测「原文优先、不压缩」；但 LoCoMo 证据短，个别题可能仍超预算，需实测交叉点。P2 因为默认关，不阻塞 US1/US2。

**Independent Test**: 压缩默认关（未启用时与 US1 装配逐字节一致）；启用后在「证据超预算」子集上配对，压缩 arm e2e 不显著回退（与保留 arm 对比）。

**Acceptance Scenarios**:

1. **Given** 证据在预算内（默认场景），**When** 装配，**Then** 原文完整保留，无任何压缩（MERGE/Abstract 默认关）。
2. **Given** 证据确定超预算且用户显式启用压缩，**When** 装配，**Then** 压缩操作符（Abstract/Merge 优先于 Rewrite）可用，输出仍不超预算。
3. **Given** 压缩 arm 启用并完成配对，**When** 与保留 arm 比较，**Then** e2e 不显著回退（配对统计，重演 Retain or Consolidate 的预算交叉或诚实报告负结论）。

---

### Edge Cases

- **检索结果为空 / 候选集无 gold**（gold 不在池）: 装配照常给已有证据，不报错、不空答（宪法 V）；不因缺 gold 改变装配行为。
- **tokenizer 不可用**: 回退字符级估算并显式记录降级（诚实标注，不静默）。
- **trace 解析失败 / 非法 ID / 超预算中间结果**: fail-closed 依次降级（重试 → 丢弃非法项 → 回退现有路径），绝不产生空 answerer 上下文。
- **类别无法判定**: 装配退化为通用结构（chunk 优先 + 分数序），不报错。
- **多 rep 配对**: 评测口径沿用 84 题 × 3 reps majority + McNemar（与 027/028/029 一致），防单次观测噪声。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 提供一条「检索后、作答前」的证据装配管线；实现限定在 `cmd/locomo-bench/` 与 `specs/030/tools/`（引擎 `memory/ embedding/ provider/ store/ internal/` 零改动，`git diff --name-only` 为空是硬门）。
- **FR-002**: 装配 MUST 使用答题模型对应的真实 tokenizer 逐条精确记账证据单元 token 数（禁止按字符/条数估算）。
- **FR-003**: 装配 MUST 默认 chunk 原文优先于 fact 碎片（保底 fraction 阈值），修复 029 的 fact 稀释（chunk fraction ~1%）。
- **FR-004**: 装配 MUST 按问题类别选择证据组织结构（temporal → 时间序；multi-hop → 实体组织；021 契约 category-conditional）。
- **FR-005**: 引用链 trace 生成 MUST 默认关闭、显式 opt-in（本地 sidecar 配置后才启用；未配置时装配路径零行为变化）。
- **FR-006**: fail-closed 门 MUST 丢弃引用候选集之外 ID 的证据并回退（纯 Go、确定性、可离线单测）。
- **FR-007**: 压缩操作符（Abstract/Merge/Rewrite）MUST 默认关闭；仅当证据确定超预算且显式启用才允许；输出 MUST 不超预算。
- **FR-008**: 每个启用 arm MUST 支持与基线配对评测（同 store/子集/answerer/judge/预算），008 铁律（majority ≥ 基线）为唯一 GO 门。
- **FR-009**: 评测口径/配置变更 MUST 与算法改动分开提交（宪法 IV 归因）；文档落 `docs/evaluation/` tracked。

### Key Entities *(include if feature involves data)*

- **EvidenceUnit**: 装配的最小单元（chunk 或 fact 原文），带稳定来源 ID、文本、检索分数、真实 token 数（FR-002 记账结果）。
- **EvidenceAssembly**: 装配器输出，一个 token 精确、类别结构化的有序证据包（US1）；是 answerer 上下文的唯一来源。
- **GroundedTrace**: 引用链中介的四层产物（plan / grounded trace / 显式动作 / 最终证据 E）；每条证据 MUST 引用 ≥1 个候选集内 ID（US2）。
- **FailClosedGate**: 校验门（US2/3），丢弃非法 ID 引用、超预算项，解析失败降级回退。
- **ConsolidationOperator**: 压缩操作符（Abstract / Merge / Rewrite），默认关，仅预算不足时 opt-in（US3）。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 装配输出的 token 数 = tokenizer 精确计数，零估算误差（离线断言可验证）。
- **SC-002**: chunk 保底 fraction ≥ 约定阈值（相对现状 ~1% 的硬修复）；fix 后 answerer 上下文 token 量级回到基线口径（~3000+，非 ~500）。
- **SC-003**: 008 铁律 e2e：启用 arm 的 majority ≥ 单次基线，且类别不回归（L0-3）；不得使既定基线显著回退（宪法 IV）。
- **SC-004**: 所有新机制默认关闭；关闭状态下与现有路径 parity（零行为变化）。
- **SC-005**: 文档落盘：verdict/复现说明写 `docs/evaluation/` tracked（verdicts-go-to-tracked-docs 纪律）。

## Assumptions

- 检索结果来自现有 `Retriever.Search`，每个结果可稳定标识（ID），候选集为闭包边界。
- 本地 sidecar（DeepSeek-flash，已在 028 验证）可用于 trace 生成；一律 opt-in、默认关，密钥走 env。
- e2e 评测沿用既有口径：LoCoMo 84 题（temporal 59 + multi-hop 25）× 3 reps majority + McNemar；基线 = 当前参考点（85.71% 全量 / 84 题子集配对基线）。
- tokenizer 对应答题模型（本地 Qwen3.6-35B 或 vllm 对应），纯 Go 侧通过离线 tokenizer 调用实现（无 CGO 约束）。
- 压缩操作符的「预算交叉」为实测问题：不预设 engram 一定交叉在哪个预算点，诚实报告正/负结论。
- 引擎不可触碰（宪法 II）为硬约束：任何超出 `cmd/locomo-bench/` 的引擎改动需求 MUST STOP 并提合同增量。
