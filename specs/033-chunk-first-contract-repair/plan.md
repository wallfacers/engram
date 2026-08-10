# Implementation Plan: Multi-hop Chunk-First Contract Repair

**Branch**: `worktree-033-chunk-first-contract-repair` | **Date**: 2026-08-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/033-chunk-first-contract-repair/spec.md`

## Summary

修复 030 evidence assembly 在 multi-hop 路径中的合同偏差：先建立唯一、规范的 evidence
顺序（全局 chunk 层 → fact 层；层内保持 coverage-first 实体组织），再让装配记录、预算截断和
answer prompt 共同消费这条顺序，禁止 renderer 二次重排。保留一个默认关闭、只用于 benchmark
归因的旧顺序 control。实现不改变检索候选闭包、不增加模型调用、不触及 engine；晋级依赖预注册
64 题 A/C 主门与 18 道 multi-hop B/C 归因探针，最终成功仍要求同口径全量三次多数至少
1387/1540。

## Technical Context

**Language/Version**: Go 1.25.0，`CGO_ENABLED=0`

**Primary Dependencies**: Go 标准库；现有 `memory.Result` 与 `cmd/locomo-bench` 装配基建；不新增依赖

**Storage**: 运行时不写 engine schema；诊断/评测只读既有每会话 SQLite store，产物写 run dir

**Testing**: Go 单元/合同测试、race test、retrieval-only frozen diagnostic、LoCoMo paired majority + exact McNemar

**Target Platform**: Linux/WSL2；纯 Go、离线机制路径

**Project Type**: benchmark CLI / evaluation harness

**Performance Goals**: 每题 top-30 装配不增加检索或模型调用；排序 O(n log n)、额外内存 O(n)；
零调用诊断覆盖 64/64 题

**Constraints**: 全局 chunk-before-fact；实体组织保留；units/prompt 同序；cap 只保留 canonical prefix；
gold/judge 信息不可进入运行时；付费云 reranker/recall 禁止；engine 目录零改动

**Scale/Scope**: LoCoMo cat 1–4 共 1540 题；预注册 target-32 + matched-guard-32；top-k 30、chunk quota 12

## Constitution Check

*GATE: Phase 0 前与 Phase 1 后均通过。*

| Principle | Design evidence | Gate |
|---|---|---|
| I 本地优先、默认离线 | 排序、分组、渲染与所有合同测试均纯本地；付费 answer/judge 仅为显式评测，不是运行机制 | PASS |
| II 引擎/适配层分离 | 只修改 `cmd/locomo-bench` 与本 feature 文档；`memory/ embedding/ provider/ store/ internal/` 零改动 | PASS |
| III 契约优先/隔离 | spec 与 `contracts/multi-hop-chunk-first.md` 先冻结顺序、错误语义、legacy control 和收据字段 | PASS |
| IV 评测回归门 | 先离线合同与 64 题三臂探针，通过才跑同口径 1540×3；历史 89.03 不作 control | PASS |
| V 优雅降级/规模诚实 | only-kind、ungrouped、empty、无 tokenizer 均有确定性行为；只声明 top-30/1540 的实测边界 | PASS |

**Post-design re-check**: PASS。Phase 1 没有引入外部接口、schema migration、云依赖或 engine 入口；
legacy order 仅为 benchmark control，默认关闭并进入 regime fingerprint。

## Phase 0 Decisions

详见 [research.md](research.md)。核心选择是 canonical flat sequence + streaming renderer：

1. `groupHitsByEntity` 产出唯一的 kind-layered flat sequence；
2. renderer 只流式消费该 sequence，在 kind/entity 边界写已有 entity header，不再 partition/sort；
3. score 同分以 SourceID，再以原始 ordinal 稳定决胜；
4. legacy control 显式 opt-in，并写入 assembly audit 与 answer regime fingerprint；
5. A baseline 与 C repaired assembly 在 64 题小探针运行；B legacy assembly 只跑其中 18 道
   multi-hop 题用于独立归因。全量只在主门通过后运行 A/C。

## Phase 1 Design

### Canonical ordering

对 multi-hop 输入闭包建立实体组，沿用旧语义按全候选组 coverage 降序、实体名升序排列。随后按
`chunk`、`fact` 两个 kind layer 依次遍历这些组；每组内按 score 降序、SourceID 升序，原始 ordinal
作最终兜底；每层 ungrouped 位于该层尾部。得到的 flat sequence 同时驱动：

- `EvidenceAssembly.Units`；
- cap 的逐尾删除；
- answer prompt 的编号 evidence 行；
- relation block 每轮截断后的重算边界。

### Benchmark control and receipt

新增 `--assembly-legacy-entity-order`，仅在 `--evidence-assembly` 下合法，默认 false。true 时完整复用
修复前的 group-major 顺序与 renderer；false 时走 canonical kind-layered 顺序。`EvidenceAssembly`
记录 `entity_order`，answer regime 记录 `evidence_assembly` 与 `assembly_entity_order`，阻止 run-dir resume
混入不同 treatment。

### Evaluation design

- **A baseline**: `--evidence-assembly=false --trace-mediation=false`
- **B isolated control**: `--evidence-assembly --assembly-legacy-entity-order --trace-mediation=false`
- **C treatment**: `--evidence-assembly --trace-mediation=false`

三臂固定相同 dataset/store/retrieval/top-k/quota/answerer/judge/prompt/context cap/repeats，并固定
`LOCOMO_NO_THINKING=0`、legacy IDK retry 开启。A/C 对 64 题各 3 reps，B 只对其中 18 道
multi-hop 题跑 3 reps：计划 primary answer/judge decisions 为 `192 + 192 + 54 = 438`，IDK
answer/rewrite retry 作为额外 provider calls 单独登记。主门以 C 对 A 计算 target 与 guard，B/C
单独给出修复归因。各 arm 使用同一 binary、同一冻结 store 的逐字节副本、fresh run dirs，并在
同一时间窗口交错启动；不声称同进程。独立副本避免多个 SQLite 进程同时设置 WAL 时争锁，同时由
副本 manifest 一致性证明 store 轴未变。主门通过后，全量只跑 A/C 各 3 reps；promotion 要求
C≥1387/1540、paired 正向、分类别
exact McNemar 经 Holm 校正后无 `p < 0.05` 的净负回退、收据完整。

付费 probe 还必须回答 backstop 的两个归因问题。C 与 B 使用独立的 runtime assembly-audit 开关，
让每次真实 answer path 写出 `total_tokens/cap/tokens_estimated/input_candidate_count/units`；结果 journal
的 provider `answer_context_tokens` 与其按 question/repeat 配对。逐题截断只按
`len(units) < input_candidate_count` 判定，exact counter 不可用时明确保留 estimated。分析器在全部
arm 结果冻结后，另按冻结 trace 派生的 19 道 chunk-gold 题及其中 `gold_rank_topk>=19` 层报告 C-vs-A；
gold source/rank 映射也只在分析器内用于判断 gold chunk 是否被 admitted；这些 gold-derived 文件
绝不传给 benchmark driver。外部诊断正文称后者为 14 题，但它列出的 rank 分布可复算为
16 题，故实现保留原文并以 trace SHA-256 和显式 ID 清单冻结 16 题口径。

正式 438-repetition 启动前先跑一题、一次、无 IDK retry 的鉴权 smoke。只有 answer/provider usage
为正、prediction 非空、judge 恰好执行一次且 binary digest 未变，driver 才创建三份 store snapshot
并允许启动；HTTP 401、空答案或任一缺失产物均 fail closed。

## Project Structure

### Documentation (this feature)

```text
specs/033-chunk-first-contract-repair/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── multi-hop-chunk-first.md
├── checklists/
│   └── requirements.md
├── diagnosis/
│   ├── target-32.txt
│   ├── guard-32.txt
│   ├── chunk-gold-19.txt
│   ├── chunk-gold-rank19.txt
│   ├── chunk-gold-map.json
│   ├── multi-hop-18.txt
│   ├── cohort-manifest.md
│   └── verdict.md
└── tasks.md
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── main.go                    # benchmark-only legacy selector + regime fingerprint/config wiring
├── assembly.go                # canonical mode routed into order/render/audit
├── entity_grouping.go         # kind-layered entity ordering + streaming renderer + legacy path
├── assembly_flow_test.go      # units/prompt/cap/non-multi contracts
└── entity_grouping_test.go    # deterministic grouping/tie/degenerate contracts
```

**Structure Decision**: 只扩展现有 LoCoMo harness 文件；不新建 engine package、不增加依赖。legacy
control 与生产修复共享同一 binary，确保小探针唯一变量可由 receipt 证明。

## Complexity Tracking

无宪法违规。benchmark-only legacy selector 是额外分支，但它是同 binary 因果归因与快速回退所需；
默认关闭、写入 fingerprint、仅存在于评测 harness，不进入 engine 或 MCP/CLI 产品路径。
