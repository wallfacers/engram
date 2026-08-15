# Tasks: 确定性次模证据装填(045)

**Generated**: 2026-08-16 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

任务按 plan 验收顺序:纯函数层(本地零模型)→ US1 离线门(本地)→ US2 接线 + probe(box)→ US3/US4(条件执行)→ 重验 ride-along + 收尾。**红线**:引擎五目录零改动、默认路径 byte-parity golden、四项权重零重调(默认 3:1:1:1 从 probe 到 LME 不动)、manifest 冻结前填满字段、box 组合批一次开机跑完即关、044 并行清理自包含纪律(不 import 其删除目标)。

## Phase 1 · 基线确认

- [ ] T001 在 045 worktree 确认基线:`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空;`git worktree list` 复核并行 sibling(044-default-off-cleanup)接触面无新增变化

## Phase 2 · 纯函数层(本地零模型,[P] 并行)

- [ ] T002 [P] 实现 `cmd/locomo-bench/submodular_packing.go` 基础件:`packEstimateTokens`(rune/4,≥1,含与 agentic_nav.estimateTokens 等价性单测)、`buildPoolCandidate`(ID/Kind/FusedScore/NormScore 池内 min-max/Shingles 词级 5-shingle FNV-1a/CoverTerms)、`packSimilarityMatrix`(shingle-Jaccard,确定性)
- [ ] T003 [P] 实现四项目标 + cost-scaled 贪心(同文件):权重 3:1:1:1 默认、增量维护、预算停、tie-break stable ID 升序、singleton fallback(仅此允许超预算,审计位);性质单测:预算硬上界、贪心单调性、确定性(同输入两跑逐字节一致)
- [ ] T004 [P] 实现 `cmd/locomo-bench/aic.go`:冻结规范化(lower+collapse-ws+子串)、多别名 any-match、UnmatchableInPool 审计;单测覆盖中英混排/空白/别名/池内不可匹配
- [ ] T005 [P] 实现 `cmd/locomo-bench/reverify_042.go` 核心件(与 T002-T004 并行):自包含 logprob 调用器(OpenAI 兼容 chat/completions + logprobs,`temperature=0` 显式体)、final-span 鲁棒映射(剥 `<|im_end|>` 等特殊 token → 最后关闭符后取 span → mean/p10/top1-top2 三特征,公式复刻 1eb9cdd,含等价性回归测试 fixtures);**不 import** counterfactual_utility*.go / confidence_deepen*.go

## Phase 3 · US1 离线装填保真门(本地,US1)

- [ ] T006 [US1] 实现 aic-gate 子命令 `cmd/locomo-bench/submodular_packing_cli.go`:三口径渲染(current-k30 / packed / top150-full)、逐题预算锚离线复算(对照渲染确定性)、门判定(packed.aic ≥ 0.95×top150 且 tokens ≤ 锚)、packing_gate.json + packing_audit.jsonl + manifest 冻结后 seal;`--slice`/`--full` 参数
- [ ] T007 [US1] 本地执行:先 `--slice 0,1`(304 题)冒烟 → `--full`(1540)全量门;核验审计(unmatchable 单列、singleton 计数);产出 go/no-go 判定。**NO-GO 分支**:写关闭 verdict 文档,勾结 T016-T017 收尾,Phase 4-6 不执行

## Phase 4 · US2 机制接线 + 1-rep 配对 probe(box,US2)

- [ ] T008 [US2] **先写**默认关 golden 测试(旗标关 → 现行配方逐字节一致;TDD 红-绿);`cmd/locomo-bench/submodular_packing_test.go`
- [ ] T009 [US2] 接线:main.go 旗标注册段(`--submodular-pack/--pack-pool-size/--pack-weights/--pack-budget-anchor`,contracts v1)+ unified_answer_contract_eval.go 冲突表新行(与 trace/consolidate/nav/iris/utility-stage 互斥 fail-closed)+ eval_runner.go 分派 + chunks.go 插点(旗标开 → packSelect 替代 applyChunkQuota)+ 逐题配对预算锚注入(anchor-run 读取);`CGO_ENABLED=0 go test -count=1 ./...` 绿
- [ ] T010 [US2] box 组合批第 1 段:对照臂 1-rep(现行配方,收逐题 usage 锚)→ 机制臂 1-rep probe(--submodular-pack --pack-budget-anchor paired);worker pool 遵守 --concurrency;产出 probe_paired.json(配对差 + McNemar exact p + 真实 usage token parity + 装填审计);GO = 配对差≥0 且不显著负。**NO-GO 分支**:verdict 收尾,Phase 5-6 不执行

## Phase 5 · US3 正批(条件:T010 GO,US3)

- [ ] T011 [US3] 同一次开机顺序执行 3-rep clean 正批(机制臂 vs 对照臂同批配对、store 复用、clean 判题);产出 result-matrix 行(净差、p 值、context parity、逐题翻转清单、平均检索条数);SC-003 判定(≥90.0% 或诚实关闭)

## Phase 6 · US4 LME 零重调迁移(条件:T011 完成且未关闭,US4)

- [ ] T012 [US4] LME(k30 unified clean 3-rep)零重调迁移批:LoCoMo 定稿参数原样上 LME;非回退 90.2% 锚即过;产出迁移门判定 + 双数据集汇总行

## Phase 7 · 重验 ride-along + 收尾

- [ ] T013 [P] reverify 子命令 CLI(`cmd/locomo-bench/reverify_042.go` 补齐):--labels 读 042 collect 工件、2-conv slice(conv 0/1,304 题,与 043 pilot2 可比)、worker pool 遵守 --concurrency、ReverifyReport 工件(AUC WMW tie-mean + bootstrap seed 43 + 双通道 flip);端点不可达 → inconclusive 不阻塞主批
- [ ] T014 [P] 全量门复核:`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...`、`CGO_ENABLED=0 go vet ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空;新旗标出现在 `--help` 且默认值正确
- [ ] T015 box 组合批第 2 段:执行 reverify(同批,answerer+judge env 走进程环境)→ ReverifyReport 判定(measurement-artifact-confirmed / signal-still-invalid / inconclusive);**只陈述测量事实,翻案权留维护者**
- [ ] T016 verdict 文档 `docs/evaluation/reports/045-submodular-packing-verdict-<date>.md`(含门链全程:US1→probe→正批→LME→重验)+ result-matrix 同步 + tasks 勾结
- [ ] T017 box 收尾:小文件备份 `/root/autodl-tmp/eval-backup-<ts>/` → vllm 按 PID 停 → `shutdown now`(必做,省钱铁律)

