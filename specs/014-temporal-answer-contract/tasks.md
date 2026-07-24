# Tasks: 答题侧时序推理契约

**Feature**: `specs/014-temporal-answer-contract/` | **Branch**: `014-temporal-answer-contract`
**Input**: spec.md · plan.md · research.md · data-model.md · contracts/temporal-answer-contract.md · quickstart.md

**约定**:纯 `cmd/locomo-bench` adapter 改;引擎(`memory/ embedding/ provider/ store/ internal/`)零改。测试先行(TDD,spec US1 要求)。

---

## Phase 1: Setup

- [ ] T001 定位改动点:阅读 `cmd/locomo-bench/runner.go` 的 `forceTemporalAnswerPrompt`(≈L227)与 `answerPromptForRegime`(≈L281),确认 `(category=2, forceAnswer=true, temporalAnswer=true, abstain=false)` 是唯一命中路径;确认 `cmd/locomo-bench/runner_test.go` 是否存在(不存在则本 feature 新建)。
- [ ] T002 基线构建自检:`CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 当前全绿,作为改动前锚。

---

## Phase 2: Foundational

- [ ] T003 记录旧契约常量原文(用于 US2 的 `old-tplan` 归因臂):把当前 `forceTemporalAnswerPrompt` 全文抄存到 `specs/014-temporal-answer-contract/contracts/old-tplan-baseline.md`,便于 e2e 时临时还原旧常量而不丢字。

---

## Phase 3: User Story 1 - 强化时序推理契约 (Priority: P1) 🎯 MVP

**Goal**: category-2 force+temporal 路径返回压三失败模式的强化契约;三处不变量不动;引擎零改。

**Independent Test**: `go test ./cmd/locomo-bench -run TemporalContract` 全绿 + 引擎 diff 空。

### Tests(先写,必须先失败)

- [ ] T004 [US1] 在 `cmd/locomo-bench/runner_test.go` 写失败测试 `TestTemporalContractAnchors`:断言 `answerPromptForRegime(2, true, true, false)` 返回文本含 C1 五组稳定 token —— `ENUMERATE`+`[event:`;`RELATIVE`(或 `relative`)+`anchor`+`Never echo`(不分大小写);`EXACT MATCH`+`DIFFERENT event`;`DURATION`+`START and the END`;`never ISO`+`never decline`。(依据 contracts/temporal-answer-contract.md C1)
- [ ] T005 [P] [US1] 在 `runner_test.go` 写失败测试 `TestTemporalContractInvariants`:断言 C2 不变量 —— `(1,true,*,false)`→`forceMultiHopAnswerPrompt`;`(3,true,*,false)`→`forceOpenDomainAnswerPrompt`;`(2,true,false,false)`→`forceAnswerSystemPrompt`(开关关);`(2,*,*,true)`→`abstainAnswerPrompt`(abstain 优先)。

### Implementation

- [ ] T006 [US1] 重写 `cmd/locomo-bench/runner.go` 的 `forceTemporalAnswerPrompt` 常量为 contracts C1 的强化契约文本(四锚 + 终局约束),使 T004 转绿;不触碰 `temporalAnswerPrompt`(非 force 变体)以外无关常量,不改 `answerPromptForRegime` 路由。
- [ ] T007 [US1] 运行 `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run TemporalContract` 确认 T004/T005 全绿;运行全包 `go test ./cmd/locomo-bench` 确认无回归。
- [ ] T008 [US1] 验证引擎零改(FR-008/SC-003):`git diff --name-only -- memory embedding provider store internal` 输出必须为空;`git diff --name-only` 仅含 `cmd/locomo-bench/` 与 `specs/`。

**Checkpoint**: US1 完成 = 可提交的算法改(契约常量 + 单测),离线可验,零 box。

---

## Phase 4: User Story 2 - 三臂配对 e2e 验证门 (Priority: P2)

**Goal**: box 全本地栈跑 base/old-tplan/new-tplan 三臂,配对判 GO/NO-GO + 归因。依赖 US1。

**Independent Test**: 三臂 category-2 逐题正误产出,配对 McNemar 可算。

- [ ] T009 [US2] 起 box 全本地栈:answer/extract vllm(:8000)+ bge-large vllm(:8001,离线 env),建隧道 `-L 8000 -L 8001`,`/v1/models` 双端点就绪校验(见 docs/locomo-e2e-eval-reproduction.md §1)。
- [ ] T010 [US2] 写 e2e 三臂脚本到 session scratchpad(**非仓库**):公共 canonical recipe(`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned --retrieval hybrid --repeats 3 --concurrency 48`,**无 cat-top-k**——维护者规范:默认 top-k 30,不上 150,cat-top-k 只作后续无奈之举;store `.locomo-run/009-bge-chunks-store`);三臂 = base(无开关)/ old-tplan(`--temporal-answer-prompt` + 临时还原旧常量)/ new-tplan(`--temporal-answer-prompt` + 新常量)。**冷启动纪律**:先跑一个 warm-up 臂丢弃,或把 base 复跑一次做干净锚。WSL2 setsid detach + 文件轮询,隧道打包进脚本内。
- [ ] T011 [US2] 跑三臂;跑完对每个 run-dir `cat regime.json` 核四要素(`force_answer=true`/`judge=mem0-aligned`/`judge_model=deepseek-v4-flash`)。
- [ ] T012 [US2] 判定:抽三臂 category-2(n=321)逐题正误(3 rep 多数投票)+ overall;配对 McNemar `new-tplan` vs **干净复跑 base**(不对冷首臂)→ SC-001(GO 需显著抬 + overall 不回退);`new-tplan` vs `old-tplan` 差分 → SC-004 归因。
- [ ] T013 [US2] 落 verdict(GO/NO-GO + 归因)到 `docs/locomo-score-levers.md` 新增「Feature 014 答题侧时序推理契约」段 + lever ledger 行;**与算法改分开提交**(Constitution IV attribution)。

**Checkpoint**: US2 完成 = GO/NO-GO 有据判定,verdict 落 tracked docs。

---

## Phase 5: User Story 3 - 可移植契约 pattern 文档化 (Priority: P3, 条件于 GO)

**Goal**: GO 则产出可移植 pattern;NO-GO 则记录升级路径。

- [ ] T014 [US3] **若 US2=GO**:写 `docs/temporal-answer-contract.md`——契约全文 + 三失败模式动机 + 复用前置条件(记忆行须带绝对 `[event:]` 锚),自包含供集成方 host 侧复用。
- [ ] T015 [US3] **若 US2=NO-GO**:在 T013 的 verdict 内记录归因 + 欠转化升级路径(确定性日期脚手架 Option B:预解析 `[event:]` 注入 TIMELINE 块),不产出移植文档。

---

## Phase 6: Polish & Cross-Cutting

- [ ] T016 SDD 形式化提交:`spec/plan/tasks/contracts/research/data-model/quickstart` 一并提交(`spec(014): SDD 形式化`)。
- [ ] T017 清理:删除 session scratchpad 内 e2e 脚本/日志;确认仓库 `git status` 无残留临时文件;box 空闲即停提醒维护者。
- [ ] T018 更新本地 memory:落 `014-temporal-contract-verdict` 指针到 MEMORY.md(verdict 正本在 tracked docs,memory 只留钩子)。

---

## Dependencies

- **Setup(T001-T002)** → **Foundational(T003)** → **US1(T004-T008)** → **US2(T009-T013)** → **US3(T014-T015)** → **Polish(T016-T018)**。
- US1 是 MVP,独立可交付(离线单测 + 引擎零改验证)。
- US2 依赖 US1 契约已实现(需 new 常量)+ T003 旧常量存档(old-tplan 臂)。
- US3 条件于 US2 判定结果。

## Parallel Opportunities

- T004 与 T005 可并行([P],同文件不同测试函数——若严格同文件写入冲突则顺序执行;逻辑独立)。
- 其余任务基本线性(单文件常量改 + 一次 box run + 条件文档),并行空间小,符合小 feature 特性。

## Implementation Strategy (MVP first)

1. **MVP = US1**:契约常量 + 单测 + 引擎零改验证,离线全绿即可提交(算法改)。
2. **门 = US2**:box 三臂 e2e 判 GO/NO-GO,冷启动纪律,verdict 分开提交。
3. **交付/收口 = US3**:GO 出可移植文档;NO-GO 记升级路径。

## 总任务数:18(Setup 2 · Foundational 1 · US1 5 · US2 5 · US3 2 · Polish 3)
