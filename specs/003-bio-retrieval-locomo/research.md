# Research: 生物启发检索涨点（feature 003）

> Phase 0 产出。机制候选来自 `docs/memory-strategy.md` 附二（两轮独立生物启发调研，
> 已收敛到「图+时间+强度/冲突」）与附一（MemOS 拆解）。本文把候选收敛为**可实现决策**，
> 每项按 Decision / Rationale / Alternatives 记录。跨论文只抄机制不比绝对分（评测
> 可靠性纪律）。

## D1. 多跑统计与配对显著性方法

**Decision**: 运行层 + 题目层双报告：
- 运行层：每配置 N 次独立重复，per-category 与 OVERALL 的准确率均值 ± 95% CI
  （t 分布，自由度 N-1）；
- 题目层（配对 diff）：同题集下 A/B 两配置按 run 配对，聚合每题多数表决后做
  **McNemar 检验**（b/c 不一致对），同时输出逐题翻转清单（A对B错 / A错B对）；
- 判定规则：「高于噪声带」= McNemar p<0.05 **或** 两配置 OVERALL 95% CI 不重叠
  （二者其一即可，报告中都打印）。

**Rationale**: 答案生成随机性主导噪声（两跑 59.5% 预测文本不同），题目层配对消掉
题目难度方差，比只看运行层均值灵敏；McNemar 是配对二元结果的标准检验，标准库可实现
（卡方近似 + 小样本二项精确），零新依赖。

**Alternatives considered**: 仅运行层 t 检验（灵敏度低，浪费配对结构）；bootstrap
CI（实现更重，收益边际）；置换检验（同上）。

## D2. 多跳联想检索机制（Strike 1）

**Decision**: 主方案 = **共现边表 + cue 质心游走 depth-2 + 对原 query 重排**
（EcphoryRAG 2510.08958 配方），组成第 4 路 RRF 信号：
1. 写入侧：同一 entry 的实体两两连边（`memory_entity_edges`，weight=共现次数）；
   embedding 余弦 >0.8 的实体对补同义边（离线批量，复用现有 `memory_embeddings`，
   零 LLM 成本；HippoRAG1 配方）。
2. 查询侧：query 抽 cue 实体 → 种子权重乘 node specificity（IDF=1/实体 entry 频次，
   查询时 `GROUP BY entity_norm` 计算，不加列）→ 沿边游走 depth≤2 收集邻居实体的
   entry → **用原 query 的 embedding 对收集结果重排**（防 topic drift，FR-006）→
   有序名单进 `fuseRRF`。
3. query-to-entity 全句匹配：现 `EntityQueryTokens` 逐 token 精确等值之外，增加
   整句对 `entity_raw` 的 FTS/子串匹配路径（HippoRAG2 query-to-triple 的降配版，
   我们无三元组，用实体表近似；消融显示该类链接 recall 提升显著）。

**备选（仅当主方案 multi-hop 未达噪声带门槛时启用）**: Personalized PageRank——
种子实体上稀疏幂迭代（纯 Go），是 1-hop/游走的严格超集，但实现与调参更重。

**Rationale**: EcphoryRAG 在同类图方案中索引成本最低（比 HippoRAG2 低 3.3×）、
无需 LLM 建图，最贴单机离线约束；「对原 query 重排」直接回应 FR-006（非多跳类别
不得回退）。共现边是抽取已产出信息的纯落库增量，抽取调用数不变。

**Alternatives considered**: 完整知识图谱（MemOS MemOperator 式实体/时序/依赖边）——
建图需额外 LLM 调用且违背预算门禁的成本精神；GraphRAG 社区摘要——离线摘要成本高，
非单用户对话记忆场景的必需。

## D3. 检索侧时间结构化（Strike 2）

**Decision**:
1. 写入侧：抽取 prompt 扩展两个字段——事件时间**范围**（start/end ISO 8601，
   以会话时间戳为锚归一化"上周/去年"）与 **2-4 个词汇别名**（"bought Fitbit"→
   "got a step counter"），同一次抽取调用顺带产出（调用数不变）。别名入
   `memory_event_aliases` 表并进 FTS（BM25 白送召回，Chronos 配方）。
2. 查询侧：**规则式时间意图解析器**（纯 Go，无 LLM 调用）——识别
   "last month / in May / recently / 去年" 等模式，以数据集提供的会话/提问时间戳
   为锚生成查询时间窗；命中时间意图的查询在 rerank 阶段对候选乘
   **T_score = exp(-|event−窗中心|/τ)**（SynapticRAG 检索侧时间分数），并可选
   硬时间窗过滤（flag 控制，默认软乘）。
3. τ 固定单值起步（固定 τ 消融 −10.54% 是相对其自适应版；我们先测固定值是否已够，
   durability→τ 映射论文未做过，属外推，不在本轮默认启用）。

**Rationale**: temporal 已证明 prompt 侧救不动，检索侧结构化是 MemOS/Chronos 共同
指向的正确层；规则解析器保 single-pass 与离线（宪法 I + FR-013）；LoCoMo/LME 的
时间表达模式集中（相对词+月份+年份），规则覆盖率足够，先测再谈上 LLM 解析。

