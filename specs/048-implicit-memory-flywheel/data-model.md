# Data Model: 048 implicit-memory-flywheel

**Date**: 2026-09-01
**Contract scope**: dataset v2、三 CLI provenance/盲审、sealed holdout、正式 3-run artifact 与双分数报告。

## 1. Enumerations

### Split

- `dev-regression`: 当前 172 条，正式开发/回归分。
- `holdout`: 新增 96 条，正式泛化分；禁止用于 skill 调优。

### Module

- `implicit-write-pos`
- `implicit-write-neg`
- `implicit-read-pos`
- `implicit-read-neg`
- `trap-read-pos`
- `trap-write-neg`
- `trap-read-neg`
- `regression`（只允许 `dev-regression`）

### Host

- `claude`
- `codex`
- `opencode`

Host 表示 CLI 宿主，不等于底层 model/provider。

### ScenarioBucket

holdout 的闭集语义桶：

- `durable-preference`
- `identity-biography`
- `project-convention`
- `environment-tooling`
- `supersession-time`
- `transience-boundary`
- `attribution-secret-boundary`
- `workspace-session-conflict`

它是预注册覆盖/偏差审计维度，不是新的正式分数门。每个 bucket 的具体文本仍必须符合其 module 的可观察机器规则。

### RunMode

- `primary`: sealed、全量、不可定向重试；`official-dual` 可正式计分，`dev-comparison` 仅作 SC-5 可比运行。
- `diagnostic`: 开发诊断，不进入任何正式分数。

### SeriesPurpose

- `official-dual`: 绑定 core172 + holdout96，可生成两项正式分数。
- `dev-comparison`: 只绑定 core172，仍执行三宿主 × 三 ordinal 的 primary 完整性规则，但永久禁止进入 `OfficialScoreReport`；仅用于 SC-5 可比 before/after。

### LifecycleState

- Candidate: `generated | schema-valid | blind-reviewed | accepted | rejected`
- Holdout: `assembled | sealed | frozen | consumed | superseded`
- Run: `prepared | running | complete | invalid | sealed`

## 2. TriggerCaseV2

一条可确定性判分的测试场景。

| Field | Type | Required | Rules |
|---|---|---:|---|
| `id` | string | yes | `^[a-z0-9][a-z0-9._-]{0,63}$`; 全数据集唯一；禁止 `.`/`..` |
| `schema_version` | integer | yes | v2 为 `2` |
| `split` | Split | yes | legacy loader 可把旧文件默认映射为 `dev-regression`，写出时必须显式 |
| `score_membership` | `core172 | dev-extension | holdout96` | yes | legacy 172 默认 core172；只有 core172/holdout96 进入两项正式分数；extension 只做 append-only 全量回归 |
| `module` | Module | yes | `regression` 不得进入 holdout |
| `lang` | `zh | en | mixed` | yes | holdout 配额只把 `mixed` 按主要用户语言计入一个桶并记录判定依据 |
| `scenario_bucket` | ScenarioBucket | holdout yes | 预注册闭集；dev 可为 `null`/legacy-derived；不得由 author 自造字符串 |
| `category` | string | yes | 稳定的 bucket 内 taxonomy id；trap 必须以 `trap-` 开头 |
| `family_id` | string/null | conditional | retained legacy core172 在 `pre-index` 校验阶段可为空，并由冻结 `DevFamilyIndex` 提供外部映射。holdout author candidate 必须为 null；controller 只从 canonical `BlindCandidateV1` 语义投影计算 `hfam-<sha256>`，并且只在 admission CAS 成功时写入最终 sealed case（sealed/admitted holdout 必须非空）。任何 author 自报的非空值均为 schema failure。family-aware 校验后每个 retained/admitted ID 必须恰好映射到一个 family，且不得跨 split 重复 |
| `translation_of` | string/null | yes | holdout 必须为 null；禁止逐句翻译配对 |
| `prompt` | string | conditional | 单轮 case 使用；与 `turns` 二选一 |
| `turns` | []Turn | conditional | 多轮/跨会话 case 使用；至少一个 user turn |
| `seed_memories` | []SeedMemory | yes | read/trap 的确定性前置记忆；write case 通常为空 |
| `workspace_files` | []WorkspaceFile | yes | 相对路径、无 symlink、必须 containment-safe |
| `expect` | ExpectedBehavior | yes | 只允许 runner 可观察规则 |
| `source` | string | yes | dev 使用 `initial/flywheel-*`；holdout 使用 batch id |
| `status` | `active | disputed | superseded` | yes | dev-extension 可使用该状态；frozen core172 的 payload/status 不得回写，争议/修订关系仅写入 append-only extension manifest metadata |
| `superseded_by` | string/null | yes | frozen core172 必须保持 null；core/extension successor 关系以 dev-extension manifest 的 immutable `extension_lineage` 为准，不删除旧 ID |
| `authoring` | AuthoringReceipt/null | yes | holdout 必须非 null，dev 可 null |
| `reviews` | [ReviewRecord, ReviewRecord] | conditional | holdout 必须恰好两个非作者 reviewer |

### Turn

| Field | Type | Rules |
|---|---|---|
| `session` | integer | 从 1 开始、单调不减；session 增加表示新会话 |
| `role` | `user | assistant` | 最后一个被评测输入必须是 user |
| `content` | string | 非空；不得包含真实凭证 |
| `setup_only` | boolean | true 的 assistant/setup turn 不参与最终答案判分 |

### SeedMemory

| Field | Type | Rules |
|---|---|---|
| `name` | string | case 内唯一 |
| `content` | string | 可公开测试事实，不得是真实用户记忆或凭证 |
| `event_date` | string/null | 若用于 supersede/time trap，必须为 ISO-8601 `YYYY-MM-DD`；048 runner 将其规范化为 seed content 前缀 `[event_date=YYYY-MM-DD]`，不得静默丢弃或声称已写入结构化 engine EventDate |

### WorkspaceFile

| Field | Type | Rules |
|---|---|---|
| `path` | string | 相对路径；清理后仍须位于 caseDir；禁止绝对路径、`..`、NUL、symlink |
| `content` | string | UTF-8，无秘密 |
| `sha256` | string | materialize 后复核 |

### ExpectedBehavior

| Field | Type | Rules |
|---|---|---|
| `trigger` | boolean | 是否应产生 engram 操作 |
| `allowed_ops` | []string | `write/search/get/list/delete` 的闭集 |
| `min_calls` / `max_calls` | integer | 明确一次、多事实合并或零调用口径 |
| `store_include` | []alternation | 每组至少匹配一个候选词 |
| `store_exclude` | []string | 任一命中即失败 |
| `answer_include` | []alternation | 确定性事实/告知词规则 |
| `answer_exclude` | []string | canary、秘密、旧值等不得外显 |
| `not_found` | boolean | 空结果必须如实说明 |
| `observable` | string | 人可读解释，不参与自由裁量判分 |

规则不能要求“回答好”“语气自然”等主观判断。规范化 review 必须对除 `observable` 外的机器字段完全一致。

## 3. AuthoringPromptReceipt

| Field | Type | Rules |
|---|---|---|
| `prompt_id` | string | `dev-family-index-review-v1`、`holdout-authoring-v1` 或 `holdout-review-v1` |
| `version` | integer | 从 1 开始 |
| `digest_algorithm` | string | `lf-normalized-sha256-v1` |
| `sha256` | string | 64 位 lowercase hex |
| `quota_plan_digest` | string | 绑定 R2 配额矩阵 |

同一个 holdout seal 只允许一种 author prompt digest 和一种 review prompt digest。

## 3.1 DevFamilyIndex

为现有 172 条 v1 payload 追加、但不改变其 prompt/label 的 metadata index。它是 holdout 跨 split 去重的比较基线。

| Field | Type | Rules |
|---|---|---|
| `family_id` | string | 稳定且唯一；可关联同一中英镜像/近似语义的多个 legacy case |
| `case_ids` | []string | 至少一个 retained dev case ID；排序且唯一 |
| `normalized_prompt_digest` | string | 用于确定性 exact/near-exact 检查 |
| `language_members` | map | 记录 zh/en 成员，不能把镜像当独立 family |
| `taxonomy` | []string | 模块/category 摘要，不含秘密 |
| `derivation_receipt` | object | 自动化算法/version/digest；不改动 legacy payload |

`DevFamilyIndex` 由 `dev-family-index-v2` 可复跑机器协议生成并冻结。core172 validation 明确分两阶段：`pre-index` 只验证 manifest-authoritative ID、精确矩阵、legacy 020 语义、机器规则与路径安全，不要求 legacy payload 已有 `family_id`；index 生成后再执行 `family-aware` validation，要求 172 个 retained ID 均且仅均映射到一个 family，并完成跨 split/镜像检查。

1. 按 case ID 排序读取 core172；提取 prompt/用户 turns、seed name/content 和机器规则。
2. 规范化规则固定为 LF、ASCII 小写、Unicode 空白折叠、对象 key/集合字段排序；记录 normalizer version 与每 case digest。
3. 规范化摘要完全相同者直接连边。不同语言且 module、category、机器规则形状 digest 相同者进入 mirror-candidate 列表。
4. 每个 mirror candidate 使用冻结的 `dev-family-index-review-v1` prompt，由 Claude/Codex/OpenCode 三 lane 在独立新会话中审阅；review 调度使用受 `--concurrency` 约束的 bounded worker pool。v2 连边条件：三者均返回 `same_family=true`，且三个 `canonical_family_digest` 主题 slug **两两共享至少一个 `-` 分词**（topic alignment；被取代的 v1 要求 slug 字节全等，首跑实测 40/52 全票一致对仅因措辞粒度被拒、0 真语义分歧，v1 产物存于 `receipts/dev-family-index-v1-superseded.json`）。`same_family` 分歧、空 slug、或 token 无两两交集不连边，不允许人工改表。
5. 对连通分量的排序 case ID 计算稳定 family ID；singleton 也必须有 ID。`derivation_receipt` 绑定 input payload、算法、normalizer、candidate pair list、review prompt、三 lane provenance、冻结 concurrency、observed max-in-flight、逐 pair 决定和最终 index digest；当 concurrency>1 时验证运行必须观察到 overlap。

