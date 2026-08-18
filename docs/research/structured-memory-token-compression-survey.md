---
title: 结构化记忆压缩 top-k token — 文献综合与 engram 落地方案
date: 2026-08-18
tags: [research, literature, memory-structure, token-compression, locomo, longmemeval]
status: 调研完成,方案待 SDD(spec 047 候选)
source: alphaXiv 深读 7 篇(2026-08-18)
---

# 结构化元信息压缩 top-k token 消耗 — 文献综合

## 问题与动机

k30 quota28 收线后(89.74%,上下文 6957 tok ≈1.9×)的核心矛盾:28 条 verbatim chunk
满槽 = "条数不变 token 变重",gold chunk 混着 4–5 个 turn 的邻居噪声,决定性证据被稀释
(box 逐题 46 救/42 翻的对冲结构)。维护者问题:**当前记忆/RAG/GraphRAG 领域有什么
结构化元信息,既缩 token 又涨点?**

约束(继承):无类别特化、无数据集定制、纯客户端可移植、不做大力出奇迹(同预算提质)、
paid rerank DEATH RULE 不变。

## 七篇文献速览

| 论文 | 核心结构 | LoCoMo/LME token 证据 | engram 适配 |
|---|---|---|---|
| **DimMem** (2605.15759) | 原子+类型化(fact/episodic/profile)+ 维度字段 {time, loc, reason, purpose, keywords} + ρ 回源指针 | LoCoMo tokens/query **3859** vs LightMem 5063(−24%)vs NaiveRAG 10234(−62%),同时 overall +7.7pp(GPT-4.1-mini 口径 80.51%) | ⭐ 最高:软维度路由与 RRF 同构;表示消融最强 |
| **LeanMem** (2608.03463) | 按可压缩性三分流:profile(属性值对)/event(时间锚+状态)/record(**gist+NER 实体+源指针,原文不压缩**) | LoCoMo 构建 −26.8%/推理 −23.6%,准确率 +5.54pp(84.87%) | ⭐ 高:chunk=record 的正确定位;类型化写调度 |
| **xMemory** (2602.02007) | decoupling before aggregation:segment→component(隔离区分性证据)→group;top-down 骨架+按需展开 | LoCoMo(Qwen3-8B)token/query **4711 vs 7755(−39%)**,F1 40.88→43.98 | 中:组件构造重,展开判据涉 041 家族 |
| **AtomMem** (2606.19847) | 原子事实 {参与者/关键词/时间/事件链接}+事件层+时间档案+RWR 图 | AtomMem-Flat(纯原子扁平)722K vs 原文 827K(−13%),J 52.08 vs 41.67 | 中:SFT 抽取器(训练线,已证伪家族);k>20 掉分佐证稀释 |
| **MRAgent** (2606.06036) | Cue–Tag–Content 图+多轮主动重构 | LME per-sample 118k vs Mem0 245k(−52%) | 低:执行层 = 029 agentic 家族 |
| **δ-mem** (2605.12357) | 8×8 在线状态矩阵+delta-rule+注意力低秩修正 | LoCoMo F1 49%(Qwen3-4B) | 排除:需训练+改 backbone 前向 |
| **Mi-Memory** (2607.18975) | 生命周期框架:L0 facts/L1 summaries/L2 profile + 混合检索 + 审计契约 | LoCoMo 93.59%(GPT-4.1-mini,evermemos 同 harness) | 参考:分层粒度与治理契约,非机制源 |

注:各篇 backbone/judge 口径互不可比,只取各自**内部**的 token/准确率配对对照。

## 四个收敛的结构模式(≥3 篇独立收敛)

### 1. 类型化记忆单元 + 显式维度元字段(表示侧,主导因子)

- AtomMem F={P,K,T,E};DimMem D={time,loc,reason,purpose,keywords}+type;LeanMem
  {profile 属性值对 / event(topic+时间锚+状态) / record(gist+实体+指针)}。
