# Tasks: 写入侧 Doc2Query 伪查询影子向量

**Feature**: `012-doc2query-shadow` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

> 傻瓜式执行:确切签名/CLI/判定门在 [contracts/](./contracts/) 已冻结,照抄。产物落 gitignored `.locomo-run/012-*/`。凭据只走 env/隧道绝不落文件。WSL2 长跑 setsid 分离。**全局硬门**:US2 期间引擎 `git diff --name-only -- memory embedding provider store internal` 必空;死规则——禁云/付费 reranker;**最终 top-k=30 不涨 + answer-context 不涨**(提质硬约束,涨即加量判负)。TDD:先写失败测试再实现。US1(engine)、US2(adapter)、US3(shipped,deferred)**分属不同 commit**。

---

## Phase 1: Setup

- [x] T001 确认前置:009 固化 bge-large chunks store 路径可用(HF `009-bge-chunks-store` 或本地缓存)、LoCoMo dataset 可读;记录到 `.locomo-run/012-doc2query/preflight.txt`(不含凭据)。核对 `.gitignore` 已忽略 `.locomo-run/`。

## Phase 2: Foundational（复用面核对）

- [x] T002 [P] 复用性核对:确认 `memory/retriever.go:821 vectorRankContext` 的 max-pool 归并**内容无关**(经 `resolveShadow` 折叠任意影子)、`memory/embedder.go` 的 `embedOne`/`resolveShadow`/`Backfill`/`Enqueue`、`memory/entrystore.go:476 PutAliases`(仿写模板)、`store/migrations.go migrationsByVersion`(现 v4)可直接扩展,不改其它语义。

**Checkpoint**:确认后 US1 可安全接线,零 011 语义改动、零新可调权重。

---

## Phase 3: User Story 1 — 引擎 `#query` 伪查询影子（P1 · FREE · ENGINE · inert-by-default）

**Goal**:有伪查询的 fact 产 `#query` 影子向量;检索时 `resolveShadow` 泛化让既有 max-pool 归并自动折叠 `#query`,影子名不泄漏;无 `memory_fact_queries` 行逐字节不变。
**Independent Test**:纯离线单测——parity + 归并升排名 + 去重一票 + 影子不泄漏 + embedOne `#query` 分支 + 退化 + 与 `#alias` 共存;`CGO_ENABLED=0` 全绿;引擎外零改。
**契约**:[contracts/query-shadow-engine.md](./contracts/query-shadow-engine.md)。

### Migration（先 store 层）

- [x] T003 [US1] 在 `store/migrations_test.go` 写 `TestMigrationV5FactQueries`:全新库迁移后 `memory_fact_queries` 存在、`schema_version` MAX=5、v1-v4 未改;Down 可回滚(先失败)。
- [x] T004 [US1] 运行 `CGO_ENABLED=0 go test ./store -run TestMigrationV5 -v` 验证失败(表不存在)。
- [x] T005 [US1] 在 `store/migrations.go` 加 `v5FactQueries`/`v5FactQueriesDown`(契约 S1 逐字),`migrationsByVersion` 末尾追加 `{Version:5,...}`;运行 T004 命令转绿。
- [x] T006 [US1] 提交点内联:`CGO_ENABLED=0 go build ./... && go test ./store -count=1` 全绿。

### EntryStore 访问器

- [x] T007 [P] [US1] 在 `memory/entrystore_test.go` 写 `TestPutFactQueries`:Put→FactQueries 往返、去重(大小写/空白归一)、覆盖(重复 Put 替换)、`FactQueryEntryNames` 只列有 query 的 fact(先失败)。
- [x] T008 [US1] 在 `memory/entrystore.go` 加 `PutFactQueries`/`FactQueries`/`FactQueryEntryNames`(契约 S2,逐字仿 `PutAliases` 的事务+去重);运行 `CGO_ENABLED=0 go test ./memory -run 'TestPutFactQueries' -v` 转绿。

### TDD:引擎影子（先写失败测试)

