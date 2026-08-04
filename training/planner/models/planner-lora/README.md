---
base_model: Qwen/Qwen2.5-7B-Instruct
library_name: peft
pipeline_tag: text-generation
tags:
- lora
- engram
- evidence-planner
- qwen2.5
---

# engram Evidence Planner — QLoRA adapter (023 r5)

本适配器是 engram 的本地 **Evidence Planner**：给定问题 + 候选证据列表，输出 022
Evidence Compiler 合同的 proposal JSON（need + KEEP/gap actions）。训练数据 = 023 spec
r5 冻结版本。spec：`specs/023-local-trained-evidence-compiler/`。

## 模型信息

- **底模**: `Qwen/Qwen2.5-7B-Instruct`（Apache-2.0）
- **训练方法**: QLoRA 4-bit（NF4 + double quant），单卡 24 GiB
- **LoRA**: `r=16`, `lora_alpha=32`, `lora_dropout=0.05`, 目标模块 `q/k/v/o_proj`
- **训练配置**: 1 epoch, lr 2e-4, batch 2 × grad_accum 8 = 16, seq ≤2048
- **训练数据**: 510 训练样本（607 全部 → train 510 / validation 97）
- **训练时长**: 360.7s（32 steps, RTX 4090）—— 远低于 FR-034 上限 24 GPU-hours
- **最终 loss**: 1.741

## 数据版本（FR-032）

- build-version: `023-b20260803-r5`，T011 门 PASS（199/200 = 99.5%，Wilson [97.2%, 99.9%]）
- 数据资产（candidates / labels / review / train jsonl + README data card）:
  https://huggingface.co/datasets/wallfacers/engram-023-planner/tree/main/data/train-r5
- 合同: 022 Evidence Compiler（proposal schema + gap source_need 审计字段）

## 推理方式

```bash
# vLLM LoRA 模式（OpenAI-compatible sidecar）
serve.sh lora --model Qwen2.5-7B-Instruct --adapter models/planner-lora
# 或合并底模（post-merge snapshot）
```

推理 prompt 模板必须与训练一致（`cmd/locomo-bench/local_planner.go` 的
plannerSystemPrompt + renderPlannerPrompt；train_lora.py 的 SYSTEM_PROMPT 镜像它）。

## 诚实边界

- 本 adapter 是**研究产物**，尚未通过三臂配对评测（T018-T020）与产品推荐门（T021）。
- 候选 >2048 token 会被截断；长对话超出单臂容量时可能降级。
- 许可：adapter Apache-2.0；训练数据 cc-by-4.0-synthetic。
