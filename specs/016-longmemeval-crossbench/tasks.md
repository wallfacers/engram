# Tasks: LongMemEval 子集先行 · 跨 benchmark 复现 coverage≠answer

**Input**: Design documents from `/specs/016-longmemeval-crossbench/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/loader-contract.md, quickstart.md

**Tests**: 包含测试任务 —— 宪法「测试先行」要求引擎行为变更 TDD；本特性虽是 adapter，
但 loader 是全部数字的信任根，同样按 TDD 执行。

**Organization**: 按用户故事分组。US1 是**不可跳过的前置门禁**，不过则 US2/US3 作废。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无未完成依赖）
- **[Story]**: 所属用户故事（US1/US2/US3）

## 并行度诚实声明

本特性改动面极小：`cmd/locomo-bench/longmemeval.go` **单文件** + `main.go` **一行** +
一份新夹具。**真实并行度很低**，绝大多数实现任务串行于同一文件。下文只在任务确实
落在不同文件且无依赖时标 `[P]`，不为凑并行虚标。

---

## Phase 1: Setup

**Purpose**: 确认基线状态，登记判据

- [x] T001 确认工作树基线：运行 `git status --short` 与 `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/`，记录当前全绿状态到会话 scratchpad；确认 `docs/locomo-score-levers.md` 与 `docs/paper-outline-eval-reliability.md` 的并发修改属于其他工作，本特性不触碰这两个文件
- [x] T002 确认数据就位：断言 `testdata/longmemeval/longmemeval_oracle.json` 与 `testdata/longmemeval/longmemeval_s_cleaned.json` 均为 500 题，且 `git check-ignore` 确认两者已被忽略

---

## Phase 2: Foundational（阻塞所有用户故事）

**Purpose**: 判据登记 —— 必须在**任何**测量之前完成

**⚠️ CRITICAL**: T003 未完成前，不得执行任何产生正确率数字的任务

- [x] T003 判据原文单独落盘于 `specs/016-longmemeval-crossbench/criterion.txt`（避免同文件自引用导致哈希范围歧义），登记记录与 SHA256 写入 `specs/016-longmemeval-crossbench/criterion-registered.md`（复现 = 条件增益 ∈ [20,50]pp 且检索侧当量 < 答题侧当量；证伪 = 条件增益 > 60pp 或检索侧当量 > 答题侧当量；其余 = 无法判定；任一桶 n<20 标记不可判），计算其 SHA256 并一并写入，随后 git commit —— 该文件在 P4 用于逐字比对（SC-008），**提交后不得修改**

---

## Phase 3: User Story 1 - 让 LongMemEval 可被诚实测量（Priority: P1）🎯 MVP

**Goal**: 评测工具能正确读入真实数据集，并证明其证据覆盖率读数是真实的。

**Independent Test**: 在 oracle 30 题小样本上运行只读覆盖诊断（零答题/判分调用），
精确证据覆盖率 ≥ 0.95。

**⚠️ 门禁**: **T021**（G-尺子门）不过 ⇒ **停止，归档判决，本特性作废**，
MUST NOT 进入 Phase 4。门禁是本阶段的**最后一项**，不是中途任何一项。

### 测试先行（US1）

- [ ] T004 [US1] 在 `testdata/longmemeval/sample_array.json` 手写真实形状夹具：数组套数组的 `haystack_sessions`、与之等长的 `haystack_dates` 与 `haystack_session_ids`、消息带 `has_answer`、含一道 `single-session-preference` 题、含一道无任何 `has_answer` 的题；**不得从数据集拷贝内容**（规避再分发）
- [ ] T005 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：加载 `sample_array.json` 时 `single-session-preference` 题型可被接受（当前硬报错，FR-001）
- [ ] T006 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：各会话的 `Date` 等于 `haystack_dates` 中同下标项且**互不相同**（当前全部塌缩为 `question_date`，FR-002）
- [ ] T007 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：构造 `haystack_dates` 长度与 `haystack_sessions` 不一致的输入，断言 `loadLongMemEval` **返回错误**且不回落到任何默认日期（FR-003）
- [ ] T008 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：每条写入的 turn 其 `DiaID` 形如 `D<会话序>:<消息序>`（两序号从 1 起），且能被 `evidenceReferencePattern` 匹配；被跳过的空消息不占用序号（FR-004）
- [ ] T009 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：`has_answer:true` 的消息、且仅这些消息，其 `DiaID` 出现在该题 `Evidence` 中（FR-005）
- [ ] T010 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：无任何 `has_answer` 的题其 `Evidence` 为空，且经 `evidenceRecallAt` 判定 `gradeable == false`（FR-007）
- [ ] T011 [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 新增失败测试：构造 chunk 命中，断言 `evidenceRecallAt` 在合成证据上返回预期的 turn recall 与 session recall（验证证据同构后计量自动成立，research R2，FR-004）
- [ ] T012 [US1] 确认既有 `TestLoadLongMemEvalSMapsAllQuestionTypes`（对象形式 `sample.json`）**保持通过**，不得删除或弱化 —— 它覆盖 `parseLongMemEvalSession` 的对象分支

### 实现（US1）—— 全部落在同一文件，串行

- [ ] T013 [US1] 在 `cmd/locomo-bench/longmemeval.go` 新增解析字段：`longMemEvalRecord.HaystackDates []string`(`haystack_dates`)、`longMemEvalRecord.HaystackSessionIDs []string`(`haystack_session_ids`)、`longMemEvalMessage.HasAnswer bool`(`has_answer`)；不改任何既有字段（FR-006）
- [ ] T014 [US1] 在 `cmd/locomo-bench/longmemeval.go` 的 `longMemEvalTypes` 追加 `{12, "single-session-preference"}`（**复用** id 12，与既有 `preference` 并列为同义名，research R8，FR-001）
- [ ] T015 [US1] 在 `cmd/locomo-bench/longmemeval.go` 改造 `parseLongMemEvalConversation` 与 `parseLongMemEvalSession`：按下标绑定 `haystack_dates`（长度不匹配返回错误）、为每条写入的 turn 合成 `DiaID`、由 `has_answer` 收集该题 `Evidence` 并经 `loadBenchmarkDataset` 写入 `locomoQA.Evidence`；对象形式分支的内嵌日期**优先**，保持既有行为（FR-002、FR-003、FR-004、FR-005、FR-007）
- [ ] T016 [US1] 在 `cmd/locomo-bench/main.go` 将 `categoryLabel(12)` 的返回值由 `"preference"` 改为 `"single-session-preference"`（research R8：避免答题侧与覆盖侧两份产物对同一批题用不同名字）
- [ ] T017 [US1] 运行 `CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/`，T005–T012 全部转绿
- [ ] T018 [US1] 用真实数据集全量验证读取：对 `longmemeval_oracle.json` 与 `longmemeval_s_cleaned.json` 各跑一次零调用读取探测，断言 500/500 题成功加载、日期塌缩为单一值的题数为 0（SC-001、SC-002）

### G-尺子门（US1）

- [x] T019 [US1] 写一次性脚本切出 30 题 smoke 子集为 `<scratchpad>/oracle_smoke30.json`，脚本落 scratchpad 不入库（FR-008）。**选取规则（实测修正）**：oracle 文件**按题型排序**，「顺序取前 30」实际只覆盖 temporal-reasoning 一种题型、且 30 题全部有证据 —— G1（`single-session-preference` 首见于第 200 题，正是今天硬报错的题型）与 G4（无证据题首见于第 55 题）**一次都不会被触发**，尺子门会在两处缺口未受检的情况下亮绿灯。改为确定性覆盖抽取：按题型名排序，每型取前 4 道**有证据**题（24 道），再取文件序前 6 道**无证据**题，并集还原文件序。实测 = 30 题 / 6 型全覆盖 / 24 有证据 + 6 无证据 / 712 条消息（比顺序取的 819 条更省）
- [ ] T020 [US1] 建 smoke 库：按 quickstart 的 setsid detach 纪律运行 `--dataset-format longmemeval --coverage-only`，embedding 走本地服务、抽取走小额付费口；**脚本必须插桩 usage 并记录实测成本，不得预先拍数**（FR-008）
- [ ] T021 [US1] **G-尺子门判定（不过即停止）**：断言精确证据覆盖率 ≥ 0.95 且日志中答题模型调用数与判分模型调用数均为 0（FR-009、SC-003）。**覆盖率的分母是 24 道可评分题** —— 另 6 道无证据题按 FR-007 判为 `gradeable=false`，不进覆盖率统计；但它们**必须**被观察到确实走了该路径，这正是 T019 把它们放进 smoke 集的目的。**未达标 ⇒ 停止本特性，把实测值与结论写入 `specs/016-longmemeval-crossbench/verdict.md` 并终止，MUST NOT 进入 Phase 4**

**Checkpoint**: US1 完成即已交付价值 —— LongMemEval 从「读不进/无尺子」变为「可被诚实测量」，
即使不做 US2/US3 也是可独立验收的增量。

---

## Phase 4: User Story 2 - 完美证据下的答题上限（Priority: P2）

**Goal**: 得到零干扰项、完美证据条件下的正确率，作为该 benchmark 的答题侧天花板。

**Independent Test**: oracle 全 500 题的总体正确率与分题型正确率。

**⚠️ 依赖远程评测机排队** —— 该机当前由其他工作（MemOS 复现）占用。本阶段全部任务
均需排队；**Phase 3 不得被本阶段的排队阻塞**。

- [ ] T022 [US2] 写 G-向量门独立只读脚本 `<scratchpad>/check_vectors.py`：给定 store 目录与模型名，逐库比对 `count(memory_embeddings WHERE model=?)` 与应有行数，输出 data-model §4.2 的 JSON，`total_missing > 0` ⇒ `pass:false`。**不进 bench**（research R5：bench 内硬断言会把 `--retrieval fts` 这条本就无向量的合法路径变成错误）
- [ ] T023 [US2] 建 ORACLE 库：对 `longmemeval_oracle.json` 全 500 题运行建库，store 目录与 S 臂**分开**（research R7）；setsid detach + 文件轮询（FR-011）
- [ ] T024 [US2] 跑 G-向量门：对 ORACLE store 执行 T022 脚本，`total_missing` 必须为 0；不为 0 则重复建库直至补齐（Backfill 受有界队列限制，一趟补不完），并把每轮实测行数落盘（FR-010、SC-004）
- [ ] T025 [US2] ORACLE 答题 + 判分：canonical 配方 `--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned --retrieval hybrid`，产出逐题结果与 `stats.json`、`cost.json`（FR-011）
- [ ] T026 [US2] 汇总 ORACLE 总体正确率与 6 种题型分项正确率，断言结果覆盖全部 500 题、缺失为 0（FR-012、SC-005）

**Checkpoint**: ORACLE 臂本身即一手数据（此前为空），可独立解读。

---

## Phase 5: User Story 3 - 真实检索下的分账与最终判决（Priority: P3）

**Goal**: 真实检索条件下按覆盖分桶做分账，对照登记判据给出最终结论。

**Independent Test**: 三个覆盖桶的题数/正确率、条件增益、两侧当量，以及三选一结论。

**⚠️ 依赖远程评测机排队**，且为成本大头（≈8.4× LoCoMo 建库量）。

- [ ] T027 [US3] 写分层抽样一次性脚本：按配额 multi-session 27 / temporal-reasoning 27 / knowledge-update 15 / single-session-user 14 / single-session-assistant 11 / single-session-preference 6 抽 100 题，固定种子（FR-013）
- [ ] T028 [US3] 产出两个抽样产物：`longmemeval_s_subset100.json`（格式与源文件一致，可直接 `--data`）与 `subset100_question_ids.json`（含 seed、配额、id 列表）；两者 gitignore，归档 HF 私仓（FR-014）
- [ ] T029 [US3] 验证抽样可复现：重复执行脚本两次，断言产出的 `question_id` 集合完全一致（SC-006）
- [ ] T030 [US3] 建 S 臂库：对子集文件运行建库，store 目录与 ORACLE 分开；setsid detach（FR-011）
- [ ] T031 [US3] 跑 G-向量门：对 S 臂 store 执行 T022 脚本，`total_missing` 必须为 0（FR-010）
- [ ] T032 [US3] S 臂答题 + 判分 ×3：canonical 配方，`repeats=3` 抑制方差（FR-015）
- [ ] T033 [US3] 产出逐题证据覆盖：对 S 臂运行只读归因/覆盖诊断，得到每题的精确证据覆盖率（FR-016）。**尺子裁定（评审修正）**：归因 trace 的 `retrieved[].mapped_gold_turns` 由 `attribution.go:165 hitMappedGoldTurns` 产生，它有两条分支 —— chunk 命中走 `chunkTurns` 精确轮次交集，**fact 命中走 `factCoversGoldTurn` 的会话门控词包含（tau）模糊配对**。而 `coverage.go:19 evidenceRecallAt`（G-尺子门与此前全部 LoCoMo 数字所用的尺子）**只认 chunk 血缘，fact 一律不计**。逐题覆盖率 MUST 按名字前缀 `chunk-c` 过滤后再算，否则一个特性内并存两把不可比的尺子，而本特性的全部目的就是跨 benchmark 比同一把尺子。LoCoMo 实测两者为 0.841（宽松）vs 0.808（严格）
- [ ] T034 [US3] 分桶分账（**输入必须是 T033 的严格尺子覆盖率**）：按覆盖率分全覆盖 / 部分覆盖 / 零覆盖三桶，输出各桶题数、正确率、Wilson 区间；**任一桶 n < 20 标记 `judgeable:false`**（FR-016、FR-018、SC-007）
- [ ] T035 [US3] 计算条件增益（全覆盖正确率 − 零覆盖正确率）、检索侧当量（零覆盖题数 × 条件增益）、答题侧当量（全覆盖仍答错的题数），输出 data-model §4.3 结构（FR-017）
- [ ] T036 [US3] **判决**：读回 T003 登记的判据原文，校验其 SHA256 未变，逐字对照后给出「复现 / 证伪 / 无法判定」三选一；**不得四舍五入到任一侧，不得事后调整判据**（FR-019、SC-008）
- [ ] T037 [US3] 计算 ORACLE vs S 同题配对的正确率对比作为上限锚，报告中**必须标注含干扰项密度混杂（约 24 倍稀释），只作上界不作因果**（FR-023）

---

## Phase 6: Polish & 归档

- [ ] T038 引擎零改动硬验证：运行 `git diff --name-only -- memory embedding provider store internal`，断言输出为空（FR-025、SC-009）
- [ ] T039 全量回归：`CGO_ENABLED=0 go build ./...` 零错误 + `CGO_ENABLED=0 go test -count=1 ./...` 全绿
- [ ] T040 LoCoMo 路径零行为变更核验：对 `--dataset-format locomo` 跑一次既有零调用探测（如 `--estimate`），确认输出与 T001 记录的基线一致
- [ ] T041 写 `specs/016-longmemeval-crossbench/verdict.md`：数据集版本与子集规模明示（不得简称全量）、官方废弃旧版本的事实、两臂结果、分桶分账、最终判决、以及本次数字与第三方旧版数字不可直接对比的声明（FR-020、FR-021、FR-022）；并显式列出本次范围排除项（官方分题型判分口径 / 弃答子集 / longmemeval_m / V2 / 干预臂，FR-026）
- [ ] T042 回填 `docs/paper-outline-eval-reliability.md` 的 RQ6 状态（SC-010）；**提交前先 `git status` 确认该文件无其他工作的未提交改动，有冲突则停下升级，不得覆盖**（FR-024）
- [ ] T043 归档：脚本与原始产物推 HF 私仓；确认数据集、store、run 目录均未入库

---

## Dependencies & 执行顺序

```
Phase 1 (T001-T002)
      ↓
