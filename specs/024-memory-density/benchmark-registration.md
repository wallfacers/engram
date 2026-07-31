# Benchmark Registration: 024 记忆密度杠杆

**Branch**: `024-memory-density` | **Date**: 2026-07-31 | **Spec**: [spec.md](spec.md)

本文件登记 024 实验的冻结基线与其资产位置。宪法 IV 要求任何机制改动不得回归基线，四臂配对（T018-T021）以此处的数字为对照。

## 冻结基线（正式可引用）

| 基准 | 分母 | 基线分 | 口径 | 来源 |
|---|---|---|---|---|
| LoCoMo B0（cat 1–4） | 1,540 | **85.32%**（1,314/1,540） | 3 次答题多数、本地 hybrid、Qwen3.6-35B answerer + deepseek judge | [docs/evaluation/results.md](../../docs/evaluation/results.md)（022 continuity-only，正式 B0） |
| LoCoMo B1-high | 1,540 | **82.1%** ⚠️ 参照，非正式基线 | 022 B1 high-profile run | AutoDL `/root/022-runs/`（远程资产，US3 开机后核对 protocol_hash 再引用） |
| LoCoMo B1-low | 1,540 | **58.8%** ⚠️ 参照，非正式基线 | 022 B1 low-profile run | AutoDL `/root/022-runs/`（远程资产，数字有冲突，见下节） |
| LongMemEval-S（cleaned）full 500 | 500 | **80.80%** | 本地栈历史 full 500、3 次答题多数 | [docs/evaluation/results.md](../../docs/evaluation/results.md)（⚠️ 受"LongMemEval-S 口径更正待刷新"约束，见该文件） |

### B1-low 基线数字冲突（评审 2026-08-01 记录，需 US3 核对）

同一 LoCoMo B1-low（cap 1,100）存在三个互不印证的来源：

| 来源 | 数字 | 状态 |
|---|---|---|
| 022 报告 rejected run（`940947…`，排序修复前） | 62.79%（957/959/964 三次 repetition） | 因 read-back 排序 bug **被拒**，仅作诊断，非分数 |
| 本地修复后 fresh run（`runs/locomo-b1-low`，`a49545dd`，cd8b81d） | **40.32%**（621/1,540），`summary.json valid=true` | self-valid 但 **未被正式验收**：无独立 read-back 记录、无 judge audit；且 `force_answer=false`（report 的 rejected run 口径是否一致未核对） |
| 本登记文档原始 58.8% | 58.8% | 来源 AutoDL `/root/022-runs/`，**远程资产，当前无法核对** |

**结论**：58.8% 与本地两个 run 都不一致，且本地 fresh run 40.32% 虽 self-valid 却无正式验收证据。因此 **B1-low 不作为 024 四臂的可依赖对照**；四臂以 B0 85.32%（本地正式、valid continuity）和 LongMemEval-S 80.80% 为主对照，B1-high 82.1% 与 B1-low 仅作参照（022 B1 正式 verdict 仍 HOLD）。US3 开机后必须：核对 AutoDL `b1-low.json` 的 protocol_hash → 确认 58.8% 的真实性与口径 → 再决定 B1-low 是否可引。

### 噪声带

- 同配置重跑 Δ = **0.84–0.93pp**（LoCoMo same-config re-run）。
- 配对判断以 exact McNemar 为准；overall 差异在噪声带内视为不显著回归。

## 资产位置

| 资产 | 位置 | 状态 |
|---|---|---|
| 022 冻结协议 `b1-high.json` / `b1-low.json` | AutoDL `/root/022-runs/` | 远程可用；US3 开机后核对 protocol_hash 再引用 |
| LoCoMo 全量数据 `locomo.json` | AutoDL + testdata/locomo（gitignored，不重新分发） | 本地仅 10 题小样本；全量需在 AutoDL 或自带数据 |
| LongMemEval-S cleaned 500 | `testdata/longmemeval/longmemeval_s_cleaned.json` | ✅ 本地就绪（277 MB） |

## 同预算配对的口径

- 四臂（关/开 write_dedup × 关/开 neighbor_extend）在 **固定 answer-context token cap** 下配对。
- 022 协议预算 profile：**high = 3,600 cap / low = 1,100 cap**（`--answer-input-cap`）。
- 本 feature 的配对消融选 **high profile（3,600）** 作为主口径（与 B0/B1-high 基线可比），low 作为稳健性复核。
- 任一新机制不显著回归 85.32%（B0）与 LongMemEval-S 80.80% 为通过；B1-high 82.1% 为参照但不作硬门（022 B1 正式 verdict 仍 HOLD，见 022 report）。

## 四臂配对结果（2026-08-01 已跑完）

**Verdict: 两机制均为负结果 → 保持默认关（FR-011）。** 完整报告见 [docs/evaluation/reports/memory-density-four-arm.md](../../docs/evaluation/reports/memory-density-four-arm.md)。

| 臂 | 机制 | 正确/1540 | Acc | Δ vs control |
|---|---:|---:|---:|---:|
| control | 无 | 1298 | **84.29%** | — |
| neighbor | +extend | 1291 | 83.83% | −0.46pp |
| dedup | +dedup | 1284 | 83.38% | −0.91pp |
| both | 双机制 | 1278 | 82.99% | −1.30pp |

要点：
- 机制叠加 accuracy 单调下降（−0.46 → −0.91 → −1.30pp），无叠加收益。
- write_dedup 在 LoCoMo 上几乎不触发：21,860 次判定仅 20 次抑制（0.09%），疑似误伤 5 例（误伤率 25%）。
- dedup 的 multi-hop +3.2pp（89.72%）被 open-domain −4.2pp 抵消，净负。
- control 臂 84.29%（summary 口径）未回归 022 基线；四臂全部 `valid=true`，protocol_hash 归因成立。
- 局限：仅在 LoCoMo 验证，LongMemEval-S 未跑（T021 说明）；neighbor×dedup 组合含 store 差异混淆变量。
