# Quickstart: Counterfactual Evidence Utility Gate

本文描述实现完成后的预注册执行顺序。命令中的 dataset/store/model revision 由维护者替换为当批冻结值；credential 必须已在
当前 shell 通过环境注入，不能写进脚本、run artifact 或 tracked file。

## 1. WSL2 implementation gates

本节只在维护者的 WSL2 working copy / 042 隔离 worktree 中执行：

```bash
cd /home/wushengzhou/workspace/github/engram/.claude/worktrees/042-counterfactual-evidence-utility
git status --short
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
CGO_ENABLED=0 go build ./...
git diff --name-only -- memory embedding provider store internal
```

最后一条必须无输出。042 worktree 已更新到 `afe9647`，并包含删除 041/040 的 commits `3cff168` / `ac5c66c`；若后续
HEAD 改变，仍须确认这两次 cleanup 在 ancestry 中，不能复制已删除实现。

## 2. AutoDL source sync, hygiene and binary

先按维护者的正常安全流程把已审查 source revision 同步到 AutoDL working copy；不要把 WSL2 二进制当作远端 build 的替代。
下列命令全部在 AutoDL 执行，正式模型批次只在 data disk 下落盘：

```bash
df -h /
mkdir -p /root/autodl-tmp/042-runs
mkdir -p /root/autodl-tmp/042-bin
CGO_ENABLED=0 go build -o /root/autodl-tmp/042-bin/locomo-bench ./cmd/locomo-bench
```

若 `/` 超过 80%，先按项目 runbook 备份旧 run 的小文件和 store 到 `/root/autodl-tmp`，再由维护者确认清理；不要把
042 run-dir 放在 `/root`。answerer 必须是本机 vLLM 或经 SSH tunnel 暴露到 loopback 的 OpenAI-compatible endpoint。

在启动前冻结非 secret 环境：

```bash
export LOCOMO_PROVIDER=openai
export LOCOMO_BASE_URL=http://127.0.0.1:8000/v1
export LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export LOCOMO_MODEL_REVISION=<exact-model-revision>
export LOCOMO_MAX_MODEL_LEN=32768
export LOCOMO_SERVER_CONFIG_DIGEST=sha256:<digest-of-non-secret-vllm-config>
export LOCOMO_NO_THINKING=0
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_MODEL_REVISION=<exact-embedding-model-revision>
export EMBED_SERVER_CONFIG_DIGEST=sha256:<digest-of-non-secret-embedding-vllm-config>
export EMBED_MAX_NUM_SEQS=1
export JUDGE_PROVIDER=<frozen-provider>
export JUDGE_BASE_URL=<frozen-endpoint>
export JUDGE_MODEL=<frozen-judge-model>
export JUDGE_MODEL_REVISION=<exact-judge-revision>
```

`LOCOMO_API_KEY`、`JUDGE_API_KEY` 等 credential 由维护者在当前 shell 单独注入。不要 echo、记录或内嵌它们。先用正常
sidecar health check确认模型已启动。answer vLLM 的实际启动 argv 必须含 `--max-model-len 32768`；16384 无法同时容纳
top-k150 输入与 `max_tokens=8000`。embedding vLLM 的实际启动 argv 必须含 `--max-num-seqs 1`，避免并发下同 query 向量漂移；
上述两个 config digest 只覆盖非 secret launch/model 参数。

runner 会在首条 benchmark call 前独立执行三项 fail-closed preflight：store 中
`memory_embeddings(model,dims)` 与 bge-large identity/1024d 一致；同一 embedding probe 两次 byte-identical；answer endpoint
支持 non-streaming top-2 logprobs。answer request 与现 recipe 一样省略 `temperature` 字段，不是显式温度 0；judge 固定
mem0-aligned 且只接收 clean final answer。任一 provenance mismatch 不得降级开跑。

## 3. Stage `label`: historical constructor regression

该步骤零模型调用。运行前先在 AutoDL 只读核验历史 roots 的实际布局；两边都必须恰有 `run-1/2/3`，且每个 rep 只有一个
可识别的 hybrid results JSONL：

