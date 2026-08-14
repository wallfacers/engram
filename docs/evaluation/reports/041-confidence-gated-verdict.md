---
title: 041 confidence-gated iterative retrieval verdict —— 犹豫门控迭代 NO-GO，top-k 缩减路径收口
summary: 040 提出「89% 增量题犹豫、仅 7% 自信地错 → 读到足够就停」的假设，041 用自动文本规则验证：US1 区分度边缘（flash recall 58–63%）、Qwen 生产栈 e2e 加深净负（iter 88.8% < k150 90.1%）、unified 栈加深无空间（U-iter < U-k30=k150）、deepseek-v4-flash API 区分度 recall 50% 不达门槛——040 的「89% 犹豫」是人工宽松判读高估，自动规则真实能力 ~50–63% 不足以支撑「犹豫→加深→救回」净赢。top-k 150 的 2.4× 上下文税是当前 90pp 的硬成本。
status: verdict
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [evaluation, locomo, iterative, confidence-gated, thinking-noise, top-k]
---

# 041 confidence-gated iterative retrieval verdict

## 一句话结论

**confidence-gated iterative retrieval NO-GO**。041 验证 040 的「读到足够就停」路线：浅检索(30)→答→犹豫→深检索(150)。自动文本规则从 answerer thinking 判犹豫的真实能力 recall 只有 **50–63%**（040 的「89% 犹豫」是人工宽松判读高估），且 thinking 噪音（`could be`/`might be`/`i think` 是正常推理语言）在多栈主导误报。Qwen 生产栈加深**净负**，deepseek-v4-flash API 区分度**不达门槛**。**top-k 缩减作为「省 2.4× 上下文税不掉分」的手段失败——90pp 仍靠 top-k150 + thinking + majority 顶着。**

## 背景：041 想回答什么

040 verdict 的数据：30→150 增量 56 题（另有 31 题反害），其中「上下文量」42 题 gap-knee 救不了，但 89% 的增量题在 top-k30 下是「犹豫」的、仅 7% 自信地错——这给了「按置信度省预算」的假设：犹豫→加深，自信→停，理论上限 91.75% + 省 4.8×。041 就是要用**运行时确定性犹豫信号**（文本规则，非模型自主判断——029 教训）把这个假设落地。

## 验证链：四条独立证据

### 1. US1：flash 栈区分度「边缘」，未全 rep 达标（032-think3，3-rep）

自动规则（threshold=3.0）在 032-think3（DeepSeek flash，top-k30 thinking run）上：

| run | recall | fp | 门槛（recall≥60% / fp≤30%） |
|---|---|---|---|
| run-1 | 60.6% | 22.7% | ✅ |
| run-2 | 57.9% | 24.4% | ❌ |
| run-3 | 63.4% | 23.4% | ✅ |

US1 结论「边缘 GO」：flash 栈是**唯一**信号存在的栈，但 recall 40% 的错题（含 ~23% 完全无犹豫表达）天然盲区。040 的 89% 是基于 3-rep 多数票的 56 增量题人工判读，自动规则单 rep 全量错题真实能力就在 60% 附近。

### 2. Qwen 生产栈 e2e：迭代加深净负（1 conv，force-answer 栈，152 题）

- iter = 纯 k30 = **88.8%** < k150 基线 **90.1%**（−1.3pp）
- 加深 49/152（32%）：**rescued 2 / harmed 4 / same 43** → 加深净负
- 87% 的加深是「答对了还加深」的无用功（fp 高）——Qwen 的 thinking 里 `could be/might be` 是正常推理语言，不是不确定
- 省 token（4592 vs 8229）但掉分，未达「省预算不降分」

### 3. unified 栈：加深无空间（1 conv，038 栈，152 题）

- U-iter 86.2% < U-k30 = U-k150 88.2%（−2.0pp）
- unified 契约下 k30 = k150：加深没有任何增量空间，迭代纯损失

