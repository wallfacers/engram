# Implementation Plan: 双基准查询期证据编译架构

**Branch**: `022-benchmark-parity-memory-architecture` | **Date**: 2026-07-30 |
**Spec**: [spec.md](./spec.md)

**Implementation base**: rebased on `master` at `d9b8916` (`eval(021): close
retrieval-side 真实超越 line — IRIS US1 + graph A both NO-GO`) on 2026-07-30. The
022 branch has no unresolved overlap with 021's `iris.go`, `iris_test.go`, or
`main.go`.

**Input**: Feature specification from
`specs/022-benchmark-parity-memory-architecture/spec.md`

## Summary

本特性不建设强制经过的多层记忆体系。它先把规范原文从可变 Atomic Fact 中分离为
append-only Evidence Ledger，再把 Atomic Fact、Semantic Episode 及以后可能出现的
Event/Scene/Profile/graph 定义为可删除、可重建、带完整来源的 projection。

查询侧新增独立的 `memory/evidencecompiler`：它只接收冻结候选，经受限 source recovery、
可验证的 KEEP/EXTRACT/DROP/MERGE/FETCH_SOURCE 和真实 answerer tokenizer 计数，生成
Grounded Trace 与 Evidence Bundle。可选本地 Planner 只能提议动作；无模型时使用确定性
extractive compiler。正式 answerer 只有在来源和预算验证通过后调用一次。

交付按因果门推进：先刷新无损 B0 连续性基线并完成 B1 的计数/协议尺子；有效的冻结候选
B1 必须等待 Ledger 提供真实 source/span 后生成，不能用 v6 的 synthetic legacy source
冒充来源链。B1 每题只物化一次 Candidate、Trace 和 Bundle，三次 answer repetition
逐字节重放该冻结输入；任何 digest 漂移都使整轮无效。有效 low/high B1 与同栈
fixed-gold oracle 先形成可行性 verdict，只有 `CONTINUE` 才进入满量表示与 Compiler
实验。随后才分别验证表示、固定候选 Compiler、Event 组件和一次结构化缺口补检。
Scene、Profile、graph 只在剩余错误支持且各自通过预注册双基准门时实现和默认启用。
达到目标或可行性为 `STOP` 后都不为“架构完整”继续扩张。

## Technical Context

**Language/Version**: Go 1.25.0

**Primary Dependencies**: Go 标准库、`modernc.org/sqlite`、现有
`github.com/modelcontextprotocol/go-sdk`；不新增托管模型或付费 reranker 硬依赖

**Storage**: 每个 namespace 一份本地 SQLite；新增 additive v7 migration，Evidence
规范原文、lifecycle、projection registry/lineage、Semantic Episode payload 分表存储

**Testing**: Go `testing`，离线 unit/contract/integration、migration rollback、
deterministic parity、namespace isolation、span 校验，以及 LoCoMo category 1–4 与
LongMemEval-S full 500 配对评测

**Target Platform**: CGO-disabled Linux/macOS/Windows；评测主机为 WSL2，本地模型通过
可替换 sidecar 提供 tokenizer/planner/answerer

**Project Type**: 纯 Go embeddable engine + 薄 MCP adapter + benchmark CLI

**Performance Goals**: 维持单用户约 100k entry 的诚实边界；lineage 读取必须批量化，
不得出现逐 candidate N+1；projection 重建可中断/恢复；Compiler 在冻结候选池内工作，
不扫描全库

**Constraints**: 默认离线；`CGO_ENABLED=0`；既有 `Retriever.Search` 与 003 graph
合同不变；实际 tokenizer 硬门；每题至多一次 gap retrieval、一次 answerer；无来源
`ADD` 为零；付费托管 reranker/recall 不得作为涨分路径

**Scale/Scope**: 两个公开 benchmark 共 2,040 题的正式回归；一个本地 DB 约 100k
Evidence/projection 量级；不包含 ANN、集群、跨 namespace 推理或百万 token 承诺

## Constitution Check

*GATE: Phase 0 前检查，并在 Phase 1 设计后复核。*

