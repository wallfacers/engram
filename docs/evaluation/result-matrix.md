---
title: 评测结果全量矩阵（复跑全集）
summary: 按数据集/答题模型/判题模型/检索/top-k/契约/口径为维度的完整评测矩阵。统一契约是唯一允许的 prompt 路径；过时与已证伪项不入主表，仅归档。每行标注复跑状态。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
canonical_for: [evaluation-result-matrix]
tags: [evaluation, locomo, longmemeval, result-matrix, rerun]
---

# 评测结果全量矩阵（复跑全集）

本文是把散落在各 verdict 里的评测结果重排成的**复跑全集**。目的：(1) 一个表格看清
全部可复用的分数；(2) 明确哪些分数已过时、哪些需要重跑；(3) 统一未来复跑的 recipe——
**任何 prompt 必须是数据集无关的统一契约，禁止 category-routed / force-answer / 特调示例**。

## 读取规则

- **只比较四轴其余条件一致的行**：跨 answerer / judge / top-k / 聚合口径的绝对分不构成系统排名。
- **judge 跨批漂移 ≥±2.5pp**，单次 run 系统噪声 ~8.6pp（037 实测）。跨批绝对分一律当"参考"不当"证据"。
- **任何 ≥90 的分数必须报 clean 口径**（`extractFinalAnswer` 剥离 thinking），raw 会被 judge 从思考前导读候选（作弊量跨数据集一致 ~1.2–1.5pp）。
- **clean 口径 = 最终答案交付口径**：engram 产品不产答案（宿主 agent 交付 final answer，thinking 是内部过程），用户判对错只看最终答案——clean 对应真实使用；raw（judge 读完整输出含 thinking）在真实使用中无对应物，judge 从 thinking 导读候选（作弊）或被 thinking 误导（042 实测 raw 跨批漂移 ±3.4pp vs clean ±0.3pp）均为评测伪影。
- **LoCoMo 绝对分不作产品能力声明**：answerer 是评测替身（Qwen），真实答题质量由宿主 agent 决定；分数仅用于同批、同四轴栈间相对比较（unified vs legacy、k30 vs k150）。
- 统一契约（`--unified-answer-contract`）是唯一允许的 answer prompt；下表 legacy/特调 行的分数仅作历史对照，不得作为产品能力声明。

## 主表 · LoCoMo（1,540 题，category 1–4）

| 数据集 | 答题模型 | 判题模型 | 检索模式 | top-k | 契约/提示词 | 聚合·口径 | 得分 | 得分说明 | 复跑状态 |
|---|---|---:|---|---|---|---:|---|---|
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid + trace | 30 | legacy（历史 control） | 3-rep majority | **85.91%** @468tok | 默认栈：trace 读侧证据中介，token 省 7.7×；base 单次 84.9%，+0.72~1.01pp（[030](reports/030-evidence-mediation-verdict.md)） | ⚠️ **重跑**：需在同批 judge + clean 口径下重建正式基线；base 侧严格 3-rep 配对未跑 |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 30 | legacy | thinking 3-rep mean | 88.23% | 思考解锁增量（禁→开 +1.4~1.8pp，[topk](reports/topk-exploration-2026-08-11.md)） | ⚠️ **重跑**：legacy 特调 prompt 栈，需统一契约下重测 thinking 增量 |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 30 | legacy | thinking 3-rep majority | 88.51% | 多数票口径（[topk](reports/topk-exploration-2026-08-11.md)） | ⚠️ 同上 |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 150 | legacy | thinking 3-rep majority | 90.13% | top-k150 全局扩预算（+1.6pp，2.4× 上下文税，需 32768 上下文 answerer，[topk](reports/topk-exploration-2026-08-11.md)） | ⚠️ 高分 recipe，但 legacy prompt + 加量型，保留作目标参考 |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 150 | legacy | thinking 3-rep majority + **clean** | **91.10%** (1403/1540) | **当前最高已验证**，独立重判一致（[repro](reports/locomo-9110-repro-2026-08-12.md)）；open-domain 68.8% 是短板 | ✅ **已验证可复现**，作当前基线锚 |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 30 | **unified** | 配对 3-rep majority | **87.9%**（control 86.6%） | **+1.4pp above-noise（p=0.019）**；cat3 -2.1pp（flips 4v2，038 -6.2pp/8v2，推断压制基本解决，残余 35 题两臂都错为检索/能力瓶颈）；temporal +1.2 / single-hop +2.1；context parity 3-run 全过；分类修订后契约 digest `1d8a8d0f`（[038](reports/unified-answer-contract-verdict-2026-08-13.md)） | ✅ **已坐实**（2026-08-14 修订后）；top-k150 高配**已取消**（042 配对已跑：k150 见下行，within-noise，勿复跑） |
| LoCoMo 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 150 | **unified** | 配对 3-rep majority | **89.42%**（control 88.85%） | **+0.57pp within-noise（p=0.386）**；修回47/害38净+9；single +1.9 为唯一收益、multi 0、temporal −1.2 / open −3.1 净害；§5b 推演净+23 未达（危害池 14→38）；control 离线 clean 重判 **91.36%**（1407/1540）复现并略超 91.10 锚（在线 raw 89.22 为口径假象；clean 跨批稳定 vs raw 漂移 ±3.4pp）；unified clean 同法 **91.43%**（1408/1540），clean 下仍 within-noise（Δ+1 题）（[042 交接](../operations/evaluation/042-unified-k150-run-handoff-2026-08-14.md)） | ✅ 042 配对（2026-08-14）；unified 修 prompt 病 / k150 修上下文量两机制正交，unified 不威胁 legacy k150 高分栈 |

