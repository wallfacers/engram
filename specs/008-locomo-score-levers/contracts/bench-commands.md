# Contract: 每 US 的确切 bench 命令 + 判定门

**冻结**——外部执行者照抄,不得改动预算/臂/口径参数(改了就不是单变量)。产物落 gitignored `.locomo-run/008-<name>/`。凭据只走 env。WSL2 长跑 setsid 分离(见 CLAUDE.md 硬规)。

## 共同 env

```
# 本地答题/抽取(US2 端到端用):
LOCOMO_PROVIDER=openai  LOCOMO_BASE_URL=http://127.0.0.1:8000/v1  LOCOMO_MODEL='Qwen/Qwen3.6-35B-A3B-FP8'  LOCOMO_API_KEY=local-eval  EXTRACT_MODEL='Qwen/Qwen3.6-35B-A3B-FP8'
# 本地 sidecar(US1/US3):
EMBED_BASE_URL=http://127.0.0.1:<tunnel>/v1  EMBED_MODEL=bge-small-en-v1.5  EMBED_API_KEY=local
EMBED_RERANK_MODEL=bge-reranker-v2-m3        # US1:触发 hybrid+rerank 臂;US3 不设
# 判题(US2 端到端用):
JUDGE_PROVIDER=openai  JUDGE_BASE_URL=https://api.deepseek.com  JUDGE_MODEL=deepseek-v4-flash  JUDGE_API_KEY=<env 提供,勿落文件>
```

## US1 — 本地 reranker 免费 coverage 闸

```bash
CGO_ENABLED=0 go run ./cmd/locomo-bench \
  --data testdata/locomo/locomo10.json \
  --store-dir .locomo-run/007-us2/cov-store \
  --coverage-only --chunks --top-k 30 --chunk-quota 12 \
  --retrieval 'hybrid,hybrid+rerank' \
  --run-dir .locomo-run/008-local-rerank
```
- **前置**:reranker sidecar 已过自检(contracts/rerank-sidecar.md);`EMBED_RERANK_MODEL` 已设。
- **零花费**:coverage-only → 无 answer/judge 调用。
- **判定门(SC-001)**:读 `.locomo-run/008-local-rerank/coverage.json`,`hybrid+rerank` overall turn@k − `hybrid` overall turn@k **≥ +0.04** → PASS(多跳为关键类,对照付费版 +0.083)。否则 FAIL:换下一个本地 reranker(jina/mxbai)重试一次,仍不过记坟场→转 US3。
- **死规则核查**:跑前 `curl $EMBED_BASE_URL/models` 确认无云 rerank 型号。

## US2 — open-domain prompt 单变量 A/B

前置:仅改 `cmd/locomo-bench/runner.go` 的 `openDomainAnswerPrompt`(5 步推理链);`forceOpenDomainAnswerPrompt` 与选择逻辑逐字不动。两版各在**旧代码**与**新代码**下跑:

```bash
# 旧版(git stash / 旧 worktree)与新版各跑一次,其余参数完全一致:
CGO_ENABLED=0 go run ./cmd/locomo-bench \
  --data testdata/locomo/locomo10.json \
  --store-dir .locomo-run/007-us2/cov-store \
  --chunks --top-k 100 --chunk-quota 50 \
  --retrieval hybrid --only-category 3 --judge-mem0-aligned \
  --run-dir .locomo-run/008-opendomain-<old|new>
```
- **近免费**:仅 open-domain(cat3,约 96 题)× 2 版答题 + flash 判。
- **判定门(SC-003)**:
  1. open-domain 旧→新% + 配对 McNemar(b=旧对新错, c=旧错新对, p);要求 open-domain 实质上升。
  2. **无回归**:以 007 原全量参考产物(`.locomo-run/007-us2/results-hybrid-mem0.jsonl`)核查 multi-hop/temporal/single-hop 与全量不因本改动下降超噪声。open-domain 单类跑不动其它类,故其它类用参考产物即可,不必重跑。
- **单变量铁律**:除 `openDomainAnswerPrompt` 文本外,两次 run 的 flag/store/judge/answer 模型逐字相同。

## US3 — 更大本地 embedder 免费 coverage A/B(备胎)

```bash
# 1) 大 embedder sidecar 起好(端口/模型见 contracts);整店重建:
CGO_ENABLED=0 go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir .locomo-run/008-bge-large-store --chunks \
  # (EMBED_MODEL=bge-large-en-v1.5, 本地 Qwen 抽取;--coverage-only 前先建店)
# 2) coverage A/B:small-store vs large-store,同预算:
CGO_ENABLED=0 go run ./cmd/locomo-bench --data testdata/locomo/locomo10.json \
  --store-dir <small|large>-store --coverage-only --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --run-dir .locomo-run/008-embed-<small|large>
```
- **判定门(SC-002 免费)**:large − small overall turn@k 为正且 open-domain/多跳明显 → 值得(可与 US1 叠);记录大向量对 Go cosine 扫描的耗时(honest-scale, FR-008)。

## US4 — 过闸落成 opt-in + 声明新参考点(需授权)

- 端到端在**默认预算 top-k 30** 重跑 `hybrid` vs `hybrid+rerank`(US1 过闸后),记录新参考点 X%(标「本地 reranker,默认关」)+ false→true flip 抽查。
- reranker 保持**默认 off**,仅 opt-in;eval 写入 `eval-log.md`,单独提交,明标口径/预算隔离、非涨点叠加(FR-010, SC-006)。

## 全局硬门(每条都适用)

- `git diff --name-only -- memory embedding provider store internal` **必空**(引擎零改, SC-004)。
- `CGO_ENABLED=0 go build ./...` 与 `go test ./cmd/locomo-bench/` 绿。
- 死规则:reranker 本地 only,禁云 rerank(SC-005)。
