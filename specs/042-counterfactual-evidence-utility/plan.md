# Implementation Plan: Counterfactual Evidence Utility Gate

**Branch**: `worktree-042-counterfactual-evidence-utility` | **Date**: 2026-08-14 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/042-counterfactual-evidence-utility/spec.md`

## Summary

在 `cmd/locomo-bench` 内新增一个默认关闭、仅用于评测的反事实效用协议。每个决策单元先以 top-k 30
作答，并通过同一次本地 OpenAI-compatible Chat Completions 调用取得 token log-prob；系统只使用最终答案
段的三个预注册特征，预测 top-k 150 重答相对浅答的效用。信号不可用时质量优先回退到深答。

实现分四道不可跨越的门：先对浅/深结果构造严格配对标签并采集新鲜概率信号；再以
leave-one-conversation-out（LOCO）方式交叉拟合固定三特征、固定 `lambda=1` 的岭效用回归；诊断达到深预算质量、
90% 绝对分及 60% 完整路径 token 门后，才用冻结的 fold rules 跑一批全新的 LoCoMo 条件浅→深确认；仅当
LoCoMo 最终 GO，才把只由 LoCoMo 校准数据拟合的全局规则原样迁移到 LongMemEval。有效实验未过硬门时立即 NO-GO；
provider/protocol/artifact/coverage 错误保持 INVALID。两者都不改换信号族或模型族。

042 worktree 已快进到主线 `afe9647`，其中包含删除 040/041 NO-GO 代码的 cleanup commits `3cff168`、`ac5c66c`。实现不复用
`confidence_gate.go`、`iterative_retrieval.go`、`adaptive_topk.go` 或 041 thinking wrapper。概率采集使用新的
harness-only 非流式 HTTP caller；`provider/` 公共事件和五个受保护引擎目录保持不变。

## Technical Context

**Language/Version**: Go 1.25.0，所有构建与测试使用 `CGO_ENABLED=0`

**Primary Dependencies**: Go 标准库；现有 `cmd/locomo-bench` prompt/retrieval/judge、JSON journal、usage、
majority、exact McNemar、Holm helpers；本地 vLLM 的 OpenAI-compatible `/v1/chat/completions` 与 `/v1/embeddings`；
无新增第三方依赖

**Storage**: 操作者指定的 session scratch / AutoDL data-disk run directory 内不可变 JSON/JSONL artifact；
复用现有只读 conversation stores；无 SQLite schema 或数据迁移

**Testing**: 测试先行的 Go unit/contract/integration tests（`httptest` 本地假端点、stub retrieval/answer/judge、
tamper fixtures）；`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`；`CGO_ENABLED=0 go build ./...`

**Target Platform**: Linux/WSL2 CLI；正式批次在 AutoDL 本地 vLLM sidecar 上运行，run-dir 必须位于
`/root/autodl-tmp/`，长任务必须 `setsid` 脱离

**Project Type**: 现有 Go benchmark CLI / research evaluation harness

**Performance Goals**: 纯 Go 信号聚合与三变量拟合相对模型调用为可忽略开销；全量 LoCoMo 处理 10 个
conversation、1540 题、3 repetitions（4620 个决策单元）；每个 policy 决策单元恰好 1 个浅答逻辑调用、至多 1 个深答逻辑调用；
最终 policy answerer 输入+输出 token 总量不超过同批固定 k150 的 60%

**Constraints**: 只允许 final-answer mean log-prob、p10 log-prob、mean top1-top2 margin 三个路由特征；
thinking 信号只报告不路由；固定 ridge `lambda=1`；训练数据内选阈值，held-out 后禁止调整；每个逻辑调用最多 3 次预注册
transient-only attempts 且逐次记账；信号缺失强制深答；temperature request field 省略；judge token 不计入运行时成本但单独记录；
query embedding 的 calls/latency 分臂记录；无 paid cloud reranker/recall；默认路径 byte-identical；
不得修改 `memory/ embedding/ provider/ store/ internal/`

**Scale/Scope**: LoCoMo cat 1–4 primary（1540 questions × 3 reps）和仅在 primary GO 后的 LongMemEval-S
transfer（500 questions × 3 reps）；研究结论只覆盖冻结模型/recipe 与该规模，不作生产或跨模型声明

## Constitution Check

*GATE: Phase 0 前已通过；Phase 1 契约完成后已复核。*

| Principle | Pre-design gate | Post-design evidence |
|---|---|---|
| I. Local-first, offline by default | PASS — 标签、校准、验证器是本地纯 Go；需要生成的阶段只接受显式配置、可替换的本地 OpenAI-compatible sidecar，默认关闭。 | PASS — CLI 契约不提供托管默认值；离线 `label`/`diagnose` 不读取 provider env；manifest 记录 endpoint digest 而不记录地址或 key。 |
| II. Engine/adapter separation | PASS — 概率信号仅用于 benchmark harness，不扩展 `provider.ProviderEvent`。 | PASS — 设计只新增 `cmd/locomo-bench/counterfactual_utility_*.go` 并最小修改 `main.go`；受保护目录 diff 必须为空。 |
| III. Contract-first & namespace isolation | PASS — 五阶段 CLI、artifact schema、错误和 gate 语义先于代码冻结；不新增产品 API 或 namespace 行为。 | PASS — [CLI contract](contracts/utility-cli.md) 与 [artifact contract](contracts/utility-artifacts.md) 固定 stage transition、digests、label-blind boundary、fail-closed 验证。 |
| IV. Evaluation regression gate | PASS — feature 本身就是同批浅/深/policy 配对门；不改变默认 retrieval/extraction/storage。 | PASS — 新鲜 confirmation 必须达到 fixed-k150 正确题数、90% 绝对分、类别容差和 60% token；exact McNemar/Holm 仍报告但不能替代硬门；LME 仅在 LoCoMo GO 后运行。 |
| V. Graceful degradation & honest scale | PASS — 单条信号不可映射则固定深答；端点整体不支持则预检后 NO-GO，避免烧全量。 | PASS — artifact 区分 `unavailable`、`invalid`、`NO-GO`；声明 10-conversation 校准、小样本和 benchmark-specific 边界；不作生产承诺。 |

无宪法违规，不需要 complexity exception。

## Phase 0 Research Conclusions

所有技术未知项已在 [research.md](research.md) 冻结：

- 使用 harness-only、非流式 OpenAI-compatible caller，请求 `logprobs=true`、`top_logprobs=2`；不修改公共 provider；
- 新 caller 与当前 wire shape 一样省略 `temperature`，不宣称有效温度为 0；answerer model revision、32768 context 与 sidecar config digest 冻结；
- query embedder 的 model/revision/endpoint/config、store model/dimension fingerprint 与 deterministic probe 冻结；正式 sidecar 要求 `max_num_seqs=1`；
- judge 固定 `mem0-aligned=true` 与 `extractFinalAnswer` clean-input mode，两项写入 manifest；
- 用 token bytes 与完整 completion 做严格逐字节映射，再以最后一个 thinking closing delimiter 确定 final-answer
  span；任一映射歧义、缺字段或非有限数值都判 `signal_unavailable`；
- 路由特征固定为 final mean log-prob、final p10 log-prob、final mean top1-top2 log-prob margin；答案长度只分层报告，
  thinking 同族聚合只作预注册旁路诊断；
- 训练目标是逐 repetition 的 `deep_correct - shallow_correct`（`-1/0/+1`）；headline 质量仍按每题 3-rep majority；
- 每个 outer fold 只用其余 conversations 标准化、拟合和选阈值；模型族为固定 `lambda=1` 三变量岭回归，
  threshold 在训练侧以“60% token 约束下最大净效用”选取，固定 tie-break；
- LoCoMo verdict 只接受 cross-fitted fold rules；在全部 LoCoMo 校准记录上拟合的 global rule 不参与 LoCoMo
  打分，只在 LoCoMo GO 后原样用于从未参与选型的 LongMemEval；
- endpoint 成功响应但缺少所需 logprob capability 时立即 valid NO-GO；transport/protocol retry 耗尽为 INVALID；个别有效回答的
  信号缺失则深答并计入成本；不回退到文本犹豫规则；
- answer/judge 每个逻辑调用最多 3 attempts，只重试 timeout/network/429/5xx；所有 attempts 记账，失败且无 usage 的 answer attempt 按 32768 token 上界计费；
- 正式 confirmation 必须是诊断后的一批新生成，控制臂与 policy 同 store、同 prompt、同 caller、同 judge、同批交错运行。

## Phase 1 Design

### Stage flow and leakage boundary

```text
historical label audit (must reproduce 56 BENEFIT / 31 HARM)
                                  |
