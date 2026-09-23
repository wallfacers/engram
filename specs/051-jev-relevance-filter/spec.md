# Feature Specification: Jev 候选相关性过滤（宽池 + 按问题筛，读侧）

**Feature Branch**: `051-jev-relevance-filter`

**Created**: 2026-09-23

**Status**: Draft — speckit-clarify 进行中

**Input**: User description: "看 docs/engram-probe-curation-design.md §19–21/§23–25，实现 Jev 过滤，解决『top-150 为什么高分，以及它在生产上的问题』；20.1 TypeSafe Jev 该接；20.2 Jev-Mem 论文先别搬架构；微软论文 Grounding Agent Memory 相关（短包/整理员/探测）后续再说。评测正本 docs/evaluation/results.md，统一答题合同。"

来源文档：[docs/engram-probe-curation-design.md](../../docs/engram-probe-curation-design.md)（§18 时机、§19 top-150 问题定位、§20 Jev 是什么、§21 核心配方、§23 跑分协议、§25 实施顺序）。本 spec 只覆盖其 **P1.5「Jev 读过滤」**。

## 背景与问题定位

统一答题合同下（`docs/evaluation/results.md`）：LoCoMo top-k 30 = 87.9% → top-k 150 = 91.43%（+3.54pp）；LongMemEval-S top-k 30 = 90.2% → top-k 150 = 92.0%（+1.8pp）。设计文档 §19 判定：该涨幅约八成来自**灌上下文**（~2.4×），不是更深的召回；k=150 且会翻错部分已答对题（稀释、排名变化）。生产默认是 `limit=8`，32k 上下文的 top-150 不能当每轮默认。

因此缺的是「宽池保覆盖、按当前问题筛掉无关命中」的第二段过滤：已有 BM25+向量+实体 RRF 做第一段召回（不动），TypeSafe Jev（System-One 校准概率模型，只给概率不生成文字）做第二段「这条对当前问题是否必要」。**不是**再训检索模型，**不是**生成式 rerank。

## Clarifications

### Session 2026-09-23

- Q: Jev 过滤器应做引擎侧契约还是适配器/skill 侧先行？ → A: A+B 双落地——引擎侧 `RelevanceFilter` + `memory_search` 增量参数与 skill/适配器侧本地过滤路径都要（共享同一过滤语义与遥测）；且读写两侧都需要 Jev 门：读侧候选过滤 + 写侧写前门（§18.1/§22，推翻先前「写门推迟」安排）。
- Q: 写前门的拦截范围到哪为止（含 `needs_probe` 路由）？ → A: 只拦「一次性」和「密钥」两类。密钥看情况：用户**明确要求**记录自己的 API key（HF/DeepSeek 等厂商 key）则照记；未要求则不记（尤其别人的 key）。环境结构类维持现行规则，`needs_probe` 题暂不启用；微软短包/整理员线后续再做，但写前门决策出口必须与其预留兼容、不得冲突。
- Q: 读侧过滤一条都没留下时最终返回什么？ → A: 两段式（选项 A）——先 θ=0.5；全灭时自动放宽到 0.35 最多留 3 条；仍全灭才诚实返回空。放宽不依赖调用方信号（读前门不在本 spec）。
- Q: 四臂跑分的合入必过门是只认 LoCoMo，还是 LoCoMo + LongMemEval-S 双数据集？ → A: 只认 LoCoMo 1,540 四臂（3-rep clean）；LME-S 抽取数据太贵，本 feature 不跑（明确推迟而非永久否决）；D 臂结论仅限「LoCoMo 口径」措辞，未来若补跑 LME-S 确认且过 SC 门后才允许扩为跨数据集措辞。
- Q: TypeSafe Jev 付费云打分与 AGENTS.md「禁止付费云 rerank/recall 模型作为打分杠杆/默认·shipped·推荐栈」死亡条款如何相容？ → A: 选项 C——修订死亡条款：Jev 允许进入推荐栈、D 臂分数允许报作涨分；AGENTS.md 硬规则随本 feature 同步改写并记录修订缘由。历史分数归因不改口（既有 91.43% 的成因仍是灌上下文，不得改称「因为上了 Jev」）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 — 宽池 + Jev 过滤的读路径（引擎侧可选 sidecar + MCP 参数） (Priority: P1)

