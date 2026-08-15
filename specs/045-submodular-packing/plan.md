# Implementation Plan: 确定性次模证据装填(045)

**Branch**: `045-submodular-packing` | **Date**: 2026-08-16 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/045-submodular-packing/spec.md`

## Summary

k30 unified clean 87.9% → 90pp+ 的 P2 候选机制:在**已存在的宽检索池**(≥300,harness 现状)上,用**确定性预算化贪心次模选择**替代 `applyChunkQuota` 的配额截断——四项目标(relevance 模块项主导 + query set-cover + facility-location 代表性 + concave 多样性,sim 用词法 shingle-Jaccard),token 预算逐题锚定对照臂实际体量(体量配对)。止损门前置:US1 离线装填保真门(answer-in-context ≥ top-150 装配的 95%,零模型零 box);e2e 1-rep 同批配对 probe(Step A 协议)GO 才 3-rep clean 正批 + LME 零重调迁移。组合批 ride-along:042 信号重验(自包含,不依赖 044 清理删除目标)。

## Technical Context

**Language/Version**: Go 1.25.0,CGO_ENABLED=0 硬门,纯客户端

**Primary Dependencies**: 无新增——只用现有 `memory.Retriever.SearchWithDiagnostics`、`memory.Result.Score`(RRF 融合分)、标准库;零模型调用(装填层与 AIC 门全程离线)

**Storage**: 复用已存 032-store(LoCoMo `.locomo-run/` 本地;box 侧 `/root/autodl-tmp/`),只读

**Testing**: `go test ./cmd/locomo-bench`(纯函数单测 + golden 默认关字节等价 + 估计器/映射等价性回归);US1 离线门本身就是可重复执行的集成测试面(本地跑,无网络)

**Target Platform**: 本地 Linux(US1)/ AutoDL box(US2-US4 与 ride-along)

**Project Type**: 评测 harness 增强(cmd/locomo-bench),非产品代码

**Performance Goals**: US1 全量 1540 题 ≤ 分钟级(R7 估算);e2e 装填层每题 <10ms;新增模型调用路径(重验)走 worker pool 遵守 --concurrency

**Constraints**: 引擎五目录零改动;默认路径 byte-parity(golden 锁);044 并行清理的自包含纪律(FR-013);manifest 冻结前填满字段;凭证只走 env

**Scale/Scope**: 单用户规模;池 ≤300/题、选中集 ≤~40/题、1540 题

## Constitution Check

| 原则 | 门 | 结论 |
|---|---|---|
| I Local-first/offline | 装填层 + AIC 门 + 重验测量全本地;US1 零网络 | ✅ |
| II Engine/adapter | 引擎零改动(R2/R8:选择层全在 harness;向量不可得 → 词法替代而非引擎改动) | ✅ |
| III Contract-first | 本 plan + contracts/cli-flags.md 冻结旗标与工件 schema 后才动代码 | ✅ |
| IV Eval regression gate | 旗标默认关 = 默认路径不变;开臂全走 008 铁律配对;eval-config 与算法分 commit | ✅ |
| V Graceful degradation | embedding 缺失→词法替代天然免疫;池空→整题回退现行装配;AIC 匹配审计单列不静默 | ✅ |

无 violation,Complexity Tracking 表空。

## Project Structure

### Documentation (this feature)

```text
specs/045-submodular-packing/
├── plan.md              # 本文件
├── research.md          # R1-R8 决策与代码实证
├── data-model.md        # EvidencePool/PackingObjective/PackedContext/AnswerInContext/ReverifyReport
├── contracts/
│   └── cli-flags.md     # 旗标契约 + 工件 schema
├── quickstart.md        # US1 本地跑法 / box 批跑法
└── tasks.md             # /speckit-tasks 生成
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── chunks.go                    # [只加分支] retrieveWithQuotaDiagnostics:旗标开 → packPaths 替代 applyChunkQuota
├── main.go                      # [只加旗标] --submodular-pack 族注册(新段,不碰 044 删除段)
├── eval_runner.go               # [只加分派] 机制 arm 路由一行 + per-question 预算锚注入
├── submodular_packing.go        # 新:池结构/四项目标/cost-scaled 贪心/singleton fallback/tie-break
├── submodular_packing_cli.go    # 新:US1 离线保真门 CLI(全量跑三口径 AIC + 审计 + 判定)
├── aic.go                       # 新:answer-in-context 指标(冻结规范化 + 审计)
├── reverify_042.go              # 新:042 信号重验(自包含 logprob 调用器 + final-span 映射 + slice 驱动)
├── submodular_packing_test.go   # 新:纯函数单测 + 确定性(tie-break/种子无关)单测
├── aic_test.go                  # 新:规范化/别名/审计单测
└── reverify_042_test.go         # 新:估计器/映射与 1eb9cdd 公式的等价性回归(本地 fixtures)
```

**Structure Decision**: 全部落在 cmd/locomo-bench(评测 harness),零引擎文件;共享文件只加不改既有行(chunks.go 一个分支、main.go 旗标段、eval_runner.go 一行分派)——与 044 清理的删除目标无交集。

## 关键技术决策(plan 冻结)

1. **插入点**:`retrieveWithQuotaDiagnostics` 的宽池之后——`if packOpts 启用 { return packSelect(wide, budget, …), diagnostics, nil }`,否则原路径字节不变(R1)。
2. **预算锚协议**: probe/正批先跑对照臂(现行配方)收逐题 `usage.InputTokens`,机制臂逐题 `B_q =` 该值(缺失 → 对照臂全局均值);离线门同公式复算对照渲染(R4)。锚值入 manifest 冻结。
3. **四项权重起点 3:1:1:1**(relevance:set-cover:facility:diversity),LoCoMo probe 阶段不许调(零重调红线从 probe 就生效;正批与 LME 用同一定稿值)。
4. **shingle k=5 词级、min-max 池内归一 RRF 分、tie-break stable ID 升序**(R2/R3)。
5. **AIC 规范化** R5 冻结;**singleton fallback 允许单条超预算仅此例外**(R3)。
6. **重验**:2-conv slice = 043 pilot2 同 slice(conv 0/1,304 题),与 043 的 AUC 口径直接可比(信号有效性对比的公平性);`temperature=0` 显式;logprob 非流式 + 流式双通道一致性复测(flip 口径同 043)。
7. **manifest 纪律**:packing 工件(pool/audit/预算锚/AIC 门)在 seal 前填满全部字段再算 digest(工程铁律)。
8. **合并顺序**:与 044 无逻辑耦合;044 需 rebase 到 e6625d8 重跑其 T001(已标记维护者),045 侧不等待。

## Phases

- **Phase A(纯函数层,本地零模型)**: submodular_packing.go + aic.go + 全部单测 + golden 默认关等价。验收:`go test ./cmd/locomo-bench` 绿;默认路径 byte-parity 测试通过。
- **Phase B(US1 离线门)**: submodular_packing_cli.go 驱动全量 1540 三口径 AIC + 审计 → go/no-go 判定工件。执行位置:本地已配 embedding sidecar(EMBED_* env)则完全本地;本地未配(2026-08-16 实测:无 env、无 Ollama)则作为组合批**同一开机第一段**在 box 执行——仅启 embedding sidecar(bge,数据盘缓存),不动 vllm;NO-GO 即刻关机(最坏 ~1 小时机时)。验收:门报告可复算、AIC 规范化冻结声明、NO-GO 则 feature 关闭收尾(verdict 文档)。
- **Phase C(机制接线 + probe,box)**: main.go/eval_runner.go/chunks.go 接线;box 组合批一次开机:对照臂 1-rep(收锚)→ 机制臂 1-rep probe → 判 GO → (GO) 3-rep 正批 → LME 迁移;ride-along reverify_042 同批。验收:配对差 + McNemar + token parity + 装填审计工件齐。
- **Phase D(收尾)**: verdict 文档 + result-matrix 行 + tasks 勾结;box 备份关机(必做)。

## Complexity Tracking

(无 Constitution violation,空)
