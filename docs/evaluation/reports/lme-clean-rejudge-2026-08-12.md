# LME clean 口径重判：judge 作弊量化 + thinking→final gap 证伪

**日期**: 2026-08-12 · **宪法 IV**: 口径归因 · **状态**: 离线重判完成

## 背景

judge 修复（`002ac27` extractFinalAnswer，见 [judge-final-answer-regime.md](judge-final-answer-regime.md)）落地后，
history judge 拿含 thinking 完整 pred 判定的历史分数需要 clean 重判校准。本报告对 LME baseline k150 / E1 全量
500 与 LoCoMo topk150 3-rep 做同批 raw vs clean flash 重判，量化 judge 作弊量并实测 thinking→final gap 规模。

## 方法

- 同批 flash judge（`deepseek-v4-flash`，`thinking: disabled`，temp=0，max_tokens 512，mem0-aligned prompt 与
  harness `judgeMem0AlignedSystemPrompt` 逐字一致，anthropic `/v1/messages` 格式）
- **raw** = 完整 pred（复现 harness 旧行为）；**clean** = `extractFinalAnswer` 提取后的 final answer
- 同批次 → 跨批 judge 漂移在两臂间抵消，raw vs clean 差异 = 提取的净效应
- 脚本: `~/.claude/session-scratch/lme-rejudge/rejudge_lme.py`、`gap_measure.py`

## 结果

### 1. LME clean 真实基线

| 数据集 | history | raw 重判 | **clean 重判** | 同批净效应 |
|---|---:|---:|---:|---:|
| 基线 k150（500 题） | 86.20% | 85.80% | **84.60%** | −24 题（−1.2pp） |
| E1 去 force（499 题） | 86.77% | 89.18% | **85.57%** | −18 题（−3.6pp） |

- **LME 真实基线（clean 口径）= 84.60%**，被 judge 从 thinking 前导读候选值作弊高估 ~1.6pp。
  90pp 目标在 clean 口径下需 +5.4pp（非此前估计的 +3.8pp）。
- **E1 结论稳健**：clean 口径下 E1 仍 **+0.80pp**（84.77→85.57，n=499 交集）→ force-answer 非 LME 主瓶颈，原 verdict 成立且略强。
- E1 raw 重判 89.18% 是单批虚高（跨批 judge 放水 +2.4pp 叠加 thinking 作弊 −3.6pp）——**任何单批 judge 分数 ≥±2.5pp 漂移**再确认。

### 2. judge 作弊量跨数据集统一

同批 raw vs clean 净效应：LoCoMo **−23 题（−1.5pp）** ≈ LME baseline **−24 题（−1.2pp）**。
judge 从 thinking 作弊 ~1.2–1.5pp 是跨数据集一致的。LoCoMo 对外报的 history→clean **+0.97pp 是跨批漂移（+2.47）掩盖**，不能归因于 clean 提取本身。

### 3. ABS 不是 clean 拖累主因

ABS 题 clean−raw 仅 −4（baseline）/ −3（E1）。E1 拖累主在 **non-ABS（−15）**，且 E1 全部 17 个作弊题 final 为**具体值**（非拒绝）——judge 对"孤立短 final"的语义匹配比"含推理完整 pred"更严，是 judge 上下文效应，不是模型拒绝表达退化。

## thinking→final gap 证伪（LoCoMo topk150 137 个 clean majority 错题）

用 flash 逐题判 thinking 文本是否含正确候选（gold 或其等价表述）：

| 判定 | 数量 | 占比 |
|---|---:|---:|
| **GAP（thinking 含候选但 final 错）** | **11/136** | **8.1%** |
| 推理本身也错 | 125/136 | 91.9% |
| unknown | 0 | — |

- **GAP 修复上限 ~0.7pp（majority）**；11 题里 2 题是 judge 边界假 gap（`one month` vs `1 month`、
  `final` 与 `gold` 文本相同），真实可救 ~9 题 ≈ **0.6pp**。
- GAP 类别：single-hop 7 / temporal 2 / open-domain 1 / multi-hop 1。
- 非 GAP 主错类：**single-hop 63** / open-domain 29 / temporal 23 / multi-hop 10 —— 推理本身错是主导。

**结论**：002ac27 报告中"真正瓶颈 = answerer 的 thinking→final 一致性"的判断被量化**证伪为次要**。
错题 91.9% 是推理本身就错（thinking 与 final 一起错），answer prompt 强化收益上限 <1pp，**不值得跑全量 3-rep**。
90pp 的真正缺口在检索/抽取层（single-hop 深层召回），属 SDD 大工程而非 prompt 微调。

## 诚实边界

- 重判离线单批次；judge temp=0 仍带 ±1pp 模型噪声（跨批已实测 ≥±2.5pp）。
- clean 口径正式基线需下次 harness eval 确认（当前代码已默认 clean 提取，下次 eval 自动生效）。
- thinking→final gap 的 flash 判定本身带 judge 噪声，8.1% 是估计量非精确值。
