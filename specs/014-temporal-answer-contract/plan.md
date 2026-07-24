# Implementation Plan: 答题侧时序推理契约

**Branch**: `014-temporal-answer-contract` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/014-temporal-answer-contract/spec.md`

## Summary

把 LoCoMo temporal(category-2)的答题瓶颈(诊断:69% 答错题 gold 已进 top-30 上下文)用一份**强化答题侧时序推理契约**各个击破三失败模式(±1 去歧 / 相对→绝对 / 时长相减)。技术路径:重写 `cmd/locomo-bench/runner.go` 的 `forceTemporalAnswerPrompt` 常量为压三模式的 CoT 契约,复用现有 `--temporal-answer-prompt` 开关 + `answerPromptForRegime` 的 category==2 路由;先离线单测锁四条推理锚 + 三处不变量,再上 box 三臂配对 e2e(base/old-tplan/new-tplan 归因锚)判 GO/NO-GO。引擎零改,纯 host 侧 adapter。

## Technical Context

**Language/Version**: Go 1.25.0(no CGO,纯 Go)

**Primary Dependencies**: 无新增。复用 `cmd/locomo-bench` 现有:`answerPromptForRegime`(prompt 路由)、`--temporal-answer-prompt` flag、`--cat-top-k`、McNemar 配对统计(`stats.go`)、box 全本地栈(answer/extract vllm Qwen + embedder vllm bge-large + judge deepseek)。

**Storage**: N/A(答题契约是常量文本;e2e 产物落 gitignored `.locomo-run/`)。

**Testing**: `go test`(离线单测,纯字符串/路由断言,零 box/token)+ box 全本地栈 e2e 门(canonical recipe)。

**Target Platform**: Linux(WSL2 开发;box 远端 GPU 跑答题/嵌入)。

**Project Type**: 单项目 CLI 适配器(eval harness),engram 引擎的下游 host 模拟。

**Performance Goals**: 无延迟/吞吐目标。唯一硬指标 = SC-001(category-2 答对率显著抬 + overall 不回退)。

**Constraints**: 引擎(`memory/ embedding/ provider/ store/ internal/`)零改(FR-008);契约 category==2 门控,三处不变量(非 cat-2 / 开关关 / abstain 优先)逐字节不变(FR-007);冷启动首臂 warm-up 纪律(SC-005)。

**Scale/Scope**: LoCoMo 10 会话,category-2 n=321;单文件常量改 + 单测文件;e2e 三臂 × repeats=3。

## Constitution Check

*GATE: Must pass before Phase 0. Re-check after Phase 1.*

| # 宪法条 | 判定 | 依据 |
|---|---|---|
| I. Local-first, offline by default | ✅ PASS | 契约是本地常量文本;单测全离线;答题/嵌入用可替换 sidecar(box vllm),非必需托管服务。核心引擎路径不依赖 box。 |
| II. Engine/adapter separation | ✅ PASS(**关键**) | 纯 `cmd/locomo-bench` adapter 改,引擎零改(FR-008,`git diff --name-only -- memory embedding provider store internal` 必空)。不新增引擎入口——本 feature 根本不需要引擎侧能力(答题是 host 职责)。 |
| III. Contract-first & namespace isolation | ✅ PASS | 契约文本 + 三处不变量在 spec/plan 冻结;先写失败单测(TDD)。无 schema / namespace 变更。 |
| IV. Evaluation regression gate (NON-NEGOTIABLE) | ✅ PASS(**本 feature 就是门**) | US2 三臂配对 e2e = 门本身;GO 判据 SC-001(cat-2 显著抬 + overall 不回退)。算法改(契约)与评测配置改分离提交(attribution)。 |
| V. Graceful degradation & honest scale | ✅ PASS | 契约 edge case 已列(缺锚不臆造/单端点给部分答案);无 million-token 声明;脚手架(Option B)明列为欠转化时的显式未来工作,非隐性承诺。 |

**无违规**,Complexity Tracking 留空。

## Project Structure

### Documentation (this feature)

```text
specs/014-temporal-answer-contract/
├── plan.md              # 本文件
├── research.md          # Phase 0:决策(强化 vs 新增开关 / 三臂设计 / 契约措辞依据)
├── data-model.md        # Phase 1:实体(契约文本 / 臂 / 配对判据)
├── quickstart.md        # Phase 1:如何跑三臂 e2e 门(box recipe + 冷启动纪律)
├── contracts/
│   └── temporal-answer-contract.md   # 契约文本 + 四锚映射三模式 + 不变量契约
├── checklists/requirements.md        # specify 阶段质量门(已过)
└── tasks.md             # Phase 2(/speckit-tasks 产出)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── runner.go            # 改:forceTemporalAnswerPrompt 常量强化(唯一算法改)
├── runner_test.go       # 新增/改:US1 离线单测(四锚在位 + 三不变量)
└── main.go              # 不改(--temporal-answer-prompt / --cat-top-k / 路由已存在)

docs/
├── locomo-score-levers.md            # 改:落 US2 verdict(GO/NO-GO + 归因)
└── temporal-answer-contract.md       # US3 条件性(仅 GO):可移植 pattern 文档
```

**Structure Decision**: 单项目 CLI 适配器。唯一算法改动 = `runner.go` 内 `forceTemporalAnswerPrompt` 常量文本;路由/开关/统计全复用。引擎目录不出现在 diff。US2 是 box 评测动作(非代码结构),US3 文档条件于 GO。

## Complexity Tracking

> 无宪法违规,留空。
