# Phase 0 Research: LoCoMo 跑分杠杆探索(008)

所有决策已在前置调研(超级大脑方向图 agent + 007 coverage 分诊 + reranker 契约核查)中收敛,**无 NEEDS CLARIFICATION 遗留**。以下为定稿。

## D1 — 本地 reranker 模型(US1)

- **Decision**: 首选 `BAAI/bge-reranker-v2-m3`(cross-encoder,约数百 MB,GPU 上快);FAIL 时依次退 `jina-reranker-v2-base-multilingual` → `mixedbread-ai/mxbai-rerank-large-v1`。用 `FlagEmbedding.FlagReranker`(或 sentence-transformers `CrossEncoder`)本地加载。
- **Rationale**: 003 已用付费 gte-rerank-v2 证实 rerank 阶段 +8.3pp 多跳 coverage / +8.1pp 多跳答题、coverage→答题近 1:1;唯一未知是**本地模型能否追平**。bge-reranker-v2-m3 是当前最强开源通用 cross-encoder 之一,离线、可移植、体积小 → 满足死规则 + 宪法 I/V。MemOS 88.83 本地成绩佐证本地栈可行。
- **Alternatives rejected**: 付费云 rerank(gte/Cohere/Jina 云 API)= **死规则硬禁**;本地大 LLM 当 reranker = 慢且过重。

## D2 — reranker sidecar 形态与部署(US1)

- **Decision**: 单个 Python sidecar,**双端点**:`/v1/embeddings`(fastembed `BAAI/bge-small-en-v1.5`,384d,保持与固化 store 同源)+ `/rerank`(bge-reranker-v2-m3),响应严格 `{"results":[{"index":int,"relevance_score":float}]}`,裸 `/rerank` 与 `/v1/rerank` 都兼容。部署在**远端 97GB GPU 机**(与 vllm 共存),本地经 SSH 隧道访问,`EMBED_BASE_URL=http://127.0.0.1:<tunnel>/v1`。记录**不含文本**的 batch 延迟。
- **Rationale**: 双端点让引擎共享一个 `EMBED_BASE_URL` 即可同时拿 embedding + rerank(引擎 `HTTPReranker` 打 `{base}/rerank`);远端 GPU 跑 cross-encoder 比本地 CPU 快得多(1540 题 × ~300 候选池)。照 007 `embed_server.py` 的 stdlib HTTP 套路,零新框架。
- **Alternatives rejected**: (a) 本地 CPU 全自托管——零远端依赖但 1540×重排池显著慢;留作 fallback。(b) 本地路由到远端 rerank 而 embedding 走本地——因引擎共享 `EMBED_BASE_URL`,会多一层无谓代理。(c) infinity/TEI 服务器——可行但版本兼容面更大,自写 sidecar 对响应 shape 完全可控。

## D3 — US1 预算与判定门

- **Decision**: coverage 闸在**可交付默认预算 top-k 30 / chunk-quota 12** 下跑,臂 = `hybrid` vs `hybrid+rerank`,复用固化 store(不重建)。**门槛**:`hybrid+rerank` 对 `hybrid` 的 overall turn@k 抬升 **≥ +4pp**(多跳为关键类,付费版曾 +8.3pp)→ PASS。
- **Rationale**: reranker 的独特价值正是「在默认预算下补排序」——若成立,可把 007 的 84.8%(靠 top-k100/quota50 宽预算)换成**可交付默认预算**下的分,合法消除宽预算伪影(SC-007)。+4pp 门槛保守低于付费版 +6.7pp overall,给本地模型留余量但仍要求实质抬升。
- **Alternatives rejected**: 用宽预算 top-k100/quota50 跑 reranker——会掩盖「默认预算」这个关键卖点,且宽预算下天花板已高、rerank 边际变小。

## D4 — US2 open-domain prompt(方案1)+ 单变量 A/B

- **Decision**: **仅**替换 `cmd/locomo-bench/runner.go` 的 `openDomainAnswerPrompt` 为 5 步推理链(扫全部记忆 → 提取人物/事件线索 → 用常识与世界知识解释因果 → 合并并排除臆测 → 选最具体最可能的结论,**末尾仍只输出短答案**);`forceOpenDomainAnswerPrompt` 与答题选择逻辑**逐字不动**;**不加 few-shot**。A/B:旧/新工作树各跑 `--only-category 3`,hybrid / top-k 100 / chunk-quota 50 / mem0-aligned / 本地 Qwen / bge / deepseek 判**完全一致**;配对 McNemar + 以 007 原参考产物核查其余三类与全量无回归。
- **Rationale**: open-domain 62.5% 是唯一短板(比另三类低 ~25pp),其 miss 多为推理/世界知识不足(证据常已检索到)→ 提示工程可捞,免费、纯 adapter、引擎零改。当前参考点实为 `force_answer=false`,故**只改非 force 变体正好隔离单变量**,force-answer 简洁契约不变。用 007 的 top-k100/quota50 配置是为了与参考点单变量对齐(不是默认预算——这与 US1 的 top-k30 目的不同,两者产物分目录不混用)。
- **Alternatives rejected**: 方案2 加 1–2 few-shot(拉长输入、锚定答案表述、无失败题模板支撑);方案3 同时重写 force 变体(违反「只改该 prompt」、且不属当前 A/B 参考基线)。

## D5 — US3 更大 embedder(备胎)

- **Decision**: 候选 `bge-large-en-v1.5`(1024d)/ `gte-large` / `e5-large`,**整店重建**(存储+查询向量同模型)到新 `--store-dir`,`--coverage-only --retrieval hybrid` A/B small-store vs large-store;记录更大向量对纯 Go cosine 扫描的性能影响(honest-scale)。
- **Rationale**: 语义臂现在只有 bge-small 384d;更强 embedder 可在 RRF 内、rerank 之前就抬 gold 召回,尤其 open-domain(最吃语义召回)。作 US1 fallback 或与之叠加(先各自证伪)。
- **Alternatives rejected**: 复用 384d store 只换查询 embedder——维度/分布漂移,语义臂失效,必须整店重建。

## D6 — 死规则执行 + 凭据

- **Decision**: sidecar **只加载本地模型文件**,代码内无任何云 rerank URL;`EMBED_BASE_URL` 仅指自托管 sidecar(经隧道)。已在 007 铲除的 7999 gte-rerank 付费代理**禁复活**。远端 host/port/password 与 API key 只走 env / SSH host alias / 隧道,**绝不**落聊天/仓库/日志/工具响应。
- **Rationale**: 死规则是本仓库 death-rule + 宪法 I;昨日 gte-rerank 已烧 ¥100+ 并被下令永禁。凭据纪律见 [docs/remote-eval-box.md](../../docs/remote-eval-box.md)。
- **Alternatives rejected**: 任何「诊断性付费 rerank 涨点」——即便标注也不得作为本特性交付路径。

## 复用资产(已就绪)

- 固化 store:`.locomo-run/007-us2/cov-store`(10 段,bge-small 384d)。
- 引擎 rerank 通道:`embedding/rerank.go` + `memory.NewRetrieverWithOptions` + `cmd/locomo-bench` `buildBenchReranker`/`--retrieval hybrid+rerank`。
- 答题/判题栈:本地 Qwen3.6 vllm(隧道 :8000)+ deepseek-v4-flash mem0-aligned(JUDGE_* env)。
- 类别号:1=multi-hop 2=temporal **3=open-domain** 4=single-hop 5=adversarial(排除);`--only-category` flag 已存在。