| 原则 | Phase 0 检查 | 设计落实 |
|------|--------------|----------|
| I. Local-first、offline 默认 | PASS | Ledger、projection、deterministic Compiler 和校验均为纯 Go/SQLite；Planner/embedding/answerer 是可替换本地 sidecar，网络默认非必需 |
| II. Engine/adapter 分离 | PASS | Ledger、projection 与 Compiler 位于 `memory/`、`store/`；MCP/CLI 只调用公开 API，不复制算法 |
| III. Contract-first、namespace 隔离 | PASS | [engine-api.md](./contracts/engine-api.md) 与 [compiler-contract.md](./contracts/compiler-contract.md) 先冻结 additive API；namespace 仍由独立 DB 隔离 |
| IV. Evaluation regression gate | PASS | B0/B1、单次物化多次重放、逐题 artifact、fixed-gold 可行性门、exact McNemar 与双基准 stop/go 在实现前定义；评测尺子、算法、配置、结果分开提交 |
| V. Graceful degradation、honest scale | PASS | Planner/optional projection 逐能力降级，结构性 counter/source 错误 fail closed；基础 Search 不受影响；保证只到约 100k 量级 |

### Phase 1 复核

Phase 1 数据模型与三个契约没有引入宪法例外：

- 没有把 hosted model、cloud reranker 或 graph 变成默认依赖。
- 没有让 adapter 访问 SQLite 或复写 retrieval/compiler。
- 没有跨 namespace 表、全局 source ID 或隐式跨域读取。
- 没有把旧 migration 改写；v7 只做 additive schema/backfill。
- 没有把实验性表示或 projection 预先写成默认产品结构。

因此所有五项门仍为 PASS，无需 Complexity Tracking 例外。

## Project Structure

### Documentation (this feature)

```text
specs/022-benchmark-parity-memory-architecture/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── engine-api.md
│   ├── compiler-contract.md
│   └── evaluation-artifacts.md
├── checklists/
│   └── requirements.md
└── tasks.md                 # 由 /speckit-tasks 创建，不在本阶段生成
```

### Source Code (repository root)

```text
store/
├── migrations.go           # additive v7 schema/backfill
├── sqlite.go               # secure-delete/purge transaction support
└── *_test.go               # fresh/backfill/rollback/purge tests

memory/
├── evidence.go             # Evidence Ledger public types and lifecycle
├── projection.go           # projection registry and batched lineage
├── episode.go              # Semantic Episode view/builder boundary
├── entrystore.go           # unchanged Upsert contract + self Evidence wiring
├── retriever.go            # additive stable Result metadata only
├── pipeline/
│   ├── pipeline.go         # Evidence-before-extraction transaction flow
│   └── *_test.go
├── prompt/
│   └── extraction.go       # source-ID-aware extraction contract
├── curation/
│   └── *.go                # union lineage when facts merge
└── evidencecompiler/
    ├── types.go
    ├── compiler.go
    ├── validate.go
    ├── extractive.go
    ├── render.go
    └── *_test.go

mcpserver/
├── tools.go                # additive thin Evidence lifecycle/source contracts
├── server.go
├── registry.go
└── *_test.go               # schema, parity and namespace isolation

cmd/locomo-bench/
├── eval_protocol.go        # frozen protocol and resume fingerprint
├── candidate_artifact.go   # ranked anchors/rendered candidate replay
├── eval_replay.go          # sync-before-answer frozen-question replay journal
├── eval_runner.go          # one materialization plus answer-only repetitions
├── eval_artifact.go        # final drift gate; INVALID runs expose no metrics
├── eval_source_bundle.go   # anchor lineage → active Evidence before token packing
├── eval_source_validate.go # independent Ledger/span/citation reconstruction gate
├── eval_fixed_gold_oracle.go # diagnostic-only same-stack all-gold ceiling CLI
├── representation_eval.go # navigation/rendering bake-off
├── compiler_eval.go        # fixed-candidate compiler arms
├── miss_attribution.go     # mutually exclusive source-survival classes
├── paired_eval.go          # exact paired statistics and promotion verdict
├── token_counter.go        # actual answerer integration
├── main.go                 # thin flag/wiring changes after 021 reconciliation
├── runner.go               # one-answer orchestration
└── *_test.go
```

**Structure Decision**: Evidence 与 projection 是 engine 能力，放在现有 `memory/` 和
`store/`；Compiler 使用独立公开子包以禁止它隐式检索或调用 answerer。评测冻结、回放和
统计只属于 `cmd/locomo-bench`。MCP 仅暴露 additive engine contract，不负责 tokenizer、
planner 或实验策略。

## Delivery Sequence and Gates

### Increment 0 — Measurement protocol, calibration and B0 continuity

1. 先让 021 IRIS 在其工作区提交或暂停，并把 022 rebase 到最新主线；不得覆盖
   `main.go`、`iris.go` 或 `iris_test.go` 的未提交工作。
2. 固化 `022.v1` artifact、dataset/prompt/provider/model/revision/tokenizer/input/output
   cap fingerprints、逐题
   candidate ID/lineage、连续 source coverage、judge audit 抽样和 exact paired statistics。
