# Tasks: engram Agent Skill

**Input**: Design documents from `/specs/020-engram-agent-skill/`

**Prerequisites**: [plan.md](./plan.md), [spec.md](./spec.md),
[research.md](./research.md), [data-model.md](./data-model.md),
[contracts/](./contracts/), [quickstart.md](./quickstart.md)

**Tests**: 本 feature 明确要求 TDD、安装矩阵、运行时契约测试、行为评估和三个真实客户端
smoke。每个 story 的测试任务必须先执行并观察预期失败，再开始对应实现。

**Organization**: 任务按四个用户故事组织。所有 scratch、隔离 HOME、npm cache、eval
workspace 和 client transcript 必须写入 session scratchpad，不得写入 repo root、真实 home
或系统 `/tmp`。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可在依赖满足后与同组任务并行，且修改不同文件
- **[Story]**: `[US1]`–`[US4]` 对应 spec 中的用户故事
- 每个任务都给出精确文件路径

---

## Phase 1: Setup（共享实施基线）

**Purpose**: 冻结当前工作区、并行 feature 与工具链事实，防止安装测试污染真实环境。

- [X] T001 重新检查 `git status`、`git log --oneline -15`、`git worktree list` 和 sibling worktree 重叠面，并把当前 commit、已有 020 变更及 `git diff --name-only -- memory embedding provider store internal` 空基线写入 `specs/020-engram-agent-skill/validation-report.md`
- [X] T002 核对 Go、Node、npm、Git、Claude Code、Codex、OpenCode 版本和可用性，登记 session scratchpad 绝对路径，并为需要 agent inference 的 runner/client 记录 `local`、`existing-flat-rate`、`metered` 或 `unknown` cost class；后两者记为 blocker，文件为 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: 工作区归属、engine 零 diff、client/toolchain 基线和 scratch 边界均有记录。

---

## Phase 2: Foundational（阻塞所有用户故事）

**Purpose**: 先建立可测试的开放格式 validator 和最小 canonical package 骨架；用户故事只在
这一层通过后开始。

**⚠️ CRITICAL**: T003 的失败测试必须先于 T004；canonical package 只能存在
`skills/engram/` 一份。

- [X] T003 编写并运行预期失败的 Node 单元测试，使用隔离 fixture 覆盖两字段 frontmatter、包含 `stats` 的必需 description 语义、目录逃逸/缺失引用、manifest/eval schema、secret-shaped fixture、`engram-body-token-estimate-v1` 边界及 `engram-package-sha256-v1` 的排序/换行/root-symlink/internal-symlink/extra-file golden cases，文件为 `scripts/validate-agent-skill.test.mjs`
- [X] T004 实现仅用 Node.js 标准库的 validator 核心、fixture root 参数、`engram-body-token-estimate-v1`、`engram-package-sha256-v1` 和清晰错误输出，使 T003 通过并导出供安装 runner 复用的 identity helpers，文件为 `scripts/validate-agent-skill.mjs`
- [X] T005 创建可被三端发现的最小两字段 frontmatter 与正文骨架，确保 description 保留含 `stats` 的全部强制语义，并加入经 LF 规范化后与根许可证相同的 standalone license，文件为 `skills/engram/SKILL.md` 和 `skills/engram/LICENSE`
- [X] T006 [P] 创建 `0.1.0`/schema v1 的 manifest shell 及一层 reference 占位骨架，capability arrays 暂留空以供 US2 contract tests 先失败，文件为 `skills/engram/references/contract.json`、`skills/engram/references/mcp.md`、`skills/engram/references/cli.md`、`skills/engram/references/install.md`
- [X] T007 [P] 创建合法但尚无用例的 eval 骨架，文件为 `skills/engram/evals/evals.json` 和 `skills/engram/evals/trigger-evals.json`
- [X] T008 运行 `node --test scripts/validate-agent-skill.test.mjs`，确认单元测试通过且 canonical source validation 因尚未完成的 story 内容诚实失败，并把输出和待关闭项写入 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: Validator 基础可信；唯一 package 可被发现；未完成能力没有被误报为 release-ready。

---

## Phase 3: User Story 1 — 一条命令安装并发现技能（Priority: P1）🎯 MVP

**Goal**: 从一个 package 用一条命令安装到 Claude Code、Codex、OpenCode 的 project/user
scope，并验证每端只发现一个同版本 `engram`；同名目标在写入前得到路径告知与确认。

