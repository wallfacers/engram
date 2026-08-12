# LME 冲突解决 prompt 精化 NO-GO（A/B 实测 6/6 零效果）

**日期**: 2026-08-12 · **宪法 IV**: 负结果记录 · **状态**: 已证伪

## 背景

LME clean 错题画像（77/500）显示三类根因：多记忆冲突选错（knowledge-update 16 / multi-session 部分）、
聚合计算错、时间推理错。据此设计"冲突解决规则"追加到默认 answer prompt
（`lme-e1-no-force-verdict` 中"90pp 需精细 LME prompt：最新记忆优先 + 计算/聚合强化"的建议），
先用 Qwen A/B 实测规则是否改变选择，再决定是否写 harness flag + 全量。

## 方法

- 6 个典型冲突错题：6a1eabeb（25:50 vs 27:12）、830ce83f（suburbs vs Chicago）、73d42213（9AM vs 1PM）、
  9ee3ecd6（100 vs 300）、affe2881（32 vs 27）、982b5123（5mo vs 3mo）
- 从 pred thinking 提取可见记忆，构造 `RETRIEVED MEMORIES` 上下文（与 harness `buildAnswerPrompt` 同构）
- 远端 Qwen vllm（Qwen3.6-35B-A3B-FP8 @8000，thinking 开，temp=0）
- 对照：旧默认 prompt vs 默认 + 冲突规则段（prefer explicit-attribute / most-recent / more-specific / nearest-CURRENT-DATE；聚合逐条列式）

## 结果

**6/6 题规则完全未改变 Qwen 的选择**（27:12/Chicago/300/27/3mo 全部复现旧错；2 题变成长段未作答）。

## 根因判定（对照 clean 重判的检索线索）

- **9ee3ecd6（gold 100）**：可见上下文只有 "needs 300 points"，gold 100 那条记忆**未进 top-150** → 检索缺，prompt 无法救
- **6a1eabeb（25:50 vs 27:12）**：两条记忆都自称 "personal best"（同 recorded 日）→ 简单规则（最新/最具体/显式属性）无区分度，需更深语义理解
- **affe2881（32 vs 27）**：model 已见 32 但判 "27 更 specific to local park" → 推理偏好，非规则缺失
- **73d42213（9AM）**：gold 需推理（doctor's appointment → clinic arrival），无显式记忆 → 推理能力问题

**结论**：LME 错题不是 prompt 规则缺失。**attribution-trace 补验（同日）证伪"检索缺"**：LME 500 题
`--attribution-trace`（lme-s500-store 复用，top-k 150 / chunk-quota 12 与 run 一致）显示错题 77 的
gold 内容 **82% 在检索 pool、gold_rank_topk median=3**（对题 98%）；典型题 6a1eabeb 的 27:12(rank1)+25:50(rank3)
同在 top-3，9ee3ecd6 的 300/200/50 全在 rank1-4。**检索正常，错题 = 多值冲突选错（候选在 top-3 内但 model 判断错）**，
是模型能力/判断问题。prompt 精化方向无效（A/B 6/6 零效果自洽）。**不再投入 prompt 微调**。

## 90+ 路径修正

- LME 到 90（clean 84.6/85.57 → 90，+4.4pp）无快速杠杆：force-answer（+0.8pp 已取）、top-k 150（+2.2pp 已取）、
  prompt 规则（0pp 实测）、**检索层（正常，非瓶颈）**。剩余需**模型能力/训练**（SaaS 方向，宪法分类 B）。
- **LoCoMo 已 91.10%**（clean majority，离线重判）——90+ 唯一已达处，但依赖单批 judge（±2.5pp 漂移）。
- **此前"LoCoMo 92% 候选缺失 = 检索层瓶颈"判断被 LME attribution 证伪为疑似误差**：flash 判 thinking 文本不含候选
  ≠ 检索缺（model 可能看过但 thinking 未写出该值；LME 实证 gold 在 top-3）。LoCoMo 检索层问题需单独验证
  （无 store，待重建），**不得外推**。

## 诚实边界

- A/B 用构造上下文（thinking 提取的记忆子集），非真实 top-150 检索结果；选择倾向的验证是定性的，
  但 6/6 零变化的信号足够强（若规则有效应至少改变 1-2 题的明显冲突如 830ce83f）。
- 未写 harness flag、未跑全量——A/B 已给出负判定，避免无谓全量。
