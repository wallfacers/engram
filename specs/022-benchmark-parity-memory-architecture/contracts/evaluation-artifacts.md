# Evaluation Protocol and Artifact Contract

**Schema**: `022.v1`

**Benchmarks**: LoCoMo category 1–4（1,540 题）与 LongMemEval-S cleaned full 500

## Purpose

本合同把“同候选、同预算、同 answerer/judge”变成可机器拒绝的运行条件，而不是报告中的
说明文字。所有机制 verdict 都来自逐题配对 artifact；历史点估计和跨栈 Mem0 数字只作
北极星，不作显著性 control。

## Baseline Ladder

### B0 — Continuity Baseline

- 从 lossless ingestion 新建 store，不复用截断超长 turn 的历史 DB。
- 使用当前产品默认 retrieval/packing/answer path。
- LoCoMo 跑 category 1–4 全 1,540，LongMemEval-S 跑 cleaned full 500。
- 随机 answerer 每题独立运行三次，以 majority correctness 为题级结果；原始三次输出
  全保留。
- B0 用于与历史产品结果衔接，不用于隔离 022 机制。

B0 可以记录当前 legacy IDK retry 的真实 answer-call 数；因此它不受“一次 answerer”
promotion validity gate 约束，也不能作为 022 treatment control。B1 及所有机制 arm 必须
关闭 legacy retry，并满足每个 repetition 一次 answerer。

B0 使用独立的 `--eval-freeze-b0-protocol` / `--eval-b0-protocol` 路径。manifest 的
`experiment.stage=b0`、`arm=legacy_product_continuity`、
`mechanism_flags.idk_retry=true`，预算 profile 为 `continuity`；它不声明 exact
answer-input cap 或 tokenizer admission，只保留实际 runtime usage。每个题/重复保存
`b0_continuity` receipt：

- `answer_calls`：legacy answer 路径的逻辑调用数，包含 IDK rewrite/wider-net 后的重答；
- `rewrite_calls`：IDK query rewrite 调用数；
- `judge_calls`：判分调用数；
- `legacy_retry`：`answer_calls>1 || rewrite_calls>0`；
- `protocol_hash` 与 `run_index`：绑定 manifest 和三次独立 repetition。

provider transport 的透明重试仍按当前产品路径执行并进入全局 cost ledger；上述 receipt
专门度量 legacy IDK 控制流，不把网络层重试冒充 memory retry。

B0 run directory 只允许 `protocol.json`、逐 repetition 的普通
`results-<arm>.jsonl`、`b0_continuity_summary.json`、`stats.json`、`cost.json` 和
既有 regime/parity 辅助文件。出现 `candidates.jsonl`、`compile_trace.jsonl`、
`bundles.jsonl`、`classification.jsonl`、formal freeze/call journal 或 fixed-gold
artifact 时，B0 validator 必须判 INVALID。B0 summary 固定
`promotion_eligible=false`，即使分数很高也不能作为 B1 或 treatment control。

### B1 — Causal Ruler

- 冻结 ranked anchor candidates、完整 rendered candidate bytes、answerer/judge、
  prompt、extractor、tokenizer 与两个 cap profile。
- navigation anchor 保留 projection identity/text digest；answer-facing candidate 必须先
  通过 direct lineage 批量展开为当前 active Evidence 原文或已验证 span。使用 legacy
  packer，但 admission 必须基于展开后的完整 answer input，并按与 treatment 相同的实际
  tokenizer/cap 执行；禁止先用较短 projection text 过 cap 再回填原文。
- 022 representation/compiler/Event/gap 的 control 都从对应 B1 artifact 派生；不能用 B0
  或历史分数替换。

预算 profile 在任何 treatment 结果产生前冻结：

- `low`：约 1.1k input token 量级；
- `high`：约 3.6k input token 量级。

“约”只用于计划；`protocol.json` 必须写精确正整数，且两个 benchmark 使用同名同值
profile。任一 cap 改动产生新 protocol hash 和新 baseline。

## Artifact Layout

