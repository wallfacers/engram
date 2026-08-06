# Research: Agentic 多步记忆导航

**Date**: 2026-08-06 · **来源**: alphaXiv 精读（MemCog / NapMem）+ 代码库探索（local_planner.go / locomo-bench）

## 决策 1：导航范式选型 —— 零训练 ReAct/JSON 工具导航为 US2 主路径，RL 为 SaaS 门禁后续

- **Decision**: US2 用「本地强模型（35B）+ JSON 工具调用 + 多步循环」的零训练导航（MemCog 式）；US4 才考虑 NapMem 式 RL 训练（SaaS 线）。
- **Rationale**:
  - MemCog 的导航是 prompt/ReAct 驱动、**不训练**，LoCoMo 92.98 / LongMemEval 95.80（SOTA 级），消融 w/o Graph Overlay −6.79pp——结构导航贡献明确。
  - NapMem 消融：w/o navigation 54.08 → 带工具 ~60.77（**导航本身 +6.7pp**，非 RL）；w/o RL → Full 48.39→62.74（RL 再叠，但主要来自 PersonaMem 隐式偏好，LoCoMo 类别增益需实测）。零训练先导成立且省钱。
  - 028 已确立「先验证机制、再投训练」的门禁模式；RL 训练成本高（Qwen3.5-9B 级 GRPO），必须门禁驱动。
- **Alternatives considered**: MRAgent / SLEUTH 推理状态（agent 多步遍历，更复杂）；单次检索（已 021 证伪）；写侧结构（已 027/028 证伪）。

## 决策 2：工具调用格式 —— 复用 local_planner 的 JSON 结构化输出，不依赖 vllm 原生 function-calling

- **Decision**: 导航 agent 每次输出固定 schema 的 JSON 工具调用（如 `{"tool":"search","query":"...","refine":true}`），Go 侧解析执行。
- **Rationale**: `cmd/locomo-bench/local_planner.go` 已证明此模式在 35B sidecar 上可靠（provider + JSON + timeout + fail-closed）；规避 Qwen/vllm 版本对原生 function-calling 的支持/兼容风险（Blackwell + vllm 版本栈已多坑）。
- **Alternatives**: vllm 原生 `tools=` function calling（Qwen3.6-35B 支持但需验证版本；非必须，不加复杂度）。

## 决策 3：预算模型 —— 导航消耗单独记账，最终证据在既有 answer-context 预算内

- **Decision**: 导航每步的检索中间结果**不计入** answer-context 预算（仅作导航决策输入）；最终「证据包」MUST ≤ 既有 answer-context cap；导航消耗（步数 × token）单独记账。
- **Rationale**: 008 纪律防「大力出奇迹」——多步导航若靠塞更多证据才赢，不构成预算下提质的赢。MemCog 每 query 2-3 步、NapMem 2.15 步，中间 token 开销可控。
- **Alternatives**: 多步全部计入预算（更严格但会让配对失配现有基线口径）；不设预算（违宪法 I/V）。

## 决策 4：检索工具实现 —— 每工具调用一次 Retriever.Search，状态在 Go 侧维护

- **Decision**: `search(query)` → 现有混合检索（语义+BM25+实体 RRF）；`expand_query(text)` → 基于中间证据的查询改写后再次 search；`follow_entity(entity)` → 实体锚定检索；`stop(evidence)` → 组装证据包。
- **Rationale**: 引擎 untouchable（FR-003），导航编排在 cmd/ 层多次调用引擎 public API（`Retriever.Search`），无引擎改动；复用现有混合检索能力（021 之前的深度召回基建）。
- **Alternatives**: 引擎新增多轮检索状态 API（明确拒绝——违反 FR-003，除非诊断证明必需，届时 STOP 提合同增量）。

## 决策 5：fail-closed —— 导航失败/超步数 → 用已收集证据或单次检索结果作答

- **Decision**: 步数硬上限（默认 4）+ 每步 timeout；导航失败/无 LLM → 回退单次检索 top-k 结果作答（与现状一致）。
- **Rationale**: 宪法 V 优雅降级 + 027/023 fail-closed 模式（errPlannerUnavailable 回退）。绝不产生空答案。
- **Alternatives**: 导航失败报错（违宪法 V）；静默空答（数据污染）。

## 决策 6：评测协议 —— 沿用 027/028 配对纪律 + 008 铁律 + 类别不回归

- **Decision**: 84 题（temporal 59 + multi-hop 25）× 3 reps majority + McNemar；GO 门 = 多步 majority ≥ 单次基线（008 铁律）；类别不回归（L0-3，temporal/multi-hop 任一显著崩则否决）。
- **Rationale**: 027/028 配对纪律延续（同 store/answerer/judge/预算）；008 铁律唯一 GO 门；L0-3 防 013/014「整体微涨类别崩」覆辙。
- **Alternatives**: 单次评测（不稳定，temp=1.0）；无类别门（重蹈 013/014）。

## 决策 7：US1 诊断 —— 三分类（gold 在池 / 单次 top-k 捞到 / 模拟多步可救）

