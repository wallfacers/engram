# Phase 1 Data Model: MCP Server 适配层

本特性**不定义新的持久化 schema**——所有记忆数据仍是引擎既有的 `memory_*` 表(每 namespace 一份独立库)。本文件定义适配层的**运行时实体、映射与不变量**。

## 持久化实体(承自引擎,不改)

- **Entry**(`memory.Entry`):记忆条目。字段:Name(唯一键)、Trigger、Content、Category、Pinned、Durability、CreatedAt、UpdatedAt、EventDate、FactSource、HitCount、LastUsedAt、CharCount、SourceSessionID 等。适配层**不新增字段**,只做 MCP 表示层序列化。
- **Result**(`memory.Result`):检索命中。字段:Name、Trigger、Content、EventDate、CreatedAt、Score。适配层据此构造 `memory_search` 输出;`snippet` 由 Content 派生。

## 运行时实体(适配层新增,非持久化)

### Namespace

- **含义**:一个隔离的记忆空间标识。
- **映射**:`namespace → dataDir/<namespace>.db`(一份独立引擎 store)。
- **校验规则(FR-012 / SC-009)**:
  - 缺省/空 → 默认 namespace `default`。
  - 合法标识 MUST 匹配 `^[A-Za-z0-9._-]{1,64}$` 且 ∉ {`.`, `..`}。
  - 拒绝含 `/`、`\`、路径分隔、绝对路径、`..` 者 → 结构化错误,不落盘、不越界。
  - 校验后 `filepath.Join(dataDir, ns+".db")`,并二次断言解析路径仍在 dataDir 内(defense-in-depth)。
- **状态**:惰性创建——首次访问某合法 namespace 时 `store.Open` 建库并跑迁移。

### NamespaceHandle(引擎单元装配)

- **含义**:一个已打开 namespace 的引擎装配单元。
- **字段**:`store *store.Store`、`entries *memory.EntryStore`、`vectors *memory.VectorStore`、`embedder *memory.Embedder`(embClient 为 nil 时仍可建,语义臂空转)、`retriever *memory.Retriever`、`pipe *pipeline.Pipeline`(仅 provider 配置时非 nil)。
- **装配来源**:复用 `cmd/locomo-bench` 既有装配序列(`store.Open`→`NewEntryStore`/`NewVectorStore`/`NewEmbedder`/`NewRetriever`[/`pipeline.New`])。
- **生命周期**:被 Registry 持有;LRU 淘汰时 `store.Close()`。

### Registry

- **含义**:`namespace → NamespaceHandle` 的并发安全注册表。
- **字段**:`dataDir string`、`embClient embedding.Client`(共享,可 nil)、`llmCaller pipeline.ModelCaller`(可 nil)、`max int`(LRU 上限,默认 64)、`mu`、`lru`。
- **不变量**:
  - **隔离(FR-010 / SC-008)**:不同 namespace 的 handle 持有不同库文件,跨 namespace 读写在存储层不可能发生。
  - **有界(FR-013)**:同时打开的 handle 数 ≤ max;超限关闭并淘汰最久未用者。
  - **惰性**:未访问的 namespace 不占资源。

### ServerConfig

- **含义**:启动配置,决定装配与工具显隐。
- **字段**:`DataDir`(必填,记忆库根目录)、`EmbedBaseURL`/`EmbedModel`/`EmbedAPIKey`(可选;全空 → 纯离线,embClient=nil)、`LLMBaseURL`/`LLMModel`/`LLMAPIKey`/`LLMProvider`(可选;配齐 → 暴露 `memory_ingest`)、`MaxOpenNamespaces`(可选,默认 64)。
- **来源**:flag + 环境变量;密钥只经环境变量,不要求写入仓库内被追踪文件(FR-018/FR-020)。

## 工具 I/O 形状(契约详见 `contracts/mcp-tools.md`)

| 工具 | 输入(除 `namespace?` 外) | 输出 |
|------|--------------------------|------|
| `memory_write` | name*, content*, trigger?, category?, pinned? | {name, created/updated} |
| `memory_search` | query*, limit? | {results:[{name,trigger,snippet,content,score,event_date,created_at}], degraded:{semantic,reason}} |
| `memory_list` | — | {entries:[Entry...]} |
| `memory_get` | name* | {entry} 或 not-found |
| `memory_delete` | name* | {deleted:bool} |
| `memory_ingest`(P4,可选) | messages:[{role,text}]* | {extracted_count, entries?} |

`*`=必填。所有工具接受可选 `namespace`(缺省 `default`)。

## 关键不变量汇总(可测)

1. **引擎不变**(SC-005):引擎公开 API 与 store schema 逐字节不变;引擎既有单测全绿。
2. **检索 parity**(SC-003):同 namespace 同输入下 `memory_search` 排序 == 直接 `Retriever.Search`。
3. **隔离零泄漏**(SC-008):任意两 namespace 互不出现对方条目。
4. **路径不逃逸**(SC-009):非法 namespace 100% 拒绝,dataDir 外读写=0。
5. **降级诚实**(SC-006):embClient=nil 时,有 FTS/实体命中即返回非空且 `degraded.semantic=true`。
6. **密钥零泄漏**(SC-007):日志/响应/产物中密钥出现=0。