每个 immutable run directory 必须包含：

```text
<run-dir>/
├── protocol.json
├── candidates.jsonl
├── compile_trace.jsonl
├── bundles.jsonl
├── classification.jsonl
└── summary.json
```

不得在这些文件中保存 API key、endpoint credential、完整 provider request header 或
dataset 未授权副本。原始 benchmark 数据继续位于 gitignored `testdata/locomo/` 或
session scratchpad；artifact 用 dataset digest 和题目 ID 引用。

所有 JSON 使用 UTF-8、稳定字段名、RFC3339 UTC 时间；用于 hash 的 canonical JSON
按字段名排序、无非语义空白。JSONL 每行都是独立完整对象。

## `protocol.json`

```json
{
  "schema": "022.v1",
  "protocol_id": "human-readable-id",
  "protocol_hash": "sha256:...",
  "created_at": "2026-07-30T00:00:00Z",
  "git": {
    "commit": "full-sha",
    "dirty": false
  },
  "benchmark": {
    "name": "locomo|longmemeval_s",
    "dataset_digest": "sha256:...",
    "split": "category_1_4|cleaned_full_500",
    "question_count": 1540,
    "question_ids_digest": "sha256:..."
  },
  "store": {
    "schema_version": 7,
    "ingestion_recipe": "ledger_lossless_chunks_v2",
    "ingestion_config_digest": "sha256:...",
    "projection_builder_versions": {
      "atomic_fact": "entry_store_explicit_v1"
    }
  },
  "models": {
    "extractor": {"id": "...", "revision": "...", "provider": "openai|anthropic", "prompt_digest": "sha256:..."},
    "answerer": {"id": "...", "revision": "...", "provider": "openai|anthropic", "prompt_digest": "sha256:..."},
    "judge": {"id": "...", "revision": "...", "provider": "openai|anthropic", "prompt_digest": "sha256:..."},
    "planner": {"enabled": false, "id": "", "revision": "", "provider": "", "prompt_digest": ""}
  },
  "retrieval": {
    "recipe": "hybrid",
    "embedding_fingerprint": "sha256:...",
    "reranker": "disabled",
    "candidate_limit": 30,
    "candidate_rules_digest": "sha256:..."
  },
  "budget": {
    "profile": "low",
    "answer_input_token_cap": 1100,
    "max_output_tokens": 8000,
    "candidate_limit": 30,
    "retrieval_call_limit": 1,
    "answer_call_limit": 1,
    "counter_fingerprint": "sha256:..."
  },
  "aggregation": {
    "answer_repetitions": 3,
    "rule": "majority_correctness",
    "judge_repetitions": 1,
    "seed_policy": "independent-recorded"
  },
  "judge_audit": {
    "all_discordant": true,
    "concordant_sampling_digest": "sha256:...",
    "reviewers": 2,
    "blinded_to_arm": true,
    "adjudication_rule": "independent_then_adjudicate"
  },
  "coverage_strata": {
    "boundaries": [0, 0.5, 0.9, 1],
    "selection_digest": "sha256:..."
  },
  "experiment": {
    "stage": "b1",
    "arm": "legacy_count_packer",
    "control_protocol_hash": "",
    "primary_cohort": "all",
    "mechanism_flags": {
      "idk_retry": false,
      "iris": false,
      "rerank": false
    }
  }
}
```

精确要求：

- `question_count` 和 `question_ids_digest` 同时匹配才可 resume/compare。
- 正式 run 要求 `git.dirty=false`；探索 run 可 dirty，但不能获得 promotion verdict。
- 正式默认 stack 的 `reranker` 必须 disabled，不能写 hosted diagnostic 结果冒充。
- answerer/extractor/judge 的 provider、model ID、revision 与 prompt digest 都属于同栈
  fingerprint；`budget.max_output_tokens` 同样冻结，运行时漂移必须在 provider 调用前拒绝。
- 模型字段为空也必须以显式 `enabled=false`/空 fingerprint 表达，不能省略产生歧义。
- `protocol_hash` 计算时排除自身字段，包含其他全部字段。

