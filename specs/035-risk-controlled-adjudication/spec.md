# Feature Specification: Risk-Controlled Second-Pass Adjudication

**Feature Branch**: `worktree-035-risk-controlled-adjudication`

**Created**: 2026-08-10

**Status**: **Closed（NO-GO 收口，2026-08-11）**——954-call Stage-0 已完整封存；历史映射仍为 1378/1540（89.48%），唯一改选为正确性中性，不进入 formal rejudge。后续决策缺口归因由 [036](../036-decision-gap-attribution/spec.md) 承接。

**Input**: User description: "继续冲击 LoCoMo 严格超过 90%；承接 034 的 89.48% NO-GO，针对错误的高置信候选改选做风险控制二次裁决，不使用付费云 reranker/recall，不接触引擎，并先通过冻结 Stage-0 止损门。"

## 实际收口（2026-08-11，NO-GO）

本 feature 已按冻结协议执行完成并正式收口：

- **执行完整**：单次批准的 V4-Pro Stage-0 完成 954/954 次调用，940 个严格有效响应、14 个
  `invalid_response`、零重试；seal、冻结诊断和 1540 条决策均有效。最终 1539 题保留父答案，仅 1 题满足
  双视图严格收敛并改选。
- **分数未转化**：父映射与新映射同为 **1378/1540（89.48%）**，judge-instability lower 为 1375；
  triggered mixed 为 61/88、lower 为 60；new-only/parent-only 为 0/0，McNemar p=1.0。唯一改选没有改变
  正确性，因此结论为 **NO-GO**，不启动 formal paired-rejudge。
- **失败机制已定位**：当前答案在完成调用中仅被严格反驳 10/473（entailment）和 16/467
  （falsification）；同时满足“反驳当前答案 + 唯一替代受支持”的视图分别只有 4 和 5 个，最终仅 1 个
  packet 在两个视图上收敛。问题不再是裁决阈值，而是裁决输入的证据形态与候选生成质量；不得依据本次
  hidden outcome 事后放宽门槛。
- **出货影响为零**：该路径保持 benchmark-only、显式付费、default-off；没有加入 hosted reranker/recall，
  没有修改 memory engine 或默认 benchmark 行为。实现与可审计产物保留为历史诊断能力，不作为产品涨点
  方案推荐。
