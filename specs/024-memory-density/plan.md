# Implementation Plan: 同预算记忆密度杠杆

**Branch**: `024-memory-density` | **Date**: 2026-07-31 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/024-memory-density/spec.md`

**Note**: This template is filled in by the `/speckit-plan` command; its definition describes the execution workflow.

## Summary

022 的 budget-ablation 证明 LoCoMo 分数主要是 answer-context token 预算的函数：同预算（~1059 tok）下 engram 76.85% 落后 MemOS 82.47%，差距在**信息密度**而非检索/作答机制。本 feature 借鉴 MemOS Tree+Fact 源码中两个与 engram 宪法兼容的机制，在**不放宽预算、不引入付费云模型、不破坏 append-only Evidence Ledger** 的前提下提高每个 answer token 携带的有效证据量：

1. **写入时冗余抑制**：新增 atomic fact 投影时，用离线信号（复用 `memory/curation` 的字符 trigram Jaccard，阈值 0.7）+ 可选 embedding 语义信号检测与已有事实的语义冗余，**抑制重复投影创建**（evidence ledger append-only 无损）。对应 MemOS `manager.py` 的 `merged_threshold=0.92` 写入去重，但只抑制投影、不做 LLM 融合写回。
2. **命中后邻居扩展**：检索命中 fact 后，在候选冻结之后、answerer 组装之前，沿 lineage/relation（共享 evidence 的兄弟 fact，depth-1 有界）扩展上下文。对应 MemOS 的 depth-2 图邻居，但用 engram 已有的 `memory_projection_sources` 离线实现，不引入图数据库。

两机制默认关、纯本地、可独立消融；在 022 冻结协议下做 LoCoMo/LongMemEval-S **同预算四臂配对**验证（宪法 IV 回归门）。

## Technical Context

**Language/Version**: Go 1.25（纯 Go，`CGO_ENABLED=0` 硬门）

**Primary Dependencies**: 无新增第三方依赖。复用现有：`modernc.org/sqlite`（存储）、`memory/curation`（离线 Jaccard 去重信号）、`memory/projection.go`（ProjectionStore / lineage）、`cmd/locomo-bench`（022.v1 eval 协议）。

**Storage**: SQLite（现有 schema v7）。冗余抑制**不新增表**（抑制即不写投影 + 可选冗余 relation 标记复用 `memory_projection_sources.relation`）；邻居扩展**不新增表**（兄弟 fact 由共享 evidence 推导）。

**Testing**: `CGO_ENABLED=0 go test -count=1 ./...`；引擎行为 TDD（先写失败测试）；离线单测（无 embedding 端点降级）；MCP 契约测试与 parity 黄金（现状 gate）。Eval 走 `cmd/locomo-bench` 配对消融。

**Target Platform**: 引擎库（Linux/macOS 交叉编译）+ `cmd/locomo-bench` eval 集成。非 adapter 工作（不改 MCP/CLI/SDK）。

**Project Type**: 引擎能力扩展（memory 层）+ eval harness 集成（研究实验型，默认关）。

**Performance Goals**: 冗余判定必须复用 FTS pre-filter 限定的 candidate pairs（O(pairs)，非 O(n²)）；邻居扩展必须有界（上限可配置，默认 depth-1、数量上限不无界放大 token）。

**Constraints**: 默认关（宪法 I/V）；纯本地无 LLM 硬依赖（Jaccard 离线降级路径，宪法 V）；不破坏 append-only（宪法 I 精神）；配对消融不回归基线（宪法 IV）；机制与算法改动分开提交、与 eval 配置改动分开提交（宪法 IV 归因）。

**Scale/Scope**: LoCoMo 1540 题 + LongMemEval-S 500 题，双基准四臂配对（repeats≥3）；引擎单用户 ~10 万条级记忆，冗余抑制的判定开销须在该量级可承受。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

**I. 本地优先，默认离线**: 两机制默认关；冗余判定的**离线 Jaccard 路径**（复用 `memory/curation`）是主路径，embedding 语义信号仅在有本地 sidecar 时可选叠加。→ PASS（离线可跑）。

**II. 引擎与适配层分离**: 本 feature 只改引擎（`memory/`）与 eval harness（`cmd/locomo-bench`），不改任何 adapter（`mcpserver/`、MCP/CLI/SDK）。机制只在引擎公共 API 内实现，harness 通过 protocol mechanism_flags 开关。→ PASS。

**III. 契约优先与命名空间隔离**: 新机制通过 022.v1 protocol 的 `mechanism_flags` 增加新 key（向后兼容新增字段，不改既有 key 语义）；新机制 `MUST` 在 protocol freeze 与 `validateFormalMechanismBinding` 中显式声明，破坏性变更才 bump MAJOR。→ PASS（新字段非破坏性）。

**IV. 评测回归门禁（NON-NEGOTIABLE）**: 两机制在 022 冻结协议下分别做 LoCoMo/LongMemEval-S 配对消融（四臂：关/开冗余抑制 × 关/开邻居扩展，repeats≥3）；overall 不显著回归基线；**机制改动与 eval 配置改动分两个 commit** 提交以保证归因。→ PASS（硬门，Increment 3 强制执行）。

**V. 优雅降级与规模诚实**: 无 embedding 端点时冗余判定退回纯离线 Jaccard；邻居扩展无邻居时行为与关闭一致；候选/token 规模有界并在文档如实声明。→ PASS。

### Phase 1 复核

已回填（2026-07-31）：data-model 确认**无新表、无新迁移**（复用 schema v7；`memory_projection_sources.relation` 新增 `redundant` 值，非破坏性）；contracts 确认 `mechanism_flags` 新 key 为向后兼容新增，不 bump MAJOR；无新第三方依赖、无新适配层、不引入图数据库。→ 无违宪复杂度，Complexity Tracking 保持空。

## Project Structure

### Documentation (this feature)

```text
specs/024-memory-density/
├── plan.md              # This file (/speckit-plan command output)
├── spec.md              # 研究型 spec（同预算记忆密度杠杆）
├── research.md          # Phase 0 output (/speckit-plan command)
├── data-model.md        # Phase 1 output (/speckit-plan command)
├── quickstart.md        # Phase 1 output (/speckit-plan command)
├── contracts/           # Phase 1 output (/speckit-plan command)
└── tasks.md             # Phase 2 output (/speckit-tasks command - NOT created by /speckit-plan)
```

### Source Code (repository root)

```text
memory/                              # ENGINE — 两机制落点
├── pipeline/pipeline.go             # storeFact 前插入冗余抑制（Increment 0）
├── curation/dedup.go                # 复用：离线 trigram Jaccard 判定信号
├── projection.go                    # 新增/扩展：兄弟 fact 邻居查询（Increment 1）
└── retrieval/                       # 检索后候选冻结处的邻居扩展（Increment 1）

