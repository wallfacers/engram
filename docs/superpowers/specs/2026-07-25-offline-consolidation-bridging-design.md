# 离线固结 · 跨 session 桥接合成（offline consolidation / bridging）

日期：2026-07-25
状态：设计已确认，待 writing-plans
作者：brainstorming session（maintainer 逐段确认）

## 1. 一句话

在「在线抽取」与「策展减法」之间，新增引擎的**第三个写入阶段**：一趟离线后台
pass，跨 session 枚举证据对并**合成新的桥接 fact**，把原本埋在检索深处、需要多跳
才能拼出的答案，预先变成一条可被直接检索到的一等公民记忆。

## 2. 为什么做这个（动机与差异化）

### 2.1 它打的是唯一还活着的真瓶颈

answerer-parity 探针收口后，对 MemOS 的 ~3.4pp 差距约七成是答题模型伪影；引擎侧
真剩余 ≈1pp 级。对 Mem0 92.5 即使 parity 后仍有 ~4.6pp——那部分只能靠机制创新挣。
而所有便宜杠杆探尽后，唯一活着的结构性瓶颈是 **dense 深召回上限**：multi-hop /
open-domain 的 gold 埋在 rank 71-90。

我们已**三向证伪**「写入侧表示改写」（011 alias / 012 doc2query / 010 query 分解）。
证伪的机理是关键：**影子向量对称地抬高噪声，gold 的相对位置不变**。

桥接合成不是加影子向量——它**造一条内容全新的记忆单元**，非对称地改变检索池的
构成、偏向 gold。这正是那三个杠杆做不到的事。

### 2.2 它是竞品的结构性盲区

| 系统 | 记忆写入 | 离线固结 |
|---|---|---|
| Mem0 | 在线 ADD 原子事实 + 图 | 无 |
| MemOS | 记忆 OS / 调度治理 / MemCube | 无 |
| AtomMem（LoCoMo SOTA） | 原子事实 + 事件层 + profile + 图 PPR | 无（仍是在线抽取） |
| Auto-Dreamer | 离线 region-rewriting + GRPO | 有，但只在 agentic 任务（ALFWorld/WebArena），且需 RL 训练 |
| **engram（本设计）** | ADD 原子事实 + 实体图 + **跨 session 桥接合成** | **新增** |

**没有任何竞品在对话记忆场景做过离线固结。** 三家在线抽取系统逐 session 处理，
天生看不见跨 session 的连接——这既是它们的盲区，也正是本设计的剪枝维度本身。

### 2.3 两条灵感轴都占

- **生物学轴**：互补学习系统（CLS）——海马快速记录 episodic 事件，睡眠期回放给
  新皮层，新皮层从零散事件中提炼跨事件关联，形成原本任何单条记忆里都没有的新
  记忆。engram 之名即神经科学的「记忆痕迹」。
- **大模型轴**：2026 年「重构式记忆 / region-rewriting」一簇（Auto-Dreamer、
  Memory is Reconstructed not Retrieved、SCM）。

**明确不抄的部分**：SCM 的 REM-dreaming（随机游走造新边）在其自身消融中对事实
召回零增益——本设计只做**有证据支撑的桥接合成**，不做随机联想。

## 3. 范围

### 3.1 本次做

跨 session 桥接合成，攻 **multi-hop**。

### 3.2 本次不做（YAGNI）

- **抽象合成**（攻 open-domain）：有砸 single-hop 粒度的已知风险（B 类粒度错题
  占 19%），排在桥接之后。
- **去重合并**：Mem0 的 UPDATE 已覆盖，创新性最低，且与现有 curation 减法职责重叠。
- **二阶固结**（桥接之上再桥接）：自举放大幻觉，第一版直接堵死。
- **多租户实现**：SaaS 是「设计上不堵死」，不是第一版交付。见 §6。

## 4. 架构与数据模型

### 4.1 位置

```
ingest ──抽取──► 原子 fact ──┬── curation：删/合并/抑制（减法，已有）
                             └── consolidation：合成桥接 fact（加法，新增）
```

新包 `memory/consolidation/`，与 `memory/curation/` **平级**，复用其 `Lease`。

**为什么不并入 curation**：curation 是减法、失败语义「宁可不删」；consolidation
是加法、失败语义「宁可不加」。水位线、判据、回滚方式全不同。分包才能各自独立
测试，也才符合单元职责单一的要求。

### 4.2 产物形态 —— 同构 entry + 旁路血缘表

**桥接产物就是一条普通的 `memory_entries`。** 同构 ⇒ **检索侧零改动**，现有三路
RRF 直接命中。任何「新 entry 类型」都要改检索器，是自找的耦合。

血缘另立表（v3 migration，新增不改旧）：

