# Judge 口径变更：只看 final answer（thinking 前剥离）

**日期**: 2026-08-12 · **宪法 IV**: evaluation-regime 变更，独立 commit + 声明新基线
**状态**: 验证进行中（离线重判 raw vs clean，数字待填）

## 变更内容

`cmd/locomo-bench` 的 `buildJudgePrompt` 现在对 predicted 应用 `extractFinalAnswer`
（剥离最后一个 `</think>`/`</thinking>`/`[/thinking]`/`[/reasoning]` 闭合标记之前的
thinking 前导，grade 其后内容）。**对无 thinking 结构的 pred 是恒等变换** ——
非 thinking 模型基线完全不受影响。

## 为什么（rationale）

1. **harness 自己的设计意图**：judge 调用已显式 `thinking: {"type": "disabled"}`
   （`provider/anthropic`，DeepSeek 需 opt-out）——judge 不该看思考。但 answerer
   （Qwen 思考模式）输出的 pred 含 `<think>…</think>` 前导，judge 却拿到了完整
   completion —— answerer 侧的疏漏，本变更补上。
2. **污染证据（LoCoMo top-k150 3-rep）**：4606/4620 pred 含 `</think>`（99.7%）。
   thinking 前导内含候选值/自我纠错，judge 依据它判定 → "pred 未变、判定翻转"成为
   思考栈的 dominant judge noise（LME 报告同结论）。
3. **通用性**：任何 thinking-capable 模型（Qwen / DeepSeek reasoning / future）接入
   时，judge 都应以 final answer 为判定对象。不针对任何数据集，非定制。

## 验证设计（raw vs clean 同批次）

对 LoCoMo top-k150 全量 3-rep（1540 题），用同一 flash judge（thinking disabled、
temp=0、max_tokens 512，复刻 harness provider）重判：

- **raw 臂**：pred 原样（复现 harness 现状）—— 已完成
- **clean 臂**：pred 经 extractFinalAnswer —— 已完成
- 同批次 → judge 自身噪声在两臂间抵消，差异 = 提取的净效应

## 结果（raw + clean 均完成）

### 发现一：judge 判定跨批次非对称漂移 ~2.5pp ⚠️

同一 pred（raw）、同一 mem0-aligned prompt（Go/Python 逐字一致，1480 bytes），
history（topk150 当时）判定 vs 本机 rejudge：

| 测量 | history | raw-rejudge | Δ |
|---|---:|---:|---:|
| majority correct | 1388（90.13%） | **1426（92.60%）** | **+38 题** |
| single-rep 判定差异 | — | 179/4620（3.9%） | — |
| majority flips | — | 50（up 44 / down 6） | 净 +38 |

flip 类别：temporal 20/1、single-hop 17/0、open-domain 4/1、multi-hop 3/4。

**含义**：judge 判定不可复现至 ±2.5pp，且非对称（重判更宽松）——历史 90.13% 可能被
当时的 DeepSeek v4-flash 版本低估。**任何单批次 judge 分数带 ≥±2.5pp**，比此前估的
±1pp 更严重。这对"单次 run 噪声 ~8.6pp"的方法论发现是补充：其中一部分可能来自
judge 漂移而非 extraction/answer。

**对 008 判定的要求升级**：不仅 extraction/answer 随机（store-dir 复用可消），
judge 判定本身也要同批对照（同一 judge 调用期）才能归因于被测变量。

### 发现二：clean 提取净效应 — 消除 judge 作弊（-23 相对 raw 同批）

| 臂 | majority correct | single-rep correct |
|---|---:|---:|
| history（topk150 当时） | 1388（90.13%） | 4151/4620（89.85%） |
| raw-rejudge（同批） | 1426（92.60%） | 4254/4620（92.08%） |
| **clean-rejudge（同批）** | **1403（91.10%）** | 4207/4620（91.06%） |

**raw→clean 净效应（同批，judge 噪声抵消）：救回 16 / 改坏 39 / 净 -23。**

抽查 12 个"raw 对、clean 错"的题，**final answer 全部真错**：
- conv-2-q-48：gold `The sunday before 3 July 2023`，final `23 July 2023`
- conv-3-q-22：gold `May 2022`，final `25 February 2022`
- conv-1-q-74：gold `savor all the good vibes`，final `Open his dance studio`（thinking 里含 'good vibes'）
- conv-2-q-128：gold `hiking`，final `Picnic`

**含义：raw 判对是 judge 从 thinking 前导里读到正确候选值（作弊）；clean 判错是诚实的——
模型最终输出的答案确实错了。** clean 提取不是"变严"，是消除 judge 对 thinking 内容的
依赖，让判定基于模型真正输出的 final answer。

### 发现三：真正的瓶颈是 answerer 的 thinking→final 一致性（通用问题）

改坏题的 thinking 里常含正确推理/候选值，但 **final answer 输出错误值**。例如
conv-3-q-78（gold `nine`，final `4`）、conv-1-q-74（thinking 提 'make memories' 候选，
final 选 'Open his dance studio'）。这是 **thinking-capable 模型共同的输出协议问题**：
final answer 应是 thinking 的结论，而不是重新选的一个值。非数据集定制，对任何
thinking 模型接入都适用。

## 诚实边界

- 重判是离线单批次（flash，thinking disabled）；judge temp=0 仍带 ±1pp 模型噪声
  （本报告已实测跨批次 ±2.5pp）。
- clean 口径的 91.10% 是离线重判估计，**正式 clean 基线需下次 harness eval 确认**
  （当前代码已默认 clean 提取，下次 eval 自动生效）。
- history→clean 的 +15 是 judge 漂移（history 低估）叠加，**不能归因于 clean**；
  clean 的贡献是消除 judge 作弊，使判定基于 final answer。
