---
title: 思考曲线与 Qwen 栈理论上限（提示词×模型强度替代曲线）
summary: 把 LoCoMo 生产栈分数阶梯、思考解锁口径差、提示词契约边际的替代曲线与 Qwen 理论上限收敛为一张归因标尺。结论：LoCoMo 90+ 由模型思考 + 检索预算 + 多数票买到，提示词贡献≈0；LME 单次 90+ 是提示词的 in-sample 调参 + 正噪声，无效。提示词契约与模型思考能力是替代品：弱栈是主场（tplan +11.2pp temporal），强栈边际归零（tplan 思考下 flips 32/32）转负（v4-pro 裁决 −1）。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
canonical_for: [thinking-curve-qwen-ceiling]
tags: [research, evaluation, locomo, prompt, thinking, attribution]
---

# 思考曲线与 Qwen 栈理论上限

本文是**归因参考**，不是新实验 verdict——所有数字都来自已收口报告，本文只做重排与合成。同栈内增删可作证据；跨栈/跨批绝对分不可横向相减（judge 跨批漂移 ±2.5pp）。

## 一句话结论

**LoCoMo 90+（90.13% / 91.10%）= 模型思考（口径差 +~1.4pp）+ 检索预算 top-k150（+1.6pp）+ 3-rep 多数票，同一份 legacy 提示词原封未动 → 提示词贡献 ≈ 0。**
**LME 单次 90% = 提示词改动的表象，但 post-hoc in-sample 调参（测试集陷阱逐字入例）+ 单次正噪声，repeat-3 均值仅 88.53% → 无效，已移除。**
**统一规律：提示词契约与模型思考能力是替代品，不是互补品。** 模型越强/思考越开，prompt 契约边际从显著（弱栈 +11.2pp temporal）→ 归零（思考下 flips 32/32）→ 转负（v4-pro 裁决 −1）。

## 1. 分数阶梯（思考/强度 × 分数）— LoCoMo answerable 1540

> 各行为跨栈/跨批，只有同一行的增减是 arm-to-arm；行间只作形状观察。

| 栈 | 检索 | 思考 | top-k | 口径 | 分数 |
|---|---|---|---|---|---|
| flash | fts（弱检索） | 禁 | 30 | keep 1-rep | 43.2%（665/1540） |
| flash | fts | 禁 | 30 | tplan 1-rep | 46.8%（720/1540）· temporal 46.1→57.3 |
| flash | hybrid | 开 | 30 | 1-rep | **88.1%** |
| Qwen3.6-35B | hybrid | 禁 | 30 | keep 1-rep | 86.8% |
| Qwen3.6-35B | hybrid | 禁 | 30 | tplan 1-rep | 87.3%（+0.5pp） |
| Qwen3.6-35B | hybrid | 开 | 30 | 3-rep mean | 88.23% [86.85, 89.60] |
| Qwen3.6-35B | hybrid | 开 | 30 | 3-rep majority | 88.51% |
| Qwen3.6-35B | hybrid | 开 | 150 | 3-rep mean | 89.8% [89.4, 90.3] |
| Qwen3.6-35B | hybrid | 开 | 150 | 3-rep majority | **90.13%**（1388/1540） |
| Qwen3.6-35B | hybrid | 开 | 150 | **clean** majority | **91.10%**（1403/1540） |
| Qwen3.6-35B | hybrid | 开 | 150 | raw majority | 92.66%（1427/1540） |
| 034 oracle（三候选任一正确） | — | — | — | 上界 | **91.62%**（1411/1540） |

曲线形状三个特征：

1. **思考是最大的模型侧杠杆**：Qwen 禁→开 = +1.43pp（3-rep mean，ci95 下沿 86.85 仍贴住禁思考 86.8）；1-rep 为 +1.77pp，全类别正向（single +2.0 / multi +1.1 / temporal +1.2 / open +3.2）。
2. **思考 > 模型品牌**：flash + hybrid + 思考（88.1%）追平 Qwen 生产栈（88.57%）——思考的杠杆盖过 Qwen vs flash 的差距。
3. **提示词在 90+ 阶梯贡献为 0**：90+ 整段用 legacy prompt（category-routed + `--force-answer`，非统一契约）原封未动；唯一"提示词改动"的 46.8%（tplan）反而在最弱的检索栈上。

