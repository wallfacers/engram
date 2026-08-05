# 抽取 Prompt 契约（027）

**用途**:定义 event 抽取的**输入形状**与系统提示词契约。提示词版本纳入 config-hash
（prompt 改动 → 投影重建）。

## 输入（每消息一次调用）

| 输入 | 说明 |
|---|---|
| `message_text` | 该条原始消息全文 |
| `speaker` | 说话人 |
| `conversation_context` | 最近 N 条消息（上下文窗口，供关系抽取；N 可配，默认 8） |
| `conversation_id` | 对话 id |

## 系统提示词（契约要点）

```
你是记忆抽取器。把给定对话消息整理成「事件条目」，包含：
1. fact_entries（事实视角）：这条消息里发生了什么（客观事件描述，自然语言，一条事实一项）
2. relation_entries（关系视角）：这条消息里体现的人际关系、因果关系、共同参与、时序关系
   （relation_type ∈ interpersonal | causal | co_participation | temporal_order | preference；
   subject/object 为参与实体；text 保留上下文语境的自然语言描述）
3. absolute_ts：若消息中有明确绝对时间则填 ISO-8601，否则空
4. relative_ref：消息中的相对时间引用原文（如「去年」「下周三」），没有则空

约束：
- 只抽取消息中【明确陈述】的内容，不得臆造或推断消息之外的事实
- fact_entries 不得为空；relation_entries 可以为空（该消息无关系信息）
- 输出必须是合法 JSON，严格符合给定 schema，不要输出 schema 之外的字段
```

## 输出

JSON，形状见 [event-contract.md](event-contract.md)。

## 校验与退回

输出经 [event-contract.md](event-contract.md) 的校验规则判定；校验失败 → fail-closed
退回原文 chunk（[fail-closed.md](fail-closed.md)）。

## 版本化

- prompt 版本号 + 模型名 + 字段枚举 共同构成 config-hash
- 任一变更 → event 投影全量重建（幂等）
