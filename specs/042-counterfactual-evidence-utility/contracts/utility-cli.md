# CLI Contract: Counterfactual Utility Stages

## Scope

本契约只扩展 `cmd/locomo-bench` 的显式研究模式。`--utility-stage` 为空时，所有新增代码不可触达，现有 benchmark
flag、journal、provider caller、输出与默认行为保持不变。该模式不构成 MCP/CLI 产品能力。

## New flags

| Flag | Type / default | Meaning |
|---|---|---|
| `--utility-stage` | string / empty | closed enum: `label | collect | diagnose | confirm | transfer`; empty=off |
| `--utility-source` | path / empty | `diagnose` 的 collect dir；`confirm` 的 diagnose dir；`transfer` 的 confirm dir |
| `--utility-label-source` | path / empty | `collect` 专用：已通过的历史 label-regression stage directory |
| `--utility-shallow-source` | path / empty | `label` 专用：历史 k30 root run directory |
| `--utility-deep-source` | path / empty | `label` 专用：历史 k150 root run directory |
| `--utility-shallow-k` | int / 30 | v1 shallow retrieval depth；正式 stage 只接受 30 |
| `--utility-deep-k` | int / 150 | v1 deep retrieval depth；正式 stage 只接受 150 |

固定且不可由 CLI 覆盖的 v1 参数：`logprobs=true`、`top_logprobs=2`、feature order、ridge `lambda=1`、
LOCO split、3 repetitions、diagnostic independent-effect floor `+25`、accuracy 0.90、token ratio 0.60、category tolerance、
Holm alpha 0.05、response limit 64 MiB、每个逻辑 answer/judge call 最多 3 attempts。
改变任一参数需要新 schema/protocol version，而不是加隐藏 flag。

## Common semantics

- `--run-dir` 是当前 stage 的新输出目录；不得等于任何 source directory，也不得包含其他 stage 的 terminal artifacts。
- 所有 path 在读取/创建前 canonicalize；source 与 output overlap、symlink escape 或 ambiguous nested layout 均拒绝。
- 正式运行使用一个全新 run-dir。resume 只允许 manifest digest 相同；COMPLETED logical call 可跳过，或在前一 attempt
  terminal retryable FAILED 且 attempt<3 时继续下一 attempt。孤立 STARTED、non-retryable/exhausted FAILED、attempt gap、
  completion 后继续、duplicate terminal 或未知文件冲突均 fail closed。
- utility stage 与 formal 022、fixed-gold、compare、adjudication、probe、navigation、assembly experimental modes 互斥。
- utility stage 不接受 `--retrieval both` 或多个 answer arms；固定为 hybrid recipe。
- stage 完成并写出一个有效 GO 或 NO-GO report 时 exit 0；配置、artifact、coverage、provider 或 protocol failure 为 INVALID，
  exit non-zero。NO-GO 不是基础设施错误，但下游 stage 必须拒绝它。
- terminal stdout 只打印 stage、validity、verdict、关键计数、token ratio 和 artifact path；不得打印 answer、prompt、endpoint、
  authorization header、raw provider body/error 或 coefficients 以外的敏感内容。

## Stage: `label`

### Purpose

零模型调用地回归 UtilityLabel 构造器，验证历史 040 同口径三次 majority 结果能精确复现 56 BENEFIT / 31 HARM。
该 stage 不产生可训练 ProbabilitySignalRecord，也不能作为后续 `diagnose` source。

### Required inputs

```text
--utility-stage label
--utility-shallow-source <k30-root>
--utility-deep-source <k150-root>
--run-dir <new-output-dir>
```

每个 source root 必须恰好包含 `run-1`、`run-2`、`run-3`，每个 repetition 中恰好一个可识别的 hybrid results JSONL。
两侧 question set、conversation/category identity、answer/judge regime 和 repetitions 必须可比。旧 artifact 缺少现代 provenance
字段时，loader 允许 `historical_provenance_incomplete=true`，但仍要求逐题 identity 和 denominator 完整；输出 claim 固定为
`label_constructor_regression_only`。

### Forbidden inputs

`--data`、`--store-dir`、provider/model env 和所有模型运行 flag都不需要也不得被此 stage 读取。

### Terminal condition

- 56 BENEFIT、31 HARM、1453 NEUTRAL：valid GO（只代表 label regression）。
- 任何其他计数：valid NO-GO；必须先解决口径/constructor，不得继续 collect。
- identity/provenance/coverage invalid：INVALID。

## Stage: `collect`

