# Data Model: 企业级文档信息架构

**Feature**: `019-docs-information-architecture`
**Date**: 2026-07-28

该模型描述 Git 仓库中的文档实体及其可观察契约，不改变 engram 的产品数据模型。

## 1. Document

一份 Markdown 文件是一个 `Document`。每份保留文档只回答一个主要问题。

| 字段 | 类型 | 约束 |
|---|---|---|
| `path` | repository-relative path | 唯一；位于 `docs/` |
| `title` | string | 非空；与唯一 H1 文本一致 |
| `summary` | string | 非空单句；同时说明回答范围和非权威范围 |
| `status` | `DocumentLifecycle` | 必填且只能取一值 |
| `audience` | set of enum | 非空；仅 `users`、`maintainers`、`agents` |
| `owner` | string | 稳定团队标识；本 feature 统一为 `engram-maintainers` |
| `last_reviewed` | date | `YYYY-MM-DD`；不晚于验收日期 |
| `canonical_for` | set of slug | 非空；全体保留文档中每个 slug 唯一归属一个文件 |
| `tags` | set of slug | 非空；只用于发现，不声明权威 |
| `outcome` | string | `archived` 的条件字段 |
| `superseded_by` | path | `archived` 的条件字段；目标存在 |
| `feature` | feature id | 历史设计的条件字段 |
| `canonical_path` | path | `relocated` 必填；直接指向登记的当前或 proposed 主题页 |
| `body` | GFM | 一个 H1；层级连续；文件内 slug 唯一 |

### DocumentLifecycle

```text
stable ───────┐
active ───────┼── current corpus
proposed ─────┼── non-current proposal corpus
archived ─────┼── historical evidence corpus
relocated ────┘── compatibility routing corpus
```

| 值 | 含义 | 当前状态检索 |
|---|---|---|
| `stable` | 已确认且持续维护的现行正本 | 包含 |
| `active` | 持续变化的现行正本 | 包含 |
| `proposed` | 未实现的 backlog、设计或探索 | 排除；只回答未来问题 |
| `archived` | 冻结历史快照或证据 | 默认排除 |
| `relocated` | 旧路径兼容入口 | 默认排除 |

### 状态转换

允许的迁移：

- `proposed → active`：正式进入执行且成为当前计划正本。
- `active → stable`：内容及责任边界已经稳定。
- `stable ↔ active`：依据事实变化频率调整，不改变其中功能状态。
- `stable | active | proposed → archived`：结论被替代或方向收口；先提炼仍有效事实。
- 旧路径被新正本替代时，新建或重写为 `relocated`；`relocated` 不再转为内容正本。

禁止：

- `archived → stable | active` 原地复活；应创建或更新新的现行正本并引用历史证据。
- `relocated → relocated` 链式跳转。
- 从文档生命周期推断能力是否出货。

## 2. CanonicalTopic

`CanonicalTopic` 是用于人和 AI 检索的稳定意图 slug。

| 字段 | 类型 | 约束 |
|---|---|---|
| `slug` | string | `^[a-z0-9]+(?:-[a-z0-9]+)*$` |
| `document` | Document | 全局恰好一个所有者 |
| `query_class` | current / proposed / historical / routing | 与文档生命周期一致 |

关系：

- 一个 `Document` 拥有一个或多个 `CanonicalTopic`。
- 一个 `CanonicalTopic` 只属于一个 `Document`。
- Q1–Q8 的主题必须属于 `stable` 或 `active` 文档。
- `tags` 可以重复，不能替代 `canonical_for` 的唯一映射。

## 3. CapabilityOrExperimentRecord

现行能力表和实验裁决表中的一条记录。

| 字段 | 类型 | 约束 |
|---|---|---|
| `subject` | string | 功能、机制或 feature；一行只描述一个可裁决单元 |
| `state` | `CapabilityState` | 必填，单值 |
| `scope` | string | 默认路径、opt-in、评测、诊断或未实现边界 |
| `outcome` | string | 可选；研究 verdict 可为 `INCONCLUSIVE` 等，不伪装成能力状态 |
| `shipping_impact` | string | 必填；明确是否改变产品默认行为 |
| `evidence` | one or more links | 指向 spec、contract、eval-log、verdict 或归档报告 |

### CapabilityState

| 值 | 产品语义 |
|---|---|
| `shipped-default` | 已交付并位于默认路径 |
| `shipped-opt-in` | 已交付，但必须显式启用 |
| `experimental-default-off` | 仅评测或诊断使用，默认关闭且无产品承诺 |
| `planned` | 已进入计划但尚未交付 |
| `closed-no-go` | 已裁决不出货 |
| `cancelled-before-implementation` | 实现前取消 |

