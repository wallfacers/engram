# Feature Specification: LoCoMo 跑分杠杆探索 — 本地 reranker + open-domain 提示(免费闸优先)

**Feature Branch**: `008-locomo-score-levers`

**Created**: 2026-07-22

**Status**: Draft

**Input**: User description(超级大脑方向图 + 维护者 brainstorm 定稿):把「对标→对齐→超越 MemOS 88.83(本地)/ Mem0 92.5」拆成一组**独立可测、免费闸优先**的检索/答题杠杆实验,设计到「外部 Agent 傻瓜式执行、无需自己再头脑风暴」的粒度。当前参考点:007 mem0-aligned hybrid = 84.8%(1540,Qwen3.6 抽取+答题、bge-small 384d、top-k 100/quota 50、deepseek-v4-flash 判)。唯一短板 open-domain 62.5%。已确认但因**死规则作废**的最强赢 = cross-encoder rerank(003:+8.3pp 多跳 coverage / +8.1pp 多跳答题)。

## 背景与定位

本特性是**评测杠杆探索特性**,"用户" = 维护者。它触及**检索(reranker)与答题(prompt)口径** → 宪法 IV 评测回归门适用:任何被采纳的杠杆必须先过**免费 coverage 闸 / 单变量端到端 A/B**,eval 结果单独提交、声明新参考点、明标口径隔离。**引擎零改**(rerank 阶段与 `embedding.Reranker` 接口引擎已具备,本特性只在 adapter/sidecar/env 层动)。

**死规则(硬,非协商)**:reranker 必须是**本地/自托管**模型;严禁任何**付费云 rerank**(DashScope gte-rerank / Cohere / Jina 云 API 等)作涨分手段、默认栈或推荐栈。本特性的全部价值在于把「已证但被判死的付费 rerank 赢」换成**纯客户端、可移植、离线**的形态——MemOS 88.83 本地成绩正证明这条路可行。

每个 User Story 是一条**独立可交付、可单独证伪**的杠杆;失败的杠杆按坟场记录(与 assoc/PCIC/abstention 一致),不进默认。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 本地 cross-encoder reranker 的免费 coverage 闸 (Priority: P1) 🏆

维护者用**本地** cross-encoder reranker(经引擎已有的 rerank 通道)在**可交付默认预算(top-k 30 / chunk-quota 12)**下,以零成本 `--coverage-only` bake-off 证明:本地 reranker 能复现 003 已确认的 rerank coverage 大赢。这是 MVP——它把「最强但被死规则作废的赢」变成合法可移植赢,且顺带能在默认预算下拿分(抹掉 007 宽预算伪影)。

**Why this priority**: 这是全组 EV 最高的一手。付费版已证 coverage→答题近 1:1(003),唯一未知=本地模型质量能否追平;这个用**零答题 token** 就能答。过闸=拿到 engram 冲 88–90%+ 的合法主路径;不过闸=便宜地证伪、转备胎。

**Independent Test**: 起本地 reranker sidecar(serve `POST {EMBED_BASE_URL}/rerank`,响应严格 `{"results":[{"index":int,"relevance_score":float}]}`),`--coverage-only` 跑 `hybrid` vs `hybrid+rerank`,复用已固化 store,零答题/判题调用;读 `coverage.json` 的 turn@k(overall+4类)。全程免费。

**Acceptance Scenarios**:

1. **Given** 本地 reranker sidecar 就绪且 `EMBED_RERANK_MODEL` 指向本地模型,**When** 跑
   `--data testdata/locomo/locomo10.json --store-dir .locomo-run/007-us2/cov-store --coverage-only --chunks --top-k 30 --chunk-quota 12 --retrieval 'hybrid,hybrid+rerank'`,
   **Then** 产出 `coverage.json`,含两臂 turn@k(overall + multi-hop/temporal/open-domain/single-hop)+ session_recall,**零** answer/judge LLM 调用。
2. **Given** 上述结果,**When** 比较 `hybrid+rerank` 对 `hybrid` 的 overall turn@k,**Then** 抬升 **≥ +4pp**(多跳为关键类,付费版曾 +8.3pp)判「本地 reranker 复现可移植赢」→ PASS;否则判「本地模型追不平」→ 记录并转 US3 备胎(可换本地模型再试一次)。
3. **Given** reranker 通道,**When** 检查 sidecar 加载来源,**Then** 确认为**本地模型文件**(非任何云 rerank API 代理);`EMBED_BASE_URL` 仅经 SSH 隧道指向自托管 sidecar。
4. **Given** 本特性任意改动,**When** `git diff --name-only -- memory embedding provider store internal`,**Then** 为空(引擎零改)。

