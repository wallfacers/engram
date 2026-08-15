# Data Model: 默认关闭机制清理

**Date**: 2026-08-16 | **Source**: spec.md + research.md(清理范围勘察)

本 feature 是**代码清理**,不引入任何新实体、不改变任何存储 schema。本文件定义"删除契约"——即清理前后 CLI 契约的变化清单,供实现与评审对照。

## 1. 待删除的 CLI flag(清理后必须从 `--help` 消失)

| flag 族 | 归属 | 删除方式 |
|---|---|---|
| `--assoc` / `--assoc-depth` | 关联图检索 | flag + arm 路由 + 冲突表 |
| `--cluster-sweep` | 枚举聚类扫描 | flag + arm 路由 + 冲突表 |
| `--temporal-score` / `--temporal-hard-filter` | 013 | flag + arm 路由 + 冲突表 |
| `--conflict-resolution` / `--superseded-penalty` | 写侧冲突 | flag + 分支 |
| `--filter-pool` | listwise 过滤 | flag + filter.go |
| `--opinion-pass` | 意见抽取 | flag + 分支 |
| `--write-dedup` / `--neighbor-extend` | 024 | flag + 分支 |
| `--episode-cluster` (+ 其 3 个 cluster-* 子 flag) | 025 | flag + 分支 |
| `--relation-context` | 031 | flag + 030/031 整链 |
| `--gap-refetch` / `--event-projection` (+ event 侧 flag) | 027 | flag + event_projection.go |
| `--temporal-resolution` | 027 | flag + 除 classifyQueryMode 外逻辑 |
| `--nav` / `--nav-max-steps` / `--nav-k` / `--nav-diagnose` | 029 | flag + nav 系文件 |
| `--iris` / `--iris-depth` | 021 | flag + iris.go |
| `--abstain-prompt` / `--abstain-hard` / `--abstain-soft` | 006 | flag + abstain 作答分支 |
| `--counter-refine` | L2 | flag + 分支 |
| `--lme-typed-prompts` | 038 变体 | flag + 分支 |
| `--trace-mediation` | 030 | flag + trace 系文件(**默认值切换,单独 commit**) |
| `--consolidate` / `--evidence-assembly` (+ assembly 子 flag) | 030 | flag + assembly 系文件 |
| `--utility-stage` 族(`--utility-source`/`--utility-label-source`/`--utility-pilot-source`/`--utility-shallow-*`/`--utility-deep-*`) | 042 | flag + 13 个 counterfactual 文件 |
| `--multi-query` / `--mq-max-subqueries` | 010 | flag + multiquery.go 作答路径 |
| `--alias-shadow` / `--doc2query` / `--doc2query-build` | 011/012 | flag + alias_shadow.go/doc2query.go + 改引用测试 |

## 2. 保留的 CLI flag(清理后必须保持不变)

| flag | 归属 | 保留理由 |
|---|---|---|
| `--unified-answer-contract` | 038 | 唯一允许 prompt,双数据集坐实 |
| `--unified-typed-prompts` | 038 | 已验证无碰撞 |
| `--chunks` / `--chunk-quota` / `--chunk-target-chars` / `--chunk-max-chars` | 022 | hybrid 配方必需 |
| `--force-answer` / `--no-idk-retry` | 统一契约配套 | 隔离评分要求 |
| `--temporal-answer-prompt` | **032** | **default-off opt-in,弱栈坐实** |
| `--temporal-date-scaffold` | **017** | **022 正式协议使用** |
| `--abstain-probe` / `--abstain-gate` / `--abstain-probe-out` | 006 诊断 | 零成本离线诊断 |
| `--oracle` | 诊断 | 全金证据天花板 |
| `--rerank` | 诊断 | opt-in,死亡规则禁云端但本地诊断允许 |
| `--pcic` / `--pcic-meta` | 005 | 待定未证伪 |
| `--compiler-arm` / `--representation` / `--planner-*` | 023 | 生产接线方向未定论 |
| `--recall-diagnostic` / `--temporal-diagnostic` / `--nav-diagnose`(如保留) | 诊断 | 检索诊断工具 |
| `--top-k` / `--retrieval` / `--repeats` / `--concurrency` 等基础 flag | 基础 | 评测必需 |

## 3. 保留的代码能力(清理后必须保持可编译可测)

- `SearchMulti`(引擎方法):被 recallDiagnostic 诊断使用,保留。
- `recallDiagnostic` 诊断逻辑(multiquery.go 内):保留。
- `classifyQueryMode`(temporal_resolution.go):被 miss_attribution 引用,保留。
- `computeAbstainSignal`(abstain.go):被 abstain_probe 诊断使用,保留(迁移或原地保留)。
- `temporalAnswerPrompt` 常量(runner.go):032 tplan 保留,不动。
- `SearchWithDiagnostics` / 混合检索 / FTS5 等引擎能力:全部不动。

## 4. 删除的代码文件(不可重建的验证资产说明)

- **042 协议(13 文件)**:counterfactual utility 协议——方向关闭,归因存疑但不重开;删除后如需复验须从 git 历史恢复。
- **030/031(assembly/trace/relation 系)**:读侧装配与关联——unified 下 Step A 证 trace 负;删除后如需恢复从 git 历史。
- **029/021/027/006 作答分支**:导航/IRIS/缺口 refetch/拒答——均已证伪,删除后不恢复。
- **011/012 shadow 臂**:alias-shadow/doc2query——NO-GO 研究臂;`SearchMulti` 保留。

## 5. 实体关系(无新实体,仅删除关系)

```
已清 flag ──删除──▶ main.go 注册 / options 字段 / answerRegimeFingerprint 标记
已清机制 ──删除──▶ eval_runner.go arm 路由 / 执行点 / unified_answer_contract_eval.go 冲突表
保留能力(SearchMulti/recallDiagnostic/classifyQueryMode) ──不动──▶ 引擎公开 API
```