- **Decision**: 对 84 题记录：①gold 在候选池（全对话 oracle）；②单次 top-k=30 是否捞到；③捞不到时「换查询改写 / 扩大粒度 / 实体线索跟链」三类模拟动作能否救回。占比 ≥20% 才进 US2。
- **Rationale**: 根除 021「没查 gold 在不在池就硬上」；027 阶段 0 先例。已有先验（199/200 gold 由 chunk 承载、gold 中位 rank 71-90 深层召回不足）指向深层可救，但必须实测归因。
- **Alternatives**: 直接投 US2（无诊断，风险盲投）；全量 oracle（1540 题，成本高，先导子集足够）。

## 决策 8：MemCog 与 MemOS 的结构辨析 —— 树+图 vs 图+向量，不是同构"树"

**背景**: 常被误以为 MemCog 与 MemOS 都是"树"结构。alphaXiv 精读（MemCog 2605.28046 / MemOS 2507.03724 长版 + GitHub `MemTensor/MemOS`）确认两者结构哲学根本不同。

**MemCog = 树 + overlay 链接图（导航优先）**:
- 存储本体: `Dimension → Page → Section` 三级层级（树）；Section 含 `summary` / `structured_facts` / `related_pages`
- 跨维度关联链接（4 类: `related_to` / `temporal_next` / `caused_by` / `contrasts_with`）在树之间织网（overlay graph）
- 消融（GLM-5.1 backbone）: `w/o Hierarchy` −6.53、`w/o Graph Overlay` −6.79 —— 树和图各自贡献
- 定位: 为"导航"优化的存储；每个节点暴露结构上下文（可见链接/兄弟页/维度位置），让 agent 决定下一步往哪走

**MemOS = 图 + 向量（资源管理优先，不是树）**:
- README 原话: *"structured as a graph, inspectable and editable by design, not a black-box embedding store"*
- Self-Host 基础设施: **Neo4j（图数据库）+ Qdrant（向量库）** —— 图数据库是存储主体
- 论文 §5.4.1 MemOperator: *"knowledge-graph structure treats memory as nodes connected via semantic edges"*；唯一层级是 task–concept–fact 任务 schema（非存储树）
- 核心抽象: `MemCube`（统一资源单元）+ `MemScheduler`（调度）+ `MemLifecycle`（生命周期）+ `MemGovernance`（治理）
- **两形态**: 主服务/Cloud（Neo4j+Qdrant 图栈）vs `memos-local-plugin 2.0`（SQLite+FTS5+vector，与 engram 混合检索同族技术）

**对照意义**: engram 与 MemOS 的对比本质是"engram 混合检索 vs MemOS 图+向量"；同栈复现（MemOS 82.40% vs engram 85.71%）还叠加 judge/answerer/预算 regime 差（见 `docs/evaluation/reports/memos-locomo-reproduction.md`），不能外推为通用排名。

## 决策 9：MemCog 分数口径辨析 —— 89 系列数字不可比，且论文无"单次检索 baseline"

**背景**: 常拿 MemCog（GLM-5.1 89.83 / GPT-4.1-mini 92.98）与 engram 探针（v4-pro answerer + v4-flash judge 89.03%）直接对比，暗示"导航机制 ≈ 0"。

**不可比轴（至少 4 项）**:

| 轴 | MemCog | engram 89.03% |
|---|---|---|
| judge | mem0 配置 LLM-as-judge | deepseek-v4-flash + mem0-aligned |
| 题数 | 论文未报具体 n | 1540（cat 1–4） |
| 上下文预算 | 多步导航 token 消耗未报 | answer-context cap 3600 / 实测 3262 |
| repeats | 未报 | 3-rep majority |

engram judge 已实证比 Mem0/OmniMemEval 严（locomo ledger 2026-07），0.8pp 级差在 judge 噪声带内，读不出"导航机制 ≈ 0"。

**关键缺口: MemCog 论文没有"单次检索 baseline"行**:
- 对比基线全是别的系统（mem0 56.16 / HyperMem 89.77 等），**没有"自己栈上关导航、单次 top-k"的对照组**
- 消融 `w/o Graph Overlay` −6.79 / `w/o Hierarchy` −6.53 是"去掉链接/层级但保留导航工具"的增量，**不是"导航 vs 单次检索"的值**
- ⇒ 论文从未证明"多步导航 > 单次检索"，这正是 029 US2（008 铁律: 多步 ≥ 单次才 GO）要补的实测缺口

**对 029 的结论**: 89 系列数字（MemCog 89.83 / engram v4-pro 89.03%）都是强闭源 answerer 的分，不是记忆机制的分；导航的潜在边际在弱 answerer + 低预算（engram 35B 场景），029 因此在本地 35B 上配对实测，不跟闭源探针比。

## 已排除方向（防重复打假）

- 写侧 event/图替换原文（027/028/014 证伪，spec 明确范围外）
- 单次检索表示/排序改进（reranker/doc2query/时间窗/IRIS 等 021 六次证伪）
- agent 融合写回（022 证伪）
- 付费云 rerank/recall 涨点（DEATH RULE）
