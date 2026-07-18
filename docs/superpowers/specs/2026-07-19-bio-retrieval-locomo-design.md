# 生物启发检索涨点 —— 顺序可测四枪，闭合 Mem0 gap（设计/方法）

> 状态：方法已定（打法 A），待用户过目。测量由用户运行、控预算；方法与实现由本方交付。
> 分支：`worktree-bio-retrieval-locomo`（隔离，不影响主线）。
> 日期：2026-07-19。

## 1. 目标与成功判据

**目标**：在 local-first 约束内（Go 单二进制 / 无 CGO / 单文件 SQLite / ~10 万条规模），把
engram 在 **LoCoMo 与 LongMemEval 两个基准上推到 85+**，达到能与 Mem0 New Memory
Algorithm（2026-04：LoCoMo 92.5 / LongMemEval 94.4）扳手腕的量级。**不碰 BEAM**
（千万 token 需换 ANN 检索引擎，属架构重写，越出定位）。

**成功判据（诚实、可测）**：
- 每一枪（strike）在**全量 10 段 × N 次重复**评测下，per-category 与 OVERALL 的
  **均值提升高于 ±3-5pp 单跑噪声带**（以 95% CI 不重叠或配对 diff 显著为准）。
- 达不到该判据的机制**诚实砍掉**，不计入涨点（尤其 temporal 在 20 题子集已 10/11，
  需证明其检索侧改造是真涨而非撞噪声）。
- 终态：LoCoMo / LongMemEval OVERALL 均 ≥ 85（可比口径含对抗题按拒答记分）。

**铁律**（沿用 `docs/memory-strategy.md` 评测可靠性纪律）：跨论文绝对分不可比，只抄机制；
引用提升一律取各论文**内部消融的相对幅度**，不取绝对分对比。

## 2. 诚实的 gap 分解：+18pp 不是一枪，是四枪

当前 ~74.7 → Mem0 92.5 的差距拆成 4 个**相互独立、可分别测量、按 ROI 排序**的杠杆。
「一击必中」= 每枪打出高于噪声带的可测增益，叠起来闭合 gap。MemOS 73→88 用了一年
迭代检索层，故本方案是**顺序增量**，不是单枪 +18pp 的幻觉。

## 3. 测量契约（本方交付基础设施，用户运行）

这是第一优先级基础设施——**没有可信测量，「一击必中」无从判断中没中**。

- **多跑 harness 升级**：`cmd/locomo-bench` 增加 `--repeats N`，全量 10 段 × N 次，
  输出 per-category 与 OVERALL 的**均值 ± 95% CI**。
- **同题配对 diff 报告**：固定题集，baseline vs treatment 逐题对比，报告翻转题数
  与配对显著性（专治「答案生成随机性主导噪声」——两跑间 59.5% 预测文本不同）。
- **LongMemEval adapter**：新增数据集加载与题型映射，复用同一答题/judge/记分链。
- 锚点：aggregator/catStat `cmd/locomo-bench/main.go:618`、reportDelta `:677`、
  数据集加载 `cmd/locomo-bench/dataset.go`。

## 4. 四枪（打法 A：逐枪打、每枪多跑测完再叠下一枪）

### Strike 0 —— 模型 regime 对齐（免费，先打）
**假设**：Mem0 92.5 跑在强模型栈，engram 74.7 跑在 deepseek-v4-pro / gpt-5.6-luna；
按文档实测「换前沿模型 +6-10pp」，部分 gap 是模型而非架构。
**动作**：用 **gpt-5.6-sol** 作答题（可选抽取）模型，跑校准 baseline，得到**真架构 gap**。
**落地**：已有 `LOCOMO_*` / `EXTRACT_MODEL` / `LOCOMO_PROVIDER` 环境变量通道
（`cmd/locomo-bench/main.go:580` `buildBenchEmbeddingClient` 同款模式），基本零改动，
主要是配置 + 多跑。**成本：一次多跑 baseline，用户控。**
**归因价值**：后续三枪的涨点都以此校准 baseline 为基线，避免「其实是模型涨的」污染。

### Strike 1 —— multi-hop 66% 天花板：实体平表 → 联想图（架构最高 ROI）
**假设**：multi-hop 卡在实体是**平表精确匹配**（`EntityMatchCounts` 逐 token 等值，
`memory/entrystore.go:319`），无法遍历多跳互补事实——正是 vs MemOS 的结构差距。
**机制（纯 Go，按 ROI 内部排序）**：
1. **query-to-triple 链接**（先上，最便宜）：整句 query 匹配实体/三元组而非先抽实体再匹配
   （HippoRAG2 消融 recall@5 +12.5%）。改 `entityRanks` `memory/retriever.go:308` 的匹配路径。
