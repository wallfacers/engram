---
description: "Task list for 017-temporal-date-scaffold"
---

# Tasks: 确定性日期脚手架(TIMELINE 块)

**Input**: Design documents from `/specs/017-temporal-date-scaffold/`

**Prerequisites**: [plan.md](./plan.md) · [spec.md](./spec.md) · [research.md](./research.md) · [data-model.md](./data-model.md) · [contracts/scaffold-contract.md](./contracts/scaffold-contract.md) · [quickstart.md](./quickstart.md)

**Tests**: **必需,非可选。** 本项目宪法「开发工作流与质量门禁」规定测试先行;
且 research D-6 已论证:没有先红的单测,US2 若 NO-GO 将无法区分「思路错」与「实现有 bug」。
契约测试清单 CT-1~CT-17 见 [contracts/scaffold-contract.md](./contracts/scaffold-contract.md)。

**Organization**: 按 user story 分阶段,US1 独立构成 MVP(零成本、可断网完整验收)。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行(不同文件、无未完成依赖)
- **[Story]**: 所属 user story(US1/US2/US3)
- 每条任务含确切文件路径

## Path Conventions

本 feature 全部改动位于 `cmd/locomo-bench/`(adapter)。
**引擎目录 `memory/ embedding/ provider/ store/ internal/` 零改动 —— 硬门。**

---

## Phase 1: Setup

**Purpose**: 确认起点干净,建立可回滚的基线

- [x] T001 确认工作区干净且引擎未被改动:运行 `git status` 与 `git diff --name-only -- memory embedding provider store internal`(后者必须无输出),记录当前 HEAD 短哈希备回滚
- [x] T002 确认基线构建与测试为绿:`CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/` 全通过(先证明"改坏了"可被检出)

**Checkpoint**: 起点已知且可回滚

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: 冻结"关闭时逐字节不变"的黄金基线 —— 这是后续所有改动的安全网

**⚠️ CRITICAL**: T003 必须在任何生产代码改动**之前**完成,否则"逐字节不变"无从验证

- [x] T003 在 `cmd/locomo-bench/bench_test.go` 新增黄金基线测试:对固定的记忆集合(含带日期/不带日期/cluster-sweep 三种形态)快照当前 `buildAnswerPrompt` 与 `buildSweepAnswerPrompt` 的输出字符串,断言与硬编码期望值逐字节相等。此测试在本 feature 全程 **MUST 保持绿**,是 FR-006/SC-003 的守门人

**Checkpoint**: 安全网就位 —— 任何破坏现有 prompt 的改动都会立刻变红

---

## Phase 3: User Story 1 - 确定性 TIMELINE 脚手架 + 离线断言 (Priority: P1) 🎯 MVP

**Goal**: 交付一个**已被证明算得对、降得下、关得掉**的确定性日期脚手架,全程零 LLM 调用

**Independent Test**: `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/` 全绿(含 CT-1~CT-17),
且 `git diff --name-only -- memory embedding provider store internal` 无输出。断网可跑完。

### 测试先行(TDD:以下测试 MUST 先写、先红,再写实现)

- [x] T004 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 写 CT-1~CT-3(短路族):`enabled=false` 返回 `""`;`category≠2` 返回 `""`;全部记忆无 `EventDate` 返回 `""`
- [x] T005 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 写 CT-4/CT-5/CT-12/CT-16(排序与呈现族):5 条有日期按升序 + 连续编号 `T1..T5` + 日期为自然语言非 ISO;无日期条目不进块;同日多条保持输入顺序;跨年正确排序
- [x] T006 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 写 CT-6/CT-7(相对表达族):有锚 → 推导出绝对日期且带 `(derived from "...")` 标记;**无锚 → 不推导、不标注、不臆造**
- [x] T007 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 写 CT-8/CT-9/CT-10(跨度族):≥2 条输出 `SPAN` 且数值精确可断言;仅 1 条**不输出** `SPAN`;端点粒度不足时降级为约略表述且**不出现精确天数**
- [x] T008 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 写 CT-11/CT-17(确定性与健壮性族):同一输入连续调用 100 次逐字节相同;空切片/畸形日期串/全空字段不 panic
- [x] T009 [US1] 运行 `CGO_ENABLED=0 go test -count=1 -run Timeline ./cmd/locomo-bench/` 确认**全部新测试为红**(编译失败或断言失败均可),并记录失败输出 —— 证明测试确实在测东西

