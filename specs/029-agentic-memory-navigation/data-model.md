# Data Model: Agentic 多步记忆导航

**Date**: 2026-08-06 · **Spec**: [spec.md](spec.md) · 契约详情见 [contracts/](contracts/)

## 实体

### NavigationTrajectory（导航轨迹）

一次查询的多步导航全记录，US2 的可审计产物（FR-007）。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `question_id` | string | ✓ | 配对子集的问题 ID（`conv-N-q-M`） |
| `query` | string | ✓ | 原始查询 |
| `steps` | NavStep[] | ✓ | 导航步骤序列（1..N，N ≤ 步数上限） |
| `final_evidence` | EvidenceBundle | ✓ | 最终喂给 answerer 的证据包（≤ 预算） |
| `budget_usage` | object | ✓ | `{steps, nav_tokens, answer_context_tokens}`（导航消耗单独记账） |
| `fallback_triggered` | bool | ✓ | 是否触发 fail-closed（超步数/失败） |
| `answer` | string | 触发后 | answerer 输出（由评测框架写入） |

**Validation**:
- `len(steps) ≤ max_steps`（默认 4，可配）
- `steps` 最后一步 MUST 是 `stop`（或触发 fail-closed）
- `budget_usage.answer_context_tokens ≤ answer_context_cap`（008 纪律）
- `fallback_triggered=false` 时 `final_evidence` MUST 非空

### NavStep（导航步骤）

单步导航动作及其决策依据。

| 字段 | 类型 | 说明 |
|---|---|---|
| `index` | int | 步序号（1-based） |
| `tool` | string | 工具名：`search` / `expand_query` / `follow_entity` / `stop` |
| `tool_args` | object | 工具参数（见 contracts/navigation-tools.md） |
| `returned_evidence` | Evidence[] | 该步检索返回的证据（中间结果，不计入 answer-context） |
| `rationale` | string | 模型给的决策理由（审计用） |
| `latency_ms` | int | 该步耗时 |

### Evidence（证据单元）

检索返回的单条证据（复用现有候选结构）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `source_id` | string | 证据源 ID（chunk id） |
| `text` | string | 证据文本 |
| `score` | float | 该步检索的融合分 |
| `retrieved_by` | string | `semantic` / `keyword` / `entity` / `hybrid` |

### EvidenceBundle（证据包）

最终传给 answerer 的证据（MUST 在 answer-context 预算内）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `evidence` | Evidence[] | 去重后的最终证据 |
| `total_tokens` | int | 证据包 token 数（≤ cap） |
| `assembly` | string | 组装方式（`first_n` / `reranked` / `dedup`） |

### DiagnosisClass（诊断分类，US1）

US1 零成本诊断的逐题分类。

| 值 | 含义 | 后续 |
|---|---|---|
| `in_pool` | gold 在候选池（全对话 oracle 证明） | 池内 |
| `topk_hit` | 单次 top-k=30 已捞到 gold | 无需导航 |
| `rescueable` | 单次未捞到，但「换查询/换粒度/跟线索」模拟可救 | **多步导航目标** |
| `not_in_pool` | gold 根本不在池 | 导航救不了 |

**Validation**: 每题的 `topk_hit` 与 `rescueable` 互斥；`rescueable` 题 MUST 记录具体模拟动作与命中的证据。

## 关系

```text
query ──1:1──▶ NavigationTrajectory ──1:N──▶ NavStep ──N:1──▶ Evidence
                  │
                  └──1:1──▶ EvidenceBundle ──N:1──▶ Evidence
```

- 一条查询产出一条轨迹；轨迹由 1..N 步组成；每步检索返回多条 Evidence；最终 `stop` 步组装 EvidenceBundle。
- US1 诊断：每题映射到一个 DiagnosisClass（`in_pool` / `topk_hit` / `rescueable` / `not_in_pool`）。

## 状态流转

```text
start → [search] → [expand_query | follow_entity] → ... → [stop] → assembled → (answerer)
  └─(步数超限 / LLM失败 / 解析失败)─→ fail-closed ─→ 用单次检索 top-k 作答（与现状一致）
```

- 正常路径：多步导航 → `stop` → 证据包 → answerer。
- 降级路径：任何一步失败/超步数 → `fallback_triggered=true` → 单次检索结果作答（宪法 V，零行为变化）。
