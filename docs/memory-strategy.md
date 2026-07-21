# 记忆子系统:产品与论文战略(2026-07 决策记录)

> 🧭 **状态**: 活跃 · **战略正本** —— 记忆子系统产品方向、论文线与涨点 backlog
> (生物启发检索 P0/P1/P2)的**权威出处**;其他文档的 backlog/竞品拆解均回链至此。
>
> 背景:`memory-hybrid-retrieval-locomo` change 完成五轮消融调优,LoCoMo 从
> 41.2%(hybrid 初版)打到 ~74.7%(可答题两跑均值)/ 72.6%(Mem0 可比口径,
> 含 446 对抗题拒答记分)。该五轮消融的原始 tasks 记录来自**已归档的 openspec
> change `memory-hybrid-retrieval-locomo`**(不在当前 spec-kit `specs/` 树内),
> 结论摘要见 CLAUDE.md 记忆子系统一节。本文记录在此基础上的两项战略评估与决策。

## 决策一:产品方向 —— 不做云 SaaS,全力做本地开源记忆产品

### 评估结论

正面对标 Mem0 的通用记忆云服务**不做**:

- 分数差距真实存在(可比口径 ~74 vs 92.5,后者含专有优化);
- Mem0 / Zep / Letta / Supermemory 已是红海,且 Mem0 开源版免费;
- 云 SaaS 还需多租户、认证计费、SOC2、SLA 等 2-3 个月外壳工程,
  而胜负不取决于外壳。

**决定的方向:本地优先(local-first)开源记忆产品**——与云厂商打错位仗:

- 单二进制 + 单文件 SQLite 存储 + 无 CGO,零依赖部署;
- 可完全离线:embedding 走本地 sidecar(fastembed / Ollama),
  LLM 抽取/curation 可指向任意本地或私有端点;
- 目标场景:私有化部署、数据合规敏感客户(金融/医疗/政企)、
  嵌入式场景(桌面 Agent、边缘设备);
- 现有资产已覆盖核心:三路混合检索(语义+BM25+实体 RRF)、ADD-only
  抽取流水线、curation 后台(跨进程 leader lease)、无 embedding 时的
  FTS 优雅降级、实测调优过的检索配置、可持续回归的评测 harness。

### 国内竞品核查(2026-07,定位据此修订)

决策落笔后的市场核查发现,国产阵营比预期拥挤,两家直接冲击原定位:

| 玩家 | 要点 | 对我们的冲击 |
|------|------|--------------|
| **MemOS**(记忆张量 MemTensor,上交大系,开源) | MemCube 分层架构;**LoCoMo 88.83 / LongMemEval 89.20**(2026),接近 Mem0 闭源水平;落地招商证券/中行/电信;另发布 OmniMemEval(14 个商业记忆产品统一评测) | 国产开源分数制高点已被占;我们 ~74 在国内开源竞品面前不占优 |
| **腾讯云 TencentDB Agent Memory**(2026-04,MIT 开源) | **本地 SQLite、零外部依赖、一条命令部署、免费商用**;OpenClaw 集成口径准确率 76.10%;企业 Pro 版做金融/政务合规 | "Agent 记忆的 SQLite"口号与私有化合规定位被大厂先占 |
| Memobase / OpenViking / Cognee 等 | 用户画像式记忆(LoCoMo 72.01)、虚拟文件系统分层记忆(省 80-96% token)等 | 第二梯队,证明赛道升温 |

生态共识:中国生态此前"重 RAG 轻 Memory",记忆层刚成为关键分层,
格局未定但窗口在收窄。

**定位修订**:"通用本地记忆库"这条路已没有差异化空间(腾讯占位)。
收窄为——

> **"内建一等记忆的本地 Agent 服务器"**:记忆不是外挂库,而是与
> 会话管理、权限系统、curation 后台、工具生态深度一体的闭环;
> Go 单二进制交付;完全离线可运行。卖的是"开箱即得带记忆的
> 本地 Agent 基座",不是"再装一个记忆组件"。

### 待办(后续单独立项,不在本文展开)

- 强化"agent server + 记忆一体"的产品叙事与文档,而非抽离通用记忆库;
- 对外 API 面(考虑 Mem0 兼容 API 以降低迁移成本);
- 规模边界如实声明:Go 侧余弦扫描适合单用户 ~10 万条级记忆,
  不承诺 BEAM 式千万 token 语料;