### 实现

- [x] T010 [US1] 新建 `cmd/locomo-bench/timeline.go`,实现 data-model E-2 `timelineEntry`(序号/日期/粒度/来源标记/正文序号)与 E-3 `interval`(起止序号/跨度值/精度标记)两个进程内类型,不导出
- [x] T011 [US1] 在 `cmd/locomo-bench/timeline.go` 实现日期读取与规范化:从 `retrievedMemory.EventDate`(`"2006-01-02"`)解析为 `time.Time` 并判定粒度;空值跳过该条(不生成条目、不丢弃记忆),遵守不变量 I-1/I-2
- [x] T012 [US1] 在 `cmd/locomo-bench/timeline.go` 实现相对表达解析:窄集合模式匹配 `Content`,**仅以该条自身 `EventDate` 为锚**(不得用 `Recorded` 或他条日期,不变量 I-3),失败静默降级
- [x] T013 [US1] 在 `cmd/locomo-bench/timeline.go` 实现稳定排序与连续编号:按日期升序,同日保持输入顺序(`sort.SliceStable`,不变量 I-10),编号从 1 连续不跳号
- [x] T014 [US1] 在 `cmd/locomo-bench/timeline.go` 实现跨度计算:条目 ≥2 时算首尾跨度(及契约规定的相邻跨度);精度**不得高于**较粗端点粒度(不变量 I-4);条目 <2 时跨度集合为空(不变量 I-5)
- [x] T015 [US1] 在 `cmd/locomo-bench/timeline.go` 实现 `buildTimelineBlock(memories []retrievedMemory, category int, enabled bool) string`,按 contracts C-2 渲染文本格式;条目为 0 时返回 `""`(整块省略,不变量 I-7);纯函数、无 I/O、无 `time.Now()`、不修改入参(contracts C-3)
- [x] T016 [US1] 运行 `CGO_ENABLED=0 go test -count=1 -run Timeline ./cmd/locomo-bench/` 确认 T004~T008 的测试**全部转绿**

### 接线(注入答题上下文)

- [x] T017 [US1] 在 `cmd/locomo-bench/runner.go` 为 `buildAnswerPrompt` 与 `buildSweepAnswerPrompt` 增加 `timeline string` 参数,注入位置为 `RETRIEVED MEMORIES` 段之后、`QUESTION:` 之前(research D-4);`timeline == ""` 时输出**逐字节等同改动前**
- [x] T018 [US1] 在 `cmd/locomo-bench/runner.go` 为 `buildAnswerContextPrompt` 增加 `category int, scaffold bool` 参数,内部调用 `buildTimelineBlock` 并把结果传给上述两个构造函数(**cluster-sweep 路径同样接入**,contracts C-3 派生要求 2)
- [x] T019 [US1] 更新 `cmd/locomo-bench/main.go` 中 `buildAnswerContextPrompt` 的全部四个调用点(约 `main.go:1509/1644/1680/1715`)传入 `qa.Category` 与开关状态;确认 `cmd/locomo-bench/filter.go` 若受签名影响一并更新
- [x] T020 [US1] 在 `cmd/locomo-bench/main.go` 新增 CLI flag `--temporal-date-scaffold`(bool,**默认 false**,contracts C-1)并接入 `options` 结构体
- [x] T021 [US1] 在 `cmd/locomo-bench/main.go` 的 `answerRegimeFingerprint`(约 `main.go:1218`)中,开关开启时追加 `;temporal_date_scaffold=true`;**关闭时逐字节不变**(contracts C-4,不变量 I-9)
- [x] T022 [P] [US1] 在 `cmd/locomo-bench/timeline_test.go` 补 CT-13/CT-14(接线回归):`timeline=""` 时 `buildAnswerPrompt` 输出等于改动前;`buildSweepAnswerPrompt` 同样接受并注入 timeline
- [x] T023 [P] [US1] 在 `cmd/locomo-bench/bench_test.go` 补 CT-15(fingerprint):开关关 → fingerprint 与今天逐字节相同;开关开 → 含 `;temporal_date_scaffold=true`

### US1 验收

