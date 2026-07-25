# Tasks: 离线固结 · 跨 session 桥接合成

**Feature**: `015-consolidation-bridging` | **Date**: 2026-07-25
**Input**: [plan.md](./plan.md) · [spec.md](./spec.md) · [research.md](./research.md) · [data-model.md](./data-model.md) · [contracts/](./contracts/) · [quickstart.md](./quickstart.md)

## 执行纪律（不可跳步）

```
Phase 1 证伪门 ──判死──► 全部作废，归档判决，Phase 2+ 永不执行
    │
   通过
    ▼
Phase 2 基础(v6 migration) ──► Phase 3 US2 合成 ──► Phase 4 US3 后台 ──► Phase 5 回归门
```

**Phase 1 未通过之前，MUST NOT 写任何引擎代码。** 这是本特性最重要的纪律。

## 并行度说明

- `[P]` = 可与同批次其他 `[P]` 任务并行（文件不重叠、无未完成依赖）
- 三个外部 agent 的分派见文末「Agent 分派」一节
- **Phase 1 的真实并行度为 1–2**（判决类工作，拆多了协调成本大于收益）
- **Phase 3 的第一批并行度为 3**（按文件边界完全隔离）

---

## Phase 1: 证伪门（US1，P1）— 门禁，判死即终止

**Story Goal**: 在写任何引擎代码之前，用已有历史产物免费判定方案是否可行。

**Independent Test**: 只用历史评测产物与已存在的 `--coverage-only` 模式即可完整
执行，不依赖本特性任何新代码，产出含四项判据实测值的判决报告。

**载体**: 一次性诊断脚本，置于会话 scratchpad，**不进代码库**（research R6）。

- [ ] T001 [US1] 从 HF 私有集 `wallfacers/engram-locomo-artifacts` 拉回门 0 所需产物到会话 scratchpad：`009-eval-runs/009-full-A-base/run-*/results-hybrid.jsonl`、`014b-oldtplan-confirm/014b-trace/trace.jsonl`、`009-bge-chunks-store/conv*.db`。产物 MUST NOT 进入仓库任何路径。
- [ ] T002 [P] [US1] 在 scratchpad 写 `gold_pairs.py`：从 LoCoMo 数据集的 `locomoQA.Evidence`（`["D1:1","D2:1"]`）经 `--chunks` 的 entry↔turn 映射解析出每题的 gold entry 集合，再取 `source_session_id` 互异的两两组合得到「gold 跨 session 对」。输出 `gold_pairs.json`。
- [ ] T003 [P] [US1] 在 scratchpad 写 `enumerate.py`：复刻 data-model.md §4 的候选枚举（全量倒排 → 按 entity_norm 分桶 → 跨 session 过滤 → `IDF=log(N/df)` 累加打分 → 全局排序取 top-K=2000，并施加 `MaxBucketSize`）。输出每 conv 的候选对集合与总规模。
- [ ] T004 [US1] 合流 T002+T003 计算门 0 两项判据并出报告：判据 A 分母=多跳失败题中「至少存在一个 gold 跨 session 对」的题数、分子=其中「至少一个此类对出现在候选集」的题数；判据 B=10 conv 候选对总数。「多跳失败题」= 009-full-A-base 三次重复中多数判错的 category-1 题。
- [ ] T005 [US1] **门 0 判决**：A ≥ 60% 且 B ≤ 2 万 → 通过，进 T006；A < 40% 或 B > 5 万 → **判死，终止整个特性**；A 落 40–60% 灰区 → 只允许调整**一次** K/IDF 阈值重测，仍在灰区按判死处理。判决写入 `docs/locomo-score-levers.md`。
- [ ] T006 [US1] 门 0 通过后：对每道命中题取最高分 gold 对，用**固定模板拼接**（**不调用 LLM**）成 oracle 桥接 entry，插入 009 store 的**副本**（原 store 不得改动），并为新 entry 补实体与向量。
- [ ] T007 [US1] 在 oracle store 副本上运行 `--coverage-only`（零回答模型调用），按 quickstart.md 的 setsid detach 规范执行，产出 coverage.json。
- [ ] T008 [US1] **门 1 判决**：判据 C=oracle 桥接进对应题 top-30 的比例（分母=插入的 oracle 桥接总数）≥ 50%；判据 D=coverage@30 相对基线 Δ **> 0 严格大于**，MUST 沿用 `cmd/locomo-bench/coverage.go` 的 `evidenceRecallAt` 同一口径。任一不过 → **判死，终止整个特性**。
- [ ] T009 [US1] 判决归档：四项判据实测值 + 结论写入 `docs/locomo-score-levers.md`；脚本与原始产物推到 HF 私有集。**无论通过还是判死都必须归档。**

**Checkpoint**: 门 0 与门 1 均通过，方可进入 Phase 2。任一判死则本特性到此为止。

---

## Phase 2: 基础设施 — 阻塞所有后续故事

