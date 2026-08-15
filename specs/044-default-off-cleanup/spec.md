# Feature Specification: 默认关闭机制清理(Default-Off Mechanism Cleanup)

**Feature Branch**: `044-default-off-cleanup`

**Created**: 2026-08-16

**Status**: Draft

**Input**: User description: "移除 cmd/locomo-bench 中已证伪(NO-GO/零增量/被取代)的默认关闭实验机制及其专属实现文件与接线,保留已坐实能力(unified/022 chunk)与诊断工具;含 trace-mediation 默认开启组件移除与默认路径切换验证。"

## User Scenarios & Testing *(mandatory)*

> 本 feature 面向**维护者与评测运营者**(内部工程清理),不面向终端产品用户。用户故事按清理价值排序:先清纯冗余、再清有依赖面的、最后清默认组件。

### User Story 1 - 移除已证伪的默认关闭机制(纯 flag 级)(Priority: P1)

维护者希望从 `cmd/locomo-bench` 移除经 verdict 判定为 NO-GO、零增量或被统一契约取代的默认关闭机制 flag,及其专属实现文件、arm 机制映射、冲突表条目与 fingerprint 标记——这些机制在默认路径关闭时零行为影响,清理后代码面更小、维护负担更低。

**Why this priority**: 这些机制占 `cmd/locomo-bench` 大量代码却无任何有效贡献(024/025/029/031/032/abstain 系/L2/temporal 系/早期检索系),是"清理优先级最高、风险最低"的一批——不触碰默认路径,byte-parity 天然保持。

**Independent Test**: 移除一批 flag 后 `CGO_ENABLED=0 go build ./... && go test -count=1 ./...` 全绿;默认路径输出与清理前逐字节一致;已移除的 flag 在 `--help` 中不再出现。

**Acceptance Scenarios**:

1. **Given** 某已证伪机制(如 `--episode-cluster`),**When** 移除其 flag/实现/arm 映射/冲突表条目/fingerprint 标记,**Then** 构建与测试全绿,默认路径逐字节不变,该 flag 从 CLI 帮助中消失。
2. **Given** 剩余活跃机制(unified/022 chunk),**When** 清理完成后,**Then** 其 flag、默认值、行为与清理前完全一致。
3. **Given** 引擎五目录(`memory/ embedding/ provider/ store/ internal/`),**When** 清理全程,**Then** `git diff --name-only -- memory embedding provider store internal` 保持为空。

---

### User Story 2 - 移除 042 反事实效用协议与 043 深化机制(维护者决策项)(Priority: P1)

维护者已决策:042 维持关闭并进清理(其 NO-GO 归因存疑但不重开重验);043 的 confidence-gated deepening pilot NO-GO(AUC 0.54 / flip 93.4%)无机制实现。移除 `--utility-stage` 族(042 协议代码)与 `--confidence-deepen` 族(043 深化)的全部接线与模型执行路径;043 的**纯函数层与 pilot 测量资产**保留(valuable,供未来复用)。

**Why this priority**: 两者都是"方向已关闭但代码仍在 master 的插拔式开关"(默认关闭不激活),维护者明确判定无需继续投入;与 US1 同属高风险最低的清理。

**Independent Test**: 移除后 `--utility-stage` / `--confidence-deepen` 族 flag 不再存在;043 纯函数层(`gapQueryFor`/`appendDedup`/AUC/lexicon)与其测试仍可独立运行(作为不接线资产);默认路径 byte-parity 保持。

**Acceptance Scenarios**:

1. **Given** `--utility-stage` 族 flag,**When** 清理完成,**Then** 该族 flag 与 counterfactual 协议文件全部移除,`--help` 不再列出。
2. **Given** 043 纯函数层,**When** 清理,**Then** 纯函数与测试保留在仓库(移至资产位置或原地保留但不接线),机制 flag/pilot 模型执行移除。
3. **Given** 042/043 的方向结论,**When** 清理后,**Then** result-matrix「过时/已证伪」表与清理计划文档同步记录,不留下"代码还在=能力还在"的误导。

---

### User Story 3 - 移除默认开启的 trace-mediation 组件(行为变更)(Priority: P2)

Step A 配对验证 unified 契约下 trace-mediation 显著负(−3.44pp, McNemar p=1.4e-04):030 的读侧证据中介在 unified 时代是负收益,仅剩 token 压缩优势。移除 `--trace-mediation`(及其配套 `--consolidate`/`--evidence-assembly`)——默认路径从"trace 中介"切换到"chunk 装配",须在 unified 配方下验证不回归(87.9% 锚本就是 trace-off)。

**Why this priority**: 唯一"默认开启"的清理项,涉及默认路径行为变更,需单独验证;但既有 evidence(unified 87.9% 锚 = trace off)表明这是把默认收敛到已验证配方,非新风险。

