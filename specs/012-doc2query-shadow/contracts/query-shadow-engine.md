# 契约：US1 引擎 `#query` 伪查询影子（冻结签名，照抄）

**范围**：仅 `store/` + `memory/`。US1 提交 `git diff --name-only` 只含这两目录。**不改** v1-v4 迁移、不改 011 `#alias` 语义。

## S1. Migration v5（`store/migrations.go`）

```go
var v5FactQueries = []string{
	`CREATE TABLE IF NOT EXISTS memory_fact_queries (
		entry_name TEXT NOT NULL,
		query      TEXT NOT NULL,
		PRIMARY KEY (entry_name, query)
	)`,
}

var v5FactQueriesDown = []string{
	`DROP TABLE IF EXISTS memory_fact_queries`,
}
```
在 `migrationsByVersion` 末尾追加 `{Version: 5, Up: v5FactQueries, Down: v5FactQueriesDown}`。**不动** v1-v4。（可选：在 `applyMigration` 加 `if m.Version == 5 { slog.Info(...) }`，非必需。）

## S2. EntryStore 访问器（`memory/entrystore.go`，仿 `PutAliases`）

```go
// PutFactQueries replaces the pseudo-queries for a fact (doc2query shadow source).
// Blank/dup (case-insensitive, whitespace-collapsed) queries are dropped.
func (s *EntryStore) PutFactQueries(ctx context.Context, name string, queries []string) error

// FactQueries returns a fact's stored pseudo-queries, ordered by query text.
func (s *EntryStore) FactQueries(ctx context.Context, name string) ([]string, error)

// FactQueryEntryNames returns the distinct entry_name values that have >=1 pseudo-query.
func (s *EntryStore) FactQueryEntryNames(ctx context.Context) ([]string, error)
```
`PutFactQueries` 事务内 `DELETE ... WHERE entry_name=?` 后 `INSERT OR IGNORE`，去重规则逐字仿 `PutAliases`（`strings.Join(strings.Fields(raw)," ")` 归一、`ToLower` 去重）。

## S3. Embedder（`memory/embedder.go`）

```go
const queryShadowSuffix = "#query"

// QueryShadowName returns the vector-row name reserved for a fact's pseudo-queries.
func QueryShadowName(factName string) string { return factName + queryShadowSuffix }

// resolveQueryShadow reports whether name is a #query shadow and its source fact.
func resolveQueryShadow(name string) (source string, isShadow bool)
```

**`resolveShadow` 泛化（retriever 折叠所依赖，关键）**：改为「识别任一影子后缀」——先试 `resolveQueryShadow`，再试既有 alias 后缀；任一命中即 `isShadow=true` 返回 source。retriever 调用点不变、行为自动扩展到 `#query`。

**`embedOne` 新增 `#query` 分支（置于 `#alias` 分支之前）**：
```go
if source, isShadow := resolveQueryShadow(name); isShadow {
	entry, err := e.entries.GetByName(ctx, source)
	if err != nil { return nil } // source gone: silent skip
	queries, err := e.entries.FactQueries(ctx, source)
	if err != nil { return err }
	text := queryEmbedText(queries) // strings.Join(queries, "\n"); 原样,不丢词
	if text == "" { return nil }
	vecs, err := e.client.Embed(ctx, []string{text})
	if err != nil { return err }
	if len(vecs) != 1 { return nil }
	return e.vectors.Put(ctx, name, e.client.Model(), vecs[0], time.Now())
}
```
`queryEmbedText(queries []string) string` = `strings.Join(nonEmpty(queries), "\n")`（**无** `aliasEmbedText` 的丢词滤器）。

```go
// QueryShadowNames returns every #query shadow row implied by memory_fact_queries.
func (e *Embedder) QueryShadowNames(ctx context.Context) ([]string, error)
```
`Backfill` 把缺失的 `#query` 影子纳入 re-embed（与既有 `AliasShadowNames` 并列）。

## S4. Retriever（`memory/retriever.go`）

**源码零改**（`vectorRankContext` 的 max-pool 归并已内容无关，经泛化后的 `resolveShadow` 自动折叠 `#query`）。仅在测试中断言其对 `#query` 生效。

## 不变量（测试须锁死）
- 无 `memory_fact_queries` 行 → 无 `#query` 影子 → `!hasShadows` 快路径逐字节等于现状（SC-001）。
- `#query` 影子命中经 max-pool 使源 fact semantic 升排名；同源 text+#query 双命中→结果一条、semantic 一票（SC-002）。
- 任何 `#query` 影子 name 不进 ranks/最终结果（SC-002）。
- client nil / 孤儿影子（源 fact 缺失）/ 空 queries → 不 panic、per-signal 降级。
- 与 011 `#alias` 共存：一 fact 同时有 alias+query 两影子 → 都折回源、max-pool 取最优、结果一票。
