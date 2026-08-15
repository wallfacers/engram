# Research: 044 默认关闭机制清理——清理范围与删除策略

**Phase 0 输出** | **日期**: 2026-08-16 | **输入**: spec.md + 代码勘察 + verdict 复核

## 1. 范围判定(逐机制核实的最终分类)

> ⚠️ **重要修正**:仅依赖 result-matrix 过时表做清单会误删。本表基于**逐个 verdict 复核**。

### 确认清理(第一类,verdict 为 NO-GO/零增量/被取代且无保留价值)

| flag | 机制/来源 | 判定依据 | 删除策略 |
|---|---|---|---|
| `--assoc` | 关联图检索 | 早期检索机制,无正 result | 删 flag + arm 路由 + 冲突表 |
| `--cluster-sweep` | 枚举聚类扫描 | 早期机制,无正 result | 同上 |
| `--temporal-score` | 013 时序检索 | 013 NO-GO | 同上 |
| `--temporal-hard-filter` | 013 | 013 NO-GO | 同上 |
| `--conflict-resolution` | 写侧冲突消解 | 早期,无正 result | 同上 |
| `--multi-query` | 010 | 010 NO-GO(cost-retrospective 明列) | 删 flag+multiquery.go 主体;`multiQueryArm` 被 alias_shadow 引用,随 011 一起处理 |
| `--filter-pool` | listwise 过滤 | 早期,无正 result | 删 flag + filter.go |
| `--opinion-pass` | 意见抽取 | 早期,无正 result | 删 flag + 分支 |
| `--write-dedup` | 024 | −0.91pp 单调降 | 删 flag + 分支 |
| `--neighbor-extend` | 024 | −0.46pp | 删 flag + 分支 |
| `--episode-cluster` | 025 | OVERALL −7.7pp | 删 flag + 分支 |
| `--relation-context` | 031 | 无显著独立增量 | **030/031 整链删**(relation_graph.go 等) |
| `--gap-refetch` + `--event-projection` | 027 | 021/027 temporal NO-GO | 删 flag + event_projection.go + gap_retrieval.go 相关 |
| `--temporal-resolution` | 027 | 027 NO-GO | **保留 `classifyQueryMode`**(被 miss_attribution 引用),删其余 |
| `--nav` 系 | 029 | −17.9pp 全负 | 删 agentic_nav.go + nav_*.go |
| `--iris` 系 | 021 | temporal 6 连 NO-GO | 删 iris.go |
| `--abstain-prompt`/`--abstain-hard`/`--abstain-soft` | 006 | NO-GO,拒答耗尽 | 删 flag + abstain.go 主体;`abstain_probe.go`(零成本诊断)**保留** |
| `--counter-refine` | L2 | −0.4pp 无正向 | 删 flag + 分支 |
| `--lme-typed-prompts` | 038 变体 | 被 unified 取代 | 删 flag + 分支 |
| `--trace-mediation` | 030 | Step A:unified 下 −3.44pp(p=1.4e-04)显著负 | **030/031 整链删 + trace 默认值移除(单独 commit)** |
| `--consolidate`/`--evidence-assembly` | 030 | trace 移除后无宿主 | 同上 |
| `--utility-stage` 族 | 042 | 维护者定关闭;归因存疑不重开 | **13 个 counterfactual 文件整删** |
| 011 `--alias-shadow` / 012 `--doc2query` | 011/012 | cost-retrospective 明列 NO-GO | 删 shadow 臂;但**先确认 recall-diagnostic 是否依赖**(待核查) |

### 保留(已坐实或 default-off opt-in,不清)

| flag | 机制 | 判定依据 |
|---|---|---|
| `--unified-answer-contract` | 038 | 双数据集 above-noise 坐实 |
| `--unified-typed-prompts` | 038 | 已验证无碰撞 |
| `--chunks`+`--chunk-quota` | 022 | hybrid 配方必需 |
| `--force-answer`/`--no-idk-retry` | 统一契约配套 | 隔离评分要求 |
| `--temporal-answer-prompt` | **032 tplan** | **default-off 保留,弱栈 opt-in(+11.2pp p=0.0001)** ← 修正自初判 |
| `--temporal-date-scaffold` | **017** | **022 正式协议路径使用**(eval_runner/eval_compile_bridge),非独立待清 |
| `--abstain-probe`/`--abstain-gate` | 006 诊断 | 零成本离线诊断工具 |
| `--oracle` | 全金证据 | 诊断-only |
| `--rerank` | 跨编码器 | 死亡规则禁云端;opt-in 诊断 |
| `--pcic`/`--pcic-meta` | 005 | 待定(无正 result 但未证伪) |
| `--compiler-arm`/`--representation` | 023 | 生产接线方向,未定论 |
| `--recall-diagnostic` 等诊断 | — | 检索诊断工具,保留 |
| bge-large embedder | 008 US3 | **唯一端到端转化的机制涨点(+1.3pp)** |