**Independent Test**: 移除后默认 run(不带 trace flag)的 unified 配方行为与 87.9% 锚配方一致(同 store/judge/clean 口径);`--trace-mediation` flag 从 CLI 消失;无 trace 的普通路径 byte-parity 保持。

**Acceptance Scenarios**:

1. **Given** 默认开启的 `--trace-mediation`,**When** 移除该组件,**Then** flag 消失,默认路径不再走 trace 中介,统一走 chunk 装配。
2. **Given** trace 移除后的默认配方,**When** 与 87.9% 锚(unified k30 hybrid trace-off)对齐,**Then** 同配置绝对分与锚一致(±系统噪声),不引入新回归。
3. **Given** trace 的 token 压缩优势,**When** 移除后,**Then** 在文档中诚实记录(压缩优势仍在但作为得分杠杆 NO),不假装压缩收益随组件保留。

---

### User Story 4 - 归档清理决策与同步文档(收尾)(Priority: P3)

清理完成后,result-matrix「过时/已证伪」表、默认关闭机制清理计划文档、README 的机制说明全部同步:已清理机制从文档移除或标注"已移除",保留机制与清理后实际 flag 一一对应。

**Why this priority**: 文档与代码脱节会让未来维护者误以为已证伪机制仍可用;同步是"清理完整闭环"的最后一环。

**Independent Test**: grep 文档确认已清机制不再被描述为可用能力;result-matrix 与清理计划文档反映清理后的实际状态。

**Acceptance Scenarios**:

1. **Given** 已清理机制,**When** 收尾,**Then** 其在 result-matrix「过时/已证伪」表与清理计划文档中的条目被更新/标注为已移除。
2. **Given** 保留机制,**When** 收尾,**Then** README/result-matrix 中对应 flag 与行为描述与实际代码一致。

---

### Edge Cases

- **清理后的依赖悬空**:某 flag 移除后,若仍被 `validateUnifiedPromptPairExperiment` 冲突表、`supportedArmMechanisms`、`answerRegimeFingerprint` 或另一保留机制引用 → 必须同步删除引用点,否则编译失败(这反而是安全网,保证不漏)。
- **默认值切换的隐藏依赖**:`trace-mediation` 默认开启的既有决策散落在 030 verdict、result-matrix、README——移除后这些文档若仍描述"默认开启"会误导,须一并更新。
- **043 纯函数层的归属**:保留为资产时,若文件依赖已移除的 042 类型(`utilityRoutingFeatureNames` 等)会导致编译失败 → 要么将依赖的 042 常量子集一起保留,要么将纯函数层归档到非编译路径;plan 阶段定夺。
- **eval-config 与算法改动分离**:trace 移除属 eval-config 变更,须单独 commit;与其它机制清理(纯代码删除)分开提交(宪法 IV attribution)。
- **诊断工具边界**:`--oracle` / `--rerank` / `--pcic` 等诊断或未定论工具**不在**本次清理范围,避免误删仍在用的诊断面。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 已证伪机制(经 verdict 判定 NO-GO / 零增量 / 被取代)的 flag、专属实现、arm 机制映射、冲突表条目、fingerprint 标记 MUST 被移除;移除后 `--help` 不再列出该 flag。**权威清理清单** = [默认关闭机制清理计划](../../../docs/evaluation/default-off-mechanism-cleanup-plan.md)「第一类:已证伪/零增量,建议清理」表(含 `--write-dedup`/`--neighbor-extend`/`--episode-cluster`/`--relation-context`/`--counter-refine`/`--temporal-answer-prompt`/`--lme-typed-prompts`/abstain 系/`--gap-refetch`/`--event-projection`/`--temporal-resolution`/`--nav` 系/`--iris` 系/`--temporal-score`/`--temporal-hard-filter`/`--assoc`/`--cluster-sweep`/`--conflict-resolution`/`--multi-query`/`--filter-pool`/`--opinion-pass`/`--trace-mediation`/`--consolidate`/`--evidence-assembly`/`--utility-stage` 族/`--confidence-deepen` 族)。
- **FR-002**: 清理范围 MUST 包含 042(`--utility-stage` 族协议,13 个 counterfactual 文件)的接线与模型执行路径移除;042 方向结论不变(维持关闭)。
- **FR-003**: 043 机制方向已 NO-GO(verdict 已出);master 不含 043 代码,故本 feature 仅在文档记录其结论,不执行代码清理;若 043 后续 merge 则另行处理。
- **FR-004**: `--trace-mediation` 默认开启组件 MUST 被移除,默认路径切换到 chunk 装配;unified 配方须在清理后与 87.9% 锚(同 store/judge/clean 口径)对齐,不引入回归。
- **FR-005**: 已坐实能力(`--unified-answer-contract` / `--unified-typed-prompts` / `--chunks` / `--chunk-quota` / `--force-answer` / `--no-idk-retry`)MUST 保持不变(flag、默认值、行为均不因清理受影响)。
- **FR-006**: 诊断与未定论工具(`--oracle` / `--rerank` / `--pcic` / `--compiler-arm` / `--representation` / `--temporal-date-scaffold` / `--abstain-probe` / `--recall-diagnostic` 及 `SearchMulti`/`classifyQueryMode`/`computeAbstainSignal`)MUST 不被误删;其去留在 plan 阶段逐个核查并给出理由。
- **FR-007**: 清理 MUST 保持引擎五目录(`memory/ embedding/ provider/ store/ internal/`)零改动;`git diff --name-only -- memory embedding provider store internal` 必须为空。
- **FR-008**: 清理后默认路径 MUST 逐字节不变(byte-parity 断言保持绿色);`CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./...` 全绿。
- **FR-009**: eval-config 变更(如 trace 默认值移除)与算法/代码清理 MUST 分开 commit(宪法 IV attribution)。
- **FR-010**: result-matrix「过时/已证伪」表、清理计划文档、README 的机制说明 MUST 与清理后实际状态同步,不留下"代码仍在=能力仍在"的误导。

