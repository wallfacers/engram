# Artifact Contract: Counterfactual Utility v1

## Common rules

- Schema ID: `counterfactual-utility/v1`.
- JSON files are UTF-8, object keys canonicalized by the existing benchmark digest helper；JSONL 每行一个完整 object。
- JSON 不允许 NaN/Inf；threshold infinity 以 closed enum 表示，不序列化 IEEE infinity。
- Record identity排序仅用于 seal digest：conversation numeric、question index/ID、repetition、arm、unit state。并发 append 顺序
  不参与语义。
- 每个 terminal stage 先验证并 fsync 所有 records，再原子写 report，最后原子写 `seal.json`。没有 valid seal 的目录不可作 source。
- `public/` artifact 可被 label-blind runner/calibrator 的 public phase读取；`hidden/` 只允许 score/training-label phase读取。
- 文件权限遵循 run-dir；artifact 绝不包含 credential、Authorization header、endpoint原文、raw provider response/error、decoded
  token strings、full thinking completion 或 gold text副本。
- API/base URL 只保留 `sha256(trimmed value)`；model name/revision不是 secret，可明文记录。

## Directory layouts

### `label`

```text
<run-dir>/
├── manifest.json
├── hidden/
│   └── utility-labels.jsonl       # historical question-majority labels
├── label-report.json
└── seal.json
```

### `collect`

```text
<run-dir>/
├── manifest.json
├── preflight.json
├── call-journal.jsonl
├── public/
│   └── answer-attempts.jsonl      # shallow + paired deep; final answer/signal/usage, no correctness
├── hidden/
│   ├── judge-outcomes.jsonl
│   └── utility-labels.jsonl       # repetition-level
├── collect-report.json            # coverage/cost only, no routing claim
└── seal.json
```

### `diagnose`

```text
<run-dir>/
├── manifest.json
├── public/
│   ├── fold-rules.json
│   ├── crossfit-decisions.jsonl   # label-blind decisions
│   └── global-transfer-rule.json  # only when diagnostic GO; quarantined for LME
├── hidden/
│   └── diagnostic-score.json      # joins decisions to held-out labels
├── diagnostic-report.json
└── seal.json
```

### `confirm` and `transfer`

```text
<run-dir>/
├── manifest.json
├── preflight.json
├── call-journal.jsonl
├── public/
│   ├── answer-attempts.jsonl      # policy shallow/conditional deep + fixed-deep control
│   └── utility-decisions.jsonl    # written without correctness/gold
├── hidden/
│   └── judge-outcomes.jsonl
├── evaluation-report.json
└── seal.json
```

只有 `confirm` GO seal 可授权 `transfer`。`transfer` 不产生新 rule file；report 引用 confirm seal 内的 global rule digest。

## `manifest.json`

Logical shape（省略 data-model 已定义的 nested provenance fields）：