```sql
CREATE TABLE IF NOT EXISTS memory_bridges (
  entry_name TEXT PRIMARY KEY,   -- 合成产物的 entry name
  source_a   TEXT NOT NULL,      -- 源 entry name（排序后较小者）
  source_b   TEXT NOT NULL,      -- 源 entry name（排序后较大者）
  pair_key   TEXT NOT NULL,      -- (source_a, source_b) 的确定性 key
  created_at INTEGER NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_memory_bridges_pair ON memory_bridges(pair_key);
```

**为什么不塞进 entry 加一列**：
1. 要能**整批回滚**——删光所有桥接 = 一条 DELETE + 级联，A/B 干净；
2. 要能**按租户统计与关停**——SaaS 那天的计费/审计/开关点。

`pair_key` 上的唯一索引同时提供幂等（§5.4）。

### 4.3 ADD-only 硬约束

**绝不改写、绝不删除源 entry。** 既是宪法要求，也是照论文失败模式做的规避：
Auto-Dreamer 栽在「过度压缩丢局部细节」上，那种损失会直接砸已量化的 single-hop
粒度错题。只增不改，天然免疫。

## 5. 算法

### 5.1 候选枚举 —— 纯 Go、零模型、确定性

不剪枝就是 N² 爆炸（一个 conv 几百 entry → 十几万对）。三层剪枝：

1. **只桥接跨 session 的对**。同 session 内的关联在线抽取时上下文已在一起，
   已被覆盖。这一层同时是剪枝和差异化本身（§2.2）。
2. **必须共享实体**。走 `memory_entities` 倒排，只枚举同桶内的对。
3. **共享实体必须稀有（IDF 剪枝）**。`EntityDocFreq` 给 df：共享 "work"（df=200）
   无信息量，共享 "Berlin"（df=3）才是真信号。

打分：`score(a,b) = Σ_{e ∈ shared(a,b)} IDF(e)`，降序取 **top-K per conv**（硬上限
兜底），**K 初始值 2000**（对应门 0 判据 B 的 10 conv ≤ 2 万）。排序须完全确定性
（同分时按 `(name_a, name_b)` 字典序），以便表驱动单测。

「再加一层语义余弦过滤」**不先投机**——门 0 会免费测出加与不加的候选召回差异，
让数据决定。

### 5.2 合成 —— LLM 必须能廉价说「没有」

对每个候选对，喂两条 fact，要求：「它们之间是否存在一条可推出的、非冗余的桥接
事实？没有就返回 NONE。」

**拒绝权就是精度阀门。** 大多数候选对是噪声；没有廉价的拒绝路径，这东西会变成
幻觉发生器。输出一条短桥接 fact + 它引用的两个源。

### 5.3 落库

合成 entry 走**正常 `EntryStore` 写入路径**——embedding、entities、FTS 镜像全部
自动，零特殊路径。随后写入 `memory_bridges` 一行。

### 5.4 作业形态（SaaS 兼容全在此）

| 属性 | 做法 |
|---|---|
| 多实例安全 | 复用 `curation.Lease` + heartbeat，照 `Worker.RunPass` 模子 |
| 幂等 | 候选对排序后作 `pair_key`，`memory_bridges` 唯一索引；重跑不重复合成 |
| 可增量/可恢复 | 由幂等自动获得——pass 中断后重跑只补未做的 |
| 预算上限 | per-pass max candidates，照 curation 的 `max_candidates_per_pass`。**SaaS 那天即租户配额旋钮** |
| 离线降级 | 无 LLM 配置 → worker inert 完全不跑（照 `NewWorker` 的 `call == nil` inert 模式）。宪法 I 满足 |
| 可显式驱动 | 导出 `RunPass`，照 `ResolveConflictsPass` 先例——LoCoMo eval 在摄入后、问答前显式跑一次 |

## 6. SaaS 兼容的确切含义

SaaS 是**后续的、条件性的**（「如果效果不错」）。第一版单机跑通，但数据模型与
作业形态从一开始就按多租户写：产物带 provenance 可审计可回滚、pass 可增量可恢复
幂等、有硬预算上限、多实例 lease 安全。

`curation.Worker` 已是这个形状——**照它的模子长，SaaS 那天基本零改造**。
除此之外不做任何多租户实现。

## 7. 验证门（写引擎代码前的先证伪）

两级门，**任一不过当场毙，不写一行引擎代码**。

### 7.1 门 0 —— 100% 免费（零模型调用，纯本地）

数据：HF 私有集 `wallfacers/engram-locomo-artifacts` 拉回的 009 store + trace，
加 LoCoMo 数据集的 `Evidence` 字段（turn 级 gold 证据，形如 `["D1:1","D2:1"]`）。
`--chunks` 模式下存在 entry→turn 映射，故 **gold entry 对可精确算出**。

「multi-hop 失败题」定义：009-full-A-base 三次重复中多数判错的 category-1 题
（与既有 levers 诊断同一口径）。

