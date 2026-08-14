---
title: 评测结果与探索历程
summary: 当前分数正本 + 2026-07→08 评测探索历程全集——按时间记录每个实验"做了什么、验证/证伪了什么假设、如何收敛到 unified 契约"。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
canonical_for: [evaluation-results]
tags: [evaluation, locomo, longmemeval, results, exploration-journal]
---

# 评测结果与探索历程

本文是 engram 评测的**全集记录**：前半给出当前分数正本（你要的结论），后半按时间线给出完整探索历程（这些结论是怎么来的）。逐项实验裁决速查见 [experiment-verdicts.md](experiment-verdicts.md)，跨 recipe 全量矩阵与重跑判定见 [result-matrix.md](result-matrix.md)，运维复现见 [LoCoMo runbook](../operations/evaluation/locomo-runbook.md)。

---

## 一、当前分数（正本）

**唯一允许的 answer prompt 是 unified answer contract**（数据集无关统一契约，零 per-dataset/category 特调）。以下分数均为其严格配对验证：

| 基准 | 契约 | 答题模型 / 判题模型 | 得分 | p | 判定 |
|---|---|---|---:|---:|---|
| LoCoMo（1,540）· top-k 30 | **unified** | Qwen3.6-35B-A3B-FP8 / deepseek-v4-flash | **87.9%** | 0.019 | above-noise · context parity ✓ |
| LoCoMo（1,540）· top-k 150 | **unified** | Qwen3.6-35B-A3B-FP8 / deepseek-v4-flash | **91.43%** | — | 042 配对 · within-noise（离线 clean 重判）|
| LongMemEval-S（500）· top-k 30 | **unified** | Qwen3.6-35B-A3B-FP8 / deepseek-v4-flash | **90.2%** | 0.000112 | above-noise · context parity ✓ |
| LongMemEval-S（500）· top-k 150 | **unified** | Qwen3.6-35B-A3B-FP8 / deepseek-v4-flash | **92.0%** | — | 3-rep clean majority · context parity ✓（[补跑记录](../operations/evaluation/lme-unified-k150-3rep-2026-08-15.md)）|

> 口径：全部 **clean 判题**（`extractFinalAnswer` 剥离 thinking，只判 final answer）。clean 是唯一可跨批比较的口径——judge 从 thinking 前导读候选的作弊/被 thinking 误导均为评测伪影，raw 跨批漂移 ±3.4pp、clean 跨批稳定 ±0.3pp（042 实证）。top-k 150 行是 042 配对 run 的离线 clean 重判（control legacy@k150 同口径 91.36%，Δ+1 题），印证两机制正交：unified 修 prompt 病、top-k 150 修上下文量，unified 不威胁高分栈。LME@k150 行是 2026-08-15 补跑的 unified 3-rep（91.0/92.0/91.8，clean majority 92.0%），对照 control 1-rep clean 87.0%（非严格配对，control 仅为参考）。

### 参考行（非 unified 主路径，仅追溯）

| 数据集 | 得分 | 说明 |
|---|---:|---|
| LoCoMo · trace（默认路径，unified 之前的默认） | 85.91% @ 468 tok | 读侧证据中介，answer context 省 7.7×；当前已被 unified 契约行取代为主路径 |
| LoCoMo · B1 正式基线（022 冻结协议） | 85.19% | chunk-verbatim fold + cap 5000；历史默认基线，见 [022 评估记录](reports/benchmark-parity-memory-architecture.md) |
| LoCoMo · v4-pro 探针 | 89.03% | 强 answerer 探针（付费 API），非默认栈能力，不做客户端涨点 |
| LongMemEval-S · v4-pro 探针 | 86.00% | 同上；同受 LME store 修复前口径约束 |
| LoCoMo · MemOS 同栈 | engram 85.68% vs MemOS 82.47% | 配对 +3.20pp，但**完全由上下文预算差驱动**，非记忆机制优势（见 §三预算剥离） |

