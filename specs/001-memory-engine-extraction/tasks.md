---
description: "Task list for 记忆引擎抽离(Memory Engine Extraction)"
---

# Tasks: 记忆引擎抽离(Memory Engine Extraction)

**Input**: Design documents from `/specs/001-memory-engine-extraction/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 本特性以保真为核心目标,测试是主线交付物(既有单测平移 + 确定性检索对拍 +
降级验证),故包含测试任务。

**Organization**: 任务按用户故事分组,支持各故事独立实现与验证。

**源码位置约定**:抽离源为 `/home/wallfacers/project/workhorse/workhorse-agent`(下称
`SRC/`);目标为 engram 仓库根(下称 `./`)。所有"搬"= 复制 + 改 import 路径,
**不改宿主仓库**(FR-012)。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行(不同文件/目录,无未完成依赖)
- **[Story]**: US1 / US2 / US3(Setup/Foundational/Polish 无 Story 标签)

---

## Phase 1: Setup(共享基建)

**Purpose**: 立起 Go module 与目录骨架

- [X] T001 在 `./` 执行 `go mod init github.com/wallfacers/engram` 并设 `go 1.22`,生成 `./go.mod`
- [X] T002 [P] 扩展 `./.gitignore`:忽略 Go 构建产物、`*.db`/`*.sqlite`、`cmd/locomo-bench` 二进制、本地 `testdata` 大文件缓存
- [X] T003 [P] 创建顶层目录骨架:`./memory/`、`./store/`、`./embedding/`、`./provider/`、`./internal/idgen/`、`./cmd/locomo-bench/`、`./testdata/parity/`

**Checkpoint**: 空 module 可 `go build ./...`(无包时应无错)

---

## Phase 2: Foundational(阻塞所有故事的前置)

**Purpose**: 搬入 memory 依赖的基础设施包 —— 引擎无它们无法编译。此阶段完成前任何故事不可开工。

**⚠️ CRITICAL**: 本阶段是 US1/US2/US3 的共同地基

- [X] T004 [P] 搬 `SRC/internal/idgen/` → `./internal/idgen/`(整包,含测试),改 import 路径为 `github.com/wallfacers/engram/internal/idgen`
- [X] T005 [P] 搬 `SRC/internal/embedding/` → `./embedding/`(整包,含 `embedder_test.go` 等),改 import 路径
- [X] T006 [P] 搬 `SRC/internal/provider/`(含 `anthropic/`、`openai/`、`policy.go`/`retry.go`/`sseparse.go` 及测试)→ `./provider/`,改 import 路径。**注意**:`anthropic/openai` 依赖 `internal/version`(设 User-Agent 头),需 T006a 先/同步到位
- [X] T006a [P] 搬 `SRC/internal/version/`(18 行,自洽)→ `./internal/version/`,改 import 路径;**去品牌化**:`UserAgent()` 返回值由 `"workhorse-agent/"` 改为 `"engram/"`(唯一允许的行为例外,依据 research.md R1b;不改变任何 MVP 门禁,无测试断言该串)。provider 两处调用点(`anthropic.go`/`openai.go`)相应指向 `engram/internal/version`
- [X] T007 切片搬 store 实现 → `./store/`:从 `SRC/internal/store/sqlite/` 取 `sqlite.go`(`Open`/`Options`/`Store`)、`migrations.go`(**仅记忆迁移**:memory_entries/fts+3触发器/curation_lease/embeddings/entities + event_date/fact_source 列,renumber 为 engram 独立链,保留 up/down)、`funcs.go`(**仅 `ProbeFTS5`,去 `extract_text`**);**不纳入**会话/权限表。依据 research.md R2/R3、data-model.md
- [X] T007a 切片搬 store 接口/类型 → `./store/store.go`:从 `SRC/internal/store/`(接口包,非 sqlite)取 **记忆相关符号** `Store` / `ErrNotFound` / `Upsert` / `BumpUsage`(`entrystore.go` 依赖,**必须纳入**);会话类型 `Session` / `SessionState` / `SessionSummary` **留宿主不搬**;共用类型按最小闭包纳入并在提交信息记录归属。依据 research.md R2a、contracts 路径映射
- [X] T007b 负向核验(SC-005):确认 `./store/` 迁移链与类型中**无任何非记忆结构**(无 sessions/messages/events/tool_calls/permissions 表,无 Session* 类型);`grep -rniE "session|permission|message|tool_call" ./store/ --include=*.go` 结果仅限记忆语义命中,否则回退清理
- [X] T008 切片搬 prompt → `./memory/prompt/`:从 `SRC/internal/prompt/` 取 `memory_extraction.go` + `curation_judge.go` + 二者 import 闭包内的模板基建(`template.go`/`builtins.go` 按需)。**陷阱规避**:**不搬 `doc.go`**、且**不整搬 `template_test.go`**——它们依赖 `internal/skills`(不在搬运范围);只平移不触碰 skills 的相关测试用例(二目标文件仅 import `fmt`/`strings`,切片子集本身干净)。依据 research.md R1a/R4
- [X] T009 搬记忆相关的 store 测试 → `./store/`:`fts5_test.go`、`migrations_test.go`(仅记忆用例)、`probe_test.go`,改 import 路径
- [X] T010 在 `./` 运行 `go mod tidy` 解析外部依赖(`modernc.org/sqlite` 等),生成 `./go.sum`;确认无 CGO 依赖进入核心路径

**Checkpoint**: `go build ./embedding/... ./provider/... ./store/... ./internal/... ./memory/prompt/...` 全绿;基础设施独立可编译

---

## Phase 3: User Story 1 - 记忆引擎作为独立库可编译、可测试 (Priority: P1) 🎯 MVP

**Goal**: memory 引擎搬入 engram、切断宿主耦合,独立构建 + 既有单测全绿 + 零宿主引用

**Independent Test**: 纯净环境 `go build ./...` + `go test ./...` 全绿,且 `grep -r workhorse-agent/internal` 引擎代码零命中

### Implementation for User Story 1

- [X] T011 [US1] 搬 `SRC/internal/memory/*.go`(非测试:entrystore/retriever/embedder/vectorstore/entities/block/budgets/writer/usagelog/migrate/snapshot/export)→ `./memory/`,批量改 import:`internal/memory`→`engram/memory`、`internal/store`→`engram/store`、`store/sqlite`→`engram/store`、`prompt`→`engram/memory/prompt`、`embedding`→`engram/embedding`、`idgen`→`engram/internal/idgen`、`provider`→`engram/provider`
- [X] T012 [US1] 内化 sessionsearch:新建 `./memory/queryplan.go`,把 `SRC/internal/tools/sessionsearch/` 的 `buildPlan`/`likeFragments` 及其 CJK 分词器(`cjk.go`/`tokenizer.go`,仅依赖 `strings`/`unicode`)落入本包;将 `retriever.go` 两处 `sessionsearch.BuildPlan`/`LikeFragments` 调用改为本包函数;删除对 `internal/tools` 的 import。依据 research.md R5
- [X] T013 [P] [US1] 搬 `SRC/internal/memory/curation/` → `./memory/curation/`(整子包,含测试),改 import 路径
- [X] T014 [P] [US1] 搬 `SRC/internal/memory/pipeline/` → `./memory/pipeline/`(整子包,含测试),改 import 路径
- [X] T015 [US1] 搬 memory 既有测试 → `./memory/`:`entrystore_test.go`、`retriever_test.go`、`embedder_test.go`、`memory_test.go`、`migrate_test.go`、`snapshot_test.go`、`export_test.go`、`usagelog_test.go`,改 import 路径;从 sessionsearch 相关测试提取 `queryplan_test.go` 锚定分词/查询解析行为
- [X] T016 [US1] `go build ./...` 转绿:解决残留宿主引用、`extract_text` 边界(research.md R3)、prompt template 闭包(R4)等编译暴露的缺口,按需最小补齐
- [X] T017 [US1] `go test ./...` 转绿:所有平移单测通过(SC-002),定位并修复任何因路径/初始化差异导致的失败(不改变行为语义)
- [X] T018 [US1] 零宿主引用核验:`grep -rn "workhorse-agent/internal" ./ --include=*.go` 应零命中(SC-004);记录核验结果

**Checkpoint**: US1 完成 —— engram 作为独立库编译通过、既有单测全绿、无任何宿主代码引用。**MVP 达成**

---

## Phase 4: User Story 2 - 检索行为在抽离前后逐条一致(保真证明) (Priority: P1)

**Goal**: 确定性检索对拍证明搬运保真;三路信号降级行为一致;二者进 CI

**Independent Test**: `go test ./memory -run TestRetrievalParity` 逐条一致率 100%;`-run TestSignalDegradation` 通过

### Implementation for User Story 2

- [X] T019 [US2] 冻存基线:用固定语料 + 固定 query 集 + 固定/打桩向量跑抽离前 `Retriever`,导出每 query 的排序 entry ID 序列为 golden,写入 engram `./testdata/parity/`(含 fixtures + 期望序列)。**采集用一次性、不提交的临时 harness**(scratch 目录或临时 `_test.go`),**宿主 workhorse 工作树须保持干净、零提交**(FR-012);产物只落 engram。依据 research.md R6
- [X] T020 [US2] 实现 `./memory/parity_test.go` 的 `TestRetrievalParity`:加载 `testdata/parity/` fixtures + golden,以打桩向量运行 engram `Retriever`,逐 query 断言排序 entry ID 序列与 golden 逐条相等(SC-003),无需外部端点
- [X] T021 [US2] 实现 `./memory/parity_test.go` 的 `TestSignalDegradation`:分别令语义/关键词/实体三路信号缺失或失败,断言其余独立降级仍返回结果且与基线一致、不整体报错(FR-009)
- [X] T022 [US2] 建 CI 工作流 `./.github/workflows/ci.yml`:每次 push/PR 跑 `go build ./...` + `go test ./...`(含对拍与降级),作为合并门禁(FR-008)

**Checkpoint**: US2 完成 —— 保真对拍 100%、降级验证通过、CI 守门

---

## Phase 5: User Story 3 - 评测/回归工具随引擎一并可用 (Priority: P2)

**Goal**: locomo-bench 搬入 engram、可构建、小子集端到端跑通;全量作一次性抽查

**Independent Test**: `go build ./cmd/locomo-bench` 成功;小子集端到端产出结果

### Implementation for User Story 3

- [ ] T023 [US3] 搬 `SRC/cmd/locomo-bench/`(main/runner/dataset/chunks/filter/journal 及测试)→ `./cmd/locomo-bench/`,改 import 路径为 engram 各包
- [ ] T024 [US3] `go build ./cmd/locomo-bench` 转绿 + 平移 bench 自带测试(`bench_test.go`/`chunks_test.go`/`filter_test.go` 等)通过
- [ ] T025 [US3] 小子集端到端跑通:配本地 embedding/LLM 端点(`EMBED_BASE_URL`/`BASE_URL` 等),`--limit` 小子集运行并产出可比口径结果(SC-006);把所需环境变量补进 `quickstart.md`
- [ ] T026 [US3] 全量数据集一次性 sanity check:跑一次完整 LoCoMo,记录结果范围与抽离前对照(SC-007);**非逐分门禁**,仅记录(FR-010)

**Checkpoint**: US3 完成 —— 评测设施随迁可用,回归 + 论文两用

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 跨故事的收尾与门禁固化

- [ ] T027 [P] 更新 `./README.md`:标注 engram 已是可构建 module,给出构建/测试/对拍/bench 入口,链接 `specs/001-memory-engine-extraction/`
- [X] T028 [P] 无 CGO 验证:`CGO_ENABLED=0 go build ./...` 通过并写入 CI(宪法原则 I)
- [X] T029 [P] 静态检查:CI 增 `go vet ./...`(可选 staticcheck)
- [ ] T030 端到端走查 `quickstart.md` 五步,确认与实际一致;修订偏差

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (P1)**:无依赖,立即开始
- **Foundational (P2)**:依赖 Setup;**阻塞所有用户故事**
- **US1 (P3)**:依赖 Foundational;MVP
- **US2 (P4)**:依赖 US1(需可编译可跑的 engram 引擎才能对拍)
- **US3 (P5)**:依赖 US1(bench 依赖 engram 各包);与 US2 可并行
- **Polish (P6)**:依赖所需故事完成

### User Story Dependencies

- **US1 (P1)**:Foundational 后即可开工,不依赖其它故事
- **US2 (P1)**:依赖 US1 产出的可编译引擎
- **US3 (P2)**:依赖 US1;与 US2 相互独立,可并行

### Within Foundational / US1

- T004/T005/T006/T006a [P] 独立基础设施包并行(T006a version 是 T006 provider 的依赖,需先/同步到位)
- T007/T007a/T007b(store 实现 + 接口切片 + 负向核验)、T008(prompt 切片)可与 T004-T006 并行,但 T007b 依赖 T007/T007a,T010 tidy 依赖全部到位
- T011(搬 memory 主体)依赖 Foundational 完成;T013/T014 [P] 子包并行;T012 内化耦合与 T011 同区需顺序;T016→T017→T018 顺序收敛

### Parallel Opportunities

- Setup:T002、T003 并行
- Foundational:T004、T005、T006 并行(三个独立包)
- US1:T013、T014 并行(curation/pipeline 两子包)
- US2 与 US3:US1 完成后可并行推进
- Polish:T027、T028、T029 并行

---

## Implementation Strategy

### MVP First(仅 US1)

1. Phase 1 Setup → 2. Phase 2 Foundational(**关键,阻塞一切**)→ 3. Phase 3 US1
4. **STOP & VALIDATE**:纯净环境 `go build ./...` + `go test ./...` 全绿 + 零宿主引用
5. 此时 engram 已是可独立编译、行为可信的引擎 —— 后续所有集成面的地基就位

### Incremental Delivery

1. Setup + Foundational → 地基就位
2. US1 → 独立库可编译可测(MVP,可交付)
3. US2 → 保真对拍 + CI 守门(证明搬运没丢护城河)
4. US3 → 评测设施随迁(回归 + 论文)
5. Polish → 无 CGO/静态检查/文档固化

---

## Notes

- [P] = 不同文件/目录、无未完成依赖
- 每个 [Story] 任务标注了对应文件路径,可独立执行
- "搬" 一律 = 复制 + 改 import 路径,**绝不修改宿主仓库**(FR-012)
- 保真是最高纪律:T017 修单测、T016 补缺口时**只允许调整路径/初始化,不得改变行为语义**;
  任何疑似行为变更须在对拍(T020)暴露并回退
- 切片归属存疑时(store 迁移、prompt 闭包):从宽纳入记忆所需部分,并在提交信息记录判断(research.md R2/R3/R4)
