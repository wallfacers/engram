# Implementation Plan: 多查询检索 —— 提质型深召回(multi-query retrieval)

**Branch**: `010-multi-query-retrieval` | **Date**: 2026-07-23 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/010-multi-query-retrieval/spec.md`

## Summary

给引擎一个新公共入口 `SearchMulti(ctx, subqueries []string, k int)`:对每个子查询各跑一遍现有三信号 hybrid,得到各自的有序命中名单作为「投票者」,再用引擎**已有的 RRF(k=60,tuning-free)**做 RRF-of-RRF 融合、返回正常 top-k。`len==1` 短路回 `SearchWithDiagnostics`、逐字节等于 `Search`,离线默认零改;引擎**只收 `[]string`、永不碰 query-时 LLM**。US1 交付这条纯 Go 融合机制(引擎增量、退化保真、单测即验、free);US2 在 adapter(`cmd/locomo-bench`)侧用 LLM 分解 ≤4 子查询喂进来,过三道门(纯 Go 契约 / 离线召回诊断 / 端到端配对 McNemar 决胜),并证明 answer-context 不涨(提质硬约束)。

## Technical Context

**Language/Version**: Go 1.25.0(CGO 禁用,纯 Go / cross-compilable)

**Primary Dependencies**: 引擎内部 —— `SearchWithDiagnostics`(每子查询精检的复用核心)、`fuseRRF`(现有 RRF,复用作二级融合)、`ranksFromOrder`(有序名单→ranks map)、`Result`/`RetrieverOptions`。US2 adapter 侧:`cmd/locomo-bench` 的答题 LLM provider(复用作 query 分解器)、现有 store 加载与 chunk 设施。

**Storage**: 复用 009 固化 SQLite store(HF `009-bge-chunks-store`,bge-large 1024d + chunks;retrieval-only,不重抽取);端到端产物写 gitignored `.locomo-run/010-*/`。

**Testing**: `CGO_ENABLED=0 go test ./memory`;US1 parity golden(`SearchMulti(ctx,[]string{q},k)` 逐字节等于 `Search`)+ 融合正确性 golden(小 fixture,gold 单查询 rank>k → RRF-of-RRF 进 top-k、共同命中排名更前,确定性无 LLM)。US2:离线召回 delta(canned 子查询,coverage 诊断)+ 端到端配对 McNemar 脚本。

**Target Platform**: 本地 Linux(WSL2 开发);US2 决胜门在 box vllm Qwen 栈(远端隧道)+ deepseek mem0-aligned judge。

**Project Type**: engine library(`memory/`)+ eval adapter(`cmd/locomo-bench`)。US1 只动 engine;US2 只动 adapter。

**Performance Goals**: `SearchMulti` 纯 Go,N 个子查询串行/并发精检(N≤4),融合是 O(Σ|L_i|);不引入云调用、不显著超过 N× 单查询检索延迟。US2 分解每题一次轻量 query 重写(远比 filter-pool 读 200 候选便宜)。

**Constraints**: 引擎离线可跑;`len==1` 退化保真逐字节不变;融合复用现有 RRF 常数、**无新可调权重**(tuning-free 可移植);禁云/付费 reranker(死规则);**最终 top-k=30 不变、answer-context 不涨**(提质硬约束——涨即加量判负);US2 期间 `git diff -- memory embedding provider store internal` 为空。

**Scale/Scope**: LoCoMo locomo10 全量 1540 题(目标类 multi-hop;single-hop/temporal/open-domain 作非目标类监控不回退);单机单用户评测规模。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| **I 本地优先/离线** | ✅ PASS | `SearchMulti` 检索路径无 query-时 LLM、纯 Go/offline;LLM 分解是 adapter opt-in 策略,默认单查询路径不变 |
| **II 引擎/适配器分离** | ✅ PASS | 融合机制(不可重造算法)入引擎;LLM 分解(调用方策略)留 adapter;US2 引擎零改 |
| **III 契约优先/命名空间** | ✅ PASS | `SearchMulti` 新增公共 API contract-first、非破坏、无 schema 变更;`len==1` 退化保真 |
| **IV 评测回归门(非协商)** | ✅ PASS | US2 三道门,端到端配对 McNemar + context-parity 提质证明;US1(engine)/US2(adapter)提交分离 |
| **V 优雅降级/诚实规模** | ✅ PASS | 空子查询/空检索静默降级;越不过=NO-GO 保留诊断;coverage 不作 verdict |
| **死规则(无付费 rerank)** | ✅ PASS | 全程无云/付费 reranker 作杠杆;不撑 top-k、不扩池(反加量哲学) |

初判无违规;Complexity Tracking 空。post-design 复检见文末。

## Project Structure

### Documentation (this feature)

```text
specs/010-multi-query-retrieval/
├── plan.md              # 本文件
├── research.md          # Phase 0:决策(len==1 退化保真如何实现 / 每子查询精检深度 / RRF-of-RRF 与既有后处理次序 / N 上限归属 / 分解器复用)
├── data-model.md        # Phase 1:SubQueries/FusedRanking/ContextParityCheck/DecompositionPolicy
├── quickstart.md        # Phase 1:如何跑 US1 单测 + 验 SC + US2 三道门流程
├── contracts/           # Phase 1:SearchMulti 引擎 API 契约 + 融合语义 + adapter 分解/context-parity 契约
└── tasks.md             # Phase 2(/speckit-tasks,非本命令产出)
```

### Source Code (repository root)

```text
memory/                          # US1 全部在此(engine 增量,单独 commit)
├── retriever.go                 # 新:SearchMulti(ctx, subqueries []string, k int) ([]Result, error)
│                                #   复用 SearchWithDiagnostics 逐子查询精检 → 有序名单投票者 → fuseRRF 二级融合 → top-k
│                                #   len==1 短路回 SearchWithDiagnostics(逐字节 parity);无新 RetrieverOptions 字段
└── retriever_test.go            # 新:parity(len==1 逐字节)+ 融合正确性 golden(fixture 断言 rank 提升 + 共同命中优先)

cmd/locomo-bench/                # US2 全部在此(adapter,引擎零改,提交分离)
├── decompose.go                 # 新:LLM query 分解策略(≤4 子查询,失败退化单查询)
├── decompose_test.go            # 新:分解退化/上限单测(离线,mock provider)
├── main.go                      # 新 flag:--multi-query(开分解+SearchMulti)+ context-parity 记账
└── (复用现有 store 加载 / journal / stats / McNemar 设施)

.locomo-run/010-*/               # gitignored:离线召回 delta + 端到端配对产物 + context 对比
```

**Structure Decision**: 双层——US1 全部落 `memory/retriever.go`(engine 增量,退化保真,先交付单独 commit);US2 全部落 `cmd/locomo-bench`(adapter,引擎零改,提交分离)。US1 最大化复用现有 `SearchWithDiagnostics`(每子查询精检)与 `fuseRRF`(二级融合),只新增「N 路投票 + RRF-of-RRF」这一薄层;不新增 `RetrieverOptions` 字段(`SearchMulti` 是并列新入口,不改 `Search` 语义)。

## Complexity Tracking

> 无 Constitution 违规,免填。

## Phase 0/1 产物

- Phase 0:[research.md](./research.md) — 全部 NEEDS CLARIFICATION 已解(见其决策表)。
- Phase 1:[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)。
- Post-design 宪法复检:融合复用现有 RRF 常数、无新可调权重、无引擎 schema 变更、`len==1` 退化保真 —— 六条仍 PASS,无新增复杂度。
