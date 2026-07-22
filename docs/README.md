# engram docs 索引

本目录是 engram 的**叙事性文档**(战略、背景、竞品、适配器用法、未决问题记录)。
逐特性的规格/实现细节在 [`../specs/NNN-*/`](../specs);工程约束正本是
[`../.specify/memory/constitution.md`](../.specify/memory/constitution.md);
一句话知识地图见根 [`../CLAUDE.md`](../CLAUDE.md) 的 Knowledge Map 一节。

> 状态语义:**活跃**=持续维护的决策/参考正本 · **已交付**=对应 spec 已实现的用法文档 ·
> **待实现**=已确认但尚未立项/实现的问题记录 · **存档**=一次性分析,结论已被吸收、不再更新。

## 索引

| 文档 | 状态 | 目标 | 正本/关联 |
|---|---|---|---|
| [capability-and-product-north-star.md](./capability-and-product-north-star.md) | 活跃 | **北极星总纲**:能力认知 + 诚实水位(83.70%)+ 拉平/论文/SaaS 习惯记忆方向 | 一处看全;技术 backlog 链回 memory-strategy |
| [memory-strategy.md](./memory-strategy.md) | 活跃 | 产品方向 + 论文线 + 涨点 backlog(生物启发检索 P0/P1/P2) | **技术/战略 backlog 正本**;数字/SaaS 演进见北极星总纲 |
| [locomo-score-levers.md](./locomo-score-levers.md) | 活跃 | LoCoMo 跑分杠杆实验台账(008 US1-US4 verdict + 新参考点) | 杠杆 verdict 正本 |
| [background-extraction-from-workhorse-agent.md](./background-extraction-from-workhorse-agent.md) | 活跃 | 为何抽离、抽离什么、对外产品形态 | 来历/边界正本 |
| [competitive-benchmarks.md](./competitive-benchmarks.md) | 活跃 | 为涨点锚定外部竞品目标 + 口径对齐 | MemOS 机制拆解正本在 memory-strategy 附 |
| [local-model-eval-setup.md](./local-model-eval-setup.md) | 活跃(计划稿) | 自托管 70B + 本地 embedding 的离线评测栈 | embedding 已在用;LLM 侧待部署 |
| [memory-freshness-and-retrieval-policy.md](./memory-freshness-and-retrieval-policy.md) | 待实现 | 记忆新鲜度/状态一致性/按需召回问题记录 | 须独立立项;**非当前能力** |
| [synthius-mem-analysis.md](./synthius-mem-analysis.md) | 存档 | Synthius-Mem 抄什么/不抄什么 + 认知域文献核查 | P0 已落地 spec 006;backlog 正本在 memory-strategy 附二 |
| [cli.md](./cli.md) | 已交付 | engram CLI 适配器用法 | spec 004-cli-ai-first |
| [mcp-server.md](./mcp-server.md) | 已交付 | MCP stdio server 构建与接入 | spec 002-mcp-server |
| [superpowers/specs/](./superpowers/specs) | 存档 | brainstorm 设计定稿(003/004/007) | 被对应 spec.md 引用,勿删 |

## 正本约定(避免重复维护)

- **涨点 backlog / 生物启发检索优先级** → 只在 `memory-strategy.md` 附二维护;
  其他文档引用不重复。
- **竞品机制拆解(MemOS 等)** → 正本在 `memory-strategy.md` 附;
  `competitive-benchmarks.md` 只记分数与差距。
- **单篇论文分析(Synthius 等)** → 结论若可执行,落到 spec 或 memory-strategy backlog 后,
  原分析文档转"存档"、不再更新。
