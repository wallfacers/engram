# Phase 1 Data Model: LoCoMo 跑分杠杆探索(008)

本特性是评测探索,无引擎 schema 变更(引擎零改)。以下为**概念实体**,用于统一 plan/tasks/产物口径。

## Entity: 杠杆(Lever)

一条独立可测的检索/答题改动。

| 字段 | 类型 | 说明 |
|---|---|---|
| `name` | string | 如 `local-rerank` / `open-domain-prompt` / `bge-large-embed` |
| `layer` | enum | `retrieval` \| `answer` \| `embedding` |
| `gate_type` | enum | `coverage-only`(免费,US1/US3) \| `single-var-e2e`(近免费,US2) |
| `budget` | string | US1=`top-k30/quota12`(默认预算);US2=`top-k100/quota50`(对齐 007 参考点) |
| `threshold` | string | US1=`overall turn@k ≥ +4pp`;US2=`open-domain% ↑ 且其余三类/全量无回归` |
| `verdict` | enum | `win` \| `marginal` \| `dead` \| `NO-GO`(未判=`pending`) |
| `default_off` | bool | 采纳后必须 `true`(宪法 V) |
| `artifact_dir` | path | gitignored `.locomo-run/008-<name>/` |

**状态流转**:`pending` →(过闸)`win`/`marginal` →(US4 授权端到端 + 落 opt-in)已交付;或 →(未过闸)`dead`/`NO-GO`(记坟场,不进默认)。

## Entity: 本地 reranker sidecar

自托管双端点服务(US1)。**无云依赖**。

| 端点 | 契约 |
|---|---|
| `POST /v1/embeddings` | OpenAI 兼容;`bge-small-en-v1.5` 384d(与固化 store 同源) |
| `POST /rerank`(裸)/ `POST /v1/rerank` | 请求 `{model,query,documents:[str],top_n}`;响应严格 `{"results":[{"index":int,"relevance_score":float}]}`(Cohere/Jina shape,引擎 `HTTPReranker` 期望) |
| `GET /v1/models` | 列 `bge-small-en-v1.5` + rerank 模型名;**不得**列任何云 rerank 型号 |

约束:`index` 必须在 `[0,len(documents))` 内(越界→引擎报错→静默降级为融合序);模型只从本地文件加载。详见 [contracts/rerank-sidecar.md](./contracts/rerank-sidecar.md)。

## Entity: coverage 产物(coverage.json)

US1/US3 的唯一免费判据。

| 字段 | 说明 |
|---|---|
| per-arm `turn_recall_at_k` | overall + {multi-hop, temporal, open-domain, single-hop} |
| per-arm `session_recall` | 段级召回(与 chunk-quota 无关) |
| 判定 | `hybrid+rerank` − `hybrid` 的 overall turn@k(US1);large − small(US3) |

## Entity: A/B 端到端产物(US2)

| 字段 | 说明 |
|---|---|
| `results-openDomain-{old,new}.jsonl` | 各含 open-domain 逐题 `{question,gold,predicted,correct}` |
| 派生 | open-domain 旧→新%、配对 McNemar(b/c/p)、其余三类/全量无回归核查(对 007 原参考产物) |

## 提交切分(宪法 IV)

- **mechanism commit**:US2 的 `runner.go` prompt 改动(+ 可选离线断言测试);US1/US3 无 Go 改动(纯 sidecar/env)→ 无 mechanism commit,只有 eval-log。
- **eval commit**:任何过闸数字写入 `specs/008-locomo-score-levers/eval-log.md`,单独提交,明标口径/预算隔离、非涨点叠加。
