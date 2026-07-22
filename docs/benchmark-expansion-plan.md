# 基准扩展计划:LongMemEval 先行 + 其他数据集盘点

> 🧭 **状态**: 活跃(计划稿) · **目标**: 回答"差距怎么推进"与"LoCoMo 之外测什么",
> 给 LongMemEval 一份可并发执行的简单计划。竞品分数正本在
> [`competitive-benchmarks.md`](./competitive-benchmarks.md);涨点 backlog 正本在
> [`memory-strategy.md`](./memory-strategy.md);本文不重复拆解。
>
> 写作日期:2026-07-23。竞品数据集覆盖经 alphaXiv 一手核实(Mem0 arXiv:2504.19413、
> MemOS arXiv:2507.03724 v4)。

---

## 1. 差距推进:现有计划(引用,不新开)

现状:LoCoMo 端到端 **83.70%**(mem0-aligned judge、无 reranker、全量 1540、单次);
差 MemOS 88.83 约 **5.1pp**(force-answer 口径 84.22%,差 ~4.6pp)、差 Mem0 92.5 约 **8.8pp**。

推进计划**已在轨,不缺计划,缺执行**:

1. **009-retrieval-attribution-gate(进行中)** — US1 归因 trace 已提交(87400f8),
   待 008 store 到位后跑全量归因,产出四象限分布;US2 排序改动被三道门 gate,
   有 Q3 证据才开工。定位:把"差距在哪一段"从猜测变成逐题归因。
2. **答题侧杠杆(008 已证方向)** — 检索召回**不是**瓶颈(+15.457pp coverage → −0.06pp
   answer, p=1.0);最弱是 open-domain 56.25%(n=96)与 temporal 82.24%(n=321)。
   候选杠杆(答题 prompt 五步推理对齐 Mem0、时间锚定)正本见
   [`locomo-score-levers.md`](./locomo-score-levers.md) 与 competitive-benchmarks §5。
3. **本文新增的一条腿:基准扩展(§2-§3)** — 单一 LoCoMo 不足以支撑对标与论文
   RQ6(结论外推),LongMemEval 是下一个必补基准。

---

## 2. LongMemEval 执行计划(简单版,可与 009 并发)

**现状纠偏**:`testdata/longmemeval/` 目前只有 2KB 手造 `sample.json`(schema 示例),
**全量数据集尚未下载**;paper-outline RQ6 标注"engram 一手结果为空"。

### 步骤

| # | 事项 | 要点 |
|---|---|---|
| T1 | **取数** | 官方仓 `xiaowu0162/LongMemEval`(GitHub / HuggingFace)。先 **LongMemEval-S**(500 精编题,haystack ≈115k token/题;论文纪律:必须明示用的是 S,不得简称全量)。落 `testdata/longmemeval/`,gitignore、不再分发;衍生 store/结果归档到 HF 私仓 `wallfacers/engram-locomo-eval-assets`(仓名后续可改为 eval-assets 通用仓) |
| T2 | **harness** | 新 `cmd/longmemeval-bench`,**adapter-only、引擎零改**(`git diff --name-only -- memory embedding provider store internal` 必须为空)。复用 locomo-bench 的 建店(抽取+embedding)→ 检索 → 答题 → judge 骨架;新增:haystack_sessions 摄入、6 题型(single-session-user/assistant/preference、multi-session、temporal-reasoning、knowledge-update)映射、abstention 子集(约 30 题)的处理**显式声明**(计入/排除须写明,对齐竞品口径) |
| T3 | **判分双口径** | 主口径 = LongMemEval **官方 per-question-type judge prompt**(可比性正本);对照口径 = 本仓 mem0-aligned judge。两者**分开报告,不混用** |
| T4 | **跑法与成本** | 同 LoCoMo 栈:远端 vllm(Qwen3.6-35B-A3B-FP8)答题+抽取 ≈ 近免费,bge-small-en-v1.5 384d,top-k30 起步;judge 走小额付费口。先 50 题 smoke,再全量 500。WSL2 setsid detach 纪律照旧 |
| T5 | **基线声明** | 宪法 IV:新数据集 = **新的独立基线**,与 LoCoMo 83.70% 基线互不替代;首跑分数即声明为 LongMemEval 基线,eval-config 与算法改动分开 commit |

### 并发纪律

独立 feature(`specs/NNN-longmemeval-bench/`,走 brainstorm → SDD)+ 独立 worktree;
只加 `cmd/` 新目录、不碰引擎 → 与 009(改 `cmd/locomo-bench` + 未来引擎 US2)**物理不相交,可安全并行**。

### 验收门

- harness 离线单测绿(sample.json 驱动,无网络);
- 全量 500 一次跑通,出 per-type 分 + overall;
- 报告含协议全披露(变体 S、judge 版本、答题模型、top-k、abstention 处理)。

---

## 3. 其他数据集盘点(竞品跑了什么,engram 补不补)

竞品覆盖(一手核实):**Mem0 论文只跑了 LoCoMo**;92.5/94.4 来自其 2026-04 平台
blog(LoCoMo + LongMemEval + BEAM)。**MemOS 论文(v4)跑了 4 个**:LoCoMo 75.80、
LongMemEval 77.8、PreFEval、PersonaMem(统一 GPT-4o-mini 口径);88.83/89.20 是其
官方仓/OmniMemEval 榜口径,OmniMemEval 扩到 10 数据集。

| 数据集 | 竞品分(口径见注) | 与 engram 契合度 | 优先级 |
|---|---|---|---|
| **LongMemEval** | Mem0 blog 94.4;MemOS 论文 77.8 / 榜 89.20 | 同任务形态(长程对话记忆 QA),两家共同第二基准,论文 RQ6 必需 | **P0 = §2 本计划** |
| PersonaMem | MemOS 论文 61.2 / 榜 v2 40.58 | 动态用户画像/个性化,正对 SaaS 习惯记忆方向([saas-habit-memory-design](./saas-habit-memory-design.md)) | P1(SaaS 方向立项时一并) |
| PreFEval | MemOS 论文 77.2(0 turns) | 偏好遵循/偏好幻觉,正对 freshness/条件召回 backlog([memory-freshness…](./memory-freshness-and-retrieval-policy.md)) | P1(同上) |
| HaluMem | MemOS 榜 80.91 | 记忆幻觉,对应 freshness 文档的 memory-hallucination 问题 | P2 |
| BEAM 1M/10M | Mem0 blog 64.1/48.6;MemOS 榜 56.75 | **超出诚实规模边界**(宪法 V:单用户 ~100k-entry class;1M/10M token 级明确是 future work) | 暂不做,不承诺 |

注:竞品"论文分"= 统一 GPT-4o-mini 的受控对比;"榜/blog 分"= 各家自报模型栈,
绝对值不可与 engram 直接混用(competitive-benchmarks §4 口径结论照旧适用)。

**顺序建议**:LongMemEval(本计划)→ PersonaMem / PreFEval(与 SaaS 习惯记忆、
freshness 立项捆绑,避免为测而测)→ HaluMem;BEAM 挂起为显式 future work。
