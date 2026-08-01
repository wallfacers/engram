# Benchmark Registration: 025 跨消息语义聚类 episode

**Branch**: `025-semantic-episode-cluster` | **Date**: 2026-08-01 | **Spec**: [spec.md](spec.md)

本文件登记 025 的配对验证结果。025 在 022 三表示 bake-off 框架下把 `semantic_episode` 从"同 session 连续边界"扩展为"跨消息语义聚类"（`--episode-cluster` 机制 flag），并与 chunk_900 对照配对。

## 配对运行（正式可引用）

两臂在**同一 store、同一 frozen 协议家族、检索候选逐字节一致**下配对（这是可配对的关键前提，见下方"配对有效性核对"）：

| 臂 | 表示 | 机制 | 协议 hash | run-dir | 时间 | EXIT | validity |
|---|---|---|---|---|---|---|---|
| control-v2 | chunk_900 | 无 | `sha256:7bec07b3…`（025-control.json） | `/root/025-runs/control-v2` | 20:34→20:47 | 0 | `valid=true`, 全 rate=1 |
| treatment | semantic_episode | episode_cluster | `sha256:05013a36…`（025-treatment.json） | `/root/025-runs/treatment` | 19:55→20:20 | 0 | `valid=true`, 全 rate=1 |

- 两协议仅差 `experiment/mechanism_flags/episode_cluster`（treatment 有，control 无），其余（models/budget/retrieval/ingestion digest）完全一致。
- 检索候选一致性抽查：`conv-0-q-0`、`conv-6-q-2`、`conv-0-q-16` 双臂 anchors 完全一致（n=30）。
- answer 模型 Qwen3.6-35B-A3B-FP8（vllm, port 8000），judge deepseek-v4-flash，embedding bge-large-en-v1.5（port 8010）。answer_context_tokens_mean: control 3399 / treatment 3428（同预算 cap 3600）。
- 噪底：LoCoMo same-config 重跑 Δ≈0.84–0.93pp（024 登记）。

## 分类别结果（3 次答题多数，LoCoMo B1-high 1,540）

| 类别 | control-v2（chunk_900） | treatment（semantic_episode） | Δ（pp） |
|---|---|---|---|
| multi-hop | 84.0%（ci95 [82.3, 85.8]） | 84.0%（ci95 [81.7, 86.4]） | **0.0** |
| open-domain | 60.8%（[50.0, 71.5]） | 62.2%（[53.1, 71.2]） | +1.4 |
| single-hop | 84.8%（[84.1, 85.6]） | 76.7%（[75.4, 77.9]） | −8.1 |
| temporal | 80.5%（[76.8, 84.1]） | 64.5%（[56.3, 72.6]） | −16.0 |
| **OVERALL** | **82.3%**（[81.0, 83.6]） | **74.6%**（[71.7, 77.4]） | **−7.7** |

## Verdict（R7 promotion rule：primary cohort Δ≥+2.0pp **且** McNemar p<0.05）

- **multi-hop（目标 cohort）Δ = 0.0pp → 无提升。** 未达 +2.0pp promotion 阈值，promotion 判负（无需再算 McNemar）。
- **OVERALL Δ = −7.7pp，single-hop −8.1pp、temporal −16.0pp → 显著负收益。**
- **结论：跨消息语义聚类 episode 表示在 LoCoMo B1-high 上是负结果，机制保持默认关，`--episode-cluster` 不进入默认路径。**（FR-010/FR-011 verdict：不提升 multi-hop，且回归单跳/时间类。）

## 负收益机制（已定位）

同一候选下，treatment 相对 control：

- `candidate_miss` 231 → 351（+120），`answerer_miss` 128 → 169。
- coverage 高覆盖 stratum `[0.900,1.000]` 题数 1310 → 1186（−124）。

原因：episode 聚合把多 source 证据压缩进一个 item，在 token cap（3600）内挤掉了其他锚点的逐证据覆盖。简单类目（single-hop/temporal）更需要精确的逐证据命中而非聚合叙事，故回归显著；multi-hop 因已能通过聚合覆盖所需证据，既无提升也无损失。这是**机制性负结果**，不是实现缺陷（validity 全绿、候选一致、clusterer 正常触发：每 conversation 19–36 个 episode，每题平均 6.56 个 episode item）。

## 配对有效性核对（US3 复核记录）

初版 treatment 曾对比旧 control（17:53, protocol `sha256:7bec07b3`，旧二进制 + 旧 store evidence `01KYYA…`），二者检索候选不一致、**不可配对**，该对比作废。正确配对必须：同 store（evidence `01KYYK…`）→ 候选逐字节一致 → 只差 episode_cluster 机制。本文档数字仅基于有效配对（control-v2）。

## 资产位置

- 配对两臂 run 目录：`/root/025-runs/control-v2`、`/root/025-runs/treatment`（AutoDL，协议冻结于 `/root/025-runs/frozen/`）。
- 本地汇总：`~/.claude/scratchpad/025-results/paired-v2-comparison.txt`。
- 详细分析报告：`docs/evaluation/reports/semantic-episode-cluster.md`。
