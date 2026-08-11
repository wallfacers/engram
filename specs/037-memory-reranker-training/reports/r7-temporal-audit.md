# R7 Temporal Payload 可判别性审计

**Date**: 2026-08-11 | **T012** | **data**: ../../testdata/locomo/locomo.json
**时间窗口**: ±7 天（答案 session 日期）

## 审计统计

- 审计组数: **321**（目标 ≥50）
- 答案文档: 283/375 含时间信号 (**75.5%**)
- 窗口外全池候选: 172257 个; 含时间信号 23402 (**13.6%**)
- **hard 池**（窗口外 + 语义相关, overlap≥2）: 40433 个;
  日期可提取 40433 (**100.0%**); 含时间信号 7557 (**18.7%**)

## 判定阈值（冻结）

- A. 答案时间信号率 ≥ 0.7 → 通过 (75.5%)
- B. hard 池日期可提取率 ≥ 0.5 → 通过 (100.0%)
- C. hard 池时间信号率 ≥ 0.5 → 不通过 (18.7%)

## 结论: **FAIL — 文本时间信号不足/无判别性，temporal-hard 训练需谨慎**

## 冻结项

- 时间信号模式集: 见 build_training_data.TIME_SIGNAL_PATTERNS（绝对日期/相对时间词/星期/顺序词）
- 时间窗口: ±7 天
- 判别性阈值: A≥0.7 / B≥0.5 / C≥0.5
- 适用边界: 判定基于 LoCoMo 文本; 未过 audit 的样本不得标 temporal_label=true
- 增强路径: 真实 baseline top-pool（--run-dir）待 US1 后补（T012b）

## 无时间信号答案案例（抽样）

- conv-26-q1 `When did Melanie paint a sunrise?` → `Melanie: You'd be a great counselor! Your empathy and understanding will really help the people you `
- conv-26-q31 `When did Melanie go camping in June?` → `Melanie: It was an awesome time, Caroline! We explored nature, roasted marshmallows around the campf`
- conv-26-q68 `How long has Melanie been practicing art?` → `Melanie: Seven years now, and I've finally found my real muses: painting and pottery. It's so calmin`
- conv-26-q72 `When did Melanie's friend adopt a child?` → `Caroline: Thanks, Melanie! I'm stoked to start this new chapter. It's been a dream to adopt and prov`
- conv-30-q7 `When did Gina launch an ad campaign for her store?` → `Gina: Hey Jon! Long time no see! Things have been hectic lately. I just launched an ad campaign for `
- conv-30-q10 `When did Gina team up with a local artist for some` → `Gina: That's awesome! I'm sure you feel great knowing your students are doing so well with dance. It`
- conv-30-q13 `When did Gina open her online clothing store?` → `Gina: Yay! My online clothes store is open! I've been dreaming of this for a while now - can't wait `
- conv-30-q20 `When did Gina get accepted for the design internsh` → `Gina: Hey Jon! Long time no talk! A lot's happened - I just got accepted for a fashion internship!`
- conv-30-q21 `When did Jon start reading "The Lean Startup"?` → `Jon: I'm currently reading "The Lean Startup" and hoping it'll give me tips for my biz.`
- conv-30-q22 `When did Gina develop a video presentation to teac` → `Gina: Proud of you for starting your own business! It takes strength to stay hopeful. What are you d`
