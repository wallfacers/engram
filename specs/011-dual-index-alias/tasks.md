# Tasks: 写入侧表示 —— dual-index alias 向量

**Feature**: `011-dual-index-alias` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

> 傻瓜式执行:确切签名/CLI/判定门在 [contracts/](./contracts/) 已冻结,照抄。产物落 gitignored `.locomo-run/011-*/`。凭据只走 env/隧道绝不落文件。WSL2 长跑 setsid 分离。**全局硬门**:US2 期间引擎 `git diff --name-only -- memory embedding provider store internal` 必空;死规则——禁云/付费 reranker;**最终 top-k=30 不涨 + answer-context 不涨**(提质硬约束,涨即加量判负)。TDD:先写失败测试再实现。US1(engine)与 US2(adapter)**分属不同 commit**。

---

## Phase 1: Setup(共享前置)

- [x] T001 确认前置:009 固化 bge-large chunks store 路径可用(HF `009-bge-chunks-store` 或本地缓存 `.locomo-run/010-artifacts/009-bge-chunks-store`)、LoCoMo dataset 可读;记录到 `.locomo-run/011-dual-index/preflight.txt`(不含凭据)
- [x] T002 核对 `.gitignore` 已忽略 `.locomo-run/`(应已有,仅核对)

---

## Phase 2: Foundational(阻塞 US1 的复用面核对)

- [x] T003 [P] 复用性核对:确认 `memory/embedder.go` 的 `embedOne`/`embedText`/`Enqueue`/`Backfill`、`memory/retriever.go` 的 `vectorRankContext`(`retriever.go:821`)、`VectorStore.LoadAllForModel`/`Put`、`embedding.TopKCosine`/`Cosine`、`ranksFromOrder`/`fuseRRF`、`memory_event_aliases` 读接口、`pipeline.storeFact`/`PutAliases` 可直接调用/扩展,不改其它语义(research D2/D3/D4)

**Checkpoint**:复用面确认 → US1 可安全接线 dual-index,零 schema 变更、零新可调权重。

---

## Phase 3: User Story 1 — 引擎 dual-index alias 向量 (Priority: P1) 🏆 MVP · FREE · ENGINE

**Goal**:有 alias 的 fact 产 `#alias` 影子向量;检索时 `vectorRankContext` 截断前 max-pool 归并回源 fact,影子 name 不泄漏;无 alias/chunk 与源 text 向量逐字节不变。
**Independent Test**:纯离线单测——parity + 归并升排名 + 去重一票 + 影子不泄漏 + embedOne 影子分支 + 退化;`CGO_ENABLED=0` 全绿;引擎外零改。

### TDD:先写失败测试(contracts/shadow-embedding-engine.md §测试契约)

- [x] T004 [P] [US1] 在 `memory/embedder_test.go` 写 `TestAliasShadow_EmbedOneShadowBranch`:影子 name(`<fact>#alias`)→ embedOne 查 aliases、合并去 Content 已含词、Put 影子向量;空文本 no-op;非影子 name 逐字节不变(先失败)
- [x] T005 [P] [US1] 在 `memory/retriever_test.go` 写 `TestAliasShadow_NoAliasParity`:无 alias fact + 所有 chunk 的 semantic 相似度与最终排序逐字节等于现状(先失败/占位——归并实现后仍须绿)
- [x] T006 [P] [US1] 在 `retriever_test.go` 写 `TestAliasShadow_MergeLiftsSource`:构造 gold fact 的 text 向量对 query 弱命中、其影子向量强命中,断言 max-pool 归并后源 fact semantic 排名上升进入结果(先失败)
- [x] T007 [P] [US1] 在 `retriever_test.go` 写 `TestAliasShadow_DedupSingleVote` + `TestAliasShadow_ShadowNameNeverLeaks`:同源 text+影子双命中→结果一条、semantic 一票;任何影子命中→结果只含源 fact name(先失败)
- [x] T008 [P] [US1] 在 `retriever_test.go`/`embedder_test.go` 写 `TestAliasShadow_Degenerate`:client nil / 孤儿影子(源 fact 缺失)/ 空 alias → 不 panic、per-signal 降级(先失败)

### 实现(memory/,engine 增量)

