---
title: 产品路线图
summary: 本文维护当前已批准的产品方向与排除项；不复制评测分数或重新开启已收口的 LoCoMo 杠杆。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [product-roadmap]
tags: [product, roadmap, decisions]
---

# 产品路线图

路线图只记录仍在维护或待决的产品方向。当前性能证据在[评测结果](../evaluation/results.md)，已收口实验在[实验裁决](../evaluation/experiment-verdicts.md)。

## 当前方向

- 保持显式写入、namespace 隔离和离线降级的可靠边界。
- 将新鲜度与状态一致性作为独立的未实现 backlog，先定义可审计状态模型再进入实施。
- 把评测可靠性、协议记录与可复现性作为持续维护能力，而不是把单次涨分当作路线输入。

## 明确排除

已收口的 LoCoMo reranker、时间窗检索打分和其他仅改善代理指标的杠杆不在当前路线中。任何重启都必须有新的端到端证据与预先声明的验收门，而不是复用历史分数或覆盖率结论。

## 决策历史

路线形成时的完整历史推理保存在[2026-07 决策归档](../archive/decisions/memory-strategy-2026-07.md)。归档解释来由，不改变本页的当前优先级。