### Key Entities

- **机制 flag**:`cmd/locomo-bench` 的 CLI 开关,每个对应一个已验证/证伪的检索或读侧机制(assoc/temporal-score/trace-mediation/…)。
- **专属实现文件**:承载单一机制的 `*.go` 文件(如 `adaptive_topk.go`、counterfactual 协议文件、deepen 文件)。
- **接线点**:`supportedArmMechanisms` 映射、`validateUnifiedPromptPairExperiment` 冲突表、`answerRegimeFingerprint` 标记、`optionsForArm` 机制路由——移除 flag 时必须同步移除的引用点。
- **verdict 记录**:`docs/evaluation/reports/*.md` 与 result-matrix 中的机制结论,是"清理哪些"的判定依据,清理后须同步。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 清理后 `CGO_ENABLED=0 go build ./...` 与 `CGO_ENABLED=0 go test -count=1 ./...` 100% 通过;默认路径输出与清理前逐字节一致(byte-parity 断言全绿)。
- **SC-002**: 第一、二类清理机制(US1+US2)的 flag 100% 从 CLI 帮助与代码中消失;`--help` 中不再出现 `--episode-cluster`、`--utility-stage`、`--confidence-deepen` 等已清 flag。
- **SC-003**: trace-mediation 移除后,默认 unified 配方与 87.9% 锚同口径绝对分一致(±系统噪声),无新增回归;该评估作为 Constitution IV 门的一部分被记录。
- **SC-004**: 引擎五目录 `git diff --name-only -- memory embedding provider store internal` 为空;043 方向结论在 result-matrix/verdict 记录中保持 NO-GO 表述(master 无 043 代码需清理)。
- **SC-005**: 已坐实机制(unified/chunk/force-answer 等)在清理后行为不变——相应已有测试全部保持绿色,无需修改它们的 flag/默认值。
- **SC-006**: result-matrix「过时/已证伪」表与清理计划文档与实际代码状态一致;已清机制在文档中不再被描述为可用能力。

## Assumptions

- **清理范围依据 verdict,不重测**:已证伪机制的判定来自既有 verdict(result-matrix 过时表 + 各 feature verdict),本次清理**不重跑**这些机制,仅移除其代码。
- **042 不重开**:维护者已定 042 维持关闭(归因存疑但不重开重验),042 协议代码进清理。
- **043 已 NO-GO**:043 机制方向关闭,纯函数层 + pilot 测量保留为资产,机制接线移除。
- **trace 移除默认收敛到已验证配方**:87.9% 锚(unified k30 hybrid trace-off)即 trace 移除后的目标配方,不是新配方。
- **043 代码不在 master(已核实 2026-08-16)**:master 不含任何 043 deepen 文件或 `--confidence-deepen` flag(043 代码在独立 worktree 未 merge);故 044 **不清理 043 代码本身**,只记录其方向 NO-GO 结论。若 043 后续 merge,其清理另立变更,不在本 feature。
- **042 代码在 master(13 个 counterfactual 文件)**:044 实际清理的对象是 master 上的 042 协议 + 其余已证伪机制 + trace 默认值切换。
- **引擎与产品契约零改动**:清理只动 `cmd/locomo-bench/`;MCP/CLI/SDK 产品适配面不受影响。
- **文档同步是验收一部分**:result-matrix / 清理计划文档 / README 的同步不属于"锦上添花",而是 SC-006 的验收项。
