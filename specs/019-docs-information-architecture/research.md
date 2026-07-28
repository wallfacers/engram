# Phase 0 Research: 企业级文档信息架构

**Feature**: `019-docs-information-architecture`
**Date**: 2026-07-28
**Scope**: `docs/` 内容重组，以及既有 `specs/` 中历史设计链接的最小修正

本文件记录实施前已经收口的研究决策。所有决策均来自仓库内现有代码、正式 feature
材料、评测日志、verdict 和已批准的信息架构设计；不存在未决澄清项。

## R-001：现行正本与历史证据分层

**Decision**

采用“领域化现行正本 + 独立历史证据 + 旧路径薄迁移页”：

- 现行层按指南、架构、评测运维、评测结论、产品和研究组织。
- `docs/archive/` 保存具有审计价值但不再描述当前状态的完整材料。
- 仍被仓库入口引用的旧路径只保留 `status: relocated` 的单跳入口。
- 每个 `canonical_for` 主题只映射到一个文档；当前查询只接受 `stable` 或 `active`
  正本。

**Rationale**

当前问题来自内容重复和状态冲突，而不是文件数量本身。把历史证据和当前答案分开，可以
同时保证 AI 查询唯一性、用户任务导航和负结果追溯；薄迁移页又能在不修改范围外入口的
前提下维持链接兼容。

**Alternatives considered**

- 只补目录和 front matter：不能消除正文冲突和多份分数矩阵。
- 合并成少数大型手册：检索粒度过粗，多个维护主题会重新耦合。
- 删除全部历史材料：会丢失测量、门控判据和决策理由。
- 引入文档站或向量检索：超出当前规模需要，并增加运行依赖。

## R-002：事实权威优先级

**Decision**

发现冲突时按以下顺序裁决，并在现行文档中链接较高层证据：

1. 项目宪法及已经发布的公共契约；
2. 当前代码、测试和数据迁移；
3. 正式 feature spec、contract、eval-log 和 verdict；
4. `stable` / `active` 现行文档；
5. 当前评测结果正本；
6. 产品路线和 `proposed` 材料；
7. `archived` 历史材料与 `relocated` 入口。

任务复选框、文件名日期和设计稿中的未来时态不能覆盖后续 verdict 或已交付实现。若
现行文档与 1–3 层冲突，必须报告并修正文档漂移。

**Rationale**

该顺序把可执行事实和正式裁决放在叙述性材料之前，避免从过期计划反推当前能力。

**Alternatives considered**

- 以最新修改时间为准：机械时间无法判断内容是否为计划、证据或实现。
- 以 `docs/` 为唯一事实源：整理前的冲突正是由此产生，且不能验证产品行为。

## R-003：文档生命周期与功能状态正交

**Decision**

文档生命周期严格使用五值：

- `stable`
- `active`
- `proposed`
- `archived`
- `relocated`

能力或实验条目严格使用六值：

- `shipped-default`
- `shipped-opt-in`
- `experimental-default-off`
- `planned`
- `closed-no-go`
- `cancelled-before-implementation`

LongMemEval Feature 016 的评测适配与 full-500 基线已经完成，但预注册研究结论是
`INCONCLUSIVE`。因此能力状态和研究 outcome 必须分列，不能把 `INCONCLUSIVE` 强行
改写为 `closed-no-go`。习惯记忆尚未立项，不应虚构为 `planned`；它只是一份
`proposed` 探索，当前能力正本给出否定结论。

**Rationale**

“一份文档是否当前”与“其中某项功能是否出货”是两个维度。分离后才能准确表达
“当前文档记录一个默认关闭的已交付能力”或“归档文档记录当时的 NO-GO”。

**Alternatives considered**

- 用 `active` 同时表示文档和功能：无法区分当前、试验、提案和出货。
- 使用 GO/NO-GO 二值：不能表达 opt-in、仅诊断、计划中和实现前取消。

## R-004：统一元数据

**Decision**

所有保留在 `docs/` 的 Markdown 文件均在文件首部提供受限 YAML front matter：
`title`、`summary`、`status`、`audience`、`owner`、`last_reviewed`、
`canonical_for` 和 `tags`。`owner` 统一使用稳定团队标识
`engram-maintainers`，日期使用 `YYYY-MM-DD`，主题和标签使用短横线 slug。

