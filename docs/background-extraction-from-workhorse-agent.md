# engram 背景:从 workhorse-agent 抽离为独立可嵌入记忆产品

> 🧭 **状态**: 现行(来历/边界正本) · **目标**: 说明 engram 为何抽离、抽离什么、对外形态。
>
> 立项日期:2026-07-18。本文说明 engram 的来历、边界与产品形态。
> 记忆子系统的**技术与战略正本**见 [`memory-strategy.md`](./memory-strategy.md)
> (含 LoCoMo 五轮消融、竞品核查、MemOS 拆解、生物启发检索机制调研);本文只讲
> **"为什么抽离、抽离什么、对外长什么样"**。

## 一句话定位

> **engram —— 可嵌入任意智能体的本地优先记忆层。** 一套记忆引擎,三个集成面
> (MCP server / skill·CLI 客户端 / API SDK),让 Codex、Claude Code、Cursor、
> 自研 Agent 等**无需自建记忆**即可获得长期记忆:混合检索 + 抽取 + curation +
> 完全离线可运行。

## 来历:它从哪来

engram 的核心引擎不是从零写,而是抽离自 **workhorse-agent**
(`github.com/wallfacers/workhorse-agent`)——一个本地单用户多会话 AI agent
服务器。其记忆子系统在 workhorse-agent 内部已达到生产可用,并经 LoCoMo 基准
五轮消融调优(41.2% → ~74.7% 可答题均值)。已验证、可直接抽离的资产:

- **三路混合检索**(语义 cosine + FTS5 BM25 + 实体精确匹配,RRF k=60 融合,
  信号独立优雅降级);
- **ADD-only 抽取流水线**(会话结束把消息蒸馏成新条目,单次 LLM 调用);
- **curation 后台**(确定性 scorer + 近重复聚类 + LLM judge,跨进程 leader
  lease 选主);
- **per-entry 分层存储**(SQLite:PINNED 区 + INDEX 清单,pinned/durability/
  hit_count/event_date/fact_source 等生物记忆天然字段);
- **可持续回归的评测 harness**(`cmd/locomo-bench`,LoCoMo A-B、Mem0 可比口径、
  JSONL resume)——既是产品回归测试,也是论文实验基础设施;
- **无 CGO 依赖**(`modernc.org/sqlite`)、embedding 走本地 sidecar
  (Ollama/fastembed),天然离线。

## 为什么要抽离(而不是留在 workhorse-agent 里)

`memory-strategy.md` 决策一定的方向是"内建一等记忆的本地 Agent 服务器"。但市场
核查发现:**通用本地记忆库赛道已被腾讯云 Agent Memory(SQLite、零依赖、MIT)
占位**,而 MemOS(LoCoMo 88.83)占了国产开源分数制高点。留在 workhorse-agent
内部,记忆能力**只能服务自家 agent**,触达面太窄,无法与已装了 Codex/Claude
Code 的开发者建立关系。

抽离的战略意义:

1. **触达任意宿主 agent**:开发者不必换掉 Codex/Claude Code,只需挂一个 MCP
   server 或装一个 skill,就能给现有 agent 加记忆——**错位竞争,不抢宿主**;
2. **单点投入、多处复用**:同一记忆引擎,三个薄适配层(MCP/CLI/SDK)分发到
   所有生态;workhorse-agent 反过来成为 engram 的**第一个一等公民宿主**(dogfood);
3. **产品叙事清晰**:卖的是"给任意 Agent 装上带调优的长期记忆",不是"再装一个
   agent 框架";
4. **合规定位可继承**:本地优先 + 完全离线,金融/医疗/政企私有化场景直接适用。

## 边界:抽离什么、不抽离什么

**抽离进 engram**:记忆引擎全部(存储、混合检索、抽取、curation、embedding
客户端、评测 harness)。

**留在 workhorse-agent**:会话管理、权限系统、工具生态、外部 agent 适配、
Bash/Read/Write 等工具、SSE 协议层。workhorse-agent 未来通过 engram 的 SDK/MCP
**消费**记忆能力,而非内嵌一份拷贝。

**规模边界如实声明**(继承自战略文档):Go 侧余弦扫描适合单用户 ~10 万条级
记忆,不承诺千万 token 语料;超此规模是后续 ANN/量化(含 FlyHash 候选)的工作。

## 三个集成面(v1 产品形态)

| 集成面 | 面向 | 形态 | 典型宿主 |
|--------|------|------|----------|
| **MCP server** | 支持 MCP 的 agent | 暴露 `memory_search` / `memory_write` / `memory_read` 等工具 | Claude Code、Cursor、任意 MCP 客户端 |
| **Skill / CLI 客户端** | CLI 型 agent / 脚本 | `engram` 二进制,stdin/args→stdout(JSON + 人类可读) | Codex、shell、CI、Claude Code skill |
| **API SDK** | 自研 agent / 服务 | 本地 HTTP + 语言 SDK(先 Go,考虑 Mem0 兼容 API 降迁移成本) | 自建 Agent、后端服务 |

三者共享同一记忆引擎与同一 SQLite 存储;记忆按 namespace 隔离不同宿主/用户
(具体契约在后续 spec 中定)。

## 与论文线的关系

`cmd/locomo-bench` 一并抽入 engram,继续"一份投入两份产出":既是 engram 的
回归测试,也是《长期对话记忆基准的评测可靠性》分析型论文的实验基础设施
(详见 `memory-strategy.md` 决策二)。

## 规范:本项目用 GitHub spec-kit

engram 采用 [github/spec-kit](https://github.com/github/spec-kit) 规范驱动开发:
`constitution → specify → plan → tasks → implement`。脚手架已初始化
(`.specify/`,Claude 集成 skills 在 `.claude/skills/`)。后续每个能力
(记忆引擎抽离、MCP server、CLI、SDK)各自走一遍 spec → plan → tasks 循环。

**下一步**(未在本次落地,后续单独执行):
1. `/speckit-constitution` —— 确立 engram 项目原则(本地优先、离线可运行、
   引擎/适配层分离、评测回归门禁等);
2. `/speckit-specify` —— 第一个能力规格:**记忆引擎抽离**(把 workhorse-agent
   的 `internal/memory` 提炼为独立库 + 稳定 API 契约);
3. 之后依次 MCP server → CLI/skill → SDK 各自立项。
