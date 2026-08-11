---
title: top-k 探索：全局检索预算 vs 思考模式（Qwen + DeepSeek judge）
summary: 全量 sweep 找到 top-k 150 = 3-rep 多数票 90.13%（+1.62pp）/ 均值 89.8%（+1.57pp），跨 90% 目标线；代价是 2.4× 上下文税，加量型涨点，出货决策留给维护者。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-11
tags: [evaluation, locomo, top-k, retrieval, thinking]
---

# top-k 探索：全局检索预算 vs 思考模式

## 背景

032-think3 证明 Qwen 思考模式下 top-k 30 = 88.23%（3-rep 均值）。本实验问：思考模式下加大全局 top-k 是否继续涨分？历史 cat-top-k 只对 multi-hop（1=150）扩预算（+0.9pp，加量税 2.4×），从未测过全局 top-k >30 在思考模式下的表现。

## 方法

- **栈**：032-store（bge-large 1024d）+ Qwen3.6-35B-A3B-FP8（vllm，`max-model-len 32768`，thinking 开 `LOCOMO_NO_THINKING=0`）+ deepseek-v4-flash judge（mem0-aligned）+ `--force-answer` + `--chunks --chunk-quota 12 --retrieval hybrid` + `--trace-mediation=false`（legacy 路径，与 032-think3 同 recipe）。
- **Sweep**：400 题分层随机子集（seed=20260811，各点同子集=配对），top-k ∈ {30, 60, 100, 150}，1-rep。
- **确认**：最优 top-k 150 跑全量 1540 × 3-rep。

## 结果

### Sweep（400 题子集）

| top-k | acc | ctx token | vs 30 |
|---|---:|---:|---:|
| 30 | 86.50% | 3557 | — |
| 60 | 87.75% | 4779 | +1.25 |
| 100 | 88.25% | 6376 | +1.75 |
| **150** | **89.25%** | 8606 | **+2.75** |

单调上升，McNemar 全正（30→60 +5、60→100 +2、100→150 +4）。

### 全量 1540 × 3-rep @ top-k 150

| 口径 | top-k 30 锚点 (032-think3) | top-k 150 | Δ |
|---|---:|---:|---:|
| 3-rep 均值 | 88.23% | **89.8%** [89.4, 90.3] | +1.57pp |
| 3-rep 多数票 | 88.51% | **90.13%** (1388/1540) | +1.62pp |

- 单次 rep：89.74% / 89.74% / 90.06%（跨 rep 极稳定，全高于锚点的单次 88.6/88.4/87.6）。
- 类别（多数票）：multi-hop 95.0%、single-hop 91.9%、temporal 87.9%、open-domain 67.7%。
- answer_context_tokens_mean = 8547（top-k 30 锚点 ~3600，**2.4× 税**）。

## 诚实边界

- **加量型涨点**：主要靠 2.4× 上下文预算（8547 vs ~3600 token），非预算下提质。按 maintainer 杠杆哲学，出货决策留给维护者（产品集成方无无限 context 预算）。
- **需 32768 上下文 answerer**：top-k 150 输入 ~8500-8900 token + max-tokens 8000 > 16384，必须 `--max-model-len 32768`。这是部署硬约束。
- **embed 512-cap 降级**：box 上 bge-large embed 服务 `max-model-len 512`，长文本（>512 token）嵌入被 vllm 拒 → semantic 降级（恒定偏置，各 top-k 点同受影响）。harness embedding.Client 无客户端截断。
- **均值 vs 多数票口径**：锚点 88.23% 是均值；多数票口径下 88.51% → 90.13%（同样 +1.6pp）。两种口径结论一致。
- 单次 judge 噪声 ±1pp（deepseek temp=0 仍不确定），但 3-rep 稳定支撑 +1.6pp。

## 结论

top-k 150 是思考模式下稳定可复现的 **+~1.6pp** 涨点（多数票口径 90.13% 跨过 90% 目标线）。代价是 2.4× 上下文税 + 32768 部署约束。次优 top-k 100（+1.75pp on 子集，上下文 6376，1.8× 税）是预算敏感者的折中。

**是否转正/设默认：维护者决策**——这是加量型杠杆（与 cat-top-k 同类），按既有哲学（预算下提质）倾向不设默认，但可作为高预算部署的旋钮。

## 复现入口

- box 数据盘：`/root/autodl-tmp/topk-sweep/`（子集 + 4 点 1-rep）、`/root/autodl-tmp/topk-full/tk150-full{1rep,3}`（全量单次 + 3-rep）、`eval-backup-20260811-144859/`。
- env：`/root/autodl-tmp/topk-run.env`（LOCOMO/EMBED/JUDGE，0o600，judge=deepseek-v4-flash）。
- 分析脚本：`/root/autodl-tmp/topk-sweep/analyze-sweep.py`（overall + 类别 + 配对 McNemar）。
