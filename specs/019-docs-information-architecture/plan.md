# Implementation Plan: 企业级文档信息架构

**Branch**: `019-docs-information-architecture` | **Date**: 2026-07-28 |
**Spec**: [spec.md](./spec.md)

**Input**: [Feature specification](./spec.md) and the approved
[documentation architecture design](../../docs/superpowers/specs/2026-07-28-documentation-information-architecture-design.md)

## Summary

把当前按写作时间堆叠的 `docs/` 重组为面向任务的现行正本层与独立历史证据层。实施先冻结
元数据、导航检索、归档迁移三份契约，再生成当前能力、当前结果、实验裁决、操作指南和
路线文档；完整旧记录按独有证据规则归档或删除，高频旧路径只保留无正文副本的迁移入口。
最终以全仓链接/锚点、元数据完整性、两跳可达性和固定 Q1–Q8 检索集验证。

## Technical Context

**Language/Version**: GitHub-Flavored Markdown；受限 YAML front matter；验证命令使用
Bash 5.2、Node.js 24 标准库、ripgrep 15 与 Git 2.53

**Primary Dependencies**: 无新增运行或构建依赖；只使用仓库现有命令行工具

**Storage**: Git 跟踪的文本文件

**Testing**: `node --test docs/validation/check-docs.test.mjs` 覆盖 validator 失败/通过
fixture；`node docs/validation/check-docs.mjs` 确定性检查元数据、标题、全仓本地链接与
GitHub 风格锚点、导航图、迁移页和固定 Q1–Q8；`git diff --check`、删除门证据、两次
独立语义复核；最终确认 Go 全量测试基线未受影响

**Target Platform**: GitHub 仓库阅读界面、本地 Markdown 阅读器和以仓库为知识源的
AI agent

**Project Type**: 纯文档信息架构重组；既有 Go library/CLI/MCP 产品行为不变

**Performance Goals**: 100% 现行文档两次导航内到达；Q1–Q8 各命中唯一权威主题；
全仓本地链接和锚点错误为 0；现行元数据完整率 100%

**Constraints**: 内容编辑限于 `docs/`；既有 `specs/` 只改历史设计链接；根
`README.md`、`README.zh-CN.md`、`LICENSE`、`CLAUDE.md`、宪法、代码、测试、迁移和
配置只读；不访问付费服务、不运行 LoCoMo、不引入文档站点或第三方校验依赖；正文以
中文为主

**Scale/Scope**: 基线 `docs/` 33 份 Markdown；全仓约 227 份 tracked Markdown 参与
只读链接验证；019 新增规格工件不计入产品文档分类

## Constitution Check

*GATE: Phase 0 前与 Phase 1 后均必须通过。*

| 原则 | 设计前判定 | 依据 |
|---|---|---|
| I. 本地优先、默认离线 | PASS | 全部编辑与验证在本地完成；无网络、云或模型依赖 |
| II. 引擎与适配层分离 | PASS | Go 引擎、适配器和公共 API 零改动 |
| III. 契约优先与 namespace 隔离 | PASS | 先交付元数据、导航检索、归档迁移契约；不涉及 namespace 行为 |
| IV. 评测回归门禁 | PASS / 不触发 | 不改评测实现或配方；现有数字只从已提交 verdict 提炼，不重跑或重解释 |
| V. 优雅降级与规模诚实 | PASS | 明确区分当前、opt-in、实验、提案和归档，消除未验证能力宣称 |
| 工作流与质量门禁 | PASS | brainstorming 已确认，019 spec 已验证；plan → tasks → analyze → implement 顺序执行 |

**设计前 Gate**: PASS，无需 Complexity Tracking 例外。

## Project Structure

### Documentation (this feature)

```text
specs/019-docs-information-architecture/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── validation-report.md
├── checklists/
│   └── requirements.md
├── contracts/
│   ├── document-metadata.md
│   ├── navigation-and-retrieval.md
│   └── archive-and-relocation.md
└── tasks.md
```

### Product documentation (repository root)

```text
docs/
├── README.md
├── CONTRIBUTING.md
├── validation/
│   ├── check-docs.mjs
│   ├── check-docs.test.mjs
│   └── retrieval-fixtures.json
├── guides/
│   ├── cli.md
│   └── mcp-server.md
├── architecture/
│   ├── memory-system.md
│   └── provenance.md
├── operations/
│   └── evaluation/
│       ├── locomo-runbook.md
│       └── remote-gpu-runbook.md
├── evaluation/
│   ├── results.md
│   ├── experiment-verdicts.md
│   ├── benchmark-roadmap.md
│   ├── competitors.md
│   └── reports/
│       └── memos-locomo-reproduction.md
├── product/
│   ├── capabilities.md
│   ├── roadmap.md
│   ├── backlog/
│   │   └── memory-freshness.md
│   └── explorations/
│       └── habit-memory.md
├── research/
│   └── paper-direction.md
├── archive/
│   ├── README.md
│   ├── decisions/
│   ├── evaluation/
│   ├── plans/
│   ├── research/
│   └── designs/
└── *.md
    # 仅保留仍被根入口引用的 relocated 迁移页；不含第二份正文
```