fresh LoCoMo collect: k30 signal + k30/k150 judged pairs
                                  |
offline LOCO diagnose: 10 held-out fold rules + global transfer rule
                                  |
                     diagnostic GO only
                                  |
fresh LoCoMo confirm: fold rule -> keep shallow / conditional deep
                    versus same-batch fixed k150
                                  |
                       LoCoMo final GO only
                                  |
LongMemEval transfer: unchanged global LoCoMo rule, no LME retuning
```

`collect` 完成并 seal 后才允许标签 loader 被 `diagnose` 使用；`confirm` runner 只读 public fold rules，不能读取
collect labels；`transfer` runner 只读 global rule 和 LoCoMo GO receipt，不能读取 LME labels。每个阶段输出绑定上游
manifest/seal digest，避免更换 dataset、prompt、model、judge、store 或代码后继续 resume。

### Signal collection and call parity

`counterfactual_utility_http.go` 复制现有 harness-side HTTP caller 的最小模式，而不是修改 `provider/`。请求与当前
answer recipe 保持相同 messages、model、max tokens、thinking 配置和 `temperature` 字段省略语义，只额外设非流式、
`logprobs=true`、`top_logprobs=2`。同一 caller 也用于 fixed-deep control，避免 transport 形态成为 arm 差异。

预检使用一个非 benchmark fixture，验证 content、usage、token bytes、sampled logprob 和至少两个 top alternatives；
HTTP response 以 64 MiB 为硬上限。其 attempts/token/call/latency 作为一次性 signal overhead 计入 policy 总账。正式回答只
持久化 final answer、usage、latency、sanitized numeric trace、聚合特征和 digest，不持久化 reasoning 文本、decoded token
strings、raw response、endpoint 或 key。

在 answer capability probe 前，runner 先冻结并验证 embedding 与 judge provenance：逐 store 汇总
`memory_embeddings(model,dims)`，与显式 `EMBED_MODEL` 和固定 probe 的输出维度比对；同一 probe 连做两次必须 byte-identical，
operator declaration 必须为 `max_num_seqs=1`。judge 必须显式启用 mem0-aligned，且固定使用 clean final answer prompt。
任一模型/revision/endpoint/config/store fingerprint/mode 不匹配都在 benchmark call 前 INVALID。

call journal 以一个 `logical_call_id` 关联至多 3 个 attempt。timeout、network error、HTTP 429/5xx 才可重试；其他错误或
重试耗尽使 stage INVALID。每个 attempt 独立 STARTED/terminal 并按臂累计 call/latency/usage；失败 answer attempt 无 usage
时用冻结的 32768 max-model-len 作 combined-token 保守上界。孤立 STARTED 仍因成本未知而 INVALID。

### Calibration and frozen rules

`diagnose` 生成 10 个 outer-fold rules。对 fold `c`：只在 `conversation != c` 的 available records 上计算均值/标准差、
拟合三变量 ridge；signal-unavailable records 不参与拟合，但在训练阈值模拟和 held-out policy 中一律 forced-deep。
候选阈值是训练 score 的相邻中点及 never/always sentinels；在训练侧完整 token ratio `<=0.60` 的候选中，按
question-majority 净效用最大、token 最少、deep calls 最少、threshold 最高的固定顺序选择。没有满足 0.60 的候选是有效
NO-GO；数值失败、conversation 泄漏、fold/coverage/provenance 缺失是 stage-level INVALID。两者都不换模型。

训练侧缺 BENEFIT 或 HARM 不会让固定数值 ridge 自动失效；它会继续拟合并记录 stability warning。held-out 某类 denominator
为零时保留 fold，相关 rate 写 `null + reason`，count/quality/net/cost 继续进入合并门。只有零 available training rows、
数值求解失败、fold/coverage/provenance 缺失才使整个 diagnose INVALID。

cross-fitted held-out 结果必须同时满足：相对浅预算净增至少 25 题、正确题数不低于同批 k150、绝对分至少 90%、
模拟完整路径 token ratio 不超过 0.60、类别损失不超过预注册容差。否则停止。通过时再以同一算法在全部 LoCoMo
校准记录上拟合一个 global rule 并封存；该规则的 LoCoMo in-sample 结果明确不报告为成绩。
这里 `+25` 是独立的保守效应量门，不是历史 040 的替代数据；若 fresh deep-vs-shallow 净值 `D<25`，有意要求 policy
比 deep 多至少 `25-D` 题，报告同时展示 `D` 与 25。

### Fresh confirmation and verdict

`confirm` 对每个 `(question_id, repetition)` 先执行 shallow answer。信号可用且 fold score 大于阈值时至多执行一次
deep answer；信号不可用也执行一次 deep answer并标记 `forced_deep_signal_unavailable`；否则保留 shallow。
独立 fixed-k150 control 与 policy 同批交错执行。三次 decision result 聚合为一个 question majority，再计算总体、类别、
BENEFIT/HARM flips、exact McNemar、95% paired delta、完整 token/call/latency。

headline token ratio 聚合每个 answer provider attempt 的实际 usage；失败 attempt 无 usage 时按 32768 token 保守上界计入所属臂。
固定本地 query embedding 不进入 generation-token ratio，但 policy/control 的 embedding calls 与 latency 独立报告；judge 成本同样单列。

LoCoMo final GO 是合取门：artifact 完整；policy correct `>=` same-batch k150；policy accuracy `>=0.90`；
`policy_answer_and_signal_tokens / fixed_k150_answer_tokens <=0.60`；每类别 correct loss 不超过
`max(1, ceil(0.01 * category_questions))`；不存在 Holm-corrected `alpha=0.05` 显著负类别。统计不显著不能豁免前述硬门。

### Transfer boundary

`transfer` 只接受 `dataset-format=longmemeval`、有效 LoCoMo final-GO seal 和其中冻结的 global LoCoMo rule。
不得重新标准化、拟合、选 threshold 或按 LME 类别改规则。LME non-regression 要求 policy correct 不低于同批 fixed-k150，
并报告类别、token、calls、latency 和信号覆盖；失败只把最终结论标为“LoCoMo mechanism GO / transfer NO-GO”，阻止
可移植性和产品晋级，不改写 LoCoMo verdict。

## Project Structure

### Documentation (this feature)

```text
specs/042-counterfactual-evidence-utility/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── utility-cli.md
│   └── utility-artifacts.md
├── checklists/
│   └── requirements.md
└── tasks.md                         # Phase 2 output; not created by speckit-plan
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── counterfactual_utility.go             # labels, decisions, gates, cost/stat summaries
├── counterfactual_utility_http.go        # harness-only logprob Chat Completions caller + strict span mapping
├── counterfactual_utility_calibration.go # fixed 3-feature ridge, train-only thresholding, LOCO cross-fit
├── counterfactual_utility_artifact.go    # schemas, canonical digests, journal/seal validation
├── counterfactual_utility_eval.go        # collect/confirm/transfer runners and label-blind stage boundaries
├── counterfactual_utility_cli.go         # stage validation, dispatch, local-sidecar wiring
├── counterfactual_utility_test.go
├── counterfactual_utility_http_test.go
├── counterfactual_utility_calibration_test.go
├── counterfactual_utility_artifact_test.go
├── counterfactual_utility_eval_test.go
└── main.go                               # minimal opt-in flags + early dedicated-mode dispatch

