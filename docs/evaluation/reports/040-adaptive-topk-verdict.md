---
title: 040 自适应检索深度 verdict —— gap-knee 截断 NO-GO，转向 confidence-gated iterative retrieval
summary: 用 top-k 30 vs 150 全量 3-rep 多数票逐题对比，把 30→150 的 +1.62pp 增量（56 题）拆成 42 上下文量 + 11 召回 + 3 短答案；gap-knee 只能救「召回」（r 上限 20%，远低于保 90pp 需 45% 的门槛），故自适应截断 NO-GO。但「自信 vs 犹豫」分析发现 89% 的增量题在 30 下是犹豫的（仅 7% 自信地错），证明 confidence-gated iterative retrieval 的触发信号存在，理论上限 91.75%（>90.13%）且省 4.8× 预算——这是下一步 041 的基石。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [evaluation, locomo, top-k, adaptive, iterative, confidence-gated]
---

# 040 自适应检索深度 verdict

## 一句话结论

**gap-knee 自适应截断 NO-GO**：30→150 的 +1.62pp 增量里 80% 是「上下文量」（gold 已召回但 answerer 需要更多上下文推理），gap-knee 只能救「召回」（r 上限 20%），远低于保 90pp 需 45% 的门槛。**但诊断发现 89% 的增量题在 30 下是「犹豫」而非「自信地错」，证明「读到足够就停」的 confidence-gated iterative retrieval 有救（理论上限 91.75%），是下一步方向。**

## 诊断方法（零模型成本，复用历史 run）

- 数据源：top-k 30 全量 3-rep（`032-think3/keep/run-{1,2,3}`）与 top-k 150 全量 3-rep（`topk-full/tk150-full3/run-{1,2,3}`）的逐题 `results-hybrid.jsonl`（`correct/gold/predicted` 字段）。
- 口径：3-rep 多数票。top-k 30 = 1363 对（88.51%），top-k 150 = 1388 对（90.13%）。
- 增量题 = 「30 错、150 对」= **56 题**（另有 31 题「150 反害」= 噪声，净 +25 题 = +1.62pp）。

## 关键数据

### 56 题增量拆解（错因分类）

| 错因 | 数量 | 判定依据（gold 关键词是否出现在 30 下 predicted） | gap-knee 能否救 |
|---|---|---|---|
| 上下文量问题 | 42（79%） | gold 已提及（在 top-30 内）但 answerer 答错 | ❌ 救不了（分数拐点无法识别） |
| 召回问题 | 11（21%） | gold 未提及（不在 top-30 内） | ✅ 能救（靠分数拐点识别） |
| 短答案（数字/Yes/No） | 3 | 匹配不可靠 | 未知 |

### gap-knee 判决

- 敏感性分析：保 90pp 需「识别召回题」召回率 **r ≥ 45%**。
- 实测 r 上限 = 11/56 = **20%**（乐观算上短答案 25%），仍低于门槛。
- 即使 gap-knee 完美，分数 = 1332 + 11 + 31 = 1374 = 89.2%，掉 0.9pp。**NO-GO。**

## 转折：confidence-gated iterative retrieval 有救

「自信 vs 犹豫」分析（56 题在 top-k 30 下的 predicted 文本，找 "not sure"/"no information"/猜测语气等信号）：

| 分组 | 强犹豫 | 弱犹豫 | 自信地错 |
|---|---|---|---|
| 上下文量（42 题） | 23（55%） | 16（38%） | **仅 3（7%）** |
| 召回（11 题） | 10（91%） | 1（9%） | 0 |

**结论：answerer 信息不足时，93% 会表达犹豫，只有 7% 自信地错**（如 `gold=Weight problem` 却硬答 `gastritis` 且自我肯定）。这否定了「信息不足时自信地错、不触发再读」的担心（029 也栽在此），证明「犹豫→再读→答对」的触发信号存在且可靠。

- **理论上限**：1332（都对）+ 31（30 下自信答对，避开 150 噪声）+ 50（犹豫→再读→答对）= **1413 = 91.75%**（比 90.13% 高 1.6pp），平均 top-k ~31（省 4.8×）。
- **三个待解难点**（对应 029 教训）：①「犹豫」的运行时低成本提取（流式检测 / logits 置信度 / prompt 显式 MORE_INFO）；②再读深度策略（跳 150 最稳 vs 渐进 60→150）；③迭代成本（判断犹豫必须比「多喂 120 条输入」便宜，否则省了个寂寞）。

## 对「缩减 top-k」的最终定性

- 「缩减 top-k 必掉分」**部分成立**：一刀切减 k 掉分（budget-ablation、top-k sweep、以及本 verdict 的 gap-knee 都是证据）。
- 但**不是「不能省」，是「不能一刀切省」**：要按 answerer 的置信度省——自信就停，犹豫就加深。这是 041（iterative retrieval）的路线。

## 资产

- gap-knee 算法 + 单测（`cmd/locomo-bench/adaptive_topk.go` / `adaptive_topk_test.go`）：已实现、测试通过，但 US2 不落地，保留为可复用资产。
- 诊断脚本（远程只读对比 + 错因分类 + 自信/犹豫分析）：未入库，逻辑见本文件「诊断方法」。
- 数据源：`/root/autodl-tmp/032-think3`、`/root/autodl-tmp/topk-full/tk150-full3`（AutoDL box）。
