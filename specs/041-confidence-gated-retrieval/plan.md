# Implementation Plan: Confidence-Gated Iterative Retrieval

**Branch**: `worktree-041-confidence-gated-retrieval` | **Date**: 2026-08-14 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `/specs/041-confidence-gated-retrieval/spec.md`

## Summary

用 **answerer 生成中的确定性犹豫/置信度信号**（而非检索分数）决定「读到多少停」：浅预算检索并作答，若 answerer 犹豫则加深检索重答，自信则停在浅预算。替代当前固定 top-k 150 的「大力出奇迹」。机制锚定 040 verdict 的「89% 犹豫、仅 7% 自信地错」实证发现，理论上限 91.75%（> 90.13%）、平均预算收敛 ~31（省 4.8×）。**正确率优先于省预算**，任何评测回退判失败。只在 eval harness 层（`cmd/locomo-bench`）实现，不触碰引擎。

## Technical Context

**Language/Version**: Go 1.25（CGO=0，无新增第三方依赖）

**Primary Dependencies**: 现有 `cmd/locomo-bench` 内部函数（无新 dep）

**Storage**: N/A（迭代决策记录写入 run-dir 的 audit JSONL）

**Testing**: Go 单测（确定性——犹豫检测器对同一文本同一判定）+ 离线集成测试（nil embedder / stub vectors，宪法 IV 的 parity 证明方式）

**Target Platform**: LoCoMo eval harness（`cmd/locomo-bench`，AutoDL 远端跑全量）

**Project Type**: eval-harness 功能（`cmd/` 包内新增文件，非引擎）

**Performance Goals**: 全量 1540 题 3-rep 跑批可完成（现有 infra）；平均输入 token 低于固定 top-k 150 基线

**Constraints**:
- **不触碰引擎**：`git diff --name-only -- memory embedding provider store internal` 必须为空（宪法 II）
- **默认关闭**：关闭时行为与固定 top-k 逐字节一致（FR-001 / SC-003）
- **正确率优先**：配对评测无统计显著回退是硬门槛；省预算不是硬承诺（SC-002 为条件结果）
- **frozen protocol 不动**：formal B1 冻结协议（`eval_runner.go` 的 `evalBudgetProtocol{RetrievalCallLimit: 1, AnswerCallLimit: 1}`）不得被迭代改变；迭代走独立 opt-in 路径
- **thinking 依赖**：犹豫信号从 answerer 生成文本（含 thinking）提取；`LOCOMO_NO_THINKING=0` 时 thinking 在 content 内（`extractFinalAnswer` 已剥离）；thinking 不可得时回退固定深度（FR-005）

**Scale/Scope**: 全量 LoCoMo 1540 题（3-rep 多数票）；只做「检索后、作答前/中」的证据量决策，不触及写入侧

### 关键既有函数（迭代的接入点）

| 函数 | 位置 | 作用 |
|---|---|---|
| `retrieveWithQuotaDiagnostics(ctx, r, query, topK, quota, selector)` | `cmd/locomo-bench/chunks.go:171` | **per-call topK 检索**——迭代的第二轮可传更深 topK |
| `answerCall(ctx, system, user)` (`usageModelCaller`) | `cmd/locomo-bench/runner.go:110` (`newUsageModelCaller`) | 一次性流式收完 answerer 生成；`p.Stream` 循环收集 EventTextDelta |
| `buildAnswerContextPrompt(question, hits, currentDate, category, scaffold)` | `cmd/locomo-bench/runner.go:492` | 把 hits（`[]memory.Result`）拼成 answerer 的 user 上下文 |
| `extractFinalAnswer(pred)` | `cmd/locomo-bench/runner.go:662` | 剥离 `</thinking>` 等 closing delims，取最终答案——**thinking 在 content 里的证明** |
| `isIDK(predicted)` | `cmd/locomo-bench/runner.go:422` | 「I don't know」bail-out 判定（现成的一个弱犹豫信号） |
| `answerSystemPromptForEval(qa, opt)` | `cmd/locomo-bench/runner.go:398` | answer prompt 选择（统一契约/legacy 栈） |
| `--top-k`（默认 30）、`--chunk-quota` | `cmd/locomo-bench/main.go:443/447` | 现有预算 flag |
| `results-hybrid.jsonl` 的 `correct/gold/predicted` | run-dir | 040 verdict 的「自信 vs 犹豫」分析数据源；US1 检测器验证的标注集 |

