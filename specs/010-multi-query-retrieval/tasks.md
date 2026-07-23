# Tasks: 多查询检索 —— 提质型深召回(multi-query retrieval)

**Feature**: `010-multi-query-retrieval` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)

> 傻瓜式执行:确切签名/CLI/schema/判定门在 [contracts/](./contracts/) 已冻结,照抄。产物落 gitignored `.locomo-run/010-*/`。凭据只走 env/隧道绝不落文件。WSL2 长跑 setsid 分离。**全局硬门**:US2 期间引擎 `git diff --name-only -- memory embedding provider store internal` 必空;死规则——禁云/付费 reranker;**最终 top-k=30 不涨 + answer-context 不涨**(提质硬约束,涨即加量判负)。TDD:先写失败测试再实现。US1(engine)与 US2(adapter)**分属不同 commit**。

> **实施进度(2026-07-23,3 个 Codex 并行 + 本会话评审/集成收口)**
> - ✅ **US1 引擎 `SearchMulti`**(commit `d5d315e`)——merged master。深度 `D=k*candidateMultiplier`、len==1 退化保真、复用 rrfK=60;parity/融合/退化 4 测试真断言(非空转)。
> - ✅ **US2 分解 `decomposeQuery`**(commit `5daa31e`)——merged。复用 `runner.go` 已有 `modelCaller`,4 种退化 + 正常路径全测。
> - ✅ **US2 接线 `--multi-query`/`--recall-diagnostic`/context_parity**(commit `53f9b27`)——merged。top-k=30 硬拦、multi 臂候选深度 `questionSearchK` 逐字复刻单臂 `widePool=max(topK*6,300)`(决胜门唯一变量=分解,不被深度污染)。
> - ✅ **门① 纯 Go 契约 PASS**:集成态 `go build/test/vet ./...` 全绿;engine/adapter 提交边界分离。
> - ⏸ **门② 离线召回诊断 / 门③ 端到端配对 McNemar**:**待 box**(vllm 8000 答题 + 8001 bge-large 嵌入;凭据每次重启轮换)。门②先跑(near-free),gold 不升 top-30 即诚实 NO-GO 省门③钱。

---

## Phase 1: Setup(共享前置)

- [ ] T001 确认前置:009 固化 bge-large chunks store 路径可用(HF `009-bge-chunks-store` 或本地缓存)、LoCoMo dataset 可读;记录到 `.locomo-run/010-multiquery/preflight.txt`(不含凭据)
- [ ] T002 核对 `.gitignore` 已忽略 `.locomo-run/`(应已有,仅核对)

---

## Phase 2: Foundational(阻塞 US1 的复用面核对)

- [ ] T003 [P] 复用性核对:确认 `memory/retriever.go` 的 `SearchWithDiagnostics`(每子查询精检核心)、`fuseRRF`(二级融合复用)、`ranksFromOrder`(有序名单→ranks map)、`Result`/`candidateMultiplier=10`/`minCandidatePool=100` 可直接调用,不改其语义(research D2/D3)

**Checkpoint**:复用面确认 → US1 可安全接线 `SearchMulti`,零新可调权重、零 schema 变更。

---

## Phase 3: User Story 1 — 引擎多查询融合机制 `SearchMulti` (Priority: P1) 🏆 MVP · FREE · ENGINE

**Goal**:纯 Go/offline 的 `SearchMulti`,`len==1` 逐字节退化保真,`len>1` 走 RRF-of-RRF 融合返回正常 top-k;引擎只收 `[]string`,永不碰 query-时 LLM。
**Independent Test**:纯离线单测——parity(`SearchMulti(ctx,[]string{q},k)` 逐字节等于 `Search`)+ 融合正确性 golden(gold 单查询 rank>k → 融合进 top-k、共同命中优先)+ 退化(空/空串/单子查询空检索不 panic);`CGO_ENABLED=0` 全绿;引擎外零改。

### TDD:先写失败测试(contracts/searchmulti-engine.md §测试契约)

