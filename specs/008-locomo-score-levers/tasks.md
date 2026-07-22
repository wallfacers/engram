# Tasks: LoCoMo 跑分杠杆探索 — 本地 reranker + open-domain 提示(008)

**Feature**: `008-locomo-score-levers` | **Spec**: [spec.md](./spec.md) | **Plan**: [plan.md](./plan.md)
**Contracts**: [rerank-sidecar](./contracts/rerank-sidecar.md) · [bench-commands](./contracts/bench-commands.md) | **Quickstart**: [quickstart.md](./quickstart.md)

> 傻瓜式执行:每个 US 的确切命令/端点/判定门在 contracts 里已冻结,照抄即可。产物落 gitignored `.locomo-run/008-*/`。凭据只走 env/隧道,绝不落文件。WSL2 长跑 setsid 分离。全局硬门:引擎 `git diff -- memory embedding provider store internal` 必空;死规则 reranker 本地 only。

---

## Phase 1: Setup(共享前置)

- [ ] T001 [P] 确认基线可构建:`CGO_ENABLED=0 go build ./...` 与 `go test ./cmd/locomo-bench/` 绿;记录当前 `git status` 干净
- [ ] T002 [P] 确认复用资产在位:固化 store `.locomo-run/007-us2/cov-store`(10 段 bge-small 384d)与 007 参考产物 `.locomo-run/007-us2/results-hybrid-mem0.jsonl` 存在
- [ ] T003 [P] 确认远端 97GB GPU 机 + vllm 答题(隧道 :8000)可达;备好 SSH 隧道能力(host/port/密码现场拿,勿落文件,见 docs/remote-eval-box.md)
- [ ] T004 建 gitignored 产物目录 `.locomo-run/008-{local-rerank,opendomain-old,opendomain-new,embed-small,embed-large}`;确认 `.locomo-run` 被 gitignore

---

## Phase 2: Foundational(阻塞检索类 US 的共享前置)

**目的**:死规则自检脚手架 + 引擎零改守卫,US1/US3 共用。

- [ ] T005 [P] 写死规则自检脚本(scratchpad,非仓库):对给定 `EMBED_BASE_URL` 跑 `curl /v1/models` 断言**无任何云 rerank 型号**(gte-rerank 等)+ `/rerank` 响应严格 `{"results":[{"index,relevance_score}]}` + `/embeddings` 维度正确;任一不符即红。见 contracts/rerank-sidecar.md §自检
- [ ] T006 [P] 写引擎零改守卫检查:`git diff --name-only -- memory embedding provider store internal` 必空,纳入每个 US 收尾

**Checkpoint**:自检脚手架就绪 → US1/US3 可安全接线本地 sidecar。

---

## Phase 3: User Story 1 — 本地 reranker 免费 coverage 闸 (Priority: P1) 🏆 MVP

**Goal**:本地 cross-encoder reranker 在默认预算 top-k30/quota12 下,`--coverage-only` 证明复现 003 已确认的 rerank 赢(overall turn@k ≥ +4pp)。
**Independent Test**:起本地 sidecar → coverage-only 跑 `hybrid` vs `hybrid+rerank` → 读 coverage.json turn@k delta,零答题/判题调用。

- [ ] T007 [US1] 写 reranker sidecar(scratchpad/远端,非仓库):fastembed `bge-small-en-v1.5`(384d)+ FlagEmbedding `FlagReranker('bge-reranker-v2-m3')`,双端点 `/v1/embeddings` + `/rerank`(裸+`/v1/rerank`),响应严格匹配 contracts/rerank-sidecar.md;只从本地模型文件加载,源码无任何云 URL
- [ ] T008 [US1] 部署 sidecar 到远端 97GB 机(与 vllm 共存),setsid 分离启动,建 SSH 隧道 → `EMBED_BASE_URL=http://127.0.0.1:<tunnel>/v1`;记录不含文本的 batch 延迟到 `.locomo-run/008-local-rerank/`
- [ ] T009 [US1] 跑死规则 + 契约自检(T005 脚本):`/v1/models` 无云型号、`/rerank` shape 正确、`/embeddings` 384d — 全绿才继续
- [ ] T010 [US1] 跑 US1 coverage 闸命令(bench-commands.md §US1):`--store-dir .locomo-run/007-us2/cov-store --coverage-only --chunks --top-k 30 --chunk-quota 12 --retrieval 'hybrid,hybrid+rerank' --run-dir .locomo-run/008-local-rerank`,`EMBED_RERANK_MODEL=bge-reranker-v2-m3`;确认零 answer/judge 调用
- [ ] T011 [US1] 计算判定门:读 `coverage.json`,`hybrid+rerank` − `hybrid` 的 overall turn@k(+ 4 类,多跳为关键)。**≥ +0.04 → PASS**;FAIL → 换本地模型(jina-reranker-v2 / mxbai-rerank)重试一次(回 T007),仍 FAIL 记坟场并转 US3
- [ ] T012 [US1] 引擎零改守卫(T006);确认全程无云 rerank 调用(SC-005)
- [ ] T013 [US1] 把 US1 结果 + verdict + 用的本地模型/体积/延迟写入 `specs/008-locomo-score-levers/eval-log.md`(过闸才进 US4)

