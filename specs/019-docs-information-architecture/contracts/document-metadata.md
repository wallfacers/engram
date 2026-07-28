# Contract: 文档元数据与正文结构

**Applies to**: `docs/**/*.md`
**Version**: 1.0
**Date**: 2026-07-28

## 1. Front matter

每个文件必须从第 1 行开始且只包含一块 YAML front matter。基准形态：

```yaml
---
title: 记忆系统架构
summary: 本文说明当前记忆写入、检索与 curation 边界；不提供部署命令或评测分数。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memory-architecture]
tags: [memory, architecture]
---
```

本契约采用 YAML 的受限子集：标量字符串、`YYYY-MM-DD` 日期和单行字符串数组；不使用
锚点、别名、对象嵌套、多行 block scalar 或自定义 tag。

## 2. 共同必填字段

| 字段 | 验证规则 |
|---|---|
| `title` | 非空字符串；去除 Markdown 标记后与唯一 H1 完全一致 |
| `summary` | 非空单句；说明“回答什么”和“不回答什么”或等价权威边界 |
| `status` | 只能是 `stable`、`active`、`proposed`、`archived`、`relocated` |
| `audience` | 非空、无重复；元素只能是 `users`、`maintainers`、`agents` |
| `owner` | 必须是 `engram-maintainers`，不得使用个人姓名 |
| `last_reviewed` | 有效 `YYYY-MM-DD`；不得晚于验收日期 |
| `canonical_for` | 非空、无重复 slug 数组；每个 slug 全局只属于一个文件 |
| `tags` | 非空、无重复 slug 数组；可跨文件重复，只用于发现 |

slug 必须匹配：

```regex
^[a-z0-9]+(?:-[a-z0-9]+)*$
```

## 3. 生命周期条件字段

### `stable` / `active`

- 不得出现 `canonical_path`。
- H1 后、第一个 H2 前必须有可见摘要和权威边界。
- 可以包含能力/实验状态，但必须使用六值枚举且逐条明确。

### `proposed`

- H1 后、第一个 H2 前必须明确“未实现”；若尚未立项，还必须明确“未立项”。
- 不得作为已交付能力或当前结果的证据。
- 不得出现 `canonical_path`。

### `archived`

- `outcome` 或 `superseded_by` 至少一个非空。
- `superseded_by` 使用仓库根相对路径，目标必须存在且不能是 `relocated`。
- 历史设计必须另有 `feature`；取值为 `003`–`019` 的三位字符串。
- H1 后、第一个 H2 前必须有可见历史警告，说明其不描述当前状态，并给出 outcome 或
  现行替代入口。

示例：

```yaml
status: archived
outcome: closed-no-go
superseded_by: docs/evaluation/experiment-verdicts.md
feature: "013"
```

### `relocated`

- `canonical_path` 必填且只能有一个路径。
- 目标必须存在，状态只能是 `stable`、`active` 或 `proposed`。指向 `proposed` 时，
  迁移说明必须明确目标未实现；当前能力查询仍排除该目标。
- 不能指向另一个 `relocated` 文件，不能形成链或循环。
- 不得出现 `outcome`、`superseded_by` 或功能状态。

## 4. 正文结构

每份文档必须满足：

1. front matter 后只有一个 H1；
2. H1 与 `title` 一致；
3. 标题层级每次最多增加一级，例如 H2 后可进入 H3，不能直接进入 H4；
4. 按 GitHub 风格规范化后的标题 slug 在文件内唯一；
5. 解释性标题和正文以中文为主；命令、字段、API、协议、模型和专有名词保留英文；
6. 本地 Markdown 链接和章节锚点有效；
7. 至少有一个来自门户、历史索引、现行裁决或正式 evidence 的入链。

当前文档不得包含逐字聊天、可识别个人姓名、脏话或与正式需求无关的寒暄。只有无法改写
而不损失判据的一手证据可以保留短引文，并必须紧邻说明保留理由。

## 5. 迁移页正文上限

`relocated` 文件在 front matter 后只允许：

1. 一个 H1；
2. 一段迁移说明；
3. 一个指向 `canonical_path` 的 Markdown 链接。

不得复制原文、命令、配置、数字、功能状态结论、目录、代码块或其他业务链接；若目标为
`proposed`，允许且必须说明“目标为未实现提案”这一文档生命周期事实。

## 6. 状态与权威性

- `status` 描述文档生命周期，不描述功能是否出货。
- `canonical_for` 声明权威查询意图，`tags` 不声明权威性。
- 当前状态索引只收录 `stable` 和 `active`。
- `proposed` 只回答计划、问题定义或探索。
- `archived` 只用于证据追溯。
- `relocated` 只用于路径兼容。

违反任一 MUST 规则即阻塞验收。

## 7. `docs/CONTRIBUTING.md` 治理契约

维护规范必须显式覆盖六类动作：

1. **新增**：先选唯一主问题、目录、生命周期、`canonical_for` 和 owner；主题已存在时
   不得另建第二正本。
2. **更新**：只在主题所有者中更新事实，同时更新 `last_reviewed` 和 evidence；不得用
   新文档旁路正本。
3. **引用**：快速变化的分数、能力和 verdict 只链接正本，不复制矩阵或状态台账。
4. **归档**：先把仍有效结论提炼到现行正本，再冻结全文，补 outcome/替代入口并加入
   archive 索引。
5. **删除**：逐项通过归档契约的五项删除门并在 validation report 留证。
6. **人工复核**：每次语义修改、证据或功能状态变化时，owner 核验元数据、当前事实、
   evidence 和本地链接后更新 `last_reviewed`；只改排版不得伪造复核日期。

维护规范必须包含以下三个固定分类样例：

| ID | 情境 | 预期分类 |
|---|---|---|
| G1 | 新的当前 benchmark 结果 | 更新 `evaluation/results.md`（`active`），其他文档只引用 |
| G2 | 尚未实现的记忆能力 | `proposed` backlog/exploration；当前能力页只写“未实现”并链接 |
| G3 | 已收口 NO-GO 实验 | verdict 索引登记 `closed-no-go`，完整过程 `archived`，从路线移除 |

两个独立审阅过程只依据维护规范分类 G1–G3，必须在生命周期、目标路径和后续动作上 3/3
一致；记录写入 `validation-report.md`。
