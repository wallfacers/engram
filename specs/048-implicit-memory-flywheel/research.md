# Research: 048 implicit-memory-flywheel 数据集独立性与正式计分

**Date**: 2026-09-01
**Scope**: `wallfacers/agent-memory-trigger-bench` 的 172-case 既有集、96-case 独立 holdout、三 CLI 造题/盲审与正式 3-run 计分。

本研究基于当前仓库工件与 runner 代码的只读审计。结论不依赖外部论文或托管 reranker。

## R1 — 172 条的定位

**Decision**: 冻结现有 172 条为不可变的正式 `dev/regression core` manifest；它继续产生正式分数，但不得承担未见分布泛化证明。飞轮后续回填作为 append-only `dev extension` 保存并做全量回归，不得悄悄改变 172 的正式分母。新增 96 条独立 holdout 产生正式 `generalization` 分数，两项永不合并。

**Rationale**: 172 条结构有效，且已覆盖 implicit write/read、trap 与 020 regression；但它曾参与失败发现、skill 修订、定向复测和判分校准，缺少 author/model/prompt/reviewer provenance。因此它适合开发回归，不适合作为唯一泛化证据。

**Alternatives considered**:

- 继续只报告 172：无法排除 Claude Code 风格与共同调优泄漏。
- 往 172 继续追加 Claude 生成题：扩大规模但不改变独立性。
- 删除有争议旧题：破坏正式回归连续性；core 只允许附加 issue annotation，不改原分母/标签；修正版进入 extension 并以 `supersedes` 关联，原 ID 不物理删除。

## R2 — 96 条 holdout 的冻结配额

**Decision**: 96 条按下表预注册。trap 延续当前可确定性判分的三个模块，不新增主观答案质量题。

| Module | Total | zh | en | Gate |
|---|---:|---:|---:|---|
| implicit-write-pos | 20 | 10 | 10 | pass median ≥90% per host |
| implicit-write-neg | 20 | 10 | 10 | false-positive median ≤10% per host |
| implicit-read-pos | 20 | 10 | 10 | pass median ≥90% per host |
| implicit-read-neg | 20 | 10 | 10 | false-positive median ≤10% per host |
| trap-read-pos | 8 | 4 | 4 | pass median ≥90% per host |
| trap-write-neg | 4 | 2 | 2 | false-positive median ≤10% per host |
| trap-read-neg | 4 | 2 | 2 | false-positive median ≤10% per host |
| **Total** | **96** | **48** | **48** | — |

作者 × 模块矩阵同时满足每条 CLI 恰好 32 条与各模块总额：

| Authoring lane | IWP | IWN | IRP | IRN | TRP | TWN | TRN | Total |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Claude Code | 7 | 7 | 6 | 6 | 3 | 2 | 1 | 32 |
| Codex | 7 | 6 | 7 | 6 | 3 | 1 | 2 | 32 |
| OpenCode2 | 6 | 7 | 7 | 8 | 2 | 1 | 1 | 32 |
| **Total** | **20** | **20** | **20** | **20** | **8** | **4** | **4** | **96** |

每条 lane 另有 16 zh / 16 en 的硬配额；每个模块禁止逐句中英翻译对，`family_id` 不得跨 split 重复。为避免总量均衡却语义集中，96 条再按 8 个闭集 scenario bucket 预注册：每桶 12、每 author 4、zh/en=6/6、10 条 implicit + 2 条 trap；每桶覆盖所有 implicit module、恰一条 trap-read-pos 与一条 trap-negative。报告输出 non-gating 的 evaluated-host × author-lane × module × language、scenario bucket 和 self-author gap 切片，以及 generation/review rejection funnel。

预注册到 author × module × language 的最终 slot（每格 `zh/en`）为：

| Authoring lane | IWP | IWN | IRP | IRN | TRP | TWN | TRN | Total zh/en |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Claude Code | 4/3 | 3/4 | 3/3 | 3/3 | 1/2 | 1/1 | 1/0 | 16/16 |
| Codex | 3/4 | 3/3 | 4/3 | 3/3 | 2/1 | 0/1 | 1/1 | 16/16 |
| OpenCode2 | 3/3 | 4/3 | 3/4 | 4/4 | 1/1 | 1/0 | 0/1 | 16/16 |
| **Total** | **10/10** | **10/10** | **10/10** | **10/10** | **4/4** | **2/2** | **2/2** | **48/48** |

