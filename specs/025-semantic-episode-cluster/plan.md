# Implementation Plan: 跨消息语义聚类 episode 表示

**Branch**: `025-semantic-episode-cluster` | **Date**: 2026-08-01 | **Spec**: [spec.md](./spec.md)

**Implementation base**: `master` 75211d8 (`docs(024/us3): add failure-interpretation and next-lever guidance to four-arm report`)。025 与 024 分支已合并删除,当前工作在 `025-semantic-episode-cluster` 分支上。

**Input**: Feature specification from `specs/025-semantic-episode-cluster/spec.md`

## Summary

在 022 已交付但**未在正式 eval 接线**的 episode 引擎(`memory/episode.go` 的 `EpisodeStore.RebuildSession` + `representation_eval.go` 三渲染器)基础上:

1. **US2 接线验证**:先确认 022 交付的 episode 引擎单测(13+14+6,已通过)覆盖设计,再补**跨 session 语义聚类能力**的失败测试。核心修正:022 的缺口不是"缺测试"(测试已存在),而是**从未在正式 pipeline 构建过 episode 投影**(`RebuildSession` 全仓库零调用、集成测试不接线)。
2. **US1 跨消息语义聚类**:新增 SemanticClusterer(跨 session 语义聚类 builder),把 episode 定义从"同 session 连续 Evidence 段"扩展为"跨原始消息/跨 session 的语义相关证据簇"。判定信号:离线确定性信号(共享实体 + 关键词/主题重叠)+ 可选 embedding cosine overlay(默认关)。全量可重建、config-hash 幂等、有界。
3. **US3 配对验证**:在 022 冻结协议(cap 3600)下,`semantic_episode` 臂(chunk_900/raw_turn_window 不动)与 chunk_900 基线配对消融,验证 multi-hop 是否涨点,不回归 overall。

**关键认知(相对 spec 的修正)**:① episode 引擎测试已存在且通过,US2 缺口是"接线验证"而非"补测试"——`RebuildSession` 全仓库零调用;② `memory_semantic_episodes` 表无 session 约束、`memory_projection_sources` lineage 天然支持跨 session → 跨消息聚类**很可能零 migration**;③ 聚类域 = 同一 namespace DB 内全部 active Evidence(LoCoMo 每 conversation 一 DB),跨 session 默认允许、有界。

## Technical Context

**Language/Version**: Go 1.25.0

**Primary Dependencies**: Go 标准库、`modernc.org/sqlite`、现有 `github.com/wallfacers/engram/memory`(EpisodeStore/ProjectionStore/LedgerStore)、`cmd/locomo-bench` 既有 eval 架构;不新增托管模型或付费 reranker 硬依赖

**Storage**: 每个 namespace 一份本地 SQLite。**预期零新 migration**:`memory_semantic_episodes`(v7)无 session 约束,`memory_projection_sources` lineage 已支持跨 session;若聚类判定需持久化审计统计,用既有 `memory_projections.config_hash` + run 侧 artifact 记录,不改 schema

**Testing**: Go `testing` 离线 unit/contract/integration;episode 跨 session 聚类失败测试 → SemanticClusterer 实现;渲染 bake-off 复用 `representation_eval_test.go` shared-anchor 框架;配对验证走 022 冻结协议(cap 3600,repeats≥3)

**Target Platform**: CGO-disabled Linux(WSL2);评测主机为 AutoDL 远程(vllm answer + embedding,`HF_HUB_OFFLINE=1`),judge 走 DeepSeek API

**Project Type**: 纯 Go embeddable engine(memory/)+ benchmark CLI(cmd/locomo-bench)

**Performance Goals**: 聚类构建全量重建时可中断/恢复;lineage 批量化无 N+1;聚类判定复杂度 O(evidence²)在单 namespace ~100k 量级下可接受(离线信号用 FTS/LIKE 预筛候选对);渲染 bake-off 不增加 answerer 调用数

**Constraints**: 默认离线;`CGO_ENABLED=0`;聚类默认关(关闭时零行为变化);无 embedding 端点时纯离线判定路径;不引入付费云 reranker/LLM;保持 append-only Evidence Ledger 无损;每次 answerer 调用次数不增

**Scale/Scope**: 单 namespace ~100k Evidence;聚类域=同 namespace 全部 active Evidence;跨 session 有界(每 episode 证据数上限可配,默认值见 research.md Decision 2)

## Constitution Check

*GATE: Phase 0 前检查,并在 Phase 1 设计后复核。*