**Alternatives considered**: LLM 查询理解（MemReader 式 MemoryCall）——每查询多一次
LLM 调用，违背预算门禁与离线默认；把时间只渲染给答题模型（现状）——已证不足；
DTW 时间序列相关性（SynapticRAG 完整版）——复杂度不匹配收益，指数衰减先行。

## D4. 拒答校准 + 冲突消解（Strike 3）

**Decision**:
1. **拒答**（bench 侧，评测口径改动，独立提交）：答题 prompt 换 **Abstain-R1
   1:4 ICL 契约**——1 个拒答反例 + 4 个正常回答正例的 few-shot，拒答时须指出缺失
   信息；**移除两级 IDK 重试**（`retryWithRewrite`/`retryWithWiderNet`），它在可答题
   净赚 12 题但在对抗题造成 121/122 自信编造，净损可比口径。不对称性由 few-shot
   比例（1:4 偏保守作答）与 prompt 措辞承载，不引入 RL。
2. **冲突消解**（引擎侧）：curation judge 从 keep/evict/merge 扩为
   **Compatible / Contradictory / Subsumes / Subsumed** 四分类（FadeMem 配方，
   去掉该机制 −22.4%）；Contradictory→新压旧：旧条目写 `superseded_by`（非破坏，
   Q2 裁决），检索默认降权（乘固定惩罚系数）并在超过阈值时过滤；Subsumes/Subsumed→
   走既有 merge 路径。时间类查询不过滤 superseded 条目（旧值正是历史问题的答案）。

**Rationale**: 对抗题 27% 编造直接压可比口径分；Abstain-R1 实证 few-shot 即触发
校准拒答（3B 模型 9.4%→59.2%），无需训练，成本为零；四分类是 ADD-only 抽取的
关键补丁，且与 MemOS 生命周期同构、有独立消融背书。

**Alternatives considered**: RL 训练拒答头（Abstain-R1 完整版）——需训练基础设施，
超出本轮；置信度阈值拒答（logprob）——中转站端点未必回传 logprob，不可依赖；
FadeMem 强度阈值拒答——FadeMem 原文无拒答机制，属外推，弃。

## D5. LongMemEval_S 适配（US1 一部分）

**Decision**: `cmd/locomo-bench` 加 `--dataset-format longmemeval` 加载器：
- 解析 LME_S 的 haystack 会话（带时间戳）→ 走同一抽取/建库管线；
- 题型映射：single-session-user / single-session-assistant / multi-session →
  对应 single/multi-hop 类别桶；temporal-reasoning → temporal 桶；
  knowledge-update → 冲突消解敏感桶（单列报告）；abstention → 对抗桶
  （按拒答记分，与 LoCoMo `--adversarial` 同口径）；preference → 独立桶单列；
- 判分复用同一 LLM judge 与记分链（FR-003 口径可比）。

**Rationale**: 复用整条链改动最小、口径天然可比；题型→桶映射保留 per-category
CI 报告能力，knowledge-update 桶恰好是 Strike 3 冲突消解的直接测点。

**Alternatives considered**: 独立新 cmd（代码重复、口径漂移风险）；官方评测脚本
（Python，引入第二套 judge 口径，破坏可比性——留作对外报分时的交叉校验，非本轮门禁）。

## D6. 费用账（FR-014）

**Decision**: bench 内建 usage 捕获 + 价目表：
- 每次 LLM/embedding 调用从 API 响应捕获 prompt/completion token 用量，按
  调用角色（抽取/答题/判分/embedding）分桶累计；
- 价目表经环境变量 `LOCOMO_PRICE_TABLE`（JSON：model→{in,out} 单价）注入；
- `--estimate` 模式：不真跑，按题数 × 历史均值 token（首跑用保守默认值）输出
  预估费用并退出；正式跑结束在 run-dir 落 `cost.json`（预估 vs 实际、分桶明细），
  报告尾部打印。

**Rationale**: 用户明确要求「跑前算账、跑后记账」；usage 数据 API 响应自带，
零额外调用；价目表外置适配中转站计价差异。

**Alternatives considered**: 靠中转站后台账单（滞后、无法分桶归因到枪/配置）；
tokenizer 本地估算（模型不一、误差大，仅用于 --estimate 的粗估兜底）。

## D7. 机制开关与归因纪律（FR-011 / 宪法 IV）

**Decision**: 每机制一个独立 bench flag（`--assoc`、`--temporal-score`、
`--abstain-prompt`、`--conflict-resolution`，默认全关），引擎侧经 `Retriever`/
`Pipeline`/`Worker` 的选项结构透传；评测口径改动（prompt/记分）与算法改动
（引擎机制）**分开提交**；每枪合并前跑可比口径多跑并把结果记入
`specs/003-bio-retrieval-locomo/eval-log.md`（预估/实际费用一并留档）。

**Rationale**: 独立开关是配对 diff 的前提；分开提交保证涨点可归因（宪法 IV 明文）。

**Alternatives considered**: 配置文件驱动（flag 已够、少一层间接）；分支隔离每机制
（合并地狱，flag 更轻）。

## 未解决项

无。所有 Technical Context 条目已定；PPR 为条件备选（D2），不属未知。
