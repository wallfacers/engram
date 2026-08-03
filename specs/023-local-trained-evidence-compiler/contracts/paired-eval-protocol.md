# 三臂配对评测协议（023 增量）

**Version**: 023.v1

**消费的冻结合同**: [022.v1 Evaluation Protocol](../..//022-benchmark-parity-memory-architecture/contracts/evaluation-artifacts.md)
**Spec**: [023 spec.md](../spec.md) FR-023/024/025/026/027/028/029/030/033
**Primary Benchmark**: LoCoMo（category 1–4，1,540 题）；**Cross-Benchmark Guard**: LongMemEval-S（500 题，Guard 后补）

## 目的

证明在 022 候选、来源、token cap、answerer、judge、prompt、重复策略与运行环境全部冻结时，
训练式本地 Planner 相对**同底模 prompt-only** 是否产生可归因、可跨基准复现的涨点。
单变量：**同 store、候选逐字节一致、只差训练状态**。

## 三臂定义

| 臂 | 训练状态 | 模型 | proposal 来源 |
|---|---|---|---|
| `deterministic`（control） | 无模型 | — | 022 确定性 Compiler（raw-fits → EXTRACT → MERGE gate） |
| `prompt-only` | 零训练 | 同底模（Qwen2.5-7B-Instruct） | `localPlanner` + 冻结 `plannerSystemPrompt`/`renderPlannerPrompt` |
| `supervised` | LoRA SFT | 同底模 + adapter | `localPlanner` + 训练后 adapter（同一 prompt 模板） |

- prompt-only 与 supervised **同一底模、同一 prompt 模板、同一 sidecar 路径**，只差
  adapter 训练状态（FR-023/024）。
- `deterministic` 是配对 control；每臂独立 verdict，复合 recipe 不得掩盖未 GO 的相邻
  阶段（FR-030）。
- 可选 post-training 臂由独立 validation 预注册条件决定，正式结果前一次性冻结（FR-023）。

## 单变量纪律（硬）

1. **同 store**：三臂复用同一 freeze store（同一 ingestion recipe、同一 schema version、
   projection builder 版本）。不在臂间重建或增删数据。
2. **候选逐字节一致**：三臂每题共享完全相同的 `rendered_candidates` bytes；
   `candidate_set_digest` 一致率 100%，`retrieval_calls_after_freeze=0`（FR-025）。
3. **只差训练状态**：answer renderer、静态 prompt、token cap fingerprints 相同；模型
   只能改变 proposal 及由其合法 Bundle 导出的 evidence payload（FR-025）。
4. **一次作答纪律**：每个 repetition 有效 Bundle 后恰好一次 answerer；IDK retry 关闭；
   legacy `answer_calls>1 || rewrite_calls>0` → INVALID。
5. **无越权调用**：Planner 无第二次 retrieval / 无扩大候选池；候选/表示/source/cap/
   Compiler validation/answerer/judge/prompt/重复策略全部冻结（FR-024）。

## Primary Cohort / Benchmark 冻结（FR-027）

- **Primary Benchmark** = LoCoMo（category 1–4）。T003 已量化 Primary Cohort：
  共同分母 1532，`compiler-eligible residual = { 题 ∈ G(1391) 且 题 ∉ D(1306) } = 149` 题
  （temporal 45 / single-hop 60 / multi-hop 27 / open-domain 17）→ 见
  [residual-cohort.json](../residual-cohort.json)。
- **Cross-Benchmark Guard** = LongMemEval-S（500）；Guard 为后补项，完成前任何跨基准
  推广声明禁止（Amendment 1）。Guard 后补本身也按本协议同 store 配对。
- 不得事后更换 benchmark 或移动题目以扩大差值。

## 预算 profile（复用 022 B1）

- 沿用 022 冻结的 `low`（~1.1k input tokens）与 `high`（~3.6k）profile；`protocol.json`
  写精确正整数，两基准同名同值（evaluation-artifacts §B1）。cap 改动 → 新 protocol hash
  与新 baseline。
- 正式默认栈 reranker **disabled**（DEATH RULE）；不得写 hosted diagnostic 结果冒充。

## 每臂 validity 判据（全部 100% 才算 valid）

| 项 | 要求 |
|---|---|
| candidate | 逐题 candidate_set_digest 与 control 100% 一致 |
| source | item citation source union 在冻结 lineage allowlist；active-Ledger 独立 read-back 全绿 |
| span | 每个 code-point span/digest 从 source 复原率 100% |
| citation | 每 item 的 candidate/source 引用均在冻结 lineage 内 |
| within-cap | 每题完整 answer input ≤ token cap（`counter_fingerprint` 匹配） |
| answerer 调用 | 每 repetition 恰好 1 次；retrieval_calls_after_freeze=0；无 IDK retry |
| fallback | Planner 臂 fallback 题单列 `fallback_rate` + `fallback_reason`；按确定性路径计分，不从分母删除 |
| 污染 | 训练数据零 benchmark test 内容/namespace/付费 teacher（FR-011/013/014，审计见 T012） |

任一 validity 阻塞项非零 → 该臂 `INVALID`，不得解释 accuracy（FR-029 闭包）。

## 统计判据（FR-028）

单阶段 `GO` MUST 同时满足：

1. **Primary Cohort majority accuracy Δ ≥ +2.0pp**（相对相邻 control：prompt-only vs
   deterministic；supervised vs prompt-only）。
2. **two-sided exact McNemar**（二项 `X ~ Binomial(b+c, 0.5)`，`p = min(1, 2·P[X≤min(b,c)]`)），
   对全部预注册训练阶段比较做 **Holm 多重校正**后 `p < 0.05`。绝不切换卡方近似。
