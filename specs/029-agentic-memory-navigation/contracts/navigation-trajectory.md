# Contract: 导航轨迹 JSON（Navigation Trajectory）

**Date**: 2026-08-06 · **Spec**: [spec.md](../spec.md) · 数据模型: [data-model.md](../data-model.md)

导航轨迹是可审计产物（FR-007），以 JSONL 落盘（每 query 一行）。`nav_analyze.py` 消费它做配对分析 + 步数/token 记账。

## Schema

```json
{
  "question_id": "conv-2-q-42",
  "query": "What area was hit by a flood?",
  "steps": [
    {
      "index": 1,
      "tool": "search",
      "tool_args": {"query": "area hit by a flood", "k": 8},
      "returned_evidence": [
        {"source_id": "chunk-12", "text": "...", "score": 0.82, "retrieved_by": "hybrid"}
      ],
      "rationale": "raw query first",
      "latency_ms": 340
    }
  ],
  "final_evidence": {
    "evidence": [{"source_id": "chunk-12", "text": "..."}],
    "total_tokens": 1800,
    "assembly": "dedup"
  },
  "budget_usage": {"steps": 3, "nav_tokens": 1400, "answer_context_tokens": 1800},
  "fallback_triggered": false,
  "answer": "..."
}
```

## Validation（重放于 `nav_analyze.py`）

- `len(steps) ≥ 1` 且 ≤ 步数上限；最后一步 tool=`stop` 或 `fallback_triggered=true`
- `budget_usage.answer_context_tokens ≤ answer_context_cap`
- `fallback_triggered=false` → `final_evidence.evidence` 非空且 `total_tokens` 一致
- 工具名 ∈ {`search`,`expand_query`,`follow_entity`,`stop`}；`tool_args` 字段与 contracts/navigation-tools.md 一致
- 全部字段存在（缺字段 → 该行标 invalid，审计计数，不静默跳过）

## 配对协议（Pair-Gate）要点

- **84 题 × 3 reps majority**：每题 3 次 majority，correct≥2 → 该题对。
- **GO 门（008 铁律）**：多步导航 majority ≥ 单次基线 majority（同 store/子集/answerer/judge/token cap）。
- **McNemar**：配对独对（nav✓/base✗ vs nav✗/base✓）双侧精确二项 p。
- **类别不回归（L0-3）**：temporal / multi-hop 任一类别相对基线显著崩（p<0.05 且 Δ 负）→ 否决，即使整体涨。
- **归因**：仅「多步导航」为单一机制差异；评测配置（repeats/步数/token cap）变更与算法分开 commit。
