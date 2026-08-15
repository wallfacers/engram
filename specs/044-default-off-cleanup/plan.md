# Implementation Plan: 默认关闭机制清理(Default-Off Mechanism Cleanup)

**Branch**: `044-default-off-cleanup` | **Date**: 2026-08-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/044-default-off-cleanup/spec.md`

## Summary

在 `cmd/locomo-bench/` 内移除经 verdict 判定为 NO-GO / 零增量 / 被取代的默认关闭实验机制(flag + 专属实现 + 接线),保留已坐实能力(unified/022 chunk/force-answer)与诊断工具(oracle/rerank/pcic/compiler-arm/representation/temporal-date-scaffold)。范围含:第一类 20+ 已证伪机制 flag、042 协议(13 个 counterfactual 文件)、trace-mediation(默认开启组件,移除后默认路径切 chunk 装配)。引擎五目录零改动、默认路径 byte-parity、eval-config 与代码分开 commit(宪法 IV)。

**关键勘察事实(plan 依据)**:
- **master 不含 043 代码**(deepen 文件/flag 均无,已在独立 worktree),044 不清理 043 代码,仅文档记录结论。
- **042 在 master**:13 个 counterfactual 文件,`--utility-stage` 族 + `utilityExperimentalFlags`(引用 nav/iris/abstain/multi-query 等做互斥)需整体移除。
- **030/031 整链可整体移除**,但执行点在共享文件 `eval_runner.go`(6 处)+ `main.go`(flag 注册)+ `unified_answer_contract_eval.go`(冲突表)——必须同步改这三个共享文件。
- **交叉引用**:多数待清 flag 引用 `eval_runner.go`(arm 机制路由)、`unified_answer_contract_eval.go`(冲突表)、`main.go`(flag 注册/options/fingerprint);清理 = 删 flag + 删孤立代码 + 同步删共享文件引用点。

## Technical Context

**Language/Version**: Go 1.25.0,CGO_ENABLED=0(硬门)

**Primary Dependencies**: 无新增;纯删除现有 `cmd/locomo-bench/` 代码与接线

**Storage**: 无 schema 变更;SQLite store 层不动(引擎零改动)

**Testing**: `CGO_ENABLED=0 go test -count=1 ./...` 全绿;byte-parity(默认路径逐字节不变)+ 已坐实机制测试保持绿;删除专属文件后其测试一并删除

**Target Platform**: Linux(本地 + AutoDL eval box 均不受影响,box 已关机)

**Project Type**: CLI 评测 harness 清理(eval-only 代码移除,不进引擎)

**Performance Goals**: 无性能目标;目标是代码面减小(预计净删 ~1.5 万行含测试)

**Constraints**:
- 引擎五目录(`memory/ embedding/ provider/ store/ internal/`)零改动(git diff 必须空)
- 默认路径 byte-parity:unified 契约 + 022 chunk + force-answer 行为不变
- eval-config 变更(trace 默认值移除)与代码清理分开 commit(宪法 IV)
- 已坐实机制 flag/默认值/行为不变;诊断工具(oracle/rerank/pcic/compiler-arm/representation/temporal-date-scaffold)不误删
- 不碰 box/AutoDL(已关机);不重跑任何已证伪机制

**Scale/Scope**: 删除 ~20 个 flag + 13 个 042 文件 + 030/031 整链文件;修改共享文件 `main.go`/`eval_runner.go`/`unified_answer_contract_eval.go`/`runner.go` 等

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 条款 | 判定 | 依据 |
|---|---|---|
| I 本地优先/默认离线 | ✅ PASS | 清理不改任何引擎路径;保留能力 offline 特性不变 |
| II 引擎/适配器分离 | ✅ PASS | 只动 `cmd/locomo-bench/`;`git diff --name-only -- memory embedding provider store internal` 必须空(tasks 列为验收项) |
| III 契约优先/命名空间隔离 | ✅ PASS | 移除的是评测 flag(非对外契约);unified 契约 digest `1d8a8d0f` 不变(不碰 prompt 常量) |
| IV 评测回归门 | ✅ PASS | 清理不改检索/抽取/存储算法,unified/022 主路径 byte-parity 证明不变(by construction);trace 默认值移除属 eval-config 变更,单独 commit 并记录(Step A 已证 unified 下 trace 显著负) |
| V 优雅降级/诚实规模 | ✅ PASS | 移除的是已证伪机制,非降级路径;保留机制降级语义不变 |

**死亡规则(云端 reranker)**: ✅ 清理不引入任何 reranker 依赖;`--rerank` 保留为 opt-in 诊断(非清理范围)。

Phase 1 后复查:无新增违规。

## Project Structure

### Documentation (this feature)

```text
specs/044-default-off-cleanup/
├── plan.md              # 本文件
├── research.md          # Phase 0:清理范围勘察与决策
├── data-model.md        # Phase 1:无新增实体(仅记录删除契约)
├── quickstart.md        # Phase 1
├── contracts/           # Phase 1:CLI 契约(待清 flag 清单 + 保留 flag 清单)
└── tasks.md             # speckit-tasks 产出
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── main.go                          # 改:删待清 flag 注册 + options 字段 + fingerprint 标记;
│                                    #   删 042/030/031/029/021 的 flag 注册与 dispatch
├── eval_runner.go                   # 改:删 arm 机制路由中已清机制 + 030/031/024/025 执行点(6 处)
├── unified_answer_contract_eval.go  # 改:冲突表删已清 flag 条目(assoc/cluster-sweep/temporal-score/...)
├── runner.go                        # 改:删 abstain/counter-refine 分支;temporalAnswerPrompt 常量保留(032)
├── counterfactual_utility*.go       # 删(13 个文件 + 7 个测试)
├── trace_mediation.go / trace_gate.go / trace_http.go   # 删
├── consolidate.go / assembly.go / assembly_tokens.go / assembly_diagnose.go / entity_grouping.go  # 删(030/031)
├── relation_graph.go                # 删(031)
├── abstain.go                       # 改:删作答分支,computeAbstainSignal 保留(供 abstain_probe 诊断)
├── abstain_probe.go                 # 保留(--abstain-probe 零成本诊断)
├── agentic_nav.go / nav_*.go        # 删(029 nav 系)
├── iris.go                          # 删(021)
├── temporal_resolution.go           # 改:保留 classifyQueryMode(被 miss_attribution 引用),删其余
├── event_projection.go / gap_retrieval.go  # 删(027 gap-refetch 系;确认无保留引用)
├── multiquery.go                    # 改:删 --multi-query 作答路径,保留 recallDiagnostic + SearchMulti
├── alias_shadow.go / doc2query.go   # 删(011/012 NO-GO 研究臂);同步改引用它们的测试
├── filter.go                        # 删(--filter-pool)
└── *对应 _test.go                   # 随专属文件删除;保留机制(032/017/诊断)的测试保留
```

**Structure Decision**: 单包(`cmd/locomo-bench/`)内删除 + 最小侵入改 5 个共享文件(`main.go`/`eval_runner.go`/`unified_answer_contract_eval.go`/`runner.go`/`temporal_resolution.go`)。删除策略分三类:整链删(030/031/042/029/021)、删 flag 保留被引用函数(temporal_resolution/assembly 的共享函数)、删专属文件(abstain/filter/event_projection/multiquery 视引用而定)。

## Complexity Tracking

> 无 Constitution Check 违规,无需豁免条目。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| — | — | — |

## 关键技术决策(承接 research.md)

1. **删除策略三分**:整链删(无保留引用)/ 删 flag 保留共享函数(被诊断/保留能力引用)/ 删专属文件。research.md 逐个列出归属。
2. **030/031 整链移除**:`kindOfEvidence`/`assembleEvidence`/`computeRelationContext` 仅在 030/031 内部 + main.go flag 注册引用,022 chunk / unified 主渲染不依赖 → 整链删除(含 assembly_diagnose/entity_grouping/relation_graph/consolidate/trace 系/assembly_tokens)。
3. **eval_runner.go 是核心共享改动点**:arm 机制路由 + 6 处 030/031/024/025 执行点需同步清理;改后必须全仓测试绿 + byte-parity。
4. **temporal_resolution 不完全删**:`classifyQueryMode` 被 `miss_attribution.go` 与 `eval_runner.go` 引用 → 保留该函数,删其余 `--temporal-resolution` 执行逻辑。
5. **042 整体删**:13 文件 + `--utility-stage` 族 flag + `utilityExperimentalFlags` 互斥检查(引用 nav/iris/abstain/multi-query 的列表随清同步删)。
6. **trace 默认值切换单独 commit**:`--trace-mediation` 默认 true→移除,默认路径从 trace 中介 → chunk 装配;Step A(统一契约下 −3.44pp 显著负)作为变更依据,单独 commit 并记录。
7. **byte-parity 验证**:清理全程每阶段跑 `go test ./...`;unified 契约 digest `1d8a8d0f` 锁锚断言保持;已坐实机制测试不改。
8. **文档同步**:result-matrix「过时/已证伪」表、清理计划文档、README 的机制说明随清理逐项更新。
9. **保留项边界(修正)**:`--temporal-answer-prompt`(032,default-off opt-in)、`--temporal-date-scaffold`(017,022 协议使用)、`--abstain-probe`(诊断)、`recallDiagnostic` + `SearchMulti`(诊断)**均不在清理范围**;`adaptive_topk.go` 已不存在无需处理。
10. **011/012 整删 + 改测试**:`--alias-shadow`/`--doc2query`(NO-GO 研究臂)整删,但被 `eval_fixed_gold_oracle_test.go`/`eval_runner_test.go`/`temporal_diagnostic_test.go` 引用为"非基准配置" → 同步改这些测试(移除已删 shadow 配置引用)。`multiquery.go` 仅删 flag+作答路径,保留 recallDiagnostic+SearchMulti。
11. **abstain 拆分**:删 `--abstain-prompt`/`--abstain-hard`/`--abstain-soft` 作答分支,`computeAbstainSignal` 迁入/保留供 `--abstain-probe` 诊断用。

## 验收顺序(供 speckit-tasks 细化)

1. **独立 worktree + 基线**:044 worktree 已建(master ee363e2);先跑全量测试确认基线绿。
2. **第一类纯 flag 级**(低风险):assoc/cluster-sweep/temporal-score/temporal-hard-filter/conflict-resolution/filter-pool/opinion-pass + 对应 arm 路由/冲突表条目/fingerprint 标记。每批删后 build+test。
3. **010/011/012 族**:`--multi-query` flag+作答路径删(multiquery.go 保留 recallDiagnostic/SearchMulti);`--alias-shadow`/`--doc2query` 整删 + 同步改引用测试。
4. **专属文件机制**:abstain 作答分支删(保留 abstain-probe)/029 nav 系/021 iris/027 gap-refetch+event-projection;temporal_resolution 保留 classifyQueryMode。
5. **030/031 整链 + trace**:trace_mediation/consolidate/assembly 系/relation_graph/entity_grouping + trace 默认值移除(单独 commit)。
6. **042 协议**:13 个 counterfactual 文件 + utility 族 flag + utilityExperimentalFlags。
7. **收尾**:result-matrix/README/清理计划文档同步;引擎五目录 diff 空确认;全量 build+test+vet 绿;保留项(temporal-answer-prompt/temporal-date-scaffold/pcic/compiler-arm/representation/oracle/rerank/abstain-probe/recallDiagnostic)逐个核对未被误删。