```bash
find /root/autodl-tmp/032-think3/keep -maxdepth 2 -type f -name '*.jsonl' -print
find /root/autodl-tmp/topk-full/tk150-full3 -maxdepth 2 -type f -name '*.jsonl' -print
test -d /root/autodl-tmp/032-think3/keep/run-1
test -d /root/autodl-tmp/032-think3/keep/run-2
test -d /root/autodl-tmp/032-think3/keep/run-3
test -d /root/autodl-tmp/topk-full/tk150-full3/run-1
test -d /root/autodl-tmp/topk-full/tk150-full3/run-2
test -d /root/autodl-tmp/topk-full/tk150-full3/run-3
```

任何布局/denominator/identity 不满足就停止，不生成 tasks 中的模型运行步骤。核验后必须得到 56 BENEFIT / 31 HARM：

```bash
/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage label \
  --utility-shallow-source /root/autodl-tmp/032-think3/keep \
  --utility-deep-source /root/autodl-tmp/topk-full/tk150-full3 \
  --run-dir /root/autodl-tmp/042-runs/label-audit
```

检查：

```bash
jq '{verdict, counts, claim}' /root/autodl-tmp/042-runs/label-audit/label-report.json
```

预期：`verdict=GO`、BENEFIT=56、HARM=31、NEUTRAL=1453、claim 仅为 label constructor regression。任何偏差都停止，
先查 denominator、clean/raw judge 口径和 run-root identity；不得修改 truth table 去对齐数字。

## 4. Stage `collect`: fresh paired LoCoMo signals/outcomes

这是长任务，必须脱离运行。下例沿用高分栈的 frozen recipe；dataset/store 路径以当机权威路径替换：

```bash
setsid bash -c '/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage collect \
  --utility-label-source /root/autodl-tmp/042-runs/label-audit \
  --data /root/autodl-tmp/datasets/locomo.json \
  --dataset-format locomo \
  --store-dir /root/autodl-tmp/032-store \
  --run-dir /root/autodl-tmp/042-runs/collect \
  --retrieval hybrid \
  --chunks --chunk-quota 12 \
  --force-answer --judge-mem0-aligned \
  --trace-mediation=false \
  --repeats 3 --max-tokens 8000 --concurrency 32 \
  > /root/autodl-tmp/042-runs/collect.log 2>&1; \
  echo $? > /root/autodl-tmp/042-runs/collect.exit' \
  </dev/null >/dev/null 2>&1 & disown
```

单次即时轮询，不运行 foreground sleep loop：

```bash
test -f /root/autodl-tmp/042-runs/collect.exit && cat /root/autodl-tmp/042-runs/collect.exit
tail -n 5 /root/autodl-tmp/042-runs/collect.log
```

有效完成应有：1540 questions、4620 decision units、9240 成功 logical answer calls（4620 shallow + 4620 `paired_deep`）、
全部 judge outcomes/utility labels、embedding/answer/judge preflight available 和 COMPLETE seal。provider attempts 可因合法 retry
大于 logical calls；每次 retry 的 calls/latency/token charge 都必须入账。成功 response 但 logprob capability unavailable 是低成本
NO-GO；retry 耗尽、non-retryable call failure、provenance mismatch 或 tamper 是 INVALID，二者不能混写。

## 5. Stage `diagnose`: offline LOCO gate

该步骤纯离线、无需 provider env或网络：

```bash
/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage diagnose \
  --utility-source /root/autodl-tmp/042-runs/collect \
  --run-dir /root/autodl-tmp/042-runs/diagnose
```

查看冻结结果：

```bash
jq '{verdict, claim, quality, cost, signal_availability, gates}' \
  /root/autodl-tmp/042-runs/diagnose/diagnostic-report.json
```

必须同时看到：10 个完整 LOCO folds、conversation overlap=0、cross-fit policy 相对 shallow 至少 +25 题、policy 不低于
same-batch deep、absolute accuracy ≥90%、simulated full-path token ratio ≤0.60、类别门通过。报告必须列出 fresh
`D=deep-shallow` 与 `required=max(25,D)`；若 `D<25`，policy 超过 deep `25-D` 题是预注册的刻意从严门，不得临时降成 D。
某 fold 类别 denominator 为零时 rate 应为 `null + reason`，fold 仍保留；只有零训练行、数值、coverage/provenance/leakage
错误才使整个 diagnose INVALID。