- [x] T009 [P] [US1] 在 `memory/query_shadow_test.go`(新建,package memory_test)写 `TestQueryShadow_EmbedOneBranch`:`Enqueue(QueryShadowName(fact))`→embedOne 查 `FactQueries`、原样 join(不丢词)、`Put` 影子向量;空 queries→no-op;非影子 name 逐字节不变(先失败)。
- [x] T010 [P] [US1] 写 `TestQueryShadow_NoQueriesParity`:无 `memory_fact_queries` 行的 fact + 所有 chunk semantic 相似度与最终排序逐字节等于现状(`!hasShadows` 快路径)(先失败/实现后仍绿)。
- [x] T011 [P] [US1] 写 `TestQueryShadow_MergeLiftsSource`:构造 gold fact 的 text 向量对 query 弱命中、其 `#query` 影子强命中,断言 max-pool 归并后源 fact semantic 排名升入结果(先失败)。
- [x] T012 [P] [US1] 写 `TestQueryShadow_DedupSingleVote` + `TestQueryShadow_ShadowNameNeverLeaks`:同源 text+`#query` 双命中→结果一条、semantic 一票;任何 `#query` 影子命中→结果只含源 fact name(先失败)。
- [x] T013 [P] [US1] 写 `TestQueryShadow_Degenerate` + `TestQueryShadow_CoexistWithAlias`:client nil / 孤儿影子 / 空 queries→不 panic、降级;一 fact 同时有 alias+query 两影子→都折回源、max-pool 取最优、结果一票(先失败)。

### 实现（memory/,engine 增量)

- [x] T014 [US1] 在 `memory/embedder.go` 加 `queryShadowSuffix`/`QueryShadowName`/`resolveQueryShadow`(契约 S3);**泛化 `resolveShadow`** 为「识别任一影子后缀」(先 query 再 alias);加 `queryEmbedText(queries)`=原样 join(无丢词滤器)。
- [x] T015 [US1] 在 `embedOne` 于 `#alias` 分支**之前**插入 `#query` 分支(契约 S3):影子 name→strip 源→`FactQueries`→`queryEmbedText`→`Embed`→`vectors.Put`;非影子逐字节不变。
- [x] T016 [US1] 加 `QueryShadowNames(ctx)`(`FactQueryEntryNames`→`#query`);`Backfill` 纳入缺失 `#query` 影子(与 `AliasShadowNames` 并列)。
- [x] T017 [US1] 确认 `memory/retriever.go` **源码零改**即通过 T011/T012(泛化后的 `resolveShadow` 自动折叠 `#query`);若 T011/T012 未过,只允许在 retriever 内改经 `resolveShadow` 的判定,不引入 α/新权重。
- [x] T018 [US1] 让 T009–T013 全绿:`CGO_ENABLED=0 go test -count=1 ./memory -run 'TestQueryShadow'`。

### 验收（对应 SC)

- [x] T019 [US1] 全引擎回归(退化保真不破 parity golden):`CGO_ENABLED=0 go test -count=1 ./store ./memory ./memory/pipeline` + `CGO_ENABLED=0 go build ./...` 全绿(SC-003)。**M2 核查**:确认 `#query` 影子只被 semantic 归并消费——`Backfill`/export/snapshot/curation 遍历向量遇 `#query` 须 resolve/跳过,不当真 entry。
- [x] T020 [US1] 验 SC-001(无 query 行 + chunk semantic 逐字节 parity)/ SC-002(归并升排名+去重+影子不泄漏,确定性可复算)/ SC-003(CGO 关无云 reranker、检索无 query-时 LLM、无 α、migration v5 独立 tx、v1-v4 未改);`git diff --name-only` 仅 `store/`+`memory/`。

**Checkpoint**:US1 独立交付——纯 Go `#query` 影子 + inert-by-default。**单独 commit(engine)**,消息注明退化保真 + max-pool 无 α + retriever 源码零改复用 011。

---

## Phase 4: User Story 2 — adapter backfill + 三道门 gated（P2 · ADAPTER · GATED）

**Goal**:`cmd/locomo-bench` 对 009 店 LLM backfill 补 `#query` 影子(不重抽取),过三道门 + 证 context 不涨才判 GO。
**Independent Test**:backfill/分层诊断单测(mock)绿;门② 目标类 gold 子层 gold 升进 top-30(诊断);门③ 配对 McNemar 目标类 above-noise + 非目标类不回退 + `answer_context_tokens` 不涨。**引擎零改。**
**契约**:[contracts/doc2query-adapter.md](./contracts/doc2query-adapter.md)。

### TDD + 接线（cmd/locomo-bench,引擎零改)

