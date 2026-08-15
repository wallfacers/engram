# Contract: CLI 清理契约(044)

**Date**: 2026-08-16 | **冻结于实现之前**(宪法 III:契约先行)

## 1. 待删除 flag 清单(清理后 `--help` 不出现)

| flag | 归属 | 备注 |
|---|---|---|
| `--assoc` / `--assoc-depth` | 关联图检索 | — |
| `--cluster-sweep` | 025 | — |
| `--temporal-score` / `--temporal-hard-filter` | 013 | — |
| `--conflict-resolution` / `--superseded-penalty` | 写侧冲突 | — |
| `--filter-pool` | listwise 过滤 | — |
| `--opinion-pass` | 意见抽取 | — |
| `--write-dedup` / `--neighbor-extend` | 024 | — |
| `--episode-cluster` / `--cluster-min-keyword-jaccard` / `--cluster-embed-thresh` / `--cluster-max-evidence` | 025 | — |
| `--relation-context` | 031 | 030/031 整链 |
| `--gap-refetch` / `--event-projection` / `--event-project` / `--build-event-project` / `--event-llm-*` | 027 | — |
| `--temporal-resolution` | 027 | 保留 classifyQueryMode |
| `--nav` / `--nav-max-steps` / `--nav-k` / `--nav-diagnose` | 029 | — |
| `--iris` / `--iris-depth` | 021 | — |
| `--abstain-prompt` / `--abstain-hard` / `--abstain-soft` | 006 | 作答分支 |
| `--counter-refine` | L2 | — |
| `--lme-typed-prompts` | 038 变体 | — |
| `--trace-mediation` | 030 | **默认值切换,单独 commit** |
| `--consolidate` / `--evidence-assembly` / `--assembly-*` / `--consolidate-*` | 030 | 030/031 整链 |
| `--utility-stage` / `--utility-source` / `--utility-label-source` / `--utility-pilot-source` / `--utility-shallow-source` / `--utility-deep-source` / `--utility-shallow-k` / `--utility-deep-k` | 042 | 13 文件整删 |
| `--multi-query` / `--mq-max-subqueries` | 010 | 保留 recallDiagnostic/SearchMulti |
| `--alias-shadow` / `--doc2query` / `--doc2query-build` | 011/012 | 同步改引用测试 |

## 2. 保留 flag 清单(清理后行为不变)

| flag | 归属 | 保留理由 |
|---|---|---|
| `--unified-answer-contract` / `--unified-typed-prompts` | 038 | 唯一允许 prompt |
| `--chunks` / `--chunk-quota` / `--chunk-target-chars` / `--chunk-max-chars` | 022 | hybrid 配方必需 |
| `--force-answer` / `--no-idk-retry` | 统一契约配套 | 隔离评分要求 |
| `--temporal-answer-prompt` | 032 | default-off opt-in,弱栈坐实 |
| `--temporal-date-scaffold` | 017 | 022 协议使用 |
| `--abstain-probe` / `--abstain-gate` / `--abstain-probe-out` | 006 诊断 | 零成本诊断 |
| `--oracle` / `--rerank` / `--pcic` / `--pcic-meta` | 诊断/待定 | 诊断或未证伪 |
| `--compiler-arm` / `--representation` / `--planner-*` | 023 | 未定论 |
| `--recall-diagnostic` / `--temporal-diagnostic` | 诊断 | 检索诊断 |
| 基础 flag(`--top-k`/`--retrieval`/`--repeats`/`--concurrency`/…) | 基础 | 评测必需 |

## 3. 互斥与组合(清理后)

- 已删 flag 从 `validateUnifiedPromptPairExperiment` 冲突表、`supportedArmMechanisms`、`optionsForArm` 中移除。
- unified 契约冲突表保留:与仍在的机制(032/017/023/诊断)的互斥关系不变。
- `--confidence-deepen`(043)不在 master,无此 flag 需处理。

## 4. 默认值切换契约

- `--trace-mediation`:默认 true → **移除**;默认路径从"trace 中介"切换为"chunk 装配"。unified 配方(87.9% 锚)本就是 trace-off,切换后行为与锚一致。
- 其余已清 flag 均默认 false/off,移除后默认路径字节不变(byte-parity 断言证明)。
