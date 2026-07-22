# Tasks: 归因门控的检索排序(evidence-gated retrieval ranking)

**Feature**: `009-retrieval-attribution-gate` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

> 傻瓜式执行:确切字段/CLI/schema/判定门在 [contracts/](./contracts/) 已冻结,照抄。产物落 gitignored `.locomo-run/009-*/`。凭据只走 env/隧道绝不落文件。WSL2 长跑 setsid 分离。**全局硬门**:US1 期间引擎 `git diff --name-only -- memory embedding provider store internal` 必空;死规则——禁云/付费 reranker。TDD:先写失败测试再实现。

---

## Phase 1: Setup(共享前置)

- [ ] T001 确认前置就绪:008 固化 store 路径可用、`.locomo-run/008-us4-e2e/results-hybrid.jsonl` 存在、LoCoMo dataset 可读;记录到 `.locomo-run/009-attribution/preflight.txt`(不含凭据)
- [ ] T002 在 `.gitignore` 确认 `.locomo-run/` 已忽略(应已有,仅核对)

---

## Phase 2: Foundational(阻塞 US1 的共享前置)

- [ ] T003 [P] 复用性核对:确认 `cmd/locomo-bench/evidence.go`(`evidenceReferences`/`evidenceSessions`/`exactTurnRecall`)、`chunks.go`(`chunkTurns`)、`coverage.go`(`coveredGoldTurns`)可直接调用,不改其语义(research D1)
- [ ] T004 在 `cmd/locomo-bench/attribution.go` 定义 US1 数据结构 `AttributionTrace` / `RetrievedHit` / `QuadrantDistribution`(字段逐一照 data-model.md;`per_signal_ranks` 本阶段省略——引擎未暴露,见 research D2)

**Checkpoint**:结构与复用面就绪 → US1 可安全接线 retrieval-only 归因。

---

## Phase 3: User Story 1 — 逐题检索归因 trace (Priority: P1) 🏆 MVP · FREE

**Goal**:retrieval-only 产出逐题四象限归因证据,零答题 token、引擎零改。
**Independent Test**:小 fixture 上 trace 正确报 `gold_rank`+`outranked_by`;全量跑产 `trace.jsonl`+分布表+embedding 探针;答题调用=0;引擎 git diff 空。

### TDD:先写失败测试

- [ ] T005 [P] [US1] 在 `cmd/locomo-bench/attribution_test.go` 写确定性 golden:构造含 gold-covering hit 排第 4、两个非 gold hit 在前的 fixture,断言 `gold_rank==4`、`outranked_by` 列前两名、`quadrant` 按注入 correct 值正确分类(先失败)
- [ ] T006 [P] [US1] 在 `attribution_test.go` 写四象限互斥/穷尽 + `gold_unresolved` 不入分母的表驱动测试(先失败)
- [ ] T007 [P] [US1] 在 `cmd/locomo-bench/embed_probe_test.go` 写 embedding 探针测试:stub 双次嵌入一致→`deterministic`、注入 δ→`bounded`/`unstable`(先失败)

### 实现

- [ ] T008 [US1] 在 `attribution.go` 实现归因判定算法(research D3):对有序 `hits` 计算 `gold_hit_indices`/`gold_in_pool`(用 widePool)/`gold_rank`/`outranked_by`(截 `--outrank-cap` 默认 5);复用 `chunkTurns`∩`goldTurns`
- [ ] T009 [US1] 在 `attribution.go` 实现四象限分类 + 与 `--join-results` 的 `(conv,q)→correct` join(research D4);`correct_source` 常量标注;缺 join 时降级 `retrieval_only`
- [ ] T010 [US1] 在 `cmd/locomo-bench/embed_probe.go` 实现 embedding 确定性探针(research D5):同 query 两次 `embedding.Client.Embed`,算 bit-identical 比例 + L2 δ + verdict
- [ ] T011 [US1] 在 `cmd/locomo-bench/main.go` 接线 `--attribution-trace` retrieval-only 入口(contracts/attribution-cli.md):解析新 flag、**断言不初始化答题/judge caller**、调 `SearchWithDiagnostics`、写 `trace.jsonl`+`quadrant-distribution.json`(+`--embed-probe` 时 `embed_probe.json`)
- [ ] T012 [US1] 让 T005–T007 全绿:`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Attribution|EmbedProbe'`

### 验收(对应 SC)

- [ ] T013 [US1] 跑全量归因(setsid 分离):`go run ./cmd/locomo-bench --attribution-trace --data <locomo> --run-dir .locomo-run/009-attribution --store-dir <008 store> --retrieval hybrid --top-k 30 --chunks --join-results .locomo-run/008-us4-e2e/results-hybrid.jsonl --embed-probe`
- [ ] T014 [US1] 验 SC:SC-003 log 中答题调用=0;SC-004 重跑一次 `diff` 两份 trace.jsonl 逐字段一致;SC-005 `git diff --name-only -- memory embedding provider store internal` 空;SC-001/002 分布表覆盖率达标
- [ ] T015 [US1] 把 Q3(US2 靶心)象限的竞争事实主导模式写入 `.locomo-run/009-attribution/q3-summary.md`(供 US2 选机制)