- [ ] T010 在 `store/migrations_test.go` 追加 v6 migration 测试（**先写，必须失败**）：断言 `memory_bridges` 表与三个索引在迁移后存在、`pair_key` 唯一索引拒绝重复、down 迁移干净移除。
- [ ] T011 在 `store/migrations.go` **追加** `v6ConsolidationBridges` / `v6ConsolidationBridgesDown` 常量并在 `migrationsByVersion` 追加 `{Version: 6, ...}`。DDL 严格按 data-model.md §1。**MUST NOT 修改 v1–v5 任何一行**。
- [ ] T012 验证 T010 转绿：`CGO_ENABLED=0 go test -count=1 ./store/`，并跑 `CGO_ENABLED=0 go build ./...` 零错误。

**Checkpoint**: v6 已落地且不破坏既有迁移，后续故事可开工。

---

## Phase 3: 桥接合成（US2，P2）— 核心能力

**Story Goal**: 显式驱动一趟固结，产出可被常规检索命中的桥接记忆，且只增不改。

**Independent Test**: 在装载了跨 session 记忆的库上显式触发一趟固结，验证新记忆
已产生、原有记忆逐条未变、新记忆可被正常检索。

### 第一批 — 三路完全隔离，可 3 agent 并行

- [ ] T013 [P] [US2] 在 `memory/consolidation/candidates_test.go` 写候选枚举的**失败**测试：确定性（两次调用序列完全相同、同分按 `(A,B)` 字典序）、跨 session 过滤（同 session 对不出现）、共享实体必需（无共享实体不出现）、IDF 打分与 top-K 截断、`MaxBucketSize` 超限跳过、二阶禁止（已在 `memory_bridges` 的 entry 不进候选）、`PairKey()` 与枚举顺序无关。全部离线零模型。
- [ ] T014 [P] [US2] 在 `memory/consolidation/verdict_test.go` 写裁决解析与校验的**失败**测试：`ParseVerdict` 对 NONE 记号/不可解析输出均返回 `Bridged=false` **且不返回错误**；`ValidateVerdict` 对空内容、悬空源引用、源与候选对不一致、内容与任一源等价（去空白后相同）五种情形逐一拒绝。
- [ ] T015 [P] [US2] 在 `memory/prompt/` 新建 `consolidation.go` 固结提示词常量：MUST 显式授予模型拒绝权并规定拒绝时的确切输出记号；MUST 要求回述两个源标识供校验；MUST 要求非冗余（不得只复述任一源事实）。照既有 extraction/curation 提示词的组织方式。

### 第二批 — 依赖第一批的对应测试

- [ ] T016 [P] [US2] 实现 `memory/consolidation/candidates.go`：`CandidatePair`、`PairKey()`、`EnumerateCandidates`，按 data-model.md §4 的单条 JOIN 查询 + 内存分桶。依赖 T013。
- [ ] T017 [P] [US2] 实现 `memory/consolidation/verdict.go`：`BridgeVerdict`、`ParseVerdict`、`ValidateVerdict`，规则严格按 data-model.md §2.2。依赖 T014。

### 第三批 — 编排，依赖前两批全部完成

- [ ] T018 [US2] 在 `memory/consolidation/worker_test.go` 写 worker 的**失败**测试（模型用 stub 闭包）：NONE 拒绝闸落库数为 0 且无残留；悬空引用拒绝落库+告警且整趟继续；ADD-only（pass 前后逐条比对源 entry 内容与总数完全不变）；幂等（连续两趟 `memory_bridges` 行数不变、无重复 entry）；`call == nil` inert 零副作用；`embedder == nil` 仍落库不 panic；单对失败不中断整趟。
- [ ] T019 [US2] 实现 `memory/consolidation/worker.go` 的 `Config`、`ModelCaller`、`PassStats`、`NewWorker`、`RunPass`：编排「枚举 → 幂等跳过 → 模型合成 → 校验 → 落库」。落库序列严格照 research R3：`Upsert → PutEntities → UpsertEdges → embedder.Enqueue`，实体取两源并集（R4），随后 `INSERT OR IGNORE INTO memory_bridges`。所有失败 fail-safe（WARN + 跳过 + 继续）。
- [ ] T020 [US2] 复用 `curation.NewLease(db)` 接入 `RunPass` 的领导租约与 heartbeat（research R2：与 curation 共用单例锁、必然互斥，此为预期语义）。
- [ ] T021 [US2] 验证 Phase 3 全绿：`CGO_ENABLED=0 go test -count=1 ./memory/consolidation/ ./memory/prompt/ ./store/` 并 `CGO_ENABLED=0 go build ./...`。

**Checkpoint**: US2 可独立演示——显式触发一趟固结并验证三项核心不变式。

---

## Phase 4: 后台作业（US3，P3）— 产品化形态

**Story Goal**: 随记忆增长自动固结，多实例安全，中断可恢复。

**Independent Test**: 两个 worker 指向同一库只有一个执行；中断后重跑不重复处理。