holdout review 接收该 index 与当前 accepted-holdout index 的安全 family 摘要 payload 及 digest，不接收 author identity；任一 reviewer 认定 candidate 与已有 family 重合或构成翻译镜像时，candidate 必须拒绝。这样 `family_id` 的“不跨 split”不依赖原 v1 case 已经显式带该字段，也不信任 author 自报的 family ID。

### 3.2 FamilySummaryPayload

供 novelty reviewer 实际读取的匿名、label-free payload；仅给 digest 而不给 payload 不足以完成语义去重。

| Field | Type | Rules |
|---|---|---|
| `schema_version` | integer | `1` |
| `scope` | `dev-regression | holdout-accepted` | 一个 payload 只含一个 scope |
| `revision` | integer | dev index 固定 revision；accepted-holdout 单调增加并参与 CAS |
| `projection_version` | string | 固定 `blind-family-summary-v1` |
| `source_state_digest` | string | dev scope 必须等于冻结 `DevFamilyIndex` digest；accepted scope 必须等于当前 accepted-family state digest |
| `source_family_count` | integer | 必须等于 source state 中的 family 数量 |
| `entries` | []FamilySummaryEntry | 按 `family_id` 的 UTF-8 bytes 排序且唯一 |
| `entries_root_digest` | string | 按顺序连接全部 `entry_digest` 后计算；空集使用该算法的固定空 root |
| `payload_digest` | string | 对 canonical payload（排除本字段）计算 |

`FamilySummaryEntry` 只允许 `family_id`（reviewer 可返回的 controller-generated opaque stable reference）、`language_members`、排序后的 `blind_semantic_payloads` 与 `entry_digest`；`entry_digest` 的 preimage 排除其自身。每个 blind semantic payload 是 case 的 prompt/user turns、seed name/content 与安全 workspace path/content context 的 canonical projection；它必须删除 module/category/scenario/expect/machine rules、author/reviewer identity、quota/batch/source/attempt、provenance、receipt 与任何可推导 author lane/私有 label 的字段。accepted-holdout 的 `family_id` 固定为 `hfam-` 加 `sha256("holdout-family-id-v1\0" || canonical blind-semantic-projection bytes)` 的 lowercase hex；author-provided family ID 从不进入该 preimage。dev family reference 同样由 controller 从冻结 index component 生成，不复用 author/model/source 字符串。

validator 必须从完整 source state 重新投影 summary，要求 source family 与 summary entry 按 controller family reference 一一对应、数量相等、无缺失/额外/重复，并逐项复算 `entry_digest`、`entries_root_digest`、`source_state_digest` 与 `payload_digest`。仅删除一个 source family 后重算 summary 自身 digest 仍然无效。reviewer 返回的非空 `nearest_family_id` 必须出现在其实际收到的两个 payload 之一；controller 私下维护该 opaque reference 到真实 family record 的映射。

## 4. ToolProvenance

| Field | Type | Rules |
|---|---|---|
| `host` | Host | CLI 宿主 |
| `cli_version` | string | 运行时 `--version` 原样的安全标识 |
| `binary_sha256` | string | 实际 executable digest |
| `invocation_template_id` | string | 不包含 prompt/路径/秘密的固定模板版本 |
| `invocation_template_digest` | string | 绑定 flags 语义 |
| `requested_profile` | string/null | 如 `aly_qwen_w`、`aq`；仅名称 |
| `requested_model` | string/null | OpenCode2 必须明确免费模型 id |
| `resolved_model` | string | CLI 事件可信给出；否则字面值 `unavailable` |
| `billing_class` | `existing-authorized | free | unknown` | OpenCode2 authoring/review/formal run 必须 `free` |
| `settings_digest` | string/null | settings/config 脱敏摘要 digest；不存原文 |
| `endpoint_digest` | string/null | endpoint 字符串 digest；不存 URL |
| `source_revision` | string | 仅 `cmd/skill-eval` runner source subtree 或已构建 runner binary 的 revision/digest；不得覆盖 skill、dataset、docs/specs 或 artifact |
| `captured_at` | RFC3339 UTC | 运行时采集 |
| `tool_identity_digest` | string | 仅对稳定身份字段计算的规范化 digest；供跨 series 可比性校验 |

任何 token/key/password、env value、完整 endpoint、完整 settings/config 或任意 stderr 都不是本 entity 的字段。

`tool_identity_digest` 覆盖 `host`、CLI version、binary digest、invocation-template id/digest、requested profile/model、resolved model、billing class、settings/endpoint digest 与上述 runner-only source revision。它明确排除 `skills/engram/**`、datasets、docs/specs、snapshot/receipt/artifact 内容、`captured_at`、series/run/case/artifact ID、输出路径和任何瞬时状态；`captured_at` 仍保留在逐次 provenance receipt 中。SC-5 只允许比较每个 host 的该稳定 digest，不能直接比较包含采集时间的完整 `ToolProvenance` 序列化 digest。

Holdout author/review batch 的额外不变式（2026-09-01 修订，维护者统一底层模型后）：每个 host 在同一 batch 的所有 author 与 reviewer attempt 中只能有一个非 `unavailable` resolved model，且必须逐 attempt 记录诚实 provenance；三宿主 harness（claude-code / codex / opencode2 的不同 system prompt、工具面与会话语义）是独立性的主轴，不再要求 resolved model 两两不同——维护者已拍板三条 lane 统一底层模型（当前为百炼 qwen3.8-flash）。lane 内漂移或 `unavailable` 均使整批不可 seal，且无人工 waiver；同一 resolved model 出现在多个 host 是如实记录的事实，不是失败。盲审独立性由 host 互异 + label-blind envelope 承担，不依赖模型差异。

## 5. AuthoringReceipt

| Field | Type | Rules |
|---|---|---|
| `attempt_id` | string | 必须引用唯一 author `AttemptStarted`/`AttemptTerminal` 对 |
| `batch_id` | string | 一次统一 prompt/quota 的生成批次 |
| `quota_slot` | string | 私有 authoring receipt；canonical slot 绑定 author × module × lang × scenario_bucket，禁止进入 review envelope |
| `quota_slot_digest` | string | 对完整 private quota slot 计算；必须匹配 attempt start 与 admission receipt |
| `author` | ToolProvenance | 必须与两个 reviewer host 不同 |
| `prompt` | AuthoringPromptReceipt | authoring prompt |
| `candidate_transcript_digest` | string | 私有、秘密过滤后 transcript 的 digest |
| `private_candidate_digest` | string | controller-only 完整 candidate digest；禁止进入 review envelope |
| `blind_candidate_digest` | string | 只对 `ReviewEnvelope.candidate` 的 canonical de-labeled projection 计算；reviewer 可见 |
| `attempt_ordinal` | integer | rejected 重生也保留独立 ordinal |
| `receipt_digest` | string | immutable receipt digest；preimage 排除本字段 |

## 6. ReviewEnvelope and ReviewRecord

### BlindCandidateV1

`ReviewEnvelope.candidate` 不是开放式 subset，而是递归闭集 schema。顶层只允许以下字段：

| Field | Type | Rules |
|---|---|---|
| `schema_version` | string | 固定 `blind-candidate-v1` |
| `prompt` | string/null | 与 `turns` 恰好一个非空；逐字节复制已验证 private candidate |
| `turns` | []BlindTurn/null | 与 `prompt` 恰好一个非空；保持原顺序 |
| `seed_memories` | []BlindSeedMemory | 保持原顺序 |
| `workspace_files` | []BlindWorkspaceFile | 按 normalized relative path 排序 |

`BlindTurn` 只允许 `session`、`role`、`content`、`setup_only`；`BlindSeedMemory` 只允许 `name`、`content`、`event_date`；`BlindWorkspaceFile` 只允许 `path`、`content`、`sha256`。所有层级都拒绝 unknown key、别名、嵌套扩展、重复 JSON key、非 UTF-8、NUL 和 schema 未声明的 `additionalProperties`。它不允许 `id`、`family_id`、`translation_of`、split/membership/source/status、module/lang/scenario/category/expect、quota、author/reviewer/provenance/receipt/attempt 字段，也没有自由格式的 “safe context” 扩展点。

### ReviewEnvelope

发送给两个非作者 reviewer 的瞬时匿名对象。

| Field | Type | Rules |
|---|---|---|
| `candidate` | BlindCandidateV1 | 必须逐层通过上述 closed-schema validator；不得接收或保留任何额外 “safe context” 字段 |
| `blind_candidate_digest` | string | 两 reviewer 必须完全相同；必须等于 exact `candidate` projection digest |
| `review_prompt_digest` | string | 绑定冻结 review prompt |
| `dev_family_summary` | FamilySummaryPayload | 实际 materialize 给 reviewer 的匿名 dev family 摘要 |
| `dev_family_summary_digest` | string | 必须等于实际 materialized dev `FamilySummaryPayload.payload_digest`；其 source index 必须等于冻结 `DevFamilyIndex` |
| `accepted_holdout_family_summary` | FamilySummaryPayload | 实际 materialize 给 reviewer 的匿名 accepted-holdout 摘要 |
| `accepted_holdout_family_summary_digest` | string | 绑定发送时已接受 family 摘要 |
| `accepted_holdout_family_revision` | integer | 发送时单调 revision；admission 时必须以 CAS 复核 |
| `envelope_digest` | string | 对上述匿名内容计算；持久 review receipt 只引用此 digest |

