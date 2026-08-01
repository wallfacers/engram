---
title: 信息密度杠杆三轮证伪——022/024/025 决策摘要
summary: 022(HOLD)/024/025 三轮回答了同一战略问题：同预算下对 MemOS 的信息密度差距从何而来、能否用"密度杠杆"缩小。答案：写时去重、命中邻居扩展、跨消息 episode 聚合三种"加证据/聚证据"机制全部证伪（multi-hop 0 提升，OVERALL 负），差距指向"命中后的原始证据覆盖"而非候选密度。本页是规划 026 的决策摘要。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-01
canonical_for: [density-mechanism-hypothesis-closed]
tags: [evaluation, locomo, strategy, decision, 022, 024, 025, information-density]
---

# 信息密度杠杆三轮证伪——022/024/025 决策摘要

> 用途：规划 026 前先读本页。它把 022(HOLD)/024/025 收口为一个决策命题，避免 026 重复已被证伪的方向。

## 起点：同预算信息密度差距

budget-ablation 证明 engram 同栈领先完全由 answerer 上下文预算驱动：预算从 3614 tok 降到 MemOS 量级（engram 1083 tok → 76.85%，MemOS 1059 tok → 82.47%）后，领先反转为 −5.62pp。差距定位在**同预算下的信息密度**：MemOS 的候选在同样多的 token 里带出更有效的证据。

## 三轮假设与结果

| 轮 | 密度杠杆假设 | 配对结果 | Verdict |
|---|---|---|---|
| 022 | 三表示 bake-off（chunk_900 / raw_turn_window / semantic_episode）+ Compiler 框架 | B0 85.32% continuity-only；正式双基准未完成 | **HOLD**（未收口，非失败） |
| 024 | 写时去重（write_dedup）+ 命中邻居扩展（neighbor_extend），四臂 | control 84.29% > both 82.99%，随机制叠加**单调下降**；dedup 几乎不触发 | **closed-negative**，默认关 |
| 025 | 跨消息语义聚类 episode 表示（--episode-cluster） | multi-hop **0.0pp**；OVERALL **−7.7pp**（single-hop −8.1、temporal −16.0）；同一 store 候选逐字节一致 | **closed-negative**，默认关 |

## 三个一致的机制信号

1. **"加证据"（024）不涨点**：写时去重抑制冗余投影在 LoCoMo 上几乎不触发，邻居扩展带出的兄弟事实稀释了精确覆盖。
2. **"聚证据"（025）反而有害**：episode 把多 source 压缩进一个 item，在 token cap（3600）内挤掉其他锚点的逐证据覆盖（candidate_miss 231→351），simple-hop/temporal 大幅退化，multi-hop 零提升。
3. **差距不在候选密度，在命中后的证据覆盖**：与 `022-original-span-recovery-vs-memos` 一致——MemOS 的优势是命中 fact 后沿血缘**回收原始 span**喂 answerer（比 022 少 3 倍 token），不是聚合/去重。

## 对 026 的约束（已收口，勿重复）

- ❌ **不要再做"密度聚合/去重/扩展"类机制**：write_dedup、neighbor_extend、semantic_episode 已三连证伪。
- ✅ **差距锚点 = 命中后的原始证据覆盖**：凡能"以更少 token 带出逐证据原文命中"的方向（如原始 span 回收、证据级路由），与 MemOS 的真实优势对齐。
- ✅ **同预算配对**：所有涨点声明必须回到 022 冻结协议（cap 3600）+ 同一 store 候选逐字节一致 + 宪法 IV 双基准门。
- ⚠️ **022 仍未收口**：正式 B1、LongMemEval-S、双 reviewer audit 待完成。026 若改变引擎检索/生成路径，须先使 022 具备可引用的 accepted baseline，否则 026 无对照可配。

## 相关资产

- 024 四臂：[报告](memory-density-four-arm.md) · [登记](../../../specs/024-memory-density/benchmark-registration.md)
- 025 配对：[报告](semantic-episode-cluster.md) · [登记](../../../specs/025-semantic-episode-cluster/benchmark-registration.md)
- 022 状态：[结果正本](../results.md) · [裁决索引](../experiment-verdicts.md)
- 裁决索引入口：[experiment-verdicts](../experiment-verdicts.md)
