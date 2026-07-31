# Tasks: 双基准查询期证据编译架构

**Input**: Design documents from
`specs/022-benchmark-parity-memory-architecture/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md),
[research.md](./research.md), [data-model.md](./data-model.md),
[contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: 本特性修改 engine、storage、extraction、retrieval 与 benchmark harness，所有
行为任务均按 TDD 执行：先写并确认测试失败，再实现，再运行对应 package 与回归门。

**Organization**: Phase 3–7 按 spec 的五个 User Story 排列。Event、gap 和窄用途
projection 是条件阶段；未满足前置 residual-cohort gate 时以 STOP verdict 完成对应实验，
不得为勾选任务预建产品层。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可在不同文件上并行，且不依赖尚未完成的同阶段任务
- **[US1]–[US5]**: 对应 [spec.md](./spec.md) 的 User Story
- 每项都包含执行或产物的精确仓库路径

## Phase 1: Setup（工作区与证据输入）

**Purpose**: 先吸收已收口的 021 和经核验的论文审计，避免在过期 harness 上生成实现。

- [X] T001 先提交或以可恢复方式保护当前 `specs/022-benchmark-parity-memory-architecture/` 与 `.specify/feature.json`，再将 022 branch rebase 到包含 `d9b8916` 的最新 `master`，确认 021 的 `cmd/locomo-bench/iris.go`、`cmd/locomo-bench/iris_test.go`、`cmd/locomo-bench/main.go` 无未解决重叠，并把实际 base SHA 记录到 `specs/022-benchmark-parity-memory-architecture/plan.md`
- [X] T002 核对并纳入 canonical 文献记录 `docs/research/high-scoring-memory-systems.md` 与 `docs/product/explorations/benchmark-parity-memory-architecture.md`，把 Merge CI 修正为 `[-0.204,-0.013]`，把 `recall>=0.95` 改为连续 coverage 分层而非通用阈值，且不改写另一 agent 的其他判断
- [X] T003 [P] 建立不含 dataset/secret 的 022 artifact fixture 说明与最小样例目录 `cmd/locomo-bench/testdata/022/README.md`
- [X] T004 在算法改动前运行 `CGO_ENABLED=0 go build ./...`、全量 Go tests、003 graph parity 与 namespace isolation，并把 commit、命令和结果记录到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

**Checkpoint**: 022 基于已收口的 021；canonical 论文证据没有已知数值/阈值错误；当前产品
健康状态有可追溯记录。

---

## Phase 2: Foundational（评测尺子、B0 连续性与 B1 provenance blocker）

**Purpose**: 建立 `022.v1` artifact、实际 token、B0 continuity 和正式 B1 所需的
provenance validity。B1 的候选/分数必须等 Ledger 提供真实 source/span 后才冻结。

**⚠️ CRITICAL (replanned 2026-07-30)**: T005–T019 是 Ledger 前的评测契约门；live
验证已证明 v6 fact candidates 没有 B1 所需的 source lineage。因此 T023–T039 是有效 B1
的硬前置，必须在 T021/T022 的正式 B1 部分之前执行。不得以 synthetic `legacy-entry:*`
绕过该 gate。

### Tests first — Foundational

- [X] T005 [P] 为 canonical protocol JSON、fingerprint、dirty-run policy 和 resume refusal 写失败测试到 `cmd/locomo-bench/eval_protocol_test.go`
- [X] T006 [P] 为 ranked anchors、rendered candidate byte replay、source coverage strata 和 candidate-set digest 写失败测试到 `cmd/locomo-bench/candidate_artifact_test.go`
- [X] T007 [P] 为 majority 聚合、任意 discordant 数的 two-sided exact McNemar、Holm category non-regression 和 GO/HOLD/STOP 写失败测试到 `cmd/locomo-bench/paired_eval_test.go`
- [X] T008 [P] 为全部 discordant + concordant 分层抽样、双 reviewer 盲标、adjudication、FN/FP 与 verdict-change 检测写失败测试到 `cmd/locomo-bench/judge_audit_test.go`
- [X] T009 [P] 为 `gold_unresolved|candidate_miss|compiler_miss|answerer_miss|success` 互斥分类和 diagnostic-only fixed-gold oracle 写失败测试到 `cmd/locomo-bench/miss_attribution_test.go`
- [X] T010 [P] 为 CJK、emoji、数字、时间、chat-template 边界、cap±1、fingerprint drift 和非加性 tokenizer 写失败测试到 `cmd/locomo-bench/token_counter_test.go`

### Measurement implementation

- [X] T011 定义供 B1 与 Compiler 共用的 provider-neutral `AnswerInput`、`TokenCounter`、candidate/source/action 基础类型到 `memory/evidencecompiler/types.go`
- [X] T012 实现 `022.v1` protocol canonicalization、hash、validation 和 resume refusal 到 `cmd/locomo-bench/eval_protocol.go`
- [X] T013 [P] 实现 ranked anchor、rendered candidate、continuous source coverage、JSONL round-trip 与 digest 校验到 `cmd/locomo-bench/candidate_artifact.go`
- [X] T014 [P] 实现 majority、exact McNemar、confidence interval、Holm category gate 与 promotion verdict 到 `cmd/locomo-bench/paired_eval.go`
- [X] T015 [P] 实现预注册 judge audit 采样、盲标导入、adjudication 和 raw/corrected summary 到 `cmd/locomo-bench/judge_audit.go`
- [X] T016 [P] 实现 source-ID 解析、互斥 miss attribution 和 diagnostic-only fixed-gold oracle 请求/不参与 promotion 的基础约束到 `cmd/locomo-bench/miss_attribution.go`
- [X] T017 实现与实际本地 answerer tokenizer/chat template 同栈的 counter、calibration 和 fail-closed fingerprint 到 `cmd/locomo-bench/token_counter.go`
- [X] T018 为 protocol→candidate→classification→summary 的完整/缺失/篡改 artifact 流写失败集成测试到 `cmd/locomo-bench/eval_protocol_integration_test.go`
- [X] T019 在 `cmd/locomo-bench/eval_runner.go`、`cmd/locomo-bench/journal.go`、`cmd/locomo-bench/jsonio.go`、`cmd/locomo-bench/stats.go` 和薄接线 `cmd/locomo-bench/main.go` 中集成 `022.v1`，并结构性关闭正式 arm 的 IRIS 与 legacy IDK retry

### Freeze and baseline

- [X] T020 先在 `cmd/locomo-bench/eval_protocol_integration_test.go` 写会复现 repetition candidate-set/trace/bundle drift 的失败测试，再修改 `cmd/locomo-bench/eval_replay.go`、`cmd/locomo-bench/eval_runner.go`、`cmd/locomo-bench/eval_artifact.go`、`cmd/locomo-bench/journal.go` 和薄接线 `cmd/locomo-bench/main.go`，使每题只物化一次 Candidate/Trace/Bundle、在首个 answer 前持久化、三次 answer repetition 逐字节重放且 drift fail closed；同时完成本地 answerer counter 校准并把测试结果、fingerprint 与既有无效运行登记到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [X] T109 在 `cmd/locomo-bench/eval_source_bundle_test.go` 先覆盖 active/tombstoned Evidence、CJK/emoji code-point span、展开后超 cap、citation/source-union 篡改和 replay 深拷贝，再实现 retrieval anchor→active Evidence 的 pre-pack source expansion、Bundle `items/sources`、Trace source receipt、独立 Ledger 重读 validator 与 source/span/citation 三项真实 validity；不得以 projection text 或 `SourceValid` 布尔代理原文/span/citation
- [X] T110 实现并测试 `cmd/locomo-bench/eval_fixed_gold_oracle.go` 的 `--fixed-gold-oracle` 独占执行路径：只接受三次 repetition 的冻结 B1 `legacy_count_packer` control，只装载全部 active gold Evidence，以 `dataset_source_ids` 区分 benchmark turn 与 Ledger ID，retrieval/extraction/embedding 调用为 0、整题不截断、同 provider/model/revision/prompt/input-output cap；合法 LongMemEval adversarial abstention 可为空 Evidence，任何分母/题级 artifact 无效时取消后续调度、summary 保留真实 answer/judge 调用数但不得输出部分分数，唯一 diagnostic 字段为 `correct/denominator/target_correct/target_met`
- [X] T111 先为 `cmd/locomo-bench/eval_runner.go`、`cmd/locomo-bench/eval_protocol.go`、对应 integration tests、`specs/022-benchmark-parity-memory-architecture/contracts/evaluation-artifacts.md` 和 `specs/022-benchmark-parity-memory-architecture/quickstart.md` 定义并实现独立 B0 continuity manifest/runner：真实记录 legacy retry，不生成或复用 B1 Candidate/Trace/Bundle，不接受 B1 validity/promotion；当前只支持 B1 的 freeze/runner 在本任务完成前不得被 T021 当作 B0
- [ ] T021 仅在 T044、T045、T109、T110、T111 均完成后，冻结 LoCoMo/LongMemEval 的 low/high、B0 与 post-Ledger B1 protocols，依 `specs/022-benchmark-parity-memory-architecture/quickstart.md` 用 WSL2 detach 规则运行 lossless B0、有效 B1 low/high 和同栈 diagnostic-only fixed-gold oracle；确认每题 retrieval/compile 只发生一次、每 repetition 只调用一次 answerer，并把 immutable artifacts 与无 secret hashes 记录到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T022 完成 B0/B1/oracle validity 与 judge audit，确认 1,540/500 分母、candidate lineage、span、token/call 字段、三次 Candidate/Trace/Bundle digest identity 均为 100%，`source_lineage_unavailable=0`、drift=0；按 FR-041（2026-07-30 replan）写唯一 F0 `CONTINUE | HOLD | STOP` 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`——`CONTINUE` = US1 Ledger 已落地 + B0/B1 artifact 有效 + judge audit 完整（**不再**卡 oracle 上界达 SC-002/SC-003；oracle 上界登记用于 candidate/compiler/answerer miss 归因）；只有 `CONTINUE` 才解锁 T046–T098 的正式满量执行