## `candidates.jsonl`

每题每 arm 一行：

```json
{
  "schema": "022.v1",
  "protocol_hash": "sha256:...",
  "question_id": "q-id",
  "query_digest": "sha256:...",
  "mode": "navigation|anchor_rendering|compiler_replay|gap_union",
  "anchor_digest": "sha256:...",
  "candidate_set_digest": "sha256:...",
  "retrieval_calls": 1,
  "anchors": [
    {
      "candidate_id": "stable-id",
      "rank": 1,
      "score": 0.0,
      "text_digest": "sha256:...",
      "source_ids": ["evidence-id"]
    }
  ],
  "rendered_candidates": [
    {
      "candidate_id": "stable-id",
      "kind": "chunk|raw_turn|semantic_episode|atomic_fact",
      "rank": 1,
      "score": 0.0,
      "text": "frozen bytes",
      "text_digest": "sha256:...",
      "source_ids": ["evidence-id"],
      "expanded_from": ["anchor-id"],
      "expansion_count": 0,
      "pre_cap_input_tokens": 0,
      "truncated": false
    }
  ],
  "gold": {
    "dataset_source_ids": ["dataset-turn-id"],
    "resolved_evidence_ids": ["evidence-id"],
    "unresolved_ids": [],
    "anchor_source_coverage": 1.0,
    "rendered_source_coverage": 1.0
  },
  "coverage_stratum": "protocol-defined-id"
}
```

### “Same candidates” Definition

表示实验分两张表报告：

1. **Navigation bake-off**：三种表示分别检索；相同的是 query、retrieval algorithm、
   embedding、pool/candidate limit，不声称候选相同。
2. **Answer-facing rendering bake-off**：三臂逐题共享完全相同的 `anchor_digest`；
   renderer 可展开不同 source closure，差异由 `source_ids/expanded_from/expansion_count`
   显式记录。

Compiler 阶段冻结获选 renderer 的整个 `rendered_candidates` array，所有 compiler arm
逐字节 replay；`candidate_set_digest` 一致率必须 100%，且每题
`retrieval_calls_after_freeze=0`。

Source ID coverage 只命名为 **gold-source survival**。Dataset gold turn ID 映射到 Ledger
Evidence，不得把它表述成答案 span 已可见。

## `compile_trace.jsonl`

每题每 compile attempt 一行：

```json
{
  "schema": "022.v1",
  "protocol_hash": "sha256:...",
  "question_id": "q-id",
  "attempt": 1,
  "candidate_set_digest": "sha256:...",
  "need": {
    "entities": [],
    "time_constraints": [],
    "operands": [],
    "list_cardinality": {"known": false, "count": 0},
    "update_state": "",
    "gap": null
  },
  "proposed_actions": [],
  "applied_actions": [],
  "relations": [
    {
      "kind": "before|after|conflicts|supports_operand",
      "left_source_id": "id",
      "right_source_id": "id",
      "operand": ""
    }
  ],
  "token_steps": [
    {
      "operation": "add|drop|replace",
      "item_id": "id",
      "full_answer_input_tokens": 0,
      "cap": 0
    }
  ],
  "fallback": {
    "used": false,
    "reason_code": ""
  },
  "remaining_gap": null,
  "source_validation": {
    "allowed_ids_digest": "sha256:...",
    "resolved_count": 0,
    "invalid_count": 0
  },
  "trace_digest": "sha256:...",
  "valid": true
}
```

Fixed-candidate stage 的 `attempt` 必须恒为 1 且 gap/retrieval 关闭。Gap stage 最多两条
trace（首轮与补检后），但 answerer 只在第二条或无补检的第一条之后调用。
Trace 的 `source_validation` 只登记 producer 从 frozen candidates 推导的结构性 allowlist；
独立 active-Ledger 验证结果以 Bundle 的 `active_validation` 为准。

## `bundles.jsonl`