**历史高分锚（legacy / 数据集特调，已移除）**：LoCoMo 91.10%（legacy 特调 + top-k 150，clean 重判）曾为基线锚，其 042 同配方 unified 重判 **91.43%** 已取代之；LME 91.1%（DeepSeek v4-pro 付费 + 已移除融合 prompt）为 post-hoc 诊断。详见下方探索历程第 3、4 阶段。

---

## 二、探索历程（2026-07 → 08，按时间线）

### 阶段 0：基线拷问——领先是机制还是预算？（07-26 ~ 08-02）

**引子**：开局只有一个"engram 比 MemOS 高 3pp"的点估计。第一个动作不是追分，而是拷问这个领先到底来自记忆机制还是上下文预算。

| 时间 | 探索 | 做了什么 | 结果 | 结论 |
|---|---|---|---|---|
| 07-26 | [MemOS 同栈复现](reports/memos-locomo-reproduction.md) | 固定 Qwen3.6 answerer / bge-large embed / 同 judge prompt，逐题配对 | engram 85.68% vs MemOS 82.47%，McNemar p=0.002895 | 点估计领先真实，但预算变量未分离 |
| 07-29 | [上下文预算剥离](reports/budget-ablation.md) | 扫 answerer 预算对齐 MemOS（3614→1083 tok） | 领先随预算**单调消失并反转**：+3.20pp → −5.62pp | **+3.20pp 全是预算不是机制**；同预算下 MemOS 的 tree/graph 更优 |
| 07-30~08-01 | [022 双基准冻结](reports/benchmark-parity-memory-architecture.md) | 冻结 B0/B1 协议（hybrid、cap、禁 IRIS/reranker/IDK retry），Evidence Ledger v7 | B0 85.32%；B1 cap 越低越掉（cap1100 仅 59.4%）；oracle recall 在 top-k30 饱和 | 建立正式评测纪律；token-accuracy 权衡是基本约束 |
| 08-01 | [024 密度四臂](reports/memory-density-four-arm.md) | 写时去重 × 邻居扩展 2×2 配对 | control 84.29% 最高，机制叠加单调下降（−0.46/−0.91/−1.30pp） | MemOS 的"写时去重+邻居扩展"不涨点 |
| 08-01 | [025 episode 聚类](reports/semantic-episode-cluster.md) | 跨消息语义聚类为 episode 候选 | multi-hop 0.0pp、OVERALL **−7.7pp** | 聚合挤掉逐证据覆盖；简单类目需要精确逐证据命中 |
| 08-02 | [B1 基线收口](reports/b1-control-packing-027.md) | 纯 harness 修复：chunk-verbatim fold + cap 5000 | majority **85.19%**（cap 是主杠杆 +1.4pp，打包仅 +0.2pp） | 85% 级正式基线收口；差距主因是 cap 截断非打包机制 |
| 08-02 | [023/SaaS 战略](reports/planner-023-saas-direction.md) | 评估训练式 Planner 与 95% 目标的缺口账 | 95% 需再救 151 题（+9.8pp），强 answerer 探针仅 89.03% | **95+ 是 SaaS 层目标，非本地口径能力**；本地检索/表示/聚类侧已 6+ 次证伪 |

**阶段教训**：预算才是分、机制不是；"同预算信息密度差距"是主战场，但 024/025 证明"加证据/聚证据"都打不动。注意力转向**证据的原始覆盖**与读侧装配。

### 阶段 1：写侧与检索侧机制证伪（08-05 ~ 08-06）

**引子**：把差距归因到"命中后的原始证据覆盖"后，连试写侧结构化（event）与检索侧推理（导航）两个大方向——**全部显著负**，唯独读侧装配翻盘。