**Checkpoint**: B0 只用于历史连续性。v6 source-lineage failure 不产出 B1 分数；完成
T023–T045、T020、T109–T111 全部完成后才可生成所有后续机制使用的同题、同
candidate/cap、同 answerer/judge B1 control。T022 为 `HOLD` 时只修评测尺子；为
`STOP` 时结束 022 的机制扩张并另开 trained compiler/answerer 特性。

---

## Phase 3: User Story 1 — 不可损失、可核验的 Evidence Ledger（Priority: P1）🎯 MVP

**Goal**: 保存不可变 message/direct-write Evidence；Atomic Fact 等 projection 可删除/
重建并直接回链全部来源；tombstone 可恢复，privacy purge 清除可恢复内容。

**Independent Test**: 摄入多消息事实、直接写入、更新、抽取失败、merge、tombstone、
restore、purge 和两个 namespace；验证原文不变、active source chain 100%、跨域读取 0、
purge closure 完整且旧 Search/write parity 不变。

### Tests first — US1

- [X] T023 [P] [US1] 为 fresh v7、v6→v7 deterministic legacy backfill、幂等重跑、失败 rollback 和 003 表不变写失败 migration tests 到 `store/migrations_test.go`
- [X] T024 [P] [US1] 为 Evidence batch 原子性、external ID 幂等/冲突、状态机、UTF-8 content 与稳定 session ordering 写失败测试到 `memory/evidence_test.go`
- [X] T025 [P] [US1] 为 projection registry、完整 direct lineage、code-point span/digest、批量 source lookup 和 stale/disabled 状态写失败测试到 `memory/projection_test.go`
- [X] T026 [P] [US1] 为现有 Upsert 自动 self Evidence、同内容复用、改内容 append、`UpsertWithSources` 和 Delete-only-projection 写失败测试到 `memory/entrystore_test.go`
- [X] T027 [P] [US1] 为 Evidence-before-extraction、实际 source IDs、unknown/empty source 拒绝、模型失败保留 raw 和 duplicate fact union lineage 写失败测试到 `memory/pipeline/pipeline_test.go`
- [X] T028 [P] [US1] 为近重复 dedup/curation merge 的 source union、无来源 merge 禁止和 rollback 写失败测试到 `memory/curation/dedup_test.go`
- [X] T029 [P] [US1] 为 tombstone/restore、secure-delete purge closure、WAL checkpoint retry、异步 embedder stale-write race 和无内容审计写失败测试到 `memory/evidence_lifecycle_test.go`
- [X] T030 [P] [US1] 为 `memory_ingest_v2` 离线保存、Evidence get/lifecycle tools、旧 tool schema parity、同 source ID 跨 namespace 隔离和 secret safety 写失败测试到 `mcpserver/evidence_contract_test.go`