- **DimMem 消融定主次**:Content-Only(丢维度、保内容)LoCoMo 掉 **10.39pp**;去掉
  dimension 检索路由只掉 1.56pp。**涨点主要来自表示结构本身,不是检索算法** —— 与
  engram 008 铁律(检索侧 coverage 不转化)方向一致:该动写侧表示。
- engram 对照:现行 fact 是无结构泛化改写文本。027/028 证伪的是 "event 表示整体替换
  chunk"(丢绝对时间锚定、7B 抽取仅 5% 锚定),**不是 "维度化元字段增强"** —— 两件事。

### 2. 按可压缩性分流,verbatim 是一等公民(LeanMem 独有洞见)

- 稳定属性 → 紧凑结构化(零 LLM 二次调用);演化事件 → 时间锚+状态;**细节密集内容
  (列表/计划/表格)→ 不压缩,存 retrieval gist + NER 实体 + 源指针**。
- 对 engram 的直接启示:**chunk 通道 = record 类型,不该被 fact 替换(027 已证伪),
  该被元数据增强**。ingestChunks 的 chunkTurns(chunk→dialogue ids)已经是 ρ 指针;
  memory_entities 表(v2 迁移)已备;缺的是 chunk 的 kind 分流与实体回填。

### 3. ρ 回链 + 结构性触发的按需展开(非置信度门控)

- DimMem:assistant 原文仅 I_ast=1 时经 ρ 拉回 — 12.2% 触发率、token 降到 34.4%,
  意图检测器 P 90.2%/R 98.2%;LeanMem:record 指针仅在"细节敏感验证"查询展开;
  xMemory:compact backbone 默认、不确定性才展开到原文。
- 与 041 证伪家族的边界:041 证伪的是**不确定性信号**(thinking 犹豫 recall 上限
  50–63%);这里的触发判据是**结构性**(问题类型/查询意图:要 timeline?要 assistant
  原文?要逐字细节?)——LeanMem/DimMem 的检测器数字(P>90%)远高于 041 的犹豫信号。
  engram 落地时触发器必须走结构判据(查询解析类别),不走置信度。

### 4. 维度软路由 = 第四 RRF 信号(检索侧,次要但零成本)

- DimMem 三路:BM25 ∪ dense ∪ dimension route(查询解析成同 schema 后维度约束软打分,
  缺席维度不稀释)→ 与 engram semantic+keyword+entity 三信号 RRF **架构同构**。
- 013 证伪的是时间窗**硬过滤**(解析器只点火 19.6%、臂对 80% 题永不触发);软打分
  加一路信号不丢候选,失败模式不同。

## 与 engram 已证伪地图对账

| 文献机制 | engram 已有 verdict | 对账结论 |
|---|---|---|
| 维度化表示(schema 抽取) | 027/028/037 写侧表示/训练三连证伪 | **不冲突**:027 是"替换+丢锚定";维度化是"增强 fact 元字段",时间归一化用窗口时间戳(LeanMem τ_t)非模型自由发挥 |
| chunk=record+元数据 | 027 event 替换 −26.2pp | **正面对齐**:LeanMem 证明细节密集内容不压缩才是对的 |
| dimension 软路由 | 013 时间窗硬过滤 NO-GO | 不冲突(软 vs 硬);预期增益低(1.5pp 档) |
| 训练抽取器 | 023/028/037 三连 | **排除训练**;DimMem 有 30B zero-shot 数据点(76.17 vs 微调 4B 79.87)——schema 提示词拿大头,engram 用现有 extract 通道改 prompt schema |
| 多轮 agentic 导航 | 029 −17.9pp | 排除 MRAgent 执行层 |
| RWR 图激活 | 014 assoc graph e2e NO-GO | 排除 AtomMem 图部分 |
| 置信度门控展开 | 040/041 | 排除;只保留结构判据触发 |
| paid/托管组件 | DEATH RULE | δ-mem(backbone 改造)排除 |

## engram 落地方案(SDD 候选:三层 increment)

