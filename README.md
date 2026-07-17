# engram

**可嵌入任意智能体的本地优先记忆层。** 一套记忆引擎,三个集成面
——MCP server / skill·CLI 客户端 / API SDK——让 Codex、Claude Code、Cursor、
自研 Agent 无需自建记忆即可获得长期记忆:三路混合检索(语义 + BM25 + 实体 RRF)
+ 抽取 + curation,完全离线可运行,无 CGO 依赖。

engram 的核心引擎抽离自 [workhorse-agent](https://github.com/wallfacers/workhorse-agent)
的记忆子系统(已经 LoCoMo 五轮消融调优)。

## 文档

- [`docs/background-extraction-from-workhorse-agent.md`](docs/background-extraction-from-workhorse-agent.md)
  —— 立项背景:为什么从 workhorse-agent 抽离、抽离什么、三个集成面的产品形态。
- [`docs/memory-strategy.md`](docs/memory-strategy.md)
  —— 技术与战略正本:LoCoMo 调优结论、国内外竞品核查、MemOS 88.83 拆解、
  生物启发检索机制调研(Ecphory/Chronos/D-MEM/Abstain-R1/FadeMem/HippoRAG…)
  与统一涨点优先级。

## 开发规范

本项目采用 [github/spec-kit](https://github.com/github/spec-kit) 规范驱动开发:
`constitution → specify → plan → tasks → implement`。脚手架已初始化于 `.specify/`,
Claude 集成 skills 在 `.claude/skills/`。

## 状态

立项中(2026-07-18)。脚手架 + 战略文档已就位;下一步走
`/speckit-constitution` 与第一个能力规格「记忆引擎抽离」。
