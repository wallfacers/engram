# Quickstart: engram MCP server(构建、配置、验证)

面向想把 engram 当记忆后端接入 MCP 客户端的使用者,以及本特性的验证者。

## 前提

- Go 1.22+,无需 C 工具链(纯 Go,`modernc.org/sqlite` + 纯 Go MCP SDK)
- P1/P2 路径(写/检索/列/查/删 + 降级检索)**无需外网、无需任何端点**
- 语义检索(增强项)需一个 embedding 端点(如本地 Ollama);抽取工具(`memory_ingest`)需一个 LLM provider——二者均可选

## 1. 构建(离线、纯净)

```bash
cd engram
CGO_ENABLED=0 go build ./...        # 引擎 + 新增 cmd/engram-mcp,须无 CGO、无缺依赖
```

**期望**:构建成功。CGO=0 通过即证 SDK 未引入 C 依赖(宪法「无 CGO」门禁)。

## 2. 纯离线启动(US2)

```bash
./engram-mcp --data-dir ~/.engram/memory
```

**期望**:server 起于 stdio,不因无 embedding/LLM 端点而退出。此模式暴露 5 个工具(write/search/list/get/delete);`memory_search` 走关键词+实体,响应标注 `degraded.semantic=true`。

## 3. 接入 MCP 客户端(US1)

在客户端(如 Claude Code)的 MCP 配置中注册:

```json
{
  "mcpServers": {
    "engram": {
      "command": "/abs/path/to/engram-mcp",
      "args": ["--data-dir", "/home/you/.engram/memory"]
    }
  }
}
```

之后 Agent 即可调用 `memory_write` / `memory_search` / `memory_list` / `memory_get` / `memory_delete`。往返验证:写一条 → 检索到它 → 列表可见 → 删除 → 再检索消失。

## 4. 启用语义检索(可选增强)

```bash
export ENGRAM_EMBED_BASE_URL=http://127.0.0.1:11434/v1   # 本地 Ollama
export ENGRAM_EMBED_MODEL=qwen3-embedding:0.6b
./engram-mcp --data-dir ~/.engram/memory
```

**期望**:`memory_search` 三路融合,响应 `degraded.semantic=false`。密钥(如需)只经环境变量,绝不写入被追踪文件(FR-018/FR-020)。

## 5. 启用抽取工具(可选,US4)

```bash
export ENGRAM_LLM_BASE_URL=...   ENGRAM_LLM_MODEL=...   ENGRAM_LLM_API_KEY=...
export ENGRAM_LLM_PROVIDER=openai   # 或 anthropic
./engram-mcp --data-dir ~/.engram/memory
```

**期望**:`tools/list` 多出 `memory_ingest`;喂一段对话,抽出的事实入库、可被检索。未配置 LLM 时该工具不出现(其余不受影响)。

## 6. 多 namespace(US3)

调用任意工具时传 `namespace`(如 `"projectA"`)即隔离;缺省落 `default`。不同 namespace 落 `--data-dir` 下不同 `.db` 文件,互不泄漏。非法 namespace(含 `..`/路径分隔)被拒。

## 验收对照

| 步骤 | 对应 SC / FR | 门禁性质 |
|------|-------------|----------|
| 1 CGO=0 构建 | 宪法「无 CGO」 | 硬门禁(CI) |
| 2 离线启动 | SC-002, FR-014 | 硬门禁 |
| 3 往返 | SC-001, US1 | 硬门禁 |
| 检索 parity | SC-003 | 硬门禁(CI) |
| 降级标注 | SC-006, FR-010 | 硬门禁 |
| 6 隔离/路径 | SC-008, SC-009 | 硬门禁 |
| 引擎不变 | SC-005 | 硬门禁(引擎单测全绿) |
| 密钥零泄漏 | SC-007 | 硬门禁(扫描) |

## 测试(验证者)

```bash
CGO_ENABLED=0 go test ./...              # 引擎既有单测 + mcpserver 契约/单测全绿
go test ./mcpserver -run Parity          # SC-003:MCP 检索 == 直接检索
go test ./mcpserver -run Isolation       # SC-008:跨 namespace 零泄漏
go test ./mcpserver -run Namespace       # SC-009:路径逃逸拒绝表
```

契约测用 SDK 进程内 client↔server 传输,无子进程、无外网、无真实客户端。
