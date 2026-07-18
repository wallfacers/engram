# Tasks: 生物启发检索涨点 —— 顺序可测四枪闭合 Mem0 gap

**Input**: Design documents from `/specs/003-bio-retrieval-locomo/`

**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/, quickstart.md

**Tests**: 宪法「测试先行」适用于引擎行为——引擎类任务先写可失败测试再实现；bench
统计/费用/加载器亦配单元测试（离线、确定性）。

**Organization**: 按 user story 分组；US1→US2 为测量地基（先行），US3→US4→US5 为
三枪机制，每枪结尾有「维护者跑评测判定」checkpoint（涉及 API 花费，由维护者执行，
先 `--estimate` 算账）。

## Format: `[ID] [P?] [Story] Description`

## Path Conventions

单 Go module，仓库根：`memory/`、`store/`、`embedding/`、`cmd/locomo-bench/`。

---

## Phase 1: Setup

**Purpose**: 记录基础设施就位（代码骨架已存在，无脚手架需求）

- [x] T001 创建 `specs/003-bio-retrieval-locomo/eval-log.md` 记录模板（每枪：预估费用/
      实际费用/stats/compare verdict/保留-回退结论），并在 `.locomo-run/env.sh` 注释里
      补 `LOCOMO_PRICE_TABLE` 示例

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: migration v3 与 Entry 结构扩展——US3/US4/US5 的共享 schema 地基
（单一 Migration 单元，一次交付；US1/US2 不依赖，可与本阶段并行）

- [ ] T002 在 `store/migrations.go` 追加 Migration v3：`memory_entity_edges` 表 +
      两索引、`memory_event_aliases` 表 + FTS5 镜像 + 同步 trigger 三件套、
      `memory_entries` 加 `event_start/event_end/superseded_by` 三列（Down 对称回滚），
      并先在 `store/migrations_test.go` 写 v2→v3→v2 迁移往返可失败测试
- [ ] T003 同步 Go 侧 Entry：`memory/entrystore.go` 的 `Entry` 结构体、
      `entrySelectCols`、`upsert`、`scanEntry` 增加 EventStart/EventEnd/SupersededBy；
      `TestRetrievalParity` 与既有单测保持全绿（零值语义=现状）

**Checkpoint**: `go test ./store ./memory` 全绿，旧库自动升级 v3 无数据动作

---

## Phase 3: User Story 1 - 可信的多跑评测地基 (Priority: P1) 🎯 MVP

**Goal**: `--repeats`/`--compare`/`--estimate`/费用账/LME_S 加载器——让任何涨点
可信地测在噪声之上（FR-001/002/003/013/014）

**Independent Test**: 离线单测验证统计正确性；对拍两个人造 run-dir 验证 compare 输出

### Tests for User Story 1

- [ ] T004 [P] [US1] 在 `cmd/locomo-bench/stats_test.go` 写可失败测试：已知样本的
      均值±95%CI（t 分布）、McNemar 已知列联表 p 值（含小样本二项精确路径）、
      CI 重叠判定、`verdict` 规则（above-noise 二判据其一）
- [ ] T005 [P] [US1] 在 `cmd/locomo-bench/cost_test.go` 写可失败测试：价目表解析
      （含 unpriced 模型标注）、分桶累计、estimate 计算、`answer_context_tokens_mean`
      与 1.5× WARNING 阈值
- [ ] T006 [P] [US1] 在 `cmd/locomo-bench/longmemeval_test.go` 写可失败测试：用
      `testdata/longmemeval/sample.json` 小 fixture 验证 LME_S 解析、7 题型→桶映射、
      abstention→对抗口径标记

### Implementation for User Story 1

- [ ] T007 [P] [US1] 实现 `cmd/locomo-bench/stats.go`：run 层均值±95%CI（标准库
      math，t 临界值表内置）、题层 McNemar（卡方近似+小样本精确）、compare.json/
      stats.json 序列化（契约 bench-cli.md §1）
- [ ] T008 [P] [US1] 实现 `cmd/locomo-bench/cost.go`：从 provider 响应捕获 usage
      （`provider/openai`、`provider/anthropic` 回传 token 数透传到 bench 记账钩子；
      embedding 客户端 `embedding/embedding.go` 同样接钩子，本地端点单价 0 仍记
      calls/tokens）、`LOCOMO_PRICE_TABLE` 解析、`--estimate` 模式、cost.json 落盘
      与报告尾打印（契约 bench-cli.md §4/§5）
