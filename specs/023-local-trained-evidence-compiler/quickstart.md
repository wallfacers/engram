# 023 Quickstart — 数据构建 + 训练 + 配对评测 runbook

**硬件决策（2026-08-03）**：48G 单卡一卡通吃（数据构建 / QLoRA 训练 / 配对评测）。
避免 24G/48G 来回切换。AutoDL 按分钟计费——**空闲必停**（省钱铁律）。

> FR-034 兼容：推荐 recipe 仍按 QLoRA 冻结（低显存），保证 24G 卡可重建；
> validation 阶段用显存 cap 验证 24G 等价，不实际切卡。

## 0. 环境与数据盘（AutoDL 纪律）

- 系统盘只放代码（<30G）；所有 run 产物在数据盘 `/root/autodl-tmp/023-runs/`。
- 拉代码：`git clone https://github.com/wallfacers/engram`（或 scp 工作副本）。
- Python venv 放数据盘：`python3 -m venv /root/autodl-tmp/023-venv`；装
  `transformers peft trl accelerate bitsandbytes datasets`。
- `df -h /` 检查系统盘；`>80%` 先清理。

## 1. 数据构建（T008-T013 正式执行，7B 提取）

vllm 起 **Qwen2.5-7B-Instruct**（OpenAI 兼容 :8000）——提取 + query 生成。

```bash
# 合成对话 + query/gold 标注
cd engram/training/planner
python3 data_build.py --base-url http://localhost:8000/v1 \
    --convos 200 --sessions 4 --out data/raw/convos.jsonl

# planner-build → 冻结候选 + gold coverage（build_version 冻结一次）
cd engram
go run ./cmd/planner-build -convos training/planner/data/raw/convos.jsonl \
    -out training/planner/data/processed/candidates.jsonl \
    -build-version 023-b20260803-r1 \
    -extract-base-url http://localhost:8000/v1 -extract-model Qwen2.5-7B-Instruct

# label → train.jsonl（双标签 + 裁决 + per-conv split）
cd training/planner
python3 label.py --candidates data/processed/candidates.jsonl \
    --out data/processed/train.jsonl --build-version 023-b20260803-r1 --seed 0

# T012 审计（污染扫描用 LoCoMo 参考；测试集内容零进入）
python3 audit.py --train data/processed/train.jsonl \
    --benchmark /root/autodl-tmp/023-runs/locomo.json --build-version 023-b20260803-r1

# T013 重建验证：同输入第二次构建 → 对比（100%）
#   （第二次构建用独立 run-dir / 不同 store-dir）

# T011 人审抽样（≥200，maintainer 填 semantic_sufficiency）
python3 review.py --train data/processed/train.jsonl --out data/processed/review.csv --n 200 --seed 0
python3 review.py --results data/processed/review.csv   # 充分率 ≥95% 且 CI 下界 ≥90%
```

## 2. 训练（T015，QLoRA 7B，≤24 GPU-hours）

```bash
python3 train_lora.py --data training/planner/data/processed/train.jsonl \
    --base-model Qwen2.5-7B-Instruct --out /root/autodl-tmp/023-runs/models/planner-lora
# 冻结：train_summary.json（数据摘要/底模摘要/config/随机性/输出摘要/完成状态）
```

## 3. 配对评测（T018-T020，answerer 35B）

**同卡切换**：停 7B vllm，起 **Qwen3.6-35B-A3B-FP8** :8000（answerer，与 T003 同款）；
Planner sidecar 用合并 LoRA 的 7B。

```bash
ADAPTER=/root/autodl-tmp/023-runs/models/planner-lora ./serve.sh lora   # planner :8001（7B）
# answerer 35B :8000（同卡，Planner/answerer 不同端口 = 同卡双实例超显存 → 分批跑）
go run ./cmd/locomo-bench --compiler-arm planner \
    --planner-base-url http://localhost:8001/v1 --planner-model Qwen2.5-7B-Instruct \
    --data locomo.json --run-dir /root/autodl-tmp/023-runs/paired \
    --retrieval hybrid ...（B1 正式协议，三臂同 store）
```

> 48G 不能同时载 35B + 7B（~50G）。分批：先 35B answerer 批跑，再 7B planner sidecar，
> 或 A10G 48G 双端口验证实测容量后定。

## 4. 省钱铁律

- 空闲必停：`shutdown -h now`（保留数据盘）；产物先拉回本地再停。
- vllm 起停跟随任务，不常驻。
- 所有凭据只走 env/隧道，绝不落盘。
