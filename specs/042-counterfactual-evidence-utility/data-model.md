# Data Model: Counterfactual Evidence Utility Gate

## Design goals

- 以稳定 identity 将 shallow、deep、signal、judge 和 policy decision 严格配对；
- 把运行时可见信号与 gold/correctness/utility label 分开，能证明 decision label-blind；
- 每个 stage 可 crash-safe resume、tamper detection 和独立复算；
- headline 统计以 question-majority 为单位，调用与成本以 repetition decision unit 为单位；
- artifact 不包含 credential、endpoint 原文、raw provider response/error 或 decoded reasoning token。

所有 JSON object 都包含 `schema: "counterfactual-utility/v1"`；所有浮点数必须有限，canonical digest 使用仓库既有
JSON canonicalization/digest helper。下述字段名是 Phase 1 contract 的逻辑模型；精确 JSON shape 见
[utility-artifacts.md](contracts/utility-artifacts.md)。

## Identity hierarchy

```text
BenchmarkRun
└── Conversation (conversation_id)
    └── Question (question_id, question_index, category)
        └── DecisionUnit (repetition = 1..3)
            ├── shallow AnswerAttempt
            │   └── ProbabilitySignalRecord
            ├── paired/control deep AnswerAttempt
            ├── UtilityLabelRecord (score-only custody)
            └── UtilityDecision (diagnose/confirm/transfer)
```

Canonical decision key:

```text
(benchmark, conversation_id, question_id, repetition)
```

Canonical logical-call key在 decision key 后增加 `stage` 与 `arm`；provider attempt key再增加 `attempt=1..3`。
`arm` closed enum 为 `shallow`、`paired_deep`、`policy_deep`、`fixed_deep`、`judge_shallow`、
`judge_paired_deep`、`judge_policy`、`judge_fixed_deep`、`preflight`。`paired_deep` 只表示 collect 中离线配对的 k150 answer，
不得与 confirm/transfer 的条件 `policy_deep` 或独立 `fixed_deep` control 混用。重复 key、缺失 repetition、同 key identity
漂移均 fail closed。

## Entity: UtilityRunManifest

一个 stage 在任何模型调用或 label loading 前冻结的 provenance root。

| Field group | Required content |
|---|---|
| Identity | `run_id`, `stage`, `created_at`, `schema` |
| Upstream | source stage path label-free digest、source manifest digest、source seal digest；`collect` 绑定 label-regression GO receipt |
| Benchmark | name/format/split、dataset digest、question count、question IDs digest、conversation IDs、category counts、repetitions=3 |
| Retrieval | arm=`hybrid`、shallow_k=30、deep_k=150、chunk quota=12、store provenance digest、store embedding fingerprint（model/dims/counts）、embedding model/revision、endpoint digest、sidecar config digest、`max_num_seqs=1`、probe dimension/digest、retrieval/prompt flags digest |
| Answerer | provider kind=`openai-compatible-local`、model、operator-supplied model revision、endpoint digest、sidecar config digest、max tokens=8000、max model len=32768、`temperature_request_mode=omitted`、thinking mode |
| Signal | feature order、mapping algorithm version、`logprobs=true`、`top_logprobs=2`、response limit=64 MiB、capability policy |
| Calibration | split=`leave-one-conversation-out`、rule=`ridge-utility-v1`、lambda=1、threshold objective/tie-break digest |
| Judge | provider、model/revision、endpoint digest、prompt digest、`mem0_aligned=true`、`clean_final_answer=extract-final-answer-v1`、temperature request mode、repetitions/majority rule |
| Calls | `max_attempts=3`、retryable reason/status allowlist、unknown answer usage charge=`max_model_len` |
| Gates | `min_net_questions=25`（independent conservative floor）、`quality_not_below_same_batch_deep=true`, `min_accuracy=0.90`, `max_token_ratio=0.60`, category tolerance, Holm alpha |
| Build | binary digest、source revision、source modified flag、Go version |

**Invariants**:

- `stage` closed enum：`label | pilot | collect | diagnose | confirm | transfer`。
- `collect` 必须同时绑定 `label` GO 与 `pilot` GO 的 receipt；`pilot` 只在前两条 conversation 上运行，是全量 collect 前的信号存在性 kill-gate。
- `collect/confirm/transfer` 必须为 local OpenAI-compatible answer endpoint；无托管 default。
- `collect/confirm/transfer` 必须显式配置 embedder；store fingerprint 中所有 embedding rows 的 model/dims 必须单一且与 sidecar probe 一致。
- answer request 不得序列化 `temperature`；若实现或 sidecar config digest 改变该 wire/default 语义，manifest 不兼容且不得 resume。
- judge 必须显式为 mem0-aligned，并只判 clean final answer；任一模式不是冻结值即拒绝。
- 正式 verdict 的 shallow/deep/repetitions/feature/rule/gate fields 必须是上述 v1 固定值；测试 fixture 可使用小 denominator，
  但 receipt 必须标 `fixture=true` 且永不 GO。
- manifest 写入后不可修改；resume 必须逐字节 digest 相等。

## Entity: CallUnitRecord

append-only crash journal 的一个状态事件。它记录真实 provider attempt，而不是 wrapper invocation。

| Field | Type | Rules |
|---|---|---|
| `logical_call_id` | string | canonical decision/stage/arm/request identity digest；全部 retries 共享 |
| `unit_id` | string | `logical_call_id + attempt` 的 canonical digest |
| `decision_key` | object | preflight 除外必须完整 |
| `arm` | enum | closed values，和 stage allowlist 一致 |
| `state` | enum | `STARTED | COMPLETED | FAILED` |
| `request_digest` | digest | system/user/model/generation params 的 sanitized digest |
| `attempt` | integer | `1..3`；同 logical call 严格递增且完成后不得继续 |
| `started_at`, `latency_ms` | timestamp/int | COMPLETED/FAILED latency 非负 |
| `input_tokens`, `output_tokens` | integer/absent | upstream 提供 valid usage 时非负；不得伪造 0 |
| `usage_status` | enum | terminal only：`reported | conservative_bound | unavailable | not_applicable` |
| `ratio_token_charge` | integer | answer attempt 必填；reported=I+O，FAILED 且 usage unavailable=32768；judge=0 |
| `answer_digest`, `response_digest` | digest | COMPLETED answer unit 必填；不存 raw response |
| `failure_reason`, `retryable` | enum/bool | FAILED only；closed reason/status class，无 raw upstream text |

**State machine**:

```text
logical call absent
  -> attempt 1 STARTED -> COMPLETED (stop)
                       \-> FAILED retryable -> attempt 2 ... -> attempt 3
                       \-> FAILED non-retryable/exhausted (stage INVALID)
```

同一 attempt 只能有一个 STARTED 和一个 terminal event。一个 logical call 至多一个 COMPLETED；COMPLETED 可在 manifest/request
digest 相同的 resume 中跳过。只有 `timeout | network_error | http_429 | http_5xx` 可令下一 attempt 合法；terminal retryable
FAILED 且 attempt<3 可在 resume 中继续。孤立 STARTED、FAILED non-retryable/exhausted、attempt gap、完成后继续、双 terminal 或
request digest 漂移使 run INVALID。每次 attempt 的 calls/latency/可得 usage 和 token charge 都进入所属臂总账。

## Entity: AnswerAttemptReceipt

一个成功 answer attempt 的 label-blind public record。

| Field | Type | Notes |
|---|---|---|
| `answer_attempt_id` | digest | successful logical answer receipt identity；不是 provider retry unit ID |
| `logical_call_id`, `completed_unit_id` | digests | 连接 CallUnitRecord logical call 与唯一 COMPLETED provider attempt |
| decision identity | object | benchmark/conversation/question/repetition |
| `arm` | enum | `shallow | paired_deep | policy_deep | fixed_deep`；`paired_deep` 仅 collect |
| `question_digest` | digest | 不以 gold 为输入 |
| `retrieval_k`, `candidate_count` | int | k30 或 k150 |
| `retrieval_calls`, `retrieval_latency_ms` | int | 非负 |
| `embedding_calls`, `embedding_failures`, `embedding_latency_ms` | int | counting client 按臂记录；不得因 engine graceful degradation 丢失 |
| `embedding_token_usage_status` | enum | 固定 `not_applicable_to_generation_ratio`；不是 provider-reported zero |
| `final_answer` | string | 从 completion 调用 `extractFinalAnswer` 得到；非空；不持久化完整 thinking completion |
| `answer_digest`, `final_answer_digest` | digest | 前者绑定未持久化的完整 content，后者绑定存储的 clean final |
| `input_tokens`, `output_tokens`, `latency_ms` | int | 成功 answerer attempt 的 reported usage / latency；全部 retry 成本在 CallUnitRecord 聚合 |
| `signal` | ProbabilitySignalRecord/null | shallow 和 preflight 必须出现；deep 可省略 |