3. **Cross-Benchmark Guard overall Δ ≥ −0.5pp** 且无显著负向配对结果。
4. **全部预注册类别**（temporal / single-hop / multi-hop / open-domain）经 Holm 校正
   non-regression；不得事后合桶。
5. **validity 阻塞项 = 0**；judge audit 完成且 corrected labels 不改变 verdict。

重复策略：answerer 非确定性 → 题级 correctness 先由 3 次独立 repetition majority 固化，
再配对；不得把 repetition 当独立题扩大样本量。

## Verdict 闭包（FR-029）

| verdict | 条件 |
|---|---|
| `INVALID` | 任一 validity 条件失败 |
| `STOP` | validity 有效但 Primary Cohort Δ ≤ 0、Guard 显著受损或任一预注册类别显著回退 |
| `GO` | 满足 FR-028 全部条件 |
| `HOLD` | validity 有效、无前述显著伤害但未满足 GO（0 < Δ < 2pp / 未显著 / 证据不足） |

每阶段独立 verdict（FR-030）；阶段 GO ≠ 产品推荐。最终 recipe 须再过 FR-031 产品推荐门
（全量正确题严格 > deterministic control + 双基准/保护类别 non-regression）才可推荐。

## 提交与归档隔离（FR-033 / 022 Submission Isolation）

1. 本协议（schema/validator/统计）单独 commit；
2. frozen protocol/config（protocol.json）单独 commit；
3. 每臂 result/verdict 单独 commit；
4. 训练数据/recipe 与 Planner 产物变更分开归档——任何分数变化可定位到单一阶段；
5. 全部正式臂一次性运行 + 预注册多重校正；不得看过正式 test 结果后回调训练、追加臂或
   选配置并复用同一结果作为无偏验证。

## 报告要求（FR-032）

正式报告给出 LoCoMo 精确计数 `/1,540`、LongMemEval-S `/500`、百分比、置信区间、配对
检验、类别结果、judge audit、训练与推理资源、延迟、成本与所有摘要；同时区分
**compiler miss vs candidate miss**（candidate oracle：gold 在 candidate 但不在 Bundle →
compiler miss；gold 不在 candidate → candidate miss；Bundle/oracle 有 gold 仍答错 →
answerer miss）。未达到 1,425/1,540 与 473/500 不得声称完成数值目标。