**Checkpoint**:US1 独立交付——检索排序错题被精确切成四象限、Q3 靶心可归因。**单独 commit(adapter,引擎零改)**。

---

## Phase 4: User Story 2 — 证据门控的定向排序改动 (Priority: P2) · GATED(前序证据 + 三道门 + 授权)

**Goal**:据 US1 证据选一个纯 Go tuning-free 排序机制(默认关),过三道门才允许出货默认。
**Independent Test**:关时 parity 逐字节;开时 Q3 象限 gold 排名上升;端到端配对 McNemar above-noise 且非目标类不回退。

- [ ] T016 [US2] ⚠️ **证据门 + 机制选择**:读 T015 的 `q3-summary.md`,从三候选(score-aware RRF / MMR 去重 / 实体·时间锚)选**一个**,记录选择理由到 `specs/009-*/eval-log.md`(research D6);若 Q3 象限过小(证据不支持)则停并上报,不硬上
- [ ] T017 [P] [US2] TDD:在 `memory/retriever_test.go` 写 parity golden——`RankingRefine=false` 时检索结果与现基线逐字节一致(先失败/占位,实现后必须绿)
- [ ] T018 [P] [US2] TDD:写打开 `RankingRefine` 的新排序单测(依选定机制断言预期重排,先失败)
- [ ] T019 [US2] 在 `memory/retriever.go` 的 `RetrieverOptions` 加 `RankingRefine bool`(零值=旧行为)+ `SearchDiagnostics.PerSignalRanks`(additive,默认 nil);实现选定机制,纯 Go/offline(contracts/ranking-option.md)
- [ ] T020 [US2] **门①纯 Go 契约**:`CGO_ENABLED=0 go build ./... && go test -count=1 ./memory` 全绿(parity 关时零变 + 新单测过)
- [ ] T021 [US2] **门②离线归因**:开 `RankingRefine` 复跑 US1 归因(T013 同配置),断言 Q3 象限 gold 平均排名上升;确认无云/付费杠杆
- [ ] T022 [US2] ⚠️ **授权门**:确认门①②过 **且** 维护者显式授权端到端花费 + 机器窗口可用;未授权则停,不产生答题成本
- [ ] T023 [US2] **门③端到端决胜**(setsid 分离,机器窗口):同机配对 `hybrid` vs `hybrid+RankingRefine`,先目标类后全量 1540,算配对 McNemar;唯一变量=排序机制
- [ ] T024 [US2] 判定:McNemar above-noise + overall 及任一非目标类不显著回退 + 超越反证基线(−0.3pp/p=1.0、−0.06pp/p=1.0)→ **GO**;否则 **NO-GO 出货,保留默认关诊断**;coverage 不作 verdict

**Checkpoint**:US2 有端到端定论。**单独 commit(engine,与 US1 分离)**。

---

## Phase 5: Polish & 跨切面

- [ ] T025 [P] 把 US1 四象限结论 + US2 GO/NO-GO verdict 写入 `specs/009-*/eval-log.md` 与 `docs/locomo-score-levers.md`(tracked 正本,跨环境不失传)
- [ ] T026 [P] 更新 `docs/README.md` 索引行(009 归因门控排序)
- [ ] T027 若 US2 GO:在 `docs/` 记录默认关 opt-in 的启用方式与限定;若 NO-GO:记入坟场(与 008 US1 reranker 同格式)

---

## Dependencies & 执行顺序

- Setup(T001-T002)→ Foundational(T003-T004)→ **US1(T005-T015,FREE,MVP)** → US2(T016-T024,GATED)→ Polish(T025-T027)。
- US2 **强依赖 US1 证据**(T016 读 T015);US1 无 US2 依赖,可独立交付。
- 门顺序硬:T020(①)→ T021(②)→ T022(授权)→ T023-T024(③),不可跳。

## 并行机会

- T005/T006/T007 [P] 三个测试文件并行写。
- T017/T018 [P] 两个引擎测试并行写。
- T025/T026 [P] 文档并行。

## MVP 范围

**MVP = US1**(逐题检索归因 trace)。它单条即可把"检索排序是最大错因"从聚合断言变成逐题四象限证据,**零答题成本、引擎零改、当前机器窗口内即可跑**,并产出 US2 选机制所需的 Q3 靶心。US2 是证据门控的拿分动作,gated。

## 提交切分(宪法 IV,硬)

1. **US1 adapter**(T001-T015):`feat(009): US1 检索归因 trace(free/adapter/引擎零改)`
2. **US2 engine**(T016-T024):`feat(009): US2 定向排序(engine/默认关/三道门)` —— 与 US1 分离
3. **docs/eval 落账**(T025-T027):单独 commit,verdict 入 tracked docs