| 原则 | Phase 0 检查 | 设计落实 |
|------|--------------|----------|
| I. Local-first、offline 默认 | PASS | SemanticClusterer 离线信号(实体+关键词重叠)是纯 Go/SQLite;embedding overlay 是可选本地 sidecar,默认关;无 embedding 端点时纯离线判定 |
| II. Engine/adapter 分离 | PASS | SemanticClusterer 在 `memory/`(engine);`cmd/locomo-bench` 只调用公开 API,不复制算法;MCP 不涉及(本 feature 是研究实验,不改 adapter) |
| III. Contract-first、namespace 隔离 | PASS | [engine-api.md](./contracts/engine-api.md) 先冻结 additive 聚类 API(新 `EpisodeStore.RebuildAll` / `SemanticClusterer`);namespace 仍由独立 DB 隔离,聚类域不跨 namespace |
| IV. Evaluation regression gate | PASS | 配对消融在 022 冻结协议下跑;chunk_900 基线不动;表示差异与检索差异分离(同 anchor);负结果记录 verdict,不进入默认路径 |
| V. Graceful degradation、honest scale | PASS | 无 embedding 端点 → 纯离线聚类;聚类构建失败只关 episode 视图,fact/chunk 路径不受影响;保证只到 ~100k 量级 |

### Phase 1 复核

Phase 1 数据模型与契约没有引入宪法例外:

- 没有把 hosted model、cloud reranker 变成默认依赖(embedding overlay 默认关,纯离线可判定)。
- 没有让 adapter 访问 SQLite 或复写聚类算法。
- 没有跨 namespace 表或隐式跨域读取(聚类域 = 同 namespace DB)。
- 没有改写旧 migration;预期零新 migration(若需审计表,additive)。
- 没有把实验性表示预先写成默认产品结构(默认关,FR-010)。

因此所有五项门仍为 PASS,无需 Complexity Tracking 例外。

## Project Structure

### Documentation (this feature)

```text
specs/025-semantic-episode-cluster/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── engine-api.md
├── checklists/
│   └── requirements.md
└── tasks.md                 # 由 /speckit-tasks 创建,不在本阶段生成
```

### Source Code (repository root)

```text
memory/
├── episode.go               # 扩展:EpisodeStore.RebuildAll(跨 session)+ SemanticClusterer 接线
├── semantic_cluster.go      # 新增:SemanticClusterer 接口 + 离线聚类实现(实体/关键词重叠)+ embedding overlay
├── semantic_cluster_test.go # 新增:跨 session 聚类失败测试(离线信号、有界、幂等、降级)
├── episode_test.go          # 已有(022 交付,13 tests):确认覆盖,扩展跨 session RebuildAll 测试
└── projection.go            # 已有:SiblingFacts/SourcesByProjectionIDs 复用

cmd/locomo-bench/
├── eval_runner.go           # 已有:formalRepresentationRendererWithEpisodes 接线(需扩展:渲染前构建 episode)
├── representation_eval.go   # 已有:semanticEpisodeRenderer(复用,anchor→episode 路由用 RebuildAll 产物)
├── representation_eval_test.go # 已有(14 tests):扩展跨 session episode 渲染测试
├── main.go                  # 扩展:--episode-cluster flag(默认关);渲染前调 RebuildAll
└── episode_cluster_eval.go  # 新增(可选):聚类判定统计审计(与 024 suppression-audit 对齐)
```

**Structure Decision**: SemanticClusterer 是 engine 能力,放 `memory/`(对齐 022 episode.go 位置);渲染与配对验证属 eval,放 `cmd/locomo-bench`;MCP 不涉及。渲染复用 022 的 `semanticEpisodeRenderer`,无需新 renderer——只需让 eval 在渲染前用 RebuildAll 构建跨 session episode 投影。

## Delivery Sequence and Gates

### Phase A — US2 接线验证 + 跨 session 聚类能力测试先行 (TDD)

1. **核实承接资产**:读 `memory/episode_test.go`(13 tests)、`representation_eval_test.go`(14 tests)、`representation_integration_test.go`(6 tests),确认覆盖 022 设计(同 session 连续边界、deterministic narrative、shared anchor、byte digest、删除不删 Evidence、segmenter 降级);CI 已绿(本计划编写时实测 `go test ./memory/ ./cmd/locomo-bench/` 通过)。
2. **记录接线缺口**:`RebuildSession` 全仓库零调用、集成测试不接线 → episode 投影从未在正式 eval 构建。这是 US2 的真实缺口。
3. **写跨 session 失败测试**到 `memory/semantic_cluster_test.go`:
   - 两个不同 session 的 Evidence,语义相关(共享实体/关键词)→ SemanticClusterer 聚成同一 episode;
   - 语义不相关的跨 session Evidence → 不聚;
   - 每 episode 证据数上限生效(超过上限截断,确定性);
   - config-hash 幂等重建(同 config 删旧建新,digest 稳定);
   - 无 embedding 端点时纯离线信号可用(测试构造共享实体/关键词);
   - 有 embedding 端点时 overlay 可改变判定(可选,stub embedder)。

**Gate**: 上述失败测试在实现前红(未实现 SemanticClusterer);实现后绿。`CGO_ENABLED=0 go test -count=1 ./memory/` 通过。

