---
title: 当前评测结果
summary: 本文维护 engram 当前可引用的 LoCoMo 与 LongMemEval-S 结果及其复现口径；不把不同评测栈的数字当作可直接比较的系统排名。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-31
canonical_for: [evaluation-results]
tags: [evaluation, locomo, longmemeval, results]
---

# 当前评测结果

本文是当前分数的唯一正本。每条结果都由数据集、答题模型、判题模型和配方共同定义；查看实验取舍请转至[实验裁决](experiment-verdicts.md)，复现步骤请转至[评测运维](../operations/evaluation/locomo-runbook.md)。

## 读取规则

- 只比较四个轴中其余条件一致的行；跨 answerer、judge、检索预算或聚合方式的绝对分数不构成系统排名。
- `full 500` 指 LongMemEval-S（cleaned）的完整 500 题，而不是早期的小样本先导运行。
- `deepseek-v4-pro` answerer 的结果是公平探针，不是产品默认模型或默认配置。

## 当前结果矩阵

| 数据集 | 样本 | 答题模型 | 判题模型 | 配方与聚合 | 结果 | 解释边界 |
|---|---:|---|---|---|---:|---|
| LoCoMo（cat 1–4） | 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | local hybrid、3 次答题多数 | 85.71% | 与同栈 MemOS 的可比基线 |
| LoCoMo（cat 1–4） | 1540 | deepseek-v4-pro | deepseek-v4-flash | canonical recipe、3 次答题多数 | 89.03% | 强 answerer 探针，不能与本地基线混作默认分 |
| LongMemEval-S（cleaned） | 500 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | local-first、3 次答题多数 | 80.80% | 历史 full 500 本地栈测量；见下方口径更正 |
| LongMemEval-S（cleaned） | 500 | deepseek-v4-pro | deepseek-v4-flash | 与本地臂相同检索、3 次答题多数 | 86.00% | 历史强 answerer 探针；同受下方口径更正约束 |

## 022/027 证据：B0 连续性 + B1 正式基线（85% 级已登记）

022 已产生一个有效但不具 promotion 资格的 LoCoMo B0 continuity：三次原始结果为
1,297、1,313、1,320/1,540，多数票 1,314/1,540（85.32%）。它真实记录 4,627 次
answer、7 次 rewrite 和 4,620 次 judge；protocol hash 为
`sha256:49ba0fa3a53afde56ac3a4a34168aea797375e9fea4bf507f7c2abda779ae41c`，
summary 文件 SHA-256 为
`4463576a0b65586d575cf08d09159960dc5bbf2ecc36f6579215cc682146fff8`。
B0 仅用于历史连续性，不能作为 B1 control 或 SC-002 结果。

### 027 B1 正式基线已登记：85% 级达成

027 修复了 B1 control 的打包粒度（chunk-verbatim fold：bundle 打包投影原文
而非 source-expand 成单条消息）并把默认 `answer-input-cap` 从 3600 提到 5000
（78.4% 的题在 3600 下被截断，实测 max answer input 4,153 tok，cap 5000 覆盖
100%）。LoCoMo B1 正式基线（Qwen3.6-35B-A3B-FP8 / bge-large-en-v1.5 /
deepseek-v4-flash，local hybrid、3 次答题多数）在 022 历史 store（restored）上：

| 口径 | 结果 | 说明 |
|---|---:|---|
| 3 次答题多数（与 B0 同口径） | **1,312/1,540（85.19%）** | ≥85% 级达成 |
| OVERALL（stats 类别均值，与 026 同口径） | 84.7% | 相对 026 control（82.6%）+2.1pp |

- validity 全绿：candidate/source/span/citation/within-cap rate 全为 1，protocol
  hash `sha256:263b52b6…`（cap 5000，store schema 7）。
- 同 store 当前环境 B0 continuity = 1,308/1,540（84.94%），B1 已追平/略超；历史
  B0 85.32%（2026-07）含 ~0.4–1.4pp 的 vllm/judge 环境漂移，当前环境两路径均为
  ~84.9–85.2% 量级。
- B1 协议禁止 legacy IDK retry（`validateEvalExperiment`），故 B0 靠 7–9 题
  rewrite 获得的 ~0.45pp 结构性不可达；B1 majority 在当前环境约 85.2% 封顶。

后续两个 LoCoMo 单次探针均不进入上方当前结果矩阵：

| 探针 | 结果 | 平均 answer context | 关键有效性边界 | Verdict |
|---|---:|---:|---|---|
| 请求 extractive compiler、实际为 legacy runner，chunk-quota=12 | 1,291/1,540（83.83%） | 3,605 token | 未带 `--eval-protocol`，compiler flag 被旧 CLI 静默忽略；非三次多数、1,546 answer + 6 rewrite | invalid mechanism evidence |
| pure fact，chunk-quota=0 | 73.70% | 1,529 token | 单次配置探针；相对前行 −10.13pp，single-hop −16.0pp | NO-GO for pure fact |

83.83% legacy 探针的无 secret artifact hashes 为：`results-hybrid.jsonl`
`bc544c43c10349528ef39f23588fa597e5c153712cdad7c1547cb141908088bc`、
`stats.json` `a86b6088fb2ed73d56fb72612d2e1dfe0b653d42c012acadb934dcbc9e68c1d0`、
`cost.json` `3005b399e52eca2ac316acf0cf40bb0e6c5d63acda3935229deae4b41a005358`。
Cost artifact 的 `actual_usd=0` 只表示 Qwen/BGE/DeepSeek 三个模型均未在价格表中定价，
不是 judge 免费；它记录 1,540 judge calls、5,572,719 answer input tokens 和
674,721 judge input tokens。

