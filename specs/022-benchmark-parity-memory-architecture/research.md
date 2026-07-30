# Phase 0 Research: 双基准查询期证据编译架构

**Feature**: 022-benchmark-parity-memory-architecture

**Date**: 2026-07-30

**Status**: Complete — no unresolved items

本文把 [spec.md](./spec.md) 和仓库已有研究记录转化为可实施决策。论文事实与引用不在
此处复制，统一由 root 工作区并行整理的
`docs/research/high-scoring-memory-systems.md` 登记；合并 022 前该文件必须先提交或一并
纳入，不能丢失其 alphaXiv 来源链。

## R1 — 先建因果尺子，再改表示或检索

**Decision**: 阶段 0 分成两个互不混用的基线：

- **B0 continuity**：在 lossless ingestion 上刷新当前默认路径的 LoCoMo category 1–4
  和 LongMemEval-S full 500 产品回归基线。
- **B1 causal ruler**：冻结 ranked anchor candidates、真实 tokenizer、answerer/judge、
  prompt 和两个预注册 token-cap profile，再用 legacy packer 重跑。022 的机制 A/B
  只与 B1 比较。

低预算 profile 用约 1.1k input token 量级，高预算 profile 用约 3.6k 量级；正式 manifest
冻结精确整数，不能看完 treatment 结果再选。两个 profile 都在两个 benchmark 上运行。

**Rationale**: 旧 LongMemEval 80.80% 来自截断超长 turn 的 store，不能继续作为因果
基线；既有 budget ablation 又证明 top-k 同时改变候选与上下文。B0 保持历史连续性，
B1 才能隔离 representation/compiler。

**Alternatives considered**:

- 直接沿用旧 LongMemEval 基线：拒绝，违反 FR-001 和 lossless 纠偏。
- 只跑一个 cap：拒绝，压缩收益本身是 budget-dependent。
- 用 top-k 代替 token cap：拒绝，不能证明预算相同。

## R2 — 021 IRIS 是独立 diagnostic，不是 022 实现基座

**Decision**: 021 必须先在自己的 branch/worktree 收口、提交或明确暂停。022 不复制
`iris.go`，不依赖 `irisRetrieve`，正式 arm 强制关闭 `--iris` 和 legacy IDK retry。
022 的补检只能由结构化 `entity | time_range | second_operand` gap 触发一次；021 的自然
语言 missing、最多三轮和先答后重试语义不得进入 022。

**Rationale**: root 当前 021 正在修改 `cmd/locomo-bench/main.go`，并新增
`iris.go/iris_test.go`；它与 022 在接线文件、轮次和 answerer 调用数上直接冲突。先隔离
才能保护另一 agent 的工作并保持实验可归因。

**Alternatives considered**:

- 在 022 worktree 覆盖 021 dirty 文件：拒绝，违反并行 feature 隔离。
- 复用 021 多轮闭环后把深度改成 1：拒绝，它仍保留不同的充分性和 IDK-answer 语义。

## R3 — Evidence Ledger 独立于可变的 `memory_entries`

**Decision**: 新 migration 建立独立的 message-level Evidence Ledger。Evidence 的规范
原文不存入 `memory_entries`；`memory_entries` 继续是现有 Atomic Fact/search projection
payload。新增 projection registry 和 projection→Evidence 直连 lineage，Semantic
Episode 使用独立 payload。

所有 projection 必须直接引用其全部 Evidence，不允许只保存 projection→projection
间接链。Raw-turn window 在查询/实验期从 Ledger 邻接消息组装，不另造一份规范原文。

**Rationale**: `memory_entries` 当前允许 upsert、curation merge 和物理 delete，天然不是
append-only truth。把 raw turn、episode 与 fact 无区分塞入同一默认 RRF，还会污染
表示 bake-off。独立 Ledger 允许 projection 丢弃重建，并让 purge 的依赖闭包保持一跳。

**Alternatives considered**:

- 给 `memory_entries` 加一个 kind 后同时充当 Ledger：拒绝，原文仍会被现有生命周期
  改写或删除。
- 为每种未来 memory space 预建专表和图：拒绝，Event/Scene/Profile/graph 尚未过门。
- 只保留 session ID：拒绝，无法验证 message-level source、span 或 multi-source fact。

## R4 — Evidence 身份、幂等和旧数据