3. 校准本地 answerer tokenizer，并冻结两个预注册 cap 和待 Ledger 完成后重新
   materialize 的 B1 protocol 模板；B0 continuity manifest/runner 由 T111 补齐后方可
   执行，不复用 B1 artifact 冒充。
4. 在 lossless LongMemEval ingestion 上运行 B0：LoCoMo 1,540 与 LongMemEval 500，
   每题三次独立 answer 后 majority 聚合；B0 的 legacy retry 如实记录，只作产品连续性。
5. v6 的 fact candidate 没有 raw Evidence lineage 时，runner 必须以
   `source_lineage_unavailable` fail closed，零 answer/judge calls，且不得产出 B1 分数。
   不得以 `legacy-entry:*`、session ID 或 chunk quota 伪造可验证来源。
6. 为重复作答建立单次物化合同：每题只执行一次 retrieval、Candidate freeze、
   legacy packing 与 Trace/Bundle 生成，后续 repetition 只能读取冻结 artifact 并调用
   answerer/judge。候选分数、排序或非 answer 字段的任何变化都不得被“最终文本相同”
   掩盖。
7. B1 的 legacy packing 输入必须先把 navigation anchor 的 direct lineage 批量展开为
   active Evidence 原文或精确 Unicode span；token admission 不得读取较短的 projection
   text。独立 validator 在 answer 前重新读取 Ledger，并逐项重建 item text、offset、
   span digest、candidate citation、source union 和完整 answer input。

**Gate**: 分母、protocol、token/answer-call 记录完整率 100%；恢复运行遇到任一 fingerprint
变化必须拒绝；三次 repetition 的 Candidate、Trace 和 Bundle digest 一致率必须为
100%。T111 完成后 B0 可独立报告连续性；v6 的 source-lineage failure 或 repetition drift 是进入
后续机制前的硬 blocker，而不是可被无效 B1 分数掩盖的算法 STOP。

### Increment 1 — Evidence Ledger foundation

1. 先以 migration、幂等、跨 namespace、tombstone/restore/purge、UTF-8 span 与 lineage
   测试定义失败形状，再增加 v7 表和公开 API。
2. 显式 ingest 先原子写 Evidence，再抽取 projection；直接 write 自动创建 self
   Evidence；curation merge 对来源取并集。
3. 增加 deterministic legacy backfill；既有 entry 仅获得 `legacy_entry` 来源，不伪造
   message provenance。
4. 验证删除/重建只影响 projection，隐私 purge 一跳清除所有直接依赖内容，并完成
   secure-delete/WAL checkpoint。
5. 只在上述 source-chain gate、独立 Bundle validator 和 executable fixed-gold oracle
   均通过本地测试后，基于同一 post-Ledger commit 重新 materialize
   LoCoMo/LongMemEval low/high B1 manifest，冻结 ranked candidates，用 legacy packer
   生成每题唯一 Candidate/Trace/Bundle，再以该冻结输入运行三次 answer repetition；
   完成 judge audit 与 fixed-gold oracle diagnostic。后续机制只和这个 B1 的同题
   control 比较。

**Gate**: source-chain、purge closure、namespace isolation、既有 Search/write parity
全部通过；100k fixture 的批量 lineage 路径无 N+1；正式 B1 的 candidate lineage、span、
token/call 字段完整率与 repetition digest identity 必须为 100%，并且
`source_lineage_unavailable=0`。Judge audit 未完成、校正改变 verdict 或任一预注册
category 显著回退时不得 GO。

### Feasibility Gate F0 — Same-stack ceiling before mechanism scale-up

1. 在两个 benchmark 上分别完成有效 low/high B1；INVALID 运行只登记基础设施问题，
   不进入均值、配对统计或 verdict。
2. 使用同一 provider、answerer/judge model+revision、prompt 与对应 input/output cap，
   对冻结 gold Evidence 运行
   diagnostic-only fixed-gold oracle；oracle 不得作为产品分数或 treatment。
3. 按固定答对题数生成唯一 verdict：
   - `CONTINUE`：LoCoMo oracle 至少 1,425/1,540，且 LongMemEval-S oracle 至少
     473/500；允许进入 Increment 2/3 的正式满量评测。
   - `HOLD`：任一 B1、oracle、judge audit 或 artifact validity 不完整；仅修评测尺子，
     不启动新机制满量运行。
   - `STOP`：所有 artifact 有效但任一 oracle 未达目标；022 停止扩建表示、Compiler 与
     optional projection，并把更换 answerer、训练专用 memory compiler 或改变评测栈
     作为独立特性重新 specify。