## 主表 · LongMemEval-S（cleaned 500 题）

| 数据集 | 答题模型 | 判题模型 | 检索模式 | top-k | 契约/提示词 | 聚合·口径 | 得分 | 得分说明 | 复跑状态 |
|---|---|---:|---|---|---|---:|---|---|
| LME-S 500 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 150 | legacy | 3-rep + clean | **84.60%** | clean 真实基线（judge 作弊 +1.6pp 已剥除，[rejudge](reports/lme-clean-rejudge-2026-08-12.md)） | 🔴 **重跑**：store 在 buildSessionChunks 修复前（1100 code point 截断），须重建后同配方重跑 |
| LME-S 500 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 30 | **unified** | 配对 3-rep + clean | **90.2%**（control 85.8%） | **+4.4pp，McNemar p=0.000112 above-noise**；preference +30.0pp（20→29/30）、multi-session +8.3、knowledge-update +7.7、temporal -0.8 / assistant 0.0 持平；context parity 3-run 全过；分类修订后契约 digest `1d8a8d0f`（[038](reports/unified-answer-contract-verdict-2026-08-13.md)） | ✅ **已坐实**（2026-08-14） |
| LME-S 500 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | hybrid | 150 | **unified** | 3-rep + clean majority | **92.0%**（460/500；control 1-rep 87.0%） | 3-rep 高度一致（91.0/92.0/91.8）；clean majority **92.0%**；preference +33.4（19→29/30）、knowledge-update +6.4、multi-session +6.0、temporal +1.5、assistant/user 0.0；对照 control 为 1-rep（87.0%）非严格配对；context parity 全过；契约 digest `1d8a8d0f`（[补跑记录](../operations/evaluation/lme-unified-k150-3rep-2026-08-15.md)） | ✅ 3-rep majority 坐实（2026-08-15）；unified 3-rep 稳定、control 1-rep 参考 |

（旧 post-hoc 90.4% / control 86.1% 已归档——无 parity、assistant -4.1pp 为配对伪影。）

## 探针（非主路径，仅记录，不构成引擎能力）

| 数据集 | 答题模型 | 判题模型 | top-k | 得分 | 说明 |
|---|---|---|---:|---|---|
| LME-S 500 | deepseek-v4-pro | v4-flash | 150 | 83.60% | v4-pro thinking 开实测：不优于 Qwen（84.60%），**强 answerer 不是 90 路径**（[lme-90pp](reports/lme-90pp-assessment-2026-08-12.md)） |
| LME-S 500 | deepseek-v4-pro | v4-flash | 150 | 79.2% | 禁思考探针（rep-1 未完成），不可比，仅证明 LME 依赖推理 |
| LoCoMo 1540 | deepseek-v4-pro | v4-flash | — | 89.03% | 强 answerer 探针（3-rep majority，不同协议），非默认分 |