| 时间 | 探索 | 做了什么 | 结果 | 结论 |
|---|---|---|---|---|
| 08-05 | [027 写侧 event](reports/027-write-side-event-verdict.md) | 7B sidecar 抽 event 结构替换原文 chunk | chunk 50.0% vs event 23.8%，**−26.2pp（p=0.0016）** | 绝对时间锚定丢失（event 仅 5% 带绝对日期）；**时间域第 7 次 NO-GO** |
| 08-06 | [028 训练抽取器](reports/028-write-side-training-verdict.md) | 训练 Qwen2.5-3B-LoRA，时间锚定 5%→96.9% | 仍 **−1.2pp（p=1.00）不转化** | 写侧 event 第三次不转化；抽取能力不是瓶颈，表示本身是 |
| 08-06 | [029 agentic 导航](reports/029-agentic-memory-navigation-verdict.md) | 模型多步导航改查询/换粒度 | 四次变体全部显著负（−13~−23pp） | **推理介入检索在当前 stack 负收益**；确定性模拟高估模型导航能力 |
| 08-06 | [030 读侧证据装配](reports/030-evidence-mediation-verdict.md) | post-retrieval 证据重排 chunk-first + trace 引用链 | 全量 **85.91% @ 468 tok**（省 7.7× token，比 base 多数 +0.72~1.01pp） | **第一个读侧结构性赢**；trace 默认开启 |

**阶段教训**：写侧结构化、检索侧推理全部证伪（原文 chunk 是最保真的写入形态）；真正的杠杆在**读侧证据精炼**——不增检索、不增推理，只把已检索到的证据装配好喂给 answerer。030 是探索史上第一个机制性正收益。

### 阶段 2：读侧与答题侧机制（08-07 ~ 08-10）

**引子**：030 证明"读侧装配"方向可行。继续在证据关联（结构上下文）与答题契约（时序推理）上挖，同时做了第一次成本总账。

| 时间 | 探索 | 做了什么 | 结果 | 结论 |
|---|---|---|---|---|
| 08-07 | [031 证据关联](reports/031-evidence-relation-verdict.md) | `--relation-context` 关系块（related_to/temporal_next） | 生效类别（temporal/multi-hop）各 +3.2pp，但整体 within-noise（p=0.253） | mechanism-go 但 default-off；related_to 在单对话被说话人主导、信息量有限 |
| 08-07 | [032 tplan 时序契约](reports/032-tplan-temporal-answer-verdict.md) | 答题侧时序推理 prompt | 弱栈 **+11.2pp（p=0.0001）**，生产栈 +0.5pp 噪声内 | temporal 主瓶颈确实在答题契约，但增量依赖 base 高低；default-off opt-in |
| 08-10 | [033 chunk-first 修复](reports/033-chunk-first-failure-analysis.md) | multi-hop chunk 提前排序 | 探针 C−A **+1 题**（门槛 +8），零翻转 | 排序没触达瓶颈；`gold_in_pool` 只证明任意命中、不证明回答链完整 |
| 08-05 | [成本复盘](reports/cost-effectiveness-retrospective-2026-08.md) | 对账净涨点与成本 | 唯一端到端转化机制涨点仅 +1.3pp（bge embedder）；云 reranker ¥150/天 → e2e −0.06pp | **中间信号不转化端到端**；催生 paid-cloud-rerank 死亡规则 |

**阶段教训**：答题侧契约（tplan）第一次在 temporal 域显著（弱栈），但强栈上被高 base 吸收；证据结构（relation）、chunk 排序都进不了主路径。方向收窄到"读侧装配 + 答题契约 + 评测纪律"。

### 阶段 3：高分冲刺与口径修复（08-11 ~ 08-12 上午）

**引子**：目标跨 90%。先试"加量"（top-k 150 大上下文）——**跨过 90 线但靠上下文税**；随即被 judge 口径问题追尾，意识到 90+ 必须 clean 口径才可信。