**US1 — 检索侧维度软路由(零 LLM、离线可验)**
- event_date/memory_entities/kind 作为第四 RRF 信号;查询侧时间/实体约束软打分。
- 验证:wide-dump + sweep_quota.py 离线重放,零 API 成本。
- 预期(按文献):低,~1.5pp 档;价值是零成本排雷 + 为 US2 铺查询解析。

**US2 — 写侧类型化抽取 schema(核心,表示侧)**
- 抽取 prompt 改造:每 fact 带 {type: fact|event|profile, time(窗口时间戳归一化),
  entities, keywords};verbatim chunk 保持 record 定位 + 实体元数据回填。
- 不训练(023/028/037 教训);时间锚定靠注入的 session date,非抽取器记忆(027 教训)。
- quota 机制升级为按 kind 分流槽位(短紧凑 fact 高密度,chunk 按需)。
- 预期:文献表示消融 −10pp 逆向 → engram 口径待验;token 账目标:6957 → ~4000 档
  (DimMem 3859 参照,−40% 级)。
- 成本:重建 store 一次抽取(本地 fastembed + box Qwen 一次性,非每题)。

**US3 — 读侧紧凑骨架 + 结构触发展开**
- 类型化 facts 作为默认骨架(短),verbatim chunk 仅在结构判据触发时展开
  (枚举/细节/assistant-原文类查询;查询解析分类,非置信度)。
- 风险:查询解析器覆盖(013 的 19.6% 前车之鉴)——触发器失败模式必须是"多给 chunk"
  (降级到现状)而非"少给"(丢 gold)。

**验证路径**:全本地零 API 先行(009-bge-chunks-store 正本 + fastembed sidecar +
wide-dump sweep 方法论);US2 重建店后 e2e 用 box 单变量配对 3-rep clean 口径
(--judge-mem0-aligned 必带),对照锚 89.74%(quota28)。

## docs/ 既有 verdict 全量对账(2026-08-18 补,走 spec 前的踩坑复审)

对账来源:experiment-verdicts、structured-content-directions(active, 2026-08-06)、
retrieval-budget-reduction-directions(closed)、low-topk-recall-context-survey、
033/026/045/027/040 verdicts、default-off-mechanism-cleanup-plan(044)。

### 坑 1 —「reader 强度」推翻文献压缩结论(最重)

**DimMem/LeanMem/xMemory 的"压缩+涨点"全部来自弱 reader 栈(GPT-4.1-mini / Qwen3-8B)。
engram 的 answerer 是 Qwen3.8-27B thinking——强 reader 下"证据密度是瓶颈"的前提不成立**
(What Survives Into Context 条件 iv 自证:3B 赢、7B 被吸收、14B 反转)。engram 自身的
四重实证同向:

| verdict | 机制 | 结果 |
|---|---|---|
| 026 exact-token | 按 need 剪枝,token −33% | −4.5pp/−3.6pp,simple 类目回归(依赖精确逐证据) |
| 045 次模装填 | 预算内贪心装填 | **−14.22pp(p<0.0001)**,短碎片项主导,single-hop −22.6pp |
| 030 T013 | trace 默认值移除 | 当前栈 **−3.44pp**(030 的 85.91%@468tok 是旧 fts 弱栈成绩) |
| 040 归因 | 30→150 增量分解 | **79% 是"上下文量"**——强 reader 要吃更多原文,压缩方向结构性反向 |

**修正:原方案 US3(紧凑骨架+按需展开)在当前栈上是被证伪家族,撤销。US2 的
token 压缩预期(6957→4000)同步撤销——压 token 与涨分在强 reader 栈上不可兼得。**

### 坑 2 — 写侧 query-agnostic 构造双向证伪

LazyMem 深读结论:"retrieve-then-construct 优于 construct-then-retrieve(query-agnostic
写时构造有不可逆信息损失)——与 024/025/027/028 写侧 NO-GO 完全一致"。engram 写侧
最近四次:024 write_dedup 几乎不触发(21,860 判定仅 20 抑制)、025 episode 聚合
OVERALL −7.7pp(聚合叙事挤掉逐证据覆盖)、027/028 event 替换三连。**写侧元字段增强
虽区别于"替换",但 engram 土壤上写侧改造尚无一次正结果。**

