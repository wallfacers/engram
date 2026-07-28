# Tasks: 企业级文档信息架构

**Input**: `specs/019-docs-information-architecture/` 下的 spec、plan、research、
data-model、contracts 和 quickstart

**Tests**: 本 feature 明确要求确定性结构门禁和独立语义复核；validator 必须分三组按
TDD 执行定向 RED → 实现 → GREEN。

**Organization**: 任务按四个 user story 组织。每个迁移任务都必须写入完整 front
matter、保持单一 H1、修复本任务引入的链接，并遵守
`contracts/document-metadata.md`。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 与同阶段其他标记任务编辑不同文件，可并行执行
- **[Story]**: 对应 `spec.md` 的 US1–US4
- 所有路径均相对仓库根目录

## Phase 1: Setup（共享准备）

**Purpose**: 固定实施基线和机器可读验收输入。

- [X] T001 在 `specs/019-docs-information-architecture/validation-report.md` 创建验收报告骨架，记录分支、`c86e47e` 基线、初始 `git status`、根 README 双语并行提交和 SC-001–SC-011 证据槽
- [X] T002 [P] 按 `specs/019-docs-information-architecture/contracts/navigation-and-retrieval.md` 的 Q1–Q8 原文、主题、路径、必需断言和禁止生命周期创建 `docs/validation/retrieval-fixtures.json`

---

## Phase 2: Foundational（阻塞前置）

**Purpose**: 在迁移正文前建立可重复、只读、无第三方依赖的文档契约门。

**⚠️ CRITICAL**: T008 完成前不得开始 user story 内容迁移。

- [X] T003 在 `docs/validation/check-docs.test.mjs` 先写 metadata/headings 的合规 fixture 与定向违规 fixture，运行测试并确认缺失/非法 front matter、重复 canonical topic、H1/层级/slug 规则处于 RED
- [X] T004 在 `docs/validation/check-docs.mjs` 实现受限 YAML front matter、生命周期/条件字段、全局主题唯一性、单一 H1、连续标题层级和 GitHub 风格 slug 检查，运行 T003 定向测试至 GREEN
- [X] T005 在 `docs/validation/check-docs.test.mjs` 追加 links/navigation 的合规 fixture 与坏文件、坏锚点、孤儿、超过两跳 fixture，确认原 metadata 组保持 GREEN 且新增组定向 RED
- [X] T006 在 `docs/validation/check-docs.mjs` 实现 tracked Markdown 本地文件/锚点扫描、入链/孤儿统计、从 `docs/README.md` 的 BFS 两跳检查和 changed-link 结构检查，运行 T005 定向测试至 GREEN
- [X] T007 在 `docs/validation/check-docs.test.mjs` 追加 retrieval/relocation 的合规 fixture 与错误 Q1–Q8、错误迁移映射/正文上限、archive 条件字段、分数消费者副本和存储能力缺失 fixture，确认前两组保持 GREEN 且新增组定向 RED
- [X] T008 在 `docs/validation/check-docs.mjs` 实现 `--metadata`、`--headings`、`--links`、`--navigation`、`--retrieval`、`--relocation` 和全量模式，读取 `docs/validation/retrieval-fixtures.json`，验证 12 个迁移映射、Q1–Q8、四个分数消费者和当前存储边界；运行全套测试至 GREEN，并把三组 RED/GREEN、Node 版本和测试数写入 `specs/019-docs-information-architecture/validation-report.md`

**Checkpoint**: validator 的三类失败与通过行为已分别锁定，可开始迁移。

---

## Phase 3: User Story 1 — 获取唯一可信的当前答案（Priority: P1）🎯 MVP

**Goal**: Q1–Q8 各自命中唯一当前正本，且不会把提案、归档或旧入口当作已交付事实。

**Independent Test**: 运行 `node docs/validation/check-docs.mjs --retrieval`，再从
`docs/README.md` 人工回答 Q1–Q8；要求 8/8 路径和结论符合固定 fixture。

### Implementation for User Story 1

