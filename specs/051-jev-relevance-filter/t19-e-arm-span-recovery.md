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

*收数后本文件补 §6 数字(B/D/E accuracy、D→E p、E token 比、flips 归因)。*
