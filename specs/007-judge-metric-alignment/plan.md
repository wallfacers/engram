# Implementation Plan: LoCoMo Judge 口径对齐(mem0-aligned)

**Branch**: `007-judge-metric-alignment` | **Date**: 2026-07-21 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/007-judge-metric-alignment/spec.md`

## Summary

把 `cmd/locomo-bench` 的 LoCoMo LLM-judge 宽松度**逐条对齐 Mem0**(`../mem0/evaluation/benchmarks/locomo/prompts.py:218-245` 的 `_JUDGE_TEMPLATE` 无 evidence 变体),剥离 65.4% vs 88.83%(同 1540 分母)差距里的 **judge 严格度伪影**。实现只改 `runner.go` 的 `judgeSystemPrompt` 规则文本(**保留现有 `{"correct": true|false}` JSON 契约**,`parseJudgeVerdict` 不动),置于**默认 off** 的 `--judge-mem0-aligned` flag 后,并在 run fingerprint 追加 `;judge=mem0-aligned` 做口径隔离。防放水靠一份 anti-放水 golden 夹具 + 两层测试(离线确定性层进 `CGO=0` CI;近免费 golden 层 env-gate)。US1 只做免费部分,不量化新分;US2(付费量化新基线)双门控、需授权。引擎零改。

## Technical Context

**Language/Version**: Go 1.25.0(no CGO — 纯 Go,引擎不变量)

**Primary Dependencies**: 仅现有 `cmd/locomo-bench` harness —— `runner.go`(`judgeSystemPrompt`/`buildJudgePrompt`/`parseJudgeVerdict`)、`main.go`(flag 定义 + run fingerprint `main.go:1018` 附近)。relay judge 走 003 冻结模型(gpt-5.6-luna 或等价 OpenAI 兼容 endpoint)。规则文本源 = 本机克隆 `../mem0/evaluation/benchmarks/locomo/prompts.py:218-245`。无新第三方依赖。

**Storage**: 无。golden 夹具是仓内静态 `testdata` 文件。无 SQLite/schema 变更。

**Testing**: `CGO_ENABLED=0 go test ./cmd/locomo-bench/...`。两层:(1) 离线确定性层(零 LLM,进 CI):断言 `judgeSystemPrompt`(mem0-aligned 模式)含 7 条规则关键子句 + `parseJudgeVerdict` 表驱动正确性;(2) 近免费 golden 层(env-gate,需 judge endpoint):真跑夹具、逐条断言 label。

**Target Platform**: Linux(WSL2 dev);离线可构建/CI;golden 层需可选 endpoint。

**Project Type**: 单项目 CLI 评测 harness(引擎 adapter)。无新二进制;`cmd/locomo-bench` 加 flag。

**Performance Goals**: US1 零答题调用;golden 层数十次判分调用(近免费)。无全量重判/重跑。

**Constraints**: 引擎 + shipped schema 零改(FR-007/SC-004);flag off 逐字零回归(SC-005);口径隔离(FR-003/SC-006);US1 零答题成本(SC-003);付费 US2 门控在 GO + 授权(FR-009)。

**Scale/Scope**: golden 夹具 ~25-30 条。改动面:1 个 prompt 常量(条件化)+ 1 个 flag + 1 处 fingerprint + 1 个夹具文件 + 2 个测试文件。

## Constitution Check

*GATE: 须在 Phase 0 前通过;Phase 1 后复检。*

- **I. Local-first, offline by default** — ✅ US1 离线可跑(离线确定性测试层进 CI);golden 层用 003 既有的可选 relay judge endpoint,与既往一致,非新增必需托管服务。默认 off,不改变离线默认行为。
- **II. Engine/adapter separation** — ✅ 全部改动在 `cmd/locomo-bench`(adapter)。引擎仅经现有路径消费,零改。FR-007/SC-004 把 `git diff -- memory embedding provider store internal` 为空设为硬门。
- **III. Contract-first & namespace isolation** — ✅ 外部契约 = 新 flag + judge 输出 JSON 契约(**保持 `{"correct":bool}` 不变**)+ fingerprint 标签 + 夹具 schema,在本 plan/contracts 冻结。无跨 namespace。judge 输出契约不变 → 无破坏性变更。
- **IV. Evaluation regression gate(NON-NEGOTIABLE)** — ✅ 本特性**是评分口径变更**:判宽松度改动 = metric 定义变更。诚实处理:默认 off(旧口径不变,SC-005 逐字零回归)、fingerprint 隔离(SC-006 新口径分永不与旧 65.4% 静默配对)、新基线声明协议(FR-008,US2 单独 eval 提交、明标"口径对齐非涨点")。US1 mechanism 提交不声称任何分数。
- **V. Graceful degradation & honest scale** — ✅ golden 层无 endpoint 时按 env-gate 跳过,离线层仍守 prompt/parse 断言(降级不失效)。诚实边界:judge 放宽同时抬高所有被对比方,只用于口径对齐公平比较,不与竞品 leaderboard 绝对值混用宣称(FR-008)。

**Verdict: PASS.** 无违规;Complexity Tracking 空。

## Project Structure

### Documentation (this feature)

```text
specs/007-judge-metric-alignment/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output(flag + judge JSON + fingerprint + 夹具 schema 契约)
├── checklists/
│   └── requirements.md  # (from /speckit-specify)
├── spec.md
└── tasks.md             # /speckit-tasks output(此处不创建)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── runner.go            # MODIFIED — judgeSystemPrompt 条件化:strict(默认)/mem0-aligned;
│                        #   新增 judgeSystemPromptFor(mode) 返回对应规则文本。parseJudgeVerdict/buildJudgePrompt 不动。
├── main.go              # MODIFIED — 新增 --judge-mem0-aligned flag(默认 false);
│                        #   run fingerprint(~:1018)追加 ;judge=mem0-aligned;把 mode 透传到 judge 调用点。
├── judge_golden_test.go # NEW — 两层测试:离线确定性(prompt 含规则子句 + parseJudgeVerdict 表驱动,进 CI)
│                        #   + env-gate golden 层(TestJudgeMem0AlignedMatchesGolden,真跑夹具,anti-放水陷阱作判据)。
└── testdata/
    └── judge_golden.jsonl   # NEW — ~25-30 条夹具:宽松该判对 + anti-放水陷阱 + 边界。

# UNCHANGED(硬门):memory/ embedding/ provider/ store/ internal/
#   verify: git diff --name-only master...007-judge-metric-alignment -- memory embedding provider store internal  → EMPTY
```

**Structure Decision**: 单项目 CLI 评测-harness 特性。核心改动是**把一个 prompt 常量条件化**(strict/mem0-aligned)+ flag/fingerprint 接线 + 夹具 + 两层测试。刻意最小面、不碰 judge 的 JSON 契约与解析,降低回归风险。引擎树 off-limits。

## Complexity Tracking

> 无宪法违规 —— 表空。

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| (none)    | —          | —                                    |
