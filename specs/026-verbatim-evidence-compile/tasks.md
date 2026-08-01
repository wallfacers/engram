# Tasks: 查询期 verbatim 证据编译

**Input**: Design documents from
`specs/026-verbatim-evidence-compile/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md),
[research.md](./research.md), [data-model.md](./data-model.md),
[contracts/](./contracts/compile-arm-contract.md), [quickstart.md](./quickstart.md)

**Tests**: 本特性修改 benchmark harness 与编译策略(adapter 层)。引擎层(`memory/evidencecompiler`)
契约冻结不改(022 已交付、测试全绿);026 增量集中在 `cmd/locomo-bench/`,行为任务按 TDD
执行:先写并确认测试失败,再实现,再运行对应 package 与回归门。

**Organization**: Phase 3–5 按 spec 的三个 User Story 排列。026 是研究实验(默认关);
任何 arm 未通过配对消融前不进入默认路径(FR-011)。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可在不同文件上并行,且不依赖尚未完成的同阶段任务
- **[US1]–[US3]**: 对应 [spec.md](./spec.md) 的 User Story
- 每项都包含执行或产物的精确仓库路径

## Phase 1: Setup(承接 022 资产验证)

**Purpose**: 先确认 022 Compiler 引擎与 exact-token arm 可承接、当前基线可引用,避免在未验证资产上生成实现。

- [ ] T001 核实 022 交付的 `memory/evidencecompiler/` 全包测试与 `cmd/locomo-bench/compiler_eval.go` exact-token arm 测试通过(`CGO_ENABLED=0 go test -count=1 ./memory/evidencecompiler/... ./cmd/locomo-bench/`),把结果记录到 `specs/026-verbatim-evidence-compile/plan.md`
- [ ] T002 [P] 核对 `docs/research/high-scoring-memory-systems.md` 已吸收 Fidelity-Before-Structure(2601.00821)、Retain-or-Consolidate(2607.17545)与 Penfield audit(6.4% 答案键错误)三条证据,并把 026 的文献证据小节加入 `specs/026-verbatim-evidence-compile/research.md`
- [ ] T003 在算法改动前运行 `CGO_ENABLED=0 go build ./...` 与全量 Go tests,确认 022/025 收口后健康状态可追溯,记录到 `specs/026-verbatim-evidence-compile/plan.md`

**Checkpoint**: 026 基于已收口 022 的 Compiler 引擎与 exact-token arm;canonical 论文证据无已知错误;当前产品健康状态有可追溯记录。

---

## Phase 2: Foundational(基线可引用 + arms 契约冻结)

**Purpose**: 建立 026 可引用的 chunk_900 对照基线,冻结 compile-arm 契约与 arm 集合。

### Tests first — Foundational

- [ ] T004 [P] 为 compile-arm 契约(arm 集合、verbatim-first 双态 gate、fail-closed、zero-retrieval、candidate oracle 字段)写失败测试到 `cmd/locomo-bench/compiler_eval_test.go`
- [ ] T005 [P] 为 arm 输出确定性(同一 query+候选池 byte-replay 逐字节一致)与 arm 集合枚举写失败测试到 `cmd/locomo-bench/compiler_eval_test.go`
- [ ] T006 [P] 为 verbatim-first 双态 gate 写失败测试:原文装得下→KEEP/FETCH_SOURCE 保留原始 span;装不下→EXTRACT(按 relevance);EXTRACT 充分→拒绝 MERGE;EXTRACT 不充分→逐句验证 MERGE 到 `cmd/locomo-bench/compiler_eval_test.go`
- [ ] T007 [P] 为 fail-closed 写失败测试:无来源 ADD 拒绝、无效 citation 丢弃、超 cap 丢弃、退回 extractive、不调 answerer 到 `cmd/locomo-bench/compiler_eval_test.go`

### Measurement implementation

- [ ] T008 确认或建立 026 可引用的 chunk_900 对照基线(复用 024/025 的 `025-control-v2` 协议,evidence `01KYYK…`;若 022 accepted baseline 未收口,记录当前可引用分数与 run 位置到 `docs/evaluation/results.md`)
- [ ] T009 冻结 compile-arm 契约到 `specs/026-verbatim-evidence-compile/contracts/compile-arm-contract.md`(arm 集合、双态 gate、fail-closed、candidate oracle 报告要求),并确认与 022 `compiler-contract.md` 无冲突

**Checkpoint**: chunk_900 对照基线可引用;compile-arm 契约冻结;arm 集合确定(legacy_count/exact_token/extractive/verbatim_first)。

---

## Phase 3: User Story 1 — 查询期 verbatim 证据编译(优先级 P1)🎯 MVP

**Purpose**: 实现 verbatim-first 双态编译与 deterministic extractive arm,在固定候选池上离线 byte-replay 可用(无 LLM/embedding 依赖)。

### Tests first — US1

- [ ] T010 [P] [US1] 为 verbatim-first 编译写失败测试:原文能装入 cap 时 bundle 含原始 turn/span(KEEP/FETCH_SOURCE),候选 ID 与 chunk_900 基线逐字节一致到 `cmd/locomo-bench/compiler_eval_test.go`
- [ ] T011 [P] [US1] 为 verbatim-first 超 cap 分支写失败测试:按 relevance 顺序 EXTRACT 有来源 span;EXTRACT 仍不够才逐句验证 MERGE 到 `cmd/locomo-bench/compiler_eval_test.go`
- [ ] T012 [P] [US1] 为 compile-arm 关闭时零行为变化写失败测试:默认关时行为与 chunk_900 基线完全一致到 `cmd/locomo-bench/compiler_eval_test.go`

### Implementation — US1

- [ ] T013 [US1] 实现 deterministic extractive arm(按 relevance 顺序 EXTRACT 有来源 span,复用 022 `internal/extract` 的 raw-fit gate)到 `cmd/locomo-bench/compiler_eval.go`
- [ ] T014 [US1] 实现 verbatim-first arm(原文优先双态:原文装得下 KEEP/FETCH_SOURCE;装不下 EXTRACT/MERGE 逐句绑 source)到 `cmd/locomo-bench/compiler_eval.go`
- [ ] T015 [US1] 把 legacy-count 固化为正式 arm(现状按条数装填)到 `cmd/locomo-bench/compiler_eval.go`
- [ ] T016 [US1] 统一 arm 入口:同一冻结候选池 → 每 arm 输出确定性 bundle+trace,candidate oracle(gold 是否在池)记录到 `cmd/locomo-bench/compiler_eval.go`

**Checkpoint**: 离线 arms(legacy_count/exact_token/extractive/verbatim_first)在固定候选池上 byte-replay 可用;verbatim-first 原文优先双态验证;默认关时零行为变化。

---

## Phase 4: User Story 2 — Compiler arms 补齐并冻结(优先级 P1)

**Purpose**: 在 022 引擎契约上验证所有 arms 的 fail-closed 与确定性,确保"编译策略差异"的干净配对前提。

### Tests first — US2

- [ ] T017 [P] [US2] 为每 arm 的 zero-retrieval 写失败测试:arm 内无任何 Search/query 调用,SourceResolver 只按 frozen lineage ID 批量 Resolve 到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T018 [P] [US2] 为每 arm 的 fail-closed 集成写失败测试:无来源 ADD → 拒绝;无效 citation → 丢弃;invalid bundle → 不调 answerer、退回 extractive 到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T019 [P] [US2] 为 TokenCounter 对完整最终 prompt 重计写失败测试:cap 内、cap±1、chat-template 边界到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`

