# Research & Dependency Receipt: 本地训练式 Evidence Planner（023）

**Status**: active ｜ **Date**: 2026-08-02 ｜ **Spec**: [spec.md](spec.md) ｜ **Plan**: [plan.md](plan.md)

## T001 — Dependency Receipt（minimal sufficient set，Amendment 1）

| 依赖项 | expected | observed | validity | 来源 |
|---|---|---|---|---|
| Compiler/Planner 合同冻结（022.v1） | contract 冻结 + 引擎测试全绿 | ✅ `memory/evidencecompiler` 根包 re-export 全部公开类型；`internal/{contracts,validate,need,extract,render,resolve}` 测试全绿（`go test ./memory/evidencecompiler/...` ok） | **valid** | [compiler-contract.md](../../specs/022-benchmark-parity-memory-architecture/contracts/compiler-contract.md) |
| LoCoMo B1 有效 | ≥85% 级、validity 全绿 | ✅ **85.19%** majority（1,312/1,540），OVERALL 84.7%，协议 `sha256:263b52b6…`，candidate/source/span/citation/within-cap rate 全 1 | **valid** | [results.md](../../docs/evaluation/results.md) · [b1-control-packing-027](../../docs/evaluation/reports/b1-control-packing-027.md) |
| fixed-gold oracle 可用 | diagnostic-only oracle 工具已实现 | ✅ `cmd/locomo-bench/eval_fixed_gold_oracle.go` + test；`--fixed-gold-oracle --eval-protocol` 独立模式，无检索 | **valid** | 代码 |
| `local_planner.go` 接入点 | 可替换本地 Planner adapter | ✅ **T002 已补全**（commit `2ed473a`）：`evidencecompiler.Planner` 实现、sidecar 接入、fail-closed、9 测试绿、engine 零改动；`--compiler-arm planner` + `--planner-base-url/--planner-model` 已接线 | **valid** | `cmd/locomo-bench/local_planner.go` |
| **Primary Cohort residual 量化** | 冻结 compiler-eligible cohort（evidence 足够 + oracle 可答 + deterministic 未答对） | ✅ **149 题 residual**（共同分母 1532：G=1391/1532=90.8%，D=1306/1532=85.25%；类别 temporal 45 / single-hop 60 / multi-hop 27 / open-domain 17） | **valid** | [residual-cohort.json](residual-cohort.json) · T003 本节 |

### Verdict（当前）

- 最小充分集 **5 项全部 valid**（含 residual 量化，2026-08-03 完成）。
- 当前状态：**`READY`**（residual 非空：149 题）。可启动正式数据构建/训练。
- 约束：Guard 后补项（LongMemEval-S B1、judge audit、miss attribution）未完成前不得跨基准外推（FR-003）。

## T003 — Primary Cohort residual 量化

### 方法与判定

```
B1 正式协议（deterministic，85.19%） → 逐题答对集合 D
fixed-gold oracle（--fixed-gold-oracle）→ 逐题答对集合 G（gold evidence 全给）
compiler-eligible residual = { 题 : 题 ∈ G 且 题 ∉ D }
```

- `residual == ∅` → deterministic Compiler 已在 evidence 充分时答对所有 oracle 可答题 → 无 compiler-side 可救空间 → **NOT_NEEDED**。
- `residual 非空` → 冻结 cohort（逐题清单 + 类别分布 + 相对 85.19% 短差）→ 023 训练目标；cohort 越小，理论增益越有限（Plan §Summary）。

### 已执行（2026-08-03，AutoDL RTX 4090 48GB）

- **环境**：answerer = 本地 vllm Qwen3.6-35B-A3B-FP8（:8000，token counter 走 `/tokenize`）；judge = deepseek-v4-flash（`JUDGE_*` env）；store 复用 t003-store（10 conv）。协议 `t003-freeze.json`（B1 正式协议，force-answer、judge mem0-aligned、repeats=3、cap=5000）。
- **D 集合**：`t003-b1-v3/classification.jsonl`（1540 题全 valid，1312/1540 = **85.19%**，与 022 参考一致）。
- **G 集合**：`t003-oracle-v8/fixed_gold_oracle.jsonl`（**1532 题全 valid**，oracle 完整跑通）。
- **对拍**：`training/planner/residual_compare.py` → 见 [residual-cohort.json](residual-cohort.json)。

