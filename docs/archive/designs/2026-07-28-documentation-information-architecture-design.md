---
title: engram 文档信息架构重组设计
summary: 本文冻结 Feature 019 的文档信息架构设计与验收依据；不描述当前待实施工作或当前主题正本。
status: archived
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [documentation-reorganization-design]
tags: [archive, design, documentation, governance]
outcome: implemented
superseded_by: docs/README.md
feature: "019"
---

# engram 文档信息架构重组设计

> **历史归档，不描述当前状态。** Feature 019 已实施；请从[文档门户](../../README.md)进入当前文档。

**日期**：2026-07-28

**设计状态**：已获维护者确认，等待实施

**范围**：`docs/`；仅为修复引用而定向更新 `specs/` 中指向历史设计稿的链接

## 1. 背景与问题

`docs/` 当前有 32 份 Markdown：顶层索引 1 份、顶层正文 20 份、
`docs/superpowers/specs/` 历史设计稿 11 份。内容本身包含大量有价值的一手实验记录，
但“当前能力、操作手册、战略方向、未实现提案、实验结论和历史过程”混在同一层。

只读审计得到以下事实：

- 176 个 Markdown 链接当前均有有效目标，标题层级也基本正确；问题不在基础格式，
  而在信息架构和内容生命周期。
- `docs/README.md` 漏掉远端评测机手册，并把 LongMemEval-S 的状态写成
  “100 题已跑、全量 500 未跑”，与当前 500 题结果正本冲突。
- CLI、curation 已交付，但北极星和历史设计仍把它们描述成未来或待实现能力。
- LoCoMo 杠杆线已经收口，顶层索引仍把完整实验流水账标为活跃文档。
- 当前论文提纲的前半部分仍以旧定位为主，文末 prior-art 复核已经建议改变定位。
- 32 份文档均无统一 front matter；状态词、权威边界、维护者和复核日期不可机器解析。
- 6 份历史设计稿没有可点击的 Markdown 入链；其中 3 份仍被正式 spec 以行内路径提及，
  另 3 份连文本引用也没有。多份设计稿页首状态仍停在“待形式化”或“待实现”。
- 多份文档反复复制评测分数、竞品差距和技术方向，更新时只能继续在旧正文顶部叠加
  更正，形成“声明了正本但仍多点维护”的状态。

因此，本次工作的核心不是美化索引，而是建立可执行的正本层级，把现行事实和历史证据
彻底分开。

## 2. 目标与非目标

### 2.1 目标

1. **一个问题只有一个现行正本**：当前能力、当前分数、实验最终裁决、操作方法和未来
   方向分别有唯一入口。
2. **用户按任务导航**：使用者无需先理解仓库历史，即可找到接入、运维、架构和评测文档。
3. **AI 可判别状态与权威性**：每份文档携带机器可读元数据；AI 能区分已交付、提案、
   NO-GO 和历史记录。
4. **历史证据可审计但不污染当前回答**：负结果、诊断和必要设计快照保留在
   `archive/`，不进入默认当前状态检索路径。
5. **消除内容漂移**：修复 LongMemEval 100/500、CLI、curation、LoCoMo 后续方向和
   论文定位等已知矛盾。
6. **保持并行入口工作安全**：不修改根 `README.md` 和 `LICENSE`；旧入口通过薄迁移页
   保持可达。

### 2.2 非目标

- 不修改 Go 代码、数据库迁移、公共 API 或评测实现。
- 不重写 `.specify/memory/constitution.md`、根 `README.md` 或 `CLAUDE.md`。
- 不借本次整理改变已完成实验的数字、判据或最终 verdict。
- 不把 `specs/NNN-*/plan.md`、`tasks.md` 的历史复选框统一修成项目状态。
- 不在线探测外部链接；本次只保证仓库内链接和 Markdown 锚点有效。
- 不引入站点生成器、文档服务或新的构建依赖。

## 3. 正本优先级

发生冲突时，信息按以下顺序解释：

1. `.specify/memory/constitution.md`：不可协商原则和质量门禁。
2. 代码、测试、`store/migrations.go`：当前真实行为、默认值和 Schema。
3. `specs/NNN-*/spec.md` 与 `contracts/`：特性需求和对外契约。
4. `docs/` 中标记为 `stable` 或 `active` 的用户指南与架构文档：对当前实现的解释。
5. `docs/evaluation/results.md` 与单 feature 的 `eval-log.md` / `verdict.md`：评测数字和
   实验事实。
6. roadmap、backlog、exploration：未来方向，不证明当前能力。
7. `docs/archive/`：历史上下文和证据，不用于回答“当前是否支持”。