此 entity 不含 gold、judge verdict、utility label 或由 correctness 派生的字段。

## Entity: ProbabilitySignalRecord

浅答正常生成 response 中抽取的可路由概率记录。

| Field | Type | Rules |
|---|---|---|
| `status` | enum | `available | unavailable` |
| `reason` | enum | available 时为空；closed unavailable reasons |
| `content_digest`, `token_trace_digest` | digest | 绑定 response content 与 sanitized trace |
| `generated_token_count`, `final_token_count` | int | available 时 final > 0 |
| `final_byte_start`, `final_byte_end` | int | 必须在 token boundary 且形成 content suffix |
| `features` | `[3]float64` | 固定顺序；available only |
| `feature_names_digest` | digest | 固定三特征顺序的 digest |
| `final_trace` | array | 每项只有 `byte_len`, `sampled_logprob`, `top1_logprob`, `top2_logprob` |
| `thinking_diagnostic` | object/null | aggregate only，固定 `routing_eligible=false` |
| `final_length_stratum` | enum | `1 | 2-4 | 5-16 | 17+`；report only |

Unavailable reasons：

```text
missing_logprobs
missing_token_bytes
content_not_generated_suffix
final_not_content_suffix
empty_final
final_boundary_inside_token
missing_top2
non_finite_probability
unsupported_response_shape
```

Unavailable record 不得有 `features` 或伪造的 zero values。HTTP/timeout/JSON parse 失败属于 CallUnit FAILED，不属于 signal
unavailable。

## Entity: JudgeOutcome

score-only custody record；运行时 rule/decision loader 不得引用此文件。

| Field | Type | Rules |
|---|---|---|
| decision identity + answer arm | object | 必须对应一个 COMPLETED AnswerAttemptReceipt |
| `answer_digest` | digest | 判的是该 answer 的 clean final |
| `correct` | bool | frozen judge verdict |
| `judge_prompt_digest`, `judge_model_digest` | digest | 必须与 manifest 一致 |
| judge usage/call/latency | ints | 单列，永不计入 runtime token ratio |

gold 可留在 benchmark 原始输入，不需要重复写入 artifact；如现有 result compatibility 要求写 gold，hidden loader 必须保证它不会
出现在 public decision types 中。

## Entity: UtilityLabelRecord

由同一 decision key 的 shallow/deep JudgeOutcome 唯一导出。

| Field | Type | Rules |
|---|---|---|
| decision identity | object | shallow/deep 完全对齐 |
| `shallow_answer_digest`, `deep_answer_digest` | digest | source binding |
| `shallow_correct`, `deep_correct` | bool | source outcomes |
| `utility` | int | `deep - shallow`，只允许 -1/0/+1 |
| `label` | enum | `HARM | NEUTRAL | BENEFIT`，与 utility 一一对应 |
| `source_manifest_digest`, `source_seal_digest` | digest | provenance |

历史 label audit 另聚合到 question-majority 后计 56/31；fresh calibration 的 model rows 保留 repetition-level label。
缺一侧、重复、judge/prompt/identity/provenance 不同均拒绝，不产生 label。

## Entity: FeatureScaler

只由一个 fold training conversations 计算。

| Field | Type | Rules |
|---|---|---|
| `feature_names` | `[3]string` | 固定顺序 |
| `means`, `population_stddevs` | `[3]float64` | training available rows only |
| `zero_variance` | `[3]bool` | true 时该 feature z 和 coefficient 均为 0 |
| `training_available_rows` | int | 与 fold counts 对齐 |

## Entity: CalibratedUtilityRule

