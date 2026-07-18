---
description: "Task list for 002-mcp-server implementation"
---

# Tasks: MCP Server 适配层

**Input**: Design documents from `/specs/002-mcp-server/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/mcp-tools.md, quickstart.md

**Tests**: 本特性**包含测试任务**——宪法强制"测试先行 + 契约测试",且 spec 把 parity(SC-003)/隔离(SC-008)/路径(SC-009)/降级(SC-006)列为硬门禁。测试任务与实现任务成对出现,测试在前。

**Organization**: 按用户故事分组,每组可独立实现与验证。P1(US1)即可交付 MVP。

## Path Conventions

engram 单仓库布局:新增二进制入 `cmd/engram-mcp/`,适配逻辑入顶层可测包 `mcpserver/`。引擎包(`memory/` `embedding/` `provider/` `store/`)**本特性不改**。

## 红线约束(每个任务都受约束)

- **不改引擎**:MUST NOT 修改 `memory/` `embedding/` `provider/` `store/` `internal/` 的任何 `.go`(FR-021/SC-005)。若确需引擎新增最小公开入口,STOP 并上报为对 001 契约的增量,不得在适配层绕过引擎。
- **无 CGO**:任何改动后 `CGO_ENABLED=0 go build ./...` MUST 通过。
- **密钥只经 env**:MUST NOT 把密钥写入任何被 git 追踪的文件、日志或工具响应。
- **契约冻结**:工具名称/输入 schema/输出形状以 `contracts/mcp-tools.md` 为准,不得擅改。

---

## Phase 1: Setup(共享基础)

**Purpose**: 引入 SDK 依赖、建包骨架,确保编译与无 CGO。

- [ ] T001 在 `go.mod` 添加依赖 `github.com/modelcontextprotocol/go-sdk` v1.5.0,运行 `go mod tidy`,并验证 `CGO_ENABLED=0 go build ./...` 通过(证 SDK 未引入 C 依赖,宪法「无 CGO」门禁)。若引入 CGO 依赖则 STOP 上报(备选:mark3labs/mcp-go 或手写,见 research R1)。产物:`go.mod`、`go.sum`。
- [ ] T002 [P] 建包骨架:`mcpserver/doc.go`(package 注释,声明"引擎无耦合的 MCP 适配层")与 `cmd/engram-mcp/main.go` 最小可编译 stub(解析 `--data-dir` flag、打印版本即退出)。产物:`mcpserver/doc.go`、`cmd/engram-mcp/main.go`。

---

## Phase 2: Foundational(阻塞所有用户故事)

**Purpose**: 配置、namespace 校验、Registry、server 构建骨架——所有工具都经此路径。

**⚠️ 完成本阶段后才能开始任何用户故事。**

- [ ] T003 实现 `ServerConfig` 与加载(flag + env)于 `mcpserver/config.go`:必填 `DataDir`;可选 `EmbedBaseURL/EmbedModel/EmbedAPIKey`、`LLMBaseURL/LLMModel/LLMAPIKey/LLMProvider`;`MaxOpenNamespaces` 默认 64;密钥只从 env 读。产物:`mcpserver/config.go`。
- [ ] T004 [P] 写**先行失败**的 namespace 校验单测于 `mcpserver/namespace_test.go`:接受表(`default`、`projectA`、`a.b_c-1`)、拒绝表(空→default、`..`、`.`、含 `/`、含 `\`、绝对路径、长度>64、非白名单字符),覆盖 SC-009。产物:`mcpserver/namespace_test.go`。
- [ ] T005 实现 namespace 校验于 `mcpserver/namespace.go`:白名单 `^[A-Za-z0-9._-]{1,64}$` 且 ∉{`.`,`..`},空→`default`;通过后 `filepath.Join(dataDir, ns+".db")` 并二次断言解析路径仍在 dataDir 内。使 T004 通过。产物:`mcpserver/namespace.go`。
- [ ] T006 [P] 写**先行失败**的 Registry 单测于 `mcpserver/registry_test.go`:`Get("default")` 惰性建库并返回含非 nil `retriever/entries` 的 handle;重复 `Get` 幂等复用;`Close()` 关闭全部。产物:`mcpserver/registry_test.go`。
- [ ] T007 实现 Registry 于 `mcpserver/registry.go`:`namespace→NamespaceHandle` 并发安全映射,惰性 `store.Open(DSN=<ns>.db)` 后按 `cmd/locomo-bench` 既有序列装配(`NewEntryStore/NewVectorStore/NewEmbedder/NewRetriever`;共享 `embClient` 可 nil;`pipe` 仅当 `llmCaller` 非 nil);`Get`/`Close`。使 T006 通过。产物:`mcpserver/registry.go`。
- [ ] T008 实现 MCP server 构建于 `mcpserver/server.go`:`mcp.NewServer` + 条件注册工具的框架 + `Run(&mcp.StdioTransport{})`;`cmd/engram-mcp/main.go` 改为加载 Config→建 Registry→建 server→Run。产物:`mcpserver/server.go`、`cmd/engram-mcp/main.go`。

---

## Phase 3: User Story 1 - 在 MCP 客户端里读写记忆(P1)🎯 MVP

**Goal**: 默认 namespace 上的 write/search/list/get/delete 五工具往返可用。

**Independent Test**: in-memory client 依次 write→search→get→list→delete→search-empty,数据往返正确。

- [ ] T009 [P] [US1] 写**先行失败**的契约测于 `mcpserver/tools_test.go`:用 SDK 进程内 client↔server 传输,在临时 dataDir 默认 ns 上跑 write→search→get→list→delete→search 空,断言各步结构化返回正确(US1 验收 1-4)。产物:`mcpserver/tools_test.go`。
- [ ] T010 [US1] 实现 `memory_write` handler 于 `mcpserver/tools.go`:构造 `memory.Entry` 调 `EntryStore.Upsert`;`ErrMemoryTooLarge`→isError 且消息含 `limit/actual`(FR-014);非法 namespace→isError。产物:`mcpserver/tools.go`。
- [ ] T011 [US1] 实现 `memory_search` handler 于 `mcpserver/tools.go`:调 `Retriever.Search(ctx,query,limit)`,映射 `Result`→输出(snippet 由 content 派生);**自身完整落实 `degraded` 字段**——依 Registry 的 `embClient==nil` 结构化设 `degraded.semantic` 与 `reason="no embedding endpoint configured (offline mode)"`(research R3 诚实口径)。产物:`mcpserver/tools.go`。
- [ ] T012 [US1] 实现 `memory_list`/`memory_get`/`memory_delete` handler 于 `mcpserver/tools.go`:分别调 `List/GetByName/Delete`;`GetByName` 未找到→承 `store.ErrNotFound`→isError。产物:`mcpserver/tools.go`。
- [ ] T013 [US1] 在 `mcpserver/server.go` 注册 5 个 CRUD 工具:各带非空 description 与输入 struct(`jsonschema` tag,含可选 `namespace`),schema 由 SDK reflection 生成(FR-003/SC-004);工具名/字段对齐 `contracts/mcp-tools.md`。产物:`mcpserver/server.go`、`mcpserver/tools.go`。
- [ ] T014 [P] [US1] 写检索 parity 测于 `mcpserver/parity_test.go`:固定语料+query,断言 `memory_search` 返回的排序 entry 序列与直接 `Retriever.Search` 逐条一致(SC-003)。产物:`mcpserver/parity_test.go`、`mcpserver/testdata/`(如需)。

**Checkpoint**: US1 独立可交付——MVP 达成。

---

## Phase 4: User Story 2 - 离线纯净启动与优雅降级(P2)

**Goal**: 无任何端点也能启动并提供 CRUD + 降级检索,如实标注降级。

**Independent Test**: 无 embed/LLM 配置启动,CRUD 全通,search 返回非空(有 FTS/实体命中时)且标注 `degraded.semantic=true`。

- [ ] T015 [P] [US2] 写**先行失败**的离线测于 `mcpserver/offline_test.go`:Config 无 embed/LLM 端点时 server 构建+启动成功;CRUD 工具全通;预置若干条记忆后 search 返回非空且 `degraded.semantic=true`(SC-002/SC-006)。产物:`mcpserver/offline_test.go`。
- [ ] T016 [US2] 验证并加固离线路径:确认 `embClient==nil` 时 Registry 装配的 retriever 走纯 FTS+实体、write/list/get/delete 完全不依赖任何端点;补齐 Registry 暴露 embClient 状态给 search 的接口(若 T011 尚未暴露)。使 T015 通过。产物:`mcpserver/registry.go`、必要时 `mcpserver/tools.go`。

**Checkpoint**: US1+US2 = 可交付的纯离线记忆适配层。

---

## Phase 5: User Story 3 - 多 namespace 隔离(P3)

**Goal**: 不同 namespace 记忆物理隔离、零泄漏;资源 LRU 有界;非法 namespace 全拒。

**Independent Test**: 向 A、B 各写,A 检索/列表只见 A、B 只见 B,删 A 不影响 B,缺省→default。

- [ ] T017 [P] [US3] 写**先行失败**的隔离测于 `mcpserver/isolation_test.go`:向 ns A、B 各写一条,断言 search/list/get 在 A 只见 A 的、B 只见 B 的;删 A 中条目不影响 B;省略 namespace 落 default 且不影响具名 ns(SC-008,US3 验收 1-4)。产物:`mcpserver/isolation_test.go`。
- [ ] T018 [P] [US3] 写**先行失败**的 LRU 淘汰测于 `mcpserver/registry_test.go`:`MaxOpenNamespaces=2` 时访问第 3 个 ns 触发关闭最久未用者;被淘汰 ns 的数据在重新 `Get` 后仍在(持久化未丢)(FR-013)。产物:`mcpserver/registry_test.go`。
- [ ] T019 [US3] 给 Registry 增加 LRU 有界与淘汰(淘汰时 `store.Close()`)于 `mcpserver/registry.go`,上限取 `Config.MaxOpenNamespaces`。使 T018 通过。产物:`mcpserver/registry.go`。
- [ ] T020 [P] [US3] 写 namespace 拒绝的工具层契约测于 `mcpserver/tools_test.go`:经工具传入含 `..`/路径分隔的 namespace→isError,且断言 dataDir 之外无文件被创建(SC-009 工具层闭环)。产物:`mcpserver/tools_test.go`。

**Checkpoint**: US1+US2+US3 = 多项目隔离、离线、有界的适配层。

---

## Phase 6: User Story 4 - 可选:从对话抽取记忆(P4)

**Goal**: 配 LLM provider 时暴露 `memory_ingest`,抽取入库;未配置则工具不出现。

**Independent Test**: 打桩 provider 配置下 `tools/list` 含 ingest 且抽取入库可检索;无 provider 下不含 ingest,其余工具正常。

- [ ] T021 [P] [US4] 写**先行失败**的抽取测于 `mcpserver/ingest_test.go`:(a) 配打桩 LLM provider 时 `tools/list` 含 `memory_ingest`,ingest 一段对话后抽出的事实入库并可 search 到;(b) 无 provider 时 `tools/list` 不含 `memory_ingest`,其余 5 工具正常(FR-017)。产物:`mcpserver/ingest_test.go`。
- [ ] T022 [US4] 由 provider 配置构建 `pipeline.ModelCaller`(复用 `provider` 包 + `cmd/locomo-bench` / `curation.NewProviderCaller` 的既有装配),在 Registry 中当 `llmCaller` 非 nil 时 `pipeline.New(...)` 装配每 ns 的 pipe。产物:`mcpserver/registry.go`、`mcpserver/config.go`。
- [ ] T023 [US4] 实现 `memory_ingest` handler(把 `messages` 转 `pipeline.Message` 调 Ingest,返回 `extracted_count/entries`)于 `mcpserver/tools.go`,并在 `mcpserver/server.go` 仅当 provider 配置时注册该工具。使 T021 通过。产物:`mcpserver/tools.go`、`mcpserver/server.go`。

**Checkpoint**: 四故事全达成。

---

## Phase 7: Polish & 横切

**Purpose**: 门禁、密钥安全、文档、终检。

- [ ] T024 [P] 加构建/测试门禁(对齐仓库既有 CI 风格;无则加 `Makefile` target):`CGO_ENABLED=0 go build ./...` + `go test ./...`(引擎既有单测须全绿 = SC-005)。产物:CI 配置或 `Makefile`。
- [ ] T025 [P] 写密钥零泄漏测于 `mcpserver/secrets_test.go`:配置一个 API key 后,断言其字符串不出现在 server 日志输出与任意工具响应中(SC-007/FR-020)。产物:`mcpserver/secrets_test.go`。
- [ ] T026 [P] `cmd/engram-mcp` 增补 `--help`、启动日志(不含密钥),并在 `README.md`(或 `docs/`)加"注册 engram MCP server"小节(含 quickstart 第 3 节的客户端配置样例)。产物:`cmd/engram-mcp/main.go`、`README.md`。
- [ ] T027 按 `quickstart.md` 端到端手验(离线启动 + 一次客户端往返),记录结果;在 plan.md 追加或新建简短收尾说明,复核宪法五项。产物:验证记录 + Constitution 终检备注。

---

## Dependencies & 执行顺序

- **Setup(T001-T002)** → **Foundational(T003-T008)** → 用户故事。
- **Foundational 阻塞一切**:T003(Config)→T005(namespace,依 T004 测)→T007(Registry,依 T006 测、依 T003/T005)→T008(server,依 T007)。
- **US1(T009-T014)** 依 Foundational。**US2(T015-T016)** 依 US1(改 search 输出)。**US3(T017-T020)** 依 Foundational+US1(工具经 Registry)。**US4(T021-T023)** 依 Foundational+US1(注册框架)。US2/US3/US4 彼此大体独立,按 P 序推进。
- **Polish(T024-T027)** 依全部故事。
- **测试先行**:每对内测试任务(T004/T006/T009/T014/T015/T017/T018/T020/T021/T025)先写且先失败,再由对应实现任务转绿。

## 并行机会

- Setup 内 T002 与 T001 收尾可并行。
- Foundational 内 T004(namespace 测)与 T006(registry 测)可并行起草。
- 各故事的"先行测试"任务标 [P](不同新文件):T009/T014/T015/T017/T018/T020/T021/T025 可并行起草。
- 同一文件的实现任务(多个 handler 都在 `tools.go`)**不可**并行——T010/T011/T012 顺序改同一文件。

## Implementation Strategy(MVP 优先)

1. **MVP = Setup + Foundational + US1(T001-T014)**:一个能在 MCP 客户端里读写默认 namespace 记忆、且检索与引擎对拍一致的 server。到此即可交付演示。
2. **加离线韧性 US2(T015-T016)**:落实"默认离线"招牌。
3. **加隔离 US3(T017-T020)**:多项目可用。
4. **加抽取 US4(T021-T023)**:配 LLM 才现身,记住整段对话。
5. **Polish(T024-T027)**:门禁 + 密钥扫描 + 文档 + 终检。

## 交付给外部 Agent 的验收锚点(SC 映射)

| 故事 | 硬验收 |
|------|--------|
| US1 | 往返(SC-001)+ parity(SC-003)+ 工具 schema 齐(SC-004) |
| US2 | 离线启动/CRUD 全通(SC-002)+ 降级如实(SC-006) |
| US3 | 跨 ns 零泄漏(SC-008)+ 路径不逃逸(SC-009) |
| US4 | ingest 显隐 + 抽取入库(FR-017) |
| 横切 | 引擎不变(SC-005)+ 密钥零泄漏(SC-007)+ CGO=0 |
