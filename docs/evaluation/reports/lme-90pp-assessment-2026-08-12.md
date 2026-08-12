# LME 错题画像 + 90pp 可达性评估

**日期**: 2026-08-12 · **口径**: clean（`extractFinalAnswer`，002ac27） · **数据源**: summary.json 重判 + attribution-trace 500 题全量

## 1. 错题画像（baseline clean，77/500）

| 类别 | 错题 | 含 ABS | 非 ABS | 主错模式 |
|---|---:|---:|---:|---|
| multi-session | 23 | 4 | 19 | 聚合/计数算术错（3 vs 2、$3,750 vs $8,750） |
| temporal-reasoning | 19 | 5 | 14 | 时间换算错（21 vs 26 days、3 vs 6 weeks） |
| knowledge-update | 16 | 4 | 12 | 最新值选错（25:50 vs 27:12、Paris vs Hawaii） |
| single-session-preference | 9 | 0 | 9 | 偏好综合建议错（gold=偏好表述） |
| single-session-assistant | 6 | 0 | 6 | 定位/多值冲突 + 过度拒绝（3 题说 "No information" 但 gold 在池内） |
| single-session-user | 4 | 4 | 0 | 全为 ABS |
| **合计** | **77** | **21** | **56** | |

## 2. 检索层彻底证伪（attribution-trace，500 题全量对齐）

| 组 | n | gold_in_pool | median topk rank |
|---|---:|---:|---:|
| 对题（clean✓） | 423 | 98% | 2 |
| 错题（clean✗） | 77 | 82% | 3 |
| — ABS 错题 | 21 | **33%** | 1 |
| — **非 ABS 错题** | 56 | **100%** | 3 |

- **非 ABS 56 个错题 gold 100% 在 top-k 150 内**（≤3 有 29、4-10 有 19、11-30 有 6、31-150 有 1）。
- 检索非瓶颈的硬证据：所有非 ABS 错题的 gold 证据都给了 model，model 判断/计算/选错。
- ABS 错题 gold_in_pool 仅 33% 是设计使然（ABS 问"不存在的信息"）。

## 3. 杠杆账（从 84.60% clean 起）

**已取**：top-k 150（基线已含）、force-answer（E1 净 +0.8pp，去 force 救 8 ABS 但非 ABS 丢 3）。
**已证伪**：冲突解决 prompt A/B（6/6 零效果）、answer-focus-prompt（LoCoMo −0.13pp）、检索层（本报告）。
**未测候选**：
- **ABS 混合契约**（force-answer 保持 + 显式"证据不充分则拒答"）：最大单杠杆，21 题上限 = **+4.2pp**；E1 实测只救 8（因简单去 force 有副作用），混合契约预期 8-14 题 = +1.6~2.8pp
- **temporal-answer-prompt 上 LME**（032 契约，LoCoMo 84 子集 +15.8pp 显著，LME 未验证）：LME temporal 是"距今 X days/weeks/months"算术，预期 +1~3pp（4-7 题）
- **assistant 过度拒绝修复**（"检索到 gold 证据就别拒"）：3 题 ≈ +0.6pp

## 4. 可达性判断：90pp 当前栈不可靠可达

| 情景 | 救回题数 | 预测得分 |
|---|---:|---:|
| 保守（ABS 8 + temporal 4 + 过度拒绝 2） | 14 | 87.4% |
| 中性（ABS 12 + temporal 6 + 过度拒绝 3 + KU 2） | 23 | 89.2% |
| 乐观上限（每个杠杆满打满算 + 无副作用） | 27 | 90.0% |

- **90pp = 450/500，需救回 27 题（+5.4pp）**。中位数 ~88-89%，乐观天花板恰好卡在 90。
- 两个最大杠杆的现实上限加起来 ~18-20 题（ABS 12-14 + temporal 6），**缺口 7-9 题只能靠模型能力**（v4-pro thinking 开重测 / 训练，宪法 B 类）。
- 聚合/计数算术错 ~16 题**无 prompt 杠杆**（answer-focus 已证伪），是能力硬账。
- **判定：Qwen thinking 栈上 90pp 不可靠可达；诚实天花板 ~89%，残余缺口属模型能力/训练。**

