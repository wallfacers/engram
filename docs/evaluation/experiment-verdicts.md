---
title: 实验裁决索引
summary: 本文汇总已收口实验的可执行 verdict 与证据入口；不提供当前完整分数矩阵或未来功能承诺。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [experiment-verdicts]
tags: [evaluation, verdicts, evidence]
---

# 实验裁决索引

本文是已收口实验的唯一裁决入口；完整当前分数见[当前评测结果](results.md)，逐次过程、旧基线和原始数字见[LoCoMo 历史实验台账](../archive/evaluation/locomo-experiment-ledger-2026-07.md)。本页不把覆盖率、代理指标或单次差值写成默认能力。

## Feature 003–022 的交付与实验裁决

| Feature | Verdict | 范围及最终结论 | 出货影响 | 证据 |
|---|---|---|---|---|
| 003 | diagnostic-only | 生物启发的实体关联检索作为可测诊断路径保留；其 `assoc` 端到端验证后来未转化为回答收益。 | 默认关闭，不进入默认检索。 | [规格](../../specs/003-bio-retrieval-locomo/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 004 | shipped-default | AI-first CLI 已交付。 | CLI 是当前接口能力；不是评测涨点声明。 | [规格](../../specs/004-cli-ai-first/spec.md) · [当前 CLI 指南](../guides/cli.md) |
| 005 | closed-no-go | PCIC-lite span selector 未越过 coverage 门。 | 不接入默认检索。 | [规格](../../specs/005-pcic-lite-selector/spec.md) |
| 006 | closed-no-go | Strike 3 abstention gate 在免费门已证伪。 | 不改变默认拒答行为。 | [规格](../../specs/006-strike3-abstention-gate/spec.md) |
| 007 | protocol-only | mem0-aligned judge 是评测口径对齐与 golden 门，不是检索算法。 | 只用于声明同口径评测，不能计作产品涨点。 | [规格](../../specs/007-judge-metric-alignment/spec.md) · [历史更正](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 008 | closed-no-go | 本地 reranker 与 open-domain prompt 均未把中间信号转成端到端收益。 | reranker 默认关闭；coverage 不可单独作为出货依据。 | [规格](../../specs/008-locomo-score-levers/spec.md) · [评测记录](../../specs/008-locomo-score-levers/eval-log.md) |
| 009 | diagnostic-only | 归因 trace 已交付；其排序机制 STOP，历史评测配置变动不等同产品默认行为。 | 不新增默认排序路径。 | [规格](../../specs/009-retrieval-attribution-gate/spec.md) · [评测记录](../../specs/009-retrieval-attribution-gate/eval-log.md) |
| 010 | closed-no-go | 多查询分解在 retrieval-only 门已证伪。 | `SearchMulti` 仅保留为默认关闭的诊断能力。 | [规格](../../specs/010-multi-query-retrieval/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 011 | closed-no-go | alias 影子向量通过契约门、未通过分层召回门。 | 新能力保留但默认关闭，不报为赢。 | [规格](../../specs/011-dual-index-alias/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 012 | closed-no-go | Doc2Query 影子向量在召回门止损。 | 不进入默认路径；未完成的生成支线不出货。 | [规格](../../specs/012-doc2query-shadow/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 013 | closed-no-go | 时间窗召回臂受低时间解析点火率限制，未建设后续召回臂。 | 当前路线不采用该臂；诊断可复用。 | [规格](../../specs/013-temporal-window-recall/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 014 | closed-no-go | 强化 temporal answer contract 端到端翻车，旧简单契约也未坐实。 | `--temporal-answer-prompt` 维持默认关闭。 | [规格](../../specs/014-temporal-answer-contract/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 015 | closed-no-go | 固结/跨 session 桥接线已收口，未形成可复现默认杠杆。 | 不增加默认检索或生成路径。 | [规格](../../specs/015-consolidation-bridging/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 016 | shipped-default | LongMemEval cross-benchmark harness 已交付；已发布分数是历史测量，须按当前口径阅读。 | 评测能力出货；结果更正与复跑要求见结果正本。 | [规格](../../specs/016-longmemeval-crossbench/spec.md) · [verdict](../../specs/016-longmemeval-crossbench/verdict.md) |
| 017 | closed-no-go | 确定性 TIMELINE 脚手架实现正确，但端到端结果落在噪声内。 | `--temporal-date-scaffold` 默认关闭、不出货。 | [规格](../../specs/017-temporal-date-scaffold/spec.md) · [历史证据](../archive/evaluation/locomo-experiment-ledger-2026-07.md) |
| 018 | shipped-opt-in | curation 生命周期与索引完整性已交付。 | 显式启用；默认 MCP 行为保持不变，不是 LoCoMo 涨点。 | [规格](../../specs/018-curation-lifecycle-cleanup/spec.md) · [任务验收](../../specs/018-curation-lifecycle-cleanup/tasks.md) |
| 021 | closed-no-go | 「真实超越」检索侧路线收口。(A) 低预算复用 003 `--assoc` graph：tk7 temporal **−7.17pp（p=0.011）**。(C/US1) IRIS 证据缺口迭代检索 temporal MVP：tk7 temporal **−9.35pp（b=15 c=45，exact p=0.000135）**，预算对齐（1099≈1083）非预算问题，slot 合并挤掉 round-0 好时序证据。两者均证伪；叠加 008/013/014/017 共 **6 次** temporal 检索/答题杠杆全 NO-GO。结论：MemOS 对齐预算下 temporal 真差距在**写入侧结构化记忆**（tree/graph），检索侧已穷尽。 | `--assoc`/`--iris` 均不进默认检索（default-off）。 | [spec 021](../../specs/021-iris-evidence-gap-retrieval/spec.md) · [budget-ablation](reports/budget-ablation.md) |
| 022 | in-progress-partial | Evidence Ledger/schema v7 与来源/隐私生命周期合同已交付；有效 LoCoMo B0 为 85.32% continuity-only；**027 修复 B1 control 打包粒度后 LoCoMo B1 正式基线已收口**：3 次多数 1,312/1,540（85.19%），stats OVERALL 84.7%（相对 026 control 82.6% +2.1pp），validity 全绿，协议 `sha256:263b52b6…`。Episode、Compiler、Event/gap/窄 projection 仍为待消融实验面；LongMemEval-S low/high B1、双 reviewer audit、双基准配对门未完成。 | Ledger 可用；**LoCoMo B1 默认基线 = chunk-verbatim fold + cap5000（85% 级）已登记**；query-time treatment/default promotion 保持关闭。 | [规格](../../specs/022-benchmark-parity-memory-architecture/spec.md) · [评测记录](reports/benchmark-parity-memory-architecture.md) |
| 024 | closed-negative | 「同预算记忆密度」四臂(write_dedup × neighbor_extend)全负：control 84.29% > neighbor 83.83%(−0.46pp) > dedup 83.38%(−0.91pp) > both 82.99%(−1.30pp)，随机制叠加单调下降。dedup 几乎不触发(21,860 判定仅 20 抑制、误伤 5)；dedup 的 multi-hop +3.2pp 被 open-domain −4.2pp 抵消。LongMemEval-S 四臂因 LoCoMo 已收口不再执行。结论：MemOS 的"写时去重 + 命中邻居扩展"机械密度杠杆在 engram 同预算下不涨点。 | 两机制默认关，不进默认路径（FR-011）。 | [spec 024](../../specs/024-memory-density/spec.md) · [配对报告](reports/memory-density-four-arm.md) · [登记](../../specs/024-memory-density/benchmark-registration.md) |
| 025 | closed-negative | 「跨消息语义聚类 episode 表示」同一 store、候选逐字节一致配对下：multi-hop(目标 cohort) 84.0% vs chunk_900 84.0%，**Δ=0.0pp 零提升**；OVERALL 74.6% vs 82.3%（**−7.7pp**），single-hop −8.1pp、temporal −16.0pp。机制：episode 聚合在 token cap 内挤掉逐证据覆盖（candidate_miss 231→351）。两臂 validity 全绿（契约方向 A 修复生效）。结论：跨消息证据"聚合叙事"伤简单类目且不提升 multi-hop；密度差距不来自候选聚合。 | `--episode-cluster` 默认关，不进默认路径（FR-011）。 | [spec 025](../../specs/025-semantic-episode-cluster/spec.md) · [配对报告](reports/semantic-episode-cluster.md) · [登记](../../specs/025-semantic-episode-cluster/benchmark-registration.md) |
| 026 | closed-negative | 「查询期 verbatim 证据编译」三臂同 store、候选逐字节一致配对（LoCoMo B1-high 1,540）：compiler 相对 control 82.6% 为 extractive 78.1%（**−4.5pp**）/ exact-token 79.0%（**−3.6pp**）；目标类别 multi-hop 也无提升（−5.7/−3.1）。机制：compiler 按 need 剪枝把 answer input token 从 3,399 压到 2,205/2,572（−33%/−24%），simple 类目更依赖精确逐证据命中，故回归。三臂 validity 全绿、候选逐字节一致，**机制性负结果**（非实现缺陷）。验证期修复 022 compiler 集成的 4 个 harness 缺陷（anchor-prefix 契约、候选去重、token 口径、digest 自洽）。LongMemEval-S 配对因 LoCoMo 已负未跑；027 更新 control 基线至 84.7% 后方向结论不变。 | `--compiler-arm` 默认关，不进默认路径（FR-011）。 | [spec 026](../../specs/026-verbatim-evidence-compile/spec.md) · [登记](../../specs/026-verbatim-evidence-compile/benchmark-registration.md) |
| 027 | closed-negative | 「写入侧 event 时序结构」阶段 1 先导配对（84 题 = temporal 59 + multi-hop 25，3 reps majority）：**event 投影替换原文 chunk 端到端大降**——chunk 50.0% vs event 23.8%，**−26.2pp，McNemar p=0.0016**；temporal p=0.0135（24 vs 9 独对）、multi-hop p=0.092（10 vs 3 独对）方向一致。机制：7B 抽取把绝对日期泛化成相对词（47/63 错题 predicted 含相对时间词，gold 含绝对日期错 25 题；build-event 仅 5% 带绝对时间锚定），写侧结构化以丢失原文保真为代价，relation 结构未补偿。**时间域第 7 次 NO-GO**（叠加 014/017/021×3），022 "Event/gap/窄 projection 待消融面" 中 Event 一支现收口。 | event 投影 / `--representation event` / `--build-event-project` 默认关、不进默认路径（FR-010）；`memory/eventstore/` 保留为可重建投影基建，重试前提是抽取侧绝对时间锚定修复后重过配对门。 | [spec 027](../../specs/027-write-side-event-structure/spec.md) · [配对报告](reports/027-write-side-event-verdict.md) |
| 028 | closed-negative | 写侧 event 训练化 US2：训练抽取器（Qwen2.5-3B-LoRA，5313 条教师数据，时间锚定 5%→**96.9%**、schema 合法 100%）彻底解决 027 抽取瓶颈，但端到端配对仍**未转化**——chunk 50.0% vs event 48.8%，**Δ=+1.2pp（chunk 胜），McNemar p=1.00**；temporal +3.3pp（52.5 vs 49.2，p=0.81）首次转正但噪声内、multi-hop −12pp（40.0 vs 52.0，p=0.51）倒退。三臂差距收窄链 −26.2pp(7B) → −6.0pp(teacher) → **−1.2pp(trained)**；蒸馏上限=教师（教师自身 −6.0pp 未转化）。**写侧 event 表示第三次端到端不转化**（027/028-US1/028-US2），US3（部署接入）不进入。 | event 投影保持 default-off（027 FR-010 已定）；训练抽取器（train_sft/train/export_deploy）作为能力资产记录，SaaS 线可复用。 | [spec 028](../../specs/028-write-side-event-training/spec.md) · [US2 配对报告](reports/028-write-side-training-verdict.md) |
| 029 | closed-negative | 检索侧**推理驱动多步导航**（US1 零成本诊断 GO：rescueable 0.655 ≥ 0.20；US2 配对 008 铁律 NO-GO）：nav **29.8% vs 单次基线 47.6%**，**−17.9pp，McNemar p=0.0059（显著负）**，temporal −20pp / multi-hop −12pp 均崩。4 次机制归因重跑（vllm enable_thinking 0.6s/步、证据补足≥12、chunk-first 组装）全部显著负（25.0→32.9→34.5→29.8%）。**根因**：①该 store 裸混合检索以短 fact 为主，chunk 需 `chunk-quota` 机制强制保底——导航组装无 quota，answerer 上下文永远劣化（~500 vs 基线 3654 tokens）；②模型自主导航不转化 US1 理论空间（73% 不 stop、改写查询命中 gold 差于确定性模拟）。**推理介入检索在本 stack 负收益，US1 模拟高估真实导航救回能力**。US3（结构化导航）/US4（RL 导航）按门禁不执行。 | `--nav` 默认关（SC-004 零行为变化）；导航代码保留为评测 harness 基建（工具解析/轨迹/预算记账）。 | [spec 029](../../specs/029-agentic-memory-navigation/spec.md) · [US1 诊断](reports/029-agentic-memory-navigation-verdict.md) · [US2 配对](reports/029-agentic-memory-navigation-verdict.md) |
| 030 | mechanism-go · trace default-on | **读侧证据装配**（post-retrieval evidence mediation）：**子集配对**（84 题 = 029 实际子集 × 3 reps majority，Qwen3.6 + bge-large + DeepSeek mem0-aligned judge）——装配器 chunk-first **keep 47.6% vs base 27.4%（p=0.0455 显著）**；trace 引用链 **50.0% vs base 27.4%（p=0.0017 显著）但 vs keep 不显著（p=0.152）**；base-slim 控制（~688 tok 仍 27.4%）排除 token-削减伪赢。**全量复跑验证**（1540 题）：base 单次 **84.9%**（与历史 85.71%/85.19% 吻合 → 环境正常，子集 27.4% 系 029 难题 84 题 topk_hit 34.5% 的真实水平）；**trace 全量 3 次多数 85.91%（1323/1540，3 次 rep 稳定 85.6→85.9），vs base 单次 84.9% / 历史多数 85.19% 高 +0.72~1.01pp，净 +15 题，answer context 468 vs 3620 tok 省 7.7 倍，类别全正向无回落**——「预算下提质」落地（省 7.7 倍 token 且正确率稳定更高）。US3 条件压缩保守 PASS（cons 41.7% vs keep 47.6%，p=1.0）。**有效杠杆 = 读侧证据精炼（chunk-first + 聚焦）**，trace 是其省 token 的实现在全量站住。缺口：US3 精确 tokenizer 未启用（缺 `--counter-fingerprint`）；全量 trace 正确率单次不显著待 repeats 确认。 | `--trace-mediation` 默认开启（全量 85.91%@468tok 验证后转正，2026-08-06）；无 sidecar 时优雅降级 legacy 字节一致；`--trace-mediation=false` 显式回 legacy。`--consolidate` 仍 default-off（SC-004）。 | [spec 030](../../specs/030-evidence-mediation/spec.md) · [整体 verdict](reports/030-evidence-mediation-verdict.md) · [US2 配对](../../specs/030-evidence-mediation/diagnosis/us2-verdict.md) · [US3 配对](../../specs/030-evidence-mediation/diagnosis/us3-verdict.md) |

## 规划中的评估（未裁决，不进裁决表）

| Feature / 文档 | 状态 | 范围及结论 | 出货影响 | 证据 |
|---|---|---|---|---|
| 023 | draft | 训练式本地 Evidence Planner：消费 022 冻结 contract，只输出 Need/actions proposal，fail-closed 退回确定性 Compiler。评估认为其价值在沉淀合同/候选/合规基建，**涨点收益受 compiler-eligible residual 规模限制**，最可能 HOLD/STOP/NOT_NEEDED；启动前提是 022 US3 收口并量化 residual（当前 022=`PARTIAL`）。**95%+ 是 SaaS 层目标，非本地口径能力**：本地基线 85.19%，强 answerer 探针 89.03%，离 95 差 ~6pp，且「agent 多步推理 + 预算放大」两块无固定口径证据，须开独立 SaaS spec 用同一套预注册纪律验证。 | 不改变默认路径；SaaS 方向为独立 opt-in 产品线，分数单独口径声明，不得回填为本地涨点（死亡规则不变）。 | [spec 023](../../specs/023-local-trained-evidence-compiler/spec.md) · [可行性 + SaaS 缺口评估](reports/planner-023-saas-direction.md) |

## 判定规则

coverage、离线代理指标或单次差值都不能单独成为出货依据。对默认行为有影响的实验必须至少说明端到端结果、对照条件、噪声标尺、适用范围与明确 verdict；尚未收口的研究不得写入本索引。
