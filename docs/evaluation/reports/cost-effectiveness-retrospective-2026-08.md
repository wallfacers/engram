---
title: 成本-涨点复盘（2026-07 → 2026-08）
summary: 算清 eval/训练/云杠杆花了多少钱、买到什么；提炼五条规律解释「涨点没涨」；收敛剩余真正可探索的窄方向（answerer 侧 agent 化 / 写入侧 tree / 评测纪律）与 arXiv 定向调研边界。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-05
canonical_for: [cost-effectiveness-retrospective]
tags: [evaluation, strategy, cost, retrospective, verdicts]
---

# 成本-涨点复盘（2026-07 → 2026-08）

本文回答一个问题：**花了很多钱/机器时间，为什么端到端涨点没涨？** 先算账，再提炼规律，
最后收敛「剩下真正值得探索的窄方向」——避免下一轮继续盲目烧钱。

## 1. 成本账目（分类 + 量级；未逐项记账处如实标注）

### 1.1 付费云端杠杆（直接烧钱）

| 项 | 花费量级 | 结果 |
|---|---|---|
| 008 云端 reranker（gte-rerank-v2） | ~¥150/天、数天（有记录） | coverage +15.5pp → **e2e −0.06pp NO-GO**；催生 DEATH RULE |
| deepseek-v4-pro 探针（LME 500 + LoCoMo） | LME ¥4.7（缓存 96%，有记录） | 89.03% / 86.00%——**唯一「花钱买到真信息」**：answerer 是天花板 |
| tokensfree relay | 白烧（有记录） | 封禁，已禁 |

### 1.2 云 GPU（AutoDL 机器时间）

| 项 | 量级 | 结果 |
|---|---|---|
| 023 QLoRA 训练 r2→r5 + 三臂全量 + residual 先导 | 多轮全量，每次 3–4h（未逐项记账） | **planner_error 149/149，训练效果从未进入测量** |
| 022/026/027 全量评测轮次 | 多轮 | 027 修复打包粒度 → baseline 摆正 |

### 1.3 本地免费轮次（时间成本）

| 项 | 量级 | 结果 |
|---|---|---|
| 检索/表示侧 6+ 机制杠杆 | 008/010/011/012/013/014/017/021/024/025/026 | **全部 NO-GO** |

## 2. 净涨点账

| 杠杆 | Δ | 类型 |
|---|---|---|
| bge-large embedder（008 US3） | +1.3pp（repeats=3 CI 外坐实） | **唯一端到端转化的机制涨点** |
| 027 打包粒度（chunk-verbatim fold + cap 5000） | +2.1pp（OVERALL 口径） | 修 baseline 正确性，非机制涨点 |
| cat-top-k | +0.9pp | 降级兜底 |
| **LoCoMo B1 正式基线（净）** | **85.19%** | — |

结论：从「机制涨点」获得的分极少，大部分是「修口径 + 换 embedder」。成本与净涨点
**严重不对称**。逐条 verdict 见 [实验裁决索引](../experiment-verdicts.md)，分数正本见
[当前评测结果](../results.md)。

## 3. 五条规律（为什么没涨）

1. **008 铁律——中间信号不转化端到端（最贵的一条）**：reranker +15.5pp coverage →
   e2e −0.06pp；episode 聚类 multi-hop 84.0% → overall **−7.7pp**。检索/表示侧每次
   「覆盖率提升」都被 answerer 的用途稀释。
2. **预算才是分，机制不是**：+3.20pp 同栈领先 = 3.4× 上下文预算的答题优势；对齐
   MemOS 预算（≈1083 tok）后 **−5.62pp 极显著落后**（p=0.000006）。flat 检索没在
   机制上赢过 tree/graph（详见 [预算剥离](budget-ablation.md)）。
3. **answerer 是硬天花板**：同检索 Qwen → pro = **+4pp**（85→89）。本地小模型侧所有
   打磨都撞这面墙。
4. **训练杠杆不可控**：LoRA 训练投入 100% 沉没，因为验证的是「生成合法性」
   （proposal 被 ValidateAction 拒绝）而非端到端效果——**烧了训练的钱，却没买到任何
   因果测量**。
5. **口径是隐藏噪声源**：judge 20pp 级伪差、环境漂移 0.4–1.4pp、answerer temp=1.0
   跨 run 噪声——大量单次差值落在噪声标尺内（017 +0.62pp 教训）。个位数 pp 的
   「观察」不可信，必须先建立噪声标尺。

## 4. 方向收敛

### 4.1 不再做

- 本地口径再堆检索/表示/打包机制（6+ 次证伪，成本高、期望值低）。
- 花钱跑全量去「验证单次差值的再观测」——先立噪声标尺，差 ≤ 标尺则不算赢。
- 付费云 rerank/recall 涨点（DEATH RULE，非纯本地可移植）。
- 训练式本地小模型杠杆（023 教训：proposal 生成合法性都无法跨越，训练投入不进入测量）。

### 4.2 才值得做的窄方向

- **A. Answerer 侧 agent 化多轮推理（最强、从未动过）**：检索/Bundle 暴露为工具，
  answerer 从「单次读一包证据」升级为「检索—评估—收敛」。唯一逻辑上跨 89→95 的轴，
  且此前所有杠杆都在检索/表示/打包侧。**属 SaaS 层**（需云端强模型），本地口径不动。
- **B. 写入侧结构化记忆（tree/graph）**：021 收敛出「temporal 真差距在写入侧」。
  但 024/025 证明机械密度杠杆不涨点——真做需 MemOS 式 tree 的完整实现，代价高，
  且必须先找到**固定变量的端到端消融证据**（目前无一篇论文有）。