- [X] T009 [P] [US1] 从 `docs/results-matrix-2026-07-26.md` 提炼唯一当前矩阵到 `docs/evaluation/results.md`，把 100 题先导完整证据写入 `docs/archive/evaluation/longmemeval-100-pilot-2026-07.md`，并把原路径重写为仅指向结果正本的 relocated 页
- [X] T010 [P] [US1] 按 R-007 和 003–018 正式证据创建 `docs/evaluation/experiment-verdicts.md`，将 `docs/locomo-score-levers.md` 全文冻结到 `docs/archive/evaluation/locomo-experiment-ledger-2026-07.md`，并把原路径重写为 relocated 页
- [X] T011 [P] [US1] 将已交付命令、离线边界和 LLM 依赖从 `docs/cli.md` 迁移到 `docs/guides/cli.md`，并把 `docs/cli.md` 重写为 relocated 页
- [X] T012 [P] [US1] 将 MCP 配置、工具、namespace 和 opt-in curation 契约从 `docs/mcp-server.md` 迁移到 `docs/guides/mcp-server.md`，并把 `docs/mcp-server.md` 重写为 relocated 页
- [X] T013 [P] [US1] 依据 `store/migrations.go`、`memory/entrystore.go`、`memory/retriever.go` 和原 `docs/memory-architecture.md` 创建 `docs/architecture/memory-system.md`，明确显式 ingest、直接 write/add、schema v6、检索降级和 curation=`shipped-opt-in`，并把原路径重写为 relocated 页
- [X] T014 [US1] 创建 `docs/product/capabilities.md`、`docs/product/backlog/memory-freshness.md` 和 `docs/product/explorations/habit-memory.md`，从 `docs/capability-and-product-north-star.md`、`docs/memory-freshness-and-retrieval-policy.md`、`docs/saas-habit-memory-design.md` 提炼当前/未实现边界，移除原始聊天和姓名，将 freshness 原路径改为指向 proposed 页的 relocated 页并删除 habit 旧路径
- [X] T015 [US1] 创建排除全部已收口 LoCoMo 杠杆的 `docs/product/roadmap.md`，把 `docs/memory-strategy.md` 全文冻结到 `docs/archive/decisions/memory-strategy-2026-07.md` 后将原路径改为 relocated 页，并在 capabilities/roadmap 均完成后删除 `docs/capability-and-product-north-star.md`
- [X] T016 [P] [US1] 将 prior-art 复核后的负结果史/共同失败机理/低成本止损方向写入 `docs/research/paper-direction.md`，把旧提纲冻结到 `docs/archive/research/eval-reliability-outline-2026-07.md`，删除 `docs/paper-outline-eval-reliability.md`
- [X] T017 [US1] 重写 `docs/README.md` 的当前答案入口和 AI 检索协议，直接链接 Q1–Q8 正本，明确生命周期过滤、事实优先级、Q6/Q7 次级证据和 archive 使用边界
- [X] T018 [US1] 运行 `node docs/validation/check-docs.mjs --retrieval`，逐题核对 Q1–Q8、当前存储断言和 proposed/archive/relocated 排除，并把 US1 独立验收结果写入 `specs/019-docs-information-architecture/validation-report.md`

**Checkpoint**: 当前能力、结果、实验裁决、CLI、MCP、记忆架构和论文方向均有唯一答案。

---

## Phase 4: User Story 2 — 按任务快速浏览文档（Priority: P2）

**Goal**: 新用户可按任务进入指南、架构、评测运维、结果、产品、研究和历史，现行/提案
文档距门户不超过两跳。

**Independent Test**: 运行 `node docs/validation/check-docs.mjs --navigation`，并从门户
分别查找 MCP/CLI、架构、LoCoMo runbook、远端 GPU、当前结果、产品路线、研究和历史。

### Implementation for User Story 2

