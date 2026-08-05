# 023 Planner r5 三臂配对评测结果（2026-08-04/05）

**结论：三臂全部退化为 deterministic extractive compiler，planner 从未成功参与
（1540/1540 `planner_error` fallback）。023 的核心假说（训练 planner 提升证据编译）
未被本次评估测到。** 本文记录结果、根因与重跑前修复清单。

## 三臂结果

| 臂 | 名义配置 | 精度 (majority, 3-rep) | validity | judge_failed | 实际行为 |
|---|---|---|---|---|---|
| det (`paired-det-full`) | `--compiler-arm extractive` | 80.52% | **valid=false** | 98 | deterministic extractive（planner 未构造，`planner_unavailable`） |
| prompt-only (`paired-prompt-only`) | `--compiler-arm planner` + base 7B | 80.39% | valid=true | 0 | planner 构造但 1540/1540 `planner_error` → extractive fallback |
| supervised (`paired-supervised`) | `--compiler-arm planner` + 7B LoRA | 79.61% | valid=true | 0 | 同 prompt-only，全部 `planner_error` → extractive fallback |

- 三臂 candidates/bundles/compile_trace hash 一致 → 配对纪律成立（同 store、
  候选逐字节一致），prompt-only/supervised 差异仅来自 answerer/judge 重复投票噪声。
- 类别精度：single-hop ~82%、multi-hop ~83%、temporal ~76–78%、open-domain ~58–65%。
- det 臂 judge_failed 98 条为首次运行 judge（deepseek API）偶发失败；prompt-only/
  supervised 复跑时 judge 稳定（0 失败）。

## planner_error 根因（已复现验证）

1. **6s 超时（主因）**：eval 的真实 `renderPlannerPrompt` 传入全部候选源
   （实测 81 候选 / ~10.6k chars ≈ 7k tokens）+ `max_tokens 512`，7B 单请求实测
   **6.2s**，超过 `local_planner.defaultPlannerTimeout = 6s` → 每次 Propose 超时
   fallback。简化 prompt（~3.2k tokens）测试仅 0.78s，未暴露该问题。
2. **base 7B 输出 JSON 不合法**：即便不超时，base 7B 生成的 proposal 常被截断/
   非法字符（`Unterminated string`）→ `parsePlannerProposal` 失败 → fallback。
3. 复现中 LoRA（`planner` 模型）输出可解析，但 5/… 个 action 引用不存在的
   candidate_id → 会被 `ValidateAction` 拒绝（fallback）。

结论：planner 臂要真正参与，必须先解决超时与输出规范性，否则配对对比无意义。

## 重跑前修复清单

1. run 脚本加 `--planner-timeout 20`（契约已有该 flag，默认 6s）——治超时。
   或改 `cmd/locomo-bench/local_planner.go` 的 `defaultPlannerTimeout`。
2. planner 输入过重：81 候选 → 评估是否降为 top-N（注意保持配对纪律与训练分布一致）。
3. planner 输出规范性：base 7B JSON 频繁非法，需更强提示词或验证 LoRA 训练后输出质量；
   LoRA 输出需消除非法 candidate_id 引用（`ValidateAction` 拒绝即 fallback）。
4. det 臂 judge_failed：重跑 det 或对 98 条做 judge 重判，取得 det 正式分。

## 环境配置经验（已调通，重跑可复用）

**GPU：RTX PRO 6000 Blackwell 98G。三服务共存 76G/98G 稳定。**

| 服务 | 端口 | 配置 |
|---|---|---|
| answerer/extractor | 8000 | `Qwen3.6-35B-A3B-FP8`，`--max-model-len 16384 --max-num-seqs 8 --gpu-memory-utilization 0.55 --moe-backend triton` |
| planner (base/LoRA) | 8001 | `Qwen2.5-7B-Instruct`，`--max-model-len 8192 --max-num-seqs 8 --gpu-memory-utilization 0.20`；LoRA：`--enable-lora --lora-modules planner=<adapter>` |
| embed | 8010 | `bge-large-en-v1.5`，`--convert embed --max-model-len 512 --gpu-memory-utilization 0.03` |

坑（均已修复/规避）：
- **answer 400**：eval 冻结 `max_tokens 8000` + bundle ≤5000 输入 ≈ 13000 tokens；
  35B `--max-model-len` 若为 8192 会 400 全挂。必须 ≥16384。而共存空间靠
  `--max-num-seqs 8` 压 KV（det 臂 256 seqs 时 35B 占 85G；seq 8 稳态 ~52G）。
- **显存共存**：35B util 0.62 实测占 85G → 7B 放不下；降 util 0.45 稳态 ~42G、
  提 max-model-len 16384 后 ~52G，free 空间足以容纳 7B（util 0.20 ≈ 20G）。
  注意 35B 加载期峰值（含 AOT compile 临时显存）高于稳态。
- planner 每次启动必须带 Blackwell kernel env（`FLASHINFER_CUDA_ARCH_LIST="12.0"`、
  `CUDA_HOME`/`CUDA_PATH` 指向 cu13、`VLLM_USE_FLASHINFER_SAMPLER=0`）。
- 服务串行启动（vllm 并发启动会 EngineCore 初始化失败）。

## 数据与备份

- 原始结果：`/root/autodl-tmp/023-runs/{paired-det-full,paired-prompt-only,paired-supervised}/`
  （AutoDL 数据盘，持久）
- 备份：`/root/autodl-tmp/eval-backup-20260805-0500/`
- 机器已关机（停止计费）；下次开机凭据/端口会轮换，vllm 需按上表重启。

## 判定（T020）

supervised vs det 的 majority Δ 无法判定——两者都是 extractive 行为（噪声差），
planner 训练效果未进入测量。`--planner-timeout` 修复并重跑 prompt-only/supervised
前，T020 保持未决。
