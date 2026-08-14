# LME unified×k150 3-rep 补跑记录（2026-08-15）

> 场景：042 只做了 LoCoMo×k150 配对；LME 只有 unified×k30（038 坐实 90.2%）。
> 本次把 unified 契约的 LME 高配（top-k 150）补成 3-rep，回答「unified 在 LME 高配下
> 是否仍 above-noise 正分」——结论：是，clean 3-rep majority 92.0%，增益模式与 k30 一致。

## 1. 配置（与 038/042 同源）

- 数据集：LongMemEval-S cleaned 500 题（`longmemeval_s_cleaned.json`）
- store：`lme-s500-store`（038 修复后重建，复用）
- answer：Qwen3.6-35B-A3B-FP8（vllm :8000，max-model-len 32768）
- embed：bge-large（vllm :8010，**确定性 `--max-num-seqs 1`**）
- judge：deepseek-v4-flash（`032-run.env`，key 验证有效）
- 二进制：`locomo-bench-150`（042 同款，含修订后契约）
- 契约：treatment digest `sha256:1d8a8d0f…`（Request classification 修订后 unified，
  与 038 坐实版一致）；`EMBED_TRUNCATE_PROMPT_TOKENS=-1`
- recipe：`--chunks --retrieval hybrid,hybrid+unified / hybrid+unified --top-k 150
  --chunk-quota 12 --judge-mem0-aligned --no-idk-retry --concurrency 32 --repeats 1/2
  --trace-mediation=false --max-tokens 16000`

## 2. 执行

- rep-1：`lme-paired-150-1rep/`（双臂配对，repeats=1）
  - 启动 00:24:07 → 完成 01:16（**~52 min**：ingest+backfill ~17 min + answer 双臂 ~35 min）
  - validation receipt valid=True（500 题，context parity 全过）
  - **paired verdict = above-noise**（flips 33/11，McNemar p=0.00126）
- rep-2/3：`lme-150-unified-global/`（unified-only，repeats=2）
  - 启动 01:26 → 完成 02:13（~47 min）
  - harness 的 unified 配对协议要求双臂 + 奇数 repeats，故 unified-only 走
    **全局 `--unified-answer-contract` + 单臂 `--retrieval hybrid`**（standalone，非配对）
  - regime 确认 answer_prompt_digest = `sha256:ff400d0e…`，与 rep-1 treatment 一致（契约无漂移）
  - 三个 rep 的 question 集合完全一致（500/500）

## 3. 结果（离线 clean 重判，deepseek-v4-flash temp=0 no-thinking，¥0.05 级）

| rep | clean | online（mem0 自带） |
|---|---:|---:|
| rep-1（配对 run） | 91.00% (455) | 91.60% (458) |
| rep-2 | 92.00% (460) | — |
| rep-3 | 91.80% (459) | — |
| **3-rep clean majority** | **92.00% (460/500)** | 92.40% (462) |

**类别分解（clean 3-rep majority）**：

| question_type | n | control(1-rep) | unified(3-rep) | Δ |
|---|---:|---:|---:|---:|
| single-session-preference | 30 | 63.3% | 96.7% | +33.4 |
| knowledge-update | 78 | 85.9% | 92.3% | +6.4 |
| multi-session | 133 | 82.7% | 88.7% | +6.0 |
| temporal-reasoning | 133 | 86.5% | 88.0% | +1.5 |
| single-session-assistant | 56 | 96.4% | 96.4% | 0.0 |
| single-session-user | 70 | 100.0% | 100.0% | 0.0 |
| TOTAL | 500 | 87.0% (435) | 92.0% (460) | +5.0 |

## 4. 解读与纪律边界

- **unified 在 LME@k150 下同样正分**，3-rep 高度一致（91.0/92.0/91.8），增益集中在
  preference（+33.4）/ knowledge-update（+6.4）/ multi-session（+6.0）——与 k30 pattern 完全一致。
- **对照 control 为 1-rep（87.0%）非严格配对**：用户决策后续主推 unified，control 不补 3-rep；
  因此该行在 result-matrix 标「3-rep majority 坐实 + control 1-rep 参考」，非配对 Δ。
- clean 与 raw 差异极小（unified clean 92.0 vs online 92.4；LME thinking 短，judge 作弊量 ±0.6pp），
  无需像 LoCoMo 那样大幅修正。

## 5. 收尾

- 备份：`/root/autodl-tmp/eval-backup-20260815-021935/`（两个 run 目录 + 脚本）
- box 已关机（shutdown now，SSH banner 不可达）
- 本地重判产物：`cost-lme150-rejudge.json`、`clean-lme150.json`、
  `cost-unified3-rejudge.json`、`clean-unified3.json`