### Engine implementation — US1

- [X] T031 [US1] 添加 additive v7 Ledger/projection/episode tables、partial unique/indexes 与 deterministic legacy backfill 到 `store/migrations.go`
- [X] T032 [US1] 实现 `LedgerStore`、append-only lifecycle、typed errors、batch reads 和 secure-delete/WAL purge 到 `memory/evidence.go`
- [X] T033 [US1] 实现 projection registry、direct N:M lineage、span validation、batch source lookup 和 stale invalidation 到 `memory/projection.go`
- [X] T034 [US1] 保持现有签名并接入 self Evidence、`UpsertWithSources`、source union 与 projection-only delete 到 `memory/entrystore.go`
- [X] T035 [P] [US1] 给抽取 prompt 注入稳定 Evidence IDs 并要求每条 fact 返回实际 `source_ids`，更新 `memory/prompt/memory_extraction.go` 与 `memory/prompt/memory_extraction_test.go`
- [X] T036 [US1] 实现 `IngestDetailed` 的 Ledger-first 两事务流程、兼容 wrapper 和 degraded extraction 结果到 `memory/pipeline/pipeline.go`
- [X] T037 [US1] 在 curation 调用的 `EntryStore.Merge` 中直连所有实际 Evidence 并拒绝空 lineage 到 `memory/entrystore.go`，以 `memory/curation/dedup_test.go` 验证
- [X] T038 [US1] 用 active projection guard 防止 purge/tombstone 后 embedder 或 side-index stale 回写到 `memory/embedder.go`、`memory/vectorstore.go` 与 `memory/retriever.go`
- [X] T039 [P] [US1] 给 `memory.Result` 添加零值安全稳定 ID/projection kind，并保持 Search 排序与信号降级不变到 `memory/retriever.go`

### Adapter and verification

- [X] T040 [US1] 仅通过 engine API 接入 `memory_ingest_v2`、Evidence get/tombstone/restore/purge tools 到 `mcpserver/tools.go`、`mcpserver/server.go` 和 `mcpserver/registry.go`
- [X] T041 [P] [US1] 添加 100k Evidence/projection fixture、SQL query counter 和 batch-lineage benchmark，证明无 per-candidate N+1 到 `memory/projection_benchmark_test.go`
- [X] T042 [US1] 添加 ingest→fact→merge→tombstone→restore→purge 的离线端到端测试到 `memory/evidence_integration_test.go`
- [X] T043 [US1] 运行 US1 touched-package tests、`CGO_ENABLED=0 go build ./...` 和全量 `go test -count=1 ./...`，把结果记录到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [X] T044 [US1] 在 B1 同口径 slice 上运行 storage/extraction regression，确认默认 Search/write、003 graph、namespace 和 answerable mean 不回退，并记录 artifact hashes 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [X] T045 [US1] 按 `specs/022-benchmark-parity-memory-architecture/quickstart.md` 完成 US1 独立验收并在 `docs/evaluation/reports/benchmark-parity-memory-architecture.md` 记录 Ledger MVP checkpoint

