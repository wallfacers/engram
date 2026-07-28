# Tasks: Curation 生命周期与记忆索引完整性

**Input**: Design documents from `/specs/018-curation-lifecycle-cleanup/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md,
contracts/curation-contract.md, quickstart.md

**Tests**: 本 feature 明确要求 TDD。每个行为变更都先写可失败测试、观察 RED，再实现
并观察 GREEN；不能把测试与实现合并成一个不可验证步骤。

**Organization**: 任务按用户故事分组。US1 与 US2 同为 P1；共享 worker 生命周期先放在
Foundational，CLI 的单次调用复用它但不依赖 MCP 装配。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可与相邻任务并行，因为触及不同文件且不依赖对方未完成的改动
- **[Story]**: 对应 spec.md 的用户故事
- 每个任务包含具体文件路径

## Phase 1: Setup（安全基线）

**Purpose**: 固定现有绿线和并行 feature 边界，避免把旧失败或其他 worktree 改动归因
到本功能。

- [X] T001 在 `specs/018-curation-lifecycle-cleanup/quickstart.md` 记录 `CGO_ENABLED=0 go test -count=1 ./...` 基线结果，并确认 `git worktree list` 中其他 active feature 不触及 `memory/entrystore.go`、`memory/curation/worker.go`、`mcpserver/` 或 `cmd/engram/`

**Checkpoint**: 基线全绿且无文件碰撞后才能进入引擎行为变更。

---

## Phase 2: Foundational（共享 Curation 生命周期）

**Purpose**: 为 US1 的后台 worker 和 US3 的同步单次 pass 提供同一套默认配置、deadline
和可等待生命周期。

**⚠️ CRITICAL**: T002 必须先 RED；T003 后同一测试才允许 GREEN。

- [X] T002 在 `memory/curation/worker_test.go` 先新增失败测试，覆盖 `DefaultConfig` 精确水位、阻塞 caller 收到两分钟 pass deadline、同一 worker 100 次重复 `Start` 仍只有一个 loop、cancel 后 `Wait` 确定返回及 inert worker 安全 no-op
- [X] T003 在 `memory/curation/worker.go` 实现唯一 `DefaultConfig`、`Config.PassTimeout`、整趟 deadline、幂等 `Start` 与 `Wait`，保持 `RunPass` 模型/解析错误 fail-safe no-op
- [X] T004 运行 `CGO_ENABLED=0 go test -count=1 ./memory/curation ./memory/pipeline`，确认新生命周期测试与现有 judge/lease/pipeline 契约全部通过

**Checkpoint**: 引擎已提供 host-agnostic 的启动、取消、等待和超时边界。

---

## Phase 3: User Story 1 — 显式开启持久异步 Curation (Priority: P1)

**Goal**: MCP 默认关闭；显式开启且 LLM 可用时，每 namespace 恰好一个异步 worker，
write/ingest 非阻塞通知，并在淘汰/关闭时先等待 worker 再关存储。

**Independent Test**: 默认 registry 写入不调用 judge；开启 registry 后两个 namespace
分别收到 write/ingest 通知，请求在阻塞 judge 完成前返回；取消、LRU 和 Close 后没有
数据库关闭后的访问；无 LLM 的 enabled 配置构造失败。

### Tests for User Story 1（先 RED）

- [X] T005 [P] [US1] 新建 `mcpserver/config_test.go`，覆盖 `ENGRAM_CURATION_ENABLED` 默认 false、合法 env、flag 覆盖 env、非法 bool，以及 enabled + nil LLM 的启动拒绝
- [X] T006 [US1] 在 `mcpserver/registry_test.go` 新增每 namespace 单 worker、100 次重复 Acquire/Start/关闭/淘汰仍无重复 loop、cancel + Wait、LRU 淘汰和 Registry.Close 关闭顺序测试
- [X] T007 [P] [US1] 在 `mcpserver/tools_test.go` 与 `mcpserver/ingest_test.go` 新增阻塞 judge 测试，证明 100 次 `memory_write` 均在 pass 前返回且 pending 通知最多 1、`memory_ingest` 每批一次通知、默认关闭零通知且 namespace 隔离

### Implementation for User Story 1

- [X] T008 [US1] 在 `mcpserver/config.go` 增加 `CurationEnabled`、`--curation-enabled` 与 `ENGRAM_CURATION_ENABLED` 解析，维持 flag 优先和非法值可操作错误
- [X] T009 [US1] 在 `mcpserver/registry.go` 为 enabled namespace 装配 `curation.Worker` 和独立 cancel context，将 `Notify` 接到 pipeline `OnWrite`，并按 cancel → Wait → embedder → store 顺序关闭
- [X] T010 [US1] 在 `mcpserver/tools.go` 的 `memory_write` 成功路径添加同 namespace 非阻塞 `Notify`，保持 `memory_ingest` 仅由 pipeline 发一次通知
- [X] T011 [US1] 在 `cmd/engram-mcp/main.go` 透传 `CurationEnabled` 并在启动 INFO 中输出 `curation=true|false`，补充 `cmd/engram-mcp/main_test.go` 的无 secret/透传断言
- [X] T012 [US1] 运行 `CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram-mcp` 与 `CGO_ENABLED=0 go build ./...`，以 100 次重复 Start/Acquire/write 和 200 次 LRU 压力用例确认配置、异步、隔离与关闭契约全绿；Go race detector 强制启用 CGO，与项目 `CGO_ENABLED=0` 硬门冲突，故不作为本仓验证命令

**Checkpoint**: US1 可独立演示；默认 MCP 完全兼容，enabled MCP 有稳定异步生命周期。

---

## Phase 4: User Story 2 — 删除与合并后保持存储完整 (Priority: P1)

**Goal**: Delete/Merge 在现有事务中清除全部可归属 side data、shadow vectors 和失效引用，
保留无关/共享/存活目标数据，任一步失败整体回滚。

**Independent Test**: 构造目标、无关对照、共享实体、reverse supersession 和
`name`/`name#alias`/`name#query` 向量；Delete/Merge 后逐表断言，且用 abort trigger
注入失败验证 base/side 全部回滚。