**Decision**: Engine 为 Evidence 分配不可变 ULID；调用方可提供 `external_source_id`
作为幂等键，它只在同一个 `source_type + source_session_id` 范围唯一。消息顺序使用
显式 `ordinal`，不能从 ID 排序推断。

Migration 对既有 entry 生成确定性的 `legacy_entry` Evidence snapshot 和 self lineage，
明确标记它没有 message-level provenance。新直接写入在同一事务创建 `direct_write`
Evidence；同名同内容重试复用当前 Evidence，同名改内容则 append 新 Evidence，不覆盖
旧内容。

**Rationale**: 外部 turn ID 在不同 session/namespace 可重复；engine ID 必须稳定且
namespace 由独立 DB 自然隔离。Legacy snapshot 诚实保住现有数据，同时不伪造 turn 级
来源。

**Alternatives considered**:

- 以 entry name 作为 Evidence ID：拒绝，name 可 upsert，不能表示历史版本。
- 强制所有旧数据重摄入：拒绝，不可行且会破坏离线升级。
- 把 legacy fact 的 session 当成精确消息：拒绝，会制造虚假 provenance。

## R5 — 抽取必须返回实际 source IDs

**Decision**: pipeline 在调用 extractor 前先持久化原始 messages；抽取 prompt 给每条消息
稳定 Evidence ID，输出的每条 fact 必须列出实际支持的 `source_ids`。解析器拒绝未知、
跨 batch、tombstoned 或空 source set 的新 fact。事实落库和 lineage 在同一事务完成；
模型/解析失败只损失 projection，不损失 Ledger。

现有 curation merge 必须把来源事实的 Evidence 并集直连到新 projection；不得把 merge
文本自身写成无来源 Evidence。

**Rationale**: 将整 session 粗暴挂给每条 fact 会使 citation 和 purge 闭包失真。先写
Ledger 能保证可选抽取失败时原文仍在。

**Alternatives considered**:

- 所有输入消息都算每条 fact 的来源：拒绝，过度声明支持关系。
- 继续只记录 `source_session_id`：拒绝，不满足 source recovery 和 span 校验。
- 允许 extractor 创建新 Evidence：拒绝，混淆 observation 与 generation。

## R6 — Tombstone、restore 与 purge 使用不同语义

**Decision**:

- `TombstoneEvidence` 追加 lifecycle event，把 head 设为 tombstoned，并令已经没有其他
  active support 的 projection 不可服务。
- `RestoreEvidence` 追加 restore event；raw turn 可立即恢复，派生 projection 标为 stale，
  经 builder 重建后才重新 active。
- `PurgeEvidence` 在一个事务内删除原文、可恢复状态及所有直接依赖 projection；保留的
  audit 只有 evidence ID、source type、action 和时间，不含 content/speaker/session。
- Purge 使用 SQLite secure-delete，并在提交后执行 WAL truncate checkpoint；checkpoint
  未完成返回可重试的 `ErrPurgeIncomplete`，不得把普通 DELETE 误报为已清除可恢复副本。
- 现有低层 `EntryStore.Delete` 仍只删除 projection；高层 Evidence API 承担上述生命周期。

**Rationale**: 普通误删需要恢复，隐私 purge 必须防止派生文本继续泄露。所有 projection
直连 Evidence 后，保守删除一跳依赖即可覆盖全部派生内容。

**Alternatives considered**:

- purge 后保留带 redact 的原始行：拒绝，仍可能泄露长度或残片。
- 仅删除 Evidence、不处理 projection：拒绝，派生文本仍含敏感内容。
- tombstone 时物理删 projection：拒绝，增加不必要的恢复成本。

Purge 的保证只覆盖 engine 管理的当前 SQLite DB、WAL 和 projection；外部备份、历史
export 或调用方复制品不在 API 可验证范围，文档必须诚实说明。

## R7 — Semantic Episode 只决定边界，不生成第二份事实

**Decision**: Semantic Episode 是同 session 内连续 Evidence 的有序分组；V1 narrative
按 source 原文和 speaker 标记确定性拼接，不生成 summary。边界可由可替换本地
segmenter 提供，输出仅是 source ID ranges；失败时该 view 不生成，raw-turn/fact 路径
继续工作。

Navigation bake-off 为三种表示分别建立 run-dir 下可删除的 shadow index，使用同一
检索算法、embedding、pool size 和 candidate budget。Shadow index 是评测资产，不是
产品第二真相；只有过门的表示才进入后续默认集成。

