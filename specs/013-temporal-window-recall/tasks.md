# Tasks: Retrieval-Side Temporal Window Recall

**Feature**: `013-temporal-window-recall` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

**Input**: plan.md · spec.md · research.md · data-model.md · contracts/ · quickstart.md

**Tests**: TDD 强制(CLAUDE.md 硬规:引擎行为改动先写失败测试)。每个引擎能力任务前置一个失败测试任务。

## 门控铁律(贯穿全程)

- **US1 是硬门**:诊断 GO 才进 US2。任一层 NO-GO → 止损,落 `docs/locomo-score-levers.md` 收口,**不建引擎机制**。
- **US1 零引擎改动**:纯适配器 + 既有引擎只读 API。US1 阶段 `git diff --name-only -- memory embedding provider store internal` 必须为空。
- **US2 条件于 US1 GO**;**US3 条件于 US2 完成**。
- **008 铁律**:coverage 仅诊断,最终 verdict 以端到端答题分为准(US3)。
- **无云 rerank 死规则** + **eval-config 与算法分开 commit**(宪法 IV)。

---

## Phase 1: Setup

- [ ] T001 确认基线绿:`CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./memory/... ./cmd/locomo-bench/...` 全绿,记录当前 `git rev-parse HEAD` 作为对照锚(用于末尾隔离核查)。
- [ ] T002 确认固化 bge-large store 与 LoCoMo 数据集可用(`docs/locomo-e2e-eval-reproduction.md` canonical recipe);记录 store 路径与 temporal 类问题计数(category=temporal)到 `specs/013-temporal-window-recall/quickstart.md` 的实际路径备注。

---

## Phase 2: Foundational (blocking prerequisites)

无跨 US 共享的阻塞性引擎前置(存储底座 migration v4 已就绪、helper 已存在)。US1 完全独立,US2/US3 各自门控。**本 phase 空**——直接进 US1。

---

## Phase 3: User Story 1 — 免费召回诊断门 (P1) 🎯 MVP + 硬门

**Goal**: 零答题/judge token 的 retrieval-only 四层诊断,判定 temporal 82.24% 短板病因归属,产出 GO/NO-GO。US1 即便机制从不实现,也独立交付诚实方向判定。

**Independent Test**: 在固化 bge-large store 上跑 `--temporal-diagnostic`,输出四层表 + 判定,全程零答题/judge token;`git diff -- memory embedding provider store internal` 为空。

### Tests (TDD, adapter)

- [ ] T003 [P] [US1] 写失败测试 `cmd/locomo-bench/temporal_diagnostic_test.go`:断言四层度量函数——Layer 0 `parse_coverage`(对样本 query 集调 `memory.ParseTemporalIntent` 统计 ok 占比)、Layer 1 `event_date_coverage`(样本事实 event_start/end/date 非空占比)、Layer 2 `buried_ratio`(gold rank 落候选池 cutoff 外比例)、Layer 3 `oracle_lift@30`(纯 event∈window 拉取把深埋 gold 抬进 top-30 的计数)。用内存 store + stub 向量,断言各层数值与手算一致。
- [ ] T004 [P] [US1] 写失败测试:断言 GO/NO-GO 判据函数——四层全过→GO;逐层单独塌→NO-GO 且 `cause` 分别为 `解析器`/`抽取侧`/`非召回瓶颈`/`天花板不足`。

### Implementation (adapter, zero engine change)

- [ ] T005 [US1] 实现 `cmd/locomo-bench/temporal_diagnostic.go`:`runTemporalDiagnostic`,复用 012 `runDoc2QueryRecallDiagnostic` 同构基建,只调既有引擎只读 API(`Retriever.Search`/`EntriesByName`/`List`/`memory.ParseTemporalIntent`);Layer 3 oracle 用 `ParseTemporalIntent` 窗 + 读事实 event 时间在适配器内做纯 event∈window 拉取(measurement,非引擎算法)。零答题/judge token。
- [ ] T006 [US1] 在 `cmd/locomo-bench/main.go` 接线 `--temporal-diagnostic` flag（+ `--store-dir`/`--data`/`--run-dir` 复用既有），dispatch 到 `runTemporalDiagnostic`;输出四层表 + 末行 `GO`/`NO-GO(cause=…)`。断言 extractNever（诊断期不调 LLM 抽取）。
- [ ] T007 [US1] `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/...` 全绿;`git diff --name-only -- memory embedding provider store internal` **确认为空**(隔离硬门)。

### Gate (run diagnostic)