该门不降低 SC-002/SC-003，也不把 oracle 表述为 Mem0 的同栈对照；它只判断当前冻结栈
是否存在达到最终目标的可验证上界。

### Increment 2 — Representation bake-off

**Entry gate**: F0 必须为 `CONTINUE`。

1. Navigation 实验让 900-character chunk、raw-turn window、semantic episode 各自检索，
   但冻结同算法、embedding、pool/candidate budget。
2. Answer-facing rendering 实验逐题回放同一个 ranked anchor artifact，让 renderer
   的 source expansion 成为唯一 treatment。
3. Episode V1 只做语义边界和原文确定性拼接；segmenter 失败只关闭 Episode view。

**Gate**: 以 [evaluation-artifacts.md](./contracts/evaluation-artifacts.md) 的 primary
cohort 和双基准 promotion rule 判定 GO/HOLD/STOP。未过门表示不进入默认路径。

### Increment 3 — Fixed-candidate Evidence Compiler

**Entry gate**: F0 必须为 `CONTINUE`，且 Increment 2 已冻结获选/保留表示。

1. 冻结获选表示的完整 rendered candidates，逐字节回放 legacy、exact relevance、
   deterministic extractive 和可选 local Planner 四个臂。
2. 所有 Compiler arm 的 retrieval call 为零；SourceResolver 只能按 frozen lineage ID
   读取；精确 counter 对完整最终 prompt 重计。
3. invalid Planner 退化为 deterministic extractive；invalid source/span/citation/cap
   不得调用 answerer。
4. 关闭 legacy IDK retry，最终 Bundle 通过后只答一次。

**Gate**: candidate ID 一致、span/citation 有效、token cap 与一次 answerer 合规率均为
100%，再应用统一双基准 promotion rule。

### Increment 4 — Conditional Event/State experiments

只在核心路径仍存在的预注册 temporal/update residual cohort，分别比较：

- E0：现有 entry 时间字段；
- E1：仅增加独立 event object；
- E2：仅增加日期算子；
- E3：仅增加 source-turn recovery。

每项单独提交、单独开关、单独 verdict。未过门不建设组合“事件层”。

### Increment 5 — Conditional gap retrieval and narrow projections

1. 只有 Trace 输出 `entity | time_range | second_operand` gap 才允许一次补检。
2. control 与 treatment 共享累计候选、token、retrieval 和 answer-call 预算。
3. Scene 只测跨 session，Profile 只测 preference/current-state，003 graph 只测缺桥；
   每项先冻结 residual cohort，再单独实现。
4. 任一阶段达到 SC-002/SC-003 后，后续 projection 可以不建。

## Neighbor Feature Isolation

- `021-locomo-iris-retrieval` 是多轮 sufficiency/refine diagnostic；其最多三轮、自然语言
  missing 和 IDK retry 不属于 022。022 不复用 `irisRetrieve`。
- `fix/longmemeval-lossless-chunks` 已在主线以 lossless ingestion 修复体现；旧 worktree
  只作历史参考，不二次合并。
- 003 graph 的 v3 schema、Go API 和既有数据保持原样。022 projection registry 不接管
  003，也不要求迁移旧 graph。
- 若实现前相关工作区仍 dirty，先记录 `git status`、commit 与重叠行；发生 collision
  时停止并由维护者决定，不替另一 feature 选赢家。

## Submission and Verification Strategy

提交顺序必须保持因果可审查：

1. measurement schema/protocol；
2. calibration 与 B0 continuity artifact，及不产生分数的 B1 template；
3. Ledger schema/API；
4. post-Ledger 单次物化/重复重放修复与 valid B1 control artifact；
5. fixed-gold oracle 与 F0 `CONTINUE | HOLD | STOP` verdict；
6. 每个独立算法 mechanism；
7. 仅包含冻结值的 eval config；
8. 结果与 GO/HOLD/STOP verdict。

每个 engine increment 先写失败测试，再实现。编辑后执行：

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./<touched-package>
CGO_ENABLED=0 go test -count=1 ./...
```

触及 retrieval/extraction/curation/storage/embedding 的提交不得只凭 unit test 合并；必须按
冻结协议完成可比 benchmark slice，并在正式默认变更前完成两个 full benchmark。
长评测依照仓库 WSL2 规则使用 `setsid` 分离，日志与凭据只进入 session scratchpad。

## Complexity Tracking

无宪法违规需要豁免。