**Checkpoint**: Evidence Ledger 可独立交付；即使 Episode/Compiler/Event 全未启用，既有写入
与搜索仍工作且所有 active Atomic Fact 可回到有效 source。

---

## Phase 4: User Story 2 — 同候选、同预算选择记忆表示（Priority: P1）

**Goal**: 分开测量 navigation 与 answer-facing rendering，在同算法/候选预算/cap 下比较
900-character chunk、raw-turn window 和可重建 Semantic Episode。

**Entry Gate**: T022 的 F0 verdict 必须为 `CONTINUE`（FR-041 2026-07-30 replan：即 US1
Ledger 已落地 + B0/B1 artifact 有效 + judge audit 完整，**不再**要求 oracle 上界达
SC-002/SC-003）；否则本阶段保持未执行并记录 `HOLD/STOP`，不得为勾选任务启动正式满量运行。

**Independent Test**: 同一 dataset/query 冻结 ranked anchors，三个 renderer 保存各自
source expansion、gold-source survival、token truncation 和逐题答案；删除 Episode 后
Ledger 不变并能确定性重建。

### Tests first — US2

- [ ] T046 [P] [US2] 为同 session 连续边界、deterministic narrative、builder/config hash、删除不删 Evidence 和 segmenter failure 降级写失败测试到 `memory/episode_test.go`
- [ ] T047 [P] [US2] 为共同 anchor、chunk/raw-window/episode source expansion、byte digest 和 exact-cap rendering 写失败测试到 `cmd/locomo-bench/representation_eval_test.go`
- [ ] T048 [P] [US2] 为三种表示使用同 query/embedding/pool/candidate budget 的 navigation shadow indexes 和删除隔离写失败测试到 `cmd/locomo-bench/representation_index_test.go`

### Implementation — US2

- [ ] T049 [US2] 实现只选择连续 Evidence 边界的 `EpisodeSegmenter`/`EpisodeStore`、确定性原文拼接和 rebuild/delete 到 `memory/episode.go`
- [ ] T050 [P] [US2] 实现 chunk_900、raw-turn window 和 Semantic Episode 的共同-anchor renderers 到 `cmd/locomo-bench/representation_eval.go`
- [ ] T051 [P] [US2] 实现 run-dir scoped、可删除且不污染产品 DB 的三表示 navigation shadow index 到 `cmd/locomo-bench/representation_index.go`
- [ ] T052 [US2] 为 navigation/rendering artifact 完整性、candidate budget/cap 相等和 Episode 故障降级添加端到端测试到 `cmd/locomo-bench/representation_integration_test.go`
- [ ] T053 [US2] 将 `representation_navigation` 与 `representation_rendering` 接入 `cmd/locomo-bench/eval_runner.go` 和薄 flags `cmd/locomo-bench/main.go`

### Experiments and gate — US2

- [ ] T054 [US2] 在 LoCoMo/LongMemEval low/high protocols 上运行三表示 navigation bake-off，并把 coverage/rank/truncation artifacts 与 hashes 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T055 [US2] 逐题 replay 同一 ranked anchors 运行三表示 answer-facing rendering bake-off，并把 candidate identity、token 与 answer artifacts 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T056 [US2] 对 primary arm/cohort 完成 judge audit、exact paired/category non-regression 与另一 benchmark gate，把 GO/HOLD/STOP 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T057 [US2] 仅当 verdict=GO 时在 `cmd/locomo-bench/eval_protocol.go` 冻结后续 Compiler 使用的 representation；HOLD/STOP 时保持 legacy representation 并记录原因
- [ ] T058 [US2] 用所选/保留表示重跑 comparable B1 slice，确认默认未启用时 parity 不变，并更新 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

**Checkpoint**: 后续 Compiler 只使用一个经 gate 选择的完整 rendered-candidate artifact；
没有表示过门时仍可继续用 legacy 表示验证 Compiler。

---

## Phase 5: User Story 3 — 固定候选 Query-time Evidence Compiler（Priority: P1）

**Goal**: 不改变 retrieval、不补检，将冻结 candidates 编译为来源有效、真实 token
不超 cap 的 Evidence Bundle；Planner 可选，失败时确定性 extractive fallback。

**Entry Gate**: T022 的 F0 verdict 必须为 `CONTINUE`（FR-041 2026-07-30 replan，同
Phase 4），且 T057 已冻结获选或保留的 representation；否则不得启动四臂正式满量评测。

**Independent Test**: 对同一 candidate bytes 运行 legacy、exact relevance、
deterministic extractive、local Planner 四臂；candidate digest 一致率、span 复原、
citation、cap 与单次 answerer 合规率均为 100%，无来源 ADD=0。

### Tests first — US3

