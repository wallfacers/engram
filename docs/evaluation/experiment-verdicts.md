---
title: 实验裁决索引
summary: 本文汇总已收口实验的可执行 verdict 与证据入口；不提供当前完整分数矩阵或未来功能承诺。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-29
canonical_for: [experiment-verdicts]
tags: [evaluation, verdicts, evidence]
---

# 实验裁决索引

本文是已收口实验的唯一裁决入口；完整当前分数见[当前评测结果](results.md)，逐次过程、旧基线和原始数字见[LoCoMo 历史实验台账](../archive/evaluation/locomo-experiment-ledger-2026-07.md)。本页不把覆盖率、代理指标或单次差值写成默认能力。

## Feature 003–018 的交付与实验裁决

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

## 判定规则

coverage、离线代理指标或单次差值都不能单独成为出货依据。对默认行为有影响的实验必须至少说明端到端结果、对照条件、噪声标尺、适用范围与明确 verdict；尚未收口的研究不得写入本索引。