```json
{
  "schema": "022.v1",
  "protocol_hash": "sha256:...",
  "question_id": "q-id",
  "candidate_set_digest": "sha256:...",
  "trace_digest": "sha256:...",
  "items": [
    {
      "item_id": "stable-id",
      "kind": "KEEP|EXTRACT|MERGE",
      "text": "rendered evidence",
      "candidate_ids": ["id"],
      "sources": [
        {
          "evidence_id": "id",
          "start_char": 0,
          "end_char": 10,
          "span_digest": "sha256:..."
        }
      ]
    }
  ],
  "source_ids": ["id"],
  "rendered_context": "exact answer-facing user message",
  "rendered_context_digest": "sha256:...",
  "evidence_tokens": 0,
  "answer_input_tokens": 947,
  "token_cap": 0,
  "counter_fingerprint": "sha256:...",
  "within_cap": true,
  "source_valid": true,
  "answer_prompt_digest": "sha256:...",
  "active_validation": {
    "checked": true,
    "allowed_ids_digest": "sha256:...",
    "evidence_state_digest": "sha256:...",
    "resolved_count": 1,
    "invalid_count": 0,
    "source_valid": true,
    "span_valid": true,
    "citation_valid": true,
    "receipt_digest": "sha256:..."
  }
}
```

原文 artifact 的 text 是否保留由 dataset/license/run-dir policy 决定；若不允许保存，
保存 encrypted local artifact 或 digest+offset，并保证验证器仍能在授权 dataset 上复原。
正式 summary 必须报告验证结果，不能因不提交原文而跳过校验。

`answer_input_tokens` 是 exact tokenizer 对最终 system+user/chat template 的完整计数；
`evidence_tokens` 仅是同一 counter 下
`max(0, final_full_input_tokens - empty_context_full_input_tokens)` 的诊断差值，不是独立
cap。唯一 admission hard gate 始终是 `answer_input_tokens <= token_cap`。

B1 source/span 规范：

- 每个 `KEEP`/`EXTRACT` item 在 B1 中恰好引用一个 frozen rendered candidate 和一个
  Evidence span；多 source 的 navigation anchor 作为一个不可拆 admission group，
  不能因 cap 只装入其部分来源。
- `full_source=1` 在 artifact 中规范化为 `[0,rune_count(content))`，`KEEP` text 必须
  等于完整 Evidence；partial ref 保留原 code-point offset 并使用 `EXTRACT`。
- `span_digest` 对恢复出的 UTF-8 bytes 计算并统一写 `sha256:` 前缀；Ledger 内部裸 hex
  digest 只在 artifact 边界规范化，不改变 engine schema。
- `source_ids` 必须精确等于全部 item citations 的去重并集；source order 用
  projection ref 的 `source_order` 渲染，只有并集字段允许排序。
- Producer 的 `source_valid=true` 和 Trace 中的结构性 allowlist/count 不是有效性证据。
  answer 前的独立 validator MUST
  重新批量读取 active Ledger，验证 lifecycle/content digest、offset、span text/digest、
  candidate allowlist、完整-anchor ranked prefix、item/source union 和重建后的完整 answer
  input；结果写入 `active_validation`，其 `evidence_state_digest` 绑定实际读取到的
  ID/type/state/revision/content digest，`receipt_digest` 绑定整个独立结果。任一失败时
  answer/judge 调用均为 0。

Summary 的三项来源率必须独立计算：

- `source_validation_rate`：item citation source union 与 Trace allowlist/receipt 合法；
- `span_recovery_rate`：每个 code-point span 和 digest 能从 source 恢复；
- `citation_coverage_rate`：每个 item 的 candidate/source 引用均在 frozen lineage 内。

不得把一个 `SourceValid` 布尔同时复制为上述三项结果。

## Fixed-gold Oracle Diagnostic

