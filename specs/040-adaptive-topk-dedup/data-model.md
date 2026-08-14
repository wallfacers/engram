# Data Model: 自适应检索深度

本 feature **无新存储表、无 schema 迁移**（宪法：schema 真相 = `store/migrations.go`，本 feature 不触碰存储）。所有实体是 eval harness 的内存态 + 诊断输出 JSON。

## 实体

### 1. 检索结果项 `memory.Result`（已存在，复用）

- **Score** `float64`：RRF 融合分数（knee 检测的输入序列）。
- **Name** `string`：以 `"chunk-"` 前缀区分「原文片段（chunk）」与「提取事实（fact）」，供 `applyChunkQuota` 分区。
- **ProjectionKind / Content / SourceSessionID**：沿用现有，本 feature 不改。

**关系**：宽池检索（`topK*6`，最小 300）产出一个按 Score 降序的有序列表；该列表是 knee 检测与 `applyChunkQuota` 的共同输入。

### 2. 自适应截断点（新增，harness 内存态）

由宽池 Score 序列经 gap-knee 检测推导：

| 字段 | 类型 | 含义 |
|---|---|---|
| `kneeIndex` | int | gap-knee 检测定位的相关→噪声拐点位置 |
| `adaptiveTopK` | int | 最终 topK（k*）；`= clamp(kneeIndex, minK, fixedTopK)` |
| `fallback` | bool | 无显著拐点时回退固定 topK（对应 FR-004） |

**推导规则**：归一化 Score 序列 → 相邻 gap → 最大拐点 → `clamp` 到 `[minK, fixedTopK]`。无拐点则 `fallback=true, adaptiveTopK=fixedTopK`。

### 3. 检索诊断记录（扩展，诊断输出 JSON）

在现有 `AttributionTrace` 之上扩展（或并列新诊断记录），逐题字段：

| 字段 | 类型 | 含义 |
|---|---|---|
| `gold_rank_pool` | int | gold 在宽池中的 1-indexed rank（复用 attribution 现有定义） |
| `gold_in_pool` | bool | gold 是否出现在宽池 |
| `adaptive_topk` | int | 本问题的自适应 k*（或回退后的 fixedTopK） |
| `knee_index` | int | 拐点位置 |
| `fallback` | bool | 是否回退 |
| `dropped_gold` | bool | `adaptive_topk < gold_rank_pool` 时为 true（收缩会丢 gold） |

**汇总指标**（headroom 诊断产出）：
- `dropped_gold_count` / `dropped_gold_ratio`：收缩会丢 gold 的问题数/占比。
- `k* 分布`（min / median / max）、`平均预算下降率`（`mean(adaptive_topk) / fixedTopK`）。
- `knee_rate`：存在可辨识拐点的问题占比。

## 状态转换

```
固定深度（flag off，默认） ── 字节一致，不触发 knee 检测
        │  --adaptive-topk 开启
        ▼
宽池 Score 序列 → 归一化 → gap 序列 → knee 检测
        │
        ├── 有拐点 → clamp(kneeIndex, minK, fixedTopK) → adaptiveTopK = k*
        └── 无拐点 → fallback=true → adaptiveTopK = fixedTopK
        │
        ▼
applyChunkQuota(wide, adaptiveTopK, quota)  # quota 不变，facts 槽位 = k* − quota
```

## 验证规则（映射自 spec FR）

- **FR-001**：`--adaptive-topk` 关闭时，检索与作答逐字节一致（不进入 knee 检测路径）。
- **FR-003**：`quota` 参数在自适应路径中恒不变（chunk 保底不取消）。
- **FR-004**：`fallback=true` 时 `adaptiveTopK == fixedTopK`，行为与固定深度一致。
- **FR-005**：`adaptiveTopK >= minK` 恒成立。