---

### User Story 2 - open-domain 答题提示对齐(方案1:5步推理链) (Priority: P2)

维护者只替换 `openDomainAnswerPrompt` 为一段显式推理链(**扫全部记忆 → 提取人物/事件线索 → 用常识与世界知识解释因果 → 合并并排除臆测 → 选最具体、最可能的结论**,末尾仍只输出短答案),`forceOpenDomainAnswerPrompt` 与选择逻辑**逐字不动**;以 `--only-category 3` 单变量 A/B 验证 open-domain(62.5%,唯一短板)是否被捞起,且其余三类与全量不回归。

**Why this priority**: 免费、纯 adapter、引擎零改、独立于 US1;直接打当前唯一短板。open-domain 的 miss 多是推理/世界知识不足(证据常已检索到),提示工程可捞。当前参考点实为 `force_answer=false`,故只改非 force 变体正好隔离单变量。

**Independent Test**: 在旧代码工作树与新代码工作树各跑 `--only-category 3`,保持 hybrid / top-k 100 / chunk-quota 50 / mem0-aligned judge / Qwen 本地答题 / bge / deepseek 判题**完全一致**;比较 open-domain%,配对 McNemar;以 007 原参考产物核查全量与其余三类未回归。

**Acceptance Scenarios**:

1. **Given** 仅 `openDomainAnswerPrompt` 文本变化(其它逐字不动),**When** 同配置对 `--only-category 3` 跑旧/新两版,**Then** 报 open-domain 旧→新% + 配对 McNemar(b/c/p),且输出仍为 force-answer 简洁短答案(判题口径不变)。
2. **Given** 新 prompt,**When** 以 007 原参考产物核查其余三类(multi-hop/temporal/single-hop)与全量,**Then** **无回归**(其它类不因本改动下降超噪声)。
3. **Given** 本改动,**When** 检查改动面,**Then** 仅 `cmd/locomo-bench/runner.go` 的 `openDomainAnswerPrompt`;引擎 `git diff` 为空;`forceOpenDomainAnswerPrompt` 与答题选择逻辑未改。

---

### User Story 3 - 更大本地 embedder 的免费 coverage A/B (Priority: P3,备胎/可叠加)

维护者换更强的**本地** embedder(如 bge-large-en-v1.5 1024d / gte-large / e5-large),重建 store(存储向量与查询向量必须同模型),以 `--coverage-only` A/B 看 RRF 内语义召回是否在 rerank 之前就抬 turn@k。作 US1 的 fallback,或与之叠加。

**Why this priority**: 有价值但需重建 store(比 US1 重),且 US1 大概率吃掉同一份排序收益;先证伪 US1 再决定是否动这条。honest-scale:更大向量拖慢纯 Go cosine 扫描,如实记(宪法 V)。

**Independent Test**: 起大 embedder sidecar,新 `--store-dir` 用大模型本地抽取(近免费),`--coverage-only --retrieval hybrid` 对比 small-store vs large-store 的 turn@k(4类)+ session_recall;零答题调用。

**Acceptance Scenarios**:

1. **Given** 大 embedder 重建的 store,**When** `--coverage-only` A/B small vs large(同 top-k/quota),**Then** 报两 store 的 turn@k(4类)+ session_recall,零答题/判题调用。
2. **Given** 结果,**When** 判定,**Then** large 对 small overall turn@k 为正且 open-domain/多跳明显 → 值得(可与 US1 叠,先各自证伪);否则记录不值得。
3. **Given** 仅 `EMBED_MODEL` 变化,**When** 检查改动面,**Then** 引擎 `git diff` 为空。

---

### User Story 4 - 过闸杠杆落成默认关 opt-in + 声明新参考点 (Priority: P4,双门控)

任一杠杆(US1/US2/US3)过其免费闸/A/B 后,维护者在显式授权下端到端确认,并把它落成**默认关 / opt-in**(宪法 V:reranker 与 embedding 保持可选、离线可降级),eval 结果单独提交、声明新参考点、明标「口径/预算隔离,非与其它杠杆叠加」。

