# Quickstart: 文档重组验证

**Feature**: `019-docs-information-architecture`
**Run from**: 仓库根目录
**Network required**: 否

本指南按验收顺序运行。字段与状态规则以
[元数据契约](./contracts/document-metadata.md) 为准，路径和固定问答以
[导航检索契约](./contracts/navigation-and-retrieval.md) 为准，历史处置以
[归档迁移契约](./contracts/archive-and-relocation.md) 为准。任一门失败都先修正文档，
不得跳过。

## 1. 前置条件

- 当前分支为 `019-docs-information-architecture`。
- Node.js 24、Bash 5.2、ripgrep 15 和 Git 2.53 可用。
- 不需要网络、付费模型、LoCoMo 数据或新增 package。
- 本 feature 在吸收已接受的双语根 README 并行提交后，以 `c86e47e` 为实施隔离基线。

```bash
git branch --show-current
node --version
bash --version | head -1
rg --version | head -1
git --version
git status --short
```

开始迁移前记录 `git status`。若目标文件出现来源不明的并行修改，停止该文件并先确认
双方意图。

## 2. 验证变更隔离

产品和仓库顶层文件必须相对 feature 基线保持不变：

```bash
git diff --name-only c86e47e -- \
  README.md README.zh-CN.md LICENSE CLAUDE.md AGENTS.md go.mod go.sum \
  '*.go' cmd internal pkg migrations
```

预期为空。

`specs/` 中除 019 规格工件外，只能修改归档迁移契约列出的 007、009–012、014–016
历史设计链接：

```bash
git diff --name-only c86e47e -- specs \
  ':!specs/019-docs-information-architecture/**'
```

逐项与
[允许的范围外链接清单](./contracts/archive-and-relocation.md#5-必须归档的历史设计)
比较；任何其他路径都失败。

## 3. 验证目标目录

```bash
find docs -type f -name '*.md' -print | sort
git status --short docs
```

确认：

- `docs/README.md` 和 `docs/CONTRIBUTING.md` 存在；
- 现行正文只位于 `guides/`、`architecture/`、`operations/`、`evaluation/`、
  `product/`、`research/`；
- `archive/` 只有历史证据及其索引；
- 顶层旧正文只剩契约登记的 12 个 `relocated` 兼容入口；
- `docs/superpowers/specs/` 已无现行设计类别。

## 4. 验证元数据与正文结构

使用 Node.js 标准库对 `git ls-files 'docs/*.md' 'docs/**/*.md'` 返回的每个文件执行
[元数据契约](./contracts/document-metadata.md) 的确定性检查。检查器只读取文件，不写入
仓库，必须报告：

```text
PASS metadata: <document-count> docs, <topic-count> canonical topics
PASS headings: <document-count> docs
```

通过条件：

- 每份文档只有一块文件首部 front matter；
- 八个共同字段完整且枚举、日期、slug、条件字段有效；
- 每个 `canonical_for` 主题只有一个所有者；
- 每份文件只有一个与 `title` 一致的 H1；
- 标题层级不跳级，GitHub 风格标题 slug 文件内唯一；
- `archived` 有 outcome 或替代入口；
- `relocated` 直接指向 `stable` / `active` 正本；
- 当前正文以中文为主，技术标识保持标准英文。

可用以下命令快速审阅生命周期分布；它不替代完整结构检查：

```bash
rg -n '^status: (stable|active|proposed|archived|relocated)$' docs -g '*.md'
rg -n '^canonical_for: \\[[a-z0-9,-]+( [a-z0-9,-]+)*\\]$' docs -g '*.md'
```

## 5. 验证全仓链接、锚点与可达性

使用同一只读 Node.js 标准库检查器扫描：

```bash
git ls-files '*.md'
```

检查 Markdown fenced code 之外的本地链接，按目标文件解析相对路径，并按 GitHub 标题
slug 解析 `#fragment`。再以 `docs/README.md` 为根对 `docs/` 本地链接图执行 BFS。

必须得到：

```text
PASS links: 0 missing files, 0 missing anchors
PASS navigation: all current/proposed docs within 2 hops, 0 orphan docs
```

其中：

- `stable`、`active`、`proposed` 距门户最多两跳；
- 每份 archive 从历史索引、现行 verdict 或正式证据链至少有一个入链；
- relocated 不进入门户，也不计作到达现行正本的一跳；
- 全部 tracked Markdown 的本地文件和章节深链都有效。

## 6. 验证 Q1–Q8

先从 front matter 构建只含 `stable` / `active` 的主题索引，逐项核对
[固定检索集](./contracts/navigation-and-retrieval.md#5-fixed-retrieval-verification-set)。
确定性检查必须输出：

```text
PASS retrieval fixtures: Q1-Q8 canonical paths and required assertions
```

重点确认：

- Q1–Q5、Q8 的主题和路径各自唯一；
- Q6/Q7 都先到 `docs/product/capabilities.md`，backlog/exploration 只是次级证据；
- Q3 得到 `shipped-opt-in`；
- Q4 得到 full 500 且结果包含 dataset/answerer/judge/recipe；
- Q5 得到 Feature 013=`closed-no-go`；
- proposed、archived、relocated 的当前答案命中数为 0。

结构门通过后，由两个独立审阅过程分别从 `docs/README.md` 开始回答 Q1–Q8，记录首个
正本、生命周期、结论和证据链接。两份结果必须 8/8 一致。

## 7. 验证迁移页和已知漂移

迁移页必须恰好是归档契约列出的 12 个路径：

```bash
rg -l '^status: relocated$' docs -g '*.md' | sort
```

逐页确认 front matter 后只有一个 H1、一段迁移说明和一个正本链接，不含数字、命令、
配置、功能状态、代码块或第二个业务链接。

以下已知错误不得出现在现行正本：

```bash
rg -n \
  'LongMemEval[^\\n]*(100 题|全量[^\\n]*未跑)|CLI[^\\n]*(未来|后续)[^\\n]*(实现|交付)|curation[^\\n]*(待实现|未交付)|习惯记忆[^\\n]*已实现' \
  docs/guides docs/architecture docs/operations docs/evaluation docs/product docs/research
```

预期为空。`proposed` 文档可以明确写“未实现”，但不能声称已经出货。

当前完整 LongMemEval-S 数字只允许出现在结果正本或明确归档的历史证据：

```bash
rg -l '404/500|430/500|80\\.80%|86\\.00%' \
  docs -g '*.md' -g '!evaluation/results.md' -g '!*archive*'
```

预期为空；其他现行文档只链接 `docs/evaluation/results.md`。

## 8. 最终回归

```bash
git diff --check
CGO_ENABLED=0 go test -count=1 ./...
git status --short
git diff --stat c86e47e
git diff --name-status c86e47e
```

预期：

- whitespace 检查和 Go 全量测试退出码为 0；
- 只有 `docs/`、019 规格工件和允许的历史设计链接文件发生变化；
- 删除候选入链为 0；
- 保留文档孤儿为 0；
- 全仓坏链与坏锚点为 0；
- 两份 Q1–Q8 独立复核完全一致。