用户/宿主调用 `memory_search` 时显式给出宽候选池与过滤器（如 `candidate_pool=150, filter="jev"`）：RRF 先取宽池，Jev pointer 模式逐条判「这条对回答/执行**当前问题**是否必要」，按校准概率阈值留下并按 p 排序，短名单交给答题模型；没有任何相关记忆时**诚实返回空**。不传新参数时行为与今天完全一致（`candidate_pool` 默认 = `limit`，`filter` 默认 `"none"`）。

**Why this priority**: 这是 §19 生产问题的直接解药——把「评测才能用的宽池」变成「生产每轮用得起的宽池」（覆盖接近 150、噪声接近 top-8、~0.3s / 约 $0.0004/次）。其余（跑分、skill 旋钮）都建立在此路径之上。

**Independent Test**: 无网络单元测试：stub RelevanceFilter 验证「宽池→过滤→短名单→失败退回原 RRF 前 limit 条」的合同；MCP 内存传输契约测试验证新参数默认值不变式（不传新参数 == 今天的行为）。带 key 的 Jev 适配器单测用录制/桩响应验证 pointer 模式请求形状与阈值切分。

**Acceptance Scenarios**:

1. **Given** 未配置 Jev（无 key / `filter="none"`），**When** 调用 `memory_search`，**Then** 结果与今天逐字节一致（parity 基线）。
2. **Given** 已配置 Jev，**When** `memory_search(query, limit=8, candidate_pool=150, filter="jev")`，**Then** RRF 先取 150 条候选，Jev pointer 模式打分，p ≥ θ 的条目按 p 排序返回，条数不超过 `k_show_max`；返回 `filter` 元数据（backend/kept/dropped/theta/degraded）。
3. **Given** 候选池里没有任何与问题相关的记忆，**When** 过滤完成，**Then** 先两段式回退（θ=0.5 全灭 → 0.35 最多 3 条）；仍全灭则返回空 hits（诚实返回，不硬塞 top-k），`scope` 语义与「检索空」一致。
4. **Given** Jev 调用超时/失败/无 key，**When** 过滤路径被触发，**Then** `degraded=true`，退回原 RRF 顺序的前 `limit` 条，不报错、不空手。
5. **Given** 用户钉过（pinned）且 trigger 命中的条目，**When** 过滤完成，**Then** 该条目即使 p < θ 也注入短名单。

### User Story 2 — 四臂跑分协议与评测正本落账 (Priority: P2)

在同一 unified answer contract、同一 clean judge、同一 store 下跑 A/B/C/D 四臂（必测数据集：LoCoMo 1,540，LME-S 推迟）：A 生产基线（RRF limit=8）、B 评测宽池（k=150 全塞，现有高分）、C 宽池+截断（150 里取前 8）、D 宽池+Jev。指标：答题准确率（clean 重判、3-rep 多数）、pool recall@150、recall@shown、短名单精确率、空注率、延迟 p50/p95/USD/token 分段、B↔D 翻错题双向列出（禁止只报净涨）。结果写入 `docs/evaluation/results.md`（正本）+ `experiment-verdicts.md`。

**Why this priority**: Constitution IV 硬门——动了检索必须可比口径跑分且不回退；且 §23 的目的正是证明「涨的是准确率与精确率，不是又灌了 150 条」。没有它，US1 不允许合入。

**Independent Test**: 跑分 harness 可复现（冻结 store/种子/合同）；四臂各自产出可独立复核的 per-question 结果与指标文件。

**Acceptance Scenarios**:

1. **Given** 四臂跑分完成，**When** 审查报告，**Then** 六项成功标准（SC-001..006）逐项有数、达标/未达标明确（未达标标 HOLD，不静默放宽）。
2. **Given** D 臂相对 B 臂的翻错题，**When** 审查报告，**Then** 题号双向列出（B 对 D 错、D 对 B 错）。

### User Story 3 — 生产旋钮与诚实降级（skill / 宿主侧） (Priority: P3)

宿主/skill 可通过显式配置启用宽池+过滤（如 `ENGRAM_SEARCH_POOL` / `ENGRAM_FILTER` 或 MCP 实参）；未配置 key 时行为与今天完全一致。适配器报告降级只基于**结构性事实**（无 key/endpoint 配置、调用失败），不探测引擎。

**Why this priority**: 把能力交到生产手上但默认不改行为；「没模型也能搜」是宪法 I 底线。