- [x] T024 [US1] 运行完整离线验收:`CGO_ENABLED=0 go build ./...` + `CGO_ENABLED=0 go test -count=1 ./...` 全绿(含 T003 黄金基线仍绿)
- [x] T025 [US1] 运行引擎零改硬验证 `git diff --name-only -- memory embedding provider store internal`,确认**无任何输出**(FR-010/SC-005)
- [x] T026 [US1] 人工核对一份样例输出:用带日期的样例记忆打印实际 TIMELINE 块,肉眼确认格式符合 contracts C-2 且**块内无任何不在输入记忆中的事实**(C-2 硬约束 1)

**Checkpoint**: US1 完成 = MVP 可交付。脚手架已证明算得对/降得下/关得掉,**但尚未证明有用**。
⚠️ **此时不得宣称任何涨点**(008 铁律;research D-6)。

---

## Phase 4: User Story 2 - 端到端 GO/NO-GO 门 (Priority: P2)

**Goal**: 回答唯一的问题 —— 开了它,temporal 端到端答分有没有真的涨

**⚠️ 双门控**: US1 全绿 **且** 维护者**显式成本授权**。未授权不得执行本阶段任何任务(FR-014)。

**Independent Test**: 产出三臂结果 + 五项必需数字 + 落地 verdict。

### 前置

- [ ] T027 [US2] 取得**显式成本授权**并记录授权范围(预算上限、允许的 box 时长);未获授权则本阶段停止
- [ ] T028 [US2] 准备 box 与端点:确认 vllm 已起、SSH 隧道已通、`JUDGE_*` env 就位;**隧道启动 MUST 打包进 `setsid` 脚本内**(否则脚本活着隧道死);凭据只走 env,**不得**写入任何文件或日志
- [ ] T029 [US2] 复用既有 store(不重建,抽取零成本),确认与 canonical recipe 的 embedder 一致(`bge-large-en-v1.5`)

### 三臂对跑

- [ ] T030 [US2] 跑 `base` 臂:canonical recipe(`--chunks --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned --retrieval hybrid --repeats 3`),脚手架**关**;**冷启动首臂结果丢弃或复跑**(FR-012,014 实测冷首臂偏低 ~2.25pp)
- [ ] T031 [US2] 跑 `ref` 臂:与 `base` **配置完全相同**的重跑,作为**噪声标尺** —— 不可省(FR-012;LongMemEval 实测同配置重跑差 2 分)
- [ ] T032 [US2] 跑 `scaffold` 臂:同配置 + `--temporal-date-scaffold`;`--temporal-answer-prompt` 状态 MUST 与前两臂固定一致(contracts C-1,否则两变量混淆)
- [ ] T033 [US2] 核验各臂 `regime.json`:`scaffold` 臂 fingerprint **必须**含 `;temporal_date_scaffold=true`;若缺失说明开关未生效,**整轮作废重跑**

### 判定(五项缺一即 inconclusive)

- [ ] T034 [US2] 算 ① temporal 类(n=321)准确率变化 与 ② `scaffold` vs 干净 `base` 的配对 McNemar 显著性(**不得**对冷启动首臂做配对)
- [ ] T035 [US2] 算 ③ overall 回退检查 与 ④ `ref` vs `base` 噪声标尺差值
- [ ] T036 [US2] 实测 ⑤ 答题上下文 **token 增量**(`scaffold` vs `base` 每题 prompt token),用于区分"提质"与变相"加量"(FR-013)
- [ ] T037 [US2] 按 GO 判据判定:**GO ⟺ temporal 配对显著抬升 AND overall 不回退**;任一不满足即 NO-GO;五项数字缺任一则判 inconclusive(SC-006)。同时对照名义上限 **2.47pp** —— 超出该上限的"增益"是错误信号,先查 bug

**Checkpoint**: 有了可信的 GO/NO-GO 结论

---

## Phase 5: User Story 3 - Verdict 与台账回填 (Priority: P3)

**Goal**: 让下一个人不必重跑就知道这条路的结局