- [ ] T004 [P] [US1] 在 `memory/retriever_test.go` 写 `TestSearchMulti_SingleQueryParity`:随机若干 query,断言 `SearchMulti(ctx,[]string{q},k)` 与 `Search(ctx,q,k)` 结果集逐字节相等(顺序 + 每 `Result` 字段含 `Score`)(先失败——方法未实现)
- [ ] T005 [P] [US1] 在 `retriever_test.go` 写 `TestSearchMulti_FusionLiftsCoHit`:小 fixture 构造 gold doc 在单查询 `q` 下 rank>k、但被两个子查询分别较高命中,断言 `SearchMulti(ctx,subs,k)` 后 gold 进 top-k(先失败)
- [ ] T006 [P] [US1] 在 `retriever_test.go` 写 `TestSearchMulti_CoHitOutranksSingleHit`:doc A 被 2 子查询命中、doc B 仅 1 子查询命中且各自 rank 相近,断言 A 排 B 前(先失败)
- [ ] T007 [P] [US1] 在 `retriever_test.go` 写 `TestSearchMulti_Degenerate`:空数组→nil;含空串→跳过不 panic;某子查询空检索→其余正常融合(先失败)

### 实现(memory/retriever.go)

- [ ] T008 [US1] 实现 `SearchMulti(ctx, subqueries []string, k int) ([]Result, error)`:①规范化 `subqueries`(`TrimSpace` 每个、丢空串、精确去重);②`len==0`→`nil`;③`len==1`→**短路 `SearchWithDiagnostics(ctx, sub, k)` 返回其 `[]Result`**(逐字节 parity,research D1)
- [ ] T009 [US1] 实现 `len>1` 融合路径(research D2/D3):每子查询串行 **`SearchWithDiagnostics(ctx, sub_i, D)`,`D = k*candidateMultiplier`**(⚠️ 必须传 `D` 非 `k`,否则截断到 30、gold 在 31–D 名被丢)取有序 name 序列 `L_i` → `ranksFromOrder(L_i)` → `fuseRRF(ranks...)` → **截断最终 top-k** → 逐条 `GetByName` 装 `[]Result`;`Result.Score` = RRF-of-RRF 分(契约明示不与单查询可比)
- [ ] T010 [US1] 让 T004–T007 全绿:`CGO_ENABLED=0 go test -count=1 ./memory -run 'TestSearchMulti'`

### 验收(对应 SC)

- [ ] T011 [US1] 全引擎回归(退化保真不破坏既有 parity golden):`CGO_ENABLED=0 go test -count=1 ./memory` + `CGO_ENABLED=0 go build ./...` 全绿(SC-003)
- [ ] T012 [US1] 验 SC-001(parity 逐字节)/ SC-002(融合 golden 确定性可复算,重跑一致)/ SC-003(CGO 关无云 reranker 依赖、检索路径无 query-时 LLM);`git diff --name-only` 仅 `memory/`

**Checkpoint**:US1 独立交付——`SearchMulti` 纯 Go 融合机制就绪,离线默认零改。**单独 commit(engine)**,消息注明退化保真 + RRF-of-RRF 复用 k=60。

---

## Phase 4: User Story 2 — adapter 分解 + 三道门 gated (Priority: P2) · ADAPTER · GATED

**Goal**:`cmd/locomo-bench` 用答题 LLM 分解 ≤4 子查询喂 `SearchMulti`,过三道门 + 证明 context 不涨才判 GO。
**Independent Test**:分解退化单测(mock provider)绿;离线召回门 gold 升进 top-30(诊断);端到端配对 McNemar multi-hop above-noise + 非目标类不回退 + `answer_context_tokens` 不涨。**引擎零改。**

### TDD + 分解策略(cmd/locomo-bench,引擎零改)

- [ ] T013 [P] [US2] 在 `cmd/locomo-bench/decompose_test.go` 写分解退化单测(mock provider):LLM 失败/超时/返回 >4/全同质 → `[]string{question}`;正常 → ≤4 子查询含原 query 兜底(先失败)
- [ ] T014 [US2] 在 `cmd/locomo-bench/decompose.go` 实现 `DecompositionPolicy`(research D5 / data-model):复用答题 LLM provider,每题一次轻量重写产 ≤`--mq-max-subqueries` 子查询;退化路径确定;让 T013 绿
- [ ] T015 [US2] 在 `cmd/locomo-bench/main.go` 接线 `--multi-query` + `--mq-max-subqueries`(默认 4)(contracts/decompose-adapter.md):开时 `decompose → SearchMulti`;缺省 = 单查询基线逐字节不变;逐题记 `answer_context_tokens`/`final_top_k`/`subquery_count` 写 `context_parity.jsonl`