纯 Go/Python 复刻 §5.1 的候选枚举，测两个数：

| 判据 | 含义 | 过 | 死 |
|---|---|---|---|
| **A 候选召回** | 见下方精确定义 | ≥ 60% | < 40% |
| **B 候选规模** | 10 conv 候选对总数 | ≤ 2 万 | > 5 万 |

**判据 A 的精确定义**（避免多证据题的歧义）：

- 一道题的 gold entry 集合 = 其 `Evidence` turn 经 entry→turn 映射得到的全部 entry。
- 该题的 **gold 跨 session 对** = 上述集合中所有 `source_session_id` 互不相同的
  entry 两两组合。
- **分母** = multi-hop 失败题中「至少存在一个 gold 跨 session 对」的题数。
  （不存在此类对的题，桥接在原理上就救不了，不计入——否则是拿救不了的题稀释判据。）
- **分子** = 上述题中，**至少有一个** gold 跨 session 对出现在候选集里的题数。
  （命中任一对即可：合成一条正确桥接就足以救这道题。）

40–60% 为灰区：**只允许调一次** K/IDF 阈值重测；调完仍在灰区按死处理。
（"只调一次"写死在设计里，用于堵住事后挪门。）

**死线依据**：multi-hop 282 题、历史失败 ~40% ≈ 113 题。桥接只能救「gold 跨
session」那部分。候选召回 60% ⇒ 上限触及 ~68 题，合成救活一半 ≈ 34 题 ≈ 全量
**+2.2pp**，够格开工。召回 <40% ⇒ 最乐观 ~1.5pp，不值得写引擎代码。
B 的 2 万对 ≈ 1200 万 token，box qwen 近免费但已是时间上限。

### 7.2 门 1 —— 近免费（只要 embedding，不需要答题 LLM）

把 gold 证据对**模板拼接**成 oracle 桥接 entry（不用 LLM），插进 store 副本，
重跑 `--coverage-only`（已存在的 retrieval-only 模式，零 LLM 调用）：

| 判据 | 过 | 死 |
|---|---|---|
| **C 可发现性** | ≥ 50%（定义见下） | < 50% |
| **D 覆盖增益** | coverage@30 Δ **> 0**（严格大于） | ≤ 0 |

**判据 C 的精确定义**：分母 = 插入的 oracle 桥接 entry 总数（每道判据 A 命中的题
各插一条，取该题得分最高的 gold 跨 session 对）；分子 = 其中在**对应题**的检索
结果里进入 top-30 的条数。

**D 沿用杀死 011/012 的同一把尺**——不换尺子，否则本次的「过」没有可比性。

这是**上界探针**：真实 LLM 合成不可能优于 oracle 拼接。上界不过即毙。

## 8. 测试策略（TDD，先写失败测试）

| 测什么 | 怎么测（全部离线、零模型） |
|---|---|
| 候选枚举 | 确定性纯函数，表驱动单测：固定 entry+entity 集，断言候选对及排序 |
| 幂等 | 同一 pass 跑两次，`memory_bridges` 行数不变、无重复 entry |
| ADD-only | pass 前后断言源 entry 内容与数量逐条不变 |
| 幻觉闸（NONE） | LLM 返回 NONE → 不落库 |
| 幻觉闸（悬空引用） | 返回的源引用不存在 → 拒绝落库 |
| 离线降级 | `call == nil` 时 `RunPass` 零副作用 |
| 多实例 | 复用 lease 测试模式，双 worker 只有一个能跑 |
| 二阶禁止 | 候选枚举跳过已在 `memory_bridges` 中的 entry |

## 9. 宪法核对

| 条款 | 本设计如何满足 |
|---|---|
| I 本地优先/离线默认 | 无 LLM 配置 → inert 完全不跑；核心读写路径不依赖固结 |
| II 引擎/适配器分离 | 纯引擎增量，无适配器改动；MCP 侧零变更 |
| III 契约优先/命名空间隔离 | v3 migration 新增不改旧；产物同构 entry，检索契约不变；一 ns 一 store 不受影响 |
| IV 评测回归门（不可协商） | 动写入路径 ⇒ 合并前全量 LoCoMo 对比，不得回归 baseline；eval 配置改动与算法改动分开提交 |
| V 优雅降级/诚实规模 | 合成失败 fail-safe（WARN + 不落库），绝不影响既有检索；无 ANN/百万级承诺 |

## 10. 开放风险

1. **候选召回可能不达标**——门 0 正是为此设的，免费当场判死。
2. **合成质量**（LLM 是否真能产出有用桥接）——门 0/1 都不覆盖这一层，它只在真实
   实现后的全量 eval 里见分晓。门只保证「值得一试」，不保证成功。
3. **桥接引入的噪声**可能挤占 top-30 名额，伤及 single-hop——全量回归门负责兜底。