- [x] T008 [US1] 跑门(box bge-large @8001,`009-bge-chunks-store`,n=321)。**NO-GO(cause=解析器)**:L0 parse_coverage **0.196(63/321)** < 0.50 首层败;L1 event_date_coverage 0.773 ✓、L2 buried_ratio 0.140(rank_pool p50=64 p90=155)✓、L3 oracle_lift@30 0.333(6/18)✓。归因:召回臂被只点火 19.6% 的 `ParseTemporalIntent` 门控,对 80% temporal 题永不点火,端到端天花板 ≈ 抬 6 条 gold(舍入误差)。**止损:不进 Phase 4/5**。verdict 落 `docs/locomo-score-levers.md` Feature 013。产物 `.locomo-run/013-temporal-diag/`。

**Checkpoint US1**: ✅ 诊断可跑、四层表产出、判定明确(NO-GO/解析器)、引擎零改。**门未过 → feature 在此止损收口。**

---

> **⛔ Phase 4/5 CANCELLED（门① NO-GO，2026-07-24）**：US1 诊断 cause=解析器,召回臂前提(点火率)不成立。T009–T018 **未实现**。temporal 真杠杆前移到 query 侧时间解析覆盖(独立 feature),event_date 召回臂留待解析覆盖上去后再评估。

## Phase 4: User Story 2 — 时间窗召回臂进 RRF 第4路 (P2)【条件于 US1 GO】

**Goal**: 新增独立召回臂——`NamesByEventWindow` 把时间窗内事实(含深埋池外的)拉进池,产出 rank list 作 RRF 平权第 4 路,把深埋 temporal gold 抬进 top-K。默认关、降级安全、parity 不破。

**Independent Test**: 引擎单测(offline,free)——范围查询正确性 + 臂关 byte-identical + 无意图/无边界降级 + 深埋 gold 抬升 + 乘子交互无害。

### Tests (TDD, engine) — contracts/ 的 test obligations

- [ ] T009 [P] [US2] 写失败测试 `memory/entrystore_test.go`(依 `contracts/engine-namesbyeventwindow.md`):`NamesByEventWindow` —— (1) 相交/部分相交/窗外三类只返回前两类;(2) 仅 event_date 回退;(3) 半开区间(start 零值 / end 零值 / 双零值→空);(4) 无 event 时间的事实排除且空非错误;(5) 排序确定性(gap 升序 + name 次级序,多次调用 byte-identical)。
- [ ] T010 [P] [US2] 写失败测试 `memory/retriever_test.go`(依 `contracts/retriever-temporal-arm.md`):(1) parity——`TemporalScore=false` 或无时间意图 query,结果序与臂未引入时逐字节相同;(2) 深埋 gold 抬升——构造语义排名在池 cutoff 外、event_date 落窗内的事实 + 带时间意图 query,臂开启后进 top-K(关时不进);(3) 降级—无意图;(4) 降级—无边界;(5) 乘子交互无害(臂+`applyTemporal` 同开,相对序断言);(6) tuning-free——RRF k 常量未变、无新 temporal 权重 option。

### Implementation (engine: memory/entrystore.go + memory/retriever.go ONLY)

- [ ] T011 [US2] 实现 `memory/entrystore.go` 的 `NamesByEventWindow(ctx, start, end) ([]string, error)`:`WHERE event_end >= ? AND event_start <= ?` 范围查询(命中 v4 索引),边界回退按 data-model 规约(start/end 空→event_date;皆空→排除),半开区间某侧无界该侧条件恒真、双零值返回空,结果按 gap 升序 + name 次级序。**不新增 migration。**
- [ ] T012 [US2] 实现 `memory/retriever.go` 的 `temporalRecallRanks(ctx, window)`(调 `NamesByEventWindow` → `ranksFromOrder`,err→`slog.Warn`+nil map)并在 `SearchWithDiagnostics` 现有 `temporal != nil` 分支处 `signals = append(signals, ...)`(`fuseRRF` 之前);保留 `applyTemporal` 乘子不动(Fork A)。
- [ ] T013 [US2] `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./memory/...` 全绿(T009/T010 由失败转通过)。
- [ ] T014 [US2] parity 硬门:`CGO_ENABLED=0 go test -count=1 ./...` 全绿,含 `testdata/parity/` 确定性 golden 与跨 namespace 隔离测试;`git diff --name-only -- memory embedding provider store internal` **只**反映 `memory/entrystore.go`+`memory/retriever.go`(+ 二者 `_test.go`),`store/`/schema 零改。