- [X] T019 [P] [US2] 从 `docs/background-extraction-from-workhorse-agent.md` 提炼宿主无关的来源与边界到 `docs/architecture/provenance.md`，移除个人/宿主耦合叙述并把原路径重写为 relocated 页
- [X] T020 [P] [US2] 将 recipe、环境变量、复现步骤和已验证陷阱从 `docs/locomo-e2e-eval-reproduction.md` 迁移到 `docs/operations/evaluation/locomo-runbook.md`，并把原路径重写为 relocated 页
- [X] T021 [P] [US2] 将机器生命周期、模型服务、端口、产物持久化和停机纪律从 `docs/remote-eval-box.md` 迁移到 `docs/operations/evaluation/remote-gpu-runbook.md`，并把原路径重写为 relocated 页
- [X] T022 [P] [US2] 从 `docs/benchmark-expansion-plan.md` 提炼仍有效未来项到 `docs/evaluation/benchmark-roadmap.md`，将原计划和 `docs/local-model-eval-setup.md` 分别冻结到 `docs/archive/plans/benchmark-expansion-2026-07.md`、`docs/archive/plans/local-model-eval-stack-2026-07.md`，删除两个旧路径
- [X] T023 [US2] 在 T009 的结果正本完成后，将 `docs/competitive-benchmarks.md` 迁移为 `docs/evaluation/competitors.md`，保留口径和厂商自报边界、删除 engram 当前矩阵副本、回链 `docs/evaluation/results.md`，并把原路径重写为 relocated 页
- [X] T024 [P] [US2] 将 `docs/memos-inhouse-locomo-repro.md` 迁移到 `docs/evaluation/reports/memos-locomo-reproduction.md`，保留同栈方法与适用范围并把原路径重写为 relocated 页
- [X] T025 [US2] 扩充 `docs/README.md` 为完整按任务门户，并创建阶段性 `docs/archive/README.md`；列出每份现行/提案文档的一行摘要、状态和权威主题，archive 只保留单一历史入口
- [X] T026 [US2] 运行 `node docs/validation/check-docs.mjs --navigation`，人工执行八类任务导航，确认所有 stable/active/proposed 文档两跳可达且 relocated 不计入路径，并把 US2 结果写入 `specs/019-docs-information-architecture/validation-report.md`

**Checkpoint**: 当前目录按读者任务组织，门户导航独立通过。

---

## Phase 5: User Story 3 — 审计历史结论与证据（Priority: P3）

**Goal**: 负结果、旧决策、诊断和设计依据可追溯，但不污染当前状态回答。

**Independent Test**: 从 `docs/evaluation/experiment-verdicts.md` 追溯 temporal T-4 和任一
保留设计，运行 `--links --relocation`，并确认三个删除目标逐文件通过五项删除门。

### Implementation for User Story 3

