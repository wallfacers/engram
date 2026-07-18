# Phase 0 Research: MCP Server 适配层

本特性是引擎之上的**薄适配层**,技术未知点集中在三处:MCP Go 库选型、namespace 隔离的落地方式、降级如何如实上报。逐条定稿如下。

## R1. MCP Go 库选型 —— 决定:官方 `github.com/modelcontextprotocol/go-sdk` v1.5.0

**Decision**:采用官方 Go SDK(`modelcontextprotocol/go-sdk`),当前稳定版 **v1.5.0**(2026-04-07 发布,MCP 项目 + Google 联合维护,spec-complete 到 MCP 2025-11-25)。仅用其 **stdio 传输**。

**Rationale**:
- **依赖可审计 / 长期维护**(宪法「依赖最小化」+「契约优先」):官方 + Google 维护,1443 importers,不是单人项目,弃坑风险低。社区替代 `mark3labs/mcp-go` 生态更大但单人维护、且其强项 HTTP/SSE 我们 MVP 不需要。
- **stdio-first**:官方 SDK 聚焦 stdio/command 传输,正是本地 Agent 接入方式(宪法「本地优先」)。
- **reflection 生成 inputSchema**:从 Go struct(带 `jsonschema` tag)自动生成工具输入 schema,~6 个工具的样板量最小,直接满足 FR-003(每工具带机器可读 schema)。
- **错误语义对齐**:v1.5.0 起"输入校验以 `CallToolResult` 返回而非 JSON-RPC 协议错误",正好承接 FR-019(引擎结构化错误→工具调用错误,保留可诊断信息)。

**最小 stdio server 骨架**(来自官方 README):
```go
server := mcp.NewServer(&mcp.Implementation{Name: "engram", Version: "v0.1.0"}, nil)
mcp.AddTool(server, &mcp.Tool{Name: "memory_write", Description: "..."}, writeHandler)
server.Run(context.Background(), &mcp.StdioTransport{}) // 跑到客户端断开
```

**Alternatives considered**:
- `mark3labs/mcp-go`:显式 option 建 schema,生态/教程多,但单人维护 + 我们不需要它的 HTTP 优势。留作备选。
- 手写 JSON-RPC over stdio:MCP stdio 本质是 newline-delimited JSON-RPC 2.0,标准库可实现,但要自己处理协议版本协商、通知 vs 请求、错误码规范、未来 spec 演进——为省一个可审计依赖而自担协议维护,不划算。否决。

**待验证(plan→impl 门禁)**:加入 SDK 后 `CGO_ENABLED=0 go build ./...` 必须仍通过(宪法「无 CGO」)。官方 SDK 基于 `encoding/json`/`jsonschema`,预期纯 Go;在 T-setup 任务里以 `go mod graph` + CGO=0 构建实测确认,若引入 CGO 依赖则回退 mark3labs 或手写。

## R2. namespace 隔离落地 —— 决定:适配层维护 `namespace → 独立引擎 store` 注册表

**Decision**:namespace 隔离**完全在适配层实现**。一个 namespace 映射到 `dataDir/<ns>.db` 一份独立 SQLite 文件,由适配层的 **Registry** 惰性 `store.Open` 并装配该 namespace 的引擎单元(EntryStore/VectorStore/Embedder/Retriever/Pipeline)。引擎**不感知 namespace,store schema 一行不改**。

**Rationale**:
- `store.Open(ctx, store.Options{DSN})` 已支持按任意路径开库(见 `store/sqlite.go`),`modernc.org/sqlite` 纯 Go、每库 `SetMaxOpenConns(1)` 自带写串行化。一个 namespace 一个文件是最自然、零引擎改动的隔离(守住 FR-011/FR-021、宪法 II/III)。
- 装配范例已存在:`cmd/locomo-bench` 每会话 `store.Open` 后 `NewEntryStore/NewVectorStore/NewEmbedder/NewRetriever`,直接复用这套装配即得每 namespace 引擎单元。
- 物理隔离(独立文件)比"单库加 namespace 列"更强:跨 namespace 查询在存储层就不可能发生(SC-008 零泄漏天然成立),也无需给引擎加迁移 v3。

**资源有界(FR-013)**:Registry 用 LRU 缓存已打开的 namespace handle,上限可配(默认 64);超限时关闭并淘汰最久未用者(其 `store.Close()`)。每 handle = 1 个 SQLite 连接,故句柄数有界。

**namespace 校验(FR-012 / SC-009,安全面)**:合法 namespace 标识 MUST 匹配保守白名单(如 `^[A-Za-z0-9._-]{1,64}$` 且不等于 `.`/`..`);拒绝含路径分隔符、`..`、绝对路径者。校验通过后才拼 `filepath.Join(dataDir, ns+".db")`,并二次断言结果仍在 dataDir 之内(defense-in-depth)。空/缺省 → 默认 namespace `default`。

