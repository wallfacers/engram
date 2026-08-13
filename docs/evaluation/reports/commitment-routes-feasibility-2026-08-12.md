---
title: 反证据/时间戳路线可行性诊断：LoCoMo 137 + LME 77 错题归属量化
summary: 零代码启发式 + temporal 逐题人工归因，把确定性时间戳聚合、反证据检索两条候选路线映射到错题占比。LoCoMo temporal 主导机制是 event 锚定/推算错（38%），LME knowledge-update 多值冲突占 44%；历史 ~0.8–1pp 数字只是错因覆盖上限。后续 counter-refine+trace 混合臂为 −0.4pp 且独立效应未识别，ENTITY_SHIFT 的候选删除/排序也已收口。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [research, locomo, longmemeval, commitment-failure, diagnostic, feasibility]
---

# 反证据/时间戳路线可行性诊断（2026-08-12）

## 背景与问题

[commitment failure 审计](../../research/committed-failure-retrieval-wrong-answer.md) 给出两条从未试过的
候选路线：① 写侧确定性时间戳/序列元数据 + 读侧确定性 `max()`（把新旧比较移出 LLM）；
② 答题后反证据二次检索 + 验证门（CounterRefine）。本报告回答：**这两条路线在本栈错题上
分别能覆盖多大比例、哪条更可行**。

## 方法（零代码）

- 数据：LoCoMo 91.10% 栈 3-rep clean 重判 majority 错题 **137** 题 + LME baseline clean
  错题 **77** 题（`~/.claude/session-scratch/lme-rejudge/`）。
- 启发式分型（零模型调用）：gold 的「值指纹」（月份/年份/完整日期/duration/货币）是否在
  answerer 的 thinking+final 里**可见**。可见但选错 = 候选冲突；不可见 = 推算/覆盖。
- **temporal 类逐题人工归因**（LoCoMo 26 + LME 相关）——这是路线决策最关键的样本，
  启发式边界误差在人工下消除。
- 脚本：`classify_commitment.py` / `classify_commitment_v2.py`，输出
  `locomo-commitment-classified.json` / `lme-commitment-classified.json`。

## 结果一：LoCoMo 137 错题启发式分型

| 类 | 数 | 占比 | 含义 |
|---|---:|---:|---|
| ENTITY_SHIFT | 66 | 48% | gold 实体在 thinking 可见但答错对象/被显著记忆压制 |
| RETRIEVAL_GAP | 50 | 36% | gold 实体词不可见（仅对照，检索缺在 LME 已证伪） |
| MULTI_CONFLICT | 10 | 7% | gold 值可见但选错候选 |
| INFER_CALC | 8 | 6% | 推算/聚合错，gold 值不可见 |
| UNKNOWN | 3 | 2% | — |

## 结果二：LoCoMo temporal 26 题人工归因（路线①靶心）

| 机制 | 题数 | 代表题 |
|---|---:|---|
| **候选冲突**（gold 值在 thinking 可见仍选错） | **6–7** | conv-3-q-22 / conv-3-q-54 / conv-4-q-55 / conv-7-q-17 / conv-7-q-43 / conv-9-q-48（+conv-5-q-16） |
| **event 锚定错**（answerer 守 harness `[event:]` 规则信了标记，gold 靠文本语义推算） | 5 | conv-2-q-48 / conv-3-q-41 / conv-4-q-10 / conv-4-q-68 / conv-8-q-80 |
| **纯推算/聚合错**（gold 值从未进 thinking） | 5 | conv-4-q-16 / conv-6-q-31 / conv-8-q-39 / conv-9-q-0 / conv-9-q-61 |
| **实体覆盖/找错对象**（gold 记忆未进 thinking 或未关联） | 8 | conv-4-q-20 / conv-5-q-55 / conv-6-q-53 / conv-8-q-24 / conv-8-q-60 / conv-9-q-27 / conv-9-q-67 / conv-6-q-11 |
| judge 边缘 | 1 | conv-5-q-47（one month vs 1 month） |

**关键机制观察**：
- answerer 高度依赖 harness 的 "read the time from [event: YYYY-MM-DD]" 规则（032 tplan 遗产）。
  但 LoCoMo 的 `[event:]` 标记常是 **conversation turn 日期而非问题所指事件日期**，导致
  conv-4-q-10（文本 "next month" 被忽略、信 event 07-16）、conv-8-q-80（event 2024 vs gold 2023）
  这类**信标记反而错**。
- conv-3-q-54：gold 关键线索 "Sunday prior to October 25, 2022" 在 thinking **最后一行可见却
  完全忽略**，选了第二个剧本——经典 commitment failure，反证据路线可救。
- 候选冲突题里多数是「选哪条记忆」，不是「新旧值冲突」——确定性 `max(timestamp)` 不一定直接命中。

## 结果三：LME knowledge-update 16 题人工归因