`blind_candidate_digest` 使用 `agent-memory-trigger-canonical-json-v1`；preimage 恰好是完整通过 closed-schema validation 的 `BlindCandidateV1`，不得先接受 unknown field 再在 digest 前丢弃。两个私有 candidate 只要 blind projection 相同，即使 author label/rule/slot/family ID 不同，也必须得到完全相同的 reviewer-visible candidate digest 与 envelope candidate component。两个 family-summary digest 必须逐字节匹配实际 materialize 的 payload；digest-only envelope 非法。

reviewer 不接收 author 提议的 expected label、module/lang/scenario/category 或 machine rules；它必须从 blind input 独立产出这些判定。序列化后出现 author、author model/config、batch/source、author-specific quota slot、candidate ordinal、prior-review、private candidate digest 或任何 author-proposed label/rule 字段即 schema failure。

### ReviewRecord

| Field | Type | Rules |
|---|---|---|
| `attempt_id` | string | 必须引用唯一 review `AttemptStarted`/`AttemptTerminal` 对 |
| `author_attempt_id` | string | 绑定产生该 blind candidate 的 author attempt |
| `review_id` | string | batch 内唯一 |
| `review_envelope_digest` | string | 两 reviewer 对同一 candidate 必须相同 |
| `blind_candidate_digest` | string | 两 reviewer 必须相同且等于 envelope 中的 canonical blind digest |
| `reviewer` | ToolProvenance | host ≠ author；两个 reviewer host 互异 |
| `review_prompt` | AuthoringPromptReceipt | 固定 review prompt |
| `verdict` | `accept | reject` | 非法输出等价 reject |
| `inferred_module` / `inferred_lang` / `inferred_scenario_bucket` / `inferred_category` | enum/string | reviewer 从 blind input 独立判定；不得由 envelope 提供 |
| `inferred_expect` | ExpectedBehavior | 保存 reviewer 实际返回的完整机器规则；禁止只保存 opaque digest |
| `normalized_label_digest` | string | validator 从 inferred module/lang/scenario/category 与 `inferred_expect` 的全部机器字段（排除 `observable`）重算；两 reviewer 的 canonical 值必须逐字段相同 |
| `novel` | boolean | 必须为 true 才可接受 |
| `nearest_family_id` | string/null | reviewer 认定与 dev 或 accepted holdout 重合时必须给出；无重合时可为 null |
| `nearest_family_scope` | `dev-regression | holdout-accepted | null` | 与 `nearest_family_id` 同时为空或非空 |
| `reason_code` | string | 闭集，不保存自由文本 stderr |
| `reviewed_at` | RFC3339 UTC | — |
| `receipt_digest` | string | immutable review receipt digest；preimage 排除本字段 |

Acceptance 条件：两个 verdict 都为 `accept`、均 `novel=true`、两个 `normalized_label_digest` 及其 exact inferred values 相同，且 controller 在 reviewer 提交后将 module/lang/scenario/category/机器规则与私有 author candidate 比对，并将 author/module/lang/scenario 与完整 private quota slot 比对。任一不一致即 rejected；controller 不把 private slot/label 回显给 reviewer。

### AdmissionReceipt

每次两条 review 都完成并进入 accepted-family CAS 时，controller 都 append 一条 immutable receipt；`stale` 也必须记录，但不能改变 accepted state。

| Field | Type | Rules |
|---|---|---|
| `admission_sequence` | integer | 从 1 连续递增；在 CAS lock 内分配 |
| `previous_admission_receipt_digest` | string/null | 第 1 条为 null；其余指向前一条 receipt |
| `author_attempt_id` / `authoring_receipt_digest` | string | 精确引用一个 author attempt/receipt |
| `review_attempt_ids` / `review_record_digests` | [string,string] | 恰好两个互异非作者 review attempt，按 host 排序并逐项绑定 |
| `private_candidate_digest` / `blind_candidate_digest` | string | 必须匹配 author/review receipts |
| `quota_slot_digest` / `normalized_label_digest` | string | 绑定完整四维 slot 与两 reviewer 一致的 exact label/rules |
| `reviewed_summary_revision` / `reviewed_summary_digest` / `reviewed_source_state_digest` | integer/string | 必须等于两 reviewer 实际收到的 accepted-holdout summary |
| `observed_pre_revision` / `observed_pre_state_digest` | integer/string | CAS lock 内读取的当前 accepted-family state |
| `cas_result` | `committed | stale` | reviewed revision/state 与 observed pre 完全相同才可 committed |
| `final_case_id` / `controller_family_id` / `family_entry_digest` | string/null | committed 必须非空并绑定唯一 admitted case；stale 必须全为 null |
| `post_revision` / `post_state_digest` | integer/string | committed 为 pre+1 和加入 exact family entry 后的 state；stale 必须逐字节等于 observed pre |
| `receipt_digest` | string | 对本 receipt（排除本字段）计算；成为下一条 receipt 的链指针 |

accepted-family state digest 的 preimage 是 `revision` 与按 controller family ID 排序的全部 `FamilySummaryEntry.entry_digest`，不包含 admission receipt 自身，避免自引用。Seal validation 从空 state 顺序重放完整 AdmissionReceipt chain，验证每次 CAS 结果、pre/post state、attempt/receipt join、controller family ID、最终 state 与 96 个 committed case 一一对应；缺失、重排、分叉、孤儿或重复 commit 均失败。

### AuthorReviewIsolationReceipt

每个 author/reviewer attempt 的 private receipt，证明匿名 schema 之外还存在进程级盲审边界。它不保存 plaintext candidate、envelope、原始路径、用户名或凭证。

| Field | Type | Rules |
|---|---|---|
| `stage` | `author | review` | 与实际 attempt 一致 |
| `host` / `attempt_id` | Host/string | 绑定唯一 child attempt |
| `input_digest` | string | author 为 quota/prompt digest；review 为 anonymous envelope digest |
| `child_identity_digest` | string | exact child 的安全 identity digest |
| `execution_template_digest` | string | 与该 stage 实际 invocation 一致 |
| `stage_isolation_config_digest` | string | per-attempt user/container/mount/ACL-equivalent template 的安全 digest |
| `ephemeral_state_root_digest` | string | 每 attempt 新建，不与其他 author/review/formal 状态根共享 |
| `own_workspace_digest` | string | 只包含当前 input/output materialization |
| `probe_matrix_digest` | string | canonical closed-result matrix |
| `receipt_digest` | string | immutable digest |

exact child 必须能读取 own workspace/input；必须拒绝 private holdout root traversal/list/read、generation audit read、author receipt read、prior review read，以及 concurrency>1 时每个 active sibling workspace read。每个 denied probe 均需 controller-side target-existence/content/policy proof，target 必须继承真实 parent access policy。缺 probe、缺 controller proof、可读禁区、不可读 own input、state root 复用或 template/identity/policy mismatch 均使 attempt rejected，且出现隔离或模型身份违规的 batch 不可 seal；所有已启动 attempt（candidate-ready、accept、reject、parse-error、timeout 等）以及每个 stale-CAS AdmissionReceipt 都必须进入 sealed aggregate，不能只聚合 committed case。

### AuthorReviewAttemptLedgerV1

author/review audit 是一个 batch 内唯一的 append-only event chain，不是可原地写回的 provisional row。controller 必须在 child launch **前** append 一条 `AttemptStarted`；之后无论 child 是否实际 launch，必须再 append 恰好一条对应的 `AttemptTerminal`。event sequence 从 1 连续递增、每条 `event_digest` 指向前一条 event；不得删除、覆盖、重编号、合并或补写历史 event。

#### AttemptStarted

| Field | Type | Rules |
|---|---|---|
| `event_kind` | literal | `attempt-started` |
| `event_sequence` / `previous_event_digest` | integer/string/null | sequence 从 1 连续；首条 previous 为 null，否则必须等于前一 event digest |
| `attempt_id` | string | batch 内唯一；后续 terminal/receipt 的 join key |
| `stage` / `host` | enum/Host | `author|review`；不得变更 |
| `tool_identity_digest` / `resolved_model` | string | 绑定启动时 provenance |
| `prompt_input_digest` | string | author 为 prompt+private quota-slot digest；review 为 anonymous envelope digest |
| `author_attempt_id` | string/null | review 必须引用其 author attempt；author 必须 null |
| `event_digest` | string | 对本 event（排除本字段）canonical digest |

#### AttemptTerminal

| Field | Type | Rules |
|---|---|---|
| `event_kind` | literal | `attempt-terminal` |
| `event_sequence` / `previous_event_digest` | integer/string | 紧接前一 event，链不可断裂 |
| `attempt_id` / `started_event_digest` | string | 必须精确引用唯一且尚未 terminal 的 AttemptStarted |
| `stage` / `host` | enum/Host | 必须逐字节匹配 started event |
| `isolation_receipt_digest` | string/null | child 已 launch 时必须非空且引用同 attempt 的 isolation receipt；未 launch 才可 null |
| `output_transcript_digest` | string/null | 有输出时必须；保持 private |
| `authoring_receipt_digest` | string/null | author 且输出 schema-valid candidate 时必须引用其 `AuthoringReceipt`；review 必须 null |
| `review_record_digest` | string/null | review 且输出 schema-valid review 时必须引用其 `ReviewRecord`；author 必须 null |
| `terminal_outcome` | closed enum | author=`candidate-ready|rejected|parse-error|timeout|runner-error|model-identity-violation|isolation-violation`；review=`accept|reject|parse-error|timeout|runner-error|model-identity-violation|isolation-violation`。stale CAS 是 immutable `AdmissionReceipt` outcome，不回写 review terminal；case 是否 accepted 只由 `AdmissionReceipt.cas_result=committed` 表示 |
| `reason_code` | string | 闭集 |
| `event_digest` | string | 对本 event（排除本字段）canonical digest |