- [ ] T059 [P] [US3] 为封闭 action union、字段矩阵、lineage allowlist、Unicode code-point span/digest、unknown action/ADD 拒绝写失败测试到 `memory/evidencecompiler/validate_test.go`
- [ ] T060 [P] [US3] 为 deterministic entity/time/operands/cardinality/update Need、Planner 不得删除显式 constraint 和有来源关系写失败测试到 `memory/evidencecompiler/need_test.go`
- [ ] T061 [P] [US3] 为 raw-fits 保留原文、over-cap 才 EXTRACT、EXTRACT 充分时拒绝 MERGE、EXTRACT 不充分才逐句验证 MERGE 写失败测试到 `memory/evidencecompiler/extractive_test.go`
- [ ] T062 [P] [US3] 为 resolver missing/tombstone/purge、counter nil/error/fingerprint drift、static prompt over-cap、invalid Planner fallback 和 cancellation 写失败测试到 `memory/evidencecompiler/compiler_test.go`
- [ ] T063 [P] [US3] 为 fixed-candidate post-freeze retrieval=0、gap disabled、invalid Bundle answerer=0、valid Bundle answerer=1 和 IDK 不重答写失败测试到 `cmd/locomo-bench/compiler_eval_test.go`

### Engine implementation — US3

- [ ] T064 [US3] 实现 Candidate/Need/Action/Trace/Bundle canonical validation、allowlist 与 typed errors 到 `memory/evidencecompiler/validate.go`
- [ ] T065 [P] [US3] 实现不依赖 benchmark category 的 deterministic Need/relationship builder 到 `memory/evidencecompiler/need.go`
- [ ] T066 [P] [US3] 实现 relevance ordering、raw-fit 检查、extractive span selection 和 MERGE 双条件 gate 到 `memory/evidencecompiler/extractive.go`
- [ ] T067 [US3] 实现 Compile orchestration、Planner proposal validation、deterministic fallback、完整 prompt 重计与 Trace/Bundle rendering 到 `memory/evidencecompiler/compiler.go` 和 `memory/evidencecompiler/render.go`
- [ ] T068 [US3] 通过窄 `Resolve(ids)` bridge 批量读取 active Ledger 且禁止 Search/query 到 `memory/evidencecompiler/source.go`

### Harness integration

- [ ] T069 [US3] 实现 legacy-count、exact-token relevance、deterministic extractive、optional local Planner 的 byte-replay arms 到 `cmd/locomo-bench/compiler_eval.go`
- [ ] T070 [P] [US3] 实现只提议 Need/actions、无 Store/answer 权限的可替换本地 Planner adapter 到 `cmd/locomo-bench/local_planner.go`
- [ ] T071 [US3] 将 retrieve→Compile→validate→exactly-one-answer 路径接入 `cmd/locomo-bench/eval_runner.go`，正式 arm 强制 `--no-idk-retry` 且不调用 `irisRetrieve`
- [X] T112 [US3] 为 compiler-arm formal 接入路径（`materializeFormalB1Question` + `compilerArm=extractive`）加厚 offline 单题集成测试到 `cmd/locomo-bench/eval_compiler_arm_integration_test.go`：新增多 anchor FullSource、单 anchor 多 source（异 session/event-date）、EXTRACT unicode span + KEEP 混合、以及 budget-impossible 诚实失效四个单题场景，分别守住 `56cef8b` 修复的 TextDigest 裸 64 位 hex / verbatim Evidence span / RenderedCandidates 不被 compiler 覆盖 / per-source SourceSessionID+EventDate 四类 formal 契约违反，并在 budget 不可行时拒绝静默产出假 valid bundle；全场景断言零 per-question invalid（无 invalid reason + `validateFormalFrozenPayload` + 跨产物 digest 同一性 + 独立 active-source 重读），engine 未触碰
- [ ] T072 [US3] 用同一正式本地 answerer runtime 重跑 counter calibration，要求全部 fixture delta=0，并更新 fingerprint 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

### Experiments and gate — US3

- [ ] T073 [US3] 在 LoCoMo/LongMemEval low/high protocols 上逐字节 replay 四个 Compiler arms，记录 candidate identity、fallback、source/span/citation、token/call/latency/cost artifacts 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T074 [US3] 完成 judge audit、continuous coverage strata、fixed-gold oracle、exact paired/category non-regression 与另一 benchmark gate，并写 GO/HOLD/STOP 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T075 [US3] 仅将双基准 GO 的 Compiler recipe 写入 `cmd/locomo-bench/eval_protocol.go`，随后运行 comparable regression slice 和全量 Go tests，把结果写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

**Checkpoint**: 核心路径完成。若此时已达到 LoCoMo ≥1,425/1,540 且 LongMemEval-S
≥473/500，停止 US4/US5 的 projection 建设，只记录“不需要”的 STOP verdict。

---

## Phase 6: User Story 4 — 独立验证 Event/State projection（Priority: P2，条件执行）

**Goal**: 只在核心路径仍有 temporal/update residual 时，分别验证 event object、date
operator 与 source recovery，禁止 bundle 归因。

**Independent Test**: E0/E1/E2/E3 在同一预注册 residual cohort、candidate/cap、
answerer/judge 下只有一个变量不同；每个 event 回到全部 Evidence，影子 projection
删除后 Ledger/默认路径不变。