| Field | Type | Rules |
|---|---|---|
| `rule_id` | digest | canonical rule digest |
| `scope` | enum | `fold | global_transfer` |
| `held_out_conversation` | int/null | fold 必填；global 必须 null |
| `training_conversations` | array | fold 不得包含 held-out；global=全部 LoCoMo |
| `training_question_ids_digest` | digest | 防止行级漂移 |
| `scaler` | FeatureScaler | training only |
| `intercept`, `coefficients[3]` | floats | finite；lambda=1 |
| `threshold` | float/sentinel | canonical `always` / `never` 或 finite midpoint |
| `threshold_candidate_count` | int | audit |
| `training_objective_receipt` | object | correct/net/token/calls + tie-break selection |
| `complexity` | object | 3 features、4 regression params、1 threshold |
| `routing_feature_digest` | digest | 必须排除 length/thinking/label fields |
| `locomo_in_sample_score_forbidden` | bool | global 必须 true |

**Decision function**:

```text
if signal unavailable: forced deep
else z = training scaler(signal.features)
     score = intercept + coefficients dot z
     deepen iff score > threshold
```

没有概率校准或 classifier accuracy 的语义承诺；`score` 只是 frozen expected-utility proxy。

## Entity: CalibrationFold

| Field | Type | Rules |
|---|---|---|
| `fold_id` | string | held-out conversation 的 canonical ID |
| `training_conversations`, `validation_conversation` | arrays/id | disjoint，union=全部 LoCoMo conversations |
| `rule` | CalibratedUtilityRule | scope=fold |
| `validation_decisions_digest` | digest | 每个 held-out decision 恰好一个 |
| fold counts | object | B/N/H、available/unavailable、keep/deepen/forced |
| fold outcomes | object | attempt + question-majority utility/accuracy/cost |
| `undefined_metrics` | object | `metric -> closed reason`；对应值必须为 null，fold 仍保留 |
| `stability_warnings` | array | 例如 training side 缺 BENEFIT/HARM；不自动等于 INVALID |
| `valid` / `invalid_reasons` | bool/array | 仅零 available training rows、数值/coverage/provenance/leakage 错误可置 invalid；fold invalid 使 diagnose stage INVALID |

10 个 folds 的 training/validation question identities 不得重叠；validation union 必须覆盖所有 questions/repetitions。

## Entity: UtilityDecision

在 correctness/gold loader 可访问前产生并持久化。

| Field | Type | Rules |
|---|---|---|
| decision identity | object | one per decision unit |
| `rule_id` | digest | confirm=对应 fold；transfer=global rule |
| `signal_status`, `features_digest` | values/digest | label-blind；原始三特征由 `shallow_attempt_id` join public AnswerAttemptReceipt 唯一复算 |
| `score`, `threshold` | number/sentinel | available only |
| `action` | enum | `keep_shallow | deepen | forced_deep` |
| `reason` | enum | `predicted_benefit | predicted_non_benefit | signal_unavailable` |
| `shallow_attempt_id`, `deep_attempt_id` | AnswerAttemptReceipt IDs | keep 无 deep；另两种必须有且至多一个；不得指向 FAILED provider unit |
| runtime cost fields | object | retrieval/query-embedding/logical-answer/provider-attempt calls、token charges、serial latency |
| `decision_digest` | digest | seal binding |

此 entity 永远不含 `correct`、`label`、`utility`、gold 或 category aggregate。score 阶段通过 decision identity join hidden
JudgeOutcome。

## Entity: EvaluationReceipt

一个 terminal `diagnose`、`confirm` 或 `transfer` 的完整报告。

| Section | Content |
|---|---|
| `validity` | manifest/source/call/decision/judge coverage、digest checks、conversation leakage、one/two-call limits |
| `population` | benchmark/questions/decision units/repetitions/categories、signal availability/reasons/length strata |
| `labels` | BENEFIT/NEUTRAL/HARM attempt counts；question-majority flips；historical regression单列 |
| `quality` | shallow/policy/fixed-deep correct + accuracy；by-category；policy-vs-control flips；exact McNemar；paired CI |
| `utility` | BENEFIT caught、HARM triggered、NEUTRAL triggered、net attempt utility、net majority questions |
| `cost` | policy/control total reported/charged token、ratio、logical answer/provider-attempt/retrieval/query-embedding/judge calls、latency distributions、preflight |
| `calibration` | fold summaries、coefficients/threshold stability；global rule只列 digest，不列 LoCoMo in-sample score |
| `gates` | 每个硬门的 observed/required/pass，固定顺序 |
| `verdict` | `GO | NO-GO | INVALID`；transfer 另有 portability status |
| `claim_boundary` | diagnostic vs fresh confirmation vs transfer；production=false |

