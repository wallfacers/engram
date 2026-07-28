---
title: 文档维护规范
summary: 本文规定 engram 文档的分类、更新、归档和复核流程；不替代任一主题正本的技术事实。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [documentation-governance]
tags: [documentation, governance, contributing]
---

# 文档维护规范

文档是面向用户与 AI 的可检索接口。每份文档只回答一个主问题，并且必须有唯一的 `canonical_for`；主题已存在时更新正本，而不是新建并列说明。

## 元数据与生命周期

每份 `docs/**/*.md` 使用 title、summary、status、audience、owner、last_reviewed、canonical_for、tags 八个字段。owner 固定为 `engram-maintainers`，日期只在事实或证据复核后更新。

- `stable`：已确认且持续维护的当前正本。
- `active`：持续变化的当前正本。
- `proposed`：未实现的 backlog、设计或探索，正文必须明确“未实现”。
- `archived`：冻结历史证据，必须有 outcome 或替代入口与显著历史警告。
- `relocated`：仅兼容旧路径，只有一个直接指向当前或 proposed 页的链接。

能力状态另行明确使用 `shipped-default`、`shipped-opt-in`、`experimental`、`planned`、`uninitiated` 或 `closed-no-go`；不要从文档生命周期推断功能是否出货。

## 操作决策树

### 新增

先选择主问题、目录、生命周期和唯一 canonical topic。若主题已有正本，新增内容应成为该正本的更新或证据，而不是第二个当前答案。

### 更新

只在主题所有者中更新事实，并同时更新 `last_reviewed` 与 evidence。命令、能力、verdict 和完整分数必须回到其唯一正本更新。

### 引用

快速变化的分数、能力与 verdict 只链接正本，不复制矩阵或状态台账。外部数字必须标明其来源和是否同栈可比。

### 归档

先把仍有效结论提炼到现行正本，再冻结全文或必要证据。归档页加入 outcome、替代入口、历史警告和 archive 索引；它不进入门户的当前目录。

### 删除

历史设计只有在正式 feature 覆盖目标、验收与契约、没有独有证据、且删除前 Markdown 入链为零时才能删除。逐项证据必须写入 feature 的 validation report；任一条件不满足即归档。

### 人工复核

语义、证据或功能状态变化后，owner 必须核对元数据、当前事实、evidence 和本地链接，再更新 `last_reviewed`。纯排版修改不能伪造复核日期。

## 固定分类样例

| ID | 情境 | 生命周期 | 目标路径 | 后续动作 |
|---|---|---|---|---|
| G1 | 新的当前 benchmark 结果 | active | `docs/evaluation/results.md` | 更新唯一结果正本；其他页面只链接 |
| G2 | 尚未实现的记忆能力 | proposed | `docs/product/backlog/` 或 `docs/product/explorations/` | 当前能力页写“未实现”并链接 |
| G3 | 已收口 NO-GO 实验 | archived | `docs/archive/`，裁决在 `docs/evaluation/experiment-verdicts.md` | 登记 `closed-no-go`，从路线移除 |

## 提交前检查

```bash
node docs/validation/check-docs.mjs
node --test docs/validation/check-docs.test.mjs
```

检查失败时先修复主题所有权、生命周期、标题或链接，而不是弱化验证条件。