**Rationale**: 8/4/4 保留 trap 的读正例、写负例、读负例三类安全面，同时避免 16 条全部落到同一操作。48/48 语言配额和 family 去重防止把翻译镜像误计为独立语义样本。此阶段保持 holdout96，不向已规划 batch 追加更多同源 synthetic cases：先验证闭集场景覆盖和 source slices；若首个 consumed version 暴露实质偏斜，创建新的 version 或引入未参与评测的第四 source，而不是污染现有 seal。

trap 门的粒度是刻意且必须显式报告的：8 条 read-positive 上 `≥90%` 等价于单 run `8/8`，8 条合并 trap-negative 上 `≤10%` 等价于单 run `0/8` 误触发；3-run 中位数通过意味着至少两个 ordinal 全绿。若未来希望允许每 run 一次错误，必须增大 trap 分母或另行修订 spec 门槛，不能在 runner 中四舍五入放宽。

**Alternatives considered**:

- 16 条 trap 平均分到四个 implicit 模块：会失去 adversarial 层的独立身份。
- 沿用当前 trap 的 18/6/4 比例缩放：整数缩放会过度偏向 read-positive。
- 每个作者生成同样的模块配额：32 无法被七个模块同时均分；采用相差不超过 2 的轮转矩阵。

## R3 — 无人工造题的三 CLI 协议

**Decision**: 用同一个版本化造题 prompt 和一个机器配额调度器驱动三条 authoring lane：

- Claude Code：`claude --settings ~/.claude/settings.json.aly_qwen_w ...`
- Codex：`codex -c model_provider=aq -c model=qwen3.8-flash --yolo ...`
- OpenCode2：`opencode2 ... --model <已确认的免费模型>`

调度器逐个 quota slot（含 scenario bucket）请求一个严格 JSON candidate。authoring host 不能审自己的 candidate；另两条 CLI 在互相隔离的新会话中收到 label-blind envelope、冻结后的匿名 dev-family summary payload 和已接受 holdout-family summary payload，各自输出严格 JSON review。reviewer-visible `blind_candidate_digest` 只覆盖 canonical de-labeled candidate projection；相同 blind subset 不得因私有 label/rule/slot 不同而得到不同 digest。family summaries 实际 materialize 给 reviewer，并由 digest/revision 绑定；只给 digest 无法做 novelty review，判无效。envelope 删除 private candidate digest、authoring receipt、author-specific quota slot、batch/source、author/model/config、attempt ordinal、prior review、以及 author 提议的 expect/module/language/scenario/category/machine rules。每个 reviewer 必须独立推导 module、language、scenario、expected label 和 machine rules；controller 在两者提交后才与私有 author candidate/slot 比对，并通过 accepted-family revision 的 CAS 原子提交。陈旧 family summary 的 review 必须在新会话重做。每个 author/reviewer child 还运行在 per-attempt ephemeral workspace，只能读取自己的 quota/prompt 或 review envelope；exact-child probes 必须拒绝 private root、generation audit、author receipt、prior review 与并发 sibling workspace，并有 controller-side target-existence/content/policy proof。controller 在 launch 前 append attempt audit row，accepted/rejected/stale/parse/timeout/model/isolation outcomes 都 terminalize 并进入 seal；不能删除 rejected attempt 来隐藏漂移或隔离失败。每 host 的 author/reviewer resolved model 必须在完整 attempt ledger 中稳定;三宿主 harness 互异是独立性主轴,底层模型经维护者 2026-09-01 拍板统一为百炼 qwen3.8-flash,允许跨 host 相同。只有两条 review 均为 `accept`、均确认不与 dev/holdout family 重合，且独立推导结果完全一致并与私有 candidate/slot 匹配，candidate 才进入 accepted set。任何分歧、非法 schema、重复 family、陈旧 CAS 或非确定性规则都直接作废或重审；没有人工仲裁或手工改题。

所有新增模型调用路径都使用有界 worker pool 并遵守 `--concurrency`，避免重复当前评测历史上的串行吞吐问题。DevFamilyIndex 的三 lane mirror review 同样记录 frozen concurrency、observed max-in-flight，并在 concurrency>1 的验证运行中证明实际 overlap。