### Purpose

生成新鲜 LoCoMo paired calibration corpus：每个 decision unit 各一次 k30 answer+signal 和一次 k150 answer，再以同一 clean
judge 得到 repetition-level utility labels。它不拟合规则、不输出 policy verdict。

### Required inputs

```text
--utility-stage collect
--utility-label-source <label-GO-dir>
--data <locomo.json>
--dataset-format locomo
--run-dir <new-output-dir>
--store-dir <frozen-032-compatible-store-root>
--retrieval hybrid
--chunks
--chunk-quota 12
--force-answer
--judge-mem0-aligned
--trace-mediation=false
--repeats 3
--utility-shallow-k 30
--utility-deep-k 150
```

必须覆盖全部 cat 1–4 questions；`--conversations`、`--questions`、`--only-category`、`--only-questions`、
`--only-enumeration` 和 adversarial expansion 在正式 run 中拒绝。fixture tests 通过内部 config 构造小 cohort，不通过正式 CLI
放宽 denominator。

### Model environment

- `LOCOMO_PROVIDER=openai`；
- `LOCOMO_BASE_URL` 必须解析为 loopback host（本机 sidecar 或 SSH tunnel）；
- `LOCOMO_MODEL` 与 `LOCOMO_MODEL_REVISION` 必须非空，revision 不允许 `unverified:*`；
- `LOCOMO_MAX_MODEL_LEN=32768` 与 `LOCOMO_SERVER_CONFIG_DIGEST=sha256:...` 必须显式声明；runner 冻结声明并由 capability
  preflight 验证长上下文请求形态，不接受 16384 配置；
- `LOCOMO_NO_THINKING=0` 必须显式设置，以匹配冻结的 90% recipe；
- answer request 固定 `temperature_request_mode=omitted`；caller 不得发送 `"temperature":0`，manifest 不得宣称有效温度 0；
- `EMBED_BASE_URL` 必须是 loopback，`EMBED_MODEL`、`EMBED_MODEL_REVISION`、`EMBED_SERVER_CONFIG_DIGEST` 必须非空，
  `EMBED_MAX_NUM_SEQS=1` 必须显式声明；不得落到 Ollama/qwen3-embedding defaults；
- API key 仅从 `LOCOMO_API_KEY` 进入 Authorization header；本地无鉴权 sidecar 可使用进程内空 key，但不写 artifact；
- judge 使用现有 `JUDGE_*` resolution，但 resolved provider/model/revision/endpoint digest 必须冻结；
  `--judge-mem0-aligned` 必须为 true，utility stage 固定 `clean_final_answer=extract-final-answer-v1`，judge request 同样记录
  temperature field omission。其 credential 仍仅来自 env。

在第一条 benchmark call 前，runner 必须先验证每个 store 的 embedding model/dimension fingerprint 与显式 embedder 相符，
再用固定 query 验证 sidecar dimension、model identity 与两次 byte-identical 输出；mismatch 为 INVALID。随后 answerer capability
preflight 必须通过。成功 response 但缺少 logprob 能力写 receipt 后 valid NO-GO；provider/protocol failure按下述 retry 契约，
耗尽后 INVALID。

每个 answer/judge logical call 最多 3 attempts。仅 timeout、network error、HTTP 429、HTTP 5xx 可重试；context cancel/length、
其他 4xx、response-too-large、decode/schema/empty-answer、invalid usage、judge parse 不重试。每个 attempt 必须写 journal 并计
calls/latency/usage；失败 answer attempt 无 usage 时按 32768 combined-token conservative charge 计入所属臂 ratio。provider
failure 不能转换为 signal-unavailable。
label source 必须是 valid `label` GO seal；其 report/seal digest 写入 collect manifest，但历史答案本身不复制进 collect。

### Outputs

见 artifact contract 的 collect layout。有效 COMPLETE 必须包含 1540 questions、4620 paired decision units、4620 shallow +
4620 `paired_deep` 成功 answer receipts、对应 judge outcomes、4620 utility labels 和完整 seal；provider attempt 数允许因合法 retry
大于 logical call 数，二者必须分别对账。

## Stage: `diagnose`

### Purpose

纯离线读取 sealed collect artifact，固定训练侧 scaler/ridge/threshold，输出 10 个 fold rules 和 cross-fitted diagnostic verdict。

### Required inputs

```text
--utility-stage diagnose
--utility-source <complete-collect-dir>
--run-dir <new-output-dir>
```