- [ ] T022 [US3] 在 `memory/consolidation/worker_test.go` 追加**失败**测试：两个 worker 指向同一 DB 只有一个真正执行；中断后重跑只补未完成部分（幂等已保证，此处验证行为）；`TopKPerPass` 上限生效；`EntryCountLow` 水位线下不执行且不产生告警噪声；任何失败被捕获且不影响既有检索/写入。
- [ ] T023 [US3] 实现 `Notify`（非阻塞去抖，buffered(1) trigger）、`Start`（后台循环至 ctx 取消）、水位线判定与单趟上限，照 `curation.Worker` 的 `run`/`shouldRun`/`heartbeat` 模子。inert worker 上 `Notify`/`Start` 均为安全空操作。
- [ ] T024 [US3] 验证：`CGO_ENABLED=0 go test -count=1 ./memory/consolidation/` 全绿。

---

## Phase 5: 收口与回归门（宪法 IV，硬门禁）

- [ ] T025 全量测试：`CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./...` 全绿（含既有 parity 与 namespace isolation 硬门）。
- [ ] T026 引擎表面零改动核验：`git diff --name-only` 确认引擎既有文件中**只有** `store/migrations.go` 被改动（且仅为追加），`memory/retriever.go`、`mcpserver/`、`cmd/locomo-bench/` 零改动。
- [ ] T027 **全量 LoCoMo 回归门**：用既有 canonical 配方（`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned --retrieval hybrid`）跑固结前/后对比。要求 multi-hop 正确率相对基线**提升**、整体正确率**不低于**基线，用 paired McNemar 判显著性。遵守冷启动纪律（首臂预热丢弃）与 setsid detach 规范。
- [ ] T028 结论写入 `docs/locomo-score-levers.md`；**eval 配置改动与算法改动分开提交**（宪法 IV 可归因要求）。

---

## Dependencies

```
Phase 1 (T001–T009)  ── 硬门禁，判死即全部终止
        │
        ▼
Phase 2 (T010→T011→T012)  ── 阻塞所有后续
        │
        ▼
Phase 3 第一批 [T013 | T014 | T015]  ── 3 路并行，文件零重叠
        │
        ▼
Phase 3 第二批 [T016 | T017]  ── 2 路并行（T016←T013, T017←T014）
        │
        ▼
Phase 3 第三批 T018→T019→T020→T021  ── 串行编排
        │
        ▼
Phase 4 T022→T023→T024
        │
        ▼
Phase 5 T025→T026→T027→T028
```

**关键串行边**：T011（migration）必须在任何 consolidation 代码之前；T019（worker
编排）必须在 T016/T017 之后；T027（回归门）必须在全部实现之后。

## Agent 分派（3 个外部 codex agent）

| 批次 | Agent A | Agent B | Agent C | 并行度 |
|---|---|---|---|---|
| **B0** Phase 1 | T001–T009 全部 | 待命 | 待命 | **1**（判决类，拆分收益 < 协调成本） |
| **B1** Phase 2 | T010–T012 | 待命 | 待命 | **1**（单文件，阻塞项） |
| **B2** Phase 3 第一批 | T013（candidates_test） | T014（verdict_test） | T015（prompt） | **3** |
| **B3** Phase 3 第二批 | T016（candidates） | T017（verdict） | 待命 | **2** |
| **B4** Phase 3 第三批 | T018–T021 | 待命 | 待命 | **1**（编排，需全局视野） |
| **B5** Phase 4 | T022–T024 | 待命 | 待命 | **1** |
| **B6** Phase 5 | T025–T028 | 待命 | 待命 | **1**（回归门需独占 box） |

**诚实结论**：本特性的真实并行度只在 B2/B3 达到 3 和 2，其余批次串行。这是纪律
（前置门禁）与架构（单文件编排）的必然结果，不是拆分不够努力。强行三路并行只会
制造文件冲突与返工。

## 文件冲突矩阵（B2/B3 并行安全性证明）

| 任务 | 触碰的文件 | 与其他并行任务重叠 |
|---|---|---|
| T013 | `memory/consolidation/candidates_test.go` | 无 |
| T014 | `memory/consolidation/verdict_test.go` | 无 |
| T015 | `memory/prompt/consolidation.go` | 无 |
| T016 | `memory/consolidation/candidates.go` | 无 |
| T017 | `memory/consolidation/verdict.go` | 无 |

**零重叠，可安全并行。**

## Implementation Strategy

**MVP = Phase 1（US1）本身**。哪怕结论是判死，它也交付了一个有价值、可归档的
判决——这正是它被定为 P1 的原因。

增量交付：Phase 1 判决 → Phase 2+3 核心能力（可演示、可评测）→ Phase 4 产品化
形态 → Phase 5 收口。

**任何阶段的失败都不应污染既有能力**：本特性全程 ADD-only，引擎既有文件仅
`store/migrations.go` 追加，回滚代价 = 删表 + 删包。

## Task Count

| 阶段 | 任务数 |
|---|---|
| Phase 1 证伪门（US1） | 9 |
| Phase 2 基础 | 3 |
| Phase 3 合成（US2） | 9 |
| Phase 4 后台（US3） | 3 |
| Phase 5 收口 | 4 |
| **总计** | **28** |
