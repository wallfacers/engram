# Tasks: 查询时时间有效性解析

**Input**: Design documents from `specs/027-temporal-validity-resolution/`

**Prerequisites**: [plan.md](./plan.md)（必需）、[spec.md](./spec.md)（必需，User Stories）、
[research.md](./research.md)、[data-model.md](./data-model.md)、[contracts/](./contracts/temporal-resolution-binding.md)、
[quickstart.md](./quickstart.md)

**Tests**: 本特性修改 benchmark harness(adapter 层)。引擎层(`memory/evidencecompiler`)契约冻结不改,
解析器只读消费 `Source.OccurredAt` 与 `compileFormalSources` 的 source list。027 的真实增量是
**查侧确定性时间组织**(当前值/演化链/时间窗) + **formal B1 配对消融**。测试先行(CLAUDE.md TDD 规则):
解析器确定性、退化基线、默认关零行为变化先写失败测试。

**Organization**: Phase 3–5 按 spec 的三个 User Story 排列。027 是研究实验(默认关);
解析机制未通过配对消融前不进入默认路径(FR-010/FR-011)。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可在不同文件上并行,且不依赖尚未完成的同阶段任务
- **[US1]–[US3]**: 对应 [spec.md](./spec.md) 的 User Story
- 每项都包含执行或产物的精确仓库路径

## Phase 1: Setup(承接 022 资产验证)

**Purpose**: 核实 022 的时间基建(Source.OccurredAt、compileFormalSources source list)与
legacy temporal flags 的拒绝路径,避免在未验证资产上重复实现或触碰已证伪机制。

- [x] T001 核实 `memory/evidencecompiler` 的 `Source.OccurredAt` 字段可读且 harness 侧
  `compileFormalSources`(`cmd/locomo-bench/eval_compile_bridge.go:244`)构建的 source list
  携带时间戳,并确认 `selectedHasTimeEvidence`(`memory/evidencecompiler/orchestrate.go:318`)
  的 OccurredAt 访问模式;结论记录到 `specs/027-temporal-validity-resolution/research.md`(D2 核实)
- [x] T002 核实 013/014/017/024 遗留 temporal flags(`temporalScore`/`temporalHardFilter`/
  `conflictResolution`/`supersededPenalty`)在 `validateFormalLegacyMechanismOptions`
  (`cmd/locomo-bench/eval_runner.go:325`)被 formal B1 拒绝,027 的 `--temporal-resolution`
  必须走 additive mechanism 分支(`densityMechanismFlagsForOptions`, `cmd/locomo-bench/eval_runner.go:359`),
  不触发 legacy 拒绝;结论记录到 `specs/027-temporal-validity-resolution/research.md`(D1/D4 核实)
- [x] T003 在算法改动前运行 `CGO_ENABLED=0 go build ./...` 与全量 Go tests,确认 022/026 收口后
  健康状态可追溯,记录到 `specs/027-temporal-validity-resolution/plan.md`

**Checkpoint**: 027 基于已收口 022 的时间基建 + 026 已验证的 mechanism-flag 接线;legacy temporal
flags 的拒绝边界明确;当前产品健康状态有可追溯记录。

---

## Phase 2: Foundational(解析器 + mechanism 接线)

**Purpose**: 实现确定性时间有效性解析器(`temporal_resolution.go`)与 mechanism flag 接线,建立
后续 US1–US3 的共享底座。

### Tests first — Foundational

- [x] T004 [P] 为 `--temporal-resolution` mechanism flag 接线写失败测试:formal B1 下开启该 flag
  产生 `mechanism_flags{temporal_resolution:true}` 且 protocol hash 与关闭态不同、三臂候选逐字节
  一致到 `cmd/locomo-bench/eval_runner_test.go`
- [x] T005 [P] 为解析器确定性写失败测试:同一 query + 同一 source list 重复运行,输出 bundle 与
  audit digest 逐字节一致到 `cmd/locomo-bench/temporal_resolution_test.go`
- [x] T006 [P] 为解析器退化路径写失败测试:无 OccurredAt / 单版本 / query 无时间语义时输出与输入
  完全一致(基线行为),不新增 token、不调用 LLM 到 `cmd/locomo-bench/temporal_resolution_test.go`

### Implementation — Foundational

- [x] T007 实现确定性主题分组与版本排序:同一事实主题的多个 source 按 OccurredAt 升序全序,
  非末位标 IsSuperseded(时间序判定);OccurredAt 缺失的 source 跳过分组到 `cmd/locomo-bench/temporal_resolution.go`
- [x] T008 实现三种解析模式:当前值解析(选最新 valid)、演化链组装(按时间全序绑 SourceID)、
  时间窗约束(按 query 显式时间范围过滤);输出 bundle 在 cap 内,超 cap fail-closed 到
  `cmd/locomo-bench/temporal_resolution.go`