根 `README.md`、`CLAUDE.md` 和 `docs/README.md` 都是入口，不应成为快速变化的分数、
Schema 版本或 feature 状态的第二正本。

## 4. 方案比较

### 4.1 方案 A：只补索引与元数据

保留全部文件路径和正文，只重写 `docs/README.md` 并添加 front matter。链接改动最少，
但旧正文中的冲突、重复和“顶部叠更正”仍然存在，不能解决正本漂移。

### 4.2 方案 B：现行正本与历史证据分层

按使用、架构、运维、评测、产品、研究六个领域建立现行文档；完整实验流水账、旧决策和
必要设计快照进入 `archive/`。现行正文合并去重，旧高频入口保留薄迁移页。

这是选定方案。它同时满足用户阅读、AI 检索和实验审计，且不要求引入文档平台。

### 4.3 方案 C：合并为少数大型手册

把现有内容压成产品、技术、评测三到五份长文。入口最少，但会重新制造长文件、多主题、
高冲突更新和粗粒度检索，不适合当前研究记录较多的仓库。

## 5. 目标目录

```text
docs/
├── README.md
├── CONTRIBUTING.md
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
└── archive/
    ├── README.md
    ├── decisions/
    │   └── memory-strategy-2026-07.md
    ├── evaluation/
    │   ├── locomo-experiment-ledger-2026-07.md
    │   ├── locomo-single-multihop-diagnosis-2026-07.md
    │   ├── temporal-t4-analysis-2026-07.md
    │   └── longmemeval-100-pilot-2026-07.md
    ├── plans/
    │   ├── benchmark-expansion-2026-07.md
    │   └── local-model-eval-stack-2026-07.md
    ├── research/
    │   ├── eval-reliability-outline-2026-07.md
    │   └── synthius-mem-analysis-2026-07.md
    └── designs/
        └── 2026-*.md
```

旧入口的薄迁移页不在上图展开。它们只用于兼容根 `README.md`、`CLAUDE.md` 和既有深链，
不属于现行内容集合，也不进入 `docs/README.md` 的默认阅读路径。

## 6. 文档元数据与生命周期

### 6.1 现行文档必填 front matter

```yaml
---
title: 文档标题
summary: 本文回答的问题及其权威边界
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memory-architecture]
tags: [memory, architecture]
---
```

字段语义：

- `title`：与唯一 H1 含义一致。
- `summary`：用一句话说明本文回答什么，以及不回答什么。
- `status`：文档生命周期，不代表功能生命周期。
- `audience`：允许 `users`、`maintainers`、`agents`。
- `owner`：使用稳定团队名，不绑定个人。
- `last_reviewed`：最后一次人工核验内容与当前事实一致的日期。
- `canonical_for`：AI 查询意图使用的稳定、短横线分隔关键词。
- `tags`：主题词，只用于发现，不声明权威性。

### 6.2 文档状态枚举

| 状态 | 含义 |
|---|---|
| `stable` | 已交付内容的现行正本，持续维护 |
| `active` | 正在推进、仍可能变化的现行文档 |
| `proposed` | 尚未实现的设计、探索或 backlog |
| `archived` | 历史快照或已收口证据，不描述当前状态 |
| `relocated` | 兼容旧路径的薄迁移页 |

归档文档必须增加 `superseded_by` 或 `outcome`；历史设计可增加 `feature`。迁移页必须增加
`canonical_path`，正文只保留一段迁移说明和一个链接。

### 6.3 功能与实验状态

文档状态与功能状态分离。`product/capabilities.md` 和
`evaluation/experiment-verdicts.md` 使用以下功能/实验枚举：

- `shipped-default`
- `shipped-opt-in`
- `experimental-default-off`
- `planned`
- `closed-no-go`
- `cancelled-before-implementation`

由此避免“代码存在”“默认出货”“实验通过”和“未来计划”被一个“活跃”状态混为一谈。

## 7. 现行文档职责

### 7.1 `docs/README.md`

同时服务用户和 AI，但只承担导航：

- “按任务查找”：接入 CLI/MCP、理解架构、运行评测、查看分数、查看产品方向、查询历史。
- “AI 检索协议”：列出正本优先级、状态语义和 archive 使用规则。
- “完整现行目录”：每份现行文档一行，给出摘要、状态和权威范围。
- “历史入口”：只链接 `archive/README.md`，不把全部历史文件铺在主导航。

### 7.2 `docs/CONTRIBUTING.md`

定义文档新增、修改和归档规则：

