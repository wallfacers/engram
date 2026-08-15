# Tasks: confidence-gated gap-guided deepening (043)

**Generated**: 2026-08-15 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

任务按 plan.md 验收顺序五段组织:纯函数层(本地) → pilot(Story 1) → 机制配对批(Story 2) → LME 迁移(Story 3) → 收尾。测试为内建要求(工作规则 test-first;008 铁律)。

## Phase 1 · Setup

- [ ] T001 在 cmd/locomo-bench/main.go flag 区新增 --confidence-deepen(false)/--deepen-pilot("")/--deepen-threshold(0)/--deepen-signal-feature("")/--deepen-k(30)/--deepen-max-gaps(3),options struct 加对应字段;--deepen-pilot 走 --utility-stage 同款早期分派模式(main.go:612 附近)
- [ ] T002 在 cmd/locomo-bench/unified_answer_contract_eval.go validateUnifiedPromptPairExperiment 冲突表加 --confidence-deepen 与 --gap-refetch/--agentic-nav/--iris/multi-query 的互斥;并校验 --confidence-deepen 必须与 --unified-answer-contract 同开
- [ ] T003 新建 cmd/locomo-bench/confidence_deepen_artifact.go:DeepenDecision/pilot-report/manifest+seal 工件函数(照抄 counterfactual_utility_artifact.go 的 digest/Write/Read/ValidateSeal 模式);manifest 全字段(含 QuestionCount/threshold/featureName/contract_digest)填满后才算 digest

## Phase 2 · Foundational(纯函数层,本地零模型)

- [ ] T004 [P] 新建 cmd/locomo-bench/confidence_deepen.go:GapItem 类型 + schema 校验(category 枚举、长度上限、每题 ≤3 条)+ <DEEPEN_META> 块解析(失败=按自信,记 failure_kind)
- [ ] T005 [P] 在 cmd/locomo-bench/confidence_deepen.go 实现 gapQueryFor(gaps, question) 确定性映射(target+slot → description → target → 原问题),纯字符串操作
- [ ] T006 [P] 在 cmd/locomo-bench/confidence_deepen.go 实现追加式 union appendDedup(round0 []memory.Result, extra []memory.Result)——按 id 去重、只追加、不改 round-0 顺序(参考 stableGapCandidateUnion 形态;禁止任何配额拆分)
- [ ] T007 [P] 在 cmd/locomo-bench/confidence_deepen.go 实现 AUC 计算(排序法 + CI bootstrap,固定种子)与阈值选择(ROC 最优点);在 runner.go isIDK 旁实现文本犹豫 lexicon 检测(与 logprob 信号并列的 HesitationSignal kind=textual)
- [ ] T008 [P] 新建 cmd/locomo-bench/confidence_deepen_test.go:TDD——先写失败测试再实现 T004-T007(schema 拒绝枚举外/超条数、映射确定性锁定表驱动用例、union 去重且 round-0 序不变、AUC 已知小样本手算值、lexicon 命中)
- [ ] T009 在 cmd/locomo-bench 加默认旗标关 golden 测试:--confidence-deepen=false 时主路径 prompt/上下文/检索逐字节与现行一致(可对照现有 journal golden 或新增最小 golden);CGO_ENABLED=0 go build ./... && go test -count=1 ./cmd/locomo-bench 全绿

## Phase 3 · User Story 1(犹豫信号 pilot,box 第 1 段)

- [ ] T010 [US1] 新建 cmd/locomo-bench/confidence_deepen_pilot.go:--deepen-pilot signal stage(照抄 runUtilityPilotStage 骨架:manifest → buildConversationRuntime 预建 → worker pool(--concurrency,硬规则并行)→ 前 2 conv 逐题 k30 答题)
- [ ] T011 [US1] pilot 内双信号采集:logprob 三特征走 utilityLogprobCaller + utilityMapFinalSignal 复用;文本犹豫走 T007 lexicon;每题记 answer-attempts.jsonl(含双信号值与解析状态)
- [ ] T012 [US1] pilot 对照构造(R8):与既有 042 k150 配对 run 的 judge 结果离线对齐,「k30 错 k150 对」=正类;**输入已到位(2026-08-15 确认):本地 `.locomo-run/042-20260815/`(stats.json + 全部 report/seal/manifest + labels,28M;box 备份在数据盘 eval-backup-20260815/042-runs 但 box 已断电,以本地为准);pilot stage 开跑前校验本地 judge/labels 文件存在**;输出双信号 AUC + 解析覆盖率到 pilot-report.json
- [ ] T013 [US1] 通道一致性对照:**先核实 87.9% 锚 run 的 thinking 配置并写入 manifest(analyze F1)**,两臂(含 logprob 通道)统一到锚配置(锚若为 thinking-off 则 logprob 通道透传 ThinkingDisabled);同题双通道(streaming vs logprob 非流式,prompt 字节一致、thinking 一致)答案比对,flip_rate 入 pilot-report;kill-gate = AUC≥0.65 且 flip_rate 在噪声带内,产 GO/NO-GO seal(照抄 utilityPilotGate 模式)
- [ ] T014 [US1] 新建 cmd/locomo-bench/confidence_deepen_pilot_test.go:pilot 纯逻辑测试(kill-gate 边界、对照构造、report schema);本地全绿后 box 执行 pilot,NO-GO 则写 verdict 报告并停止后续所有 phase

