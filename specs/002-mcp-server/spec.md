# Feature Specification: MCP Server 适配层

**Feature Branch**: `002-mcp-server`

**Created**: 2026-07-18

**Status**: Draft

**Input**: User description: "MCP Server 适配层:把 engram 记忆引擎包装为 Model Context Protocol server,让任意 MCP 客户端(Claude Code/Desktop 等 Agent)通过标准 MCP 协议对 engram 做记忆读写。引擎与适配层分离,本地优先,stdio 传输。"(补充决策:本特性纳入多 namespace 隔离;纳入可选 LLM 抽取工具。)

## 背景与目标 *(mandatory)*

feature 001 已把记忆引擎抽离为自洽、可独立编译、可离线运行的 Go 库(`github.com/wallfacers/engram/memory` 等公开包)。但库只能被 Go 程序 `import`——普通 Agent 用户无法直接消费。

本特性是 **001 契约文档明确点名的"后续适配层"**(见 `specs/001-.../contracts/go-api-surface.md`:"namespace 隔离、MCP 兼容 API 等新契约留待后续契约 spec"),也是宪法原则 II「引擎与适配层分离」落地的第一个适配层:在**不改动引擎公开面、不改引擎 store schema** 的前提下,新增一个独立的 MCP server 可执行程序,把引擎的记忆读写能力通过 **Model Context Protocol** 标准协议暴露出去,使任意 MCP 客户端(Claude Code、Claude Desktop 及其他 MCP-capable Agent)能把 engram 当作记忆后端即插即用。

**性质**:纯适配(adapter),不重设计引擎、不改引擎算法、不动引擎公开 API。MCP server 是引擎之上的一层薄壳(thin shell),把 MCP 工具调用翻译成对引擎公开方法的调用,再把结果翻译回 MCP 响应。

**关键架构约束(namespace)**:本特性纳入多 namespace 隔离,但隔离**在适配层实现**——一个 namespace 对应一份独立的引擎记忆库(独立 store 实例),由适配层维护 `namespace → store` 的注册与生命周期。引擎本身**不感知 namespace、store schema 不加 namespace 列**,从而保住 001 已对拍的引擎行为与"不改引擎"根基(FR-016)。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 在 MCP 客户端里读写记忆(Priority: P1)

一个使用 Claude Code(或任意 MCP 客户端)的用户,在客户端配置中注册 engram MCP server(指向一个本地数据目录)。此后在对话中,Agent 可以调用 server 暴露的记忆工具:写入一条记忆、按查询检索相关记忆、列出/查看/删除记忆。用户无需写任何 Go 代码,只需一行配置即可让 engram 成为该 Agent 的持久记忆。未显式指定 namespace 时,操作落在默认 namespace,MVP 即可零配置演示往返。

**Why this priority**: 这是整个特性的存在理由——把"只能被 Go import 的库"变成"任意 Agent 一行配置就能用的记忆后端"。没有它,001 的引擎对终端用户不可达。它在默认 namespace 上即构成可独立演示、可交付的 MVP。

**Independent Test**: 用标准 MCP 客户端(或 MCP inspector / 一段最小 MCP 客户端脚本)连上 server,依次调用写入工具存一条记忆、检索工具按相关 query 取回、列表工具看到它、删除工具移除它、再检索确认已删除——全程无需触碰引擎源码、无需指定 namespace,往返数据正确即通过。

**Acceptance Scenarios**:

1. **Given** 一个已注册 engram MCP server 的 MCP 客户端与一个空的默认 namespace,**When** 客户端调用"写入记忆"工具存入一条记忆(名称 + 内容,不指定 namespace),**Then** 工具返回成功,且该记忆可被后续检索/列表读到。
2. **Given** 默认 namespace 中已有若干条记忆,**When** 客户端调用"检索记忆"工具并给出查询与期望条数上限,**Then** 工具返回按相关性排序、不超过上限的记忆列表,排序与直接调用引擎 `Retriever.Search` 一致。
3. **Given** 默认 namespace 中存在一条已知名称的记忆,**When** 客户端调用"查看记忆"/"删除记忆"工具,**Then** 分别返回该记忆的完整字段 / 删除成功,删除后再检索或查看该名称不再返回它。
4. **Given** MCP 客户端首次连接 server,**When** 客户端请求工具清单(MCP 标准 `tools/list`),**Then** server 返回一组带名称、描述、输入 schema 的记忆工具,客户端可据此正确构造调用。