- 一份文档只回答一个主问题。
- 当前分数只在 `evaluation/results.md` 维护。
- 实验最终 verdict 只在 `evaluation/experiment-verdicts.md` 汇总；细节回链 feature 证据。
- roadmap、exploration 必须显式说明非当前能力。
- 一个 H1、标题层级不跳级、同文件标题 slug 不重复。
- 新文档必须从 `docs/README.md` 或最近一级索引获得入链。
- 归档前先把仍有效结论迁入现行正本，再冻结原文。
- 中文为主；命令、字段、API、协议名和模型名保留原文。

### 7.3 当前能力与路线

`product/capabilities.md` 只描述已交付或明确默认关闭的当前能力，并链接代码、契约和指南。
它不复制完整评测矩阵。

`product/roadmap.md` 合并北极星和战略文档仍然有效的方向，删除“追赶 MemOS 5pp”、
“实体图和时间结构仍待验证”等已被后续证据推翻的执行建议。已收口的 LoCoMo 杠杆线只
保留一句结论并链接 verdict 索引。

### 7.4 评测正本

`evaluation/results.md` 是当前数字唯一正本。每个数字必须携带
“数据集 × 答题模型 × 判题模型 × 配方”四元组；现行 LongMemEval-S 只按全量 500 题
表述。100 题先导实验移入 archive，不能在当前结论段与全量结果并列。

`evaluation/experiment-verdicts.md` 为 003–018 的实验性工作提供短表，至少包含：

- feature / 日期
- 假设与范围
- 最终状态
- 对出货的影响
- 一句话结论
- 正式 spec 和证据链接

完整 `locomo-score-levers.md` 冻结为历史 ledger，不再逐段回填新状态。

### 7.5 操作手册

`operations/evaluation/locomo-runbook.md` 只负责评测 recipe、环境变量、复现步骤和已验证
陷阱。

`operations/evaluation/remote-gpu-runbook.md` 只负责租用机器生命周期、模型服务、
端口、产物持久化和停机纪律。旧本地模型选型计划不得继续充当现行 runbook。

### 7.6 产品问题与探索

`product/backlog/memory-freshness.md` 保留正式问题定义、失败模式、验收方向和能力边界；
删除包含个人姓名和口语化内容的原始聊天记录。

`product/explorations/habit-memory.md` 保留完整产品/技术设计，但状态固定为 `proposed`，
首屏明确“未立项、未实现”，不得从中推断当前公共 API。

### 7.7 研究方向

`research/paper-direction.md` 采用现有论文提纲文末 prior-art 复核后的方向：完整负结果史、
共同失败机理和低成本止损方法。旧“评测可靠性审计”提纲整体归档，避免旧 headline 和新
定位在同一现行文档中互相否定。

## 8. 逐文件迁移

| 当前文件 | 处置 | 目标 |
|---|---|---|
| `docs/README.md` | 原位重写 | 当前门户与 AI 检索协议 |
| `background-extraction-from-workhorse-agent.md` | 提炼现状并迁移；保留薄迁移页 | `architecture/provenance.md` |
| `benchmark-expansion-plan.md` | 拆分未来事项与历史计划 | `evaluation/benchmark-roadmap.md` + `archive/plans/benchmark-expansion-2026-07.md` |
| `capability-and-product-north-star.md` | 合并去重后删除旧入口 | `product/capabilities.md` + `product/roadmap.md` |
| `cli.md` | 迁移；保留薄迁移页 | `guides/cli.md` |
| `competitive-benchmarks.md` | 去除当前分数副本后迁移；保留薄迁移页 | `evaluation/competitors.md` |
| `local-model-eval-setup.md` | 冻结归档 | `archive/plans/local-model-eval-stack-2026-07.md` |
| `locomo-e2e-eval-reproduction.md` | 迁移；保留薄迁移页 | `operations/evaluation/locomo-runbook.md` |
| `locomo-score-levers.md` | 提炼 verdict 后冻结；保留薄迁移页 | `evaluation/experiment-verdicts.md` + `archive/evaluation/locomo-experiment-ledger-2026-07.md` |
| `locomo-single-multihop-failure-diagnosis.md` | 冻结归档 | `archive/evaluation/locomo-single-multihop-diagnosis-2026-07.md` |
| `mcp-server.md` | 迁移；保留薄迁移页 | `guides/mcp-server.md` |
| `memory-architecture.md` | 迁移；保留薄迁移页 | `architecture/memory-system.md` |
| `memory-freshness-and-retrieval-policy.md` | 正式化并迁移；保留薄迁移页 | `product/backlog/memory-freshness.md` |
| `memory-strategy.md` | 提炼有效路线后冻结；保留薄迁移页 | `product/roadmap.md` + `archive/decisions/memory-strategy-2026-07.md` |
| `memos-inhouse-locomo-repro.md` | 迁移；保留薄迁移页 | `evaluation/reports/memos-locomo-reproduction.md` |
| `paper-outline-eval-reliability.md` | 重写当前方向并冻结旧提纲 | `research/paper-direction.md` + `archive/research/eval-reliability-outline-2026-07.md` |
| `remote-eval-box.md` | 迁移；保留薄迁移页 | `operations/evaluation/remote-gpu-runbook.md` |
| `results-matrix-2026-07-26.md` | 重写当前正本并抽出 100 题历史；保留薄迁移页 | `evaluation/results.md` + `archive/evaluation/longmemeval-100-pilot-2026-07.md` |
| `saas-habit-memory-design.md` | 迁移并固定 proposed 状态 | `product/explorations/habit-memory.md` |
| `synthius-mem-analysis.md` | 冻结归档 | `archive/research/synthius-mem-analysis-2026-07.md` |
| `temporal-t4-design.md` | 冻结归档 | `archive/evaluation/temporal-t4-analysis-2026-07.md` |