fixed-gold oracle 从 `stage=b1`、`arm=legacy_count_packer`、空
`control_protocol_hash`、三次 repetition 且 `idk_retry/iris/rerank=false` 的已冻结
control protocol 派生。control 的 `retrieval.recipe` 必须精确为无 suffix 的 `fts` 或
`hybrid`；`hybrid+rerank`、`hybrid+pcic`、`hybrid+assoc` 等 recipe 和等价的全局
mechanism flag 一律拒绝，不能登记成 legacy control。oracle 使用独立 run directory：

```text
<oracle-run-dir>/
├── protocol.json
├── fixed_gold_oracle.jsonl
├── fixed_gold_oracle_calls.jsonl
└── fixed_gold_oracle_summary.json
```

它不是 B1/treatment artifact，不生成 `metrics`、`paired_vs_control`、`promotion` 或正式
verdict。每题只能按 dataset gold ID 读取全部 active raw-message Evidence；不暴露
Retriever、projection、extractor、embedding、filter、rewrite 或 reranker 接口。
所有 gold source 必须一次进入同一完整 answer input，禁止 prefix pack、截断或补检。
answerer/judge 的 provider、model/revision、prompt suite、input cap、output cap、token
counter 和三次 majority policy 必须与 control 一致。control 中的 embedding fingerprint
继续作为 provenance 保存，但 oracle runtime 不读取、不实例化也不验证 embedding
sidecar。

每题 artifact 使用 `dataset_source_ids` 保存 benchmark gold turn IDs；该字段不是 B1
Bundle 中的 Ledger Evidence `source_ids`。独立 read-back 必须从 dataset 重新构建
`dataset_source_id → active raw-message Evidence` 映射，不能把 artifact 自报 ID 当真。

```json
{
  "schema": "022.v1",
  "stage": "fixed_gold_oracle",
  "arm": "all_gold_evidence",
  "diagnostic_only": true,
  "control_protocol_hash": "sha256:...",
  "oracle_protocol_hash": "sha256:...",
  "question_id": "q-id",
  "retrieval_calls": 0,
  "dataset_source_ids": ["D1:1", "D2:3"],
  "answer_input_digest": "sha256:...",
  "answer_prompt_digest": "sha256:...",
  "answer_input_tokens": 0,
  "counter_fingerprint": "sha256:...",
  "answer_calls": 3,
  "judge_calls": 3,
  "valid": true,
  "repetition_results": [
    {"run_index": 1, "answer": "...", "answer_digest": "sha256:...", "judge_input_digest": "sha256:...", "judge_verdict": "{\"correct\":true}", "judge_correct": true, "judge_verdict_digest": "sha256:...", "input_tokens": 947, "output_tokens": 12},
    {"run_index": 2, "answer": "...", "answer_digest": "sha256:...", "judge_input_digest": "sha256:...", "judge_verdict": "{\"correct\":true}", "judge_correct": true, "judge_verdict_digest": "sha256:...", "input_tokens": 947, "output_tokens": 11},
    {"run_index": 3, "answer": "...", "answer_digest": "sha256:...", "judge_input_digest": "sha256:...", "judge_verdict": "{\"correct\":true}", "judge_correct": true, "judge_verdict_digest": "sha256:...", "input_tokens": 947, "output_tokens": 12}
  ]
}
```

`repetition_results` 每项保存 `run_index`、原 answer 与 digest、judge input digest、原
judge verdict 与 digest、解析后的 correct、input/output tokens。任一 raw verdict 与
解析结果不一致即 INVALID。

三个 oracle 文件必须在任何 provider 调用前以 create-exclusive 方式建立，任一已存在即
拒绝覆盖。`fixed_gold_oracle_calls.jsonl` 对每次 answer/judge provider attempt 在调用前
fsync `intent`、返回后 fsync `terminal`；orphan、重复、失败或 digest/count 不一致使整轮
INVALID。失败目录是审计证据，重跑必须使用新目录。formal wrapper 不得在一次已记录
intent 内透明 retry。