**Rationale**: 三分来源降低单一 Claude Code 造题风格的主导性；双非作者盲审降低作者同时定义题目与答案造成的同源偏差。直接丢弃分歧题比让人类仲裁更符合“人工不参与”。

**Alternatives considered**:

- 作者自审：无法减弱同源标签偏差。
- 一条非作者 CLI 审阅：单 reviewer 的宿主偏好会直接进入标签。
- 三条 CLI 投票且包含作者：作者票仍影响标签，违背禁止自审。
- 人工终审：用户明确排除。

现有 172 条最初没有 `family_id`，因此 validation 先做 `pre-index`（ID/矩阵/020 语义/机器规则/路径，不要求 family），再由可重跑的自动化 `DevFamilyIndex` 为其创建不改变 payload 的 metadata 映射，最后做 family-aware validation；它将已知中英镜像归为同一 family。`dev-family-index-v1` 冻结以下算法：按 case ID 排序读取 core172；对 prompt/用户 turns、seed name/content 和机器规则做 LF 规范化、ASCII 小写、Unicode 空白折叠与排序字段摘要；规范化摘要完全相同者直接连边；不同语言且 module、category 与机器规则形状摘要相同者成为 mirror candidate。每个 mirror candidate 由三个 CLI 在新会话中使用 `dev-family-index-review-v1` 独立判断，只有三者均返回 `same_family=true` 且 canonical-family digest 一致才连边；分歧保持为不同 family。最后按连通分量生成稳定 family ID，singleton 也获得 ID。derivation receipt 记录输入 payload digest、算法/version、normalizer、pair list、review prompt digest、三 CLI provenance、frozen concurrency、observed max-in-flight、逐 pair 决定和输出 digest；禁止手工增删映射。

holdout candidate 的 exact/normalized 检查、锁定的 DevFamilyIndex、已接受 holdout-family index 与两条 reviewer 的 novelty verdict 一起构成去重。任何已知 family/翻译镜像命中都 fail closed；不能把 legacy case ID 或作者自报的 `family_id` 当作独立性证明。

## R4 — 造题 prompt 与 case digest

**Decision**: 造题、审阅 prompt 分别以 `holdout-authoring-v1`、`holdout-review-v1` 标识；prompt 文件使用 UTF-8，先把 CRLF/CR 规范化为 LF，再对完整字节做 SHA-256，记录为 `prompt_digest`。任何非空白语义或格式修订都 bump prompt version 并生成新 batch，禁止把不同 digest 的 candidate 混入一个 seal。

case payload digest 对最终、规范化的 case JSON 字节计算。dataset payload digest 只覆盖 manifest-authoritative case files；先把它写入 completed manifest，再对排除 seal object 的完整 manifest 计算独立 digest。任何 digest preimage 都不得含存放自身 digest 的字段。reviewer-visible blind digest 则只覆盖 de-labeled projection，完整私有 candidate digest 永不进入 envelope。算法在 `contracts/dataset-protocol.md` 冻结。

**Rationale**: prompt 版本只写名称不足以复现；原始文件字节 digest 简单、跨工具、无需新增依赖。

**Alternatives considered**:

- 只记录 prompt 文本：容易发生格式或版本漂移。
- 依赖某语言的 map JSON 序列化：跨语言稳定性不足。
- 引入第三方 JSON Canonicalization 依赖：此规模无必要，且增加供应链面。

## R5 — 实际模型与配置 provenance

**Decision**: `ToolProvenance` 必须记录 host、CLI 版本与二进制 digest、固定 invocation-template digest、请求的 profile/model、CLI 事件实际报告的 resolved model ID、脱敏配置摘要 digest、**仅限 `cmd/skill-eval` runner source subtree 或已构建 runner binary** 的 `source_revision` 和 UTC 时间。`source_revision` 的 digest preimage MUST 排除 `skills/engram/**`、所有数据集、spec/docs、snapshot/receipt/artifact 路径及其内容；它不是全仓库 Git revision。另计算只覆盖这些稳定身份字段的 `tool_identity_digest`，明确排除 `captured_at`、series/run/artifact ID、路径和瞬时状态。跨 series 可比性只比较该稳定 digest；时间仍保留在逐次 receipt 中。若实际 model ID 无法从 CLI 可信获得，写 `unavailable`，不得由 “Claude/Codex/OpenCode” 品牌反推模型。一般诊断/正式运行可以如实记录 `unavailable`；但 holdout authoring batch 的三个 lane 在 seal 前必须各自解析到单一、稳定且两两不同的 resolved model ID，任一 unavailable、lane 内漂移或跨 lane 重复都使 batch 不可封存且不得人工 waiver。

