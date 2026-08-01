# Tasks: 查询期 verbatim 证据编译

**Input**: Design documents from
`specs/026-verbatim-evidence-compile/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md),
[research.md](./research.md), [data-model.md](./data-model.md),
[contracts/](./contracts/compile-arm-contract.md), [quickstart.md](./quickstart.md)

**Tests**: 本特性修改 benchmark harness(adapter 层)。引擎层(`memory/evidencecompiler`)
契约冻结不改,verbatim-first(原文优先双态 + MERGE 双条件 gate)已由 022 完整实现且测试
全绿(`TestExtractionPlanKeepsRawCanonicalEvidenceWhenRawFits` /
`TestExtractionPlanUsesExactSentenceSpansOnlyWhenRawDoesNotFit` /
`TestMergeGateRequiresRawOverCapAndExtractiveInsufficiency` /
`TestCompileUsesRawWhenItFitsAndNeverCallsPlannerCompression`)。**026 的真实增量是
验证与配对消融**:把 `--compiler-arm extractive`(verbatim-first)与 `--compiler-arm
exact_token` 放到 022 冻结协议(formal B1)下,与 chunk_900 基线**同 store、候选逐字节
一致**配对,确认 query-time verbatim 编译是否同预算优于 write-time 固定 chunk。

**Organization**: Phase 3–5 按 spec 的三个 User Story 排列。026 是研究实验(默认关);
任何 arm 未通过配对消融前不进入默认路径(FR-011)。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可在不同文件上并行,且不依赖尚未完成的同阶段任务
- **[US1]–[US3]**: 对应 [spec.md](./spec.md) 的 User Story
- 每项都包含执行或产物的精确仓库路径

## Phase 1: Setup(承接 022 资产验证)

**Purpose**: 核实 022 Compiler 引擎(verbatim-first 已实现)+ exact-token arm 可承接、当前基线可引用,避免在未验证资产上重复实现。

- [ ] T001 核实 022 交付的 `memory/evidencecompiler/` 全包测试与 `cmd/locomo-bench/compiler_eval.go` exact-token arm 测试通过(`CGO_ENABLED=0 go test -count=1 ./memory/evidencecompiler/... ./cmd/locomo-bench/`),并确认 verbatim-first 双态(原文优先 + MERGE 双条件)已在引擎实现(读 `internal/extract/extract.go` 的 `BuildExtractionPlan`/`SelectPackingItems`/`MergePermitted`),把结论记录到 `specs/026-verbatim-evidence-compile/plan.md`
- [ ] T002 [P] 核对 `docs/research/high-scoring-memory-systems.md` 已吸收 Fidelity-Before-Structure(2601.00821)、Retain-or-Consolidate(2607.17545)与 Penfield audit(6.4% 答案键错误)三条证据,并把 026 的文献证据小节加入 `specs/026-verbatim-evidence-compile/research.md`
- [ ] T003 在算法改动前运行 `CGO_ENABLED=0 go build ./...` 与全量 Go tests,确认 022/025 收口后健康状态可追溯,记录到 `specs/026-verbatim-evidence-compile/plan.md`

**Checkpoint**: 026 基于已收口 022 的 Compiler 引擎(verbatim-first 已实现)+ exact-token arm;canonical 论文证据无已知错误;当前产品健康状态有可追溯记录。

---

## Phase 2: Foundational(基线可引用 + 配对工具验证)

**Purpose**: 建立 026 可引用的 chunk_900 对照基线,验证 formal B1 下 `--compiler-arm` 的接线完整、配对工具可用。

### Tests first — Foundational

- [ ] T004 [P] 为 formal B1 下 `--compiler-arm` 接线写失败测试:extractive/planner/exact_token 三臂在同一冻结候选池上 byte-replay 确定性、zero-retrieval、fail-closed(无来源 ADD 拒绝、无效 citation 丢弃、退回 extractive)到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T005 [P] 为配对有效性写失败测试:两臂同 store、候选逐字节一致(只差 arm),report candidate oracle(gold 是否在池)到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T006 [P] 为分类别配对统计写失败测试:overall + multi-hop/temporal/single-hop/open-domain 分类别,exact McNemar,Holm non-regression 到 `cmd/locomo-bench/paired_eval_test.go`

### Measurement implementation

- [ ] T007 确认或建立 026 可引用的 chunk_900 对照基线(复用 024/025 的 `025-control-v2` 协议,evidence store `01KYYK…`;若 022 accepted baseline 未收口,记录当前可引用分数与 run 位置到 `docs/evaluation/results.md`)
- [ ] T008 冻结 compile-arm 契约到 `specs/026-verbatim-evidence-compile/contracts/compile-arm-contract.md`(arm 集合、双态 gate 已由引擎实现、fail-closed、candidate oracle 报告要求),并确认与 022 `compiler-contract.md` 无冲突

**Checkpoint**: chunk_900 对照基线可引用;formal B1 下 compiler arms 接线验证通过;配对工具可用。

---

## Phase 3: User Story 1 — 验证 verbatim-first 编译可用(优先级 P1)🎯 MVP

**Purpose**: 确认 `--compiler-arm extractive`(verbatim-first)与 `--compiler-arm exact_token` 在 formal B1 冻结协议下可用:同一候选池 byte-replay、默认关零行为变化、fail-closed 生效。**不实现新机制**(引擎已实现)。

### Tests first — US1

- [ ] T009 [P] [US1] 为 verbatim-first 臂在 formal B1 下可用写失败测试:原文能装入 cap 时 bundle 含原始 turn/span(KEEP/FETCH_SOURCE),候选 ID 与 chunk_900 基线逐字节一致到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T010 [P] [US1] 为 verbatim-first 超 cap 分支写失败测试:按 relevance 顺序 EXTRACT 有来源 span;EXTRACT 仍不够才逐句验证 MERGE 到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T011 [P] [US1] 为 compile-arm 关闭时零行为变化写失败测试:默认关(`--compiler-arm` 未设)时行为与 chunk_900 基线完全一致到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`

