# Tasks: 默认关闭机制清理(044)

**Generated**: 2026-08-16 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

任务按 plan.md 验收顺序分为七段:基线 → 纯 flag 级 → 010/011/012 族 → 专属文件机制 → 030/031+trace → 042 协议 → 收尾。每批删除后跑构建+测试门。**红线**:引擎五目录零改动、默认路径 byte-parity、已保留能力(032/017/诊断/未定论)不误删、commit 前缀 `chore(cleanup)` 且不 push、trace 默认值移除单独 commit。

## Phase 1 · 基线确认

- [ ] T001 在 044 worktree 确认基线:`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...`、`CGO_ENABLED=0 go vet ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空;记录待删文件清单(counterfactual_utility*.go 13 个 + trace/assembly/relation/nav/iris 等专属文件)与保留文件清单(032/017/诊断)

## Phase 2 · 纯 flag 级删除(低风险,每批删后 build+test)

- [ ] T002 删除 `--assoc`/`--assoc-depth`/`--cluster-sweep`/`--temporal-score`/`--temporal-hard-filter` 的 flag 注册与 options 字段(main.go)、arm 机制路由(eval_runner.go + main.go supportedArmMechanisms)、冲突表条目(unified_answer_contract_eval.go)、fingerprint 标记(main.go answerRegimeFingerprint);`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T003 删除 `--conflict-resolution`/`--superseded-penalty`/`--filter-pool`/`--opinion-pass` 的 flag 注册与 options 字段(main.go)、分支执行点、冲突表条目;确认 `filter.go`/`doc2query.go`(仅 opinion-pass 分支)无保留引用后删;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿

## Phase 3 · 010/011/012 族(多查询/影子臂)

- [ ] T004 [P] 删除 `--multi-query`/`--mq-max-subqueries` 的 flag 注册与 options 字段(main.go)与作答路径(multiquery.go 中 `opt.multiQuery` 分支);**保留 `recallDiagnostic` 诊断逻辑与 `SearchMulti` 引用**(multiquery.go 中 `recallDiagnosticQuestion`/`SearchMulti` 调用);确认 alias_shadow.go 中 `multiQueryArm` 引用随 011 删除一并消失;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T005 [P] 删除 011 `--alias-shadow` 与 012 `--doc2query`/`--doc2query-build` 的 flag 注册与 options 字段(main.go)与专属文件 `alias_shadow.go`/`doc2query.go` + 测试 `alias_shadow_test.go`/`doc2query_test.go`;**同步修改引用它们的测试**:`eval_fixed_gold_oracle_test.go`(删 doc2query build/arm 配置行)、`eval_runner_test.go`(删 doc2query/alias-shadow 配置行)、`temporal_diagnostic_test.go`(若引用 doc2queryDiscardLogger 则保留该辅助或内联);确认 `multiquery.go` 中 `multiQueryArm` 无引用后一并删;`CGO_ENABLED=0 go test -count=1 ./...` 绿

## Phase 4 · 专属文件机制(每批删后 build+test)