- [X] T027 [P] [US3] 将 `docs/locomo-single-multihop-failure-diagnosis.md` 冻结到 `docs/archive/evaluation/locomo-single-multihop-diagnosis-2026-07.md`，补历史警告/outcome/evidence 入链后删除旧路径
- [X] T028 [P] [US3] 将 `docs/temporal-t4-design.md` 冻结到 `docs/archive/evaluation/temporal-t4-analysis-2026-07.md`，标记 `closed-no-go`，核对 T010 已从 `docs/evaluation/experiment-verdicts.md` 回链该目标后删除旧路径
- [X] T029 [P] [US3] 将 `docs/synthius-mem-analysis.md` 冻结到 `docs/archive/research/synthius-mem-analysis-2026-07.md`，补适用时间、outcome 和现行替代入口后删除旧路径
- [X] T030 [P] [US3] 将 `docs/superpowers/specs/2026-07-21-judge-口径-alignment-design.md`、`docs/superpowers/specs/2026-07-22-retrieval-ranking-attribution-gate-design.md`、`docs/superpowers/specs/2026-07-23-multi-query-retrieval-design.md` 分别移动到 `docs/archive/designs/2026-07-21-judge-口径-alignment-design.md`、`docs/archive/designs/2026-07-22-retrieval-ranking-attribution-gate-design.md`、`docs/archive/designs/2026-07-23-multi-query-retrieval-design.md`，补 feature/outcome 元数据并更新 `specs/007-judge-metric-alignment/spec.md`、`specs/009-retrieval-attribution-gate/spec.md`、`specs/010-multi-query-retrieval/spec.md` 链接
- [X] T031 [P] [US3] 将 `docs/superpowers/specs/2026-07-24-write-side-alias-embedding-design.md`、`docs/superpowers/specs/2026-07-24-doc2query-pseudo-query-shadow-design.md`、`docs/superpowers/specs/2026-07-24-answer-side-temporal-reasoning-contract-design.md` 分别移动到 `docs/archive/designs/2026-07-24-write-side-alias-embedding-design.md`、`docs/archive/designs/2026-07-24-doc2query-pseudo-query-shadow-design.md`、`docs/archive/designs/2026-07-24-answer-side-temporal-reasoning-contract-design.md`，补 feature/outcome 元数据并更新 `specs/011-dual-index-alias/spec.md`、`specs/012-doc2query-shadow/spec.md`、`specs/014-temporal-answer-contract/spec.md` 链接
- [X] T032 [P] [US3] 将 `docs/superpowers/specs/2026-07-25-offline-consolidation-bridging-design.md`、`docs/superpowers/specs/2026-07-26-longmemeval-subset-design.md` 分别移动到 `docs/archive/designs/2026-07-25-offline-consolidation-bridging-design.md`、`docs/archive/designs/2026-07-26-longmemeval-subset-design.md`，补 feature/outcome 元数据并更新 `specs/015-consolidation-bridging/spec.md`、`specs/015-consolidation-bridging/plan.md`、`specs/015-consolidation-bridging/research.md`、`specs/016-longmemeval-crossbench/spec.md`、`specs/016-longmemeval-crossbench/plan.md`、`specs/016-longmemeval-crossbench/research.md` 链接
- [X] T033 [US3] 对 `docs/superpowers/specs/2026-07-19-bio-retrieval-locomo-design.md` 单独核验正式目标/验收/契约覆盖、无独有证据和删除前零入链，在 `specs/019-docs-information-architecture/validation-report.md` 留下五项证明后删除；任一门失败则改归档并记录理由
- [X] T034 [US3] 对 `docs/superpowers/specs/2026-07-20-cli-ai-first-design.md` 单独核验正式目标/验收/契约覆盖、无独有证据和删除前零入链，在 `specs/019-docs-information-architecture/validation-report.md` 留下五项证明后删除；任一门失败则改归档并记录理由
- [X] T035 [US3] 对 `docs/superpowers/specs/2026-07-28-curation-lifecycle-side-table-cleanup-design.md` 单独核验正式目标/验收/契约覆盖、无独有证据和删除前零入链，在 `specs/019-docs-information-architecture/validation-report.md` 留下五项证明后删除；任一门失败则改归档并记录理由
- [X] T036 [US3] 运行 `rg -l '^status: relocated$' docs -g '*.md' | sort` 并用 `node docs/validation/check-docs.mjs --relocation` 与 `specs/019-docs-information-architecture/contracts/archive-and-relocation.md` 第 6 节逐项对账，证明恰好 12 页、映射一致、无 relocation 链且每页只有一个业务链接
- [X] T037 [US3] 完整更新 `docs/archive/README.md` 的决策/评测/计划/研究/设计索引和所有现行 evidence 回链，运行 `node docs/validation/check-docs.mjs --links --relocation`，把 archive/删除/迁移独立验收写入 `specs/019-docs-information-architecture/validation-report.md`

**Checkpoint**: 历史证据完整可达，删除逐项有证明，当前查询不会命中历史正文。

---

## Phase 6: User Story 4 — 持续维护而不重新漂移（Priority: P4）

**Goal**: 新增、更新、引用、归档、删除和人工复核都有一致规则，两个独立过程能对 G1–G3
给出完全相同的分类。

**Independent Test**: 只阅读 `docs/CONTRIBUTING.md` 分类 G1–G3，并运行
`node docs/validation/check-docs.mjs --metadata --headings`。

### Implementation for User Story 4

- [X] T038 [P] [US4] 创建 `docs/CONTRIBUTING.md`，固化单一职责、八字段元数据、五种生命周期、六种能力状态、新增/更新/引用/归档/删除/复核决策树、分数/verdict 单一正本规则和 G1–G3 固定样例
- [X] T039 [US4] 审计并规范化全部 `docs/**/*.md` 的 front matter、唯一 H1、连续层级、唯一 slug、中文正文、可见权威边界、archive 警告、relocated 内容上限和至少一个导航/evidence 入链
- [X] T040 [US4] 在 `docs/product/capabilities.md`、`docs/product/roadmap.md`、`docs/evaluation/competitors.md`、`docs/research/paper-direction.md` 强制回链 `docs/evaluation/results.md`，删除完整当前 score tuple/矩阵副本并保留必要口径说明
- [X] T041 [P] [US4] 以全新上下文只阅读 `docs/CONTRIBUTING.md`，独立分类 G1–G3，将 Reviewer A 的生命周期、目标路径和后续动作写入 `specs/019-docs-information-architecture/reviews/governance-review-a.md`
- [X] T042 [P] [US4] 以全新上下文且不读取 `specs/019-docs-information-architecture/reviews/governance-review-a.md`，只阅读 `docs/CONTRIBUTING.md` 独立分类 G1–G3，将 Reviewer B 结果写入 `specs/019-docs-information-architecture/reviews/governance-review-b.md`
- [X] T043 [US4] 比较 `specs/019-docs-information-architecture/reviews/governance-review-a.md` 与 `governance-review-b.md`，要求生命周期、目标路径和动作 3/3 一致，再把逐项比较写入 `specs/019-docs-information-architecture/validation-report.md`
- [X] T044 [US4] 运行 `node docs/validation/check-docs.mjs --metadata --headings`，确认元数据/标题错误为 0 且 T043 通过，把 US4 独立验收结果写入 `specs/019-docs-information-architecture/validation-report.md`

