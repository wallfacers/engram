# Contract: `memory_search` 契约增量（MCP）

## Parity 不变式（HARD）**适用前提：服务端 `ENGRAM_SEARCH_POOL`/`ENGRAM_FILTER` 未设置**（两旋钮为服务端 config，由 `mcpserver/config.go` 读取，MCP 客户端无法设置服务端 env）：

**不传新参数的 `memory_search` 调用，响应与今天逐字节一致。** 契约测试锁定；破坏此不变式 = 阻断合入。

## 输入增量

| 参数 | 类型 | 默认 | 说明 |
|------|------|------|------|
| query | string | 必填 | 不变 |
| limit | int? | 8 | 不变；filter 路径下仅决定降级回退条数 |
| candidate_pool | int? | = limit | RRF 宽池；显式给大值才走宽池（>limit 时须有 filter，否则等价 limit 并注记） |
| filter | string? | `"none"` | `"none"` \| `"jev"`；未配置 Jev 客户端时 `"jev"` 退化为 none + `degraded=true` |
| theta | float? | 配置默认 0.5 | 仅调参/评测用覆盖 |

校验/夹取：`candidate_pool ≥ 1`、`candidate_pool ≤ 500` 硬夹（诚实规模：评测 150 级，拒绝隐式更大）；`candidate_pool < limit` → 取 limit；`theta ∉ (0,1)` → 拒绝；`RelaxTheta > Theta` → 自动夹为 Theta；`filter="jev"` 且 `pool == limit` → 正常对 top-limit 过滤；`filter` 取值白名单。每条规则配一条测试。（Slice-3 仲裁钉入）`theta` 单独出现（无 filter/pool）→ 惰性走 parity 分支（不发遥测）；env 旋钮校验：`ENGRAM_JEV_THETA/RELAX_THETA` 越界**拒绝**、`ENGRAM_SEARCH_POOL>500` 夹 500、`<1` 拒绝；`filter="jev"` 已配置但降级 → `backend="jev"`+`degraded:true`（不报假 backend），未配置 → `backend="none"`；legacy 分支不输出 `pool_size`（parity 优先）；写门开启但无 client → `gate{backend:"none",degraded:true}` 并放行（结构性 fail-open）；显式 `filter="none"` 无池 → legacy 分支（最小响应）。展示数组字段名 = **`results`**（旧称 hits 以此为准）；`theta` 覆盖为调用方旋钮，预注册纪律归评测协议（服务端不加锁）。

## 输出增量

```json
{
  "results": [ ... ],                 // 过滤后短名单（filter!=none 时；可能为空数组）
  "scope": "ranked_subset",        // 不变；过滤空与检索空同一语义
  "limit": 8,
  "returned": 3,
  "pool_size": 150,                // 新增：本次宽池大小（无过滤时 = returned 上限）
  "filter": {                      // 新增：过滤遥测
    "backend": "jev", "theta": 0.5,
    "kept": 3, "dropped": 145, "degraded": false,
    "latency_ms": 310, "cost_usd": 0.0004
  }
}
```

## 行为矩阵

| 场景 | 行为 |
|------|------|
| 默认调用（无新参数，且 `ENGRAM_SEARCH_POOL`/`ENGRAM_FILTER` 未设） | 与今天逐字节一致（parity 不变式；不输出 `filter`/`pool_size` 字段） |
| `filter="jev"` + 客户端可用 | 宽池 → Select（两段式）→ 短名单；`filter.degraded=false` |
| 全灭且放宽仍空 | `results: []`，诚实空（不硬塞） |
| Jev 失败/超时/无 key | `results` = RRF 前 limit 条，`filter.degraded=true`（结构性事实，不报假 backend） |
| `candidate_pool > limit` 且 `filter="none"` | 返回前 limit 条（等价默认），`pool_size` 如实记 |
