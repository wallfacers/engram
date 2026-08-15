# 低 top-k 达到 top-150 效果:文献调研(2026-08-15)

**问题**:LoCoMo (1,540) top-k 30 = 87.9%,top-k 150 = 91.43%。如何在不加 top-k 的前提下补上这 ~3.5pp。
**方法**:alphaXiv MCP 文献调研(6 篇深读 + 12 篇扫描),与 repo 已走过路径(docs/evaluation/experiment-verdicts + specs/)交叉对照。
**红线**:纯客户端/离线;零重调可迁移;禁数据集/问题标注;禁云端 reranker 作为得分手段。

---

## 0. 诊断重校准(读文献前的前提)

两条 repo 硬诊断决定方向:

- **040 verdict**:LoCoMo 30→150 的增量中 **79% 是"上下文量"问题**(gold 已在 top-30)、仅 21% 是召回。
- **LME**:非 ABS 错题 gold 100% 在 top-150 内(中位 rank 3),检索不是瓶颈。

因此本问题**不是"提升召回率"问题,而是"低 k 下让 answerer 拿到与 150 条等价的信息"**。文献独立支持这一重校准(见 §1.1)。

---

## 1. 深读论文结论

### 1.1 What Survives Into Context — answer-in-context 诊断 + 次模装填
**arXiv:2607.00725**(2026-07, PKDD 风格)

- **诊断**:recall@k 在"检索集"上计分,但 reader 消费的是**装填后的上下文**。预算约束下两者解耦:recall@5=1 的题中 **27% 在装填时丢掉答案**(F1 0.61 vs 0.20,4.6× EM gap)。提出 **answer-in-context**:gold 答案是否作为连续 span 出现在最终上下文——预测力优于 recall(ΔR²=+0.17,partial corr +0.43),且经 2Wiki 干预实验证实因果(移动 coverage 不移动 answer-in-context → 分数不动)。
- **方法**:预算化单调次模最大化,四项目标:relevance(模块)+ query set-cover + facility-location representativeness + concave diversity。cost-scaled 贪心 + Lin–Bilmes singleton fallback。HotpotQA 160-token 预算 + 3B reader:比 focused heuristic +2.2 F1、比 naive +5.1、比 MMR +4.2,**token 更少**。
- **诚实的 scope map(关键)**:赢需四条件同时成立——(i) multi-hop 互补结构、(ii) 检索已浮出证据、(iii) 预算紧但不极端(倒 U,峰值 ~160)、(iv) **reader 弱到证据密度是瓶颈:3B 赢、7B 被吸收、14B 反转**。
- **对 engram 的含义**:
  - `answer-in-context` 应取代 `gold_in_pool` 成为 harness 诊断指标(033 教训:`gold_in_pool` 只证任意命中,不证回答链完整;008 铁律:coverage 不可单独作为出货依据——本文给出的是更强的离线诊断,但仍须过 e2e 配对门)。
  - 次模装填与 026(LLM 剪枝,模型行为)、M2(删除去重,无预算意识)机制不同——确定性 set-level 选择。但**我们的 answerer 是强模型(条件 iv 大概率不满足)**,e2e 期望值须压低;MMR 单独用反而显著变差是重要反例。
  - 图压缩与次模装填是**部分替代品**(ACE 上装填反而 −2.1 F1)——若已有血缘 span/结构压缩,装填的边际空间更小。

### 1.2 S2G-RAG — 结构化充分性/缺口门控迭代检索
**arXiv:2604.23783**(2026-04, Soochow U)

- **机制**:judge 读 (q, 累积证据 C_t) 输出 `(sufficient: bool, gap_items[])`;gap item 是**通用 schema**:`{category: bridge_entity|attribute|relation|evidence_span|other, target, slot, description}`,映射为下一轮检索查询。句子级 Evidence Extractor 维持紧凑证据上下文(压缩比 4.5–6.4×)。
- **结果**:HotpotQA BM25 +10.6 EM(vs SIM-RAG);judge **误报率仅 6.44%**(说"充分"时 93.6% 真充分),偏保守(31.6% 真充分被误判不充分)。**去 SFT 的裸 backbone + 结构化 schema 也超过全部 baseline**(全量 43.3 → 39.2,仍远超次优 32.7)。
- **对 engram 的含义**:这是 041(confidence-gated iterative retrieval)→ 042(logprob 路由)线的直接学界对应。关键增量:**gap 结构化**让第二轮检索"补缺"而非"再来一批"——这正是 021 IRIS(slot 合并挤掉 round-0 好证据)和 029(自由导航上下文劣化)失败的解法:round-0 chunk 配额固定,gap 导向单轮补检。schema 是领域无关契约,不涉数据集标注。

### 1.3 TAA-k — 尾部感知查询自适应 k
**arXiv:2606.11907**(2026-06, 37 互动娱乐)

