---
title: 冷 vllm 上的 jev shard 风暴 — 排障与解法(2026-09-28 E 臂夜)
summary: 冷启动 vllm 上 12-question jev shard 全线 30s 超时降级(shard failure → negative cache 连锁);$D9 的 p50 1.1s 依赖连跑多日的热机 vllm。解法 = --jev-deadline 放宽(慢但语义完整),劣化为分相/warmup(裸文本 prefill 无效,chat-template 块不对齐)。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-09-28
tags: [operations, autodl, jev, openjev-shim, vllm, shard-failure, degraded]
---

# 冷 vllm 的 jev shard 风暴

## 症状(E 臂夜,2026-09-28 21:21-22:30,七版 run)

冷启动(当天重启)的 vllm 上,`--jev-arms` 的 filter 调用:

- journal `latency_ms` **精确钉在 30,000ms**(= `jevFilterTimeout` 常数),`degraded:true`,`notes=["shard failure: no partial adoption"]`
- 后续 30s 窗口全部走 `negative cache` 降级(免网络调用,`latency_ms=0`)
- shim 日志:`answers failed ... upstream call failed: Post :8000/v1/chat/completions: context canceled`(elapsed 4-8s,客户端 deadline 层取消)
- 并发越低越糟:c16 下 25 行全部 degraded;c32(v3)反而多数**成功但慢**(14-21s)

## 证据链

1. `specs/051-jev-relevance-filter/pilot-artifacts/jev_filter_calls.digest.txt`(**9/24 pilot**,非 $D9):205/214 degraded,同样的 shard-failure 风暴 → 病不是 E 臂引入,是**冷 vllm 固有**。
2. $D9(9/28)= p50 1,092ms / degraded 0.42%:其 vllm 从 9/24 起连跑 4 天,prefix cache 全热 → 12q shard 的 9k prefill 几乎全命中,仅剩 ~300 tok JSON decode(A3B MoE 单流 ~250 tok/s)。
3. 裸文本 warmup(completions prompt=store 文本)无效:shim 走 chat/completions,**chat template 的 token 块与裸 prompt 不对齐**,prefix cache 不命中。
4. 物理账:16-44 个 thinking answer 流(B 臂 8.4k prompt)把 decode 带宽切到 ~20 tok/s/流,shard 排队+全价 prefill 必然撞 30s。

## 解法(按优先级)

| 方案 | 效果 | 代价 |
|---|---|---|
| **`--jev-deadline 150s`(489c61b flag)** | shard 慢但**成功** — filter 语义完整,分数/token 有效 | 每题 filter ~1-2min,run 变慢;SC-007 延迟门如实 HOLD |
| 分相运行(filter 与 answer 不共载) | 根治 | harness 结构改动(未做) |
| 保持 vllm 常热(不重启) | $D9 证明可行 | 与"空闲必停"冲突 |
| 裸文本 warmup | **无效** | — |

## 相邻发现

- `--concurrency 8`(<10 conv worker)触发 harness **死锁**(all goroutines asleep,zero IO;`gateUsageAttempts` 环路;c32 从未复现)。栈存 `*-deadlock-c8/gated.log`。修复前不要用 c<10。
- filter journal 的 `latency_ms=0` 行 = negative-cache 降级,**不是**成功;算真实调用延迟要过滤。
- shard 常数:eval `jevFilterShardSize=12`(~9k tok/shard);150 cand → 13 shard 并发打 shim。