```json
{
  "schema": "counterfactual-utility/v1",
  "run_id": "...",
  "stage": "collect",
  "created_at": "2026-08-14T00:00:00Z",
  "source": {
    "stage": "label",
    "manifest_digest": "sha256:...",
    "seal_digest": "sha256:...",
    "report_digest": "sha256:..."
  },
  "benchmark": {
    "name": "locomo",
    "dataset_digest": "sha256:...",
    "question_count": 1540,
    "question_ids_digest": "sha256:...",
    "conversation_ids": [0, 1, 2, 3, 4, 5, 6, 7, 8, 9],
    "repetitions": 3
  },
  "recipe": {
    "retrieval": "hybrid",
    "shallow_k": 30,
    "deep_k": 150,
    "chunk_quota": 12,
    "force_answer": true,
    "trace_mediation": false,
    "thinking": "enabled"
  },
  "retrieval_provenance": {
    "store_digest": "sha256:...",
    "store_embedding_fingerprint": "sha256:...",
    "embedding_model": "BAAI/bge-large-en-v1.5",
    "embedding_revision": "...",
    "embedding_endpoint_digest": "sha256:...",
    "embedding_server_config_digest": "sha256:...",
    "embedding_max_num_seqs": 1,
    "embedding_dimension": 1024,
    "determinism_probe_digest": "sha256:..."
  },
  "answerer": {
    "provider": "openai-compatible-local",
    "model": "...",
    "revision": "...",
    "endpoint_digest": "sha256:...",
    "server_config_digest": "sha256:...",
    "temperature_request_mode": "omitted",
    "max_tokens": 8000,
    "max_model_len": 32768
  },
  "signal_protocol": {
    "mapping": "strict-final-suffix-v1",
    "logprobs": true,
    "top_logprobs": 2,
    "response_body_limit_bytes": 67108864,
    "features": [
      "final_mean_logprob",
      "final_p10_logprob",
      "final_mean_top1_top2_margin"
    ]
  },
  "calibration_protocol": {
    "split": "leave-one-conversation-out",
    "rule": "ridge-utility-v1",
    "lambda": 1,
    "threshold_objective": "max-majority-net-subject-to-token-ratio-v1"
  },
  "gates": {
    "minimum_net_questions": 25,
    "minimum_net_semantics": "independent-conservative-effect-floor",
    "quality_not_below_same_batch_deep": true,
    "minimum_accuracy": 0.9,
    "maximum_token_ratio": 0.6,
    "category_loss": "max(1,ceil(0.01*n))",
    "holm_alpha": 0.05
  },
  "judge": {
    "provider": "...",
    "model": "...",
    "revision": "...",
    "endpoint_digest": "sha256:...",
    "prompt_digest": "sha256:...",
    "mem0_aligned": true,
    "clean_final_answer": "extract-final-answer-v1",
    "temperature_request_mode": "omitted"
  },
  "call_policy": {
    "max_attempts": 3,
    "retryable": ["timeout", "network_error", "http_429", "http_5xx"],
    "unknown_answer_usage_charge": "max_model_len"
  },
  "store": {},
  "build": {}
}
```

Stage-specific source chain：

| Stage | Required source |
|---|---|
| label | two historical run roots digests |
| collect | label GO manifest/seal/report digests |
| diagnose | collect manifest/seal digests |
| confirm | diagnose GO manifest/seal/report + fold/global rule digests |
| transfer | confirm GO manifest/seal/report + global rule digest |

Source filesystem path 可记录为 operator-local relative display path，但不进入 portability claim；digest 是 authority。

## `preflight.json`

```json
{
  "schema": "counterfactual-utility/v1",
  "stage": "confirm",
  "embedding": {
    "status": "available",
    "store_embedding_fingerprint": "sha256:...",
    "model": "BAAI/bge-large-en-v1.5",
    "revision": "...",
    "dimension": 1024,
    "max_num_seqs": 1,
    "probe_digest": "sha256:...",
    "repeated_bytes_equal": true,
    "calls": 2,
    "latency_ms": 12
  },
  "answerer": {
    "status": "available",
    "request_digest": "sha256:...",
    "response_digest": "sha256:...",
    "mapping": {
      "generated_tokens": 5,
      "final_tokens": 2,
      "feature_names_digest": "sha256:..."
    },
    "usage": {"input_tokens": 12, "output_tokens": 5},
    "logical_calls": 1,
    "provider_attempts": 1,
    "latency_ms": 20,
    "unavailable_reason": ""
  },
  "judge_recipe": {
    "mem0_aligned": true,
    "clean_final_answer": "extract-final-answer-v1",
    "prompt_digest": "sha256:..."
  }
}
```

各 `status` closed enum `available | unavailable | invalid`。answerer unavailable 是端点成功但缺少 logprob 合约能力，stage
valid NO-GO；embedding model/dimension/determinism/store mismatch 是 INVALID。network/HTTP/timeout 等按最多 3 attempts 的 retry
契约处理，耗尽或 non-retryable protocol error 写 `invalid` 并使 stage INVALID。任何 response/probe text 不落盘。