- 校准拒答(对抗题 27% 编造率)是产品质量的下一个真实杠杆;
- 分数追赶参考 MemOS 公开的评测代码与数据集(已开源至 Hugging Face)。

## 决策二:论文 —— 要发,方向定为评测可靠性分析文

### 评估结论

系统本身(RRF/FTS5/embedding/抽取/verbatim chunk 的组合)是高质量工程
复现,**不足以支撑顶会方法/系统论文**。但五轮消融产出了 4 个经 alphaxiv
占坑核查、文献中查不到的实证发现:

| # | 贡献 | 占坑核查结果 | 评级 |
|---|------|--------------|------|
| 1 | **评测可靠性审计**:LoCoMo 单跑噪声 ±3-5pp/类别;噪声分解为答案生成随机性主导(两跑间 59.5% 预测文本不同)vs LLM judge 仅 2.4% 翻转;"每类保留最好"的 journal 拼接会系统性虚高(78.4 拼接 vs 72.6 干净重跑);各家发布分数因协议差异(私有 judge、对抗题记分口径)不可比 | 通用 agentic 随机性(arXiv 2602.07150)与 judge 可靠性(多篇)已有,但记忆基准的噪声分解 + 拼接偏差陷阱 + 协议可比性对照**没人做全** | 主线 |
| 2 | **负结果边界条件**:pairwise cross-encoder rerank 与 recall 导向 listwise LLM filter 在强一段召回下均为净负——"二段选择只在一段召回弱时有效",与 MemReranker(2605.06132)、EMem 等正结果形成直接对话 | rerank-for-memory 正结果扎堆,负边界无人立论 | 弹药 |
| 3 | 异构联合索引(verbatim chunks ∪ 抽取事实)的 RRF 事实偏置诊断与 per-kind quota 修复;per-category 检索预算的尖峰宽度曲线(multi-hop 峰 k=150、single-hop 峰 quota=30) | An 2026(2601.00821)证明 union store 有效但未报告配额必要性 | 弹药 |
| 4 | 反 IDK 机制与拒答校准的定量张力:两级 IDK 重试在可答题净赚 12 题,在对抗题上导致 122 错中 121 个自信编造 | 校准文献多,记忆 QA 场景该组数字缺失 | 弹药 |

**论文形态**:分析型论文《长期对话记忆基准的评测可靠性》(暂名),
核心论点:*该领域大量已发布的 <5pp 改进可能在单跑噪声以内,
且跨论文分数因协议差异不可比*。

**成稿所需补实验**(现有数据不够严谨):

- 2-3 个 answer 模型 × 每配置 ≥5 次重复跑;
- 至少增加 LongMemEval 一个基准交叉验证;
- 预算估计 ¥500-1500,工期 2-4 周。

**投稿定位**:NeurIPS/ICLR workshop 或主会 analysis / datasets & benchmarks
track;不冲主会方法论文。

**竞态提醒(2026-07)**:MemOS 团队已发布 OmniMemEval(14 个商业记忆
产品的统一评测榜单)——他们回答"谁分高",我们质疑"单跑分数本身
是否可信"(噪声分解/拼接偏差/协议不可比),角度互补且他们是必引
对象;但评测方法论方向已在升温,**成稿宜快**。

**当前状态:方向已定,执行计划后续再议。**

## 附:MemOS 88.83 分技术拆解(2026-07 调研)

> 来源:论文 arXiv 2507.03724(v4)、GitHub MemTensor/MemOS、
> v1.0.3 发布说明及中文技术解读交叉核对。

### 关键时间线:88.83 不是一次做到的

| 时间 | 版本 | LoCoMo | 备注 |
|------|------|--------|------|
| 2025-07 | 论文版(memos-0630) | 73.31 | backbone = GPT-4o-mini,所有 baseline 同底座 |
| 2025-12 | v1.0.3 | 75.80 | token 消耗 1589,远低于 Zep 的 2701 |
| 2026-07 | 最新版 | 88.83 | 自家 OmniMemEval harness,backbone 未明示 |

**论文版 73.31(GPT-4o-mini)≈ 我们当前 ~74.7(deepseek-v4-pro)。**
88.83 是此后一年检索层持续迭代的结果,不是架构一步到位。

### 扛分机制拆解

1. **查询理解层(MemReader)——与我们最大的结构性差异**:每个查询
   先解析成结构化 MemoryCall(时间范围、主题实体、任务意图、上下文
   锚点),temporal 题在**检索侧**按时间窗结构化过滤后再语义检索;
   我们只是把 `[event:]` 渲染给答题模型自己看。