## 2. 替代曲线（思考/强度 × 提示词契约边际）

| 栈 | 模型 | 思考 | 契约 | Δ vs keep |
|---|---|---|---|---|
| fts 弱检索 | flash | 禁 | `--temporal-answer-prompt` | **全量 +3.6pp（p=0.000101）/ temporal +11.2pp**；84 题子集 +13.5pp / temporal +15.8pp（p=0.000729） |
| hybrid 生产 | Qwen3.6 | 禁 | tplan | +0.5pp（p=0.504，within-noise） |
| hybrid 生产 | Qwen3.6 | 开 | tplan | **0.0（flips 32/32，p=0.90）**；temporal 单类 +2.1pp 被 single/open 各 −1pp 抵消 |
| 裁决侧 | v4-pro | 开 | 裁决 tplan（`--adjudication-temporal-prompt`） | **−1**（33 缺口净 −1，temporal −1） |
| LME-S 500 | Qwen3.6 | 开 | abstain / typed / conflict / answer-focus | −0.4（p=0.878）/ +0.8（p=0.608，temporal +2.2）/ 0 / −0.13 |

**机制**：模型越强/思考越开，时序与聚合推理被模型内化，显式契约从"补齐能力"退化为"冗余指令"，最后变成干扰。**提示词边际是模型能力的补集**——这是它只在弱栈显著的原因，也是 038 统一契约大概率不涨分的原因（强栈上 prompt 边际本来就 ≈ 0）。

## 3. 思考解锁的分解（口径差 vs 机制增量）

- **定性**：思考是模型默认能力（Qwen3.6 / DeepSeek-v4 默认开，部分模型无法关闭），放开 = 对齐真实部署口径，不是新增机制；paid-cloud-rerank DEATH RULE 不适用（思考无额外第三方成本）。按 [[force-answer-regime-gap]] 同类"评测口径对齐"逻辑。
- **定量**：Qwen 禁→开 = +1.43~1.77pp（1-rep/3-rep 差异），弱栈上思考增量更大（fts flash +6.7pp 量级），强栈上 ≈ 平基线——思考的边际同样反比于栈强度。
- **成本**：Qwen 思考生成量 >10× 正常 answer；tplan 时序 prompt 曾诱导超长思考（50 分钟仅 93 题）。vllm 提速配方：`max-num-seqs 32 + gpu-mem 0.85`（MoE 大 batch 专家并行充分），全量 ~8.8h → ~0.5h。
- **副作用**：思考输出混在 `content` 里（非独立 reasoning_content），judge 从 thinking 前导读候选是稳定污染（clean−raw = −1.56pp LoCoMo，跨数据集统一 ~1.2–1.5pp）。**任何 90+ 分数必须报 clean 口径。**

## 4. Qwen 非强模型场景：理论上限

**可达上限推算（LoCoMo answerable 1540）：**

1. **候选侧理论天花板 ≈ 91.6%**（034 oracle 91.62%，1411/1540 = 三候选任一正确）——裁决/生成再强也拿不回的上界。
2. **Qwen top-k150 + 思考 + clean majority 实测 91.10%，距天花板只剩 ~0.52pp** → **Qwen 栈已贴在候选天花板附近**。剩余空间不在模型/提示词，而在候选覆盖。
3. **错题画像（clean 91.10%，137 错题）**：

   | category | 正确/总数 | 占比 | 错题占比 |
   |---|---|---|---|
   | multi-hop | 270/282 | 95.7% | 12/137 = 8.8% |
   | temporal | 295/321 | 91.9% | 26/137 = 19.0% |
   | single-hop | 772/841 | 91.8% | 69/137 = 50.4% |
   | open-domain | 66/96 | **68.8%** | 30/137 = **21.9%**（单类最密集） |

