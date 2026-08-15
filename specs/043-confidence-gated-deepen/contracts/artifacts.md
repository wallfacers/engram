# Contract: 工件 Schema(043)

**Date**: 2026-08-15 | 实体字段定义见 [data-model.md](../data-model.md),此处冻结工件形状与 seal 语义。

## pilot-report.json

```json
{
  "stage": "signal-pilot",
  "conversations": ["<conv-id>", "<conv-id>"],
  "signals": [
    {"kind": "logprob", "feature": "final_p10_logprob", "auc": 0.0, "auc_ci95": [0.0, 0.0], "parse_coverage": 0.0},
    {"kind": "textual", "feature": "hesitation_lexicon", "auc": 0.0, "auc_ci95": [0.0, 0.0], "parse_coverage": 0.0}
  ],
  "channel_parity": {"n": 0, "flips": 0, "flip_rate": 0.0},
  "chosen": {"kind": "", "feature": "", "threshold": 0.0},
  "gate": {"rule": "auc>=0.65 AND flip_rate<=noise_band", "verdict": "GO|NO-GO", "reason": ""}
}
```

- `chosen.threshold` = 选定信号 ROC 最优点;pilot seal 后机制臂只读。
- `channel_parity`:同题双通道(streaming vs logprob 非流式)答案对照,flip_rate 超噪声带 ⇒ NO-GO(plan 决策 2)。

## manifest.json / seal.json

照抄 042 语义(`counterfactual_utility_artifact.go`):manifest 全字段(含 QuestionCount、arm、threshold、featureName、contract_digest、dataset_digest)填满后才计算 digest 并写 seal;下游 loader 先验 seal。改 manifest 必然导致 `ManifestDigest` 不匹配 = 加载失败。

## public/deepen-decisions.jsonl

每行一个 DeepenDecision(data-model.md 字段表,字段名 snake_case)。append-only,manifest 收 `decision_count` 计量。

## public/answer-attempts.jsonl

每行:`{decision_id, arm, round: 0|1, final_answer, final_answer_digest, usage:{...}, signal:{...}}`。round=1 仅在 triggered=true 时存在。

## 结果行(result-matrix 登记)

```
LoCoMo 1540 | <answerer> | <judge> | hybrid+unified+deepen | k30(+deepen) | 3-rep majority | clean
  得分 / p(与 hybrid+unified 对照臂配对 McNemar) / avg_retrieved_items / context parity ✓
```