### Phase B — US1 跨消息语义聚类实现 (SemanticClusterer)

1. 新增 `memory/semantic_cluster.go`:
   - `SemanticClusterer` 接口:`Cluster(ctx, []Evidence) ([]EpisodeCluster, error)`,输出跨 session 的 Evidence 证据簇;
   - 离线判定:`sharedEntityOverlap`(共享实体)+ `sharedKeywordOverlap`(关键词/主题重叠),阈值可配,任一达成即聚(见 research.md Decision 1);
   - embedding overlay:可选 `embedding.Client`,默认 nil(离线路径);
   - 有界:每簇证据数上限(默认值 research.md Decision 2),确定性截断顺序;
   - 全量重建、config-hash 幂等。
2. 扩展 `memory/episode.go`:`EpisodeStore.RebuildAll(ctx, clusterer, builderVersion, configHash)` 遍历 namespace 内全部 active Evidence(跨 session),用 SemanticClusterer 聚类,复用 `buildEpisodeTx` 写 `semantic_episode` projection + lineage(与 RebuildSession 同一条事务路径)。`RebuildSession` 保留(022 验证补全)。
3. 判定统计审计:`decisions / clusters / suspected_mis` 输出(对齐 024 suppression-audit 风格)。

**Gate**: Phase A 的失败测试全绿;`CGO_ENABLED=0 go test -count=1 ./memory/` 通过;`go build ./...` 零错误。engine 跨 session 能力可用。

### Phase C — US1 eval 接线:semantic_episode 臂构建真实 episode

1. `cmd/locomo-bench/main.go`:`--episode-cluster` flag(默认关),开启时在 eval 前对每个 conversation 的 store 调 `EpisodeStore.RebuildAll`(SemanticClusterer 实例,离线或带 embedding overlay);
2. `eval_runner.go`:`formalRepresentationRendererWithEpisodes` 的 semantic_episode 臂在渲染前保证 episode 投影已构建(flag 关时退化为 fallback 直读 anchor source,与现状一致);
3. 复用 022 `semanticEpisodeRenderer`:anchor(fact)→ 经 lineage 定位 episode projection → 渲染 episode narrative;
4. **零行为变化验证**:`--episode-cluster` 关时,`semantic_episode` 臂与现状逐字节一致(FR-003)。

**Gate**: 单元/集成测试通过;`--episode-cluster` 开/关对比的 artifact digest 在关时与现状一致;渲染 bake-off 不增加 answerer 调用数。

### Phase D — US3 双基准配对验证

1. 在 022 冻结协议(cap 3600,repeats≥3)下跑配对:chunk_900 基线 vs semantic_episode(--episode-cluster)。
2. 分类别报告(multi-hop/open-domain/single-hop/temporal)+ 配对统计 + token 记账;表示差异与检索差异显式分离(同 anchor)。
3. LoCoMo 与 LongMemEval-S 双基准;multi-hop 预期增益,负则记录负结果。
4. 结果与 verdict 写入 `specs/025-semantic-episode-cluster/benchmark-registration.md` + `docs/evaluation/reports/`。

**Gate**: 宪法 IV 回归门——overall 不显著回归基线;multi-hop 报告明细。若 semantic_episode 无收益或负收益,机制保持默认关并记录 verdict(FR-010/FR-011)。

## Neighbor Feature Isolation

- 本 feature 在 `025-semantic-episode-cluster` 独立分支工作;无需 worktree(无并行 feature)。
- 024 已合并删除,其 suppression/sibling 代码保留在 master(引擎 `curation/suppress.go`、`ProjectionStore.SiblingFacts`);025 **不复用** write_dedup/neighbor_extend(024 已证伪),只复用 episode 引擎资产。
- 022 已合并(verdict HOLD),其 episode 引擎、v7 表、冻结协议均为本 feature 承接基础。
- 若实现前相关代码被其他 feature 改动,先记录 `git status`/`git log`;发生 collision 时停止并由维护者决定。

## Submission and Verification Strategy

提交顺序保持因果可审查:

1. Phase A:跨 session 聚类失败测试(红)→ 实现(绿),独立提交;
2. Phase B:SemanticClusterer + RebuildAll 引擎能力,独立提交(含审计统计);
3. Phase C:eval 接线(--episode-cluster flag),独立提交;
4. Phase D:配对验证结果 + verdict,独立提交。

每个 engine increment 先写失败测试,再实现。编辑后执行:

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./memory/
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/
CGO_ENABLED=0 go test -count=1 ./...
```

触及聚类/检索的提交不得只凭 unit test 合并;必须按 022 冻结协议完成配对 slice,并在正式默认变更前完成双基准 full 验证(宪法 IV)。长评测按 WSL2 规则用 `setsid` 分离,日志与凭据只进入 session scratchpad。

## Complexity Tracking

无宪法违规需要豁免。
