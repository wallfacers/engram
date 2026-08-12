# Verdict: `--answer-focus-prompt`（单数聚焦 + 精确值优先）— NO-GO

**日期**: 2026-08-12 · **动机**: PowerContext（OceanBase，LoCoMo 自报 90.78%）的 answer prompt 含
"singular question → answer only the requested fact" 与 "prefer exact names/dates" 两条规则，正好对
应 engram 已归因的 single-hop 错题（显著记忆压制 ~50 题 + 精确值丢失）。**前提已验证**：single-hop
68 错题中 65%（44/68）的 gold ≥50% 出现在 answerer 的 thinking（gold 在上下文，甄别选错 → prompt 有
发挥空间）。

## 实验（run 内配对，同批 judge）

- **栈**：Qwen3.6-35B-A3B-FP8（thinking 开 `LOCOMO_NO_THINKING=0`，vllm :8000）+ 032-store（bge-large，
  embedding 512-cap 降级，semantic→keyword+entity，恒定偏置 base/focus 同受影响）
- **子集**: topk-sweep/subset.txt 400 题（single-hop 219 / temporal 83 / multi-hop 73 / open-domain 25）
- **协议**: `--top-k 30 --repeats 3 --force-answer --judge-mem0-aligned --chunks --chunk-quota 12`，与 topk
  sweep 一致；base（无 flag）vs focus（`--answer-focus-prompt`）
- **判定**: 同批 flash rejudge（base+focus 共 2400 次，clean final answer 提取，judge 噪声同批抵消）

## 结果

| 臂 | rejudge-majority | 历史 harness |
|---|---|---:|
| base | 356/400（89.00%） | 349 |
| focus | 354/400（88.50%） | 342 |
| **Δ** | **-2 题（-0.13pp）** | |

flips 14 题，**双向对称**：single-hop 3 救 3 坏（净 0）、temporal 1/2、multi-hop 1/2、open-domain 1/1。

## 机制（flip 明细）

focus 改坏的代表：
- conv-8-q-142: base 对 → focus **拒答**（'Not mentioned in the retrieved memories.'）——"精确值优先"
  让 answerer 在证据不够精确时倾向拒答
- conv-4-q-99: base 对 → focus 换成另一个事实（'Watching Home Alone'）
- conv-3-q-41: base 对 → focus **时间答错**（'6 October 2022' → '14 September 2022'）
- conv-5-q-46: base 'Two' → focus '2'（数值等价，多数票翻转）

focus 救回的对称存在（conv-8-q-113 'Kayaking'→'Painting'、conv-9-q-61 'Until Oct'→'Aug-Oct'）——
**指令确实改变了 answerer 行为，但方向随机，净 0**。

## 结论：NO-GO

1. **回答侧显式契约对 Qwen 思考模型第 3 次证伪**（继 032 tplan、014 temporal contract）——Qwen thinking
   已内置聚焦/精确能力，额外指令是冗余甚至干扰。
2. **PowerContext 的 answer prompt 有效性依赖其上下文构造，非 prompt 本身**：它给 answerer 的是
   "抽取记忆（保精确值）+ 按引用回源 Source"，engram 给的是 chunk 原文（精确值在但被相似记忆噪声
   淹没）。同一指令在噪声上下文里不转化。
3. **single-hop 显著记忆压制的根因在上下文区分度，不在 prompt 聚焦度**——指向检索侧/上下文构造
   （PowerContext 的抽取记忆粒度 vs engram 的 chunk），非回答 prompt。

## 处置

- `--answer-focus-prompt` **保留为 default-off 诊断 flag**（与其他诊断 flag 一致，不影响任何基线），
  不转正、不设默认。
- 诚实边界：400 题子集 ±2-3 题噪声边界，-2 题不显著；但 flips 双向对称 + 改坏模式（拒答/换错/时间
  错）说明不存在被噪声掩盖的正向机制。