## `call-journal.jsonl`

每行是 `CallUnitRecord` state event：

```json
{"schema":"counterfactual-utility/v1","logical_call_id":"sha256:logical...","unit_id":"sha256:attempt-1...","decision_key":{"benchmark":"locomo","conversation_id":0,"question_id":"conv-0-q-0","repetition":1},"arm":"shallow","state":"STARTED","attempt":1,"request_digest":"sha256:...","started_at":"..."}
{"schema":"counterfactual-utility/v1","logical_call_id":"sha256:logical...","unit_id":"sha256:attempt-1...","decision_key":{"benchmark":"locomo","conversation_id":0,"question_id":"conv-0-q-0","repetition":1},"arm":"shallow","state":"FAILED","attempt":1,"request_digest":"sha256:...","failure_reason":"timeout","retryable":true,"usage_status":"conservative_bound","ratio_token_charge":32768,"latency_ms":30000}
{"schema":"counterfactual-utility/v1","logical_call_id":"sha256:logical...","unit_id":"sha256:attempt-2...","decision_key":{"benchmark":"locomo","conversation_id":0,"question_id":"conv-0-q-0","repetition":1},"arm":"shallow","state":"STARTED","attempt":2,"request_digest":"sha256:...","started_at":"..."}
{"schema":"counterfactual-utility/v1","logical_call_id":"sha256:logical...","unit_id":"sha256:attempt-2...","decision_key":{"benchmark":"locomo","conversation_id":0,"question_id":"conv-0-q-0","repetition":1},"arm":"shallow","state":"COMPLETED","attempt":2,"request_digest":"sha256:...","answer_digest":"sha256:...","response_digest":"sha256:...","input_tokens":100,"output_tokens":20,"usage_status":"reported","ratio_token_charge":120,"latency_ms":250}
```

FAILED 的 `failure_reason` closed enum：

```text
timeout
context_canceled
network_error
http_429
http_4xx
http_5xx
context_length
response_too_large
decode_error
schema_error
empty_choice
empty_answer
invalid_usage
judge_parse_error
```

只有 `timeout | network_error | http_429 | http_5xx` 的 `retryable=true` 合法；最多 3 attempts，首次 COMPLETED 后禁止继续。
FAILED answer attempt 有 valid upstream usage 时照实写 reported usage/charge；否则写 `conservative_bound` 与 manifest
`max_model_len=32768`，不得伪造 input/output=0。judge FAILED 无 usage 可写 `usage_status=unavailable`、`ratio_token_charge=0`，
但 calls/latency 仍单列。不写 status body/message。HTTP body 固定上限为 64 MiB（67,108,864 bytes），实现读取至
`limit+1`；超限=`response_too_large`，不可重试。

## `public/answer-attempts.jsonl`

```json
{
  "schema": "counterfactual-utility/v1",
  "answer_attempt_id": "sha256:answer-receipt...",
  "logical_call_id": "sha256:logical...",
  "completed_unit_id": "sha256:attempt-2...",
  "decision_key": {
    "benchmark": "locomo",
    "conversation_id": 0,
    "question_id": "conv-0-q-0",
    "question_index": 0,
    "category": "single-hop",
    "repetition": 1
  },
  "arm": "shallow",
  "question_digest": "sha256:...",
  "retrieval": {
    "k": 30,
    "candidate_count": 30,
    "calls": 1,
    "latency_ms": 9,
    "embedding_calls": 1,
    "embedding_failures": 0,
    "embedding_latency_ms": 4,
    "embedding_token_usage_status": "not_applicable_to_generation_ratio"
  },
  "final_answer": "Oslo",
  "answer_digest": "sha256:...",
  "final_answer_digest": "sha256:...",
  "usage": {"input_tokens": 3500, "output_tokens": 600},
  "latency_ms": 13000,
  "signal": {
    "status": "available",
    "reason": "",
    "content_digest": "sha256:...",
    "token_trace_digest": "sha256:...",
    "generated_token_count": 600,
    "final_token_count": 2,
    "final_byte_start": 4200,
    "final_byte_end": 4205,
    "feature_names_digest": "sha256:...",
    "features": [-0.12, -0.20, 1.91],
    "final_trace": [
      {"byte_len":3,"sampled_logprob":-0.1,"top1_logprob":-0.1,"top2_logprob":-2.0},
      {"byte_len":2,"sampled_logprob":-0.14,"top1_logprob":-0.14,"top2_logprob":-2.06}
    ],
    "final_length_stratum": "2-4",
    "thinking_diagnostic": {"routing_eligible":false,"token_count":598,"mean_logprob":-0.8}
  }
}
```