| 时间 | 探索 | 做了什么 | 结果 | 结论 |
|---|---|---|---|---|
| 08-11 | [top-k 探索](reports/topk-exploration-2026-08-11.md) | 思考模式下全局 top-k 30→150 sweep | top-k 150 全量 3-rep **多数 90.13%**（+1.6pp，上下文 2.4× 税） | 加量型涨点；需 32768 上下文，不设默认、作高预算旋钮 |
| 08-11 | [90pp 方向探索](reports/90pp-direction-exploration.md) | 全部廉价杠杆实测：judge 宽松/生成前置/v4-pro 重裁决/多数票 | 全部到顶或证伪；诚实底线 1381（89.68%） | **90pp 不需要更强模型，需要更好的输入上下文**；拒绝换分母 |
| 08-11 | [036 决策缺口归因](reports/036-decision-gap-attribution-verdict.md) | 33 题裁决缺口逐题归因 | factually_wrong 22（67%）主导 | 缺口在裁决甄别（多候选选错）非生成缺失 |
| 08-12 | [judge 口径修复](reports/judge-final-answer-regime.md) | judge 只判 final answer（剥离 thinking） | judge 跨批漂移 **±2.5pp**；clean 净 −23（作弊） | 90.13% 可能被旧 judge 版本低估/污染；**clean 是唯一可跨批口径** |
| 08-12 | [LME clean 重判](reports/lme-clean-rejudge-2026-08-12.md) | LME 同批 raw vs clean | LME 真实 clean 基线 **84.60%**（judge 作弊 ~1.6pp） | 90pp 在 clean 口径需 +5.4pp；thinking→final gap 仅 8.1%，推理本身才错 |
| 08-12 | [91.10 复现](reports/locomo-9110-repro-2026-08-12.md) | 独立重判 LoCoMo top-k150 3-rep | **CLEAN majority 91.10%（1403/1540）复现一致** | LoCoMo 90+ 站得住（clean 口径）；open-domain 68.8% 是短板 |
| 08-12 | [错题画像](reports/locomo-error-patterns-2026-08-12.md) | 152 错题形态分类 | 92% 是 answerer 真错；temporal 确定性偏移、single-hop 显著记忆压制 | 方向：clean 口径（A）、时间锚定契约（B）、跨栈稳定 cohort（C） |
| 08-12 | [037 重排训练](reports/037-memory-reranker-verdict.md) | 记忆专用 reranker（0.6B LoRA） | rerank **−1.1pp NO-GO**；发现单次 run 系统噪声 **≈8.6pp** | cross-encoder 永不进本地栈（死亡规则）；跨 run 对比必须 run 内配对 |

**阶段教训**：top-k 150 拿到 90+，但 judge 口径追尾证明它**只认 clean 口径**；错题画像揭示真瓶颈是推理/甄别而非检索。方法论里程碑：judge 必须 clean + 同批、检测 ~1pp 级杠杆必须 repeats≥3 + store 复用。

### 阶段 4：契约收敛（08-12 下午 ~ 08-14）

**引子**：所有"加机制"的杠杆（导航/重排/写侧/冲突 prompt）几乎全数证伪，剩一条从未系统验证过的路——**收敛 answer 契约本身**。unified answer contract 用"零特调"换"above-noise"，把探索从"追分"扭转到"坐实可移植的契约"。