**Checkpoint**: 治理规范和分类一致性独立通过。

---

## Phase 7: Polish & Cross-Cutting Concerns

**Purpose**: 019 设计最终归档、全仓验收、语义复核和变更隔离。

- [X] T045 [P] 以全新上下文只从 `docs/README.md` 开始回答 Q1–Q8，将 Reviewer A 的首个正本、生命周期、结论和 evidence 写入 `specs/019-docs-information-architecture/reviews/retrieval-review-a.md`
- [X] T046 [P] 以全新上下文且不读取 `specs/019-docs-information-architecture/reviews/retrieval-review-a.md`，只从 `docs/README.md` 开始回答 Q1–Q8，将 Reviewer B 结果写入 `specs/019-docs-information-architecture/reviews/retrieval-review-b.md`
- [X] T047 比较 `specs/019-docs-information-architecture/reviews/retrieval-review-a.md` 与 `retrieval-review-b.md`，要求首个正本、生命周期和结论 8/8 一致，再把逐项比较写入 `specs/019-docs-information-architecture/validation-report.md`
- [X] T048 在 US1–US4 checkpoint 全部通过后，将 `docs/superpowers/specs/2026-07-28-documentation-information-architecture-design.md` 移到 `docs/archive/designs/2026-07-28-documentation-information-architecture-design.md`，设置 feature=`019`/outcome=`implemented`，更新 `specs/019-docs-information-architecture/spec.md`、`specs/019-docs-information-architecture/plan.md` 自引用和 `docs/archive/README.md`
- [X] T049 运行 `node --test docs/validation/check-docs.test.mjs`、`node docs/validation/check-docs.mjs` 和 `specs/019-docs-information-architecture/quickstart.md` 的过期事实/分数副本扫描，将完整输出写入 `specs/019-docs-information-architecture/validation-report.md`
- [X] T050 审阅 `git diff c86e47e -- '*.md'` 中每个变更链接的文字与目标职责，确认 12 个 legacy 映射、八份既有设计入链、019 自引用、archive superseded_by 和根 `README.md`/`README.zh-CN.md` 兼容链接语义正确，并记录到 `specs/019-docs-information-architecture/validation-report.md`
- [X] T051 运行 `git diff --check` 和 `specs/019-docs-information-architecture/quickstart.md` 的范围隔离命令，确认根 `README.md`、`README.zh-CN.md`、`LICENSE`、`CLAUDE.md`、Go/迁移/配置相对 `c86e47e` 不变，既有 specs 仅发生白名单设计链接或记录在契约中的最小全仓链接修复，并记录结果
- [X] T052 运行 `CGO_ENABLED=0 go test -count=1 ./...`，确认纯文档重组未改变产品行为并把包级结果写入 `specs/019-docs-information-architecture/validation-report.md`
- [X] T053 在 `specs/019-docs-information-architecture/validation-report.md` 建立 disposition manifest，逐项登记 21 个顶层源文件、恰好 12 个 relocated、八个既有设计归档、三个删除/转归档结论、019 设计归档和全部允许修改的 specs，并与 `git diff --name-status c86e47e` 对账
- [X] T054 完成 `specs/019-docs-information-architecture/validation-report.md` 的 SC-001–SC-011 逐项证据映射、最终文件/生命周期/主题/孤儿计数和 Constitution 五原则复核，确认全部门通过后再勾选完成

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: 可立即开始；T001 与 T002 可并行。
- **Phase 2 Foundational**: 依赖 T002；严格按 T003 RED → T004 GREEN → T005 RED →
  T006 GREEN → T007 RED → T008 GREEN 执行，T008 阻塞全部内容迁移。
