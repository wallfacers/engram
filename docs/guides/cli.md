---
title: CLI 使用指南
summary: 本文说明当前 engram CLI 的安装、常用命令和离线边界；不替代 MCP 配置或内部存储架构说明。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [cli-usage]
tags: [cli, guide, operations]
---

# CLI 使用指南

CLI 为 `shipped-default` 的已交付接口，默认命令可直接管理本地 namespace 中的记忆；MCP 客户端接入请参阅[MCP server 指南](mcp-server.md)。

## 安装与帮助

从仓库构建后使用 `engram version` 进行无状态的可执行文件诊断。当前
`engram --help` 只返回简短 usage 并以 usage code 退出，不能作为完整命令或参数
参考；稳定命令表以本页和运行时契约测试为准。

如果 Agent 需要在 Claude Code、Codex 或 OpenCode 中编排这些已有命令，安装
[engram Agent Skill](../../skills/engram/references/install.md)。skill 只提供工作流知识，
不会安装这个 CLI、修改 `PATH`，也不会注册 MCP server；二进制、数据目录和模型配置仍由
用户单独管理。

## 命令

| 目的 | 命令族 | 当前用途 |
|---|---|---|
| 写入事实 | `add`、`delete` | 添加或删除显式指定的记忆；不触发对话抽取 |
| 显式抽取 | `ingest` | 对输入材料执行显式 ingest 与记忆抽取；需要 LLM 配置 |
| 检索与查看 | `search`、`get`、`list`、`stats`、`export` | 查询、查看、统计或导出 namespace 中的记忆 |
| 维护与发现 | `curate`、`namespaces`、`version` | 显式执行 curation、列出 namespace 或查看版本 |

## 离线与模型边界

基础检索可在没有 embedding 服务时退化为本地 keyword 路径。需要抽取、semantic 检索或模型生成的命令依赖相应模型配置；失败时应保留错误，不应把降级结果当作同等语义结果。具体存储、检索和 curation 语义见[记忆系统架构](../architecture/memory-system.md)。

## 自动化建议

脚本应固定二进制版本、namespace、模型配置和输入来源，并在运行前执行 `engram version` 或 `engram <command>` 的诊断输出确认能力可用。不要依赖当前工作目录隐式选择 namespace，也不要把评测命令当作常规生产写入流程。
