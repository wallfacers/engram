# Data Model: 写入侧事件时序结构记忆（027）

## 概述

027 不改变 Evidence Ledger 的 append-only 原文；新增**可重建投影**（event 投影），
按 config-hash 幂等重建，删除/重建不得删改原文（FR-003）。relation-summary 为派生态，
不持久化。

## 复用实体（不新增 schema）

| 实体 | 来源 | 角色 |
|---|---|---|
| Evidence Ledger | 022 | append-only 消息级原文；event 投影的唯一真相源 |
| Ranked Anchor | 022/025 | 检索命中的稳定候选锚点；event 渲染器围绕同一 anchor 展开 |
| Local LLM provider | `provider/` | event 抽取的模型接口（本地 sidecar，可替换） |

## 新增实体（可重建投影）

### Event（事件条目）

一条原始消息（或消息内的连续段）抽出的结构化事件单元。

| 字段 | 类型 | 约束 |
|---|---|---|
| `event_id` | string | 确定性（ledger_id + config-hash），重建幂等 |
| `source_ledger_ids` | []string | 引用的原始消息 id（1..N） |
| `fact_entries` | []FactEntry | 事实视角：发生了什么（客观事件描述） |
| `relation_entries` | []RelationEntry | 关系视角：谁和谁/因果/共同参与 |
| `absolute_ts` | *string | 可解析则填（ISO），否则空 |
| `relative_ref` | string | 原始相对引用原文（「去年」「下周三」），可能为空 |
| `conversation_id` | string | 所属对话（同 ledger） |
| `speaker` | string | 说话人 |

### FactEntry

- `text`: 客观事件描述（自然语言，非三元组）
- `grounded`: bool —— 是否能在 source ledger 中定位到支撑（fail-closed 时 false 则不产生）

### RelationEntry

- `relation_type`: 类别（interpersonal / causal / co_participation / temporal_order / preference）
- `subject` / `object`: 参与实体
- `text`: 关系描述（保留上下文语境的自然语言，不是实体-关系三元组）
- `grounded`: 同上

### RelationSummary（跨事件关系摘要，派生态）

周期性把语义相关、时间相邻的 event 合并成的跨事件关系文本。

| 字段 | 类型 | 约束 |
|---|---|---|
| `summary_id` | string | 确定性 |
| `event_ids` | []string | 被合并的 event 集合 |
| `window_start_ts` / `window_end_ts` | string | 覆盖的时间窗（可含相对引用原文） |
| `text` | string | 合成摘要（显式写出跨事件关系） |
| `grounded_events` | int | 摘要中可回溯到具体 event 的比例（fidelity 审计用） |

## 投影关系

```
event 投影 = f(Evidence Ledger, config{抽取模型, prompt版本, 时间锚定策略})
relation-summary = g(event 投影, config{合并窗口, 触发阈值, 证据数上限})
```

- 两者都是确定性函数（给定输入 + config-hash），可丢弃可重建
- `config-hash` 变化 → 全量重建（不改原文）
- 有界：每 event 证据数上限可配（默认值由 tasks.md 定），不得无界放大候选/token（FR-007）

## 状态转换

```
Evidence Ledger (append-only)
   │ 双视角抽取 (LLM sidecar, fail-closed)
   ▼
Event 投影 (可重建)
   │ 周期性合并 (窗口/阈值触发)
   ▼
RelationSummary (派生态, 不持久化)
```

- 抽取失败（schema 校验不过）→ 该条消息**退回存原文 chunk**，不产生 event，记录失败率（FR-005）
- 跨 session 事件：同一 namespace 内允许跨 session 关联（有界）；跨 namespace 隔离（FR-011）
