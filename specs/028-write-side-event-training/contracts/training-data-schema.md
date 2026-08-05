# Contract: 训练数据 Schema（028）

**Purpose**: 冻结训练抽取器的数据格式（JSONL），保证教师标注、人工精修、训练、审计四环节读写一致。实现于 `specs/028/tools/build_training_data.py`。

**Status**: Draft · **Version**: 0.1.0

## 格式

一行一个 JSON 对象（JSONL）。字段定义见 [data-model.md](../data-model.md) E1。

```json
{
  "id": "train-0001",
  "conv_id": "conv-3",
  "source_msg_id": "evt-…",
  "input_text": "Evan: I went out with my friends last Friday…",
  "context_turns": ["Sam: How was your week?", "Evan: …"],
  "event_json": {
    "source_id": "evt-…",
    "fact_entries": [
      {"id": "f1", "text": "Evan went out with his friends on 2023-01-06", "grounded": true}
    ],
    "relation_entries": [],
    "absolute_ts": "2023-01-06",
    "relative_ref": "last Friday"
  },
  "abs_time_label": "2023-01-06",
  "source": "teacher",
  "revised": false,
  "revision_notes": ""
}
```

## 校验（实现时 MUST 断言）

1. `event_json` 通过 027 `ValidateLenient`（丢弃未知 relation_type，保留事件）
2. `event_json.absolute_ts` 为 ISO 日期（`YYYY-MM-DD`）；含时间语义但无绝对时间 → 记 `anchor_fail`
3. 含时间语义（`relative_ref` 非空或事实含时间表达）MUST 有 `abs_time_label`
4. `source=human_refined` MUST 有 `revision_notes`
5. `id` 全局唯一

## 审计输出（build 脚本产出）

`audit.json`（FR-002）：
- `n_total` / `n_success` / `n_schema_fail` / `n_anchor_fail`
- `time_anchor_rate` = 含绝对时间事件数 / 总事件数
- `source` 分布（teacher vs human_refined）
- 修订率 = `revised=true` / 总数

## 与 027 的兼容

`event_json` 字段名与 `memory/eventstore` Event 类型对齐；教师 prompt 复用 027 抽取 prompt + 时间锚定强化（见 `teacher-extract-prompt.md`）。