只有前两值可回答“产品已支持”；第三值必须附带实验性限定；后三值回答“未交付”。
一个 feature 若包含多个机制，必须拆成多条记录，不能让一个状态覆盖不同结论。

## 4. EvidenceRecord

支持一个当前结论的可追溯材料。

| 字段 | 类型 | 约束 |
|---|---|---|
| `path` | path | 目标存在且链接可导航 |
| `kind` | spec / contract / eval-log / verdict / report / design | 单值 |
| `historical` | boolean | 为真时不参与当前状态默认回答 |
| `supports` | relation | 指向至少一个当前结论或历史索引条目 |
| `outcome` | string | 历史材料必须可见 |

保留条件满足任一项即可：

1. 含独有实测结果；
2. 含独有方法或可复现步骤；
3. 含独有门控判据；
4. 含独有决策理由；
5. 仍被正式材料引用。

删除必须同时满足：正式规格完整覆盖、没有上述独有证据、仓库入链为零。

## 5. NavigationEntry

把读者任务或查询意图映射到一个正本。

| 字段 | 类型 | 约束 |
|---|---|---|
| `label` | string | 中文任务描述 |
| `destination` | Document | 本地链接有效 |
| `category` | guide / architecture / operations / evaluation / product / research / history | 单值 |
| `distance_from_portal` | integer | 现行文档不超过 2 |

`docs/README.md` 是根导航节点。当前目录不能直接铺开全部 archive；只提供
`docs/archive/README.md` 历史入口。

## 6. RelocationEntry

`status: relocated` 的兼容文档。

| 字段 | 类型 | 约束 |
|---|---|---|
| `legacy_path` | path | 固定旧路径 |
| `canonical_path` | path | 唯一且直接指向 `stable`、`active` 或 `proposed`；不得指向 archive/relocated |
| `message` | paragraph | 一段迁移说明，不含事实结论 |
| `link` | local link | 正文唯一业务链接；与 `canonical_path` 相同 |

迁移页不得包含分数、命令清单、配置样例、能力状态或第二跳迁移。指向 `proposed` 时，
迁移说明必须明确目标是未实现提案；这不使提案成为当前能力正本。

## 7. 关系与全局不变量

```text
docs/README.md
  └─ NavigationEntry ──> current Document ──> EvidenceRecord
                              │
                              ├─ owns CanonicalTopic
                              └─ contains CapabilityOrExperimentRecord

legacy path ── RelocationEntry ──────────────> target Document (current or proposed)
archive index ───────────────────────────────> archived EvidenceRecord
```

全局不变量：

1. 每个保留文档都有至少一个导航或证据入链。
2. 每个 canonical topic 只有一个文档所有者。
3. Q1–Q8 只解析到 `stable` / `active`。
4. 当前分数完整矩阵只存在于 `evaluation/results.md`。
5. 归档和迁移内容不作为当前能力证据。
6. 所有本地文件链接和锚点有效。
7. 范围外产品文件相对 feature 基线不变。

## 8. GovernanceFixture

验证维护者能否一致使用规范的固定分类输入。

| 字段 | 类型 | 约束 |
|---|---|---|
| `id` | `G1` / `G2` / `G3` | 唯一 |
| `scenario` | string | 当前结果、未实现提案或已收口实验 |
| `expected_lifecycle` | DocumentLifecycle | 单值 |
| `expected_path` | path | 指向唯一正本、提案目录或 archive 类别 |
| `expected_actions` | set | 更新、引用、登记 verdict、归档、移出路线等 |
| `review_results` | two records | 两个独立过程都必须与期望完全一致 |

G1 更新 active 结果正本；G2 进入 proposed 并由能力正本给否定结论；G3 在 active verdict
登记 NO-GO 并把完整过程 archived。Q1–Q8 不能替代该分类测试。

## 9. ValidationEvidence

`specs/019-docs-information-architecture/validation-report.md` 中的一条验收证据。

| 字段 | 类型 | 约束 |
|---|---|---|
| `check` | string | 对应 SC 或契约门 |
| `command_or_method` | string | 可重复命令或人工审阅方法 |
| `result` | pass / fail | fail 时不得完成 feature |
| `observed_output` | string/table | 记录计数、路径或分类结论 |
| `reviewer` | stable process label | 不记录个人敏感信息 |

每个删除目标另有五项门、删除前零入链和最终处置的独立记录；全局汇总不能代替逐文件
证明。