不得要求 `--data`、`--store-dir` 或任何 provider/judge env；实现必须在解析这些 env 之前 early-dispatch。source 必须是有效
LoCoMo collect COMPLETE seal，并且 historical label regression receipt digest 已在 collect manifest 中声明为通过。

### Label boundary

diagnose 可读取 collect hidden judge/utility records；但每个 fold builder 的 API 分两步：先以 training conversation allowlist
加载训练 labels并冻结 rule，再以只有 public signals/identity 的 held-out records生成 decisions，最后 score function 才 join held-out
labels。测试用 spy loader 证明 threshold/rule freeze 前未访问 held-out labels。

### Terminal condition

- 全部 diagnostic hard gates通过：GO，写 10 fold rules + 一个 quarantined global transfer rule。
- 任一有效 hard gate失败：NO-GO，不写可供 confirm 使用的 active rule seal；不得运行 confirm。
- leakage、missing fold、tamper、数值异常：INVALID。

## Stage: `confirm`

### Purpose

用 diagnostic 后的一批新生成验证真实条件执行：LoCoMo policy 使用 held-out fold rules；独立 fixed-k150 control 与之同批运行。

### Required inputs

```text
--utility-stage confirm
--utility-source <diagnose-GO-dir>
--judge-mem0-aligned
<与 collect 完全相同的其余 LoCoMo dataset/store/recipe/model/judge flags and env>
--run-dir <new-output-dir>
```

source 必须是 valid diagnostic GO；feature/rule/model/prompt/store/question-set digests 必须与当前 manifest 相容。confirmation
run-dir 不得包含 collect answers，且 request seed/answer cache 不得从 collect 复用。

### Runtime call contract

每个 decision unit：

1. 一次 shallow retrieve + shallow answer（logprobs caller）；
2. available 且 score>`threshold`：一次 policy deep retrieve + answer；
3. unavailable：一次 policy deep retrieve + answer，reason=`signal_unavailable`；
4. 其余不执行 policy deep；
5. 独立 fixed-deep control 恰好一次；
6. policy decision 写入并 seal 后，score path才 join judge correctness。

因此 policy **logical answer calls** 每 decision unit只能为 1 或 2；control 恒为 1。provider attempts 可因预注册 retry 增加，
必须按臂完整记录并计费。control calls 不计入 policy numerator；fixed control 也使用 logprob-enabled caller而忽略其信号，
保持 caller parity。query embedding calls/latency 同样按 policy/control 臂单列，generation-token ratio contribution 标为不适用。

### Terminal condition

有效 artifact 后按所有 final LoCoMo hard gates给 GO/NO-GO。NO-GO 不产生 transfer authorization；GO seal 包含
`global_transfer_rule_digest`，但仍明确 `production_authorized=false`。

## Stage: `transfer`

### Purpose

在 LongMemEval-S 上验证 LoCoMo global rule 的 zero-retune non-regression。

### Required inputs

```text
--utility-stage transfer
--utility-source <confirm-GO-dir>
--data <longmemeval_s.json>
--dataset-format longmemeval
--run-dir <new-output-dir>
--store-dir <frozen-lme-store-root>
--judge-mem0-aligned
<其余 frozen LME k150 recipe flags and current answer/judge env>
```

source 必须是 valid LoCoMo final GO。runner 只加载 global rule public fields；不得加载 LME correctness before decisions，也不得
重新拟合 scaler/coefficients/threshold、改变 feature order或加 LME typed router。`--lme-typed-prompts` 的开关必须与 frozen
LME control recipe一致并写 manifest，不能因 transfer 结果改变。

### Terminal condition

- policy correct `>=` same-batch fixed-k150 且 artifact valid：transfer GO；报告成本/类别但不自动产品化。
- policy correct `<` fixed-k150：transfer NO-GO，claim=`locomo_mechanism_only_not_portable`。
- 任一 protocol/artifact/call failure：INVALID。

## Flag validation and default parity

- 任一 `--utility-*` auxiliary flag 在 `--utility-stage` 为空时是 usage error，避免“设置了但被忽略”。
- utility stage 必须在普通 benchmark journal 创建前 early-dispatch；ordinary run不得产生任何 utility artifact。
- utility stages拒绝会改变 answer path的其他实验 flag，包括但不限于 nav、IRIS、counter-refine、trace mediation、unified contract、
  abstain、filter/multi-query、rerank/PCIC、category top-k override。
- 实现测试必须捕获 default run 的 request body/result journal/regime digest，证明 utility off 时 byte-identical。