memory/ embedding/ provider/ store/ internal/  # MUST remain unchanged
```

**Structure Decision**: 保持在现有 `cmd/locomo-bench` `package main` 内，以直接复用未导出的 prompt、retrieval、
judge、majority 和统计 helpers，同时把 HTTP、校准、artifact、runner 分文件隔离。公共 provider 不具备 logprob event，
为研究模式扩展它会违反受保护引擎边界；独立 harness caller 是最小、默认关闭且可删除的实现。

## Implementation Strategy

1. 以已更新的 `afe9647` 基线开始；Phase 2 的第一个前置任务在 AutoDL 只读核验两个历史 root 恰有 `run-1/2/3`、每 rep
   恰有一个 hybrid results JSONL 且 denominator/identity 可比，不满足即停。随后写 label truth table、HTTP schema/span mapping、
   artifact tamper 和 LOCO leakage 的失败测试。
2. 实现 `label`、embedding/answer/judge preflight 与 `collect`；跑历史 040 label regression，未复现 56/31 则停。
3. 实现固定 ridge/threshold 和 `diagnose` seal；在读取任何 held-out 汇总前冻结 feature/model/complexity digests。
4. 仅在 diagnostic GO 后实现/启用 `confirm` conditional runner；以 fixture 验证 unavailable→deep、最多两次 policy answer、
   默认路径 parity 与完整 token ledger。
5. 仅在 LoCoMo confirmation GO 后运行 `transfer`；无论 verdict 如何都记录 LME non-regression，不做 LME retuning。
6. 本地执行 package test、全仓 build 和 protected-directory diff；模型侧正式 eval 在 AutoDL data disk 脱离运行，输出独立 verdict。

## Complexity Tracking

无 Constitution 违规。
