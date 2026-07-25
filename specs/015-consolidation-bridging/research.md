# Phase 0 · 技术调研：离线固结 · 跨 session 桥接合成

**日期**: 2026-07-25
**上游**: [设计文档](../../docs/superpowers/specs/2026-07-25-offline-consolidation-bridging-design.md) · [spec.md](./spec.md)

本轮调研的目的是把设计文档的决策落到既有代码的确切接缝上。调研中发现**两处
设计文档与代码现状不符**（R1、R2），已按事实修正并回写设计文档。

---

## R1（修正）Migration 版本是 v6，不是 v3

**Decision**: 新增的 `memory_bridges` 表走 **migration v6**。

**Rationale**: 设计文档写的「v3 migration」在代码现状下是错的。
`store/migrations.go` 的 `migrationsByVersion` 现为：

| 版本 | 内容 | 归属 |
|---|---|---|
| v1 | memory store + FTS5 镜像 + curation lease | 001 |
| v2 | hybrid：event_date/fact_source + embeddings + entities | 002 |
| v3 | `v3BioRetrieval`（关联/时序/冲突索引） | 003 |
| v4 | `v4TemporalIndexes` | 013 |
| v5 | `v5FactQueries` | 012 |
| **v6** | **`v6ConsolidationBridges`（本特性）** | **015** |

宪法与 CLAUDE.md 均规定「绝不修改已发布的 migration，只新增版本」，故取下一个
可用号 v6。

**Alternatives considered**: 无（版本号由现状唯一决定）。

---

## R2（修正）Lease 是单例行锁，consolidation 与 curation 必然互斥

**Decision**: **共用**既有的 `memory_curation_lease` 单例锁，即 consolidation 与
curation **不能同时运行**。consolidation 包 import `memory/curation` 并调用
`curation.NewLease(db)`，不新建 lease 表、不改 curation 任何代码。

**Rationale**: 设计文档只说「复用 `curation.Lease`」，未点明其后果。实际表结构是

```sql
CREATE TABLE memory_curation_lease (
  id INTEGER PRIMARY KEY CHECK (id = 1),   -- 单例行
  ...
)
```

`id = 1` 的 CHECK 约束使它是一把**全局单锁**，不是按名字分的多锁。两个 Worker
共用它 ⇒ 天然互斥。

这个互斥**恰好是我们想要的**，不是需要规避的缺陷：存储层是
`SetMaxOpenConns(1)` 的单写连接，两个重后台 pass 并发只会在 SQLite 写锁上互相
阻塞，不会有任何吞吐收益，反而增加长事务与超时风险。让它们串行是正确语义。

代价：`consolidation → curation` 的单向包依赖（无环，可接受）。

注意：`Lease.procMu` 是**每个 Lease 实例**的进程内互斥。同进程内两个不同实例
（curation 一个、consolidation 一个）的 procMu 相互独立，进程内互斥退化，但 DB 层
CAS 仍作用于同一行，**互斥保证不受影响**。

**Alternatives considered**:
- *给 lease 表加 name 列做多命名锁*：需要新 migration 改表 + 改 curation 代码
  路径，为一个我们并不想要的并发能力付出改动成本。否决。
- *consolidation 自建独立 lease 表*：实现两把锁，允许两个重 pass 并发争抢单写
  连接——正是上面论证要避免的。否决。

---

## R3 桥接产物的落库序列

**Decision**: 照抄 `memory/pipeline/pipeline.go` 的既有写入序列：

```go
entries.Upsert(ctx, entry)             // 基表 + FTS5 镜像（触发器自动）
entries.PutEntities(ctx, name, ents)   // 实体倒排
entries.UpsertEdges(ctx, pairs)        // 实体图边
embedder.Enqueue(name)                 // nil-safe，异步向量化
```

**Rationale**: 这是引擎既有的唯一正规写入路径，走它即自动获得 FTS5 镜像、实体
索引与向量，无需任何特殊路径——直接满足 FR-016（产物同构、检索侧零改动）。
`embedder.Enqueue` 本身 nil-safe，未配置向量化时静默跳过，满足宪法 I。

**Alternatives considered**: 自建写入路径。否决——会绕过 FTS 触发器与实体索引，
产物将无法被三路检索完整命中。

---