配置摘要只允许记录 allowlist 字段：profile 名、settings 文件 digest、模型名、非秘密功能开关与 endpoint digest。禁止保存 API key/token、原始 settings/config、完整 endpoint URL、任意环境变量值或任意 stderr。OpenCode2 的 family review、holdout author/review seal 与 formal run 均要求 `billing_class=free` 的运行者声明和 resolved model ID；holdout authoring 缺任一项则 batch 不可封存，formal run 缺任一项则 series INVALID。

**Rationale**: 本机 Claude Code 通过 `aly_qwen_w` settings 使用阿里云百炼 qwen3.8-flash（2026-09-01 起；历史记录时点为 glm_w 代理），宿主品牌不等于底层模型。旧 plan 的 Codex/OpenCode 版本也已与 2026-08-31 本机值漂移，因此 provenance 必须运行时采集而非写死。把 skill 或文档修改混入 runner revision 会令 SC-5 的“skill 是唯一有意变量”无法验证，故 revision scope 必须收窄。

**Alternatives considered**:

- 保存完整配置以求复现：违反 secrets hard gate。
- 只记录 CLI 名：无法归因模型/配置偏移。
- 从配置文件猜测实际模型：运行时 override 可能使结论错误。

## R6 — holdout 的隐藏、封存与消费生命周期

**Decision**: holdout case 正文在调优期间位于仓库外的受限目录；仓库只提交协议、测试 fixture 与不含 case 正文的 seal receipt。普通 Git digest 只提供完整性，不提供保密性。dataset seal 只证明 admission/provenance/case-payload integrity、完整 all-attempt ledger 和 author/reviewer-stage isolation aggregate，不包含未来 candidate 的执行隔离证明。正式 `series prepare` 在 exact skill package validator 通过且最终 invocation/concurrency/worker identity 冻结后生成 `ProtectedExecutionReceipt`，并对每个可执行 host × worker slot 自动运行 staged-workspace `WorkspaceCanaryReceipt`：逐 slot 验证 protected-root traversal/list/read、author/review audit/state read 和并发 sibling-workspace read 均拒绝，当前 worker workspace/canary 可读；每个 denied probe 都有 controller-side target-existence/content/policy proof，单独 chmod sentinel 或无目标的 `not-found` 无效。每个 formal case 绑定实际使用的 prepared slot/probe，并使用 disposable、从不复用的 HOME/XDG/cache/session/container root；在 child 启动前验证 prior-case state 与 retired-case workspace 不可读，结束后写 reset/teardown receipt。core 与 holdout 使用不相交 allocator，holdout roots 在其 leg 前不得使用。formal roots 与 author/review roots 分离，concurrency>1 时每个 worker 使用独立 user/container/mount/ACL-equivalent boundary。

生命周期为：

`generated → schema-valid → blind-reviewed → accepted → assembled → dataset-sealed → holdout-pipeline-green → frozen-skill-snapshot/package-validated → series-prepare-green → series-prepared/runtime-receipts-sealed → core-primary-complete → fresh-pre-holdout-green → binding-attempt-associated → holdout-primary-complete-and-sealed → consumed → scored`