**Checkpoint**:US1 独立交付——本地 reranker 是否复现可移植赢,有零成本定论。

---

## Phase 4: User Story 2 — open-domain 提示对齐(方案1) (Priority: P2)

**Goal**:只改 `openDomainAnswerPrompt` 为 5 步推理链,`--only-category 3` 单变量 A/B 捞起 open-domain(62.5%)且其余不回归。
**Independent Test**:旧/新代码各跑 cat3,同配置,比 open-domain% + McNemar + 无回归核查。
> **确定性(U1)**:答题 `provider.Request` 未设 Temperature → 默认 **temp=0**(runner.go:96),答题已确定;配对 McNemar 有效,残余仅 vllm 批处理级微噪声(可忽略,如显著可加 repeats)。**单变量铁律**:唯一变量 = `openDomainAnswerPrompt` 文本;`openDomainAnswerPrompt` 仅 cat-3 非-force 分支使用(runner.go:292 `answerPromptForRegime`),故 cat 1/2/4 由构造不受影响,免回归核查可直接用 007 参考产物。

- [ ] T014 [US2] 在 `cmd/locomo-bench/runner.go` **仅**替换 `openDomainAnswerPrompt` 为 5 步推理链(扫全部记忆→提取人物/事件线索→常识世界知识解释因果→合并排除臆测→选最具体最可能结论,**末尾仍只输出短答案**);不加 few-shot;`forceOpenDomainAnswerPrompt` 与答题选择逻辑逐字不动
- [ ] T015 [P] [US2] (可选)加离线确定性测试 `cmd/locomo-bench/*_test.go`:断言新 `openDomainAnswerPrompt` 含 5 步关键子句 + 保留短答案约束(进 CGO=0 CI,零 LLM)
- [ ] T016 [US2] `CGO_ENABLED=0 go build ./...` + `go test ./cmd/locomo-bench/` 绿;确认 `forceOpenDomainAnswerPrompt`/选择逻辑未改、引擎 `git diff` 空(T006)
- [ ] T017 [US2] 跑**旧版** A(git stash 或旧 worktree)US2 命令(bench-commands.md §US2):`--top-k 100 --chunk-quota 50 --retrieval hybrid --only-category 3 --judge-mem0-aligned --run-dir .locomo-run/008-opendomain-old`
- [ ] T018 [US2] 跑**新版** B(同配置逐字一致,唯一变量=prompt)→ `.locomo-run/008-opendomain-new`
- [ ] T019 [US2] 计算:open-domain 旧→新% + 配对 McNemar(b/c/p);以 007 参考产物 `.locomo-run/007-us2/results-hybrid-mem0.jsonl` 核查 multi-hop/temporal/single-hop 与全量**无回归**(SC-003)
- [ ] T020 [US2] 记结果到 `eval-log.md`;按宪法 IV 切分提交:mechanism(T014 prompt 改动,+ T015 测试)一 commit、eval-log 一 commit

**Checkpoint**:US2 独立交付——open-domain 提示工程是否捞分,有近免费定论。

---

## Phase 5: User Story 3 — 更大本地 embedder 免费 coverage A/B (Priority: P3,备胎)

**Goal**:大 embedder 是否在 rerank 之前就抬 turn@k。
**Independent Test**:整店重建大 embedder store → coverage-only A/B small vs large。