4. **"更强模型"在可答子集上未证明更高**：v4-pro 3-rep majority 89.03%（不同协议）低于 Qwen top-k150；90pp-direction 判定"90pp 不需要更强模型——需要更好的输入上下文"。
5. **提示词在 Qwen 思考栈理论增量 ≈ 0**（§2 替代曲线已归零/转负）。

**Qwen 栈理论结论**：`86.8%（禁思考）→ ~91%（思考 + top-k150 + clean 多数票）`是这条栈能买到的全量。**再往上只剩两条非模型杠杆**：
- **top-k 150 → 更大**：未实测、收益递减、预算税 2.4×（8547 vs ~3600 tok）之上再加；
- **open-domain 召回/候选覆盖**：真实召回缺口，单类最密集（21.9% 错题）。

提示词在此栈的理论上限就是非劣（038 的推广门 ≥−0.5pp），不是涨点。

## 5. 弱栈是提示词的主场（对照）

| 栈 | 结果 |
|---|---|
| fts + flash 禁思考（弱检索弱模型） | keep 43.2% → tplan 46.8%（+3.6pp）；temporal 46.1→57.3（+11.2pp） |
| 84 题子集 × 3-rep majority | keep 19.4% → tplan 32.9%（+13.5pp），temporal +15.8pp，p=0.000729 |

弱栈即使上更强 prompt 也到不了 90——fts 弱检索的候选覆盖本身就差，**提示词在弱栈只能拿回"已在上下文里"的增量，拿不回调不到的**。提示词工程的有效区间 = 廉/弱栈是主场、强栈是补集 ≈ 0。

## 6. 对 038 与未来实验的用法

- **归因标尺**：任何新 prompt 实验先定位在替代曲线（§2）上——若在"强栈 + 思考"一侧，预期边际 ≤ 0；若出现显著涨点，优先怀疑 in-sample 泄漏（038 用"零示例、零 benchmark 词"静态门自守，正是为此）。
- **038 预期校准**：统一契约的价值是一致性/可迁移性/无泄漏，不是涨分；其推广门（LoCoMo 非劣 ≥−0.5pp + held-out 行为门）与替代曲线预测一致。
- **90+ 归因不可挪用**：LoCoMo 90+ 用 legacy prompt 拿到，不能替统一契约背书；LME 单次 90% 是噪声/泄漏，不可作任何能力声明。

## 7. 诚实边界

- 跨批 judge 漂移 ±2.5pp，绝对分不可跨批横比；只有同栈内 arm-to-arm 增删可作证据。
- 90+ 整段是"口径对齐（思考）+ 加预算（top-k）+ 多数票"买到的，与提示词契约无关。
- raw 92.66% vs clean 91.10% 的 1.56pp 差是 judge 从 thinking 前导读候选的稳定污染，**必须报 clean 口径**。
- 本表数字全部来自已收口报告，无新增测量；分数随后续实验更新。

## 8. 数据源

- `thinking-unlock-rationale.md`（思考解锁口径差 + vllm 提速 + flash 思考对照）
- `032-tplan-temporal-answer-verdict.md`（替代曲线 fts/生产栈/思考三点 + 子集配对）
- `topk-exploration-2026-08-11.md`（sweep 单调 + top-k150 全量 3-rep）
- `locomo-9110-repro-2026-08-12.md`（clean 3-rep majority 91.10% + 类别分布 + judge 作弊量）
- `90pp-direction-exploration.md`（oracle 91.62% + 裁决 tplan 证伪 −1 + v4-pro 89.03%）
- `lme-entity-verify-verdict-2026-08-13.md`（LME 单次 90% = in-sample + 噪声，repeat-3 88.53%）
- `lme-contract-arms-2026-08-12.md`、`lme-conflict-prompt-nogo-2026-08-12.md`、`answer-focus-prompt-verdict.md`（LME 提示词全 NO-GO/within-noise）
