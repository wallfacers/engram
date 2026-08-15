# Implementation Plan: confidence-gated gap-guided deepening

**Branch**: `043-confidence-gated-deepen` | **Date**: 2026-08-15 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/043-confidence-gated-deepen/spec.md`

## Summary

在 cmd/locomo-bench harness 内实现"犹豫门控、缺口导向、单轮加深"读侧机制(默认关旗标):round-0 k30 + unified 契约字节不动;answerer 信号(logprob 置信三特征或文本犹豫,pilot 择优)低于阈值时,由 answerer 输出通用 schema 缺口,确定性拼接为补检查询,`Retriever.Search` 追加证据(去重、不动 round-0)后重答一次。前置 2-conv 信号质量 pilot(AUC≥0.65 硬门);LoCoMo 配对 3-rep clean 定分;LME 零重调迁移门。引擎(memory/ 等)零改动。

## Technical Context

**Language/Version**: Go 1.25.0,CGO_ENABLED=0(硬门)

**Primary Dependencies**: 现有 cmd/locomo-bench harness;`memory.Retriever.Search`(只调用不改);042 的 logprob 基础设施(`counterfactual_utility_http.go` 的 `utilityMapFinalSignal` 三冻结特征、`utilityLogprobCaller`);manifest/seal 工件模式(`counterfactual_utility_artifact.go`)

**Storage**: run-dir 下 jsonl artifact + manifest/seal(照抄 042 布局);`--store-dir` conversation store 复用

**Testing**: `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench`(纯函数单测:信号解析、gap schema 校验、查询映射、追加 union、AUC 计算);离线 golden 测默认路径逐字节一致

**Target Platform**: Linux(本地 + AutoDL eval box,box 侧只跑模型依赖实验)

**Project Type**: CLI 评测 harness 扩展(eval-only 机制,不进引擎)

**Performance Goals**: pilot(2-conv)本地/box 单次开机内完成;全量配对批吞吐对齐 042 水平(worker pool + `--concurrency`,硬规则:模型侧必须并行)

**Constraints**: 默认旗标关 = 主路径逐字节不变(FR-003);契约 digest `1d8a8d0f` 不变(`answerRegimeFingerprint` 自动拦截漂移);禁云端 reranker;box 一次开机合并跑(042 残余 + Step A unified×trace 臂 + 本 feature),跑完必关

**Scale/Scope**: LoCoMo 1,540 题 ×3 rep ×2 臂;LME 500 题 ×3 rep;每题最多 +1 轮补检 +1 次重答

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 条款 | 判定 | 依据 |
|---|---|---|
| I 本地优先/默认离线 | ✅ PASS | 机制纯 harness 读侧;补检索走本地 hybrid;answerer 是既有可替换 sidecar;默认旗标关 |
| II 引擎/适配器分离 | ✅ PASS | 只动 cmd/locomo-bench;`memory.Retriever.Search` 只调用;`git diff --name-only -- memory embedding provider store internal` 必须为空(tasks 里列为验收项) |
| III 契约优先/命名空间隔离 | ✅ PASS | 038 unified 契约字节不动(digest fingerprint 拦截);gap schema 是新输出格式契约,在 plan/contracts 冻结后才实现 |
| IV 评测回归门 | ✅ PASS | 本 feature 本身就是评测机制;008 铁律配对协议写入 tasks;eval-config 与算法分开 commit |
| V 优雅降级/诚实规模 | ✅ PASS | 信号解析失败→按自信不加深(不失败);补检失败→回退 round-0 答案;预算诚实报告(平均检索条数必报) |

**死亡规则(云端 reranker)**: ✅ 补检索复用本地 hybrid,无任何 reranker 依赖。

Phase 1 后复查:无新增违规。

## Project Structure

### Documentation (this feature)

```text
specs/043-confidence-gated-deepen/
├── plan.md              # 本文件
├── research.md          # Phase 0:R1-R8 决策
├── data-model.md        # Phase 1:GapItem/HesitationSignal/DeepenDecision
├── quickstart.md        # Phase 1
├── contracts/           # Phase 1:CLI 契约 + 工件 schema
│   ├── cli-flags.md
│   └── artifacts.md
└── tasks.md             # speckit-tasks 产出
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── confidence_deepen.go            # 新:GapItem schema 校验、信号解析(logprob 阈值 + 文本犹豫 lexicon)、
│                                   #   确定性 gap→query 映射、追加式 union(纯函数,无模型调用)
├── confidence_deepen_test.go       # 新:上述纯函数单测
├── confidence_deepen_pilot.go      # 新:2-conv 信号质量 pilot(stage 旗标早分派,照抄 runUtilityPilotStage 骨架:
│                                   #   manifest → runtime 预建 → pool 答题 → 双信号 AUC → GO/NO-GO seal)
├── confidence_deepen_pilot_test.go # 新:pilot 纯逻辑测试(AUC 计算、kill-gate、对照构造)
├── confidence_deepen_artifact.go   # 新:DeepenDecision jsonl + manifest/seal(照抄 counterfactual_utility_artifact.go)
├── main.go                          # 改:flag 区加 --confidence-deepen 等;options 字段;互斥校验;
│                                    #   answerAndJudgeWithAbstainEvidenceDiagnosticsQuery 内加 deepen 钩子;
│                                    #   answerRegimeFingerprint 追加 confidence_deepen 标记
├── runner.go                        # 改:文本犹豫检测放 isIDK 旁;不动 unifiedAnswerContractPrompt(:293)
└── unified_answer_contract_eval.go  # 改:validateUnifiedPromptPairExperiment 冲突表加 --confidence-deepen
```

**Structure Decision**: 单包(cmd/locomo-bench)内新文件 + 最小侵入改 3 个现有文件;检索/统计/parity/store 全部复用零改动。**不复用** `gap_retrieval.go` 的 `gapBudget`(N-r 拆分让补检吃 round-0 配额,是 021 的死因,FR-007 禁止);只参考其 `renderStructuredGapQuery`(确定性拼接)与 `stableGapCandidateUnion`(追加不删)两个函数形态。

## Complexity Tracking

> 无 Constitution Check 违规,无需豁免条目。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| — | — | — |

## 关键技术决策(承接 research.md)

1. **信号双测**(`research.md` R1):pilot 同时算 logprob 三特征(`final_mean_logprob` / `final_p10_logprob` / `final_mean_top1_top2_margin`,复用 `utilityMapFinalSignal`)与文本犹豫(新 lexicon)的 AUC,选高者;阈值取 pilot ROC 最优点,LoCoMo 定稿后 LME 零改动(R5)。
2. **答题通道**:机制臂答题走 042 的 `utilityLogprobCaller`(thinking-on、非流式、logprobs=true)以便取信号;对照臂维持现行 streaming 通道——**两条通道的 prompt 字节必须一致**(digest 校验),差异仅 stream/logprobs 参数。**thinking 开关必须两臂对齐**(analyze F1:主评测通道默认 `LOCOMO_NO_THINKING != "0"` = thinking off,与 logprob 通道的 thinking-on 是第二个变量):进 pilot 前先核实 87.9% 锚 run 的 thinking 配置,两臂统一到锚配置并写入 manifest;若锚为 thinking-off,则 logprob 通道也须以 thinking-off 跑(`ThinkingDisabled` 透传),thinking 开关差异不得引入。pilot 需先验证通道差异本身不改变答案分布(同题双通道对照,若翻转超噪声带则 NO-GO)。
3. **gap 输出获取**:重答前置一次"缺口产出"调用?否——缺口与信号同轮产出:answerer 的输出在 final answer 之外附 JSON 缺口块(输出格式契约,非新答题 prompt;契约字节冻结于 contracts/artifacts.md)。解析失败按自信处理(FR-001 场景 2)。
4. **追加 union**:补检结果按条目去重(与 round-0 id 集合比对)后**整体追加在 round-0 上下文之后**,不重排、不截断 round-0;round-0 chunk 配额在补检前后逐字节相同(校验项)。
5. **AUC kill-gate**:照抄 `utilityPilotGate` 模式;pilot 对照构造用 R8(「k30 错 k150 对」为正类,已有 042 配对 run 的 judge 结果离线对齐,零新标注)。
6. **clean 判题**:按 042/LME 先例做 box 侧脚本(判题零件复用 `runner.go:663-724`),产物 clean-*.json 入 run-dir;harness 内不新增在线 clean 判题路径。
7. **Step A 臂**(unified×trace-mediation,决策记忆)与 042 残余 box 任务并入同一次开机;本 feature 的 pilot 在同一次开机先跑(AUC 门不过则后续配对批不跑,直接省下)。

## 验收顺序(供 speckit-tasks 细化)

1. 纯函数层(本地,零模型):schema/映射/union/阈值/AUC 单测绿;默认旗标关 golden 逐字节一致。
2. pilot 层(box 第 1 段):2-conv 双信号 AUC + 通道一致性对照 → GO/NO-GO seal。NO-GO 则 feature 关闭,写 verdict 报告。
3. 机制层(box 第 2 段):LoCoMo 全量配对 3-rep(arm 交错、同批、store 复用)→ clean 重判 → ≥90.0% 判定(SC-002)+ 平均检索 ≤60(SC-003)。
4. 迁移层(box 第 3 段):LME 零重调配对 3-rep → 非回退 90.2% 锚(SC-004)。
5. 收尾:result-matrix 配对行 + verdict 文档 + manifest seal 一致性(冻结前填满字段再算 digest,硬规则)。