条件字段：

- `archived` 至少提供 `outcome` 或 `superseded_by`；历史设计另加 `feature`。
- `relocated` 必须提供唯一 `canonical_path`，并直接指向 `stable`、`active` 或
  `proposed` 的目标主题页；不得指向 archive 或另一个迁移页。旧新鲜度路径是唯一允许
  指向 `proposed` 的兼容入口，当前能力查询仍先到 `product/capabilities.md`。
- `stable` / `active` 在唯一 H1 后、第一个 H2 前给出可见摘要和权威边界。

**Rationale**

结构化元数据使 AI 和确定性检查都能先按状态、主题和受众过滤，再读取正文。稳定团队标识
和复核日期也避免维护责任绑定个人。

**Alternatives considered**

- 只在正文写“状态”：不可稳定解析，也容易在长文中被忽略。
- 允许自由状态词：会重现“完成、活跃、冻结、待做”语义重叠。

详细字段规则见
[document-metadata.md](./contracts/document-metadata.md)。

## R-005：逐文件迁移

**Decision**

| 原文件 | 处置 | 唯一目标 |
|---|---|---|
| `docs/README.md` | 原位重写 | 文档门户与 AI 检索协议 |
| `background-extraction-from-workhorse-agent.md` | 提炼并保留薄入口 | `architecture/provenance.md` |
| `benchmark-expansion-plan.md` | 拆分后删除旧路径 | `evaluation/benchmark-roadmap.md`、`archive/plans/benchmark-expansion-2026-07.md` |
| `capability-and-product-north-star.md` | 合并后删除 | `product/capabilities.md`、`product/roadmap.md` |
| `cli.md` | 迁移并保留薄入口 | `guides/cli.md` |
| `competitive-benchmarks.md` | 去除分数副本并保留薄入口 | `evaluation/competitors.md` |
| `local-model-eval-setup.md` | 冻结归档 | `archive/plans/local-model-eval-stack-2026-07.md` |
| `locomo-e2e-eval-reproduction.md` | 迁移并保留薄入口 | `operations/evaluation/locomo-runbook.md` |
| `locomo-score-levers.md` | 提炼裁决、归档全文、保留薄入口 | `evaluation/experiment-verdicts.md`、`archive/evaluation/locomo-experiment-ledger-2026-07.md` |
| `locomo-single-multihop-failure-diagnosis.md` | 冻结归档 | `archive/evaluation/locomo-single-multihop-diagnosis-2026-07.md` |
| `mcp-server.md` | 迁移并保留薄入口 | `guides/mcp-server.md` |
| `memory-architecture.md` | 迁移并保留薄入口 | `architecture/memory-system.md` |
| `memory-freshness-and-retrieval-policy.md` | 正式化并保留薄入口 | `product/backlog/memory-freshness.md` |
| `memory-strategy.md` | 提炼路线、归档全文、保留薄入口 | `product/roadmap.md`、`archive/decisions/memory-strategy-2026-07.md` |
| `memos-inhouse-locomo-repro.md` | 迁移并保留薄入口 | `evaluation/reports/memos-locomo-reproduction.md` |
| `paper-outline-eval-reliability.md` | 重写当前方向并归档旧提纲 | `research/paper-direction.md`、`archive/research/eval-reliability-outline-2026-07.md` |
| `remote-eval-box.md` | 迁移并保留薄入口 | `operations/evaluation/remote-gpu-runbook.md` |
| `results-matrix-2026-07-26.md` | 重写正本、拆出先导、保留薄入口 | `evaluation/results.md`、`archive/evaluation/longmemeval-100-pilot-2026-07.md` |
| `saas-habit-memory-design.md` | 迁移并固定 proposed | `product/explorations/habit-memory.md` |
| `synthius-mem-analysis.md` | 冻结归档 | `archive/research/synthius-mem-analysis-2026-07.md` |
| `temporal-t4-design.md` | 冻结归档 | `archive/evaluation/temporal-t4-analysis-2026-07.md` |

**Rationale**

每个目标只承担一个主要问题；当前分数、能力、路线和实验裁决分别只有一个维护位置，完整
历史仍可追溯。

**Alternatives considered**