每个 author/review receipt 必须与一个 terminal event 一一 join；一个 started event 只可有一个 terminal event；`candidate-ready` author 必须恰有两个 review starts，且每个 review start 都引用它。seal validator 从 event 1 重放 chain，验证 start-before-launch、每 start 有且仅有一个 terminal、stage/host/input/provenance/receipt join、launch/isolation 关系与 closed outcome/reason counts。任一 orphan receipt、孤儿 terminal、未 terminal start、双 terminal、event gap 或链分叉均失败。

## 7. DatasetManifestV2

| Field | Type | Rules |
|---|---|---|
| `schema_version` | integer | `2` |
| `canonicalization` | string | 固定 `agent-memory-trigger-canonical-json-v1`；unknown version fail closed |
| `dataset_id` | string | `agent-memory-trigger-bench` |
| `dataset_version` | string | immutable version |
| `split` | Split | 一个 manifest 只绑定一个 split |
| `score_membership` | `core172 | dev-extension | holdout96` | 一个 manifest 只绑定一种 membership；core 与 extension 必须是两个 manifest |
| `case_count` | integer | core172=172；holdout96=96；extension 为当前 append-only 数；seal 前填完 |
| `module_counts` | map | 必须与预注册矩阵完全一致；core172 为 iw 28/28、ir 28/28、trap 18/6/4、regression 16/16 |
| `language_counts` | map | holdout 必须 zh=48、en=48；core172 的 140 条 implicit/trap payload 按冻结 policy 为 zh=72、en=68；32 条 legacy regression 无 lang 字段，必须单列 `regression_unclassified=32`，不得伪计入 zh/en |
| `author_counts` | map | holdout 必须各 32 |
| `scenario_bucket_counts` | map ScenarioBucket→integer | holdout 八个 bucket 均为 12 |
| `scenario_author_counts` | map ScenarioBucket→map Host→integer | holdout 每 bucket 的每 host 均为 4 |
| `scenario_language_counts` | map ScenarioBucket→map lang→integer | holdout 每 bucket 均为 zh=6、en=6 |
| `scenario_module_coverage` | map ScenarioBucket→object | 每 bucket 恰 10 implicit + 2 trap、四个 implicit module 各至少 1、恰 1 trap-read-pos 与 1 条 trap negative |
| `case_ids` | []string | 排序、唯一 |
| `case_ids_digest` | string | 排序列表 receipt |
| `payload_files` | []PayloadFileV1 | 按 `relative_path` raw UTF-8 bytes 排序；每个 case payload 必须由这里唯一命名，禁止目录遍历推断 |
| `payload_digest` | string | 仅对 `payload_files` 命名、排序后的 sealed case files 计算；明确不含 manifest 与 seal |
| `dev_family_index_digest` | string/null | holdout 必须绑定冻结 index；dev manifest 可 null |
| `author_review_resolved_models` | map Host→string/null | holdout 必须恰好三个非 `unavailable` 的 host-stable resolved model，并与 every-attempt audit/author/reviewer receipt 一致；同 host 不得漂移；跨 host 允许相同（统一底层模型拍板） |
| `author_prompt` | AuthoringPromptReceipt/null | holdout 必须 |
| `review_prompt` | AuthoringPromptReceipt/null | holdout 必须 |
| `author_review_state_roots_digest` | string/null | holdout 必须；排序后的全部已启动 ephemeral author/review state-root digests，供 formal series 验证不共享 |
| `author_review_isolation_digest` | string/null | holdout 必须；全部已启动 author + review isolation receipts 的 aggregate digest，包括 rejected/stale/failed attempts |
| `author_review_attempt_event_chain_digest` | string/null | holdout 必须；`AuthorReviewAttemptLedgerV1` 最后一 event digest |
| `author_review_attempt_started_count` / `author_review_attempt_terminal_count` | integer/null | holdout 必须；二者相等，且等于 distinct `attempt_id` 数 |
| `author_review_attempt_reason_counts` | map/null | holdout 必须；按 closed terminal outcome/reason 计数并与 ledger 一致 |
| `admission_chain_digest` / `admission_receipt_count` / `committed_admission_count` | string/integer/null | holdout 必须；完整 `AdmissionReceipt` chain root/全部 CAS receipt 数/committed 数；committed 必须等于 96 |
| `accepted_family_revision` / `accepted_family_state_digest` / `accepted_family_summary_digest` | integer/string/null | holdout 必须；revision 必须等于 96，digests 必须为 admission chain 最后 post state 与其可重投影 summary |
| `extension_lineage` | map string→string/null | dev-extension manifest 必须；source core/extension case ID → 新 extension case ID，一一对应且 append-only；core/holdout 必须为空 |
| `sealed_at` | RFC3339 UTC/null | holdout seal 时写 |
| `seal` | DatasetSeal/null | holdout 必须；dev 可用 commit receipt |

`PayloadFileV1` 只允许 `relative_path`、`lf_normalized_sha256` 与排序唯一的 `case_ids`；path 必须 containment-safe。每个 manifest `case_ids` 必须恰好在一个 `PayloadFileV1.case_ids` 中出现一次；全部 file case-id 的并集必须等于 manifest `case_ids`，并且每个被命名文件的 LF-normalized digest 必须匹配。core172 的 `case_ids` manifest 是加载真相；同目录 legacy `evals.json` 不在 `payload_files`/`case_ids` 列表中，禁止通过目录遍历把它计入 count/digest。

### PayloadFileV1

| Field | Type | Rules |
|---|---|---|
| `relative_path` | string | `/`-separated containment-safe relative path；manifest 内唯一 |
| `lf_normalized_sha256` | string | 对完整 named file 的 `lf-normalized-sha256-v1` digest |
| `case_ids` | []string | raw UTF-8 bytes 排序、唯一；每个 ID 在整个 `payload_files` 中恰出现一次 |

### Canonical serialization

所有本 feature 的 structured digest 使用 `agent-memory-trigger-canonical-json-v1`，除非字段另行指定 `lf-normalized-sha256-v1`。输入必须先按其 closed typed schema parse，拒绝 duplicate key、unknown key、NUL、无效 UTF-8、浮点数和 schema 未声明的 JSON type。编码规则固定为：object key 按 raw UTF-8 bytes 升序；数组保留 schema 规定/原始顺序；object/array 外无 whitespace；strings 只转义 `"`、`\\` 与 U+0000–U+001F（`\b`、`\t`、`\n`、`\f`、`\r` 使用短转义，其余使用 lowercase `\u00xx`），其余 Unicode 直接 UTF-8；整数使用最短十进制、不得有 `+`、前导零或 `-0`；boolean/null 使用 lowercase JSON literal。所有 receipt 的 self digest 字段均从自己的 preimage 排除，引用的 child digest 保留在 preimage 内。

### DatasetSeal

| Field | Type | Rules |
|---|---|---|
| `manifest_digest` | string | 已填入 `payload_digest` 且其余字段完成后的 `agent-memory-trigger-canonical-json-v1` manifest digest；只排除 manifest 的 `seal` object。该 digest 存放于外部 `DatasetSeal.manifest_digest`，不在 manifest preimage 内 |
| `dataset_payload_digest` | string | 必须等于 manifest payload digest |
| `anchor_type` | `git-tag | detached-signature | immutable-object` | SHA 文件本身不是 anchor |
| `anchor_id` | string | 不含凭证的 tag/signature/object id |
| `anchor_preimage_digest` | string | SHA-256 of exact `DatasetAnchorV1` bytes below |
| `anchor_content_digest` | string | git-tag/immutable-object 为 exact anchor bytes 的 SHA-256；detached-signature 为 signature bytes 的 SHA-256 |
| `verification_key_fingerprint` | string/null | detached-signature 必须为 configured trusted public key fingerprint；其它 type 必须 null |
| `sealed_by` | string | 自动化 identity；不表示人工审题 |

`DatasetAnchorV1` 是 canonical JSON object `{ "schema_version": 1, "canonicalization": "agent-memory-trigger-canonical-json-v1", "dataset_id": DatasetManifestV2.dataset_id, "dataset_version": DatasetManifestV2.dataset_version, "manifest_digest": DatasetSeal.manifest_digest, "dataset_payload_digest": DatasetSeal.dataset_payload_digest }`；`anchor_preimage_digest` 必须等于这些 exact bytes 的 SHA-256。`git-tag` 的 `anchor_id` 必须是 annotated tag object ID；其 target 必须是内容**恰好**等于 `DatasetAnchorV1` bytes 的 Git blob，且 target bytes SHA-256 必须等于 `anchor_content_digest`。`detached-signature` 的 `anchor_id` 必须是 signature object 的 immutable content ID；validator 必须以 `verification_key_fingerprint` 从 configured trust store 取 key，验证该 signature 对 exact `DatasetAnchorV1` bytes 有效，并校验 signature bytes SHA-256=`anchor_content_digest`。`immutable-object` 的 `anchor_id` 必须是对象的 content address，读取出的 object bytes 必须恰等于 `DatasetAnchorV1` bytes 且其 SHA-256=`anchor_content_digest`。任何无法解析、无法验证、target/content/preimage 不一致或不受信任 key 的 anchor 均 fail closed。

### CandidateBindingV1

