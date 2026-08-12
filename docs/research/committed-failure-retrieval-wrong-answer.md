---
title: 检索到但答错：commitment failure 文献审计与本地优先路线
summary: 基于 2026-08-12 alphaXiv 逐篇核实的 8 篇 RAG/记忆冲突论文，确认 engram 错题（gold 已在 top-k 但 answerer 选错）在文献中对应 commitment failure；两条可本地优先落地的机制（确定性时间戳聚合、答题后反证据检索）与已证伪的"换表示/加检索/prompt"路线正交。全部为外部证据，未在本栈验证。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-12
canonical_for: [committed-failure-retrieval-wrong-answer]
tags: [research, rag, memory, locomo, longmemeval, conflict-resolution, commitment-failure, local-first]
---

# 检索到但答错：commitment failure 文献审计与本地优先路线

## 问题定义（对应 engram 错题归因）

engram 的错题画像（[LME attribution](evaluation/reports/lme-conflict-prompt-nogo-2026-08-12.md)、
[LoCoMo clean 重判](evaluation/reports/locomo-9110-repro-2026-08-12.md)）一致显示：**错题不是检索缺**
（LME gold 82% 在检索 pool、median rank 3；LoCoMo thinking 里候选在 top-3 但选错），而是
**多值冲突/推理选错**——检索层正常，答案层判断错。

本文从 alphaXiv 逐篇核实 8 篇直接处理该问题的论文，确认这是文献成熟问题，并给出两条
**本地优先、确定性、无付费云模型**的候选机制。**全部为外部证据（未在本栈验证）**。

## 核心归因：commitment failure vs access failure

**CounterRefine**（arXiv:2603.16091，SimpleQA 官方 grader）原文定义：*"many errors are not
failures of access but failures of commitment: the system retrieves relevant evidence, yet still
settles on the wrong answer."* 这与 engram 错题归因逐字吻合。它证明此类错误可通过
**answer-conditioned counterevidence retrieval + KEEP/REVISE 门 + 确定性验证**修复：
SimpleQA +5.8pp（63.7→67.7 Claude / 67.3→73.1 GPT-5），仅改动 5.6% 的预测、利/害比 22.5:1。
修复的主要错误是 entity 混淆、date、number——正是 LoCoMo open-domain 与 LME 错题类型。

## 论文分级（按"是否可本地优先、有受控单变量证据"）

| 级别 | 论文 | 核心机制 | 结果 | 本地优先可行性 |
|---|---|---|---|---|
| **A · 机制强+本地可落地** | Don't Ask the LLM to Track Freshness（2606.01435） | 候选提取 + **确定性 max(serial/timestamp) 聚合**，把新旧比较移出 LLM | FC-SH +10.8pp；LongMemEval knowledge-update max(timestamp) 有效 | ✅ 纯 Go 确定性聚合，无模型 |
| **A · 机制强+本地可落地** | CounterRefine（2603.16091） | 答题后**反证据检索** + KEEP/REVISE + 确定性验证 | SimpleQA +5.8pp；HotpotQA EM +3~5pp | ✅ 二次检索+判定门 |
| **A · 机制强+本地可落地** | Temporal Validity（2606.26511） | 写侧**确定性 (s,r,o) supersession**，无 LLM 调用 | stale-fact error 15-40% → ~0 | ✅ 写侧规则+bi-temporal ledger |
| **B · 条件性/局部支持** | A-TMA（2607.01935） | 三层解耦（bank/retrieval/QA）+ 显式 state roles 标签 | LTP conflict +0.240；LoCoMo temporal F1 0.0295→0.1705（host 依赖） | ⚠️ 收益 host 依赖，Qwen 侧部分有效 |
| **B · 条件性/局部支持** | ConflictRAG（2605.17301） | 冲突检测 + Entropy-TOPSIS 来源可信度 | +5.3~6.1% | ⚠️ 需训练 MLP 检测器 + 元数据 |
| **C · 诊断/评测（非机制）** | MemConflict（2605.20926） | 动态/静态/条件三类冲突评测框架 | 6 系统表现不均 | 评测工具，非涨点 |
| **C · 诊断/评测（非机制）** | Separating Semantic Competition（2605.27294） | 语义竞争 vs 上下文长度隔离协议 | 硬竞争者替换 +4.5~6 EM | 诊断协议，支持"减少竞争噪音" |
| **C · 产品正确性（非涨点）** | ChronoMem（2607.27773） | 记忆版本控制+语义回滚 | rollback 场景 +10pp | 产品能力，非 eval 涨点 |

## 关键机制细节（已核实）

### 1. 确定性时间戳/序列聚合（2606.01435）—— 对应多值冲突

- 机制：LLM 只做**候选提取**（不判新旧、不选 winner），新旧选择交给 **Python `max(serial)`**；
  完整 pipeline ≈50 行 Python。