- [ ] T076 [US4] 从 T074 artifacts 冻结 temporal/update residual cohort、primary arm 与继续条件到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`；若核心已达标或无 eligible residual，记录 STOP 并结束 US4
- [ ] T077 [P] [US4] 为 E0 current fields、E1 event object、E2 date operator、E3 source recovery 的单变量 flags、lineage 和相同 candidate/cap 写失败测试到 `cmd/locomo-bench/event_projection_test.go`
- [ ] T078 [P] [US4] 实现只存在于 run-dir、带完整 source IDs、可清空重建的 E1 event shadow objects 到 `cmd/locomo-bench/event_projection.go`
- [ ] T079 [P] [US4] 实现不改变候选集合的 E2 deterministic date operators 到 `cmd/locomo-bench/date_operator.go`
- [ ] T080 [P] [US4] 实现只沿现有 candidate lineage 的 E3 source-turn recovery 到 `cmd/locomo-bench/source_recovery.go`
- [ ] T081 [US4] 将 E0/E1/E2/E3 互斥 arms 接入 `cmd/locomo-bench/eval_runner.go`，并添加组合 flags 拒绝测试到 `cmd/locomo-bench/event_projection_test.go`
- [ ] T082 [US4] 在 LoCoMo/LongMemEval 预注册 temporal/update cohort 运行 E0/E1/E2/E3，同预算保存逐题 artifacts 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T083 [US4] 分别完成 judge audit、exact paired/category non-regression 与另一 benchmark gate，把每项 GO/HOLD/STOP 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T084 [US4] 仅对 GO 的单项在 `specs/022-benchmark-parity-memory-architecture/spec.md`、`data-model.md` 和 `contracts/` 追加独立 productization increment；未 GO 时不得添加 Event product table/default

**Checkpoint**: Event 仍是独立实验证据；没有任何“事件层”因架构完整性被预建。

---

## Phase 7: User Story 5 — 一次缺口补检与窄用途 projection（Priority: P3，条件执行）

**Goal**: 只对 Trace 明确的 entity/time-range/second-operand gap 补检一次；Scene、
Profile、003 graph 只在各自 residual cohort 独立验证。

**Independent Test**: 无 gap 检索 0 次；合法 gap 最多补检 1 次；control/treatment union
candidate、token、answer-call 上限相同；三种窄 projection 分开开关且默认关闭。

### Structured one-refetch

- [ ] T085 [P] [US5] 为三种合法 StructuredGap、自由文本/低置信度拒绝、最多一次补检和第二轮强制停止写失败测试到 `cmd/locomo-bench/gap_retrieval_test.go`
- [ ] T086 [P] [US5] 为 control=N、treatment=(N-r)+r、union<=N、共同 cap 和一次 answerer 的公平预算写失败测试到 `cmd/locomo-bench/gap_budget_test.go`
- [ ] T087 [US5] 实现从 validated Trace 确定性渲染一次 gap query、补检和稳定 candidate union 到 `cmd/locomo-bench/gap_retrieval.go`
- [ ] T088 [US5] 将 two-compile/one-refetch/one-answer orchestration 接入 `cmd/locomo-bench/eval_runner.go`，并保持 `memory/evidencecompiler` 无 Retriever 依赖
- [ ] T089 [US5] 在预注册 eligible questions 上运行 one-pass control 与 one-refetch treatment，完成 judge audit/paired gate并写 artifacts/verdict 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

### Narrow projections

- [ ] T090 [US5] 从 candidate_miss residual 冻结 Scene=cross-session、Profile=preference/current-state、graph=missing-bridge 三个互斥 cohorts 和 STOP 条件到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T091 [P] [US5] 为三种 projection 独立开关、source lineage、相同 candidate/cap 和 003 contract/data 不变写失败测试到 `cmd/locomo-bench/projection_experiment_test.go`
- [ ] T092 [P] [US5] 实现仅用于 cross-session candidate expansion 的可删除 Scene shadow view 到 `cmd/locomo-bench/scene_projection.go`
- [ ] T093 [P] [US5] 实现仅用于 preference/current-state 的可删除 Profile shadow view 到 `cmd/locomo-bench/profile_projection.go`
- [ ] T094 [P] [US5] 通过现有 003 public API 实现 missing-bridge graph experiment adapter 到 `cmd/locomo-bench/graph_projection.go`，不得修改 `memory/graph.go` 或 v3 schema
- [ ] T095 [US5] 在各自预注册 cohort 分别运行 Scene/Profile/graph arms，保存逐题 candidate/source/cap artifacts 到 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T096 [US5] 对 Scene/Profile/graph 分别完成 judge audit、exact paired/category non-regression 与另一 benchmark gate，把 GO/HOLD/STOP 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T097 [US5] 只为 verdict=GO 的 projection 在 `specs/022-benchmark-parity-memory-architecture/spec.md`、`data-model.md` 和 `contracts/` 建立后续 productization increment；HOLD/STOP projection 保持不存在于默认请求路径
- [ ] T098 [US5] 用全部已 GO、同一公开 recipe 的机制跑双基准 target checkpoint，并把是否达到 1,425/1,540 与 473/500 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`

