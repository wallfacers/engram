# 028 US2 Verdict：训练时间锚定抽取器（写侧 event 训练化）

**Status**: **NO-GO（未转化）** · **2026-08-06** · 84 题 × 3 reps 配对，同 store/answerer/judge，McNemar
**Feature**: [spec 028](../../../specs/028-write-side-event-training/spec.md) US2（训练抽取器 + 复测）
**前置**: [027 写侧 event verdict](./027-write-side-event-verdict.md)（7B 抽取丢时间锚定，−26.2pp）

## 结论一句话

**训练抽取器彻底解决了 027 的抽取瓶颈（时间锚定 5% → 96.9%），端到端差距从 −26.2pp 一路收窄到 −1.2pp，但未转化——008 铁律下 verdict = NO-GO。**

## 配对结果（84 题 × 3 reps majority，chunk vs trained-model event）

| 臂 | OVERALL | multi-hop | temporal |
|---|---|---|---|
| chunk（027 基线） | **50.0%** (42/84) | 52.0% (13/25) | 49.2% (29/59) |
| event（训练模型 Qwen3B-LoRA） | 48.8% (41/84) | 40.0% (10/25) | **52.5%** (31/59) |

**配对表（chunk vs event）**：C✓E✓=28 · C✓E✗=14 · C✗E✓=13 · C✗E✗=29
**Δ = +1.2pp（chunk 胜）· McNemar p = 1.0000**（不显著，方向相反）

分类别 McNemar：
- temporal: chunk✗/event✓=10, chunk✓/event✗=8 → **event +3.3pp**（52.5 vs 49.2），p=0.8145（不显著，方向正确）
- multi-hop: chunk✓/event✗=6, chunk✗/event✓=3 → **event −12.0pp**（40.0 vs 52.0），p=0.5078（不显著，方向倒退）

## 训练有效性（中间指标）

| 指标 | 7B 无训练（027） | 教师 DeepSeek-flash（028 US1） | 训练模型 Qwen3B-LoRA（028 US2） |
|---|---|---|---|
| 时间锚定（语义） | 5% | 86.4% | **96.9%**（T013，n=300 审计） |
| schema 合法率 | — | 100% | **100%** |
| 非法时间戳 | — | 0 | 0 |
| 端到端 event 臂 | 23.8% | 44.0% | **48.8%** |
| 对 chunk 差距 | −26.2pp | −6.0pp（p=0.44） | **−1.2pp（p=1.00）** |

训练配置：Qwen2.5-3B-Instruct，bf16 LoRA(r=16)，5313 条教师数据（含 session date），3 epochs，loss 1.32→0.44。全程本地栈，配对 $0 成本。

## 判定依据

- **008 铁律**：event ≥ chunk 才 GO。event 48.8% < chunk 50.0%，Δ=+1.2pp 方向反而 chunk 好 → **NO-GO**。
- **三臂差距收窄链**：−26.2pp(7B) → −6.0pp(teacher) → −1.2pp(trained)。写侧 event 表示的瓶颈确为**抽取能力**（027 结论验证），训练把抽取能力拉到 96.9% 锚定，但端到端仍不转化。
- **temporal 单类首次转正**（49.2→52.5，+3.3pp）但 p=0.81 噪声内；multi-hop 反而 −12pp（p=0.51）。类别方向不一致，无法构成部分转化主张。
- 训练数据全部来自教师（DeepSeek-flash）——**蒸馏上限**：模型不可能超过教师的事件语义质量。教师自身 −6.0pp 未转化，训练模型追平并微超教师（44→48.8）但仍在 chunk 之下。

## 生产影响

- event 投影 / `--representation event` **保持 default-off**（027 FR-010 已定，028 US3 不进入）。
- 训练抽取器（`specs/028/tools/train_sft.py` + `train.sh` + `export_deploy.sh`，含 transformers 5.x 兼容修复）作为**能力资产**记录，SaaS 线（训练化写侧结构）可复用。
- 写侧 event 表示端到端不涨点是**第三次验证**（027/028-US1/028-US2），时间域/结构域杠杆线正式收口。

## 复现资产

- 远端（已备份 `/root/autodl-tmp/eval-backup-028-us2/`，6.4M）：`pair-model/`（3 reps 原始结果）+ `full-model-project.json`（5882 events）+ `audit-model-project.json` + `train-config.json` + train/deploy logs
- 本地：`~/.claude/engram-028/pair-model/run-{1,2,3}.jsonl` + `~/.claude/engram-028/pair-teacher/run-{1,2,3}.jsonl`（teacher 对照）
- 训练数据：`specs/028/tools/train-028-v2.jsonl`（5313 条，含 session date，语义锚定 100%）
- 模型产物（远端，可重建）：`/root/autodl-tmp/028-runs/model-us2/`（lora + merged）
