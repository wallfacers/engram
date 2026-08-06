# Tasks: 读侧证据关联装配（Evidence Relation Assembly）

**Input**: Design documents from `/specs/031-evidence-relation-assembly/`
**Prerequisites**: plan.md ✓ spec.md ✓ research.md ✓ data-model.md ✓ contracts/ ✓ quickstart.md ✓
**Tests**: 本项目纪律要求测试先行（CLAUDE.md / 宪法「测试先行」）——harness 离线单测在实现前先写 FAIL。

## Phase 1: Setup（Shared Infrastructure）

**Purpose**: 确认 030 装配地基可用 + 配对脚本骨架

- [ ] T001 只读核对 030 装配器契约：`cmd/locomo-bench/assembly.go` 的 `assembleEvidence`/`orderHitsForAssembly`/`renderAssembledPrompt` 与 `assemblyConfig` 字段，确认结构上下文注入点（renderAssembledPrompt 输出后）+ 030 基线记录（`docs/evaluation/reports/030-evidence-mediation-verdict.md`）
- [ ] T002 创建配对脚本骨架 `specs/031-evidence-relation-assembly/run031.sh`（复用 030 `run030.sh` 模式：`--chunks --retrieval hybrid --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned` + `--only-questions phase0-ids-029-84.txt --repeats 3` + `--compare` 分析），占位 031 flag

---

## Phase 2: Foundational（Blocking Prerequisites）

**Purpose**: 关系计算的核心数据结构与工具（US1/US2 共享）

- [ ] T003 定义 `RelationEdge` 与 `StructuralContextBlock` 类型于 `cmd/locomo-bench/relation_graph.go`（字段见 `specs/031/data-model.md`：Type 枚举 related_to/temporal_next/caused_by、Evidence、Rank；无自环、同对至多一 Type 约束）
- [ ] T004 实现实体提取工具（复用 029 `nav_diagnose_cli.go:extractEntitiesFromHits` 模式：title-case + quoted 短语，去重、cap 5）+ 内置因果指示词词典（`map[string]bool`：because/due to/led to/caused by/resulted in/therefore/as a result/consequently/since/thus/triggered/in response to）于 `cmd/locomo-bench/relation_graph.go`

**Checkpoint**: 类型 + 工具就绪，US1/US2 可并行开始。

---

## Phase 3: User Story 1 - 确定性证据关系计算（Priority: P1）🎯 MVP

**Goal**: 从候选证据计算三类关系边（related_to / temporal_next / caused_by），纯 Go 离线确定性。

**Independent Test**: 离线单测——同输入两次 `computeRelationContext` 逐字节一致；三类关系按 `Text`/`EventDate` 正确标注；空关系返回 nil；不改变证据内容与排序。

### Tests for User Story 1（先写 FAIL）

- [ ] T005 [P] [US1] 离线单测：确定性（同输入两次逐字节一致）、空关系 fail-soft（返回 nil block）、边 cap（related_to ≤4 / temporal_next ≤1 / caused_by ≤2）于 `cmd/locomo-bench/relation_graph_test.go`

### Implementation for User Story 1

- [ ] T006 [US1] 实现 `computeRelationContext(ctx, hits, category) (*StructuralContextBlock, error)` 于 `cmd/locomo-bench/relation_graph.go`：实体提取 → 共享实体建 related_to → EventDate 建 temporal_next → 因果词+共享实体双条件建 caused_by；三类关系确定性生成
- [ ] T007 [US1] 确定性排序与 tie-break（稳定排序 + `SourceID` 字典序兜底；无日期单元置尾——复用 030 `assemblyDateRank` 日期键）于 `cmd/locomo-bench/relation_graph.go`

**Checkpoint**: US1 独立可测——`go test ./cmd/locomo-bench -run TestRelationCompute` 全绿。

---

## Phase 4: User Story 2 - 结构上下文装配与类别条件注入（Priority: P1）

**Goal**: 按类别把关系边渲染为结构上下文块注入 answerer 上下文；默认关，关闭时逐字节不变。

**Independent Test**: 离线——multi-hop 出 related_to+caused_by 链、temporal 出 temporal_next 链、其余类别 nil；`--relation-context` 关闭时装配输出与 030 逐字节一致（parity）。

### Tests for User Story 2（先写 FAIL）

- [ ] T008 [P] [US2] 离线单测：类别映射（multi-hop→related_to+caused_by / temporal→temporal_next / 其余 nil）、块渲染形状（`From --Type(依据)--> To`）、parity 门（关闭时逐字节一致 `TestRelationContextParity`）于 `cmd/locomo-bench/relation_graph_test.go`

### Implementation for User Story 2

- [ ] T009 [US2] 实现类别映射与 `[relations]` 块渲染（`From --Type(依据)--> To` 多行，multi-hop 按链序 / temporal 按时间后继序；块 token 计入 cap）于 `cmd/locomo-bench/relation_graph.go`
- [ ] T010 [US2] 接线：`main.go` 注册 `--relation-context` flag（BoolVar，默认 false）；`assembly.go` 在 `renderAssembledPrompt` 之后、token 记账之前注入结构上下文块（enabled=false 时完全不调用、输出逐字节不变）
- [ ] T011 [US2] trace 叠加引用门：结构上下文块引用的证据必须落在 `trace_gate.go` 的 closed candidate boundary 内（复用 `idsInside`/`overlapsAny`），越界不注入（fail-closed）

