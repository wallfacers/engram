# US2 Verdict: 推理驱动多步导航（配对）

**Feature**: 029-agentic-memory-navigation · **日期**: 2026-08-06 · **子集**: 84 题（temporal 59 + multi-hop 25）× 3 reps majority

## 结论

**NO-GO（008 铁律）。** 多步导航 majority **29.8% < 单次基线 47.6%**（−17.9pp，McNemar p=0.0059 显著负）。temporal −20pp / multi-hop −12pp 均崩。US3（结构化导航）/ US4（RL 导航）不执行。

## 配对设计（与 027/028 同一子集纪律）

同 store（`009-bge-chunks-store`）/ 子集（84 题）/ answerer（Qwen3.6-35B-A3B-FP8 @8000）/ judge（mem0-aligned DeepSeek）/ 预算（answer-context cap 3600 tokens）。基线 = 现有单次检索路径；导航臂 = `--nav`（4 步上限，每步工具调用 JSON 决策）。

## 4 次机制归因重跑（全部显著负）

| 变体 | 修复点 | nav | Δ | p |
|---|---|---|---|---|
| 首测 | — | 25.0% | −22.6pp | 0.0026 |
| enable_thinking | vllm 禁用推理（0.6s/步纯 JSON） | 32.9% | −16.7pp | — |
| minEvidence 补足 | 证据数补到 ≥12 | 34.5% | −13.1pp | 0.043 |
| chunk-first 组装 | chunk 优先排序 | 29.8% | −17.9pp | 0.0059 |

**基线**：47.6%（单次检索，temporal 51% / multi-hop 40%）。

## 根因归因

1. **store 检索以短 fact 为主，chunk 需 `chunk-quota` 机制强制保底**。
   裸 `Retriever.Search` 的 top-N 结果 fact 主导（导航 final evidence 平均 500 tokens，chunk 占比仅 1%，基线 3654 tokens 来自 `chunk-quota=12` 强制 chunk slots）。导航组装没有 quota → answerer 上下文永远劣化。这是 4 次变体全部显著负的**结构性原因**。
2. **模型导航策略未转化 US1 理论空间**。
   73% 的题模型不 `stop`（4 步 search 到上限 fallback）；模型自主改写查询命中 gold 的能力远低于 US1 确定性模拟（单词/短语变体）。**「换查询可救」的空间需要 oracle 式改写，35B 自主导航达不到**。
3. US1 的 `rewrite 76% 归因` 在真实导航下不成立：模型写的自然语言查询对 FTS trigram 关键词检索反而不利。

## 与先前证伪线的衔接

- 写侧 event 替换原文：027/028/028-US2 三次证伪（时间锚定/蒸馏）。
- 检索侧单次表示/排序：021/013/014 等六次证伪。
- **029：检索侧多步导航（推理介入检索）首次实测证伪**——不是「排序不好」或「表示缺失」，而是「推理驱动检索本身在当前 stack 负收益」。
- 有价值发现：US1 零成本诊断（确定性模拟）**高估**了真实导航模型的救回能力。导航的推理环节（35B 自主决策）是负资产而非救星。

## 判定

008 铁律（多步 ≥ 单次，同预算）**不满足** → NO-GO。US3/US4 按门禁不执行。导航代码保留为评测 harness 基础设施（工具解析/轨迹/预算记账），关闭默认（`--nav` 默认 false，SC-004 零行为变化）。
