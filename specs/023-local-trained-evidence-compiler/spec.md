# Feature Specification: 本地训练式 Evidence Planner

**Feature Branch**: `023-local-trained-evidence-compiler`

**Created**: 2026-07-30

**Status**: Draft

**Input**: User description: "新开一个用于涨点的 spec，但必须先评估并避免与 022
冲突。新特性应消费 022 冻结的固定候选与 Evidence Compiler 合同，训练可自托管的
本地 Evidence Planner；不得重做 022 已拥有的 Ledger、表示、确定性 Compiler 或评测
基础设施。"

## Amendment (2026-08-02)

**maintainer 决策**：开启 023 实施。022 依赖代码已冻结（Compiler/Planner 合同、fixed-gold
oracle、LoCoMo B1 85.19% 收口）；缺失件由 023 前置任务补全。本节修正/补充以下条款，与
正文冲突时以本节为准：

1. **启动门禁 → 最小充分集（修正 FR-003）**：023 正式启动（数据构建/训练/集成/评测）的
   依赖收据最小充分集 = 022 Compiler/Planner 合同冻结 ✅ + Primary Benchmark（LoCoMo）B1
   有效 ✅ + fixed-gold oracle 可用 ✅ + `local_planner.go` 接入点就绪（023 前置任务补全，
   harness 侧 adapter，engine 零改动）+ Primary Cohort residual 量化（023 前置任务产出）。
   LongMemEval-S B1、双 reviewer audit、miss attribution 降为 Cross-Benchmark Guard /
   后补项，不阻塞启动。已知风险如实记录：residual 归因在双基准收口前基于单基准（LoCoMo），
   提升不得跨基准外推。
2. **底模选型（补充 Assumptions）**：默认底模 **Qwen2.5-7B-Instruct**（Apache-2.0，无
   gating/商用上限，产物可分发可推荐；7.6B，BF16 ~15 GiB，单卡 24 GiB 上 LoRA/QLoRA 训练
   与 p95≤2s 推理均可行）。备选同族 Qwen2.5-3B/1.5B（tokenizer/chat-template 一致，可降级）。
   validation 正式冻结前可更换；冻结后所有模型臂共用同一底模。
3. **训练数据机制（FR-013 落地）**：V1 训练资产主路径 = **本地合成 + 自举**：本地 Qwen 生成
   虚构多会话记忆对话 → 灌入 engram（离线）提取/建索引 → 生成 query → 用 fixed-gold oracle
   + 规则确定期望 Need/actions 标签；辅路径 = 公共许可语料（逐语料确认许可/隐私，如 MIT 系）
   跑同一 pipeline。付费托管 teacher 调用数 MUST 为 0。
4. **参考硬件（FR-034 落地）**：单卡 24 GiB（L40S / RTX 4090 / A10G / AutoDL 对应档）；一次
   正式重建 ≤ 24 GPU-hours（含数据构建验证 + LoRA 训练 + 冻结重放），并发 1 Planner p95
   ≤ 2.0s。
5. **SaaS 边界（补充 Out of Scope）**：95%+ / 模型助手 / SaaS 方向为**独立后续计划**
   （maintainer 2026-08-02 决定），不进本 spec。023 产物为纯离线、可自托管本地 Planner；
   付费云端 answerer/Planner 作为正式涨点路径仍被禁止（FR-013/021/033 不变）。

## Clarifications

### Session 2026-07-30

- Q: 023 V1 的正式训练资产采用哪种数据来源边界？ → A: 仅使用有明确许可的非 benchmark
  公共语料和本地生成的合成数据；V1 禁止任何用户或 namespace 数据。
- Q: 023 的 post-training arm 应如何进入正式评测？ → A: 仅由独立 validation 决定是否
  纳入；在任何正式 benchmark test 结果可见前冻结全部实验臂，一次性评测并做跨阶段
  多重校正。
- Q: 023 应在 022 的哪种 F0 结果下启动？ → A: 仅当 F0=`CONTINUE`、022 完成 US3 并
  仍有 compiler-eligible residual 时启动；`HOLD` 由 022 修尺子，`STOP` 转独立本地
  answerer/eval-stack 特性，二者都不启动 023。
- Q: 单阶段 `GO` 是否足以让训练式 Planner 成为推荐配置？ → A: 不足；阶段 verdict
  仍只判断相邻训练收益，最终 recipe 还必须在 Primary 全量正确题数上严格胜过 022
  deterministic control，并在两个全量 benchmark 和保护类别上无预注册显著回退。
- Q: 推荐的 Planner 产物采用什么本地资源上限？ → A: 正式底模由 validation 冻结且各
  模型臂共用；最终 recipe 必须能在单张 24 GiB GPU 上离线重建与运行，峰值显存不超过
  24 GiB、并发 1 的 Planner p95 不超过 2 秒、一次正式重建不超过 24 GPU-hours；超限
  结果只能作为研究产物。

## Decision and Relationship to Feature 022

