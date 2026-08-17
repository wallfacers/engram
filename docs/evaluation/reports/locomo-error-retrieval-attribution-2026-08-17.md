---
title: LoCoMo 错题检索归因 — 池外召回判死,90% 错题是 answerer 侧
summary: 用 009 attribution trace(1540 题逐题 gold_rank_pool)+ tk150-full3 3-rep 152 错题清单 join,零模型成本完成检索 vs 回答归因:全量池外仅 14/1540(0.9%),152 错题池外仅 4 题、gold rank≤150 的 137 题(90%)——"open-domain 池外召回"假设判死;temporal 契约 × Qwen thinking 一度是唯一未测机制杠杆,但载体 --temporal-answer-prompt 是类别特化提示词,2026-08-17 维护者裁决不作方案手段——LoCoMo 杠杆线整体收线,诚实锚 91.10%(tk150 clean)/87.9%(生产配方)。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-17
tags: [evaluation, locomo, attribution, retrieval, recall, error-analysis]
---

# LoCoMo 错题检索归因(2026-08-17,零模型成本)

**触发**:046 closure 后"剩 open-domain 池外召回"遗留问句的验证。结论:该假设**判死**,
本文件为检索侧归因的收口正本。

## 一句话结论

**"gold 不在宽池"在 LoCoMo 上几乎不存在**(全量 14/1540=0.9%):152 道错题里 4 题池外、
21 题 rank 151-900(截断层,加量型已哲学否决)、**127 题(83%)gold 在 k150 上下文内仍答错**——
错题主体是 answerer 侧,与 [[locomo-error-patterns-2026-08-12]] 的形态分类独立交叉一致。

## 诊断方法(零 LLM/judge/embedding 成本)

- **数据源 A(错题清单)**:`~/.claude/session-scratch/topk-err/wrong-list.json`——tk150-full3
  3-rep majority 的 152 错题(raw-judge 口径;clean 重判后基线 91.10%,错题≈137,量级不变)。
- **数据源 B(gold rank)**:`.locomo-run/009-attribution-fixed/trace.jsonl`——009 归因 run 的
  全量 1540 题逐题 `gold_in_pool / gold_rank_pool`(900 宽池,gold turn 映射口径见 009)。
- **方法**:按 (conv,q) join,分类别桶化 gold rank,与 1388 对题对照。

## 归因表(152 错题 × gold_rank_pool)

| 类别 | n | 池外 | ≤30 | 31-150 | 151-900 | rank 中位 |
|---|---:|---:|---:|---:|---:|---:|
| single-hop | 68 | 3 | 20 | 37 | 8 | 60 |
| temporal | 39 | 0 | 11 | 22 | 6 | 67 |
| open-domain | 31 | 1 | 8 | 15 | 7 | 104 |
| multi-hop | 14 | 0 | 9 | 5 | 0 | 12 |
| **sum** | **152** | **4** | **48** | **79** | **21** | — |
| 对题对照 | 1388 | 10 | 579 | 689 | 110 | 46 |

- **池外召回判死**:全量 1540 池外 14 题(0.9%),错题池外 4 题(理论 +0.26pp)。
  "open-domain 短板=池外召回"的说法(含 [[locomo-9110-repro-verdict]] 的模糊表述与本仓库
  046 closure 的转述)是**误读**——open-domain 31 错里 23 题(74%)gold 在 k150 上下文内。
- **截断层残余**:21 题 rank 151-900(理论 +1.4pp)属加量型(top-k>150),已哲学否决。
- **错题 vs 对题的 rank 差距存在但不大**(中位 60-104 vs 46):检索质量与答对与否弱相关
  ——与 009/[[009-cattopk-verdict]] "深池排序不转化"的既有结论一致。
- **拒答口径**:tk150 全量 `force_answer=true`(answer_regime 字段实证),45 题 idk 形态错题
  是 force 下 answerer 仍写出不确定式回答——"开 force-answer"零成本杠杆**已用掉**
  ([[force-answer-regime-gap]] 的遗留问句在思考栈已闭合)。

## 剩余机制空间(2026-08-17 维护者裁决后收线)

1. ~~temporal 锚定契约 × Qwen thinking 栈确认~~ **维护者否决(2026-08-17)**:该机制的载体
   `--temporal-answer-prompt` 是 LoCoMo temporal 类别**特化提示词**(category-conditional
   prompt)——数据集/类别特化 prompt 不作为方案手段(与 046 spec "MUST NOT 引入数据集/
   问题级特化"红线同源)。032 的 flash 栈 GO 结论保留为历史事实,"生产栈确认"后续不做。
   temporal 20 题稳定 ±1 月偏移归入 answerer 能力带。
2. single-hop 显著记忆压制(~50 题):prompt 契约已第 3 次证伪([[answer-focus-prompt-verdict]]);
   上下文构造侧(030 GO 先例)的呈现顺序/结构未系统试过,但 022/026/030/031/033-035 证据
   装配线已密集探索,新先验不明。
3. open-domain 31 错:错因分散(翻转/模糊化/judge 粒度),[[90pp-direction-exploration]] §五
   已判"无单点杠杆,不建议主攻"。
4. judge 噪声带 ±1pp(1375-1390):90pp 探索已证 judge 杠杆用尽。

**总体判定:LoCoMo 杠杆线收线(2026-08-17)**——检索侧归因归零(池外 0.9%、错题主体
gold 在上下文答错),回答侧剩余错题(±1 月偏移/显著记忆压制/推断翻转/force 下拒答)
多次归因为 Qwen answerer 能力带([[thinking-curve-and-qwen-ceiling]] oracle 91.62% 贴顶),
类别特化 prompt 手段被红线排除。诚实锚:tk150 思考栈 clean 3-rep majority **91.10%**
([[locomo-9110-repro-verdict]]),生产配方(k30 unified)**87.9%**;新杠杆需等新证据
(更强 answerer / 架构性上下文构造),不在当前数据集上继续挖。

## 诚实边界

- 009 trace 是 009 时代 store/检索配方,非 tk150 栈的 032-store 快照——量级近似;方向性结论
  (池外可忽略、错题主体 answerer 侧)有 error-patterns 形态分类的独立交叉支撑。
- `gold_rank_pool` 口径 = 009 的 900 宽池 gold turn 映射;gold turn 映射失败会落"池外"
  (14 题含映射噪声,真实池外率只会更低)。
- wrong-list 为 raw-judge 152 错;clean 重判后 91.10% 基线下错题≈137,各类量级同比例微缩,
  不改变结论。
- 单 rep 桶内 rank 不可比 RRF 分数跨题尺度(本诊断只用 rank 不用分数)。

## 复现

```bash
# join 脚本(本机 session-scratch,一次性诊断):wrong-list.json × 009-attribution-fixed/trace.jsonl
# 数据:~/.claude/session-scratch/topk-err/{wrong-list.json,tk150-run-{1,2,3}.jsonl}
#      .locomo-run/009-attribution-fixed/trace.jsonl
```
