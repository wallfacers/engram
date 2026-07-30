---
title: MCP Server 配置指南
summary: 本文说明如何配置 engram MCP server、namespace 与工具边界；不替代 CLI 命令参考或评测运维手册。
status: stable
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [mcp-integration]
tags: [mcp, guide, configuration]
---

# MCP Server 配置指南

engram MCP server 提供面向客户端的本地记忆接入。配置文件应以已安装二进制的 `--help` 输出为准；本文说明稳定的接入模型与安全边界。

## 配置

在 MCP 客户端中注册 `engram-mcp` server，明确指定可执行文件和数据目录。namespace 是每次 MCP 工具调用的参数；每个 namespace 独立保存，不应把不同用户或环境混入同一 namespace。

示例形态如下；字段名随客户端而异：

```json
{
  "mcpServers": {
    "engram": {
      "command": "engram-mcp",
      "args": ["--data-dir", "/path/to/engram-data"]
    }
  }
}
```

## 工具与写入边界

MCP 工具用于显式搜索、读取、写入和 ingest。只有客户端显式调用 ingest 才会触发抽取；`write` 或 `add` 直接保存给定内容。不要假定普通聊天内容会被自动持久化。

[engram Agent Skill](../../skills/engram/references/install.md) 可在 Claude Code、Codex 和
OpenCode 中选择这些已经连接的工具；它不会安装 `engram-mcp`、编辑 MCP 配置或赋予工具
权限。skill 激活后仍应优先检查实际 `tools/list`，每次调用只使用一个明确 namespace，并在
MCP 不可用时才考虑独立配置的 CLI 路径。

## Curation

curation 是 `shipped-opt-in`：它已交付，但仅在显式开启或调用时运行。启用前请先确认 namespace、模型依赖和审计需求；它不是默认后台进程。完整生命周期与检索边界见[记忆系统架构](../architecture/memory-system.md)。

缺少 LLM 时，CRUD、`memory_ingest_v2` 与 Evidence 读取/生命周期工具仍可离线工作。
`memory_ingest_v2` 必须带稳定 session/source ID 和 ordinal；它会先保存原文，随后将
`extraction_unavailable` 如实报告为“已保存、未抽取”。条件 `memory_ingest` 仍不会出现；
skill 不得把缺失的模型能力报告为已抽取成功。namespace 默认是 `default`，但项目、用户或
数据目录切换必须显式确认，不能以同名 namespace 推断跨 adapter 的同一存储。

## 故障排查

先用[CLI 使用指南](cli.md)中的 `version` 与命令诊断确认二进制和模型依赖，再检查客户端日志、工具调用中的 namespace 参数和数据目录权限。配置问题不要通过删除已有 namespace 来“重置”；先导出或备份需要保留的数据。