对 022 当前 spec、plan、tasks、Compiler 合同和工作副本的只读审计确认：source-expanded
B1、fit-aware packer、deterministic Need、`EXTRACT`、本地 Planner 接入、`MERGE` gate
及固定候选四臂实验均已由 022 拥有。若 023 再实现这些能力，会同时争用同一合同、同一
评测路径和同一 Compiler 实现面，属于硬冲突。

因此 023 不接管或重写固定候选 Compiler。它是严格串行的后继特性，只交付一个通过 022
公开 Planner 合同提出 Evidence Need 与受限 actions 的可自托管训练式模型。来源解析、
proposal 校验、Evidence Bundle 生成、精确 token 门、确定性 fallback 和一次作答纪律
仍由 022 的 Compiler 独占。

| 能力面 | 022 所有权 | 023 所有权 | 并行结论 |
|---|---|---|---|
| Evidence Ledger、source lineage、删除与 purge | 定义、实现并冻结 | 只读消费已发布来源 | 不冲突 |
| B1、fixed-gold oracle、候选冻结与 miss attribution | 定义、实现并发布逐题产物 | 验证依赖收据并重放 | 不冲突 |
| 表示 bake-off、retrieval、候选排序与 source expansion | 定义、实现并冻结一个输入表示 | 不改变候选或表示 | 不冲突 |
| deterministic Need、packer、`EXTRACT`、`MERGE`、Bundle 校验与 fallback | 定义、实现并维护 | 不复制、不修改，只接受校验结果 | 不冲突 |
| Planner proposal 合同与本地接入边界 | 定义并冻结版本与摘要 | 产出符合该合同的模型 proposal | 串行依赖 |
| 训练语料、训练阶段、模型产物、data/model card | 不拥有 | 定义、训练、审计和发布 | 023 独占 |
| 正式涨点报告 | 发布 022 基线与依赖产物 | 在独立报告中做冻结重放与 promotion verdict | 串行依赖 |

023 的 spec 可以与 022 并行评审，但数据构建、训练、集成和正式评测 MUST 等待 022 合并
并通过依赖收据。若 023 发现必须改变 022 的公开合同或评测主路径，023 MUST 标记
`BLOCKED` 并把变更请求交回 022；不得在 023 内绕过、复制或隐式扩展该能力。

023 不是 022 F0=`STOP` 的接棒路径。`STOP` 表示同栈 fixed-gold Evidence 仍无法让冻结
answerer 达到目标，此时 Planner 不能诚实突破 answerer ceiling；后续必须另开本地
answerer 或 eval-stack 特性。只有 F0=`CONTINUE`、022 已完成并冻结 US3 deterministic
Compiler/Planner 合同，且仍存在 compiler-eligible residual 时，023 才具备启动资格。

## Background and Scope

022 建立确定性的 source-grounded Compiler，并用固定候选实验区分 candidate miss、
compiler miss 与 answerer miss。确定性方法可提供可审计的离线基线，但它不能从训练数据
中学习复杂 query 与 Evidence Need、跨源关系或保留优先级。外部机制证据显示，小型
constructor/compiler 在固定输入预算下经监督训练和后训练可能显著提高 evidence utility；
该结果尚未在 engram 的标准 LoCoMo 1,540 题与 LongMemEval-S 500 题口径上复现。

本特性验证一个窄问题：在 022 的候选、来源、token cap、answerer、judge 和一次作答调用
全部冻结时，训练式本地 Planner 相对同底模的 prompt-only Planner 是否产生可归因、
可跨基准复现的涨点。

训练式 Planner 只能提出 Need 和受限 actions。它没有 Store、Search、Bundle 写入或作答
权限；所有输出必须经过 022 fail-closed 校验。模型缺失、超时、崩溃、合同不兼容或输出
不合法时，查询退回 022 的确定性 Compiler，基础产品路径继续离线工作。

本特性不改变 retrieval、top-k、候选 IDs、候选排序、表示、source expansion、token cap、
answerer、judge 或作答调用次数；不建设 Episode、Event、Scene、Profile、graph 或补检；
不把付费托管 reranker、recall model、Planner 或 answerer 作为正式涨点路径。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 通过 022 依赖收据后再启动 (Priority: P1)

维护者需要在消耗训练算力或修改集成前，证明 022 已交付有效、稳定且可重放的 Compiler
输入与合同，并证明剩余错误确实存在 compiler-side 可改善空间。

**Why this priority**: 没有有效 B1、fixed-gold ceiling、source-grounded candidates 和冻结
合同，训练结果既无法归因，也会与仍在变化的 022 实现冲突。

**Independent Test**: 仅检查 022 的最终提交与产物，不训练模型；对所有必需摘要、计数、
validity 证明和 verdict 生成唯一的 `READY`、`NOT_NEEDED`、`NOT_ELIGIBLE` 或 `BLOCKED`
收据。

**Acceptance Scenarios**:

1. **Given** 022 已合并并发布有效的双基准 B1、fixed-gold oracle、Compiler 合同版本、
   F0=`CONTINUE`、冻结候选和确定性 Compiler 结果，且仍有 compiler-eligible residual，
   **When** 校验摘要和 replay invariants，
   **Then** 023 记录 `READY`，且后续实验引用同一不可变依赖收据。