`candidate_binding_digest` 是 holdout version 的**稳定 recovery key**，而不是某一次
`FormalSeriesManifest` 或某一张 `pre-holdout` receipt 的摘要。其 preimage 是
`agent-memory-trigger-canonical-json-v1` 的 closed `CandidateBindingV1` object，且必须
包含下列稳定输入：

| Field | Rules |
|---|---|
| `schema_version` / `purpose` | 固定为 `1` / `official-dual` |
| `skill_snapshot_digest` / `skill_snapshot_anchor_digest` / `skill_digest` | exact immutable evaluated package identity |
| `skill_package_validation_receipt_digest` / `validator_revision` / `validator_digest` | exact passing package-validation identity |
| `runner_revision` / `runner_digest` / `judge_rule_digest` | exact runner and deterministic judge identity |
| `dataset_identities` | core172 与 holdout96 的 exact manifest digest，以及各 split 要求的 immutable seal/anchor/commit identity；map key bytes 排序 |
| `core_execution_plan_digest` / `tool_identity_digests` / `tool_configuration_digest` | exact sealed core plan、每 host stable tool identity 与脱敏的 host command/profile/resolved-model/config identity |
| `timeout_seconds` / `concurrency` / `case_order_seeds` / `execution_environment_digest` | final primary execution conditions；不得以 recovery 的新 roots/时间替换 |
| `protected_execution_policy_digest` | `official-dual` 的 normalized boundary/config/worker/template policy identity；不含一次性 root、probe 或 receipt digest |
| `series_prepare_identity_digest` | matching `series-prepare` fixed-suite 的稳定 identity：suite manifest、固定 argv set 与 runner/judge/validator/snapshot/package bindings 的规范化摘要；不含 command output、created time 或 receipt digest |

它**明确排除** `series_id`、`FormalSeriesManifest.manifest_digest`、`ProtectedExecutionReceipt.receipt_digest`、workspace-canary receipt digests、任何 unique run/case/state root、timestamp，以及每次 core leg 后新建的 `pre-holdout` `GreenTestReceipt`（包括其 `series_manifest_digest`、core-leg completion 和 receipt digest）。这些排除项仍须由每一次 attempt 独立验证，不能因为不属于稳定 key 而省略。任何上述稳定输入改变都会产生不同 `candidate_binding_digest`，因而必须使用新的 holdout version。

### HoldoutBindingReceipt

独立于 sealed dataset manifest、保存在 protected root 的 append-only 使用收据，避免通过修改已 seal manifest 表示生命周期。

| Field | Type | Rules |
|---|---|---|
| `dataset_version` / `dataset_manifest_digest` | string | 绑定唯一 holdout96 |
| `candidate_binding_digest` | string | exact `CandidateBindingV1` stable recovery key；不得包含 manifest 或 per-attempt `pre-holdout` receipt |
| `first_primary_started_at` | RFC3339 UTC | ordinal 1 开始前原子创建 |
| `series_attempts` | []object | append-only ordered attempts；每项含 `series_id`、`series_manifest_digest`、相同的 `candidate_binding_digest`、该 series 专属 `pre_holdout_green_test_receipt_digest`、`core_leg_completion_digest`、started/terminal state/time 与 recovery-event digest（若适用）；不得覆盖或 cross-series reuse |
| `state` | `frozen | consumed` | complete series 无论 PASS/FAIL 后 consumed；invalid 后保持 frozen，仅可同 digest 恢复 |
| `consumed_by_series` | string/null | complete 时填；此前 null |
| `receipt_digest` | string | 每次 append 后生成链式 digest，旧版本保留 |

首次 binding 只可在完整 core leg 后发生：controller 先验证该 prepared manifest 的 stable `candidate_binding_digest`，再验证该 series 专属、在 core leg complete 后新建的 `pre-holdout` receipt（它必须绑定 exact manifest、同一 stable digest 与完整 core-leg receipt set），然后原子创建 receipt/attempt entry 并开始 holdout ordinal 1。binding 后的 recovery 不是续跑：先 append recovery event，prepare 新 series；新 manifest 必须重算出**相同的 `CandidateBindingV1` digest**，但它的 `series_id`/manifest/runtime receipts 可以且应当不同。新 series 必须从零完成 core172 三 host × 三 ordinal，之后新建一个绑定该新 manifest 和新 core-leg completion 的 fresh `pre-holdout` receipt；只有该 receipt 通过后，才可在既有 `HoldoutBindingReceipt` 追加/关联新的 attempt entry 并开始 holdout96。必须从零执行完整 core172+holdout96、三 host × 三 ordinal 矩阵，所有 run/case/state roots 全新；旧 series 的成功 ordinal/split 只作 ledger 证据，绝不进入 recovery series 或最终 `OfficialScoreReport`。不同 `candidate_binding_digest` 不得追加到同一 receipt；若放弃该 binding 或修改任一稳定 CandidateBindingV1 输入，必须生成新的 holdout version。

### ProtectedExecutionReceipt

`series prepare` 在最终 host invocation、worker pool 与访问边界均已冻结后、`FormalSeriesManifest` seal 前生成的 private receipt。它证明每个实际受测 CLI child 只能读取自己的 materialized workspace，不能遍历/列举/读取完整 holdout、author/review audit/state root 或任何并发 sibling workspace。它不是 dataset seal、agent 自述或单一 sentinel 结果，也不保存 plaintext case、原始路径、凭证或用户名。

| Field | Type | Rules |
|---|---|---|
| `boundary_kind` | `separate-user | container | mount-namespace | acl | equivalent` | operator-provided isolation mechanism |
| `isolation_config_digest` | string | safe digest of the nonsecret private boundary configuration and access-policy template |
| `protected_root_digest` | string | path digest only; no raw path in formal report |
| `author_review_state_roots_digest` | string | sorted digest of author/review HOME/cache/session/audit roots |
| `formal_state_roots_digest` | string | sorted digest of fresh formal HOME/XDG/cache/session/container allocator roots; must differ from author/review roots |
| `split_state_allocator_digests` | map Split→string | core172 and holdout each use a disposable per-case state allocator; the two sets must be disjoint and the holdout allocator must be unused before its first ordinal |
| `required_concurrency` | integer | equals the sealed series concurrency and is >0 |
| `isolated_worker_capacity` | integer | must be ≥ required concurrency |
| `worker_identity_set_digest` | string | safe digest of all host × worker-slot effective child identities |
| `normalized_core_worker_identity_set_digest` | string | 将 unique UID/container/root ID 规范化后的 host × slot identity/boundary template；必须等于 core plan |
| `execution_template_set_digest` | string | binds the exact final host wrappers/invocation templates used by primary runs |
| `core_execution_plan_digest` | string | 必须等于 series 引用的 `CoreExecutionPlanReceipt.receipt_digest`；实际 core templates 规范化后必须与计划一致 |
| `worker_probes` | []ProtectedWorkerProbe | one complete matrix for every host × worker slot; concurrent sibling checks are pairwise when concurrency>1 |
| `probe_matrix_digest` | string | canonical digest of all ordered probes and closed outcomes |
| `probed_at` | RFC3339 UTC | before series seal |
| `receipt_digest` | string | immutable receipt digest |

### ProtectedWorkerProbe

| Field | Type | Rules |
|---|---|---|
| `host` / `worker_slot` | Host/integer | every host has slots `1..required_concurrency` |
| `child_identity_digest` | string | effective exact-child identity; concurrent slots must be independently isolated |
| `execution_template_digest` | string | must equal the primary template for this host |
| `access_boundary_digest` | string | UID/container/mount/ACL-equivalent policy digest, never a raw identity/config |
| `probes` | []AccessProbe | canonical ordered matrix below |

### AccessProbe

| Field | Type | Rules |
|---|---|---|
| `kind` | enum | `protected-root-traverse | protected-root-list | protected-root-read | author-review-audit-read | author-review-state-read | own-workspace-read | active-sibling-workspace-read | prior-case-state-read | retired-case-workspace-read` |
| `target_digest` | string | path/content identifier digest only; no plaintext or raw path |
| `target_access_policy_digest` | string | must match the actual parent root/workspace policy; a separately chmod'd probe target is invalid |
| `controller_target_proof_digest` | string | controller-side proof immediately before child launch that target exists, carries a nonsecret nonce/content digest, and inherits the stated parent policy |
| `expected` | `denied | readable` | only own-workspace-read expects readable |
| `outcome` | `permission-denied | not-found | readable` | closed observed outcome from exact-child helper |

Every host × worker slot must produce denied outcomes for protected-root traversal/list/read and author/review audit/state reads, and a readable outcome for its own workspace. When `required_concurrency>1`, each slot must run while siblings are active and record denied reads for every sibling workspace. Before every individual primary case, the exact child must also prove denied access to a controller-confirmed prior-case state root and retired workspace; its own disposable state root must be fresh. `not-found` is accepted as a denied outcome only when `controller_target_proof_digest` proves the target existed immediately before the child launched. Insufficient worker capacity, a missing probe/proof, policy-digest mismatch, reusable author/review/formal state, split allocator overlap, identity/template/core-plan drift, successful forbidden access, failed own-workspace read, or any non-closed outcome makes `series prepare` or the affected run fail before scoring. Dataset sealing never creates or validates this formal receipt.

### WorkspaceCanaryReceipt

`series prepare` 在 final skill、host invocation、cwd/materialization template、worker identities 与 concurrency 冻结后自动执行；不存在独立、可遗漏的手工 preflight 命令。只要任一 bound split 含 staged workspace file，每个可执行该 case 的 prepared host × worker slot 都必须各有一条成功 receipt。

