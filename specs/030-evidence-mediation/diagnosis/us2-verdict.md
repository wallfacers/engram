# US2 Verdict: 引用链证据中介（配对）

**Feature**: 030-evidence-mediation · **日期**: 2026-08-06 · **子集**: 84 题（temporal 59 + multi-hop 25，**029 实际子集**）× 3 reps majority

## 结论

**机制方向 GO，但归因修正。** trace majority **50.0% > base majority 27.4%**（McNemar p=0.0017 显著，flips A→B=26 / B→A=7），控制实验排除 token-削减伪赢。**但装配器对照揭示：更简单的 chunk-first 装配（keep，无 trace）也显著优于 base（47.6%，p=0.0455），且 trace 相对 keep 不显著（p=0.152）**。→ 有效杠杆是**读侧证据精炼（chunk-first + 聚焦）**，trace 引用链 sidecar 无独立显著增量。US3 按门禁执行。

## 配对设计（008 铁律）

同 store（`/root/locomo-artifacts/009-bge-chunks-store`，bge-large 1024d）/ 子集（84 题）/ answerer（Qwen3.6-35B-A3B-FP8 @8000）/ judge（mem0-aligned DeepSeek v4-flash）/ cap 预算（3600 tokens）。基线 = 现有单次检索路径（`--chunks --retrieval hybrid --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned`）；中介臂 = 基线 + `--trace-mediation`（sidecar 生成 grounded trace → fail-closed gate → evidence 为答案上下文）。

## 子集纠偏（重要）

030 最初自抽的 84 题（temporal 前 59 + multi-hop 前 25）**与 029 实际子集不同且偏简单**（base 跑出 90.5%，天花板效应）。**改用 029 实际 84 题**（从 `specs/029/diagnosis/diagnosis-report.json` per_question 提取 → `specs/030/diagnosis/phase0-ids-029-84.txt`，跨 9 conv）。注意：本 verdict 的基线是**配对内 base（27.4%）**，非 029 记录的 47.6%（029 US2 子集文件已删、不可复现；配对纪律只看 base vs trace 同子集）。

## 结果

| Arm | OVERALL (J) | mean±ci | multi-hop | temporal | context tok |
|---|---|---|---|---|---|
| base（chunk-quota 12） | 27.4% | 29.8% [23.8,35.7] | 28.0% | 27.1% | 3600-4065 |
| **trace（--trace-mediation）** | **50.0%** | **55.6% [34.0,77.2]** | 52.4% | 56.6% | ~250-725 |

`--compare`：n=3/3，flips A→B=26 B→A=7，McNemar **p=0.0017**，verdict=**above-noise**。

**类别不回归（L0-3）**：temporal 净 +14（A→B=19 / B→A=5），multi-hop 净 +5（A→B=7 / B→A=2）——两类别均正向翻转，无类别崩。

## Gate 状态分布（248 记录）

- **valid 219（88.3%）**——sidecar trace 绝大多数被 fail-closed 门接受
- parse_failed 28（11.3%，含重试后仍失败），fallback 1
- evidence_count 分布：valid+1=215（89%）、valid+2=3、valid+3=1——**trace 平均只选 1 条精选 evidence**

## 控制实验（排除 token-削减伪赢，008 铁律同预算纪律）

trace 上下文仅 ~250-725 tok，base 塞满 3600-4065 tok。跑 **base-slim 控制臂**（`--top-k 5 --chunk-quota 1`，上下文 ~688 tok，复现 trace 预算量级）：

| Arm | 正确率 | context tok |
|---|---|---|
| base 满 | 27.4% | 3600-4065 |
| **base-slim（同 trace 预算）** | **27.4%** | ~688 |
| trace | **50.0%** | ~250-725 |

**base 削减 token 不改变正确率（27.4% → 27.4%）；trace 在更小预算下 50%。** → trace 的赢来自**证据选择**（sidecar 精选 1 条最相关 evidence），非 token 削减。

## 装配器对照（关键修正，008 铁律归因纪律）

跑完 US3 keep 臂（`--evidence-assembly`，chunk-first 装配 + cap 内排序）后发现**更简单的机制已捕获主要增益**：

| 配对 | OVERALL | flips | McNemar p | 判定 |
|---|---|---|---|---|
| base（legacy 12 chunk） | 27.4% | — | — | — |
| keep（装配器 chunk-first） | **47.6%** | A→B=18 B→A=7 | **0.0455** | 显著优于 base |
| trace（1 条精选） | 50.0% | keep→trace A→B=16 B→A=8 | **0.152** | **与 keep 持平（不显著）** |

**结论**：装配器（无 trace sidecar，纯 chunk-first 排序 + cap 内排序）就把 base 从 27.4% 提到 47.6%（p=0.0455 显著）；trace 相对 keep 仅 +2.4pp 且 p=0.152 **不显著**。→ **trace 引用链机制没有独立于装配器的显著增量**；主要增益来自"证据精炼"（chunk 优先 + 聚焦）这一读侧结构方向，trace 只是其一种实现。

## 机制归因（修正版）

1. **证据精炼是有效杠杆（读侧结构）**：base 塞满 12 条 chunk（3600 tok）被噪声稀释（27%）；keep 装配器 chunk-first 排序（47.6%）与 trace 精选 1 条（50.0%）都显著优于 base。**chunk 优先 + 聚焦优于 score 序塞满**——这是 030 读侧装配的真实机制（US1 已证），非引用链独有。
2. **trace 引用链无独立增量**：keep→trace p=0.152 不显著。sidecar 选的 1 条 evidence 未必优于装配器 top chunk。fail-closed gate（valid 88.3%）管线稳定但机制收益与简单装配重叠。
3. **evidence_count 几乎全 1**：sidecar 倾向只选 1 条，可能低估 trace 上限，但现状下不构成独立优势。

## 与先前证伪线的衔接

- 检索侧单次排序/表示：021/013/014 六次证伪。
- 检索侧多步导航：029 证伪（推理介入检索负收益）。
- **030：post-retrieval 证据精炼（读侧装配）实证有效**——chunk-first 装配（keep）显著优于 legacy base；trace 引用链作为其一种实现，无独立显著增量。读侧结构增量区别于写侧（027/028 证伪）和检索侧（029 证伪）。

## 判定

008 铁律（paired majority ≥ 基线）字面满足：trace（50.0%）显著优于 legacy base（27.4%，p=0.0017）。但**归因修正**：增益主要来自装配器 chunk-first 精炼（keep 47.6% 亦显著，p=0.0455），trace 相对 keep 无独立显著增量（p=0.152）。**US2 判定：机制方向 GO（读侧证据精炼），引用链 sidecar 无独立价值（相对简单装配不显著）**。trace 代码保留为 opt-in（`--trace-mediation` 默认 false）；US3（条件压缩）继续执行。