Phase 2 (T003 判据登记) ←── 阻塞所有产生正确率的任务
      ↓
Phase 3 US1 (T004-T021)  ← 全本地，不受远程机排队影响
      ↓
   T021 G-尺子门 ──不过──► 停止，写 verdict.md，特性作废
      ↓ 通过
Phase 4 US2 (T022-T026)  ← 需排队
      ↓
Phase 5 US3 (T027-T037)  ← 需排队，成本大头
      ↓
Phase 6 (T038-T043)
```

**故事间依赖**：US2 与 US3 均**强依赖** US1（尺子），彼此之间无依赖 —— 但 US3 的
上限锚对比（T037）需要 US2 的结果，故实际顺序为 US1 → US2 → US3。

## 并行机会（真实，未虚标）

| 可并行组 | 任务 | 理由 |
|---|---|---|
| A | T004 与 T022 | 不同文件（夹具 vs 独立脚本），无依赖；T022 可提前写好等 US2 |
| B | T005–T011 | 均在 `longmemeval_test.go`，**同文件 ⇒ 不标 [P]**，但可由同一人一次性写完 |
| C | T027 与 T022 | 两个独立脚本，不同文件 |

**其余任务全部串行**：T013–T016 同属 `longmemeval.go`/`main.go` 的实现链且互相依赖；
T023–T026、T030–T037 是评测流水线，天然串行。

**结论：本特性真实并行度 ≈ 2**，不适合多 agent 并行分工；建议单线程执行。

## 文件冲突矩阵

| 文件 | 涉及任务 | 冲突风险 |
|---|---|---|
| `cmd/locomo-bench/longmemeval.go` | T013, T014, T015 | 高 —— 必须串行 |
| `cmd/locomo-bench/longmemeval_test.go` | T005–T012 | 高 —— 必须串行 |
| `cmd/locomo-bench/main.go` | T016 | 低 —— 仅一行，且与 longmemeval.go 无重叠 |
| `testdata/longmemeval/sample_array.json` | T004 | 无 —— 新文件 |
| scratchpad 脚本 | T019, T022, T027 | 无 —— 各自独立文件 |
| `docs/paper-outline-eval-reliability.md` | T042 | **中 —— 其他工作正在改，提交前必须核对** |

## Implementation Strategy

**MVP = Phase 1 + Phase 2 + Phase 3（US1）**。US1 单独交付即有价值：把 LongMemEval
从「读不进、无尺子」变成「可被诚实测量」，且全程本地、不依赖远程算力。

US1 的 G-尺子门是**真正的止损点** —— 它用约 658 条消息的成本，决定要不要投入
8.4× LoCoMo 建库量的 US3。这条纪律直接来自 015 的教训（设计建在未经测量的前提上）
与 A 实验的教训（伪影差点被当作模型结论报出）。
