# Phase 0 Research: 记忆引擎抽离

本文记录抽离动作的技术未知量核查结果。所有结论基于对 workhorse-agent
当前源码(`go 1.22.0`,`module github.com/wallfacers/workhorse-agent`)的直接勘察。

## R1. 依赖闭包:memory 引擎实际牵动哪些包

**Decision**: 抽入 engram 的包集合 = {memory 全部} ∪ {embedding, idgen, provider} 整包
∪ {store/sqlite, prompt 的记忆切片} ∪ {sessionsearch 的两个纯函数}。

**Rationale**: 勘察 `internal/memory` 及 `cmd/locomo-bench` 的 internal import,得直接依赖:
`store/sqlite`(仅用 `Open`/`Options`/`Store`)、`embedding`、`provider`(+`anthropic`/`openai`)、
`prompt`、`store`、`idgen`、`tools/sessionsearch`(仅 2 个符号)。逐包核查反向依赖:
`embedding`/`idgen`/`provider`/`store` 对其它 internal 包**零依赖**;`store/sqlite` 仅依赖
其自身接口包 `store`。故这些基础设施包是自洽的,整包搬不会拖入宿主耦合。

**Alternatives considered**: 用接口把 store/provider/embedder 抽象、实现留宿主——被 Q1
否决(那是"重设计契约",破坏纯搬运的可归因性)。

**补充勘察(第三个混杂包)**:除 `store/sqlite` 外,memory 还直接 import **`internal/store`**
(接口/类型包,221 行)。该包同样**混杂**:含记忆符号 `store.Store` / `store.ErrNotFound` /
`store.Upsert` / `store.BumpUsage`(被 `entrystore.go` 依赖),也含会话类型 `Session` /
`SessionState` / `SessionSummary`。因此它与 `store/sqlite`、`prompt` 并列,是**第三个需要
按记忆相关性切片**的对象:记忆符号切入 engram 的 `store/` 包,会话类型留宿主。注意这些符号
是记忆**必需**的,必须**纳入**而非去除。见 R2a。

## R2. store/sqlite 切片:记忆迁移可独立成链吗

**Decision**: 可以。engram 的 `store/` 重建一条**只含记忆迁移**的链,renumber 为 v1..vN。

**Rationale**: `migrations.go` 是单一 `schema_version` 线性链。v1–v6 为会话/消息/事件/
工具调用/权限/messages_fts/会话字段;记忆迁移从其后开始,含:
`memory_entries`(+`idx_memory_pinned`)、`memory_entries_fts`(FTS5 虚拟表)+ 3 触发器
(ai/ad/au)、`memory_entities`(+`idx_memory_entities_norm`)、`memory_embeddings`、
`memory_curation_lease`、以及 `ALTER memory_entries ADD event_date/fact_source`(含 down)。
关键:**记忆表对 sessions 无外键**(`source_session_id` 只是 TEXT,无 FK 约束),因此记忆
schema 与会话 schema 完全解耦,可独立建链。engram 从空库直接建到记忆终态即可。

**Alternatives considered**: 整包搬 migrations 再运行时忽略会话迁移——被 Q2 否决(engram
会携带宿主 schema,违背原则 II)。

## R2a. internal/store 接口包切片

**Decision**: engram 的 `store/` 除承载 SQLite 实现外,一并纳入 `internal/store` 里**记忆相关
的接口/类型/函数**(`Store`、`ErrNotFound`、`Upsert`、`BumpUsage` 等 entrystore 依赖的符号);
会话类型(`Session`/`SessionState`/`SessionSummary` 等)留宿主、不搬。

**Rationale**: `entrystore.go` 直接依赖这些符号,是记忆 CRUD/使用记账的接口契约,必须随迁,
否则 engram 无法编译。它与 `store/sqlite`、`prompt` 同为混杂共享包,切法一致:只取记忆闭包。
归属存疑者(如某类型被记忆与会话共用)从宽纳入记忆所需部分,实现时以编译暴露最小闭包。

**Follow-up**: 实现时核查 `store.go`/`types.go` 中记忆符号是否引用了会话类型;若有共用类型,
连带其最小定义纳入 engram/store,并在提交信息记录归属判断。

## R3. 自定义 SQLite 函数:engram 需要哪些

**Decision**: engram 的 `store/funcs.go` 只保留 `ProbeFTS5`,**不搬 `extract_text`**。

