---
title: 043 confidence-gated deepening — pilot NO-GO verdict
summary: 犹豫信号 2-conv pilot 双门皆败(AUC 0.542 / flip 93.4%);测量层两处结构 bug 已修复(temperature 省略 + 042 content-后缀映射在当前 vllm 栈结构性失败);机制性结论:unified 契约 + thinking 下 final answer 不泄露不确定性。
status: final
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-15
tags: [evaluation, verdict, 043, confidence-gated, no-go]
---

# 043 confidence-gated gap-guided deepening — 信号质量 pilot verdict(NO-GO)

**日期**:2026-08-15 | **Feature**: specs/043-confidence-gated-deepen | **门**: FR-002(AUC≥0.65 且通道 flip≤0.10)

## TL;DR

**NO-GO 关闭**。测量层修复后(pilot run2,parse_coverage=1.0),犹豫/置信信号对错题的判别力 **AUC 0.49-0.54**(CI 全部远离 0.65),且双通道 flip_rate 93.4% 超噪声带。confidence-gated deepen(041→043 线)在当前栈**没有可用的门控信号**——040 的"93% 信息不足时表达犹豫"前提不迁移到 unified 契约 + thinking 配方。

## 结果正本(pilot run2, 2026-08-15, 2 conv / 304 题)

| 信号 | AUC | 95% CI | parse_coverage |
|---|---:|---|---:|
| logprob · final_mean_logprob | 0.503 | [0.436, 0.566] | 1.00 |
| logprob · final_p10_logprob | 0.493 | [0.425, 0.555] | 1.00 |
| logprob · final_mean_top1_top2_margin | 0.540 | [0.473, 0.605] | 1.00 |
| textual · hesitation lexicon | 0.542 | [0.510, 0.578] | 1.00 |
| **channel parity** | flip_rate **0.934**(284/304) | — | — |

- 判分分布:192 对 / 112 错(63.2%;conv 0/1 为 temporal/open-domain 重难点集,正类充足,AUC 有效)。
- 犹豫表达触发率:**19/304 = 6.3%**——112 道错题几乎全部"自信地错"。
- 配方 = 87.9% 锚正本(hybrid+unified k30 + chunk-quota 12 + thinking-on + LOCOMO_NO_THINKING=0 + 032-store 复用 + 同 judge)。
- 工件:本地 `.locomo-run/043-20260815/`(pilot=run1 作废件、pilot2=run2 正本,manifest/seal/report/attempts 全)。

## 机制性归因

040 的犹豫观测来自 **trace-mediation 时代的证据中介装配**(468 token 紧凑证据上下文)。当前主路径是 unified 契约 + 原始 chunk 装配:answerer 在 thinking 里消化不确定性后,**final answer 通道只输出收敛后的自信表述**。logprob 三特征同样无效(AUC≈0.5)——答案 token 的生成置信不携带"证据不足"信息。结论:**此路线的信号源在当前配方下不存在**,不是阈值/特征选择问题。

## 过程中修复的两个测量层结构 bug(commit 1eb9cdd)

1. **logprob 通道 temperature 省略**(042 代码有意为之):temp≈1 采样 vs 流式通道 temp=0 → run1 flip 95% 的主因之一。
2. **042 `utilityMapFinalSignal` 的 content-后缀前置在当前 vllm 栈结构性失败**:vllm 未开 reasoning-parser,thinking 内联 content + 末尾 `<|im_end|>` 特殊 token → "content 必须是重建字节的精确后缀"永不成立 → 304/304 `content_not_generated_suffix`。043 侧以 `deepenFinalSpanSignal`(剥特殊 token + 最后关闭符后取 span,公式复刻冻结版,含等价性回归测试)修复。

**⚠️ 附注(维护者决策项)**:042 的"信号 5/5958 NO-GO"走同一映射代码、同一 box、同一 restart 脚本——**有强嫌疑是同一 bug 的产物**,即 042 的信号采集可能从未成功,其 NO-GO 归因(信号无区分度)未经有效测量验证。是否重开 042 由维护者决定;本修复(Temperature 可选 + 鲁棒映射)已具备重验条件。

## 对 k30→90pp+ 目标的影响

主杠杆(P1 confidence-gated deepen)关闭。剩余开放项:
- **Step A(unified × trace-mediation)**:与 040 前提同源(trace 时代犹豫可观测)——探针对(1-rep × trace off/on)已于同日 box 批次启动,结果另行报告;
- P2 次模装填(reader>7B 优势消失的 scope 警告仍在,期望压低);
- P3 TAA-k(只省预算,非得分杠杆)。

## 门后处置(按 spec US1 NO-GO 路径)

- T015-T022(机制钩子/配对批/LME 迁移)**不执行**——机制代码从未落地,`--confidence-deepen` 无实现(纯函数层 + pilot 保留为资产)。
- tasks.md T010-T014 勾结,Phase 4/5 关闭。
- box 收尾:Step A 完成后备份小文件 → 关机(必做)。