- [ ] T009 [US1] 在 `cmd/locomo-bench/main.go` 接线 `--repeats N`：逐 run 落
      `run-<i>/results.jsonl`（question_id/category/correct/answer/token 用量），
      跑完聚合写 stats.json；`--compare A B` 子模式读双 run-dir 对齐 question_id
      输出配对报告；同步接线 `--no-idk-retry`（禁用两级重试，判定口径必开，
      默认关=现行为）（依赖 T007）
- [ ] T010 [US1] 实现 `cmd/locomo-bench/longmemeval.go`：`--dataset-format
      longmemeval` 加载 LME_S haystack（带时间戳会话）走既有抽取/建库/答题/判分链，
      题型映射与 per-桶报告（依赖 T006 fixture；契约 bench-cli.md §3）
- [ ] T011 [US1] 报告层打通：`aggregator` 扩展 per-桶 CI 输出、`reportDelta` 兼容
      compare 模式、`results.jsonl` 记录 LME 原始题型（`cmd/locomo-bench/main.go`）

**Checkpoint**: 全部离线单测绿；`--estimate` 对 locomo10 输出费用预估；
人造 run-dir 对拍 compare verdict 正确

---

## Phase 4: User Story 2 - 模型 regime 校准基线 (Priority: P1)

**Goal**: gpt-5.6-sol 答题 + 抽取 A/B，产出全程冻结的校准基线（FR-004）

**Independent Test**: 仅切模型 env 重跑，基线与模型贡献被记录在 eval-log.md

### Implementation for User Story 2

- [ ] T012 [US2] 校验 bench 对 OpenAI 协议中转站的答题/抽取模型切换纯 env 化无死角
      （`LOCOMO_MODEL`/`EXTRACT_MODEL`/`LOCOMO_PROVIDER` 贯通 usage 记账），补
      `cmd/locomo-bench/main.go` 缺口并在 quickstart.md 修正命令（依赖 T008）
- [ ] T013 [US2] 产出 Strike 0 运行脚本 `.locomo-run/strike0.sh`（quickstart §Strike 0
      的三步：estimate → A/B 两库多跑 → compare），内置价目表与 run-dir 约定；
      所有运行命令带 `--no-idk-retry`（判定口径）
- [ ] T014 [US2] ⏸️ **维护者执行**：跑 strike0.sh（先 estimate 确认花费）；本方把
      两库 stats/compare、冻结抽取模型决定、校准基线、模型贡献差值与实际费用记入
      `specs/003-bio-retrieval-locomo/eval-log.md`

**Checkpoint**: 校准基线（均值±CI）已冻结留档——后续三枪的一切对比以此为参照

---

## Phase 5: User Story 3 - 多跳联想检索 (Priority: P2)

**Goal**: 共现图 + IDF 种子 + 质心游走 depth-2 + 原 query 重排，作第 4 路 RRF 信号
攻 multi-hop 66%→80+（FR-005/006，research D2）

**Independent Test**: 离线 fixture 验证游走与无回退；维护者多跑 compare 判定

### Tests for User Story 3

- [ ] T015 [P] [US3] 在 `memory/graph_test.go` 写可失败测试：UpsertEdges 规范化
      （a<b、共现累计、syn 权重）、NeighborsOf 两向查询、EntityDocFreq、depth-2
      游走含 IDF 加权与深度截断
- [ ] T016 [P] [US3] 在 `memory/retriever_test.go` 增可失败测试：
      `TestAssociativeNoRegression`（单跳 fixture 开 Associative 后 top-1 不变）+
      `TestSignalDegradation` 扩展（边表空/无 cue/无 embedding 三行降级矩阵）

### Implementation for User Story 3

- [ ] T017 [P] [US3] 实现 `memory/graph.go` + `memory/entrystore.go` 边表访问器：
      `EntityEdge`/`UpsertEdges`/`NeighborsOf`/`EntityDocFreq`（契约 engine-api §2）
- [ ] T018 [P] [US3] 写入侧建边：`memory/pipeline/pipeline.go` storeFact 内同 entry
      实体两两 UpsertEdges；`memory/curation/worker.go` curation pass 内离线补同义边
      （扫 `memory_embeddings` 余弦>0.8，复用 `embedding.Cosine`）