- reject 记录保留在私有 generation audit，但永不进入 scored dataset。
- seal 后 case、label、judge rule、配额、顺序或 manifest 任何变化都使 seal 失效。
- primary ordinal 1 开始后不得修改 frozen skill snapshot、runner、judge、dataset 或调用配置；mutable `skills/engram` 不是正式 evaluated package，之后的源目录改动不能替换已绑定 snapshot。
- holdout ordinal 1 前必须完成该 series 的 core172 三宿主 × 三 ordinal，并在其后新建固定 suite 的 `pre-holdout` GreenTestReceipt；它必须绑定 exact sealed manifest、stable `CandidateBindingV1` digest 与 complete core-leg receipt set。manifest/receipt 不可跨 series 复用，也不能事后补发。
- 受保护目录内的 holdout binding receipt 只把 version 锁定到 stable `CandidateBindingV1` digest：snapshot/anchor/package receipt、runner/judge/dataset/validator、core plan、tool/config/execution policy 与 `series-prepare` `stable_identity_digest`。series ID、manifest digest、exact per-series green/runtime receipts/roots/times 和 `pre-holdout` receipt 明确不在该 stable preimage 内。首次或 recovery holdout ordinal 1 前，controller 原子记录当前 series 的新 manifest + fresh pre-holdout + core-completion attempt association。若 series 因基础设施问题 `invalid`，新 series 必须重算出相同 stable digest，但先从零完整重跑 core172，再为新 manifest 生成 fresh pre-holdout，关联后才跑 holdout96；不得续跑、拼接或复用旧 series 的任一成功 run/receipt。完整 series 无论 PASS/FAIL 均标记 `consumed`；放弃同 digest 恢复也不得把该 version 转给新 candidate。
- holdout case 可以在 benchmark 发布时公开，但公开后只保留历史 official score 身份，不能再充当新的盲测集。

holdout 的正式 series root、raw/normalized events、store dump、workspace receipt、failure receipt 和任何含 prompt/seed/label 的材料都继承受保护目录的保密边界。仓库或调优环境只能接收不含 case plaintext、绝对私有路径和失败内容的聚合 report/seal 摘要；dev `FailureArchive` 构建器必须机械拒绝任何 `split=holdout` receipt。

由于 Claude/Codex/OpenCode 三个 host family 同时参与 author/review/evaluation，报告只能称为未参与 skill 调优且执行会话隔离的 synthetic holdout generalization evidence；不得声称底层模型从未生成、审核或见过这些 synthetic case。

**Rationale**: seal 防篡改而非防读取；同一用户下用 `--yolo` 运行受测 CLI 时，tracked plaintext holdout 可被主动读取。仓外隔离和逐 case materialization 才能建立真实的调优阻断。

**Alternatives considered**:

- 把 holdout JSON 直接提交仓库并只记录 SHA：完整但不盲。
- 评测后继续用同一 holdout 调 skill：把 holdout 变成第二个 dev 集。
- 删除失败 holdout：选择性删除会制造幸存者偏差。

## R7 — 正式 3-run 与诊断重试

**Decision**: runner 增加不可混淆的 `primary` 与 `diagnostic` 模式，并以 series purpose 区分 `official-dual` 与 core-only `dev-comparison`。两种模式都显式接收并 honor `--concurrency`；diagnostic 仍永久 score-ineligible。在 comparable baseline 前，`core-plan create` 一次性封存 `CoreExecutionPlanReceipt`：core172 manifest、最终 runner/judge、每 host `tool_identity_digest`、timeout/concurrency、三 ordinal seeds 与 normalized evaluated-child execution/isolation template；skill、purpose、unique roots 和 holdout-only receipt明确排除。每个 primary series 另绑定由唯一 producer 生成的 `FrozenSkillPackageSnapshot` 与 exact-snapshot `SkillPackageValidationReceipt`：producer 对完整 package 做 sorted recursive file inventory、逐文件 digest、020 validator 绑定、原子 materialization 与 immutable anchor；series/child 对 snapshot 重验，不得从 mutable source 读取。`dev-comparison` baseline 与 post-change `official-dual` core leg 必须引用同一个 plan digest。后者同样遵守三宿主 × 三 ordinal 的 primary 完整性规则，但永久不能产生 score/headline，只用于 SC-5 before/after；purpose/unique artifact IDs 不得改变 core child invocation 语义。正式 artifact identity 为：

`(series_id, host, split, primary_ordinal)`，其中 ordinal 只能为 1、2、3。