### Link-only changes outside `docs/`

```text
specs/007-judge-metric-alignment/spec.md
specs/009-retrieval-attribution-gate/spec.md
specs/010-multi-query-retrieval/spec.md
specs/011-dual-index-alias/spec.md
specs/012-doc2query-shadow/spec.md
specs/014-temporal-answer-contract/spec.md
specs/015-consolidation-bridging/{spec.md,plan.md,research.md}
specs/016-longmemeval-crossbench/{spec.md,plan.md,research.md}
specs/019-docs-information-architecture/{spec.md,plan.md}
```

019 的两处链接仅在已批准设计移入 `docs/archive/designs/` 后更新到新路径；它们属于本
feature 自身的规格维护，不改变需求或方案。

**Structure Decision**: 采用“领域化现行层 + 单独 archive + 旧路径薄迁移页”。现行层按读者
任务拆分，快速变化的分数和实验裁决各只有一个正本；archive 保存完整独有证据但退出默认
导航。该结构比只补索引更能消除内容漂移，也比合并成少数长手册更适合精确检索和并行维护。

## Phase 0: Research

研究产物 [research.md](./research.md) 固定以下决策并记录备选：

1. 正本优先级和“一个权威主题一个现行文档”规则。
2. 最小可机器识别元数据及文档/功能两套互斥状态。
3. 现有 21 个顶层文档的合并、迁移、归档与删除映射。
4. 历史设计的独有证据保留门和八份被引用设计的迁移方式。
5. 旧路径兼容策略与范围外只读链接的处理。
6. 位于 `docs/validation/`、无新增依赖的链接、锚点、元数据、孤儿和两跳可达性验证器。
7. Q1–Q8 固定检索集的判定方式。
8. 当前本地存储能力、降级边界和“表存在不等于功能出货”的事实基线。
9. 当前结果、未实现提案、已收口实验三类维护决策 fixture。

Phase 0 结束条件：所有决策均有 rationale 和 alternatives，且不存在未决澄清项。

## Phase 1: Design & Contracts

### Data model

[data-model.md](./data-model.md) 定义文档、权威主题、生命周期、功能/实验状态、证据记录、
导航条目和迁移入口；包含字段、互斥约束、唯一性、关系和状态转换。

### Contracts

- [document-metadata.md](./contracts/document-metadata.md)：front matter 必填字段、允许值、
  条件字段、中文正文和标题约束。
- [navigation-and-retrieval.md](./contracts/navigation-and-retrieval.md)：门户分类、正本唯一性、
  Q1–Q8 期望主题/结论、分数消费者回链、两跳可达与 archive 默认排除规则。
- [archive-and-relocation.md](./contracts/archive-and-relocation.md)：独有证据保留门、删除条件、
  archive 首屏声明、迁移页最大内容和旧路径兼容清单。

### Validation guide

[quickstart.md](./quickstart.md) 给出实施前基线、`docs/validation/check-docs.mjs` 命令、
过期事实扫描、变更隔离和最终回归命令及明确期望结果。实施结束时将全部确定性输出、三份
删除门证明、两次独立 Q1–Q8 复核和两次独立治理分类复核记录写入
`validation-report.md`。

## Post-Design Constitution Check

| 原则 | Phase 1 后判定 | 依据 |
|---|---|---|
| I. 本地优先、默认离线 | PASS | 三份契约、validator 和 quickstart 仅依赖本地仓库与标准工具 |
| II. 引擎与适配层分离 | PASS | data model 只描述文档实体；无产品代码接口 |
| III. 契约优先与 namespace 隔离 | PASS | 实施前已冻结文档对人和 AI 的可观察契约 |
| IV. 评测回归门禁 | PASS / 不触发 | quickstart 只核对已存在结果与变更隔离，不产生新评测口径 |
| V. 优雅降级与规模诚实 | PASS | 提案、历史和实验状态不能冒充当前能力；规模声明仍回链现行正本 |
| 工作流与质量门禁 | PASS | 设计产物覆盖 spec，下一步只生成 tasks，不提前编辑产品文档 |

**设计后 Gate**: PASS，无宪法违例，无 Complexity Tracking 条目。

## Complexity Tracking

无宪法违例或需要豁免的复杂度。
