# Target-32 Gold-in-Pool Diagnostic (independent)

> **2026-08-10 勘误（033 付费探针后）**：下文“32/32 gold 都在 top-30、无召回缺口”的
> 解释不成立。`gold_in_pool=true` 只表示至少一个 gold turn 在 wide pool 出现，
> `gold_rank_topk` 也只取第一个覆盖任意 gold turn 的候选；它们不要求回答链完整。按 top-30
> `mapped_gold_turns` 并集重算：32/32 至少命中一个 turn，但仅 **26/32** 覆盖全部 gold turns；
> 9 道 multi-hop 仅 **4/9** 全覆盖，平均 turn recall 70.4%。此外 chunk 的 DiaID 覆盖不检查
> `blip_caption` 是否进入 chunk；target 的 16 道 single-hop 残差中有 10 道关键细节依赖 caption，
> 当前默认 store 未包含它。完整勘误与失败归因见
> [033 failure analysis](../../../docs/evaluation/reports/033-chunk-first-failure-analysis.md)。下文保留为
> 当时的独立诊断原文，不再作为现行结论引用。

**Author**: main-session backstop（独立贡献，非 033 实现者）
**Date**: 2026-08-10
**Method**: `locomo-bench --attribution-trace`（retrieval-only，零 answer/judge 调用）对 `diagnosis/target-32.txt` 跑 hybrid 检索，bge-large 1024d 匹配 `009-bge-chunks-store`，top-k 30 / chunk-quota 12。
**Artifacts**: `~/.claude/session-scratch/v4pro_probe/target32-diag/trace.jsonl`（本机 scratch，非 repo）

## 结论一：救回空间真实存在（无召回缺口）

- **32/32 题的 gold 都在 top-30 候选池内**（`gold_in_pool=true`）。
- 说明 target-32 的失败**不是** "gold 不在候选池 / caption 缺口 / 召回不足" 型 —— 排序修复的救回空间成立，且不能把失败归咎于召回。

## 结论二：靶心 = chunk-gold 挤在 top-30 尾部

- **19/32（59%）题的 gold 覆盖证据是 chunk**，其余为 fact。
- chunk-gold 的 `gold_rank_topk` 分布：`[1,2,9,19,19,19,19,19,19,19,19,19,19,20,20,22,23,23,25]`，**中位数 19**，14 题 ≥19（top-30 尾部）。
- 逐题实证（`0-q-133`）：检索返回前 18 项**全是 fact**，gold chunk（`chunk-c0-s16-004`）排在 rank 19；rank 19-25 才是 chunk。当前装配下 gold chunk 被 18 个 fact 压在后面；修复（chunk-first）后它前移到所有 fact 之前。

## 结论三：门禁评估

- **target 净救 ≥8/32 有希望**：19 个 chunk-gold 题里 14 个 rank≥19（靠后前移型），修复直接受益。
- **全量 >90% 存疑**：两个不确定点见下；排序修复的期望增量 +0.3~0.8pp，落在 89.5-90 边界。

## 给 033 实现者的两个检查项（探针前必做）

1. **cap 截断检查**：确认这 19 个 chunk-gold 题在当前装配下是否真的触发 cap 截断、gold chunk 是否在截断外。
   - 若**截断**（gold 本不可见）→ 修复是"截断救回"，增量大。
   - 若**不截断**（gold 在上下文但靠后）→ 修复只是"聚焦重排"，增量小、可能 within-noise。
   - 主会话实测全量 legacy `answer_context_mean=3270 tok < 5000 cap`，**暗示多数题不截断**，增量偏向后者 —— 探针必须报告每题 context 与是否截断，不能只看翻正数。
2. **chunk-gold 分组分析**：64 题探针的 C-vs-A 翻正，按"chunk-gold 靠后（rank≥19）" vs 其余分组报告，不要只报总体。

## 强 answerer 敏感性背景（主会话同日实测）

- v4-pro 无装配（legacy，context 3270）：1-rep **88.1%**；3-rep 基准 **89.03%**。
- v4-pro + trace 装配（context 压到 620）：1-rep **87.4%** —— **装配扰动对强 answerer 净反伤**（尤其 open-domain −4.2pp）。
- 033 的 evidence-assembly 只重排、不激进压缩 context，风险小于 trace，但 high-base（v4-pro + 思考）下机制增量可能缩水 —— 与 032 tplan 规律一致（base 越高、契约增量越小）。

## 关联

- 本文件是独立 backstop 贡献；033 实现/评测归外部 agent，本文件不改变其 verdict 口径。
- 数据 store/检索：`009-bge-chunks-store`（bge-large 1024d / schema 7），与 spec preflight 一致。
