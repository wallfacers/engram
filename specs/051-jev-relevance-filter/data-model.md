# Data Model: 051-jev-relevance-filter（Phase 1）

无数据库 schema 变更（无迁移、无新表列）。以下为进程内模型，签名用 Go 表述，权威语义见 [contracts/](contracts/)。

## 实体

### filter.Candidate（过滤器输入，中性类型）

| 字段 | 类型 | 说明 |
|------|------|------|
| ID | string | 条目 ID（pointer 模式里的短 id） |
| Name | string | 条目名（进入 Jev state） |
| Text | string | 条目内容（进入 Jev state） |
| Trigger | string | 触发词（pinned 旁路判定用） |
| Pinned | bool | 用户钉过（旁路注入） |
| Score | float64 | RRF 融合分（降级/退化排序保底） |

映射：`memory.Result` → `Candidate`（`Trigger`/`Pinned` 源自 `memory.Entry`；**`memory.Result` 需增补 `Pinned` 字段**（今日缺失，结构变更记于此，见 plan）。

### filter.FilterMeta（单次过滤遥测）

| 字段 | 类型 | 说明 |
|------|------|------|
| Backend | string | `"none"` \| `"jev"` \| `"openjev"`（未来本地近似） |
| Theta | float64 | 本次生效阈值 |
| Kept / Dropped | int | 留/丢条数（pinned 旁路不计 Dropped） |
| LatencyMs | int | 过滤耗时 |
| CostUSD | float64 | 本次成本（usage × 单价；无 usage 时 0 并注记 unknown） |
| Degraded | bool | true = 失败/无 key/分片失败，调用方已退回 RRF 前 limit 条 |
| Notes | []string | 引擎侧注记（cost unknown / pinned override），**不进 MCP 序列化**（Slice-1 仲裁） |
| InputTokens / OutputTokens | int | usage 透传（Slice-4 仲裁增补；SC-007 token 计量，切片 5 落地） |

### filter.Policy（阈值策略，纯数据）

| 字段 | 默认 | 说明 |
|------|------|------|
| Theta | 0.5 | 主阈值 |
| RelaxTheta | 0.35 | 二段放宽阈值 |
| RelaxMax | 3 | 放宽轮最多取 3 条 |
| KShowMax | 12 | 硬顶（防变相 150） |
| RelaxDisabled | false | `ENGRAM_JEV_RELAX=0` 映射；true = 跳过放宽段（Slice-1 仲裁） |

状态机（Select 纯函数）：`打分 → [p≥θ 且 top KShowMax] → 若空: [p≥0.35 且 top 3] → 若空: 空` ＋ pinned+trigger 命中旁路注入。

### filter.GateDecision（写前门决策）

| 字段 | 类型 | 说明 |
|------|------|------|
| Route | string | `write` \| `skip` \| `defer_to_packet`（本期只产生前两者；`defer_to_packet` 占位与短包线兼容） |
| Reasons | map[string]float64 | `is_durable` / `is_preference` / `is_secret` / `needs_probe` 校准概率 |
| Meta | FilterMeta | 同上遥测 |

路由规则（clarify Q4）：一次性 → `skip`；密钥 → `skip` **除非**用户明确要求记录自己的 key → `write`；`needs_probe` 本期恒 0 不启用；门失败 → 调用方视为无门（现行规则）。

## 接口

```go
package filter

type RelevanceFilter interface {
    // 返回与 cands 对齐的校准概率；失败时 error 非空，调用方降级。
    Filter(ctx context.Context, query string, cands []Candidate) ([]float64, FilterMeta, error)
}

type WriteGate interface {
    Gate(ctx context.Context, userTurn, draft string) (GateDecision, error)      // 简写 = UserRequested:false
    GateWithOptions(ctx context.Context, req GateRequest) (GateDecision, error) // 规范入口（Slice-1 仲裁）
}

// GateRequest{UserTurn, Draft, UserRequested} 为写门规范输入。

// 规范入口（Slice-1 仲裁）：SelectForQuery 携带 query 做 trigger 旁路；Select ≡ SelectForQuery("", …)。
func SelectForQuery(query string, probs []float64, cands []Candidate, pol Policy) []int
func Select(probs []float64, cands []Candidate, pol Policy) []int
```

`None{}` 实现 = 全 1.0 概率 + `Backend:"none"`（阈值恒过；截到 show 由组合层完成，走 `Select` 同一路径；Slice-1 仲裁）；`jev.Client` 同时实现 `RelevanceFilter` 与 `WriteGate`。构造函数对标 `embedding.NewReranker`：未配置返回 **具体 nil + nil error**，由调用方在边界折叠为非类型化 nil。

## MCP 契约增量（详见 contracts/memory-search.md）

`memory_search` 输入 +`candidate_pool`（默认 = limit）/+`filter`（默认 `"none"`）/+`theta`（可选覆盖）；输出 +`pool_size` +`filter:{backend,theta,kept,dropped,degraded,latency_ms,cost_usd}`。`memory_write` +可选 `user_turn`/`user_requested`（写门输入映射，均缺省；FR-010 为准，写门由服务配置 opt-in）。
