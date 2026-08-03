# 023 Planner 训练 pipeline

**离线、自举、零污染**。生成虚构记忆对话 → 灌入 engram（Go）→ 检索冻结候选 →
oracle/规则标签 → TRL SFT + LoRA → 本地 sidecar 服务 → 三臂配对。

详见 [specs/023/data-model.md](../../specs/023-local-trained-evidence-compiler/data-model.md)
与 [specs/023/plan.md](../../specs/023-local-trained-evidence-compiler/plan.md)。

## 目录

```text
training/planner/
├── data_build.py    # [Python] 本地 Qwen 生成虚构多会话记忆对话 + query/gold 标注 → data/raw/convos.jsonl
├── label.py         # [Python] 目标标签：Need 解析 + Actions 规则/oracle + 双标签裁决 → data/processed/train.jsonl
├── train_lora.py    # [Python] TRL SFT + LoRA（supervised 臂）
├── serve.sh         # vLLM 起 OpenAI 兼容 sidecar（lora 模式）
├── configs/train.yaml
└── .gitignore       # 权重/数据/产物不跟踪
```

## Pipeline

```text
data_build.py ──合成对话──▶ convos.jsonl
        │
        ▼   [Go] cmd/planner-build（engine 零改动）：
        │        灌 engram（提取+索引）→ 检索冻结候选 → gold coverage
        │        产出 data/processed/candidates.jsonl（含 source lineage）
        ▼
label.py ──Need 解析 + Actions 规则/oracle + 双标签裁决──▶ data/processed/train.jsonl
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

## 状态（T008 已落地）

- **`cmd/planner-build`** ✅ 灌对话 → store（离线提取+索引）→ 每 query 检索冻结候选 →
  gold turn→Evidence 覆盖 + source lineage → `candidates.jsonl`。`-embed-base-url` 空时
  keyword-only（graceful degradation）。单测 + 集成测试 + CLI smoke 全绿。
- **`label.py`** ✅ 确定性 Need 解析（022 need-builder 语义）+ oracle 覆盖 Actions
  （KEEP/EXTRACT）+ 双独立标签（A/B）+ 独立裁决 + per-conversation split（FR-012）+
  content_digest（FR-010）。候选未覆盖 → `gap` 负样本（FR-016 fail-closed）。

## 工具状态（T009–T013 已落地，正式执行待数据/人工）

- **T009** `corpus_adapter.py`：ultrachat-jsonl / oasst-jsonl → convos.jsonl + 许可清单；
  `data_build.py --gen-queries-only` 复用 query 生成。
- **T010** label.py 双标签（labeler_a/b）+ 独立裁决（adjudicate）机制已就位。
- **T011** `review.py`：分层随机抽样人审表 + 语义充分率 Wilson 95% CI 门（≥95% 且下界 ≥90%）。
- **T012** `audit.py`：provenance/许可/schema/split/近重复/污染（8-gram benchmark 扫描）/privacy。
- **T013** `rebuild_check.py`：两次独立构建样本集/split/content-digest/全局摘要一致率 100%。
- 全部自测固化于 `test_tools.py`（14 测试）；正式数据构建（合成生成/语料下载）、人审、
  两次构建验证需租机端点 + maintainer 参与。

运行全部工具自测：

```bash
cd training/planner && python3 -m unittest test_tools -v
```

## 运行（租机后，数据盘）

```bash
# 1. 合成对话（本地 Qwen sidecar 已起）
python3 data_build.py --base-url http://localhost:8000/v1 \
    --convos 200 --sessions 4 --out data/raw/convos.jsonl

# 2. planner-build → candidates.jsonl + gold coverage
go run ./cmd/planner-build -convos data/raw/convos.jsonl \
    -out data/processed/candidates.jsonl -build-version 023-b20260803-r1 \
    -extract-base-url http://localhost:8000/v1 -extract-model Qwen2.5-7B-Instruct

# 3. label → train.jsonl
python3 label.py --candidates data/processed/candidates.jsonl \
    --out data/processed/train.jsonl --build-version 023-b20260803-r1 --seed 0

# 4. 训练（≤24 GPU-hours）
python3 train_lora.py --data data/processed/train.jsonl \
    --base-model Qwen2.5-7B-Instruct --out models/planner-lora

# 5. serve
ADAPTER=models/planner-lora ./serve.sh lora
# 6. 配对评测
go run ./cmd/locomo-bench --compiler-arm planner \
    --planner-base-url http://localhost:8000/v1 --planner-model Qwen2.5-7B-Instruct ...
```