**Independent Test**: 在隔离环境分别完成 3 clients × 2 scopes，以及一次三端组合安装；
验证实际发现路径、版本/`engram-package-sha256-v1` digest、重复安装、取消覆盖、
copy/symlink fallback 和显式调用。

### Tests for User Story 1

> **NOTE**: T009/T010 必须先运行，并在 T011–T014 前因安装说明/入口尚未完成而失败。

- [X] T009 [P] [US1] 扩展失败测试，覆盖固定 `skills@1.5.20`、由 skill version 派生的 `engram-skill-v<version>` immutable release tag、commit-SHA 自引用拒绝、三端 agent flags、global/project/single-client 形态、标准目标路径预警、默认无 trailing `-y`、README/docs 命令同步和 `<ENGRAM_SKILL_TAG>` release gate，文件为 `scripts/validate-agent-skill.test.mjs`
- [X] T010 [P] [US1] 编写并先运行隔离安装矩阵 runner，复用 `engram-package-sha256-v1` 覆盖 3 clients × 2 scopes、三端组合、copy/symlink、同版本重跑、未知同名取消、明确替换、模拟中断恢复，并隔离 HOME/XDG/npm/temp/Claude/Codex/GH 配置，文件为 `scripts/test-agent-skill-install.mjs`

### Implementation for User Story 1

- [X] T011 [US1] 完成 pinned remote/local/manual 安装正本，写明预声明 tag→写入 literal→冻结候选→授权发布→tag/commit/digest 核验顺序、user/project/single-client 命令、实际发现目录、路径覆盖警告、安装方式、reload/invocation、升级恢复、无 Node/离线 fallback 和“只装 skill、不装二进制/不改 MCP config”，文件为 `skills/engram/references/install.md`
- [X] T012 [P] [US1] 重新核对 sibling README 意图后，在英文用户入口加入同一 canonical quick command、三个正式支持客户端、替换目标警告及 CLI/MCP 独立依赖说明，文件为 `README.md`
- [X] T013 [P] [US1] 在中文用户入口同步与英文入口语义和命令完全一致的 skill 安装区，文件为 `README.zh-CN.md`
- [X] T014 [P] [US1] 在文档门户加入同一 canonical quick command 并回链唯一安装正本，不新增第二份完整安装手册，文件为 `docs/README.md`
- [X] T015 [US1] 运行 `scripts/test-agent-skill-install.mjs` 的全部 local-source matrix，核对真实 home/repo/MCP config mutation 为 0，并把每个 case、最终 version、digest algorithm/digest 和任何失败写入 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T016 [US1] 仅在 T002 已证明 `local` 或 `existing-flat-rate` 时，在 session scratchpad 隔离 profile 中用当前 stable Claude Code、Codex、OpenCode 分别验证 project/user discovery 和显式调用，再验证一次三端组合；把 client version、实际路径、发现数量、调用语法、cost class、incremental cost `0` 和 blocker 写入 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: US1 可独立证明本地 package 的安装和发现；若任一真实 client 未验证，只能标记
MVP blocker，不能宣称三端正式支持。

---

## Phase 4: User Story 2 — 可靠执行记忆工作流（Priority: P2）

**Goal**: Skill 只使用真实 MCP tools/CLI commands，MCP-first 且一次只选一个 surface 和
namespace；empty/not-found/degraded/error 都以实际证据报告。

**Independent Test**: 在 MCP-only、CLI-only、两者都有、两者都无环境运行 write→search、
get/list/delete、conditional ingest 和 CLI-only curate/stats/export/namespaces/version，
确认无假工具、双写、跨 store 合并或虚假成功。

### Tests for User Story 2

> **NOTE**: T017/T018 必须先对 Phase 2 的空 capability arrays 失败，T019 必须先于正文实现。

- [X] T017 [P] [US2] 编写并运行预期失败的 CLI runtime contract test，直接把 `knownCommands` 与 manifest 对照并加入 stale/unknown command 负例及 11 intents 覆盖断言，文件为 `cmd/engram/skill_contract_test.go`
- [X] T018 [P] [US2] 编写并运行预期失败的 MCP runtime contract test，使用 official in-memory transport 对照 offline 五工具与 LLM 条件 `memory_ingest`，并拒绝 CLI-only fake tools，文件为 `mcpserver/skill_contract_test.go`
- [X] T019 [P] [US2] 在正文实现前加入 MCP-only、CLI-only、both/no-surface、write→search、empty/not-found、conditional ingest、curate 和 CLI-only management 的客观行为用例与 assertions，文件为 `skills/engram/evals/evals.json`

