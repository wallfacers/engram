# Implementation Plan: 查询期 verbatim 证据编译

**Branch**: `026-verbatim-evidence-compile` | **Date**: 2026-08-01 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/026-verbatim-evidence-compile/spec.md`

## Summary

022/024/025 三轮证伪"加证据/聚证据"机械密度杠杆后,决策摘要把同预算对 MemOS 的信息密度差距锚定为**命中后的原始证据覆盖**。Fidelity-Before-Structure(2601.00821)给出受控实证:固定 pipeline 内 verbatim chunks 比 LLM-extracted artifacts 高 15.9pp(LoCoMo)/22.0pp(LongMemEval-S),机制是 write-time lossy distillation。026 把差距锚点落成可验证机制:**查询期(query-time)verbatim 证据编译**——固定候选池内按 query 选择原始 turn/span 组装 answer bundle,原文优先(KEEP/FETCH_SOURCE)、装不下才有来源压缩(EXTRACT/MERGE 逐句绑 source)。在 022 冻结协议(cap 3600)下同 store 配对消融,验证 query-time 选原文 > write-time 固定 chunk_900。

## Technical Context

**Language/Version**: Go 1.25.0(CGO_ENABLED=0 硬门)

**Primary Dependencies**: `modernc.org/sqlite`(已有)、`github.com/modelcontextprotocol/go-sdk`(已有,不涉及)、`memory/evidencecompiler`(022 已交付,测试全绿)、`cmd/locomo-bench/compiler_eval.go`(022 已落地 exact-token arm)

**Storage**: 复用 022 Evidence Ledger SQLite schema;编译只读 SourceResolver(不新增 schema,除非 verbatim-first 需要 source-span 索引——见 research.md 决策)

**Testing**: `CGO_ENABLED=0 go test -count=1`(引擎离线测试,向量 stub);arms 用固定候选池 byte-replay 测试;配对验证走 `cmd/locomo-bench` formal protocol

**Target Platform**: Linux(WSL2 开发)/ AutoDL GPU 评测箱(远程 eval)

**Project Type**: Go library(引擎侧少量)+ eval harness(`cmd/locomo-bench` 增量)

**Performance Goals**: 编译路径为 deterministic 纯 Go,单题 <100ms;不新增 N+1 source lookup(022 已用批量 Resolve)

**Constraints**: 默认关(FR-003/004);纯 Go 离线可退化(FR-006);禁止付费云 reranker(DEATH RULE);配对必须同 store + 候选逐字节一致(025 纪律)

**Scale/Scope**: 单用户 ~100k entry;LoCoMo 1,540 answerable + LongMemEval-S 500

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| I. Local-first、offline 默认 | PASS | deterministic 编译纯 Go;无 LLM 端点时退化 extractive;机制默认关 |
| II. Engine/adapter 分离 | PASS | Compiler 位于 `memory/evidencecompiler`(022 已满足);026 主要改动在 `cmd/locomo-bench`(adapter);若需新公开入口走显式 increment |
| III. Contract-first、namespace 隔离 | PASS | 复用 022 冻结的 compiler-contract;不新增跨 namespace 访问 |
| IV. 评测回归门 | **PENDING** | 配对消融必须在 comparable slice 确认不回归基线,否则不得 merge(026 核心验证就是此门) |
| V. 优雅降级 & 诚实规模 | PASS | 无 embedding/LLM 时 deterministic extractive 路径可跑;不承诺超 100k entry 能力 |

## Project Structure

### Documentation (this feature)

```text
specs/026-verbatim-evidence-compile/
├── spec.md                # feature spec
├── plan.md                # this file
├── research.md            # Phase 0: NEEDS CLARIFICATION 决策
├── data-model.md          # Phase 1: 026 增量数据/契约
├── quickstart.md          # Phase 1: 快速跑通
├── contracts/             # Phase 1: 026 增量契约(若 engine 需要公开入口)
└── tasks.md               # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
# 继承 022 资产(不改引擎契约)
memory/evidencecompiler/          # 022 已交付:contracts/need/extract/validate/render/resolve + compiler.go 门面
cmd/locomo-bench/compiler_eval.go # 022 已落地 exact-token arm