- [ ] T021 [US3] 写大 embedder sidecar(scratchpad/远端):fastembed/本地加载 `bge-large-en-v1.5`(1024d)或 gte-large/e5-large,`/v1/embeddings` + `/v1/models`;本地 only
- [ ] T022 [US3] 整店重建:新 `--store-dir .locomo-run/008-embed-large-store`,`EMBED_MODEL=<大模型>`,本地 Qwen 抽取(近免费);**small-store = 复用 `.locomo-run/007-us2/cov-store`(bge-small 384d),不另建**(A1 钉死)
- [ ] T023 [US3] coverage-only A/B(bench-commands.md §US3):small vs large,同 `--top-k 30 --chunk-quota 12 --retrieval hybrid`,零答题调用
- [ ] T024 [US3] 计算:large − small overall turn@k(+ 4 类);记录大向量对纯 Go cosine 扫描的耗时(honest-scale, FR-008);引擎 `git diff` 空(T006)
- [ ] T025 [US3] 记结果到 `eval-log.md`(值得则可与 US1 叠,先各自证伪)

**Checkpoint**:US3 独立交付——更大 embedder 是否值得,有零成本定论。

---

## Phase 6: User Story 4 — 过闸落成 opt-in + 声明新参考点 (Priority: P4,双门控:前序过闸 + 显式授权)

**Goal**:任一杠杆过闸后端到端确认,落默认关 opt-in,声明新参考点。
**Independent Test**:授权后端到端默认预算重跑,记新参考点 + flip 抽查。

- [ ] T026 [US4] ⚠️ **授权门**:确认 US1(或 US2/US3)已过闸 **且** 维护者显式授权端到端花费;未授权则停,不产生答题成本
- [ ] T027 [US4] 端到端默认预算(top-k 30)重跑 `hybrid` vs `hybrid+rerank`(US1 过闸后),记新参考点 X%(标「本地 reranker,默认关」)+ false→true flip 抽查
- [ ] T028 [US4] 确认采纳杠杆**默认 off / opt-in**(宪法 V);`eval-log.md` 写新参考点,**单独提交**、与 mechanism 分离、明标「口径/预算隔离,非涨点叠加」(FR-010, SC-006)

---

## Phase 7: Polish & 跨切面

- [ ] T029 [P] 更新记忆:新杠杆 verdict 落 `competitive-targets` / 相关 memory(win 记路径,dead 记坟场,与 assoc/PCIC/abstention 一致)
- [ ] T030 [P] 最终扫尾:引擎 `git diff` 空、死规则无云 rerank、`CGO=0` build/test 绿、产物全在 `.locomo-run/008-*`、无凭据落盘
- [ ] T031 若 US1 过闸:在 eval-log 明确「reranker 使默认预算 top-k30 下拿分 → 消除 007 宽预算伪影」(SC-007)

---

## Dependencies & 执行顺序

- **Setup(T001-T004)** → 阻塞全部。
- **Foundational(T005-T006)** → 阻塞 US1/US3(自检脚手架)。
- **US1(T007-T013)**、**US2(T014-T020)**、**US3(T021-T025)** 相互**独立**,可并行(不同文件/产物目录)。US2 不依赖 sidecar。
- **US4(T026-T028)** 依赖 US1(或其它)过闸 + 授权。
- **Polish(T029-T031)** 最后。

## 并行机会

- Phase 1:T001/T002/T003 并行。
- Phase 2:T005/T006 并行。
- **跨 US 并行**:US1(远端 GPU reranker)、US2(本地纯 prompt A/B)、US3(远端大 embedder)可三线并行——US2 完全不碰 sidecar,US1/US3 用不同 sidecar/端口/产物目录。
- Polish:T029/T030 并行。

## MVP 范围

**MVP = US1**(本地 reranker 免费 coverage 闸)。它单条即可回答「engram 冲 88–90%+ 的合法主路径是否成立」,零答题成本,引擎零改。US2 是免费的并行加分(打唯一短板),US3 是 US1 的备胎/叠加,US4 是过闸后的落成。

## 提交切分(宪法 IV,硬)

- US2 mechanism(prompt + 测试)与其 eval-log **两个 commit**。
- US1/US3 无 Go 改动 → 只有 eval-log commit。
- 任何新参考点单独提交,明标口径/预算隔离、非涨点叠加。