### Implementation for User Story 2

- [X] T020 [US2] 按 runtime tests 填满 lexical MCP/CLI sets 和 11 个 intent 的 mutating/surface/condition 映射，使 T017/T018 通过，文件为 `skills/engram/references/contract.json`
- [X] T021 [P] [US2] 编写 MCP 按需参考，准确记录五个常驻工具、条件 `memory_ingest` 的 input/output/error、结构性 degradation 和明确不存在的工具名，文件为 `skills/engram/references/mcp.md`
- [X] T022 [P] [US2] 编写 CLI 按需参考，准确记录 11 个 commands、global flags 必须位于 command 前、stdin ingest、exit/error 语义、CLI-only intents 和不依赖 `engram --help` 的诊断路径，文件为 `skills/engram/references/cli.md`
- [X] T023 [US2] 把 preflight、MCP-first surface selection、single namespace/data-store、11 intent routing、显式 mutation 和 OperationEvidence 输出契约写入精简正文，并直接链接三份一层 references，文件为 `skills/engram/SKILL.md`
- [ ] T024 [US2] 运行 `CGO_ENABLED=0 go test -count=1 ./mcpserver ./cmd/engram ./cmd/engram-mcp` 及 US2 行为用例的 with-skill/without-skill 对照，把真实 tool/command coverage、往返结果和失败证据写入 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: MCP-only 与 CLI-only 往返独立通过；所有 manifest 名称来自 runtime；同一请求
无双写、无跨 store 推断。

---

## Phase 5: User Story 3 — 保持离线、隔离与秘密安全（Priority: P3）

**Goal**: 无网络、embedding、LLM 时本地 CRUD/关键词检索仍诚实工作；非法 namespace、secret、
隐式 mutation、缺 LLM 和 MCP/CLI store mismatch 均在调用前阻断或保留真实失败。

**Independent Test**: 在完全离线环境完成 CRUD 和 keyword search，再运行 `.`, `..`、separator、
overlength namespace、secret-bearing write、无 LLM ingest/curate 和不同 data dir cases；
越界调用、secret persistence、付费推荐与虚假成功均为 0。

### Tests for User Story 3

> **NOTE**: T025 应先让 source validation 因缺少安全用例而失败，再执行 T026–T029。

- [X] T025 [US3] 扩展失败测试，要求 eval 覆盖 offline、missing embedding/LLM、非法 namespace、secret input、cross-store mismatch，并拒绝 secret-shaped fixture、hosted reranker 推荐和越界 namespace 示例，文件为 `scripts/validate-agent-skill.test.mjs`

### Implementation for User Story 3

- [X] T026 [US3] 增加完全离线 CRUD/keyword、missing LLM ingest/curate、非法 namespace、secret-bearing input、content budget、empty/not-found 和 MCP/CLI 不同 data dir 的行为 cases/assertions，文件为 `skills/engram/evals/evals.json`
- [X] T027 [US3] 在主流程加入默认 `default`、namespace regex 与 `.`/`..`/separator 拒绝、明确 state-change、secret stop、模型能力检查、诚实 degradation 和禁止 paid reranker/recall model 的规则，文件为 `skills/engram/SKILL.md`
- [X] T028 [P] [US3] 补齐 MCP reference 的 local-first、returned degradation、LLM conditional、content/trigger budget、namespace 和 env-only secret 边界，文件为 `skills/engram/references/mcp.md`
- [X] T029 [P] [US3] 补齐 CLI reference 的 offline output、exit codes、namespace/data-dir 确认、LLM capability error、content rejection 和 API-key env-only 边界，文件为 `skills/engram/references/cli.md`
- [ ] T030 [US3] 运行现有 offline/parity/namespace/isolation Go tests；仅在合格 cost class 下运行 US3 with-skill/without-skill cases，确认无网络和 engram 模型配置时 CRUD/keyword 成功且 LLM intents 不虚假成功，并把结果写入 `specs/020-engram-agent-skill/validation-report.md`
- [X] T031 [US3] 扫描 skill、docs、tests 和 scratch 输出中的 secret-shaped 内容及 hosted reranker 建议，重新确认 engine directory diff、metered provider/client 配置和新增模型费用均为 0，并把 cost ledger 与审计结果写入 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: US3 的离线、namespace、secret、LLM 和 store identity 安全 cases 全部通过，且
没有修改 engine、配置 metered 路径或产生增量模型费用。

