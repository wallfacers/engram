---
title: 分数坐实协议 — 从 post-hoc 观测到 verified 基线
summary: LME unified 90.4% 等 post-hoc 分数不可直接采信（ci 重叠 + 无 context parity）。本文定义分数层级声明规范与坐实硬性步骤（配对 SHA-256 context parity + 同批 judge + 3-rep majority + clean 口径 + exact McNemar + held-out 行为门），并列出复跑前置阻塞。正式协议见 specs/038-unified-answer-contract/contracts/evaluation-protocol.md，本文是 docs 层执行指引。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [evaluation, scoring, verdict, protocol, locomo, longmemeval]
---

# 分数坐实协议

## 背景：为什么 90.4% 未坐实

背景：特调提示词已并入统一系统提示词（`--unified-answer-contract`，数据集无关、
零示例、零 category 分支），需要干净的分数来判定该方向。LME unified 90.4%
（[038 verdict](reports/unified-answer-contract-verdict-2026-08-13.md)）是
post-hoc 非配对观测，不可直接采信，原因有二：

1. **ci 重叠（统计不显著）**：control 86.1% ci=[84.3, 87.8]，unified 90.4%
   ci=[85.7, 95.1]，两区间在 85.7–87.8 重叠 → 差值可能只是 judge/模型噪声，
   McNemar 非显著。
2. **无 context parity（不能严格归因）**：LME 因 embed 512 上限 fail-closed 拒绝，
   改为两臂分开跑，未逐题校验两臂输入字节一致 → 差值可能混入检索/运行条件差异，
   不能归因于契约本身。

**任何分数在坐实前必须完成下文 8 步**。缺失即按 [evaluation-protocol](../../specs/038-unified-answer-contract/contracts/evaluation-protocol.md)
判定为 `BLOCKED` 或 `post-hoc`，不写进当前结果矩阵的已验证区。

## 分数层级声明规范

| 层级 | 判定 | 允许的用途 |
|---|---|---|
| **verified（坐实）** | 配对 + context parity 全过 + 同批 judge + 3-rep majority + clean + 统计检验 + held-out 门 | 当前基线、产品决策、写 results.md |
| **post-hoc（诊断）** | 非配对 / 非严格 / ci 重叠 | 方向信号、机制洞察；不可当分数 |
| **probe（探针）** | 单次 / 换 answerer / 非产品路径 | 能力上限参考；不可当系统能力 |

## 坐实硬性步骤（8 步）

1. **修 embed truncate**（前置）：bge-large 512 上限致 0.031% 杂讯 chunk（代码/
   数值表/外语）embed 400 → 配对 fail-closed。评估证实这些 chunk **0/122 承载
   gold、对答题零影响**（[评估](reports/lme-overlong-chunk-assessment-2026-08-14.md)），
   经 `truncate_prompt_tokens`（vllm 服务端或 embedding.Client 配置）修复即可，
   无需重建 store。
2. **重建 store**（LME 前置）：`buildSessionChunks` 曾截断超 1100 code point 单
   turn（[Feature 016 口径更正](../../specs/016-longmemeval-crossbench/verdict.md)），
   修复后重嵌受影响向量。
3. **配对**：control = legacy 通用 prompt，treatment = unified 契约；同数据集
   字节、同 store 快照、同检索、同模型/版本/生成参数/重试/并发；唯一变量 =
   answer 系统提示词 digest。**逐题校验 `sha256_of_actual_provider_answer_user_bytes`
   相等**（context parity），任一不等即失败。
4. **同批 judge**：两臂/所有 rep 在同一次 judge 调用期交错执行，消除跨批漂移
   （实测 ±2.5pp）。
5. **3-rep majority**：repeats=3 以上，每题多数票；单次 run 系统噪声 ~8.6pp
   （[037](reports/037-memory-reranker-verdict.md)），1-rep 不具结论性。
6. **clean 口径**：`extractFinalAnswer` 剥离 thinking 前导再判；raw 会被 judge
   从思考前导读候选作弊（跨数据集一致 ~1.2–1.5pp）。≥90 分数必须报 clean。
7. **统计检验**：逐题 flips（control-only / treatment-only）+ **exact McNemar**；
   声明 delta 必须报 ci 与 p。LoCoMo 非劣门 = answerable majority delta
   ≥ -0.5pp 且无显著回归。
8. **held-out 行为门**（promotion 前）：≥149 独立 directly-supported 案例、
   blinded 人工标注、false-abstention ≤2%、上置信限 ≤2%；17 例 smoke 不满足。

## 已知前置阻塞

| 阻塞 | 状态 | 影响 |
|---|---|---|
| embed 512 上限 | 已评估（零答题影响），修法待定 | 038 配对 fail-closed |
| LME store 修复前 | 待重建 | LME 基线不可作正本 |
| held-out 行为集 | 未编写/未冻结 | promotion 前置不满足 |
| 038 LME 臂 top-k | verdict 未注明 | 复跑前从 run-dir 确认 |

## 复跑执行顺序（最新栈）

1. embed truncate 落地 → 恢复配对能力。
2. **LME unified 配对**（最高优先）：control vs unified，同批 judge、3-rep、clean。
   - 坐实则 90.4% 从 post-hoc 转 verified；证伪则记录 NO-GO。
3. **LoCoMo unified + top-k150 高配**：细化「推断 vs 拒答」边界后，过非劣门。
4. 重建 LME/LoCoMo 基线，写回 result-matrix.md 的 verified 区。

## 与正式协议的关系

- 本文件是 docs 层执行指引；**权威判定标准是
  [specs/038/contracts/evaluation-protocol.md](../../specs/038-unified-answer-contract/contracts/evaluation-protocol.md)**
  （冻结轴、gates、BLOCKED/NO-GO 语义）。
- 违反任一条即 `BLOCKED`，不描述为任何 gate 失败之外的加分项。
- LongMemEval 单独不能 promotion；LoCoMo 非劣 + held-out 行为门同过才可转正。
