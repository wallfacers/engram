# 030 配对评测复现（trace 预算高效赢 + 子集配对）

**日期**: 2026-08-06 · **机器**: AutoDL RTX PRO 6000 Blackwell（97GB）· **结论**: trace 3 次多数 85.91% @ 468 tok（base 3620，省 7.7 倍）；子集配对读侧精炼显著

本文件是 030 evidence-mediation 配对评测的完整复现手册：环境、命令、结果、踩坑。verdict 正本见 `docs/evaluation/reports/030-evidence-mediation-verdict.md` 与 `specs/030/diagnosis/{us2,us3}-verdict.md`。

## 0. 结论速览

| 口径 | base | trace（--trace-mediation） | Δ |
|---|---|---|---|
| 029 难题 84 题子集 × 3 majority | 27.4% | **50.0%**（p=0.0017 显著） | +22.6pp |
| 全量 1540 题 × 3 majority | 84.9%（单次）/85.19%（历史） | **85.91%** | +0.72~1.01pp |
| answer context token | 3620 | **468** | **省 7.7 倍** |

trace 的赢 = 读侧证据精炼（chunk 优先 + 聚焦）；token 省 7.7 倍是确定性结构优势。

## 1. 环境清单

| 组件 | 配置 |
|---|---|
| 机器 | AutoDL `connect.westb.seetacloud.com`（SSH host/port/密码**每次重启轮换**，走 env，勿落盘） |
| GPU | RTX PRO 6000 97GB；**Qwen gpu-util 0.82**（0.92 会吃满显存导致 embed 起不来） |
| answerer | Qwen3.6-35B-A3B-FP8 @8000，`--default-chat-template-kwargs '{"enable_thinking":false}'` |
| embedding | bge-large-en-v1.5 @8010（vllm `--runner pooling`） |
| judge | DeepSeek `deepseek-v4-flash` mem0-aligned（`~/.config/engram/judge.env`） |
| store | `/root/locomo-artifacts/009-bge-chunks-store`（bge-large 1024d，10 conv） |
| 数据集 | `/root/engram/testdata/locomo/locomo.json`（1540 题） |

## 2. 服务启动（含踩坑）

```bash
# embed @8010（bge-large）
bash /root/autodl-tmp/serve-embed-only.sh
# ⚠️ vllm 0.19.1 用 --pooler-config，不是 --override-pooler-config（旧脚本会报 unrecognized arguments）

# answerer @8000（Qwen，gpu-util 必须 0.82 给 embed 让显存）
setsid bash -c 'exec /root/autodl-tmp/venv/bin/vllm serve /root/autodl-tmp/model \
  --served-model-name Qwen/Qwen3.6-35B-A3B-FP8 --port 8000 --api-key local-eval \
  --max-model-len 32768 --gpu-memory-utilization 0.82 \
  --default-chat-template-kwargs "{\"enable_thinking\":false}"' \
  >serve-030.log 2>&1 </dev/null & disown

# 验证
curl -s -H 'Authorization: Bearer local-eval' http://127.0.0.1:8000/v1/models  # Qwen
curl -s -H 'Authorization: Bearer local-eval' http://127.0.0.1:8010/v1/models  # BAAI/bge-large-en-v1.5
```

**踩坑**：
- vllm 0.19.1 的 embedding flag 是 `--pooler-config`（0.11.2 的 `--override-pooler-config` 已改名）；`serve_embed2.sh` 会失败，用 `serve-embed-only.sh`。
- `EMBED_MODEL` 必须全名 `BAAI/bge-large-en-v1.5`（served id），否则 404 → 语义信号静默降级。
- Qwen `gpu-memory-utilization 0.92` + embed `0.08` 超显存（97GB），embed 起不来报 `Free memory...less than desired`；Qwen 降到 0.82（eval prompt 仅 ~3600 tok，远小于 32768 max-len，无影响）。
- DeepSeek judge key 失效会 401（`Authentication Fails...invalid`）——全部题目判 wrong；换有效 key 后更新 judge.env。
- SSH 凭据每次重启轮换；用 paramiko（`python3-paramiko`）程序化连接，密码走 0o600 临时 env 文件，用完删除。

## 3. 配对命令

