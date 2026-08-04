# 023 Planner r5 三臂配对评测 runbook

执行中：2026-08-04。三臂 = deterministic (extractive) / prompt-only / supervised，
同一 `t003-store` + 同一冻结协议（唯一差异 = compiler-arm 训练状态）。目标：确认
Planner 训练是否带来可归因、可迁移的涨点（FR-023/024/025）。

## 协议（compiler=true 冻结协议）

022 B1 基线协议 `t003-freeze.json` 的 compiler mechanism flag = `false`（legacy packer）。
`--compiler-arm` 非空即请求 `compiler=true`，必须用**重新冻结**的协议（mechanism-bindings
rule 4：每个 mechanism 绑定独立 B1 manifest）。

冻结命令（本地 clean git worktree + 与 v3 一致的环境指纹）：

```bash
# 环境指纹必须与 v3 完全一致（协议 hash 才会绑定同一基线）
export LOCOMO_PROVIDER=openai
export LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export EXTRACT_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
export EMBED_MODEL=BAAI/bge-large-en-v1.5
export JUDGE_PROVIDER=anthropic
export JUDGE_MODEL=deepseek-v4-flash

./locomo-bench --eval-freeze-protocol t003-compiler-freeze.json \
  --data testdata/locomo/locomo.json \
  --retrieval hybrid \
  --compiler-arm extractive \
  --force-answer --judge-mem0-aligned \
  --eval-budget-profile high \
  --answer-input-cap 5000 \
  --counter-fingerprint sha256:4806660dd8fa2e2b5cfe9ebfefed34e661faf4ba4cace6b7d119ddf6ddc8239b \
  --chunks --top-k 30 --chunk-quota 12
```

冻结后校验与 v3 逐项一致（除 `mechanism.compiler=true`）：
- embed fingerprint `sha256:a69101...`（bge-large-en-v1.5）
- extractor/answerer provider `openai`、id `Qwen/Qwen3.6-35B-A3B-FP8`、prompt `8187fe...`
- judge provider `anthropic`、id `deepseek-v4-flash`、prompt `99bd7d...`
- 协议 hash（compiler 版）：`sha256:f989f9a2...`（v3 基线 hash `65f6769c...`）

## 服务编排（48G 单卡，35B+7B 不能同载）

GPU 实测：35B @ 0.85 util ≈ 42.4G / 49G，free 仅 6G；7B ≈ 8G。**两模型不能同时跑**。
三臂运行顺序（每臂完成后切模型）：

| 臂 | answerer (8000) | planner (8001) | compiler-arm |
|---|---|---|---|
| deterministic (extractive) | 35B | 无 | `extractive` |
| prompt-only | 35B | 7B base（无 adapter） | `planner` + `--planner-base-url` |
| supervised | 35B | 7B + LoRA adapter | `planner` + `--planner-base-url` |

> 显存冲突解法待实测：35B 降 util 至 0.60（~30G）+ 7B 0.10（~4G）≈ 34G < 49G，
> 但 35B 需要 KV cache 空间（当前 42G 含 cache）。或者 planner 臂用 separate-conv
> 分批。**若同卡不可行，planner 臂需单跑 7B + 用已生成的 extractive 候选**——这违背
> 配对单变量纪律，是备选方案，不优先。

## 运行命令（deterministic 臂已验证跑通）

```bash
# 远程 /root/autodl-tmp/run-det-full.sh
# cd /root/engram-git  (clean git repo @ 582035a，协议 git 校验需要)
/root/autodl-tmp/locomo-bench-r5 --eval-protocol t003-compiler-freeze.json \
  --run-dir /root/autodl-tmp/023-runs/paired-det-full \
  --store-dir /root/autodl-tmp/023-runs/t003-store \
  --data locomo.json --chunks --top-k 30 --chunk-quota 12 \
  --retrieval hybrid --force-answer --judge-mem0-aligned \
  --concurrency 8 --repeats 3 --max-tokens 8000 \
  --compiler-arm extractive \
  --token-counter-base-url http://127.0.0.1:8000/v1
```

## 关键踩坑（已解决）

1. **vllm 0.26 `--task` 参数被移除** → embed 用 `--convert embed`；answerer 无 `--task`。
2. **并发启动 vllm 冲突** → embed/answerer 必须串行启动（首次并发导致 EngineCore 初始化失败）。
3. **embed max-model-len 8192 > bge 512** → 设 `--max-model-len 512`。
4. **协议 mechanism `compiler=false` 拒绝 `--compiler-arm`** → 重新冻结 compiler=true 协议。
5. **freeze 环境指纹漂移**（provider/model/embed/prompt digest 全错）→ 必须显式设 6 个 env
   与 v3 一致；否则 `model providers differ` / `embedding fingerprint differs` /
   `answer or judge prompt differs` 三连失败。
6. **协议 git 校验**：运行时必须在 clean git repo 且 HEAD = 协议 commit → 远程 clone
   `/root/engram-git`（582035a）。
7. **正式 B1 拒绝 dataset/question sampling** → 不能 `--conversations N` smoke；链路验证
   靠观察早期 log（store 复用 + answer POST 200）。

## 判定（T020）

- Primary Cohort majority Δ（supervised vs deterministic）≥ +2.0pp
- exact McNemar（Holm 校正）p<0.05
- Guard overall（prompt-only vs supervised 全类 non-regression）≥ −0.5pp
- validity 全绿（candidate/source/span/citation/within-cap、answerer=1、retrieval=0）
- 类目 non-regression

## 80G Blackwell (RTX PRO 6000) 服务启动踩坑（2026-08-04 实测）

vllm 0.26 + flashinfer 0.6.14 对 Blackwell sm_120/CUDA 13 支持不完整，35B MoE 启动
连踩 6 个 kernel 编译错误。最终可用组合（serve-ans80g.sh）：

```bash
export FLASHINFER_CUDA_ARCH_LIST="12.0"          # flashinfer arch 自动检测失败
export CUDA_HOME=/root/autodl-tmp/023-venv/lib/python3.12/site-packages/nvidia/cu13
export CUDA_PATH=$CUDA_HOME                     # flashinfer 误用系统 nvcc 12.8
export VLLM_USE_FLASHINFER_SAMPLER=0            # 跳过 flashinfer 采样 JIT（CUDA13/CCCL 头不兼容）
python -m vllm.entrypoints.openai.api_server --model ... \
  --moe-backend triton \                          # deepgemm/cutlass FP8 MoE 均不可用
  --max-num-seqs 256 --gpu-memory-utilization 0.80
```

踩坑链（每步一个错误，逐步定位）：
1. `FlashInfer requires sm75+` → FLASHINFER_CUDA_ARCH_LIST=12.0
2. `SM 12.x requires CUDA >= 12.9` → CUDA_HOME 指向 torch 的 cu13（否则用系统 nvcc 12.8）
3. `deepgemm NVCC compilation failed` → --moe-backend triton
4. `CUTLASS FP8 MoE disabled` → 换 triton
5. `CUDA compiler and toolkit headers incompatible` → VLLM_USE_FLASHINFER_SAMPLER=0
   （注意：该 env 需要整数 0/1，不是 "false"——会 `int()` 解析报错）

**80G 提速实测**：answer generation 345 vs 49G 260 tok/s（~1.3×），但整体每题仍受
retrieval + compiler + judge（deepseek API）制约。评测总时长瓶颈不在 GPU 算力，
而在 judge 与 pipeline 阶段。