- [x] T009 将解析器接入 `compileFormalSources` 产出的 source list 之后、bundle 组装之前,
  仅在 `--temporal-resolution` 开启时激活,关闭时零行为变化到 `cmd/locomo-bench/eval_compile_bridge.go`

**Checkpoint**: 解析器确定性、可退化、纯 Go 零 LLM;mechanism flag 接线通过 formal B1 冻结。

---

## Phase 3: User Story 1 — 验证时间解析在 formal B1 下可用(优先级 P1)🎯 MVP

**Goal**: `--temporal-resolution` 在 formal B1 冻结协议下可用:当前值/演化链/时间窗三模式确定运行、
默认关零行为变化、per-question audit 可审计。**MVP = 证明"候选内已有证据可被按时间组织"的验证
前提成立。**

**Independent Test**: 构造含 superseded/current 双版本与显式时间窗的固定 source list,断言解析器
输出符合确定性规则且 digest 一致;关闭 flag 时与 chunk_900 基线逐字节一致。

### Tests first — US1

- [x] T010 [P] [US1] 为默认关零行为变化写失败测试:`--temporal-resolution` 未设时行为与 chunk_900
  基线完全一致到 `cmd/locomo-bench/temporal_resolution_test.go`
- [x] T011 [P] [US1] 为当前值解析写失败测试:知识更新/当前状态语义 query 选最新 valid 版本,
  superseded 版本排除且记录到 `cmd/locomo-bench/temporal_resolution_test.go`
- [x] T012 [P] [US1] 为演化链组装写失败测试:演化语义 query 按 OccurredAt 全序组装完整
  superseded→current 链,逐项绑 SourceID 到 `cmd/locomo-bench/temporal_resolution_test.go`
- [x] T013 [P] [US1] 为时间窗约束写失败测试:query 含显式时间范围时仅保留 OccurredAt 覆盖该范围
  的版本,越窗排除并记录原因到 `cmd/locomo-bench/temporal_resolution_test.go`

### Implementation — US1

- [x] T014 [US1] 接线 `--temporal-resolution` flag 到 `cmd/locomo-bench/main.go` + `eval_runner.go`
  (`densityMechanismFlagsForOptions` / `formalTreatmentFreeze` / `mergeMechanismFlags`),formal B1
  下独立冻结,非 formal 上下文 fail-closed
- [x] T015 [US1] 记录 per-question 解析 audit(mode / group_count / versions_considered /
  superseded_excluded / window_excluded / unresolved_time / resolution_oracle)到
  `cmd/locomo-bench/temporal_resolution.go`,供归因(spec FR-008)

**Checkpoint**: 三模式在 formal B1 下可用、默认关零行为变化;audit 可审计。**MVP 达成。**

---

## Phase 4: User Story 2 — 同 store 配对消融(优先级 P1)

**Purpose**: 在 022 冻结协议下,对"`--temporal-resolution` vs chunk_900 基线"做同 store、候选
逐字节一致配对,确认查侧时间组织是否补上 temporal 短板且 overall 不回归。

**Independent Test**: 在 022 冻结协议与同一 store 下跑对照 vs 处理臂,repeats≥3,对比 overall 与
分类别配对统计(重点 temporal / knowledge-update / 演化类 multi-hop)。

### Tests first — US2

- [x] T016 [P] [US2] 为配对有效性断言写失败测试:两臂同 store、候选逐字节一致、只差机制 flag;
  报告 resolution oracle(superseded/current 是否在池)到 `cmd/locomo-bench/eval_runner_test.go`
- [x] T017 [P] [US2] 为分类别配对统计写失败测试:overall + temporal/knowledge-update/multi-hop/
  single-hop,exact McNemar,Holm non-regression 到 `cmd/locomo-bench/paired_eval_test.go`

### Implementation — US2

- [ ] T018 [US2] 在 022 冻结协议下跑配对消融(同 store、repeats≥3,需 LOCOMO_*/EMBED_*/JUDGE_* 端点
  或 AutoDL 评测箱,run-dir 必须在 `/root/autodl-tmp/`):chunk_900 对照 vs `--temporal-resolution`,
  LoCoMo 1,540 answerable,run-dir 记录到 `docs/operations/evaluation/` 或 quickstart.md
- [ ] T019 [US2] 报告分类别明细 + 配对统计 + token 记账 + resolution oracle;LoCoMo 6.4% 答案键噪声
  记录在案,小 delta 不单独作 promotion 依据到 `docs/evaluation/reports/temporal-validity-resolution.md`
- [ ] T020 [US2] 写 benchmark-registration 到 `specs/027-temporal-validity-resolution/benchmark-registration.md`;
  verdict 写入 `docs/evaluation/experiment-verdicts.md`