### Implementation — US2

- [ ] T020 [US2] 接线 formal B1 pipeline:固定候选池 → 各 arm → bundle → 一次 answerer(frozen protocol 下),关闭 legacy IDK retry 到 `cmd/locomo-bench/eval_runner.go`
- [ ] T021 [US2] 记录每 arm 的 Grounded Trace(Evidence Need、actions、span、source IDs、token 取舍、未满足 gap)到 `cmd/locomo-bench/compiler_eval.go`

**Checkpoint**: 所有 arms 在 formal 协议下 zero-retrieval、fail-closed、cap 合规;Trace 可审计。

---

## Phase 5: User Story 3 — 同 store 配对消融(优先级 P1)

**Purpose**: 在 022 冻结协议下对"各 arm vs chunk_900 基线"做同 store、候选逐字节一致的配对消融,确认 verbatim 编译是否带来信息密度增益且不回归基线。

### Tests first — US3

- [ ] T022 [P] [US3] 为配对有效性写失败测试:两臂同 store、候选逐字节一致(只差 arm);报告 candidate oracle 区分 compiler miss 与 candidate miss 到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T023 [P] [US3] 为分类别配对统计写失败测试:overall + multi-hop/temporal/single-hop/open-domain 分类别,exact McNemar,Holm non-regression 到 `cmd/locomo-bench/paired_eval_test.go`