### Tests for User Story 2（先 RED）

- [X] T013 [US2] 在 `memory/entrystore_test.go` 新增 Delete 完整性失败测试，覆盖三类 vector、entities、event aliases 及其 FTS、fact queries、reverse `superseded_by`、无引用 entity edges 和无关对照数据
- [X] T014 [US2] 在 `memory/entrystore_test.go` 新增 Merge 完整性失败测试，覆盖 consumed source 全清、surviving target 派生失效、指向 target 的有效 supersession 保留、共享 edge 与无关数据保留
- [X] T015 [US2] 在 `memory/entrystore_test.go` 用参数化临时 abort triggers 覆盖 embeddings、entities、event aliases、fact queries、reverse reference 与 entity-edge prune 的每个可注入失败阶段，分别证明 Delete 与 Merge 的 base entry、side rows、reverse reference 全部原子回滚

### Implementation for User Story 2

- [X] T016 [US2] 在 `memory/entrystore.go` 将现有 helper 拆为派生失效、真实删除引用清理和无引用 entity-edge prune，并在 Delete/Merge 的调用方事务内实现 `name`/`name#alias`/`name#query` 精确清理
- [X] T017 [US2] 运行 `CGO_ENABLED=0 go test -count=1 ./memory`，确认新增完整性/回滚测试、alias/query shadow、graph、retrieval parity 与 degradation 测试全部通过

**Checkpoint**: US2 可独立交付；显式 Delete/Merge 不再产生新孤儿索引。

---

## Phase 5: User Story 3 — 显式执行一次 CLI Curation (Priority: P2)

**Goal**: `engram curate` 在当前 namespace 同步执行一趟、两分钟超时；缺能力明确失败；
普通 `add`/`ingest` 不触发 curation。

**Independent Test**: stub judge 下命令输出确定性 completed Markdown；无 LLM 返回 code 4；
阻塞 judge 收到 deadline 且不迟到 apply；普通 add/ingest 的 judge 调用数为零。

### Tests for User Story 3（先 RED）

- [X] T018 [P] [US3] 在 `cmd/engram/commands_test.go` 新增 `curate` 路由、无参数、确定性 stdout、缺 LLM capability code 与 namespace 隔离测试
- [X] T019 [US3] 在 `cmd/engram/lifecycle_test.go` 与 `cmd/engram/ingest_test.go` 新增阻塞 caller 的 cancel/deadline 测试，以及普通 `add`/`ingest` 不启动或通知 curator 的计数断言

