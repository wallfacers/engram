# Contract: RelevanceFilter + Jev pointer 协议 + 阈值策略

冻结于实现之前（Constitution III）。破坏性变更须 MAOR/MAJOR + 迁移说明；本期为纯增量。

## 1. Go 接口（引擎包 `filter`）

见 [data-model.md](../data-model.md)「接口」节，语义硬约束：

1. `Filter` 返回概率与输入**下标对齐**；适配器不排序、不截断（取舍只在 `Select`）。
2. 失败（HTTP 非 2xx / 超时 / 任一分片失败 / 概率数组长度不符）→ `error` 非空 + `Meta.Degraded=true`；**不做部分采用**（R4）。
3. 构造未配置（缺 BaseURL 或 APIKey）→ 返回具体 `nil, nil`（typed-nil 纪律，边界折叠）。
4. 请求指令必须条件化：「这条记忆对**当前这个问题**是否必要」（per-query 条件化，不是全局重要性）。
5. 池 >64 分片（32–64/片）并发请求，响应按 index 合并；整过滤 deadline 默认 1s（ctx 派生、可配），单请求超时取 min(剩余, 1s)；不重试（护 SC-005 降级臂确定性）；失败后 30s **客户端级**负缓存直接降级（任一失败在 TTL 内抑制全部调用；防逐轮打挂掉端点的永久税；非熔断，到期自动重试）。（Slice-1 仲裁）

## 2. Jev pointer 协议（wire shape）

请求（§21.4，示例即合同）：

```json
{
  "model": "<pinned-revision>",
  "state": {
    "query": "<用户问题>",
    "memories": { "m1": {"name": "...", "text": "..."} }
  },
  "questions": {
    "need_m1": {
      "type": "noul",
      "instructions": "Memory m1 is necessary to answer or act on the query. The query is about <问题条件化描述>."
    }
  }
}
```

响应：每题一个校准概率（0–1）。客户端只依赖「question key → 概率」这一形状；字段细节适配器内收敛，wire 变更不波及接口。（Slice-1 注记）实现对响应形态容忍解析（probabilities/questions/results 多形态）；**T15/T19 前置条件**：评测/冒烟前须对真实端点钉死 canonical 形态与路由 path（默认 `/answers`）。（Slice-6 仲裁）请求里的 `text` 是**遮蔽后的记忆文本**（`filter.MaskSecrets`）：用户明确要求写入（`user_requested`）的密钥只入本地库，不发云；闸门侧 draft/userTurn 同样遮蔽后再发。

## 3. Select 策略（纯函数，测试即文档）

输入 `probs`、`cands`、`Policy{0.5, 0.35, 3, 12}`：

1. 保留 `p ≥ Theta`，按 p 降序，截 `KShowMax`；
2. 若 0 条：放宽保留 `p ≥ RelaxTheta`，按 p 降序，截 `RelaxMax`；
3. 若仍 0 条：返回空（诚实空，调用方不得硬塞 top-k）；
4. `Pinned && Trigger 命中 query` 的条目无条件注入（排最前，不计 Dropped）。trigger 命中定义：trigger 非空且 query **大小写不敏感包含** trigger；pinned 优先占位且**计入 KShowMax 总数硬顶**（护 SC-002）；病态 pinned>KShowMax 时全部展示并在遥测注记 override；
5. 输出下标按「pinned 先、其余按 p 降序」。（互审修订）输出条目 `score` 仍为 RRF 融合分，与展示顺序（p 降序）**非单调**；消费方 MUST NOT 再按 `score` 重排（CLI render 按展示序输出）。
6. （Slice-1 仲裁冻结）规范入口 = `SelectForQuery(query, probs, cands, pol)`；`Select(probs, cands, pol) ≡ SelectForQuery("", …)`（无 query 不做 trigger 旁路）。放宽段触发 = 阈值段（pinned 除外）保留 0 条；pinned 注入永不进 Dropped；空 `Trigger` 的 pinned 不旁路（item 4 字面）。`None` 语义 = 全 1.0 概率（阈值恒过），截到 show 由组合层完成（§4.3）。`FilterMeta.Notes` 引擎侧注记不进 MCP 序列化。

## 4. 组合点（memory/retriever.go）

`(*Retriever).SearchFiltered(ctx, query string, pool, show int, flt filter.RelevanceFilter, pol filter.Policy) ([]Result, filter.FilterMeta, error)`：

1. `Search(ctx, query, pool)` 取宽池（RRF 原序）；
2. 映射 `[]Result→[]Candidate`，调 `Filter`+`Select`；
3. 按 Select 下标重排返回；`flt == nil` 时等价 `Search(ctx, query, min(pool, show))` 截断（none 语义）；截断归属：降级/none 路径由组合层按 `show` 截，Select 路径由 `KShowMax`/`RelaxMax` 截；组合层以自己的 `SelectForQuery` 实算结果覆盖 `FilterMeta.Kept/Dropped`（过滤器侧初值仅供参考）。
4. 过滤失败 → 降级返回 `Search` 结果的前 `show` 条 + `Degraded=true`，且 **error 为 nil**（过滤失败不向上传播）；error 仅在底层 `Search` 失败时非空。

现有 `Search/SearchMulti/SearchWithDiagnostics` **签名与行为不变**（parity 测试锁定）。

5. （Slice-2 仲裁冻结）退化输入与遥测口径：`pool ≤ 0` → 按 `show`；`show ≤ 0` → 8（与 Search 默认 k 一致）；空候选池仍调用过滤器（none/jev 遥测可辨）；错误路径 meta = 过滤器 meta 强制 `Degraded=true` 且 Kept/Dropped 按实收重算；`Dropped` = 池条数 − 展示条数，恒 ≥0（pool<show 退化输入夹取）；typed-nil 判定 helper 统一由 filter 包导出（slice 3 落地，供 ADR-008 共用）。
