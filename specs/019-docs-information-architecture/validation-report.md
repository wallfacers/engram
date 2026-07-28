# 验收报告：企业级文档信息架构

**Feature**: `019-docs-information-architecture`
**Branch**: `019-docs-information-architecture`
**Implementation baseline**: `c86e47e` (`docs(readme): 精简 License 节,去掉多余贡献条款声明`)
**Started**: 2026-07-28

## 实施起点

- 初始工作树状态：clean。
- 根 `README.md`、`README.zh-CN.md`、`LICENSE` 和 `CLAUDE.md` 属于已接受的并行工作，
  本 feature 只通过 12 个 relocated 页面保持其 docs 深链有效。
- Node.js：`v24.14.1`；后续 validator 只使用 Node.js 标准库。
- 不运行 LoCoMo、付费模型或联网检查。

## TDD 记录

| 组 | RED 证据 | GREEN 证据 |
|---|---|---|
| Metadata / headings | `node --test docs/validation/check-docs.test.mjs`：预期 `ERR_MODULE_NOT_FOUND`，因为 `check-docs.mjs` 尚未实现；1 test file / 1 fail | T004 后：3 tests / 3 pass；验证完整元数据、重复主题、单一 H1、层级和 slug |
| Links / navigation | `node --test docs/validation/check-docs.test.mjs`：metadata/headings 3 pass；新增 2 tests 因 `validateLinks` / `validateNavigation` 尚未实现而失败 | T006 后：5 tests / 5 pass。 |
| Retrieval / relocation | T007：原有 5 tests 继续通过；新增 2 tests 因 `validateRetrieval` / `validateRelocation` 尚未导出而失败。 | T008 后：7 tests / 7 pass；覆盖 Q1–Q8、迁移正文限制、归档条件字段、分数消费者和能力边界。 |
| 全仓链接扫描 | 复核发现中文路径在非 `-z` Git 输出下被漏掉，并发现 specs 的历史坏链。 | 改用 `git ls-files -z`，全仓 257 份 Markdown 链接扫描通过；新增 Unicode 路径与目录链接 fixture 后 9/9 tests 通过。 |

测试环境：Node `v24.14.1`；定向测试命令为 `node --test docs/validation/check-docs.test.mjs`。

## User Story 验收

### US1：唯一当前答案

`node docs/validation/check-docs.mjs --retrieval --relocation` 以 0 退出并输出
`Documentation validation passed.`。Q1–Q8 分别由 MCP、CLI、记忆架构、结果、实验裁决、
能力边界和论文方向的唯一 current owner 回答；Q6/Q7 的 proposed 页面只作为次级链接。

### US2：任务导航

`node docs/validation/check-docs.mjs --links --navigation` 以 0 退出。门户直接给出使用、
架构、评测运维、评测结论、产品、研究与唯一历史入口；所有 19 份 stable/active/proposed
页面都由 BFS 验证为两跳内可达且无孤儿。

### US3：历史证据与迁移

`rg -l '^status: relocated$' docs -g '*.md' | sort` 输出恰好 12 页，与迁移契约逐项一致；
`--relocation` 通过，验证 direct target、无链式跳转和单一业务链接。archive 索引已覆盖
决策、评测、计划、研究与设计，并且 `--links` 通过。

### US4：治理

`docs/CONTRIBUTING.md` 固化八字段、五种生命周期、六种能力状态和新增/更新/引用/归档/
删除/复核流程。Reviewer A 与 B 的 G1–G3 分类记录见 `reviews/governance-review-a.md` 与
`reviews/governance-review-b.md`：生命周期、目标路径与后续动作均为 3/3 一致。

## 删除门证明

下列目标均在删除前执行 Markdown 链接扫描，结果为 0；它们是设计草案而非唯一实测数据，
目标、验收和契约已分别进入正式 feature artifacts，故满足五项删除门。

| 候选 | 正式目标与验收覆盖 | 契约/实现覆盖 | 独有证据检查 | 删除前入链 | 处置 |
|---|---|---|---|---|---|
| `2026-07-19-bio-retrieval-locomo-design.md` | 003–016 的正式 specs 与当前评测/裁决页覆盖各方向 | 现行架构、实验裁决和归档设计保留实现/判定依据 | 仅含早期机制假设，无独有可复算结果 | 0 | 删除 |
| `2026-07-20-cli-ai-first-design.md` | `specs/004-cli-ai-first/spec.md` 覆盖命令与验收 | CLI contract、实现和当前 CLI 指南覆盖接口边界 | 仅含已交付设计说明，无独有实测证据 | 0 | 删除 |
| `2026-07-28-curation-lifecycle-side-table-cleanup-design.md` | `specs/018-curation-lifecycle-cleanup/spec.md` 覆盖用户目标与验收 | 018 contract/plan、记忆架构和 MCP 指南覆盖 curation 边界 | 仅含已落实设计推理，无独有评测证据 | 0 | 删除 |

扫描命令：`rg -l --glob '*.md' "\\]\\([^)]*<旧路径>" .`。三个路径的结果均为零；若未来
出现新的正式入链，应从 Git 历史恢复为 archive，而不是复用 deleted 路径。

## 独立检索复核

`reviews/retrieval-review-a.md` 与 `reviews/retrieval-review-b.md` 都从门户开始。两份记录的
Q1–Q8 首个正本、生命周期与结论为 8/8 一致；没有把 proposed、archived 或 relocated 文档
回答为当前已交付事实。

## 链接语义与范围审阅