**Checkpoint**: US1+US2 独立可测——`go test ./cmd/locomo-bench -run 'Relation'` 全绿；`CGO_ENABLED=0 go build ./...` 通过。

---

## Phase 5: User Story 3 - 配对评测与 GO 门（Priority: P2）

**Goal**: 008 铁律配对验证 031 增量；majority ≥ 030 基线且类别不回归为 GO。

**Independent Test**: 84 题子集 × 3 reps majority：`--relation-context` arm ≥ 030 keep 基线，multi-hop/temporal 不回归（L0-3）。

### Implementation for User Story 3

- [ ] T012 [US3] 完成 `run031.sh`：`--evidence-assembly --relation-context` 臂 + 叠加臂（`--trace-mediation`）+ `./locomo-bench --compare` 输出 flips + McNemar p
- [ ] T013 [US3] 子集配对评测（云端 AutoDL，同 store/answerer/judge/cap）：base/keep 基线 + `--relation-context` 3 reps + 叠加 3 reps；`--compare` 分析，写配对报告初稿（含 flips、p、类别分布）
- [ ] T014 [US3] 子集 GO 后全量 1540 复跑（`--evidence-assembly --relation-context --repeats 3` + 叠加臂），计算 majority 与 token（复用 030 分析脚本）
- [ ] T015 [US3] verdict 收口：写 `docs/evaluation/reports/031-evidence-relation-verdict.md`（GO/NO-GO + 机制归因 + 诚实边界：MemCog 消融 delta 是系统内移除、非 engram 叠加，029 模拟高估教训）+ 更新 `docs/evaluation/experiment-verdicts.md` 031 行

**Checkpoint**: US3 收口——verdict 文档落盘，experiment-verdicts 有行。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 硬门校验与交付

- [ ] T016 [P] 引擎零改动校验：`git diff --name-only -- memory embedding provider store internal` 为空（FR-008 硬门，失败即阻断）
- [ ] T017 [P] 全包构建测试：`CGO_ENABLED=0 go build ./...` + `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`；跑 `specs/031/quickstart.md` 离线段验证
- [ ] T018 文档收口：`specs/031/quickstart.md` 按实测命令校对；若 trace 叠加有净增量，按维护者决定是否更新 README（031 保持 default-off，不主动改 README 基准）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (T001-T002)**: 无依赖，先做
- **Foundational (T003-T004)**: 依赖 T001；阻塞 US1/US2
- **US1 (T005-T007)**: 依赖 Foundational；MVP
- **US2 (T008-T011)**: 依赖 Foundational（T010 依赖 T009；T011 依赖 T010）
- **US3 (T012-T015)**: 依赖 US1+US2 完成
- **Polish (T016-T018)**: 依赖 US3 收口

### User Story Dependencies

- **US1（P1）**: Foundational 后独立可测——纯算法，零接线
- **US2（P1）**: Foundational 后独立可测——接线 + parity 门（不依赖 US1 正确性，仅依赖类型）
- **US3（P2）**: 依赖 US1+US2（需完整装配路径）

### Within Each User Story

- 测试（T005/T008）MUST 先写并 FAIL，再实现
- 类型 → 算法 → 渲染 → 接线 → 评测

### Parallel Opportunities

- T005 / T008 并行（同文件不同测试函数，单测可先写）
- T003 / T004 并行（类型与工具独立）
- US1 与 US2 在 Foundational 后并行（US2 仅依赖类型，不依赖 US1 算法正确性）

---

## Parallel Example: Foundational

```bash
# 类型定义与实体提取/词典可并行
Task T003: "定义 RelationEdge / StructuralContextBlock 于 cmd/locomo-bench/relation_graph.go"
Task T004: "实体提取 + 因果词典 于 cmd/locomo-bench/relation_graph.go"
```

## Implementation Strategy

### MVP First（US1 Only）

1. T001-T002 Setup
2. T003-T004 Foundational
3. T005-T007 US1（关系计算 + 单测）
4. **STOP 验证**：`go test ./cmd/locomo-bench -run TestRelationCompute` 绿 = MVP

### Incremental Delivery

1. Setup + Foundational → 类型/工具就绪
2. US1 → 关系计算可测（MVP）
3. US2 → 装配注入 + parity 门可测
4. US3 → 配对评测 → verdict

---

## Notes

- [P] tasks = 不同文件/独立，可并行
- 引擎零改动（T016）是 FR-008 硬门，任何实现阶段触碰 `memory/ embedding/ provider/ store/ internal/` 即失败
- 默认关 + parity：`--relation-context` 默认 false，关闭时 030 装配输出逐字节不变（SC-004）
- 配对纪律 008：同 store/子集/answerer/judge/cap；单次差值不算数，3 reps majority 为准
- 提交按任务组：每完成一个 phase 的 checkpoint 提交一次