---

## Phase 6: User Story 4 — 单一来源维护三端兼容（Priority: P4）

**Goal**: 一个 canonical package、机器 manifest、确定性 validator、触发/行为评估和 CI
共同阻止三端副本、过期工具名、命令漂移或 description 误触发。

**Independent Test**: 在 scratch copy 中逐一注入额外 frontmatter、坏引用、第二份正文、未知
tool/command、命令漂移、失衡 trigger set 和 release placeholder；每一类都必须被对应 gate
拒绝，而 canonical source、20+ trigger set 和行为 benchmark 全部通过。

### Tests for User Story 4

> **NOTE**: T032 必须在 final validator、trigger set 和 docs integration 前失败。

- [X] T032 [US4] 扩展失败测试，覆盖唯一 canonical body、未知 frontmatter、description 语义、`engram-body-token-estimate-v1`/line 限制、一层引用、manifest 排序与 intent 完整性、eval ID/schema、trigger 正负平衡、README/docs 命令一致、预声明 tag 和 `<ENGRAM_SKILL_TAG>` release placeholder，文件为 `scripts/validate-agent-skill.test.mjs`

### Implementation for User Story 4

- [X] T033 [US4] 实现 canonical repo scan、normalized quick-command comparison、完整 manifest/eval/trigger coverage、source/release modes、`engram-body-token-estimate-v1`、`engram-package-sha256-v1`、duplicate body、license 同步、commit-SHA 自引用和 unreplaced user-facing tag 检查，使 T032 通过，文件为 `scripts/validate-agent-skill.mjs`
- [X] T034 [P] [US4] 创建至少 20 个真实、具体、含中英文与近邻歧义的 trigger cases，正例覆盖直接/间接持久记忆，负例覆盖 RAM/cache/database/transient context，文件为 `skills/engram/evals/trigger-evals.json`
- [X] T035 [P] [US4] 补足至少 12 个行为 eval、唯一 ID、expected_output 和客观 expectations，确保 US2/US3 所有代表性 case 可分组运行，文件为 `skills/engram/evals/evals.json`
- [ ] T036 [US4] 先记录并验证 `local` 或 `existing-flat-rate` runner/model，再按 skill-creator 流程在 session scratchpad 同批运行 with-skill 与 without-skill、grade assertions、聚合 benchmark、执行 analyst pass并生成 static review viewer，把调用数、incremental cost `0`、benchmark 摘要和人工复核入口写入 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T037 [US4] 等待并读取维护者 viewer 结论；只有明确的 `approved-no-comments`、`changes-requested` 或 `approved-after-changes` 才更新 disposition，按反馈迭代正文与 eval；无响应记录 blocker 而非 no-change，`changes-requested` 必须继续迭代到两个 approved 状态之一，文件为 `skills/engram/SKILL.md`、`skills/engram/evals/evals.json` 和 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T038 [US4] 使用 held-out trigger cases，仅在 `local` 或 `existing-flat-rate` 且 incremental cost 为 0 的评测路径上优化 description，达到显式请求 100%、间接正例 ≥90%、近邻负例误触 ≤10%，并更新 `skills/engram/SKILL.md`
- [X] T039 [P] [US4] 纠正完整 `engram --help` 的过期宣称，增加 skill 与 CLI 的依赖边界及安装正本交叉链接，文件为 `docs/guides/cli.md`
- [X] T040 [P] [US4] 增加 skill 与 MCP 的依赖边界、LLM/namespace 提醒及安装正本交叉链接，不复制完整安装矩阵，文件为 `docs/guides/mcp-server.md`
- [X] T041 [US4] 在 CI 配置 Node.js 24，并加入 Node validator tests/source validation、隔离 local install matrix、adapter contract tests 和现有 docs gates，同时保留全部 Go/CGO=0 gates，文件为 `.github/workflows/ci.yml`
- [ ] T042 [US4] 运行 authoritative source validator、docs tests/validator、runtime contract tests 和 US4 scratch mutation cases；仅当 Agent Skills reference validator 的准确 distribution/version 已 pin/cache 时运行并标记 advisory，实际失败必须修复或记录以当前开放规范证成的 version-incompatibility disposition；把每个 gate、trigger metrics、cost ledger、benchmark 和最终 approving human disposition 写入 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: 单一来源、标准格式、命令同步、runtime contract、trigger metrics、行为审阅和 CI
全部独立通过；任何 drift fixture 都能明确失败。

