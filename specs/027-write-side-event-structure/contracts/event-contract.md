# Event 数据契约（027）

**用途**:定义 event 抽取的**输出形状**与校验规则。这是引擎侧 `memory/eventstore/` 与
本地 LLM sidecar 之间的边界契约（宪法 II/III：契约先定，再实现）。

## 抽取输出（LLM sidecar 返回的 JSON）

```json
{
  "conversation_id": "conv-3",
  "source_ledger_ids": ["msg-41", "msg-42"],
  "speaker": "user",
  "fact_entries": [
    {"text": "Caroline 去年 8 月参加了 Pride 游行"}
  ],
  "relation_entries": [
    {
      "relation_type": "co_participation",
      "subject": "Caroline",
      "object": "Melanie",
      "text": "Melanie 对 Caroline 的 Pride 经历很感兴趣，想下次一起去"
    }
  ],
  "absolute_ts": "2023-08-17T00:00:00Z",
  "relative_ref": "去年"
}
```

## 字段规则

| 字段 | 必填 | 规则 |
|---|---|---|
| `conversation_id` | 是 | 非空 string |
| `source_ledger_ids` | 是 | 非空 string 数组；每条必须在 Evidence Ledger 中存在 |
| `speaker` | 是 | 非空 string |
| `fact_entries` | 是 | 非空数组；每项 `text` 非空 string，`grounded` 默认 true |
| `relation_entries` | 否 | 可空数组；每项 `relation_type` ∈ 枚举（见 data-model.md），`subject`/`object`/`text` 非空 |
| `absolute_ts` | 否 | 可空；ISO-8601 或空串 |
| `relative_ref` | 否 | 可空；原文相对引用，允许空 |

## 校验规则（fail-closed 判定）

满足**任一**则整条退回原文 chunk（不产生 event，记录失败）:

1. JSON 非法或顶层非 object
2. `conversation_id` / `source_ledger_ids` / `speaker` / `fact_entries` 缺失或类型错误
3. `source_ledger_ids` 中任一 id 不存在于 ledger
4. `fact_entries` 为空数组
5. 任一 `text` 字段为空或超过长度上限（默认 500 字符）
6. `relation_type` 不在枚举内
7. 单条输出超过总体 token 上限（默认 2000）

## 边界

- **有界**:每条消息最多产生 1 个 Event（消息过长时按连续段拆分，段上限可配）
- **幻觉治理**:阶段 1 抽样人审 + 判定统计（抽取数/失败率/疑似幻觉率），不运行时校验
- **默认关**:不配置 event 抽取时，store 行为与现状逐字节一致（FR-004）
