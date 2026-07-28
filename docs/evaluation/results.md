---
title: 当前评测结果
summary: 本文维护 engram 当前可引用的 LoCoMo 与 LongMemEval-S 结果及其复现口径；不把不同评测栈的数字当作可直接比较的系统排名。
status: active
audience: [users, maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-07-28
canonical_for: [evaluation-results]
tags: [evaluation, locomo, longmemeval, results]
---

# 当前评测结果

本文是当前分数的唯一正本。每条结果都由数据集、答题模型、判题模型和配方共同定义；查看实验取舍请转至[实验裁决](experiment-verdicts.md)，复现步骤请转至[评测运维](../operations/evaluation/locomo-runbook.md)。

## 读取规则

- 只比较四个轴中其余条件一致的行；跨 answerer、judge、检索预算或聚合方式的绝对分数不构成系统排名。
- `full 500` 指 LongMemEval-S（cleaned）的完整 500 题，而不是早期的小样本先导运行。
- `deepseek-v4-pro` answerer 的结果是公平探针，不是产品默认模型或默认配置。

## 当前结果矩阵

| 数据集 | 样本 | 答题模型 | 判题模型 | 配方与聚合 | 结果 | 解释边界 |
|---|---:|---|---|---|---:|---|
| LoCoMo（cat 1–4） | 1540 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | local hybrid、3 次答题多数 | 85.71% | 与同栈 MemOS 的可比基线 |
| LoCoMo（cat 1–4） | 1540 | deepseek-v4-pro | deepseek-v4-flash | canonical recipe、3 次答题多数 | 89.03% | 强 answerer 探针，不能与本地基线混作默认分 |
| LongMemEval-S（cleaned） | 500 | Qwen3.6-35B-A3B-FP8 | deepseek-v4-flash | local-first、3 次答题多数 | 80.80% | 当前 full 500 本地栈结果 |
| LongMemEval-S（cleaned） | 500 | deepseek-v4-pro | deepseek-v4-flash | 与本地臂相同检索、3 次答题多数 | 86.00% | 相对本地 answerer +5.20pp；McNemar p=0.0049 |

## 同栈竞品对照

唯一已完成的同栈对照固定 Qwen3.6-35B-A3B-FP8 answerer、bge-large-en-v1.5 embedding、同一 judge prompt 与同一 judge：LoCoMo 上 engram 为 85.71%，MemOS 为 82.40%。更换为 deepseek-v4-pro judge 后，engram 为 83.77%，MemOS 为 80.26%。

这说明在该受控栈中 engram 相对 MemOS 的差为 +3.31 至 +3.51 个百分点；它不证明通用的“记忆机制领先”，因为两侧仍使用各自的默认检索预算与上下文预算。厂商自报榜单仅供[竞品口径](competitors.md)追溯，不能横向相减。

## 结果维护要求

更新一行结果时必须同时登记数据集版本与样本数、answerer、judge、完整 recipe、聚合方式、日期和证据路径。早期小样本数字仅作为[历史先导](../archive/evaluation/longmemeval-100-pilot-2026-07.md)保存，不能作为当前结论引用。