- **US1 (Phase 3)**: 依赖 T008；T009–T013、T016 可并行，T014 → T015 串行，
  T017 依赖 T009–T016，T018 最后验收。
- **US2 (Phase 4)**: 依赖 T008；T019–T022、T024 可并行；T023 另依赖 T009 的结果
  正本，T025 依赖全部 US1/US2 目标，T026 最后验收。
- **US3 (Phase 5)**: 依赖 T008；T027–T032 可按文件并行；T033 → T034 → T035 因共享
  validation report 串行，T036 依赖所有 relocated 任务，T037 依赖此前全部 archive
  产物。
- **US4 (Phase 6)**: T038 可在 T008 后独立开始；T039–T040 等待全部内容/归档迁移；
  T041/T042 依赖 T038 且相互隔离，T043 比较二者，T044 最后验收。
- **Polish (Phase 7)**: 依赖 US1–US4 全部 checkpoint；T045/T046 相互隔离且可并行，
  T047 比较二者；T048 后再按 T049 → T050 → T051 → T052 → T053 → T054 完成。

### User Story Dependencies

```text
Foundation
  ├─ US1 当前正本 ──┬─ US2 完整导航 ──┐
  │                 └─ US3 证据回链 ──┼─ US4 全集治理 ── Polish
  └─ US4 CONTRIBUTING（可提前）────────┘
```

- **US1** 是建议 MVP，独立交付 Q1–Q8 唯一当前答案。
- **US2** 的内容迁移可与 US1 并行，但最终门户依赖 US1 正本路径稳定。
- **US3** 的历史文件迁移可与 US1/US2 并行，但最终 archive 索引依赖两者产生的历史件。
- **US4** 的规范正文可提前，全集元数据和分类验收必须等待全部迁移。

### Within Each User Story

- 新目标先完整写入并核对，再把来源改为 archive、relocated 或删除。
- 仍有效结论先进入当前正本，完整旧正文随后冻结。
- 删除任务必须先留五项证据和零入链输出；任一候选失败只改变该候选处置。
- Reviewer A/B 不读取对方输出，也不并行编辑 validation report；compare 任务统一落盘。
- 每个 story 的独立门通过后才能宣称该 story 完成。

## Parallel Opportunities

### Setup

```text
T001 validation-report 骨架
T002 retrieval-fixtures.json
```

### User Story 1

```text
T009 当前结果/100 题归档
T010 003–018 verdict/ledger
T011 CLI 指南
T012 MCP 指南
T013 记忆架构
T016 论文方向/旧提纲
```

### User Story 2

```text
T019 provenance
T020 LoCoMo runbook
T021 remote GPU runbook
T022 benchmark roadmap/计划归档
T024 MemOS reproduction
```

### User Story 3

```text
T027 single/multihop 诊断
T028 temporal T-4
T029 Synthius 研究
T030 designs 007/009/010
T031 designs 011/012/014
T032 designs 015/016
```

### Independent Reviews

```text
T041 governance Reviewer A
T042 governance Reviewer B
T045 retrieval Reviewer A
T046 retrieval Reviewer B
```

## Implementation Strategy

### MVP First（US1）

1. 完成 Setup 与三段 validator Foundation。
2. 并行生成 Q1–Q8 所需正本，随后处理其旧来源。
3. 重写最小门户并运行 retrieval fixture。
4. 在继续全目录迁移前确认 8/8 唯一答案。

### Incremental Delivery

1. **Foundation**：每类契约违规先有定向 RED，再有 GREEN。
2. **US1**：当前事实唯一且状态正确。
3. **US2**：所有当前任务两跳可达。
4. **US3**：历史可审计，当前语料不受污染。
5. **US4**：治理可重复，两个独立过程分类一致。
6. **Polish**：019 最终归档，全仓链接、范围隔离、Go 基线和 Constitution 全部留证。

## Notes

- `[P]` 只表示文件级独立；并行执行者仍须在开始和结束时检查 `git status`。
- 不编辑根 README、README.zh-CN、LICENSE、CLAUDE、产品代码、测试、迁移或配置。
- 不运行 LoCoMo、付费模型或联网检查。
- 既有 specs 只允许修改 contracts 列出的历史设计链接。
- 任何目标文件出现并行重叠时停止该文件并报告，不得 reset、覆盖或回滚。