cmd/locomo-bench/                    # EVAL — 机制绑定与配对验证
├── main.go                          # mechanism flag + validateMechanismArms 扩展
├── eval_runner.go                   # mechanism_flags 新 key + freeze/validate binding
└── (materialize)                    # 邻居扩展插入点：候选冻结后、answerer 组装前

memory/curation/ 测试                # TDD：Jaccard 复用 + 抑制行为的失败测试先行
cmd/locomo-bench/ 测试               # 契约/集成测试：protocol 绑定 + 四臂开关
```

**Structure Decision**: 单引擎库 + eval harness（与 022/023 完全一致的结构）。无新包、无新适配层；机制以内聚函数/小接口挂在现有 `pipeline`/`projection`/`retrieval` 上，默认关。不引入图数据库（与 spec 范围边界一致）。

## Delivery Sequence and Gates

### Increment 0 — 写入时冗余抑制（engine）

1. 在 `pipeline.storeFact` 创建 fact 投影前，调用可注入的冗余判定器（接口：`ShouldSuppress(existing, incoming) bool`）。
2. 默认实现 = 离线路径：复用 `memory/curation` 的 `normalizeText` + 字符 trigram Jaccard（阈值 0.7，可配），用 FTS pre-filter 限定 candidate pairs（O(pairs)）。
3. 可选叠加：embedding 语义相似度（阈值可配，默认关，需本地 sidecar）。
4. 抑制行为：不创建新 projection；原始 evidence 完整保留；记录审计统计（判定/抑制/疑似误伤计数）。
5. 判定器开关：默认关；关闭时逐字节与现状一致。
6. 测试（TDD）：离线写入两条近似事实 → 第二条被抑制且 evidence 完整；关闭时行为不变；无 embedding 端点降级路径；冲突事实不被抑制。

### Increment 1 — 命中后邻居扩展（engine）

1. 检索候选冻结后、answerer 上下文组装前，对命中的 fact 取"共享 evidence 的兄弟 fact"（`memory_projection_sources` 同 evidence 推导，depth-1）。
2. 扩展有界：兄弟数量上限可配置（默认小、不无界放大 token）；无邻居时行为与关闭一致。
3. 扩展上下文按确定性顺序（evidence 顺序 / fact name）追加，供 answerer 组装。
4. 开关：默认关；关闭时候选与现状一致。
5. 测试（TDD）：两 fact 共享 evidence → 命中其一断言另一出现在扩展候选；无邻居 → 零变化；上限生效。

### Increment 2 — 022.v1 协议绑定（eval）

1. `mechanism_flags` 增加两个新 key（如 `write_dedup` / `neighbor_extend`，向后兼容新增，不改既有 key）。
2. `freezeFormalProtocol` / `validateFormalMechanismBinding` 支持两新机制；CLI flag 对应接入；机制 flag 在 formal context 外 fail closed（复用现有机制校验框架）。
3. 新机制 protocol 与已冻结 protocol 独立；不动已冻结的 022 资产。
4. 测试：mechanism 绑定契约测试（flag 生效/静默忽略 fail closed）+ 离线单问流程。

### Feasibility Gate F0 — 单基准（LoCoMo）同预算配对，方向确认

1. 在 022 冻结协议 + **固定 answer-context token cap** 下，先跑 LoCoMo 四臂（关/开冗余抑制 × 关/开邻居扩展，repeats≥3）。
2. 门标准：任一机制相对基线的变化不显著回归（宪法 IV）；若两机制均有方向性收益或其一显著为正，进入 Increment 3；若均无收益或负，记录负结果、保持默认关、收口（允许以诚实负结果结束，spec SC-003）。
3. **归因提交**：机制代码与 eval 配置改动分两个 commit。

### Increment 3 — 双基准四臂配对验证 + 报告

1. LongMemEval-S 同预算四臂配对（与 F0 同 recipe）。
2. 报告：双基准 overall + 分类别明细 + 配对统计（exact McNemar）+ token 预算记账 + 候选密度/冗余下降度量（SC-001）。
3. 交互效应显式报告（两机制同开 vs 各自单开，SC-003）；负收益机制保持默认关并记录 verdict。
4. 结论写入 `docs/evaluation/`（成功 → 讨论默认路径；负 → 归档）。

## Neighbor Feature Isolation

- 022（同栈前置，已交付 Evidence Ledger/protocol）：本 feature 只读其资产（protocol、projection/lineage），不改 022 已冻结内容；`feature.json` active 已切到 024，022 分支不并行改动。
- 023（local-trained evidence compiler）：本 feature 的写入抑制/邻居扩展与之正交（025/026 未创建）；两 feature 均独立开关，无共享变量。
- 合并前：`git worktree list` + 读各 active sibling `specs/*/spec.md`，确认无重叠面再合并。

## Submission and Verification Strategy

- **提交分离（宪法 IV 归因）**：Increment 0/1（engine 机制代码）与 Increment 2（eval 协议绑定）分 commit；eval 配置改动（protocol/manifest）与算法改动分 commit。
- **引擎门**：每次编辑后 `CGO_ENABLED=0 go build ./...` 零错误；触达包 `go test -count=1 ./<pkg>`。TDD：先写失败测试。
- **eval 门（宪法 IV）**：F0 在 LoCoMo 同预算配对不回归；Increment 3 双基准四臂全量；parity 黄金（`memory_search` == 直接 `Retriever.Search`）+ 引擎测试在纯 adapter 变更场景证明不变性；本 feature 改引擎，故直接跑配对 eval。
- **adapter 无关**：`git diff --name-only -- mcpserver` 必须为空（本 feature 不动 adapter）。
- **收尾**：机制负收益也如实归档（spec SC-003 允许负结果），不以加 projection/预算掩盖。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| （无）| — | 两机制均以现有表/现有离线信号实现，无新表、无新依赖、无新适配层；不加复杂度。 |
