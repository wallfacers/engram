# 小于五个百分点，还是噪声？长期对话记忆评测的可靠性审计

> **英文暂名**: *Less Than Five Points, or Just Noise? Auditing the Reliability of Long-Term Conversational Memory Evaluation*
>
> **文档性质**:论文骨架 + engram 现有证据台账，不是已完成论文，也不是新的
> leaderboard 声明。本文遵守[宪法 V](../.specify/memory/constitution.md#v-优雅降级与规模诚实-graceful-degradation--honest-scale):
> 单跑只称“先导观察”，缺原始产物的历史数字只称“待复现线索”，跨系统绝对分不在
> 未对齐口径下归因给记忆算法。
> 上位命题与成稿门来自[`capability-and-product-north-star.md` 第 4 节](./capability-and-product-north-star.md#4-远期一论文--评测可靠性分析方向已定)；
> 本文只展开 M2 论文线，不改写产品路线或引擎能力。
>
> **文献核验渠道**:2026-07-22 只通过 alphaXiv MCP 读取 arXiv 论文，未使用 Web
> 搜索。OmniMemEval 当前按官方软件/评测仓库与本地源码审计引用；alphaXiv 检索未发现
> 可替代该 artifact 的独立 OmniMemEval 论文记录，因此不把它伪装成论文引用。

## 一句话论点

长期对话记忆领域中，许多小于 5 个百分点的已发布改进可能落在单跑波动内；而当
数据切片、答题模型、答题策略、检索预算、judge 与聚合方法未同时对齐时，跨论文
绝对分数不能被解释为记忆系统本身的质量差异。这里的“可能”和“不能归因”是待用
多模型、多重复、跨基准实验检验的研究命题，不是从 engram 单个系统外推出的既成事实。

## 摘要（当前证据版）

长期对话记忆工作通常以 LoCoMo 等基准上的单次端到端分数衡量进步，但一次分数同时
混合了记忆写入、检索、答案生成、拒答策略和 LLM judge。我们计划对这条评测链做
可靠性审计，而非提出新的记忆架构。engram 的先导实验显示，重复运行之间存在远超
题目抽样误差的整体漂移；在预测文本逐字相同的样本上，judge 翻转率相对较低，提示
答案生成或服务端状态可能是主要波动来源。[E-003] 一份更早的内部审计还记录了
“按类别挑最好 run”把 72.6% 的干净重跑结果拼成 78.4% 的选择偏差，但原始归档已不在
当前仓库，正式论文必须复现后才能引用为结果。[E-LEGACY]

最强的当前案例来自受控的 reranker A/B：本地 cross-encoder 将 exact-turn
turn@30 coverage 从 77.012% 提至 92.468%，即增加 15.457 个百分点；同一设置的
端到端答题却从 83.70% 降至 83.64%，McNemar p=1.0，并在 temporal 类净损 9 题。
[E-008] 这说明“召回类指标显著改善”并不蕴含“最终回答改善”。另一个协议 A/B 中，
允许拒答的 83.70% 与强制作答的 84.22% 相差 0.52 个百分点，但收益在 temporal、
multi-hop、single-hop 与 open-domain 间方向不一；当前 flag 还同时改变 prompt，故它是
协议敏感性证据，不是纯拒答因果效应。[E-FA]

正式研究将以 2--3 个 answer 模型、每配置至少 5 次独立重复、冻结 transcript 的
重复判题，以及 LongMemEval 交叉验证来估计方差来源、选择偏差、代理指标到端到端
指标的转化率和协议敏感性。[P-EXP] 预期产出是一套“什么差异足以称为进步”的报告
规范，以及对 rerank/listwise 二段选择和拒答策略适用边界的实证分析。

## 贡献清单

### C1. 记忆 QA 的评测噪声分解

把端到端分数拆成固定检索结果后的答案生成方差、固定答案后的 judge 方差、运行时
后端漂移，以及题目/对话抽样方差。先导证据包括 5 次重复中约 21 个百分点的 run 间
摆幅、4.9%--27.9% 的 IDK 率范围，以及 7,154 对逐字相同预测上的 1.2% judge 翻转率。
[E-003]

**一手数据在哪**:

- tracked 实验日志:[`specs/003-bio-retrieval-locomo/eval-log.md`](../specs/003-bio-retrieval-locomo/eval-log.md#strike-0-calibration-baseline)。
- 历史摘要另报“两跑预测文本 59.5% 不同、judge 翻转 2.4%”，但当前树中找不到
  对应原始 run，二者也未证明与上面的 1.2% 是同一条件统计量；论文不得合并使用，
  只能在复现实验后择一报告。[E-LEGACY]

### C2. “每类取最好”造成的选择偏差

形式化 journal 拼接、best-of-run、反复调参后只报最好配置的乐观偏差。历史摘要记录
拼接分 78.4%、独立干净重跑 72.6%，差 5.8 个百分点；由于原始 tasks 已归档且不在
当前树中，这只是明确的复现目标，尚不是可投稿证据。[E-LEGACY]

**一手数据在哪**:

- 当前可追溯摘要:[`docs/memory-strategy.md` 决策二](./memory-strategy.md#决策二论文--要发方向定为评测可靠性分析文)。
- 摘要指向的原始 `openspec/changes/memory-hybrid-retrieval-locomo/tasks.md` 已归档，
  当前仓库和 git 历史均不可见。正式实验须从完整重复矩阵重新模拟选择过程。[P-EXP]

### C3. coverage 不等于 answer 的端到端反例

在相同 store、相同 top-k、相同 answer/judge 栈下，对比无 reranker 与本地
`bge-reranker-v2-m3`。coverage 增加 15.457 个百分点，端到端反而减少 0.06 个
百分点；三个非 temporal 类合计净救 8 题，但 temporal 净损 9 题，最终净损 1 题，
McNemar p=1.0。[E-008]

**一手数据在哪**:

- tracked 结果:[`specs/008-locomo-score-levers/eval-log.md` US4](../specs/008-locomo-score-levers/eval-log.md#us4-端到端答题-ab--reranker-coverage-是否兑现决胜)。
- 本机原始 coverage:[`.locomo-run/008-local-rerank/coverage.json`](../.locomo-run/008-local-rerank/coverage.json)。
- 本机原始配对结果:[`.locomo-run/008-us4-e2e/paired.json`](../.locomo-run/008-us4-e2e/paired.json)
  及同目录两臂 JSONL。原始目录被 gitignore，投稿前必须转成去凭据、可发布的补充材料。

### C4. 二段选择的负结果边界

论文不主张“reranker 无效”，而检验更窄的边界：当一段召回已经强、候选预算固定时，
通用 pairwise/pointwise cross-encoder 或 recall 导向的 listwise filter 可能按局部相关性
挤掉组合推理和时序上下文，从而净负。008 为 pairwise reranker 提供了完整端到端反例；
早期 listwise filter 的净负结论只有战略摘要，必须补跑后才能与 MemReranker 等正结果
对话。[E-008][E-LEGACY]

**一手数据在哪**:

- pairwise reranker:同 C3 的 coverage、配对 JSONL 与 US4 日志。[E-008]
- listwise filter:当前只有[`docs/memory-strategy.md` 决策二](./memory-strategy.md#决策二论文--要发方向定为评测可靠性分析文)
  的历史结论；实现入口仍在 `cmd/locomo-bench/filter.go`，但没有可投稿的原始结果。[E-LEGACY]

### C5. 协议差异与拒答校准会改变总分含义

当前 engram 83.70% 运行的 fingerprint 为 `force_answer=false`，允许 IDK；同一批
1,540 道可答题切到强制作答后为 84.22%，144 道题发生翻转、净增 8 题。分类别净值
为 temporal +5、multi-hop +3、single-hop +2、open-domain -2。[E-FA]
这说明拒答不是一个可从总分中忽略的实现细节。更早的两级 IDK 重试摘要还记录：
它在可答题上净救 12 题，却在 122 个对抗题错误中产生 121 个自信作答；该历史数字
没有当前原始 run，必须复现。[E-IDK]

**一手数据在哪**:

- force-answer tracked 台账:[`docs/locomo-score-levers.md`](./locomo-score-levers.md#force-answer-口径对齐-ab2026-07-22)。
- force-answer 原始数据:[`.locomo-run/008-force-answer/regime.json`](../.locomo-run/008-force-answer/regime.json)、
  [`stats.json`](../.locomo-run/008-force-answer/stats.json) 和 `results-hybrid.jsonl`；off 臂来自
  `.locomo-run/008-us4-e2e/results-hybrid.jsonl`。[E-FA]
- 两级 IDK 重试的 tracked 决策记录:[`specs/003-bio-retrieval-locomo/research.md` D4](../specs/003-bio-retrieval-locomo/research.md#d4-拒答校准--冲突消解strike-3)；
  原始 run 当前不可见。[E-IDK]

## 证据登记表与可信度

全文实验数字必须跟一个证据编号。证据等级不是结果好坏，而是当前可审计程度。

| 编号 | 内容与关键数字 | 一手位置 | 当前等级 / 可声明边界 |
|---|---|---|---|
| **E-003** | 5 repeats；每臂 1,540 题；run 间摆幅约 21pp；IDK 4.9%--27.9%；7,154 对同文预测上 judge 翻转 1.2%；剔除 IDK 后仍约有 +/-5pp 漂移 | [`003 eval-log`](../specs/003-bio-retrieval-locomo/eval-log.md#strike-0-方差诊断测量协议修正的依据) | **B: tracked 日志**。原始 `.locomo-run/strike0` 不在当前树；且日志把大幅漂移归因于中转站后端质量，不能直接等同于纯采样温度噪声 |
| **E-LEGACY** | 单跑按类别约 +/-3--5pp；预测文本差异 59.5%；judge 翻转 2.4%；journal 78.4% vs 干净重跑 72.6% | [`memory-strategy.md` 决策二](./memory-strategy.md#决策二论文--要发方向定为评测可靠性分析文) | **C: 一手团队摘要**。原始归档不在当前树；只能用于立题和复现实验设计，不能作为终稿主结果 |
| **E-008** | turn@30 77.012% -> 92.468%（+15.457pp，n=1,532）；端到端 83.70% -> 83.64%（-0.06pp，n=1,540）；flips 79/78；p=1.0；temporal 净 -9 | [`008 eval-log`](../specs/008-locomo-score-levers/eval-log.md#us4-端到端答题-ab--reranker-coverage-是否兑现决胜) + `.locomo-run/008-*` | **A-: 当前可复算先导数据**。但端到端每臂仅 1 run，不能估计跨 run 效应分布 |
| **E-FA** | force off 83.70% -> on 84.22%（+0.52pp）；144 flips；净 +8；分项 +5/+3/+2/-2 | [`locomo-score-levers.md`](./locomo-score-levers.md#force-answer-口径对齐-ab2026-07-22) + `.locomo-run/008-force-answer/` | **A-: 当前可复算先导数据**。每臂仅 1 run，且 force flag 同时更换 prompt，不能称为纯 policy 因果效应 |
| **E-IDK** | 两级 IDK 重试在可答题净 +12；对抗题 122 错中 121 个自信作答 | [`003 research.md` D4](../specs/003-bio-retrieval-locomo/research.md#d4-拒答校准--冲突消解strike-3) | **C+: tracked 决策摘要**。缺当前原始 run，必须复现 |
| **E-PROTOCOL** | LoCoMo cat 1--4 共 1,540 题；OmniMemEval 与 engram 在分母/类别/聚合轴可对齐，但检索预算、answer 模型等仍不同；008 off 臂另有 force-answer 差异 | [`competitive-benchmarks.md` §§4,6](./competitive-benchmarks.md#4-口径核对结论2026-07-21读-omnimemeval-源码实证)、[`runner.go`](../cmd/locomo-bench/runner.go) | **B: 源码审计 + 当前 fingerprint**。应写“部分轴已对齐但完整系统分不可直接归因”，不能写成“所有协议轴都不同” |
| **X-MEMOS / X-OMNI** | 本地一手来源审计记录旧版 `memos-0630` LoCoMo 73.31；当前 alphaXiv v4 论文 Table 3 报 MemOS-1031 75.80（统一 GPT-4o-mini）；当前官方仓库另报 LoCoMo 88.83、LongMemEval 89.20，OmniMemEval 仓库称覆盖 14 个商业记忆产品和 10 个数据集 | [`memory-strategy.md` MemOS 时间线](./memory-strategy.md#关键时间线8883-不是一次做到的)、[MemOS v4 论文](https://arxiv.org/abs/2507.03724)、[MemOS 官方仓库](https://github.com/MemTensor/MemOS)、[OmniMemEval 官方仓库](https://github.com/MemTensor/OmniMemEval) | **X: 外部一手来源 + 本地源码/版本审计**。当前 v4 论文不再以 73.31 为主表结果，也不包含 88.83/89.20；73.31 -> 75.80 -> 88.83 只能作为版本/regime 审计案例，不能作为噪声证据 |

`pp` 表示 percentage points。表中 `+/-` 仅为 ASCII 记法，不代表已经完成正式方差估计。

## 研究问题与可证伪假设

### RQ1: 单跑 LoCoMo 分数有多稳定，噪声主要来自哪一层？

- **H1a**:同一检索产物、同一 answer 模型的独立生成重复会产生非忽略的分数方差。
- **H1b**:冻结预测文本后重复 judge 的方差小于重新生成答案的方差。
- **证伪条件**:若在所有入选 answer 模型上，单跑 95% 预测区间均窄于待检测效应，
  或 judge 方差与 answer 方差同量级，则分别否定 H1a 或 H1b。
- **先导依据**:[E-003]；59.5%/2.4% 仅作待复现目标。[E-LEGACY]

### RQ2: best-of-run 与按类别拼接会虚高多少？

- **H2**:从同一批重复中挑选最高 overall 或逐类别最高值，会相对预先指定的独立
  holdout run 产生系统性正偏。
- **证伪条件**:跨模型、跨配置的 optimism gap 置信区间覆盖 0，且无稳定正方向。
- **先导依据**:历史 78.4% vs 72.6% 线索。[E-LEGACY]

### RQ3: 检索代理指标改善是否能预测端到端改善？

- **H3**:turn@k coverage 的正增益与最终 QA 增益相关但不充分；相关性会被题型调节，
  temporal 尤其可能因局部相关性重排而受损。
- **证伪条件**:跨模型、配置、类别的 coverage delta 能稳定、校准良好地预测 answer
  delta，且不存在方向翻转。
- **先导依据**:+15.457pp coverage -> -0.06pp answer，temporal 净 -9。[E-008]

### RQ4: 二段选择何时有效，何时伤害？

- **H4**:二段选择的收益取决于一段召回强度、候选预算和查询类型；通用相关性
  reranker 在强一段召回下容易损伤需要证据集合或时间顺序的题。
- **证伪条件**:reranker/listwise 在不同一段召回强度上均稳定正向，且题型交互不显著。
- **先导依据**:pairwise 反例 [E-008]；listwise 结论待复现 [E-LEGACY]。

### RQ5: 协议选择会不会改变系统排序或结论？

- **H5**:force-answer、judge 规则、answer 模型、检索预算和类别纳入方式中的任一改变，
  都可能产生与小算法改进相当的分数变化，并可能改变类别结论或系统排序。
- **证伪条件**:协议矩阵中的系统排序和 effect size 对上述轴稳定。
- **先导依据**:force-answer 总体 +0.52pp 但 open-domain 净 -2 题 [E-FA]；历史 IDK
  收益/编造张力 [E-IDK]；源码口径审计 [E-PROTOCOL]。

### RQ6: 结论能否从 LoCoMo 外推到 LongMemEval？

- **H6**:绝对噪声大小和类别敏感性会变，但“多跑才足以判断小改进”“coverage 不保证
  answer”“协议必须完整披露”三条结论可在 LongMemEval 上复现。
- **证伪条件**:LongMemEval 上单跑稳定、代理指标稳定转化，或协议变化不影响结论。
- **当前状态**:LongMemEval 最终验证在 003 日志中仍为空，当前没有 engram 一手结果；
  这是必补实验，不得在摘要中暗示已经完成。[P-EXP]

## 章节骨架

### 1. 引言

1. 用“已发布的小改进是否超过测量误差”而非“谁又高了几分”提出问题。
2. 解释长期记忆 QA 分数实际测量的是整条 pipeline，不是单独的 memory engine。
3. 以 US4 的 coverage/answer 方向翻转作为开场案例。[E-008]
4. 以 MemOS 从旧版 73.31、当前 v4 论文 75.80 到仓库自报 88.83 的版本演进说明：没有完整 regime、
   重复和方差报告，读者无法把跨版本差额分解为算法、模型、harness 或随机性。
   [X-MEMOS][X-OMNI] 这里不得使用“噪声收割”作既成归因。
5. 给出本文贡献和报告规范。

### 2. 背景：一个分数经过了哪些随机变量

定义评测流水线:

`conversation -> memory write -> retrieval -> optional second-stage selection -> answer -> abstain/force policy -> judge -> aggregation`

逐轴记录必须冻结或报告的 regime:

- 数据版本、切片、类别纳入与题目数；
- memory 写入/抽取模型及 revision；
- retrieval 模式、top-k、per-kind quota、reranker/listwise 配置；
- answer 模型、revision、temperature、top-p、prompt hash；
- `force_answer`、IDK retry 与拒答记分；
- judge 模型、prompt hash、解码参数和重判次数；
- micro/macro 聚合、重复次数、挑选或停止规则。

用 [E-PROTOCOL] 说明一个诚实细节：OmniMemEval 和 engram 的 LoCoMo 分母、类别与
聚合轴可以对齐，不应笼统称“完全不同协议”；但 008 的 83.70% 仍允许拒答，且跨论文
answer 模型、检索预算、judge revision 等未必相同，因此绝对分仍不能直接归因给记忆层。

### 3. 方法：可靠性审计协议

#### 3.1 因子设计

- **answer 模型**:2--3 个。至少包含 008 当前锚点
  `Qwen/Qwen3.6-35B-A3B-FP8` [E-008]，再选 1--2 个不同提供方或规模的模型；选定后
  pin 精确 revision，不以“最新模型”作配置。
- **重复**:每个完整配置至少 5 次独立 answer run；所有候选配置使用相同题序和
  block 时间窗，避免把服务端昼夜漂移误当算法效应。[P-EXP]
- **检索臂**:固定 hybrid baseline、hybrid + 本地 reranker、hybrid + listwise filter。
  reranker 与 listwise 的候选池、最终预算相同；不得拿不同 top-k 冒充算法差异。
- **协议臂**:拒答/强制作答分开；严格 judge/mem0-aligned judge 用同一冻结 transcript
  重判；类别纳入和聚合只做分析，不混成一个新 leaderboard 数。
- **基准**:LoCoMo 为主，LongMemEval 为外部交叉验证；二者使用各自官方题型，不把
  类别名相似误当完全同分布。[P-EXP]

#### 3.2 噪声分解

1. 冻结 store 和 retrieval hits，多次生成答案，估计 answer/run 方差。
2. 冻结每条预测文本，多次调用同一 judge，估计 judge 方差。
3. 至少用一个第二 judge regime 重判同一 transcript，估计规则/模型敏感性。
4. 以 conversation 为 cluster 做 bootstrap；同时报告 run-level 分布和 item-level 配对
   结果，避免把同一长对话内的问题当独立样本。
5. 用交叉随机效应模型或等价方差分解，把 answer model、run、judge repeat、question、
   conversation 的贡献分开；正文同时给出不依赖模型假设的 bootstrap 区间。

#### 3.3 选择偏差实验

从完整重复矩阵离线模拟以下报告策略:

- 预先固定的单跑；
- 多跑均值；
- best overall run；
- 每类别取最好后拼接；
- 顺序试验，看到“涨点”即停止。

选择只在 tuning repeats 上发生，偏差用未参与选择的 holdout repeats 衡量。主结果是
`reported score - holdout score` 的分布，而不是再展示一个更高的拼接分。历史 5.8pp
差额只作复现靶点。[E-LEGACY]

#### 3.4 coverage 到 answer 的转化

对每个 question、category、answer model 同时记录:

- exact-turn turn@k 与 session recall；
- gold evidence 是否进入最终 answer context；
- answer correctness；
- baseline/treatment 的 false->true 与 true->false 翻转；
- temporal 题中被 reranker 移除或降位的时序证据。

主分析不是只做相关系数，而是估计 `Delta answer ~ Delta coverage * category * answer model`
的交互，并报告“coverage 上升但 answer 不变/下降”的象限比例。US4 是一个预先存在的
案例，不得在同一数据上先看结果再发明阈值。[E-008]

#### 3.5 协议敏感性与拒答前沿

- 把 `force_answer` 与 prompt 文本拆成两个独立因子，修复当前 A/B 的混淆。[E-FA]
- 同时报 answerable accuracy、adversarial refusal accuracy、false refusal、fabrication
  与按题型分解，不用一个 overall 隐藏取舍。
- 将强制作答、提示式拒答、两级 IDK retry 画成 operating frontier；历史“+12 可答 /
  121 of 122 自信编造”只作为待复现点。[E-IDK]
- judge 对拒答的规则必须明确：可答题 IDK 是否必错、false-premise 拒答是否算对、
  cat-5 是否进入主分。不同定义的分数分表报告。

#### 3.6 统计与判定规则

- 报告每配置每次 run，不只报告均值。
- 报告均值、标准差、95% CI、effect size 和 paired flips；p 值不能替代 effect size。
- 两臂同题配对用 McNemar 或适合重复/cluster 结构的扩展；conversation-cluster
  bootstrap 作为稳健性检查。
- 预先给出最小关心效应，并做 power analysis；“小于 5pp”是待检验的领域命题，
  不是把 5pp 写死成统一显著性阈值。[P-EXP]
- 多配置、多类别比较做 multiplicity 控制，或者把探索性结果明确标为探索性。

### 4. 结果章节预期表格

#### 4.1 单跑稳定性

表:每个 answer 模型 x 配置的 run 分数、均值、标准差、95% CI、最小值、最大值。
图:同一配置的 raincloud/point-range；不画只展示最佳 run 的柱状图。

#### 4.2 方差来源

表:answer generation、judge、conversation/item、后端时间块的方差贡献与区间。
对照 [E-003] 的 1.2% 同文 judge 翻转和历史 2.4% 摘要，但只使用新实验的统一定义。

#### 4.3 选择偏差

图:单跑、多跑均值、best-run、按类拼接相对 holdout 的 optimism gap。正文回答历史
78.4% vs 72.6% 是否可复现。[E-LEGACY]

#### 4.4 代理指标错位

表:每个 retrieval 臂的 coverage delta、answer delta、McNemar、类别交互。
案例框:US4 的 +15.457pp -> -0.06pp 与 temporal 净 -9。[E-008]

#### 4.5 二段选择边界

按一段 recall 分桶，比较 pairwise reranker、listwise filter 和不做二段选择；报告正、
零、负区域，而不是只报平均最优臂。与 MemReranker 的 temporal/causal reasoning 目标
做直接对照。[X-RERANK]

#### 4.6 协议敏感性与拒答

协议矩阵:force/abstain、judge regime、answer model、category policy。展示同一系统在
不同协议下的绝对分和排序变化。将当前 +0.52pp、144 flips、分项方向冲突作为先导点，
不作为多跑结论。[E-FA]

#### 4.7 LongMemEval 交叉验证

复用同一审计框架，重点看 multi-session reasoning、temporal、knowledge update 和
abstention。报告哪些结论复现、哪些只属于 LoCoMo；不要求两个基准的绝对分同尺度。

### 5. 讨论

1. **对论文作者**:小改进必须配多跑分布、paired effect 和完整 regime；coverage 只能
   作诊断，不能代替端到端 verdict。
2. **对 benchmark 维护者**:发布 machine-readable evaluation card，明确 answer/judge
   模型、prompt hash、force-answer、类别纳入、预算和重复策略。
3. **对 leaderboard**:同协议横评有价值；跨协议、跨版本的绝对分变化不得自动归因于
   memory architecture。
4. **对系统开发者**:负结果不是“reranker 永远无用”，而是指出 query type 与一段召回
   强度决定适用边界。
5. **局限**:engram 是单一实现；LoCoMo 对话数有限；answer/judge 提供方可能漂移；
   旧证据部分缺 raw artifact；LongMemEval 尚未跑。所有局限必须保留到终稿。

### 6. 报告规范（论文的可复用产物）

建议附一张 Evaluation Reliability Card，至少包含:

- dataset revision、split、question/category counts；
- memory system commit 与配置指纹；
- extraction/retrieval/answer/judge 的模型 revision 和 prompt hash；
- decoding、并发、重试、超时与失败处理；
- top-k、candidate pool、final context budget；
- force-answer/IDK/adversarial 规则；
- repeats、随机种子或不可控随机性说明、运行时间块；
- 全部 per-run/per-question 结果和选择规则；
- 均值、区间、paired test、cluster 处理和多重比较方法。

## 必补实验与最小成稿门

### P-EXP: 必做

- **多模型多跑**:2--3 个 answer 模型 x 每个完整配置至少 5 次独立重复；008 的单跑
  只可作为预注册案例，不可直接升级成总体结论。
- **judge 分解**:冻结 transcript 后重复判题，统一定义“judge flip”；同时重跑
  answer 生成，才能比较 answer 与 judge 方差，解决 1.2%/2.4% 口径不明问题。
- **journal 复现**:从完整重复矩阵重建 best-run 和 per-category stitching，并用未参与
  选择的 holdout runs 测 optimism gap。
- **二段选择矩阵**:baseline、pairwise reranker、listwise filter 在相同候选池和最终
  上下文预算下比较；至少覆盖不同一段 recall 区间和 temporal/non-temporal 分层。
- **force-answer 解混**:把“是否允许拒答”与“prompt 文本”拆成两个因子；复现当前
  0.52pp/144-flip 先导观察。[E-FA]
- **LongMemEval 交叉**:至少增加一个 LongMemEval 设置，使用与 LoCoMo 相同的多跑、
  transcript 冻结和协议登记纪律。LongMemEval 原论文包含 500 个精编问题；若使用
  LongMemEval_S 或其它子集，必须明确命名，不能简称为全量。[X-LME]

### 成稿硬门

- 旧归档缺失的 59.5%、2.4%、78.4/72.6、+12 与 121/122 全部已复现，或从终稿结果
  与摘要中删除。[E-LEGACY][E-IDK]
- 所有主结论至少跨 2 个 answer 模型成立；模型异质结果写成边界，不做平均掩盖。
- LoCoMo 与 LongMemEval 至少共享一条可复现结论，否则标题和外推范围收窄到 LoCoMo。
- 发布去凭据的 per-run、per-question、retrieval coverage、transcript、judge verdict、
  regime fingerprint 和分析脚本。
- 不以 best run、拼接分或仅 coverage 的增益作 headline。

## 相关工作定位

### 长期对话记忆基准

- [LoCoMo](https://arxiv.org/abs/2402.17753)提供长对话 QA、事件总结和多模态生成任务，
  是本文主审计对象。[X-LOCOMO]
- [LongMemEval](https://arxiv.org/abs/2410.10813)覆盖信息抽取、跨 session 推理、时序、
  knowledge update 与 abstention，并提供 500 个精编问题；本文用它检验结论是否只属于
  LoCoMo。[X-LME]

本文不再发明一个新 benchmark，贡献是审计现有 benchmark 的测量可靠性、代理指标
有效性和报告协议。

### MemOS 与 OmniMemEval（必引，角度互补）

- [MemOS](https://arxiv.org/abs/2507.03724)提出 memory OS 架构；当前 alphaXiv v4 论文
  Table 3 在统一 GPT-4o-mini 下报告 LoCoMo 75.80。88.83/89.20 是后续官方仓库结果，
  不能写成论文表格数字。[X-MEMOS][X-OMNI]
- [OmniMemEval](https://github.com/MemTensor/OmniMemEval)把 ingestion、retrieval、answer
  generation、LLM-as-Judge、聚合与报告纳入统一 pipeline，并支持 LoCoMo、LongMemEval
  等用户记忆评测及 agent memory 评测。这里引用的是评测仓库，不是独立论文。[X-OMNI]

两者回答的是“在统一 harness 下，哪些 memory backend/agent 表现更好”；本文回答的是
“一次分数是否稳定、方差来自哪一层、代理指标是否转化、哪些协议轴必须披露”。因此是
互补而非反驳。MemOS 73.31 -> 75.80 -> 88.83 的版本演进是重要 case study，但因为旧
revision、当前论文和后续仓库结果之间的版本、backbone 与 harness 没有全部冻结，它只证明
版本化 regime 审计的必要性，不能被写成“他们的提升是噪声”。[X-MEMOS][X-OMNI]

### Reranker-for-memory

[MemReranker](https://arxiv.org/abs/2605.06132)明确指出通用语义 reranker 在 temporal、
causal reasoning 和对话消歧上的不足，并训练 reasoning-aware reranker；其正结果主要
落在 memory retrieval metrics。[X-RERANK] 本文的 US4 与它直接对话：不是否定专用
reranker，而是要求任何 retrieval 正结果继续证明端到端 answer 转化，并报告 temporal
等类别上的负迁移。[E-008]

### Agentic 随机性与 LLM judge 可靠性

[On Randomness in Agentic Evals](https://arxiv.org/abs/2602.07150)在另一类 agent benchmark
上显示单跑 pass@1 可随选中的 run 波动 2.2--6.0 个百分点，2--3 个百分点的改进可能是
噪声，并建议多次独立运行与 power analysis。[X-RANDOM] LLM-as-a-judge 可靠性工作则
研究固定输出后的判题一致性。本文的区别是把二者放入长期记忆 QA 的同一流水线，并加入
journal 选择偏差、coverage/answer 转化和拒答协议三个 memory-specific 问题。

### 记忆/RAG 论文中的不确定性报告实例

[EcphoryRAG](https://arxiv.org/abs/2510.08958)的 Table 1 对自身结果报告 10 次运行的
`mean +/- std. dev.`，但同表 baselines 只运行一次。[X-ECPHORY] 这不是“其增益属于噪声”
的证据，而是同表各方法拥有不对称不确定性可见度的实例，支持本文要求比较双方使用相同
重复预算。[FadeMem](https://arxiv.org/abs/2601.18642)的 Table 3 报告 LoCoMo multi-hop
F1 29.43，相对 Mem0 的 28.37 高 1.06；Table 3 表注及相邻结果正文未报告该比较的
重复次数或不确定性。[X-FADE] 本文只把它作为“小差异为什么需要方差信息”的审计样本，
不据此否定方法有效性。

[`memory-strategy.md` 附二](./memory-strategy.md#对论文线决策二的额外弹药)
还记录了 D-MEM 的 run-count 对称性和 SynapticRAG 的超参数敏感性线索；本轮 alphaXiv
核验未把前者的重复设计、后者与 `tau_scale` 对应的精确实验条件同时钉牢，因此两项不进入
当前论证和引用表。终稿若使用，必须先补齐论文页码、表号、运行次数和可复述的比较条件。

## 投稿定位

### 首选 track 类型

**NeurIPS/ICLR 下一轮主会中与 evaluation、datasets/benchmarks、analysis 或
reproducibility 对口的 track。** 这是按[`memory-strategy.md` 决策二](./memory-strategy.md#决策二论文--要发方向定为评测可靠性分析文)
给出的投稿类型，不是对某一届 CFP、track 名称或截止日期的实时声明。本任务没有通过允许的
渠道核验会议信息，因此不写具体年份和 deadline；投稿前必须用获准的官方渠道重新确认。

### 备选

- ICLR/NeurIPS 下一轮与 agents、memory、evaluation 或 reproducibility 对口的 workshop；
  workshop 名称和 CFP 必须在投稿时按官方页面重新核验。
- 若多模型、多跑和 LongMemEval 未完成，只投 workshop/short analysis，不包装成完整的
  领域级可靠性结论。
- 不按“新记忆方法”投主会 methods track；没有新架构贡献，强行包装会削弱论文。

### 投稿叙事

主标题和摘要强调 **measurement reliability**，副标题/案例才提 LoCoMo。审稿价值不是
“engram 分数高”，而是:

1. 给出可复现的噪声分解；
2. 量化选择偏差；
3. 证明 retrieval surrogate 可能与 answer 失配；
4. 提供跨论文可比所需的 evaluation card；
5. 给出负结果适用边界，而不是再堆一个单跑 SOTA。

## 外部引用编号

- **X-LOCOMO**: Maharana et al., *Evaluating Very Long-Term Conversational Memory of LLM Agents*, [arXiv:2402.17753](https://arxiv.org/abs/2402.17753).
- **X-LME**: Wu et al., *LongMemEval: Benchmarking Chat Assistants on Long-Term Interactive Memory*, [arXiv:2410.10813](https://arxiv.org/abs/2410.10813).
- **X-MEMOS**: Li et al., *MemOS: A Memory OS for AI System*, [arXiv:2507.03724](https://arxiv.org/abs/2507.03724). alphaXiv 核验版本为 v4（2025-12-03）。
- **X-OMNI**: MemTensor, [MemOS 官方仓库](https://github.com/MemTensor/MemOS)与 [OmniMemEval 官方仓库](https://github.com/MemTensor/OmniMemEval)；它们是软件/评测 artifact，不冒充独立论文。
- **X-RERANK**: Li et al., *MemReranker: Reasoning-Aware Reranking for Agent Memory Retrieval*, [arXiv:2605.06132](https://arxiv.org/abs/2605.06132).
- **X-RANDOM**: Bjarnason et al., *On Randomness in Agentic Evals*, [arXiv:2602.07150](https://arxiv.org/abs/2602.07150).
- **X-ECPHORY**: Liao, *EcphoryRAG: Re-imagining Knowledge-Graph RAG via Human Associative Memory*, [arXiv:2510.08958](https://arxiv.org/abs/2510.08958).
- **X-FADE**: Wei et al., *FadeMem: Biologically-Inspired Forgetting for Efficient Agent Memory*, [arXiv:2601.18642](https://arxiv.org/abs/2601.18642).