- 独立审阅最初发现 CLI/MCP 示例命令与二进制不符、一个 specs 历史坏链，以及中文路径
  漏扫；已改为代码实际命令、修复 archive 链接，并以 `git ls-files -z` 覆盖全仓 Markdown。
- 审阅 `git diff c86e47e -- '*.md'`：12 个 legacy 链接文字均描述迁移，目标与
  `canonical_path` 对应；八份既有设计的正式 spec 入链均已改为 archive；019 的 spec 与
  plan 自引用已改为 archive；archive 的 `superseded_by` 均指向相符的当前正本或门户。
- 根 `README.md`、`README.zh-CN.md`、`LICENSE`、`CLAUDE.md` 相对 `c86e47e` 无改动；
  Go、迁移和配置路径亦为空。
- 范围外 specs 为允许的 007、009、010、011、012、014、015、016 设计链接修改，另有
  `specs/009.../research.md` 的 archive 修复和 `specs/002.../tasks.md` 的误写链接修复；
  两项最小例外已写入 archive 契约。019 自身 artifacts、报告、任务、quickstart 与 reviews
  属 feature 内工件。

## Disposition Manifest

### 顶层源文件（21）

| 源文件 | 最终处置 |
|---|---|
| `README.md` | 重写为文档门户 |
| `background-extraction-from-workhorse-agent.md` | relocated → `architecture/provenance.md` |
| `benchmark-expansion-plan.md` | archive/plans/benchmark-expansion-2026-07 |
| `capability-and-product-north-star.md` | 删除；结论并入 capabilities 与 roadmap |
| `cli.md` | relocated → `guides/cli.md` |
| `competitive-benchmarks.md` | relocated → `evaluation/competitors.md` |
| `local-model-eval-setup.md` | archive/plans/local-model-eval-stack-2026-07 |
| `locomo-e2e-eval-reproduction.md` | relocated → `operations/evaluation/locomo-runbook.md` |
| `locomo-score-levers.md` | relocated → `evaluation/experiment-verdicts.md` |
| `locomo-single-multihop-failure-diagnosis.md` | archive/evaluation/locomo-single-multihop-diagnosis-2026-07 |
| `mcp-server.md` | relocated → `guides/mcp-server.md` |
| `memory-architecture.md` | relocated → `architecture/memory-system.md` |
| `memory-freshness-and-retrieval-policy.md` | relocated → `product/backlog/memory-freshness.md` |
| `memory-strategy.md` | relocated → `product/roadmap.md` |
| `memos-inhouse-locomo-repro.md` | relocated → `evaluation/reports/memos-locomo-reproduction.md` |
| `paper-outline-eval-reliability.md` | archive/research/eval-reliability-outline-2026-07 |
| `remote-eval-box.md` | relocated → `operations/evaluation/remote-gpu-runbook.md` |
| `results-matrix-2026-07-26.md` | relocated → `evaluation/results.md` |
| `saas-habit-memory-design.md` | 删除；未实现探索并入 `product/explorations/habit-memory.md` |
| `synthius-mem-analysis.md` | archive/research/synthius-mem-analysis-2026-07 |
| `temporal-t4-design.md` | archive/evaluation/temporal-t4-analysis-2026-07 |

### 设计与允许修改的 specs

- 既有 Feature 007、009、010、011、012、014、015、016 设计均归档到
  `docs/archive/designs/`，各自带 `feature` 与 `outcome`；它们的 12 个正式链接改为
  archive 目标。
- 三个删除候选已按上表逐项删除；Feature 019 设计已归档为
  `2026-07-28-documentation-information-architecture-design.md`，`feature: "019"`、
  `outcome: implemented`。
- `git diff --name-status c86e47e` 对账结果只含 `docs/`、019 feature artifacts 与上述
  允许 specs 链接文件及两项记录在契约中的全仓链接修复。

## Success Criteria Evidence

| Criterion | 验收证据 | 结果 |
|---|---|---|
| SC-001 两跳可达 | `--navigation` 通过；门户任务入口人工复核 | 通过 |
| SC-002 Q1–Q8 唯一正本 | fixture、`--retrieval`、A/B 8/8 复核 | 通过 |
| SC-003 本地链接与锚点 | `--links` 通过；链接语义 diff 审阅 | 通过 |
| SC-004 现行元数据 | `--metadata --headings` 通过 | 通过 |
| SC-005 archive / relocated | archive 索引、12 页 `--relocation` 通过 | 通过 |
| SC-006 已知状态漂移清零 | quickstart drift 与结果副本扫描为空 | 通过 |
| SC-007 唯一结果矩阵 | 四个消费者回链与副本检测通过 | 通过 |
| SC-008 无状态误判 | fixture 和 A/B 复核均排除 proposed/archive/relocated | 通过 |
| SC-009 入链与删除证明 | 三份删除门、archive 证据与 manifest | 通过 |
| SC-010 范围隔离 | `git diff --check`、范围命令和 `CGO_ENABLED=0 go test -count=1 ./...` | 通过 |
| SC-011 治理分类一致 | governance A/B 3/3 一致 | 通过 |

## 最终汇总

- 当前 Git 追踪的 Markdown：49 份；生命周期分布为 stable 9、active 6、proposed 3、
  archived 19、relocated 12。
- 元数据 validator 已确认每份文档的 canonical topic 唯一；门户 BFS 的当前/提案孤儿数为 0。
- Constitution 复核：local-first/离线降级、adapter 边界、namespace 隔离、评测口径与
  诚实降级均由相应当前正本明确；本 feature 未改产品实现。