### 门① 纯 Go 契约(FREE)

- [ ] T016 [US2] 门① = US1 的 T004–T007 + 分解退化 T013 全绿;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestDecompose'` + `git diff --name-only -- memory embedding provider store internal` 空(SC-007)

### 门② 离线召回诊断(NEAR-FREE,retrieval-only,仅诊断不作 verdict)

- [ ] T017 [US2] 加 `--recall-diagnostic`(retrieval-only,**断言不初始化答题/judge caller**):在固化 store 上对目标 multi-hop 题跑 single vs multi,输出 gold rank delta + coverage@30 delta 到 `.locomo-run/010-recall/`
- [ ] T018 [US2] 跑门②(setsid 分离,quickstart 命令),确认目标题 gold 从 rank>30 升进 top-30 / coverage@30 ↑;**记为诊断,不单独作 GO 依据**(SC-005 / FR-011)

### 门③ 端到端决胜(BOX 窗口,repeats=3,配对 McNemar)

- [ ] T019 [US2] ⚠️ **提质前置校验**:确认两臂 `final_top_k=30` 恒等、canonical recipe 一致、唯一变量=分解;box vllm Qwen 栈(隧道)+ deepseek mem0-aligned judge env 就绪(不落凭据)
- [ ] T020 [US2] 跑 single 臂基线(setsid,repeats=3):`.locomo-run/010-e2e-single`(quickstart 命令)
- [ ] T021 [US2] 跑 multi 臂(setsid,repeats=3,同批同栈):`.locomo-run/010-e2e-multi --multi-query --mq-max-subqueries 4`
- [ ] T022 [US2] 配对 McNemar + context-parity 判定:①multi-hop above-noise ↑ ②overall 及任一非目标类不显著回退 ③`context_parity.jsonl` 里 `multi` 臂 `answer_context_tokens` 不显著 > `single`(SC-004/006)。三者全满足 → GO;否则 NO-GO

### 收口(对应 SC-006 判定诚实)

- [ ] T023 [US2] 把结论(GO/NO-GO + multi-hop delta + p 值 + context 对比 + coverage 诊断)写入 `docs/locomo-score-levers.md` 杠杆台账;须对比反证基线(008 reranker −0.06/p=1.0、009 cluster-sweep +0.4 噪声内、cat-top-k +0.9 加量带税)——不加 context 拿到可比/更好 multi-hop 转化才算提质赢
- [ ] T024 [US2] NO-GO 情形:保留 `--multi-query` 为默认关诊断能力,如实记录(与 008 reranker / 009 cluster-sweep 同样处理);GO 情形:记录为提质赢候选 + 复现 recipe。**adapter 改动单独 commit(与 US1 engine 分离,FR-014)**

**Checkpoint**:US2 判定完成——GO 则提质赢入账,NO-GO 则诚实保留诊断;引擎全程零改,提交分离。

---

## 依赖与并行

- **Phase 顺序**:Setup(T001-2)→ Foundational(T003)→ US1(T004-12)→ US2(T013-24)。
- **US1 内**:T004–T007 [P] 可并行写(同文件不同测试函数,注意合并);T008→T009→T010 顺序(实现依赖);T011-12 验收在最后。
- **US2 内**:T013-14(分解)→ T015(接线)→ 门①T16 → 门②T17-18 → 门③T19-22 → 收口T23-24,门间顺序硬依赖(证据链)。
- **提交边界**:US1 完成即单独 commit(engine);US2 完成单独 commit(adapter)。**绝不混提**(FR-014 归因)。

## 独立可交付性

- **US1 单独可交付**:`SearchMulti` 纯 Go 融合机制,单测证明退化保真 + 融合正确,不依赖 US2 即有价值(引擎新增可移植能力)。
- **US2 依赖 US1** 的 `SearchMulti` API + box 窗口;越不过门③仍保留诊断价值。

## 全局验收(收尾核对)

- [ ] SC-001 parity 逐字节 · SC-002 融合 golden 确定性 · SC-003 CGO 关纯 Go 无云 reranker
- [ ] SC-004 两臂 top-k=30 恒等 + context 不涨 · SC-005 召回诊断(不作 verdict)
- [ ] SC-006 GO/NO-GO 由配对 McNemar + context-parity 决定,超越反证基线 · SC-007 引擎零改 + 提交分离