**Checkpoint**: 配对归因干净(只差解析机制);temporal/knowledge-update 若相对 chunk_900 有可测增益
→ 评估进默认路径;负收益 → FR-010/FR-011 默认关并记录 verdict。

---

## Phase 5: User Story 3 — 归因: temporal 短板是否真在"未解析演化"(优先级 P2)

**Purpose**: 逐题分类 candidate miss / resolution miss / answerer miss,确认 resolution-miss 占比;
只有非空才支持 027 机制价值,避免 candidate miss 假阳性。

**Independent Test**: 对 temporal/knowledge-update 全量错题逐题检查 superseded/current 版本是否在
候选池,输出归因表与 resolution-miss 占比。

### Tests first — US3

- [x] T021 [P] [US3] 为 candidate-oracle 归因写失败测试:每题 superseded/current 版本在池判定,
  区分 resolution miss 与 candidate miss 到 `cmd/locomo-bench/miss_attribution_test.go`

### Implementation — US3

- [ ] T022 [US3] 对 temporal/knowledge-update 全量错题逐题归因(candidate/resolution/answerer miss),
  复用 fixed-gold oracle,输出归因表到 `docs/evaluation/reports/temporal-validity-resolution.md`
- [ ] T023 [US3] 统计 resolution-miss 占比并写结论:占比显著 → 解析机制增益归因到该机制;
  占比可忽略 → 记录负结论,027 不作为推荐路径(FR-011)到 `docs/evaluation/experiment-verdicts.md`

**Checkpoint**: 归因覆盖 temporal/knowledge-update 全量错题;resolution-miss 占比决定推广与否。

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 收口 027:引擎零改动复核、文档同步。

- [x] T024 复核引擎未动:027 增量限定在 `cmd/locomo-bench/`(adapter 层);若触碰 `memory/ embedding/
  provider/ store/ internal/` 须先说明为显式 contract increment(宪法 II),`git diff --name-only -- memory
  embedding provider store internal` 必须为空
- [ ] T025 更新 `docs/research/high-scoring-memory-systems.md`:登记 027 实测结果(若配对跑完),
  并核对 APEX-MEM(2604.14362)条目已含"append-only + retrieval-time resolution +14~25pp temporal"
  证据;跑 quickstart.md 验证

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (Phase 1)**: 无依赖,立即开始
- **Foundational (Phase 2)**: 依赖 Setup;BLOCKS 所有 User Stories
- **US1 (Phase 3)**: 依赖 Foundational;验证解析器在 formal B1 可用(MVP)
- **US2 (Phase 4)**: 依赖 US1;配对消融
- **US3 (Phase 5)**: 依赖 US2(需要配对结果才能归因);但 candidate-oracle 归因框架(T021)可
  在 US2 前搭建
- **Polish (Phase 6)**: 依赖 US1–US3

### User Story 依赖

- **US1 (P1)**: Foundational 后可独立开始(MVP = formal B1 下三模式可用验证)
- **US2 (P1)**: 依赖 US1(需要解析器可用才能配对)
- **US3 (P2)**: 依赖 US2(需要配对结果才能归因)

### Parallel 机会

- Phase 1 T001/T002/T003:可并行(不同核实面)
- Phase 2 T004/T005/T006:可并行(不同测试文件/断言)
- Phase 3 T010–T013:可并行(不同解析模式断言)
- Phase 4 T016/T017:可并行(配对有效性 vs 配对统计)
- Phase 5 T021:可与 US2 并行(归因框架不依赖配对结果)

---

## 实现策略

**MVP first**: Phase 3(US1)完成即达 MVP——`--temporal-resolution` 三模式在 formal B1 下可用验证、
默认关零行为变化、audit 可审计。这证明"候选内已有证据可被按时间组织"的验证前提成立(纯 Go
确定性,无 LLM 依赖,引擎已具备时间基建)。Phase 4–5 才是正式配对消融与归因。

**诚实边界**: APEX-MEM(2604.14362)的 +14~25pp temporal 是跨栈证据(GPT5 answerer),engram 固定栈
增益必须独立配对验证,不外推(research D5)。若配对证明时间解析无收益或负收益,机制保持默认关
并记录 verdict(FR-010/FR-011),不堆叠机制强行涨点。

**已证伪边界**: 027 不触碰 013/014/017 的检索侧重排/过滤(`temporalScore`/`temporalHardFilter`)、
014 的 prompt 脚手架、024 的写侧 supersede 惩罚(`conflictResolution`/`supersededPenalty`)——
这些在 `validateFormalLegacyMechanismOptions` 被 formal B1 拒绝,027 的查侧组装面是新机制
(research D1)。