LongMemEval-S 仅 `adversarial=true && question_type=abstention` 可使用空 Evidence；其他空、
未知、tombstoned、purged、非 raw、digest/mapping 错误或全量 gold 超 cap 均使该题和整轮
INVALID。首个无效题触发取消，不再调度后续题；已经在途的调用仍按 journal 如实落盘。
INVALID summary 保留原因及 `answer_calls`/`judge_calls` 总数，但省略
`oracle_diagnostic`，不得暴露部分正确率。

独立 `--eval-validate` 必须重新读取 dataset、artifact 与 call journal，重建 ordered gold
source、完整 answer input、judge input 和 raw verdict/majority，并要求落盘 summary 与
read-back 派生 summary 完全一致；仅检查 artifact 内部 digest 形状不够。

仅当 question count/order/digest、每题 artifact、三次 answer/judge 调用和 token
fingerprint 全部有效时，summary 才包含：

```json
{
  "answer_calls": 4620,
  "judge_calls": 4620,
  "valid": true,
  "oracle_diagnostic": {
    "correct": 0,
    "denominator": 1540,
    "target_correct": 1425,
    "target_met": false
  }
}
```

LongMemEval-S 使用固定 `denominator=500`、`target_correct=473`。该对象始终
`diagnostic_only=true`，不能进入 promotion。

## `classification.jsonl`

逐题答案、judge 和互斥错误分类：

```json
{
  "schema": "022.v1",
  "protocol_hash": "sha256:...",
  "question_id": "q-id",
  "category": "temporal|multi_hop|...",
  "cohorts": ["all", "pre_registered-primary"],
  "gold_answer_digest": "sha256:...",
  "answer_runs": [
    {
      "run_index": 1,
      "answer": "model output",
      "answer_digest": "sha256:...",
      "judge_correct": true,
      "judge_reason_digest": "sha256:...",
      "answer_calls": 1,
      "input_tokens": 0,
      "output_tokens": 0,
      "latency_ms": 0,
      "cost": 0.0
    }
  ],
  "majority_correct": true,
  "judge_audit": {
    "selected": false,
    "selection_reason": "",
    "reviewer_labels": [],
    "adjudicated_correct": null
  },
  "gold_resolution": {
    "resolved": true,
    "candidate_coverage": 1.0,
    "bundle_coverage": 1.0
  },
  "miss_class": "gold_unresolved|candidate_miss|compiler_miss|answerer_miss|success",
  "retrieval_calls": 1,
  "answer_calls": 3,
  "valid": true,
  "invalid_reasons": []
}
```

分类顺序固定：

1. dataset gold 不能解析到 Ledger → `gold_unresolved`；
2. frozen candidate lineage 未覆盖全部 required gold sources → `candidate_miss`；
3. candidate coverage=1 但 Bundle coverage<1 → `compiler_miss`；
4. 两者 coverage=1 但 majority wrong → `answerer_miss`；
5. majority correct → `success`。

所有题保留在 accuracy 分母。`gold_unresolved` 只从 source-survival miss-rate 分母排除。
无 parseable gold source 的题仍有答案得分，但不能伪造 source attribution。

`answer_calls` 的 protocol 上限按一次“正式答题”定义；三次独立 repetition 是三次独立
run instance。每个 run instance 内 answerer 只能调用一次，不能先答、IDK 后重答。

## `summary.json`

