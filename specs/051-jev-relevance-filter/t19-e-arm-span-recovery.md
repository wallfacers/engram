# T19 · E 臂 — 血缘原文回收(span recovery)设计与 run 记录

**日期**:2026-09-28 晚 · **commit**:`7b985e6`(E 臂)+ `ccefb50`(speaker 前缀去重)
**维护者 goal**:分数回到 91.43 栈水平,token 是它的 ~1/6。授权直接代码跑、不跑对照实验。

## 1. 动机(错题解剖,$D9 数据)

B对D错 94 题(cat1 27 / cat2 28 / cat4 30)的三种死法,解药同一——带上命中事实的**血缘原文**:

| 死法 | 真实例子(conv-0) | 原文证据 |
|---|---|---|
| 枚举完整性 | q-15 "What activities does Melanie partake in?" gold=pottery,camping,painting,swimming | 4 项散在 5+ session(`D1:18` swimming…`D5:4` pottery);kept mean 5.1 砍任一项即错 |
| 交集多跳 | q-55 "…both painted?" gold=Sunsets | 两条分低事实(`D14:7`+`D17:12`)必须同时存活 |
| 限定词存活 | q-70 "transgender-specific events" | 限定词在原文(`D17:19`),提取短事实易丢;干扰项 relevance 更高 |

## 2. 设计(单变量纪律)

```
k150 池(与 D 完全同池、同一次 jev 调用、同降级语义)
  ├─ kept facts(= D 的 shortlist)
  └─ span 卡:kept 血缘(优先)+ pool 头 30 未 kept 者血缘(补被砍项)
       → EntryStore.SourceRefs → LedgerStore.GetMany → code-point 窗口合并
       → cap --jev-span-cap(默认 6;0 = E 严格退化为 D)
```

- E−D 唯一变量 = 追加 span 卡 → D→E contrast 可归因;chunk entry(无 atomic_fact 投影)跳过(Content 已是原文)。
- registration 冻结 SpanCap;verdict 增 D→E、B→E contrast;derived 增 `e_shown`/`e_spans`。
- 引擎零改动(只调公开 `SourceRefs`/`GetMany`/`filter.SelectForQuery` 组合)。

## 3. 飞行前实证(2026-09-28 盒上)

**Store 探针(conv0.db)**:四活动 fact→evidence 血缘全通(pottery 23 / camping 23 / painting 38 / **swim 2 条稀缺项**);**31 条 evidence 被 2+ 活动 fact 共同引用**("Last Fri I finally took my kids to a pottery workshop…we love painting")——一张 span 卡覆盖多枚举项,cap=6 预算效率高。发现并修复:evidence content 已含 speaker 前缀 → spanCardText 去重(`ccefb50`)。

**Journal 中途采样(run 前期 47 行/臂)**:D presented=kept(mean 9.79);**E presented−kept=6.00 严格 = cap**;`kept=1` 的近空注入题 span 仍补满 6(空注入保护)。

## 4. Run `$E1 = e-arm-20260928T212141`(gated,进行中)

- 3-rep majority(**91.43 = 3-rep+clean 口径**,9110 复现报告钉死),answer 臂 B/D/E,θ=0.5 与 $D9 可比
- 命令 = P3-RUNBOOK §6 step2 + `--jev-answer-arms B,D,E --jev-span-cap 6`
- 负载 = 27,720 大请求(filter 13,860 + answer 13,860)≈ $D9 的 4.5 倍;GPU decode ~884 tok/s 物理满载 → 预计 **14-17h**(明午收数)
- 健康:车队效应符合手册(vllm stop 累计推进、KV 67%、error=0、shim 正常)

## 5. 下一旋钮(若 E 正向但水位未达 91.43;均参数级零代码)

1. θ 0.5→0.35-0.4(报告 §5 首选)
2. max-tokens 8000→16000(042 口径;注意 token-counter 校准漂移风险,见 runbook §5 fix 方向)
3. degraded pass(SC-005 完整性,非分数项)