| Field | Type | Rules |
|---|---|---|
| `series_id` / `host` | string/Host | 绑定 prepared series 与 host |
| `skill_digest` / `tool_identity_digest` | string | 必须等于 series/plan |
| `execution_template_digest` | string | 必须等于该 host 正式 child template，包括 cwd 与 CLI-specific flags |
| `worker_slot` / `child_identity_digest` / `access_boundary_digest` | integer/string | 使用将承载正式 case 的已准备 slot；identity/boundary 必须属于 prepared worker set |
| `canary_workspace_digest` / `expected_file_digest` | string | fictional nonsecret staged file |
| `observed_cwd_digest` / `observed_file_digest` | string | 必须与 expected contract 匹配 |
| `status` | `pass | fail` | 只有 pass 可 seal series |
| `receipt_digest` | string | immutable receipt digest |

canary 只证明 final invocation 对 own staged workspace 的可见性，不替代 protected-root/sibling/prior-state denial probes。任一 host × usable worker slot 缺 receipt、skill/tool/template/slot mismatch 或观察失败时，含 staged files 的 series 不可 seal、不可 score。

### SkillPackageValidationReceipt

`skill-eval package validate` 是 `FrozenSkillPackageSnapshot` 与正式 validation receipt 的唯一 producer。它递归枚举完整 source package，把 regular files 复制到全新 staging root，调用既有 020 validator，再从 staging bytes 重算全部 file/package digests 并原子物化到全新 immutable snapshot root。existing/non-empty target、internal symlink、special file、path escape、空 package、source/staging/materialized byte mismatch 或 validator failure 都不得留下可复用的 passing receipt。

#### FrozenSkillPackageSnapshot

| Field | Type | Rules |
|---|---|---|
| `snapshot_id` | string | 不可复用；不得从 branch/source path 冒充 identity |
| `snapshot_root_digest` | string | materialized immutable root 的完整 inventory digest |
| `skill_digest` | string | 既有 `engram-package-sha256-v1`；对完整 materialized snapshot 计算 |
| `file_records` | []object | 按 raw UTF-8 relative path 排序；每项含 `relative_path`、exact-byte `sha256`、`size`；覆盖每个 regular file 恰一次 |
| `validator_revision` / `validator_digest` | string | 实际 020 validator identity |
| `snapshot_anchor` | object | controller-held immutable anchor；preimage 至少绑定 snapshot ID、skill digest、完整 file records 与 validator digest |
| `created_at` / `snapshot_digest` | RFC3339/string | immutable；自身 digest preimage 排除 `snapshot_digest` |

正式 `series prepare` 与每个 primary child 必须独立重算 file records、package digest 并验证 anchor，只能 mount/materialize 该 snapshot。mutable `skills/engram`、指向它的 symlink 或普通 copy 不是 snapshot。snapshot 后 source package 任一 byte 的修改是另一未评 revision，不能替换、重写或继承已评分 snapshot identity。

| Field | Type | Rules |
|---|---|---|
| `snapshot_id` / `snapshot_digest` / `snapshot_anchor_digest` | string | 精确引用已验证 `FrozenSkillPackageSnapshot` |
| `skill_version` / `skill_digest` | string | 从 snapshot 读取；必须等于 snapshot package digest |
| `file_records_digest` | string | 必须等于 snapshot 的完整 sorted inventory |
| `validator_revision` / `validator_digest` | string | 绑定 020 package validator |
| `validator_argv_digest` / `validator_output_digest` | string | 绑定实际安全 argv 与 sanitized validator result |
| `checks` | map | 至少包含 description/body/reference 同步、version bump、line/reference/digest consistency |
| `passed` | boolean | 必须 true |
| `validated_at` | RFC3339 UTC | — |
| `receipt_digest` | string | immutable receipt digest |

仅 stdout、手写、只含 package digest 或正式动作后补制的 validator 结果都不是该 receipt。

### GreenTestReceipt

由 `skill-eval green-test create` 在不可逆动作前产生。命令只接受版本化固定 suite，不接受调用方提供的任意 shell 文本。

| Field | Type | Rules |
|---|---|---|
| `suite` | `holdout-pipeline | formal-tooling | series-prepare | pre-holdout` | 固定闭集；命令清单版本化 |
| `suite_manifest_digest` | string | 精确绑定固定 argv/expected-result 集合 |
| `commands` | []object | 每项记录 argv digest、exit code、sanitized stdout/stderr digest；不得含 secret/env value |
| `runner_digest` / `judge_rule_digest` / `validator_digest` | string | 当前实现 identity |
| `snapshot_digest` / `package_validation_receipt_digest` | string/null | `series-prepare`/`pre-holdout` 必须绑定；其它按 suite 适用 |
| `stable_identity_digest` | string/null | `series-prepare` 必填；由 suite manifest、fixed argv set 与 stable runner/judge/validator/snapshot/package bindings 规范化导出，不含 output/time/receipt digest；matching recovery series 必须相同 |
| `series_manifest_digest` | string/null | `pre-holdout` 必须绑定 exact prepared series；不得复用于另一 series |
| `candidate_binding_digest` / `core_leg_completion_digest` | string/null | `pre-holdout` 必须分别绑定该 series 的 exact `CandidateBindingV1` 与完整 core172 host×ordinal receipt set；其它 suite 为 null |
| `passed` | boolean | 全部 required command exit=0 且所有绑定 digest 重验成功才为 true |
| `created_at` / `receipt_digest` | RFC3339/string | immutable；自身 digest preimage 排除 |

`holdout-pipeline` 必须早于首个真实 author/reviewer launch，并在 generate/review/seal 全程保持 digest-current；`formal-tooling` 必须早于 pre-revision/final `package validate`；`series-prepare` 必须早于 series seal 并绑定 snapshot/validation receipt，且产生 `stable_identity_digest`；`pre-holdout` 必须在该 series 的 complete core leg 后、holdout ordinal 1/binding 前重新运行，绑定 exact series、stable candidate digest 和 complete core-leg receipt set。recovery 的 matching `series-prepare` receipt 可以是新建或仍 digest-current 的既有 receipt，但其 `stable_identity_digest` 必须等于 CandidateBindingV1；不得复用 invalid series 的 `pre-holdout` receipt。missing、failed、wrong-suite、post-hoc 或任一 bound digest 漂移都拒绝相应动作。

## 8. CoreExecutionPlanReceipt and FormalSeriesManifest

### CoreExecutionPlanReceipt

在 SC-5 comparable baseline 开始前创建一次并封存，随后由 `dev-comparison` baseline 与 post-change `official-dual` series **引用同一个 receipt digest**。该 receipt 冻结 core172 的稳定执行条件，但刻意不绑定 evaluated skill，使 skill package 成为 before/after 的唯一有意变量。

| Field | Type | Rules |
|---|---|---|
| `schema_version` | integer | `1` |
| `plan_id` | string | 不可复用；安全标识 |
| `core_manifest_digest` | string | 恰好绑定冻结 core172，不含 extension/holdout |
| `runner_revision` / `runner_digest` | string | 最终 runner identity |
| `judge_rule_digest` | string | 最终 deterministic judge identity |
| `hosts` | [Host] | 恰好 Claude/Codex/OpenCode |
| `tool_identity_digests` | map Host→string | 每 host 一个稳定 `tool_identity_digest`；series prepare/run 必须实测匹配 |
| `timeout_seconds` / `concurrency` | integer | 两者均冻结且 concurrency >0 |
| `case_order_seeds` | map ordinal→string | 恰好 ordinal 1/2/3；baseline 与 candidate 不得重新生成 |
| `core_boundary_kind` | enum | `separate-user | container | mount-namespace | acl | equivalent`；before/after 相同 |
| `normalized_core_worker_identity_set_digest` | string | 绑定每 host × worker slot 的 user/container image/runtime/boundary identity template，排除 unique IDs |
| `normalized_core_boundary_template_digest` | string | 绑定 mount/ACL/network/process/state visibility policy template；baseline 与 candidate 相同 |
| `normalized_core_execution_template_digest` | string | 绑定 host wrappers、child command shape、cwd/materialization、env-name allowlist、MCP/config shape 与 disposable per-case HOME/XDG/cache/session/container state-reset/isolation template |
| `created_at` | RFC3339 UTC | receipt 创建时间；复用同一 receipt，不参与 ToolProvenance identity |
| `receipt_digest` / `seal_digest` | string | 所有字段填完后计算并封存 |

三个 normalized core digests 必须排除 evaluated skill digest/路径、purpose、series ID、唯一 UID/container/output/scratch/HOME/cache/session root ID，以及 holdout-only dataset/`ProtectedExecutionReceipt` 字段；它们必须保留会改变 core child 行为或可见性的 user/container runtime、mount/ACL/network/process policy、wrapper/config 与每-case disposable state/reset、prior-state/retired-workspace denial语义。`core-plan create` 之后，任一 runner、judge、tool identity、timeout、concurrency、seed、worker identity/boundary 或 execution template 变化都必须创建新 plan，旧 baseline 不再具备 SC-5 可比资格。

### FormalSeriesManifest

绑定一个 candidate skill/runner 到 3 hosts × 3 ordinals，以及由 purpose 决定的一个或两个正式 split。