2. **Given** 022 尚未合并、合同仍在变化、B1 无效、F0=`HOLD` 或必要产物不完整，
   **When** 尝试开始数据构建、训练、集成或正式评测，
   **Then** 状态为 `BLOCKED`，上述工作均不得启动。
3. **Given** 022 的核心路径已经达到 LoCoMo 至少 1,425/1,540 且 LongMemEval-S 至少
   473/500，或没有 compiler-eligible residual，**When** 评估继续条件，
   **Then** 023 记录 `NOT_NEEDED`，不为完成特性而强行训练。
4. **Given** fixed-gold oracle 表明冻结 answerer 即使得到充分证据也无法达到预注册目标，
   并使 F0=`STOP`，**When** 归因剩余错误，
   **Then** 023 记录 `NOT_ELIGIBLE`，不把 answerer ceiling 伪装成 Compiler 问题，并把
   后续工作交给独立本地 answerer/eval-stack 特性。

---

### User Story 2 - 构建无泄漏、可复现的训练资产 (Priority: P1)

研究者需要把 query、冻结 candidates、来源和合法 proposal 组织成可审计训练样本，同时
确保最终 benchmark 测试题、用户私有记忆和 judge 反馈不会泄漏进训练或模型选择。

**Why this priority**: test-set contamination 可以制造不可迁移的名义涨分；缺少来源与拆分
证明时，任何训练收益都不能成为产品证据。

**Independent Test**: 在不运行正式 benchmark 的情况下，对一份获准语料重复构建两次；
验证样本、split 与全局摘要一致，所有 proposal 均可回到来源，并且污染扫描为零。

**Acceptance Scenarios**:

1. **Given** 一组获准用于训练或开发的对话，**When** 生成训练样本，
   **Then** 每个样本都包含 query、冻结 candidate 摘要、source lineage、期望 Need/actions、
   数据来源、许可、split 和构建版本，且目标同时通过合同校验与语义充分性审校。
2. **Given** 同一输入与构建配置，**When** 独立构建两次，
   **Then** 样本集合、split 分配和内容摘要完全一致。
3. **Given** LoCoMo 或 LongMemEval-S 最终测试 conversation、question、answer、judge 输出
   或逐题 treatment 结果出现在候选训练资产中，**When** 执行污染扫描，
   **Then** 该资产被拒绝，且不得用于训练、早停、超参、prompt 或模型选择。
4. **Given** 任意 namespace 或用户记忆出现在 V1 训练输入中，无论是否具有 opt-in 授权，
   **When** 执行来源审计，**Then** 该输入被拒绝且不会离开本地主机。
5. **Given** 近重复 conversation 跨越 train 与 validation/test split，
   **When** 执行 split 审计，**Then** 整个来源组被归入单一 split 或该构建被拒绝。
6. **Given** 两个独立标签判断对 query constraints、必要 source spans 或 action 选择不一致，
   **When** 构建正式训练资产，**Then** 该样本必须经独立裁决形成唯一标签，否则被排除。

---

### User Story 3 - 合同安全、默认可降级的本地 Planner (Priority: P1)

集成者需要一个可以自托管的 Planner 产物。它只能在候选 lineage 内提出 Need/actions，
不能读取任意 Store、重新检索、直接生成 Bundle 或回答问题。

**Why this priority**: 模型收益只有在 source、预算与权限边界不变时才可归因；确定性
fallback 是本地产品可用性的底线。

**Independent Test**: 用正常、越权、无效 span、无来源内容、超时、断连和合同版本漂移
夹具调用 Planner；验证合法 proposal 被 022 接受，其他情况全部 fail closed 并精确退回
确定性 Compiler。

**Acceptance Scenarios**:

1. **Given** 可自托管 Planner 正常返回符合冻结合同的 proposal，**When** 022 校验，
   **Then** 只有 lineage 内、字段合法、source/span 可复原的 Need/actions 被接受。
2. **Given** Planner 提议未知 action、无来源内容、越权 source、错误 span、猜测 cardinality
   或删除显式 query constraint，**When** 022 校验，
   **Then** 整个 proposal 被拒绝，不进行部分采纳，查询退回确定性 Compiler。
3. **Given** Planner 未配置、加载失败、触发其自身规划超时、进程退出或输出无法解析，
   **When** 用户查询，
   **Then** Store 与 retrieval 不受影响，确定性 Compiler 继续产生一次作答所需 Bundle。
4. **Given** Planner 产物声明的合同版本、tokenizer/chat-template 摘要或模型摘要与已批准
   配置不一致，**When** 加载或正式 replay，
   **Then** 产物被拒绝，不静默使用近似配置。
5. **Given** 运行环境切断外部网络但保留本机 sidecar，
   **When** 执行完整 Planner 路径，
   **Then** 合法 proposal、校验、Bundle 与作答路径均不依赖托管服务。
6. **Given** 调用方取消请求或调用方 context deadline 到期，**When** Planner 正在执行，
   **Then** 取消或 deadline error 原样传播，deterministic fallback 与 answerer 均不运行。

---

### User Story 4 - 单阶段归因并只推广真实涨点 (Priority: P1)

