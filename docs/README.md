---
title: Engram 文档门户
summary: 本文按任务导航 engram 的当前文档与历史证据；不复制会变化的命令、分数或实验台账。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [docs-portal]
tags: [documentation, portal, navigation]
---

# Engram 文档门户

从这里按任务进入唯一当前正本。`stable` 与 `active` 是当前答案；`proposed` 只描述未实现工作；`archived` 只作历史证据；`relocated` 只保留旧路径兼容，均不应被当作当前能力。

## 常见问题的首个正本

| 问题 | 首个正本 | 当前结论 |
|---|---|---|
| 如何配置 MCP server？ | [MCP Server 配置指南](guides/mcp-server.md) | stable 使用指南 |
| CLI 支持哪些命令？ | [CLI 使用指南](guides/cli.md) | CLI 已交付；完整命令参考不依赖 `engram --help` |
| 如何让 Agent 使用 engram 长期记忆？ | [skill 安装正本](../skills/engram/references/install.md) | 正式支持 Claude Code、Codex、OpenCode |
| 何时抽取记忆，curation 如何运行？ | [记忆系统架构](architecture/memory-system.md) | 仅显式 ingest 抽取；curation 为 shipped-opt-in |
| 当前 LoCoMo 与 LongMemEval-S 结果？ | [当前评测结果](evaluation/results.md) | LongMemEval-S 为 full 500；每行带完整评测口径 |
| Feature 013 是否出货？ | [实验裁决索引](evaluation/experiment-verdicts.md) | closed-no-go，不在当前路线 |
| 新鲜度与状态一致性是否实现？ | [当前能力边界](product/capabilities.md) | 尚未实现，见 proposed backlog |
| SaaS 习惯记忆是否为当前能力？ | [当前能力边界](product/capabilities.md) | 未立项、未实现，见 proposed exploration |
| 当前论文方向？ | [论文方向](research/paper-direction.md) | 负结果史、共同失败机理与低成本止损 |

## 使用指南

- [engram Agent Skill](../skills/engram/references/install.md)：安装、发现、重载与恢复的唯一详细正本；只正式支持 Claude Code、Codex、OpenCode。
- [CLI 使用指南](guides/cli.md)：安装、命令、离线与模型边界。
- [MCP Server 配置指南](guides/mcp-server.md)：MCP 配置、工具、namespace 与 curation。

### 安装 Agent Skill

前置条件：Node.js >=22.20.0、npx/npm、Git 与网络。该命令只安装 skill，不安装 CLI
二进制，也不修改 MCP 配置。用户作用域下，安装器会写入 `~/.claude/skills/engram`、
`~/.codex/skills/engram` 与 `~/.config/opencode/skills/engram`，并在写入前要求确认。

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/<ENGRAM_SKILL_TAG>/skills/engram --global --agent claude-code --agent codex --agent opencode
```

默认选择 `Symlink`，受限文件系统选择 `Copy`；安装后重载各客户端，并确认每个客户端
恰好发现一个 `engram` skill。项目作用域、单客户端、离线后备与升级说明只维护在
[skill 安装正本](../skills/engram/references/install.md)。

## 架构

- [记忆系统架构](architecture/memory-system.md)：写入、抽取、存储、检索和 curation 边界。
- [Provenance 架构](architecture/provenance.md)：来源、事件与宿主边界。

## 评测与运维

- [当前评测结果](evaluation/results.md)：唯一当前结果矩阵。
- [实验裁决索引](evaluation/experiment-verdicts.md)：已收口实验与 Feature 013 verdict。
- [竞品与基准口径](evaluation/competitors.md)：同栈复现与厂商自报的比较纪律。
- [MemOS LoCoMo 同栈复现报告](evaluation/reports/memos-locomo-reproduction.md)：可追溯的同栈证据。
- [LoCoMo 评测运行手册](operations/evaluation/locomo-runbook.md)：recipe、验证和常见故障。
- [远端 GPU 评测运维](operations/evaluation/remote-gpu-runbook.md)：机器生命周期、资产和停机纪律。
- [基准扩展路线](evaluation/benchmark-roadmap.md)：未实现的 proposed 评测方向。

## 产品与研究

- [当前能力边界](product/capabilities.md)：已交付和未实现能力的唯一当前答案。
- [产品路线图](product/roadmap.md)：当前方向与明确排除项。
- [记忆新鲜度与状态一致性 Backlog](product/backlog/memory-freshness.md)：未实现的 proposed backlog。
- [SaaS 习惯记忆探索](product/explorations/habit-memory.md)：未立项、未实现的 proposed 探索。
- [论文方向](research/paper-direction.md)：评测可靠性研究方向。

## AI 检索协议

1. 先用 `canonical_for` 定位主题，再只接受 `stable` 或 `active` 文档作为当前答案。
2. 对计划问题才读取 `proposed`，并明确“未实现”；对历史原因才读取 `archived`，并说明适用时间与 outcome。
3. 将 `relocated` 视为路由，不从其正文推断功能状态。
4. 当前能力、命令、路线、裁决和完整分数以各自正本为准；需要依据时再沿其 evidence 链接进入归档。
5. Q6、Q7 的 proposed 页面是次级说明，当前答案唯一来自[当前能力边界](product/capabilities.md)。

## 历史与维护

- [历史归档索引](archive/README.md)：决策、评测、计划、研究与历史设计。
- [文档维护规范](CONTRIBUTING.md)：新增、更新、引用、归档、删除与人工复核规则。