### Implementation — US3

- [ ] T024 [US3] 在 022 冻结协议下跑配对消融(同 store、repeats≥3,需 LOCOMO_*/EMBED_*/JUDGE_* 端点或 AutoDL 评测箱):chunk_900 对照 vs legacy_count/exact_token/extractive/verbatim_first,LoCoMo 1,540 answerable,run-dir 记录
- [ ] T025 [US3] 跑 LongMemEval-S 500 配对(同协议),确认双基准共同过门(宪法 IV)
- [ ] T026 [US3] 报告分类别明细 + 配对统计 + token 记账 + candidate oracle;LoCoMo 6.4% 答案键噪声记录在案,小 delta 不单独作 promotion 依据到 `docs/evaluation/reports/verbatim-evidence-compile.md`
- [ ] T027 [US3] 写 benchmark-registration 到 `specs/026-verbatim-evidence-compile/benchmark-registration.md`;verdict 写入 `docs/evaluation/experiment-verdicts.md`

**Checkpoint**: 配对归因干净(只差 arm);verbatim-first 若同预算下相对 chunk_900 有可测 multi-hop/temporal 增益 → 评估进默认路径(需双基准共同过门);负收益 → FR-011 默认关并记录 verdict。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 收口 026:verdict 更新、默认关验证、文档同步。

- [ ] T028 确认所有 arm 默认关且关闭时双基准结果与基线一致(回归门对照)到 `docs/evaluation/reports/verbatim-evidence-compile.md`
- [ ] T029 更新 `docs/evaluation/experiment-verdicts.md` 与 `docs/research/high-scoring-memory-systems.md`(若文献证据小节有遗漏),收口 026 verdict
- [ ] T030 复核引擎未动:026 增量限定在 `cmd/locomo-bench/`(adapter 层);若触碰 `memory/ embedding/ provider/ store/ internal/` 须先说明为显式 contract increment(宪法 II)

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖,立即开始
- **Foundational (Phase 2)**: 依赖 Setup;BLOCKS 所有 User Stories
- **US1 (Phase 3)**: 依赖 Foundational;实现离线 arms(MVP)
- **US2 (Phase 4)**: 依赖 US1;formal 协议接线
- **US3 (Phase 5)**: 依赖 US2;配对消融
- **Polish (Phase 6)**: 依赖 US3;verdict 收口

### User Story 依赖

- **US1 (P1)**: Foundational 后可独立开始(MVP = 离线 arms 可用)
- **US2 (P1)**: 依赖 US1(需要 arm 实现才能接线 formal 协议)
- **US3 (P1)**: 依赖 US1+US2(需要 arms 完整才能配对)

### Parallel 机会

- Phase 1 T001/T002/T003:可并行
- Phase 2 T004–T007:可并行(不同测试文件/断言)
- Phase 3 T010–T012:可并行(不同失败测试)
- Phase 4 T017–T019:可并行
- Phase 5 T022/T023:可并行(配对工具 vs 配对统计)

## 实现策略

**MVP first**: Phase 3(US1)完成即达 MVP——离线 arms byte-replay 可用、verbatim-first 双态验证、默认关零行为变化。这证明"候选内已有证据可被编译成更可回答的 bundle"(无 LLM 依赖)。Phase 4–5 才是正式配对消融。

**增量交付**: 每 arm 独立提交(legacy_count → exact_token 已有 → extractive → verbatim_first),配对结果独立登记。