- [ ] T038 [US3] 在 `docs/locomo-score-levers.md` 新增本 feature 的 verdict 节:判据、五项数字、成本、产物指针;NO-GO 时归因**必须区分**「思路错」/「上下文被稀释」(014 翻车模式)/「落在噪声内」(看 `ref` 标尺)
- [ ] T039 [US3] 更新 `docs/locomo-score-levers.md`「剩余未验方向盘点」中 #3 的状态(当前为"已立项 017"),并同步修正剩余项计数
- [ ] T040 [US3] 若 GO:声明新参考点,并把 eval 结果与实现改动**分开提交**(宪法 IV / FR-016),明标"这是端到端答题增益,不与任何已计入基线的口径改动叠加"
- [ ] T041 [US3] 若 NO-GO:`--temporal-date-scaffold` 维持默认关、不出货、不产出移植文档;记录 `cmd/locomo-bench/timeline.go` 可原子删除的回滚路径
- [ ] T042 [US3] 产物归档前逐文件扫描凭据(SSH host/port/password、API key、Bearer),确认**零命中**后再归档(FR-015)

---

## Phase 6: Polish & Cross-Cutting

- [ ] T043 [P] 为 `cmd/locomo-bench/timeline.go` 补齐包内注释:说明脚手架的立论(014 证伪"让模型自己推理"→ 改用确定性代码替代)与三条不变量(不臆造/可降级/确定性),使后来者不必读 spec 也能理解设计意图
- [ ] T044 [P] 在 `docs/locomo-e2e-eval-reproduction.md` 记录 `--temporal-date-scaffold` 的存在与默认状态(仅当 US2 判 GO 时才写入 canonical recipe;NO-GO 则只记"已试过、默认关")
- [ ] T045 清理会话 scratchpad 中的临时脚本与日志,确认仓库 `git status` 除预期改动外干净(临时文件**不得**留在仓库路径)

---

## Dependencies

```
Phase 1 (T001-T002) ─→ Phase 2 (T003) ─→ Phase 3 US1 (T004-T026) ─→ Phase 4 US2 (T027-T037) ─→ Phase 5 US3 (T038-T042)
                                                    │                                                      │
                                                    └──────────────── Phase 6 Polish (T043-T045) ←─────────┘
```

**硬依赖**:

- **T003 阻塞一切生产代码改动** —— 没有黄金基线就无法验证"逐字节不变"
- **T009(确认测试为红)阻塞 T010 起的所有实现** —— TDD 不可跳
- **T016 阻塞 T017** —— 先让脚手架本身绿,再接线
- **T024/T025 阻塞 Phase 4 整体** —— US1 不全绿不准花钱
- **T027(成本授权)阻塞 T028 起的所有 US2 任务** —— 未授权零成本
- **T030-T032 阻塞 T034-T037** —— 先有三臂数据才能判定
- **T037 阻塞 Phase 5** —— 先有结论才能写 verdict

**Story 独立性**:US1 可独立交付并验收(MVP);US2 依赖 US1 的正确性保证;US3 依赖 US2 的结论。

## Parallel Opportunities

**Phase 3 测试族(T004-T008)**:五条均在 `timeline_test.go` 的不同测试函数,可并行编写。
> ⚠️ 同文件写入,若由多人/多 agent 并行须协调;单人顺序写更省事。

**Phase 3 接线回归(T022-T023)**:分属 `timeline_test.go` 与 `bench_test.go`,**真正可并行**。

**Phase 6(T043-T044)**:分属代码注释与 docs,**真正可并行**。

**Phase 4 三臂(T030-T032)**:⚠️ **不可并行** —— 共享同一台 box 的 GPU,并行会互相拖慢并污染
冷启动判断;且 `ref` 作为噪声标尺必须与 `base` 在可比条件下跑。

## Implementation Strategy

**MVP = Phase 1 + 2 + 3(T001-T026)**。US1 独立完整:交付一个已证明正确的确定性脚手架,
零成本、可断网验收、开关默认关 —— 对 canonical recipe **零风险**。

**增量交付**:
1. **先交 MVP**,此时可以诚实地说"脚手架实现正确,是否有用未知";
2. **拿到成本授权后**再跑 US2,此时若 NO-GO 也已有干净归因(实现无 bug,是思路不转化);
3. **无论结局都做 US3**,把结论钉进 tracked docs。

**风险最高的一步是 T032→T037**:014 的教训是"给模型更多结构可能反而更差"。
若 `scaffold` 臂显著低于 `base`,那不是意外而是**已预期的失败模式之一**,
T038 的归因必须能识别它,而不是含糊记成"没效果"。
