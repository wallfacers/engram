---
title: 023 训练式 Evidence Planner — STOP verdict(e2e 不再验证,文档级收口)
summary: 023 训练线关闭:训练与人审交付达标(r5 99.5%)但 e2e 三臂评测(T018-T020)不再执行——判例先验(028 US2 训练中间指标 96.9%/100% e2e −1.2pp 未转化、037 US2 同型、026 查询时编译器 −4.5/−3.6pp)+ 装配层六连证伪(008/014/037/040/041/045)+ 2026-08-17 归因(152 错题 90% gold 已在上下文,装配层无错可纠)三重否证其 Δ≥+2.0pp GO 门;预注册预测(experiment-verdicts 023 行"最可能 HOLD/STOP/NOT_NEEDED")应验。产物留 HF 归档,恢复点=先零成本分诊 r5 proposal 救分潜力再谈三臂。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-17
tags: [evaluation, verdict, 023, planner, training, stop]
---

# 023 训练式 Evidence Planner — STOP verdict(2026-08-17)

**一句话结论**:**STOP**。r5 训练产物已达标交付(合同符合率/人审 99.5%),但 e2e 三臂评测
(T018-T020)不再执行——GO 门(Primary Cohort majority Δ≥+2.0pp)被三重独立证据否证到
"花 box+API 钱验证一个先验极差的机制"不再合理。训练线(028 抽取器 / 037 reranker / 023
planner)三连未转化,与装配层六连证伪同日收口。

## 交付现状(已完成的,如实记账)

| 项 | 状态 | 证据 |
|---|---|---|
| 训练数据 r5 | ✅ | HF `engram-023-planner/data/train-r5` |
| QLoRA 训练 + adapter | ✅ | HF `wallfacers/engram-planner-lora`,代码已 push |
| T011 人审(≥200 样本,语义充分率 ≥95%) | ✅ **99.5%**(199/200 pass) | `specs/023-local-trained-evidence-compiler/audit/review-r5/`(200 条 verdicts,1 fail) |
| harness 接入点 | ✅ | `cmd/locomo-bench/local_planner.go`(proposal-only,fail-closed,023 前置交付) |
| 早期 pilot e2e | ⛔ STOP | residual 149/149 `planner_error`(proposal 质量硬伤,非超时)——r5 重训的直接动因 |

## STOP 依据(三重独立证据,全部为已入库事实)

1. **同型判例(训练线)**:028 US2 训练抽取器把中间指标打到锚定 96.9%/schema 100%,e2e
   **−1.2pp 未转化**(蒸馏上限=教师);037 US2 自训 reranker e2e −1.1pp。023 当前状态
   (中间指标 99.5%、e2e 未验)与两者停摆前的状态同构。**训练中间指标不转化已在两个
   独立机制上应验。**
2. **同族判例(装配层)**:planner 编译的是"检索后、答题前"的装配层——008 rerank /
   014 assoc / 037 rerank / 040 截断 / 041 门控 / 045 装填六连 NO-GO,外加 026 查询时
   编译器 −4.5/−3.6pp 直接是 compiler 侧先例。
3. **今日归因(机制解释)**:[locomo-error-retrieval-attribution-2026-08-17](locomo-error-retrieval-attribution-2026-08-17.md)
   ——152 错题 90% 是 gold 已在上下文仍答错,装配层**无错可纠**;装配可作用面(rank
   31-900)仅 ~100 题,该面上"改善排序/选择不转化"已被六连证伪。

**预注册应验**:experiment-verdicts 023 行(2026-08 起)即预测"涨点收益受 compiler-eligible
residual 规模限制,最可能 **HOLD/STOP/NOT_NEEDED**"。本 verdict 是该预测的收口,不是新结论。

## 未执行任务处置(tasks.md 勾结同步)

- **T016(prompt-only 对照)/ T017(合并 LoRA 冻结产物)/ T018-T020(bridge+三臂+统计)/
  T021(推荐门)/ T022(model card)/ T023(结果登记)**:CLOSED — not executed,随本
  verdict 关闭;重开条件见下。
- T011 补勾(实际 2026-08 已完成,99.5%)。

## 资产保留与恢复点

- **保留**:HF 训练产物与数据(归档不动)、`local_planner.go` adapter、022
  `memory/evidencecompiler` 引擎合同(其 extractive 路径的正式契约价值独立于本 verdict)、
  `specs/023/residual-cohort.json`(149 题)。
- **恢复点(若未来重启,按序)**:①零成本分诊——本机 CPU(ollama)serve r5,对
  residual-cohort 149 题生成 proposal,判"有效救分潜力"(pilot 的 149/149 error 是否已被
  r5 真正修复);②分诊显著正 → 才谈 box 三臂。跳过①直接开机的三臂不被本 verdict 支持。
- 生产接线(023 plan.md"后续工作"段)随本 verdict 一并搁置——接线对象(planner)未证 GO;
  若只接确定性 Compiler 属 022 线的独立决策,不在 023 范围。

## 对外口径

023 不报任何分数(未跑 e2e);不宣称"planner 无效"(未测),只宣称"不值得再花验证成本"
(判例+归因+预注册三重依据)。诚实锚不变:LoCoMo tk150 clean 3-rep **91.10%** / 生产配方
k30 unified **87.9%**(2026-08-17 杠杆线收线口径)。
