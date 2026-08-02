# engram 023 eval 远端环境（AutoDL 48GB）— 完整复现手册

> 目的：下次租机/后续 Agent 可 100% 复现 023 T003 的 eval 环境，避免重新摸索。
> 更新于 2026-08-03（T003 运行实测）。

## 1. 机器与登录

- **AutoDL 按分钟计费**，空闲必停。登录凭据每次重启轮换，由维护者现场提供（经 env/tunnel，绝不落盘）。
- 登录后 **系统盘仅 ~30G，数据盘 `/root/autodl-tmp` ~250G**。eval run-dir 必须放数据盘（`/root/autodl-tmp/`），系统盘只放代码。

## 2. 目录布局（数据盘）

```text
/root/autodl-tmp/
├── 023-venv/                # Python venv：vllm 0.26 + huggingface_hub + modelscope
├── go/ go1.25.0.linux-amd64.tar.gz  gocache/  gopath/   # Go 1.25 toolchain
├── hf-cache/                # HF_HOME：Qwen3.6-35B-A3B-FP8 (~11G) + bge-large-en-v1.5
├── 023-runs/                # eval run 目录（t003-*）
├── locomo-bench             # 编译好的 locomo-bench 二进制（CGO=0）
├── serve.sh                 # vllm 启动脚本（answerer 8000 + embedding 8010）
├── build-bench.sh           # 本地编译 locomo-bench 后上传的命令
└── engram-src.tgz           # 源码快照
```

代码工作副本：`/root/engram`（git HEAD 34d641e，协议冻结 commit）。

## 3. vllm 启动（关键命令）

**Embedding（8010）**：
```bash
/root/autodl-tmp/023-venv/bin/python -m vllm.entrypoints.openai.api_server \
  --model /root/autodl-tmp/hf-cache/bge-large-en-v1.5 \
  --served-model-name BAAI/bge-large-en-v1.5 \
  --convert embed --dtype float32 --port 8010 \
  --max-model-len 8192 --gpu-memory-utilization 0.10
```

**Answerer（8000）**：
```bash
/root/autodl-tmp/023-venv/bin/python -m vllm.entrypoints.openai.api_server \
  --model /root/autodl-tmp/hf-cache/Qwen3.6-35B-A3B-FP8 \
  --served-model-name Qwen/Qwen3.6-35B-A3B-FP8 \
  --dtype auto --port 8000 --max-model-len 16384 \
  --max-num-seqs 128 --gpu-memory-utilization 0.85 --trust-remote-code
```

**token counter**：B1 formal 的 token 计数走 answerer 的 `POST /tokenize` 端点
（`--token-counter-base-url http://127.0.0.1:8000/v1`，代码里 `TrimSuffix("/v1")` 转根路径）。

> ⚠️ vllm 0.26 注意：embedding 用 `--convert embed`（不是 `--task embed`）；
> **answerer 曾 `EngineDeadError` 崩溃**（Qwen3.6-35B-A3B-FP8 Mamba 层疑似 bug），崩溃后
> token counter 不可用 → 全题 invalid。重启即可，store 复用避免重付 extraction。

## 4. 模型清单（HF cache）

| 模型 | 用途 | HF 引用 | 大小 |
|---|---|---|---|
| Qwen3.6-35B-A3B-FP8 | answerer + extract | `Qwen/Qwen3.6-35B-A3B-FP8` | ~11G |
| BAAI/bge-large-en-v1.5 | embedding | `BAAI/bge-large-en-v1.5` | ~1.3G |
| deepseek-v4-flash | judge | DeepSeek anthropic 端点 | API |
| Qwen2.5-7B-Instruct | 023 训练底模（本地） | `Qwen/Qwen2.5-7B-Instruct` | ~15G BF16 |

## 5. env 配置（secret 值不落盘，仅格式）

```bash
# judge（DeepSeek anthropic 兼容端点；JUDGE_* 优先，fallback 到 LOCOMO_*）
export JUDGE_PROVIDER=anthropic
export JUDGE_BASE_URL=https://api.deepseek.com/anthropic
export JUDGE_MODEL=deepseek-v4-flash
export JUDGE_API_KEY=<from judge.env, never tracked>

# answerer + extract（本地 vllm）
export LOCOMO_PROVIDER=openai
export LOCOMO_BASE_URL=http://127.0.0.1:8000/v1
export LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export LOCOMO_MODEL_REVISION=Qwen/Qwen3.6-35B-A3B-FP8
export EXTRACT_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export LOCOMO_API_KEY=local-eval

# embedding
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export EMBED_BASE_URL=http://127.0.0.1:8010/v1
export EMBED_API_KEY=local-eval

# 离线
export HF_HUB_OFFLINE=1
```

## 6. B1 formal 协议关键参数（T003 freeze）

- protocol hash：`sha256:65f6769c6e128791bcf0900f91945dad932fcb9c0abed64edb0613ce46e01f6e`
- counter fingerprint：`sha256:4806660dd8fa2e2b5cfe9ebfefed34e661faf4ba4cace6b7d119ddf6ddc8239b`
- stage=b1, arm=legacy_count_packer, cap=5000, repeats=3, retrieval=hybrid, top-k=30, chunk-quota=12
- 机制全禁：idk_retry/iris/rerank=false

## 7. T003 跑法（对照 runbook）

```bash
BENCH=/root/autodl-tmp/locomo-bench
DATA=/root/engram/testdata/locomo/locomo.json
STORE=/root/autodl-tmp/023-runs/t003-store
RUNS=/root/autodl-tmp/023-runs
BASE="--data $DATA --chunks --top-k 30 --chunk-quota 12 --retrieval hybrid \
      --force-answer --judge-mem0-aligned --concurrency 25 --repeats 3 --max-tokens 8000"

# B1 deterministic（D 集合）
$BENCH --eval-protocol $RUNS/t003-freeze.json --run-dir $RUNS/t003-b1-v2 $BASE \
  --token-counter-base-url http://127.0.0.1:8000/v1 --store-dir $STORE

# fixed-gold oracle（G 集合，跳过空 evidence 题）
$BENCH --eval-protocol $RUNS/t003-freeze.json --run-dir $RUNS/t003-oracle-v2 $BASE \
  --token-counter-base-url http://127.0.0.1:8000/v1 --fixed-gold-oracle

# residual 对拍
python3 training/planner/residual_compare.py \
  --b1-classification $RUNS/t003-b1-v2/classification.jsonl \
  --oracle-artifact $RUNS/t003-oracle-v2/fixed_gold_oracle.jsonl \
  --out $RUNS/t003-oracle-v2/residual_cohort.json
```

## 8. 已知数据缺陷（oracle 已修复跳过）

LoCoMo 官方 locomo.json 有 **4 题 gold evidence 为空**（category 3）：
`conv-0-q-30`、`conv-0-q-46`、`conv-9-q-39`、`conv-9-q-42`。
fixed-gold oracle 遇之即 fail（`gold_evidence_empty` → cancelRun 级联），已在
commit `b359dc6` 修复：跳过并从 denominator 排除（summary 记 `empty_evidence_skipped`）。
B1 不受影响（走 chunks 检索）。
