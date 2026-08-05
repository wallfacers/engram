# Research: 写入侧事件时序结构记忆（027）

**Phase 0 决策记录**。每个决策含 Decision / Rationale / Alternatives considered。
决议 NEEDS CLARIFICATION（spec 未留 marker，此处的 D1–D5 是 Technical Context 的
技术未知解析，非范围歧义）。

## D1: event 抽取模型选型

- **Decision**: 阶段 1 用本地 vllm **7B**（Qwen2.5-7B-Instruct，023 planner 已部署于
  8001，零新增部署）做 event 抽取；**fail-closed 兜底**（格式非法 → 退回原文 chunk）。
  阶段 1 先导同时抽检抽取质量（schema 合法率、幻觉抽样）；合法率 < 95% 或幻觉率 > 5%
  时升 **35B**（Qwen3.6-35B-A3B，与 answerer 同栈）。
- **Rationale**: event 抽取是**固定字段输出**（比 023 planner 的开放动作规划简单得多）；
  7B 已部署、零成本启动；fail-closed 保证格式错误不污染 store，故 7B 的试错是安全的。
  35B 与 answerer 同栈但更贵/更慢，仅当 7B 质量不足时启用。
- **Alternatives considered**: 只用 35B（成本高、启动慢，先导不必要）；Ollama（需新部署 +
  不确定的本地模型质量）；无 LLM（退 chunk 路径，无法抽 event，是降级基线非方案）。

## D2: event 投影落位 + relation-summary 存储

- **Decision**: event 投影放引擎侧 **`memory/eventstore/`**，为**可丢弃可重建投影**
  （config-hash 幂等，如 022 fact 投影），**不新增 SQLite schema**；relation-summary 是
  event 的**派生态**（重建时从 event 聚合生成，不持久化）。
- **Rationale**: 引擎 feature 进引擎（宪法 II），投影保持 append-only ledger 无损（FR-003）。
  summary 由 event 确定性派生，持久化是重复状态（类似 fact 投影的聚合视图）。
- **Alternatives considered**: 放 harness 侧（违反引擎/适配分离，宪法 II）；新增 SQLite
  表存 summary（过度设计——投影可重建即无持久化必要；且追加 schema 触发迁移成本）。

## D3: 时间锚定方案

- **Decision**: 事件条目保留**两个时间字段**：`absolute_ts`（能解析则填，否则空）+
  `relative_ref`（原始相对引用原文，如「去年」「下周三」）。合并摘要时把时间上下文原样
  带入，不强制解析。相对引用解析失败**不丢弃条目**，退回保留引用。
- **Rationale**: 013 死因是时间解析器点火率低（仅 19.6% temporal 题触发），把杠杆压在一个
  解析器上必翻车。保留原文相对引用，让 answerer 在合并摘要中做相对→绝对推理，比强制
  解析可靠，且抽取本身不因解析失败而污染。
- **Alternatives considered**: 强制绝对时间解析（013 翻车，解析失败即丢信息）；只留 turn
  序号（丢失相对语义，temporal 题答不了）。

## D4: fail-closed 校验机制

- **Decision**: 两层校验：(1) **schema 校验**（JSON 含 required 字段 + 类型正确 + 长度
  上限 + 双视角结构完整），失败 → 该条消息退回存原文 chunk（作为普通 evidence），**不产生
  event**，记录失败率；(2) **幻觉审计**：不做运行时校验（贵），阶段 1 抽样人审 + 判定统计
  （抽取数/失败率/疑似幻觉率，StructMem fidelity audit 式）。
- **Rationale**: 023 教训——LLM 输出规范性不可控是训练杠杆死亡的主因，fail-closed 是
  engram 本地 LLM 写入的唯一安全姿势。运行时幻觉校验每题一次 LLM 调用，成本不可接受；
  先抽样审计掌握基数，若幻觉率超阈值再升级校验。
- **Alternatives considered**: 运行时 LLM 幻觉校验（每题贵一次调用）；静默接受非法输出
  （023 教训，必翻车）；纯规则过滤（无法识别语义幻觉）。

## D5: 配对基线

- **Decision**: 先导基线用 **`--only-questions` residual 子集**（temporal + multi-hop 题，
  按类别从全量过滤），对照已冻结 chunk store；全量配对用 **LoCoMo 85.19%（B1 已收口）**
  + LongMemEval-S 500。answerer/judge 固定（Qwen3.6-35B-A3B / deepseek-v4-flash，
  local hybrid，3 次答题多数），token cap 不变量。
- **Rationale**: 022 B1 已收口可引用（results.md 正本）；先导复用 023 的 formal-subset 模式
  （~35 分钟/次），不做全量起步，符合复盘「不盲烧」纪律。
- **Alternatives considered**: 直接全量配对（贵，且先导未知时全量是浪费）；无基线自比
  （违反宪法 IV）；只跑单 rep（answerer temp=1.0 噪声大，repeats≥3 为 025 纪律）。

## 承接资产清单

- `provider/`（LLM 抽象，本地 sidecar 可替换）——event 抽取的模型接口
- `memory/pipeline/`（022 fact 抽取）——抽取 prompt 的既有模式参考
- `cmd/locomo-bench/representation_eval.go`（025 三渲染器 shared-anchor 设计）——event 渲染器复用
- `--only-questions` formal 子集模式（023）——先导跑法复用
- `docs/evaluation/reports/cost-effectiveness-retrospective-2026-08.md` §6（alphaXiv 调研）——机制来源与不可比边界
