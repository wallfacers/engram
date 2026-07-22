# Implementation Plan: 归因门控的检索排序(evidence-gated retrieval ranking)

**Branch**: `009-retrieval-attribution-gate` | **Date**: 2026-07-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/009-retrieval-attribution-gate/spec.md`

## Summary

先建逐题检索归因证据基座(US1,纯 adapter/引擎零改/near-free),把"检索排序是最大错因"细化为逐题四象限证据 + `gold_rank`/`outranked_by`;再用证据门控一个纯 Go tuning-free 定向排序改动(US2,引擎增量/默认关/三道门)。技术路线复用 `cmd/locomo-bench` 现有 `evidence.go`/`coverage.go`/`chunks.go` 的 gold 解析与 chunk→turn 映射设施,扩展出一条无条件、含逐 hit rank 的归因 trace。

## Technical Context

**Language/Version**: Go 1.25.0(CGO 禁用,纯 Go / cross-compilable)

**Primary Dependencies**: 现有引擎公共 API `memory.Retriever.SearchWithDiagnostics`;harness 内 `evidence.go`(gold 解析 `evidenceReferences`/`evidenceSessions`/`exactTurnRecall`)、`chunks.go`(`chunkTurns` chunk→turn 映射)、`coverage.go`(`coveredGoldTurns`)、`journal.go`(JSONL 逐题记录)。US2 触及 `memory/retriever.go`(`fuseRRF`/候选构建)与 `RetrieverOptions`。

**Storage**: 复用 008 固化 SQLite store(retrieval-only,不重抽取);trace 与分布表写入 gitignored `.locomo-run/009-*/`。

**Testing**: `CGO_ENABLED=0 go test`;US1 确定性 golden(小 fixture 断言 `gold_rank`+`outranked_by`);US2 parity golden(关时逐字节)+ 离线归因 delta + 端到端配对 McNemar 脚本。

**Target Platform**: 本地 Linux(WSL2 开发);US2 决胜门在本地 vllm Qwen 栈(远端隧道)+ deepseek judge。

**Project Type**: engine library(`memory/`)+ eval adapter(`cmd/locomo-bench`)。US1 只动 adapter;US2 动 engine。

**Performance Goals**: US1 retrieval-only,答题模型调用 = 0(near-free);归因确定性可复算。US2 排序机制纯 Go,不显著增加检索延迟(参照现 `fuseRRF` 量级)。

**Constraints**: 引擎离线可跑;禁云/付费 reranker(死规则);US1 期间 `git diff -- memory embedding provider store internal` 为空;US2 默认关零值 = 现基线逐字节不变。

**Scale/Scope**: LoCoMo locomo10 全量 1540 题(目标类 single/multi-hop/temporal);单机单用户评测规模。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| **I 本地优先/离线** | ✅ PASS | US1 retrieval-only 全离线;US2 纯 Go/offline,禁云 reranker |
| **II 引擎/适配器分离** | ✅ PASS | US1 纯 adapter 引擎零改;US2 走 `RetrieverOptions` 公共契约、默认关。**风险点**:per-signal rank 不在 `Result` 上暴露 → 见 research 决策(延到 US2 引擎增量,US1 不碰引擎) |
| **III 契约优先/命名空间** | ✅ PASS | US2 contract-first,零值不变;US1 不碰引擎 schema |
| **IV 评测回归门(非协商)** | ✅ PASS | US2 三道门,端到端配对 McNemar 决胜,提交与 US1 分离 |
| **V 优雅降级/诚实规模** | ✅ PASS | US2 越不过=NO-GO 保留诊断;coverage 不作 verdict |
| **死规则(无付费 rerank)** | ✅ PASS | 全程无云/付费 reranker 作杠杆 |

初判无违规;Complexity Tracking 空。post-design 复检见文末。

## Project Structure

### Documentation (this feature)

```text
specs/009-retrieval-attribution-gate/
├── plan.md              # 本文件
├── research.md          # Phase 0:决策(基座复用/per-signal rank 归属/embedding 探针/四象限 join)
├── data-model.md        # Phase 1:AttributionTrace/QuadrantDistribution/EmbeddingDeterminismProbe/RankingOption
├── quickstart.md        # Phase 1:如何跑 US1 trace + 验 SC + US2 三道门流程
├── contracts/           # Phase 1:CLI 归因模式契约 + trace.jsonl schema + RetrieverOptions bool 契约
└── tasks.md             # Phase 2(/speckit-tasks,非本命令产出)
```

### Source Code (repository root)

```text
memory/
├── retriever.go         # US2 only:fuseRRF/候选构建 + RetrieverOptions 新 bool(默认关);
│                        #   US2 增 SearchDiagnostics.PerSignalRanks 暴露(contract-first)
└── retriever_test.go    # US2:parity(关时零变)+ 新排序单测

cmd/locomo-bench/        # US1 全部在此(引擎零改)
├── attribution.go       # 新:无条件逐题归因 trace(gold_rank/outranked_by/四象限)
├── attribution_test.go  # 新:确定性 golden(fixture 断言 rank+outranked_by)
├── embed_probe.go       # 新:embedding 查询确定性探针
├── evidence.go          # 复用:gold 解析 + chunk→turn 映射(不改语义)
├── coverage.go          # 复用:coveredGoldTurns
├── chunks.go            # 复用:chunkTurns
├── journal.go           # 复用/微扩:trace 记录落 JSONL
└── main.go              # 新 flag:--attribution-trace(retrieval-only 入口)

.locomo-run/009-*/       # gitignored:trace.jsonl + 分布表 + US2 端到端产物
```

**Structure Decision**: 双层——US1 全部落 `cmd/locomo-bench`(adapter,引擎零改,先交付单独 commit);US2 落 `memory/retriever.go`(engine 增量,默认关,提交分离)。US1 最大化复用 harness 现有 gold 解析/映射设施,只新增"无条件 + 逐 hit rank + 归因判定"这一薄层。

## Complexity Tracking

> 无 Constitution 违规,免填。

## Phase 0/1 产物

- Phase 0:[research.md](./research.md) — 全部 NEEDS CLARIFICATION 已解(见其决策表)。
- Phase 1:[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)。

## Post-Design Constitution Re-Check

设计后复检:per-signal rank 归属决策(延到 US2 引擎增量、US1 严格零引擎改动)消除了唯一的 II 风险点;其余六项维持 PASS。无新违规,可进入 `/speckit-tasks`。