**Rationale**: 这样把 segmentation 与 synthesis 分开，Episode 可以逐字回到 Ledger，
并避免在实验前建设通用 Scene 层。

**Alternatives considered**:

- 让模型直接写 episode summary：拒绝，同时改变边界和文本，无法归因。
- 固定 token/message 数当作 semantic episode：拒绝，正是待比较的 baseline。
- 未过门就把三种索引都永久写入产品 DB：拒绝，增加维护和默认污染。

## R8 — 表示实验的“同候选”使用 ranked anchor artifact

**Decision**: 表示阶段冻结同一 ranked anchor-candidate artifact，而不是要求不同
projection 拥有同一 entry ID/text。每个 anchor 保存稳定 candidate ID、rank、score、
全部 lineage source IDs 和 text digest。

三个 renderer 可围绕同一 anchor 展开不同 source closure；这正是 treatment。逐题必须
记录 rendered source IDs、扩展数、gold-source visibility 和 token 裁剪。Navigation
bake-off 与 answer-facing rendering bake-off 分开报告。Compiler 阶段则在选定表示后，
冻结完整 rendered candidate objects，所有 compiler arm 逐字节 replay。

**Rationale**: raw window 和 semantic episode 的候选单元必然不同；强行要求 rendered
source set 相同会裁掉要测的邻接/边界价值。Anchor 相同才能控制检索起点，展开差异才能
成为显式 treatment。

**Alternatives considered**:

- 三种表示各自检索后直接比较答案：拒绝，ranking 与 rendering 混杂。
- 要求三臂 rendered source IDs 完全相同：拒绝，会使 raw window/episode 退化。
- Compiler 每个 arm 重新 retrieve：拒绝，不能证明 fixed-candidate。

## R9 — Compiler 是检索后的独立 engine 子包

**Decision**: 新建 `memory/evidencecompiler` 公共子包。它接收已冻结 `[]Candidate`，不拥有
Retriever、不调用 answerer、不知道 benchmark category。Source recovery 通过只含
`Resolve(ids)` 的窄接口完成，该接口没有 Search/query 方法。

现有 `Retriever.Search` 签名和默认语义保持不变；Result 可加稳定 projection ID/kind 等
向后兼容字段，但 source lineage 由批量 resolver 获取。

**Rationale**: 独立边界能结构性保证 fixed-candidate 和 `FETCH_SOURCE` 不偷做新检索，
也让 MCP/CLI 的旧平面 Search 在 Compiler 不可用时继续工作。

**Alternatives considered**:

- 让 `Search` 直接返回 Evidence Bundle：拒绝，会把 retrieval、planning 和 answer
  contract 混成一次不可消融改动。
- 把 Compiler 全放在 `cmd/locomo-bench`：拒绝，产品集成无法复用，违反引擎/适配层分离。

## R10 — Action 是封闭 union，offset 使用 Unicode code-point

**Decision**: V1 action 只允许 KEEP、EXTRACT、DROP、MERGE、FETCH_SOURCE。SourceSpan
使用规范原文上的 Unicode code-point `[start,end)`，保存 source ID 与 span digest。
未知 action、越界、非法 UTF-8、越 lineage source、tombstoned/purged source 均拒绝。

MERGE 输出由逐句 `GroundedSentence` 组成，每句至少一个有效 SourceSpan；deterministic
fallback 从不生成 MERGE。MERGE 默认关闭，只有规范原文经实际 tokenizer 证明装不下，
且有来源的 EXTRACT 仍不能满足全部 Evidence Need 时，Planner 才可提出。Projection
文本只有 lineage 不足以直接 KEEP：必须回收原文或证明它是可复原的 extractive
candidate。V1 没有 ADD。

**Rationale**: Code-point offset 对中英文一致且可在纯 Go 中稳定复原；封闭 union 让
模型 proposal 始终经过 engine 验证。

**Alternatives considered**:

- byte offset：拒绝，跨语言易切断 UTF-8。
- 自由文本 citation：拒绝，无法机器验证。
- 复用 bench-local turn ID 当 span：拒绝，只能证明消息级来源，不能证明引用片段。

## R11 — 精确 token 由 answerer integration 注入

**Decision**: `TokenCounter` 是 provider-neutral interface，输入即将真实发送的 model、
system 和最终 user message，输出 input token 与 fingerprint。Compiler 每次选择后对完整
prompt 重计，禁止把 item token 数相加。Fingerprint 包含 model/tokenizer revision、
chat template 和 special-token policy。

