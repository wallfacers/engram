# Implementation Plan: 写入侧表示 —— dual-index alias 向量

**Branch**: `011-dual-index-alias` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/011-dual-index-alias/spec.md`

## Summary

给引擎一个 dual-index alias 向量能力:**有 alias 的 fact** 产一条独立 name(`<factname>#alias` 约定)的影子向量(其 aliases 合并去冗后嵌入)存入 `memory_embeddings`;检索时 `vectorRankContext` 在 cosine 全集上把影子 cosine **折叠回源 fact 取 max**(max-pool,截断前归并),影子 name 不进 ranks、不入最终结果,源 fact 的 text 向量与无 alias/chunk 逐字节不变。融合复用现有 `fuseRRF`(k=60)、max-pool 无参数——**无 α、无 schema 变更、tuning-free**。US1 交付纯 Go 引擎能力(退化保真、单测即验、free);US2 在 adapter(`cmd/locomo-bench`)对 009 固化店**只重嵌入不重抽取**补影子向量,过三道门(纯 Go 契约 / 分层召回诊断止损 / 端到端配对 McNemar),证 top-k=30 与 answer-context 不涨。

## Technical Context

**Language/Version**: Go 1.25.0(CGO 禁用,纯 Go / cross-compilable)

**Primary Dependencies**: 引擎内部 —— `vectorRankContext`(semantic 主信号,归并注入点,`retriever.go:821`)、`VectorStore.LoadAllForModel`/`Put`、`embedding.TopKCosine`/`Cosine`、`embedOne`/`embedText`(`embedder.go:68-90`,影子写入注入点)、`memory_event_aliases`(已存,influx 源)、`pipeline.storeFact`/`PutAliases`(`pipeline.go`,enqueue 影子注入点)、`fuseRRF`/`ranksFromOrder`。US2 adapter 侧:`cmd/locomo-bench` 的 store 加载 + `buildAttributionTrace`/`evidenceRecallAt`(分层召回复用)+ McNemar/context-parity 设施。

**Storage**: 复用 009 固化 SQLite store(HF `009-bge-chunks-store`,bge-large 1024d + chunks);baseline/treatment 均复制到 run-local `alias-store` 后只重嵌入不重抽取,baseline Backfill 后剥离影子,treatment 保留;canonical 不打开为运行店。端到端产物写 gitignored `.locomo-run/011-*/`。**无 schema 变更**(`memory_embeddings` 复用,影子为独立 name row,`entry_name` PK 不变)。

**Testing**: `CGO_ENABLED=0 go test ./memory`;US1 parity golden(无 alias fact + chunk semantic 逐字节等于现状)+ 归并正确性 golden(alias 影子强命中经 max-pool 使源 fact 升排名、去重、影子 name 不泄漏,确定性 stub embedder 无 LLM)。US2:分层离线召回 delta(有 alias 子层 vs 全局,coverage 诊断)+ 端到端配对 McNemar 脚本。

**Target Platform**: 本地 Linux(WSL2 开发);US2 决胜门在 box vllm Qwen 栈(远端隧道)+ box bge-large 8001 嵌入 + deepseek mem0-aligned judge。

**Project Type**: engine library(`memory/`)+ eval adapter(`cmd/locomo-bench`)。US1 只动 engine;US2 只动 adapter。

**Performance Goals**: 影子向量数 ≤ 有 alias 的 fact 数(52% 覆盖,每 fact 一条影子,非每 alias 一条);`vectorRankContext` 归并是 O(|candidates|) 单遍 max-pool,不显著超过现有 semantic 延迟;写入侧每有 alias fact 多一次 embed(与 text 向量同 embedder,离线)。

**Constraints**: 引擎离线可跑;无 alias/chunk 与有 alias fact 的 text 向量**逐字节不变**;归并复用现有 RRF 常数 + 无参数 max-pool,**无新可调权重(无 α)**;禁云/付费 reranker(死规则);**最终 top-k=30 不变、answer-context 不涨**(提质硬约束——涨即加量判负);US2 期间 `git diff -- memory embedding provider store internal` 为空。

