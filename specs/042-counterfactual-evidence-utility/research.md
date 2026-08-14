# Research: Counterfactual Evidence Utility Gate

本文件冻结 042 的可证伪协议。依据包括 040/041 的同栈实测、仓库中已经通过 alphaXiv 逐篇核验的
[top-k 缩减研究](../../docs/research/retrieval-budget-reduction-directions.md)、现有 benchmark 代码，以及 vLLM
官方协议文档。论文只提供机制先验；LoCoMo/LongMemEval 以外的绝对分不迁移为本 feature 的成绩。

## Decision 1: 目标是深检索的增量效用，不是浅答正确率

**Decision**: 每个 fresh calibration 决策单元定义为 `(benchmark, conversation_id, question_id, repetition)`。
浅答和深答各用相同 recipe 独立生成并以同一 clean judge 判定：

```text
utility = deep_correct - shallow_correct
BENEFIT = +1   (false -> true)
NEUTRAL =  0   (false -> false or true -> true)
HARM    = -1   (true -> false)
```

校准以 repetition-level utility 为监督目标，因为运行时每个 repetition 只能看到自己的浅答信号并至多决定一次
深答。headline 质量仍把三个 repetitions 按 `question_id` 聚合为 majority；SC-003 的“+25 题”和所有最终质量门
都在 question-majority 层计算，不能把 4620 次调用当作独立 benchmark 题。

历史 040 k30/k150 三次 majority 只承担标签构造回归：必须得到 56 BENEFIT、31 HARM、1453 NEUTRAL。
它没有所需概率信号，不能作为 042 的训练数据。最终诊断和 verdict 只接受新鲜、同批、同口径的 paired run。
fresh collect/confirm/transfer 的 judge regime 固定为 `mem0-aligned=true`，且 judge user prompt 只接收
`extractFinalAnswer` 后的 clean final answer；两项 mode 及 prompt digest 都进入 manifest，不能依赖 CLI default。

**Rationale**: 040 的 56 次救回同时伴随 31 次反害。预测“浅答错”会把“深答也错”与“深答能救回”混在一起；
只预测 BENEFIT 但不惩罚 HARM 又会复现 041 的过度加深。三值效用直接等于一次深答决策对正确数的增量。

**Alternatives rejected**:

- 浅答 correctness 二分类：目标错位，无法区分 BENEFIT 与错→错 NEUTRAL。
- 只对 56 个历史救回题训练：选择偏差，完全看不到 HARM 和大多数 NEUTRAL。
- 把每个 question 的 majority label 复制给三个 repetitions：把其他调用的结果泄漏给单次运行时决策。

## Decision 2: 用独立 harness HTTP caller 取得真实生成概率

**Decision**: 新增 benchmark-only、非流式 OpenAI-compatible Chat Completions caller。它要求
`LOCOMO_PROVIDER=openai`，沿用当前 answerer 的 base URL、model、system/user messages、`max_tokens` 和
thinking-on recipe。现有 provider 路径虽把 Go `Temperature` 置为零，但 OpenAI wire 使用 `omitempty`，因此实际请求
**不携带** `temperature`；新 caller 必须同样省略该字段，manifest 写
`temperature_request_mode=omitted`，不得把它误报为有效温度 0。由于有效采样由服务端决定，answer model revision、
`max_model_len=32768` 与 sidecar configuration digest 一并冻结。相对现有 wire shape只增加：

```json
{
  "stream": false,
  "logprobs": true,
  "top_logprobs": 2
}
```

不得向 `provider.Request`、`provider.ProviderEvent` 或任何 provider adapter 增加 logprob 字段。所有 utility stages
默认关闭；普通 benchmark 继续使用现有 streaming caller。fixed-k150 control 和 utility policy 都使用新的 caller，
使 transport/request shape 在两个实验臂之间一致。

vLLM 的官方 Chat Completion request schema支持 `logprobs` 和 `top_logprobs`，响应按 generated token 返回
sampled `logprob`、bytes 和 `top_logprobs`；官方 serving 实现将它转换为 OpenAI-style logprob content：