- 原文件原位改名式重写：无法同时拆出当前结论和历史证据。
- 所有旧路径都留迁移页：会保留无实际入链的噪声；只保留已确认的高频兼容路径。

## R-006：当前事实快照

**Decision**

实施时以以下已核验事实纠正状态漂移：

- LongMemEval-S 当前结果为 cleaned full 500：local-first 栈 `404/500 = 80.80%`；
  仅替换 answerer 为 `deepseek-v4-pro` 的公平探针为 `430/500 = 86.00%`，
  `+5.20pp`，McNemar `p=0.0049`。86% 是付费 answerer 天花板探针，不是默认能力。
- LoCoMo 的当前可比结果必须同时写明数据集、answerer、judge 和 recipe；不得从旧文档
  单独复制一个百分比。
- CLI 已交付 `add`、`search`、`get`、`list`、`delete`、`ingest`、`curate`、
  `stats`、`export`、`namespaces`、`version`。其中 `ingest` 和 `curate` 需要 LLM。
- Curation 是 `shipped-opt-in`：MCP 后台模式默认关闭；CLI `curate` 是显式、同步、
  单次操作。普通 `add` / `ingest` 不触发 curation。
- 当前存储是 local-first：每个 namespace 使用独立 SQLite 文件；schema v6 以
  `memory_entries` 为事务真相，提供 FTS5 mirror，并保存 provenance、event time、
  `superseded_by` 和单调 `revision`。side tables 保存 embedding、entity、alias、
  fact-query 和 entity-edge。默认检索是 keyword、可选 semantic、entity 三信号 RRF；
  embedding 未配置时降级为 keyword + entity，再缺 entity 时退化为 keyword，离线 CRUD
  和关键词检索仍可用。
- side table 或实验代码存在不等于产品默认出货。Associative、cluster sweep、temporal
  score、multi-query、Doc2Query 等机制的出货状态必须以实验裁决表为准；不能从 schema
  推断它们已启用。
- 完整记忆新鲜度、状态一致性、read-your-writes、精确 source-span 闭包和按需动态召回
  尚未实现；已有时间与 supersession 原语不能被描述成完整能力。
- 习惯记忆未立项、未实现，设计中的目标 API 不是当前公共 API。
- 当前论文方向是“完整负结果史、共同失败机理与低成本止损方法”；旧“评测可靠性审计”
  headline 只作为历史提纲。

**Rationale**

这些事实是本次整理中已发现的明确冲突点，也是固定检索验收的状态断言。存储事实以
`store/migrations.go`、`memory/entrystore.go` 和 `memory/retriever.go` 为实现证据。

**Alternatives considered**

- 保留旧文档顶部的叠加更正：AI 仍可能抽取正文中的相反结论。
- 重新运行付费评测：不在本 feature 范围内，且现有 verdict 已足以裁决文档状态。

## R-007：003–018 裁决归一化

**Decision**

`evaluation/experiment-verdicts.md` 对每个 feature 记录“能力状态”和“研究 outcome”两列；
若一个 feature 含多个机制，按机制拆行，不能制造不存在的单一结论。

| Feature | 能力/机制状态摘要 | 研究 outcome 或出货影响 |
|---|---|---|
| 003 | 仅保留实验诊断；主要杠杆不进入默认路径 | assoc 负向、temporal 噪声内、cluster sweep NO-GO |
| 004 | `shipped-default` | CLI 已交付；LLM 命令明确依赖边界 |
| 005 | `closed-no-go` | PCIC-lite 不出货 |
| 006 | `closed-no-go` | abstention 信号不足以通过严格操作门 |
| 007 | `experimental-default-off` | 对齐 judge 用于可比评测，不改变产品行为 |
| 008 | 按 prompt、reranker、eval-stack 分行 | prompt 与 reranker NO-GO；eval stack 只作为评测候选 |
| 009 | trace 为 `experimental-default-off`；排序机制 `closed-no-go` | 诊断表明问题不是 top-K 排序 |
| 010 | `closed-no-go` | multi-query 不进入默认检索 |
| 011 | `closed-no-go` | alias shadow 不进入默认栈 |
| 012 | `closed-no-go` / 写入路径 `cancelled-before-implementation` | Doc2Query 不出货 |
| 013 | `closed-no-go` / 召回机制 `cancelled-before-implementation` | temporal recall 不出货 |
| 014 | `closed-no-go` | 强化契约已回滚；flag 继续默认关闭 |
| 015 | `cancelled-before-implementation` | 门 0 失败，consolidation 未实现 |
| 016 | 评测能力 `shipped-default` | full-500 基线完成；预注册研究 verdict 为 `INCONCLUSIVE` |
| 017 | `closed-no-go` | date scaffold 增益小于噪声标尺 |
| 018 | `shipped-opt-in` | curation 已交付且默认关闭 |

