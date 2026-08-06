# Contract: 导航工具集（Navigation Tools）

**Date**: 2026-08-06 · **Spec**: [spec.md](../spec.md) · 数据模型: [data-model.md](../data-model.md)

导航 agent 暴露的工具白名单。工具调用通过 JSON 结构化输出传递（复用 `cmd/locomo-bench/local_planner.go` 模式，不依赖 vllm 原生 function-calling）。

## 工具 schema

### `search(query: string, k: int = 8)`

混合检索（语义 + BM25 + 实体 RRF，现有引擎能力）返回 top-k 证据。

- `query`: 检索查询（通常为原始查询或带上下文的改写）
- `k`: 返回证据数（默认 8，≤ 12）

**返回**: `Evidence[]`（`source_id` / `text` / `score` / `retrieved_by`）

### `expand_query(text: string, k: int = 8)`

基于中间证据的查询改写后再次混合检索。

- `text`: 模型根据已见证据构造的补充/改写查询（可含时间、实体、关系约束）

**返回**: `Evidence[]`（同 `search`）

### `follow_entity(entity: string, k: int = 8)`

实体锚定检索——以命名实体为锚，召回含该实体的证据（利用引擎实体信号）。

- `entity`: 实体名（如人名、地名、组织）

**返回**: `Evidence[]`

### `stop(evidence_ids: string[], assembly: string = "first_n")`

结束导航并组装最终证据包。

- `evidence_ids`: 从各步已见证据中选择的最终证据 ID 列表（去重后 ≤ 预算）
- `assembly`: 组装方式（`first_n` / `dedup`）

**返回**: `EvidenceBundle`（`evidence` + `total_tokens`，MUST ≤ answer-context cap）

## 调用/返回格式

模型每步输出单个 JSON 对象（系统 prompt 约束，无额外文本）：

```json
{"tool": "search", "tool_args": {"query": "...", "k": 8}, "rationale": "..."}
```

Go 侧解析 → 执行 → 把 `returned_evidence` 注入下一轮 prompt。

## 硬约束

- **工具名白名单**: 仅 `search` / `expand_query` / `follow_entity` / `stop`。未知工具 → 该步作废并重新请求（或计入失败重试）。
- **步数上限**: 默认 4（可配）。超限未 `stop` → `fallback_triggered=true`。
- **预算纪律**: `stop` 的证据包 `total_tokens ≤ answer_context_cap`（008 纪律）；中间检索结果不计入，但 `budget_usage.nav_tokens` 单独记账。
- **fail-closed**: LLM 调用失败 / JSON 解析失败 / 超步数 → 用单次检索 top-k 结果作答（与现状逐字节一致），绝不产生空答案。
- **幂等**: 相同 query + 相同 store 的检索结果确定性一致（复用现有混合检索，无随机）。
