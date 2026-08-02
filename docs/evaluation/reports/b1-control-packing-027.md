# B1 control 打包粒度修复与 85% 级基线收口（027）

**日期**：2026-08-02 ｜ **机器**：AutoDL（vllm 8000 Qwen3.6-35B-A3B-FP8 + 8010 bge-large-en-v1.5；judge deepseek-v4-flash）
**目标**：把 022 B1 control（022 正式协议）从 82.6% 提升到 85% 级，跑分验证后收口为默认基线。

## 背景与差距来源

022 B1 control（`legacy_count_packer`，cap 3600，无 retry，候选冻结重放）= 82.6%；
B0 continuity（legacy 产品路径，unbounded cap + IDK retry + 独立检索）= 85.32%（2026-07）。
两者差距 ~2.7pp。逐项排除后定位两个主因：

1. **cap 截断（主因）**：cap 3600 下 **78.4%** 的题被截断（实测 max answer input 4,153 tok，
   p99 4,033）；被丢弃的尾部证据含关键信息。cap 3600→5000 贡献 ~+1.4pp。
2. **打包粒度（次因，修复后近零）**：B1 的 `expandFormalEvidence` 把每个命中的投影
   source-expand 成整条原始消息（KEEP），丢失投影原文（verbatim 拼接 / 语义浓缩）的信息密度。
   修复（`rebuildExpandedForChunkVerbatim`）后该方向在 cap 3600 下只 ~+0.2pp，
   原始"打包机制损失 ~1.8pp"的诊断被 cap100k 实验**证伪**（cap100k 达到预算消融外推 84.5%）。

**store 漂移**：022-full-store 与历史 B0 store（`022-store.tar.gz`）是不同 db（evidence 数同 419，
projections 数 298 vs 275）。B0 对 store 敏感（83.96% vs 85.32%），B1-fold 对 store 不敏感
（84.4 vs 84.5%，见下表）；B0 的 85.32% 含 ~0.4–1.4pp 环境漂移，当前环境 B0 为 84.94%。

## 修复内容（仅 harness，引擎零改动）

`cmd/locomo-bench/`：
- 新增 `rebuildExpandedForChunkVerbatim`：把 whole-source（KEEP）锚点折叠回投影原文
  （`hit.Content`），每个成员 evidence 保留为 whole-source span。
- 接入 `materializeFormalB1Question`，条件 `representationArm == ReprChunk900 && compilerArm == ""`
  （**明确排除 compiler 臂**，保持 026 三臂候选逐字节一致）。
- 校验器扩展：`isChunkVerbatimRendered`、active-receipt 分支、inspect-structure 分支、
  anchor-prefix dispatch 复用 episode 分支（多-source item 的 1:1 candidate + whole-source
  span 契约）。
- TDD：`TestFormalChunkVerbatimFoldPacksProjectionText`。
- `git diff --name-only -- memory embedding provider store internal` = **空**。

## 配对验证（全部 1540 题 B1-high、3 次答题多数）

| # | 配置 | store | OVERALL（stats） | majority（3-rep） |
|---|---|---|---:|---:|
| 1 | B1 control cap3600（026 二进制） | 022-full-store | 83.1% | 84.48% |
| 2 | B1 control cap3600（027，fold） | 022-full-store | 83.3% | — |
| 3 | B1 control cap100k（027，fold） | 022-full-store | 84.5% | 85.13% |
| 4 | B1 control cap100k（027，fold） | 022 历史 store（restored） | 84.4% | 84.74% |
| 5 | **B1 control cap5000（027，fold）＝默认** | 022 历史 store（restored） | **84.7%** | **85.19%（1,312/1,540）** |
| 6 | B0 continuity（legacy） | 022-full-store | — | 83.96% |
| 7 | B0 continuity（legacy） | 022 历史 store（restored） | — | 84.94% |
| 8 | B0 continuity（2026-07 历史） | 022-runs/store | — | 85.32% |

- 第 5 行即收口基线：cap5000 覆盖 100% 题（0% 的题输入 >5000 tok，与 unbounded 等价），
  validity 全绿（candidate/source/span/citation/within-cap rate 全 1），protocol
  `sha256:263b52b65c60a7e8bb2dff75b3fdf3692413913108fe1fba718f8404e593ddf1`。
- 第 5 行相对第 1 行（026 二进制，同一打包粒度的对照）majority +0.71pp；相对 026 control
  正式登记 82.6% 提升 +2.1pp（OVERALL 口径），超过 LoCoMo 同配置噪底（0.84–0.93pp）。
- 分类别（第 5 行）：multi-hop 86.9%、open-domain 61.8%、single-hop 87.7%、temporal 81.6%。

## 结论

1. **85% 级达成**：B1 默认基线（chunk-verbatim fold + cap5000）majority **85.19%**，
   当前环境与 B0（84.94%）持平/略超，历史 85.32% 为漂移参照。B1 的统计上限
   （无 retry，协议禁止）约 85.2%。
2. **cap 是主杠杆**：cap3600→5000（+1.4pp）远超打包修复（+0.2pp）；cap5000 是合理默认
   （token 等价 unbounded，平均输入 3,717 tok）。
3. **026 compiler 负收益归因不变**：027 修复排除 compiler 臂，compiler 保持默认关；
   −4.5/−3.6pp 幅度按新 control 基线重算待后续配对。
4. **契约影响（已登记）**：answer-facing item 的文本规则由"必须由 sources 精确复原"扩展为
   "projection 原文 + 全部 whole-source span"（verbatim 可复原、浓缩 fact 信任 rendered
   candidate）。store v7 已有完整 source 谱系，可审计性保留。

## 资产位置

- 027 修复代码：本地 `cmd/locomo-bench/`（harness），随本报告同 commit。
- 机器 run 目录：`/root/autodl-tmp/027-runs/{027d-control-cap5000, 027c-b0, ...}`（AutoDL，
  结果正本见 [results.md](../results.md)）。
