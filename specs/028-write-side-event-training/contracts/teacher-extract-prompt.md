# Contract: 教师抽取 Prompt（028 时间锚定强化）

**Purpose**: 冻结 US1 教师抽取（DeepSeek-v4-pro）与 US2 训练数据的标注指令。基于 027 的双视角 event 抽取 prompt，增加**强制绝对时间锚定**。

**Status**: Draft · **Version**: 0.1.0

## 与 027 prompt 的关系

027 已有 `EventExtractionSystemPrompt`（`memory/eventstore/extract.go`，双视角抽取 + `[source_id=...]` 标记）。本契约是它的**强化版**（教师/训练标注用），不改引擎 prompt；引擎仍走 027 的 fail-closed 抽取。

## 系统提示（增量：时间锚定段）

在 027 双视角抽取指令基础上追加：

```
TIME ANCHORING (MANDATORY):
- Convert every relative time expression to an ABSOLUTE date before storing.
  Examples: "last Friday" → "2023-01-06"; "yesterday" → "2023-01-09";
  "three weeks ago" → "2022-12-19"; "next month" → "2023-02-01".
- Put the absolute date in "absolute_ts" (ISO YYYY-MM-DD). Keep the original
  relative expression in "relative_ref" ONLY as a trace, never as the answer.
- A FACT about when something happened MUST carry the absolute date in its text.
- If the conversation provides enough context to resolve the relative time,
  you MUST resolve it. Only leave absolute_ts empty when the date is truly
  unresolvable (then note it in relative_ref).
```

## 用户提示（增量：标注输出）

复用 027 `buildEventUserPrompt`（含 `[source_id=<SourceLedgerID>]`），输出 schema 不变（`fact_entries` / `relation_entries` / `absolute_ts` / `relative_ref`）。

## 输出校验（教师调用后 MUST 断言）

1. `event_json` 过 027 `ValidateLenient`
2. 含时间语义事件 MUST 有非空 `absolute_ts`（否则计 `anchor_fail`）
3. `absolute_ts` 格式 `YYYY-MM-DD`

## 参数化

US1 用 `EVENT_LLM_MODEL=deepseek-v4-pro` + 本 prompt；US2 训练后同 schema 输出（模型已学入锚定）。

## 部署注记

教师调用走 DeepSeek API（SaaS 线成本）；引擎侧 prompt 版本记录进 027 的 config-hash（可重建）。
