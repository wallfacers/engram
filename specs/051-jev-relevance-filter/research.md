# Research: 051-jev-relevance-filter（Phase 0）

全部 NEEDS CLARIFICATION 在此解决。每项：Decision / Rationale / Alternatives considered。

## R1. 过滤器抽象形态：`filter/` 新包，返回概率而非硬切

- **Decision**: 新建引擎包 `filter/`，`RelevanceFilter.Filter(ctx, query, []Candidate) → ([]float64 /*p*/, FilterMeta, error)` 返回**校准概率**（与输入对齐）；阈值/两段式/k_show_max/pinned 旁路等策略收敛在纯函数 `filter.Select(probs, cands, Policy) → []int`。`memory` 单向依赖 `filter`，`filter` 不依赖任何 store 类型（`Candidate{ID,Name,Text,Trigger,Pinned,Score}` 承载所需全部字段——`memory.Entry` 已实证有 `Trigger`/`Pinned`）。
- **Rationale**: 打分（模型）与取舍（策略）分离，策略可离线单测、可校准调参不改适配器；返回概率保住 Jev 的核心价值（校准过、按阈值留，不死切 top-k）；无 import cycle（范式同 `embedding.Reranker`：签名只用中性类型）。
- **Alternatives considered**: ① 仿 `embedding.Reranker` 直接收 `[]string`——被否：Jev pointer 模式需要 name/trigger 元数据构造条件化问题；② 接口直接返回 keep 列表——被否：策略被锁进适配器，θ 调参要改实现；③ 接口放 `memory/` 内——被否：实现包会回引 `memory` 成 cycle。

## R2. Jev 客户端：自包含 `net/http`，pointer 模式协议

- **Decision**: `filter/jev/` 自包含 HTTP 客户端（`BaseURL/Model/APIKey/Timeout` 配置，对标 `embedding.NewReranker` 的 `(nil, nil)` typed-nil 纪律）。请求形状按设计文档 §21.4：共享 `state{query, memories:{id:{name,text}}}` + 每记忆一道 `noul` 题（"Memory X is necessary to answer or act on the query. The query is about ..."，指令写「对当前这个问题是否必要」而非「这条重不重要」）；响应取每题概率。BaseURL 可指向 TypeSafe 或 OpenRouter 转售端点。
- **Rationale**: 无 SDK 依赖 = 核心路径不 import 死绑 TypeSafe（FR-004）；pointer 模式避免 150 条全文复制进 150 道题（成本从天价回到 ~$0.0004/请求）。
- **Alternatives considered**: ① 官方 SDK——被否：引入厂商绑定 + 依赖膨胀；② inline 全文模式——被否：输出 token 成本爆炸（§21.4 明令禁止）。

## R3. 阈值策略：两段式，集中默认 0.5 → 0.35(≤3) → 诚实空

- **Decision**: `Policy{Theta: 0.5, RelaxTheta: 0.35, RelaxMax: 3, KShowMax: 12}`；全灭（无 p ≥ θ）时放宽一轮取最多 3 条，仍全灭返回空。pinned 且 trigger 命中条目旁路注入（不计 dropped）。θ 等参数可经 `ENGRAM_JEV_THETA`/`ENGRAM_JEV_RELAX_THETA`/`ENGRAM_JEV_KSHOW_MAX` 覆盖（评测/校准用）。
- **Rationale**: spec clarify Q5 拍板选项 A；兼顾「不硬塞」与「该查不空手」（§21.2）；0.3–0.5 为社区稳态区间，0.5 起步、自有标注校准。
- **Alternatives considered**: ① 一律诚实空——被否：「该查却空」风险（问包管理器时 pnpm 条目 0.45 会被误杀）；② 调用方提示位控制——推迟（读前门 FR-009 Deferred）。
- **互审修订（R3a）**: θ/RelaxTheta/KShowMax/池档在门限跑前**预注册**冻结进 run manifest，任何 sweep 仅诊断、不得作为报告门限结果（防 in-sample 调参，对标 entity-verify 教训）；放宽段加 `ENGRAM_JEV_RELAX=0` kill-switch，评测含 D-noRelax 变体供 SC-004 测量（不推翻 clarify Q5 默认）。MCP `candidate_pool` 缺省 = limit（parity-safe）；设计 §20.4「默认 64」是生产推荐档而非隐式默认（刻意偏离，记录于 tasks/analyze 阶段）。