| 时间 | 探索 | 做了什么 | 结果 | 结论 |
|---|---|---|---|---|
| 08-12 | [commitment 路线诊断](reports/commitment-routes-feasibility-2026-08-12.md) | 时间戳/反证据两条候选路线分型 | 各覆盖仅 ~13-17 题（0.6-1.0pp）；ENTITY_SHIFT（显著记忆压制）是错题大头 | 都不是快赢；压制是模型判断问题非候选呈现 |
| 08-12 | [涨点计划复盘](reports/score-increase-plan-2026-08-12.md) | 竞争诊断 + 候选压缩/排序 + counter-refine | 压制假设成立（四信号显著），但候选侧删除误伤/排序无效、gold 排最前仍答错 | 压制是模型偏好问题；候选侧无快杠杆 |
| 08-13 | [v4-pro LME 探针](reports/deepseek-v4pro-lme-verdict-2026-08-13.md) | 更强 answerer 是否突破 90 | v4-pro LME 91.1%，但**无记忆答不出**（真依赖记忆）；付费 API 非客户端技术 | 强 answerer 是真实杠杆但按宪法不算客户端涨点 |
| 08-13 | [counter-refine 全量](reports/counter-refine-verdict-2026-08-13.md) | 反证据二次检索 + 验证门 | 组合臂 −0.40pp（p=0.878）| 混合配方无净收益；独立效应未识别，不能写成 isolated causal NO-GO |
| 08-13 | [entity-verify 诊断](reports/lme-entity-verify-verdict-2026-08-13.md) | 实体替换门 prompt | +2.0pp 但 **p=0.1102 不显著**；审计发现 prompt 示例用了测试集真实陷阱 | **in-sample 特调**；看过测试集后调参不可泛化，已随 038 移除 |
| 08-14 | [040 adaptive topk](reports/040-adaptive-topk-verdict.md) | gap-knee 自适应截断 | 只能救 21% 的召回错因（r 上限 20% < 45% 门槛），**NO-GO** | 30→150 增量 80% 是上下文量；转向 confidence-gated（041） |
| 08-13→14 | [unified 038 坐实](reports/unified-answer-contract-verdict-2026-08-13.md) | 数据集无关统一契约，严格配对 + context parity fail-closed | 初版 cat3 open-domain 回落 NO-GO → Request classification 修订后 **LoCoMo +1.4pp（p=0.019）/ LME +4.4pp（p=0.000112）above-noise** | **unified 是唯一允许的 answer prompt**；不靠 per-dataset 特调 |
| 08-14 | [042 unified×k150](../operations/evaluation/042-unified-k150-run-handoff-2026-08-14.md) | unified 契约在 top-k 150 的配对复测 | unified 91.43% vs control(legacy) 91.36%（clean 重判），Δ+1 题 within-noise | 两机制正交：unified 修 prompt 病、k150 修上下文量；口径假象澄清（clean 跨批稳定） |
| 08-15 | [LME unified@k150 3-rep 补跑](../operations/evaluation/lme-unified-k150-3rep-2026-08-15.md) | LME 的 k150 配对（此前只有 k30 坐实）；同配方跑 unified 3-rep | 3-rep clean majority **92.0%**（91.0/92.0/91.8 高度一致）；preference +33.4pp 为最大增益源；对照 control 1-rep 87.0% | unified 在 LME 高配下同样 above-noise 正分，preference/multi-session/knowledge-update 增益模式与 k30 完全一致 |

**阶段教训**：Entity-verify 的 in-sample 教训把"特调高分"彻底钉死，unified 用零特调在双数据集 above-noise 坐实——**可移植的契约正确性 > 数据集目标额的高分**。042 收尾同时澄清了一个此前困扰的谜团：早期 90+ 与近期 ~89 的落差是 judge 口径差异（raw 判 thinking vs clean 判 final answer），不是能力回退。08-15 的 LME@k150 补跑把 unified 契约的验证面从「k30 双数据集」扩展到「k150 高配」，增益模式跨 top-k 稳定（preference 始终 +30pp 级），且 control 1-rep（87.0%）作为参考不构成严格配对——如需坐实 control 需补 control 3-rep。

---

## 三、方法论教训（探索沉淀的评测纪律）

