# 031 Verdict: 读侧证据关联装配（Evidence Relation Assembly）

**日期**: 2026-08-07 · **口径**: 子集配对（84 题 = 029 实际子集，temporal 59 + multi-hop 25）× 3 reps majority **+** 全量 1540 × 1 rep · **模型**: deepseek-v4-flash（禁思考，OpenAI 直连）答题+判题 · **检索**: fts（keyword-only 弱检索，本地 bge-m3 hybrid 对照见探针）· **store**: flash-84/store（全量 extraction 复用）· **装配**: `--evidence-assembly`（030 地基）+ 031 `--relation-context`

## 结论

**机制方向 GO（读侧结构提示），无显著独立增量；default-off 保留。** 结构上下文（related_to / temporal_next / caused_by 显式关系提示）在弱检索（fts）下两个口径都**方向正**且**生效类别（temporal / multi-hop）一致正向**——子集 +2.4pp（p=0.75）、全量 +1.04pp（p=0.253），但均 **within-noise**。与 flash 探针预测完全一致：**结构精炼的展示空间在弱检索，增量有方向但不显著**。

## 配对结果（008 铁律，arm-to-arm：同 store/answerer/judge/cap/检索）

### 子集 84 题 × 3 reps majority（fts）

| 臂 | OVERALL | temporal | multi-hop | 判定 |
|---|---|---|---|---|
| keep（装配器，无关系提示） | 27.4%（23/84） | 32.2%（19/59） | 16.0%（4/25） | 基线 |
| **relation（+结构上下文块）** | **29.8%（25/84）** | 33.9%（20/59） | **20.0%（5/25）** | 净 +2 题 |

flips keep→relation 6 / relation→keep 4，McNemar **p=0.75** within-noise。方向正，类别全正向无回落。

### 全量 1540 × 1 rep（fts）

| 臂 | OVERALL | temporal | multi-hop | single-hop | open-domain |
|---|---|---|---|---|---|
| keep | 47.66%（734/1540） | 44.2%（142/321） | 55.3%（156/282） | 44.5%（374/841） | 64.6%（62/96） |
| **relation** | **48.70%（750/1540）** | **47.4%（152/321）** | **58.5%（165/282）** | 44.7%（376/841） | 59.4%（57/96） |
| Δ | **+1.04pp（+16 题）** | **+3.2pp** | **+3.2pp** | +0.2pp | −5.2pp |

逐题：relation 赢 **94** / keep 赢 78，净 +16 题 favor relation，McNemar **p=0.253** within-noise。

**关键模式**：031 只作用于 multi-hop / temporal（其余类别 fail-soft 不注入），而这两个生效类别**一致 +3.2pp**；single-hop（不生效）+0.2pp、open-domain（不生效）−5.2pp —— 后两者是检索/采样噪声，非 031 因果。→ **增量方向集中在机制生效的类别**。

## 机制归因

1. **关系提示在弱检索下有方向正的增量**：fts 下候选证据弱，answerer 需要显式结构提示（谁的时间后继、谁的共享关联）来组织证据——全量生效类别一致 +3.2pp，与 flash 探针"弱检索下结构精炼 +2.8pp（p=0.006）"方向吻合。但增量幅度小、样本不显著。
2. **related_to 在单对话候选集上信息量受限（结构性发现）**：LoCoMo 是单对话数据，所有 chunk/fact 共享说话人双方，`related_to`（共享实体）被参与双方主导、无区分度；MemCog 的 Graph Overlay（`2605.28046` §3.2.3）是在 **LLM 抽取的结构化事实 → 语义聚类成页面 → 跨维度页面** 之间建链，与 031 的"单对话候选集内临时共现"形态不同。031 的 `temporal_next`（EventDate，store 已有、可靠）才是 LoCoMo 上真正有信息量的关系类型。
3. **因果词典需数据适配**：`since` 在 LoCoMo 是时间介词（"since 2023"），一次产出 17 条虚假 caused_by，已从词典移除。

## 诚实边界

- **全量单 rep**（非 3 次 majority）；judge = flash（与 answerer 同模型）。
- **叠加臂（relation + trace）未实测**：基于独立臂结果（全量 +1.04pp within-noise）+ 探针（trace 与 relation 同属读侧结构精炼，强检索下 trace 无增量）+ 030（trace 相对装配器无独立增量），推断 relation 在 trace 之上无显著增量；叠加效应未实测，不混报。
- **绝对分不可跨口径比**：fts 弱检索（keep 47.66%）vs 030 hybrid Qwen（85.91%）不可比；008 铁律 arm-to-arm 只在同口径内成立。
- **实体提取是确定性启发式**（说话人排除 + 停用词 + 单 token 专名），对话数据上残留噪音由共享度过滤 + 全局边 cap 抑制；MemCog 用 LLM 抽取的结构化事实，031 为保持零模型 / 零写侧（FR-008 + Constitution I/V）未采用。
- 029 模拟高估教训：本 verdict 以 engram 同栈配对 majority 为准，不以 MemCog 消融 delta（w/o Graph Overlay ↓6.79pp）推断必然涨点。

## 出货影响

- **default-off**（`--relation-context` 默认 false，SC-004 parity：关闭时装配路径逐字节不变）。不改变默认 MCP/CLI/检索路径。
- 作为 opt-in 评测能力保留（与 030 trace / consolidate 同纪律）。