Rules：

- answer arm closed enum 为 `shallow | paired_deep | policy_deep | fixed_deep`；`paired_deep` 只允许在 collect，明确表示用于
  calibration label/offline simulation 的 k150 answer。
- `answer_attempt_id` 标识成功的 logical answer receipt；`completed_unit_id` 必须指向该 logical call 唯一 COMPLETED provider
  attempt。retry FAILED unit 永远不能被 decision 当作 answer。
- `final_answer` 是唯一持久化 answer text；完整 content/thinking 只在内存中用于 mapping/judge 前处理。
- available signal 必须有恰好三个 finite features，且从 `final_trace` 可逐字节重算；聚合不一致使 seal invalid。
- unavailable signal 必须省略 `features`/`final_trace`，并给一个 closed reason；不能写 null/zero 假装 available。
- deep arms 不需要 signal；若 caller response 有 logprobs也不得把 control/deep signal 用作 rule 输入。
- 每条 retrieval receipt 必须由 counting embedding client 提供 calls/failures/latency；query embedding 不进 generation-token
  ratio，但不得因 engine 的 per-signal graceful degradation 而从报表消失。
- question/category metadata 可用于报表分层，但 calibration model API 只接受 `[3]float64`，测试断言无其他字段可被反射/读取。

## `hidden/judge-outcomes.jsonl`

```json
{
  "schema": "counterfactual-utility/v1",
  "decision_key": {},
  "answer_arm": "shallow",
  "answer_digest": "sha256:...",
  "correct": true,
  "judge_prompt_digest": "sha256:...",
  "judge_model_digest": "sha256:...",
  "usage": {"input_tokens": 90, "output_tokens": 5},
  "logical_calls": 1,
  "provider_attempts": 1,
  "latency_ms": 400
}
```

每个需要评分的 answer receipt 恰好一个 outcome；judge 必须只收到 `extractFinalAnswer` 后的 `final_answer`，并使用 manifest
冻结的 mem0-aligned prompt。answer arm identity 不进入 judge prompt。合法 retry 可使 provider attempts > logical calls；全部
attempt 成本由 call journal 重算，outcome 只对应最终成功逻辑调用。

## `hidden/utility-labels.jsonl`

```json
{
  "schema": "counterfactual-utility/v1",
  "decision_key": {},
  "shallow_answer_digest": "sha256:...",
  "deep_answer_digest": "sha256:...",
  "shallow_correct": false,
  "deep_correct": true,
  "utility": 1,
  "label": "BENEFIT",
  "source_manifest_digest": "sha256:...",
  "source_seal_digest": "sha256:..."
}
```

label stage 使用相同 shape，但 identity 不含 repetition 并加 `aggregation:"three-repetition-majority"`。Constructor 必须从 bool
truth table派生 utility/label，并重验 source answers；serialized utility/label 不作为 authority。

## `public/fold-rules.json`

