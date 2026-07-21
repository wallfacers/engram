# Tasks: LoCoMo Judge 口径对齐(mem0-aligned)

**Feature**: 007-judge-metric-alignment | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

**分工约定**:标 **[EXT]** 的任务交外部 AI agent 实现(写代码/夹具);无标记的由维护者(本会话)执行——评审、决策、兜底、门禁校验。维护者对 [EXT] 产物独立复跑测试 + `git diff` 核验引擎未动 + 确认 anti-放水陷阱真断言(非自比空转)。

**TDD 硬序**:先夹具 + 失败测试(红)→ 再条件化 prompt 转绿 → anti-放水陷阱全程守住。

**规则源**:`../mem0/evaluation/benchmarks/locomo/prompts.py:218-245`(2026-07-21 采集)。

---

## Phase 1: Setup

- [ ] T001 记录改动前基线:`git diff --name-only master...007-judge-metric-alignment -- memory embedding provider store internal` 当前为空,存为引擎零改门的起点(维护者)。
- [ ] T002 确认 golden 层 env 约定(`LOCOMO_JUDGE_GOLDEN=1` + `LOCOMO_API_KEY/BASE_URL/MODEL`),写入外部 agent 提示词(维护者)。

## Phase 2: Foundational（阻塞 US1 的最小骨架）

- [ ] T003 [EXT] 在 `cmd/locomo-bench/runner.go` 抽出 `judgeSystemPromptFor(mode)`(mode: strict|mem0-aligned),`strict` 分支返回**与现有 `judgeSystemPrompt` 逐字相同**的文本;暂不填 mem0-aligned 正文(占位,后续 T009 填)。`buildJudgePrompt`/`parseJudgeVerdict` 不动。
- [ ] T004 [EXT] 在 `cmd/locomo-bench/main.go` 加 `--judge-mem0-aligned` bool flag(默认 false),存入 options;把 judge 口径 mode 透传到 judge 调用点(不改 judge 输出契约)。

**Checkpoint**: 骨架就位,flag off 行为不变(下方 T007 回归断言守此)。

## Phase 3: User Story 1 — 免费口径对齐 + 防放水护栏 (P1, MVP)

**Goal**: 交付"已验证不放水的 mem0-aligned judge + 口径隔离",零答题成本。
**Independent Test**: 见 [quickstart.md](./quickstart.md) 第 2-4 步。

### TDD:先夹具 + 失败测试(红)

- [ ] T005 [EXT] 建 golden 夹具 `cmd/locomo-bench/testdata/judge_golden.jsonl`,~25-30 条,按 [data-model.md](./data-model.md) 覆盖矩阵:宽松该判对(partial-credit 列表1/2、date ≤14d、duration ±50%、emotion-valence、paraphrase、extra-detail、same-referent)+ anti-放水陷阱(wrong-name、contradiction、wrong-number、zero-overlap、idk、date>14d)+ 边界(boundary-14d=true、list-zero-hit=false、emotion-opposite=false)。每条含 `expected_correct/rule/note`。**维护者重点复审此文件**(护栏本体)。
- [ ] T006 [EXT] 建 `cmd/locomo-bench/judge_golden_test.go` 的**离线确定性层**(进 `CGO=0` CI,零 LLM):(a) 断言 `judgeSystemPromptFor("mem0-aligned")` 含 7 条规则关键子句(部分给分/±14天/时长50%/情绪同价/同referent/额外细节/重事实);(b) `parseJudgeVerdict` 表驱动正确性用例。此刻 mem0-aligned 正文未填 → (a) 红。
- [ ] T007 [EXT] 在同测试文件加**回归断言**:`judgeSystemPromptFor("strict")` 输出 == 改动前的 `judgeSystemPrompt` 常量(逐字),守 SC-005。
- [ ] T008 [EXT] 加 **golden 层** `TestJudgeMem0AlignedMatchesGolden`(env-gate:无 `LOCOMO_JUDGE_GOLDEN` 时 `t.Skip`):读夹具,用 mem0-aligned judge 真跑每条,断言 label == expected_correct;**任一 anti-放水陷阱判对 → t.Fatal**。此刻正文未填 → 宽松案例红。

