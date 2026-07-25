# Phase 1 · 数据模型：离线固结 · 跨 session 桥接合成

**日期**: 2026-07-25 | **上游**: [research.md](./research.md) · [spec.md](./spec.md)

## 1. 持久化：migration v6

新增**一张**表，不改任何既有表、不改任何已发布 migration（宪法 III）。

```go
// store/migrations.go
var v6ConsolidationBridges = []string{
	`CREATE TABLE IF NOT EXISTS memory_bridges (
		entry_name TEXT    PRIMARY KEY,
		source_a   TEXT    NOT NULL,
		source_b   TEXT    NOT NULL,
		pair_key   TEXT    NOT NULL,
		created_at INTEGER NOT NULL DEFAULT 0
	)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_bridges_pair ON memory_bridges(pair_key)`,
	`CREATE INDEX IF NOT EXISTS idx_memory_bridges_source_a ON memory_bridges(source_a)`,
	`CREATE INDEX IF NOT EXISTS idx_memory_bridges_source_b ON memory_bridges(source_b)`,
}

var v6ConsolidationBridgesDown = []string{
	`DROP INDEX IF EXISTS idx_memory_bridges_source_b`,
	`DROP INDEX IF EXISTS idx_memory_bridges_source_a`,
	`DROP INDEX IF EXISTS idx_memory_bridges_pair`,
	`DROP TABLE IF EXISTS memory_bridges`,
}
```

在 `migrationsByVersion` 追加 `{Version: 6, Up: v6ConsolidationBridges, Down: v6ConsolidationBridgesDown}`。

### 字段语义

| 字段 | 类型 | 语义 |
|---|---|---|
| `entry_name` | TEXT PK | 合成产物在 `memory_entries` 中的 name。PK 保证一条桥接记忆只有一行血缘 |
| `source_a` | TEXT | 源记忆 name，**排序后较小者** |
| `source_b` | TEXT | 源记忆 name，**排序后较大者** |
| `pair_key` | TEXT | `source_a + "\x00" + source_b`，唯一索引 ⇒ **幂等**（FR-019） |
| `created_at` | INTEGER | unix 微秒，与全库时间口径一致 |

### 设计约束的落点

| 需求 | 由什么保证 |
|---|---|
| FR-019 幂等 | `idx_memory_bridges_pair` 唯一索引；插入用 `INSERT OR IGNORE`，受影响行数为 0 即「已合成过」 |
| 同一对不同枚举顺序视为同一对（Edge Case） | 写入前对 `(a, b)` 排序，故 `pair_key` 与枚举顺序无关 |
| FR-017 整批查询/删除 | 独立表 ⇒ `SELECT * FROM memory_bridges` / `DELETE FROM memory_bridges` 即全量；配合按 `entry_name` 删对应 `memory_entries` |
| FR-018 禁止二阶固结 | 候选枚举时 `LEFT JOIN memory_bridges` 排除已是桥接产物的 entry |
| 源记忆并发删除（Edge Case） | `memory_entries` 与 `memory_bridges` **无外键**（沿用「memory 表无 FK」的既有约定）；落库前重查源存在性，不存在则跳过 |

**无外键说明**: 沿用 CLAUDE.md 记载的既有约定（memory 表不对 sessions 建 FK）。
悬空血缘由写入前校验 + 删除路径清理避免，而非 FK 约束。

## 2. 内存态实体

### 2.1 `CandidatePair` — 候选证据对

```go
type CandidatePair struct {
	A       string   // 源记忆 name（排序后较小者）
	B       string   // 源记忆 name（排序后较大者）
	Shared  []string // 共享的规范化实体，字典序
	Score   float64  // Σ IDF(e) over Shared
}
```

**不变式**:
- `A < B`（字典序），构造时即排序 ⇒ 同一对只有一种表示
- `len(Shared) >= 1`（FR-010）
- A、B 分属不同 `source_session_id`（FR-009）
- A、B 均不在 `memory_bridges.entry_name` 中（FR-018）

**排序规则**（FR-012 确定性）：按 `Score` 降序；`Score` 相同时按 `(A, B)` 字典序
升序。同一输入永远产出同一序列。

### 2.2 `BridgeVerdict` — 语言模型的合成裁决

```go
type BridgeVerdict struct {
	Bridged bool   // false ⇒ 模型判定「无可推出的连接」（FR-013）
	Content string // 桥接事实正文；Bridged 为 false 时为空
	SourceA string // 模型回述的源 A（用于校验，FR-014）
	SourceB string // 模型回述的源 B
}
```

**校验规则**（全部不通过即拒绝落库，告警但不中断整趟）：

| 校验 | 依据 |
|---|---|
| `Bridged == false` → 不落库 | FR-013 |
| `Content` 为空或纯空白 → 不落库 | FR-013 |
| `SourceA`/`SourceB` 不在库中 → 不落库 | FR-014 |
| `SourceA`/`SourceB` 与候选对不一致 → 不落库 | FR-014 |
| `Content` 与任一源记忆内容等价（去空白后相同）→ 不落库 | Edge Case「非新增信息」 |

### 2.3 桥接产物 Entry（复用既有 `memory.Entry`，不新增类型）

| 字段 | 取值 |
|---|---|
| `Name` | 由 `pair_key` 确定性派生，保证幂等重跑同名 |
| `Content` | `BridgeVerdict.Content` |
| `SourceSessionID` | 空（跨 session 产物不归属单一 session） |
| `EventDate` | 两条源记忆 `EventDate` 中**较晚**者；均为空则为空 |
| `FactSource` | `"consolidation"`（既有 `fact_source` 列的新取值，非新列） |
| 实体 | 两条源记忆实体的**并集**（research R4） |

`FactSource` 取新值而非加新列——`fact_source` 本就是自由文本 provenance 列
（既有取值 `'' | user | agent | extraction`），扩展取值不构成 schema 变更。

## 3. 状态流转

```
候选枚举(纯Go,确定性,零模型)
      │
      ├─► 已在 memory_bridges ──────────────────────────► 跳过（幂等，FR-019）
      │
      └─► 未合成过
            │
            ▼
      LLM 合成裁决
            │
            ├─► Bridged=false / 校验失败 ──► 告警，不落库（FR-013/014），继续下一对
            │
            └─► 校验通过
                  │
                  ▼
            Upsert → PutEntities → UpsertEdges → embedder.Enqueue
                  │
                  ▼
            INSERT OR IGNORE INTO memory_bridges
```

**失败语义**: 任一步失败 ⇒ WARN + 跳过该对 + 继续整趟（FR-025）。整趟固结
永不因单对失败而中止，也永不影响既有检索/写入能力。

## 4. 候选枚举查询

```sql
-- 全量倒排 + session 归属，一次扫描；排除已有桥接产物（FR-018）
SELECT me.entity_norm, me.entry_name, e.source_session_id
FROM memory_entities me
JOIN memory_entries e ON e.name = me.entry_name
LEFT JOIN memory_bridges b ON b.entry_name = me.entry_name
WHERE b.entry_name IS NULL
ORDER BY me.entity_norm, me.entry_name;
```

内存中按 `entity_norm` 分桶，桶内两两组合并施加 FR-009（跨 session）过滤，
用 `df = 桶大小` 算 `IDF = log(N / df)` 累加打分，最后全局排序取 top-K。

**规模控制**: 高 df 实体的桶会产生 O(df²) 对。设 `MaxBucketSize`，超过即跳过
该实体桶——高 df 实体本就是低信息量的（IDF 趋近 0），跳过不损失有效候选，却
直接消除组合爆炸的主因。