- [vLLM Chat Completion protocol](https://docs.vllm.ai/en/latest/api/vllm/entrypoints/openai/chat_completion/protocol/)
- [vLLM Chat Completion serving / logprob construction](https://docs.vllm.ai/en/latest/api/vllm/entrypoints/openai/chat_completion/serving/)
- [vLLM OpenAI-compatible server](https://docs.vllm.ai/en/latest/serving/online_serving/openai_compatible_server/)

不得设置 `include_reasoning=false`：vLLM 官方 reasoning 文档明确说明该配置会同时抑制 per-token metadata。端点可用
独立 `reasoning` 字段，也可把 `<think>...</think>` 放在 content 中；042 的 mapper 不依赖某一种表示：
[vLLM reasoning outputs](https://docs.vllm.ai/en/latest/features/reasoning_outputs/)。

**Capability preflight**: 正式 collect/confirm/transfer 在任何 benchmark call 前执行一次固定非 benchmark prompt，
并验证：HTTP 200、恰好一个 choice、非空 content、usage 非负、token bytes 可重建生成 suffix、每个 final token 有有限
sampled logprob 和至少两个有限 top alternatives。成功 response 但缺少所需 logprob/mapping 能力才是 valid
unavailable/NO-GO；transport、HTTP、timeout、decode、schema 或
invalid usage 按有界 retry 处理，耗尽后是 INVALID，不能混成机制 NO-GO。
preflight 的全部 attempts/calls/tokens/latency 计入 policy signal overhead。response body 上限固定为 64 MiB，实现用
`io.LimitReader(limit+1)` 检测超限；该上限覆盖 `max_tokens=8000`、reasoning 和逐 token top-2 logprob 的合法非流式响应，
超限以 `response_too_large` 失败，不能截断后继续解析。

**Retrieval/embedder preflight**: 正式 hybrid stage 不接受 `EMBED_*` defaults。manifest 在调用前冻结 embedding model、
operator-supplied revision、endpoint digest、sidecar configuration digest、declared `max_num_seqs=1` 和预期维度。runner
从每个只读 conversation store 的 `memory_embeddings(model,dims)` 计算 store embedding fingerprint，再用固定 probe query
调用 sidecar 两次；只有 store model 与 `EMBED_MODEL` 完全一致、维度一致、两次 probe byte-identical 才继续。任何 store
混合 model/dimension、endpoint model mismatch 或不确定 probe 都在首条 benchmark call 前 INVALID。`max_num_seqs=1`
是本批的确定性前置，不能只记录而不检查 operator declaration。

**Rationale**: 当前公共 provider event 只有 text/reasoning/usage，没有 token logprob。为一次 eval 实验扩展公共 engine
契约违反“engine untouchable”边界；仓库已有 `nav_http.go` / `trace_http.go` 的 harness-only OpenAI-compatible 先例。
非流式响应把 content、usage 和整段 logprobs 放在一个原子 response 中，显著简化严格映射和 crash journal。

**Alternatives rejected**:

- 修改 `provider/`：扩大公共契约和测试面，且 042 明确是 research-only。
- 从 streaming SSE 拼 logprobs：可行但状态机和中断边界更复杂，首轮没有收益。
- 调额外 closed-book/probe 模型：新增一次生成，不满足“正常浅答同步信号”与 60% 成本目标。
- hosted confidence/rerank 服务：违反 local-first 与 paid cloud death rule。

## Decision 3: 严格映射 final-answer token，并冻结三个特征

**Decision**: mapper 先按 response 顺序拼接每个 token 的 `bytes`。若 bytes 缺失，则只在 token 是有效 UTF-8 且
重新编码无歧义时使用 `token`；其余情况不可用。然后：

1. response `message.content` 必须是重建 token bytes 的精确 suffix（允许前缀属于独立 reasoning）；
2. 对 content 调用现有 `extractFinalAnswer`，所得非空 final text 必须又是 content 和重建 bytes 的精确 suffix；
3. final text 的首尾必须落在 token boundary；不得按字符比例切一个跨界 token；
4. final span 内每个 token 必须有有限 sampled logprob 和至少两个不同候选的有限 logprob；
5. 任一条件失败，整条记录为 `signal_unavailable`，不能部分填零、用长度代替或回退文本规则。

路由特征顺序和定义固定为：

| Index | Feature | Definition |
|---|---|---|
| 0 | `final_mean_logprob` | final span sampled token logprob 的算术均值 |
| 1 | `final_p10_logprob` | final sampled logprob 升序后的 nearest-rank 10th percentile，index=`ceil(0.10*n)-1` |
| 2 | `final_mean_top1_top2_margin` | 每个位置最大与次大 top alternative logprob 之差的均值 |

全部在 log space 计算，不 `exp`。`final_token_count` 仅用于审计和长度 strata（1、2–4、5–16、17+），不进入模型。
若有 reasoning prefix，可用同一聚合式生成 `thinking_diagnostic`，但其字段写死 `routing_eligible=false`，任何 fold
不得读取它。短答案、数字和专名因此会被单独报告，而不会成为隐藏第四特征。

**Rationale**: mean 代表序列整体支持度，p10 捕获局部薄弱 token，top1-top2 margin 捕获候选竞争；三者覆盖 041
遗留的“最终答案概率、低概率尾部、候选间隔”而保持极低容量。严格 suffix + boundary 规则避免把 thinking 概率、
chat-template 标签或 tokenizer 解码误差混入 final answer。

**Alternatives rejected**:

- sequence probability product：长度偏置和数值下溢。
- minimum logprob：对单个异常 token 过敏；p10 是预注册的稳健尾部统计。
- token count 入模：可能只学到答案类型/类别捷径；长度只作分层。
- held-out 后在 final 与 thinking 特征之间择优：明确的 multiple-comparison / post-hoc 泄漏。
- 更大的 top-logprobs 或整词概率：增加 payload 和 tokenizer 特例，首轮无必要。

## Decision 4: 固定三变量 ridge 直接拟合期望效用

**Decision**: 规则族固定为三变量 ridge least-squares utility regression，无第三方 ML 依赖、无模型搜索。
对每个 outer LOCO fold：

1. 只取其余 conversations 中 signal available 的 training rows；unavailable rows 不拟合，但在策略模拟中 forced-deep；
2. 每个 feature 用 training population mean/std 标准化；零方差 feature 的标准化值和系数固定为 0；
3. 令 `y in {-1,0,+1}`，中心化 `y`，求解
   `beta = (Z^T Z + I)^-1 Z^T (y - mean(y))`，即固定 `lambda=1`；intercept=`mean(y)`，不惩罚；
4. 用带 partial pivot 的确定性 3x3 Gaussian elimination；非有限输入、零个 available training row 或不可逆数值结果
   使该 fold INVALID；训练侧缺 BENEFIT 或缺 HARM 不改变模型族，仍按数值 `y` 拟合并写 stability warning；
5. runtime score=`intercept + beta dot z`，且仅当 `score > threshold` 才 deepen；相等时 keep-shallow。

低容量上限因此固定为 4 个自由参数（intercept + 3 coefficients）和一个训练内 threshold。没有 learning rate、树深、
hidden units 或候选 classifier 可调。

held-out fold 即使某个真实类别为零也必须保留；对应 precision/recall/rate 写 `null` 与 closed reason，count、净效用、
question-majority quality 和成本仍参与合并门。类别稀疏本身不升级为 INVALID；缺行、coverage/provenance 不完整或确定性
求解失败才按 stage-level INVALID 处理。

**Rationale**: 数值标签本身就是深答的单位正确数增量，线性回归直接估计 `E[utility | signal]`，比“先预测浅答错再
间接推效用”更符合目标。固定 ridge 使三维小样本稳定且可用标准库实现。conversation-level outer split隔离同一长对话
里的共享实体、人物和表述，避免 question-random split 的高估。

**Alternatives rejected**:

- logistic “浅答是否错”：仍是错误目标。
- multinomial logistic：参数更多且最终还要把三类概率重组为效用；首轮没有必要。
- tree/boosting/neural/LLM router：容量和搜索空间不符合用户选择的预注册低容量规则。
- 全量 LoCoMo 拟合后再报 LoCoMo 成绩：训练内泄漏。

## Decision 5: 阈值只在 training side 按完整成本约束选择

**Decision**: 每个 fold 的 threshold candidates 是 unique training scores 的相邻中点，再加 `+Inf`（never deepen）和
`-Inf`（always deepen）。每个候选在 training conversations 上按三个 repetitions 重建 question-majority policy：

- available 且 `score > threshold`：policy 使用 paired deep outcome/cost；
- available 且未触发：使用 shallow outcome/cost；
- unavailable：forced-deep，成本为 shallow + deep；
- policy token charge 是 shallow 的全部 provider attempts，加条件 deep 的全部 provider attempts；reported usage 与无 usage
  FAILED attempt 的 32768 conservative bound 使用 Decision 7 的同一规则；纯 Go rule 为 0 token；
- fixed-deep denominator 对相同训练 rows 的 deep provider attempts 使用同一 charge 规则。

先丢弃 token ratio `>0.60` 的候选，再用以下固定 lexicographic tie-break：

1. 最大化 question-majority `policy_correct - shallow_correct`；
2. 最小化 total policy tokens；
3. 最小化 deep answer calls；
4. 选择更高 threshold；
5. IEEE value 完全相同时按 canonical candidate index。

所有 feature scaling、coefficients 和 threshold 都只能来自 training side。held-out conversation 的 label、correctness、
gold、category aggregate 和成本不得进入选择。

**Diagnostic GO** 是 10 个 held-out folds 的 cross-fitted 合并结果同时满足：

- 历史 label audit 精确复现 56 BENEFIT / 31 HARM；
- 10 folds 完整、conversation overlap 为 0、全部决策有来源；
- policy majority correct 至少比 shallow 多 25 题，且不低于同批 deep；
- policy absolute accuracy `>=0.90`；
- simulated full-path token ratio `<=0.60`；
- 每类别 loss 不超过 `max(1, ceil(0.01*n_category))`，且没有 Holm `alpha=0.05` 显著负回退。

任一失败立即 NO-GO；不运行 fresh confirmation，也不换 threshold objective。

`+25` 不是历史 040 的 deep 增益估计，而是独立、预注册的保守效应量下限。令 fresh collect 中
`D = deep_correct - shallow_correct`：合取门等价于 `policy_net >= max(25, D)`。因此 `D<25` 时有意要求 policy
通过 repetition-level keep/deepen 选择比 deep 多至少 `25-D` 题；`D>25` 时同批 deep non-regression 门更严格。
报告必须同时展示 `25`、`D` 和实际 policy net，避免把批次漂移伪装成信号收益。

**Rationale**: 单独最大化分类 accuracy 会偏向占绝对多数的 NEUTRAL；单独最大化 BENEFIT recall 会吞掉 HARM 和预算。
训练侧直接模拟最终 majority 正确数和完整路径成本，目标与最终产品价值一致。固定 60% 可行域把“事后发现省不了”前移
到便宜的 offline diagnostic。

## Decision 6: 用 cross-fit confirmation，global rule 只做外部迁移

**Decision**: diagnostic GO 后封存两类规则：

- `fold_rules[conversation_id]`：各自由另外 9 个 LoCoMo conversations 拟合，只用于一批全新 LoCoMo confirmation；
- `global_transfer_rule`：同一固定算法在全部 LoCoMo calibration rows 上拟合，标记
  `locomo_in_sample_score_forbidden=true`，只允许在 LoCoMo final GO 后用于 LongMemEval。

confirmation 不能复用 collect 的答案。每个 policy decision 真实执行 shallow call，再依据对应 fold rule选择 keep/deepen；
独立 fixed-k150 control 在同批交错运行。signal-unavailable 强制 deep。decision 在 judge correctness 被读取前写入并 seal，
score 阶段才加载 label/judge fields。

LoCoMo final GO 的硬门：policy correct `>=` fixed-deep、accuracy `>=0.90`、token ratio `<=0.60`、类别容差通过；
同时报告 exact McNemar 和 paired CI，但“未显著回退”不能覆盖少一题的事实。

LME 只接受 global LoCoMo rule 和 LoCoMo GO seal；不重新计算 scaling、不选 threshold、不按 LME question type 调规则。
LME quality correct 少于同批 fixed-deep 即 transfer NO-GO；LoCoMo mechanism verdict 保留，但不得作 portable/product claim。

**Rationale**: fold-specific confirmation 在同一个 benchmark 上仍保持 conversation label 隔离；global rule 对 LoCoMo 是训练内，
所以只能去一个未参与选择的 benchmark 做真正外部 transfer。该边界诚实地区分“机制在 LoCoMo 成立”与“有一个可迁移规则”。

**Alternatives rejected**:

- collect cross-fit 结果直接当 e2e：同一批生成同时被用于拟合和模拟，缺少新鲜确认。
- confirmation 前用全部 LoCoMo refit：会让 LoCoMo final score 训练内泄漏。
- LME 按结果重新标准化或调 threshold：把 transfer set 变成开发集。
- LME 先跑：若 primary 已失败，继续烧成本没有信息价值。

## Decision 7: 成本、调用、延迟和失败语义

**Decision**: headline token ratio 使用总量比而非逐题 ratio 平均：

```text
answer_attempt_charge = upstream input+output usage when valid
                      | answerer max_model_len conservative bound when a failed attempt has no usage

policy_runtime_tokens = sum(preflight + shallow + conditional/forced-deep answer_attempt_charge)
fixed_deep_tokens = sum(all control-deep answer_attempt_charge)
token_ratio = policy_runtime_tokens / fixed_deep_tokens
```

judge input/output 不进该比例，因为它是评测器而非运行时路径；但 judge calls/tokens/latency 必须单列。模型端返回 logprobs
是 response metadata，不额外制造 generation token；其网络 payload 和解析时间反映在 shallow call latency 中。固定本地
query-embedding sidecar 不是 generation call，hard ratio contribution 按协议为 not-applicable，而不是伪报 provider usage=0；
policy/control 的 embedding calls、失败和 latency 必须按臂单列。另报告：

- logical answer calls / provider attempts；
- retrieval calls、query-embedding calls 与各自 latency；
- policy serial critical-path latency（shallow + conditional deep）；
- fixed control latency；
- preflight overhead；
- run wall-clock（受并发影响，只作运维指标）。

每个 answer/judge **逻辑调用**最多 3 次 provider attempt（1 次初始 + 2 次重试）。仅 `timeout`、`network_error`、
HTTP 429 与 HTTP 5xx 可进入下一 attempt；context cancellation/length、其他 4xx、response size、decode/schema、empty answer、
invalid usage 与 judge parse 都不可重试。每个 attempt 有独立 `unit_id`，共享 `logical_call_id`，并各写 STARTED/terminal；
一旦某 attempt COMPLETED，该逻辑调用完成，后续 attempt 禁止。retryable FAILED 且 attempt<3 允许同 digest resume 到下一
attempt；耗尽或 non-retryable FAILED 使 stage INVALID。孤立 STARTED 因实际成本未知仍使 run-dir INVALID。

每次 attempt 的 call 与 latency 都进入所属臂总账；若失败 response 提供 valid usage，则按 usage 计 token，否则 answerer attempt
以 manifest 冻结的 `max_model_len=32768` 作 combined-token 保守上界并标 `usage_status=conservative_bound`。judge 的失败
attempt 仍单列 calls/latency/可得 usage，但不进入 runtime ratio。只有成功回答后概率字段或 final span 不满足契约才是
`signal_unavailable` 并执行质量优先 deep fallback；provider failure 绝不能伪装成 unavailable。FAILED 记录只含 closed reason
code/status class，不含 upstream raw body。

**Rationale**: 041 只看上下文量会漏掉重复作答和信号调用。总量比直接对应完整工作负载，且不会让短问题的小 denominator
支配平均值。有界重试避免一次瞬时故障烧掉已完成批次；逐 attempt journal 与未知 usage 的保守上界确保重试不会被当作免费。

## Decision 8: Artifact custody and privacy

**Decision**: 每阶段先写 canonical manifest，再写 append-only call/decision records，最后生成 seal。artifact 包含 dataset、
question set、store embedding fingerprint、retrieval/embedder identity、answer/judge request mode、prompt、model revision、sidecar
configuration、binary/source、rule family、feature order、judge regime、upstream stage 的 digests。
概率记录保留：final answer、sanitized per-token numeric trace（byte length、sampled logprob、top1、top2）、聚合特征、usage、
latency 和 response/content digest；不保留 decoded token text、thinking text、raw response、raw error、base URL 或 API key。
endpoint 只保存不可逆 digest，key 只从环境流入 request header。

score/diagnose loader 必须严格拒绝：重复/missing identity、不同 repetitions、question set 漂移、arm provenance 不一致、
schema version 不同、digest tamper、非有限数值、label 先于 decision seal 被读取。输出分为 `GO`、`NO-GO`、`INVALID`；
`INVALID` 不能被写成实验失败或成功。

**Rationale**: 研究需要复算信号和成本，但没有理由持久化 reasoning 或 endpoint secret。分阶段 seal 使 label-blind 的运行时
决策边界可被测试，而不仅是文档约定。

## Paper-direction boundary

现有 alphaXiv 调研的共同启示是 `relevance != utility`，并把 logits/log-prob 留作 040/041 后尚未验证的确定性信号。
042 只验证“浅答正常生成概率能否预测深答的反事实效用”。以下方向明确不在失败后的同 feature 追逐范围：

- TAA-k / score knee（040 已 NO-GO）；
- 文本 hesitation / self-report（041 已 NO-GO）；
- generator-aligned passage-by-passage IGP（需要 N+1 probes，是独立 feature）；
- answer verifier、semantic segmentation、redundancy selector、local listwise reranker；
- 更高容量 router 或训练型 gate。

若 042 NO-GO，结论应是“这组三个正常生成概率信号不足以预测 deep-vs-shallow utility”，而不是“所有概率方法”或“所有
top-k 缩减”被证明不可能。下一方向必须重新走 spec，并带独立成本与无泄漏协议。
