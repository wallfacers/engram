# Data Model: 查询时时间有效性解析

**Branch**: `027-temporal-validity-resolution` | **Date**: 2026-08-06 | **Spec**: [spec.md](spec.md)

## 概述

027 **不新增存储 schema**。解析器的输入/输出全部在 `cmd/locomo-bench/`（harness 内存结构），
只读消费 022 已交付的时间信息。数据流：

```
compileFormalSources（候选→source，026 已实现）
  → [027] temporal_resolution（确定性时间组织）
  → answer bundle（022 cap 内）
  → 固定 answerer
```

## 输入：解析器看到的候选/source（复用 026 结构）

解析器从 `compileFormalSources` 产出的 source list 消费以下字段（只读）：

| 字段 | 来源 | 说明 |
|---|---|---|
| `SourceID` | evidencecompiler.Source | 证据 ID，bundle 绑定锚点 |
| `OccurredAt *time.Time` | evidencecompiler.Source | **事实发生时刻**（可空）——时间解析的唯一时间轴 |
| `Text` / 内容 | evidencecompiler.Source | 证据原文 |
| `Candidate.SourceIDs` | compileFormalSources | 候选→source 映射（三臂共享同一 flat list → 逐字节一致） |

不新增字段、不改结构；解析器只读。

## 中间结构：主题-版本分组（确定性 supersede 判定）

解析器内部按"同一事实主题的多个版本"分组，**不持久化**：

```text
主题分组（TemporalGroup）:
  key:     确定性主题键（V1：候选内实体/属性提及聚类的规范化键）
  versions: []Version（按 OccurredAt 升序全序）
    Version:
      SourceID   string      # 绑回证据
      OccurredAt time.Time   # 版本时间
      Content    string      # 版本内容
      IsSuperseded bool      # 非末位即 superseded（时间序判定）
```

- V1 主题判定 = 确定性规则（同实体/属性提及聚类）；不引入 LLM（spec Assumptions）。
- `OccurredAt` 缺失的 source 不参与分组，按基线行为处理（spec FR-005 fail-closed）。

## 输出：时间组织后的 bundle 选择

| 解析模式 | 触发 | 输出 |
|---|---|---|
| **当前值解析** | query 为知识更新/当前状态语义 | 选主题组最新 valid 版本；superseded 版本排除但记录（供 audit） |
| **演化链组装** | query 为演化/时序语义 | 按 OccurredAt 全序组装完整 superseded→current 链，逐项绑 SourceID |
| **时间窗约束** | query 含显式时间范围（EvidenceNeed.TimeConstraints） | 仅保留 OccurredAt 覆盖该范围的版本，越窗排除并记录原因 |
| **退化** | 无 OccurredAt / 单版本 / query 无时间语义 | 输出与基线一致（零变化） |

输出 bundle 在 022 冻结 token cap 内（沿用 fail-closed 预算行为，超 cap 按 relevance 裁剪）。

## 审计字段（per-question，进归因报告）

| 字段 | 说明 |
|---|---|
| `mode` | current_value / evolution_chain / temporal_window / degraded |
| `group_count` | 主题分组数 |
| `versions_considered` | 参与解析的版本数 |
| `superseded_excluded` | 被判定 superseded 而排除的版本数 |
| `window_excluded` | 时间窗过滤排除的版本数 |
| `unresolved_time` | OccurredAt 缺失的 source 数 |
| `resolution_oracle` | superseded/current 版本是否在候选池（区分 resolution miss vs candidate miss） |

## 与既有 schema 的关系

- `memory_evidence.occurred_at`：底层时间来源之一（经 evidencecompiler.Source.OccurredAt 暴露）。
- `memory_evidence_events` / `memory_evidence_heads`：append-only 生命周期（V1 可选只读对照，
  用于校验时间序与 supersede 判定；不改变 V1 主路径）。
- **不新增** `memory_entries` 列、不新增投影、不新增关系表（migration 历史中
  event_start/superseded_by/revision 曾被 ADD 又 DROP，014 已证伪写侧时间 schema 路线）。