Verdict precedence：artifact/coverage/leakage/call-contract 失败先判 INVALID；有效实验再按硬门判 GO/NO-GO。INVALID 不可被
转换为 NO-GO 以掩盖基础设施错误。

## Entity: PilotReceipt

全量 collect 前信号存在性 pilot 的 terminal receipt，只承担负向 kill-gate，不构成 held-out 成绩。

| Section | Content |
|---|---|
| `identity` | benchmark、conversation_ids（恰为前两条）、questions/repetitions 计数、source label-GO digests |
| `signal` | shallow 信号可用性、unavailable reasons、final length strata |
| `labels` | pilot 语料的 BENEFIT/HARM/NEUTRAL attempt 计数；BENEFIT 或 HARM 为 0 时 AUC 不可定义 |
| `auc` | 固定 ridge 得分对 BENEFIT 类别的 in-sample AUC（面积）；`null+reason` 当类别缺失 |
| `gate` | `auc_gate=0.65`、observed、passed；AUC<0.65 或不可定义 → NO-GO |
| `cost` | pilot 的浅/深 answer、judge calls/tokens/latency，作为 8/10 采集预算的对照 |
| `verdict` | `GO | NO-GO`（valid）；coverage/provenance/数值错误为 `INVALID` |
| `claim_boundary` | `signal_existence_pilot_only`；production=false |

pilot `verdict=GO` 只授权全量 collect；后续诊断的 held-out 权威仍来自 10-conversation LOCO cross-fit。pilot AUC 使用 pilot 自身数据拟合并计算（in-sample），其乐观性是该 kill-gate 的设计意图，不作为可部署性能。

## Entity: StageSeal

| Field | Rules |
|---|---|
| manifest digest | 与 stage manifest 精确匹配 |
| upstream digests | 与 source stage seal chain 匹配 |
| artifact digests | canonical sorted file/record digests |
| identity counts | expected logical calls、started/completed/failed attempts、retry counts、question/repetition/arm counts |
| usage totals | 按 arm/role 聚合 reported usage、conservative charges 与 unavailable judge usage，与 CallUnitRecord 重算一致 |
| rule/decision/report digests | applicable stages 必填 |
| terminal status | `COMPLETE | INVALID`; NO-GO 是有效 COMPLETE report 的 verdict，不是 seal failure |

下游 stage 必须先完整验证上游 seal，再读取任何 rule 或 hidden outcome。

## Aggregate rules

### Repetition to question majority

- 每题必须恰好 3 个 repetition outcomes；少/多/重复均 INVALID。
- 使用现有 `majorityCorrectness`；不得以 `>= half` 的偶数容错替代。
- policy 的每个 repetition answer 由当时 action 决定，再聚合；不能先聚合 signal 后做一次 question-level oracle decision。

### Category tolerance

对 category denominator `n`，policy 相对 fixed-deep 可接受的 correct loss 为：

```text
max(1, ceil(0.01 * n))
```

超出即硬失败；同时复用 exact McNemar + Holm `alpha=0.05`，任何显著负类别也失败。总体 correct 一题都不能低于
fixed-deep，因此类别容差只允许类别间净抵消中的小波动，不豁免总体硬门。

### Cost aggregation

- numerator 包括 preflight、全部 policy shallow、条件/forced policy deep 的所有 answer provider attempts：成功使用 reported
  input+output；失败若无 usage 则使用 32768 combined-token conservative charge；
- denominator 以同一规则包括同批 fixed-deep control 的所有 answer provider attempts；
- judge usage 不进入 ratio，但必须完整存在并单列；
- 固定本地 query embedding 对 generation-token ratio 的贡献为 not-applicable，不得写成 provider-reported zero；policy/control
  的 embedding calls、failures 与 latency 必须按臂单列；
- 纯 Go signal extraction/ridge 的 token=0，CPU time 可纳入 runner overhead latency；
- ratio 使用总 token 之比，不平均 per-question ratios。
