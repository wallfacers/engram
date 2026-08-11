---
title: LME v4-pro @ k150：禁思考探针 79.2%（rep-1，未完成 3-rep）——诊断性结果，不可比
summary: v4-pro（LOCOMO_NO_THINKING=1）@ top-k 150 在 LME-S 全量 500 上 rep-1 = 79.2%（396/500），显著低于 Qwen thinking 基线 k150 86.2% raw / ~87.2% clean。逐题对比证明主因是 answer 模型被禁思考——multi-session -19.5pp、temporal -10.6pp 全崩，只有 knowledge-update +6.4pp。结论：禁思考的 v4-pro 与 thinking 开的 Qwen 基线不可比，需重跑 v4-pro thinking 开才是同口径。同时 eab218a 修复 chunks store 无 --chunks 复用（本 run 暴露的第三 regime）。
status: diagnostic
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-12
tags: [evaluation, longmemeval, v4-pro, thinking, top-k, aborted]
---

# LME v4-pro @ k150：禁思考探针（诊断性，未完成）

## 背景

问 v4-pro（DeepSeek 官方 API，比 Qwen 强的 answer 模型）在 LME top-k 150 上能否超越 Qwen 基线（k150 raw 86.2% / clean ~87.2%）。历史 LoCoMo 上 v4-pro 相对 Qwen 稳定 +2pp 以上，预期 LME 同向。**但 run 用 `LOCOMO_NO_THINKING=1` 禁了 v4-pro 的思考**（沿用探针惯例，未意识到 LME 是推理重基准），得到相反结果。

## 方法（部分执行）

- **栈**：`lme-s500-store`（512,662 entries，bge-large 1024d）+ **deepseek-v4-pro**（`LOCOMO_NO_THINKING=1`，禁思考）+ deepseek-v4-flash judge（mem0-aligned）+ `--force-answer --chunk-quota 12 --retrieval hybrid --top-k 150 --trace-mediation=false`。
- **⚠️ 注意**：本 run **未带 `--chunks`** 但复用含 chunk 的 store——这触发了 `eab218a` 修复的第三 regime（chunks store 无 `--chunks` 复用）。run 期间该 fix 尚未合入（二进制在 fix 前编译）。检索实际仍含残留 chunk（quota 12 生效），但与 Qwen 基线（`--chunks` 全摄入）在 chunk 组成上不完全一致。**这是次要混杂因素，不改变主结论。**
- **进度**：500/500 build 完成，rep-1 完成（25 min），rep-2 刚初始化时被维护者叫停。**3-rep 未完成**。

## 结果（rep-1，J 口径）

| 类别 | v4-pro 禁思考 | Qwen thinking 基线 (k150) | Δ |
|---|---:|---:|---:|
| multi-session | 88/133 (**66.2%**) | 114/133 (85.7%) | **-19.5pp** |
| temporal-reasoning | 103/133 (77.4%) | 117/133 (88.0%) | **-10.6pp** |
| single-session-preference | 19/30 (63.3%) | 21/30 (70.0%) | -6.7pp |
| knowledge-update | 66/78 (84.6%) | 61/78 (78.2%) | **+6.4pp** |
| single-session-assistant | 51/56 (91.1%) | 50/56 (89.3%) | +1.8 |
| single-session-user | 69/70 (98.6%) | 68/70 (97.1%) | +1.5 |
| **OVERALL (J)** | **396/500 (79.2%)** | 431/500 (86.2%) | **-7.0pp** |

## 根因：thinking 模式差异（决定性证据）

逐题对比 v4-pro 判错而 Qwen 判对的题，pred 形态完全不同：

```
v4-pro 本轮 pred 含 thinking:  0 条   ← LOCOMO_NO_THINKING=1（禁思考，直接简答）
Qwen 基线 pred 含 thinking:   44 条  ← thinking 开（带推理）
```

LME 的 multi-session（求和/聚合）与 temporal（多步时态推理）需要逐步推理。v4-pro 禁思考后直接给最终答案，在 26 个求和/聚合/时态题上算错或不能识别"信息不足"（ABS 题全废）。典型：

| 题 | GOLD | v4-pro 禁思考 | Qwen thinking |
|---|---|---|---|
| 狗碗/量杯/洁齿/项圈总价 | $50 | **$85** | 长推理正确求和 |
| 教育年数 (ABS) | 信息不足 | **14 years**（不知拒绝） | 长推理识别信息不足 |
| 游戏总时长 | 140h | **185h** | 长推理正确求和 |
| Rachel 现公司 | TechCorp | **Old Company** | 长推理 |
| 上月最贵超市 | Thrive Market | **Walmart** | 长推理 |
| 每周健身课 | 5 | **6** | 长推理 |

**judge 没冤枉它**：$85 vs $50 是 v4-pro 真算错，不是 judge 误判。同一个 flash judge，Qwen 的 pred 在这些题上判对。

## 结论

1. **禁思考的 v4-pro 在 LME 上不是 Qwen 基线的有效替代**——LME 是推理重基准（相对 LoCoMo 更偏 multi-session 求和/temporal），禁思考直接砍掉 answer 模型的核心能力。v4-pro 只在 knowledge-update（事实更新检测，少推理）显著占优。
2. **与 Qwen 基线不可比**：Qwen 基线 thinking 开。要公平对比 v4-pro vs Qwen，必须 `LOCOMO_NO_THINKING=0` 重跑。
3. **副产品**：本 run 暴露的"chunks store 无 `--chunks` 复用"第三 regime 已由 `eab218a` 修复（拒绝该方向，恢复纯 fact-only 或 `--chunks` 全摄入两条合法复用方向）。

## 后续（待维护者决策）

- **重跑 v4-pro `LOCOMO_NO_THINKING=0` @ k150**（同 Qwen 基线 thinking 开）才是同口径对比。store/embed 服务完好，build 可复用（`reusing persisted extraction` 已验证），仅改 env 重启 answer。
- 若重跑后 multi-session 回到 ~85%、OVERALL ≥ 86%，则坐实"禁思考是主因"；若仍低，才是 v4-pro 相对 Qwen 在 LME 上的真实差距。
- 3-rep 全量需 ~6-7h（每 rep ~2h，32 并发硬上限）。

## 诚实边界

- **未完成 3-rep**：仅 rep-1，无 majority/CI。79.2% 是单次观测，judge 噪声 ±1pp。
- **混杂因素**：run 未带 `--chunks` 复用 chunks store（eab218a 修复的 regime），检索 chunk 组成与 Qwen 基线不完全一致。不改变"禁思考拖累推理类"的主结论。
- v4-pro 为官方 API 付费调用（非本地），本 run 成本仅 rep-1 的 answer+judge。
