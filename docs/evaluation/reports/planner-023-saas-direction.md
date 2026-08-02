---
title: 023 训练式 Planner 可行性 + SaaS「95%+」目标缺口
summary: 评估训练式本地 Planner（023）的可行性与收益边界；把「提示词工程 + 模型助手」的 SaaS 方向及其 95%+ 跑分目标与当前 85% 级基线之间的缺口、所需条件和依赖证据写清楚。本文是战略评估，不是能力承诺，也不注册任何尚未预注册的分数。
status: draft
audience: [maintainers]
owner: engram-maintainers
last_reviewed: 2026-08-02
tags: [evaluation, strategy, 023, saas, planning]
---

# 023 训练式 Planner 可行性 + SaaS「95%+」目标缺口

## 1. 起点：已登记的硬事实

| 事实 | 值 | 来源 |
|---|---:|---|
| LoCoMo B1 正式基线（majority，027 收口） | **85.19%**（1,312/1,540） | [results.md](../results.md) · [b1-control-packing-027](b1-control-packing-027.md) |
| 同基线 OVERALL（stats 类别均值） | 84.7% | 同上 |
| B0 continuity（当前环境 / 历史） | 84.94% / 85.32% | 同上（无 retry 下 majority 约 85.2% 封顶） |
| 强 answerer 探针（deepseek-v4-pro，同检索、canonical recipe） | **89.03%** | [results.md](../results.md) |
| 预算剥离：对齐 MemOS 预算（≈1,083 tok） | **76.85%**（极显著落后） | [budget-ablation](budget-ablation.md) |
| 查询时编译器（026） | −4.5/−3.6pp 负收益 | [results.md](../results.md) |

**核心读数**：当前分数量级由两件事决定——answerer 能力和上下文预算。检索/表示/聚类侧的机制杠杆已累计 6+ 次 NO-GO（008/010/013/014/017/021/024/025/026）。answerer 是最大单一天花板：同一检索下从 Qwen 换成 pro 级模型，分从 85 级跳到 89 级。

## 2. 023 训练式 Planner 可行性评估

023 是「训练式证据挑选器」而非 answerer。它消费 022 冻结的候选与 contract，只输出 Evidence Need + 受限 actions，全部过 022 fail-closed 校验，失败退回确定性 Compiler。

| 维度 | 结论 |
|---|---|
| 方案 | query + 冻结候选 → proposal；三臂单变量相邻比较（deterministic → prompt-only → supervised，可选 post-training） |
| 涨点门（FR-028） | Primary Cohort majority **≥ +2.0pp** 且多重校正 **p<0.05**、Guard overall ≥ −0.5pp、类别 non-regression、validity 全绿 |
| 推荐门（FR-031） | 全量分母 **至少 +1 个正确题** + 双基准/保护类别 non-regression |
| 资源硬约束（FR-034） | 单张 **24 GiB** GPU 重建并运行；一次正式重建 ≤ **24 GPU-hours**；Planner p95 ≤ **2.0s**；超限只作研究产物 |
| 模型规模推断 | 7B 级 LoRA/QLoRA（BF16 权重 ~14–15 GiB）或 1.5–3B 全参；spec 不预设，validation 冻结 |
| 理论增益 | **受 compiler-eligible residual 规模限制**；该量 022 US3 至今未量化。当前证据（026 负收益、85.2% 封顶、B1 已达 1,425 目标的 majority）指向增益空间有限，最可能 **HOLD / STOP / NOT_NEEDED** |
| 当前状态 | **Draft，不可启动**：022 为 `PARTIAL`（US3 双基准与 audit 未完成），且 B1 majority 已达成 1,425/1,540 的 majority 口径 |

**结论**：023 的价值不在"承诺涨到 95"，而在它被迫沉淀的东西——不可变依赖收据、冻结候选 replay、contract 校验、数据来源/污染审计、单变量配对门禁。**这套评测与合规基建正是 SaaS 方向的地基。** 023 应被定位为基建而非涨点承诺。

## 3. 战略主张：提示词工程 + 模型助手 → SaaS 铺垫，目标 95%+

95%（≈1,463/1,540）相对当前 1,312 需再救 **151 题 ≈ +9.8pp**。