- **C. 评测纪律**：judge 噪声抽样审计（L0-4）、类别 non-regression gate（L0-3）、
  预算作为固定轴。低成本、防伪涨点。

## 5. arXiv 定向调研边界

lever-batch（[本地 vs SaaS 借鉴批次](../../research/lever-batch-local-vs-saas.md)）已逐篇
核实 12 篇。本调研只补缺口、不泛泛翻论文，三个定向问题：

1. **agent-loop 记忆**（多轮检索-评估-收敛）的**独立消融**证据——支撑方向 A；
2. **写入侧 tree/graph** 的**固定变量端到端消融**证据——支撑方向 B；
3. **judge 噪声**的定量研究（FN/FP 方向与幅度）——支撑方向 C。

任何新方向必须先落预注册口径、冻结臂、non-regression 门再跑；跨过噪声标尺且端到端
转化才算赢（008 铁律 + [低成本止损](../../research/paper-direction.md) 双重纪律）。

## 6. 调研结果（2026-08-05，alphaXiv 逐篇精读）

三定向问题逐一落地，方向 A/B/C 的升降级据此更新：

### 6.1 方向 A 降级：多步推理不是 LoCoMo 涨点来源

[MemCog](https://www.alphaxiv.org/abs/2605.28046) 是唯一给出固定变量消融的
「多步导航 vs 单次检索」系统，但其消融结论与直觉相反：**移除 Proactive 推理协议在
LoCoMo（被动 QA）上只掉 −0.35pp**（Rec@5 −14.63 只发生在它自造的主动触发评测
ProactiveMemBench）；真正掉分的是结构——移除 graph overlay −6.79pp、移除层级 −6.53pp。
即 **MemCog 92.98 的高分来自结构化记忆，不是来自「多步」本身**——多步导航只是承载
结构的执行手段。[Memory in the Loop](https://www.alphaxiv.org/abs/2607.05690) 是延迟工程
论证（in-process ~100μs vs 网络 ~110ms，固定每步预算下延迟改变任务结果），非 benchmark
涨点证据，且其小任务上「重述基线 5/5 > 记忆工具 3.6–4.8/5」。**结论：answerer 侧
agent 化多步推理在 LoCoMo/LongMemEval 被动 QA 上无干净支持，008 铁律同域成立。**

### 6.2 方向 B 升级：写入侧 event-centric 时序结构有干净消融（两份）

- [SEGTREEMEM](https://www.alphaxiv.org/abs/2606.04555)：temporal 有序 segment tree vs
  非时序相似聚类树——同 annotation prompt/embedding/检索/evidence budget，只变在线构建
  算法，三数据集一致更好；**temporal permutation（交换 30% turn 对）SEGTREEMEM 掉 0.111、
  非时序树只掉 0.020**，证明 temporal order 真的被利用。LoCoMo 类别上 multi-hop 0.580 /
  temporal 0.542（Best），远高于 Dense 0.421/0.385。
- [StructMem](https://www.alphaxiv.org/abs/2604.21748)：event 双视角抽取 + 时间锚定 +
  周期性跨事件 synthesis vs Flat/Graph（同 gpt-4o-mini backbone / embedding / judge）——
  **temporal 81.62 vs flat 78.50；post-hoc 实体图反而 76.64（比 flat 更低）**。
- **两篇共同且与 engram 既有证据（Mem0g multi-hop 有害、014/021）精确对齐：post-hoc
  实体-关系图不涨点、甚至伤 temporal；涨点的是写入侧的 event-centric 时序层级（segment
  tree / 双视角 event + consolidation）**。这正是 021 已收敛的「temporal 真差距在写入侧」。
- 注意不可比点：SEGTREEMEM 用 1986 全量口径（含 adversarial），StructMem/engram 用
  1540 cat 1–4；judge 均为 LLM-as-judge 但模型/口径不同（engram deepseek-v4-flash +
  mem0-aligned prompt）。只取机制证据，不取绝对分。

### 6.3 方向 C 升级：judge/口径噪声被定量坐实

[MemDelta](https://www.alphaxiv.org/abs/2606.29914)（LongMemEval-S，500 题，三模型家族）
给出四个「单变量即可翻转结论」的混淆：**只换 embedding（同 pipeline）+6.2pp（p=0.004），
翻转 Mem0 vs RAG 从 +11pp 到 −1.2pp**；同 S1-vs-S4 比较跨模型结论从 −31pp 到 +14pp
（Sonnet 63% full-context 错是显式拒答）；agent 自记忆 42% < 基本检索 47%；Mem0 匹配子集
与 cloud RAG 打平（p=1.0）但 **50× write-path 成本**。engram 已固定 embedding/judge/answerer
符合其协议，但**缺 cross-model-family 稳定性验证**（只测 Qwen 家族）——低成本可补。

### 6.4 收敛结论

| 方向 | 升降级 | 依据 |
|---|---|---|
| A（answerer 侧多步推理） | **降级**——多步本身无支持；若做走「SaaS 强模型 + 结构化检索」，不是多步导航 | MemCog 消融 −0.35pp |
| B（写入侧 event-centric 时序结构） | **升级**——唯一有干净固定变量消融、且命中已收敛缺口（temporal/multi-hop） | SEGTREEMEM + StructMem |
| C（评测纪律） | **升级**——已被定量背书；补 cross-model 稳定性即可 | MemDelta |

B 的落地约束：两篇都是 LLM 写入侧（event 抽取 / node summary）。engram 若做，需本地
sidecar 抽取 event——比 023 的 planner proposal 简单得多，但仍属写入侧改造，须先证明
端到端转化（008 铁律）。SaaS 层若启动，B 的结构可作其检索侧基底。