```json
{
  "schema": "counterfactual-utility/v1",
  "rules": [
    {
      "rule_id": "sha256:...",
      "scope": "fold",
      "held_out_conversation": 0,
      "training_conversations": [1,2,3,4,5,6,7,8,9],
      "training_question_ids_digest": "sha256:...",
      "scaler": {
        "feature_names": ["final_mean_logprob","final_p10_logprob","final_mean_top1_top2_margin"],
        "means": [-0.4,-1.1,1.2],
        "population_stddevs": [0.2,0.5,0.6],
        "zero_variance": [false,false,false],
        "training_available_rows": 4000
      },
      "intercept": 0.01,
      "coefficients": [-0.1,-0.2,0.05],
      "threshold": {"kind":"finite","value":0.04},
      "lambda": 1,
      "complexity": {"routing_features":3,"regression_parameters":4,"threshold_parameters":1},
      "routing_feature_digest": "sha256:...",
      "training_objective_receipt": {}
    }
  ]
}
```

`threshold.kind` closed enum `finite | always | never`；always 表示每个 available record都 deepen，never 表示都 keep。
全局规则 shape 相同，`scope=global_transfer`、`held_out_conversation=null`、training conversations=全部 LoCoMo，且
`locomo_in_sample_score_forbidden=true`。

fold validator 必须证明：

- 恰好一个 rule per manifest conversation；
- training 与 held-out disjoint；
- all folds feature/rule/complexity/gate digests相同；
- coefficients/scaler仅由对应 training labels重算得到；
- global rule只在 cross-fit diagnostic GO 后存在。

训练侧缺 BENEFIT/HARM 只产生 stability warning，不得改变 rule family 或自动丢 fold；held-out 类别 denominator 为零的 rate
必须序列化为 `null` 并带 closed reason。零 available training rows、数值失败、coverage/provenance/leakage 错误才令 fold
invalid，任一 invalid fold 的后果是整个 diagnose stage INVALID，而不是 NO-GO。

## `public/crossfit-decisions.jsonl` and `public/utility-decisions.jsonl`

```json
{
  "schema": "counterfactual-utility/v1",
  "decision_key": {},
  "rule_id": "sha256:...",
  "signal_status": "available",
  "features_digest": "sha256:...",
  "score": 0.08,
  "threshold": {"kind":"finite","value":0.04},
  "action": "deepen",
  "reason": "predicted_benefit",
  "shallow_attempt_id": "sha256:...",
  "deep_attempt_id": "sha256:...",
  "runtime_cost": {
    "retrieval_calls": 2,
    "embedding_calls": 2,
    "embedding_latency_ms": 8,
    "logical_answer_calls": 2,
    "provider_attempts": 2,
    "reported_input_tokens": 12000,
    "reported_output_tokens": 1100,
    "ratio_token_charge": 13100,
    "serial_latency_ms": 26000
  },
  "decision_digest": "sha256:..."
}
```

Closed actions/reasons：

| Signal / score | Action | Reason | Policy answer calls |
|---|---|---|---|
| available and score > threshold | deepen | predicted_benefit | 2 |
| available and score <= threshold | keep_shallow | predicted_non_benefit | 1 |
| unavailable | forced_deep | signal_unavailable | 2 |

decision file不允许 `correct`、`gold`、`label`、`utility`、category aggregate或 hidden source path。`features_digest` 不是信号值
的唯一副本：validator 必须沿 `shallow_attempt_id -> public/answer-attempts.jsonl.signal.features` join 并复算 digest/score。
crossfit decision 的 deep attempt ID 指向 collect `paired_deep`（offline simulation）；confirm/transfer 指向真实
`policy_deep` conditional attempt。

## Reports

### Gate record

每个硬门使用统一 shape：

```json
{
  "name": "quality_not_below_fixed_deep",
  "observed": 1403,
  "required": ">=1403",
  "passed": true,
  "authority": "fresh-question-majority"
}
```