## Phase 4 · User Story 2(机制配对批,box 第 2 段)

- [ ] T015 [US2] 在 cmd/locomo-bench/main.go answerAndJudgeWithAbstainEvidenceDiagnosticsQuery 加 deepen 钩子:round-0 答题(契约字节不动)→ 读 pilot seal 的 threshold/featureName(命令行显式传非定稿值报错)→ 触发时经 <DEEPEN_META> 解析 gap → gapQueryFor → retriever.Search(ctx, q, --deepen-k) → appendDedup → 重答一次
- [ ] T016 [US2] 降级路径:outcome_kind 全枚举覆盖(信号不可得/gap 解析失败/查询空回退原问题/检索错误或空→回退 round-0 答案,不重试不报错);judge 输入剥离 <DEEPEN_META> 块(clean 口径不受污染)
- [ ] T017 [US2] 审计:每题写 DeepenDecision jsonl(含 round0_context_digest 补检前后一致性校验、两轮答案 digest、final_from_deepen);answerRegimeFingerprint 追加 confidence_deepen 标记防 journal 串档;supportedArmMechanisms/optionsForArm 加 "deepen" 臂(hybrid+unified+deepen)
- [ ] T018 [US2] 单测:钩子旗标关零执行(T009 golden 扩展)、**机制臂(旗标开)的 answer_prompt_digest 与对照臂逐字节相等断言(analyze F3)**、触发/不触发/各 outcome_kind 路径表驱动测试;本地全绿
- [ ] T019 [US2] box 执行 LoCoMo 全量配对批:--retrieval hybrid,hybrid+unified+deepen --repeats 3 --store-dir 复用,worker pool 并行;跑后 clean 重判(box 脚本,042/LME 先例)
- [ ] T020 [US2] 判定:clean 3-rep majority ≥90.0% 且 above-noise(McNemar vs 对照臂)且 avg_retrieved_items ≤60;任一不达 ⇒ verdict NO-GO 收尾;达成 ⇒ 进 Phase 5

## Phase 5 · User Story 3(LME 零重调迁移,box 第 3 段)

- [ ] T021 [US3] box 执行 LME 同配方(阈值/特征/k 零改动),机制臂 vs 对照臂 3-rep 配对 + clean 重判
- [ ] T022 [US3] 迁移门判定:机制臂不显著低于 90.2% 锚;若回退,不得回改参数(FR-010)——回退即 verdict 记迁移失败

## Phase 6 · Polish & 收尾

- [ ] T023 result-matrix.md 登记配对行(得分/p/avg_retrieved/context parity/逐题翻转清单);docs/evaluation/ 写 043 verdict 文档
- [ ] T024 引擎零改动验收:git diff --name-only -- memory embedding provider store internal 为空;CGO_ENABLED=0 go build ./... && go test -count=1 ./... 全绿
- [ ] T025 eval-config 改动与算法改动分开 commit(宪法 IV);manifest seal 一致性自查(冻结前字段全填)
- [ ] T026 box 收尾:小文件备份到 /root/autodl-tmp/eval-backup-<ts>/ → 关机(必做);更新 auto-memory(pilot/机制/迁移三段结论)

## Dependencies(故事完成顺序)

```
T001-T003(Setup) → T004-T009(纯函数,可并行) → US1(T010-T014)
  └─ NO-GO ⇒ T023/T024/T026 收尾即止
US1 GO → US2(T015-T020) → US3(T021-T022) → Polish(T023-T026)
```

- US1/US2/US3 串行(后者的输入是前者 seal);Phase 2 内 T004-T008 全部可并行(不同文件/同文件不同函数,建议单人顺序执行避免冲突,机器并行无收益)。
- MVP = T001-T014(US1):pilot 单独交付 go/no-go 判定,不含任何机制代码即可止损。

## Parallel execution examples

- T004/T005/T006/T007 同在 confidence_deepen.go,逻辑无关可按序快速完成;T008 测试文件与其并行编写。
- US2 的 T015/T016/T017 有依赖(同一钩子函数),必须顺序;T018 与 T019(本地/box)可并行准备。

## Implementation strategy

MVP 优先(US1 止损门)→ 机制实现(US2)→ 迁移(US3)。任何一段 NO-GO 立即收尾写 verdict,不进入下一段;box 全部模型侧实验合并一次开机,跑完必关。
