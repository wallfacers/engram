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

### B1 — Causal Ruler

- 冻结 ranked anchor candidates、完整 rendered candidate bytes、answerer/judge、
  prompt、extractor、tokenizer 与两个 cap profile。
- 使用 legacy packer，但按与 treatment 相同的实际 tokenizer 和完整 answer-input cap
  执行。
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
    "ingestion_recipe": "lossless",
    "ingestion_config_digest": "sha256:...",
    "projection_builder_versions": {}
  },
  "models": {
    "extractor": {"id": "...", "revision": "...", "prompt_digest": "sha256:..."},
    "answerer": {"id": "...", "revision": "...", "prompt_digest": "sha256:..."},
    "judge": {"id": "...", "revision": "...", "prompt_digest": "sha256:..."},
    "planner": {"enabled": false, "id": "", "revision": "", "prompt_digest": ""}
  },
  "retrieval": {
    "recipe": "both",
    "embedding_fingerprint": "sha256:...",
    "reranker": "disabled",
    "candidate_limit": 0,
    "candidate_rules_digest": "sha256:..."
  },
  "budget": {
    "profile": "low|high",
    "answer_input_token_cap": 0,
    "candidate_limit": 0,
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
    "boundaries": [],
    "selection_digest": "sha256:..."
  },
  "experiment": {
    "stage": "b0|b1|representation_navigation|representation_rendering|compiler|event|gap|projection",
    "arm": "pre_registered-arm-id",
    "control_protocol_hash": "sha256:...",
    "primary_cohort": "pre_registered-cohort-id",
    "mechanism_flags": {}
  }
}
```

精确要求：

- `question_count` 和 `question_ids_digest` 同时匹配才可 resume/compare。
- 正式 run 要求 `git.dirty=false`；探索 run 可 dirty，但不能获得 promotion verdict。
- 正式默认 stack 的 `reranker` 必须 disabled，不能写 hosted diagnostic 结果冒充。
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
  "rendered_context_digest": "sha256:...",
  "evidence_tokens": 0,
  "answer_input_tokens": 0,
  "token_cap": 0,
  "counter_fingerprint": "sha256:...",
  "within_cap": true,
  "source_valid": true,
  "answer_prompt_digest": "sha256:..."
}
```

原文 artifact 的 text 是否保留由 dataset/license/run-dir policy 决定；若不允许保存，
保存 encrypted local artifact 或 digest+offset，并保证验证器仍能在授权 dataset 上复原。
正式 summary 必须报告验证结果，不能因不提交原文而跳过校验。

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