1. **预算才是分，机制不是**：+3.20pp 同栈领先完全由上下文预算驱动；对齐预算后反超为 −5.62pp。任何跨栈差值必须先剥离预算。
2. **中间信号不转化端到端**：reranker、导航、冲突 prompt、事件结构——信号再强、再"机制正确"，不端到端配对显著就不算赢（008 铁律）。
3. **judge 必须 clean + 同批**：judge 从 thinking 前导读候选作弊 ~1.2–1.5pp（跨数据集一致）；跨批漂移 ≥±2.5pp。任何 ≥90 的分数必须报 clean 口径，配对实验必须同批交错。
4. **单次 run 噪声 ≈8.6pp**：检测 ~1pp 级杠杆必须 repeats≥3 + `--store-dir` 复用；跨 run 单臂对比不可靠。
5. **单次正点估计 + 不显著 p 不能转正**：entity-verify +2.0pp（p=0.11）最终证伪为 in-sample 特调。看过测试集后调参、category 路由、固定拒答措辞的分数一律不可泛化。
6. **确定性模拟高估真实模型**：029 的 US1 理论救回空间（0.655）在真实导航中不转化（−17.9pp）。
7. **`gold_in_pool` ≠ 回答链完整**：任意 gold turn 命中不证明所需证据全部进入上下文；DiaID 命中 ≠ 答案承载内容进入 prompt。
8. **收敛的力量**：当"加机制"全面证伪时，最后的杠杆是把**契约本身**收敛干净（unified）——零特调 + 严格配对，反而在双数据集 above-noise。

---

## 四、完整当前矩阵与对照（追溯细节）

### 读侧证据中介（trace，unified 之前的默认）

`--trace-mediation` 把检索候选压缩成带引用的证据再交给 answerer，answer context 从 ~3,614 降到 ~468 token（省 7.7×），3-rep majority 85.91%（single-hop 88.23% / multi-hop 87.23% / temporal 84.42% / open-domain 66.67%）。机制拆解见 [030 verdict](reports/030-evidence-mediation-verdict.md)。

### B1 正式基线（022 冻结协议）

LoCoMo B1（Qwen3.6-35B-A3B-FP8 / bge-large-en-v1.5 / deepseek-v4-flash，local hybrid、3-rep majority）：**majority 1,312/1,540（85.19%）**、stats 84.7%（相对 026 control +2.1pp），validity 全绿。协议禁止 IDK retry，故 B0 靠 rewrite 获得的 ~0.45pp 结构性不可达。详见 [b1-control-packing-027](reports/b1-control-packing-027.md)。

### LongMemEval-S 口径更正

LME-S 500 的历史行来自 store 修复前：`buildSessionChunks` 截断 >1100 code point 的单 turn，四道 gold-bearing 答案受影响。修复后 clean 真实基线 = **84.60%**（[lme-clean-rejudge](reports/lme-clean-rejudge-2026-08-12.md)）。在重建向量并同配方 full 500 复跑前，不得当作已刷新基线。

### 同栈竞品对照与预算剥离

固定 Qwen3.6 answerer / bge-large embed / 同 judge prompt：engram 85.68% vs MemOS 82.47%（1529 配对，McNemar p=0.002895）。但扫 answerer 预算对齐 MemOS 后领先**单调消失并反转**（3614tok +3.20pp → 1083tok −5.62pp），**因此 +3.20pp 完全由上下文预算驱动，不是记忆机制优势**。完整数据见 [budget-ablation](reports/budget-ablation.md) 与 [memos-locomo-reproduction](reports/memos-locomo-reproduction.md)。

### 不同评测栈下的公开成绩（市场背景，不算系统间差值）

| 数据集 | engram 实测 | MemOS 自报 | Mem0 自报 |
|---|---|---|---:|
| LoCoMo | 91.43%（unified·k150·clean） | 88.83% | 92.5% |
| LongMemEval | 92.0%（unified·k150·clean·3rep） | 89.20% | 94.4% |

Mem0 高分来自托管平台，无同栈复现；MemOS 公开分在 engram 受控栈下实测 82.40%。口径与来源见 [competitors.md](competitors.md)。

---

## 五、结果维护要求

更新任何一行结果时必须同时登记：数据集版本与样本数、answerer、judge、完整 recipe（含 top-k/契约/聚合）、口径（raw/clean）、日期和证据路径。早期小样本数字仅作为[历史先导](../archive/evaluation/longmemeval-100-pilot-2026-07.md)保存，不能作为当前结论引用。