```json
{
  "schema": "022.v1",
  "protocol_hash": "sha256:...",
  "artifact_hashes": {
    "candidates.jsonl": "sha256:...",
    "compile_trace.jsonl": "sha256:...",
    "bundles.jsonl": "sha256:...",
    "classification.jsonl": "sha256:..."
  },
  "validity": {
    "questions_complete": 0,
    "expected_questions": 0,
    "candidate_digest_match_rate": 1.0,
    "source_validation_rate": 1.0,
    "span_recovery_rate": 1.0,
    "citation_coverage_rate": 1.0,
    "within_cap_rate": 1.0,
    "per_instance_answer_call_compliance": 1.0,
    "unattributed_add_count": 0,
    "valid": true
  },
  "metrics": {
    "overall": {},
    "by_category": {},
    "by_cohort": {},
    "gold_source_survival": {},
    "miss_classes": {},
    "judge_noise": {
      "audited": 0,
      "false_negative": 0,
      "false_positive": 0,
      "raw_accuracy": 0.0,
      "corrected_accuracy": 0.0,
      "inter_reviewer_agreement": 0.0,
      "verdict_changed": false
    },
    "tokens": {"mean": 0.0, "p95": 0},
    "retrieval_calls": {"mean": 0.0, "p95": 0},
    "latency_ms": {"mean": 0.0, "p95": 0},
    "cost": 0.0
  },
  "paired_vs_control": {
    "control_protocol_hash": "sha256:...",
    "paired_questions": 0,
    "control_correct_treatment_wrong": 0,
    "control_wrong_treatment_correct": 0,
    "accuracy_delta_pp": 0.0,
    "exact_mcnemar_two_sided_p": 1.0,
    "confidence_interval": [0.0, 0.0]
  },
  "paired_by_category": {},
  "promotion": {
    "primary_cohort": "id",
    "other_benchmark_protocol_hash": "sha256:...",
    "verdict": "GO|HOLD|STOP|INVALID",
    "reasons": []
  }
}
```

## Exact Paired Statistics

Control/treatment 必须题 ID 集合和 majority rule 完全相同。记：

- `b = control correct, treatment wrong`
- `c = control wrong, treatment correct`

使用二项分布 `X ~ Binomial(b+c, 0.5)` 的 two-sided exact McNemar：

```text
p = min(1, 2 * P[X <= min(b,c)])
```

无论 discordant pair 数量多大都不得切换卡方近似。总体/分类别置信区间方法在 protocol
中冻结；不得看结果后在 Wilson/bootstrap/normal 之间切换。

预注册 category 分别计算 exact paired test，并对负向检验使用 Holm correction；不得因
某 category 样本小而事后合桶。Category 校正结果写入 `paired_by_category`。

若 answerer 非确定性，题级 correctness 先由三次独立 repetition majority 固化，再做
paired test。不得把三次 repetition 当作三个独立题扩大样本量。

## Uniform Promotion Rule

每阶段必须在 run 前指定唯一 primary arm 与 primary cohort。GO 同时要求：

1. artifact、lineage、span/citation、cap、candidate identity、answer-call validity
   全部为 100%，unattributed ADD 为 0；
2. primary cohort majority accuracy 相对 control `Δ >= +2.0pp`；
3. primary cohort two-sided exact McNemar `p < 0.05`；
4. 另一 benchmark overall `Δ >= -0.5pp`，且没有显著负向配对结果；
5. 任一预注册 category 均无 Holm-corrected 显著负向；
6. candidate coverage 和 gold-source survival 不退；
7. judge audit 已完成、reviewer agreement 达到 protocol 门且 corrected labels 不改变
   GO/HOLD/STOP；若改变则以 corrected verdict 为准并扩大审计；
8. 机制仍满足 offline/default-cost/公开 recipe 约束。

Verdict：

- **GO**：全部条件成立，可进入下一阶段或默认候选。
- **HOLD**：`0 < Δ < 2pp`、未显著或证据不足，但没有 hard harm；保持实验性。
- **STOP**：`Δ <= 0`、另一 benchmark 显著受损、coverage 回退或成本/架构约束失败。
- **INVALID**：artifact/fingerprint/cap/source/candidate/answer-call 任一 hard validity
  不到 100%，或必需 judge audit 未完成；不得解释 accuracy。

## Judge Noise Audit

Protocol 在 treatment 前冻结审计样本：

1. 全部 control/treatment raw-judge discordant questions；
2. 从 concordant positive 和 concordant negative 中按 benchmark/category 分层抽样；
3. 两名不知道 arm/模型/原始 judge label 的 reviewer 独立判定，再按冻结规则裁决。

`classification.jsonl` 保存选择原因和 reviewer/adjudicated labels。Summary 同时报告 raw
与 corrected score、FN/FP、方向性和 reviewer agreement。若 corrected labels 改变
promotion verdict、agreement 未达 protocol 门或发现明显单向偏差，当前 run 为 HOLD，
扩大审计后重新裁决；不得换另一个 LLM judge 后直接宣布通过。