**Rationale**

该表示忠实保留混合 feature 的粒度，并防止把“代码存在”“评测完成”或
`INCONCLUSIVE` 错写成产品出货。

**Alternatives considered**

- 每个 feature 强制一个状态：会丢失 008、009、012、013、016 的关键边界。
- 继续使用散落在 ledger 的自然语言：不能支持 Q5 和当前路线的确定性检索。

## R-008：历史设计保留与删除

**Decision**

下列八份仍有正式入链或独有依据，迁移到 `docs/archive/designs/`，补充 `feature` 和
`outcome`，并把既有引用改成可点击的新路径：

- `2026-07-21-judge-口径-alignment-design.md`
- `2026-07-22-retrieval-ranking-attribution-gate-design.md`
- `2026-07-23-multi-query-retrieval-design.md`
- `2026-07-24-answer-side-temporal-reasoning-contract-design.md`
- `2026-07-24-doc2query-pseudo-query-shadow-design.md`
- `2026-07-24-write-side-alias-embedding-design.md`
- `2026-07-25-offline-consolidation-bridging-design.md`
- `2026-07-26-longmemeval-subset-design.md`

本 feature 的已批准设计在实施完成后同样归档。下列三份只有在实施前再次确认“正式规格
完整覆盖、无独有证据、仓库入链为零”后删除：

- `2026-07-19-bio-retrieval-locomo-design.md`
- `2026-07-20-cli-ai-first-design.md`
- `2026-07-28-curation-lifecycle-side-table-cleanup-design.md`

**Rationale**

入链和独有证据是可审计的保留标准；历史价值不能由文件年龄或主观“看起来重复”决定。

**Alternatives considered**

- 所有设计都归档：保留三份完全被正式规格覆盖的噪声。
- 所有设计都删除：破坏 007、009–012、014–016 的正式证据链。

## R-009：兼容入口与范围外链接

**Decision**

保留以下 12 个 `relocated` 旧路径：

- `docs/background-extraction-from-workhorse-agent.md`
- `docs/cli.md`
- `docs/competitive-benchmarks.md`
- `docs/locomo-e2e-eval-reproduction.md`
- `docs/locomo-score-levers.md`
- `docs/mcp-server.md`
- `docs/memory-architecture.md`
- `docs/memory-freshness-and-retrieval-policy.md`
- `docs/memory-strategy.md`
- `docs/memos-inhouse-locomo-repro.md`
- `docs/remote-eval-box.md`
- `docs/results-matrix-2026-07-26.md`

每个入口直接指向登记的 `stable`、`active` 或 `proposed` 主题页，不得串联到另一个
迁移页，也不得复制配置、数字或结论。除新鲜度旧路径按已批准映射指向 proposed backlog
外，其余 11 个入口都指向现行正本。既有 `specs/` 只修改八份归档设计的链接目标，不
改变其正文语义。

**Rationale**

这能保持根 `README.md`、`README.zh-CN.md`、维护者入口和既有深链有效，同时使旧路径
不参与当前状态回答。

**Alternatives considered**

- 修改根 `README.md`、`README.zh-CN.md` 和 `CLAUDE.md`：违反已批准的范围边界，并
  可能覆盖并行工作。
- 在旧路径保留摘要副本：摘要仍会随着正本变化而漂移。

## R-010：确定性验证与固定检索集

**Decision**

不新增依赖，在 `docs/validation/` 保留可重复使用的只读检查器和固定 fixture：

- `check-docs.mjs` 使用 Node.js 标准库，扫描 Git 跟踪文件且不写入仓库；
- `retrieval-fixtures.json` 机器可读地固化 Q1–Q8 的原文、主题、路径和必须结论。

检查器配合 Git、Bash 和 ripgrep 验证：

