# Benchmark Registration: 026 查询期 verbatim 证据编译

**Branch**: `026-verbatim-evidence-compile` | **Date**: 2026-08-02 | **Spec**: [spec.md](spec.md)

本文件登记 026 的配对验证结果。026 在 022 冻结协议（B1-high）下把 query-time 证据编译（`--compiler-arm extractive|exact_token`）作为 **b1 additive mechanism**（`mechanism_flags{compiler:true}`，与 024 density / 025 episode_cluster 同构）配对验证，compiler 引擎（022 交付的 `memory/evidencecompiler/`）在检索候选池内按查询相关性选择/重排 verbatim 证据喂给 answerer。

## 配对运行（正式可引用）

三臂在**同一 store、同一协议家族、同一冻结候选**下配对（协议仅差 `experiment/mechanism_flags/compiler`）：

| 臂 | 机制 | 协议 hash | run-dir | 时间 | EXIT | validity |
|---|---|---|---|---|---|---|
| control | b1/legacy_count_packer（无 compiler） | `sha256:666084b0…`（026-control.json） | `/root/026-runs/control` | 22:07→22:20 | 0 | `valid=true`, 全 rate=1 |
| extractive | b1 + `compiler:true`（extractive） | `sha256:4d7d6b0f…`（026-extractive.json） | `/root/026-runs/extractive` | 00:33→01:06 | 0 | `valid=true`, 全 rate=1 |
| exact-token | b1 + `compiler:true`（exact_token） | `sha256:b04a3914…`（026-exact-token.json） | `/root/026-runs/exact-token` | 01:08→01:19 | 0 | `valid=true`, 全 rate=1 |

- 协议仅差 `experiment/mechanism_flags/compiler`（treatment 有、control 无），其余（models/budget/retrieval/ingestion digest）完全一致。
- 检索候选逐字节一致（T114 candidate-replay 契约：`compileFormalSources` 三臂共享同一 flat source list）。
- answer 模型 Qwen3.6-35B-A3B-FP8（vllm, port 8000），judge deepseek-v4-flash，embedding bge-large-en-v1.5（port 8010）。answer_context_tokens_mean: control 3399 / extractive 2205 / exact-token 2572（同预算 cap 3600）。
- 噪底：LoCoMo same-config 重跑 Δ≈0.84–0.93pp（024 登记）。

## 分类别结果（3 次答题多数，LoCoMo B1-high 1,540）

| 类别 | control | extractive | Δ | exact-token | Δ |
|---|---|---|---|---|---|
| multi-hop | 85.1%（[81.0, 89.2]） | 79.4%（[77.7, 81.2]） | **−5.7** | 82.0%（[78.4, 85.7]） | **−3.1** |
| open-domain | 63.2%（[60.2, 66.2]） | 58.0%（[50.5, 65.5]） | −5.2 | 61.1%（[57.2, 65.1]） | −2.1 |
| single-hop | 84.9%（[83.3, 86.4]） | 79.7%（[77.0, 82.5]） | −5.2 | 80.4%（[78.0, 82.7]） | −4.5 |
| temporal | 80.7%（[75.9, 85.5]） | 78.6%（[73.9, 83.3]） | −2.1 | 77.9%（[74.8, 81.0]） | −2.8 |
| **OVERALL** | **82.6%**（[81.7, 83.5]） | **78.1%**（[77.7, 78.4]） | **−4.5** | **79.0%**（[76.9, 81.0]） | **−3.6** |

## Verdict（R7 promotion rule：primary cohort Δ≥+2.0pp 且 McNemar p<0.05）

- **multi-hop（compiler 的目标类别）Δ = −5.7pp（extractive）/ −3.1pp（exact-token）→ 无提升，未达 promotion 阈值。**
- **OVERALL Δ = −4.5 / −3.6pp → 显著负收益。**
- **结论：查询期 verbatim 证据编译在 LoCoMo B1-high 上是负结果，机制保持默认关，`--compiler-arm` 不进入默认路径。**（FR-011 verdict：负收益默认关。）

## 负收益机制（已定位）

同一候选下，compiler 相对 control：