## Coverage and Oracle Diagnostics

Candidate coverage 作为 `[0,1]` 连续量和 protocol-defined strata 报告。外部论文的
0.990/0.889 只用于说明 Compiler 受 retrieval ceiling 限制，不得硬编码通用 0.95
启用阈值。

固定 gold Evidence 的 oracle run 使用相同 answerer/judge/prompt/cap，帮助区分：

- gold 不在冻结 candidate → retrieval/candidate miss；
- gold 在 candidate 但不在 Bundle → assembly/compiler miss；
- gold 在 Bundle 或 oracle evidence 仍答错 → answerer/reasoning miss。

Oracle run 必须使用独立 stage/arm 和显式 `diagnostic_only=true`，不参与正式 accuracy、
promotion 或产品 recipe。

SC-002（LoCoMo ≥1,425/1,540）和 SC-003（LongMemEval ≥473/500）是最终共同目标，不替代
每个机制的 promotion rule。达到数值目标但相对 B1 没有同栈证据时，只能报告达到北极星，
不能声称受控优于 Mem0。

## Stage-specific Arms

### Representation

- Navigation：`chunk_900`、`raw_turn_window`、`semantic_episode` 各自检索。
- Rendering：同 ranked anchors，三个 renderer；记录 source expansion 和 cap truncation。
- Primary arm/cohort 在 protocol 冻结；未过门不切默认 representation。

### Fixed-candidate Compiler

- `legacy_count_packer`；
- `exact_token_relevance_packer`；
- `deterministic_extractive_compiler`；
- `local_planner_compiler`（可选、仅本地）。

四臂完整 rendered candidate bytes 相同、gap disabled、post-freeze retrieval=0、IDK retry
disabled。Local Planner 缺失不能让整个产品失败，但该实验 arm 若实际 fallback，必须单列
`fallback_rate`，不能把 fallback 样本都算成 model treatment。

### Event/State

在预注册 temporal/update residual cohort：

- E0 current entry time fields；
- E1 event object only；
- E2 date operator only；
- E3 source-turn recovery only。

不得一次启用 E1+E2+E3 后把 bundle 差值归因给 Event。

### Gap Retrieval

- Eligibility 在 treatment 前从 control Trace 冻结，只允许三种 structured gap。
- Control：一轮、最多 N candidates。
- Treatment：首轮最多 `N-r`，一次补检最多 r，union `<=N`。
- 两臂相同累计 token cap、一次 answer call；低置信度不 eligible。

### Scene/Profile/Graph

- Scene primary cohort：跨 session candidate miss；
- Profile：preference/current-state residual；
- graph：缺桥/local relation residual。

每项使用单独 flag/artifact/verdict。003 graph 合同不变，不能被包装进默认 bundle 后合并
归因。

## Resume and Comparison Refusal

Resume 前重算并比较：

- protocol hash；
- question ID/dataset digest；
- git commit 和 dirty policy；
- store schema/ingestion/projection builder；
- model/prompt/extractor/embedding；
- candidate rules/limit/anchor/candidate set；
- tokenizer/counter/cap；
- repetitions/aggregation；
- mechanism flags。

任一变化立即拒绝 resume，并提示创建新 run directory。Artifact 某行 hash 不匹配、重复/
缺 question、跨 protocol line 或候选 text digest 漂移同样 INVALID，不能“尽量继续”。

## Submission Isolation

以下必须分开 commit，保证 attribution：

1. artifact schema、validator、paired statistics；
2. frozen protocol/config；
3. B0/B1 result manifest；
4. 单一 representation/compiler/Event/gap/projection mechanism；
5. 该 mechanism 的 result/verdict。

Eval-config 改动不得与算法改动同 commit。大型逐题 artifact 若不适合 Git，提交 immutable
manifest、hash、生成命令和受控存放位置；不得只提交手写 summary。