**Checkpoint**: 查询至多一次 gap retrieval、一次 answerer；003 合同未变；只有经独立双基准
gate 的机制有资格进入产品化设计。

---

## Phase 8: Polish & Cross-Cutting Concerns

**Purpose**: 完成文档、性能、安全、宪法和最终双基准门；不得用清理阶段偷改算法或评测
配置。

- [ ] T099 [P] 把最终机制证据、正确分母/judge 口径、Merge CI 和非阈值化 coverage 结论同步到 `docs/research/high-scoring-memory-systems.md` 与 `docs/product/explorations/benchmark-parity-memory-architecture.md`
- [ ] T100 [P] 更新公开能力/路线/竞争边界且不把实验臂写成已交付能力到 `docs/product/capabilities.md`、`docs/product/roadmap.md` 和 `docs/evaluation/competitors.md`
- [ ] T101 [P] 更新 022 当前分数、artifact hashes、成本与所有负结果到 `docs/evaluation/results.md`、`docs/evaluation/experiment-verdicts.md` 和 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T102 运行 `node --test docs/validation/check-docs.test.mjs` 与文档链接/metadata 校验，修复本特性引入的问题到 `docs/validation/check-docs.mjs` 或对应 022/docs 文件
- [ ] T103 运行 `CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...` 和 `CGO_ENABLED=0 go vet ./...`，把完整输出摘要写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T104 运行 migration rollback、deterministic parity、MCP schema、namespace isolation、003 graph unchanged 和 offline degradation 门，并把结果写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T105 运行 100k Evidence/projection 性能、batch lineage 无 N+1、Compiler candidate bounds 和 purge checkpoint 压测，把诚实边界写入 `docs/product/capabilities.md`
- [ ] T106 [P] 执行 secret/log/artifact 扫描与 privacy purge 恢复性检查，修复范围限于 `mcpserver/secrets_test.go`、`cmd/locomo-bench/testdata/022/README.md` 和相关 engine tests
- [ ] T107 用冻结 default recipe 完成 LoCoMo 1,540 与 LongMemEval-S 500 full runs、judge audit、逐题 artifacts、exact paired/category gates和成本统计，把最终 hashes/verdict 写入 `docs/evaluation/reports/benchmark-parity-memory-architecture.md`
- [ ] T108 对照 `.specify/memory/constitution.md` 和 `specs/022-benchmark-parity-memory-architecture/spec.md` 审核五项原则与 SC-001–SC-015；若双目标或必需合同仍未满足，使用 `speckit-converge` 把剩余证据支持的工作追加到 `specs/022-benchmark-parity-memory-architecture/tasks.md`，不得把 022 标为完成

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 — Setup**: 无代码依赖；T001 后才能可靠修改 harness。
- **Phase 2 — Foundational**: 依赖 Phase 1；T005–T020、T109–T111 冻结评测契约和
  B1/B0 前置；T111 完成后 B0 才可独立运行。有效 B1 须再等待 US1 的
  T023–T045 source-chain/checkpoint；
  T022 F0 是后续满量机制的共同入口门。
- **Phase 3 — US1**: 依赖 T005–T019；Ledger 是所有 source-aware 表示与 Compiler 的地基，
  并阻塞 T021/T022 的正式 B1 部分。
- **Phase 4 — US2**: 依赖 US1 与 T022=`CONTINUE`；Episode/raw-window 必须从 Ledger 重建。
- **Phase 5 — US3**: 实现依赖 US1 与 T022=`CONTINUE`，正式 candidate replay 依赖 US2 verdict。
- **Phase 6 — US4**: 依赖 US3 residual；核心已达双目标时以 STOP verdict 结束。
- **Phase 7 — US5**: one-refetch 依赖 US3 Trace；窄 projection 依赖 residual cohort，
  可在 US4 STOP 后继续。
- **Phase 8 — Polish**: 依赖所有实际执行的阶段；T107/T108 是最终完成门。

### User Story Dependencies

```text
Measurement contract + B0 continuity
        │
        ▼
US1 Evidence Ledger
        │
        ▼
Valid B1 causal control
        │
        ▼
F0 low/high + fixed-gold oracle
        │
   CONTINUE only
        │
        ▼
US2 Representation ──> US3 Fixed-candidate Compiler
                              │
                     ┌────────┴────────┐
                     ▼                 ▼
               US4 Event          US5 One-refetch
                (conditional)       + narrow views
                     └────────┬────────┘
                              ▼
                         Final dual gate
```

- **US1** 可作为独立 MVP 交付，不要求任何新表示或模型。
- **US2** 仅在 F0=`CONTINUE` 后可独立给出三表示 GO/HOLD/STOP；即使全部 STOP，US3
  仍可使用 legacy 表示。
- **US3** 可独立证明 post-retrieval compilation；Planner unavailable 时 deterministic
  extractive arm 仍完整。
- **US4**、**US5** 的独立成功是“完成受控实验并遵守 verdict”，不是强制出现 GO。

### Within Each User Story

