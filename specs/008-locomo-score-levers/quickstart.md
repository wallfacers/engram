# Quickstart: LoCoMo 跑分杠杆探索(008)验证指引

傻瓜式验证路径。确切命令/契约见 [contracts/bench-commands.md](./contracts/bench-commands.md) 与 [contracts/rerank-sidecar.md](./contracts/rerank-sidecar.md)。产物一律落 gitignored `.locomo-run/008-*/`。凭据只走 env/隧道。

## 前置(一次)

1. 远端 97GB GPU 机就绪(vllm 答题 + 本地 sidecar 共存);SSH 隧道:`ssh -L 8000:127.0.0.1:8000 -L <tunnel>:127.0.0.1:<sidecar-port> …`(host/port/密码现场拿,勿落文件,见 [docs/remote-eval-box.md](../../docs/remote-eval-box.md))。
2. 固化 store 在位:`.locomo-run/007-us2/cov-store`(bge-small 384d)。
3. `CGO_ENABLED=0 go build ./...` 绿。

## US1(旗舰,免费)— 本地 reranker coverage 闸

1. 起 reranker sidecar(bge-small embed + bge-reranker-v2-m3),过 3 步自检(contracts/rerank-sidecar.md §自检),**确认 `/v1/models` 无云 rerank 型号**。
2. 设 `EMBED_BASE_URL / EMBED_MODEL / EMBED_RERANK_MODEL`,跑 US1 命令(bench-commands.md §US1)。
3. **期望**:`coverage.json` 两臂 turn@k;`hybrid+rerank` overall − `hybrid` overall **≥ +0.04** → PASS(本地 reranker 复现可移植赢)。多跳应显著抬升。
4. 记结果 + verdict 到 `eval-log.md`(过闸才进 US4)。零答题/判题调用。

## US2(免费)— open-domain 提示对齐

1. 仅改 `runner.go` 的 `openDomainAnswerPrompt`(5 步推理链,末尾短答案);`go build` + `go test ./cmd/locomo-bench/` 绿;确认 `forceOpenDomainAnswerPrompt` 与选择逻辑未动、引擎 `git diff` 空。
2. 旧版 / 新版各跑 US2 命令(`--only-category 3`,top-k100/quota50,mem0-aligned),产物分 `008-opendomain-old|new`。
3. **期望**:open-domain 旧→新% 实质上升;配对 McNemar 报 b/c/p;以 007 参考产物核查其余三类/全量**无回归**。
4. 记结果到 `eval-log.md`(mechanism 与 eval 分两提交)。

## US3(备胎,免费)— 更大 embedder

1. 起大 embedder sidecar;整店重建到 `008-bge-large-store`;coverage A/B small vs large(bench-commands.md §US3)。
2. **期望**:large − small overall turn@k 为正且 open-domain/多跳明显 → 值得;记大向量 cosine 耗时(honest-scale)。

## US4(需授权)— 落成 opt-in + 新参考点

1. US1 过闸后,端到端默认预算(top-k 30)重跑 `hybrid` vs `hybrid+rerank`,记新参考点 + flip 抽查。
2. reranker **默认 off**;eval 单独提交,明标口径/预算隔离、非涨点叠加。

## 通过标准(对照 spec SC)

- SC-001:US1 overall turn@k ≥ +4pp(或明确 FAIL + 原因)。
- SC-002:US1/US3 零 answer/judge 调用。
- SC-003:US2 单变量,open-domain 升 + 其余三类/全量无回归。
- SC-004:引擎 `git diff` 空。
- SC-005:无云 rerank,sidecar 本地模型经验证。
- SC-006/007:采纳杠杆默认 off;若 US1 过闸,默认预算下拿分 → 消除 007 宽预算伪影。