# 026 增量(主要在 harness / adapter 侧 + 确定性策略)
cmd/locomo-bench/
├── compiler_eval.go              # [026] 补齐 arms:legacy-count / deterministic extractive / verbatim-first
├── compiler_eval_test.go         # [026] arms byte-replay + fail-closed 测试
├── eval_compiler_arm_integration_test.go  # [026] formal 协议下 arms 配对集成测试
└── eval_source_bundle.go         # [026] verbatim-first bundle 组装(bundle 边界复用 022)
```

**Structure Decision**: 引擎层(`memory/evidencecompiler`)不动(契约已冻结、测试全绿);026 的增量集中在 `cmd/locomo-bench/`(arm 实现 + 配对验证)。若 verbatim-first 需要新的确定性编译策略(如 source-span 索引),先评估能否在 adapter 内实现;确需引擎公开入口时,作为显式 contract increment。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| 无 | 022 已为 Compiler 建独立子包;026 不新增包,只在 harness 层补 arms | — |

## Phases

### Phase 0: 决策与研究方向

见 research.md。核心决策:
- **D1**: verbatim-first 编译策略的具体形式(KEEP/FETCH_SOURCE 优先级、EXTRACT 排序、MERGE 双条件 gate)——复用 022 extract.go 的 raw-fit/MERGE gate,不重写
- **D2**: 需要哪些 arms(legacy-count / exact-token 已有 / deterministic extractive / verbatim-first)
- **D3**: verbatim-first 是否需要 source-span 索引增量(评估 N+1 风险,倾向复用 022 批量 Resolve)
- **D4**: 配对基线如何建立(022 accepted baseline 若未收口,先用当前 chunk_900 可引用分数)

### Phase 1: 设计与契约

- 026 增量契约(若 adapter 需要新公开入口):verbatim-first 编译策略的输入/输出形状
- data-model.md:026 复用 022 类型(Candidate/Source/Action/Bundle/Trace),不新增实体除非 source-span 索引
- quickstart.md:离线 arms byte-replay + 配对跑法

### Phase 2: 实现计划(见 tasks.md,经 /speckit-tasks 生成)

**MVP**: 验证 formal B1 下 `--compiler-arm extractive`(verbatim-first)与 `exact_token` 可用(byte-replay 确定性、fail-closed、默认关零行为变化)——**不实现新机制**(022 引擎已完整实现原文优先双态 + MERGE 双条件 gate,测试全绿)。
**完整**: arms 配对消融(022 冻结协议,LoCoMo + LongMemEval-S,同 store 候选一致),candidate oracle 区分 miss,分类别报告,verdict 收口。

## Key Technical Decisions

1. **承接而非重写**:022 的 `memory/evidencecompiler` 契约与 verbatim-first 引擎实现(原文优先双态 + MERGE 双条件 gate)已完整且测试全绿;`--compiler-arm extractive/exact_token` 已接线 formal B1(`compileFormalSources` → `evidencecompiler.Compile`)。026 **不实现新机制**,增量是验证 + 配对消融。引擎契约改动须走显式 increment(宪法 II)。
2. **verbatim-first 双态(已由 022 引擎实现)**:原文装得下 → KEEP/FETCH_SOURCE 保留原始 span;装不下 → EXTRACT(按 relevance 排序)仍不够才 MERGE(每句逐句验证 source)。这直接落地 Fidelity-Before-Structure + Retain-or-Consolidate(MERGE 宽松预算显著负)。026 的职责是验证它在 formal B1 下是否同预算优于 chunk_900。
3. **配对纪律(025 教训)**:两臂必须同一 store、候选逐字节一致,只差编译策略;报告 candidate oracle 区分 compiler miss vs candidate miss;LoCoMo 6.4% 答案键噪声记录在案,小 delta 不单独作 promotion 依据。
4. **fail-closed**:无来源 ADD 拒绝、无效 citation 丢弃、退回 extractive(022 引擎已实现),不调 answerer。
