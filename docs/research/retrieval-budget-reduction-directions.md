---
title: 缩减 top-k 的方向调研（预算下提质，非加量）
summary: 针对 top-k150 加量型涨点不被认可的问题，经 alphaXiv 深入 6 篇 + 补充检索，把「缩减 top-k」的正解拆为四个未探索方向：自适应截断（TAA-k，纯 Go 确定性）、冗余感知子集选择（AdaGReS 次模，纯 Go embedding）、本地 listwise reranker（jina-reranker-v3.5，opt-in）、generator-aligned 信息增益剪枝（IGP，需 logits）。核心洞察：固定 top-k 在 query-dependent/heavy-tail 分布下失败；relevance≠utility。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
canonical_for: [retrieval-budget-reduction]
tags: [research, retrieval, top-k, budget, rag, adaptive]
---

# 缩减 top-k 的方向调研

## 背景与问题

当前 LoCoMo 91.10%（clean 3-rep 多数票）靠 top-k150 拿到，代价是 **2.4× 上下文税**
（8547 vs ~3600 token）+ 32768 部署约束，是加量型涨点（[[topk150-exploration-verdict]]）。
Qwen 栈已贴候选天花板（oracle 91.62%，差距仅 ~0.52pp），剩余空间在候选覆盖与证据选择
质量，不在模型/提示词/加预算。本调研问：**如何在更小 top-k 下保持或提升精度**——
即「预算下提质」，对齐 maintainer 的 [[lever-philosophy-signal-not-volume]]。

## 核心洞察（论文交叉验证）

1. **固定 top-k 是脆弱的**：检索相似度分布随 query 剧烈变化（heavy-tail），单一全局 cutoff
   对部分 query 引入噪声、对另一部分漏掉关键证据（TAA-k、Know-Before-You-Fetch、
   R³AG、SAGE 一致指向）。
2. **relevance ≠ utility**：离线相关性（NDCG）与端到端 QA 质量弱相关、多证据注入下甚至
   **负相关**（Less-is-More IGP 图 1，Spearman 从 +0.11 到 −0.54）。高相关 chunk 可能
   near-dup / 冲突 / 非决定性，占用预算并摊薄生成分布。
3. **因此「缩减 top-k」的正解不是简单减 k，而是**：自适应截断 + 去冗余 + generator-aligned
   选择 + 精排。这四类都在「检索之后、作答之前」，不碰写入侧（027/028/037 已三证伪）。

## 四个未探索方向（按本地可落地性排序）

### 方向 1：自适应 top-k（TAA-k）——纯 Go 确定性，零模型 ⭐ 最直接