2. **共现边表**：migration v3 新增 `memory_entity_edges(entity_a, entity_b, weight)`，
   抽取落库时按同 entry 共现连边（`memory/pipeline/pipeline.go:159` 附近）。
3. **激活扩散**：query 实体为种子做 **Personalized PageRank**（稀疏矩阵幂迭代，纯 Go）
   或 EcphoryRAG **cue 质心游走 depth-2 + 对原 query 重排**（防 topic drift）。
   注入 retriever 作第 4/5 路信号，进 `fuseRRF` `memory/retriever.go:347`；
   现有 1-hop `entityNeighbors` `:194` 是其严格子集，可升级。
4. **node specificity + 同义边**（零 LLM 成本）：种子概率 ×IDF（新增 `entity_doc_freq`）；
   余弦>0.8 自动连同义边（复用 `embedding.Cosine` + curation 近重复聚类）。
**目标**：multi-hop 66 → 80+。**Kill 判据**：多跑下 multi-hop 提升 < 噪声带即回退该子机制。

### Strike 2 —— temporal：prompt → 检索侧结构化过滤
**假设**：temporal 靠答题 prompt 救不动，正确的层在检索侧结构化过滤。
**机制**：
- `event_date` 升级为 **start/end 范围**（migration v3 加列）；"recently/last month" 对
  会话时间戳做多分辨率归一化（MemOS「查询侧时间窗过滤」完整配方）。
- **event 词汇别名喂 FTS5**（2-4 个换词别名，近乎白送召回；FTS 镜像 `store/migrations.go:47`）。
- **T_score 作 RRF 第 4 路**（基于 event_date 指数衰减）；复用 curation 已有衰减曲线
  `memory/curation/scorer.go:56/76/87`。
**⚠️ 诚实门**：20 题子集 temporal 已 10/11，**这一枪必须多跑证明真涨**，否则砍掉。

### Strike 3 —— 对抗题 27% 编造：拒答校准 + 冲突消解
**假设**：对抗题编造伤「可比口径」分；现有「两级 IDK 重试」救可答题却害对抗题。
**机制**：
- **Abstain-R1**：答题 prompt 换 **1:4 ICL 拒答+澄清契约**（1 反例 + 4 正例 few-shot），
  奖励 = 0.3 拒答 + 0.7 澄清正确，**不对称误拒惩罚**（误拒可答题重罚）。替换
  `retryWithRewrite`/`retryWithWiderNet`（`cmd/locomo-bench/main.go:506/549`）。
- **冲突消解四分类**：curation judge 从 keep/evict/merge 扩成
  Compatible/Contradictory/Subsumes/Subsumed（`memory/curation/judge.go:88`），
  矛盾时新压旧（降旧条目强度），包含时合并——对 ADD-only 抽取是关键补丁。
**目标**：对抗题编造率 27% → 显著下降，可比口径 OVERALL 上移。

## 5. 顺序与测量流

```
Strike 0 (校准 baseline) ──多跑──► 真架构 gap
   └─► Strike 1 (图化) ──多跑配对 diff──► 达标? 叠 / 未达标? 回退子机制
          └─► Strike 2 (时间) ──多跑──► 达标? 叠 / 砍
                 └─► Strike 3 (拒答+冲突) ──多跑（含 --adversarial）──► 终态
```

每枪：本方实现最小改动 → 用户跑多跑评测 → 配对 diff 判定是否高于噪声 → 达标叠加，
否则回退并记录（负结果也是论文弹药）。

## 6. 约束与非目标

- **约束**：Go 单二进制、无 CGO、纯 Go SQLite（`modernc.org/sqlite`）、余弦全扫适合 ~10 万条；
  所有新机制不得引入 CGO / 第三方向量库 / 外部服务依赖。
- **非目标**：BEAM 千万 token；ANN 检索引擎替换（`embedding.TopKCosine` `embedding/vector.go:61`
  仅在真正上规模时才碰，本轮不动）；Ebbinghaus 时间衰减剪枝（单用户持久库上可能误删
  后面要考的事实，**先只做滞回防抖，衰减做消融验证再说**）。

## 7. 交付物

- `cmd/locomo-bench` 多跑 + CI + 配对 diff + LongMemEval adapter（Strike 0 前置）。
- 四枪各自的最小改动 PR/commit，独立可测、可回退。
- 每枪的多跑结果记录（均值 ± CI、配对 diff）写入 `docs/`，达标/砍掉均如实记。