## R4. 分片并发与失败语义

- **Decision**: 池 >64 条按 32–64/片切分，goroutine 并发（对齐 AGENTS.md「模型侧阶段必须并行」硬规则精神），结果按 index 合并；**任一片失败/超时 → 整次过滤 `Degraded=true`，调用方退回 RRF 前 `limit` 条**（不部分采用，防止半份概率造成不一致短名单）。单次失败不熔断、不累计成永久降级。
- **Rationale**: 150 条 ≈ 3–5 片，并发后 p50 仍 ~0.3s 量级；部分失败时的半份打分比降级更危险（SC-005 要求降级准确率 ≥ A 臂）。
- **Alternatives considered**: ① 串行分片——被否：延迟线性上涨（AGENTS.md 042 教训）；② 部分采用已返回片——被否：短名单不可复现、空注率统计失真。
- **互审修订（R4a）**: 整过滤 deadline 默认 1s（ctx 派生、可配），单请求超时取 min(剩余,1s)；不重试（降级臂确定性，护 SC-005）；失败后 30s 同桶负缓存直接降级（防逐轮打挂掉端点的永久税；非熔断，到期自动重试）。

## R5. 计费与遥测

- **Decision**: `FilterMeta{Backend, Theta, Kept, Dropped, LatencyMs, CostUSD, Degraded}`；CostUSD 由响应 usage（input tokens × 单价，输出免费）计算，单价为客户端配置常量（文档注明随价目表更新）；元数据随 `memory_search` 结果 `filter` 字段返回，评测 harness 另行分段落盘（宽池检索/Jev/答题各自延迟与 token）。
- **Rationale**: §3.3「分项记账」与 SC-007 要求；adapter 侧只报结构性事实（无 usage 字段时 CostUSD=0 且注记 unknown，不编数）。
- **Alternatives considered**: 写库记 usagelog——被否：无 schema 变更约束（存储层不动），且评测侧落文件已够。

## R6. 写前门放置：引擎 `filter.WriteGate` + MCP opt-in 接线 + skill 调用

- **Decision**: `filter.WriteGate.Gate(ctx, userTurn, draft) → GateDecision{Route: write|skip|defer_to_packet, Reasons: {is_durable,is_preference,is_secret,needs_probe}, Meta}`，Jev 适配器同客户端实现（§18.1 四题，经 clarify Q4 修订：`needs_probe` 本期恒不启用、`defer_to_packet` 仅占位）。MCP `memory_write` 经 `ENGRAM_JEV_WRITE_GATE=1` opt-in 接线；门失败/无 key → 本次视为无门（现行 skill 规则照跑，**不得因门失败拦截写入**）。密钥判定带例外：用户明确要求记录自己的 key（HF/DeepSeek 等）→ `write`。决策出口预留 `defer_to_packet` 路由位与未来短包/整理员线兼容（clarify Q4：两者不得冲突）。
- **Rationale**: 「读写都需要」（clarify Q3）且 A+B 双落地；fail-open 保住路径 A 当轮写入契约；路由占位满足「与微软线不冲突」。
- **Alternatives considered**: ① 纯 skill 侧实现——保留为 B 路径（不经 MCP 参数的宿主本地调用同样支持，接口同源）；② 本期就启用 needs_probe 路由——被否：短包线未交付，拦下无处可去（clarify Q4）。
- **互审修订（R6a，BLOCKER 修复）**: 传输边界——密钥形状草稿先本地预判短路（明确要求→本地 write；未要求→本地 skip），**不发云**；发云内容先遮蔽密钥形状子串；写门调用超时 500ms fail-open；仅手动 `memory_write` 门控；AGENTS.md 修订条款须点名写门路径（FR-007 已同步）。
- **互审修订（R6b，F5 修复）**: 写门输入映射——`draft = memory_write.content`；`memory_write` +可选 `user_turn`（缺省空→draft-only 判定）+可选 `user_requested`（缺省 false；true 时密钥形状内容本地直接 write）。

