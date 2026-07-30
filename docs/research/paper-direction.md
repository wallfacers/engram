---
title: 论文方向
summary: 本文定义当前评测可靠性与双基准记忆架构研究方向及其证据边界；不把先导负结果或外部高分写成已证实的自身能力。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-29
canonical_for: [research-direction]
tags: [research, evaluation, reliability, memory]
---

# 论文方向

当前研究有两条受同一证据纪律约束的支线：继续做评测可靠性分析；同时基于
[长期记忆系统成绩与机制证据](high-scoring-memory-systems.md)探索能在 LoCoMo 与
LongMemEval-S 上超过 Mem0 数值目标的下一代结构。后者目前只是
[未实现 proposed 探索](../product/explorations/benchmark-parity-memory-architecture.md)，
不是当前能力或已批准实现。当前结果只在[评测结果](../evaluation/results.md)维护；
完整历史证据可从本文链接进入 archive。

## 负结果史

已有负结果包括：coverage 明显提升并不保证端到端答案提升；时序或 rerank 等局部杠杆可能在噪声范围内或导致净负；不同 answerer、judge、预算和聚合会改变绝对分数的含义。它们是可审计先导证据，不是可脱离条件外推的普遍结论。

## 共同失败机理

这些失败共同指向同一风险：把代理指标、单次运行或未对齐的评测栈误读为记忆系统因果改进。端到端回答由写入、检索、answerer、拒答策略、judge 与聚合共同决定；只改变其中一轴时，必须把其余轴固定并记录。

当前最强的跨系统证据是 MemOS 同栈逐题配对复现；[当前结果正本](../evaluation/results.md)记录了总体统计证据。这足以支撑“该固定栈下总体领先具有统计证据”，但 MemOS 单 answer run、显著不同的上下文预算以及 v4-pro 缺逐题标签，仍阻止将结果写成“记忆机制优于 MemOS”。事实上[上下文预算剥离](../evaluation/reports/budget-ablation.md) 已证明该 +3.20pp 完全由上下文预算驱动：对齐 MemOS 预算（1083 ≈ 1059 tok）后 engram 极显著落后（−5.62pp，exact p=0.000006），持平交叉点约 2240 tok（2.1 倍 MemOS 预算）。这把“领先”从机制优势降级为预算效应——一个干净的负面证据，也说明“固定其余轴”必须包括上下文预算这一轴。详细方法、数值和复算入口见[MemOS 同栈复现报告](../evaluation/reports/memos-locomo-reproduction.md)。

## 低成本止损

在投入实现前先做低成本、可复现的覆盖检查、同配置重复与冻结 transcript 重判；若效应没有超过噪声标尺，或没有兑现到端到端回答，就停止扩展该方向。该止损规则优先于为新机制补叙事。

## 论文复核给出的机制假设

Chronos、EverMemOS、Mandol、True Memory、ByteRover、Mi-Memory 和 Hindsight 的
高分来自不同完整 bundle，不能据此推出层级图、event 或多 memory space 的独立贡献。
更干净的实验反而支持：先比较语义 episode 与固定分段；在固定候选后做 source-grounded
evidence planning、span extraction 和预算组装；只有原始证据装不下时才压缩。

这个外部证据与[上下文预算剥离](../evaluation/reports/budget-ablation.md)一致：当前
路径在同预算下仍有 multi-hop/temporal 缺口。下一轮先区分 candidate miss 与
compiler miss，测试“Evidence Ledger + Semantic Episode + Query-time Evidence
Compiler”，不预建完整 event/scene/profile/graph 层级，也不把新信号接到同一个扁平
RRF 后扩大 top-k。

## 下一步研究设计

评测可靠性支线应在多个 answerer、独立重复、冻结协议和 LoCoMo/LongMemEval
交叉验证下量化方差来源、选择偏差与代理指标转化率。早期提纲与原始证据登记在
[评测可靠性历史归档](../archive/research/eval-reliability-outline-2026-07.md)。

架构支线先刷新 lossless-chunk 后的双基准，再在相同候选和 token cap 下比较固定字符块、
raw-turn window 与 semantic episode；随后冻结候选比较 exact-token packer 和
extractive/compiler 路径。只有剩余错题给出依据时，才分别试 Event、一次缺口补检、
Scene、Profile 或 graph。详细合同和 stop/go 顺序只维护在
[查询期证据编译架构探索](../product/explorations/benchmark-parity-memory-architecture.md)。
