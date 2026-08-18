---
title: k30 chunk-quota 28 — top-k 30 预算内的 90pp 冲线(已收线)
date: 2026-08-18
tags: [evaluation, locomo, retrieval, chunk-quota, k30]
status: 收线定格 clean 89.74%(+4 vs 锚 89.48%);90pp 冲线目标取消(2026-08-18 维护者决定:box 实例 GPU 已释放不可重启、flash 全量太贵)
---

# k30 chunk-quota 28 — 同预算槽位重分配

## 背景与假设

GOAL:LoCoMo 1540 · top-k 30 · 无类别特化/无数据集定制/无 paid 服务 ≥90.00%(1386/1540)。
起点:Qwen3.8-27B k30 quota12 clean 3-rep majority **89.48%**(1378/1540),72% 错题是 k30 检索截断。

假设:gold 几乎全由 verbatim chunk 承载(199/200),而 RRF 信号偏 fact(chunks 无实体、
嵌入弥散,无 quota 时仅占 top-k 0–6%——chunks.go 设计注释自证),**quota=12 是从未被
sweep 验证的保守值**:top-30 的 30 条预算内,facts 一直在挤占 gold chunks。提高 quota 是
同条数预算下的内容重分配,不是扩池(区别于 k150 的 5× 检索面)。

## 方法(零 API 成本 sweep)

- harness 新增 `--wide-dump N`(attribution 模式):每题记录宽池前 N 条 fact/chunk 交错序
  + gold 标注(retrieval-only,零 LLM 调用;commit e22b52e)。
- 本地复现 canonical 店:HF `wallfacers/engram-locomo-artifacts` 的 `009-bge-chunks-store`
  (=032-store 正本,所有出货 verdict 复用)+ fastembed bge-large FP32 sidecar。
- `sweep_quota.py` 离线重放 `applyChunkQuota`,自洽校验:quota=12 重放 vs 记录的 narrow
  top-30 **0/1540 错配**。

## Sweep 结果(本地序)

| quota | gold@30 | 错题救回 | 对题翻车 | net |
|---:|---:|---:|---:|---:|
| 12(现状) | 1373 | — | — | — |
| 18 | 1421 | 21 | 4 | +17 |
| 24 | 1437 | 30 | 6 | +24 |
| **28** | **1452** | **37** | **5** | **+32** |
| 30 | 1451 | 38 | 10 | +28 |

与 k150 救回(25 题)仅重叠 6/38 — 槽位重分配与扩池正交。

已知局限:本地 fastembed 查询向量 vs box vllm 存储向量跨实现漂移(本地 dump 错题归因
97 上下文内 / 37 截断,box 口径 45/80),且 HF 快照缺部分嵌入(box store 已 backfill)——
绝对数不可对标,方向(高位 quota 正效应)结构性成立,幅度以 box 实测为准。

## Box 实测(2026-08-18,单变量配对)

配方:与 k30 quota12 锚唯一差异 `--chunk-quota 28`(unified ff400d0e、force_answer=false、
no-idk-retry、mem0-aligned flash 在线判、3-rep;store=032-store;Qwen3.8-27B vllm 32768)。

| 口径 | quota28 | 锚(quota12) | Δ |
|---|---:|---:|---:|
| 在线 3-rep mean | 88.17%(88.2/88.2/88.1) | 87.62% | +0.55pp |
| **clean 3-rep majority** | **1382/1540 = 89.74%** | 1378 = 89.48% | **+4 题** |
| raw 3-rep majority | 1388 = 90.13% | — | — |

类别(clean):single-hop 92.6% / temporal 91.0% / multi-hop 87.9% / open-domain 65.6%。
上下文成本:`answer_context_tokens_mean` 3614 → **6957 tok(≈1.9×)**——chunk 满槽使单条
预算内容变长,严格说是"条数不变、token 变重",哲学上介于重分配与加量之间,如实记录。

### 逐题对账(clean majority)

救回 46 / 翻车 42 / 净 +4。检索杠杆真实存在(46 题 gold 新进上下文后答对),但被对冲
(42 题失去原 top-30 内容后翻错)——与 040 的 k150 非单调(56 救/31 害)同构:top-k
组成不是单调好。本地 sweep 预估(37 救/5 失)对 box 序大幅打折(46/42),归因:跨实现
序漂移 + answerer 对 28-chunk 上下文的稀释敏感。

### stream 失败污染(可修复缺口)

quota28 上下文更长 → thinking 更长 → **47 次 answer SSE 超时**(`context deadline
exceeded`,harness 判错),影响 33 题:11 题 ≥2 rep 失败(majority 必错),其中 **6 题
quota12 锚判对 = 纯 run 质量损失**。1382 + 6 = 1388 = **90.13%**。

补跑协议(数据完整性修复,非刷分):33 题全新 3-rep 同配方(`--only-questions` 子集),
替换被污染题的 majority,其余 1507 题原判定不动。名单 `q28-failed-questions.txt`,
脚本 `run-q28-retry.sh` + timeout 干净重跑 `run-q28-clean-rerun.sh`(`--per-call-timeout`
flag 已进 harness,6b160e5)。**A/B 计划均未执行,已作废**:box 实例 GPU 释放后不可
重启(凭证路径不存在),flash 全量被维护者否决(太贵)。若未来重新租卡,补跑 20min
或 t15 干净重跑 3.4h 的脚本与 binary 均已备齐,直接可用。

## 中间结论

1. chunk-quota 高位带在 box 上方向成立(+0.55pp 在线 / +4 clean),但幅度远小于本地 sweep
   预估——quota 单杠杆不足以独立跨 90pp。
2. 失败 rep 补跑后预期 90.1–90.3%(1388–1391);若不足,下一杠杆须解决"翻车 42 题"
   (更细的 quota 点位或 chunk 排序,均未试)。
3. 无任何类别/数据集特化;quota 是全局检索参数;无 paid 服务。

## 附:answerer 互补性发现(flash smoke,98 题分层,2026-08-18)

等 box 凭证期间用本地栈(HF 009 store 副本 + fastembed bge-large sidecar + DeepSeek
flash API)对 quota28 做了 answerer 替换探查(answerer=deepseek-v4-flash 禁思考,
k30/quota28/unified 同配方,在线 judge):

- **对题保持 50/50 = 100%(零翻车)**;Qwen3.8(思考)错题救回 **16/48 = 33.3%**
- 全量外推 ~93%(1-rep 在线口径,未验证 3-rep+clean)
- 机制假设:思考模型在 28-chunk 长上下文上"想太多"引入自我怀疑/拒答倾向,
  禁思考模型直接抽取反而更准——与 unified 契约×k150 的 idk 观察同族
  (上下文越长,思考的边际伤害越大)
- flash 全量 3-rep run 启动后被维护者中止(花费授权确认中,~10% 进度,未产生数据)

无论后续走哪条路线,这个互补性本身是可复用的结论:**LoCoMo k30 高位 quota 配方下,
answerer 选型存在"思考 vs 抽取"的权衡,不是单调"更强模型更好"**。

## 产物

- box: `/root/autodl-tmp/046-qwen38-runs/locomo-k30-q28-qwen38-3rep`(备份 `eval-backup-20260818-160838`),已关机
- 本地: `/tmp/qwen38-eval/results-q28/`(3×1540)、`rejudge-out/locomo-q28-*`、`dump-k30/trace.jsonl`、`sweep_quota.py`