1. 所有标记 “Tests first” 的任务先运行并确认因缺失行为失败。
2. Schema/types → store/service → adapter/harness → integration。
3. Unit/contract 全绿后才运行付费或长时间 benchmark。
4. Config/protocol commit 与 algorithm commit 分离；result/verdict 再单独提交。
5. 任一 hard validity 失败先修 artifact，不解释 accuracy。

## Parallel Opportunities

### Setup/Foundation

- T003 可与 T004 的环境检查并行，但不得开始算法实现。
- T005–T010 分布在六个测试文件，可并行编写。
- T013–T016 分布在独立 artifact/statistics 文件，可在 T011/T012 contract 稳定后并行。

### Parallel Example: User Story 1

```text
Task T023: migration/backfill tests in store/migrations_test.go
Task T024: Ledger API tests in memory/evidence_test.go
Task T025: projection/lineage tests in memory/projection_test.go
Task T027: ingest provenance tests in memory/pipeline/pipeline_test.go
Task T030: MCP contract/isolation tests in mcpserver/evidence_contract_test.go
```

实现时 `memory/evidence.go` 与 `memory/projection.go` 可在 migration contract 冻结后并行；
`entrystore.go`、pipeline、curation 与 MCP wiring 等待二者公开 API。

### Parallel Example: User Story 2

```text
Task T046: Episode engine tests in memory/episode_test.go
Task T047: renderer tests in cmd/locomo-bench/representation_eval_test.go
Task T048: shadow-index tests in cmd/locomo-bench/representation_index_test.go
```

T050 renderer 与 T051 shadow index 使用不同文件，可在 Episode contract 稳定后并行。

### Parallel Example: User Story 3

```text
Task T059: action/source validation tests
Task T060: deterministic Need tests
Task T061: raw-fit/extractive/MERGE gate tests
Task T063: harness one-answer/fixed-candidate tests
```

T065 deterministic Need、T066 extractive packer、T070 local Planner adapter 可并行；T067
Compiler orchestration 等待其公开行为。

### Parallel Example: User Story 4

```text
Task T078: Event shadow object
Task T079: Date operator
Task T080: Source-turn recovery
```

三项是互斥 treatment 文件，可并行实现，但 T081 必须拒绝组合启用。

### Parallel Example: User Story 5

```text
Task T085: StructuredGap/one-refetch tests
Task T086: cumulative budget tests
Task T091: projection experiment isolation tests
Task T092: Scene shadow view
Task T093: Profile shadow view
Task T094: 003 graph experiment adapter
```

Scene/Profile/graph 可并行，但必须分别运行、分别 verdict。

## Implementation Strategy

### MVP First — Evidence Ledger

1. 完成 Phase 1 与 T005–T019，先获得可信 measurement contract 与 B0 prerequisites。
2. 完成 US1 的 tests、v7、Ledger/projection lineage、MCP thin adapter。
3. 完成 T020 单次物化/重复重放，以及 T109 active Evidence/Bundle/validator、T110
   executable fixed-gold oracle 和 T111 B0 continuity runner 前置。
4. 在 T045 独立确认无 Episode/Compiler 时基础写入搜索仍完整后，才执行 T021 low/high
   B1 + fixed-gold oracle。
5. T022 只有 `CONTINUE` 才进入表示或 Compiler；`HOLD` 只修尺子，`STOP` 结束本特性扩张。

### Incremental Delivery

1. **Measurement foundation + B0** → 可信的 protocol、预算与历史连续性。
2. **US1 Ledger** → 不可损失、可追踪、可隐私删除的事实地基。
3. **Valid B1** → post-Ledger 单次物化、三次重放的 low/high control。
4. **F0 ceiling** → same-stack fixed-gold oracle 给出唯一 `CONTINUE | HOLD | STOP`。
5. **US2 Representation** → 仅在 `CONTINUE` 后选择过门表示，允许全部 NO-GO。
6. **US3 Compiler** → 固定候选证明 answer-facing contract。
7. **US4/US5** → 只按 residual 缺口引入单变量机制。
8. **Final gate** → 同一公开 recipe 同时达到双目标才完成；否则 converge，不粉饰结果。

### Commit Isolation

按以下顺序分开提交：

1. measurement schema/tests；
2. frozen protocol/config、calibration 与 B0 manifest；
3. Ledger schema/API；
4. repetition replay tests 与实现修复；
5. post-Ledger B1 manifest/control artifact；
6. fixed-gold oracle 与 F0 verdict；
7. 每个单一 mechanism 的 tests+implementation；
8. 该 mechanism 的 results/verdict；
9. 默认 recipe promotion；
10. 最终文档。

## Notes

- `[P]` 仅表示文件级可并行，不授权多个 agent 同时编辑 `main.go`、`eval_runner.go`、
  `entrystore.go` 或 `tasks.md`。
- 运行 artifact、dataset、DB、模型文件和凭据只进 session scratchpad/gitignore。
- 远程 GPU 按 `docs/operations/evaluation/remote-gpu-runbook.md` 使用，空闲必停。
- 付费 hosted reranker/recall 只能 diagnostic，不能作为 GO/default 得分。
- 每个 benchmark task 先 `--estimate`，再按 WSL2 `setsid` detach；不得前台等待。
- 未满足 SC-002/SC-003、artifact validity、judge audit 或宪法门时，022 不得标为完成。