## R4（设计文档未覆盖的落地决策）桥接产物的实体取源记忆实体的并集

**Decision**: 桥接记忆的实体 = 两条源记忆实体的**并集**，不额外要求语言模型输出
实体。

**Rationale**: 设计文档只规定了产物是同构 entry，未规定其实体从何而来。取并集
是确定性的、零额外模型输出的，且语义正确——桥接事实谈论的正是两条源事实共同
涉及的对象。让模型再输出一份实体会引入第二个可失败的解析点，与「LLM 输出面越小
越可靠」的取向相悖。

**标注**: 本项为设计文档未覆盖的实现细节，由 plan 阶段裁定并记录在此，不构成对
设计决策的更改。

**Alternatives considered**:
- *让模型同时返回实体*：多一个解析失败面，且模型可能生成源记忆中不存在的实体，
  污染实体倒排。否决。
- *对桥接内容重跑实体抽取*：需要额外模型调用，成本翻倍，收益不明。否决。

---

## R5 候选枚举的数据访问方式

**Decision**: consolidation 的 Worker 持有 `*sql.DB` 句柄直接查询
`memory_entities`（照 `curation.Worker` 同时持 `store` 与 `db` 的既有先例），
**不在 `memory` 包新增公开 API**。

**Rationale**: 候选枚举需要「全量 entity → entry 倒排 + 每 entry 的 session
归属」，现有公开 API（`EntityDocFreq`、`EntitiesByEntry`、`EntityMatchCounts`）
都是按给定键查询的，拼不出全量扫描且效率差。直接一条 JOIN 查询最简单。
`curation.Worker` 已经确立了「后台 worker 持 db 句柄」的先例，遵循它可保持引擎
公开表面最小（宪法 III 契约优先：不为内部实现扩大对外契约）。

**Alternatives considered**: 在 `memory` 包加 `AllEntityPostings()` 等方法。
否决——为单一内部消费者扩大公开契约，且该方法对外无普适价值。

---

## R6 证伪门（P1）的执行载体

**Decision**: 门 0 与门 1 均作为**一次性诊断脚本**执行，产物为判决报告，**不进入
引擎代码库**；脚本置于会话 scratchpad，报告归档到 `docs/locomo-score-levers.md`
与 HF 私有集。

**Rationale**: 证伪门的全部价值在于「在写引擎代码之前判死」。把它做成引擎代码
就自相矛盾了。它复用的都是**已存在**的东西：

| 需要的能力 | 既有来源 |
|---|---|
| 标准证据标注 | `locomoQA.Evidence`（`["D1:1","D2:1"]` 形式的 turn 级标注） |
| entry → turn 映射 | `--chunks` 模式下的 `chunkTurns map[string][]string` |
| 仅检索、零回答模型的评测 | 已存在的 `--coverage-only` 标志 |
| 覆盖度量口径 | `cmd/locomo-bench/coverage.go` 的 `evidenceRecallAt` |
| 历史 store 与 trace | HF 私有集 `wallfacers/engram-locomo-artifacts` |

**Alternatives considered**: 把诊断做成 `cmd/locomo-bench` 的新标志。否决——为
一次性判决增加常驻代码与维护面；且若判死，这些代码即成死代码。

**注意**: 本地 scratchpad 缓存已清空，门 0 执行前需从 HF 私有集重新拉取 store 与
trace。这不影响可执行性（spec Assumptions 已声明）。

---

## R7 `ModelCaller` 契约沿用既有函数类型

**Decision**: consolidation 定义与 `curation.ModelCaller` / `pipeline.ModelCaller`
同形的类型：

```go
type ModelCaller func(ctx context.Context, system, user string) (string, error)
```

**Rationale**: 引擎中已有两处采用完全相同的签名，第三处沿用可保持一致性，并让
适配层用同一个闭包接三处。`nil` 即「未配置」，直接支撑 FR-020 的 inert 语义
（照 `curation.NewWorker` 的 `call == nil` 先例）。

**Alternatives considered**: 复用 `curation.ModelCaller` 类型本身。否决——会让
consolidation 的对外契约依赖 curation 的类型，语义上不相干的两个包产生契约耦合
（R2 的实现依赖是单向且局部的，契约耦合则会外溢到调用方）。

---

## 未解决项

无。所有 NEEDS CLARIFICATION 均已解决。