- 为什么有效：LLM 在"过滤+新鲜度+抗先验+作答"纠缠在一起时，无法可靠应用显式新旧规则；
  尤其候选多时 serial 跟踪漂移（262K 上下文掉 14pp）。确定性 `max()` 精确、无先验覆盖、可审计。
- **重要边界（诚实）**：在 LongMemEval knowledge-update（真实时间戳）上只与 LLM 判断**打平**
  （57.8% vs 64.4%），只在 "what is X currently?" 类题上稳赢；Yes/No、历史、聚合题需
  question-type-aware 组合。
- **A-TMA 警告（2607.01935）**：单独时间戳/图标签仍可能在答题时失败——需要把显式
  state roles（current/historical/transition）暴露给 QA，而不是只靠元数据排序。

### 2. 答题后反证据检索（CounterRefine, 2603.16091）—— 对应 commitment 修复

- 机制：先答草稿 `a0` → 用 `(q, a0)` 反查证据 `R1` → refiner 输出 KEEP/REVISE → 确定性验证
  （类型规则+证据覆盖度+词法重叠）后才接受修订。
- 价值不在"多思考"，而在"**改变什么证据在具体假设形成后变得可见**"：若草稿年份错，加该年份反查
  可直接证伪它。
- 修复的正是 entity/date/number 混淆——与 LoCoMo open-domain 错题（含 date/数量推断）直接对应。

### 3. 语义竞争隔离（2605.27294）—— 支撑"减少竞争噪音"

- 正确 passage 在上下文里，但**语义竞争者**（多个看似相关的 passage）会压制 reader 选出正确答案；
  固定长度下把硬竞争者换成远距 passage 恢复 +4.5~6 EM。反过来说：**竞争者越多越有害**，这与
  engram top-k 150 大池可能自带大量竞争者的观察一致。

## 与已证伪路线的正交性（为什么这次可能不同）

engram 已证伪且**均为"换表示/加检索/改 prompt"**路线：temporal prompt（032）、event 表示
（027/028）、本地 reranker（008/037）、agentic nav（029）、冲突 prompt A/B（LME 6/6 零效果）。

论文给出的两条路线**从未在本栈试过**：

1. **写侧确定性元数据 + 读侧确定性 `max()` 选择**——把"哪个最新/哪个真"从 answerer 的 LLM 判断
   中剥离，变成确定性代码。正交于 032 的"prompt 让 LLM 去推理时序"（已 NO-GO）。
2. **答题后反证据二次检索 + 确定性验证门**——正交于 029 的"检索前多步导航"（已 NO-GO），是
   答案形成后的验证层，不是扩展检索。

## 本地优先评估（未验证，估算）

| 候选路线 | 对应错题 | 宪法合规 | 本地成本 | 不确定性 |
|---|---|---|---|---|
| 确定性时间戳聚合 | LME knowledge-update / LoCoMo temporal 多值冲突 | ✅ 纯 Go 确定性 | 低（写侧元数据+读侧选择） | 文献只打平 LLM；需要 fact 粒度时间戳，可能动 store schema |
| 反证据检索+验证门 | LoCoMo open-domain / LME entity/date/number | ✅ 二次检索+判定 | 中（额外一轮检索+refine 调用） | 需要 LLM refine 调用，成本 +1 call/题；收益在长尾 |
| 语义竞争压缩（减 top-k 竞争噪音） | 大池竞争压制 | ✅ | 低 | 与 90.13% 的 top-k150 提升可能冲突，需先诊断 |

## 建议下一步

1. **先诊断（零代码）**：从 LoCoMo/LME 错题集里，把"候选在 top-k 内但 answerer 选错"进一步
   拆分为"多值冲突（同一槽位新旧/真假值）"vs"实体/数字推断错"——决定走时间戳路线还是反证据路线。
2. 若多值冲突占主导 → 探索**写侧确定性 supersession 或读侧确定性 max(时间戳)**（先不扩 schema：
   从现有 `event_date` 元数据开始，只在 answer 装配层加确定性选择）。
3. 若实体/数字推断占主导 → 探索 **CounterRefine 式答题后验证**（eval harness 内先跑，flag
   default-off，宪法 IV 归因）。
4. 两条都需在宪法 IV 下跑可比较 eval 确认，禁止未验证声称涨点。

## 诚实边界

- 本文全部为**外部文献证据**，未在本栈跑过任何一行实验；"可行性"是机制层判断，不是实测。
- 2606.01435 在 LongMemEval 只打平 LLM 判断（n=45，CI 宽）；A-TMA 收益 host 依赖；2605.27294
  是 SQuAD 小型 reader 诊断，非端到端。
- 不构成对"检索已正常"归因的推翻——该归因（LME attribution）独立成立；本文只提供
  答案层的修复候选。
- 所有论文均经 alphaXiv 逐篇核实（非记忆/WebSearch 臆测）。