## 9. 历史设计稿处置

`docs/superpowers/specs/` 不再作为独立文档类别保留。

以下三份既没有 Markdown 入链，也没有正式 spec 的行内路径引用；对应正式 spec 已完整
承接其需求与契约，直接删除：

- `2026-07-19-bio-retrieval-locomo-design.md`
- `2026-07-20-cli-ai-first-design.md`
- `2026-07-28-curation-lifecycle-side-table-cleanup-design.md`

以下八份仍被正式 spec、plan 或 research 通过 Markdown 链接或行内路径引用，移入
`docs/archive/designs/`，把引用统一改成可点击链接，并在 front matter 中标明对应
feature 和最终 outcome：

- `2026-07-21-judge-口径-alignment-design.md`
- `2026-07-22-retrieval-ranking-attribution-gate-design.md`
- `2026-07-23-multi-query-retrieval-design.md`
- `2026-07-24-answer-side-temporal-reasoning-contract-design.md`
- `2026-07-24-doc2query-pseudo-query-shadow-design.md`
- `2026-07-24-write-side-alias-embedding-design.md`
- `2026-07-25-offline-consolidation-bridging-design.md`
- `2026-07-26-longmemeval-subset-design.md`

本设计在实施完成后也移入 `docs/archive/designs/`，状态改为 `archived`。

## 10. 兼容策略

审计开始时，根 `README.md` 有并行修改，`LICENSE` 是并行产生的未跟踪文件；设计自审
期间，这些工作已由并行任务提交为 `fac6c3c` 和 `617b692`。本任务不编辑二者，也不为
修链接重写 `CLAUDE.md`。

为避免既有入口失效，下列旧路径保留 `relocated` 薄迁移页：

- `background-extraction-from-workhorse-agent.md`
- `cli.md`
- `competitive-benchmarks.md`
- `locomo-e2e-eval-reproduction.md`
- `locomo-score-levers.md`
- `mcp-server.md`
- `memory-architecture.md`
- `memory-freshness-and-retrieval-policy.md`
- `memory-strategy.md`
- `memos-inhouse-locomo-repro.md`
- `remote-eval-box.md`
- `results-matrix-2026-07-26.md`

薄迁移页不复制任何结论、配置或数字，只提供新正本路径。其存在是兼容措施，不计入现行
文档数量。其余旧路径在全仓入链修复后移除。

## 11. AI 检索行为

`docs/README.md` 明示以下规则：

1. 查询当前状态时，忽略 `status: archived` 和 `status: relocated`。
2. `status: proposed` 只能回答“计划或探索是什么”，不能回答“系统已支持什么”。
3. 当前功能查 `product/capabilities.md`。
4. 当前分数查 `evaluation/results.md`。
5. 实验是否通过查 `evaluation/experiment-verdicts.md`。
6. 操作步骤查 `guides/` 或 `operations/`。
7. 需要解释某个 verdict 的证据链时，才沿链接进入 `archive/` 或 feature 的
   `eval-log.md` / `verdict.md`。
8. 若文档与代码、迁移或正式契约冲突，报告文档漂移，不用低优先级文字覆盖实现事实。

典型查询必须形成唯一默认落点：