研究者需要依次比较确定性 Compiler、同底模 prompt-only Planner、监督训练 Planner 和
可选后训练 Planner。相邻实验臂只改变一个训练阶段，避免把底模、预算或候选变化误报成
训练收益。

**Why this priority**: 023 的价值不是交付一个不可解释 bundle，而是证明训练本身在 engram
固定口径下是否带来可迁移的分数提升。

**Independent Test**: 对预注册 compiler-eligible cohort 和两个完整 benchmark 重放相同
Candidate bytes；检查相邻臂只有一个训练状态变化，对逐题配对结果生成阶段
GO/HOLD/STOP，并独立执行最终 recipe 相对 deterministic control 的产品推荐门。

**Acceptance Scenarios**:

1. **Given** 冻结的 022 候选、表示、cap、answerer、judge 和 prompt，
   **When** 比较 prompt-only 与监督训练 Planner，
   **Then** 除模型的监督训练状态外其余评测变量和输入摘要完全相同。
2. **Given** 独立 validation 未达到预注册的 post-training 纳入条件，
   **When** 冻结正式实验，
   **Then** post-training 不进入正式实验臂，且不得在看过正式 supervised 结果后补跑。
3. **Given** 独立 validation 达到 post-training 纳入条件，
   **When** 冻结正式实验，
   **Then** post-training 在任何正式 test 结果可见前完成并冻结，只与 supervised arm
   做相邻单变量比较，且所有预注册 arm 在同一次正式评测中获得独立 verdict。
4. **Given** 某复合产物总分上涨但其中一个训练阶段单独 HOLD/STOP，
   **When** 作出 promotion 决策，
   **Then** 不把复合差值归因给该阶段，也不推广未经独立证明的 recipe。
5. **Given** 某臂只靠更多候选、更大 cap、第二次 retrieval、额外 answerer 调用或付费
   托管服务过门，**When** 审核 validity，
   **Then** 该运行标记 `INVALID`，不能构成涨点或发布证据。
6. **Given** 最终 recipe 所含训练阶段均获得 `GO`，
   **When** 决定是否成为推荐的 opt-in Planner，
   **Then** 只有其在冻结 Primary Benchmark 全量分母上的正确题数严格高于 022
   deterministic control，且两个全量 benchmark 与所有保护类别均无预注册显著回退时
   才能推荐；否则只能保留为研究产物。
7. **Given** 最终 recipe 通过分数与 non-regression 门，
   **When** 在预注册的单张 24 GiB 参考 GPU 上从冻结底模重建并以并发 1 重放代表性输入，
   **Then** 只有训练不超过 24 GPU-hours、峰值显存不超过 24 GiB 且 Planner p95 不超过
   2 秒时才能推荐；任一项超限都只能作为研究产物。

### Edge Cases

- 022 合并后合同摘要与 023 编写 spec 时预期不同：依赖收据失败；先更新并重新评审 023，
  不在实现中猜测兼容。
- 022 的 B1/Compiler verdict 为 `HOLD`：只允许补齐 022 证据，023 不抢先训练。
- 候选包含正确 source，但 022 fixed-gold oracle 仍答错：归为 answerer-side ceiling，不纳入
  compiler-eligible primary cohort；若 F0 因此为 `STOP`，023 记录 `NOT_ELIGIBLE`。
- 训练样本的 source 后续被 purge 或许可撤回：从未来构建中排除，撤销受影响数据版本与
  模型产物；不得用无法追溯的增量训练声称已完成删除。
- 底模已知预训练语料不可完全审计：model card 明确记录未知 contamination 风险；不得把
  “未发现”写成“确认不存在”。
- 模型权重或训练数据许可不允许分发、自托管或目标用途：实验可记录为研究结果，但产物不得
  成为推荐配置。
- Planner 在部分题目 fallback：逐题记录 fallback reason；该题按实际确定性路径计分，
  不从 treatment 分母静默删除。
- Planner 自身规划预算耗尽可触发 deterministic fallback；调用方 cancellation 或 deadline
  必须原样传播且 answerer 调用为 0，不得把被取消请求伪装成成功降级。
- Planner proposal 合法但最终 Bundle 超 cap：沿用 022 的 fail-closed 预算行为；不得提高 cap
  或丢弃超预算题以挽救分数。
- 训练或正式评测中断：只接受内容摘要一致、阶段状态完整的 checkpoint；不拼接口径不同的
  run 形成正式结果。
- 任一正式 benchmark test 结果在实验臂、模型产物或 recipe 全部冻结前已对选择者可见：
  该轮正式评测无效，必须在不复用已暴露 test 结果的新验证设计下重新预注册。
- 某阶段产生正向总体差值但任一预注册类别显著回退：该阶段不能 GO。
- 某 recipe 通过阶段与产品分数门但超过 24 GiB、24 GPU-hours 或 Planner p95 2 秒：
  可诚实报告研究结果，但不得成为推荐配置。
- 训练式 Planner 已涨点但未达到双基准最终数值目标：可以报告经门禁验证的局部杠杆，
  但不得声称 022/项目目标已经完成。

## Requirements *(mandatory)*

### Functional Requirements