### 4. deepseek-v4-flash API：区分度 recall 50% 不达门槛（本次校准，1 conv，152 题）

- **harness 修复生效**：152/152 的 shallow_answer 含 `<thinking>`（median 485 字符）——之前 041D-iter deepened=0 不是 API 无 thinking，是 harness 丢弃了 deepseek 的独立 thinking block（根因见下）
- judge 结果：wrong=20 / right=132（86.8%，与 041D-k30 的 87.5% 一致，run 稳定）

| threshold | recall | fp |
|---|---|---|
| 2.0 | 50.0% | 30.3% |
| **3.0（门槛）** | **50.0%** | **24.2%** |
| 3.5 | 20.0% | 6.1% |

**没有任何 threshold 同时满足 recall≥60% 且 fp≤30%**。比 US1 的 flash 栈还差一截。

**信号噪音归因（直接证据）**：

| 信号 | wrong 命中 | right 命中 | 误报比 |
|---|---|---|---|
| mid_guess（could be/might be） | 10 | 40 | 1:4 |
| weak_hedge（i think/probably） | 12 | 41 | 1:3.4 |
| empty_final | 3 | 0 | 精确但样本小 |

## 根因修复（保留资产）

**deepseek API（Anthropic 协议）把 thinking 放在独立 content block**，harness 的 model caller 只拼 `EventTextDelta`，reasoning 全丢——answerer 的 thinking 从未到达 detector，导致 041D 系列 deepened=0。

修复：`cmd/locomo-bench/runner.go` 新增 `newUsageModelCallerWithThinking`（reasoning delta → `<thinking>…</thinking>` 前缀，`extractFinalAnswer` 自动剥离），仅 `--confidence-gated` 时 answerer 使用；judge 路径保持 text-only（避免 verdict 污染）。有单测守护（`runner_thinking_test.go`），关闭态/无 thinking 时与旧 caller 字节一致。**这是对的工具**——它让 deepseek 的 thinking 可见，但即使可见，区分度依然不达门槛。

## 对「top-k 缩减」的最终定性

- 040：「一刀切减 k 必掉分，但可按置信度省」——**「按置信度省」的前提（置信度信号可靠）被 041 证伪**。
- 041 四条证据链：flash 区分度边缘（US1）、Qwen 加深净负（e2e）、unified 加深无空间、deepseek recall 50%——**没有一条栈的自动文本犹豫信号能支撑净赢**。
- 结论：**top-k150 的 2.4× 上下文税是当前 90pp 的硬成本**，无免费午餐。要突破需要信号质量的质变（logits/流式概率——迭代检索论文综述里「确定性信号优先」的剩余方向），而非文本规则。

## 资产清单（保留，default-off）

- `cmd/locomo-bench/confidence_gate.go`：确定性犹豫检测器（纯文本规则，无模型调用）+ 短语集（经 032-think3 条件概率调优）
- `cmd/locomo-bench/iterative_retrieval.go`：`runConfidenceGatedQuestion` 浅→深迭代循环 + `conf_gate_decisions.jsonl` 审计
- `cmd/locomo-bench/hesitation_probe.go` / `hesitation_calibrate.go`：`--probe-hesitation` 离线区分度探针 / `--confidence-calibrate` 阈值 sweep
- `cmd/locomo-bench/runner.go` `newUsageModelCallerWithThinking` + `runner_thinking_test.go`：deepseek thinking block 合并（对 harness 的修复）
- 以上全部 default-off（`--confidence-gated` 关闭时路径零改动），不影响现有评测

## 数据源

- `/root/autodl-tmp/041C-k30`（deepseek-v4-flash API 区分度校准，本次）
- `/root/autodl-tmp/041Q-*`、`041U-*`（Qwen / unified 栈 1 conv 三臂）
- `/root/autodl-tmp/041D-*`（deepseek API 旧跑，thinking 未进 predicted 的对照）
- `/root/autodl-tmp/032-think3`、`topk-full/tk150-full3`（US1 区分度）
