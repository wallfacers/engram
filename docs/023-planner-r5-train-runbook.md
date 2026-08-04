# 023 Planner r5 训练 runbook

执行记录：2026-08-04。在冻结的 022 Evidence Compiler contract 上训练本地
Evidence Planner（Qwen2.5-7B-Instruct + QLoRA），数据 = r5 标注层（外部 AI 全量标注
+ gap 复审）。r2 runbook（`023-planner-r2-train-runbook.md`）记录了 r2 规则 oracle 层的
历史，r5 是其继任者。

## 数据（冻结产物）

| 层 | 版本 | 路径 | 摘要 |
|---|---|---|---|
| candidates | 023-b20260803-r1 | `training/planner/data/processed/candidates.jsonl` | 799 行 → 空候选剔除 607 |
| 标注 | r5 | `specs/.../audit/labeling-work-r3/all-607-labeled.jsonl` | 外部 AI 全量语义标注 |
| 复审 | r5 | `specs/.../audit/labeling-work-r3/gap-recheck[-labeled].jsonl` | 48 可疑 gap → 修正 3 |
| 合并标注 | r5 | `specs/.../audit/labeling-work-r3/all-607-labeled-r5.jsonl` | 607 行 |
| train | 023-b20260803-r5 | `training/planner/data/processed/train-r5.jsonl` | 607（train 510 / val 97）；gap 317 / keep 290 |
| 审查表 | r5 | `specs/.../audit/review-r5/review-r5-023-b20260803-r5-reviewed.csv` | 200 行 |

HF 数据资产：`wallfacers/engram-023-planner` 数据集 repo 的 `data/train-r5/` 子目录
（含 candidates/labels/review/train jsonl + data card README）。

## 门（T011 PASS）

- T011：199/200 pass（99.5%），Wilson 95% CI `[97.22%, 99.91%]` — 门 PASS
  （receipt：`specs/.../audit/t011-review-r5-023-b20260803-r5.md`）
- T012 audit：全干净（contamination 0 / blocking=false）
- T013 FR-010：确定性重建 100%

## 训练环境（48G 单卡，AutoDL 数据盘）

- venv：`/root/autodl-tmp/023-venv`（torch 2.11.0+cu130 / transformers 5.14.1 /
  peft 0.20.0 / datasets 5.0.1 / bitsandbytes 0.50.0 / vllm 0.26.0）
- 模型：`/root/autodl-tmp/models/models/Qwen--Qwen2.5-7B-Instruct/snapshots/master`
- 注意：transformers 5.x `_validate_args` 会对 `max_steps=None` 抛 TypeError——
  `train_lora.py` 已修：0 (unset) → `-1`（用 epochs）。

## 训练命令

```bash
cd /root/autodl-tmp/023-runs/train-r5
/root/autodl-tmp/023-venv/bin/python train_lora.py \
    --data train-r5.jsonl \
    --base-model /root/autodl-tmp/models/models/Qwen--Qwen2.5-7B-Instruct/snapshots/master \
    --out /root/autodl-tmp/023-runs/models/planner-lora \
    --config configs/train.yaml \
    --seed 0
```

configs/train.yaml 冻结：LoRA r=16 / alpha=32 / dropout=0.05 / q,k,v,o_proj；
batch 2 × grad_accum 8 = 16；lr 2e-4；epochs 1；max_seq 2048。

## 训练结果（2026-08-04）

- steps：32/32（510 样本 / 有效 batch 16）
- train_loss：1.741（final）
- 时长：360.7s（~6 分钟，RTX 4090）— 远低于 FR-034 的 24 GPU-hours
- adapter：`/root/autodl-tmp/023-runs/models/planner-lora/`（40MB safetensors，
  r16/alpha32/dropout0.05/qkv_o_proj）
- 冻结摘要：`train_summary.json`（base/seed/config/train_examples/lora_r）

## 训练后

1. serve：`serve.sh lora --adapter .../planner-lora`（vLLM --enable-lora）
2. 配对评测（T018-T020）：deterministic / prompt-only / supervised 三臂同 store，
   candidates byte-identical，只差训练状态（FR-023/024/025）
3. 结果门槛 FR-028/029：Primary Cohort Δ≥+2.0pp、exact McNemar p<0.05
   （Holm-corrected）、Guard ≥-0.5pp、category non-regression

## HF 资产

- 模型 repo（private）：`wallfacers/engram-planner-lora`（adapter + tokenizer +
  model card + train_summary）
- 数据集 repo：`wallfacers/engram-023-planner` → `data/train-r5/`
- token 走 env（HF_TOKEN）；本机本地备份 `training/planner/models/planner-lora/`
