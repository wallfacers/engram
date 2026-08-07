# 放开深度思考限制 — 评测口径决策与计划

**日期**: 2026-08-07 · **决策**: 维护者要求放开思考限制做全量跑分，视为正常涨点来源

## 论点

评测长期禁思考（`LOCOMO_NO_THINKING` 默认开 → `ThinkingDisabled=true`），但：

1. **深度思考是当前模型的默认能力**——Qwen3.6 / DeepSeek-v4 都默认开启思考，部分模型思考**无法关闭**。
2. **评测禁思考 = 保守口径、自缚手脚**——真实部署/生产调用不会刻意关思考；竞品评测（Mem0/MemOS 等）大概率不关。
3. **放开思考是"对齐模型真实默认能力"，不是额外涨分机制**——思考是模型自带能力，非付费云模型/不可移植依赖。因此 **paid cloud rerank DEATH RULE 不适用**（区别：思考无额外成本、非第三方托管服务）。

与 [[force-answer-regime-gap]]（83.70% 是允许拒答的严口径）同属"评测口径对齐"逻辑。

## 技术前提（2026-08-07 已核实）

- **vllm/Qwen 的思考输出混在 `content` 里**（"Here's a thinking process:..."），非独立 reasoning_content 字段 → predicted 会含思考文本。
- **mem0-aligned judge 可从中提取正确答案**（思考模式 tplan 全量 93 题 correct=true 验证），anti-放水 golden 门 26/26 覆盖。
- **思考控制 gotcha**：`thinking:{type:disabled}`（Anthropic/DeepSeek 格式）对 vllm/Qwen **无效**（Qwen 仍思考）；真正生效的是 `chat_template_kwargs:{enable_thinking:false}`（curl 实测 vllm 0.26 直接输出答案）。openai provider 已加该参数（provider/openai/wire.go + openai.go）。思考版 = **不设 ThinkingDisabled** → 不发该参数。

## 诚实边界

- **思考成本高**：Qwen 思考生成量 >10x 正常 answer（tplan 时序 prompt 诱导超长思考，曾 50 分钟仅 93 题）。全量思考版可能 10+ 小时。
- **思考下 tplan 是否还有增量**：tplan 时序 prompt 在思考模式下可能被思考吸收/干扰，需实测。
- **思考 vs 非思考的分数差是否算"涨点"**：属口径对齐。报告必须区分"记忆机制增量"（同思考口径内 arm-to-arm）与"思考口径差"（跨口径），不得混报。

## 计划

1. 非思考全量 keep+tplan（对照，运行中）
2. 思考版全量 keep+tplan（`LOCOMO_NO_THINKING=0`）→ 与对照配对
3. 结论：①思考涨多少（口径差）；②思考下 tplan 增量是否仍成立（同口径 arm-to-arm）
4. 若思考版太慢，先 84 题子集验证方向再全量

## 结果（2026-08-07，生产栈 hybrid+Qwen，全量 1540 × 1-rep）

**keep 思考版 88.57% vs 非思考 86.8% → +1.77pp，全类别正向无回落。**

| 类别 | 非思考 | 思考版 | Δ |
|---|---|---|---|
| single-hop | 88.6% | 90.6% | +2.0pp |
| multi-hop | 87.6% | 88.7% | +1.1pp |
| temporal | 87.9% | 89.1% | +1.2pp |
| open-domain | 65.6% | 68.8% | +3.2pp |
| OVERALL | 86.8% | **88.57%** | **+1.77pp** |

**加速 gotcha（重要）**：vllm 默认 `max-num-seqs 8 + gpu-mem 0.55` 下思考版全量 ~8.8h（Qwen 思考生成量大）；提到 **`max-num-seqs 32 + gpu-mem 0.85` 提速 ~16 倍（0.5h）**——MoE 模型（Qwen3.6-35B-A3B）batch 越大专家并行利用越充分。重启注意：先清残留 vllm 引擎进程（`kill -9` 残留 Qwen 引擎可占 53G 显存致新实例 OOM）。

**诚实边界**：1-rep 单次观测（temp=1.0），+1.77pp 方向明确、全类别无回落，但单点差需多-rep 坐实后才能作正式口径差宣称。

## 关联

- [[thinking-unlock-request]]、[[032-tplan-temporal-answer-verdict]]、[[force-answer-regime-gap]]、[[locomo-reference-83]]

## 补充结果（2026-08-07，3-rep 坐实 + flash 思考对照）

**Qwen 思考版 3-rep 正式口径已坐实**（远端 AutoDL 生产栈 hybrid+bge-large，LOCOMO_NO_THINKING=0，temp=1.0）：

| rep | OVERALL |
|---|---|
| run-1 | 88.64% |
| run-2 | 88.44% |
| run-3 | 87.60% |
| **3-rep mean** | **88.23%** · ci95 [86.85, 89.60] |

各类别 mean：single-hop 90.5% / multi-hop 89.0% / temporal 87.2% / open-domain 69.1%。3-rep mean 88.23% 略低于单 rep 88.57%（单点抽样波动），ci95 下界 86.85 仍贴住非思考 86.8% 之上——**思考口径差方向稳但幅度 ~1.4pp**。

**flash 思考对照**（本机 hybrid + 隧道 8010 同 store，1-rep）：OVERALL **88.1%**（single-hop 90.2 / multi-hop 87.6 / temporal 87.9 / open-domain 70.8），与 Qwen 思考版 88.57% 基本持平——**flash 思考 + hybrid 已追平 Qwen 生产栈**。vs flash 非思考 fts 43.2%（弱栈）思考本身在 hybrid 上增量远小于弱栈（弱栈 +6.7pp / 强栈 ~持平基线 86.8）。

**思考下 tplan 失效（032 补测）**：hybrid 思考 + `--temporal-answer-prompt` 全量 1-rep = **88.1%（与 keep 持平）**，McNemar flips 32/32 p=0.90 within-noise。temporal 单类 +2.1pp（87.9→90.0）被 single/open 各 -1pp 抵消。**结论：思考模式内置时序推理，tplan 的 prompt 契约冗余 → 增量消失**。tplan 只在禁思考弱栈（fts +11.2pp）是杠杆。这也解释了为何 032 生产栈非思考 tplan 仅 +0.5pp：base 高则契约边际小，思考模式直接归零。