**Why this priority**: 有价值但依赖前序闸通过且需端到端花费;默认不执行。翻默认或纳入推荐栈是**单独决策**。

**Acceptance Scenarios**:

1. **Given** US1 过 coverage 闸,**When** 端到端在默认预算(top-k 30)重跑 `hybrid` vs `hybrid+rerank`,**Then** 记录新参考点 X%(标注「本地 reranker,默认关」)+ false→true flip 抽查;reranker **默认 off**,仅 opt-in 开启。
2. **Given** 任一新参考点,**When** 提交,**Then** eval 结果与 mechanism 分离提交(宪法 IV),明标口径/预算隔离,不与其它杠杆收益叠加。
3. **Given** 未授权,**When** 任何时候,**Then** US4 不执行,不产生端到端答题成本。

---

### Edge Cases

- **本地 reranker 追不平 gte**:US1 场景2 判 FAIL 时,允许换一个本地模型(bge-reranker-v2-m3 → jina-reranker-v2 / mxbai-rerank)再证伪一次;仍不过则记坟场、转 US3。
- **sidecar `/rerank` 响应格式不符**:引擎 `HTTPReranker` 期望严格 `{"results":[{"index","relevance_score"}]}`;index 越界或缺 results → 引擎按 per-signal 失败静默降级为融合序(不崩)。sidecar 必须严格匹配该 shape。
- **embedder 维度漂移(US3)**:大 embedder 的查询向量必须与**同模型重建**的存储向量同源;混用 384d store + 1024d 查询 = 语义臂失效。US3 必须整店重建。
- **open-domain prompt 过长锚定(US2)**:5步推理链不得引入 few-shot 示例锚定答案表述(方案2 已排除);保持零样本、末尾强制短答案。
- **default 预算 vs 宽预算混淆**:US1 用 top-k 30/quota 12(可交付默认);US2 用 top-k 100/quota 50(对齐 007 参考点做单变量);两者**不得混用**,产物分目录。
- **凭据泄漏**:远端 host/port/password 与任何 API key **绝不**粘进聊天/仓库/日志/工具响应;只走 env / SSH host alias / 隧道。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**(死规则,硬):系统采用的 reranker MUST 为本地/自托管 cross-encoder;MUST NOT 使用任何付费云 rerank/召回 API 作涨分手段、默认栈或推荐栈。sidecar MUST 从本地模型文件加载,不得代理任何云 rerank 端点。
- **FR-002**(引擎零改,硬):本特性 MUST NOT 修改 `memory/ embedding/ provider/ store/ internal/` 下任何 `.go`;`git diff --name-only -- memory embedding provider store internal` MUST 为空。reranker 复用引擎已有的 `embedding.Reranker` 接口与 `NewRetrieverWithOptions` rerank 阶段。
- **FR-003**(免费闸优先,宪法 IV):任何检索/embedding 杠杆(US1/US3)MUST 先过**零答题/零判题**的 `--coverage-only` 闸;未过闸不得进入端到端花费。答题级杠杆(US2)MUST 以单变量端到端 A/B 验证并核查无回归。
- **FR-004**:US1 的 coverage 闸 MUST 在**可交付默认预算**(top-k 30 / chunk-quota 12)下评测,并复用已固化 store `.locomo-run/007-us2/cov-store`(bge-small 384d,不重建);对比臂 = `hybrid` vs `hybrid+rerank`。
- **FR-005**:本地 reranker sidecar MUST serve `POST {EMBED_BASE_URL}/rerank`(裸 `/rerank` 亦兼容),请求 `{model,query,documents,top_n}`,响应严格 `{"results":[{"index":int,"relevance_score":float}]}`;查询/存储 embedding MUST 保持 bge-small-en-v1.5(384d)以匹配已固化 store。
- **FR-006**:US2 MUST 仅替换 `cmd/locomo-bench/runner.go` 的 `openDomainAnswerPrompt` 为指定 5 步推理链(扫记忆→线索→常识因果→排臆测→选最具体结论,末尾短答案);`forceOpenDomainAnswerPrompt` 与答题选择逻辑 MUST 逐字不变;不得加 few-shot。
- **FR-007**:US2 的 A/B MUST 单变量:旧/新工作树各跑 `--only-category 3`,hybrid / top-k 100 / chunk-quota 50 / mem0-aligned judge / 本地 Qwen 答题 / bge / deepseek 判题**完全一致**;MUST 报 open-domain 旧→新% + 配对 McNemar,并以 007 原参考产物核查其余三类与全量**无回归**。
- **FR-008**:US3 MUST 整店用大 embedder 重建(同模型存储+查询向量),以 `--coverage-only` A/B 对比;MUST 记录更大向量对 Go cosine 扫描的性能影响(honest-scale)。
- **FR-009**(宪法 V,默认关):任何采纳的 reranker/大 embedder MUST 保持**默认 off / opt-in**、离线可降级;MUST NOT 成为默认或必需的推荐栈。
- **FR-010**(宪法 IV,归因):任何新参考点 MUST 单独提交、与 mechanism 分离、明标「口径/预算隔离,非涨点叠加」;失败杠杆 MUST 记入坟场(不进默认)。
- **FR-011**(凭据):远端 host/port/password 与 API key MUST 只走 env / SSH host alias / 隧道;MUST NOT 落入聊天、仓库文件、日志或工具响应。
- **FR-012**(可执行性):每个 US 的验证 MUST 有确定命令与判定门,外部执行者无需再设计(傻瓜式);产物落 gitignored `.locomo-run/008-*/`(与 007 产物分目录)。

