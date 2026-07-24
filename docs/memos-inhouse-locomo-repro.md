# MemOS 同栈 LoCoMo 复现(追评任务记录)

> 🧭 **状态**: 待实现(任务已确认,未开跑) · **目标**: 用 MemOS **自家代码**在
> **engram 同款模型栈**(答题模型跑在 AutoDL 租用 GPU box,非本地)下跑一遍 LoCoMo,
> 得到 apples-to-apples 的 MemOS 分。
> 竞品分正本 + 口径对齐见 [`competitive-benchmarks.md`](./competitive-benchmarks.md);
> 本文只记这一条复现线的动机、做法、约束、预期产出。

记录日期:2026-07-25。

---

## 1. 为什么要自己复现(动机)

`competitive-benchmarks.md §5④` 已用源码证实:engram 与 MemOS/OmniMemEval 的
**分母 / 类别(cat-5 排除)/ 拒答 / 聚合四轴相同**,可比;但仍有 **两个 regime 变量未固定**:

- **答题模型强度** —— MemOS leaderboard 的 `ANSWER_MODEL` 未 pin,极可能是强模型;
  engram 用本地/relay 模型。88.83 里有多少来自"答题模型更强"而非"记忆机制更好",未知。
- **judge 宽松度** —— OmniMemEval judge 默认 gpt-4o-mini + "沾同一主题即 CORRECT",
  比 engram judge 宽松(§6 逐条)。

单看 leaderboard 的 88.83 无法剥离这两块。**唯一干净的剥离办法**:让 MemOS 跑在
**和 engram 完全相同的答题模型 + 相同 judge** 上。此时若 MemOS 仍显著高于 engram,
差距才是**真机制差距**(抽取/检索/记忆组织);若差距大幅收窄,则原 23pp 里很大一块是
regime 伪影(答题模型 + judge 强度),不是 engram 的记忆能力短板。

这条线直接回答 §5④ 遗留问题,是"按瓶颈分兵"的前置诊断。

## 2. 做法

- **拉代码**:`MemTensor/MemOS`(https://github.com/MemTensor/MemOS)
  + 其 LoCoMo 评测框架 `MemTensor/OmniMemEval`(§4 已确认是同一套驱动代码)。
- **答题(answer)**:**AutoDL 租用 GPU box** 上 vllm 部署的 **qwen** 模型(OpenAI-compatible,**非本地**)。
  与 engram e2e 评测同一答题栈,即 [`remote-eval-box.md`](./remote-eval-box.md) 那台
  (SSH host/port/password 每次重启轮换,凭据只走 env/tunnel、绝不落库)——固定答题模型强度这一变量。
- **判题(judge)**:**deepseek-v4-flash**,与 engram 侧对齐(同一 judge → judge 宽松度可比)。
- **数据集 / 分母**:LoCoMo,**cat 1-4 = 1540 题,cat-5 排除**(与 engram 同口径)。
- **产出**:MemOS 在"engram 同款答题 + 同款 judge"下的复现分,分类别(single/multi/temporal/open)拆。

## 3. 约束(硬)

- **只能同 harness 才可比**:MemOS 复现分不得与其 leaderboard 88.83 直接混用宣称——
  前者是"engram 同栈下 MemOS 分",后者是"MemOS 自报栈分"。只有前者能与 engram 现状对齐比较。
- **诊断/对标线,不碰引擎**:本任务在 engram 仓外(拉 MemOS 到 scratchpad / 独立目录)跑,
  产物落 session scratchpad,不进 engram 引擎、不改 `memory/ embedding/ provider/ store/`。
- **死规则复核(禁付费云 rerank)**:若 MemOS 默认栈里带**云 reranker / 云 recall 模型**,
  必须显式标注——那部分不是"纯本地栈的赢",复现时应记录是否启用、启用会否污染"机制差距"结论。
  (engram 侧对标口径始终是纯客户端/离线可跑。)
- **省钱**:vllm 答题跑在 **AutoDL 租用 GPU box(计费,metered)** —— **空闲必停**(`remote-eval-box.md` 非议)。
  deepseek-v4-flash judge 是付费 token,按 LoCoMo 1540 题的判题量预估成本、过成本闸。
- **文献口径**:MemOS/OmniMemEval 方法学核对若需查论文,走 alphaXiv MCP,不用 WebSearch。

## 4. 预期产出与下一步

| 产出 | 用途 |
|---|---|
| MemOS@同栈 LoCoMo 总分 + 分类别 | 与 engram 现状(可答 65.4% / 诚实水位 83.70%)直接对比 |
| 复现分 vs leaderboard 88.83 的落差 | 量化"答题模型 + judge 强度"regime 伪影占多少 |
| 剥离 regime 后的真机制差距 | 定位 engram 主战场:抽取质量 vs 检索质量 vs 记忆组织 |

跑完把结论(尤其"regime 伪影 vs 真差距"的分离数字)回填到
`competitive-benchmarks.md §5④`,并把本文状态改为"存档:结论已吸收"。
