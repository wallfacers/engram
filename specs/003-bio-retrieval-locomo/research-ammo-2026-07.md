# 弹药调研合成裁决（2026-07-19，三路并行调研）

> 调研触发：Strike 1 评测期间的方向预研。三路：时间检索机制、LoCoMo multi-hop
> 失分对症（PPR 备选评估）、LongMemEval_S 失分地形。本文档是设计裁决，
> 后续批次简报据此生成。

## 裁决 1：Strike 2（US4 时间结构化）设计修订

维持主体（规则解析 + T_score 软加权），实证背书：Mandol（arXiv 2606.29778）
纯规则路由 + 结构化事件链达 LoCoMo temporal **89.1%**（Mem0 仅 55.5%），
证明不需要时序 KG 或 LLM 参与检索。四条修订：

- **R1（最大杠杆，新增任务）**：temporal 专用**答题计划 prompt 分支**——查询
  意图分类结果接到答题侧，temporal 类注入固定 CoT：「列出候选 [event:
  YYYY-MM-DD] → 归一化 → 比较 → 输出绝对日期」。TRACE（2607.00339）消融：
  去掉此项 temporal 单项 **−17.6pp**，超过任何检索侧组件（去图层仅 ±0.2pp）。
- **R2**：意图分类增加 **current vs historical 状态位**（A-TMA 2607.01935：
  Zep 宿主 temporal F1 0.030→0.171）；historical 查询不压制 superseded 事实，
  current 查询压制——与 US5 冲突消解共用一个机制位。
- **R3**：T_score 形式定为 `exp(−α·gap)`（gap = 事件区间与查询窗口距离，重叠
  =0）；**软加权，禁止硬过滤**（DyG-RAG +18.3pp、IA-RAG +14.07pp 均为软方案，
  无硬过滤胜例）。次序题（before/after 锚点）用 SQL 方向谓词做**补充召回**
  （并集），不删语义候选。
- **R4**：事件时间归一化的两条防线：相对表述锚定同上下文最近绝对日期，无锚
  回退 session date 并标 fuzzy（不丢弃）；同 timestamp 事件不得推导次序。
  答题 prompt 强制绝对日期输出（现有 prompt 已有，保持）。

## 裁决 2：Strike 1 的 Plan B 换掉——PPR 降级，cluster-sweep 上位

- **PPR 不做**（除非 Strike 1 数据给出反证）。实证：Query-Aware Spreading
  Activation（2606.30133）深度扫描显示 depth 1→2 是最大增益、≥3 退化，带
  embedding 门控的游走反超 PPR 系 HippoRAG 2；PPR 与 depth-2 同样集中于种子
  邻域，**对枚举完备性无结构性帮助**，预测仅 +0~3pp。
- **multi-hop 真实病根 = 单轮 top-k 覆盖截断**：xMemory（2602.02007）量化
  LoCoMo 全证据覆盖平均需 **10.81 块**；EviMem（2604.27695）失败分析与我们
  「95% 失分为部分答案」完全同构。
- **新 Plan B（Strike 1.5 候选，若 S1 判定后 multi-hop 仍 <50%）**：
  **枚举意图 cluster-sweep**——检测枚举/计数/比较意图后不走 top-k 截断，对
  种子实体沿 co/syn 边取实体簇全部关联记忆，按 session 分组喂答题聚合。
  同思路系统（EviMem Edge 层）multi-hop 43.2%→**85.2%**。engram 的实体图
  就是现成簇索引，零新增 LLM 依赖。multi-hop 基线 29% 是最大失分洼地，
  cluster-sweep 是唯一攻击"覆盖不足"本身的方案。

## 裁决 3：LongMemEval_S 冲刺清单（按性价比排序）

地形（各家 per-type 实测）：ss-user 接近饱和；**ss-assistant 是抽取式系统
专属坑**（Zep 80.4 vs full-context 94.6，LightMem 低至 19.6，verbatim 存储的
Emergence 直接 100）；preference 桶 rubric 判分（Zep 56.7 vs Mem0 96.7）；
temporal-reasoning 弹性最大（无时间结构 ~60，有则 95+）；knowledge-update
的教训：**仅存储不压制会倒扣**（Mem0 保留历史 −2.6pp）；abstention 30 题
（`_abs` 后缀）judge 只要求"识别出不可答"。

- **白给分 1**：ss-assistant 口径——assistant 轮次 verbatim 存储 + 命中后整
  session 召回（参照差距 17.7~26pp，零算法成本）。
- **白给分 2**：读取 prompt 两件套——JSON 结构化 + Chain-of-Note（原论文
  +10 绝对分）+ 一行 abstention 分支指令（吃下 30 题大部分）。
- 四枪映射确认：时间结构化→temporal-reasoning + knowledge-update；冲突消解
  →knowledge-update（Zep 型"一律信新"边失效即够 83+，检索时压制是关键——
  与 US5 的 superseded_by + penalty 设计一致，实证背书）；拒答校准→abstention；
  联想图→multi-session（Mem0 靠 top_50 大召回拿 93.2，弱证据支持大 top-k）。

## 任务影响汇总

| 影响 | 内容 |
|------|------|
| US4 批次（T023-T028） | 按 R1-R4 修订：新增 temporal 答题计划任务；意图分类加状态位；T_score 定式 exp(−α·gap)；次序补充召回 |
| Strike 1 判定后 | multi-hop <50% 时立 Strike 1.5（cluster-sweep），PPR 从备选清单移除 |
| LME_S 批次（T037 前） | 新增：assistant verbatim 存储口径、CoN 读取 prompt、abstention 分支指令（均为口径改动，独立 commit） |

来源索引：Mandol 2606.29778 / TRACE 2607.00339 / A-TMA 2607.01935 /
DyG-RAG 2507.13396 / IA-RAG 2606.06044 / HippoRAG2 2502.14802 /
QASA 2606.30133 / EviMem 2604.27695 / xMemory 2602.02007 /
LongMemEval 2410.10813 / Zep 2501.13956 / Mem0 temporal blog / Emergence blog