### Key Entities *(include if feature involves data)*

- **杠杆(lever)**:一条独立可测的检索/答题改动。字段:name、层(检索/答题/embedding)、闸类型(coverage-only / 单变量端到端)、判定门、verdict(win/dead/marginal/NO-GO)、是否默认关。
- **本地 reranker sidecar**:自托管 cross-encoder + bge-small embedding 的双端点服务;契约 = `/v1/embeddings`(384d)+ `/rerank`(Cohere/Jina shape)。**无云依赖**。
- **coverage 产物**:`coverage.json` 的 turn@k(overall+4类)+ session_recall;US1/US3 的唯一免费判据。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**(US1 核心门):本地 reranker 在默认预算(top-k 30/quota 12)下,`hybrid+rerank` 对 `hybrid` 的 overall turn@k 抬升 **≥ +4pp**(多跳为关键类)→ 判可移植赢 PASS;否则明确记 FAIL + 原因。
- **SC-002**(免费):US1/US3 的 coverage 闸全程**零 answer / 零 judge LLM 调用**;US2 仅 open-domain 类(约 96 题)× 2 版 + 判题,近免费。
- **SC-003**(US2 单变量):open-domain A/B 除 `openDomainAnswerPrompt` 外**逐字同配置**;报 open-domain 旧→新% + McNemar,且其余三类与全量**无回归**。
- **SC-004**(引擎零改):全特性 `git diff --name-only -- memory embedding provider store internal` 为空。
- **SC-005**(死规则):无任何云 rerank 调用;sidecar 加载本地模型经验证;`EMBED_BASE_URL` 仅指自托管 sidecar。
- **SC-006**(宪法 V/IV):任何采纳杠杆默认 off;新参考点单独提交、明标口径/预算隔离、非涨点叠加。
- **SC-007**(预算伪影消除):若 US1 过闸,reranker 使「可交付默认 top-k 30」下的分逼近/超过 007 宽预算 84.8% → 84.8% 的宽预算伪影被合法消除,得到可交付默认分。

## Assumptions

- 远端 97GB GPU 机(见 [docs/remote-eval-box.md](../../docs/remote-eval-box.md))可与 vllm 共存挂 reranker(US1)/大 embedder(US3)sidecar;本地 CPU 自托管为 fallback(1540 题×重排池会显著慢)。凭据只走 env/隧道。
- US1/US2 复用已固化 store `.locomo-run/007-us2/cov-store`(bge-small 384d);US1 不重建、US3 必重建。
- 答题/抽取继续用本地 Qwen3.6(vllm),判题用 deepseek-v4-flash(mem0-aligned,golden 26/26 已守),与 007 参考点同栈,保证 US2 单变量隔离。
- 竞品数字(MemOS 88.83 本地 / Mem0 92.5 托管)为**方向锚**,非受控对比(抽取/答题模型不同)。
- 死规则相关:007 期间在 7999 上发现并已铲除的 gte-rerank 付费代理**不得复活**;本特性所有 reranker 均本地。
- 本特性是 007 之后的新特性(008),与 007 US2「同栈严判臂」闭环并行但独立;单 worktree/master 上推进,遵守并行隔离规约。