### 结果

| 集合 | 数量 | 占比 |
|---|---|---|
| 共同分母 | 1532 | 1540 − 4 empty − 4 unresolved |
| G（oracle 全 gold 答对） | 1391 | 90.8%（answerer 天花板） |
| D（deterministic 答对） | 1306 | 85.25% |
| **Residual（G∖D）** | **149** | 理论 Planner 可救空间 ≈ +85 题 ≈ +5.5pp |

类别分布：temporal 45 / single-hop 60 / multi-hop 27 / open-domain 17。

### Oracle 修复历程（022 工具的 3 类 bug，均 harness 侧、引擎零改动）

fixed-gold oracle 在完整 LoCoMo 上连挂 5 轮才跑通，根因按层拆解：

1. **数据标注脏格式**（cancelRun 直接触发）：
   - 分号打包 `D8:6; D9:17`（conv-0-q-37，全数据 1 处）→ `fixedGoldSplitEvidenceDatasetIDs` 按 `;`/`,`/空白拆分。
   - 空格打包 `D9:1 D4:4 D4:6`（conv-8×3）+ 前导零 `D30:05`（conv-9）→ split 扩充分隔符 + `:0+(\d+)$` 归一。
   - 引用缺失 source `D10:19`/裸`D`/`D:11:26`/`D4:36`（conv-3/4/6 各 1）→ `fixedGoldUnanswerableQuestionIDs` 从 denominator 排除（`unresolved_evidence_skipped`），不 cancelRun。
2. **瞬时 answer 失败**（fail-closed 一题失败毁全 run）：同 input run1 成功 run2 失败（vllm 全程 200、无卡顿）→ oracle 是 diagnostic 臂，answer/judge 改 `gateUsage`（透明重试 1 次），B1 正式 `gateUsageOnce` 不动。
3. **重建校验 drift**（run 完整后暴露）：expected 的 `DatasetSourceIDs` 未用 split → 与 runtime record 不一致 → `question_reconstruction_invalid`。修复 `buildFixedGoldExpectedQuestions` 也走 split。

> 教训：oracle fail-closed 语义（任一题 invalid → cancelRun 级联 16 个）使任何单个缺陷都表现为"卡在某 conv"；v3/v4 表面像 vllm 崩溃，实为数据缺陷 + 级联。诊断用 call journal 的 terminal `success` 与位置分布（失败全挤末尾 = 级联）最有效。

## P0.4 — 底模选型记录（T004）

- **默认：Qwen2.5-7B-Instruct（Apache-2.0）** —— 许可可分发/可推荐（Llama-3.1/Gemma-2 均 gated + 商用条款受限）；7.6B（7,615.6M，qwen2 架构）BF16 ~15 GiB，单卡 24 GiB LoRA 训练 + p95≤2s 推理可行；同族 3B/1.5B 可降级（tokenizer 一致）。
- **tokenizer**：Qwen2.5 tokenizer（GPT2 风格 BPE，vocab 151,936）；**chat template**：ChatML（`<|im_start|>`/`<|im_end|>`）。train_lora.py 用 `apply_chat_template`，与 local_planner.go 的 system/user 渲染保持一致。
- **摘要/digest**（validation 冻结时用 HF transformers 计算并记录，FR-022）：tokenizer digest、chat-template 摘要、模型 SHA。本记录为已知事实，正式 digest 待冻结。
- 来源：[plan.md Key Decision 1](plan.md) · [spec.md Amendment 2](spec.md)。

## 待办挂钩

- ✅ T003 已完成（residual = 149 题，verdict **`READY`**，2026-08-03）。
- 下一阶段：数据构建（T008–T013）与训练（T015）可启动（spec FR-003）。
- Guard 后补（不阻塞启动，收口前不跨基准外推）：LongMemEval-S B1、judge audit、miss attribution。