## 2. 删除策略三分(关键技术决策)

1. **整链删**(无保留引用):030/031(trace/assembly/consolidate/relation_graph/entity_grouping + 诊断)、042(13 文件)、029(nav 系)、021(iris)、040(已无文件)。
2. **删 flag 保留共享函数**:`temporal_resolution.go` 的 `classifyQueryMode`(被 miss_attribution 用)、`multiquery.go` 的 `multiQueryArm`(被 alias_shadow 用——若 011 也删则无依赖,可整删)。
3. **删专属文件**:abstain(保留 abstain_probe)、filter、event_projection、gap_retrieval(若无保留引用)。

## 3. 交叉引用(删除时必须同步改的共享文件)

| 共享文件 | 改动点 |
|---|---|
| `main.go` | 删 20+ flag 注册 + options 字段 + `answerRegimeFingerprint` 标记 + 042 dispatch |
| `eval_runner.go` | 删 arm 机制路由中已清机制 + 030/031/024/025 执行点(6 处) |
| `unified_answer_contract_eval.go` | 删冲突表已清 flag 条目(assoc/cluster-sweep/temporal-score/…) |
| `runner.go` | 删 abstain/counter-refine 分支;`temporalAnswerPrompt` 常量**保留**(032 tplan 是保留项) |
| `alias_shadow.go` / `doc2query.go` | 011/012 shadow 臂——待核查 recall-diagnostic 依赖后定 |
| `counterfactual_utility_cli.go` | 042 的 `utilityExperimentalFlags`(引用 nav/iris/abstain/multi-query)随 042 整删 |

## 4. 待核查项(已核实,决策敲定)

- **T1. 011 alias-shadow / 012 doc2query**:已核实为独立 shadow 比较臂(off/baseline/treatment 三态),**不依赖 recallDiagnostic**;但被 `eval_fixed_gold_oracle_test.go`/`eval_runner_test.go`/`temporal_diagnostic_test.go` 作为"非基准配置"锁定引用。**决策:整删 011/012 的 flag+作答路径+专属文件,同步改引用它们的测试(改为不再引用已删 shadow 配置)**;`SearchMulti`/`recallDiagnostic`(multiquery.go 内的独立诊断)**保留**。
- **T2. 014 temporal-answer-prompt 与 032**:032 verdict 明确"default-off 保留,弱栈 opt-in"。**`--temporal-answer-prompt` + `temporalAnswerPrompt` 常量保留,不作为 044 清理项**(从清理清单移除)。
- **T3. 017 temporal-date-scaffold**:已被 eval_runner/eval_compile_bridge 正式协议使用。**保留,不作为 044 清理项**。
- **T4. 040 adaptive-topk**:文件已不存在(040 已移除),plan 清单中 `adaptive_topk.go` 行**删除**(无需清理)。
- **T5. abstain_probe 保留**:`abstain_probe.go` 依赖 `computeAbstainSignal`(abstain.go)。**决策:删 abstain.go 的 flag+作答分支,`computeAbstainSignal` 迁入 abstain_probe.go 或保留该函数;`--abstain-probe` 诊断工具保留**。
- **T6. multiquery 边界**:`multiquery.go` 同时含 `--multi-query` 作答路径(删)与 `recallDiagnostic` 诊断(保留,含 `SearchMulti` 调用)。**决策:删 flag+作答分支,保留 recallDiagnostic + SearchMulti 引用**。

## 5. 决策记录

| Decision | Choice | Rationale | Alternatives |
|---|---|---|---|
| 032 tplan 去留 | **保留**(default-off opt-in) | verdict 明确弱栈 +11.2pp p=0.0001,非 NO-GO | 初判误列清理,已修正 |
| 017 scaffold 去留 | **保留** | 022 正式协议路径使用 | — |
| 030/031 整链删 | **整链删** | unified 主路径不依赖;Step A 证 trace 负 | 保留函数(被 assembly_diagnose 引用,但其也是 030 专属,一并删) |
| 042 整删 | **整删** | 维护者定关闭 | 保留为资产(被否) |
| 011/012 处理 | **待 T1 核查** | recall-diagnostic 依赖未明 | — |