### Implementation for User Story 3

- [X] T020 [US3] 在 `cmd/engram/engine.go` 用同一 LLM caller 和 `curation.DefaultConfig` 构造一次性 curator，但不调用 `Start`、不设置 pipeline `OnWrite`
- [X] T021 [US3] 新建 `cmd/engram/curate.go`，实现无命令参数校验、同步 `RunPass`、两分钟 context、capability/取消/超时诊断和契约规定的 completed Markdown
- [X] T022 [US3] 在 `cmd/engram/run.go` 注册并分派 `curate`，保持其他命令路径与退出码不变
- [X] T023 [US3] 运行 `CGO_ENABLED=0 go test -count=1 ./cmd/engram`，确认新命令与全部现有 offline/e2e/lifecycle 测试通过

**Checkpoint**: US3 可独立演示；CLI 同步边界明确，常用命令零额外维护成本。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 更新用户文档、执行宪法硬门并做规格/实现一致性收口。

- [X] T024 [P] 在 `docs/mcp-server.md` 文档化显式开关、LLM 前置条件、异步 write/ingest 时序、每 namespace 生命周期、两分钟 pass 上限和默认关闭兼容性
- [X] T025 [P] 在 `docs/cli.md` 添加 `curate` 语法、同步耗时/两分钟上限、一次性与持久模式优缺点，并明确普通 `add`/`ingest` 不自动 curation
- [X] T026 按 `specs/018-curation-lifecycle-cleanup/quickstart.md` 执行 focused/offline 验证，修正任何与实际命令输出或错误码不一致的契约文档
- [X] T027 运行 `CGO_ENABLED=0 go test -count=1 ./...`、`CGO_ENABLED=0 go build ./...` 与 `go vet ./...`，把零失败证据记录到 `specs/018-curation-lifecycle-cleanup/quickstart.md`
- [X] T028 先运行确定性 retrieval parity、signal degradation、namespace isolation 和 `cmd/locomo-bench` 离线测试；随后在取得显式成本授权后按当前 canonical recipe 运行可比 LoCoMo 并核对 85.71% 参考点与显著回退门，结果记录到 `specs/018-curation-lifecycle-cleanup/plan.md`；未授权、未运行或显著回退时本任务保持未完成且 feature 不得合并
- [X] T029 对照 `specs/018-curation-lifecycle-cleanup/spec.md`、`contracts/curation-contract.md` 和 `.specify/memory/constitution.md` 做最终一致性审查，确认无 secret、仅包含已规划并验证的 v6 revision migration、无新依赖、无未验证完成声明

---

## Phase 7: Independent Review Remediation

**Purpose**: 修复显式开启异步 destructive worker 后暴露的并发线性化问题，并把独立
review 反馈回填到规范。

- [X] T030 在 `memory/curation/worker_test.go`、`memory/embedder_test.go` 与 `memory/entrystore_test.go` 先观察 RED，覆盖 judge 等待期间同名 rewrite/pin、Merge source rewrite、Delete 后迟到 vector 复活和 live base name 与 shadow key 碰撞
- [X] T031 在 `memory/entrystore.go`、`memory/curation/worker.go`、`memory/vectorstore.go` 与 `memory/embedder.go` 实现 transaction revision CAS 和 owner-revision conditional vector upsert
- [X] T032 在 `memory/curation/worker_test.go`、`mcpserver/registry_test.go` 与 `cmd/engram/*_test.go` 先观察 RED，覆盖 heartbeat join、周期起点、LRU 不持全局锁、提交后 cancel 不误报和不完整 LLM capability
- [X] T033 在 `memory/curation/worker.go`、`mcpserver/registry.go` 与 `cmd/engram/` 实现 heartbeat join、start-to-start periodic schedule、closing marker + lock 外 shutdown、显式 pass outcome 与 capability 分类
- [X] T034 重跑 review remediation focused stress、全仓 test/build/vet 和确定性评测门，并将结果记录到 `quickstart.md`

---

## Phase 8: Revision Token Review Remediation

**Purpose**: 消除相同 `updated_at` 与 conflict supersede 的剩余 stale-decision 窗口。

