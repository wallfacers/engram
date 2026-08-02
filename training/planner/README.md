# 023 Planner 训练 pipeline

**离线、自举、零污染**。生成虚构记忆对话 → 灌入 engram（Go）→ 检索冻结候选 →
oracle/规则标签 → TRL SFT + LoRA → 本地 sidecar 服务 → 三臂配对。

详见 [specs/023/data-model.md](../../specs/023-local-trained-evidence-compiler/data-model.md)
与 [specs/023/plan.md](../../specs/023-local-trained-evidence-compiler/plan.md)。

## 目录

```text
training/planner/
├── data_build.py    # [Python] 本地 Qwen 生成虚构多会话记忆对话 → data/raw/convos.jsonl
├── label.py         # [Python] 目标标签：Need 解析 + Actions 规则/oracle → data/processed/*.jsonl（待实现/接 Go 产物）
├── train_lora.py    # [Python] TRL SFT + LoRA（supervised 臂）
├── serve.sh         # vLLM 起 OpenAI 兼容 sidecar（lora 模式）
├── configs/train.yaml
└── .gitignore       # 权重/数据/产物不跟踪
```

## Pipeline

```text
data_build.py ──合成对话──▶ convos.jsonl
        │
        ▼   [Go] planner-build 工具（待实现，engine 零改动）：
        │        灌 engram（提取+索引）→ 生成 query → 检索冻结候选
        │        产出 data/processed/candidates.jsonl（含 fixed-gold oracle 覆盖）
        ▼
label.py ──Need 解析 + Actions 规则/oracle + 双标签裁决 + 人审──▶ data/processed/train.jsonl
        │
        ▼
train_lora.py ──▶ models/planner-lora/（adapter + train_summary.json）
        │
        ▼
serve.sh lora ──▶ local_planner.go（--compiler-arm planner --planner-base-url/--planner-model）
        │
        ▼
三臂配对评测（T018–T020，deterministic / prompt-only / supervised）
```

## 关键一致性（不能漂移）

1. **prompt 模板**：`train_lora.py` 的 `SYSTEM_PROMPT` + `render_user()` 必须与
   `cmd/locomo-bench/local_planner.go` 的 `plannerSystemPrompt` + `renderPlannerPrompt`
   **逐字一致**。改任何一处，两处一起改，并重跑配对（否则训练分布与推理分布错位）。
2. **wire format**：训练输出的 target JSON 用 snake_case
   （`need/actions/entities/time_constraints/operands/list_cardinality/update_state/gap`；
   `kind/candidate_id/source_id/span/sentences/reason_code`），与 adapter 解析器一致
   （data-model.md §5）。改 schema 必须同步改 Go `parsePlannerProposal`。
3. **候选冻结**：训练样本的 `candidates` 必须是 engram 实际检索输出，推理时同样输入
   （FR-017）；任何臂不得改变候选池。

## 待实现（阻塞于租机/引擎集成）

- **Go `cmd/planner-build` 工具**：灌对话 → store → 检索 → 冻结候选 + oracle 覆盖。
  复用 `cmd/locomo-bench` 的 store 构建与检索逻辑；engine 零改动。
- **`label.py`**：接 planner-build 产物，实现 data-model.md §2 的标签规则 + 双标签 +
  人审导出。

## 运行（租机后，数据盘）

```bash
# 1. 合成对话（本地 Qwen sidecar 已起）
python3 data_build.py --base-url http://localhost:8000/v1 \
    --convos 200 --sessions 4 --out data/raw/convos.jsonl

# 2. (planner-build) → candidates.jsonl + oracle 覆盖

# 3. label → train.jsonl

# 4. 训练（≤24 GPU-hours）
python3 train_lora.py --data data/processed/train.jsonl \
    --base-model Qwen2.5-7B-Instruct --out models/planner-lora

# 5. serve
ADAPTER=models/planner-lora ./serve.sh lora
# 6. 配对评测
go run ./cmd/locomo-bench --compiler-arm planner \
    --planner-base-url http://localhost:8000/v1 --planner-model Qwen2.5-7B-Instruct ...
```