- **论文**：[Tail-Aware Adaptive-k](https://www.alphaxiv.org/abs/2606.11907)（37 Interactive Entertainment / 武大，2026-06）。
- **机制**：训练无关，直接在排序相似度序列上做 (1) knee detection 定位相关→噪声过渡区
  （排序曲线呈 steep–flat–steep 形状）(2) 在该窗口内用 EVT/GPD 拟合 + Cramér–von Mises
  goodness-of-fit 验证「最早稳定最小值」= 截断点。复杂度 O(N + √N log N · M)，比全局 EVT
  快 10×，near-oracle F1（ΔF1 2–3%）。
- **为什么契合 engram**：engram 现在 RRF 融合三信号后固定 top-k 150；TAA-k 直接在该分数
  序列上算 per-query k——single-hop 大量题可缩到 10–30，只有重尾/多跳题需要大 k。
  **预算下降、去噪后精度可能不降反升**。纯统计，无模型，纯 Go 可写，宪法 I/V 完全满足。
- **风险/验证点**：engram 是 hybrid RRF（语义+keyword+实体三信号融合），分数不是纯 cosine，
  EVT 的 GPD 假设需验证；但 knee detection 是模型无关的几何性质，可先单独落地。已有关键
  子集（multi-hop 95.7% / temporal 91.9% / single-hop 91.8%）里 single-hop 是最大错题池
  （69/137），若自适应能把 single-hop 的 k 收缩而不漏 gold，收益最集中。

### 方向 2：冗余感知子集选择（AdaGReS 次模）——纯 Go embedding，去冗余提质 ⭐

- **论文**：[AdaGReS](https://www.alphaxiv.org/abs/2512.25052)（中南大学 / 意智，2025-12），
  配套 [What Survives Into Context](https://www.alphaxiv.org/abs/2607.00725)（2026-07）次模证据打包。
- **机制**：set-level 评分 F = α·Σsim(q,c) − β·Σ_{i<j}sim(c_i,c_j)（relevance 减冗余），
  贪心选择边际增益，β 用候选池统计 + 预算的**闭式自适应解**（消除手工调参）。ε-近似次模，
  有 (1−1/e) 级近优保证。只用 L2-normalized embedding 余弦，无模型。
- **为什么契合**：engram 的 chunk 相邻重叠、near-dup 是真实痛点（尤其 multi-hop 证据链
  常被重复 chunk 挤占预算）。从 150 宽召回里贪心选一个非冗余子集（如 30–50），token 降
  2–3×、覆盖不降、去冗余提升证据密度。**本质是 MMR 的升级**（MMR 固定权重+局部贪心 →
  自适应 β + set-level 目标 + 闭式解），engram 当前 RRF 融合后**没有**冗余去重步骤。
- **与 026 区分（关键）**：026 证伪的是「LLM need-剪枝式编译在固定候选内不涨」（multi-hop
  无提升）；AdaGReS 是**确定性 embedding 冗余去重**，不是 LLM 剪枝、不是压缩证据，机制不同，
  可独立试。二者同属「检索后选择」但一个用模型、一个用几何。

### 方向 3：本地开源 listwise reranker（jina-reranker-v3.5）——opt-in sidecar

- **论文**：[jina-reranker-v3.5](https://www.alphaxiv.org/abs/2607.18152)（Jina AI，2026-07）。
- **机制**：0.6B listwise reranker（Qwen3-0.6B backbone，hybrid 3L2G attention + 三阶段自蒸馏），
  BEIR nDCG@10 = 63.20，**用 1/7 参数匹配 4B Qwen3-Reranker**；长文档延迟 1.56× 加速。
  **开源（HF，非商业许可）**，可经 fastembed/Ollama 本地 sidecar 部署。
- **为什么值得重看**：engram 试过本地 reranker（008 US1 coverage +15pp 但 e2e NO-GO）和
  自训 reranker（037 三证伪），但**那都是 pointwise cross-encoder 或自训**。jina-v3.5 是
  **现成开源 listwise**，绕开 037 的训练/vllm-serving 坑。listwise 的 LBNL 交互做跨文档
  比较，与 pointwise 不同。
- **硬约束（008 纪律不豁免）**：coverage 提升不转化 e2e 的教训仍适用——必须先过
  **端到端配对**（固定候选/answerer/judge/预算），不是 coverage 涨就出货。且 jina-v3.5
  非商业许可，opt-in sidecar、默认关、无模型退化到现状，符合宪法 I/V。

### 方向 4：generator-aligned 信息增益剪枝（IGP）——需 answerer logits，优先级低

- **论文**：[Less is More for RAG](https://www.alphaxiv.org/abs/2601.17532)（大连理工等，2026-01）。
- **机制**：从生成器视角定义证据效用 = 注入该 passage 后 top-k entropy 的下降量（信息增益），
  按 IG 排序 + 阈值剪枝。多证据设置 +12–20% F1、减 76–79% token；小模型+IGP 可超更大模型。
- **落地障碍**：需 step-wise logits / TOPK log-probs（本地 vllm Qwen 需开 logits + 每 query
  N+1 次 probe 调用，成本高）；且与 engram 现有多数票聚合（已隐含不确定性）部分重叠。
  记录为后备方向，不优先。

## 证据强度与迁移边界（诚实）

- 四篇论文的实证都在 **open-domain QA**（NQ/TriviaQA/HotpotQA/2Wiki/MuSiQue），**不是
  LoCoMo/LongMemEval**；judge 口径、分母、answerer 与 engram 不同，绝对分数不可横比
  （[[high-scoring-memory-systems]] 的老教训）。机制级证据可迁移，系统级分数不可。
- 增益幅度需在 engram 栈重测：LoCoMo 的候选覆盖（top-k30 recall 0.808）与 open-domain QA
  的 recall@50 量级不同，方向 1/2 的「去噪不丢 gold」需用 gold-rank 诊断验证（L0-2 纪律）。

## 与已有 verdict 的关系（避免重复踩坑）

| 方向 | 是否新 | 与已有 verdict 的关系 |
|---|---|---|
| TAA-k 自适应截断 | **新** | ≠ 固定 top-k 全局 sweep（topk150 是固定 k，从未做 per-query k） |
| AdaGReS 冗余去重 | **新** | ≠ 026 的 LLM need-剪枝（这是确定性 embedding 去冗余） |
| jina-reranker-v3.5 | 半新 | ≠ 008 pointwise / 037 自训（现成开源 listwise）；仍需 008 端到端配对纪律 |
| IGP 信息增益 | 新 | 需 answerer logits，与多数票聚合重叠，后备 |

## 建议的探索顺序

1. **方向 1（TAA-k 自适应截断）**：纯 Go、零模型、零成本，直接改 harness 的固定 top-k →
   per-query 截断，先做 knee detection 部分，用现有 store + gold-rank 诊断验证「收缩 k 不丢
   gold」。若 single-hop 的 k 能收缩且不降，是最干净的「预算下提质」赢。
2. **方向 2（AdaGReS 冗余去重）**：纯 Go、零模型，作为 RRF 之后、截断之前的确定性去冗余
   步骤。二者可叠加（先自适应截断定 k 上界，再去冗余选子集）。
3. **方向 3（jina-v3.5）**：opt-in sidecar，端到端配对验证（008 纪律），默认关。
4. **方向 4（IGP）**：等 answerer logits 通路明确后再评估。

方向 1+2 都是**纯 Go 确定性、零模型、零网络**，天然满足宪法 I（local-first）/V（诚实规模），
不触碰付费云 rerank DEATH RULE，也不走已被证伪的写入侧/换表示/agentic 路线。它们应作为
下一轮「缩减 top-k」的先导实验（低成本的 coverage/gold-rank 诊断先行，遵循
[[offline-coverage-bakeoff-setup]] 的近零成本打法）。

## 数据源

- TAA-k：arXiv 2606.11907（自适应截断，EVT + knee detection）
- AdaGReS：arXiv 2512.25052（冗余感知贪心，次模）；2607.00725（预算受限次模证据打包）
- jina-reranker-v3.5：arXiv 2607.18152（0.6B listwise，自蒸馏）
- IGP：arXiv 2601.17532（信息增益剪枝）
- 补充检索：2606.29959（校准预算分配）、2608.07152（无固定 top-L 的 hybrid 融合）、
  2606.02581（cost-aware 路由）、2605.05245（AdaGATE gap-aware 装配）