**Alternatives considered**:
- 引擎 store 加 namespace 列 + 迁移 v3:违反 FR-021(改 schema)、破坏 001 已对拍行为、破坏 constitution「单一存储真相」需重解释。否决。
- 单库多 attach:复杂度高、无收益。否决。

## R3. 降级如何如实上报 —— 决定:结构化(是否配 embedding)标注,不做逐调用探测

**Decision**:检索响应的降级标注**基于结构化事实**——适配层知道是否装配了 embedding client(`embClient == nil` ⟺ 语义臂结构性关闭)。响应据此标注 `degraded.semantic = true` 且 `reason = "no embedding endpoint configured (offline mode)"`。**不**尝试逐 query 探测"已配置端点是否本次调用失败"。

**Rationale**:
- 引擎 `Retriever.Search` **有意静默降级**:`Result` 结构不含"哪些信号生效",`vectorRanks` 端点失败时静默返回空 map(见 `retriever.go:85-135`)——这正是宪法 V「优雅降级,不整体报错」的体现。
- 要暴露"逐调用某端点是否失败"必须改引擎在 Result/Search 里回传信号状态 → 违反 FR-021(改引擎公开面)。故适配层只上报它**能诚实知道**的结构性事实:有没有配 embedding。
- 这已覆盖 FR-010/FR-015/SC-006 的主场景——"端点未配置/离线"(US2 的核心)。已配置端点的偶发抖动,与引擎今日行为一致地静默降级,不谎称成功也不谎称失败。

**如实性边界(写入 quickstart/契约)**:标注语义为"语义臂是否被启用",非"本次是否命中语义"。这是诚实且可机器验证的口径(SC-006:embClient 为 nil 时,有 FTS/实体命中即返回非空且标注 semantic=false,比例 100%)。

## R4. 可选 LLM 抽取(US4)—— 决定:provider 配齐才装配 pipeline,工具按此显隐

**Decision**:仅当启动配置提供了可用 LLM provider(复用引擎 `provider` 抽象 + `pipeline.New`)时,Registry 才为每 namespace 装配 `pipeline.Pipeline`,且 `memory_ingest` 工具才注册进 `tools/list`(FR-017)。未配置时 `pipeline.New` 返回 nil(引擎既有 inert 语义),工具不注册。

**Rationale**:`pipeline.Config` 需要 `ModelCaller`(provider 驱动);`pipeline.New` 在 `Call==nil` 时返回 nil(inert)。适配层据"provider 是否配置"决定注册与否,天然实现"能力按依赖显隐",保 P1/P2 纯离线不被 LLM 依赖污染(宪法 I)。抽取 prompt/管线复用引擎既有实现,适配层不重写。

## R5. 项目布局 —— 决定:`cmd/engram-mcp/`(薄 main)+ 顶层 `mcpserver/`(可测适配包)

**Decision**:
- `cmd/engram-mcp/main.go`:薄入口——解析配置(flag/env)、构建 Registry、构建 SDK server、`Run` stdio。
- `mcpserver/`(顶层包):适配逻辑,可单测/契约测——Registry、工具 handler、namespace 校验、配置、降级标注。命名 `mcpserver` 避免与 SDK 的 `mcp` import 标识冲突。

**Rationale**:宪法 II 要求适配层薄且引擎无耦合;把逻辑放进可测顶层包(而非全塞 main)满足「测试先行 / 契约测试」。与既有布局(顶层 `memory/`、`cmd/locomo-bench/`)一致。

## R6. 测试策略 —— 决定:in-memory transport 契约测 + parity + CGO=0 门禁

**Decision**:
- **契约/集成测**:用 SDK 的进程内(in-memory pipe)client↔server 传输,在临时 dataDir 上端到端驱动工具,断言 US1 往返、US3 隔离、US2 降级标注、US4 工具显隐——全程无子进程、无外网、无真实客户端。
- **单测**:namespace 校验(路径逃逸拒绝表,覆盖 SC-009)、Registry 惰性开库/隔离/LRU 淘汰。
- **parity(SC-003)**:固定语料+query,断言 `memory_search` 结果与直接 `Retriever.Search` 逐条一致;复用 001 `testdata/parity` 思路。
- **门禁**:`CGO_ENABLED=0 go build ./...` 与 `go test ./...` 进 CI;引擎既有单测在适配层引入后仍全绿(SC-005)。

**宪法 IV(评测门禁)映射**:适配层只调用引擎既有检索/抽取/存储/embedding 路径、不改算法,故 LoCoMo 指标**按构造不变**。以 SC-003(MCP 检索 == 直接检索 parity)+ SC-005(引擎单测全绿、公开面/schema 逐字节不变)作为"未触动引擎行为"的机器证明,替代一次全量 bench 重跑;若实现意外改动引擎,parity/单测即失败拦截。

**SDK in-memory transport 的确切 API** 在 impl 阶段对照 v1.5.0 文档确认;若无进程内传输则退化为 pipe 子进程契约测。