- [ ] T019 [US3] 实现游走信号并接入检索：`memory/graph.go` cue→IDF 种子→depth-2
      质心游走→原 query embedding 重排；`memory/retriever.go` 加 `RetrieverOptions`/
      `NewRetrieverWithOptions`、`fuseRRF` 扩可变多路、`associativeRanks` 第 4 路
      （依赖 T017；契约 engine-api §1）
- [ ] T020 [US3] query-to-entity 全句匹配：`memory/entities.go` 增整句对
      `entity_raw` 的 FTS/子串匹配路径并入 `entityRanks`（`memory/retriever.go:308`）
- [ ] T021 [US3] bench 接线 `--assoc`/`--assoc-depth`（`cmd/locomo-bench/main.go`
      透传 RetrieverOptions），报告带 flag 指纹入 results.jsonl
- [ ] T022 [US3] ⏸️ **维护者执行**：Strike 1 多跑（estimate→跑→compare vs 校准基线）；
      判定 above-noise 且无类别回退且 token≤1.5×→保留；否则触发备选 PPR（新任务
      追加）或记负结果——结论与费用记入 eval-log.md

**Checkpoint**: multi-hop 判定完成，`--assoc` 去留有据

---

## Phase 6: User Story 4 - 检索侧时间结构化 (Priority: P3)

**Goal**: 事件时间范围 + 词汇别名（写入侧）+ 规则时间窗解析 + T_score（查询侧），
攻 temporal（FR-007，research D3）；诚实门：within-noise 即砍

**Independent Test**: 解析器表驱动单测；别名 FTS 召回 fixture；维护者多跑判定

### Tests for User Story 4

- [ ] T023 [P] [US4] 在 `memory/temporal_test.go` 写可失败测试：表驱动覆盖
      "last month / in May 2023 / recently / 去年 / 上周" 等模式 + 锚点归一化 +
      无时间意图返回 ok=false + `event_start<=event_end` 校验
- [ ] T024 [P] [US4] 在 `memory/retriever_test.go` 增可失败测试：T_score 软乘排序
      影响、硬过滤窗外剔除、无 event 范围/无时间意图两行降级矩阵；
      `memory/entrystore_test.go` 增 PutAliases + 别名 FTS union 召回

### Implementation for User Story 4

- [ ] T025 [P] [US4] 实现 `memory/temporal.go`：`ParseTemporalIntent`（纯规则，
      契约 engine-api §3）与 `T_score = exp(-|event-窗中心|/τ)`
- [ ] T026 [P] [US4] 抽取扩展：`memory/prompt/memory_extraction.go` 增 event_start/
      event_end/aliases 字段（缺省容错）；`memory/pipeline/pipeline.go` 落库时间范围
      并 `PutAliases`（契约 engine-api §4/§5——同一次调用顺带产出，调用数不变）
- [ ] T027 [US4] 检索接线：`memory/retriever.go` keywordRanks union 别名 FTS；
      rerank 阶段命中时间意图乘 T_score，`TemporalHardFilter` 可选硬过滤
      （依赖 T025；契约 engine-api §1）
- [ ] T028 [US4] bench 接线 `--temporal-score`/`--temporal-hard-filter`
      （`cmd/locomo-bench/main.go`），会话/提问时间戳注入 `RetrieverOptions.Now`
- [ ] T029 [US4] ⏸️ **维护者执行**：Strike 2 多跑 compare（vs Strike 1 获胜配置）；
      temporal above-noise→保留，within-noise→砍（负结果照记 eval-log.md，含费用）

**Checkpoint**: temporal 判定完成，时间机制去留有据

---

## Phase 7: User Story 5 - 拒答校准与冲突消解 (Priority: P3)

**Goal**: Abstain-R1 1:4 ICL 拒答契约（口径改动，独立提交）+ curation 四分类非破坏
压制（引擎改动），攻对抗题 27% 编造（FR-008/009/010，research D4）

**Independent Test**: 压制生命周期离线单测；维护者 `--adversarial` 多跑判定

### Tests for User Story 5

- [ ] T030 [P] [US5] 在 `memory/entrystore_test.go` 写可失败测试
      `TestSupersedeLifecycle`：Supersede 校验（存在性/非自指/非 pinned）、
      Unsupersede 回退、检索默认降权、时间意图查询不惩罚（契约 engine-api §4/§7）
- [ ] T031 [P] [US5] 在 `memory/curation/judge_test.go` 增可失败测试：四分类 JSON
      解析（含旧格式无 conflicts 容错）、apply 顺序 merge→conflicts→evict、
      Contradictory→Supersede 落库