## Dependencies

```
T001 → T002/T003/T004/T005(纯函数层,可并行)
T002+T003+T004 → T006 → T007(US1 门)
T007 GO → T008 → T009 → T010(probe)
T010 GO → T011 → T012(条件链)
T005 → T013(重验 CLI,可与 Phase 3-4 并行开发)
T015 依赖 T013 + box 开机窗口(与 T010-T012 同批)
T016/T017 收尾(无条件执行)
```

- US1 NO-GO → T008-T012 跳过;probe NO-GO → T011-T012 跳过;每级关闭都写 verdict。
- T013-T015(重验)不依赖 045 主链的 GO/NO-GO——即使装填早关,重验照常同批执行(它回答的是 042 的测量问题)。

## Parallel execution examples

- T002/T003/T004/T005 四件互不依赖(不同新文件),可四窗口并行。
- T013(重验 CLI)与 Phase 3/4 全程并行。
- T014 在每个接线 commit 后局部跑,终值在收尾前跑。

## Implementation strategy

MVP = US1(T001-T007):纯本地、零模型、零 box,独立交付 go/no-go。此后每一级都是条件执行的门(US1 门 → probe 门 → SC-003 → 迁移门),任何一级失败即诚实关闭并保留工件;重验 ride-along 与主链解耦,同批开机执行。box 侧一切实验合并一次开机,跑完即关。