若 verdict 不是 GO，本 feature 在这里 NO-GO 收口：不启动 confirm，不看某个 feature/threshold 的事后替代，不运行 LME。

## 6. Stage `confirm`: fresh LoCoMo conditional execution

只有 diagnostic GO 才运行。它必须是新 run-dir、新 answer generations；环境/recipe/model/store 与 collect 一致：

```bash
setsid bash -c '/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage confirm \
  --utility-source /root/autodl-tmp/042-runs/diagnose \
  --data /root/autodl-tmp/datasets/locomo.json \
  --dataset-format locomo \
  --store-dir /root/autodl-tmp/032-store \
  --run-dir /root/autodl-tmp/042-runs/confirm \
  --retrieval hybrid \
  --chunks --chunk-quota 12 \
  --force-answer --judge-mem0-aligned \
  --trace-mediation=false \
  --repeats 3 --max-tokens 8000 --concurrency 32 \
  > /root/autodl-tmp/042-runs/confirm.log 2>&1; \
  echo $? > /root/autodl-tmp/042-runs/confirm.exit' \
  </dev/null >/dev/null 2>&1 & disown
```

完成后检查：

```bash
jq '{verdict, claim, quality, utility, cost, categories, gates, production_authorized}' \
  /root/autodl-tmp/042-runs/confirm/evaluation-report.json
```

LoCoMo final GO 是严格合取：policy correct 不低于 fixed k150、绝对分 ≥90%、完整 policy token ratio ≤0.60、类别门通过。
exact McNemar/CI 只解释噪声，不可用“不显著”豁免少一题。`production_authorized` 必须仍为 false。

## 7. Stage `transfer`: LongMemEval non-regression

只有 confirm GO 才运行。global rule 已在 LME 标签可见前冻结；下例不得增加 LME-specific router 或 threshold：

```bash
setsid bash -c '/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage transfer \
  --utility-source /root/autodl-tmp/042-runs/confirm \
  --data /root/autodl-tmp/datasets/longmemeval_s.json \
  --dataset-format longmemeval \
  --store-dir /root/autodl-tmp/lme-s500-store \
  --run-dir /root/autodl-tmp/042-runs/transfer-lme \
  --retrieval hybrid \
  --chunks --chunk-quota 12 \
  --force-answer --judge-mem0-aligned \
  --trace-mediation=false \
  --repeats 3 --max-tokens 8000 --concurrency 32 \
  > /root/autodl-tmp/042-runs/transfer-lme.log 2>&1; \
  echo $? > /root/autodl-tmp/042-runs/transfer-lme.exit' \
  </dev/null >/dev/null 2>&1 & disown
```

检查：

```bash
jq '{verdict, claim, no_retune, quality, cost, categories, portable_claim_authorized, production_authorized}' \
  /root/autodl-tmp/042-runs/transfer-lme/evaluation-report.json
```

LME policy correct 少于同批 fixed-k150 即 transfer NO-GO。此时最终表述必须是“LoCoMo mechanism GO，但不可移植、不得产品
晋级”；不能回到 LoCoMo 改 feature/scaler/threshold 后重测同一 LME。即使 transfer GO，生产接线仍需新的 contract-first feature。

## 8. Final audit

每个 source chain 均应离线重验：

```bash
/root/autodl-tmp/042-bin/locomo-bench \
  --utility-stage diagnose \
  --utility-source /root/autodl-tmp/042-runs/collect \
  --run-dir /root/autodl-tmp/042-runs/diagnose-replay
```

`diagnose-replay` 的 fold rules、crossfit decisions、report semantic digest 必须与首轮一致；`created_at/run_id` 等非语义字段
可不同。最终 verdict 文档必须分别列出 historical label audit、fresh cross-fit diagnostic、fresh LoCoMo confirmation、LME transfer，
不得把 oracle、training fold、simulated cost 或 global-rule LoCoMo in-sample结果当成部署成绩。