- [ ] T021 [US2] 在 `cmd/locomo-bench/doc2query_test.go`(新建)写单测(契约 A6,mock caller/embedder):canonical `#query` 向量行全程=0;baseline 剥离=0、treatment>0;backfill 不触发抽取(extraction call 计数=0);校验拒绝 `--doc2query treatment`+`--top-k 40`/`--multi-query`;分层桶 `gold_hit` 正确(先失败)。
- [ ] T022 [US2] 在 `cmd/locomo-bench` 加 `--doc2query off|baseline|treatment` flag + 校验(契约 A1);方案 A 两店隔离照搬 011 `--alias-shadow`(复制 009 店到 `<run-dir>/doc2query-store`,canonical 不打开为运行店)。
- [ ] T023 [US2] 实现 backfill(契约 A2/A5):遍历运行店 `FactSource=="extraction"` 且无 query 行的 fact→LLM 生成 2-3 问句(冻结提示词,温度固定,失败跳过)→`PutFactQueries`→treatment `Enqueue(QueryShadowName)`;baseline 剥离 `#query` 向量;产物 `doc2query_backfill_<arm>.jsonl`。
- [ ] T024 [US2] 分层召回诊断接线(契约 A3):复用 011 `--recall-diagnostic` 骨架,分层键 `gold_hit`,写 `doc2query_recall_<arm>.jsonl`+配对 `doc2query_recall.json`。
- [ ] T025 [US2] 让 T021 全绿:`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestDoc2Query'`;**验引擎零改**:`git diff --name-only -- memory embedding provider store internal` 空。

### 门②:分层召回诊断（near-free,retrieval-only,box bge-large 8001)

- [x] T026 [US2] 门② multi-hop(cat1):完成。gold_has_query 子层 n=207,entered/left=0/0,rank@30 Δ=+0.227(变差),coverage@30 Δ=0.00000。产物 `.locomo-run/012-recall-cat1/`。
- [x] T027 [US2] 门② open-domain(cat3):完成。gold_has_query 子层 n=51,entered/left=2/0,rank@30 Δ=−0.627(微前移),coverage@30 Δ=0.00000。产物 `.locomo-run/012-recall-cat3/`。
- [x] T028 [US2] **止损决策点 = NO-GO**:两目标类 coverage@30 Δ 恒 0(cat3 有 2 gold 进 top-30 但无新增 turn 覆盖 = 端到端无预期增益),严格触发止损门 → **跳过 T029-T031**,直接 T032 收口。box 已停(GPU 0 MiB)、隧道拆、凭据清。

### 门③:端到端配对 McNemar（box,repeats=3,唯一变量=`#query` 影子)——仅门② GO 才跑

- [~] T029 [US2] **SKIPPED（门② NO-GO)**——未启动,省 1540×3 答题 + judge 窗口。
- [~] T030 [US2] **SKIPPED（门② NO-GO)**。
- [x] T031 [US2] box 收尾:vllm 已杀、`nvidia-smi` GPU 0 MiB、隧道拆、askpass 清(凭据仅存活于 env,从未落盘)。

### 收口（对应 SC-005/006/007)

- [x] T032 [US2] 结论(门① PASS / 门② NO-GO + 子层/全局 delta + coverage 诊断 + 三向证伪收敛)已写入 `docs/locomo-score-levers.md` 台账 Feature 012;`--doc2query` 保留默认关(与 008/010/011 同族)。**单独 commit(adapter)`294ca0d`**,`git diff -- memory…` 空。

**Checkpoint**:US2 独立交付完成——门① PASS、门② NO-GO(止损)、门③ 未启动。诚实 NO-GO。

---

## Phase 5: User Story 3 — shipped 写入路径（P3 · **CANCELLED**·门② NO-GO)

> **前置门未过**:US3 仅当 US2 门③ GO 才启动。门② NO-GO(2026-07-24)→ **本 Phase 取消实现**。`#query` 引擎能力作纯 Go/退化保真/可移植能力保留(默认惰性,无 `memory_fact_queries` 行的店逐字节零改)。

- [~] T033 [US3] **CANCELLED（门② NO-GO)**——抽取 prompt 不接 `"queries"` 字段。
- [~] T034 [US3] **CANCELLED（门② NO-GO)**——pipeline 不接 shipped 写入路径。
- [~] T035 [US3] **CANCELLED（门② NO-GO)**——无新 baseline 声明。