## R7. 评测口径与落账

- **Decision**: 四臂 A/B/C/D（§23）在 `cmd/locomo-bench` 以 recipe 扩展实现，全臂同一 unified answer contract、clean judge、同 store、3-rep 多数；必测数据集 LoCoMo 1,540（LME-S 推迟，clarify Q2）。指标：准确率、pool recall@150、recall@shown、短名单精确率（沿用已有 evidence/gold 标签，缺标注的题集线性外推不作数）、空注率（gold 缺失题）、分段延迟/USD/token、B↔D 翻错题双向列题号。结果落 `docs/evaluation/results.md` + `experiment-verdicts.md`，TypeSafe 与未来本地方案分支分开报告；所有模型侧阶段走 worker pool 尊重 `--concurrency`（硬规则）。eval 配置改动独立提交。
- **Rationale**: Constitution IV + §23 六项成功标准 + 仓库既有纪律（clean 口径、HOLD、翻错题不许只报净涨）。
- **Alternatives considered**: ① 独立新 harness——被否：复用 locomo-bench 的 store/judge/合同栈才能同口径配对；② LME-S 同跑——clarify 拍板推迟（抽取成本）。
- **互审修订（R7a）**: 按 038 正式协议机器执行：四份冻结 manifest + 新机制键登记（eval_runner 机制绑定）+ intended variable 按对比对重声明（A↔B=预算，B↔D=过滤器）+ 上下文字节 parity 门的替代声明；category-5 以 declared block 纳入 C/D（refusal-scoring、总体声明）；SC-003 等预算（C@12 门 / C@8 参考）；run 内配对 + 精确 McNemar + 配对 CI（paired_eval.go），结论归属 promotionVerdictFor 需显式声明；门限跑前先 `--estimate` 成本预估（AutoDL 纪律）；Jev 模型钉具体版本进 manifest（禁 `jev-latest` 浮标）。（R7c，Slice-4 仲裁）实际接线缝在 chunks.go（非 multiquery.go，后者是 recall 诊断模块）——以实码为准；FilterMeta 增 `InputTokens/OutputTokens`（usage 透传，SC-007 token 分段计量与 jev_call_journal 的 filter tokens 据此，切片 5 落地）；`--jev-arms` 要求 `--no-idk-retry`（IDK 重试会破坏配对，属对 unified contract 的声明性偏离，**门限跑前需维护者签核**）；038 产物（candidate/trace/bundle/…）由 038 runner 先行产出到同 run-dir，本 harness 消费+校验（缺失即 HOLD），不复刻其产出器；SC-005 对照 = A 与 C@8 双口径，不一致即 HOLD；门限/降级两 pass 合并 verdict（registration digest 一致）方可晋升。
- **互审修订（R7b，F3/F4/F11 修复）**: 四臂统一冻结 `--answer-input-cap=32768`，B 臂**无截断硬断言**（bundle ≤ cap 且 packer 未触顶，触顶则该 run 无效）；SC-002 token 比以 packer 实际注入 token 计、B 臂 reported shown = packer 实收数。Jev 调用不占 `RetrievalCallLimit`（非检索调用），以独立 `filter` 调用类进逐行 audit 并记 usage/cost（SC-007）；manifest 新增 filter 角色字段 `filter_model` 承载 Jev 钉版，机制键 `filter.jev.v1`（协议哈希扩展影响已声明）。038 合法性工件具名：formal call journal（逐行 provider-call audit）、`evalArtifactValidity.isComplete`（逐 rep 校验回执）、B0 continuity receipts、pilot 门 + warm-up 处置、3-rep 同窗执行。promotionVerdictFor：SC-001..007 总判定经 HOLD 进入 `promotionVerdictFor`；SC-005 用配对 A↔C 显著性（McNemar + 配对 CI）。