- `primary` 拒绝 `--only`、`--sample`、`--limit`、holdout case 选择和 case-level agent retry；每个 case 只有一个 terminal result。
- 每个 ordinal 使用全新 artifact root、per-case cwd/store、ephemeral host session 与预注册 case-order seed。
- `diagnostic` 写入独立目录，永不参与 `score`；`diagnostic --split holdout` 直接拒绝。
- 不可逆动作之前都需要 fixed-suite `GreenTestReceipt`：holdout author/review/seal 需要 holdout-pipeline，pre-/final snapshot validation 需要 formal-tooling，`series prepare` 需要 snapshot-bound series-prepare，holdout ordinal 1 需要在 complete core leg 后创建的 series-bound pre-holdout。receipt 固定命令、输出与适用 runner/judge/validator/snapshot/series digests；pre-holdout 还固定 stable candidate digest 与 core-leg receipt set。缺失、失败、post-hoc、cross-series reuse 或任何 digest 漂移都拒绝继续。
- 任一 primary ordinal 覆盖不完整、provenance 漂移、case 重复/缺失或 runner-unavailable，则整个 host × split series 不产生正式分数。恢复必须建立新 series，旧 artifact 不覆盖；若 holdout binding 已存在，恢复还必须重新运行两 split 的完整官方矩阵，旧成功 ordinal 永远不参与新 report。
- seed/preflight 的基础设施重试与 agent case retry 分离并逐 attempt 留收据；正式 case 的模型调用不透明重试为 0。
- `failure-archive` 只接受 complete sealed `dev-comparison` core receipts；`compare` 只读取 baseline/candidate 的 exact core paths。两者写独立 seal、拒绝 holdout plaintext/child receipt，且都不能生成 official score。
- `official-dual` core leg 未完整或 series 已 INVALID 时不得开始 holdout。若尚未开始 holdout ordinal 1，可保留失败 series、用相同 stable CandidateBindingV1 inputs 与同一 core plan 创建新 series ID 并完整重跑 core；此阶段不得创建 binding attempt、消费或重绑 holdout。binding 后 INVALID 时，binding ledger 追加恢复事件；新 series 必须重算出同一 stable candidate digest，从 core172 ordinal 1 起重跑完整 core，随后为新 manifest 生成 fresh pre-holdout 并关联 ledger，才可重跑 holdout96。旧 manifest、pre-holdout 或成功 run receipt 均不得复用。

**Rationale**: 当前 runner 会覆盖同名 raw/report、会对非 timeout 失败自动重试，并允许 `--only` 定向补分；这些行为不适合正式计分。新模式把开发便利保留在 diagnostics，同时让主分不可被事后挑选。

**Alternatives considered**:

- “later run wins”：会把有利重试写成主分。
- 缺题时使用已完成 case 的部分分母：不同 run 不可比。
- 自动补齐一个失败 ordinal：无法区分恢复和挑选；新 series 更诚实。

## R8 — 正式分数与门槛

**Decision**: 每个 host × split × module 先计算三个完整 run 的率，再取排序后的中间值。正式结果是两个并列的 score family：

- `dev/regression score`
- `generalization score`

不得生成二者的合并分数。每个宿主独立按 SC-1～SC-4 的适用门验收；跨宿主平均/总计仅为 supplemental，不得产生 PASS。official report 必须绑定单一完整 series、stable `CandidateBindingV1` digest、关联该 exact manifest/core completion/fresh pre-holdout 的 holdout-binding receipt、frozen skill snapshot/anchor、core plan、skill package validation、series-prepare/pre-holdout green tests、protected execution 与 workspace canary digests。binding 后 INVALID 的历史 series 只能出现在 non-scoring binding-ledger evidence 中。source/scenario bias cell 同时报 numerator、denominator、独立 case 数与 low-N；三个 ordinal 不能冒充三个独立样本。具体规则见 `contracts/scoring-report.md`。

terminal `runner-error` 使用保守且对称的 gate 映射：正例模块中它不计 pass；负例模块中它单独报告为 runner-error，同时计入 official negative gate 的失败分子。这样未知结果不能在负例分母中把误触发率稀释为更好。

**Rationale**: 用户明确选择 A。per-host gate 防止 Claude/Codex/OpenCode 其中一个持续失败却被另外两个平均掩盖。

**Alternatives considered**:

- holdout 只报分不设门：用户否决。
- 三宿主合并后判门：用户否决。
- 合并 dev 与 holdout：用户明确禁止。

## R9 — Codex 文件 trap 等价性

