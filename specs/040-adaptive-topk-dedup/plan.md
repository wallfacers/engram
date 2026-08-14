# Implementation Plan: 自适应检索深度

**Branch**: `worktree-040-adaptive-topk-dedup` | **Date**: 2026-08-13 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/040-adaptive-topk-dedup/spec.md`

## Summary

在 eval harness 层实现一个 **opt-in、默认关闭** 的「自适应检索深度」机制：对每个问题的 RRF 融合分数序列做 gap-knee 检测，per-query 决定检索证据量 k*（替代当前所有问题共用同一 `--top-k 150`），在「不丢关键证据、不显著回退」的前提下降低平均证据消耗。**去冗余方向因 024/025/026 三连证伪已移出 scope**。第一优先级是零成本诊断（US1）——先证明「缩减深度不丢 gold」这一前提，再谈实现（US2）。引擎公开 API 零改动。

技术选型见 [research.md](research.md)，实体见 [data-model.md](data-model.md)，接口见 [contracts/cli-adaptive-topk.md](contracts/cli-adaptive-topk.md)。

## Technical Context

**Language/Version**: Go 1.25.0（纯 Go，`CGO_ENABLED=0`）

**Primary Dependencies**: 无新第三方依赖（gap-knee 检测是标准库 `sort` + 数值计算的纯 Go 实现）

**Storage**: SQLite（本 feature **不触碰存储**，无新表、无迁移）

**Testing**: `CGO_ENABLED=0 go test ./cmd/locomo-bench`（gap-knee 检测单测 + 诊断逻辑单测，离线 stub）

**Target Platform**: Linux（WSL2 开发 + AutoDL 评测 box）

**Project Type**: Go library（引擎）+ eval harness（`cmd/locomo-bench`）；本 feature 只改 harness

**Performance Goals**: knee 检测是 O(n) 单次扫描（宽池 ≤300 条），每 query 微秒级，不增加检索延迟

**Constraints**: 纯 Go 无 CGO；引擎零改动（宪法 II）；离线可运行（宪法 I）；`--adaptive-topk` 关闭时逐字节一致（FR-001）

**Scale/Scope**: LoCoMo cat 1–4 共 1540 题；宽池 `max(300, topK*6)`

## Constitution Check

*GATE: 五原则逐一核对，全部通过才进入实现。*

| 原则 | 核对 | 结论 |
|---|---|---|
| I. 本地优先/默认离线 | 自适应截断纯计算，无网络/模型调用；诊断仅复用本地检索 | ✅ |
| II. 引擎/适配层分离 | 改动全在 `cmd/locomo-bench`（适配层），`memory/` 引擎零改动 | ✅ |
| III. 契约优先/命名空间隔离 | spec + contracts 已冻结 CLI flag 与诊断 schema；引擎契约不变（无 MAJOR bump） | ✅ |
| IV. 评测回归门禁 | 阶段 1 端到端配对（同库同模型同 judge）是硬门禁；`--adaptive-topk` 默认关，转正须不显著回退 | ✅ |
| V. 优雅降级/规模诚实 | 无拐点回退固定深度（FR-004）；信号缺失独立降级（FR-007）；规模如实声明（1540 题评测，无夸大） | ✅ |

无违宪项，Complexity Tracking 保持空。

## Project Structure

### Documentation (this feature)

```text
specs/040-adaptive-topk-dedup/
├── plan.md              # 本文件
├── research.md          # Phase 0：算法/诊断/插入点/评测协议选型
├── data-model.md        # Phase 1：检索结果项、自适应截断点、诊断记录
├── contracts/
│   └── cli-adaptive-topk.md  # Phase 1：CLI flag 契约 + 诊断输出 schema
├── quickstart.md        # Phase 1：阶段 0 诊断 + 阶段 1 配对验证
├── spec.md              # 需求规格（scope 已收窄为自适应深度）
└── tasks.md             # Phase 2（/speckit-tasks 产出，非本命令）
```

### Source Code (repository root)

本 feature **只改 eval harness**，引擎（`memory/ embedding/ provider/ store/ internal/`）零改动（用 `git diff --name-only -- memory embedding provider store internal` 验证为空）。

```text
cmd/locomo-bench/
├── adaptive_topk.go          # 新增：gap-knee 检测 + 自适应 k* 计算（纯 Go）
├── adaptive_topk_test.go     # 新增：knee 检测单测（离散分数序列、无拐点回退、minK clamp）
├── adaptive_diagnose.go      # 新增：headroom 诊断命令（--adaptive-topk-diagnose）
├── adaptive_diagnose_test.go # 新增：诊断汇总逻辑单测
├── chunks.go                 # 改：retrieveWithQuotaDiagnostics 插入自适应 k*（flag off 时不触达）
└── main.go                   # 改：新增 --adaptive-topk / --adaptive-min-k / --adaptive-topk-diagnose flag
```

**Structure Decision**: 单项目结构。诊断与自适应逻辑都放在 harness 包内（复用现有 `retrieveWithQuotaDiagnostics` 的宽池检索路径），引擎公开 API 不变。

## Complexity Tracking

> 无违宪复杂度，本节留空。
