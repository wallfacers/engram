# Research: 确定性次模证据装填(045)

**Date**: 2026-08-16 | 决策依据 = 代码实证(grep/读码)+ 文献(docs/research/low-topk-recall-context-survey-2026-08-15.md)+ repo verdict 链

## R1 · 现行装配路径与预算插入点(代码实证)

**发现**:`retrieveWithQuotaDiagnostics`(cmd/locomo-bench/chunks.go:171)已经先取**宽池**(`widePool = max(6×topK, 300)`)再做配额截断——`applyChunkQuota`(chunks.go:208)按 kind 分区(facts/chunks,以 `chunk-` name 前缀区分),保留 `topK-quota` facts + `quota` chunks、互相 backfill、按融合分降序还原。

**Decision**: 装填层**替换 `applyChunkQuota` 的截断逻辑**(旗标开启时),从同一宽池(≥300)做预算化次模选择。spec 里"检索池加宽至 top-150"的表述按现实修正:**池已存在且更宽(≥300),机制只改选择层,检索调用零改动**——比 spec 假设更干净,FR-004 自然满足。
**Alternatives rejected**: (a) 新增独立检索路径取 150——重复宽池逻辑,无必要;(b) 改引擎检索——违反宪法 II。

## R2 · 引擎公开面与向量可得性(代码实证)

**发现**:`memory.Result`(memory/retriever.go:110)只带 `Score`(RRF 融合分)与文本字段,**不带 embedding 向量**;`memory_embeddings` 只经 `memory/vectorstore.go` 内部访问,无公开读 API;`memory/snapshot`、`memory/export` 目录实际不存在(CLAUDE.md 架构图为规划性描述)。

**Decision**: 多样性/代表性用**词法 shingle-Jaccard 相似度矩阵**替代余弦:对每条候选取词级 k-shingle(定 k=5,Decision 冻结)集合,两两 Jaccard。确定性、离线、零模型调用、零引擎改动。verbatim 对话 chunk 的冗余以字面重复为主(同一说话人前缀/重复短语),词法近似契合主导冗余模式;语义改写的冗余会被低估——作为已知局限写入 verdict 口径。
**Alternatives rejected**: (a) 引擎加公开 `Embeddings(ids)` 读 API——合法但属引擎契约增量,probe 阶段不值;机制存活(GO+正批达标)后再作显式增量提案。(b) 用检索器自身当相似 oracle(对每条池内条目发一次 Search)——每题 300 次引擎查询,e2e 每题 +秒级开销,拒绝。(c) 本地 sidecar 重嵌——引入模型依赖,违反 US1 零模型调用。

## R3 · 目标函数四项的具体形式(文献复刻 + 042/M2 前科校准)

**Decision**(权重起点在 plan 冻结,LoCoMo 定稿后零重调):
1. **relevance(模块项)**: 归一化 RRF 融合分(`Result.Score` min-max 到 [0,1],池内归一)。
2. **query set-cover**: query 的内容词集合(去停用词、小写化、词干截断不引入词表依赖)被候选覆盖的增量比例;沿用 042 已有 gap/query 解析的词法工具风格,自包含复制。
3. **facility-location 代表性**: `Σ_pool max_{s∈S} sim(s, e)` 的增量形式,sim = R2 的 shingle-Jaccard。
4. **concave 多样性**: 已选集内两两相似度的平方根惩罚(边际递减),同一相似度矩阵。
- **cost-scaled 贪心**: 每步取 `Δf / cost(e)` 最大者,cost = 该条目渲染 token(estimator,R4)。
- **singleton fallback**: 预算内放不下任何条目时强制选 relevance 最高一条(允许单条超预算,仅此例外)。
- **relevance 主导**: 权重 3:1:1:1 起点(FR-002 防退化;MMR 反例 = 纯多样性显著变差)。
**Alternatives rejected**: 纯 MMR(文献证伪);la lazy greedy 优先队列实现(池 ≤300、预算 ≤40,朴素 O(B·N·|S|) 已是毫秒级,不值得复杂度)。

## R4 · token 预算估计器与口径分离(代码实证)

**发现**: ledger 的 `AnswerContextTokens` = answerer wire 返回的 `usage.InputTokens`(main.go:2755 等 4 处)——**真实 tokenizer 计数**;而 `estimateTokens`(agentic_nav.go:419,rune/4,≥1)只是装配期近似。⚠️ `agentic_nav.go` 是 044-default-off-cleanup T007 的**删除目标**。