2. **图组织 + 多路混合检索(MemOperator)**:记忆为知识图谱节点
   (实体/时序/依赖边)+ 标签 + 分层;检索 = 结构化过滤 ⊕ 向量 ⊕
   BM25 ⊕ 图召回。多跳题走图遍历取互补事实——正是我们 multi-hop
   66% 卡住的层:我们的 entity 信号是平表精确匹配,他们是可遍历的边。
3. **任务对齐路由**:查询分解为 topic–concept–fact 三层,
   MemoryPathResolver 决定搜什么/在哪搜/什么顺序。
4. **生命周期管理**:五状态(Generated→Activated→Merged→Archived→
   Expired),与我们 curation 引擎同思路,但接入了检索优先级。
5. KV 激活记忆/参数记忆/Next-Scene 预加载:主要买延迟(TTFT -91%),
   对 LoCoMo 分数贡献有限,是产品卖点。
6. **73.31→88.83 的 +15.5pp 来自**(官方 changelog):明文检索/BM25/
   图召回全面升级、混合检索策略重写、偏好记忆——即一年时间在自家
   评测 harness 上持续调优检索层。

### 用我们的评测可靠性视角看

- 88.83 出自他们自己的 OmniMemEval(评测代码已开源,好于 Mem0 的
  黑箱),但 backbone 是否仍为 4o-mini 未明示——按我们实测
  "换前沿模型 +6-10pp"的经验,架构净贡献需打折;
- 73→88 的迭代模式与我们 69→78.4(拼接)同构,存在噪声收割风险——
  **这正是我们论文的核心论据,MemOS 是最好的案例研究对象之一**。

### 对我们可抄的作业(按 ROI 排序)

1. **查询侧时间窗过滤**(MemReader 最小版):temporal 已证明 prompt
   救不动,检索侧结构化过滤才是正确的层;
2. **实体图化**:把 `memory_entities` 从平表升级为可 1-hop 遍历的边
   (rerank 阶段做过 1-hop 邻居,但未进主检索路径)——直击 multi-hop;
3. **偏好记忆借鉴要谨慎**:我们 v23 已实测"灌观点条目进同一个池子"
   是负收益(single-hop -25 题);MemOS 做的是独立记忆类型 + 独立
   检索通道——这个区别恰好解释了我们为什么失败。

## 两条线的关系

论文为开源产品做技术信誉背书(Mem0 的既有路径);评测 harness
(`cmd/locomo-bench`)同时是论文的实验基础设施和产品的回归测试,
一份投入两份产出。

## 附二:生物启发检索机制调研(2026-07,两轮独立分析 + 交叉验证)

> 目标:从生物学/脑科学/果蝇记忆方向的 alphaxiv 论文里,找能在 LoCoMo 上
> 真涨分、且契合 Go 单二进制 + 单文件 SQLite + 离线约束的机制。两轮独立分析
> (不同论文入口)**收敛到同一个方向**,交叉印证了 MemOS "可抄作业"的判断:
> **记忆图化 + 检索侧时间过滤 + 强度/遗忘/冲突消解**。论文已存入 alphaxiv 库
> 「Memory 涨点-生物启发」。

### 诚实前提(沿用决策一的评测可靠性纪律)

- **跨论文绝对分不可比,只抄机制**:FadeMem LoCoMo multi-hop F1 仅 29.43
  (Mem0 28.37,差 +1.06 **落在我们实测 ±3-5pp 噪声带内**);SynapticRAG 用
  自建 SMRCs + PerLTQA 数据集、只测检索准确率不测端到端 QA;HippoRAG2 用
  MuSiQue/2Wiki/HotpotQA。引用的提升幅度一律取**各论文内部消融的相对降幅**
  (这可信),不取绝对分对比。
- **核过的消融数字(可信)**:SynapticRAG 固定 τ −10.54%(Table 3);
  FadeMem 去 fusion −53.7%、去冲突消解 −22.4%(Fig 2 / 正文,均基于
  multi-hop F1 29.43 基线)。
- **两处"论文没做、被写成落地建议"的外推(需自证,勿当结论)**:
  (a) "SynapticRAG τ ← durability 映射"是工程嫁接——论文 τ 实际按**命中间隔
  Δt** 更新(LIF 膜时间常数),与 evergreen/volatile 无关;且 τ_scale 尖峰后
  断崖式崩(与我们"尖峰宽度曲线"同病)。(b) "FadeMem 强度阈值触发拒答"——
  FadeMem **无拒答机制**,只做 v<ε_prune 剪枝;拒答校准的有实证杠杆是
  Abstain-R1(见下),不是 FadeMem。