- **token 更少但信息不足**：mean_input_tokens 3399 → 2205 / 2572（−33% / −24%）。compiler 按 need 剪枝（admit/drop）把证据压缩进预算，simple 类目（single-hop/temporal）更依赖精确逐证据命中而非剪枝后的相关子集，故回归；multi-hop 也因剪枝丢失跨消息证据链而无提升。
- **exact-token > extractive**（−3.6 vs −4.5pp）：exact-token 的 verbatim 整源保留比 extractive 的句子级剪枝保留更多完整证据 span，但仍低于 control 的逐证据全量打包。
- 这是**机制性负结果**，不是实现缺陷（三臂 validity 全绿、候选逐字节一致、token 契约与 digest 自洽——见下方修复记录）。

## 修复记录（022 compiler 集成缺陷，全量 LoCoMo 暴露，026 验证期修复）

026 全量配对跑出前四轮全部 invalid，逐层诊断定位并修复了 **022 compiler 集成的四个真实缺陷**（全部在 harness 桥接 `cmd/locomo-bench/`，引擎 `memory/` 零改动）：

1. **B1 anchor-prefix 契约冲突**（`validateFormalB1CompilerAnchorPrefix`）：compiler 按 relevance 重选/重排 bundle，非 mechanical ranked prefix，旧 rank-prefix 校验拒绝全部 1540 题。放宽为可审计的 1:1 item 集（逐项映射 rendered candidate、whole-source allowed span、KEEP/EXTRACT）。
2. **候选重复**（`buildCompileCandidates` + `compileBundleItems` 去重）：多 hit 命中同一 candidate 未去重，compiler 产出重复 bundle item，401/1540 结构校验失败。
3. **token 契约口径**（`formalCompileRenderer` + `buildCompileBundle`）：compiler 计数只算证据、不含 query，冻结的 `AnswerInputTokens` 与 answer 阶段 harness preflight 必然不一致（1139/1540 无分）。renderer 计入 query，`AnswerInputTokens` 由 harness counter 对最终 answer input 重数固定。
4. **digest 不自洽**（`revalidateFrozenFormalSources`）：改 `Trace.Valid` 后未重算 `TraceDigest`，第二次 envelope 校验 mismatch 致全题 invalid + `source_state_drift`；且 `EvidenceTokens`（compiler 口径）> `AnswerInputTokens`（harness 口径）破坏 citation 门。`EvidenceTokens` 改 harness 口径（全量−静态），revalidate 改 Valid 后重算 digest。

修复后三臂 validity 全绿、全 rate=1，修复 commit：`6d19f31`、`7b40fce`。

## 配对有效性核对

三臂同 store、同 frozen 协议家族（protocol hash 稳定）、compiler 臂共享同一 flat candidate list（T114 candidate-replay 逐字节一致）。本结果数字基于有效配对。

## 027 更新：control 打包粒度修复（control 基线已更新）

027 修复了 B1 control 的打包粒度（`rebuildExpandedForChunkVerbatim`：bundle 打包投影原文
而非 source-expand 成单条消息）并把默认 `answer-input-cap` 从 3600 提到 5000。该修复
**明确排除 compiler 臂**（`materializeFormalB1Question` 仅在 `compilerArm == ""` 时启用
fold），故本表三臂的候选逐字节一致与 compiler 负收益的方向结论不受影响；但 `compiler:false`
的 control 参照从 82.6% 更新为 **84.7%**（stats OVERALL 口径，027 cap5000 于 022 历史
store；3 次多数 majority 85.19%，validity 全绿，protocol `sha256:263b52b6…`）。
compiler 相对**旧** control 的 −4.5pp/−3.6pp 幅度不能直接平移；在新 control 基线下
重新配对验证（同一新 store + 新 frozen 协议族）尚未运行，compiler 保持默认关的结论不变。

## 资产位置

- 三臂 run 目录：`/root/026-runs/{control,extractive,exact-token}`（AutoDL，协议冻结于 `/root/026-runs/frozen/`）。
- 本地脚本：`~/.claude/scratchpad/026-rerun-extractive.sh`、`026-rerun-exact-token.sh`。
- 后续：LongMemEval-S 500 配对（T017）未跑——LoCoMo 已负，优先级低。