**Scale/Scope**: LoCoMo locomo10 全量 1540 题(目标类 open-domain/multi-hop;single-hop/temporal 作非目标类监控不回退);单机单用户评测规模。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| **I 本地优先/离线** | ✅ PASS | 检索路径无 query-时 LLM;alias 向量与 fact 同源 embedder、纯 Go/offline;离线时 semantic 缺席、aliases 走原 keyword 通道 |
| **II 引擎/适配器分离** | ✅ PASS | dual-index 归并机制(不可重造算法)入引擎;re-embed 编排 + 三道门留 adapter;US2 引擎零改 |
| **III 契约优先/命名空间** | ✅ PASS | 无 schema 变更、非破坏新增;无 alias/chunk 与 text 向量退化保真;影子 name 不泄漏契约 |
| **IV 评测回归门(非协商)** | ✅ PASS | US2 三道门,分层召回诊断止损 + 端到端配对 McNemar + context-parity;有 alias fact 声明意图变更 + 新 baseline;US1/US2 提交分离 |
| **V 优雅降级/诚实规模** | ✅ PASS | 空 alias/缺向量/离线 per-signal 降级;越不过 NO-GO 保留能力;coverage 不作 verdict;52% 覆盖天花板诚实标注 |
| **死规则(无付费 rerank)** | ✅ PASS | 全程无云/付费 reranker;不撑 top-k、不扩池、不加 context(表示提质非加量) |

初判无违规;Complexity Tracking 空。post-design 复检见文末。

## Project Structure

### Documentation (this feature)

```text
specs/011-dual-index-alias/
├── plan.md              # 本文件
├── research.md          # Phase 0:影子 name 约定 / 归并注入点与截断次序 / 影子写入与枚举 / 生命周期 / re-embed 编排
├── data-model.md        # Phase 1:AliasShadowVector / MaxPooledSemantic / ContextParityCheck / StratifiedRecallDiagnostic
├── quickstart.md        # Phase 1:US1 单测 + US2 三道门流程
├── contracts/           # Phase 1:shadow-embedding 引擎契约 + recall-adapter 分层诊断/context-parity 契约
└── tasks.md             # Phase 2(speckit-tasks,非本命令产出)
```

### Source Code (repository root)

```text
memory/                          # US1 全部在此(engine 增量,单独 commit)
├── embedder.go                  # 改:embedOne 识别 `#alias` name → 查 aliases → 嵌入合并去冗文本 → Put 影子向量;
│                                #   影子枚举(aliases 表 distinct entry_name → 应有影子 names)供 Backfill/re-embed
├── retriever.go                 # 改:vectorRankContext 在 cosine 全集上把 `#alias` 影子折叠回源 fact 取 max(截断前归并),
│                                #   影子 name resolve 回源、去重、不进 ranks/结果;无 alias/chunk 路径逐字节不变
├── pipeline/pipeline.go         # 改:storeFact 对有 alias fact 在 PutAliases 后 enqueue 影子 name
└── *_test.go                    # 新:parity(无 alias/chunk 逐字节)+ 归并 golden(升排名/去重/影子不泄漏)+ 退化

cmd/locomo-bench/                # US2 全部在此(adapter,引擎零改,提交分离)
├── (新)reembed 编排            # 两臂复制 009 店并 Backfill;baseline 剥离影子,treatment 保留;canonical 不写
├── (新)分层召回诊断           # baseline vs treatment,按 gold fact 是否有 alias 分层,复用 attribution/coverage
├── main.go                      # 新 enum:--alias-shadow off|baseline|treatment / --recall-diagnostic 复用 / context-parity 记账
└── (复用现有 store 加载 / journal / stats / McNemar 设施)

.locomo-run/011-*/               # gitignored:分层召回 delta + 端到端配对产物 + context 对比
```

**Structure Decision**: 双层——US1 全部落 `memory/`(engine 增量:embedder 影子写入 + retriever 归并 + pipeline enqueue,退化保真,先交付单独 commit);US2 全部落 `cmd/locomo-bench`(adapter,引擎零改,提交分离)。归并注入点选 `vectorRankContext`(cosine 全集、截断前 max-pool),避免源 fact 低 cosine 被提前截断丢失 alias 增益;影子为独立 name row 而非新表/新列(零 schema 变更);aliases 合并成一条影子(非每 alias 一条),防影子爆炸稀释。

## Complexity Tracking

> 无 Constitution 违规,免填。

## Phase 0/1 产物

- Phase 0:[research.md](./research.md) — 全部技术决策已解(影子 name 约定 / 归并次序 / 影子生命周期 / re-embed 编排 / max-pool 无 α)。
- Phase 1:[data-model.md](./data-model.md)、[contracts/](./contracts/)、[quickstart.md](./quickstart.md)。
- Post-design 宪法复检:归并复用现有 RRF 常数 + 无参数 max-pool、无引擎 schema 变更、无 alias/chunk 与 text 向量退化保真、影子 name 不泄漏 —— 六条仍 PASS,无新增复杂度。