| 查询 | 默认正本 |
|---|---|
| 如何配置 MCP | `guides/mcp-server.md` |
| CLI 支持哪些命令 | `guides/cli.md` |
| 记忆何时提取和 curation | `architecture/memory-system.md` |
| 当前 LoCoMo / LongMemEval 分数 | `evaluation/results.md` |
| Feature 013 最终是否出货 | `evaluation/experiment-verdicts.md` |
| 新鲜度问题是否已解决 | `product/capabilities.md` + `product/backlog/memory-freshness.md` |
| 为什么 temporal T-4 被拒绝 | verdict 索引后进入 `archive/evaluation/temporal-t4-analysis-2026-07.md` |
| 习惯记忆是不是当前能力 | `product/explorations/habit-memory.md`，答案必须是 proposed |

## 12. 迁移顺序

1. 重新确认 `git status` 和 `HEAD`，以 `617b692` 作为根 `README.md` / `LICENSE`
   并行工作的隔离锚。
2. 创建目标目录、`docs/CONTRIBUTING.md` 和新的 `docs/README.md`。
3. 先生成所有现行正本并核对事实，再处理旧文件，避免迁移中丢失信息。
4. 冻结历史全文，添加 archive 元数据和“不得描述当前状态”提示。
5. 提炼评测 verdict、当前结果、能力和 roadmap，消除跨文件重复。
6. 迁移八份仍被引用的设计稿，删除三份冗余设计稿，定向更新 `specs/` 链接。
7. 为仍被根入口引用的路径写薄迁移页，删除其余旧入口。
8. 全仓修复相对链接和锚点，最后执行验收。

任何阶段若发现目标文件与实施期间出现的并行修改重叠，停止该文件的迁移并报告冲突；
不得覆盖、回滚或吸收来源不明的修改。

## 13. 验收标准

### 13.1 结构与元数据

- `docs/` 中除 `README.md`、`CONTRIBUTING.md` 和薄迁移页外，不再有未分类的顶层正文。
- 所有现行和归档 Markdown 都有且仅有一个 H1。
- 所有现行文档包含完整必填 front matter，字段值符合枚举。
- 所有归档文档标明 `superseded_by` 或 `outcome`。
- 所有迁移页标明 `canonical_path`，且正文不复制原文。
- 每份现行文档都能从 `docs/README.md` 到达；archive 只通过历史入口或证据链接进入。

### 13.2 链接与格式

- 全部 tracked Markdown 的本地文件链接无缺失目标。
- GitHub 风格内部锚点无缺失目标。
- 同一文件中不存在重复标题 slug。
- 标题层级不跳级。
- `git diff --check` 无 whitespace error。

### 13.3 事实一致性

- 现行文档不再出现“LongMemEval-S 全量 500 未跑”。
- 当前能力不再把 CLI 写成未来能力。
- curation 的已交付/opt-in 状态与 spec 018 和当前指南一致。
- 实体图、temporal prompt、T-4、Doc2Query 等 NO-GO 方向不再出现在当前 roadmap。
- 当前结果中的 LongMemEval-S 数字明确为全量 500，100 题只在 archive 出现。
- 当前论文方向与 prior-art 复核后的定位一致。
- `product/backlog/memory-freshness.md` 明确该能力尚未实现。
- `product/explorations/habit-memory.md` 明确未立项、未实现。

### 13.4 变更隔离

- `git diff 617b692..HEAD -- README.md LICENSE` 为空，本任务未改变已合入的入口与许可。
- 除修复八份历史设计稿入链外，不修改 `specs/` 内容。
- Go 源码、测试、迁移和配置零改动；本次为纯文档变更，不运行 LoCoMo 或付费服务。

## 14. 风险与控制

| 风险 | 控制 |
|---|---|
| 批量移动导致深链失效 | 更新全仓入链；高频旧路径保留无内容副本的迁移页；执行文件和锚点扫描 |
| 合并时丢失负结果或口径边界 | 先归档完整原文，再写现行摘要；verdict 回链正式证据 |
| archive 中的旧表述被当成当前事实 | 统一 `archived` 元数据、首屏警告，并从默认目录移除 |
| front matter 再次漂移 | `CONTRIBUTING.md` 固定字段、枚举和人工复核规则；验收扫描覆盖全部现行文档 |
| 当前数字再次被多处复制 | `evaluation/results.md` 成为唯一正本；其他现行文档只链接，不维护矩阵 |
| 并行修改被覆盖 | 每阶段重查 `git status`；碰撞时停止并报告，不做 reset/revert |

## 15. 完成定义

当目录、正本、归档、元数据和链接全部通过第 13 节验收，且用户能从
`docs/README.md` 按任务找到唯一现行答案、AI 能根据状态和正本优先级避免把历史提案当成
当前能力时，本次重组完成。
