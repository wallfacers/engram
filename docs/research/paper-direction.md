---
title: 论文方向
summary: 本文定义当前评测可靠性研究方向及其证据边界；不把先导负结果写成已发表或已证实的普遍定律。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [research-direction]
tags: [research, evaluation, reliability]
---

# 论文方向

当前研究方向是评测可靠性分析，而不是再提出一套未经验证的记忆架构。当前结果只在[评测结果](../evaluation/results.md)维护；完整历史证据可从本文链接进入 archive。

## 负结果史

已有负结果包括：coverage 明显提升并不保证端到端答案提升；时序或 rerank 等局部杠杆可能在噪声范围内或导致净负；不同 answerer、judge、预算和聚合会改变绝对分数的含义。它们是可审计先导证据，不是可脱离条件外推的普遍结论。

## 共同失败机理

这些失败共同指向同一风险：把代理指标、单次运行或未对齐的评测栈误读为记忆系统因果改进。端到端回答由写入、检索、answerer、拒答策略、judge 与聚合共同决定；只改变其中一轴时，必须把其余轴固定并记录。

## 低成本止损

在投入实现前先做低成本、可复现的覆盖检查、同配置重复与冻结 transcript 重判；若效应没有超过噪声标尺，或没有兑现到端到端回答，就停止扩展该方向。该止损规则优先于为新机制补叙事。

## 下一步研究设计

研究应在多个 answerer、独立重复、冻结协议和 LoCoMo/LongMemEval 交叉验证下量化方差来源、选择偏差与代理指标转化率。早期提纲与原始证据登记在[评测可靠性历史归档](../archive/research/eval-reliability-outline-2026-07.md)。
