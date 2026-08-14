# Research: 自适应检索深度

本文件解决 plan 阶段的技术选型，全部结论对应 spec 的 FR 与 SC。

## Decision 1: 自适应截断算法 —— gap-based knee detection（非 EVT）

- **Decision**: 在 RRF 融合后的排序分数序列上，用「归一化分数曲线的 knee 拐点 + gap-based 规则」确定 per-query 截断点 k*；**不采用** TAA-k 论文的 EVT/GPD 尾部拟合。
- **Rationale**: engram 的融合分数是 `1/(60+rank)` 的**离散加和**（最多 4 信号：keyword/semantic/entity/associative），取值有界且高度离散，不是 TAA-k 假设的连续 cosine 重尾分布。EVT/GPD 拟合在有限离散样本上不稳定，且 TAA-k 论文自承「severe score overlap 或极小候选池会模糊相关/噪声分离」。因此只取其**几何洞察**（排序曲线呈 steep→flat→steep：相关头部 → 过渡区 → 噪声尾部），配合 gap-based 截断规则。
- **具体规则**（参考 Adaptive-k 的 score-gap 与 LooComp 的 gap heuristic）：
  1. 对宽池的融合分数序列做 min-max 归一化（TAA-k 式 `y_i = (s_i − s_N)/(s_1 − s_N)`）。
  2. 计算相邻分数差 gap 序列，定位「最大 gap 拐点」（相关→噪声过渡处）。
  3. 若 gap 序列无显著拐点（分布平坦）→ 回退固定 top-k（对应 FR-004）。
  4. 施加保守下限 k* ≥ minK，防止截断点贴着关键证据边界抖动（对应 FR-005）。
- **Alternatives considered**:
  - TAA-k 完整 EVT/GPD + Cramér–von Mises 拟合 → 拒绝：RRF 离散分数不满足连续重尾假设。
  - 全局固定 top-k sweep → 拒绝：这是当前已证明「加量才涨分」的路径，非本 feature 目标。

## Decision 2: 诊断复用 + 新增 headroom 诊断

- **Decision**: 复用现有 `--attribution-trace`（已产出 `gold_rank_topk` / `gold_rank_pool` / `gold_in_pool` / `outranked_by`）与 `--recall-diagnostic` 作为 gold-rank 数据源；新增一个「自适应 headroom」诊断，对宽池分数序列跑 gap-knee 检测得到 k*，逐题比较 k* 与 `gold_rank_pool`，汇总「k* < gold_rank_pool 的问题数/占比（收缩会丢 gold）」与「k* 的分布 / 平均预算下降」。
- **Rationale**: 「缩减深度是否丢关键证据」是本 feature 的生死前提（US1）。现有 attribution 基础设施已能定位 gold 在宽池中的 rank，headroom 诊断只需叠加一个纯计算的 knee 检测，无需新模型/新存储。
- **Alternatives considered**:
  - 从零写诊断 → 拒绝：`--attribution-trace` / `--recall-diagnostic` 已覆盖 gold-rank 定位。

## Decision 3: 插入点 —— harness 层，引擎零改动

- **Decision**: 自适应截断完全落在 eval harness 的 `retrieveWithQuotaDiagnostics`：宽池检索之后（此时 `[]memory.Result` 每条带融合 `Score`）、`applyChunkQuota` 之前。k* 作为 `topK` 参数传入 `applyChunkQuota(wide, k*, quota)`；`quota` 不变（chunk 保底不被触碰，对应 FR-003）。
- **Rationale**: 引擎的 `Search(query, k)` 公开接口不变，自适应只是 harness 侧「如何决定 k」的策略，完全满足宪法 II（引擎/适配层分离）与「引擎改动须最小化」。k* 的语义是「facts+chunks 总槽位数」，facts 槽位 = k* − quota，chunks 槽位 = quota——knee 检测在纯分数序上定位拐点，`applyChunkQuota` 的既有分区拼装逻辑不变。
- **Alternatives considered**:
  - 引擎层截断（在 `SearchWithDiagnostics` 内部改 `fused[:kEff]`）→ 拒绝：触及引擎契约与宪法 IV 回归面更大，且引擎不知 chunk-quota 语义。

## Decision 4: 评测协议 —— 沿用 topk150 recipe + 自适应臂

- **Decision**: 端到端配对沿用当前 91.10% 的 recipe（`--top-k 150 --chunk-quota 12 --chunks` + Qwen3.6-35B thinking + 3-rep 多数票 + deepseek-v4-flash clean 重判），自适应臂 = 同 recipe 基础上增加 `--adaptive-topk`（opt-in，默认关）。配对用 McNemar + 类别非回归门（沿用 L0-3 的 category non-regression 纪律）。
- **Rationale**: 宪法 IV 要求「同库、同 answerer、同 judge、同聚合口径」配对；沿用已冻结的 topk150 recipe 保证可比、可归因。
- **Alternatives considered**: 无——配对口径必须与当前基线完全一致。

## 历史 verdict 前置（本 feature 的 scope 边界）

- 024 `write_dedup`（−0.91pp，0.09% 触发率）、025 `semantic-episode`（−7.7pp）、026 query-time 剪枝编译（−4.5pp）三连证伪「去冗余 / 聚合 / 压缩 context」在 LoCoMo 上的可行性。本 feature **不做去冗余**，只做「按需决定检索深度」，且其「按需而非一刀切」是唯一区别于 026 失败模式之处——这一点必须由 US1 诊断先行证明。
