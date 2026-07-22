# engram 能力认知与产品北极星

> 🧭 **状态**: 活跃 · **北极星总纲** —— 一处看清 engram「**是什么 / 水位在哪 / 往哪走(拉平 → 论文 → SaaS)**」,方便后续推进。
> 分工:技术**涨点 backlog 正本**在 [memory-strategy.md 附二](./memory-strategy.md)(生物启发 P0/P1/P2),本文只链接不复制;竞品分数在 [competitive-benchmarks.md](./competitive-benchmarks.md);逐特性细节在 [`../specs/NNN-*/`](../specs)。
> **诚实纪律(宪法 V)**:第 1–2 节是**当前实测能力**;第 4–7 节标注为**方向/远期**,非当前能力,尤其习惯记忆/主动召回是未立项的新能力。

---

## 1. engram 是什么(能力正本)

**本地优先、可嵌入的记忆层**:一套调优过的记忆引擎(存储 · 混合检索 · 抽取 · curation)+ 薄适配器(今日 MCP,后续 CLI/SDK)。从 `workhorse-agent` 抽离,行为保持。

- **引擎(host-agnostic,离线可跑)**:
  - **存储**:SQLite(modernc 纯 Go,无 CGO),单文件、WAL、FTS5 trigram。
  - **混合检索**:三信号 —— 语义(向量余弦)+ 关键词(FTS5 BM25 + LIKE 兜底)+ 实体(精确匹配),RRF(k=60,免调参)融合;可选 cross-encoder rerank(默认关)。**逐信号优雅降级**(无 embedding→关键词+实体;无实体→仅关键词)。
  - **抽取**:ADD-only 从对话轮抽事实。
  - **curation**:确定性打分 + 近重复 dedup + LLM judge + 跨进程 leader-lease。
- **适配器(薄,只调引擎公共 API)**:MCP stdio server(已交付,namespace 隔离,一 ns 一独立 store)。CLI/SDK 后续。
- **五条不可谈判**(宪法):① 本地优先离线默认;② 引擎/适配器分离;③ 契约先行 + namespace 隔离;④ 评测回归门(不可回退 LoCoMo 基线);⑤ 优雅降级 + 诚实规模。
- **诚实规模**:单用户 ~10 万条级;不承诺千万 token;ANN/量化是明确的未来工作,不是隐含承诺。

## 2. 当前诚实水位(2026-07,post 007/008)

**LoCoMo 端到端 overall = 83.70%**(mem0-aligned judge / 本地 Qwen3.6-35B-A3B-FP8 答题 / bge-small 检索 / top-k30 / 全量 1540 / 无 reranker)。

**关键认知修正**:旧的 50.7% / ~74 是**判题口径伪影**(strict judge + luna 栈)。对齐 Mem0 判题口径(007)后真实水位 83.70%,**差距远比想象小**:

| | 分数 | 对 engram 差距 |
|---|---:|---:|
| **engram(新参考点)** | **83.70%** | — |
| MemOS(local,开源) | 88.83 | **~5.1pp** |
| Mem0 | 92.5 | ~8.8pp |

**分类差距地图**(拉平靶心;正本 [locomo-score-levers.md](./locomo-score-levers.md)):

| 类别 | 正确率 | n | 诊断 |
|---|---:|---:|---|
| single-hop | 86.68% | 841 | 已接近 MemOS 级;**大 n → 每 +1pp 贡献最大** |
| multi-hop | 85.82% | 282 | 已接近 MemOS 级;瓶颈在实体平表 vs 可遍历图 |
| temporal | 82.24% | 321 | 次弱、脆;时序推理需**检索侧**时间结构化 |
| **open-domain** | **56.25%** | 96 | **最弱**;加满召回几乎不动(54→56)→ 答题/推理/判题问题,**非检索** |

