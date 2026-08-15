# 默认关闭机制清理计划(Default-Off Mechanism Cleanup)

**Feature**: 044-default-off-cleanup | **状态**: ✅ 已清理完成(2026-08-16)

移除 `cmd/locomo-bench` 中已证伪(NO-GO / 零增量 / 被取代)的默认关闭实验机制及其专属实现文件与接线,保留已坐实能力(unified / 022 chunk / 032 tplan / 017 scaffold)与诊断工具。

## 清理范围与决策

### 第一类:已证伪 / 零增量,建议清理(✅ 全部移除)

| flag / 机制 | 归属 | 决策 | 044 commit |
|---|---|---|---|
| `--assoc` / `--assoc-depth` | 关联图检索 | 移除 | T002 |
| `--cluster-sweep` | 025 | 移除 | T002 |
| `--temporal-score` / `--temporal-hard-filter` | 013 | 移除 | T002 |
| `--conflict-resolution` / `--superseded-penalty` | 写侧冲突 | 移除 | T003 |
| `--filter-pool` | listwise 过滤 | 移除(filter.go 整删) | T003 |
| `--opinion-pass` | 意见抽取 | 移除 | T003 |
| `--multi-query` | 010 | 作答路径移除;`--mq-max-subqueries` 保留归 recall-diagnostic | T004 |
| `--alias-shadow` | 011 | 移除(alias_shadow.go 整删) | T005 |
| `--doc2query` / `--doc2query-build` | 012 | 移除(doc2query.go 整删) | T005 |
| `--confidence-deepen` 族 | 043 | 移除(已合并 master,confidence_deepen 6 文件) | 043-code |
| `--abstain-prompt` / abstain-hard / abstain-soft | 006 | 作答分支移除;`computeAbstainSignal` + `--abstain-probe` 保留 | T006 |
| `--nav` / `--nav-*` | 029 | 移除(nav 5 文件) | T007 |
| `--iris` / `--iris-depth` | 021 | 移除(iris.go) | T008 |
| `--gap-refetch` / `--event-projection` / `--event-project` / `--build-event-project` / `--event-llm-*` | 027 | 移除(event_projection/gap_retrieval/eventstore_eval + E0-E3 投影) | T009 |
| `--temporal-resolution` | 027 | 作答移除;`classifyQueryMode` 保留 | T010 |
| `--counter-refine` | L2 | 移除 | T011 |
| `--lme-typed-prompts` | 038 变体 | 移除 | T011 |
| `--trace-mediation` / `--consolidate` / `--evidence-assembly` / `--assembly-*` / `--relation-context` | 030/031 | 整链移除(9 文件);trace 默认值移除单独 commit(T013,Step A −3.44pp) | T012/T013 |
| `--utility-stage` 族 | 042 | 移除(counterfactual 13 文件) | T014 |

### 保留项(✅ 未误删)

| 项 | 归属 | 保留理由 |
|---|---|---|
| `--unified-answer-contract` / `--unified-typed-prompts` | 038 | 唯一允许 prompt |
| `--chunks` / `--chunk-quota` 等 | 022 | hybrid 配方必需 |
| `--temporal-answer-prompt` | 032 | default-off opt-in,弱栈坐实 |
| `--temporal-date-scaffold` | 017 | 022 协议使用 |
| `--abstain-probe` / `--abstain-gate` | 006 诊断 | 零成本诊断 |
| `--oracle`(arm)/ `--rerank` / `--pcic` / `--pcic-meta` | 诊断/待定 | 诊断或未证伪 |
| `--compiler-arm` / `--representation`(chunk_900/raw_turn_window/semantic_episode) | 023 | 未定论 |
| `--recall-diagnostic` / `--temporal-diagnostic` / `--mq-max-subqueries` | 诊断 | 检索诊断 |
| `SearchMulti` / `classifyQueryMode` / `computeAbstainSignal` | 引擎/诊断 | 保留函数 |

## 红线验证

- 引擎五目录(`memory/ embedding/ provider/ store/ internal/`)零改动 ✓
- 每批 `CGO_ENABLED=0 go build ./... && go test -count=1 ./...` 全绿 ✓
- 默认路径 byte-parity 保持 ✓
- 已删 flag 从 `--help` 消失 ✓