### Implementation — US1

- [ ] T012 [US1] 确认 formal B1 下 `--compiler-arm extractive/exact_token` 的 arm 冻结、绑定与 fail-closed 校验在 `cmd/locomo-bench/eval_runner.go`(formalTreatmentFreezeOptions / compileFormalSources / compileExactTokenArm)完整,缺失处补齐(不重写引擎)
- [ ] T013 [US1] 记录每 arm 的 Grounded Trace(Evidence Need、actions、span、source IDs、token 取舍、未满足 gap)到 `cmd/locomo-bench/compiler_eval.go`,确认可审计

**Checkpoint**: `--compiler-arm extractive/exact_token` 在 formal B1 下可用、byte-replay 确定性、默认关零行为变化;Trace 可审计。**MVP = 证明"候选内已有证据可被编译成更可回答的 bundle"的验证前提成立。**

---

## Phase 4: User Story 2 — 配对消融前检查(优先级 P1)

**Purpose**: 在 022 冻结协议下,对"各 arm vs chunk_900 基线"做同 store、候选逐字节一致的配对消融,确认 verbatim 编译是否带来信息密度增益且不回归基线。

### Tests first — US2

- [ ] T014 [P] [US2] 为配对运行的有效性断言写失败测试:两臂同 store(evidence `01KYYK…`)、候选逐字节一致、只差 arm;报告 candidate oracle 区分 compiler miss 与 candidate miss 到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`
- [ ] T015 [P] [US2] 为分类别配对统计写失败测试:overall + multi-hop/temporal/single-hop/open-domain,exact McNemar,Holm non-regression 到 `cmd/locomo-bench/paired_eval_test.go`

### Implementation — US2

- [ ] T016 [US2] 在 022 冻结协议下跑配对消融(同 store、repeats≥3,需 LOCOMO_*/EMBED_*/JUDGE_* 端点或 AutoDL 评测箱):chunk_900 对照 vs `--compiler-arm extractive`/`exact_token`,LoCoMo 1,540 answerable,run-dir 记录
- [ ] T017 [US2] 跑 LongMemEval-S 500 配对(同协议),确认双基准共同过门(宪法 IV)
- [ ] T018 [US2] 报告分类别明细 + 配对统计 + token 记账 + candidate oracle;LoCoMo 6.4% 答案键噪声记录在案,小 delta 不单独作 promotion 依据到 `docs/evaluation/reports/verbatim-evidence-compile.md`
- [ ] T019 [US2] 写 benchmark-registration 到 `specs/026-verbatim-evidence-compile/benchmark-registration.md`;verdict 写入 `docs/evaluation/experiment-verdicts.md`

**Checkpoint**: 配对归因干净(只差 arm);verbatim-first 若同预算下相对 chunk_900 有可测 multi-hop/temporal 增益 → 评估进默认路径(需双基准共同过门);负收益 → FR-011 默认关并记录 verdict。

---

## Phase 5: User Story 3 — verdict 收口与文档同步(优先级 P1)

**Purpose**: 收口 026:verdict 更新、默认关验证、文档同步。

- [ ] T020 [US3] 确认所有 arm 默认关且关闭时双基准结果与基线一致(回归门对照)到 `docs/evaluation/reports/verbatim-evidence-compile.md`
- [ ] T021 [US3] 更新 `docs/evaluation/experiment-verdicts.md` 与 `docs/research/high-scoring-memory-systems.md`(若文献证据小节有遗漏),收口 026 verdict
- [ ] T022 [US3] 复核引擎未动:026 增量限定在 `cmd/locomo-bench/`(adapter 层);若触碰 `memory/ embedding/ provider/ store/ internal/` 须先说明为显式 contract increment(宪法 II)

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖,立即开始
- **Foundational (Phase 2)**: 依赖 Setup;BLOCKS 所有 User Stories
- **US1 (Phase 3)**: 依赖 Foundational;验证 arms 在 formal B1 可用(MVP)
- **US2 (Phase 4)**: 依赖 US1;配对消融
- **US3 (Phase 5)**: 依赖 US2;verdict 收口

### User Story 依赖

- **US1 (P1)**: Foundational 后可独立开始(MVP = formal B1 下 arms 可用验证)
- **US2 (P1)**: 依赖 US1(需要 arms 可用才能配对)
- **US3 (P1)**: 依赖 US1+US2(需要配对结果才能收口)

### Parallel 机会

- Phase 1 T001/T002/T003:可并行
- Phase 2 T004/T005/T006:可并行(不同测试文件/断言)
- Phase 3 T009–T011:可并行(不同失败测试)
- Phase 4 T014/T015:可并行(配对工具 vs 配对统计)

## 实现策略

**MVP first**: Phase 3(US1)完成即达 MVP——`--compiler-arm extractive/exact_token` 在 formal B1 下可用验证、默认关零行为变化。这证明"候选内已有证据可被编译成更可回答的 bundle"的验证前提成立(无 LLM 依赖,引擎已实现)。Phase 4–5 才是正式配对消融与 verdict。

**诚实修正(2026-08-01)**:初版 tasks 假设要实现 verbatim-first arm;核实后确认 022 引擎已完整实现(原文优先双态 + MERGE 双条件 gate,测试全绿)。026 改为**验证 + 配对消融**——这是规划时漏查引擎现状的修正,不改 spec 方向(验证 query-time verbatim 编译是否同预算优于 write-time 固定 chunk 的目标不变)。