### 转绿:条件化 prompt 正文

- [ ] T009 [EXT] 在 `judgeSystemPromptFor` 的 mem0-aligned 分支填入 Mem0 7 条规则 + 收口("仅零命中或完全跑题才 WRONG"),保持 `{"correct":true|false}` 输出契约。目标:T006(a) + T008 全绿,且 T007 仍绿、T008 陷阱全 WRONG。
- [ ] T010 [EXT] 在 `main.go` run fingerprint(`~:1018`)于 mode==mem0-aligned 时追加 `;judge=mem0-aligned`;加离线单测断言开/关 flag 的 fingerprint 不同(SC-006)。

### 门禁校验（维护者兜底）

- [ ] T011 维护者独立复跑 `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/...`,确认离线层全绿。
- [ ] T012 维护者跑引擎零改门:`git diff --name-only master...007-judge-metric-alignment -- memory embedding provider store internal` 为空(FR-007/SC-004)。
- [ ] T013 维护者近免费跑 golden 层(`LOCOMO_JUDGE_GOLDEN=1 ...`),确认 SC-001(陷阱全 WRONG)+ SC-002(宽松全 CORRECT);人工抽查任一陷阱确为真断言、非自比空转。记录调用次数证零答题(SC-003)。
- [ ] T014 维护者核验 T005 夹具:每条陷阱是真实"错答案"、每条宽松是真实"该判对",无标注错误(护栏可信度)。

**Checkpoint US1 完成**:mem0-aligned judge 可用且验证不放水;默认 off 零回归;口径隔离生效;引擎零改。**mechanism 提交,不声称任何分数。**

## Phase 4: User Story 2 — 授权后量化新基线 (P2, 双门控, 默认不执行)

> ⚠️ 仅在 US1 交付 + **显式成本授权**后执行。默认 STOP。

- [ ] T015 维护者:查旧答题 transcript 是否存活;存活则用新 judge 重判(仅判分 token),失效则规划最小重跑口径。
- [ ] T016 维护者(授权后):同 transcript 双口径对跑/重判,量出新口径分 X%。
- [ ] T017 维护者:eval-log 新开 "Judge 口径 v2 (mem0-aligned) — 新基线 X%(老严格 judge 65.4%)" + false→true flip 抽查;**明标"口径对齐非涨点,不得叠加"**;**eval 结果单独提交**(宪法 IV,FR-008)。
- [ ] T018 维护者:视结果决定是否翻默认(FR-010),单独决策 + 单独提交。

## Phase 5: Polish

- [ ] T019 [EXT] `judge_golden_test.go` 顶注释说明夹具用途、env-gate、陷阱判据;`testdata/judge_golden.jsonl` 附一行 schema 说明(或同目录 README)。
- [ ] T020 维护者:更新 `docs/competitive-benchmarks.md` §6,把"engram judge 未对齐"标注改为"US1 已提供 mem0-aligned 口径(默认 off),新基线待 US2"。

---

## Dependencies

- Setup(T001-T002)→ Foundational(T003-T004)→ US1(T005-T014)→ US2(T015-T018,门控)→ Polish。
- US1 内 TDD 序:T005-T008(红)→ T009-T010(绿)→ T011-T014(门禁)。
- US2 依赖 US1 完成 + 授权,逻辑独立可延后。

## Parallel Opportunities

- T005(夹具)与 T006/T007(离线测试骨架)可并行起草,但 T008/T009 依赖 T005+T006。
- T003 与 T004 可并行(不同文件/不同函数)。

## MVP Scope

**US1(T001-T014)= 完整免费 MVP**。US2 付费、双门控、默认不做。

## 交外部 AI agent 的任务清单(代码/夹具)

T003, T004, T005, T006, T007, T008, T009, T010, T019 —— 其余为维护者评审/门禁/决策/兜底。