---

### User Story 2 - 离线纯净启动与优雅降级(Priority: P2)

一个注重隐私/离线的用户,在没有外网、没有配置任何 LLM、embedding 端点可用或不可用的环境里启动 engram MCP server。server 必须能正常启动并提供记忆的写入、列表、查看、删除,以及检索(在语义信号不可用时,自动降级为关键词 + 实体检索),不因缺少外部端点而整体报错或拒绝启动。

**Why this priority**: 直接落实宪法原则 I「本地优先 / 默认离线」与原则 V「优雅降级」。这是 engram 相对通用记忆云服务的核心差异化;若 server 缺了端点就起不来,本地优先就是空话。它在 US1 之上加"离线韧性"这一独立可验证切面。

**Independent Test**: 在断网、未配置 embedding/LLM 端点的环境启动 server,执行 US1 的写入/列表/查看/删除全部成功;调用检索工具,server 返回基于关键词/实体的结果(而非报错),响应中如实标注语义信号已降级。

**Acceptance Scenarios**:

1. **Given** 无外网且未配置任何外部端点,**When** 启动 server,**Then** server 成功启动并对外提供工具清单,不 panic、不因缺端点退出。
2. **Given** server 在离线态运行,**When** 客户端调用写入/列表/查看/删除工具,**Then** 全部正常完成(这些能力本就不依赖外部端点)。
3. **Given** embedding 端点不可用,**When** 客户端调用检索工具,**Then** server 返回基于关键词 + 实体信号的降级结果,并在响应中如实告知语义臂不可用,而非整体失败。

---

### User Story 3 - 多 namespace 记忆隔离(Priority: P3)

一个在多个项目/上下文间切换的用户,希望不同项目的记忆彼此隔离——项目 A 的记忆不该在项目 B 的检索里冒出来。用户在调用记忆工具时可带一个 namespace 标识;同一个 server 进程为每个 namespace 维护一份独立的记忆空间。省略 namespace 时落在默认 namespace。

**Why this priority**: 隔离是"一个 Agent、多个上下文"的真实刚需,也是本特性明确纳入的能力。它在 P1 的工具面上增加一个正交维度(namespace 参数)与一条强不变量(跨 namespace 不泄漏),可独立验证,且不阻塞 P1 的默认-namespace MVP。

**Independent Test**: 向 namespace A 写入一条记忆、向 namespace B 写入另一条;在 A 里检索/列表只看到 A 的、在 B 里只看到 B 的;删除 A 中的条目不影响 B;省略 namespace 的调用只作用于默认 namespace。断言三者互不泄漏即通过。

**Acceptance Scenarios**:

1. **Given** 空的 namespace A 与 B,**When** 客户端向 A 写入记忆 x、向 B 写入记忆 y,**Then** 在 A 检索/列表只返回 x、在 B 只返回 y,x 与 y 互不出现在对方结果中。
2. **Given** A 与 B 各有记忆,**When** 客户端删除 A 中某条,**Then** B 的记忆不受影响。
3. **Given** 客户端调用工具时省略 namespace,**When** 执行写入/检索,**Then** 操作作用于默认 namespace,且不影响任何具名 namespace。
4. **Given** 客户端传入一个此前未使用过的 namespace 标识,**When** 首次写入,**Then** server 为其惰性创建独立记忆空间并完成写入,不影响其他 namespace。

---

### User Story 4 - 可选:从对话抽取记忆(Priority: P4)

当用户为 server 配置了一个 LLM provider(经引擎已内置的 provider 抽象)后,server 额外暴露一个"摄取"工具:客户端提交一段对话轮次(user/assistant 消息)与可选 namespace,server 调用引擎的抽取管线把其中的事实抽出并入库到对应 namespace。未配置 LLM 时,该工具不出现在工具清单中(能力按依赖存在与否显隐),其余工具不受影响。

