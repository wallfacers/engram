# Contract: 导航与 AI 检索

**Portal**: `docs/README.md`
**Version**: 1.0
**Date**: 2026-07-28

## 1. 门户职责

门户只承担导航和检索协议，不复制会变化的分数、命令全集或实验台账。它必须提供：

1. 按任务查找；
2. AI 检索与事实优先级；
3. 完整现行目录；
4. 单一历史入口；
5. 文档维护规范入口。

任务分类固定为：

| 分类 | 读者任务 | 现行目的地 |
|---|---|---|
| 使用指南 | 配置 CLI 或 MCP | `docs/guides/` |
| 架构 | 理解记忆系统与 provenance | `docs/architecture/` |
| 评测运维 | 复现 recipe 或管理远端机器 | `docs/operations/evaluation/` |
| 评测结论 | 查看当前结果、裁决、路线和竞品边界 | `docs/evaluation/` |
| 产品 | 查看当前能力、路线、backlog 和探索 | `docs/product/` |
| 研究 | 查看当前论文方向 | `docs/research/` |
| 历史 | 审计旧决策和证据 | `docs/archive/README.md` |

## 2. 导航图约束

- `docs/README.md` 是图的唯一起点。
- 每份 `stable`、`active` 和 `proposed` 文档距离门户不超过两次链接跳转。
- 一次动作是跟随一个本地 Markdown 链接；站内搜索不计。
- archive 不进入完整现行目录；门户只直接链接 `docs/archive/README.md`。
- 每份 archive 文档必须从 archive 索引、现行 verdict 或正式 feature 证据至少一处可达。
- `relocated` 不进入门户导航，也不作为到达现行文档所需的一跳。

## 3. 当前状态过滤

检索当前能力、当前结果、当前操作或当前路线时：

1. 先按 `canonical_for` 定位主题；
2. 只接受 `status: stable` 或 `status: active`；
3. 排除 `proposed`、`archived` 和 `relocated`；
4. 如需解释结论，再沿正本中的 evidence 链进入 feature 或 archive；
5. 若文档与宪法、代码、测试、迁移或正式 feature 裁决冲突，报告文档漂移，不用低
   权威文字覆盖实现事实。

计划或探索查询可以读取 `proposed`，但答案必须明确“未实现”；历史原因查询可以读取
`archived`，但答案必须明确其适用时间和 outcome。

## 4. 权威主题唯一性

- 每个 `canonical_for` slug 在全部 `docs/` 中只属于一个文件。
- 高频当前主题的所有者必须是 `stable` 或 `active`。
- `tags`、标题关键词和 archive 内容不能创造第二个默认正本。
- 当前完整分数矩阵只由 `evaluation-results` 主题所有者维护。
- Q6/Q7 的 backlog 和 exploration 是次级链接；二者的当前答案仍由
  `current-capabilities` 唯一提供。

### 分数消费者

下列现行文档必须链接 `docs/evaluation/results.md`，不得维护第二份 engram 当前分数
矩阵：

- `docs/product/capabilities.md`
- `docs/product/roadmap.md`
- `docs/evaluation/competitors.md`
- `docs/research/paper-direction.md`

validator 必须检查四份回链存在，并禁止它们复制当前结果正本中的完整 score tuple 或
多行 engram 结果矩阵。单句定性结论或竞品厂商自报数字可以保留，但必须标明口径并链接
结果正本；人工语义复核在 validation report 确认没有以改写格式规避门禁。

### 当前存储能力

`current-capabilities` 正本必须说明并回链架构：

- 每个 namespace 使用独立本地 SQLite 文件，schema v6；
- `memory_entries`、FTS5、provenance/event/supersession/revision 是当前存储原语；
- 默认检索为 keyword、可选 semantic、entity 三信号 RRF，并能在 embedding 缺席时
  离线降级；
- side table 或实验代码存在不代表 associative、temporal、multi-query、Doc2Query 等
  机制已经出货；
- curation 是 `shipped-opt-in`，完整 freshness/state consistency 仍未实现。

validator 必须检查这些边界存在，并检查能力页链接
`docs/architecture/memory-system.md`；不得只列 CLI 和未来方向。