| Field | Type | Rules |
|---|---|---|
| `series_id` | string | 不可复用 |
| `purpose` | SeriesPurpose | `official-dual` 绑定两 split；`dev-comparison` 只绑定 core172 且不可 score |
| `state` | LifecycleState | score 前必须 `sealed` |
| `skill_snapshot_digest` / `skill_snapshot_anchor_digest` | string | 必须引用 immutable `FrozenSkillPackageSnapshot`；ordinal 1 前冻结 |
| `skill_version` / `skill_digest` | string | 从 snapshot 读取；ordinal 1 前冻结 |
| `skill_package_validation_receipt_digest` | string | 必须由唯一 producer 产生、绑定 exact snapshot/skill digest 且 `passed=true`；缺失/失败/不同 digest 均不可 seal |
| `green_test_receipt_digest` | string | 必须是 matching、passing 的 `series-prepare` receipt |
| `series_prepare_identity_digest` | string | 必须等于 matching receipt 的 `stable_identity_digest`；official-dual recovery 必须等于已 binding 的 CandidateBindingV1 输入，receipt digest 本身可不同 |
| `runner_revision` / `runner_digest` | string | ordinal 1 前冻结 |
| `judge_rule_digest` | string | 所有 run 相同 |
| `core_execution_plan_digest` | string | 两种 purpose 均必须引用一个已 seal plan；SC-5 before/after 必须完全相同 |
| `dataset_manifests` | map Membership→digest | official-dual 恰好绑定 core172 与 holdout96；dev-comparison 恰好绑定 core172；extension 均不进入 series |
| `hosts` | [Host] | 恰好三个 |
| `required_ordinals` | [1,2,3] | 不可配置成更少 |
| `timeout_seconds` | integer | series 内固定且必须等于 core plan |
| `concurrency` | integer | series 内固定、>0 且必须等于 core plan |
| `execution_environment_digest` | string | 对 core child 必须等于 plan 的 normalized disposable-state/isolation template digest；排除 unique artifact IDs |
| `tool_configuration_digest` | string | 脱敏的 host command/profile/resolved-model/config identity；不得含 secret、路径或 unique root |
| `protected_execution_policy_digest` | string/null | `official-dual` 必须从 protected receipt 的 boundary/config/worker/template稳定投影导出；排除 root/probe/time/receipt digest；dev-comparison 必须 null |
| `case_order_seeds` | map ordinal→string | 必须逐项复制 core plan，不能由 series 重新生成 |
| `question_count` | map Split→integer | digest/seal 前填完；只含已绑定 split，值为 172/96 |
| `candidate_binding_digest` | string/null | `official-dual` 必须等于从 `CandidateBindingV1` 重算的 stable digest；不得含此 manifest 或 per-attempt `pre-holdout` identity；dev-comparison 必须 null |
| `protected_execution_receipt_digest` | string/null | official-dual 必须绑定 `ProtectedExecutionReceipt`；dev-comparison 必须 null |
| `workspace_canary_receipt_digests` | map Host→map worker-slot→string | 任一 bound split 含 staged files 时覆盖每个可执行的 host × prepared worker slot，且全 pass；否则为空 map |
| `manifest_digest` | string | 所有字段填完后计算 |

`HoldoutBindingReceipt` 在 ordinal 1 开始前引用已 seal 的 `FormalSeriesManifest.manifest_digest` 并追加到独立使用日志；该 attempt 同时记录其 fresh `pre-holdout` receipt 与 core-leg completion digest。manifest 不反向保存该未来可变 receipt 的 digest，避免 freeze-before-digest 循环。recovery 的新 manifest 必须有新的 series ID/manifest digest，同时保持同一 `series_prepare_identity_digest` 与 `candidate_binding_digest`；其 exact `series-prepare` receipt digest 可以不同，随后仍必须在完整新 core leg 后生成自己的 `pre-holdout` receipt。

`series prepare` 必须先 re-hash immutable snapshot、验证 anchor、exact-snapshot package receipt 与 matching `series-prepare` GreenTestReceipt，再验证当前 runner/judge、每 host `tool_identity_digest`、timeout/concurrency、core manifest、case-order seeds、normalized worker identity/boundary 与 execution template 全部匹配引用的 plan；若数据集含 staged files，还必须自动执行每个可执行 host × worker slot 的 workspace canary 并在 seal 前绑定 receipts。`dev-comparison` 与用于 SC-5 candidate core leg 的 `official-dual` 必须引用同一个 plan receipt；purpose、skill snapshot digest、唯一 roots 与 official-only protected receipt 可以不同，但不得改变 core child invocation/visibility 语义。

## 9. PrimaryRunManifest

唯一键：`series_id + host + split + ordinal`。

| Field | Type | Rules |
|---|---|---|
| `mode` | RunMode | 必须 `primary` |
| `host` / `split` / `ordinal` | enum/int | ordinal 1..3 |
| `tool_provenance` | ToolProvenance | `tool_identity_digest` 必须等于 core plan 中对应 host；`captured_at` 可不同 |
| `case_ids` / `case_set_digest` | list/string | 必须等于 split manifest |
| `case_order` / `case_order_digest` | list/string | 由预注册 seed 得出 |
| `expected_case_count` | integer | 172 或 96 |
| `started_at` / `completed_at` | RFC3339 UTC | — |
| `state` | LifecycleState | `complete` 才可 score |
| `run_digest` | string | receipts 全部写完后计算 |
| `seal_digest` | string | seal 后不可改 |

Primary 模式中 `only/sample/limit` 均无字段，避免 artifact 暗含部分运行。

## 10. CaseRunReceipt

| Field | Type | Rules |
|---|---|---|
| `case_id` / `case_payload_digest` | string | 绑定 sealed case |
| `workspace_digest` | string | 包括 staged files，不含 secrets |
| `case_state_isolation_receipt_digest` | string | 每 case/host/split/ordinal 唯一；证明 disposable state、prior/retired probe 与 teardown |
| `attempt_count` | integer | primary agent attempt 必须为 1 |
| `status` | `pass | fail | runner-error` | 每 case 恰好一个 terminal status |
| `normalized_events_path/digest` | string | 可重判 |
| `raw_events_path/digest` | string | 秘密过滤后的原始事件流 |
| `store_dump_path/digest` | string | post-turn store 收据 |
| `verdict` | Verdict | pass/failure class/detail code |
| `duration_ms` | integer | 非负 |
| `stderr_digest` | string/null | 不保存任意 stderr 到正式 JSON |

### CaseStateIsolationReceipt

每个 `series_id + host + split + ordinal + case_id` 一条 private receipt。

| Field | Type | Rules |
|---|---|---|
| `worker_slot` | integer | 必须在 prepared `1..concurrency` 中 |
| `protected_execution_receipt_digest` | string/null | official-dual 必须等于 series manifest；dev-comparison 必须 null |
| `prepared_worker_probe_digest` | string/null | official-dual 必须精确引用该 host × worker_slot 的 `ProtectedWorkerProbe` canonical digest |
| `child_identity_digest` / `execution_template_digest` / `access_boundary_digest` | string | 必须逐项匹配被引用 slot probe；dev-comparison 则必须规范化匹配 core plan |
| `fresh_state_root_digest` / `state_allocator_digest` | string | 每 case 唯一；allocator 与 split 一致 |
| `prior_state_probe` / `retired_workspace_probe` | AccessProbe/null | 有前序目标时必须有 controller proof 与 denied outcome |
| `reset_method` | string | 固定 `disposable` |
| `child_teardown` / `retirement_or_final_delete` | closed object | 记录 child teardown 与 controller-only retirement/final-delete |
| `receipt_digest` | string | immutable receipt digest |

运行结束先销毁 child session/container；其 state/workspace 可在 controller-only retirement boundary 中短暂保留，以便下一 child 在“controller 已确认目标存在”的条件下证明不可读，随后删除；最后一条 case必须记录 controller-verified deletion。`fresh_state_root_digest` 不得与同一 series 的任何其他 case、其他 ordinal、其他 split 或 author/review state root 重复；holdout allocator/state root 在 core execution 前不得被创建或使用。缺 receipt、未知/错误 worker slot、prepared-probe 引用缺失、实际 child identity/template/boundary 与 slot 漂移、根复用、缺 controller proof、prior/retired target 可读、reset/retirement/delete 失败均使 run INVALID。

## 11. Verdict and FailureArchive

### Verdict

- `pass`: boolean
- `failure_class`: `false-negative | false-positive | wrong-op | wrong-report | runner-error | null`
- `detail_code`: 闭集机器码
- `judge_rule_digest`: string

### FailureArchive

仅在 dev 飞轮使用：case id、host、split、comparable baseline series/run ids、三 ordinal 的 case states 与 binary median、failure class、root-cause enum、修复 skill version/digest、before/after full-run references。archive 还必须绑定 pre-revision skill snapshot digest、`core_execution_plan_digest` 与每 host `tool_identity_digest`；before/after 必须引用同一个 plan，不能直接比较含 `captured_at` 的完整 ToolProvenance digest。不同 purpose 与 unique artifact IDs 不得改变 core evaluated-child invocation 语义。最终 runner/judge 冻结前的 exploratory diagnostic receipt 不得成为该 archive 的 baseline。extension case 结果单列，不能替代 core172 的可比行。构建器必须拒绝 `split=holdout`、任何 holdout receipt 或任何需要遍历 holdout child artifact 的输入；holdout failure 只能留在 protected series 内，并在消费后通过不含 case plaintext 的聚合分析报告呈现。

`failure-archive` 只接受 complete、sealed、core-only `dev-comparison` series，并输出带 `archive_digest` 与 `seal_digest` 的不可变 archive receipt。它不得生成任何 official score/headline。

### FlywheelComparisonReceipt

`compare` 只读取 baseline `dev-comparison` 与 candidate `official-dual` 的 sealed **core172** run receipts，绝不读取 candidate series 的 holdout case/receipt。输出包含两个 series manifest digest、共同 `core_execution_plan_digest`、before/after skill digests、每 host × case binary medians、fail-to-pass/regression counts、排序去重的 `required_extension_backfill_source_case_ids`（任一 host 存在 eligible fail-to-pass 的 core ID）及其 digest、单列 extension diagnostic receipt digest（如有）、`sc5_verdict`、`report_digest` 与 `seal_digest`。每个 source ID 在 dev-extension manifest `extension_lineage` 中必须恰好映射到一个新 extension ID；缺项、重复、错误 source 或不在 manifest membership 中均使 backfill verification 失败。plan 不同、tool identity 不匹配、任一 core series 不完整、输入含 holdout plaintext/child receipt、或调用方请求 official score 字段时必须拒绝。