这条主张的内在逻辑成立：**本地 85 级是基底，95 级需要的东西（更强 answerer、agent 多步推理、更大的上下文预算）不在「纯本地小模型 + 确定性 Compiler」的能力边界内，而在「云端强模型 + 模型助手编排」的 SaaS 层。** 因此：

- **提示词工程**：是打磨本地默认路径的持续手段，也是 SaaS 层 prompt 的原型来源。
- **模型助手（agent）**：把 engram 的检索/Bundle 作为工具暴露给云端强模型，让 answerer 从「单次读一包证据」升级为「多轮检索—评估—收敛」，这是跨过 89%→95% 的最可能路径，且是全新机制（此前所有杠杆都锚定在检索/表示/打包侧，从未动过「answer 侧多步推理」）。
- **SaaS 铺垫**：本地引擎保持离线默认（宪法 I），SaaS 是**显式 opt-in 的产品层**，用云端模型是产品形态，不是本地评测的涨分杠杆。

## 4. 95%+ 目标的事实缺口（诚实账）

| 缺口 | 支撑证据 | 现状判断 |
|---|---|---|
| answerer 能力 | 89.03%（pro 探针）；同栈换 answerer 是已验证的最大单变量 | 离 95 还差 **≈6pp**，需要比 pro 级更强的模型 + agent 化，**未验证** |
| 上下文预算 | 预算剥离单调：预算↑ → 分↑；对齐 MemOS 时 76.85% | SaaS 层可放预算（云成本换分），本地受限 ~3.6k tok |
| agent 多步推理 | **无固定口径证据**（本项目从未做过 answer 侧多步） | **纯假设**，必须新 spec 预注册验证 |
| 机制杠杆 | 025/024/026 全负，检索/表示侧已穷尽 | 不建议再在本地侧堆机制 |

**结论：95 在本地固定口径下不是提示词工程或 023 能达到的。** 它是 SaaS 层的目标，且达成需要「更强 answerer + 预算放大 + answer 侧 agent 化」三件事一起成立——目前只有第一件有单点证据（89.03%），后两件是未验证假设。写成本文不代表可引用为能力。

## 5. 分层路径与门禁边界

```
本地层（当前 / 离线默认）        SaaS 层（战略目标 / opt-in）
─────────────────────        ─────────────────────────────
确定性 Compiler default       云端强 answerer + 模型助手编排
提示词工程持续打磨             检索/Bundle 作为工具暴露
85% 级（85.19% 已登记）        目标 95%+（未验证）
023：合同/候选/合规基建沉淀      口径单独声明，不进本地默认分
```

**死亡规则边界（必须写清）**：本地引擎的评测分**不得**借付费云端 reranker/recall/answerer 刷分——这条保持不松动（[CLAUDE.md 死规则](../../../CLAUDE.md)）。SaaS 产品线是独立交付形态，其分数以独立口径报告，不能回填为「本地引擎涨点」。SaaS 层若要冲 95，也必须走预注册、冻结臂、non-regression 的同一套纪律，否则 95 无证据意义。

## 6. 依赖与下一步

1. **022 US3 收口**（当前 `PARTIAL`）：量化 compiler-eligible residual → 023 才能判 `READY` / `NOT_NEEDED`。
2. **023 按 receipt 分流**：residual 若可救 → 训练式 Planner 作为本地基建杠杆跑受控实验；residual 若小 → 诚实记 `NOT_NEEDED`，不强行训练。
3. **若 95%+ 是战略目标**：开一个独立的 SaaS 层 spec（与本地宪法正交、opt-in），在预注册固定口径下验证「强 answerer + 模型助手 + 预算放大」三条件。在拿到其首个预注册配对结果前，95+ 只能记为**方向性假设**。
4. **不建议**：在本地侧继续堆检索/表示/打包机制去够 95——已有 6+ 次 NO-GO 证据；同样不建议用付费云端杠杆回填本地分数（死亡规则）。

## 结论

- 023 大概率**不涨点或不可启动**，但它是 SaaS 的地基基建，值得按 receipt 走完分流。
- 95+ 是**SaaS 层目标，不是本地口径能力**；从 89.03% 探针出发仍差 ~6pp，且 agent 化 + 预算放大两块至今零证据。
- 下一步唯一有信息量的动作：**先收口 022 US3 量化 residual**，再决定 023 是跑还是记 `NOT_NEEDED`；SaaS 方向若确认，开新 spec 用同一套预注册纪律验证，而不是用目标当已达成。
