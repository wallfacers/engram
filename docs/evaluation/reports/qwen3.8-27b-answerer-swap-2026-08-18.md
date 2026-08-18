# Qwen3.8-27B 换答题模型 — 双数据集 3-rep Verdict (2026-08-18)

> 场景：数据集答题模型从 Qwen3.6-35B-A3B-FP8（MoE，8 月前栈）换成 **Qwen/Qwen3.8-27B**
> （dense 27B，同日 vllm 部署），LoCoMo 1540 + LongMemEval-S 500 各 k30 unified 3-rep，
> 对 038 锚点出 verdict。目的：评估 dense 新模型作为 answerer 是否引入回归、是否涨点。

## 一句话结论

**无回归；LME 增益明显，LoCoMo 持平。**

- **LoCoMo 1540 · k30 unified · 3-rep**：在线 3-rep mean **87.62%**（锚 87.73%）→ **−0.11pp
  within-noise 无回归**；离线 clean 3-rep majority **89.48%**（锚无同批 clean 数据可比）。
- **LME 500 · k30 unified · 3-rep**：离线 clean 3-rep majority **93.40%**（038 锚同为
  "3-rep + clean" 口径 **90.2%**）→ **+3.20pp 同口径增益**；在线 mean 93.7%。

## 评测配置

- **契约**：unified（`unifiedAnswerContractPrompt`），answer_prompt_digest
  `sha256:ff400d0e…`（box `042-bin` 编译，**与 038 锚点同契约**——038 k30 锚 regime
  实证同为 ff400d0e）。judge = `mem0-aligned` + deepseek-v4-flash。force_answer=false、
  no-idk-retry、trace-mediation=false。
- **模型**：answerer = Qwen3.8-27B（dense BF16，vllm 0.26，max-model-len 32768，thinking on，
  content 内嵌 `<think>` 块——vllm 未分离 reasoning_content，harness `extractFinalAnswer`
  judge 前剥除，与锚点思考口径对齐）；embed = bge-large `--max-num-seqs 1`（确定性）；
  judge = deepseek-v4-flash（**新 key**，2026-08-17 应用）。
- **retrieval**：top-k 30 / chunk-quota 12 / hybrid+unified（三信号 RRF）。
- **repeats**：3。**成本**：box 计费 ~5.5h（下载 4h + 跑分 1.5h）+ judge API ¥0.1 级。

## 执行

- 模型部署：modelscope CLI 下载 55.6GB（~4h，box 外网受限 ~3-4MB/s，hf-mirror Xet 死），
  后 vllm 启动。smoke 1-conv 验证契约 digest ff400d0e + mem0-aligned + OVERALL 88.2%。
- LoCoMo 3-rep（run-1/2/3，各 1540）→ LME 3-rep（run-1/2/3，各 500）→ box 备份
  `/root/autodl-tmp/eval-backup-20260818-021844` → **box 关机**。
- 离线 clean 3-rep majority 重判（本地，deepseek-v4-flash temp=0 no-thinking，与 038
  clean 口径同 prompt 逐字）。

## 结果

### LoCoMo 1540

| 口径 | Qwen3.8-27B | 锚 Qwen3.6 (038) | Δ |
|---|---:|---:|---:|
| 在线 3-rep mean | **87.62%**（ci95 [87.4, 87.9]） | 87.73%（在线 stats） | **−0.11pp** |
| 离线 clean 3-rep majority | **89.48%**（1378/1540） | —（038 无同批 clean） | 参考 |

类别（clean 3-rep majority）：

| category | n | clean |
|---|---:|---:|
| single-hop | 841 | 91.4% |
| multi-hop | 282 | 89.4% |
| temporal | 321 | 91.9% |
| open-domain | 96 | 64.6% |

clean vs raw 净效应：救回 37 / 拖累 42（净 −5，thinking 内嵌导致 raw judge 轻微高估）。

### LongMemEval-S 500

| 口径 | Qwen3.8-27B | 锚 Qwen3.6 (038) | Δ |
|---|---:|---:|---:|
| 在线 3-rep mean | **93.7%**（ci95 [93.4, 94.0]） | — | — |
| 离线 clean 3-rep majority | **93.40%**（467/500） | **90.2%**（038 同为 3-rep+clean） | **+3.20pp** |