**Independent Test**: 无 key 环境下全测试绿；skill 文档化配置面与降级语义。

**Acceptance Scenarios**:

1. **Given** 无任何 Jev 环境变量，**When** skill 按默认流程 `memory_search`，**Then** 行为与今天完全一致。
2. **Given** Jev 端点中途失败，**When** 一次检索，**Then** 本次 `degraded=true` 且结果=RRF 前 limit 条，后续调用可恢复（不熔断成永久降级）。

### User Story 4 — 写前门（Jev opt-in，读侧之外的写侧） (Priority: P2)

在 `memory_write` 路径前加可选 Jev 写前门：判「一次性 vs 稳定」「是不是密钥」，一次性直接 skip；密钥默认 skip，但用户明确要求记录自己的 key 则照记（详见 FR-010）；失败/无 key 一律 fail-open 沿用现行 skill 规则。不破坏路径 A 的「当轮写入 + 当轮告知」契约——门只拦不该写的，绝不延迟该写的。决策出口为未来短包/整理员线路预留路由位，两者不得冲突。

**Why this priority**: 维护者拍板「读写都需要」；写侧是记忆入口，把「这周赶 demo」「sk-xxx」挡在门外的收益直接，且不依赖任何推迟中的短包/整理员能力（拦截边界见澄清）。

**Independent Test**: 无网络单测（stub 门）验证 fail-open 与拦截路由；带桩响应的写门单测验证「一次性/密钥 skip、稳定事实照写」。

**Acceptance Scenarios**:

1. **Given** 用户说「这周赶 demo，先快点」（一次性细节），**When** 门启用，**Then** 不写入、不当轮告知、不追问。
2. **Given** 草稿含密钥/token 且用户未要求记录，**When** 门启用，**Then** 不写入且不在任何响应/日志中回显内容。
3. **Given** Jev 失败/无 key，**When** 走写入路径，**Then** 行为与今天完全一致（fail-open）。
4. **Given** 用户亲口稳定事实（「以后都用 pnpm」），**When** 门启用且判定 durable，**Then** 仍当轮写入+告知，路径 A 契约不变。
5. **Given** 用户明确要求「把我的 HF API key 记下来」，**When** 门启用，**Then** 照常写入用户本地记忆库（不进日志/工具响应/受跟踪文件），当轮告知。

### Edge Cases