#### Dependency and Ownership

- **FR-001**: 023 MUST 将 022 的最终合并提交、Compiler 合同版本与摘要、tokenizer 与
  prompt fingerprints、双基准协议、valid B1、fixed-gold oracle、F0 verdict、US3
  completion、冻结候选、确定性 Compiler 逐题结果、judge audit 和 miss attribution
  记录为一份不可变依赖收据。
- **FR-002**: 依赖收据 MUST 为每个必需项给出 expected、observed、validity 和来源；
  只允许产生唯一的 `READY`、`NOT_NEEDED`、`NOT_ELIGIBLE` 或 `BLOCKED` verdict。
- **FR-003**: 当依赖收据的最小充分集满足时可为 `READY` 并启动正式数据构建、训练、集成
  和评测：022 Compiler/Planner 合同冻结、Primary Benchmark（LoCoMo）B1 有效、fixed-gold
  oracle 可用、`local_planner.go` 接入点就绪（023 前置补全，harness 侧 adapter，engine
  零改动）、Primary Cohort residual 已量化。LongMemEval-S B1、judge audit、miss
  attribution 为 Guard 后补项，不阻塞启动；在其完成前，任何跨基准推广声明 MUST 禁止。
  （见 [Amendment 2026-08-02](#amendment-2026-08-02)；其余非 `READY` 分流：F0=`STOP` 记
  `NOT_ELIGIBLE`、核心已达双目标记 `NOT_NEEDED`、依赖不完整记 `BLOCKED`。）
- **FR-004**: 当 022 已达 LoCoMo 1,425/1,540 与 LongMemEval-S 473/500、没有
  compiler-eligible residual 时，023 MUST 记录 `NOT_NEEDED`；当 F0=`STOP`、answerer
  ceiling 不足时 MUST 记录 `NOT_ELIGIBLE` 并转交独立本地 answerer/eval-stack 特性；
  F0=`HOLD` 或依赖不完整时 MUST 记录 `BLOCKED`。任何非 `READY` verdict 都不得为完成
  特性强行训练或改变问题归因。
- **FR-005**: 023 MUST NOT 修改或复制 022 所拥有的 Ledger、表示、retrieval、候选冻结、
  source expansion、deterministic Compiler、Bundle validator、token 门、fallback 或评测
  主路径。
- **FR-006**: 若 023 需要 022 未提供的合同能力，工作 MUST 停止并形成显式的 022 合同
  增量请求；023 MUST NOT 通过私有存储访问、旁路检索或平行实现绕过缺口。

#### Training Data and Provenance

- **FR-007**: 每个训练或开发样本 MUST 记录 query、冻结 candidate 内容摘要、source
  lineage、期望 Need/actions、来源、许可、split、构建版本与内容摘要；目标 proposal
  必须通过与 022 正式路径相同的合同、lineage 和 source/span 复原校验。
- **FR-008**: 每个目标 proposal MUST 通过预注册语义充分性 rubric：保留 query 明示的
  entity、time、operand、cardinality 和 update constraints，覆盖 reference answer 所需
  的可用 source spans，不引入 unsupported content，且在冻结 cap 下只删除有记录理由的
  非必要证据。
- **FR-009**: 每个正式训练标签 MUST 有两个相互独立的判断；两者对 Need constraints、
  必要 source spans 或 actions 不一致时，必须经独立裁决形成唯一结果，否则样本被排除。
  正式训练前还 MUST 对至少 200 个分层随机样本（不足 200 时全部）做人审，语义充分率
  必须至少 95%，且其 95% 置信区间下界不得低于 90%。
- **FR-010**: 训练资产 MUST 可从获准来源和冻结构建配置确定性重建；同输入独立构建的
  样本集合、split 和摘要一致率 MUST 为 100%。
- **FR-011**: LoCoMo 与 LongMemEval-S 最终测试 conversations、questions、answers、
  judge outputs、逐题 treatment 结果和人工纠错标签 MUST NOT 用于训练、数据筛选、早停、
  超参选择、prompt 选择或模型选择。
- **FR-012**: 数据拆分 MUST 以来源 conversation 或更大的近重复组为隔离单位；同组内容
  不得跨越 train、validation 和独立 evaluation。
- **FR-013**: V1 正式 Training Asset MUST 仅由具有明确许可的非 benchmark 公共语料与
  本地生成的合成数据组成；任何用户或 namespace 数据即使具有 opt-in 授权也不得进入
  V1 训练资产、离开本地主机或写入发布模型。正式可复现 recipe MUST NOT 依赖付费托管
  teacher。
- **FR-014**: 数据构建 MUST 输出 provenance、许可、污染、近重复、标签语义充分性和
  privacy 审计；任一阻塞项非零时，该数据版本不得进入正式训练。
- **FR-015**: 每个训练阶段 MUST 冻结并记录输入数据摘要、底模与 tokenizer 摘要、训练
  配置摘要、随机性设置、输出产物摘要和完成状态。

#### Planner Contract and Degradation

- **FR-016**: 训练式 Planner MUST 仅通过 022 冻结的公开 proposal 合同提交 Evidence
  Need 与受限 actions；它不得拥有 Store、Search、Bundle 写入或 answerer 权限。
- **FR-017**: Planner MUST NOT 改变冻结 Candidate IDs、rank、text、source IDs 或内容
  摘要，也不得请求第二次 retrieval 或扩大候选池。
- **FR-018**: Planner proposal MUST 经 022 原有的完整 validation、lineage allowlist、
  source/span 复原、token cap 和 fail-closed 规则；023 不得放宽校验以提高接受率。
- **FR-019**: Planner 未配置、不可用、触发 Planner 自身规划超时、崩溃、合同不兼容或
  proposal invalid 时，系统 MUST 退化到 022 确定性 Compiler，且 Store、retrieval 和
  一次作答路径继续工作；调用方 `context.Canceled` 或 `DeadlineExceeded` MUST 原样传播，
  deterministic fallback 和 answerer 调用数均为 0。
- **FR-020**: 无 Planner 配置时的 Bundle、答案输入、错误语义和 namespace isolation
  MUST 与冻结的 022 确定性路径保持 parity。
- **FR-021**: 正式与推荐路径 MUST 能在外部网络断开的环境中使用自托管模型完整运行；
  付费托管 reranker、recall model、Planner 或 answerer 的调用次数 MUST 为零。
- **FR-022**: 每个可评测 Planner 产物 MUST 具有明确的合同版本、权重或 adapter 摘要、
  tokenizer/chat-template 摘要、基础模型与许可、数据版本和 model card；任一项漂移必须
  fail closed。

#### Controlled Evaluation and Promotion

- **FR-023**: 正式实验 MUST 至少包含确定性 control、同一底模的 prompt-only arm 和
  supervised-training arm。可选 post-training arm 是否纳入 MUST 仅由独立 validation
  的预注册条件决定；全部纳入的训练阶段、模型产物和 recipe 必须在任何正式 benchmark
  test 结果可见前一次性冻结。
- **FR-024**: prompt-only→supervised 与 supervised→post-trained 的相邻比较 MUST 各自
  只改变一个训练阶段；候选、表示、source、cap、Compiler validation、answerer、judge、
  prompt、重复策略和正式运行环境必须冻结。
- **FR-025**: 每题 Candidate 与 source 输入 MUST 在各 arm 间逐字节 replay，answer
  renderer、静态 prompt 和 token cap fingerprints MUST 相同；模型只能改变 proposal 及
  由其合法 Bundle 导出的 evidence payload，不能重新物化 retrieval 或选择不同输入。
- **FR-026**: 每个正式 arm MUST 保持冻结 token cap、在有效 Bundle 后恰好调用一次
  answerer，并记录 proposal validity、fallback、source/span/citation、token、调用次数、
  latency、资源占用、逐题答案和成本。
- **FR-027**: Dependency Receipt MUST 在任何数据构建或训练前指定唯一 Primary Benchmark
  与 Primary Cohort。候选 benchmark 是具有非空 compiler-eligible residual 的 LoCoMo
  或 LongMemEval-S；选择相对其数值目标 percentage-point shortfall 较大的一个，若相等则
  选择 LongMemEval-S。Primary Cohort 是该 benchmark 中 candidate evidence 足够、
  fixed-gold oracle 可答且确定性 Compiler 未答对的冻结题集；另一个 benchmark 明确定义为
  Cross-Benchmark Guard。不得事后更换 benchmark 或移动题目以扩大差值。
- **FR-028**: 单阶段 GO MUST 同时满足：Primary Cohort majority accuracy 相对相邻 control
  提升至少 2.0 个百分点、two-sided exact McNemar 在全部预注册训练阶段比较的多重校正后
  `p < 0.05`、Cross-Benchmark Guard overall 不低于 -0.5 个百分点且无显著负向、所有
  预注册类别通过 Holm 校正 non-regression，且 validity 阻塞项为零。
- **FR-029**: verdict MUST 形成互斥完备闭包：任一 validity 条件失败判为 `INVALID`；
  validity 有效但 Primary Cohort `Δ ≤ 0`、Cross-Benchmark Guard 显著受损或任一预注册
  类别显著回退判为 `STOP`；满足 FR-028 的全部条件判为 `GO`；其余所有 validity 有效、
  无前述显著伤害但未满足 GO 的结果一律判为 `HOLD`。
- **FR-030**: 每个训练阶段 MUST 获得独立 verdict；复合 recipe 不得掩盖未 GO 的相邻
  阶段，也不得把复合差值归因给多个组件；阶段 `GO` 本身不得等同于产品推荐。
- **FR-031**: 只有双基准门禁允许的 `GO` 阶段才可进入最终候选 recipe。该 recipe 成为
  推荐的 opt-in Planner 前，MUST 相对 022 deterministic control 在冻结 Primary
  Benchmark 全量分母上增加至少 1 个正确题，并在 LoCoMo 1,540 题、LongMemEval-S
  500 题及所有预注册保护类别上通过预注册的配对 non-regression 判据；否则 MUST 仅作为
  研究产物保留。deterministic Compiler MUST 保持无模型默认与故障 fallback。
- **FR-032**: 正式报告 MUST 给出 LoCoMo 精确计数 `/1,540`、LongMemEval-S 精确计数
  `/500`、百分比、逐题产物、置信区间、配对检验、类别结果、judge audit、训练与推理
  资源、延迟、成本和所有摘要；未达到 1,425/1,540 与 473/500 时不得声称完成数值目标。
- **FR-033**: 评测配置、训练数据/recipe 和 Planner 产物变更 MUST 分开归档，使任何分数
  变化可定位到单一阶段；全部正式 arm 必须一次性运行并对跨阶段比较做预注册多重校正。
  不得在看过正式 test 结果后回调训练、追加 arm 或选择配置，并复用同一结果作为无偏验证。
- **FR-034**: validation MUST 在正式 test 前冻结唯一底模、参考硬件、代表性 replay
  输入与执行配置，所有模型 arm MUST 使用同一底模。推荐的最终 recipe MUST 能在一张
  具有 24 GiB 物理显存的参考 GPU 上离线从冻结底模重建并运行：一次正式重建的总训练
  用量不得超过 24 GPU-hours，并发 1 时峰值设备显存不得超过 24 GiB，Planner proposal
  latency 的 p95 不得超过 2.0 秒。任一资源门不通过时，即使 score gate 通过也只能发布为
  研究产物，不得标为推荐配置。

### Key Entities

- **Dependency Receipt**: 023 对 022 最终提交、合同、B1、F0、US3、候选、评测协议、
  逐题产物和 feasibility verdict 的不可变验收记录；以 `READY`、`NOT_NEEDED`、
  `NOT_ELIGIBLE` 或 `BLOCKED` 决定 023 是否可启动及后续分流。
- **Compiler-Eligible Residual**: 冻结候选已有充分 Evidence、fixed-gold oracle 可答，
  但相邻 control 未答对的预注册题目集合。
- **Primary Benchmark / Primary Cohort**: 在训练前按冻结的 target shortfall 规则唯一选出的
  benchmark 及其 compiler-eligible residual；未选中的 benchmark 是 Cross-Benchmark Guard。
- **Training Example**: 带 query、冻结 candidates、source lineage、合法目标 proposal、
  来源、许可、split 和摘要的最小训练单位。
- **Training Asset**: 一组可重建、无测试泄漏且通过 provenance/privacy/license 审计的
  Training Examples。
- **Training Stage**: 对同一底模依次施加的 prompt-only、supervised 或可选 post-training
  状态；每个阶段有独立输入、配置、输出摘要和 verdict。
- **Planner Artifact**: 可自托管、符合 022 proposal 合同并带权重/adapter、tokenizer、
  许可、数据与 model card 摘要的版本化产物。
- **Evaluation Arm**: 在同一冻结 Candidate replay 和评测栈上，仅指定一个 Planner
  training state 的实验条件。
- **Stage Verdict**: 基于相邻训练臂的效应、统计、跨 benchmark/category non-regression
  与 validity 产生的 `GO`、`HOLD`、`STOP` 或 `INVALID` 决策。
- **Product Recommendation Gate**: 在所有 Stage Verdict 之后，独立比较最终 recipe 与
  022 deterministic control 的全量正确题数及双基准/保护类别 non-regression，只决定
  该 recipe 可被推荐还是仅作为研究产物保留。
- **Reference Hardware Envelope**: 在 validation 阶段冻结的单张 24 GiB GPU、驱动/
  runtime、并发、代表性 replay 输入和计量方法；用于验证正式重建 GPU-hours、峰值显存与
  Planner latency，而不是事后选择对结果有利的机器。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 022 仍在活动或依赖产物未冻结时，023 对 022 所有能力面的修改次数为 0；
  所有正式工作均由一份 `READY` Dependency Receipt 授权。
- **SC-002**: Dependency Receipt 对必需提交、合同、fingerprints、B1、fixed-gold、
  F0、US3、candidates、deterministic results、judge audit 和 miss attribution 的覆盖率
  为 100%，且只有一个可审计 verdict；非 `READY` 时启动 023 正式工作的次数为 0。
- **SC-003**: 正式 Training Asset 的样本 provenance、许可、split、source lineage、
  内容摘要、双标签/裁决和目标 proposal 合同校验完整率为 100%；分层人审语义充分率
  至少 95% 且 95% 置信区间下界至少 90%；最终 benchmark 测试内容、任何用户/namespace
  数据、
  无效或未解决语义标签和跨 split 近重复的检出后放行数均为 0。
- **SC-004**: 同一输入与构建配置的两次独立数据构建，其样本集合、split 和全局摘要
  一致率为 100%。
- **SC-005**: Planner 合同与故障注入夹具中，合法 proposal 的可复原率为 100%；
  越权、无来源、非法 span/action、版本漂移和故障 proposal 的接受数为 0，确定性 fallback
  成功率为 100%；调用方 cancellation/deadline 的原样传播率为 100%，对应 fallback 与
  answerer 调用数均为 0。
- **SC-006**: 无 Planner 配置时，冻结夹具上的 Bundle/answer-input digest、错误语义和
  namespace isolation 相对 022 确定性路径 parity 为 100%。
- **SC-007**: 各正式 arm 间 Candidate/source replay digest 以及 renderer、静态 prompt、
  token cap fingerprints 一致率均为 100%；final answer-input 的差异全部可归因到合法
  Bundle evidence payload，post-freeze retrieval 调用为 0，超 cap 的已作答题为 0，
  有效题 answerer 调用数均为 1。
- **SC-008**: 数据构建或训练前唯一 Primary Benchmark/Cohort 的冻结率为 100%，且选择
  结果完全符合 target shortfall 与 tie-break 规则；至少完成 prompt-only→supervised 的
  预注册相邻比较并产生唯一 verdict。只有满足 `Δ ≥ +2.0pp`、`p < 0.05`、
  Cross-Benchmark Guard `Δ ≥ -0.5pp`、类别 non-regression 与 validity 全绿的阶段可记为 GO。
- **SC-009**: 可选 post-training 的 validation 纳入条件、全部正式 arm/model/recipe 的
  test 前冻结率以及跨阶段多重校正覆盖率均为 100%，且其独立相邻 verdict 覆盖率为
  100%；在正式结果可见后追加的 arm 数、未独立 GO 却进入最终 recipe 的阶段数，以及
  未满足 FR-031 产品推荐门却被标为推荐的 recipe 数均为 0。
- **SC-010**: 正式推荐产物在外部网络断开时完成全路径运行；付费托管 reranker、recall
  model、Planner 或 answerer 的正式调用数和费用均为 0。
- **SC-011**: 每个正式 Planner 产物均具有完整 data card、model card、合同/模型/
  tokenizer/训练数据摘要、许可和冻结参考硬件上的训练 GPU-hours、峰值内存、吞吐及
  p50/p95 latency；推荐产物的一次正式重建不超过 24 GPU-hours、并发 1 峰值显存不超过
  24 GiB、Planner p95 不超过 2.0 秒，未测量的规模或性能声明数为 0。
- **SC-012**: 正式报告对 LoCoMo 1,540 题与 LongMemEval-S 500 题的分母、精确计数、
  百分比、统计、逐题证据和成本披露率为 100%；目标未满足却声称达到 1,425/1,540 与
  473/500 的次数为 0。
- **SC-013**: 若依赖门、feasibility、许可、数据合规或 score gate 不通过，系统产生
  `NOT_NEEDED`、`NOT_ELIGIBLE`、`BLOCKED`、`HOLD`、`STOP` 或 `INVALID` 的诚实结论并
  保持 deterministic default；为制造 GO 而扩大候选、预算、调用次数或改变
  answerer/judge 的次数为 0。

## Assumptions

- 022 会先以 F0=`CONTINUE` 完成并合并 US3，发布稳定 Planner proposal 合同、有效
  source-grounded B1、候选 replay、fixed-gold oracle、确定性 Compiler control 和逐题
  miss attribution；023 不以当前脏 worktree 或未冻结产物作为正式依赖。
- 022 提供的 Planner 接入边界足以加载符合合同的本地模型；若不成立，023 阻塞并请求
  独立合同增量，而不是修改引擎内部。
- 可获得具有明确许可且不属于最终 benchmark 的公共训练/开发语料，并能在本地生成
  可审计合成样本；V1 不依赖任何用户或 namespace 数据。
- 具体底模、参数规模和训练框架在 validation 阶段按许可、单张 24 GiB GPU 自托管能力与
  24 GPU-hours 正式重建预算选择并在 test 前冻结；spec 不预设某一家模型或 provider。
- 训练可以使用维护者控制的计算资源，但正式可复现训练 recipe 与正式推理不得依赖付费
  托管 teacher、reranker、recall、Planner 或 answerer；凭据、原始数据和模型权重不写入
  tracked source。
- Planner 始终是 opt-in sidecar；engram 无模型的 deterministic Compiler 保持默认、
  离线可用且可在约 100k-entry 单用户诚实规模边界内运行。
- 完成一项受控实验并得到负 verdict 是有效结果。023 不承诺训练必然涨点，也不以继续堆叠
  模型阶段来替代预注册证伪。

## Out of Scope

- 重新设计或实现 022 的 Evidence Ledger、B1、表示、candidate/source expansion、
  deterministic Compiler、validator、packer、Bundle、token counter、fallback 或 harness。
- 改变 retrieval、top-k、embedding、rerank、候选预算、token cap、answerer、judge、
  answer prompt、作答次数或 benchmark 分母。
- Episode、Event/State、Scene、Profile、graph、conditional refetch 或跨 namespace 推理。
- 付费托管 reranker/recall/Planner/answerer 作为正式或推荐涨点路径。
- F0=`STOP` 后在 023 内更换 answerer、改变 eval stack 或继续训练 Planner；这些工作必须
  进入独立特性，并重新建立可达性与因果门。
- 任何用户或 namespace 数据进入 V1 训练、验证或模型选择；即使用户 opt-in，也留待独立
  privacy/consent 特性验证，不在 023 内扩张。
- 把训练模型设为默认硬依赖，或在没有可分发许可、data/model card 和离线降级证明时发布。
