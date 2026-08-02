# Research & Dependency Receipt: 本地训练式 Evidence Planner（023）

**Status**: active ｜ **Date**: 2026-08-02 ｜ **Spec**: [spec.md](spec.md) ｜ **Plan**: [plan.md](plan.md)

## T001 — Dependency Receipt（minimal sufficient set，Amendment 1）

| 依赖项 | expected | observed | validity | 来源 |
|---|---|---|---|---|
| Compiler/Planner 合同冻结（022.v1） | contract 冻结 + 引擎测试全绿 | ✅ `memory/evidencecompiler` 根包 re-export 全部公开类型；`internal/{contracts,validate,need,extract,render,resolve}` 测试全绿（`go test ./memory/evidencecompiler/...` ok） | **valid** | [compiler-contract.md](../../specs/022-benchmark-parity-memory-architecture/contracts/compiler-contract.md) |
| LoCoMo B1 有效 | ≥85% 级、validity 全绿 | ✅ **85.19%** majority（1,312/1,540），OVERALL 84.7%，协议 `sha256:263b52b6…`，candidate/source/span/citation/within-cap rate 全 1 | **valid** | [results.md](../../docs/evaluation/results.md) · [b1-control-packing-027](../../docs/evaluation/reports/b1-control-packing-027.md) |
| fixed-gold oracle 可用 | diagnostic-only oracle 工具已实现 | ✅ `cmd/locomo-bench/eval_fixed_gold_oracle.go` + test；`--fixed-gold-oracle --eval-protocol` 独立模式，无检索 | **valid** | 代码 |
| `local_planner.go` 接入点 | 可替换本地 Planner adapter | ✅ **T002 已补全**（commit `2ed473a`）：`evidencecompiler.Planner` 实现、sidecar 接入、fail-closed、9 测试绿、engine 零改动；`--compiler-arm planner` + `--planner-base-url/--planner-model` 已接线 | **valid** | `cmd/locomo-bench/local_planner.go` |
| **Primary Cohort residual 量化** | 冻结 compiler-eligible cohort（evidence 足够 + oracle 可答 + deterministic 未答对） | ⏳ **待 T003**：需要完整 LoCoMo（1,540 cat1-4）+ answerer/judge/token-counter 端点；本机仅 `locomo10.json`（10 题样本），eval env 全部 unset | **pending** | T003 本节 |

### Verdict（当前）

- 最小充分集 **5 项中 4 项 valid，1 项 pending**（residual 量化）。
- 当前状态：**`PENDING`（待 residual 量化）**。在 T003 完成前**不启动**正式数据构建/训练。
- 分流：T003 完成后 residual 为空 → **`NOT_NEEDED`**（不训练）；非空 → **`READY`**（可启动数据构建/训练，仍受 Guard 后补约束：双基准收口前不得跨基准外推，FR-003）。

## T003 — Primary Cohort residual 量化

### 方法与判定

```
B1 正式协议（deterministic，85.19%） → 逐题答对集合 D
fixed-gold oracle（--fixed-gold-oracle）→ 逐题答对集合 G（gold evidence 全给）
compiler-eligible residual = { 题 : 题 ∈ G 且 题 ∉ D }
```

- `residual == ∅` → deterministic Compiler 已在 evidence 充分时答对所有 oracle 可答题 → 无 compiler-side 可救空间 → **NOT_NEEDED**。
- `residual 非空` → 冻结 cohort（逐题清单 + 类别分布 + 相对 85.19% 短差）→ 023 训练目标；cohort 越小，理论增益越有限（Plan §Summary）。

### 前置条件（租机后第一步）

1. **完整 LoCoMo 数据**：`testdata/locomo/locomo.json`（1,540 answerable cat1-4）。本机仅有 `locomo10.json`（10 题小样本，仅可 smoke）。
2. **eval 端点**（env，不落盘）：
   - answerer + token counter：租机 vllm（Qwen3.6-35B-A3B-FP8，`LOCOMO_API_KEY/BASE_URL/MODEL`，counter 用 `--token-counter-base-url`）
   - judge：`JUDGE_PROVIDER/BASE_URL/MODEL/API_KEY`（deepseek-v4-flash，独立端点，fallback 到 LOCOMO_*）
3. **跑法**（对照 027 runbook，run-dir 必须在数据盘）：
   ```bash
   go run ./cmd/locomo-bench \
     --data <locomo.json> --run-dir /root/autodl-tmp/023-runs/t003 \
     --eval-protocol <022.v1 B1 protocol> \
     --compiler-arm extractive --no-idk-retry --repeats 3 \
     --fixed-gold-oracle
   ```
   产出 `fixed_gold_oracle_summary.json` + B1 `results-*.jsonl`。
4. **比对**：用逐题 answer 对拍 G 与 D，输出 residual 清单（脚本进 `training/planner/`）。

## P0.4 — 底模选型记录

- **默认：Qwen2.5-7B-Instruct（Apache-2.0）** —— 许可可分发/可推荐（Llama-3.1/Gemma-2 均 gated + 商用条款受限）；7.6B BF16 ~15 GiB，单卡 24 GiB LoRA 训练 + p95≤2s 推理可行；同族 3B/1.5B 可降级（tokenizer 一致）。
- 冻结点：T004（validation 前正式冻结，tokenizer/chat-template 摘要记录）。
- 来源：[plan.md Key Decision 1](plan.md) · [spec.md Amendment 2](spec.md)。

## 待办挂钩

- T003 前置条件满足 → 跑 residual → 回填本节 Verdict。
- 数据构建（T008–T013）与训练（T015）仅在 Verdict 为 `READY` 后启动（spec FR-003）。