**Why this priority**: 抽取是引擎已有能力(`memory/pipeline`),把它暴露成工具能让 Agent"记住整段对话"而非逐条手写,价值高但**依赖 LLM**,与"默认离线"张力最大,故列末位、做成按配置可选,保证 P1/P2 的纯离线 MVP 不被它拖累;且它需复用 namespace 维度,排在 US3 之后。

**Independent Test**: 配置一个(可打桩的)LLM provider,调用摄取工具喂入一段含明确事实的对话,断言抽出的事实作为记忆条目入库到指定 namespace、可被 US1/US3 的检索/列表读到;在未配置 provider 的 server 上,断言工具清单里不含该工具。

**Acceptance Scenarios**:

1. **Given** 已配置 LLM provider 的 server,**When** 客户端调用摄取工具提交一段对话与 namespace,**Then** 引擎抽取管线运行,产出的事实作为记忆入库到该 namespace 并可被检索。
2. **Given** 未配置 LLM provider 的 server,**When** 客户端请求工具清单,**Then** 清单中不包含摄取工具,且写入/检索/列表/查看/删除工具照常可用。

---

### Edge Cases

- **写入超限**:写入内容超过引擎的单条字符预算时,工具必须返回引擎既有的结构化拒绝(`ErrMemoryTooLarge` 语义:含限额与实际值),而非静默截断或通用错误。
- **同名写入**:在同一 namespace 内写入一个已存在名称的记忆时,行为必须与引擎 `Upsert` 语义一致(覆盖/更新既有条目),不产生重复条目。
- **检索空库 / 无命中**:对空 namespace 或无匹配的查询,检索工具返回空列表并成功,而非报错。
- **非法 namespace 标识**:namespace 标识含路径分隔符、`..`、绝对路径或其他可能逃逸配置数据目录的字符时,server 必须拒绝该调用并返回结构化错误,绝不允许在配置数据目录之外读写文件。
- **非法参数**:客户端传入缺失必填字段、类型不符、上限为负等非法输入时,server 返回 MCP 标准的工具调用错误(结构化、可读),不崩溃、不写入脏数据。
- **并发调用**:多个工具调用并发到达(含对同一 namespace)时,底层记忆库不产生数据竞争或损坏(依赖引擎/SQLite 既有并发保证);对不同 namespace 的并发操作互不阻塞。
- **客户端断连**:MCP 客户端在调用中途断开 stdio 连接时,server 干净地结束当前请求处理并可退出,不留下损坏的库文件。
- **数据目录不可写**:配置的数据目录不存在或不可写时,server 在启动阶段以清晰错误退出,而非启动后在首次写入时才隐晦失败。
- **namespace 数量增长**:大量不同 namespace 被访问时,server 对每 namespace 的 store 资源以有界方式管理(如按需打开/复用),不因 namespace 增多而无界耗尽句柄。

## Requirements *(mandatory)*

### Functional Requirements

**协议与传输**

- **FR-001**: server MUST 作为一个独立的可执行程序存在于 engram 仓库内(与引擎包分离的适配层),其构建不改动、不依赖引擎包之外的宿主代码。
- **FR-002**: server MUST 通过 stdio 传输实现 Model Context Protocol,能被标准 MCP 客户端发现并握手(initialize / tools 能力协商)。
- **FR-003**: server MUST 响应 MCP 标准的工具发现请求(`tools/list`),为每个记忆工具提供名称、人类可读描述与机器可读输入 schema(含可选 namespace 参数的说明)。
- **FR-004**: server MUST 把每个工具调用(`tools/call`)翻译为对引擎**现有公开方法**的调用,不在适配层内重新实现引擎算法或绕过引擎写/检索路径。

**记忆工具面(P1 核心)**