Gate order固定：validity、coverage/leakage、historical label regression receipt（diagnose only，不提供训练/held-out rows）、fresh
cross-fit net +25（diagnose only）、quality not below same-batch deep、absolute accuracy、token ratio、category tolerance、Holm
negative category、primary authorization / transfer non-regression。fresh diagnostic 必须同时报告
`deep_net_vs_shallow=D`、`policy_net_vs_shallow` 和 `required=max(25,D)`；`D<25` 时超越 deep 是刻意从严，不得把 25 改成 D。
report consumer不得只读最后一个 bool而忽略 earlier invalidity。

### Diagnostic report minimum fields

```text
verdict
claim = cross_fitted_diagnostic_only
historical_label_regression receipt（constructor audit only）
folds[10]
undefined fold metrics as null + closed reason; training class-absence warnings
signal availability + unavailable reasons + length strata
attempt B/N/H capture/trigger/net
question-majority shallow/policy/deep correct + accuracy
fresh D、independent +25 floor、policy required=max(25,D)
policy/deep total charged-token ratio + logical calls/provider attempts/query-embedding calls + latency simulation
category comparisons + exact McNemar/Holm
gate records
global_transfer_rule_digest (GO only; no LoCoMo in-sample score)
```

### Confirm report minimum fields

```text
verdict
claim = fresh_locomo_mechanism_confirmation
source diagnostic/rule digests
runtime signal/decision/degradation coverage
shallow/policy/fixed-deep majority quality and flips
BENEFIT captured / HARM triggered / NEUTRAL triggered / net
full policy vs control charged-token/logical-call/provider-attempt/retrieval/query-embedding/latency ledger (judge separate)
category + exact McNemar + paired CI + Holm
all gate records
production_authorized = false
```

### Transfer report minimum fields

```text
verdict
claim = external_transfer_non_regression | locomo_mechanism_only_not_portable
source LoCoMo confirmation/global-rule digests
no_retune = true
policy/fixed-deep quality + categories + statistics
signal coverage + charged cost/logical calls/provider attempts/query-embedding calls/latency
portable_claim_authorized (true only on transfer GO)
production_authorized = false
```

## `seal.json`

```json
{
  "schema": "counterfactual-utility/v1",
  "stage": "confirm",
  "status": "COMPLETE",
  "manifest_digest": "sha256:...",
  "source_seal_digest": "sha256:...",
  "artifact_digests": {"public/answer-attempts.jsonl":"sha256:..."},
  "counts": {
    "questions": 1540,
    "decision_units": 4620
  },
  "usage_by_arm": {},
  "decision_digest": "sha256:...",
  "report_digest": "sha256:...",
  "verdict": "GO",
  "global_transfer_rule_digest": "sha256:..."
}
```

正式 seal 的 `counts` 还必须由 journal 重算并加入 started/completed/failed、各 arm calls、signal available/unavailable、
logical calls/provider attempts/retries、query-embedding calls/failures、judge outcomes 等实际计数；`usage_by_arm` 分开 reported
usage 与 conservative token charge。因 conditional deep 数量未知，示例不伪造它们。合法 retryable FAILED attempt 后有成功
attempt 时仍可 COMPLETE；non-retryable/exhausted FAILED 或 orphan STARTED 不可 COMPLETE。`status=COMPLETE` 可配 GO 或 NO-GO；artifact
invalid 时不写伪 COMPLETE seal，而写 best-effort `status=INVALID` receipt 并 non-zero exit。下游只接受 COMPLETE。

## Validation order

严格按以下顺序，避免 hidden label提前进入：

1. parse manifest并重算 self/source/build/dataset/store/recipe digests；
2. validate public answer/signal/call coverage、attempt state machine、retry allowlist、token charges、embedding provenance/counters 与 numeric invariants；
3. 对 diagnose/confirm/transfer，生成或重放 public decisions并核对 decision digest；
4. validate decision seal/call limits；
5. 只有此时加载 hidden judge/utility records；
6. 重算 majority、quality、statistics、cost和所有 gates；
7. 重算 report与seal digests。

任一步失败立即 INVALID；不得返回 partial score、自动丢行、缩小 denominator或以旧 report 的 cached verdict 代替重算。