### Implementation for User Story 5

- [ ] T032 [P] [US5] 实现 `EntryStore.Supersede/Unsupersede`（`memory/entrystore.go`）
      与检索降权：`memory/retriever.go` 融合后乘 `SupersededPenalty`、时间意图豁免
- [ ] T033 [US5] curation 四分类：`memory/prompt/curation_judge.go` prompt 扩展、
      `memory/curation/judge.go` `ConflictDecision` 解析、`memory/curation/worker.go`
      apply 接 Supersede（依赖 T032；契约 engine-api §6）
- [ ] T034 [US5] bench 接线 `--conflict-resolution`/`--superseded-penalty`
      （`cmd/locomo-bench/main.go`，作用于建库/curation 阶段）
- [ ] T035 [US5] **独立提交（口径改动）**：`cmd/locomo-bench/runner.go` 增
      Abstain-R1 1:4 ICL 答题 prompt（拒答须指出缺失信息）；`--abstain-prompt` flag
      门控，仅替换答题 prompt——重试禁用由既有 `--no-idk-retry` 承担（T009），
      二者独立组合；编造率/误拒率进 per-桶报告
- [ ] T036 [US5] ⏸️ **维护者执行**：Strike 3 多跑（`--adversarial`，compare vs
      Strike 2 获胜配置）；判定编造率显著降且误拒 within-noise→保留；记 eval-log.md

**Checkpoint**: 三枪判定全部完成，获胜 flag 组合确定

---

## Phase 8: Polish & Cross-Cutting Concerns

- [ ] T037 ⏸️ **维护者执行**：LongMemEval_S 终态验证（先 `--estimate` 分批算账，
      获胜 flag 组合 `--repeats 3`）；SC-001/SC-002 达标判定记 eval-log.md
- [ ] T038 [P] 文档同步：README.md（新 flag 与多跑用法）、`docs/memory-strategy.md`
      附二补「已落地/已回退」状态标注
- [ ] T039 [P] CI 离线门禁扩展：`.github/` workflow 增
      `TestAssociative*|TestSupersede*|temporal` 测试与 `CGO_ENABLED=0 go build`
      （沿用 002 既有 workflow 模式）
- [ ] T040 quickstart.md 校验走查：按手册从零执行一遍离线路径（build/test/estimate），
      修正偏差；确认「算法 commit 与口径 commit 分开」在 git 历史中成立

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: 无依赖
- **Phase 2 Foundational**: 无依赖（US1/US2 不等它；它只 block US3/US4/US5）
- **Phase 3 US1**: 仅依赖 Setup——**与 Phase 2 可并行**
- **Phase 4 US2**: 依赖 US1（多跑/费用账/compare 是校准基线的工具）
- **Phase 5 US3**: 依赖 Phase 2 + US2 checkpoint（校准基线冻结后才能归因）
- **Phase 6 US4**: 依赖 US3 判定（叠加在获胜配置上测）
- **Phase 7 US5**: 依赖 US4 判定（同上）；T035 口径改动独立提交
- **Phase 8 Polish**: T037 依赖三枪判定；T038-T040 可随进度并行

### 关键路径

T001 → T004..T011(US1) → T013 → **T014(维护者)** → T015..T021(US3，其中 T002/T003
需先毕) → **T022(维护者)** → T023..T028(US4) → **T029(维护者)** → T030..T035(US5)
→ **T036(维护者)** → T037

### Parallel Opportunities

- T002/T003（Foundational）与整个 US1（T004-T011）并行
- 每 story 内标 [P] 的测试先并行写，再并行实现不同文件（如 T017/T018、T025/T026）
- 维护者跑评测期间（T014/T022/T029/T036），下一 story 的测试与实现可先行开发
  （flag 默认关，不影响判定中的配置）

---

## Implementation Strategy

**MVP = Phase 1-4（测量地基 + 校准基线）**：纯离线开发零 API 花费；T014 是第一次
花钱节点（先 estimate）。此后每枪「开发（免费）→ 维护者跑（花钱、先算账）→ 判定
（保留/回退）」循环，任一枪 within-noise 都不阻塞下一枪开发，只影响叠加配置的选择。

**费用节点一览（全部先 `--estimate` 后执行）**: T014（两库×5 跑）、T022、T029、
T036（各 1 配置×5 跑 + compare）、T037（LME_S 建库 + 3 跑，最大头，分批）。