- **FR-005**: server MUST 暴露"写入记忆"工具,把客户端提供的记忆**名称(唯一键,必填)**与内容(及可选的触发/类别等引擎已有字段)经引擎写入路径持久化到目标 namespace,行为与直接调用引擎写入等价。名称必填源于引擎约束(`Entry.Name` 唯一非空、为 FTS/去重语义键,引擎 ID 生成器不对外);同名写入即更新(Upsert 语义)。
- **FR-006**: server MUST 暴露"检索记忆"工具,接受查询文本、返回条数上限与可选 namespace,返回引擎 `Retriever.Search` 产出的按相关性排序结果;相同输入与同一 namespace 下工具返回的排序与直接调用引擎一致。
- **FR-007**: server MUST 暴露"列出记忆""查看单条记忆""删除记忆"工具,语义分别等价于引擎的 List / GetByName / Delete,且作用范围限定在目标 namespace。
- **FR-008**: 所有工具的成功响应 MUST 以结构化、客户端可解析的形式返回记忆字段(名称、内容及引擎已有的相关元数据),不丢失引擎返回的字段信息。

**namespace 隔离(P3)**

- **FR-009**: 所有记忆工具 MUST 接受一个**可选** namespace 参数;省略时 MUST 落在单一确定的默认 namespace。
- **FR-010**: server MUST 保证不同 namespace 的记忆完全隔离——一个 namespace 的写入 MUST NOT 出现在另一 namespace 的检索、列表或查看结果中,删除亦不跨 namespace 影响。
- **FR-011**: server MUST 在适配层实现 namespace 隔离(每 namespace 独立引擎 store),MUST NOT 为此修改引擎的 store schema 或引擎公开 API;首次访问某 namespace 时惰性创建其记忆空间。
- **FR-012**: server MUST 校验 namespace 标识,拒绝任何可能逃逸配置数据目录的标识(路径分隔符、`..`、绝对路径等),防止越界读写。
- **FR-013**: server MUST 以有界方式管理各 namespace 的底层资源(句柄/连接),namespace 数量增长时不发生资源无界耗尽。

**离线与降级(P2)**

- **FR-014**: server MUST 能在无外网、未配置任何 embedding/LLM 端点的环境下成功启动并提供写入/列表/查看/删除及降级检索,不因缺少外部端点而拒绝启动或整体报错。
- **FR-015**: 当语义(向量)信号因端点缺失/不可用而无法参与检索时,server MUST 让检索降级为其余可用信号(关键词 + 实体),返回结果并在响应中如实标注降级,保持引擎既有的三路独立降级行为。
- **FR-016**: server MUST NOT 引入任何需要 C 工具链(CGO)或强制联网的启动期依赖,保持引擎的纯 Go / 默认离线特性。

**可选抽取(P4)**

- **FR-017**: 当且仅当配置了可用的 LLM provider 时,server MUST 额外暴露"摄取对话"工具,调用引擎抽取管线把对话中的事实抽出入库到目标 namespace;未配置时该工具 MUST 不出现在工具清单中,其余工具不受影响。

**配置与错误语义**

- **FR-018**: server MUST 通过启动配置(如数据目录路径、可选端点/模型/密钥)完成初始化,配置来源不得要求把密钥写入仓库内被追踪的文件。
- **FR-019**: server MUST 把引擎返回的结构化错误(如内容超限)映射为 MCP 标准的工具调用错误,保留可诊断信息(限额/实际值等),不降级为无信息的通用失败。
- **FR-020**: server MUST NOT 在日志、工具响应或运行产物中泄露配置的密钥。
- **FR-021**: 本特性 MUST NOT 修改引擎的公开 API(方法签名、字段、错误类型)与 store schema;适配层只消费引擎现有公开面。若发现引擎缺少适配所必需的最小公开入口,须显式记录为对 001 契约的增量,而非在适配层内绕过引擎。

**范围边界(非目标,写明以锁定 diff)**

- **FR-022**: 本特性范围**不含**:HTTP/SSE 远程传输(仅 stdio);后台 curation(dedup/judge/lease)的自动运行与暴露;Mem0 兼容 HTTP API;跨 namespace 的检索/合并;namespace 级鉴权/ACL;算法涨点。

### Key Entities *(include if feature involves data)*