- **机制**:排序相似度曲线呈 steep–flat–steep;knee 检测粗定位(O(N))→ 窗口内 GPD 拟合 + Cramér–von Mises 拟合优度找尾部稳定起点,即 relevance→noise 转换点。**训练自由、确定性、跨 retriever(Contriever/BGE/Qwen3)与维度(64–1024d)稳健**,复杂度 O(√N log N·M)。WebQ/2Wiki/MuSi:recall ~94%(oracle ~96%),F1 距 oracle 2–3%。
- **对 engram 的含义**:即 docs/research/retrieval-budget-reduction-directions.md 方向 1 的原型。与 040 NO-GO 不冲突:040 证伪的是"分数 gap-knee"形态,TAA-k 是 GPD 拟合稳定性(模型无关几何性质)。**但它解决的是"该截在哪",不解决 21% 召回尾部**;定位=预算再分配器(强查询早停省 token → 供 P1 加深用),非得分杠杆。

### 1.4 LazyMem — retrieve-broadly, construct-selectively
**arXiv:2607.22690**(2026-07, ECNU; ICML 投稿)

- **机制**:写时零构造(verbatim 存储);查询时 hybrid 检索 n=50(dense+BM25+RRF+cross-encoder)→ 命中消息 ±w 邻域展开、重叠合并成窗口 → 训练的 4B 模型(SFT+GRPO)按窗口并行 KEEP/DROP+压缩。LME 0.85 @ **213 answer-context tokens**(21× 少于最强 baseline);LoCoMo(cat1-4, 314 题)零目标域训练 0.68。
- **对 engram 的含义**:
  - **方向确认**:retrieve-then-construct 优于 construct-then-retrieve(query-agnostic 写时构造有不可逆信息损失)——与 024/025/027/028 的写侧 NO-GO 系列完全一致,给"读侧优先"哲学提供了外部佐证。
  - 其"命中 ±w 邻域展开 + 重叠合并"与 022 血缘 span 回收同构(机制配对已 +10);增量在于**跨 hit 合并去重**(idx 级)——若 022 尚未做跨 span 合并,这是确定性小改。
  - 训练 4B 构造器、依赖 cross-encoder reranker → 均不符合红线,不跟进其实现。

### 1.5 MRAgent — Cue–Tag–Content 主动记忆重构
**arXiv:2606.06036**(2026-06, NUS; ICML 2026)

- **机制**:Cue–Tag–Content 异构图(tag=语义桥),LLM 多轮选择 tag 路径→取内容→prune→继续/停止。LoCoMo J 84.21(Gemini)/88.32(Claude)vs Mem0 68.31;理论证明 active(stateful)检索严格强于 passive(stateless)。多跳 recall 随轮次 +30%。
- **对 engram 的含义**:**不推荐跟进实现**——LLM-in-the-loop 检索导航与 029 agentic 导航(−17.9pp, 4 变体全负)同族,且依赖 Gemini/Claude。可提取的思想:(a) "tag 作为 cue 与 content 之间的显式关联层"可作**写时确定性元数据结构**候选(与 M2 entity 关联已有重叠,边际待证);(b) 其"增加每轮并行预算 K 无法替代加深深度 T"的消融,与 040"80% 是上下文量"互为反面印证——**深度的收益来自证据链组合,不是并列更多**。

### 1.6 Ψ-RAG — 层级抽象树跨文档检索
**arXiv:2605.00529**(2026-05, HKUST-GZ; ICML 2026)

- **机制**:相似度排序→迭代 merging & collapse 建树(确定性,树建 O(n²) 但实测 258s/1.3M tokens)+ 每个抽象节点 LLM 生成摘要(8B 也要 2058s)+ 多粒度 agentic 检索器。超 RAPTOR +25.9% F1、HippoRAG2 +7.4%。
- **对 engram 的含义**:**不推荐跟进**——写侧 LLM 抽象与 025(语义聚类 episode 挤掉逐证据覆盖)/027/028(表示瓶颈)失败模式同族;budget-ablation 也已示同预算下 MemOS tree/graph 占优但机制未转化。唯一可取:确定性建树部分(纯 embedding 相似度)若未来做层级索引,树构建本身便宜,瓶颈在抽象节点生成。

### 1.7 扫描级(未深读,备查)