- front matter、枚举、条件字段和主题唯一性；
- 单一 H1、连续标题层级和唯一 GitHub slug；
- 全部 tracked Markdown 的本地文件链接和章节锚点；
- 所有保留文档的入链、所有现行文档从门户两跳可达；
- 迁移页内容上限和 archive 状态；
- Q1–Q8 的固定主题、路径和状态断言；
- 范围外文件不变、无空白错误、Go 基线仍通过。

Q1–Q8 的默认正本固定为：

| ID | 主题 | 唯一路径 |
|---|---|---|
| Q1 | `mcp-integration` | `docs/guides/mcp-server.md` |
| Q2 | `cli-usage` | `docs/guides/cli.md` |
| Q3 | `memory-architecture` | `docs/architecture/memory-system.md` |
| Q4 | `evaluation-results` | `docs/evaluation/results.md` |
| Q5 | `experiment-verdicts` | `docs/evaluation/experiment-verdicts.md` |
| Q6 | `current-capabilities` | `docs/product/capabilities.md` |
| Q7 | `current-capabilities` | `docs/product/capabilities.md` |
| Q8 | `research-direction` | `docs/research/paper-direction.md` |

Q6 和 Q7 的 backlog / exploration 是次级证据，不是第二个当前正本。结构门禁之后，再由
两个独立审阅过程从 `docs/README.md` 开始执行相同八问；8/8 路径与结论一致才通过。
实施结果写入 `specs/019-docs-information-architecture/validation-report.md`；三份删除
候选逐份记录正式覆盖、独有证据复核和删除前入链为零的证明。

**Rationale**

确定性检查负责可复现的结构正确性，独立语义复核负责自然语言结论；两者互补且无需网络或
付费模型。

**Alternatives considered**

- 只依赖人工点击：不稳定且容易漏掉深链。
- 只让 LLM 判断：不能证明主题唯一性、链接图和状态过滤。
- 只使用一次性临时脚本：无法被后续维护者重复执行，规范会再次漂移。
- 新增第三方 lint 工具：当前规模无需增加依赖维护面。

## R-011：正文语言与敏感上下文

**Decision**

现行文档的标题、章节和解释正文以中文为主；命令、字段、API、协议、模型、配置键和标准
错误文本保留英文。新鲜度问题文档删除逐字聊天、个人姓名、脏话和无关寒暄，只保留正式
问题、失败模式和验收方向。历史材料可保留理解裁决所必需的一手语境，但首屏必须明确
历史状态。

**Rationale**

统一叙述语言提高浏览一致性，去除不必要的个人和口语上下文能让问题定义更专业、更适合
长期检索，同时不损失验收语义。

**Alternatives considered**

- 原样搬运聊天：保留了不必要的身份和口语噪声。
- 全部翻译命令和专有名词：会损坏可复制性和技术准确性。

## R-012：维护治理分类 fixture

**Decision**

`docs/CONTRIBUTING.md` 必须分别定义新增、更新、引用、归档、删除和人工复核规则，并给出
一个可执行决策树。SC-011 固定使用以下三个输入，不与 Q1–Q8 混用：

| ID | 输入 | 唯一正确分类与动作 |
|---|---|---|
| G1 | 产生一组新的当前 benchmark 结果 | 更新 `active` 的 `docs/evaluation/results.md`；其他文档只链接，不新建分数矩阵 |
| G2 | 提出尚未实现的记忆能力 | 创建或更新 `proposed` backlog/exploration；`product/capabilities.md` 只给未实现结论和链接 |
| G3 | 一项实验已经收口为 NO-GO | 在 `active` verdict 索引登记 `closed-no-go`；完整过程以 `archived` 证据冻结并退出当前路线 |

两个独立审阅过程只阅读 `docs/CONTRIBUTING.md` 后分别分类 G1–G3；3/3 的生命周期、目标
路径和引用/归档动作完全一致才通过，结果写入 `validation-report.md`。

**Rationale**

固定治理样例验证维护规则能阻止未来漂移；它与 Q1–Q8 的“如何检索现有事实”是不同的
验收目标。

**Alternatives considered**

- 用 Q1–Q8 代替治理复核：只能证明当前导航，不能证明新增内容如何落位。
- 只写原则不写样例：不同维护者仍可能把提案或 NO-GO 放进当前能力/路线。