- **后续承接**：不再沿 035 的“更保守二次裁决”轴继续付费试探。候选 oracle 1411 与 selected 1378
  之间的 33 题缺口，已由 [036 决策缺口归因](../036-decision-gap-attribution/spec.md) 以零模型调用方式承接。
  完整调用收据、门禁与失败分析见 [quickstart.md](quickstart.md#7-measured-stage-0-result-2026-08-10)，任务与
  验证收据见 [tasks.md](tasks.md#completion-record-2026-08-10)。

## Decision and Scope

Feature 034 已证明候选空间足够（candidate oracle 1411/1540），但单次证据裁决只得到 1378/1540：相对
可执行 control 救回 23 题、反伤 13 题。53 个触发 fallback 即使按候选 oracle 全部修复，也只能达到
1383，不能单独突破 90%。因此 035 不再调整召回、证据顺序或 confidence 接受阈值，而是对公开可观察的
“决策风险队列”做第二次、候选对称、保守的证据审计。

冻结队列由 034 的已验证公共 packet 与 seal/decision 产物确定，不读取历史正确性：

- 424 个已接受选择，其规范化答案与确定性文本 control 不同；
- 53 个触发题 fallback；
- 合计 477 题进入双审计；其余 1063 题保持 034 决策且零新增调用。

每个审计视图都平等评估覆盖三个既有候选的 2 或 3 个唯一规范化答案组，只能引用原 packet 的
E01–E30，不得知道哪个候选是 034 选择或 control。最终 resolver 默认保留 034 决策；只有两个互补视图
的独立调用收据都以有效引用判定当前答案被反驳，并一致
判定同一个替代候选被支持时，才允许改选。该 feature 是 benchmark-only、显式付费、默认关闭的诊断
流程；它不是 reranker、recall、第四答案生成器或正式 LoCoMo 评分协议。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 冻结无标签决策风险队列 (Priority: P1)

评测维护者从 034 的有效公共 packet、label-free call journal、seal 和 sealed decisions 构建二次审计
输入，得到可复算的 477 题队列；构建过程在任何模型调用前完成，且不会打开 034 的 score、slot map、
custody 或原始正确性日志。队列只由 packet 与 decisions 派生，call journal 仅用于重放父 seal 完整性。

**Why this priority**: 034 已经在同一历史标签上做过诊断。只有先把队列规则限制为公开、机制性的决策
状态，才能避免逐题挑选错误样本或用 hidden label 驱动新调用。

**Independent Test**: 完全离线构建两次并改变路径、输入顺序及所有 hidden verdict 字段；公开审计 packet、
477 题队列、两个审计视图和协议摘要逐字节一致，模型调用数为零。

**Acceptance Scenarios**:

1. **Given** 完整且有效的 034 public artifacts、seal 与 decisions，**When** 构建风险队列，**Then** 精确
   产生 424 个 accepted semantic override 与 53 个 triggered fallback，共 477 个唯一问题。
2. **Given** 769 个 non-trigger 与 294 个 accepted-but-control-agree 问题，**When** 构建队列，**Then**
   它们全部不进入审计，并绑定为“保留 034 决策、零新增调用”。
3. **Given** 034 seal/decision/packet 任一 digest、计数或状态被篡改，**When** 构建，**Then** 在产生审计
   packet 或调用前失败关闭。
4. **Given** 原始候选正确性、gold 或 historical score 被改变，**When** 重建公开审计输入，**Then**
   产物与队列不变，且构建过程没有读取这些字段。

---

### User Story 2 - 双视图证据审计与保守改选 (Priority: P1)

对每个风险题生成两个候选顺序不同、角色互补的审计视图。审计者逐个判断 2 或 3 个唯一规范化答案组
是否有直接支持、是否有直接反证，并分别给出 packet 内引用；每组仍绑定一个或多个原始候选 digest，
因此不能产生新答案。系统不把 034 选择、control、来源 run 或历史标签暴露给审计者；任何失败、分歧
或证据冲突/不足都保留 034 决策。两个调用有独立收据，但不声称统计独立。

**Why this priority**: 034 的问题是改选精度而不是候选缺失。候选对称审计降低锚定风险，双审计一致性与
默认保留策略直接限制新的反伤，而不会通过放宽低置信输出扩大错误面。

**Independent Test**: 用完全离线 stub 覆盖双审计一致改选、不一致、当前答案仍被支持、引用无效、输出
无效、调用失败和并发乱序；只有严格一致的支持/反驳组合改变答案，最终产物逐字节确定。

**Acceptance Scenarios**:

1. **Given** 两个视图都以有效引用判定当前组有反证且无直接支持，且一致判定同一唯一替代组有直接支持
   且无反证，**When** resolver 处理结果，**Then** 改选到该组预先冻结的既有代表答案，并记录两组引用。
2. **Given** 任一视图认为当前组有直接支持、两个视图指向不同替代组、任一替代组支持与反证冲突、证据
   不足或引用无效，**When** resolver 处理结果，**Then** 原样保留 034 决策。
3. **Given** 审计返回新答案、未知字段、重复/越界引用、缺少任一答案组判断或自由文本，**When** 验证，
   **Then** 该视图失败关闭，不允许改选。
4. **Given** 477 个风险题和并发 32，**When** 完成可评分运行，**Then** 每题每视图恰好一次尝试，合计
   954 次新增调用且零重试；1063 个非风险题新增调用数为零，不能根据第一视图结果短路第二视图。
5. **Given** 任一调用失败或进程中断，**When** 恢复或封印，**Then** 不重复不确定调用；无法证明完整性
   时拒绝 seal，已终止失败只能导向“保留 034 决策”。

---

### User Story 3 - 封印后评分与严格止损 (Priority: P2)

维护者先得到覆盖 1540 题的完整二次决策 seal，再由独立评分阶段加入冻结历史 verdict，比较 035 与 034，
报告风险队列、改选、paired flips、mixed-verdict、类别回归与 judge-instability 敏感性。

**Why this priority**: 只有 seal-first 才能证明 hidden labels 没有影响调用或 resolver。034 已在同一历史数据
上探索过，因此 035 仍只能作为候选方向 Stage-0；即使 GO，也只授权新的正式配对重判协议。

**Independent Test**: 一个 spy hidden loader 在任何无效 seal 上保持零读取；有效冻结 fixture 精确复现
034 基线 1378/1540、61/88 以及 035 的 paired flips、类别门和晋级判据。

**Acceptance Scenarios**:

1. **Given** 二次审计尚未完整封印或有 orphan/duplicate/tamper，**When** 请求评分，**Then** hidden loader
   调用数为零且不写有效 score。
2. **Given** 有效 seal，**When** 加入冻结 historical verdict，**Then** 报告 1540/477/954 口径、相对 034
   的 old-only/new-only、exact McNemar、分类别 Holm 结果及 13/5 judge-instability 敏感性。
3. **Given** 新历史映射与 judge-instability 最坏界均至少 1387/1540、triggered mixed 及其最坏界均至少
   69/88、相对 034 净增至少 9、总体 exact McNemar p<0.05、temporal 净变化不负、无 Holm 显著净负类别
   且所有完整性门通过，**When** 裁决，**Then** Stage-0 为 GO，但仍标为 historical mapping。
4. **Given** 任一晋级门失败，**When** 裁决，**Then** 输出 NO-GO 并停止，不创建或执行正式 rejudge。

### Edge Cases

- 两个候选规范化后相同：在 provider 视图中合并为一个答案组，resolver 仅按组与 answer digest
  对齐，不允许通过重复 slot 获取历史标签优势。
- 两个审计的候选顺序不同：resolver 按答案 digest 对齐，不能按视图 slot 位置误配。
- 多个替代候选都有直接支持：视为不唯一，不改选。
- 当前候选与替代候选都有直接支持，或同一组同时有支持与反证：保留当前选择。
- 当前候选有反证但替代候选没有直接支持：保留当前选择。
- 引用同一证据的重复 ID、未知 ID，或 support/contradiction=`yes` 却无对应引用：该判断无效；
  `no`/`unclear` 的引用必须为空。
- 034 的 49 个 low-confidence 与 4 个 invalid-response fallback 均进入同一风险规则，不按隐藏表现区别对待。
- temporal 类别只要双审计不完全一致就保留 034 决策；类别身份不得改变总体 resolver 规则或引入逐题调参。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 先重放验证 034 public packets、label-free call journal、seal 和 sealed decisions，
  再只从 packets 与 decisions 派生审计输入；在新 seal 验证前 MUST NOT 打开 historical score、slot map、
  custody、raw candidates、gold 或 correctness。
- **FR-002**: 风险队列 MUST 使用冻结的 label-blind 规则精确派生 477 题：424 accepted semantic override
  加 53 triggered fallback；队列外 1063 题 MUST 保留 034 决策且零新增调用。
- **FR-003**: 系统 MUST 为每个风险题生成两个候选顺序不同且不标记 current/control/source 的审计视图；
  改变路径、输入顺序或 hidden verdict MUST 不改变视图及队列 digest。
- **FR-004**: 每个审计 MUST 对覆盖三个原候选的全部 2 或 3 个唯一规范化答案组分别给出闭集的 support
  与 contradiction（yes/no/unclear）及对应 E01–E30 引用，不得直接推荐候选、生成第四答案或输出自由
  文本；yes 必须有非重复有效引用，no/unclear 必须无引用。
- **FR-005**: resolver MUST 默认保留 034 决策；只有两个有效视图均判当前组 contradiction=yes 且
  support!=yes、均对同一唯一替代组 support=yes 且 contradiction!=yes，并且任一视图均无第二个 supported
  替代组时才能改选。任一失败、不一致、冲突、歧义或证据不足 MUST 保留原决策。
- **FR-006**: 相同答案 MUST 按规范化答案与精确 answer digest 处理，不得按 blinded slot 或 source 身份
  选择历史上更有利的 verdict。
- **FR-007**: 每个风险题每个审计视图在同一 protocol/run directory 内 MUST 最多一次 provider attempt；
  有效 Stage-0 seal MUST 恰好有 954 个 started/terminal/provider attempts、retries=0，并以 append-only
  journal 记录开始与闭集终态。不得根据一个视图的结果跳过另一个视图。
- **FR-008**: hosted 审计 MUST 显式 opt-in、默认关闭、使用专用环境配置；密钥、原始 endpoint、raw provider
  response/error MUST 不进入 artifact、日志或 tracked file。
- **FR-009**: 系统 MUST 在全部 1540 个最终决策完整、排序确定且无 orphan/duplicate 后才生成 seal；seal
  MUST 绑定 034 protocol/decision digests、新 packet/prompt/model/binary digests、调用与 fallback 计数。
- **FR-010**: score MUST 先完整验证 public artifacts、journals、决策和 seal，再允许 hidden loader 读取冻结
  verdict；任何验证失败 MUST 保持 hidden reads 为零。
- **FR-011**: score MUST 报告 034 基线、035 mapping、1540/477/954 口径、61/88 mixed 基线、13/5 instability
  及总体/mixed 最坏与最好界、paired flips、总体 exact McNemar、类别 exact McNemar/Holm、temporal 净变化、
  调用/失败/成本与完整性状态。
- **FR-012**: Stage-0 GO MUST 同时满足：点映射及 judge-instability 最坏界至少 1387/1540、triggered mixed
  点映射及最坏界至少 69/88、相对 034 净增至少 9、总体 exact McNemar p<0.05、temporal 净变化不负、
  无 Holm 校正显著净负类别、完整性/冻结诊断全部通过；否则 MUST NO-GO 并停止。
- **FR-013**: 输出 MUST 始终标记为 historical verdict mapping；GO 只授权独立的新正式 paired-rejudge
  spec，不得直接宣称新的 LoCoMo 分数。
- **FR-014**: 本 feature MUST 不修改检索、召回、reranker、抽取、curation、storage、embedding、memory
  engine 或默认 benchmark 路径；不得使用付费云 reranker/recall 作为涨点手段。
- **FR-015**: 离线 build/validate/stub-run/score fixture MUST 无网络运行、确定且可复算；托管模型不可用时
  不影响这些离线能力。

### Key Entities

- **034 Baseline Receipt**: 034 的 protocol、packet set、decision set 与 seal 身份，定义二次审计唯一合法
  输入基线，不包含 hidden verdict。
- **Risk Queue**: 由公开决策状态派生的 477 个问题集合，分为 accepted semantic override 与 triggered
  fallback；只决定是否审计，不包含正确性。
- **Audit View**: 同一问题的一种候选盲化顺序与审计角色，包含三候选和原有证据，但不标记 current/control。
- **Candidate Assessment**: 某一规范化答案组的 support/contradiction 闭集判断与各自 packet 内引用。
- **Conservative Resolution**: 两个审计结果与 034 决策的确定性合并；默认保留，仅双一致时改选。
- **Second-Pass Seal**: 覆盖 1540 个最终决策、477 个风险题及恰好 954 次终态调用的完整性收据。
- **Historical Score Join**: seal 后才创建的诊断映射，包含 paired/category/sensitivity 门，不是正式分数。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 冻结 034 输入上精确、可复算地产生 477 个风险题（424 override + 53 fallback）和 1063 个
  零调用保留题；hidden verdict 任意变化不会改变任何审计输入字节。
- **SC-002**: 离线 fixture 覆盖所有 resolver 分支，100% 保证“不满足双审计唯一一致条件就保留 034 决策”，
  且并发完成顺序不改变最终决策或 seal digest。
- **SC-003**: 可评分 Stage-0 恰好产生 954 次新增 provider attempts、零自动重试、1540/1540 完整决策；
  任一缺失、重复、orphan 或身份漂移均无法生成有效 seal。
- **SC-004**: Stage-0 点映射与 judge-instability 最坏界均至少达到 1387/1540、69/88 triggered mixed，
  相对 034 净增至少 9，总体 exact McNemar p<0.05，temporal 净变化不负，且没有 Holm 校正显著净负类别；
  任一未达即 NO-GO。
- **SC-005**: 所有产物均不包含 gold/correct/verdict 或 credential；provider-facing view/prompt 不包含
  current/control/provider source 标记。私有 resolver/seal 只允许保存可审计的 baseline decision digest
  与 provider/model 身份摘要；hidden loader 在无效 seal 场景的读取次数为零。
- **SC-006**: 默认产品与普通 benchmark 行为保持不变，memory engine 变更数为零；全部离线验证在无网络
  环境下完成。

## Assumptions

- 034 的 protocol、public packets、seal 与 sealed decisions 可用且 digest 固定；035 不修改它们。
- 477 风险队列规则是在看过 034 聚合诊断后提出，因此 035 仍是探索性 Stage-0，不能在相同历史标签上
  升级为正式结论。
- 两个审计视图可以使用同一模型身份，但必须使用 domain-separated 的不同候选排列与互补审计角色；
  只宣称输入视图和调用收据独立，不宣称模型错误统计独立或一致性带来平方级误差下降。
- 现有 E01–E30 是统一重建证据，不是三个历史 answerer 的精确原始上下文；8/1540 context parity 例外
  继续作为 provenance limitation 报告，不作为队列选择条件。
- hosted answer-side audit 只用于显式授权的 benchmark/SaaS Stage-0；本地优先 engine 与默认栈不依赖它。
- 任何新 credential 只通过 operator environment 提供，不进入聊天、脚本、tracked file 或 artifact。