## 5.5 收数决策树(1-rep B,E run `e1be-…` 落地时执行)

| B→E 实测 | 判定 | 动作 |
|---|---|---|
| Δ ≥ +3pp 且 p<0.05 | span 机制显著有效 | 直接 `run-3rep.sh`(B,E × 3-rep + `--jev-filter-arms D,E`)冲 91.43 口径 |
| Δ +1~3pp 或 p≥0.05 | 有效但弱 | 3-rep 前+θ 0.4(`ENGRAM_JEV_THETA`,与 filter-arms 同轮) |
| Δ ≈ 0 / 负 | span 未兑现 | 查 derived `e_spans`(=6?)与 E 空注入率;若 span 正常但分数不动 → θ 0.35 + KShowMax 16 再试;仍不动 → span 假设降权,回 042 形态(chunk-verbatim 主导) |
| E token/B > 1/3 | token 超预算 | `--jev-span-cap 4` |
| E token/B < 1/10 | token 余量 | `--jev-span-cap 8`(枚举题受益) |

归因必做:`attribute.py` 交叉 $D9 的 94 道 B对D错 → E 救回数(span 对症率的直接证据)。

*收数后本文件补 §6 数字(B/D/E accuracy、D→E p、E token 比、flips 归因)。*

## 6. 结果(2026-09-29 夜,decider-4b filter,flash 答题,1-rep n=1540)

**Run** `$E1RD = e1rd-20260929T023000`(gated EXIT=0,零降级)。filter 上游已从 Vercel `typesafe-ai/jev`(credit 耗尽 402)换为**本地 decider-4b v2**(Apache-2.0,4090 bf16 + COMPILE + 修桶;shim `OPENJEV_UPSTREAM=typesafe` 直指 `/v1/systemone`,零映射,`8f968a5` 散文化 instructions 为无害重构:mean|ΔP|=0.0146、θ=0.5 翻转 0)。答题=deepseek-flash(DeepSeek 官方),判题同。**B 臂未跑**(维护者指令:太贵,历史锚 80.00%/8408 tok = Qwen 口径,跨口径声明)。token 计数:准入=:8003 CPU Qwen tokenizer(冻结指纹 4806… 不变);记账=deepseek usage。

| 臂 | accuracy | answer tok/题 | shown | kept | degraded |
|---|---|---|---|---|---|
| D(filter) | 77.08% | 927(=B 锚 1/9.1) | 5.7 | 5.74 | 0/1540 |
| **E(+血缘 span)** | **79.94%** | **1249(=B 锚 1/6.7 ≈ 目标 ~1/6)** | 11.7 | 5.74 | 0/1540 |

**D→E = +2.86pp,p<0.0001,paired CI [+1.51,+4.21]pp —— 血缘原文机制显著有效**(枚举/多跳题从 span 卡回收原始证据)。E≈B 历史锚(79.94 vs 80.00,但跨口径:答题模型 flash≠Qwen;flash 口径自身基线漂移 82-85%,严格配对需花 B 的钱重测,留维护者决策)。SC-002 PASS(D=11.0% of B tokens);SC-001/004/005/007 HOLD(B 不答题/cat5 未跑/degraded pass 未跑/filter p95 23.1s 本地排队口径,均预期内可解释)。

运维快照:decider 全量 40k shard 请求 errors=0;吞吐 ~240 shard/min(compile+修桶 1.94×);全程 2.1h(02:30-05:00)。**backlog**:摘录瘦身收益重估为 ~1.2×(真实 payload 98% 落 512-1024 桶);下轮 p50 优化=filter 闸 4→2。**token 维度达成(E≈1/6.7 B 锚);分数维度 91.43 未达**(E 79.94,机制有效但水位不足,后续:θ/枚举规则/3-rep 口径)。
