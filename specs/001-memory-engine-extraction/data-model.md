# Phase 1 Data Model: 记忆引擎抽离

抽离**保真**,故数据模型 = workhorse-agent 记忆 schema 原样迁移(v7+v8 记忆迁移),
在 engram 内 renumber 为独立链但**列定义逐字段不变**。所有时间戳为 INTEGER unix 微秒。

## 实体一览

| 实体 | 表 | 角色 |
|------|-----|------|
| 记忆条目 Memory Entry | `memory_entries` | 存储与检索的基本单位 |
| 条目 FTS 镜像 | `memory_entries_fts`(FTS5, trigram) | BM25 关键词信号,经触发器与基表同步 |
| 向量 Embedding | `memory_embeddings` | 每条一个 float32 向量 BLOB,换模型可重建 |
| 实体索引 Entity | `memory_entities` | 归一化实体 → 条目,支撑实体精确匹配信号 |
| curation 租约 | `memory_curation_lease` | 单行,跨进程 curation 选主 leader-lease |

## memory_entries(基表)

| 列 | 类型 | 约束/默认 | 含义 |
|----|------|-----------|------|
| id | TEXT | PRIMARY KEY | 条目标识 |
| name | TEXT | NOT NULL UNIQUE | 条目名(检索排序对拍的键) |
| trigger | TEXT | NOT NULL DEFAULT '' | 触发线索 |
| content | TEXT | NOT NULL DEFAULT '' | 记忆正文 |
| pinned | INTEGER | NOT NULL DEFAULT 0 | 是否固定(PINNED 区) |
| durability | TEXT | NOT NULL DEFAULT 'volatile' | 留存度(evergreen/volatile 等) |
| category | TEXT | NOT NULL DEFAULT '' | 分类 |
| hit_count | INTEGER | NOT NULL DEFAULT 0 | 命中次数 |
| last_used_at | INTEGER | 可空 | 最近使用时刻 |
| created_at | INTEGER | NOT NULL | 记录时刻 |
| updated_at | INTEGER | NOT NULL | 更新时刻 |
| char_count | INTEGER | NOT NULL DEFAULT 0 | 字符预算统计 |
| source_session_id | TEXT | NOT NULL DEFAULT '' | 来源会话(**仅文本,无外键**——记忆与会话解耦的关键) |
| event_date | INTEGER | 可空(v8) | 事实**发生**时刻,区别于 created_at(**记录**时刻) |
| fact_source | TEXT | NOT NULL DEFAULT ''(v8) | 溯源:''｜user｜agent｜extraction |

索引:`idx_memory_pinned ON (pinned)`。

**解耦要点**:`source_session_id` 无 FK 约束,记忆表不引用 sessions,故记忆 schema 可脱离
会话 schema 独立建链——这是 engram store 能"只含记忆结构"的根本原因。

## memory_entries_fts(FTS5 镜像)

`fts5(name, trigger, content, tokenize='trigram')`,三列均为明文,经 ai/ad/au 三触发器与
基表同步。**不使用 extract_text**(区别于会话侧 messages_fts)。

## memory_embeddings

| 列 | 类型 | 约束 | 含义 |
|----|------|------|------|
| entry_name | TEXT | PRIMARY KEY | 关联 memory_entries.name |
| model | TEXT | NOT NULL DEFAULT '' | 生成向量的模型 |
| dims | INTEGER | NOT NULL DEFAULT 0 | 向量维度 |
| vec | BLOB | NOT NULL | float32 向量 |
| updated_at | INTEGER | NOT NULL DEFAULT 0 | 更新时刻 |

## memory_entities

| 列 | 类型 | 约束 | 含义 |
|----|------|------|------|
| entry_name | TEXT | NOT NULL | 关联条目 |
| entity_norm | TEXT | NOT NULL | 归一化实体(精确匹配键) |
| entity_raw | TEXT | NOT NULL DEFAULT '' | 原始实体文本 |
| — | — | PRIMARY KEY (entry_name, entity_norm) | 复合主键 |

索引:`idx_memory_entities_norm ON (entity_norm)`。

## memory_curation_lease

单行(`CHECK id = 1`),`holder` / `expires_at` / `heartbeat_at`,跨进程 curation 选主。
建链时 `INSERT OR IGNORE` 一行种子。

## 迁移链(engram 独立)

engram store 从空库直接建到记忆终态。保留 up/down 对(源 v7/v7Down、v8/v8Down 的记忆部分),
在 engram 内 renumber 为 v1/v2(或合并为单版本终态,实现时择一,以能通过既有 migrate 测试为准)。
**验收**:平移的 `migrations_test.go` 记忆相关用例全绿。

## Go 侧实体(公开类型)

`memory` 包对外类型保持同形(FR-013):`Entry`、`Result`(检索结果项)、`EntryStore`、
`VectorStore`、`Embedder`、`Retriever`、`Budgets`、`UsageLogger`、`Snapshot`/`Loader`,
以及错误类型 `ErrMemoryTooLarge`/`ErrTriggerInvalid`/`ErrPinnedBudgetExceeded`。
字段与方法签名不变,仅包路径由 `internal/memory` 变为 `github.com/wallfacers/engram/memory`。
