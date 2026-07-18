# Data Model: 生物启发检索涨点（feature 003）

> Phase 1 产出。全部为 migration v3 增量（`store/migrations.go` 追加
> `Migration{Version:3}`），向后兼容：新表独立、新列 nullable/带默认值，
> 旧库升级零数据迁移动作。命名空间语义不变（宪法 III）。

## 1. 表变更总览

| 对象 | 类型 | 用途 | 服务的机制 |
|------|------|------|------------|
| `memory_entity_edges` | 新表 | 实体共现/同义边 | Strike 1 联想游走 |
| `memory_event_aliases` | 新表 + FTS | 事件词汇别名 | Strike 2 BM25 召回 |
| `memory_entries.event_start` | 新列 INTEGER NULL | 事件时间范围起（epoch 秒） | Strike 2 时间过滤/T_score |
| `memory_entries.event_end` | 新列 INTEGER NULL | 事件时间范围止 | 同上 |
| `memory_entries.superseded_by` | 新列 TEXT NULL | 非破坏压制指针 | Strike 3 冲突消解 |

既有 `event_date` 列保留（兼容读路径）；写路径改写 `event_start/event_end`，
单点事件 start=end。**不加 strength 列**——压制降权用固定系数，最小化 schema。

## 2. 新表定义

### memory_entity_edges

```sql
CREATE TABLE memory_entity_edges (
  entity_a   TEXT NOT NULL,   -- EntityNorm 归一化，字典序 entity_a < entity_b
  entity_b   TEXT NOT NULL,
  kind       TEXT NOT NULL DEFAULT 'co',  -- 'co'=共现 | 'syn'=同义(余弦>0.8)
  weight     REAL NOT NULL DEFAULT 1.0,   -- co: 共现次数累计; syn: 余弦值
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (entity_a, entity_b, kind)
);
CREATE INDEX idx_entity_edges_a ON memory_entity_edges(entity_a);
CREATE INDEX idx_entity_edges_b ON memory_entity_edges(entity_b);
```

- 写入：`Pipeline.storeFact` 内同 entry 实体两两连边（`ON CONFLICT` weight+1）；
  同义边由离线批任务（curation pass 内）扫 `memory_embeddings` 补写。
- 无向图：入库前排序 (a,b)，查询时两向索引各查一次。
- node specificity（IDF）不落列：查询时
  `SELECT entity_norm, COUNT(*) FROM memory_entities GROUP BY entity_norm` 内存缓存。

### memory_event_aliases

```sql
CREATE TABLE memory_event_aliases (
  entry_name TEXT NOT NULL,
  alias      TEXT NOT NULL,
  PRIMARY KEY (entry_name, alias)
);
CREATE VIRTUAL TABLE memory_event_aliases_fts USING fts5(
  alias, entry_name UNINDEXED, tokenize='trigram'
);
-- 同步 trigger 仿照 memory_entries_fts 三件套（store/migrations.go:53-64 模式）
```

- 写入：抽取产出 2-4 个别名随 entry 落库；`keywordRanks` 对两个 FTS 表 union，
  别名命中折算到宿主 entry 的 BM25 排名。

## 3. 列变更

```sql
ALTER TABLE memory_entries ADD COLUMN event_start INTEGER;   -- NULL=无事件时间
ALTER TABLE memory_entries ADD COLUMN event_end INTEGER;
ALTER TABLE memory_entries ADD COLUMN superseded_by TEXT;    -- NULL=未被取代
```

Go 侧同步点：`Entry` 结构体、`entrySelectCols`、`upsert`、`scanEntry`
（`memory/entrystore.go:18/147/101/151`）。

## 4. 实体关系与生命周期

```
memory_entries 1 ──< memory_entities >──edges──< memory_entities >── 1 memory_entries
      │ 1                    (memory_entity_edges: co/syn)
      ├──< memory_event_aliases（FTS 镜像）
      ├── event_start/event_end ──→ 查询时间窗匹配（T_score）
      └── superseded_by ──→ memory_entries.name（取代者）
```

**压制生命周期（非破坏，Q2 裁决）**：

```
active（superseded_by IS NULL）
  └─ curation 判 Contradictory(新压旧) ─→ superseded（superseded_by=新条目 name）
        ├─ 默认检索：score × supersededPenalty（固定系数）；超阈值过滤
        ├─ 时间意图查询：不过滤（旧值即历史答案）
        └─ 误判回退：置 NULL 即恢复（审计路径可见全史）
Subsumes/Subsumed ─→ 走既有 merge（内容合并，被并者按既有 evict 语义处理）
```

**校验规则**：
- `superseded_by` 必须指向存在的 entry name；指向自身非法；链长不限但检索只看一跳
  （是否被取代），不递归解链。
- pinned 条目不可被压制（沿用 curation `apply` 对 pinned 的保护）。
- `event_start <= event_end`（写入侧校验，违者交换）。
- 边表 `entity_a < entity_b`（写入侧规范化）。

## 5. 降级矩阵（宪法 V，扩展 TestSignalDegradation）

| 缺失条件 | 行为 |
|----------|------|
| 边表空/查询无 cue 实体 | 联想信号返回空，RRF 退回三路 |
| 无 embedding client | 同义边不建；游走重排跳过（按游走权重序）；语义路既有降级不变 |
| event_start/end 全 NULL | T_score 信号跳过，无时间过滤 |
| 查询无时间意图 | 同上（解析器返回零窗） |
| judge 不可用 | 冲突消解不运行，superseded_by 保持现状，检索照常 |

## 6. 评测侧数据（不入引擎 schema）

- run-dir 下每次重复跑落 `run-<i>/results.jsonl`（逐题：question_id、category、
  correct、answer、token 用量）；`stats.json`（per-category 均值±CI）；
  `cost.json`（预估 vs 实际、按角色分桶）。配对 diff 读两组 run-dir 对齐
  question_id 计算。LongMemEval_S 题型在 results.jsonl 记原始 type 与映射桶。
