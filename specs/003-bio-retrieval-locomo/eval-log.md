# Feature 003 Evaluation Log

This file records maintainer-run evaluations. Every run must be preceded by
`--estimate`; retain the estimate, actual cost, statistics, comparison verdict,
and the keep/revert decision together.

## Strike 0: Calibration Baseline

- Date: 2026-07-19 (10:01–12:42 +08:00, 单次连续运行)
- Dataset / repeats: locomo10.json 全量 10 段 × 1540 题(可判分四类) × repeats=5, single-pass (--no-idk-retry), hybrid, top_k=30
- Answer model: gpt-5.6-sol (答题+判分, 中转站 tokensfree)
- Extract model candidates: A=gpt-5.6-luna, B=gpt-5.6-sol
- Frozen extract model: **gpt-5.6-luna**（见 Decision）
- Estimate output: A ¥76.36 / B ¥77.89（合计 ¥154.25）
- Actual cost (`cost.json`): A **¥25.10** / B **¥48.81**（合计 **¥73.91**，为预估 48%）。
  高估主因：答题输出实测均值 ~47 tok/题（含 reasoning），远低于预估假设 300 tok/题。
  A 臂 answer 输入 12.1M tok vs B 臂 27.3M tok——luna 抽取的记忆更紧凑
  （answer_context_tokens_mean 1576 vs 3545），连带砍半答题成本。
- Run directories: `.locomo-run/strike0/extract-{a,b}/run-{1..5}`
- `stats.json`: A overall 51.0% CI95=[40.7, 61.4]; B overall 50.6% CI95=[37.3, 63.8]。
  per-category (A/B mean%): multi-hop 29.2/28.7, temporal 58.4/59.6,
  single-hop 55.8/54.5, open-domain 48.5/49.6。
- `compare.json` / verdict: flips A→B=219, B→A=202, McNemar p=0.436, CI overlap=true
  → **within-noise**（两抽取模型无统计差异）。
- Calibration baseline (mean +/- 95% CI): hybrid+luna 抽取 = **51.0% ± 10.3pp**。
  ⚠️ 该 CI 不可用作后续判定门（见下方方差诊断）。
- Model contribution delta: 与旧基线（74.7@luna, 带 IDK 重试, 单跑）差 -23.7pp，
  为模型 regime + single-pass 口径 + 后端漂移三者混合，如实记录不拆分。
- Decision: **冻结抽取模型 = gpt-5.6-luna**。质量 within-noise (p=0.436) 且成本占优
  （抽取 ¥0.39 vs ¥3.24，8×；紧凑上下文使答题成本再省一半），SC-007 token 预算
  也更有利。全部后续枪一律 EXTRACT_MODEL=gpt-5.6-luna。

### Strike 0 方差诊断（测量协议修正的依据）

逐 run overall（1540 题，抽样 SE 理论值仅 ±1.3pp）：

| run | A 臂 | A IDK 率 | B 臂 | B IDK 率 |
|-----|------|---------|------|---------|
| 1 | 52.5% | 15.4% | 59.7% | 9.7% |
| 2 | 44.7% | 24.1% | 40.8% | 27.9% |
| 3 | 43.0% | 25.9% | 40.9% | 27.3% |
| 4 | 50.9% | 16.4% | 63.7% | 4.9% |
| 5 | 64.1% | 5.5% | 47.6% | 19.6% |

- run 间摆幅 ~21pp，为抽样噪声的 ~8×；四类别同涨同跌 → run 级全局潜因，
  定位为**中转站对 gpt-5.6-sol 的后端服务质量随时间窗漂移**（库跨 repeat 复用、
  检索确定，唯一随机源是答题+判分调用）。
- 判分噪声可忽略：7154 对「预测文本逐字相同」的跨 run 比较仅 1.2% 判分翻转。
- 主杠杆是**拒答通道**：IDK 率 4.9%~27.9%（差 5.7×），与 overall 几乎完美负相关；
  single-pass 口径下可判分四类全部可答，每个 IDK 必判错。
- 剔除 IDK 后子集 acc 仍有 ~±5pp 残余漂移（弱 run 拒答多且答错多，非选择效应），
  即后端漂移同时压低回答质量，堵拒答通道只能消掉最大头。
- 答题调用未显式设 temperature（wire 层 omitempty → 服务端默认采样），但 run 内
  1540 题独立采样无法解释 run 级同向漂移，temperature 非主因。

### 测量协议修正（自 Strike 1 起生效）

1. **同进程配对双臂**：bench 已内建多臂机制（`--retrieval both` = fts+hybrid 共库、
   同时间窗答题判分）。各枪判定改为在**同一次调用内并跑 baseline 臂与 treatment 臂**
   （例：hybrid vs hybrid+assoc），run 级后端漂移在 per-question 配对中作为共同因子
   被 McNemar 抵消。跨目录 `--compare`（不同时间窗）仅作参考，不作判定门。
2. **反拒答口径（answerable 四类）**：answer prompt 移除 "I don't know" 出口、
   要求必给最佳猜测（single-pass 不变，符合 Mem0 等对照 regime；对抗题/拒答校准
   由 Strike 3 独立口径处理）。此为口径改动，独立 commit（宪法 IV）。
3. 校准基线数字本身不再作为跨时间比较锚点；每枪比较自带同窗 baseline 臂。

对账：预估模型的答题输出假设从 300 tok/题 修正为 **~50 tok/题**（sol 短答几乎
不动用 reasoning）；抽取(luna) 实测 ¥0.39/全量建库一次。

## Strike 1: Associative Retrieval

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- `compare.json` / verdict:
- Token budget ratio:
- Decision (keep / revert):
- Notes:

## Strike 2: Temporal Retrieval

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- `compare.json` / verdict:
- Token budget ratio:
- Decision (keep / revert):
- Notes:

## Strike 3: Abstention and Conflict Resolution

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- `compare.json` / verdict:
- Token budget ratio:
- Decision (keep / revert):
- Notes:

## LongMemEval_S Final Validation

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- Decision:
- Notes:
