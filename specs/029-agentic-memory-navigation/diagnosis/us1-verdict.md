# US1 Verdict: 多步导航救回空间（Zero-Cost 诊断）

**Feature**: 029-agentic-memory-navigation · **日期**: 2026-08-06 · **子集**: 84 题（temporal 59 + multi-hop 25）

## 结论

**GO — 进 US2。** `rescueable_share = 65.5%`（55/84）远超 20% 门禁。

## 方法（零成本，无 answerer/judge）

1. **检索诊断产出**：`locomo-bench --nav-diagnose`（Go harness，复用现有混合检索 + embedding 8010）对 84 题逐题输出：gold turns、in_pool（全对话 oracle，chunk 真实存在于 store）、单次 top-30 gold rank、wide-pool rank、确定性模拟动作的检索结果。
2. **分类**：`tools/nav_diagnose.py` 三分类 + 归因。
3. **模拟动作（确定性，诚实——不偷看 gold）**：
   - `rewrite`（换查询）：question 的内容词/显著词/时间词/引号短语变体
   - `follow_entity`（跟线索）：已见 top-30 证据里的引号短语/Title-case 实体
   - `deep`（换粒度）：wide-pool（top-300）gold rank

## 三分类计数（84 题）

| 类别 | 计数 | 占比 |
|---|---|---|
| topk_hit（单次 top-30 已捞到） | 29 | 34.5% |
| **rescueable（多步可救）** | **55** | **65.5%** |
| not_in_pool | 0 | 0% |
| gold_unresolved | 0 | 0% |

全部 84 题 gold 在池且可解析——**not_in_pool = 0 确认召回缺口纯在排序/查询质量，非表示缺失**。

## 归因分布（rescueable 55 题）

| 机制 | 计数 | 占比 |
|---|---|---|
| rewrite（换查询） | 42 | 76% |
| deep（换粒度/深挖） | 12 | 22% |
| follow_entity（跟线索） | 1 | 2% |

**主机制 = 换查询**：单次 top-30 用完整问题检索，被噪声稀释；聚焦查询（关键实体/书名/时间词）命中深池。wide_rank 中位约 52–138，与 023 residual 的「gold 中位 rank 71–90」深层召回不足诊断一致。

## 抽样人审（真实场景示例）

| question | single_rank | wide_rank | 救回机制 |
|---|---|---|---|
| When did Melanie read the book "nothing is impossible"? | -1 | 52 | rewrite（书名短语） |
| When did Maria go to the beach? | -1 | 138 | rewrite（实体/时间） |
| When did Caroline go to a pride parade during the summer? | -1 | 80 | rewrite |
| When did Melanie go camping in June? | -1 | 73 | rewrite |

全部为 temporal 问题，单次 top-30 漏、gold 沉在深池、聚焦查询可救——符合多步导航「推理救回证据」的预期机制。

## 诚实性说明

- 模拟是启发式（单词/短语变体），代表**多步导航救回空间的下界近似**——真实 LLM 导航的改写质量可能更高（US2 实测）。
- rewrite 主导部分反映「检索对长查询弱响应，聚焦查询强」——这是**查询表示**问题，正是导航工具 `expand_query` 的机制。
- US1 判定救回空间存在且大；**是否端到端转化由 US2 配对决定（008 铁律）**。

## 判定

`rescueable_share = 0.655 ≥ 0.20` → **GO 进 US2**（84 题配对：基线单次检索 vs `--nav` 多步导航）。