| 机制 | 题数 | 代表题 |
|---|---:|---|
| **真多值冲突**（同一槽位多候选、时序/状态选择错） | **7** | 830ce83f（suburbs vs Chicago）/ 50635ada（Silver vs Gold）/ 9ea5eabc（Paris vs Hawaii）/ 852ce960（$400k vs $350k）/ 6a1eabeb（25:50 vs 27:12）/ f685340e（weekly vs every other week）/ 10e09553 |
| ABS 拒答边界（answerer 未按 ABS 拒答或槽位替换） | 5 | 2133c1b5 / 0ddfec37 / f685340e_abs 等 |
| 覆盖（gold 值未进 thinking） | 3 | 7e974930（$420 未出现）/ 07741c45 / 69fee5aa（38 未出现） |
| 边界 | 1 | 031748ae |

**LME knowledge-update 才是多值冲突主导（44%）**，且多为「时序/状态选择」（previous vs current、
更近 vs 更早）——正是确定性时间戳/聚合路线的靶心。但 LME 数据**没有** LoCoMo 的 `[event:]` 标记，
engram 写侧（chunk 表示）也无 fact 粒度时间戳，需先抽取——而 event 表示抽取正是 **027/028 已
证伪方向**（7B 抽取仅 5% 锚定，e2e −26.2pp）。

## 两条路线可行性结论

| 路线 | LoCoMo 可救 | LME 可救 | 合计上限 | 主要障碍 |
|---|---:|---:|---:|---|
| ① 确定性时间戳/聚合 | ~6–7/137（0.4pp 量级） | ~7/77（1.4pp 量级） | ~13 题 ≈ 0.6–0.9pp | 写侧无可靠 fact 时间戳（027/028 教训）；LoCoMo 方向可能反（信 event 反而错） |
| ② 反证据检索+验证 | ~6–10/137 | ~7/77 | ~13–17 题 ≈ 0.8–1.0pp | 需 LLM refine 调用（+1 call/题，非纯 Go）；收益在长尾；只救候选冲突类 |

**两条路线都不是快赢**：
- 路线①受制于写侧元数据不可靠，且 LoCoMo 上「读侧信 event」本身已被证明会固化错误
  （conv-4-q-10/conv-8-q-80 就是信 event 才错）；LME 上虽有 44% 靶心但需先解决抽取能力。
- 路线②收益上限 ~1pp，且是 LLM 调用型（违反「纯 Go 确定性优先」哲学倾向），机制上只覆盖
  候选冲突类。

## 意外发现：ENTITY_SHIFT 是两数据集错题大头

- LoCoMo 48% / LME 42% 的错题是 **ENTITY_SHIFT**：gold 实体在 thinking 可见（answerer 见过
  正确记忆的上下文）但最终答错对象/被人物显著记忆带跑（conv-9-q-151 gold=Photography
  pred="Fixing cars"；conv-2-q-128 gold=hiking pred=Picnic）。
- 这类错题**两条路线都不覆盖**：不是值冲突、不是可证伪断言，是**长上下文中「显著记忆压制
  精确对齐」的选择问题**（画像文档机制三）。它对应文献 Separating Semantic Competition
  （2605.27294）「竞争者越多越有害」——潜在方向是**减竞争噪音**（而非加元数据/加检索），
  但与大池 top-k150（已证 +4.4pp）存在张力，需先诊断再动。

## 建议下一步（按可行性与成本排序）

1. **不投入时间戳写侧抽取**（027/028 已两次证伪 event 表示；LoCoMo 方向可能反）。
2. **反证据路线后续（2026-08-13）**：小样本曾有 2/10 救回；Qwen LME 500 的
  `counter-refine + trace` 组合臂为 432/500，对 trace-off baseline 434/500，`p=0.8776`。组合配方
  无正向信号，但 trace 未对齐使单机制效应不可识别。基于额外调用成本停止投入；若未来需要因果裁决，
  必须补 trace-off 的完全配对实验。见 [全量组合臂报告](counter-refine-verdict-2026-08-13.md)。
3. **ENTITY_SHIFT 后续（2026-08-12）**：竞争密度诊断虽显著，但候选删除先导出现误伤、非删除排序
  没有改变答案，候选侧减竞争路线已收口，不再作为当前主线。若未来重开，应提出不同于删除/排序的
  新机制并重新预注册，不能沿用本报告的错因覆盖率声称收益。
4. 全部机制未经 eval 验证前**禁止声称涨点**（宪法 IV）。

## 诚实边界

- LoCoMo 137 错题的启发式分型含边界误差；temporal 26 题 + LME knowledge-update 16 题为
  人工逐题归因（读 thinking），可信度高；其余类别为启发式估算。
- 「可救题」是**机制上限估算**，非实测——表示该题错因类型落在路线射程内，不代表路线必救它。
- LME 部分错题依赖 ABS 判定边界，与机制无关。
- 分析基于 3-rep clean 重判数据（`~/.claude/session-scratch/lme-rejudge/`，脚本与
  classified.json 同目录，未入库）。