- [ ] T006 [P] 删除 006 abstain 作答分支:`--abstain-prompt`/`--abstain-hard`/`--abstain-soft` 的 flag 注册与 options 字段(main.go)、runner.go 中 `answerPromptForRegime` 的 abstain 分支;**保留 `computeAbstainSignal` 函数**(被 abstain_probe.go 诊断引用,迁入 abstain_probe.go 或原地保留)、保留 `--abstain-probe`/`--abstain-gate`/`--abstain-probe-out` 诊断 flag;删 abstain.go 的作答相关代码;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T007 [P] 删除 029 nav 系:`--nav`/`--nav-max-steps`/`--nav-k`/`--nav-diagnose` 的 flag 注册与 options 字段(main.go)+ 专属文件 `agentic_nav.go`/`nav_http.go`/`nav_tools.go`/`nav_trajectory.go`/`nav_diagnose_cli.go` + 测试;确认 nav 无被保留路径引用后整删;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T008 [P] 删除 021 iris 系:`--iris`/`--iris-depth` 的 flag 注册与 options 字段(main.go)+ 专属文件 `iris.go` + 测试;确认无保留引用后整删;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T009 [P] 删除 027 gap-refetch 系:`--gap-refetch`/`--event-projection`/`--event-project`/`--build-event-project`/`--event-llm-*` 的 flag 注册与 options 字段(main.go)+ 专属文件 `event_projection.go` + 测试;`gap_retrieval.go` 若仅服务于 gap-refetch 则删,若有保留引用则保留相关函数;**保留 `renderStructuredGapQuery`/`stableGapCandidateUnion` 的决策依 research T1 定**;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T010 [P] 删除 027 temporal-resolution 的 `--temporal-resolution` flag 注册与 options 字段(main.go)与作答执行逻辑(temporal_resolution.go 中除 `classifyQueryMode` 外);**保留 `classifyQueryMode` 函数**(被 miss_attribution.go 与 eval_runner.go 引用);`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿
- [ ] T011 [P] 删除 L2 `--counter-refine` 与 038 变体 `--lme-typed-prompts` 的 flag 注册与 options 字段(main.go)、runner.go/eval_runner.go 分支、冲突表条目;确认无保留引用后删对应代码;`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` 绿

## Phase 5 · 030/031 整链 + trace 默认值移除(核心行为变更)

- [ ] T012 删除 030/031 整链文件:`trace_mediation.go`/`trace_gate.go`/`trace_http.go`/`consolidate.go`/`assembly.go`/`assembly_tokens.go`/`assembly_diagnose.go`/`entity_grouping.go`/`relation_graph.go` + 测试;同步删 main.go 的 `--trace-mediation`/`--consolidate`/`--evidence-assembly`/`--assembly-*`/`--relation-context` flag 注册与 options 字段、eval_runner.go 的 030/031 执行点(6 处)、unified_answer_contract_eval.go 冲突表条目;**保留 `kindOfEvidence`/`assembleEvidence`/`computeRelationContext` 若被非 030 路径引用的部分(已确认 022 chunk/unified 主渲染不依赖,可整删)**;`CGO_ENABLED=0 go test -count=1 ./...` 绿
- [ ] T013 **trace 默认值移除(单独 commit)**:确认 `--trace-mediation` 默认值从 true 移除后,默认路径从 trace 中介切换到 chunk 装配;运行 unified 配方(hybrid k30 + unified + chunk-quota 12,同 87.9% 锚)验证与锚同口径一致(无回归);commit 消息注明 Step A 依据(−3.44pp, McNemar p=1.4e-04);`CGO_ENABLED=0 go test -count=1 ./...` 绿

## Phase 6 · 042 协议删除

- [ ] T014 删除 042 协议:`--utility-stage`/`--utility-source`/`--utility-label-source`/`--utility-pilot-source`/`--utility-shallow-source`/`--utility-deep-source`/`--utility-shallow-k`/`--utility-deep-k` 的 flag 注册与 options 字段(main.go)+ 早期 dispatch 块;删除 `counterfactual_utility.go`/`counterfactual_utility_artifact.go`/`counterfactual_utility_calibration.go`/`counterfactual_utility_cli.go`/`counterfactual_utility_eval.go`/`counterfactual_utility_http.go`/`counterfactual_utility_run.go` 及 7 个测试文件;确认 `utilityExperimentalFlags`(引用 nav/iris/abstain/multi-query)随清;`CGO_ENABLED=0 go test -count=1 ./...` 绿

## Phase 7 · 收尾

- [ ] T015 result-matrix「过时/已证伪」表同步:标记已清机制为"已移除";[default-off-mechanism-cleanup-plan.md](../../../docs/evaluation/default-off-mechanism-cleanup-plan.md)同步删除已清项;README 机制说明同步
- [ ] T016 保留项核查:`--temporal-answer-prompt`(032)/`--temporal-date-scaffold`(017)/`--abstain-probe`/`--oracle`/`--rerank`/`--pcic`/`--pcic-meta`/`--compiler-arm`/`--representation`/`--recall-diagnostic`/`SearchMulti`/`classifyQueryMode`/`computeAbstainSignal` 均未被误删且行为不变
- [ ] T017 全量门:`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...`、`CGO_ENABLED=0 go vet ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空;确认已删 flag 不再出现在 `--help`;记录净删行数

## Dependencies(删除批次顺序)

```
T001(基线) → T002/T003(纯 flag 级)→ T004/T005(010/011/012)
  → T006-T011(专属文件机制,可并行)→ T012/T013(030/031+trace)
  → T014(042)→ T015-T017(收尾)
```

- T002-T011 各批相互独立(不同 flag/文件),可并行;但**共享文件**(main.go/eval_runner.go/unified_answer_contract_eval.go)的并发修改有冲突风险,建议单人顺序执行避免合并冲突。
- T012 依赖 T011 完成(030/031 与 L2/abstain 的冲突表条目都在 unified_answer_contract_eval.go)。
- T013(trace 默认值)依赖 T012(030/031 文件删除),且**单独 commit**(宪法 IV attribution)。
- T014(042)依赖 T002-T011(utilityExperimentalFlags 引用的 flag 已清,避免悬空引用)。

## Parallel execution examples

- T002/T003 同改 main.go 的 flag 区,不同 flag 组,可并行(建议顺序避免同文件冲突)。
- T006-T011 各改不同专属文件 + main.go 的不同 flag 段,可并行。
- T015/T016/T017 收尾可并行(文档/核查/全量门)。

## Implementation strategy

按"低风险 → 高风险"推进:纯 flag 级(T002/T003)→ 有交叉的 010/011/012(T004/T005)→ 专属文件(T006-T011)→ 核心行为变更 trace(T012/T013)→ 大块协议 042(T014)→ 收尾(T015-T017)。每批独立 commit、独立验证,任何一批 build/test 失败立即停下修,不回滚已完成批次。trace 默认值移除单独 commit 并注明 Step A 依据。
