# 023 Planner r5 residual 先导（2026-08-05）

**目标**：不烧全量三臂，先用最小成本验证 planner 假说——在 Primary Cohort
（residual 149 题）上，planner 臂（prompt-only + supervised）能否救回足够多的题。
先导只跑 planner 臂 × residual 149 题，det 复用已冻结结果。

## 判据（先导 gate，非正式 verdict）

| 先导结果（planner 臂 majority 在 149 题上的正确数） | 决策 |
|---|---|
| ≥ 5 题 | 值得全量确认（T018–T020 3×3×1540 重跑） |
| 3–4 题 | 边缘；看 1-rep 明细 + category 分布再定 |
| ≤ 2 题 | **直接 STOP**，023 归档为 research artifact（008 铁律：证据选择不转化） |

依据：GO 门槛 FR-028 = Primary Cohort majority 提升 ≥2.0pp 且 McNemar p<0.05；
149 题上 det 全错（c=0）时需 planner 答对 ≥5 题才 `(0.5)^b < 0.05`。

## 前置修复（本次已部分落地）

1. **`--planner-timeout`**：报告根因 1 = 7B 单请求 6.2s > 默认 6s。
   先导用 `--planner-timeout 20s`（契约 flag 已存在，`main.go:264`）。
   ⚠️ FR-034 要求 p95 ≤ 2.0s——20s 只适用于研究先导；若 GO 需降 planner 输入
   token（见修复清单 3）才能成为推荐配置。
2. **`--only-questions`（本次已实现 + build/test 验证）**：locomo-bench 新增 flag，
   按 `conv-N-q-M` 白名单过滤（`readQuestionWhitelist`，与 formal B0/B1 互斥）。
   residual-cohort.json 的 `residual_questions` key 与 `questionID()` 格式逐字节一致，
   可直接作为输入，零转换。
3. **det 98 条 judge_failed 重判**：推迟到全量确认阶段（先导只关心 planner 臂相对
   det 的救回数，det 分数不参与先导 gate）。

## 环境启动（RTX PRO 6000 Blackwell 98G，三服务共存 76G/98G）

planner 每次启动必须带 Blackwell kernel env：
`FLASHINFER_CUDA_ARCH_LIST="12.0"`、`CUDA_HOME`/`CUDA_PATH`→cu13、
`VLLM_USE_FLASHINFER_SAMPLER=0`。vllm 必须**串行启动**（并发启动 EngineCore 失败）。

| 服务 | 端口 | 配置 |
|---|---|---|
| answerer/extractor | 8000 | `Qwen3.6-35B-A3B-FP8`，`--max-model-len 16384 --max-num-seqs 8 --gpu-memory-utilization 0.55 --moe-backend triton` |
| planner (base/LoRA) | 8001 | `Qwen2.5-7B-Instruct`，`--max-model-len 8192 --max-num-seqs 8 --gpu-memory-utilization 0.20`；LoRA：`--enable-lora --lora-modules planner=<adapter>` |
| embed | 8010 | `bge-large-en-v1.5`，`--convert embed --max-model-len 512 --gpu-memory-utilization 0.03` |

模型/venv/数据均在数据盘 `/root/autodl-tmp/`（磁盘卫生规则见 CLAUDE.md）。

## 运行命令（先导）

数据盘准备白名单（residual 149 题）：

```bash
python3 -c "
import json
d=json.load(open('/root/autodl-tmp/023-runs/t003-b1-v3/../residual-cohort.json'))
open('/root/autodl-tmp/023-runs/residual-ids.txt','w').write('\n'.join(d['residual_questions']))
"
# 或在本地 spec 里直接取：specs/023-local-trained-evidence-compiler/residual-cohort.json
```

先导（2 臂 × 149 题 × 3 rep；det 复用 `paired-det-full`）：

```bash
cd /root/autodl-tmp/engram  # 数据盘工作副本
RUN=/root/autodl-tmp/023-runs
IDS=$RUN/residual-ids.txt
```bash
cd /root/autodl-tmp/engram  # 数据盘工作副本
RUN=/root/autodl-tmp/023-runs
IDS=$RUN/residual-ids.txt
export EMBED_BASE_URL=http://localhost:8010 EMBED_MODEL=bge-large-en-v1.5
# 注：embedding 只走 env（main.go buildBenchEmbeddingClient），无 --embed-* flag。
# prompt-only 臂（base 7B）
go run ./cmd/locomo-bench --data /root/autodl-tmp/locomo.json \
  --run-dir $RUN/pilot-prompt-only --retrieval both \
  --compiler-arm planner --planner-base-url http://localhost:8001 \
  --planner-model Qwen2.5-7B-Instruct --planner-timeout 20s \
  --repeats 3 --only-questions $IDS
# supervised 臂（LoRA）
go run ./cmd/locomo-bench --data /root/autodl-tmp/locomo.json \
  --run-dir $RUN/pilot-supervised --retrieval both \
  --compiler-arm planner --planner-base-url http://localhost:8001 \
  --planner-model Qwen2.5-7B-Instruct --planner-timeout 20s \
  --repeats 3 --only-questions $IDS
```

> LoRA 臂的 planner model 名取决于 `--lora-modules planner=<adapter>` 的注册名；
> 若 vllm 以 `planner` 为 LoRA tag 服务，则 model 仍传 base 名、由 base URL 侧决定
> adapter 是否生效。supervised 臂必须确认实际加载了 LoRA（`vllm serve` 日志或
> `/v1/models` 返回值含 adapter）。

## 数据分析

先导输出在 `$RUN/pilot-{prompt-only,supervised}/`。统计 149 题 majority 正确数：

- 直接对两臂 3-rep 各自做 majority，看 correct 数；
- 与 det 已冻结结果对比：det 在 residual 上应全错（定义），verify 一下；
- 看 category 分布（single-hop 60 / temporal 45 / multi-hop 27 / open-domain 17），
  判断 planner 救回集中在哪类（若只救 open-domain 而 temporal 无动，结合历史
  temporal 三路 NO-GO 判断意义）。

## 决策路径

- **STOP**（≤2 题救回）：023 记录 `HOLD/STOP`，归档 research artifact，结论 =
  planner 训练不转化到 Primary Cohort。机器即关。
- **GO 方向**（≥5 题）：再做 det 98 条 judge 重判拿正式基线，然后全量
  T018–T020 3×3×1540（修复清单：planner 输入降 top-N、LoRA 输出规范性、p95≤2s）。

## 备份与磁盘卫生

- 结果备份：小文件（`*.json`/`*.log`/`*.sh`/`store/`）→ `/root/autodl-tmp/eval-backup-<ts>/`
- 先导 run-dir 本身在数据盘，系统盘不再堆积（`formal_calls.jsonl` 由 `--only-questions`
  收缩到 149 题量级）
- 跑前 `df -h /`，>80% 先清系统盘
- 机器空闲必停（省钱铁律），凭据/端口每次重启轮换