## 4.5 v4-pro thinking 开实测（box run 核实 + 同批重判）

**结论：v4-pro thinking 开 @ k150 已跑过，实测 83.60%，不优于 Qwen。** 维护者记忆正确（box 有 `lme-v4pro-k150-think` run）。首轮核实误判"未跑过"，因只查本地 docs/tar 未查 box run 目录；box 核查纠正。

| 日期 | 配置 | 结果 | thinking 状态 |
|---|---|---|---:|---|
| 2026-07-28 | v4-pro @ k30/50 · 3-rep 多数 | 86.00% [86.0, 85.4, 86.2] | 禁（ThinkingDisabled，LOCOMO_NO_THINKING 默认开） |
| 2026-08-12 探针 | v4-pro @ k150 · 1-rep | 79.2% | 禁（LOCOMO_NO_THINKING=1） |
| **2026-08-12 think** | **v4-pro @ k150 · run-1 完整 500** | **83.60%（418/500）** | **思考（LOCOMO_NO_THINKING=0 → 不发字段 → DeepSeek 默认思考）** |

- **thinking 机制澄清**：DeepSeek v4-pro 无 `thinking` 字段时**默认思考**（探针实测：无参数返回 525 字完整 thinking 块）。harness 丢弃 thinking 块只留 final text，故 run-1 pred 是短答案（497/500 <200 字），但**确是思考后产物**。8/12 探针"pred 含 thinking 0 条"是 harness 丢弃，非模型没思考。
- **同批 flash 重判**：run-1 500 题重判 = 418/500 = **83.60%**（与 history judge 一致，v4-pro pred 干净故两口径相同）。
- **vs Qwen**：Qwen thinking clean 84.60%（同 judge prompt）。v4-pro 救回 Qwen 的 28 个难推理错题（6a1eabeb/982b5123/4dfccbf7/9a707b81 等），但丢 33 个 Qwen 对题，净 −5（−1.0pp）。
- **路径证伪**：v4-pro 强 answerer **不是 90 路径**——k150 大上下文下 v4-pro 思考整体不优于 Qwen 思考（83.60 vs 84.60）。7/28 的 +5.2pp 是 k30/50 旧基线（Qwen 当时 80.80% 禁思考）的历史优势，未迁移到 k150 + Qwen 思考提升后的现状。

## 5. 目标设定（修正后）

- **近期目标（本地栈，可达）**：LME clean **87.5-88.5%**。路径 = ABS 混合契约（+1.6~2.8pp）+ **temporal/multi-hop 契约迁移**（LME category 9/8 当前落到通用 force prompt，迁移 032/033 验证过的契约）+ 过度拒绝修复（+0.6pp）。
- **90pp 判定**：本地栈天花板 ~89%；**v4-pro 强 answerer 已实测证伪**（83.60%）。仅剩 SaaS 训练（宪法 B 类）或更好的本地模型。90 不作为近期承诺目标。
- **下一步实验**：改 harness 让 LME temporal(cat9)→temporalAnswerPrompt、multi-session(cat8)→multiHopAnswerPrompt，跑全量 A/B；ABS 契约用 `--abstain-prompt`（现有 flag）测净效果。

## 6. 验证实验计划（Qwen box 可用时）

1. **ABS 混合契约 A/B（零全量风险）**：对 21 个 ABS 错题构造上下文（同 conflict-prompt 方法），测"证据不充分→拒答"契约是否让 Qwen 从"给具体值"转"正确拒答"≥8 题；PASS → 写 harness flag + 全量。
2. **LME 全量 temporal-answer-prompt**：同 store/同 judge/同 rep 对比 baseline 同批。
3. **v4-pro thinking 开 @ k150 重跑**：同口径对比 Qwen 基线（当前 79.2% 是禁思考不可比探针）。

## 诚实边界

- 错题分类基于 clean 重判（同批 flash judge，±1pp 噪声）；单批判定含 ±2.5pp 跨批漂移风险。
- 非 ABS 错题"gold 在 pool"由 attribution-trace 证明，但"model 看到了证据却没用对"是推断（thinking 长度有限）。
- 各杠杆救回数为估计区间，未跑全量验证（box 未开）。