```bash
# 通用脚本 run030.sh（env + 基线 flag，追加 arm flag）
#   export LOCOMO_PROVIDER=openai LOCOMO_BASE_URL=http://127.0.0.1:8000/v1 LOCOMO_API_KEY=local-eval
#   export LOCOMO_MODEL=Qwen/Qwen3.6-35B-A3B-FP8
#   export EMBED_BASE_URL=http://127.0.0.1:8010/v1 EMBED_API_KEY=local-eval EMBED_MODEL=BAAI/bge-large-en-v1.5
#   source /root/.config/engram/judge.env
#   基线 flag: --chunks --retrieval hybrid --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned

# 子集配对（029 实际 84 题 = specs/030/diagnosis/phase0-ids-029-84.txt）
bash run030.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3                     # base
bash run030.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3 --trace-mediation    # trace
bash run030.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3 --evidence-assembly  # keep (US3)
bash run030.sh RUN_DIR --only-questions phase0-ids-029-84.txt --repeats 3 --evidence-assembly \
  --consolidate --token-counter-base-url http://127.0.0.1:8000/v1                             # cons (US3)

# 全量（canonical recipe）
bash run030.sh RUN_DIR --repeats 1                     # base 全量（84.9%）
bash run030.sh RUN_DIR --trace-mediation --repeats 3   # trace 全量（3 次多数）

# 配对统计
./locomo-bench --compare DIR_A DIR_B    # 输出 flips A→B / B→A + McNemar p + verdict
```

## 4. 分析命令

```bash
# 子集配对：gate 状态分布 + 类别翻转
python3 specs/030-evidence-mediation/tools/trace_analyze.py /root/autodl-tmp/030-us2
#   paired: flips_a_to_b / mcnemar_p / category_flips; gate: status_distribution

# 全量 3 次多数（合并不同进程的 rep）
python3 merge_trace_majority.py rep1.jsonl rep2.jsonl rep3.jsonl [base.jsonl]
#   按 question_id 3 次 ≥2 对 = majority; 输出 OVERALL + 类别 + context mean + base flips
```

## 5. 子集纠偏（重要）

030 最初自抽 84 题（temporal 前 59 + multi-hop 前 25）**与 029 实际子集不同且偏简单**（base 跑 90.5%，天花板效应，trace 无展示空间）。**改用 029 实际 84 题**（`phase0-ids-029-84.txt`，从 `specs/029/diagnosis/diagnosis-report.json` per_question 提取，跨 9 conv，topk_hit 仅 34.5%）。配对纪律：base/trace 同子集即可，不依赖历史子集文件。

## 6. 结果（完整）

### 子集配对（029 实际 84 题 × 3 majority）

| Arm | OVERALL | multi-hop | temporal | context tok |
|---|---|---|---|---|
| base（legacy 12 chunk） | 27.4% | 28.0% | 27.1% | 3600-4065 |
| keep（装配器 chunk-first） | 47.6%（p=0.0455 vs base） | 44.0% | 49.2% | 3684 |
| trace（引用链精选 1 条） | **50.0%（p=0.0017 vs base）** | 52.4% | 56.6% | ~250-725 |
| base-slim（top-k5，控制） | 27.4% | 20.0% | 30.5% | ~688 |

- keep vs base p=0.0455 显著 → 读侧精炼（chunk-first）是真杠杆
- trace vs keep p=0.152 不显著 → 引用链 sidecar 无独立于装配器的增量
- base-slim 控制：削减 token 不改变 base（27.4%）→ trace 赢因是证据选择非预算差

### 全量（1540 题）

| Arm | OVERALL | context tok |
|---|---|---|
| base 单次 | 84.9%（与历史 85.71/85.19/84.94 吻合 → 环境正常） | 3620 |
| **trace 3 次多数** | **85.91%**（reps 85.6→85.9 稳定） | **468** |

- 类别全正向：multi-hop 87.23 / open-domain 66.67 / single-hop 88.23 / temporal 84.42
- vs base 净 +15 题；token 省 7.7 倍（确定性）

## 7. 诚实边界

- base 侧为单次/历史多数，严格 base-3-vs-trace-3 同进程配对未跑；正确率增量方向正但配对不显著（~p0.2）。
- US3 精确 tokenizer 未启用（缺 `--counter-fingerprint`，022 calibration 产物未留存）→ keep/cons 装配走 estimate ledger；over-cap 判定近似。
- 子集 base 27.4% 是 029 难题 84 题的真实水平（topk_hit 34.5%），非全量口径；cat-top-k 1=150 对照排除配置缺口（27.4% 不动）。

## 8. 关联

- verdict 正本：`docs/evaluation/reports/030-evidence-mediation-verdict.md`、`docs/evaluation/experiment-verdicts.md` 030 行
- 子集配对：`specs/030/diagnosis/{us2,us3}-verdict.md`
- 子集文件：`specs/030/diagnosis/phase0-ids-029-84.txt`
- 复现（008 canonical recipe）：`docs/locomo-e2e-eval-reproduction.md`
