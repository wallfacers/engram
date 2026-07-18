# Phase 1 Contract: MCP 工具面

**说明**:本特性对外契约是 MCP server 通过 `tools/list` 暴露的**工具集**(名称 + 描述 + 输入 schema)与其输出形状、错误映射。无 REST/GraphQL。契约稳定性 **v0**;破坏性变更须走后续 spec + 版本 MAJOR(宪法 III)。

**协议**:MCP over stdio(JSON-RPC 2.0),官方 SDK v1.5.0。server 声明 `tools` 能力;工具输入 schema 由 Go 输入 struct 经 reflection 生成(`jsonschema` tag)。

## 通用约定

- **namespace 参数**:除非另注,每个工具输入均含可选字段 `namespace`(string)。缺省/空 → `default`。非法(路径逃逸)→ 工具调用错误(见错误映射)。
- **成功返回**:结构化 JSON(经 SDK 的 structured content / `CallToolResult`),字段名 snake_case。
- **错误返回**:引擎结构化错误映射为 `CallToolResult` 的错误结果(isError),保留可诊断信息;非协议级 JSON-RPC error(对齐 SDK v1.5.0 输入校验语义,FR-019)。

## 工具清单(P1 核心 5 + P4 可选 1)

### 1. `memory_write`(US1 / FR-005)

写入或更新一条记忆(同 namespace 内 name 唯一,重名=更新,等价引擎 `EntryStore.Upsert`)。

**输入**:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| namespace | string | 否 | 目标空间,缺省 default |
| name | string | **是** | 唯一键/标题 |
| content | string | **是** | 记忆正文 |
| trigger | string | 否 | 触发线索 |
| category | string | 否 | 分类 |
| pinned | bool | 否 | 是否常驻 |

**输出**:`{ "name": string, "written": true }`

**错误**:内容超单条字符预算 → isError,消息含 `limit`/`actual`(承 `memory.ErrMemoryTooLarge`,FR-014);非法 namespace → isError。

### 2. `memory_search`(US1/US2 / FR-006, FR-010, FR-015)

三路混合检索,返回按相关性排序的命中。

**输入**:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| namespace | string | 否 | 缺省 default |
| query | string | **是** | 查询文本 |
| limit | int | 否 | 上限,缺省引擎默认(8),必须 > 0 |

**输出**:
```json
{
  "results": [
    {"name": "...", "trigger": "...", "snippet": "...", "content": "...",
     "score": 0.123, "event_date": "2024-05-01T00:00:00Z|null", "created_at": "..."}
  ],
  "degraded": {"semantic": true, "reason": "no embedding endpoint configured (offline mode)"}
}
```
- 排序 MUST 与直接 `Retriever.Search(ctx, query, limit)` 逐条一致(SC-003)。
- `degraded.semantic` = 该 server 是否**未装配** embedding client(结构化诚实口径,research R3);`false` 表示语义臂已启用。空库/无命中 → `results: []` 且成功(非错误)。

### 3. `memory_list`(US1 / FR-007)

列出 namespace 内全部记忆(等价 `EntryStore.List`)。

**输入**:`{ namespace?: string }`
**输出**:`{ "entries": [ {name, trigger, content, category, pinned, created_at, updated_at, event_date, ...} ] }`(引擎 Entry 字段,不丢失)

### 4. `memory_get`(US1 / FR-007)

按 name 取单条(等价 `EntryStore.GetByName`)。

**输入**:`{ namespace?: string, name: string(必填) }`
**输出**:`{ "entry": {…Entry字段…} }`;未找到 → isError,消息承 `store.ErrNotFound` 语义。

### 5. `memory_delete`(US1 / FR-007)

按 name 删除(等价 `EntryStore.Delete`,作用域限本 namespace)。

**输入**:`{ namespace?: string, name: string(必填) }`
**输出**:`{ "deleted": true }`;未找到 → `{ "deleted": false }` 或 isError(承引擎 Delete 语义,impl 阶段对齐)。

### 6. `memory_ingest`(US4 / FR-017)—— 仅当配置 LLM provider 时出现

把一段对话轮次经引擎抽取管线抽为事实并入库。

**输入**:
| 字段 | 类型 | 必填 | 说明 |
|------|------|------|------|
| namespace | string | 否 | 缺省 default |
| messages | array | **是** | `[{role: "user"|"assistant", text: string}]` |

**输出**:`{ "extracted_count": int, "entries": [ {name, content} ] }`
**显隐**:未配置 provider 时,本工具 MUST NOT 出现在 `tools/list`;其余 5 工具不受影响(FR-017)。

## 契约级验收

- **C-1**:标准 MCP 客户端 `tools/list` 得到上述工具(离线态 5 个 / 配 LLM 后 6 个),每个含非空 description 与合法 inputSchema(SC-004)。
- **C-2**:`memory_write`→`memory_search`/`memory_get`/`memory_list` 往返数据一致(US1)。
- **C-3**:`memory_search` 排序 == 直接引擎检索(SC-003)。
- **C-4**:跨 namespace 零泄漏(SC-008);非法 namespace 全拒(SC-009)。
- **C-5**:离线(无 embedding)时 search 返回非空 + `degraded.semantic=true`(SC-006)。
- **C-6**:引擎公开面/schema 前后不变——引擎既有单测在适配层引入后全绿(SC-005)。

## 非契约(明确不做)

跨 namespace 检索/合并、HTTP/SSE 传输、后台 curation 工具、Mem0 兼容 HTTP API、namespace 级鉴权——均不在本契约(FR-022),留后续 spec。