---

## Phase 7: Polish & Cross-Cutting Release Gates

**Purpose**: 先把预声明 tag 写入完整 package 并冻结候选，再经维护者授权发布 tag，完成真实
远程安装与三端发布验收，最后运行全仓门禁。

- [ ] T043 从 `contract.json` skill version 派生并确认尚不存在的 `engram-skill-v<version>` release tag，在创建 tag 前把 literal tag 同步写入 `skills/engram/references/install.md`、`README.md`、`README.zh-CN.md` 和 `docs/README.md`，运行 `node scripts/validate-agent-skill.mjs --release`，通过现有 maintainer-approved commit workflow 形成精确候选提交，确认 user-facing 文件中无 `<ENGRAM_SKILL_TAG>`/mutable ref/commit-SHA 自引用，并把 proposed tag、candidate commit 与 `engram-package-sha256-v1` digest 写入 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T044 仅在维护者明确授权并把 T043 的预声明 tag 发布到精确 candidate commit 后，验证 tag→commit→digest 绑定，再用 pinned `skills@1.5.20` 和 exact tag URL 在 session scratchpad 运行 `--list`、单端 project/user、三端组合、同版本重跑、copy/symlink 与恢复 smoke，把 tag、commit、installer lock、digest algorithm/digest、路径和结果写入 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T045 仅在 `local` 或 `existing-flat-rate` 路径上用 exact release command，在隔离 profile 对当前 stable Claude Code、Codex、OpenCode 完成 project/user 显式调用和一次三端组合 discovery；任何缺失、认证或不合格计费路径都按 release blocker 记录，并把 cost class、调用数与 incremental cost `0` 写入 `specs/020-engram-agent-skill/validation-report.md`
- [ ] T046 按 `specs/020-engram-agent-skill/quickstart.md` 运行 Node、docs、targeted Go、`CGO_ENABLED=0 go build ./...`、全量 test/vet、`git diff --check`、engine-directory empty diff、duplicate/secret/tag 扫描，最终填写宪法五项、zero-incremental-cost ledger、LoCoMo 未触发理由及所有 skip/failure 到 `specs/020-engram-agent-skill/validation-report.md`

**Checkpoint**: 只有 T043–T046 全部完成且三个真实 client 都通过后，feature 才能标记 release-ready。

---

## Dependencies & Execution Order

### Phase Dependencies

- **Phase 1 Setup**: 无依赖，必须先冻结工作区与 scratch 边界。
- **Phase 2 Foundational**: 依赖 Phase 1；阻塞所有 user stories。
- **US1 (Phase 3)**: 依赖 Foundational；主要修改 installation/docs。
- **US2 (Phase 4)**: 依赖 Foundational；可与 US1 的主体实现并行，主要修改 runtime contract、
  workflow references 和正文。
- **US3 (Phase 5)**: 依赖 US2 的 routing/eval 基线，在同一正文和 references 上增加安全规则。
- **US4 (Phase 6)**: 依赖 US1、US2、US3 的完整 source，负责最终 validator、eval loop、docs/CI。
- **Release Gates (Phase 7)**: 依赖全部 stories；先预声明 tag 并写入最终 package，形成候选
  commit 后才能由维护者授权发布该 tag，remote smoke 必须验证 tag/commit/digest 绑定。

### User Story Dependency Graph

```text
Setup → Foundational ─┬─→ US1 ───────┐
                     └─→ US2 → US3 ─┼─→ US4 → Release Gates
                                     ┘
```

### Within Each User Story

- 失败 tests/evals 必须先写并观察失败。
- Manifest/schema/reference 实现必须使对应 tests 转绿，不删除负例。
- Story-specific source 完成后运行其 independent test 并记录 evidence。
- 修改 `SKILL.md`、`evals.json` 或 `validation-report.md` 的任务需要串行；不同 reference/docs
  文件可按 `[P]` 并行。
