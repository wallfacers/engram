# Benchmark Registration: 024 记忆密度杠杆

**Branch**: `024-memory-density` | **Date**: 2026-07-31 | **Spec**: [spec.md](spec.md)

本文件登记 024 实验的冻结基线与其资产位置。宪法 IV 要求任何机制改动不得回归基线，四臂配对（T018-T021）以此处的数字为对照。

## 冻结基线（正式可引用）

| 基准 | 分母 | 基线分 | 口径 | 来源 |
|---|---|---|---|---|
| LoCoMo B0（cat 1–4） | 1,540 | **85.32%**（1,314/1,540） | 3 次答题多数、本地 hybrid、Qwen3.6-35B answerer + deepseek judge | [docs/evaluation/results.md](../../docs/evaluation/results.md)（022 continuity-only，正式 B0） |
| LoCoMo B1-high | 1,540 | **82.1%** | 022 正式 B1 全量 run（AutoDL，冻结协议 `b1-high.json`） | AutoDL `/root/022-runs/`（远程资产，见下） |
| LoCoMo B1-low | 1,540 | **58.8%** | 022 正式 B1 low-profile run | AutoDL `/root/022-runs/`（远程资产，见下） |
| LongMemEval-S（cleaned）full 500 | 500 | **80.80%** | 本地栈历史 full 500、3 次答题多数 | [docs/evaluation/results.md](../../docs/evaluation/results.md)（⚠️ 受"LongMemEval-S 口径更正待刷新"约束，见该文件） |

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
