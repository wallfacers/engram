# 023 Planner r2 训练 runbook

执行记录：2026-08-04。目标：在冻结的 022 Evidence Compiler contract 上训练本地
Evidence Planner（Qwen2.5-7B-Instruct + QLoRA），数据 = r2 标注层。

## 数据（冻结产物）

| 层 | 版本 | 路径（AutoDL 数据盘） | 摘要 |
|---|---|---|---|
| candidates | 023-b20260803-r1 | `/root/autodl-tmp/023-runs/data/processed/candidates.jsonl` | 799 行；r1 冻结，未重跑 |
| train | 023-b20260803-r2 | `/root/autodl-tmp/023-runs/data/processed/train-r2.jsonl` | 537 样本（train 450 / val 87）；gap 190 / KEEP 347 |
| 审查表 | r2 | `specs/023-local-trained-evidence-compiler/audit/review-r2-023-b20260803-r2.csv` | 200 行；gold_answer 200/200 + lineage |
| rebuild | r2 | `/root/autodl-tmp/023-runs/data/processed/rebuild-r2/train.jsonl` | 与 train-r2 100% 一致 |

r2 标注层相对 r1：false-gap guard 把 54 个漏标样本纠正为 KEEP（gap 244→190）。
r1 审计收据见 `specs/023-local-trained-evidence-compiler/audit/t011-fails-023-b20260803-r1.md`
（r1 INVALID：审查表缺 gold_answer，门 93.5% 未过）。

## 训练环境（48G 单卡）

- conda：`/root/miniconda3`（base）
- 依赖：torch 2.8.0+cu128 / transformers 4.57.6 / trl 0.29.1 / peft 0.20.0 /
  accelerate 1.14.0 / datasets 5.0.1 / bitsandbytes 0.50.0
  - 注意：transformers 必须 <5、trl 必须 <1（train_lora.py 用 trl 0.x SFTTrainer
    API `dataset_text_field`/`max_seq_length`）
- 模型：`/root/autodl-tmp/models/models/Qwen--Qwen2.5-7B-Instruct/snapshots/master`（modelscope 下载）
- 验证（2026-08-04）：7B QLoRA 4bit 加载成功，GPU 7.8 GB / 48 GB，trainable 10.1M 参数

## 训练命令

```bash
cd /root/engram/training/planner
/root/miniconda3/bin/python3 train_lora.py \
    --data /root/autodl-tmp/023-runs/data/processed/train-r2.jsonl \
    --base-model /root/autodl-tmp/models/models/Qwen--Qwen2.5-7B-Instruct/snapshots/master \
    --out /root/autodl-tmp/023-runs/models/planner-lora \
    --config configs/train.yaml \
    --seed 0
```

configs/train.yaml 冻结：LoRA r=16 / alpha=32 / dropout=0.05 / q,k,v,o_proj；
batch 2 × grad_accum 8 = 16；lr 2e-4；epochs 1；max_seq 2048。FR-034：≤24 GPU-hr。

## 前置门

**T011 独立人审通过后启动**（充分率 ≥95% 且 Wilson 95% CI 下界 ≥90%）。
人审表 = review-r2 csv；判定方式见训练流水线对话中的「T011 r2 独立人审提示词」。

## 训练后

1. serve：`serve.sh lora --adapter .../planner-lora`（vLLM --enable-lora）
2. 配对评测（T018-T020）：deterministic / prompt-only / supervised 三臂同 store，
   candidates byte-identical，只差训练状态（FR-023/024/025）
3. 结果门槛 FR-028/029：Primary Cohort Δ≥+2.0pp、exact McNemar p<0.05
   （Holm-corrected）、Guard ≥-0.5pp、category non-regression

## HF 上传（收尾 task）

数据产物（convos/candidates/train-r2/审查表）+ LoRA adapter + model card →
HF `wallfacers/*`，token 走 env。r1 审计收据作为基线一并归档。