| 论文 | 方向 | 一句话 |
|---|---|---|
| Know Before You Fetch (2606.29959) | 校准的检索预算分配 | 按 query 难度分配预算,与 TAA-k/S2G 互补 |
| HALT (2608.02009) | verification-aware 停止 | 多跳搜索 agent 的停止问题 |
| TASR (2606.13814) | training-free 自适应停止 | 迭代检索的收敛检测 |
| ScoreGate (2606.14269) | 双分数统计融合选块 | query 复杂度感知的 chunk 选择 |
| AdaGATE (2605.05245) | gap-aware token 高效装配 | multi-hop 证据装配(与 S2G 同族) |
| PACMS (2606.20047) | 次模上下文选择可插拔引擎 | agent 场景的次模选择,与 1.1 同族 |
| AdaGReS (2512.25052) | 冗余感知贪心上下文选择 | 已列 docs 方向 2 |
| AtomMem (2606.19847) | 原子事实记忆系统 | 与 engram ADD-only 原子事实路线同型,可对标 |
| δ-mem (2605.12357) / LeanMem (2608.03463) / MemoryCPT (2608.04843) | 高效长期记忆 | agent 记忆效率侧,低优先 |

---

## 2. 与已排除路径的对照(为什么这三条不是重复证伪)

| 已 NO-GO | 新方向的机制区分 |
|---|---|
| 026 查询期 LLM 剪枝(−4.5pp) | P2 次模装填是**确定性、零模型调用**的 set-level 选择;026 是模型行为 |
| M2 删除去重(误删 gold) | P2 有预算约束 + 覆盖目标,非单纯去重;MMR 单独也证伪(1.1)——须完整四项目标 |
| 021 IRIS 迭代检索(temporal 6 连 NO-GO) | P1 的 gap 是**结构化补缺**,round-0 配额不动;021 是 slot 合并挤压 round-0 |
| 029 agentic 导航(−17.9pp) | P1 是单门控 + 单轮补检,非多轮自由导航;S2G 消融显示 4 轮内收敛 |
| 040 分数 gap-knee | P3 用 GPD 拟合稳定性(统计量),非分数间隙启发式;且定位改为省预算 |
| 025/027/028 写侧构造 | P1/P2/P3 全部读侧/查询时,不碰写侧表示(1.4 方向确认) |
| entity-verify in-sample tuning(已删) | P1 gap schema 是领域无关契约(target/slot 为通用槽位),不看过测试集 |

---

## 3. 推荐优先级与门禁

### P1 · 结构化充分性/缺口门控加深(首选)
- **载体**:041→042 logprob 路由(040 已证触发信号存在:93% 信息不足时表达犹豫;理论上限 91.75% @ 4.8× 预算)。
- **文献增量**(S2G-RAG):gap 结构化 schema → 第二轮"补缺"查询,替代"同查询再来 k 条";未训练 judge + schema 即可超 baseline,起步无需训练。
- **设计要点**:round-0 `--chunks --chunk-quota 12` 配方字节不动;仅当置信信号低于阈值时触发一轮 gap 补检;gap→查询映射是确定性拼接(target+slot)。
- **门禁**:008 铁律(run 内配对 + repeats≥3 + store 复用);LoCoMo 学到的阈值须 LME 零重调验证(042 先例 T034/T039)。

### P2 · 确定性次模证据装填(次选,期望压低)
- **实现**:budgeted greedy,relevance 用现有 RRF 分,coverage/diversity 用现有 embedding;纯 Go。
- **期望管理**:1.1 的条件 (iv)(弱 reader)大概率不满足;期望值=「在不加 k 的前提下让 30 条的信息密度逼近 150 条」,主要打 79% 的"上下文量"份额。
- **顺带**:`answer-in-context` 作为 harness 诊断指标落地(替代 gold_in_pool 的位置,非出货依据)。

### P3 · TAA-k 自适应截断(工具位)
- 纯算法、零重调、跨 embedding 稳健。**不解决召回尾部**;定位为预算再分配器——强查询早停省下的 token 供 P1 加深,组合成"adaptive budget"闭环(与 Know Before You Fetch 同思路)。

### 元数据结构改造(用户授权范围,但文献支持弱)
- 唯一有独立文献支撑的确定性改造:命中 span 的跨条合并去重(1.4,idx 级,与 022 血缘回收互补);tag 关联层(1.5)边际与 M2 重叠且提取质量是 027/028 已证瓶颈,不优先。

---

## 4. 引用

- Bala, *What Survives Into Context: A Diagnostic for Budget-Constrained Multi-Hop RAG and When Submodular Evidence Packing Improves It*, arXiv:2607.00725, 2026.
- Li et al., *S2G-RAG: Structured Sufficiency and Gap Judging for Iterative Retrieval-Augmented QA*, arXiv:2604.23783, 2026.
- Song et al., *Tail-Aware Adaptive-k: Query-Adaptive Context Selection for RAG*, arXiv:2606.11907, 2026.
- Yu et al., *LazyMem: Retrieve Broadly, Construct Selectively for Efficient Long-Term Agent Memory*, arXiv:2607.22690, 2026.
- Ji et al., *Memory is Reconstructed, Not Retrieved: Graph Memory for LLM Agents*, arXiv:2606.06036, 2026.
- Zhao & Yang, *Hierarchical Abstract Tree for Cross-Document RAG (Ψ-RAG)*, arXiv:2605.00529, 2026.
