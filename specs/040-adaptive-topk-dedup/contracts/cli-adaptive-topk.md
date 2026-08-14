# Contract: 自适应检索深度 CLI 与诊断输出

本 feature 的对外接口只有两层：eval harness 的 CLI flag（opt-in）与诊断输出 JSON。**引擎公开 API 不变**（`Retriever.Search(query, k)` 语义照旧）。

## CLI flag 契约

| flag | 类型 | 默认 | 语义 |
|---|---|---|---|
| `--adaptive-topk` | bool | `false` | 开启 per-query 自适应检索深度。关闭时检索与作答与当前固定深度**逐字节一致**（FR-001）。 |
| `--adaptive-min-k` | int | 待冻结 | 自适应截断的保守下限；`k* ≥ minK`（FR-005）。默认值在实现阶段按诊断的 gold-rank 分布冻结。 |

**约束**：
- `--adaptive-topk` 与固定 `--top-k N` 的关系：`N` 是自适应搜索的**上界**（`fixedTopK`），knee 检测在宽池（`max(300, N*6)`）的分数序列上运行，`k* ∈ [minK, N]`。
- `--adaptive-topk` 不得改变 `--chunk-quota` 的语义（FR-003）：quota 恒定，自适应只压缩 facts 槽位。
- 引擎无新 flag、无新 API。

## 诊断输出 schema（headroom 诊断）

新增检索-only 诊断模式（无 answerer/judge 调用），逐题写 JSONL，汇总写 JSON。

**逐题记录**（每问题一行）：

```json
{
  "conv": 3, "q": 7, "category": 1,
  "gold_rank_pool": 42, "gold_in_pool": true,
  "adaptive_topk": 18, "knee_index": 18, "fallback": false,
  "dropped_gold": false
}
```

**汇总指标**（`adaptive-headroom.json`）：

```json
{
  "n": 1540,
  "fixed_topk": 150,
  "min_k": 30,
  "dropped_gold_count": 61,
  "dropped_gold_ratio": 0.0396,
  "knee_rate": 0.71,
  "adaptive_topk": {"min": 10, "median": 34, "max": 150},
  "mean_budget_reduction": 0.62
}
```

**判定线（对应 US1 的生死前提）**：若 `dropped_gold_ratio` 超过诊断阶段冻结的阈值，或 `knee_rate` 过低（分数序列普遍无拐点），则自适应方向判为不可行（STOP），不进入实现。