正式 recipe 先用 CJK、emoji、长数字和 system/user 边界校准集，对比 counter 与同一
本地 answerer runtime 自报 usage，要求每例差值为 0。Counter nil、失败或 fingerprint
变化时 compiled-answer fail-closed，answerer 调用为 0；基础 Search 仍可用。

**Rationale**: 当前 `provider.Usage` 只能在模型调用后得到，`strings.Fields`、字符数和
固定 tiktoken 都不能对任意本地模型执行调用前硬门。

**Alternatives considered**:

- 扩 `provider.Provider.CountTokens`：拒绝，不是所有 provider 都有预检能力，且会破坏
  全部 provider 实现。
- 使用调用后 usage：拒绝，已经越过硬门并消耗了一次 answerer。
- 在 engine 硬编码 Qwen tokenizer：拒绝，违反模型可替换性。

## R12 — Planner 可降级，grounding/counter 不可伪降级

**Decision**: 可选本地 1.5B–4B model 只实现 `Planner`，提出 Need/actions；engine 负责
lineage、offset、citation 和 token 验证。Planner nil、超时、malformed 或越权输出时，
Trace 记录原因并退到 deterministic extractive compiler。Context cancellation 直接传播。

Resolver 的结构性存储错误或 TokenCounter 错误导致 Compile 失败，不得伪装成 planner
降级。Deterministic fallback 在 raw fits 时保持原文；只有超 cap 才做 sentence/span
extraction，并按 need coverage、词法重合、原 rank、source ID 固定顺序。

**Rationale**: 模型策略可以不可靠，但来源和预算是安全不变量。二者必须有不同错误语义。

**Alternatives considered**:

- 模型直接生成最终 Bundle：拒绝，无法阻止无来源 ADD 或越界引用。
- Planner 失败回 legacy count packer：拒绝，会让同一个实验臂静默改变预算语义。
- Counter 失败用字符估算：拒绝，会把 invalid run 混入正式结果。

## R13 — Miss attribution 使用 source survival 的互斥分类

**Decision**: 对有 parseable gold source 的题依次分类：

1. `gold_unresolved`：dataset gold 无法映射到 Ledger；
2. `candidate_miss`：冻结 candidate lineage 未覆盖全部 required gold sources；
3. `compiler_miss`：candidate coverage=1，但 Bundle coverage<1；
4. `answerer_miss`：candidate/bundle coverage=1，但 majority answer 错；
5. `success`：最终答对。

所有题仍留在 benchmark accuracy 分母；`gold_unresolved` 只从 miss-rate 分母排除。
指标名称必须写作 **gold-source survival**，不能声称 source ID 等于 answer-span
visibility。

**Rationale**: LoCoMo/LongMemEval gold 多为 turn ID，不是答案 span。互斥分桶才能避免
把召回、编译和作答错误互相甩锅。

**Alternatives considered**:

- 继续用 session-level recall：拒绝，明显高估证据可见性。
- 用 fact lexical containment 当唯一 gold：拒绝，只是近似 attribution。
- 所有“gold in top-k but wrong”都叫 compiler miss：拒绝，会吞掉 answerer 失败。

## R14 — 统一 stop/go、artifact 和提交隔离

**Decision**: 每阶段先冻结一个 primary cohort 和 primary arm。Promotion 统一要求：

- artifact/lineage/cap/answer-call validity 100%；
- 目标 cohort majority accuracy 相对 control `Δ ≥ +2.0pp`；
- two-sided exact McNemar `p < 0.05`；
- 另一 benchmark overall `Δ ≥ -0.5pp` 且无显著负向；
- 任一预注册 category 不得出现经多重比较校正后的显著负向；
- candidate/gold-source coverage 不退。

`0 < Δ < 2pp` 或不显著为 HOLD；`Δ ≤ 0`、显著伤害另一 benchmark 或任一 validity
hard gate 失败为 STOP/invalid。Event、gap、Scene、Profile、graph 使用各自预注册 residual
cohort，不事后合并小桶凑显著。

评测产物使用 `022.v1` schema：`protocol.json`、`candidates.jsonl`、
`compile_trace.jsonl`、`bundles.jsonl`、`classification.jsonl` 和 `summary.json`。
Resume 时 protocol/candidate/tokenizer/cap hash 任一变化即拒绝。评测尺子、算法、配置、
结果/verdict 分开提交。