### 坑 3 — 046 完美信号账封印"分数族"方法

040:完美信号上限 89.2% < k150 的 90.13%——42 体量题(79% 增量)对 RRF 曲线/
EVT/rerank/**任何相关性分数**隐形。**原方案 US1(维度软路由)属分数族,预期从
"低"降为"结构性封印",降为不做或仅诊断。**

### 坑 4 — 033 归因口径与 caption 缺口

`gold_in_pool` 只证任意命中≠回答链完整;DiaID 命中≠答案文本进 prompt。**target 16 道
single-hop 残差中 10 道关键细节只在 blip_caption**(全局 --image-captions 已被 018
证伪 −0.71pp;正确姿势 = 查询期命中 turn 后的 late-binding)。任何表示/检索改造
救不了 caption 缺失——这是独立缺口。

### 坑 5 — 工程坑(执行时必带)

- **跨实现序漂移**:本地 fastembed 序 vs box vllm 序绝对数不可对标(quota28 教训:
  本地 net+32 → box +4);本地 sweep 只判方向,幅度以 box 单变量实测为准。
- **`--judge-mem0-aligned` 必带**(k30 unified 口径,漏传 76% 假读数)。
- **embed 并发非确定**:`--max-num-seqs 1`(并发≥4 同文本微差向量→parity fail-closed)。
- **不同 `--chunk-target-chars` 的 store 不可比**(flag 自带告警)——粒度 sweep 每档一店。
- **LME 杂讯 chunk**:`EMBED_TRUNCATE_PROMPT_TOKENS=-1`(0.031% 超长 chunk 400 会
  fail-closed 整批)。
- **SSE 超时**:`--per-call-timeout` 15m(思考模型×长上下文 8m 不够)。
- 044 已删 013/025 temporal-score、030/031 装配链、021/029/042 等 flag——相关机制
  重做需新代码,且注意"没转正的机制最终被清理"的维护成本哲学。

## 修正后的方案(覆盖前文 US1/US2/US3 排序)

对账后真正开放、且 docs 明确认可的结构方向只有一个主轴:

**A2 — turn/语义粒度 chunk(structured-content-directions 标注"唯一未测的写侧粒度结构")**
- 机制:检索单元从 900-char chunk(混 5–10 turns)细化到 turn/语义段粒度,同 30 槽位
  覆盖更多 gold turn、剔除邻居噪声——**token 降(gold 密度升)而非内容压**,
  不踩坑 1 的"压缩"语义。EverMemOS 证据:语义分段 vs 固定切块 +4.6pp 同候选同预算。
- 对症:quota28 verdict 自己指出"下一杠杆须解决翻车 42 题(更细 quota 点位或 chunk
  排序,均未试)";040 的"体量"需求在小粒度下同 token 可容纳更多单元。
- 已有基建:lossless chunking(DiaID 每片段保留)、`--chunk-target-chars` 现成、
  chunkTurns 映射、wide-dump sweep 方法论。
- 已知风险对冲:009"turn@k 对 fact 级 assoc 失明"→ turn±1 邻窗或语义段(EverMemOS
  式连贯分段而非硬 turn);045 碎片化反例 → 统一粒度+邻窗≠贪心选短项,但 sweep 必须
  看类别分解(multi-hop/计数类是否断链);LME 侧超长 turn 已有拆分逻辑。
- 验证:本地多粒度重建店 sweep(gold@30 × token × 类别分解,零 API)→ 方向成立后
  box 单变量 3-rep clean 配对。

**次优先(独立缺口,非 token 主线)**:caption late-binding(033 的 10/16 钥匙,
查询期命中后绑定);跨 hit 邻域合并去重(idx 级确定性,LazyMem 借鉴)。

**明确不做**(本轮对账新增强制项):紧凑骨架/按需展开(坑 1)、写侧类型化聚合
(坑 2)、维度分数路由(坑 3)、迭代/置信度门控(041/042)、训练线、图、agentic。
