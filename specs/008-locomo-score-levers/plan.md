# Implementation Plan: LoCoMo 跑分杠杆探索 — 本地 reranker + open-domain 提示(免费闸优先)

**Branch**: `008-locomo-score-levers` | **Date**: 2026-07-22 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/008-locomo-score-levers/spec.md`

## Summary

把「对齐/超越 MemOS 88.83(本地)/ Mem0 92.5」拆成一组**独立可测、免费闸优先**的杠杆,交付到外部 Agent 傻瓜式执行的粒度。四条 User Story:US1 本地 cross-encoder reranker 的零成本 coverage 闸(旗舰,把 003 已证但被死规则作废的 rerank 赢换成本地/可移植形态,且在**可交付默认预算 top-k 30** 下拿分);US2 只改 `openDomainAnswerPrompt` 的 5 步推理链 + `--only-category 3` 单变量 A/B;US3 更大本地 embedder 的 coverage A/B(备胎/可叠加);US4 过闸后落成默认关 opt-in + 声明新参考点。**引擎零改**——reranker 阶段与 `embedding.Reranker` 接口引擎已具备,本特性只在 adapter(`cmd/locomo-bench`)+ 本地 sidecar + env 层动。**死规则**:reranker 一律本地自托管,禁付费云 rerank。

## Technical Context

**Language/Version**: Go 1.25.0(CGO 禁用,纯 Go);sidecar 用 Python 3(stdlib HTTP + fastembed + FlagEmbedding)。

**Primary Dependencies**: 已有引擎 `embedding.Reranker`/`HTTPReranker`(`embedding/rerank.go`)、`memory.NewRetrieverWithOptions` rerank 阶段、`cmd/locomo-bench`(`buildBenchReranker`、`--coverage-only`、`--only-category`、`--retrieval hybrid+rerank`)。新增仅:本地 reranker/embedder sidecar(不进仓库,scratchpad/远端脚本)。

**Storage**: 复用已固化 SQLite store `.locomo-run/007-us2/cov-store`(bge-small 384d,US1/US2);US3 用大 embedder 整店重建到新 `--store-dir`。产物落 gitignored `.locomo-run/008-*/`。

**Testing**: `CGO_ENABLED=0 go test ./cmd/locomo-bench/`(离线,US2 prompt 改动的确定性层);coverage-only bake-off(US1/US3,零 LLM 花费)是免费门;US2 单变量端到端 A/B(近免费,本地答题 + flash 判)。

**Target Platform**: Linux/WSL2 本地 + 远端 97GB GPU 机(reranker/embedder sidecar,与 vllm 共存,SSH 隧道)。

**Project Type**: 评测 adapter 探索(单项目;`cmd/locomo-bench` + 外部 sidecar)。

**Performance Goals**: US1 reranker sidecar 需在 1540 题 × 重排池(默认 top-k30 → widePool≈300 候选)下可接受吞吐(GPU cross-encoder 批处理);记录不含文本的 batch 延迟。coverage-only 全程零答题。

**Constraints**: 死规则(本地 reranker only);引擎目录 `git diff` 必须空;凭据只走 env/隧道;采纳杠杆默认 off(宪法 V);eval 单独提交、声明新参考点(宪法 IV)。

**Scale/Scope**: LoCoMo locomo10 全量 10 段 × 1540 题(cat1-4);US1/US3 coverage 复用固化 store;US2 仅 open-domain(cat3,约 96 题)A/B。

## Constitution Check

*GATE: 必须在 Phase 0 前通过;Phase 1 后复检。*

| 宪法条 | 本特性状态 | 依据 |
|---|---|---|
| **I. Local-first / offline** | ✅ 且**强化** | 全部 reranker/embedder 本地自托管、离线可跑;死规则禁付费云 rerank(FR-001)。任何采纳杠杆默认 off、per-signal 可降级(FR-009)。 |
| **II. 引擎/适配器分离** | ✅ | 引擎零改(FR-002);reranker 复用引擎已有 `Reranker` 接口 + rerank 阶段。改动仅 `cmd/locomo-bench` + 外部 sidecar。`git diff -- memory embedding provider store internal` 必空(SC-004)。 |
| **III. Contract-first / namespace 隔离** | ✅ | sidecar `/rerank` 契约(Cohere/Jina shape)+ bench 命令契约在 Phase 1 冻结(contracts/)。不涉 namespace 变更。 |
| **IV. 评测回归门(非协商)** | ✅ 核心遵循 | 免费闸优先:US1/US3 走 `--coverage-only`(零花费)先证伪;US2 单变量端到端 + 核查其余三类/全量无回归(FR-003/007)。新参考点单独提交、明标口径/预算隔离、非涨点叠加(FR-010, SC-006)。 |
| **V. 优雅降级 / 诚实规模** | ✅ | reranker/大 embedder 默认 off、opt-in、离线可降级(FR-009);更大向量拖慢 Go cosine 如实记(FR-008,honest-scale)。 |
| **死规则(禁付费云 rerank)** | ✅ 中心约束 | FR-001 + SC-005 + edge case:本地 only,禁复活已铲除的 gte-rerank 代理。 |

**Gate 结论**:无违规,无需 Complexity Tracking。本特性是**适配器/评测层**探索,天然满足引擎不可触碰规约;唯一硬风险=死规则,已提为 FR-001/SC-005 中心约束。

## Project Structure

### Documentation (this feature)

```text
specs/008-locomo-score-levers/
├── plan.md              # 本文件
├── research.md          # Phase 0:模型选型/预算/门槛/sidecar 决策
├── data-model.md        # Phase 1:lever / sidecar / coverage 产物实体
├── quickstart.md        # Phase 1:每 US 的可运行验证指引(傻瓜式)
├── contracts/
│   ├── rerank-sidecar.md #   本地 sidecar /rerank + /v1/embeddings 契约
│   └── bench-commands.md #   每 US 的确切 bench 命令 + 判定门契约
├── checklists/
│   └── requirements.md   # 已生成(spec 质量清单)
└── tasks.md             # Phase 2(/speckit-tasks,本命令不生成)
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── runner.go            # US2:仅替换 openDomainAnswerPrompt(5 步推理链);其余逐字不动
├── main.go              # 已有 --coverage-only / --only-category / --retrieval hybrid+rerank / buildBenchReranker;无需改(reranker 由 EMBED_RERANK_MODEL 触发)
├── coverage.go          # 已有 coverage-only + turn@k;US1/US3 复用,不改
└── *_test.go            # US2:离线确定性层(prompt 文本断言,可选)

# 引擎(零改,只读依赖):
embedding/rerank.go      # HTTPReranker:POST {base}/rerank, {results:[{index,relevance_score}]}
memory/retriever.go      # rerank 阶段(NewRetrieverWithOptions)

# 仓库外(不进 git):
<scratchpad|远端>/rerank_sidecar.py   # US1:bge-small embed + bge-reranker-v2-m3 cross-encoder
<scratchpad|远端>/embed_large.py      # US3:大 embedder sidecar
.locomo-run/008-*/       # gitignored 产物(coverage.json / results / 日志)
.locomo-run/007-us2/cov-store          # 复用的固化 store(US1/US2)
```

**Structure Decision**: 单项目 adapter 探索。改动面**最小**:US1/US3 **零 Go 改动**(纯 sidecar + env + 已有 flag);US2 **仅** `runner.go` 一处 prompt 常量(可选加离线断言测试)。引擎全程只读依赖,`git diff -- memory embedding provider store internal` 保持空。

## Complexity Tracking

> 无宪法违规,无需填写。