- [X] T035 在 `store/migrations_test.go`、`memory/entrystore_test.go`、
  `memory/embedder_test.go` 与 `memory/curation/worker_test.go` 先观察 RED，覆盖 v6
  默认 revision、相同时间戳 rewrite 后的 delete/merge/vector 拒绝，以及 loser/winner
  任一变化后的 supersede 拒绝
- [X] T036 在 `store/migrations.go`、`memory/entrystore.go`、
  `memory/vectorstore.go` 与 `memory/curation/worker.go` 实现数据库单调 revision、
  `id + revision` CAS 和 Supersede 双端原子验证
- [X] T037 重跑 revision focused stress、全仓 test/build/vet、确定性评测门与独立
  Critical/Important 复审，并将证据记录到 `quickstart.md`

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: 无依赖；先固定基线。
- **Phase 2 Foundational**: 依赖 T001；阻塞 US1 和 US3。
- **US1 (Phase 3)**: 依赖 Phase 2；不依赖 US2/US3。
- **US2 (Phase 4)**: 只依赖安全基线；为保持单人 TDD 节奏，默认在 US1 后执行，但可与
  Phase 2/US1 独立开发。
- **US3 (Phase 5)**: 依赖 Phase 2；不依赖 US1 的 MCP 装配，也不依赖 US2。
- **Polish (Phase 6)**: 依赖计划交付的全部用户故事。

### User Story Dependency Graph

```text
T001 baseline
├── Phase 2 worker lifecycle ──┬── US1 MCP async
│                              └── US3 CLI sync
└── US2 storage integrity

US1 + US2 + US3 ──> docs + full regression + constitution review
```

### Within Each User Story

- 先写测试并单独运行，必须观察到因目标能力缺失而失败。
- 实现最小变更使 focused 测试通过。
- 再运行该 package 的全部测试，防止只满足新用例。
- 故事 checkpoint 全绿后才勾选该故事最后任务。

### Parallel Opportunities

- T005（配置测试）和 T007（tool 通知测试）触及不同文件，可并行起草；T006 单独处理
  registry 生命周期。
- US2 的 `memory/entrystore*` 与 US1 的 `mcpserver/*` 文件不重叠，可独立开发。
- T018（CLI contract）可在 US1 进行时起草，但 T019–T023 依赖 Phase 2 Worker API。
- T024 与 T025 文档文件不同，可并行。

## Parallel Examples

### User Story 1

```text
Task T005: MCP curation 配置契约测试（mcpserver/config_test.go）
Task T007: write/ingest 非阻塞通知测试（mcpserver/tools_test.go, ingest_test.go）
```

T009 合并配置和生命周期测试期望后再统一实现 registry，避免多个任务同时修改
`mcpserver/registry.go`。

### User Story 2 与 User Story 1

```text
Task group A: T005–T012（mcpserver/, cmd/engram-mcp/）
Task group B: T013–T017（memory/entrystore.go, entrystore_test.go）
```

两组没有文件交叉；单人执行时仍建议先完成一个 RED→GREEN 闭环再切换。

### Documentation

```text
Task T024: docs/mcp-server.md
Task T025: docs/cli.md
```

## Implementation Strategy

### Recommended MVP

本次用户明确要求同时处理两个现存缺陷，因此最小可接受交付不是只做 US1，而是：

1. T001 基线；
2. Phase 2 共享生命周期；
3. US1 MCP 显式异步；
4. US2 存储完整性；
5. 分别完成两个 P1 checkpoint。

US3 是已确认的 P2 操作入口，紧接 MVP 完成，不应混入 MCP 写请求热路径。

### Incremental Delivery

1. DefaultConfig + deadline + Start/Wait → 引擎生命周期可验证。
2. MCP opt-in + Notify + safe close → 持久模式可验证。
3. Delete/Merge side cleanup → 数据完整性可验证。
4. CLI one-shot curate → 同步操作模式可验证。
5. 文档、全仓回归和宪法审查 → feature 收口。

## Notes

- `[P]` 只表示文件和前置依赖允许并行，不授权多 agent；当前执行仍由一个工作流完成。
- 仅新增独立 schema v6 revision migration，不修改既有 migration，也不自动 sweep
  历史孤儿。
- 不为 `curate` 添加动作统计；`status: completed` 不等于实际 merge/evict。
- 不运行未经授权的付费 LoCoMo。确定性门若显示检索行为变化，立即停止并请求授权。