## 过时 / 已证伪（不入主表，仅归档）

这些分数**不再使用**：prompt 已从代码移除、为特调（in-sample / category-routed）、或被配对证伪。复跑一律走统一契约，不再碰这些。

| 项 | 曾测得 | 为何过时 |
|---|---|---|
| LME entity-verify 融合 | 89.80% / 90.00% | 用测试集真实陷阱写进示例 = **in-sample tuning**；`--lme-entity-verify` 已随 038 移除 |
| v4-pro + 融合 prompt | LME 91.1% | 付费 answerer + 已移除 prompt，post-hoc 诊断，非系统能力 |
| `--lme-typed-prompts`（C 臂） | LME 87.00%（+0.8pp within-noise） | category 路由特调，被统一契约取代 |
| `--abstain-prompt`（B 臂） | LME 85.80%（-0.4pp） | NO-GO，拒答杠杆耗尽 |
| `--answer-focus-prompt` | LoCoMo 400 子集 -0.13pp | NO-GO，已 revert |
| `--temporal-answer-prompt`（tplan） | 弱栈 +11.2pp；生产栈 within-noise | category 特调 + 强栈边际归零，default-off |
| `--counter-refine` | LME 86.40% vs 86.80%（-0.4pp） | 组合配方无正向信号；独立效应未识别 |
| 037 记忆专用 reranker | LoCoMo -1.1pp | NO-GO（cross-encoder 永进本地栈，死亡规则） |
| 034 裁决协议（候选+裁决） | 89.48% | 旧协议，非当前主路径 |
| 写侧/检索侧证伪线（027/028/029/031/033） | — | 已证伪，无分数 |

## 什么样的分数需要重跑（判定规则）

满足任一即**不可作为当前证据**，必须重跑：

1. **judge 非 clean / 旧口径**：judge 从 thinking 作弊 +1.2~1.5pp（跨数据集一致）。history 分数一律重判。
2. **judge 跨批 / 非同批对照**：跨批漂移 ≥±2.5pp。配对实验必须同批 judge 交错。
3. **单次 1-rep**：单次 run 系统噪声 ~8.6pp（037）。检测 ~1pp 级杠杆须 repeats≥3 + `--store-dir` 复用。
4. **非配对 / post-hoc**（如 unified LME 90.4%）：无 context parity 校验，增量不可归因。
5. **特调 prompt 的分数**（category-routed / force-answer / entity-verify / typed）：已移除，只作历史对照。
6. **embed 512-cap 降级的 run**：bge-large 512 token 上限，超长 chunk 不嵌入（语义降级恒定偏置）；重跑须修客户端截断或明确标注。
7. **store 修复前的分数**：buildSessionChunks 曾截断超 1100 code point 的单 turn（四道 gold-bearing 答案受影响），重建向量前不得作基线。
8. **跨配置绝对分**（不同 top-k / thinking / judge）：只允许同配置 arm-to-arm 增删，不作系统排名。

## 复跑建议顺序

1. ✅ **LME unified 配对**（2026-08-14 完成）：truncate 修 embed 512 上限 → 配对（context parity）+ 同批 judge + 3-rep + clean → **+4.4pp above-noise 坐实**。
2. ✅ **LoCoMo unified 配对**：top-k30 已坐实（+1.4pp above-noise，2026-08-14）；top-k150 042 配对已跑（unified 89.42 vs control 88.85，within-noise，2026-08-14，见主表）。
3. **重建 LME 基线**：修复 store（buildSessionChunks 1100 code point 截断）后同配方重跑 clean 84.60% 基线。
4. **重建 LoCoMo trace 默认栈基线**：同批 judge + clean，与 85.91% 对齐。
5. **held-out 行为门**（149+ 题 blinded 人工标注）作为 unified 契约 promotion 前置。
6. 全程遵守宪法 IV：eval-config 与算法改动分开 commit，prompt 只允许统一契约，跨批/单次分数一律标注。
