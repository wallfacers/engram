---
title: engram vs MemOS 上下文预算剥离
summary: 固定 store/embed/judge，扫 answerer 上下文预算对齐 MemOS 的 ~1059 tok，分离 engram 同栈领先中"预算"与"记忆机制"的贡献；结论是领先完全由预算驱动。
status: stable
audience: [maintainers, researchers]
owner: engram-maintainers
last_reviewed: 2026-07-29
canonical_for: [budget-ablation]
tags: [evaluation, locomo, ablation, fairness]
---

# engram vs MemOS 上下文预算剥离

## 背景与动机

[同栈复现报告](memos-locomo-reproduction.md) 的逐题配对 McNemar 给出：固定 v4-flash judge 栈下 engram 总体领先 **+3.20pp**（1529 配对，exact p=0.002895），主要由 single-hop 驱动。但该报告同时列出一条硬边界——engram answerer 上下文约 **3614 token/次**（Qwen vLLM tokenizer，`009-full-A-base` 实测均值），MemOS 约 **1059 token/次**（tiktoken 离线估算，MemOS 探针未记 usage），即 enggram 给答题器喂了 **~3.4 倍** 的上下文。因此 +3.20pp 可能部分或全部来自更大的上下文预算，而非记忆机制本身。本实验剥离该预算变量。

## 方法

- **固定轴**（与 009 / MemOS 完全同口径）：`009-bge-chunks-store`（bge-large-en-v1.5 1024d）+ Qwen3.6-35B-A3B-FP8 answerer（本地 vLLM）+ deepseek-v4-flash judge + mem0-aligned judge prompt + `--force-answer` + 三次答题多数票 + 1529 配对 McNemar（折叠 11 组重复 `(conv, question)`）。
- **唯一变量**：answerer 上下文预算，通过 `--top-k`（并按比例设 `--chunk-quota`）收窄送入答题器的 memory 条数。扫四个点：top-k 30 / 18 / 9 / 7。
- **对齐目标**：MemOS 的 ~1059 tok。实测 top-k=7 的 `answer_context_tokens_mean` = **1083 tok**，与 MemOS 1059 量级对齐（见边界：tokenizer 不同）。
- **联合效应说明**：`--top-k` 同时收窄 retrieval 候选池与 answerer 输入，因此准确率下降是"少预算 + 少召回"的联合效应，无法在当前 harness 内完全分离二者。但 MemOS 同样在 ~1059 tok 的少预算下运行其 tree/graph 检索，故"同预算下谁更强"的对照仍然公平。

## 结果

answerer 预算（token/次）与 enggram − MemOS 配对差（1529 配对，双侧 exact McNemar）：

| 点 | answerer 预算 | engram | MemOS | Δ | b | c | exact p |
|---|---:|---:|---:|---:|---:|---:|---:|
| top-k=30（009 基线）| 3614 | 85.68% | 82.47% | **+3.20pp** | 155 | 106 | 0.002895 |
| top-k=18 | 2239 | 82.93% | 82.47% | +0.46pp | 147 | 140 | 0.723280 |
| top-k=9 | 1339 | 79.27% | 82.47% | −3.20pp | 149 | 198 | 0.009872 |
| top-k=7（≈ MemOS 1059）| 1083 | 76.85% | 82.47% | **−5.62pp** | 136 | 222 | 0.000006 |

分类别差值（Δ = engram − MemOS，exact p 在括号内，显著 <0.05 加粗）：

| 类别 | top-k=30 | top-k=18 | top-k=9 | top-k=7 |
|---|---:|---:|---:|---:|
| single-hop | +6.02pp（**0.000014**）| +3.37pp（**0.023**）| +0.12pp（1.0）| −3.37pp（0.051）|
| multi-hop | −1.77pp（0.500）| −5.32pp（**0.040**）| −9.57pp（**0.0003**）| −10.99pp（**0.000022**）|
| temporal | −0.62pp（0.905）| −3.12pp（0.308）| −9.03pp（**0.0028**）| −9.35pp（**0.0018**）|
| open-domain | +6.25pp（0.327）| +4.17pp（0.503）| +6.25pp（0.308）| +3.12pp（0.690）|

## 结论

1. **engram 的 +3.20pp 同栈领先完全由上下文预算驱动，不是记忆机制优势。** 预算从 3614 降到 MemOS 级别（1083 ≈ 1059），领先单调消失并反转为**极显著落后**（−5.62pp，exact p=0.000006）。
2. **持平交叉点约 2240 token**：engram 需要约 **2.1 倍** MemOS 的上下文预算才能追平，约 **3.4 倍**（3614）才能取得 +3.20pp 的显著领先。
3. **同预算下 MemOS 的 tree/graph 检索显著优于 enggram 扁平检索**，且差距随预算收紧而扩大——multi-hop 在 top-k=7 达 −10.99pp（p=0.000022），temporal 达 −9.35pp（p=0.0018）。single-hop 是 enggram 唯一在高预算下的优势来源，预算一降即消失。
4. open-domain 对预算最不敏感：engram 在全部四个预算点都小幅领先 MemOS（+3 到 +6pp），但均不显著（样本仅 96）。

这一结果**不推翻** [同栈复现报告](memos-locomo-reproduction.md) 的"+3.20pp 是固定 v4-flash 栈下的点估计差"——它精确化了该差值的来源：**预算，而非机制**。因此此前的"+3.20pp（p=0.002895）领先"不得解读为"engram 记忆机制优于 MemOS"；它是"engram 在 3.4 倍上下文预算下的答题优势"。

## 诚实边界

- **tokenizer 不严格等价**：engram 的 token 由 Qwen vLLM tokenizer 报告，MemOS 的 1059 由 tiktoken 离线估算。两者量级对齐（1083 vs 1059），但非严格等价；精确对齐需统一 tokenizer 重算两端 prompt。
- **top-k 的联合效应**：降 top-k 同时收窄候选池与答题器输入，无法在当前 harness 内分离"少预算"与"少召回"的各自贡献。
- **MemOS 单次答题运行**：MemOS 侧没有 answer-run 误差带；engram 侧是三次多数票。
- **单一 store / 单一 judge**：仅 `009-bge-chunks-store` + deepseek-v4-flash judge，未跨 store/judge 重复。

## 复算入口

配对统计入口为 [`scripts/mcnemar.py`](../../../scripts/mcnemar.py)。各预算点的逐题产物（`results-hybrid.jsonl`）与 MemOS verdict（`memos_judged_detail.json`）从受控评测资产取得后：

```bash
# 对每个预算点（top-k 30/18/9/7）的 run-1/2/3 results-hybrid.jsonl
python3 scripts/mcnemar.py '<run-dir>/tk<N>-cq<M>/run-*/results-hybrid.jsonl' memos-parity/memos_judged_detail.json
```

recipe（固定 store/embed/judge，仅变 `--top-k`/`--chunk-quota`）见 [LoCoMo 评测运行手册](../../operations/evaluation/locomo-runbook.md)。`answer_context_tokens_mean` 记录在每个 run 的 `cost.json`。可重建但代价高的逐题产物与 manifest 存放在受控评测资产位置；不得将私有数据、令牌或远端连接信息复制到本仓库。