**Decision**:
- **选择期预算**: 045 自带 `packEstimateTokens`(公式复刻 rune/4,含与原实现的等价性单测)——自包含,不受 044 删除影响。
- **parity 口径(SC-005)**: 用 ledger 的真实 `usage.InputTokens` 均值对比,不用估计器——两臂同一口径自动可得。
- **预算锚定**: 每题锚 `B_q = 对照臂该题渲染后的 answer-context tokens`(同批配对,逐题公平);US1 离线阶段对照渲染是确定性的,同公式离线复算。全局兜底:若逐题锚缺失(对照臂该题失败),用对照臂全局均值。
**Alternatives rejected**: 全局均值锚(题目长短差异大,逐题配对才公平);估计器口径做 parity(与 ledger 口径不一致,会误导)。

## R5 · AIC 匹配规范化(决策冻结)

**Decision**: `normalize(s) = lower(strip(collapse-whitespace(s)))`;AIC = 归一化后**任一** gold 别名(LoCoMo adjudicated answers 列表)作为**连续子串**出现在归一化后的最终上下文。分词边界不要求(LoCoMo gold 多为短语/实体,子串即可;数字/日期原样)。匹配审计:对每题记录命中别名与位置;gold 在**池内任何条目**都匹配不到的题单列(数据集别名缺口,不是机制失败)。规则在门计算前冻结,不因分数回调。
**Alternatives rejected**: 分词序列匹配(对中英混排 chunk 引入分词器依赖);模糊匹配/编辑距离(可调参数,违零重调精神)。

## R6 · 042 重验自包含实现(数据实证)

**发现**: `.locomo-run/042-20260815/` 有 `collect/`(信号采集)+ `label-audit/`(标签)工件;032-store 在 `hf-restore/`。043 的修复(1eb9cdd)在 `counterfactual_utility_http.go`(T014 删除目标)与 `confidence_deepen.go`(043 专属,044 可能扩删)。

**Decision**: 045 新增 `reverify_042.go` 自包含三件:(a) 极简 OpenAI 兼容 logprob 调用器(chat/completions + logprobs,`temperature=0` 显式传);(b) final-span 鲁棒映射(剥 `<|im_end|>` 等特殊 token → 最后关闭符后取 span → mean/p10/top1-top2 三特征,公式复刻 1eb9cdd,含等价性单测);(c) 2-conv slice 驱动(读 042 collect 工件的问题子集)。不 import 任何 042/043 文件。
**Alternatives rejected**: 直接调用 042/043 现有函数——044 删除后悬空引用;等 044 合并后再写——串行化两个 feature,没必要。

## R7 · 计算复杂度与离线全量可行性(估算)

- 相似度矩阵: 300×300 Jaccard over ~200-shingle 集合 ≈ 每题 <10ms(Go map/set)。
- 贪心: B≈20-40 步 × 300 候选 × 增量更新 ≤40 项 ≈ 亚毫秒级。
- **US1 全量 1540 题**: 检索(本地 SQLite hybrid,~10ms/题)+ 装填 + 三口径渲染匹配 ≈ **分钟级,全量可行**(假设条目成立)。
- e2e probe 每题新增开销: 装填 <10ms,可忽略(相对 answerer 秒级)。

## R8 · 与 044-default-off-cleanup 的合并协调(接触面清单)

- 045 **只新增文件**:`submodular_packing.go` / `submodular_packing_cli.go` / `submodular_packing_test.go` / `aic_gate.go` / `reverify_042.go`(+各自 _test)。
- 045 **触碰共享文件仅 main.go**(旗标注册区 + arm 路由一行)+ `eval_runner.go`(装配点一个分支)——与 044 的 T002-T014 删除段不同区域,git 文本冲突可机械解决。
- 合并顺序: 无逻辑耦合,先并谁都可以;045 的自包含纪律(FR-013)保证 044 删除不破坏 045。
- **已标记维护者**: 044 窗口的 spec 前提"master 无 043 代码"已过时(e6625d8 合并了 043 的 confidence_deepen*.go)——044 需 rebase 到 e6625d8 后重跑 T001 基线,把 043 文件纳入其删除分类。

## 文献锚

- What Survives Into Context(arXiv:2607.00725): answer-in-context 诊断 + 预算化次模装填(四目标 + cost-scaled 贪心 + singleton fallback);scope map 条件 (iv) reader>7B 优势消失 → 期望压低。
- survey 正本: docs/research/low-topk-recall-context-survey-2026-08-15.md §1.1/§3-P2。