截至 2026-08-02，LoCoMo B1 正式基线已登记（上表，85% 级 majority 85.19%），
LongMemEval-S low/high B1、两 benchmark fixed-gold diagnostic、两位独立 reviewer
的 judge audit 和正式配对消融仍未形成完整有效产物。故 1,425/1,540 目标已由
027 修复达成 majority 85.19%（statistics 口径 84.7%），473/500 目标仍无
accepted result；022 状态由 `HOLD` 转为 `PARTIAL`（LoCoMo B1 基线已收口，
双基准与 audit 未完成）。

## LongMemEval-S 口径更正待刷新

两条 LongMemEval-S 500 题行均来自修复前的 store：基线 `buildSessionChunks` 会截断超过 1100 code point 的单 turn，已确认四道 gold-bearing assistant turn 的关键答案位于旧截断点之后。该缺陷使 DiaID coverage 不能证明答案片段真的进入索引或答题上下文。

现已改为无损拆分超长 turn，并在内容变化或 chunk 消失时清理旧持久化 chunk/向量。历史测量暂保留以保证可追溯性；在**重建或补齐受影响向量并完成同配方 full 500 复跑**前，不得将它们描述为已刷新基线，也不得从该修复预先声称任何涨点。详细缺陷、四题证据和替换条件见 [Feature 016 verdict](../../specs/016-longmemeval-crossbench/verdict.md)。

## 同栈竞品对照

唯一已完成的同栈对照固定 Qwen3.6-35B-A3B-FP8 answerer、bge-large-en-v1.5 embedding、同一 judge prompt 与同一 judge：LoCoMo 原始 1540 行汇总中 engram 为 85.71%，MemOS 为 82.40%。

原始数据含 11 组重复问题。按唯一 `(conv, question)` 折叠并复现 bench 的三次运行多数票后，得到 1529 个完整配对：engram 为 1310/1529（85.68%），MemOS 为 1261/1529（82.47%），差为 +3.20 个百分点；不一致对 `b=155`（engram 对、MemOS 错）、`c=106`（反向），双侧 McNemar exact `p=0.002895`。分类别结果如下：

| 类别 | n | engram | MemOS | 差值 | exact p |
|---|---:|---:|---:|---:|---:|
| single-hop | 830 | 88.80% | 82.77% | +6.02pp | 0.000014 |
| open-domain | 96 | 65.62% | 59.38% | +6.25pp | 0.326940 |
| multi-hop | 282 | 87.59% | 89.36% | -1.77pp | 0.499560 |
| temporal | 321 | 81.93% | 82.55% | -0.62pp | 0.904975 |
| **overall** | **1529** | **85.68%** | **82.47%** | **+3.20pp** | **0.002895** |

因此可以声明“固定 v4-flash 同栈下 engram 的总体领先具有配对统计证据”，且领先主要由 single-hop 驱动。更换为 deepseek-v4-pro judge 后，原始汇总为 engram 83.77%、MemOS 80.26%，但 MemOS 没有保存逐题 verdict，+3.51pp 仍不得写成“配对显著”。

这些结果不证明通用的“记忆机制领先”：MemOS 侧只有一次 answer run，两侧仍使用各自的默认检索与上下文预算。完整方法、诚实项和复算入口见[MemOS LoCoMo 同栈复现报告](reports/memos-locomo-reproduction.md)；厂商自报榜单仅供[竞品口径](competitors.md)追溯，不能横向相减。

### 上下文预算剥离

上述 +3.20pp 是在 engram ~3.4 倍上下文预算差下取得的（engram ~3614 tok/次 vs MemOS ~1059 tok/次）。为分离"预算"与"记忆机制"的贡献，在其余轴不变的前提下扫 answerer 上下文预算对齐 MemOS：

| answerer 预算 | engram | Δ(engram−MemOS) | exact p | 结论 |
|---:|---:|---:|---:|---|
| 3614 tok（009 默认）| 85.68% | +3.20pp | 0.002895 | 显著领先 |
| 2239 tok | 82.93% | +0.46pp | 0.723 | 持平 |
| 1339 tok | 79.27% | −3.20pp | 0.0099 | 显著落后 |
| 1083 tok（≈ MemOS 1059）| 76.85% | −5.62pp | 0.000006 | 极显著落后 |

领先随预算下降**单调消失并反转**；对齐 MemOS 预算后 engram 极显著落后。**因此 +3.20pp 完全由上下文预算驱动，不是记忆机制优势**——engram 需约 2.1 倍 MemOS 预算才持平，3.4 倍才显著领先；同预算下 MemOS 的 tree/graph 在 multi-hop/temporal 上显著优于 engram 扁平检索。完整数据、分类别趋势、诚实边界与复算入口见[上下文预算剥离报告](reports/budget-ablation.md)。

## 结果维护要求

更新一行结果时必须同时登记数据集版本与样本数、answerer、judge、完整 recipe、聚合方式、日期和证据路径。早期小样本数字仅作为[历史先导](../archive/evaluation/longmemeval-100-pilot-2026-07.md)保存，不能作为当前结论引用。
