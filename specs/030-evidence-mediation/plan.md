# Implementation Plan: 读侧证据装配结构（Evidence Mediation）

**Branch**: `030-evidence-mediation` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/030-evidence-mediation/spec.md`

## Summary

把「检索后、作答前」的证据装配结构固化并验证：US1 预算诚实的装配地基（真实 token 精确记账 + chunk 原文优先保底 + 类别条件组织结构），US2 引用链证据中介（MemChain 式 plan→trace→actions→evidence，sidecar opt-in），US3 条件压缩操作符（预算不足才压缩）。所有 arm 默认关、端到端配对为唯一 GO 门（008 铁律），引擎零改动，只动 `cmd/locomo-bench/` 与 `specs/030/tools/`。

## Technical Context

**Language/Version**: Go 1.25.0，**无 CGO**（硬约束，`CGO_ENABLED=0 go build/test` 必须绿）

**Primary Dependencies**: 现有 harness（`cmd/locomo-bench`）零新增框架依赖；可能新增：纯 Go tokenizer（精确记账用，待 research 决策）或 harness 侧 vllm `/tokenize` HTTP 调用（复用 029 `nav_http.go` 的 harness-side vLLM caller 模式）

**Storage**: 现有 SQLite store 只读复用（装配不写库）；审计产物 = 轨迹/装配 JSONL（`os.OpenFile(..., 0o600)`，029 模式）

**Testing**: `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`；离线单测（stub tokenizer/provider，无网络）；配对评测 `cmd/locomo-bench`（同 store/子集/answerer/judge/预算，008 铁律）

**Target Platform**: Linux（WSL2 编译 → 远端 AutoDL 跑 e2e；本地可跑 US1 纯 Go 装配）

**Project Type**: 评测 harness 扩展（纯 Go 库级扩展 + 可选 sidecar 调用），非独立服务

**Performance Goals**: US1 装配零模型成本（纯 Go）；US2 trace 生成每题 ≤ 数秒（DeepSeek-flash，028 已验证）；装配管线不增加 answerer 上下文延迟

**Constraints**:
- 引擎 untouchable（宪法 II）：`git diff --name-only -- memory embedding provider store internal` 必须为空
- 无 CGO、依赖最小化（纯 Go 优先）
- 新机制默认关（FR-005/007），关闭时与现有路径 parity
- answer-context 预算 cap 3600（`defaultAnswerContextCap`，008 纪律）
- 密钥走 env，绝不进 tracked 文件/log（constitution Secrets）

**Scale/Scope**: LoCoMo 1540 全量（US1 离线装配，零模型成本）+ 84 题配对子集（US2/US3 答题，029 模式）；参考点 LoCoMo 85.71%

**Known unknowns (→ research.md)**:
1. **[UU-1]** 真实 token 精确计数的实现路径：vllm `/tokenize` API（harness 侧 HTTP，需连答题模型）vs 纯 Go tokenizer（离线、无 CGO）vs 保留估算+离线校准。影响 FR-002 的实现与离线单测边界。
2. **[UU-2]** 配对评测口径：84 题子集（temporal 59 + multi-hop 25，029 模式）vs 全量 1540。影响 US2/US3 的统计功效与成本。
3. **[UU-3]** chunk 保底 fraction 目标阈值：029 现状 ~1%，基线 3654 tokens ≈ 12 chunk-quota 槽；阈值定多少（≥50%? ≥80%?）影响 SC-002 判定。
4. **[UU-4]** 类别条件装配的复用边界：`buildTimelineBlock`（017 temporal 块）+ `--cat-chunk-quota` 已有雏形；multi-hop 实体组织是否有 021 契约可复用（`contracts/iris-loop.md`）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 检查 | 状态 |
|---|---|---|
| **I 本地优先/默认离线** | US1 纯 Go 零模型离线；US2 trace 生成走本地 sidecar opt-in（DeepSeek-flash，默认关）；无托管服务必需 | ✅ PASS |
| **II 引擎/适配分离** | 实现仅 `cmd/locomo-bench/` 与 `specs/030/tools/`；FR-001 硬门：引擎 diff 为空 | ✅ PASS |
| **III 契约优先/隔离** | 本 plan 先定 data-model/contracts（trace schema、装配契约、fail-closed 规则）再实现 | ✅ PASS |
| **IV 评测回归门禁** | 008 铁律 majority ≥ 基线为唯一 GO 门；评测配置与算法分开 commit；LoCoMo 全量离线装配可作不回归佐证 | ✅ PASS |
| **V 优雅降级/诚实** | estimateTokens 保留为 fallback（显式降级标记）；fail-closed 依次降级；新机制默认关 + parity | ✅ PASS |

**Complexity Tracking**: 无违规（本 feature 不新增依赖到引擎核心路径、不引入引擎改动；纯 harness 扩展，无复杂度需要证成）

## Project Structure

### Documentation (this feature)

```text
specs/030-evidence-mediation/
├── plan.md              # This file
├── research.md          # Phase 0 output（UU-1..4 决策）
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── evidence-assembly.md
│   ├── grounded-trace.md
│   └── consolidation.md
└── tasks.md             # Phase 2 output (/speckit-tasks)
```

### Source Code (repository root)

```text
cmd/locomo-bench/            # harness 扩展（全部新增文件；现有文件仅加 flag/接线）
├── assembly.go              # US1: 证据装配器（精确记账 + chunk 优先 + 类别条件排序）
├── assembly_tokens.go       # US1: token 精确计数（UU-1 决策实现；estimateTokens fallback）
├── assembly_test.go         # US1: 离线单测（记账精确性/chunk 保底/类别排序/parity）
├── trace_mediation.go       # US2: 引用链中介（plan→trace→actions→evidence）
├── trace_gate.go            # US2: fail-closed 校验门（纯 Go 确定性：非法 ID 丢弃/解析失败回退）
├── trace_http.go            # US2: harness-side sidecar caller（DeepSeek-flash，029 nav_http 模式）
├── trace_mediation_test.go  # US2: 离线单测（stub provider；非法 ID/解析失败/parity）
├── consolidate.go           # US3: 条件压缩操作符（默认关；仅超预算 opt-in）
├── consolidate_test.go      # US3: 离线单测（默认关 parity；超预算触发）
└── main.go                  # 仅加 --evidence-assembly / --trace-mediation / --consolidate flags（默认关）
specs/030-evidence-mediation/
├── tools/
│   ├── assembly_diagnose.py   # 装配诊断分析（token 记账/chunk fraction/类别结构审计）
│   ├── trace_analyze.py       # trace 配对分析（majority + McNemar + 类别回归）
│   └── consolidation_analyze.py # 压缩配对分析（预算交叉验证）
└── diagnosis/                 # 各 US verdict（us1/ us2/ us3）
```

**Structure Decision**: 单项目扩展（评测 harness 既有形态），全部新增独立 `.go` 文件 + 现有文件仅加 flag/接线，保持 029 的「新增文件 + 条件分支 + 默认关」模式，引擎零改动。

## Complexity Tracking

> 无宪法违规需要证成。本 feature 复杂度来自论文机制本身（trace 四层、预算交叉），非绕过约束所致；已通过默认关 + 分级 US 门禁（P1→P2）控制。