### 数据事实（research 依据）

- top-k 30 = 88.51%，top-k 150 = 90.13%（全量 3-rep 多数票，`040-adaptive-topk-verdict.md`）
- 30→150 的 56 题增量：42 上下文量 + 11 召回 + 3 短答案；另有 **31 题「150 反而害」**
- 56 题在 30 下的犹豫分布：**强犹豫 55% + 弱犹豫 38% + 自信地错仅 7%**
- 理论上限 = 1332（30 下对）+ 31（避 150 害）+ 50（犹豫→加深救回）= 1413 = **91.75%**

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 评估 | 判定 |
|---|---|---|
| I. 本地优先离线 | 迭代是 harness 内纯本地逻辑，无新增网络/云依赖 | ✅ PASS |
| II. 引擎/适配分离 | 只在 `cmd/locomo-bench`（harness 层），引擎五个目录零改动 | ✅ PASS（git diff 门禁） |
| III. 契约优先 | CLI flag + 犹豫检测器 API + 迭代决策 JSON 在 plan/contracts 冻结 | ✅ PASS |
| IV. 评测回归门禁（**NON-NEGOTIABLE**） | 迭代默认关闭 → 关时逐题与基线一致（SC-003）；开时配对比对基线 90.13% 无显著回退；评测口径改动（新 flag）与算法分开提交 | ✅ PASS（硬门禁，验证手段 = 单测 + parity + 远程全量配对） |
| V. 优雅降级 | thinking 不可得/信号缺失 → 回退固定深度（FR-005）；不整体报错 | ✅ PASS |

## Project Structure

### Documentation (this feature)

```text
specs/041-confidence-gated-retrieval/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output（CLI flags + 检测器 API + 决策记录 JSON）
└── tasks.md             # Phase 2 output (/speckit-tasks)
```

### Source Code (cmd/locomo-bench/)

```text
cmd/locomo-bench/
├── confidence_gate.go           # 犹豫信号检测器：确定性文本规则 → 犹豫强度得分 → 判定
├── confidence_gate_test.go      # 检测器单测：已知犹豫/自信样本 + 确定性 + 边界（空/无 thinking）
├── iterative_retrieval.go       # 迭代循环：浅 retrieve → answer → 犹豫判定 → 深 retrieve → 重答 → 决策记录
├── iterative_retrieval_test.go  # 迭代流程单测（stub answerer 返回可控犹豫/自信）+ parity（关闭时逐字节一致）
├── iterative_retrieval_cli.go   # flag 接线（--confidence-gated 等）+ run-dir audit 落盘
└── main.go                      # flag 注册（+1~2 行）
```

**Structure Decision**: 全部新增文件放 `cmd/locomo-bench/`（harness 包内，`package main`），沿用 040 `adaptive_topk.go` 的既有模式（同目录、同包、单测同包）。这是本项目 eval 功能的既定结构（agentic_nav / gap_retrieval / confidence 系同款），不新建目录。

## Complexity Tracking

无违反。本 feature 未触碰引擎、未新增依赖、未改 frozen protocol、默认关闭——宪法五项原则均无违规，无需 justify。

## Phase 0: research.md 待决议

1. **犹豫信号规则集**（FR-002/003）：从 predicted 文本（含 thinking）确定性提取的具体规则
2. **检测器验证方法**（US1）：全量标注集上的区分度口径与门槛
3. **迭代结构**（FR-004）：两轮 vs 多轮，深浅预算取值，maxRounds
4. **成本计量**（SC-002）：上下文 token vs LLM call 的经济学
5. **frozen protocol 隔离**：迭代 opt-in 路径与 formal B1 冻结协议如何共存