- [x] T009 [US1] 在 `memory/embedder.go` 加 `aliasShadowName(factName)`/`resolveShadow(name)` helper + `embedOne` 影子分支(research D1/D3):影子 name → strip 源 fact → 查 `memory_event_aliases` → 合并去冗嵌入文本 → `vectors.Put(影子 name)`;非影子逐字节不变
- [x] T010 [US1] 在 `memory/embedder.go` 加影子枚举 `AliasShadowNames(ctx)`(`memory_event_aliases` distinct entry_name → `#alias`)供 `Backfill`/US2 re-embed;`Backfill` 纳入缺失影子(research D4)
- [x] T011 [US1] 在 `memory/pipeline/pipeline.go` `storeFact`:`PutAliases` 之后,对有非空 aliases 的 fact `Enqueue(aliasShadowName(entry.Name))`(research D4)
- [x] T012 [US1] 在 `memory/retriever.go` `vectorRankContext` 实现截断前 max-pool 归并(research D2):对 `LoadAllForModel` 全 candidates 算 cosine → 按 `resolveShadow` 折叠 `#alias` 到源 fact 取 max → 去重 → 排序取 topK → `ranksFromOrder`;影子 name 不进 ranks/结果
- [x] T013 [US1] 让 T004–T008 全绿:`CGO_ENABLED=0 go test -count=1 ./memory ./memory/pipeline -run 'TestAliasShadow'`

### 验收(对应 SC)

- [x] T014 [US1] 全引擎回归(退化保真不破坏既有 parity golden):`CGO_ENABLED=0 go test -count=1 ./memory ./memory/pipeline` + `CGO_ENABLED=0 go build ./...` 全绿(SC-003);**M2 核查**:确认影子只被 semantic 归并消费——`Backfill.NamesMissingModel`/export/snapshot/curation 不把影子当真 entry(遍历向量后 `GetByName` 的路径遇影子须 resolve/跳过)
- [x] T015 [US1] 验 SC-001(无 alias/chunk + 源 text 向量逐字节 parity)/ SC-002(归并升排名+去重+影子不泄漏,确定性可复算)/ SC-003(CGO 关无云 reranker、检索路径无 query-时 LLM、无 α);确认 `store/migrations.go` 未改、`schema_version` 不变(FR-006 无 schema 变更,L1);`git diff --name-only` 仅 `memory/`

**Checkpoint**:US1 独立交付——dual-index 纯 Go 归并机制就绪,离线默认零改。**单独 commit(engine)**,消息注明退化保真 + max-pool 无 α + 复用 RRF k=60。

---

## Phase 4: User Story 2 — adapter re-embed + 三道门 gated (Priority: P2) · ADAPTER · GATED

**Goal**:`cmd/locomo-bench` 对 009 店 re-embed 补影子(不重抽取),过三道门 + 证 context 不涨才判 GO。
**Independent Test**:re-embed/分层诊断单测(mock)绿;门② 目标类「gold 有 alias」子层 gold 升进 top-30(诊断);门③ 配对 McNemar 目标类 above-noise + 非目标类不回退 + `answer_context_tokens` 不涨。**引擎零改。**

### TDD + 接线(cmd/locomo-bench,引擎零改)

- [x] T016 [P] [US2] 在 `cmd/locomo-bench/*_test.go` 写 re-embed 编排单测(mock):枚举有 alias fact→enqueue 影子→不调抽取 caller(calls=0);分层诊断按 `gold_has_alias` 分桶正确(先失败)
- [x] T017 [US2] 在 `cmd/locomo-bench` 实现方案 A re-embed 编排:baseline/treatment 都复制 canonical `conv*.db`(+存在的 WAL/SHM)到 `<run-dir>/alias-store` 后 Backfill;baseline 剥离并断言 0,treatment 断言 `>0`;canonical 不作为运行店打开/写入
- [x] T018 [US2] 在 `cmd/locomo-bench/main.go` 接线 `--alias-shadow off|baseline|treatment` enum(默认 off;baseline/treatment 均指向副本;与 `--multi-query` 互斥;要求 `--top-k 30`/`--store-dir`):逐题记 `answer_context_tokens`/`final_top_k` 写 `context_parity.jsonl`,alias 两臂断言 final top-k 恒等 30

### 门① 纯 Go 契约(FREE)

- [x] T019 [US2] 门① = US1 的 T004–T008 + re-embed/分层 T016 全绿;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestAliasShadow|TestReembed'` + `git diff --name-only -- memory embedding provider store internal` 空(SC-007)