- **对"时间衰减能涨分"打问号**:LoCoMo 是单用户持久库、可能问几个月前的事,
  FadeMem 衰减以天计(半衰期 5–11 天),**盲目上会误删后面要考的事实**。
  FadeMem 在 LoCoMo 真正扛分的是**冲突消解 + fusion**(与 MemOS 生命周期状态
  同构),不是遗忘曲线本身。

### 短板 1:multi-hop 卡 66% —— 实体平表 → 联想图(最高 ROI)

| 机制 | 技巧 | 落地到 workhorse-agent | 来源 |
|------|------|------------------------|------|
| **轻量实体图 + cue 游走** | engram=实体+metadata+源 chunk 指针;共现连边;query 抽 cue → 加权质心 embedding 逐跳游走(depth=2)→ **对原 query 重排**防 topic drift | `memory_entities` 已有实体→entry 半张表,补一张共现边表 + retriever 增加质心游走 stage,并回 RRF 前重排 | **EcphoryRAG** 2510.08958(EM 0.475>HippoRAG2 0.390,索引 token 低 3.3×,更贴单机约束) |
| **Personalized PageRank** | query 实体为种子在实体图跑一次 PPR,单步完成多跳;纯 Go 可实现(稀疏矩阵幂迭代);比迭代 RAG 便宜 10-30× | 备选更重方案;我们 rerank 已做 1-hop 邻居但没进主检索,PPR 是其严格超集 | **HippoRAG/HippoRAG2** 2405.14831 / 2502.14802 |
| **query-to-triple 链接** | 用整句 query 匹配三元组(而非抽实体再匹配),recall@5 **+12.5%**;passage/entry 节点也进种子集,reset 权重 ×0.05 | 最便宜的一刀,改 retriever 的实体匹配路径即可 | HippoRAG2 消融 Table 4/5 |
| **node specificity / 同义边** | 种子概率乘 IDF 式 s_i=1/实体文档频次;两实体 embedding 余弦>0.8 自动连边 | 加 `entity_doc_freq` 列;`memory_embeddings` 已有,零 LLM 成本离线建同义边,复用 curation 近重复聚类 | HippoRAG1 |

### 短板 2:temporal prompt 救不动 —— 检索侧时间结构化(最高 ROI)

| 机制 | 技巧 | 落地 | 来源 |
|------|------|------|------|
| **时间"范围"而非点** | event 存 start/end ISO 8601;"recently/last month" 以对话时间戳为锚做多分辨率归一化 | 升级 `event_date` 列为范围;这是 MemOS "查询侧时间窗过滤"的完整配方 | **Chronos** 2603.16862(去 event 索引直接砍半准确率) |
| **词汇别名喂 FTS** | 每 event 生成 2-4 个换词别名("bought Fitbit"→"got a step counter") | 直接进 FTS5 BM25,几乎白送召回,成本极低 | Chronos |
| **检索侧时间分数 T_score** | 基于 event_date 用指数衰减/DTW 算时间相关性,与余弦 C_score **相乘**成 P_score | RRF 加第 4 路时间信号,或 rerank 阶段乘 T_score | **SynapticRAG** 2410.13553 |
| **动态时间常数 τ** | 近期问题小 τ(快衰)、长期偏好大 τ(慢衰);固定 τ 消融 −10.54% | ⚠️ 桥接:复用 durability(evergreen→大τ / volatile→小τ)——**论文未做此映射,需自证**;注意 τ 尖峰脆弱 | SynapticRAG(τ 实按命中间隔更新) |

### 短板 3:curation 质量 + 冲突污染 + 写入信噪比

