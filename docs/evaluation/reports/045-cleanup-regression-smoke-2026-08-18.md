---
title: 045 清理回归烟测 — master f6f8366 端到端健康确认
summary: 045 NO-GO 代码清理(删 2687 行)后,最新 master 在远端 box 上 qwen38 栈跑 conv-0 全量 152 题烟测:OVERALL 86.8%(exit=0),与历史锚点同区间,判定无回归。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-18
canonical_for: [045-cleanup-regression-smoke]
tags: [evaluation, locomo, regression, cleanup, smoke]
---

# 045 清理回归烟测(2026-08-18)

## 一句话结论

045 NO-GO 关闭后,`submodular_packing/aic/reverify_042` 共 2,687 行代码被清理删除并合入
master(`f6f8366`,见 [045 verdict](045-submodular-packing-verdict-2026-08-17.md))。清理对
引擎零触碰(`git diff 415a1c6..f6f8366 -- memory embedding provider store internal` 为空)。
本次在远端 box(qwen38 栈)对最新 master 做 conv-0 端到端烟测:exit=0、复用 032-store、
OVERALL **86.8%**(132/152),与历史锚点 88.2% 落在同一区间 → **清理无回归**。

## 配置

- 二进制:本地 `CGO_ENABLED=0 go build`(`vcs.revision=f6f8366, modified=false`)。
- answerer/extract:`Qwen/Qwen3.8-27B`(vllm, thinking on);embed:`BAAI/bge-large-en-v1.5`;
  judge:`deepseek-v4-flash`(032-run.env)。
- 配方:`--chunks --retrieval hybrid+unified --top-k 30 --chunk-quota 12
  --judge-mem0-aligned --no-idk-retry --concurrency 32 --repeats 1 --conversations 1`。
- store:复用 `/root/autodl-tmp/032-store`(提取复用,facts=213,无 extraction 重付)。
- 数据集:LoCoMo 1,540 可答题中的 conv-0(152 题)。

## 结果与锚点对比

| 维度 | **最新 master(8-18)** | 旧 smoke(8-17, 042-bin) | 差 |
|---|---:|---:|---:|
| OVERALL | **86.8%** (132/152) | 88.2% (134/152) | −1.4pp |
| multi-hop | 81.2% | 84.4% | −3.2pp |
| temporal | 86.5% | 91.9% | −5.4pp |
| open-domain | 92.3% | 92.3% | 0 |
| single-hop | 88.6% | 87.1% | +1.5pp |

耗时 9.6 分钟(22:55:44 → 23:05:21),exit=0。

同日 box 上另有 Qwen3.8-27B 10-conv 3-rep 全量跑(非本次验证,记录备查):OVERALL 87.6%。

## 判定

- **端到端跑通**:清理后 locomo-bench 无运行时残留,exit=0。
- **分数同区间**:−1.4pp(2/152 题)属单次 1-rep 烟测正常波动(非配对、跨时点 judge
  漂移);结合引擎零触碰 + 本地全量 `go test ./...` 绿,按 Constitution IV 判定
  invariant by construction,无需全量锚定。
- 附注:run 脚本中原 `--trace-mediation` flag 已被 044 清理移除,新版烟测配方去掉该参数。