### 门② 分层召回诊断(NEAR-FREE,retrieval-only,仅诊断不作 verdict)

- [x] T020 [US2] 扩展 `--recall-diagnostic` 支持 alias-shadow 两臂(retrieval-only,**断言不初始化 decomposition/答题/judge caller**):同 query single 检索,按 `gold_has_alias` 分层输出 gold rank/coverage@30 delta 到 `.locomo-run/011-recall/`;rank delta 为 treatment-baseline(负=改善),coverage delta 为 treatment-baseline(正=改善)
- [x] T021 [US2] 跑门②(setsid 分离,quickstart 命令,目标类 open-domain=cat3 + multi-hop=cat1),确认**「gold 有 alias」子层** gold 从 rank>30 升进 top-30 / coverage@30 ↑;**记为诊断,不单独作 GO 依据**(SC-005/FR-011);子层不升即 NO-GO 止损、不跑门③

### 门③ 端到端决胜(BOX 窗口,repeats=3,配对 McNemar)

- [~] T022 [US2] ⚠️ **提质前置校验**:确认两臂 `final_top_k=30` 恒等、canonical recipe 一致、唯一变量=alias 影子向量;box vllm Qwen 栈(隧道)+ box bge-large 8001 嵌入 + deepseek mem0-aligned judge env 就绪(不落凭据)
- [~] T023 [US2] 跑 baseline 臂(setsid,repeats=3,`--alias-shadow baseline`):`.locomo-run/011-e2e-base`(quickstart 命令)
- [~] T024 [US2] 跑 treatment 臂(setsid,repeats=3,同批同栈,`--alias-shadow treatment`):`.locomo-run/011-e2e-shadow`
- [~] T025 [US2] 配对 McNemar + context-parity 判定:①目标类 above-noise ↑ ②overall 及任一非目标类不显著回退 ③treatment `answer_context_tokens` 不显著 > baseline ④`final_top_k` 两臂恒等=30(SC-004/006)。四者全满足 → GO;否则 NO-GO

### 收口(对应 SC-006 判定诚实)

- [x] T026 [US2] 把结论(GO/NO-GO + 子层/全局 delta + p 值 + context 对比 + coverage 诊断)写入 `docs/locomo-score-levers.md` 杠杆台账 Feature 011;对比反证基线(008 reranker −0.06/p=1.0、009 cluster-sweep +0.4、010 分解 NO-GO)——不加 context 拿到目标类转化才算提质赢
- [x] T027 [US2] NO-GO 情形:保留 `--alias-shadow` 为默认关能力,如实记录(与 008 reranker / 010 分解同样处理);GO 情形:记录为提质赢候选 + 复现 recipe。**adapter 改动单独 commit(与 US1 engine 分离,FR-014)**

**Checkpoint**:US2 判定完成——GO 则提质赢入账,NO-GO 则诚实保留能力;引擎全程零改,提交分离。

---

## 依赖与并行

- **Phase 顺序**:Setup(T001-2)→ Foundational(T003)→ US1(T004-15)→ US2(T016-27)。
- **US1 内**:T004–T008 [P] 可并行写(不同测试函数);实现 T009→T010(embedder)、T011(pipeline)、T012(retriever)可分工但 T012 归并依赖 T009 影子写入语义;T013-15 验收在最后。
- **US2 内**:T016-18(re-embed+接线)→ 门①T019 → 门②T020-21 → 门③T022-25 → 收口T26-27,门间顺序硬依赖(证据链)。
- **提交边界**:US1 完成即单独 commit(engine);US2 完成单独 commit(adapter)。**绝不混提**(FR-014 归因)。

## 独立可交付性

- **US1 单独可交付**:dual-index 纯 Go 归并机制,单测证明退化保真 + 归并正确,不依赖 US2 即有价值(引擎新增可移植能力)。
- **US2 依赖 US1** 的引擎能力 + box 窗口;越不过门③仍保留能力。

## 全局验收(收尾核对)

- [ ] SC-001 无 alias/chunk+源 text 向量逐字节 parity · SC-002 归并 golden 确定性 · SC-003 CGO 关纯 Go 无云 reranker 无 α
- [ ] SC-004 两臂 top-k=30 恒等 + context 不涨 · SC-005 分层召回诊断(不作 verdict)
- [ ] SC-006 GO/NO-GO 由配对 McNemar + context-parity 决定,超越反证基线 · SC-007 引擎零改 + 提交分离