**Rationale**: `funcs.go` 在 `init()` 里注册确定性标量函数 `extract_text`——它从 JSON
`content_json` 抽取文本,专供 **messages_fts**(会话 FTS)。记忆 FTS(`memory_entries_fts`)
直连 `content` 列、不走 JSON 抽取(源码注释明确 "Unlike messages_fts")。全局 grep 确认
`internal/memory` 无任何 `extract_text` 引用。`ProbeFTS5`(探测 SQLite 是否编入 FTS5)是
记忆 FTS 建表前的通用前置,需保留。

**Risk/Follow-up**: 若切片后发现 memory 打开路径间接依赖 `extract_text` 注册(如共享
migration 里残留 messages_fts),在实现时以编译 + 单测暴露,按需最小补齐。

## R4. prompt 切片:精确到两文件

**Decision**: engram 的 `memory/prompt/` 只搬 `memory_extraction.go` + `curation_judge.go`,
外加二者 import 闭包内的模板基建(`template.go` / `builtins.go` 按需)。

**Rationale**: memory 引用的 prompt 符号共 7 个,全部落在这两文件:
`MemoryExtractionSystemPrompt` / `BuildMemoryExtractionUserPrompt` / `MemoryExtractionMessage`
(memory_extraction.go);`CurationJudgeSystemPrompt` / `BuildCurationJudgeUserPrompt` /
`CurationJudgeCandidate` / `CurationJudgeCluster`(curation_judge.go)。其余 prompt 文件
(builtins/environment/basetodo 等)属会话/agent,不搬。

**Follow-up**: 实现时核查这两文件是否依赖 `template.go` 的渲染器;若是,连带其最小闭包搬入
`memory/prompt/`,并平移 `template_test.go` 中与之相关的用例。

## R5. sessionsearch 耦合:如何内化

**Decision**: 把 `buildPlan` / `likeFragments`(及其依赖的 CJK 分词器)搬进
`memory/queryplan.go`,`retriever.go` 两处调用改为本包函数;切断对 `internal/tools` 的依赖。

**Rationale**: `BuildPlan`/`LikeFragments` 只是 `cjk_export.go` 里对 `buildPlan`/`likeFragments`
的导出别名,实体在 `cjk.go`/`tokenizer.go`,**只 import 标准库 `strings`/`unicode`**,零宿主
依赖。retriever.go:236/260 用它们构建 FTS `MATCH` 表达式与 LIKE 回退片段。内化后行为逐字节
一致,只换 import 路径。sessionsearch 自身的相关测试平移为 `queryplan_test.go`,锚定行为。

**Alternatives considered**: 让 engram 依赖一个抽出来的 `tokenizer` 小包——增无谓包边界,
不如直接内化进检索层(它本就是检索的一部分)。

## R6. 保真证明:为何用确定性检索对拍而非端到端分数

**Decision**: 以**检索层确定性对拍**为合并门禁;全量 LoCoMo 端到端跑作一次性 sanity check。

**Rationale**: 战略文档实测 LoCoMo 单跑噪声 ±3–5pp、两跑 59.5% 预测文本不同——端到端分数
被答题随机性主导,不适合作逐分门禁,也难归因。而我们搬的检索/存储/抽取代码中,**检索层
在给定语料 + query + 向量下是确定性的**(RRF 融合、FTS BM25 排序、实体精确匹配、余弦
排序皆确定)。因此:固定语料 + 固定 query 集 + 固定/打桩向量 → 断言 engram `Retriever`
返回的排序 entry ID 序列与抽离前基线**逐条相等**。这确定、免费、精确证明"搬运未改变行为",
且把答题噪声完全排除。此纪律恰是评测可靠性论文的核心主张(别拿单跑噪声当信号)。

**Baseline capture**: 在 workhorse 侧用同一组固定输入跑 `Retriever`,把排序 entry ID 序列
冻存为 `testdata/parity/` 下的 golden 快照,随对拍用例进 engram CI。

**Alternatives considered**: 全量端到端 ±噪声带对拍(慢、贵、归因弱);冒烟级(不够证明保真)。

## R7. 离线可运行边界

**Decision**: 核心路径(存储/检索/抽取/curation)默认离线;embedding 与 LLM 调用经环境变量
指向本地 sidecar(`EMBED_BASE_URL`/`BASE_URL` 等),无硬编码托管端点。

**Rationale**: 沿用源引擎设计——embedding 走本地 sidecar,provider 抽象可指任意兼容端点。
抽离不改这一点。对拍用例用打桩向量,不需活端点即可在 CI 跑通(强化离线可测)。

## 未决澄清

无。所有 NEEDS CLARIFICATION 已在勘察中解决;归属存疑项(R3 的 extract_text 边界、R4 的
template 闭包)已定为"实现时以编译 + 单测暴露、按需最小补齐"的确定性收敛策略,不阻塞规划。
