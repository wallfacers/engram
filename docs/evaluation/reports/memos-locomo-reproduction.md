---
title: MemOS LoCoMo 同栈复现报告
summary: 本文保留 MemOS 在 engram 同栈上的 LoCoMo 复现方法、结果与限制；不将该单次对照外推为通用机制排名。
status: stable
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [memos-locomo-reproduction]
tags: [evaluation, memos, reproduction, report]
---

# MemOS LoCoMo 同栈复现报告

本报告保存 2026-07-26 的同栈复现证据。当前分数矩阵见[当前评测结果](../results.md)，竞品解释规则见[竞品与基准口径](../competitors.md)。

## 复现设置

MemOS 使用其自家代码与 LoCoMo cat 1–4（1540 题），答题模型固定为 Qwen3.6-35B-A3B-FP8，embedding 固定 bge-large-en-v1.5，judge 固定 deepseek-v4-flash 与相同 prompt。MemOS 继续使用其默认检索形态；因此这不是等上下文预算实验。

## 结果与可声明范围

在该栈下 MemOS 得到 82.40%，而 engram 同口径参考为 85.71%，差为 +3.31 个百分点。MemOS 公开 88.83 的数字不能与本结果直接混用；两者的 answerer 与 judge regime 不同。

该结论的限制同样重要：MemOS 侧只有一次答题运行，未做逐题配对 McNemar，且 engram 输入 answerer 的上下文预算更高。因而可声明“此受控栈下的点估计差”，不可声明记忆机制的通用优劣。

## 复现资产

可重建但代价高的逐题产物、脚本与 manifest 存放在受控私有评测资产位置。资产必须在使用前核对数据版本、模型 revision 和凭据清理状态；不得将私有数据、令牌或远端连接信息复制到本仓库。