- 任一任务发现 engine API 缺失必须停止并开独立 engine contract increment，不得绕过。

### Parallel Opportunities

- Foundation: T006 与 T007 可在 T005 后并行。
- US1 与 US2 的主体可在 Foundational 后并行，但写 `validation-report.md` 时必须串行合并。
- US1: T009/T010 可并行；T012/T013/T014 可在 T011 后并行。
- US2: T017/T018/T019 可并行；T021/T022 可在 T020 后并行。
- US3: T028/T029 可在 T027 的边界冻结后并行。
- US4: T034/T035 可并行；T039/T040 可并行。

---

## Parallel Example: User Story 1

```text
并行测试：
- T009: scripts/validate-agent-skill.test.mjs
- T010: scripts/test-agent-skill-install.mjs

安装正本冻结后并行更新入口：
- T012: README.md
- T013: README.zh-CN.md
- T014: docs/README.md
```

## Parallel Example: User Story 2

```text
并行写测试/eval：
- T017: cmd/engram/skill_contract_test.go
- T018: mcpserver/skill_contract_test.go
- T019: skills/engram/evals/evals.json

Manifest 通过后并行写 references：
- T021: skills/engram/references/mcp.md
- T022: skills/engram/references/cli.md
```

## Parallel Example: User Story 3

```text
安全正文边界冻结后并行补充两个 adapter reference：
- T028: skills/engram/references/mcp.md
- T029: skills/engram/references/cli.md
```

## Parallel Example: User Story 4

```text
并行完善评估集：
- T034: skills/engram/evals/trigger-evals.json
- T035: skills/engram/evals/evals.json

并行更新交叉入口：
- T039: docs/guides/cli.md
- T040: docs/guides/mcp-server.md
```

---

## Implementation Strategy

### MVP First（US1）

1. 完成 Setup + Foundational。
2. 完成 US1 的一条命令、隔离安装矩阵和真实 client discovery。
3. **STOP AND VALIDATE**：确认每端只发现一个相同 version/`engram-package-sha256-v1` digest 的 skill。
4. 此时是“安装/发现 MVP”，不是公开 release；US2/US3/US4 未完成前不得宣称 workflow、
   safety 或正式三端兼容已交付。

### Incremental Delivery

1. Foundation → 标准 package/validator 骨架。
2. US1 → 可安装、可发现。
3. US2 → MCP/CLI 真实工作流与证据语义。
4. US3 → 离线、namespace、secret、LLM 安全边界。
5. US4 → 单一来源、trigger/behavior eval、docs/CI 防漂移。
6. Release Gates → predeclared immutable tag、remote install、三端真实调用、全仓验证。

### Parallel Team Strategy

1. 共同完成 Setup/Foundation。
2. Developer A 执行 US1 installation/docs；Developer B 同时执行 US2 runtime/workflow。
3. US2 完成后 Developer B 执行 US3；Developer A 可准备 US1 isolated client evidence。
4. 汇合后单线执行 US4 的 shared validator/eval/CI 和最终 release gates。
5. 任何 agent 写同一文件前先刷新 `git status` 与文件内容；不覆盖其他 agent/feature 工作。

---

## Notes

- `[P]` 仅表示文件无冲突；仍需满足前置依赖。
- 所有 Go test files 位于 adapter packages；`memory/ embedding/ provider/ store/ internal/`
  始终只读。
- 每次 Go test 编辑后先运行 touched package 的 `CGO_ENABLED=0 go test -count=1`，再继续。
- 文档/skill copy 若跳过 Go build，必须在 validation report 说明；Phase 7 仍运行全量门禁。
- Behavior/trigger eval 不配置 metered engram provider，不运行 LoCoMo，不使用 hosted reranker；
  只接受 `local`/`existing-flat-rate` 并记录 incremental cost `0`。
- `skills@1.5.20` 的 `-y` 会删除重建同名目标；默认 quick command 不得带 trailing `-y`。
- T043 只准备预声明 tag 和精确候选；创建/推送 tag 是外部发布动作，没有维护者明确授权必须
  停止。T044 前 tag 必须已指向该候选且 digest 相同。
- T045 任一 client 未实际发现/调用均为 release blocker，不能用 installer exit 0 替代。
- 每个 logical group 完成后重新检查 sibling worktree 和 engine diff；绝不丢弃他人工作。
