# 90pp Direction Exploration — 分数地图与可行路线

**Date**: 2026-08-11
**Goal**: 把 LoCoMo 诚实基线跨过 90%（1386/1540）。
**Status**: 探索诊断（非 verdict），含一次机制实证证伪。数据来自本机 034/035 真实产物 + README 基线 + v4-pro 探针。

## 一、分数地图（当前所有诚实口径）

| 口径 | 分数 | 距 90% |
|---|---:|---:|
| 034 oracle（三候选任一正确） | **91.62%**（1411/1540） | 已超 ✓（候选空间上界） |
| 034 裁决 selected | **89.48%**（1378/1540） | **+8 题** |
| v4-pro 3-rep majority | 89.03%（1371/1540） | +15 题 |
| Qwen + trace（默认栈） | 85.91% @468tok | +51 题 |

**核心事实：90% 完全在候选空间内（oracle 91.62%）。** 差距只在"裁决/生成怎么利用 33 题缺口"。

## 二、裁决协议的价值与上限

034 裁决相对 majority 的**净增量 = +7**（救回 23 题，改错 16 题）。改错的 16 题里 6 题是
temporal factually_wrong。**若裁决少改错 8 题（16→8），净增量翻倍到 +15，直接 90%+。**

## 三、33 缺口的真实本质（人工重分类）

- **真推理错误仅 8 题**（temporal 7 + multi 1）：conv-3-q-41（9Sep→5Oct）、4-q-20（NYC→Seattle）、
  4-q-64（Nov→Dec）、4-q-69（Jan→Feb）、6-q-31（9天→10天）、6-q-59（7May→30Oct）、8-q-63（10Oct→7-8Oct）、8-q-4（None→painting）
- **语义改写/judge 粒度 14 题**（7 真语义等价 + 7 真不同）
- semantic_equivalence 7 + evidence_insufficient 4

## 四、裁决 tplan 契约实证（v4-pro 全量 33 缺口探针）— 证伪

实现 `--adjudication-temporal-prompt`（category-conditional，默认 off byte-identical）后，用
v4-pro（禁思考，temp=0，与 034 裁决同构）对全部 33 缺口做 generic vs tplan 双臂探针：

| 臂 | 33 缺口救回 | temporal 12 救回 |
|---|---:|---:|
| generic | 8 | 3 |
| tplan | 7 | 2 |
| 净 | **-1** | **-1** |

**结论：裁决侧 tplan 契约在 v4-pro 上无增益（净 -1），证伪。** 032 的 tplan 增益只在弱模型
（flash）成立；强模型（thinking-on 的 v4-pro）已隐含时序推理能力，显式契约反而干扰。与 032
生产栈结论（高 base 下 +0.5pp within-noise）一致。

**副产物**：generic v4-pro 单次重跑救回 8/33——是 v4-pro 相对 034 当时的随机性/更强，非稳定增量。
temporal 缺口多数题在 gen/tpl 两次都选错，说明**证据本身不足以区分候选**，非 prompt 契约问题。

代码改动（已测试，默认 off）：`--adjudication-temporal-prompt` + `adjudicationSystemPromptFor` +
`adjudicationTemporalSystemPrompt`。**不转正，保留为可选诊断 flag。**

## 五、open-domain 是最大绝对缺口但最分散

v4-pro 分类别：single-hop 90.96% ✓、temporal 89.41%（差2）、multi-hop 88.65%（差4）、
**open-domain 71.88%（差17）**。open-domain 27 题错，错因分散（judge 粒度、检索漏、答案推断），
无单点杠杆；但 budget-ablation 显示其对预算不敏感且 engram 一直领先 MemOS。

## 六、90pp 可达性判定（已实证收紧）

90.0% = 1386 = 1378（裁决）+ 8。33 缺口实测可救构成：

| 机制 | 实测救回 | 状态 |
|---|---:|---|
| 裁决 tplan 契约（v4-pro） | **-1** | **证伪**（本机实测） |
| 裁决多数票（temp=0 3-rep） | **无效**（3-rep 完全一致） | **证伪**（temp=0 确定性） |
| judge 语义粒度对齐 | +3（7 语义等价里 majority 已对 4） | 未实测，估计 |
| 更强裁决整体替换（v4-pro generic） | +8 单次 | 系统性替换，非增量机制 |

**判定：单靠"裁决契约 + judge 宽松 + 多数票"到不了确定性的 90%。** tplan 契约和多数票已实证
证伪（多数票因 temp=0 确定性，3-rep 完全一致）。judge 宽松上限 +3。8/33 的"救回"是 v4-pro 相对
034 当时的系统性强弱差（temp=0 确定性，无法靠重复投票放大），本质是"换更强裁决器"的整体替换而非
增量杠杆。剩余路径只有系统性大杠杆（更强 answerer/裁决，或生成前置）。

## 七、诚实边界

- v4-pro 探针为单次（temp=0 确定性，但模型本身有随机性），未做 3-rep。
- tplan 契约在 flash 弱模型先导救回 2/12，v4-pro 净 -1——弱模型先导有噪声，不能外推强模型。
- open-domain 到 90% 需 +17 题，是最大但最分散的缺口，不建议作为主攻。
- 未发起任何全量 1540 provider 验证（除 33 缺口探针）；所有 90pp 数字基于 034 sealed 数据推算。

## 八、决策点（给维护者）

90pp 需要强 answerer/裁决或生成前置这类大杠杆，不是已建机制的简单组合。三条路线：
1. **更强裁决多数票**：v4-pro generic 多 rep majority（探针单次救 8，majority 可稳）——低成本，
   但只补裁决不确定性，上限约 +8。
2. **生成前置**（035 指定方向）：裁决前用血缘原始上下文重新生成候选，推高 oracle 上限。改动最大。
3. **接受 89.48% 为当前诚实上限**：90pp 需新机制，当前已建机制（tplan/judge）均证伪或边缘。

**诚实底线**：不宣称 90pp 已达成。探索证明它"在候选空间内但需新大杠杆"，tplan 契约已实证非杠杆。
