# Contract: 证据装配（Evidence Assembly，US1）

**契约对象**: 检索之后、作答之前的证据装配器（`cmd/locomo-bench/assembly.go`）
**范围**: 装配器输入 → 输出 EvidenceAssembly + answerer prompt；引擎零改动

## 输入

| 字段 | 类型 | 来源 |
|---|---|---|
| `question` | string | 当前问题文本 |
| `candidates` | memory.Result[] | 检索候选集 C_q（闭包；含 Name/Content/Score/EventDate） |
| `category` | int | 问题类别（`qa.Category`，LoCoMo 1-4） |
| `current_date` | string | `YYYY-MM-DD`（017 date scaffold） |
| `cap` | int | answer-context 预算（默认 3600） |

## 装配流程（三阶段）

### 1. token 记账（FR-002）

- 每个 `candidates` 单元 → `EvidenceUnit{token_count}`，通过**批量 `/tokenize`**（vLLM，复用 `vllmTokenCounter` 基建，token_counter.go）一次请求多条文本，逐条返回精确 count。
- vllmTokenCounter 不可用（离线/未连答题模型）→ `estimateTokens` fallback，该装配标记 `tokens_estimated=true`（显式降级，宪法 V）。
- **禁止**：按字符数 / 条数 / `len(runes)/4` 当作最终 token 数。

### 2. chunk 原文优先（FR-003）

- 按 `Name` 前缀 `"chunk-"` 分 chunk / fact（复用 chunks.go:210 判定）。
- 装配顺序：**chunk 先入**（按 score 降序）→ fact 补足 → 到 `cap` 为止。
- 输出 MUST 报告 `chunk_fraction = Σ chunk tokens / total_tokens`（SC-002；029 现状 ~1%）。
- 与检索侧 `--chunk-quota`（槽位保留）解耦：装配器对**已进入候选的证据**重排，保证 chunk 占比。

### 3. 类别条件结构（FR-004）

per-category 策略选择器（复用 `retrievalFor(qa.Category)` 的 per-category 覆盖模式）：

| category | structure | 行为 |
|---|---|---|
| 2 (temporal) | `temporal` | 复用 `buildTimelineBlock`（timeline.go:70）渲染时间线块；装配单元按 `event_date` 时间序 |
| 1 (multi-hop) | `entity` | **新建**实体组织：提取候选内实体 → 按实体分组呈现 → 组内分数序（借鉴 IRIS slot-merge 去重/边界纪律） |
| 3/4 | `generic` | chunk 优先 + 分数序（无额外结构） |

- 类别无法判定 → `generic`，不报错。
- `structure` MUST 写入 EvidenceAssembly 记录。

## 输出

`EvidenceAssembly`（data-model.md）：`units[]` + `total_tokens`（≤ cap）+ `structure` + `chunk_fraction` + `tokens_estimated`。

answerer prompt 渲染：在现有 `buildAnswerPrompt` 基础上，把 `memories` 替换为装配后的 `units`（chunk 优先序 + 类别结构块）。**关闭时（默认）渲染路径逐字节不变**（SC-004 parity）。

## 错误语义

| 情形 | 行为 |
|---|---|
| 候选集为空 | 照常装配（空 units），渲染 `(none)`，不报错不空答 |
| tokenizer 不可用 | estimateTokens fallback + `tokens_estimated=true` |
| 类别无法判定 | `generic` |
| 全部关（默认） | 装配器不介入，现有 `buildAnswerPrompt` 原样走 |

## 验收（离线可断言）

1. `total_tokens == Σ 精确 token_count`（零估算误差；批量 `/tokenize` 或 stub）
2. 混合 chunk+fact 输入下 `chunk_fraction ≥ 阈值`（SC-002）
3. temporal 输入 `structure == temporal` 且单元按时间序；multi-hop → `entity`
4. 全关默认：与现有路径 parity（逐字节）