**Checkpoint US2**: 召回臂落地、六项契约测试绿、parity 不破、引擎面积符合契约。

---

## Phase 5: User Story 3 — 端到端配对 eval (P3)【条件于 US2 完成】

**Goal**: box repeats≥3 配对 eval 证 temporal 类答题分真涨且总分不回归,以答题分为准(008 铁律)。宪法 IV 硬门。

**Independent Test**: 臂 off/on 配对(repeats=3),temporal 类 McNemar,非 temporal 类总分不回归。

- [ ] T015 [US3] 在 `cmd/locomo-bench` 接线 `--temporal-arm {off|on}` eval flag(off = `TemporalScore` 组合下臂不接;on = 臂接入),复用既有 store/repeats/judge 接线;**此 flag 改动单独 commit**(eval-config 与算法分离,宪法 IV / FR-016)。
- [ ] T016 [US3] box 前置:启 vllm 答题栈(Qwen @8000)+ bge-large @8001 + SSH 隧道 + JUDGE_* env;凭据仅 env/隧道不落盘。跑臂 off/on 配对 repeats=3(WSL2 `setsid` detach,日志→scratchpad)。**空闲必停。**
- [ ] T017 [US3] 配对分析:temporal 类答题分 on vs off(McNemar)+ 非 temporal 各类不回归核查;判 GO(涨且噪声带外且不污染)/ within-noise / 污染 NO-GO。
- [ ] T018 [US3] box teardown(vllm 杀、GPU 0 MiB 核实、隧道拆、askpass 清);结论(GO 或 NO-GO)落 tracked `docs/locomo-score-levers.md` Feature 013 段;写/更新 memory verdict 指针(`verdicts go to tracked docs` 纪律)。

**Checkpoint US3**: 端到端 verdict 落地,诚实收口(赢则出货路径明确;NO-GO 则病因收口不计赢)。

---

## Phase 6: Polish & Cross-Cutting

- [ ] T019 更新 `specs/013-temporal-window-recall/tasks.md` 各任务勾选与结果注记(仿 012 收口体例:[x]/[~] + 一行结论)。
- [ ] T020 若 US2/US3 GO:更新 `docs/memory-strategy.md` 附二短板2 P0 行状态(检索侧时间窗 → 已验证/已出货);若 NO-GO:记录证伪结论与病因,保留机制为默认关能力(不计赢)。

---

## Dependencies & Execution Order

```
Phase 1 (Setup: T001-T002)
        ▼
Phase 3 US1 硬门 (T003-T008)  ── NO-GO ──► 止损收口(T008 末),feature 结束
        │ GO
        ▼
Phase 4 US2 引擎机制 (T009-T014)  [条件于 US1 GO]
        ▼
Phase 5 US3 端到端 (T015-T018)  [条件于 US2 完成]
        ▼
Phase 6 收口 (T019-T020)
```

**Story 依赖**:US1 独立(纯诊断,零引擎);US2 依赖 US1 GO;US3 依赖 US2。三者严格串行门控(非并行——这是止损设计,不是吞吐设计)。

## Parallel Opportunities

- **US1 内**:T003 ∥ T004(不同断言,同文件不同函数——若同文件则顺序写,标 [P] 表示逻辑独立)。
- **US2 内**:T009 ∥ T010(`entrystore_test.go` vs `retriever_test.go`,不同文件,真并行)。
- 跨 US **无并行**(门控串行)。

## Independent Test Criteria (per story)

- **US1**: 四层诊断表产出 + GO/NO-GO 明确 + 引擎零改(`git diff -- memory embedding provider store internal` 空)+ 零答题 token。
- **US2**: 六项契约测试绿 + parity byte-identical + 引擎面积仅 entrystore/retriever + 深埋 gold 可抬升。
- **US3**: 配对 McNemar 出 verdict + 非 temporal 类不回归 + 结论落 tracked docs。

## Suggested MVP Scope

**MVP = US1(Phase 1 + 3)**。US1 单独交付一个诚实的方向判定(GO/NO-GO + 病因归属),即便 US2/US3 从不实现也有价值——这正是"先跑免费召回诊断门"的止损设计。US2/US3 是条件增量。

## Format Validation

所有任务遵循 `- [ ] [TaskID] [P?] [Story?] 描述 + 文件路径`:Setup/Foundational/Polish 无 Story 标签;US 任务带 [US1/US2/US3];[P] 仅标真并行(不同文件、无未完成依赖)。