类别（clean 3-rep majority）：

| question_type | n | clean |
|---|---:|---:|
| single-session-preference | 30 | 100.0% |
| single-session-user | 70 | 97.1% |
| knowledge-update | 78 | 97.4% |
| single-session-assistant | 56 | 96.4% |
| multi-session | 133 | 91.0% |
| temporal-reasoning | 133 | 88.7% |

clean vs raw 净效应：救回 21 / 拖累 18（净 +3）。

## Verdict

- **LME +3.20pp（同口径，可信）**：038 锚 90.2% 明确为"配对 3-rep + clean"（result-matrix），
  与本次 clean 3-rep majority **完全同口径**。+3.20pp 超过 judge 跨批漂移上限（±2.5pp）
  的一半，方向明确。Qwen3.8-27B 在 LME 上优于 Qwen3.6。
- **LoCoMo 持平（在线口径，可信）**：在线对在线 −0.11pp，within-noise。clean 89.48%
  高于在线锚 1.75pp，但跨 judge 批次（038 无同批 clean 数据），且 042 已示 clean 普遍
  > 在线 1-2pp，故 clean 相对值不夸大结论——**不构成涨点，也不构成回归**。
- **open-domain 仍是短板**（LoCoMo 64.6%），与既有认知一致（检索覆盖问题，非答题深度）。

## 为什么 LoCoMo 没到 90pp（错题归因，009 trace join）

Qwen3.8 k30 的 162 错题按 gold_rank_pool 归因：

| 错因 | 题数 | 占比 |
|---|---:|---:|
| **k30 检索截断**（gold 在 rank 31-150，未进 top-30 上下文） | 80 | 49% |
| 检索大缺（rank 151+） | 33 | 20% |
| 池外（0.9% 已知） | 4 | 2% |
| **上下文内仍答错（真 answerer 能力）** | 45 | 28% |

- **结论反转**：8-17 归因（k150 栈）是"90% 错题在上下文内=answerer 侧"；本次 k30
  栈 72% 错题是**检索截断**——不是模型能力问题，是 k30 检索面太窄。
- **90pp 距离 8 题**（1378→1386）：截断区 80 题里 Qwen3.6(k150) 能对 32 题 → 扩到
  k150 理论上限 **91.56%**（1410/1540），跨 90pp 绰绰有余。这与
  [topk-exploration](../topk-exploration-2026-08-11.md)（k150 = 90.13% +1.62pp）一致。
- **上下文内 45 题**（能力带）：multi-hop 19 / single-hop 17 / open-domain 7 / temporal
  6——与 Qwen3.6 k150 的能力带（single-hop 60 / temporal 33）形态不同，Qwen3.8 的
  multi-hop 组装是相对短板。8-17 已收线（oracle 91.62% 贴顶，类别特化 prompt 红线排除）。
- **建议**：Qwen3.8 若要 90pp，跑 k150 即可（同 Qwen3.6 已验证）；k30 下无 90pp 空间。

## 运行特征（dense 模型关键差异）

- Qwen3.8-27B dense 每 answer ~74s（vs Qwen3.6 MoE ~1-2s），answer 与 judge 共享
  `sem`（容量=concurrency），32 answer goroutine 占满 sem → **judge 被饿死**：results
  在线阶段几乎不写（judge 批量积压到 rep 末），进度必须看 **vllm generation_tokens**
  而非 results 行数。这是慢 answer 模型的固有调度特征，非 bug（smoke 152 题 answer
  排队短故 judge 能穿插）。
- 3-rep 串行：LoCoMo ~3.4h + LME ~1.1h + ingest ~0.5h ≈ 5.5h。

## 收尾

- box 备份 `/root/autodl-tmp/eval-backup-20260818-021844`（两 run 目录 + 脚本），**已关机**。
- 本地重判产物 `/tmp/qwen38-eval/rejudge-out/{locomo,lme}-{clean,raw}-judged.jsonl` +
  `*-clean-majority.json`。
- 新 judge key 应用至 box `032-run.env` 与本地 `~/.config/engram/judge.env`（0600，不落库）。