- **记忆条目(Memory Entry)**:引擎已定义的 `Entry`——名称、触发、内容、类别、pinned、时间戳、来源等。适配层不新增字段,只做 MCP 表示层的序列化/反序列化。
- **namespace**:一个隔离的记忆空间标识。适配层把它映射到一份独立的引擎记忆库;引擎不感知它。默认 namespace 承接省略该参数的调用。
- **记忆工具(MCP Tool)**:一个对外暴露的操作单元,含名称、描述、输入 schema(含可选 namespace),一一对应引擎的一个公开能力(写/检索/列/查/删/摄取)。
- **检索结果(Retrieval Result)**:引擎 `Result` 的 MCP 表示——命中记忆及其相关性信息,附降级状态标注。
- **server 配置(Server Config)**:启动参数集合——数据目录路径、可选 embedding/LLM 端点与模型、可选密钥;决定哪些可选工具(如摄取)被暴露。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 一名从未接触 engram 源码的用户,仅凭一份配置(数据目录 + 客户端注册)即可在标准 MCP 客户端中让 Agent 完成"写入一条记忆并随后检索到它"的完整往返(默认 namespace,零 namespace 配置)。
- **SC-002**: 在断网、无任何外部端点配置的环境中,server 启动成功率 100%,且写入/列表/查看/删除工具调用成功率 100%。
- **SC-003**: 对同一组固定语料与查询、同一 namespace,经 MCP"检索记忆"工具取回的记忆排序,与直接调用引擎 `Retriever.Search` 逐条一致(适配层不改变检索结果)。
- **SC-004**: 100% 的记忆工具在工具清单中都带有非空描述与合法输入 schema,标准 MCP 客户端能据此零人工干预地构造合法调用。
- **SC-005**: 引擎公开 API 面与 store schema 在本特性前后逐字节不变(除非记录为 001 契约的显式增量),可由引擎既有单测在适配层引入后仍全绿机器验证。
- **SC-006**: 语义端点不可用时,检索工具返回非空(在有关键词/实体命中的前提下)且如实标注降级的比例为 100%,不出现整体失败。
- **SC-007**: 配置与运行产物中密钥出现次数为 0(可由对日志/响应/落盘产物的扫描验证)。
- **SC-008**: 跨 namespace 隔离零泄漏——在任意一对不同 namespace 间,一方写入的记忆在另一方的检索/列表/查看中出现的次数为 0。
- **SC-009**: 所有含路径逃逸字符的 namespace 标识 100% 被拒绝,配置数据目录之外的文件读写次数为 0(可由越界路径用例集验证)。

## Assumptions

- **传输默认 stdio**:MVP 仅实现 MCP 的 stdio 传输(本地 Agent 的标准接入方式,最贴合"本地优先");HTTP/SSE 远程传输留待后续特性。
- **namespace 在适配层隔离**:一个 namespace 对应一份独立引擎记忆库(独立 store 实例 / 独立 SQLite 文件),由适配层管理注册与生命周期;引擎不感知 namespace、schema 不变。多个项目靠 namespace 而非多进程隔离。
- **curation 默认不自动运行**:MVP 是薄读写适配层,不在 server 内自动跑后台整理(dedup/judge/lease);该能力可后续按需暴露。
- **抽取按依赖显隐**:摄取工具仅在配置了 LLM provider 时暴露,保证纯离线 MVP(P1/P2)不被 LLM 依赖污染。
- **复用引擎既有能力**:检索、写入、抽取、降级均直接调用 001 已交付并对拍验证过的引擎公开方法,适配层不重实现。
- **MCP 协议版本与 SDK**:面向当前主流 MCP 协议版本(JSON-RPC 2.0 之上的 initialize / tools 能力);具体协议版本与是否采用官方 Go MCP SDK 属实现细节,在 plan 阶段定。
- **无鉴权**:stdio 本地进程模型下,信任边界即本机进程;MVP 不做工具级或 namespace 级鉴权/ACL。

## Dependencies

- feature 001 交付的 engram 引擎公开包(memory / embedding / provider / store),及其对拍验证过的检索/写入/抽取/降级行为。
- 一个支持 MCP 的客户端用于端到端验证(如 MCP inspector 或最小 MCP 客户端脚本);单测/契约验证不依赖真实客户端。