**Decision**: Codex formal case 同时设置 `cmd.Dir=caseDir` 和 `codex exec -C caseDir`，并使用 ephemeral session。不存在可遗漏的独立 preflight 命令；`series prepare` 在 final skill/template/worker slots 冻结后，自动让每个可执行 host × slot 读取自己的无秘密 canary workspace并绑定 cwd/path/file/identity/template/boundary digests。任一 slot 看不到 staged file 或与 prepared slot 漂移时，series 不可 seal，不允许退化为“memory vs empty environment”后继续比较。

**Rationale**: 当前 runner 明示 Codex 在共享 scratch 下运行，file-backed trap 对 Codex 不等价。本机 Codex CLI 支持 `-C/--cd`，因此可在 adapter 修复，不需引擎变化。

**Alternatives considered**:

- 保留现状并在报告注记：不满足宿主同权比较。
- 为 Codex 删除 file-backed trap：改变数据集分母并降低覆盖。
- 让 trap 访问仓库根：扩大文件与 holdout 泄漏面。

## R10 — 可重判 artifact 与安全失败

**Decision**: 每 case 保存经秘密过滤的 agent stdout event stream、normalized events、post-turn store dump、workspace digest、attempt receipt、judge-rule digest 和 verdict。正式 JSON 只写闭集错误码与 stderr digest，绝不内嵌任意 stderr。路径字段与 case ID 必须通过 containment 校验，LLM 生成的 `../`、绝对路径、分隔符逃逸和 symlink 均 fail closed。`GreenTestReceipt` 也只保存固定 argv、退出码与 sanitized output digest，不保存原始 settings、环境值或任意 stderr。

当前 `engram add` 与 MCP `memory_write` 均不能写入结构化 `memory.Entry.EventDate`。048 不扩展这些 adapter contract：v2 `SeedMemory.event_date` 若非空，runner 必须验证 ISO-8601 日期并把 `[event_date=YYYY-MM-DD]` 作为确定性前缀写入 seed content，再沿现有 `engram add` 播种；receipt 同时记录原字段、rendered-content digest 与 engine-event-date-unsupported 标记。静默丢弃该字段或宣称写入了结构化 EventDate 都是失败。

manifest 的全部字段（尤其 question count、case set、digests、provenance）必须在计算 manifest digest 与写 seal 前填完；seal 后禁止补字段。

在 WSL2，所有长时真实 author/review、baseline、primary 与 recovery CLI 操作必须使用项目规定的 detached `setsid` 启动方式，并保留 session scratchpad 日志 digest、exit-file digest/exit code 与 launch-mode receipt；前台 stdout EOF 不是完成证据。该操作 receipt 只补充 provenance，不能替代 green/package/snapshot/series receipt。

**Rationale**: 当前 runner 不保存 store dump，无法从历史 raw 忠实复判 write 结果；它还会把截断 stderr 写入 verdict，且 generated file path 未做逃逸校验。

**Alternatives considered**:

- 只保存 verdict：无法独立重判。
- 保存完整未过滤 stderr/config：违反 secret safety。
- 信任生成模型输出的路径：扩大 destructive/path-escape 风险。

## R11 — 宪法与并行 feature 边界

**Decision**: 所有实现限定在 `cmd/skill-eval/`、`skills/engram/`、`docs/` 与 `specs/048-*`。`memory/ embedding/ provider/ store/ internal/` 必须零改动；LoCoMo/LongMemEval 算法与 `cmd/locomo-bench` 不在本 feature 范围。活动 sibling 042 修改 `cmd/locomo-bench` 的评测效用路径，048 只修改 `cmd/skill-eval`，当前无文件交集；进入 tasks/implement 前仍需再次核对 worktree 与新提交。

**Rationale**: 048 是 skill 与评测 adapter 能力，公共引擎 API 已足够。agent inference 使用维护者既有授权端点，但 engram MCP/store/judge 路径保持本地离线；不得误称整个 agent 推理链“零网络”。

**Alternatives considered**:

- 为 namespace/隔离修改引擎：当前 per-case data-dir 已可通过公共 CLI/MCP 完成。
- 借付费云 reranker/recall 涨分：违反项目 death rule，且与触发评测无关。
- 与 042 共用 locomo utility artifact：两个 feature 的问题、分母和 contract 不同。
