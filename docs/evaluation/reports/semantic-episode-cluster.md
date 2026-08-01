---
title: 025 跨消息语义聚类 episode 表示配对验证
summary: 在 022 三表示 bake-off 框架下把 semantic_episode 从"同 session 连续边界"扩展为"跨消息语义聚类"（--episode-cluster），与 chunk_900 在同一 store、候选逐字节一致的配对下验证。结论：multi-hop（目标 cohort）0.0pp 零提升，single-hop −8.1pp、temporal −16.0pp、OVERALL −7.7pp 显著负收益。机制性负结果，按 FR-011 保持默认关并记录 verdict。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-01
canonical_for: [semantic-episode-cluster]
tags: [evaluation, locomo, 025, semantic-episode, cross-message-clustering, paired-ablation]
---

# 025 跨消息语义聚类 episode 表示

## 背景与动机

022 交付了 episode 引擎资产（`memory/episode.go` 的 EpisodeSegmenter/EpisodeStore、`representation_eval.go` 的 `ReprSemanticEpisode` 渲染器），原设计是"同 session 连续边界"聚类。025 把该机制扩展为**跨消息语义聚类**：把语义相关的多跳证据（跨原始消息/跨 session）聚类成 episode 候选，缩小同预算对 MemOS 的信息密度差距（同预算 1083 tok 76.85% vs 1059 tok 82.47%）。

动机链：024 四臂负结果证明"共享 evidence 的机械兄弟"（write_dedup/neighbor_extend）无法缩小信息密度差距；multi-hop 类目最缺的是跨消息的语义相关证据。025 把"跨消息语义聚类"作为新的信息密度杠杆。

实现：`SemanticClusterer`（offline 信号：共享 entity token 或 keyword Jaccard ≥0.25；可选 embedding 余弦叠加，默认关）+ `EpisodeStore.RebuildAll`（跨 session 重建，config-hash 幂等）+ `EpisodesForEvidence`（evidence→episode 反查）。详见 [specs/025-semantic-episode-cluster/spec.md](../../../specs/025-semantic-episode-cluster/spec.md)。

## 方法

- **配对设计**：`--episode-cluster` 作为 b1 additive density mechanism flag（与 024 的 write_dedup/neighbor_extend 同构），冻结于 `025-treatment.json`；对照 `025-control.json` 仅差该 key。
- **关键前提——同 store 配对**：两臂复用**同一 store**（evidence `01KYYK…`），跳过 extraction 重建，检索候选逐字节一致（抽查 3 题 anchors 完全一致）。保证差异**仅**来自 episode 聚类/表示，而非检索差异。
- 协议 cap 3600、retrieval hybrid、answerer Qwen3.6-35B-A3B-FP8、judge deepseek-v4-flash、3 次答题多数。
- **配对有效性纠偏**：初版 treatment 曾对比旧 control（旧二进制+旧 store `01KYYA…`），候选不一致、不可配对，该对比作废。本文档数字仅基于有效配对。

## 结果（LoCoMo B1-high，1,540 题）

| 类别 | control-v2（chunk_900） | treatment（semantic_episode） | Δ（pp） |
|---|---|---|---|
| multi-hop | 84.0% | 84.0% | **0.0** |
| open-domain | 60.8% | 62.2% | +1.4 |
| single-hop | 84.8% | 76.7% | −8.1 |
| temporal | 80.5% | 64.5% | −16.0 |
| **OVERALL** | 82.3% | 74.6% | **−7.7** |

两臂 validity 均 `valid=true`（candidate_identity / source_validation / span_recovery / citation_coverage / within_cap / answer_call_compliance 全 rate=1），契约修复已生效（方向 A：episode 项允许多 source 整源 span）。

## 负收益机制

同一候选下，treatment 相对 control：

- `candidate_miss` 231 → 351（+120）
- `answerer_miss` 128 → 169
- 高覆盖 stratum `[0.900,1.000]` 题数 1310 → 1186（−124）

**解释**：episode 聚合把多 source 证据压缩进一个 item，在 token cap 内挤掉了其他锚点的逐证据覆盖。简单类目（single-hop/temporal）更需要精确的逐证据命中而非聚合叙事，故回归显著；multi-hop 因已能通过聚合覆盖所需证据，既无提升也无损失。这是**机制性负结果**，非实现缺陷——validity 全绿、候选一致、clusterer 正常触发（每 conversation 19–36 个 episode，每题平均 6.56 个 episode item）。

## Verdict

按 R7（primary cohort = multi-hop Δ≥+2.0pp 且 McNemar p<0.05 才 promotion）：

- multi-hop Δ = **0.0pp** → **未达 promotion**。
- OVERALL −7.7pp → **显著负收益**。
- **结论：跨消息语义聚类 episode 表示在 LoCoMo B1-high 上为负结果，`--episode-cluster` 保持默认关，不进入默认路径。**

## 后续方向（若继续）

- 缩小 episode item 体积（降 max-evidence），或只在 multi-hop 检索命中时启用聚合，避免伤害单跳/时间类。
- 用同预算 MemOS 1083/1059 tok 小预算重测——025 的原始动机是缩信息密度差距，大预算下聚合叙事挤占覆盖的负效应可能在同预算下更清晰。
- 本实验与 024 一致地确认：**提升信息密度的方向需要"逐证据命中覆盖"而非"证据聚合"**。

## 资产

- 配对 run：AutoDL `/root/025-runs/control-v2`、`/root/025-runs/treatment`；协议冻结 `/root/025-runs/frozen/`。
- 登记：[specs/025-semantic-episode-cluster/benchmark-registration.md](../../../specs/025-semantic-episode-cluster/benchmark-registration.md)。