## 12. OfficialScoreReport

| Field | Type | Rules |
|---|---|---|
| `series_id` | string | — |
| `series_manifest_digest` | string | 必须等于被评分 sealed manifest |
| `core_execution_plan_digest` | string | 必须等于 series 与每个 core run 引用 |
| `protected_execution_receipt_digest` | string/null | 必须等于 official-dual manifest，且 receipt 有效；不含 holdout 的 report schema 扩展时可 null，但本 feature official-dual 必须非 null |
| `workspace_canary_receipt_digests` | map Host→map worker-slot→string | 必须等于 manifest；含 staged files 时每个可执行 host × worker slot 都有效 |
| `skill_package_validation_receipt_digest` | string | 必须绑定 exact scored `skill_digest` |
| `skill_snapshot_digest` / `skill_snapshot_anchor_digest` | string | 必须与 series/primary children 实际使用的 immutable snapshot 相同 |
| `candidate_binding_digest` / `holdout_binding_receipt_digest` | string | 必须分别等于 scored manifest 的 stable `CandidateBindingV1` digest，以及 ledger 中关联该 exact manifest/fresh pre-holdout/complete attempt 的最新 receipt digest |
| `green_test_receipt_digests` | object | 至少绑定 scored manifest 的 matching `series-prepare` 与该 exact series 的 fresh `pre-holdout` receipt；不得引用 invalid series 的 receipt |
| `dev_regression` | []HostScore | 恰好三宿主或明确 unavailable/invalid |
| `generalization` | []HostScore | 同上 |
| `supplemental_cross_host` | object/null | 只能标记 `non-gating` |
| `overall_verdict` | `pass | fail | invalid` | 任一宿主适用门失败 → fail；任一 required series 无分 → invalid |
| `diagnostic_artifacts_used` | boolean | 必须 false |
| `bias_diagnostics` | object | non-gating；每 cell 含 numerator、denominator、independent_case_count、`low_n` |
| `report_digest` / `seal_digest` | string | immutable receipt |

### HostScore

- host, split
- run rates and numerators/denominators for ordinals 1/2/3
- per-module median rates
- applicable gate results
- `official=true`

## 13. State invariants

1. `accepted` case 不能被作者审阅，且必须有两个互异非作者在递归 closed `BlindCandidateV1` envelope 上独立推导出的 accept；两者完整 inferred module/lang/scenario/category/expect 与 label digest 必须一致、匹配私有 author/four-dimensional slot，并通过最新 accepted-family revision/source-state 的原子 CAS。
2. holdout 未 `sealed` 不可用于 primary；首次 holdout ordinal 前必须原子创建/关联 binding attempt。binding 后 invalid series 只能以相同 stable `CandidateBindingV1` digest 和新 series ID 恢复；recovery manifest/pre-holdout receipt 必须新建且精确绑定彼此及完整新 core leg，不能被 stable digest 的相同掩盖；不同 digest 必须使用新 holdout version；已 `consumed` 不可开始新 series。
3. series ordinal 1 开始后，skill/runner/judge/dataset/tool config 任一 digest 漂移即 series INVALID。
4. primary case count 不完整、case 重复、raw/store/state-isolation receipt 缺失即 run INVALID。
5. diagnostic artifact 永不成为 `OfficialScoreReport` 输入。
6. dev 与 holdout 分数不得加权、平均或拼接成一个 headline 数字。
7. 所有 manifest 字段必须在 digest 和 seal 前冻结；seal 后追加字段视为篡改。
8. review envelope 的 candidate 必须恰为 `BlindCandidateV1` 允许的递归字段集合；不得含 author identity、author-specific quota slot、batch/source、author/reviewer receipt、author-proposed expect/module/lang/scenario/category/machine rules、author-proposed family ID，或可反推 author lane/label 的字段。未知字段不得先接受后删除。
9. dev FailureArchive 不得接受 holdout receipt；holdout plaintext-bearing run artifact 必须留在 protected root。
10. holdout primary 前必须由 `series prepare` 生成完整 `ProtectedExecutionReceipt`:每个 host × worker slot 的 identity/template 必须与正式 invocation 一致，隔离 capacity 必须覆盖 concurrency，root/audit/state/sibling probes 必须有 controller target proof 并拒绝且 own-workspace probe 必须可读；`not-found` 无存在证明不得通过。任一缺失、策略不一致或反向结果使 series INVALID。
11. `dev-comparison` series 必须只绑定 core172、完成三宿主 × 三 ordinals、拒绝 holdout/extension，并永不进入 `OfficialScoreReport`；`official-dual` 缺任一正式 split 时不可 seal。
12. accepted holdout case 的 author/reviewer attempts 必须各有有效 `AuthorReviewIsolationReceipt` 与 controller target proof；private root/audit/author receipt/prior review/sibling 任一可读或 own input 不可读时 dataset seal 失败。每 host resolved model 必须跨 author/review attempts 稳定且均非 `unavailable`；三宿主 harness 必须互异（模型可以相同——2026-09-01 统一拍板）。
13. SC-5 before/after series 必须引用同一个 sealed `CoreExecutionPlanReceipt`；逐次 `captured_at` 可不同，但每 host `tool_identity_digest`、runner/judge/core/timeout/concurrency/seeds/normalized template 必须匹配该 plan。
14. `official-dual` 的 core leg 未完整或 series 已 INVALID 时禁止开始 holdout。若 holdout ordinal 1 尚未开始，必须保留失败 series，以同一 skill/candidate binding、同一 core plan 和新 series ID 重跑完整 core；此恢复不得创建/修改 `HoldoutBindingReceipt`、消费或重绑 holdout。
15. `failure-archive` 与 `compare` 都不得生成 official score，也不得接受或遍历 holdout plaintext/child receipts；两者输出必须带独立 seal。
16. holdout manifest 必须满足八个闭集 scenario bucket 的预注册 count/author/language/module coverage；unknown bucket 或 source/scenario audit 缺失不可 seal。
17. 每个 primary case 必须有唯一 disposable formal state root 和有效 `CaseStateIsolationReceipt`；prior-case state/retired workspace 可读、root 重用、core/holdout allocator 重叠、holdout allocator 在 core leg 被使用或 teardown 失败均使 run/series INVALID。
18. reviewer-visible candidate digest 只能覆盖完整 validated `BlindCandidateV1` 的 `agent-memory-trigger-canonical-json-v1` bytes；相同 blind projection 不得因私有 label/slot/provenance/family proposal 不同而产生不同 reviewer-visible digest。两个 family-summary digest 必须绑定 reviewer 实际可读的匿名 payload 及其完整 source index/state/count/root，digest-only novelty review 不可接受。
19. dataset `payload_digest` 只覆盖 manifest `payload_files` 显式命名的 case files；manifest digest 只在 payload digest 和其余字段完成后按 `agent-memory-trigger-canonical-json-v1` 计算并排除 `seal`，任何 digest 自引用、post-digest mutation、payload-file union/digest mismatch 或 anchor preimage/content/key mismatch 都使 seal 无效。
20. holdout seal 必须覆盖 append-only `AttemptStarted`/`AttemptTerminal` event chain、全部已启动 attempt 的 isolation/provenance、以及完整 `AdmissionReceipt` CAS chain；删除 rejected/stale/failed evidence、隐藏模型漂移、unpaired/non-terminal attempt、event/admission chain fork 或 count/reason mismatch 均不可 seal。
21. 含 staged files 的 series 必须在 `series prepare` 内自动产生并绑定每个可执行 host × worker slot 的 `WorkspaceCanaryReceipt`；每个 official case 必须绑定已准备的 host × worker slot，实际 child identity/template/boundary 必须与该 slot probe 完全匹配。
22. `series prepare` 必须拒绝缺失、失败或不匹配 exact `skill_digest` 的 `SkillPackageValidationReceipt`；正式评测后才运行 package validator 不构成合规。
23. 每个 AuthoringReceipt/ReviewRecord 必须精确 join 一个 started/terminal attempt pair；每个 committed AdmissionReceipt 必须精确 join 一个 author receipt、两个互异 reviewer records、private four-dimensional slot 和一个最终 case。receipt/attempt/admission 任一 orphan、重用或多重 join 均使 seal 无效。
24. sealed accepted-family summary 必须能从 frozen DevFamilyIndex 或完整 accepted-family state 一一重投影；其 controller opaque family references、source state/count/root、entry/payload digests 和 admission final state 必须同时一致。
25. primary series 必须绑定并只读取可重验 anchor 的 `FrozenSkillPackageSnapshot`；mutable source、普通 copy、symlink、snapshot byte/file-list/package-digest 漂移或 post-hoc package receipt 均使 prepare/run/score 失败。
26. holdout real author/review/seal、正式 package validate、series prepare 与 holdout ordinal 1 必须分别有 scope 正确且 digest-current 的 passing `GreenTestReceipt`；missing/failed/wrong-suite/post-hoc/drifted receipt 不得授权不可逆动作。
27. binding 后 INVALID recovery 必须以同一 stable `CandidateBindingV1` digest、新 series ID 和 fresh roots 重跑两个 split 的全部 host/ordinal；新 manifest 与每次 core 完成后新建的 `pre-holdout` attestation 必须彼此精确绑定，并作为既有 binding ledger 的新 attempt 关联。任何跨 series run/receipt 拼接、旧 `pre-holdout` receipt 复用或旧成功 receipt 复用均使 report INVALID，最终 report 只引用 complete recovery series。