## 5. Fixed Retrieval Verification Set

以下 query、主题、路径和结论都是验收 fixture，不得在实施时改成更容易通过的问法。
`docs/validation/retrieval-fixtures.json` 必须逐项镜像本表，作为检查器的机器可读输入；
本表和 feature spec 是语义正本，validator 必须在 fixture 缺项、重复 ID 或路径漂移时
失败。

| ID | 固定查询文本 | 唯一主题 | 唯一默认正本 | 必须得到的状态或结论 |
|---|---|---|---|---|
| Q1 | 如何配置 engram MCP server？ | `mcp-integration` | `docs/guides/mcp-server.md` | `stable` 使用指南 |
| Q2 | engram CLI 支持哪些命令？ | `cli-usage` | `docs/guides/cli.md` | CLI 已交付，返回现行命令参考 |
| Q3 | engram 的记忆什么时候抽取，curation 怎么运行？ | `memory-architecture` | `docs/architecture/memory-system.md` | 只有显式 MCP/CLI ingest 才抽取，write/add 直接写入；curation=`shipped-opt-in` |
| Q4 | engram 当前 LoCoMo 和 LongMemEval-S 分数是多少？ | `evaluation-results` | `docs/evaluation/results.md` | LongMemEval-S 为 full 500；每条结果含 dataset/answerer/judge/recipe |
| Q5 | Feature 013 最终是否出货？ | `experiment-verdicts` | `docs/evaluation/experiment-verdicts.md` | `closed-no-go`，不在当前路线 |
| Q6 | 记忆新鲜度和状态一致性是否已经实现？ | `current-capabilities` | `docs/product/capabilities.md` | 尚未实现；链接 proposed backlog |
| Q7 | SaaS 习惯记忆是不是当前能力？ | `current-capabilities` | `docs/product/capabilities.md` | 未立项、未实现；链接 proposed exploration |
| Q8 | engram 当前论文方向是什么？ | `research-direction` | `docs/research/paper-direction.md` | 完整负结果史、共同失败机理、低成本止损 |

## 6. 结构验收

运行 `node docs/validation/check-docs.mjs`，确定性检查必须证明：

- 上表每个主题恰好有一个文档所有者；
- 所有者路径与表一致；
- 所有者状态只为 `stable` / `active`；
- `proposed`、`archived`、`relocated` 的 fixture 命中数为 0；
- 所有现行文档距离门户不超过 2；
- Q1–Q8 的要求关键词或等价结构化结论存在，且禁止状态不存在。
- 四个分数消费者都回链唯一结果正本，且没有复制完整当前矩阵。

fixture 的每一项至少包含 `id`、`query`、`topic`、`canonical_path`、
`required_assertions` 和 `forbidden_lifecycles`。`required_assertions` 是可审阅的中文
语义标签，不允许以宽泛的单字符或只匹配标题的模式制造假阳性。

### 链接语义

文件和锚点存在只是基础门。validator 还必须确定性检查：

- 12 个旧路径与归档契约中的目标逐项一致，正文唯一链接等于 `canonical_path`；
- 八份设计的正式入链都指向对应 archive 文件，不存在旧设计路径；
- archive 的 `superseded_by` 指向语义相符的现行索引或正本；
- 门户任务标签指向该分类登记的 canonical topic；
- Q1–Q8 的 topic、路径和正文断言一致。

对无法从元数据确定的普通叙述链接，实施者必须审阅本 feature 的链接变更 diff，逐项确认
链接文字和目标职责相符，并在 `validation-report.md` 记录。未修改的既有链接只需通过
文件/锚点存在性门。

## 7. 独立语义复核

结构检查通过后，由两个独立审阅过程分别：

1. 使用新的阅读上下文；
2. 只从 `docs/README.md` 开始；
3. 对 Q1–Q8 逐题记录首个正本路径、文档状态、回答结论和证据链接；
4. 不使用仓库搜索直接跳过门户；
5. 比较两份记录。

通过条件是 8/8 的首个正本与登记路径一致，状态结论一致，且没有把提案、归档或迁移页
报告为当前已交付事实。Q6/Q7 不能把次级提案页报告为第二当前正本。
