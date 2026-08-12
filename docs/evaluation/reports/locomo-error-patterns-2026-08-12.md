# LoCoMo 错题画像：top-k150 Qwen-thinking 栈（3-rep）

**日期**: 2026-08-12 · **口径**: LoCoMo 1540 全量 × 3-rep，top-k 150 + Qwen3.6-35B 思考模式 + flash judge（即 90.13% majority 栈）· **数据**: westb AutoDL `topk-full/tk150-full3` 逐题 results（拉回本地 `~/.claude/session-scratch/topk-err/`）· **方法**: 零模型调用，clean-final-answer 提取 + Jaccard 启发式形态分类 + locomo.json gold join + sweep 配对验证

## 结论摘要

**152 错题（majority 判定）中 140 题（92%）是 answerer 真错**（3-rep 均不接近 gold），仅 12 题是单 rep 能力波动/judge 边缘。三个可操作的机制发现：

1. **99.7% pred 含 Qwen thinking 残留，judge 全程拿到污染 pred**（`</think>` 前的候选值/自我纠错进判定）。
2. **temporal 错误是确定性时间锚定偏移**（3-rep 稳定差 ±1 月，跨栈同错），非随机。
3. **single-hop 错误是"显著记忆压制"**（同一 pred 复现于多题，人物显著记忆带跑 answerer），但 top-k 150 本身不反伤 single-hop（配对证伪）。

## 错题分解

| 类别 | 错数 | 错率 | 3-rep 稳定错 | 主导形态 |
|---|---|---|---:|---:|---|
| single-hop | 68 | 8.1% | 50 | 答错对象/显著记忆压制（~50）→ 拒答（25） |
| temporal | 39 | 12.1% | 26 | 确定性时间锚定偏移（22 可量化、20 稳定） |
| open-domain | 31 | 32.3% | 23 | 推测正反翻转 + 事实答错 + 信息不足模糊化 |
| multi-hop | 14 | 5.0% | 12 | 拒答为主 |
| **sum** | **152** | **9.9%** | **111** | |

- **3-rep 全错 111 / 2 错 41 / 1 错 0**——错题以稳定错误为主，几乎无"单 rep 随机翻车"。
- 12 题存在单 rep clean==gold：属 answerer 能力波动（1/3 rep 能答对），非 judge 误杀 majority。

## 机制一：judge 口径污染（99.7%）

- `</think>` 标记在 4606/4620 pred 中出现；judge 的 `buildJudgePrompt` 把完整 thinking 发给 judge。
- clean 提取后 raw-J≥0.5 从 0% → 53.2%，精确==gold 1083/4620。**污染是系统性的，不是错题特例**。
- majority 层面影响小（3-rep 判定自洽），但对 single-rep 判定可信度、以及任何依赖 judge 判定一致性的分析都是污染源。LME 报告已证同栈单侧救 ~17 题/k30。

## 机制二：temporal 确定性时间偏移

22/39 可量化月差，20/22 稳定（≥2/3 rep 同一 pred），±1 月为主（+1: 6 题、−1: 7 题）：

| 题 | gold | pred（稳定） |
|---|---|---|
| conv-4-q-64 | December 2023 | November 2023（3/3） |
| conv-0-q-6 | June 2023 | July 2023（3/3） |
| conv-0-q-63 | September 2023 | October 2023（3/3） |
| conv-6-q-31 | 19 days | 9 days（3/3） |
| conv-8-q-80 | Jan 9 2023 | Jan 9 **2024**（3/3） |
| conv-2-q-48 | Sunday before 3 Jul | 23 July 2023（3/3） |

- **3-rep 完全一致 → 确定性系统错误，非噪声**。answerer 答了"接近但错误"的时间，说明相关记忆已进上下文但时间锚定失败。
- **跨栈验证**：036 归因的 8 题真推理错误，7/8 在本栈（完全不同模型/协议）同样错或部分错（`conv-3-q-41/4-q-20/4-q-64/4-q-69/6-q-31` 全错，`6-q-59` 部分，`8-q-63/8-q-4` 本栈对）——这批是数据本身的稳定难点。

## 机制三：single-hop 显著记忆压制

- 同一 pred 复现于不同题：conv-9-q-151（gold=Photography）与 conv-9-q-115（gold=car）pred 均为 "Fixing cars"；conv-8-q-102 与 conv-8-q-104 均为 "Grilled chicken and veggie stir-fry"。
- → answerer 在长上下文（~8500 token）中把人物最显著的信息当答案，而非精确对齐问题问的那条记忆。
- **但 sweep 配对证伪"预算过大反伤"**：single-hop 正确率 k30=89%→k150=91% 仍涨；temporal 84→88、multi-hop 89→93、open-domain 持平 68%。**top-k 150 对各类均不反伤**，single-hop 的 off_topic 是回答侧甄别问题，不是检索预算问题。

## 探索方向（008 铁律 / 便宜优先 / 无 paid rerank 约束下排序）

### A. clean final answer 口径修复（最便宜、确定性）
judge 前提取 `</think>` 后文本。零训练、纯 harness 口径变更（宪法 IV 独立 commit）。LME 同栈已证单侧 +17 题/k30；本栈重判 3-rep clean pred（~$0.5，flash）可实测 majority 影响并确立诚实口径。

### B. 时间锚定契约 × Qwen thinking answerer（机制最实）
temporal 20 题确定性偏移是**系统性时序推理缺陷**。032 tplan 契约在 flash 有效、v4-pro 证伪——但**从未在 Qwen 思考 answerer 上测过**。方向：prompt 级时间锚定指令（区分事件时间/讨论时间、相对→绝对、周/月初末精度、多事件并列时间不混淆）。零训练；配对实测（repeats ≥3 + store 复用）。风险：032/014 教训"显式契约干扰强模型"，需配对证伪才收口。

### C. 跨栈稳定 cohort 作为验证集（方法资产）
036 8 题 + 7/8 同错 → 这批是数据真难点。任何新机制（A/B/未来）可先在这批上验证有效再全量，避免单次 run 噪声（~8.6pp）掩盖 ~1pp 级杠杆。不是涨点方向本身。

### D. 已证伪/关闭
- **top-k 150 反伤 single-hop**：证伪（配对仍涨）。
- rerank（037 NO-GO）、paid cloud rerank（死亡规则）。
- 加量型 top-k 再加大（150→更大）：上下文税 2.4× 已是 32768 上限，非"预算下提质"哲学。

## 诚实边界

- **Jaccard 是启发式**：多 token 语义等价可能误判，但 3-rep 稳定性与跨栈交叉验证支撑主结论。
- **sweep 是 1-rep 子集**（400 题），类别正确率可能含 ±2pp 噪声；配对方向（无类别反伤）可信。
- **"gold 在上下文"是推断**：answerer 稳定答接近时间而非拒答，间接支持；未直接验证 top-k chunk 内容（context_parity 无 chunk）。
- 未做 clean-pred 真实重判（成本 ~$0.5，方向 A 的第一步）。

## 数据与复现

- 逐题 results：westb `topk-full/tk150-full3/run-{1,2,3}/results-hybrid.jsonl`（已拉至本机 session-scratch）
- 分析脚本：`~/.claude/session-scratch/topk-err/{analyze_errors,clean_analysis,time_shift}.py`
- sweep 配对：`topk-sweep/tk{30,60,100,150}`（400 题同子集）