Stage 0 同时冻结 judge audit 抽样规则：全部 control/treatment judge-discordant 题，加上按
benchmark/category/label 分层的 concordant 样本，由不知道 arm 的两位审阅者独立标注并
裁决。分类结果同时报告 raw/corrected score、FN/FP 方向和一致率；审计未完成、审阅一致率
不足或校正会改变 verdict 时只能 HOLD/INVALID，不能 GO。

**Rationale**: 当前 journal 没有 candidate ID 和 bundle token，`regime.json` 也没有冻结
足够字段；现有大样本 McNemar 走近似分支，不能满足论文级 exact 配对声明。

**Alternatives considered**:

- 只看 overall 点估计：拒绝，无法区分噪声、类别伤害和预算效应。
- 任一 benchmark 涨点就默认：拒绝，违反双基准共同过门。
- run 后再选 cohort/cap：拒绝，形成选择偏差。

## R15 — 文献审计提供诊断维度，不提供通用经验阈值

**Decision**:

- Stage 0 连续记录每题/每 cohort 的 all-required-source coverage，并按 protocol
  预注册 coverage strata；不能把外部论文的两个观测硬编码成产品阈值。
- 现有 `candidate_miss → compiler_miss → answerer_miss` 互斥归因保留；另跑固定 gold
  evidence/oracle diagnostic 测量 answerer ceiling，但它只作诊断，不进入机制分数。
- Judge 噪声审计与 category non-regression 成为 promotion gate。
- Retain or Consolidate 的 LongMemEval loose-budget Merge 正确结果登记为
  `−0.107 [−0.204, −0.013]`；它支持 MERGE 双条件默认关闭。

**Rationale**: alphaXiv 对原论文复核显示，LazyMem 的 All@50 是 LongMemEval 0.990、
LoCoMo 0.889，但论文没有验证通用 `recall >= 0.95` 决策线，并明确把 crossover 归因于
relative evidence-fit pressure。另一方面，LoCoMo judge audit 显示强方向性 false negative；
小幅机制收益若不做人审抽样，可能只是在移动 judge 噪声。

**Alternatives considered**:

- 将 `0.95` 写成 Compiler 启用硬阈值：拒绝，仅两个跨数据集观测，且会把外部配置偶然性
  固化进产品。
- 只报告 overall exact McNemar：拒绝，可能掩盖 temporal/update 等类别崩溃。
- 依赖第二个 LLM judge 代替人审：拒绝，只是换一套未知偏差，不能校准 FN/FP 方向。
- 把 oracle diagnostic 算入正式 accuracy：拒绝，它使用不可部署的 gold evidence。

## R16 — 有效 B1 必须在 Ledger source-chain 后生成

**Decision**: 2026-07-30 的 live formal-runner 验证发现，v6 的 Atomic Fact hit 只有 entry
name/session，缺少直接 raw Evidence lineage 与可复原 span。正式 runner 对这类 hit 标记
`source_lineage_unavailable`，零 answer/judge call，并将整次运行标记 INVALID。保留
calibration、B0 continuity 和 B1 protocol template，但把正式 B1 control 的 candidate freeze、
legacy-packer run、oracle 与 judge audit 移到 Increment 1 的 source-chain gate 之后。

**Rationale**: 022 的 B1 不只是一个相同 cap 的分数；它还是后续 Compiler 的 answer-facing
evidence contract。若把 `legacy-entry:*`、session ID 或 raw-chunk quota 当作 fact 的来源，
会伪造 source/span validity，并把没有证据的题混入可比较 accuracy。Ledger-first B1 仍保持
control 使用 legacy packer，故表示/Compiler 的 treatment 变量没有提前进入 control。

**Alternatives considered**:

- 接受 synthetic `legacy-entry:*`：拒绝，违反 FR-006、FR-009、FR-014 和 fail-closed
  compiler contract。
- 只用 raw-turn candidates 立即跑 B1：拒绝，它改变了 current-product candidate composition；
  若要作为独立 raw-turn baseline，必须另行 specify，不能替代 022 B1。
- 把 INVALID run 记为零分或部分分：拒绝，validity failure 不是模型错误，计分会误导
  baseline 和后续 paired statistics。

## Unresolved Items

无。Product 默认采用哪一种 representation、是否进入 Event/Scene/Profile/graph 由上述
预注册实验产生的 GO/HOLD/STOP verdict 决定；这是有意的条件分支，不是技术未知项。