- 过滤结果为空：两段式回退——θ=0.5 全灭时放宽到 0.35 取最多 3 条，仍全灭才诚实返回空（US1-3；放宽不依赖调用方信号，读前门不在本 spec）。
- 候选池 > 单请求片上限：按 §21.4 分片并发，单片 32–64 条，合并各片概率。
- Jev 超时/429/配额尽：一律走 US1-4 降级；单次失败不累计成永久降级。
- θ 误设过高导致大面积空：`k_show_max=12` 硬顶与调参建议记录在运维文档，不静默改 θ。
- pinned 条目注入与 `k_show_max` 冲突：已定死——trigger 命中 = query 大小写不敏感包含 trigger；pinned 旁路注入优先占位且**计入 KShowMax 总数硬顶**（护 SC-002）；病态 pinned>KShowMax 时全部展示并在遥测注记 override；pinned 不计 Dropped。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 引擎 MUST 提供可选 `RelevanceFilter` 接口（`Filter(ctx, query, []ScoredEntry) → ([]ScoredEntry, FilterMeta, error)`），与 embedder 同款「可选 sidecar、typed-nil 纪律」；提供 `none` 实现（直接截断到 limit）。同时 MUST 提供适配器/宿主侧过滤路径（B 路径 = `cmd/engram-filter` 薄 CLI：调用方提供候选集，本地过滤，不依赖新 MCP 参数），两条路径共享同一过滤语义与遥测。核心检索路径（BM25/FTS5/向量/实体/RRF）MUST NOT 被替换或依赖过滤器。
- **FR-002**: `memory_search` MUST 新增可选参数 `candidate_pool`（默认 = `limit`）与 `filter`（默认 `"none"`）及 `theta`（可选覆盖）；默认值下行为与今天逐字节一致（parity 不变式）；校验/夹取按 contracts/memory-search.md 表（pool<limit→limit；theta∉(0,1)→拒绝；RelaxTheta>Theta→夹为 Theta；pool ≤500 硬夹）。
- **FR-003**: Jev 适配器 MUST 使用 pointer 模式组请求（共享 `state`，每条记忆一道条件化「对当前问题是否必要」的题），按校准概率阈值保留（θ 默认 0.5），`k_show_max=12` 硬顶，pinned+trigger 命中条目旁路注入；空结果回退 MUST 两段式：θ=0.5 全灭时放宽到 0.35 最多留 3 条，仍全灭 MUST 诚实返回空（不硬塞 top-k）；放宽段可经 `ENGRAM_JEV_RELAX=0` 关闭（kill-switch），评测含 D-noRelax 变体供 SC-004 测量。
- **FR-004**: 无 key / 离线 / 调用失败 MUST 降级：`degraded=true`，退回 RRF 前 `limit` 条；核心路径禁止 `import` 死绑 TypeSafe；密钥 MUST 只经环境变量流转，禁止写入日志/工具响应/受跟踪文件。
- **FR-005**: 每次过滤 MUST 产出遥测元数据（backend、θ、kept、dropped、latency、cost、degraded），随工具结果的 `filter` 字段返回。
- **FR-006**: 评测 MUST 按 §23 四臂协议执行（unified answer contract、clean judge、3-rep 多数），报告 §23 全部指标并写入评测正本（`results.md` + `experiment-verdicts.md`）；TypeSafe Jev 与本地近似（Open-Jev 等）的评测分支 MUST 分开报告、禁止混报；历史既有分数的归因 MUST 不改口。
- **FR-007**: 合规姿态（维护者 2026-09-23 拍板，选项 C）：修订 AGENTS.md「No paid cloud rerank/recall model as a scoring lever」死亡条款——Jev 允许 shipped、允许进推荐栈、D 臂分数允许报作涨分（仍受 SC-001..006 门约束，过门才有涨分措辞）；条款改写须随本 feature 在 AGENTS.md（Key Conventions）落地并记录修订缘由与生效范围（仅限本 Jev 过滤与写门面——读写双侧，维护者「读写都需要」拍板；且须同 commit 同步修订 CLAUDE.md 中的镜像条款；其他云打分模型仍受原条款约束，除非另行修订）。
- **FR-008**: 明确不做（Out of scope）：Jev-Mem（arXiv:2609.23986）的图控制器/记忆分型/预算架构（20.2 决定：先别搬）；微软 Grounding Agent Memory（arXiv:2609.11060）相关的短包/整理员/探测/`tool_observation` 等 §0–17 全部内容（维护者决定：后续再说）；写侧 Jev 门（§22）已由维护者（2026-09-23，「读写都需要」）纳入本 spec（见 US4/FR-010），不再属本条排除项；用 Jev 替换 BM25/FTS5；默认生产 k=150 且不过滤；用 Jev 生成记忆正文。
- **FR-009**: §18.2 读前门（`needs_memory` 先判「这题要不要搜」，false 则不打 MCP）——**Deferred**（澄清名额已用尽）：它为独立薄增量，不阻塞本 spec（FR-003 空结果回退不依赖它）；留待 /speckit-plan 决定是否并入或另立小 spec。
- **FR-010**: 写侧 MUST 提供可选 Jev 写前门（§18.1/§22 题目经修订）：opt-in、默认关。拦截规则：①「一次性 vs 稳定」——一次性 skip；②密钥——默认 skip，但用户**明确要求**记录自己的 API key（HF/DeepSeek 等厂商 key）时 MUST 记录；未明确要求时不记录（尤其他人密钥）；密钥内容仍 MUST NOT 进入日志/工具响应/受跟踪文件，仅存于用户本地记忆库。传输边界（互审修订）：含密钥形状的草稿 MUST 先经**本地形状预判**短路（用户明确要求记录→本地直接 `write`；未要求→本地直接 `skip`），**不发云**；确需发云的 userTurn/draft MUST 先遮蔽密钥形状子串；写门云调用超时 500ms 且 fail-open；门控仅覆盖手动 `memory_write`（`memory_ingest`/抽取/curation 不门控）。输入映射（互审修订）：`draft = memory_write.content`；`memory_write` 新增可选参数 `user_turn`（缺省空→`is_durable`/`is_preference` 仅按 draft 判定）与 `user_requested`（缺省 false；true = 用户明确要求持久化，密钥形状内容本地直接 `write`）。环境结构类维持现行规则，`needs_probe` 题暂不启用；门失败/无 key MUST fail-open 沿用现行 skill 写规则；MUST NOT 破坏路径 A「当轮写入 + 当轮告知」契约。决策出口 MUST 为未来短包/整理员线路预留路由兼容（如 `route: write|skip|defer_to_packet`，本期只产生 write/skip，`defer_to_packet` 仅占位），两者不得冲突。

