# Tasks: 跨消息语义聚类 episode 表示

**Input**: Design documents from `specs/025-semantic-episode-cluster/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/

**Tests**: 本 feature 是引擎行为研究实验(宪法开发工作流:测试先行)。所有引擎行为变更先写失败测试,再实现。renderer 已由 022 交付测试,US2 补的是跨 session 能力失败测试。

**Organization**: Tasks are grouped by user story to enable independent implementation and testing of each story.

## Format: `[ID] [P?] [Story] Description`

- **[P]**: Can run in parallel (different files, no dependencies on incomplete tasks)
- **[Story]**: Which user story this task belongs to (e.g., US1, US2, US3)
- Include exact file paths in descriptions

## Path Conventions

- 引擎能力在 `memory/`;eval harness 在 `cmd/locomo-bench/`(见 plan.md Project Structure)

---

## Phase 1: Setup

**Purpose**: 确认工作区与基线

- [ ] T001 确认当前分支为 `025-semantic-episode-cluster`、`git status --short` 无来源不明改动、`CGO_ENABLED=0 go build ./...` 零错误;shell 内 `export SPECIFY_FEATURE_DIRECTORY=specs/025-semantic-episode-cluster`

**Checkpoint**: 基线绿,可开始实现

---

## Phase 2: Foundational

**Purpose**: 本 feature 无新增共享基础设施——022 已交付 episode 引擎、v7 表、eval 架构,直接复用。无阻塞性前置任务(承接资产核实并入 US2)。

---

## Phase 3: User Story 2 - 022 episode 引擎验证补全,承接资产可信 (Priority: P1)

**Goal**: 核实 022 交付的 episode 引擎单测覆盖设计且通过;确认"`RebuildSession` 未在正式 eval 接线"的真实缺口;为跨 session 语义聚类补失败测试(红),钉住构建链路

**Independent Test**: ① 核实 `memory/episode_test.go`(13 tests)、`cmd/locomo-bench/representation_eval_test.go`(14 tests)、`representation_integration_test.go`(6 tests)覆盖 022 设计且全绿;② 跨 session 聚类失败测试在实现前红、Phase 4 实现后绿

### 承接资产核实(单测已存在,非补写)

- [ ] T002 [US2] 核实 022 交付的 episode/渲染单测覆盖设计与通过:`memory/episode_test.go`、`cmd/locomo-bench/representation_eval_test.go`、`representation_integration_test.go`,确认 `RebuildSession` 全仓库零调用(仅 `memory/episode.go` 定义处与测试),接线缺口结论与 `research.md` R0、spec US2 一致;跑 `CGO_ENABLED=0 go test -count=1 ./memory/ ./cmd/locomo-bench/` 全绿

### Tests for User Story 2(TDD — 先红后绿) ⚠️

> **NOTE: Write these tests FIRST, ensure they FAIL before Phase 4 implementation**

- [ ] T003 [P] [US2] 写跨 session 语义聚类失败测试到 `memory/semantic_cluster_test.go`:两个不同 `source_session_id` 的 Evidence 共享实体/关键词 → SemanticClusterer 聚成同一 episode;语义不相关的跨 session Evidence → 不聚;每 episode 证据数上限生效(超上限确定性截断);无 embedding 端点时纯离线信号可判定;config-hash 幂等重建 digest 稳定(research.md R1/R3/R4)
- [ ] T004 [P] [US2] 扩展 `memory/episode_test.go`:为 `EpisodeStore.RebuildAll`(跨 session)写失败测试——跨 session Evidence 聚成同 episode 投影且 lineage source_order 连续、同 config 重建删旧建新 revision 递增、空 Evidence 返回空结果、删除不删 Evidence(research.md R2)

**Checkpoint**: US2 失败测试红,证明跨 session 能力缺失;实现后(Phase 4)转绿

---

## Phase 4: User Story 1 - 跨消息语义聚类引擎 (Priority: P1)

**Goal**: 实现 `SemanticClusterer`(跨 session 语义聚类,离线信号为主、embedding overlay 可选)+ `EpisodeStore.RebuildAll`(全量可重建、config-hash 幂等);让 US2 失败测试转绿

**Independent Test**: `CGO_ENABLED=0 go test -count=1 ./memory/` 全绿;T003/T004 失败测试全部转绿;跨 session 相关证据可聚、不相关不聚、有界、幂等、纯离线可判定

### Implementation for User Story 1

- [ ] T005 [US1] 实现 `SemanticClusterer` 接口与离线实现到 `memory/semantic_cluster.go`:`Cluster(ctx, []memory.Evidence) ([]EpisodeCluster, error)`,离线信号 = 共享实体(复用 `memory_entities` entity_norm 归一化)OR 共享关键词 Jaccard ≥ `minKeywordJaccard`(默认 0.25),任一达成即聚;输出有界确定性排序证据簇(research.md R1/R3,contracts/engine-api.md §2)
- [ ] T006 [P] [US1] 实现可选 embedding overlay 到 `memory/semantic_cluster.go`:`NewHybridClusterer(opts, embedder)`,embedding cosine ≥ `embedThresh`(默认 0.9)作为额外聚信号;`embedder == nil` 时退化为离线(typed-nil 折叠,CLAUDE.md 纪律);默认走 `NewOfflineClusterer`(research.md R3)
- [ ] T007 [US1] 实现 `EpisodeStore.RebuildAll(ctx, clusterer, builderVersion, configHash)` 到 `memory/episode.go`:遍历 namespace 内全部 active Evidence(跨 session),用 clusterer 聚类,复用 `buildEpisodeTx` 写 `semantic_episode` projection + lineage(source_order 从 0 连续);同 config 删旧建新、revision 递增;`clusterer == nil` 返回 `ErrEpisodeClustererRequired`;跳过 RebuildSession 的同 session 连续校验(research.md R2,contracts/engine-api.md §3)
- [ ] T008 [P] [US1] 实现 `EpisodesForEvidence(ctx, evidenceIDs) (map[string][]Projection, error)` 到 `memory/episode.go`:按 evidence_id 反查引用它的 active `semantic_episode` projections,批量化无 N+1,确定性排序;无命中返回空 map(渲染 fallback)(research.md R5,contracts/engine-api.md §4)
- [ ] T009 [US1] 实现聚类判定统计审计到 `memory/semantic_cluster.go`:`decisions / clusters / suspected_mis`(疑似 = 被有界截断或边缘阈值),输出供 run 侧 JSON artifact(对齐 024 suppression-audit;FR-006,research.md R6)
- [ ] T010 [US1] 验证:Phase 3 的 T003/T004 失败测试全部转绿;`CGO_ENABLED=0 go test -count=1 ./memory/` 与 `CGO_ENABLED=0 go build ./...` 通过

**Checkpoint**: US1 引擎能力(跨 session 语义聚类)完整可用、可测试;US2 验收达成

---

## Phase 5: User Story 1 - eval 接线:semantic_episode 臂构建真实 episode (Priority: P1)

**Goal**: `--episode-cluster` flag(默认关)接线;渲染前 RebuildAll 构建跨 session episode;semantic_episode 臂渲染真实 episode 而非 fallback;关闭时零行为变化

**Independent Test**: `--episode-cluster` 关时 artifact digest 与现状一致(FR-003);开时 semantic_episode 臂渲染真实 episode narrative;answerer 调用数不增

### Implementation for User Story 1 (eval 接线)

- [ ] T011 [P] [US1] 添加 `--episode-cluster` flag(默认 false)与阈值 flags(`--min-keyword-jaccard` 默认 0.25、`--embed-thresh` 默认 0.9、`--max-evidence-per-episode` 默认 8)到 `cmd/locomo-bench/main.go`;`--episode-cluster` 开启时才创建 clusterer 与调 RebuildAll(闭合时零副作用)(research.md R3/R4)
- [ ] T012 [US1] 接线到 `cmd/locomo-bench/eval_runner.go`:构建 conversation store 后、渲染前,若 `--episode-cluster` 开启则对该 store 调 `EpisodeStore.RebuildAll`(离线或 hybrid clusterer),缓存 episode 投影供渲染路由(与 024 neighbor_extend 的"候选冻结后扩展"不同——这是渲染前构建投影,不改变检索)
- [ ] T013 [US1] 扩展 `semanticEpisodeRenderer` 路由到 `cmd/locomo-bench/representation_eval.go`:anchor(fact/chunk)→ 经 `SourcesByProjectionIDs` 取 evidence lineage → `EpisodesForEvidence` 定位所属 episode → 渲染 episode narrative;未命中 episode 时保持现有 fallback(直读 anchor source),零行为变化;不改 renderer 公开签名(research.md R5)
- [ ] T014 [P] [US1] 写零行为变化测试到 `cmd/locomo-bench/representation_eval_test.go`:`--episode-cluster` 关时 semantic_episode 臂 artifact digest 与现状一致(FR-003);`--episode-cluster` 开时渲染真实 episode narrative(跨 session evidence 进候选)
- [ ] T015 [US1] 扩展集成测试到 `cmd/locomo-bench/representation_integration_test.go`:全 bake-off flow 下 `--episode-cluster` 开时 semantic_episode 臂渲染 episode narrative、answerer 调用数与 control 相等、关闭时与现状一致

**Checkpoint**: US1 完整(引擎 + eval 接线),可进入 US3 配对验证

---

## Phase 6: User Story 3 - 双基准配对验证,语义 episode 不回归基线 (Priority: P1)

**Goal**: 022 冻结协议(cap 3600,repeats≥3)下,chunk_900 基线 vs semantic_episode(--episode-cluster)配对消融;multi-hop 预期增益,不回归 overall;负结果按 FR-010 记录 verdict

**Independent Test**: LoCoMo(1540 题)与 LongMemEval-S(500 题)配对,分类别明细 + 配对统计 + token 记账齐全;表示差异与检索差异显式分离(同 anchor)

### Implementation for User Story 3

- [ ] T016 [US3] 冻结配对 eval-config 到 022 协议 manifest:`episode_cluster / cluster_signals(min_keyword_jaccard, embed_thresh) / max_evidence_per_episode` 字段加入 protocol manifest,重算 `protocol_hash`;独立提交(评估口径变更与算法改动分开,宪法 IV)(data-model.md Evaluation Entities)
- [ ] T017 [US3] 在 AutoDL 上跑 LoCoMo 配对:chunk_900 基线 vs `--representation semantic_episode --episode-cluster`,repeats≥3,`setsid` 分离 + 轮询(WSL2 硬规则);judge 走 DeepSeek API;记录 suppression 审计(聚类判定统计)到 run 侧 artifact
- [ ] T018 [US3] 跑 LongMemEval-S 配对验证(同协议、同 cap);四臂状态与 024 分开记(本 feature 只改 semantic_episode 一臂的 source closure)
- [ ] T019 [US3] 汇总结果与 verdict 到 `specs/025-semantic-episode-cluster/benchmark-registration.md` + `docs/evaluation/reports/semantic-episode-cluster.md`:分类别(multi-hop 重点)accuracy、配对统计(复用 022 `paired_eval.go` exact McNemar + promotion rule,research.md R7)、token 记账、表示/检索差异分离说明;**跨消息证据覆盖度量**(episode 开启前后,multi-hop 题 answer-context 内含 >1 个 source_session 的证据占比,SC-001);负结果如实记录(FR-010)
- [ ] T020 [US3] 判定:multi-hop 相对 chunk_900 基线有可测提升且 overall 不显著回归 → 记录为候选;无收益或负收益 → 机制保持默认关并记录 verdict(FR-010/FR-011);`--episode-cluster` 不进入默认路径,除非双基准配对通过

**Checkpoint**: 宪法 IV 回归门结论明确;verdict 记录在案

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 文档与收尾

- [ ] T021 更新 `specs/025-semantic-episode-cluster/quickstart.md` 与 spec.md 使实际 flags/验证路径与实现一致(若实现偏离 plan 中预设)
- [ ] T022 清理临时文件(scratchpad 外的 run 日志/下载移入 session scratchpad);确认 git 提交序列(US2 测试 → US1 引擎 → US1 接线 → US3 结果+verdict)各自独立提交,engine 未被 adapter 改动污染(`git diff --name-only -- memory embedding provider store internal` 对齐预期)

**Checkpoint**: feature 收尾,可进入 /speckit-analyze 交叉一致性审查

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: No dependencies - can start immediately
- **Foundational (Phase 2)**: 无阻塞任务(承接资产已存在)
- **User Stories (Phase 3+)**: US2(Phase 3)先写失败测试 → US1 引擎(Phase 4)实现转绿 → US1 eval 接线(Phase 5) → US3(Phase 6)配对验证
- **Polish (Phase 7)**: Depends on all user stories complete

### User Story Dependencies

- **US2 (P1)**: T002 核实承接资产 → T003/T004 失败测试(红)
- **US1 (P1)**: 引擎(Phase 4)依赖 US2 失败测试(红)作为 TDD 靶子;eval 接线(Phase 5)依赖引擎完成
- **US3 (P1)**: 依赖 US1 eval 接线完成;与 024 配对口径共享 022 冻结协议

### Within Each User Story

- Tests MUST be written and FAIL before implementation(T003/T004 先红)
- SemanticClusterer(T005/T006)先于 RebuildAll(T007)——RebuildAll 依赖 clusterer
- RebuildAll(T007)先于 EpisodesForEvidence(T008)与 eval 接线(T012/T013)
- 引擎(memory/)先于 eval(cmd/locomo-bench/)

### Parallel Opportunities

- T003 与 T004 不同文件(`semantic_cluster_test.go` / `episode_test.go`),可并行
- T005 与 T006 不同文件内独立功能,可并行(同一文件建议顺序)
- T011 与 T014 不同文件(`main.go` / `representation_eval_test.go`),可并行
- T017 与 T018 不同 benchmark(LoCoMo / LongMemEval-S),可并行(需各自 eval box 或串行)

---

## Parallel Example: User Story 2 (Tests First)

```bash
# Launch both failure-test tasks together (different files):
Task: "Write cross-session semantic clustering failure tests in memory/semantic_cluster_test.go"
Task: "Extend memory/episode_test.go with EpisodeStore.RebuildAll cross-session failure tests"
```

---

## Implementation Strategy

### MVP First (US1 + US2 完成即可独立交付)

1. Complete Phase 1: Setup
2. Complete Phase 3: US2(核实 + 失败测试红)
3. Complete Phase 4: US1 引擎(失败测试转绿)——**STOP and VALIDATE**: `go test ./memory/` 全绿
4. Complete Phase 5: US1 eval 接线(零行为变化验证)
5. 可先跑一个小 slice(如 LoCoMo cat 1)验证 multi-hop 方向,再进入全量 US3

### Incremental Delivery

1. Setup → US2 失败测试(红)→ US1 引擎(绿)→ US1 eval 接线 → US3 配对验证 → 报告
2. 每阶段独立提交,验证独立完成

### Parallel Team Strategy

单开发(维护者 + agent):按 Phase 顺序推进;T003/T004、T005/T006 等 [P] 任务可交由外部 agent 并行

---

## Notes

- [P] tasks = different files, no dependencies
- [Story] label maps task to specific user story for traceability
- **引擎不可触碰纪律(CLAUDE.md)**:eval 接线(Phase 5)只在 `cmd/locomo-bench/` 与公开 API 层面,不修改 `memory/` 内部算法;跨 session 能力全部通过新增公开 API(RebuildAll/EpisodesForEvidence)暴露
- Verify tests fail before implementing(T003/T004 红)
- Commit after each task or logical group(测试 → 引擎 → 接线 → 结果)
- **宪法 IV 硬门**:Phase 6 配对验证未完成前,不得把 `--episode-cluster` 或语义聚类当作默认路径
- 长评测用 `setsid` 分离 + 轮询,日志/凭据只进 session scratchpad