| 机制 | 技巧 | 落地 | 来源 |
|------|------|------|------|
| **冲突消解四分类** | Compatible/Contradictory/Subsumes/Subsumed:矛盾时新压旧(降旧条目强度),包含时合并;去掉 −22.4% | LLM judge 已是 keep/evict/merge,扩成 4 类——**对 ADD-only 抽取是关键补丁**,否则过时信息污染检索;与 MemOS 生命周期同构 | **FadeMem** 2601.18642 |
| **智能 fusion** | 时间-语义聚类后 LLM 合并、保留时序/因果;去掉 −53.7%(单点 ROI 冠军) | curation 的 `memory_merge` 是最值得加投入处;强度取簇内 max + 方差奖励 | FadeMem |
| **多巴胺 RPE 写入门** | 写入=Utility×(Surprise+β),硬阈值把 transient 闲聊归零;Surprise 用 z-score 归一化解决 embedding 各向异性 | ADD 抽取加写入门,降噪即提 multi-hop(D-MEM 42.7 vs A-MEM 27.0) | **D-MEM** 2603.14597 |
| **Shadow Buffer** | SKIP 的原文进 O(1) FIFO,答题低置信时回退读原文 | 专治对抗题"我刚是不是提过X";我们 verbatim-chunk 库已是半个 shadow buffer | D-MEM |
| **Ebbinghaus 衰减(谨慎)** | v=exp(−λ·age^β),λ 按 hit_count 自适应、β 按 durability 分层;滞回 θ_promote>θ_demote 防抖 | ⚠️ 单用户持久库上**衰减可能误删后面要考的事实**,LoCoMo 上贡献疑为噪声;先只上滞回防抖,衰减做实验验证再说 | FadeMem |

### 短板 4:对抗题 27% 编造 —— 校准拒答(有实证,可立刻做)

| 机制 | 技巧 | 落地 | 来源 |
|------|------|------|------|
| **拒答+澄清定式** | 不只说"我不知道",而是**指出缺了什么**;奖励=0.3 拒答+0.7 澄清正确;可答题误拒重罚(-1>-0.5,不对称) | 换答题 prompt 的输出契约;`--adversarial` 口径正好量 | **Abstain-R1** 2604.17073 |
| **1:4 ICL 触发** | 1 反例+4 正例 few-shot 即触发拒答(3B U-Ref 9.4%→59.2%),不必 RL | 先上 few-shot 版 A/B,替换现有"两级 IDK 重试"(它救可答题却害对抗题) | Abstain-R1(校准拒答**不随模型变大自动出现**) |

### 果蝇 FlyHash / BioHash:效率杠杆,非分数杠杆(诚实定位)

FlyHash=稀疏扩展+k-WTA→稀疏高维二进制 hash;BioHash 让投影可学习。**对我们
~10 万条、Go 余弦扫描已够快的规模,它不是 LoCoMo 涨分点**(瓶颈在图/时间结构,
不在 ANN 速度)。两个真能用的边角:① 汉明距离做**廉价近重复/dedup**,可加速
curation 的 char-trigram Jaccard 聚类,或当 D-MEM 写入门的 **Surprise 廉价代理**
(不用 LLM/不用全扫);② 未来上规模时的检索引擎候选。来源:2001.04907、
FlyVec 2101.06887、Fly-CL 2510.16877。

### 统一执行优先级

| 优先级 | 动作 | 短板 | 来源 |
|--------|------|------|------|
| **P0** | `memory_entities` 加共现边 + depth-2 质心游走 + 原 query 重排;query-to-triple 链接 | multi-hop | EcphoryRAG + HippoRAG2 |
| **P0** | `event_date` 升级为时间范围 + event 词汇别名喂 FTS + T_score 进 RRF 第 4 路 | temporal | Chronos + SynapticRAG |
| **P1** | ADD 抽取加冲突消解四分类 + D-MEM 写入门 + Shadow Buffer | 冲突/噪声/对抗 | FadeMem + D-MEM |
| **P1** | 答题 prompt 换 Abstain-R1 1:4 ICL 拒答+澄清契约 + 不对称误拒惩罚 | 编造 27% | Abstain-R1 |
| **P2** | curation 滞回防抖;衰减曲线**先做消融验证是否涨分再上** | curation 稳定性 | FadeMem |
| **备选** | FlyHash 汉明 dedup / surprise 代理 | 效率/规模 | BioHash |

### 对论文线(决策二)的额外弹药

- EcphoryRAG(报均值±std,10 次跑)、D-MEM(单跑 baseline vs 多跑)本身是
  **"别人单跑、我多跑"噪声论据的现成对照**;
- SynapticRAG τ_scale 断崖 + FadeMem LoCoMo +1.06 落在噪声带,都是
  **"<5pp 改进可能在单跑噪声以内"论点的一手案例**;
- 三篇独立生物隐喻收敛到"图+时间+强度",可作为"方向共识已成、评测可靠性
  才是真问题"的引子。一份调研两份产出。