**决定性教训(008 US4)**:**检索召回不是瓶颈。** 本地 reranker 拿 +15.457pp turn@k coverage,端到端答题 **−0.06pp(McNemar p=1.0)**——它 helps 3 类 +8 但把 temporal 砸 −9。**⇒ 以后杠杆一律以端到端答题分为 verdict,coverage 只作诊断;拉平要打答题精度,不是堆召回。**

## 3. 拉平比分的方向(技术,收敛结论)

**罗盘**:提 open-domain + temporal + single-hop 精度,**不是检索召回**。逐类方向(细则见 [memory-strategy 附二 P0/P1/P2](./memory-strategy.md#附二生物启发检索机制调研2026-07两轮独立分析--交叉验证)):

- **multi-hop** → 实体平表升级为**可 1-hop 遍历的联想图** + depth-2 质心游走 + 原 query 重排(P0,EcphoryRAG/HippoRAG2)。
- **temporal** → **检索侧**时间结构化:`event_date` 升为时间范围 + 事件词汇别名喂 FTS + T_score 进 RRF 第 4 路(P0,Chronos/SynapticRAG)。⚠️ 已证 prompt 救不动(005/008 US2)。
- **open-domain 56%** → **最深的未知**;US4 证明非检索。**待背景研究 agent 抽样读错题定位**(判题严 / 答题推理弱 / gold 模糊)后再定杠杆。
- **single-hop** → 大 n 隐藏杠杆,提精度(去噪写入门 D-MEM / 冲突消解)对它和整体都有量级贡献。

**纪律**:端到端答题分为准(US4 教训)· 死规则(禁付费云 rerank 涨点)· 引擎从适配器不可改 · 评测/机制分离提交(宪法 IV)。

## 4. 远期一:论文 —— 评测可靠性分析(方向已定)

正本 [memory-strategy Decision 2](./memory-strategy.md#决策二论文--要发方向定为评测可靠性分析文)。核心论点:*该领域大量 <5pp 改进可能在单跑噪声内,且跨论文分数因协议差异不可比*。

**US4 新增一手弹药**:**+15.457pp 召回 → 0 答题增益(p=1.0)**——"召回类指标改进不必然转化为答题"的教科书案例,与 reranker-for-memory 的正结果扎堆(MemReranker 等)直接对话。补强了 memory-strategy 已列的"二段选择只在一段召回弱时有效"负结果边界。

## 5. 远期二:SaaS —— 用户操作习惯记忆(⚠️ 方向/新能力,非当前能力)

### 5.1 战略演进(为何 SaaS 重新打开)

[memory-strategy Decision 1](./memory-strategy.md#决策一产品方向--不做云-saas全力做本地开源记忆产品) 当年判"**不做云 SaaS**",两个前提如今都变了:

1. **分数前提变了**:当时 ~74 vs 92.5(~18pp);现 **83.70 vs 88.83(~5pp)**。"胜负不取决于外壳"依然成立,但"分数差距大到不值得做产品"不再成立。
2. **定位前提变了**:当年拒的是"**通用记忆云**"红海(Mem0/腾讯已占)。现在要做的是**垂直的「设备/应用用户习惯记忆」**——错位赛道,不是通用记忆库。

> **新定位(收窄)**:engram = **「用户操作习惯记忆层」**,让**设备/应用厂商零门槛接入**,记住并主动利用用户偏好/习惯。卖的不是"再装一个记忆组件",是"端侧、隐私安全、开箱即用的习惯记忆能力"。

### 5.2 场景实质化(把痛点落到产品)

| 场景 | 用户行为 | 记忆层做什么 | 厂商价值 |
|---|---|---|---|
| **车机系统** | 用户上车习惯打开音乐 | 沉淀"进车→播放音乐"的**行为模式**,在**进车场景**主动召回 | 上车自动/建议播放,无需手动 |
| **手机厂商** | 用户每天熬夜刷抖音 | 沉淀"夜间高频短视频"**作息偏好** | 健康提醒 / 个性化推荐 / 家长管控 |
| 通用 | 常用操作序列、偏好、作息 | 偏好画像 + 场景条件召回 | 个性化、减少重复操作 |

### 5.3 痛点 → 为何 engram 契合(错位优势)

- **厂商不想自建记忆基建** → engram = **单二进制嵌入式**,drop-in,无云依赖。
- **习惯数据极敏感(隐私)** → **local-first / 离线 = 数据留在端侧、不上云**。合规是**卖点**而非负担(直击车机/手机厂商最怕的隐私风险)。
- **多用户/多设备隔离** → **namespace isolation 现成**(一 ns 一独立 store)。
- **端侧算力有限** → 纯 Go 无 CGO、SQLite、embedding sidecar 可选/可关(优雅降级)。

### 5.4 "如何方便对接"(集成形态)

- **薄适配器**:厂商只调公共 API(HTTP/SDK/MCP),**不碰引擎**(宪法 II)。
- **事件摄入面**:`ingest(event: 时间 + 动作 + 场景上下文)` → 抽取习惯/偏好 → `recall(场景上下文)` 返回偏好。
- **降迁移成本**:考虑 **Mem0 兼容 API**(memory-strategy 待办已提),厂商从 Mem0 平移过来零改造。
- **一个参考集成**(车机或手机)作样板,证明"一天接入"。

### 5.5 能力缺口 → 产品缺口映射(**关键洞察:拉平的技术活正好是产品要的**)

| 产品需要 | 当前状态 | 与拉平分数的关系 |
|---|---|---|
| **新鲜度 / 状态一致性 / 条件召回** —— 习惯会变("以前爱 X 现在爱 Y") | ⚠️ 非当前能力([freshness 文档](./memory-freshness-and-retrieval-policy.md) 待立项) | **= temporal 涨分要做的检索侧时间结构化 + curation 冲突四分类**。一份投入,分数 + 产品双份产出 |
| **主动召回 / 场景触发** —— "上车就想起播放音乐" | ⚠️ 全新方向(当前仅被动 query) | 新能力;与 temporal/上下文检索相关 |
| **行为序列 / 频率模式抽取** —— 从操作流沉淀习惯 | ⚠️ 当前抽取是对话事实,非行为序列 | pipeline 新方向;去噪写入门(D-MEM)对信噪比有帮助 |

## 6. 三线关系(一份投入多份产出)

- **评测 harness**(`cmd/locomo-bench`)= 论文实验台 + 产品回归门。
- **temporal / curation / freshness 技术活** = 拉平分数 **同时** 是习惯记忆产品的基石。
- **local-first** = 论文的诚实性叙事 **同时** 是 SaaS 的隐私核心卖点。

## 7. 里程碑 / 目标锚(留目标)

| 里程碑 | 目标 | 门槛 |
|---|---|---|
| **M1 拉平** | 端到端对齐 MemOS 88.83(约 +5pp);先做 P0 实体图 + temporal 检索侧时间结构化,**端到端 A/B 验证**(不用 coverage) | 死规则 + 引擎不可改 + 评测门 |
| **M2 论文** | 评测可靠性分析文成稿,投 workshop / analysis track;US4 coverage≠answer 作弹药 | 2-3 answer 模型 × ≥5 重复 + LongMemEval 交叉 |
| **M3 SaaS MVP** | 习惯记忆:事件摄入 + 习惯抽取 + 偏好召回 + 场景触发;1 个厂商参考集成(车机/手机) | 隐私(端侧)+ namespace 隔离 + 易对接不可妥协 |

> **贯穿红线**:诚实规模不吹、local-first 不破、引擎从适配器零改动、涨点只走纯客户端可移植技术。

---

**相关正本**:战略/技术 backlog → [memory-strategy.md](./memory-strategy.md);竞品分数 → [competitive-benchmarks.md](./competitive-benchmarks.md);杠杆实验台账 → [locomo-score-levers.md](./locomo-score-levers.md);未决 freshness/条件召回 → [memory-freshness-and-retrieval-policy.md](./memory-freshness-and-retrieval-policy.md)。
