# Contract: 归档、删除与旧路径迁移

**Version**: 1.0
**Date**: 2026-07-28

## 1. 归档原则

历史材料退出当前状态检索，不等于失去价值。满足任一条件必须保留完整证据：

- 含独有实测结果或原始数字；
- 含独有方法、复现步骤或失败诊断；
- 含独有 GO/NO-GO 门、噪声标尺或适用范围；
- 含独有决策理由；
- 仍被正式 spec、plan、research、contract、eval-log 或 verdict 引用。

保留文件移入与证据类型匹配的 `docs/archive/` 子目录，正文原则上冻结，只补元数据、历史
警告、outcome、现行替代入口和必要的链接修复。

## 2. 删除门

删除历史设计必须同时证明：

1. 正式 feature spec 已覆盖原设计的用户目标；
2. 正式 feature spec 已覆盖验收要求；
3. 正式 feature 材料包含或链接原设计的契约依据；
4. 原设计不含第 1 节定义的独有证据；
5. 删除前全仓 Markdown 入链数为 0。

任一条件不满足即不得删除。Git 历史不是绕过该门的理由。

本 feature 的候选删除清单仅限：

- `docs/superpowers/specs/2026-07-19-bio-retrieval-locomo-design.md`
- `docs/superpowers/specs/2026-07-20-cli-ai-first-design.md`
- `docs/superpowers/specs/2026-07-28-curation-lifecycle-side-table-cleanup-design.md`

实施时必须再次扫描入链；出现新的并行引用则停止删除并改为归档。

每个实际删除目标必须在
`specs/019-docs-information-architecture/validation-report.md` 单独记录五项门的证据、
入链扫描命令和删除前的零入链输出。候选清单不是删除证明。

## 3. Archive 首屏契约

每份 `archived` 文档必须：

- 使用完整通用元数据；
- 至少设置 `outcome` 或 `superseded_by`；
- 在 H1 后、第一个 H2 前声明“历史归档，不描述当前状态”；
- 给出最终 outcome 或现行替代入口；
- 保持原始数字的 dataset、model、judge、recipe 和日期上下文；
- 不出现在门户的现行目录中。

历史设计另须设置三位字符串 `feature`。设计 019 在实施完成后移到
`docs/archive/designs/` 并设置 `outcome: implemented`。

## 4. Archive 索引

`docs/archive/README.md` 按以下类别列出全部 archive：

- 决策；
- 评测；
- 计划；
- 研究；
- 设计。

每项提供标题、日期或 feature、outcome 和链接。索引不复述完整结论，也不能成为当前
分数或能力的第二正本。

## 5. 必须归档的历史设计

下列设计有正式入链，必须迁移而不能删除：

| Feature | 原文件 |
|---|---|
| 007 | `2026-07-21-judge-口径-alignment-design.md` |
| 009 | `2026-07-22-retrieval-ranking-attribution-gate-design.md` |
| 010 | `2026-07-23-multi-query-retrieval-design.md` |
| 014 | `2026-07-24-answer-side-temporal-reasoning-contract-design.md` |
| 012 | `2026-07-24-doc2query-pseudo-query-shadow-design.md` |
| 011 | `2026-07-24-write-side-alias-embedding-design.md` |
| 015 | `2026-07-25-offline-consolidation-bridging-design.md` |
| 016 | `2026-07-26-longmemeval-subset-design.md` |

迁移后，以下范围外正式材料只允许修改对应链接目标：

- `specs/007-judge-metric-alignment/spec.md`
- `specs/009-retrieval-attribution-gate/spec.md`
- `specs/010-multi-query-retrieval/spec.md`
- `specs/011-dual-index-alias/spec.md`
- `specs/012-doc2query-shadow/spec.md`
- `specs/014-temporal-answer-contract/spec.md`
- `specs/015-consolidation-bridging/{spec.md,plan.md,research.md}`
- `specs/016-longmemeval-crossbench/{spec.md,plan.md,research.md}`

不得改变这些文件的需求、状态、数字或 verdict。

本 feature 的批准设计归档后，还必须把
`specs/019-docs-information-architecture/{spec.md,plan.md}` 中指向
`docs/superpowers/specs/` 旧路径的链接更新为 archive 新路径；这两处是 019 自身工件
维护，不计入上述既有 feature 链接白名单。

## 6. Relocation 契约

以下旧路径必须保留：

| 旧路径 | 唯一 `canonical_path` |
|---|---|
| `docs/background-extraction-from-workhorse-agent.md` | `docs/architecture/provenance.md` |
| `docs/cli.md` | `docs/guides/cli.md` |
| `docs/competitive-benchmarks.md` | `docs/evaluation/competitors.md` |
| `docs/locomo-e2e-eval-reproduction.md` | `docs/operations/evaluation/locomo-runbook.md` |
| `docs/locomo-score-levers.md` | `docs/evaluation/experiment-verdicts.md` |
| `docs/mcp-server.md` | `docs/guides/mcp-server.md` |
| `docs/memory-architecture.md` | `docs/architecture/memory-system.md` |
| `docs/memory-freshness-and-retrieval-policy.md` | `docs/product/backlog/memory-freshness.md` |
| `docs/memory-strategy.md` | `docs/product/roadmap.md` |
| `docs/memos-inhouse-locomo-repro.md` | `docs/evaluation/reports/memos-locomo-reproduction.md` |
| `docs/remote-eval-box.md` | `docs/operations/evaluation/remote-gpu-runbook.md` |
| `docs/results-matrix-2026-07-26.md` | `docs/evaluation/results.md` |

每份文件只能包含 front matter、一个 H1、一段迁移说明和一个目标链接。目标必须是
`stable`、`active` 或 `proposed`，不得指向 archive 或另一个 relocated 页面。只有
`memory-freshness-and-retrieval-policy.md` 按批准映射指向 proposed backlog；其迁移
说明必须明确“未实现提案”，且当前状态答案仍由 `product/capabilities.md` 提供。

## 7. 冲突与失败处理

- 移动或删除前重新检查 `git status` 和入链。
- 若并行修改与目标文档重叠，停止该文件的迁移，报告双方意图；不得覆盖或回滚。
- 若范围外文档存在无法通过新路径或兼容入口修复的坏链，feature 阻塞并请求扩大权限。
- 链接、锚点、孤儿、元数据或状态过滤任一门失败，都不能宣称迁移完成。
- validator 输出、删除门证据、范围隔离和两次独立 Q1–Q8 复核必须写入
  `validation-report.md`，不能只存在于终端滚动记录中。