### Key Entities *(include if feature involves data)*

- **RelevanceFilter**: 引擎可选接口；实现有 `none`（截断）、`jev`（TypeSafe 适配器）；未来可有本地近似（Open-Jev 等，单列评测分支）。
- **FilterMeta**: 单次过滤遥测（backend/theta/kept/dropped/latency_ms/cost_usd/degraded）。
- **CandidatePool**: RRF 宽池（`candidate_pool`，评测用 150，可选 64/100 用跑分选定）。
- **EvalArm**: 四臂 A/B/C/D 定义（§23），D 臂短名单规模由阈值决定而非死切 top-k。

## Success Criteria *(mandatory)*

### Measurable Outcomes

（§23 六项硬门，全部在 unified answer contract + clean judge + 3-rep 口径下；任一不达标 → 标 HOLD，禁止静默放宽）

- **SC-001**: D 臂答题准确率 ≥ B 臂 − 0.5pp 且无统计显著回退（必测门：LoCoMo 1,540，3-rep clean 多数，**run 内配对 + 精确 McNemar p + 配对 CI**（复用 paired_eval.go，禁止跨 run 比较）；LongMemEval-S 500 因抽取成本过高本 feature 不跑——推迟而非否决——结论仅限「LoCoMo 口径」）。
- **SC-002**: D 臂平均展示条数 ≤ 12，且注入答题上下文的 token ≤ B 臂的 1/3。
- **SC-003**: 同等展示预算下 D 臂 recall@shown ≥ C 臂（两口径均报：①门限口径 C 截至 12；②**逐题等预算对照** C 截到 D 该题实际展示数；另报 C@8 参考；指标源用既有 evidenceRecallAt 缝；证明条件化过滤比「150 里傻取前 N」更能保住金块）。
- **SC-004**: 金块不存在的题目（LoCoMo category-5 对抗题，按 038 协议以 declared block 纳入 C/D 跑分、refusal-scoring 口径、总体声明）上：空注操作定义（互审修订）= **该题展示集为空（工具字段 `results` == []）**（单一判据，删除恒真的 gold-overlap OR）；判定两则均需满足：① D-noRelax 空注率 ≥ C；② **D-noRelax 空注率绝对下限 ≥ 50%**（预注册值；如需调整必须在门限跑前修订并记录，跑后不得改动）。SC-004 在 **D-noRelax** 变体上测量（见 FR-003）。
- **SC-005**: Jev 降级路径（FR-004）的准确率不低于 A 臂（降级展示集 ≡ **C@8** 展示集——降级截断到调用方 limit=8；C@12 仅用于 SC-003 门限对照；同集可比，不设独立降级臂；显著性用配对 A↔C McNemar + 配对 CI；SC-001..007 总判定经 HOLD 进入 `promotionVerdictFor`）。
- **SC-006**: 无 API key 时全部测试仍绿（degraded 路径），默认行为 parity。
- **SC-007**: 延迟 p50/p95 与 USD/token 分段记账（宽池检索 / Jev / 答题），Jev 单次调用 ≈0.3s、≈$0.0004 量级预期，超一个数量级视为异常需归因。

## Assumptions

- TypeSafe Jev API（或 OpenRouter 转售）可经密钥访问；请求形状按 §21.4 pointer 模式（`state` 共享 + 每记忆一道 noul 题）。
- 答题模型/判题模型口径与评测正本一致（Qwen3.6-35B-A3B-FP8 / deepseek-v4-flash，unified 契约、clean 判题），评测跑在既有远程 GPU 评测箱（run-dir 全部在 `/root/autodl-tmp/`）。
- 阈值 θ=0.5 起步，需用自有标注校准；社区经验区间 0.3–0.5 仅参考。
- 本地开源近似（Open-Jev 等）后接，单列评测分支，不与 TypeSafe 分数混报。
- pinned 条目语义沿用现有引擎约定（trigger 命中即注入）。
