# Feature Specification: 预算对齐下的真实超越 — IRIS 证据缺口迭代检索

**Feature Branch**: `021-iris-evidence-gap-retrieval`

**Created**: 2026-07-29

**Status**: Draft

**Input**: 维护者「真实超越」方向。前置证据链：(1) [budget-ablation](../../docs/evaluation/reports/budget-ablation.md) 证明 engram 对 MemOS 的 +3.20pp 同栈领先**完全由上下文预算驱动**——对齐 MemOS 预算（tk7, ~1059 tok）后反转为 −5.62pp（p=0.000006），短板集中在 multi-hop（−10.99pp）与 temporal（−9.35pp）。(2) 前置测量 A（[experiment-verdicts](../../docs/evaluation/experiment-verdicts.md) 021 行）证明**低预算下复用 003 `--assoc` graph 仍显著伤 temporal（−7.17pp, p=0.011）**——008 铁律（结构化检索不转化 + 伤 temporal）在低预算下依然成立。本特性用 **IRIS（Evidence-Gap-Driven Iterative Retrieval，EviMem, arXiv 2604.27695）** 在 MemOS 对齐预算下追求 multi-hop/temporal 的真实翻正。EviMem 在同一个 LoCoMo、同样的 temporal/multi-hop 类别上已证明：temporal 58.8%→81.6%、multi-hop 81.4%→85.2%（vs single-pass），且方法 model-agnostic（可用本地模型）。

## 背景与定位

本特性是**评测杠杆探索特性**，"用户" = 维护者。它触及**检索 + 答题口径** → 宪法 IV 评测回归门适用：任何被采纳的杠杆必须先过单变量端到端 A/B（配对 McNemar），eval 结果单独提交、声明口径。**引擎零改**（宪法 II）：全部改动在 `cmd/locomo-bench/`，复用引擎现有检索/抽取/reranker 接口与 harness 已有的 IDK-rewrite 迭代骨架。

**核心洞察（A 锐化）= category-conditional**：engram 当前的单次 retrieve-then-reason + 扁平 RRF 在 temporal/multi-hop 上结构性失败（证据散布在多 session、无表面关键词重叠）。但解法**不是**无脑加结构化检索（A 已证 graph 伤 temporal）。EviMem 的提升来源是**迭代证据缺口闭环**：检索 → 评估证据是否充分 → 诊断缺什么 → 针对性 refine query → 再检索，且对 temporal 有专门策略。因此：
- **temporal 杠杆 = IRIS 迭代闭环 + temporal-aware answering**（**不用 graph**）。
- **multi-hop 杠杆 = IRIS 迭代闭环 + graph 扩展**（仅 multi-hop 启用 003 `--assoc`，作 LaceMem Edge 层）。

**死规则（硬，非协商）**：
1. 纯本地 / 自托管模型（Qwen3.6-35B 答题与评估、bge-large embedding）；严禁任何付费云 rerank / 评估模型作涨分手段（宪法 I/V，[[no-paid-rerank-lever]]）。
2. 上下文预算必须保持 MemOS 对齐（answer_context_tokens_mean ≈ 1059，tk7）——"真实超越"的定义就是同预算下翻正，不得靠堆预算取胜（[[lever-philosophy-signal-not-volume]]）。
3. engine 零改：`git diff --name-only -- memory embedding provider store internal` 必须为空。

## 核心机制（IRIS 闭环，harness 层）

harness 已有一条三阶段 IDK-rewrite 迭代链（`retryWithRewrite` → `retryWithWiderNet`，被动 IDK 触发 + 盲改写，`--no-idk-retry` 可关）。IRIS 把它从**被动**升级为**主动**：

1. **检索**：复用 `hybrid`（US2 在 multi-hop 类叠加 `--assoc` graph 扩展）。
2. **EvalSufficiency（新组件，本地 Qwen）**：检索后**主动**评估累积证据是否足以回答问题，输出三级 tier（`EXACT` / `INFERRABLE` / `PARTIAL`）+ 自然语言缺口诊断 `m`（"还缺什么"）。这是相对现有链的核心增量——不等答题 IDK 才发现不足。
3. **终止 / 答题**：tier 充分（EXACT/INFERRABLE 且置信度过阈）→ 答题；否则进入下一步。temporal 类用更严阈值 + 现有 `--temporal-answer-prompt` 时序契约。
4. **Diagnosis-driven refine（升级现有 rewriteCall）**：用缺口诊断 `m` 驱动 query 改写（而非盲改写），双路检索（原问题 + 改写），累积证据。最多 k 轮（k=2~3）。

契约正本见 [contracts/iris-loop.md](contracts/iris-loop.md)（EvalSufficiency 的输入/输出 schema、refine 策略、终止阈值）。

## User Scenarios & Testing *(mandatory)*

### User Story 1 — IRIS MVP：temporal 类证据缺口迭代验证 (Priority: P1) 🏆

在 harness 现有 IDK-rewrite 链上插入 **EvalSufficiency + diagnosis-driven refine + 多轮**（temporal 类**不**启用 graph），以 `--only-category 2` 单变量 A/B 验证：IRIS 主动迭代能否把 temporal 从被 MemOS 碾压（tk7 baseline temporal 73.21%）救起。这是 MVP——它验证 C 的核心假设（IRIS 迭代是 EviMem temporal +23pp 的真实来源，且在 engram 栈上可迁移），是 US2/US3 的前置。

**Why this priority**：temporal 是同预算下被碾压最狠、且被 A 证明 graph 救不了的类别；IRIS 迭代是学术界针对"单次检索在 temporal 失败"的明确解法。过闸 = 核心假设成立、拿到 temporal 翻正路径；不过闸 = 便宜证伪、STOP（低成本止损，不扩展 US2/US3）。

**Independent Test**：固定 009-bge-chunks-store + Qwen3.6-35B 本地 vllm + deepseek-v4-flash judge + `--force-answer` + tk7（chunk-quota 3）+ 3-run majority；`--only-category 2` 跑 `hybrid`（baseline，已有本地产物）vs `hybrid+iris`（新臂），1529 配对中 temporal 子集（n=321）配对 exact McNemar。对照预算：两臂 `answer_context_tokens_mean` 都须 ≈1059（IRIS 迭代多检索但每轮仍 tk7 截断，总送答题器的上下文须对齐）。

**Acceptance Scenarios**：
1. **Given** IRIS 闭环（EvalSufficiency + diagnosis-refine，temporal 不用 graph）就绪，**When** `--only-category 2` 同栈同预算跑 baseline vs iris，**Then** 报 temporal 旧→新% + 配对 exact McNemar（b/c/p）+ 两臂 answer_context_tokens_mean。
2. **Given** 上述结果，**When** 比较 iris 对 baseline 的 temporal Δ，**Then** Δ **> 0** 且 exact p **< 0.05** → 判「IRIS 救起 temporal」→ PASS，进 US2；Δ ≤ 0 或 p ≥ 0.05 → 记录并 **STOP**（C 核心假设证伪，回维护者重新定向）。
3. **Given** IRIS 多轮检索，**When** 核对每题 LLM 调用来源，**Then** 答题与评估均走**本地 vllm**（Qwen），零付费云调用。
4. **Given** 本特性任意改动，**When** `git diff --name-only -- memory embedding provider store internal`，**Then** 为空。

---

### User Story 2 — multi-hop 类：IRIS 迭代 + graph 扩展 (Priority: P2)

在 US1 的 IRIS 闭环基础上，对 multi-hop 类（category 1）叠加 003 `--assoc` graph 扩展（LaceMem Edge 层，多跳证据链），`--only-category 1` 单变量 A/B 验证 multi-hop 翻正。A 已显示 graph 对 multi-hop 有 +2.13pp 倾向（不显著），IRIS 的 sufficiency-eval 控制图扩展时机应能放大它且不伤 temporal（temporal 类本就不用 graph）。

**Why this priority**：multi-hop 是同预算下另一被碾压点（−10.99pp）；EviMem 证明 Edge 图扩展是 multi-hop 高分的必要条件。依赖 US1（复用 IRIS 闭环）。

**Independent Test**：同 US1 固定轴，`--only-category 1` 跑 `hybrid+assoc`（US1 的 IRIS + graph）vs `hybrid`（baseline），multi-hop 子集（n=282）配对 exact McNemar。

**Acceptance Scenarios**：
1. **Given** US1 已 PASS 且 multi-hop 臂（IRIS + `--assoc`）就绪，**When** `--only-category 1` 同栈同预算对照，**Then** 报 multi-hop Δ + 配对 exact McNemar。
2. **Given** 上述结果，**When** 比较 Δ，**Then** multi-hop Δ **> 0** 且 p **< 0.05** → PASS；否则记录（可能 multi-hop 需更强图或 IRIS 单独够）。
3. **Given** graph 仅作用于 multi-hop，**When** 回看 US1 temporal 结果，**Then** temporal 不因 US2 改动而回归（category-conditional 隔离）。

---

### User Story 3 — 全量同预算对照：真实超越判定 (Priority: P3)

US1+US2 通过后，tk7 全量 1540（折叠 1529 配对）跑 IRIS 最优组合（temporal 走纯 IRIS、multi-hop 走 IRIS+graph）vs flat hybrid baseline，验证**真实超越**：multi-hop/temporal 配对显著翻正、overall 不回归、且 answer_context_tokens 仍对齐 MemOS。

**Acceptance Scenarios**：
1. **Given** US1/US2 已 PASS，**When** tk7 全量同栈同预算跑 IRIS 组合 vs baseline，**Then** 1529 配对 exact McNemar：multi-hop Δ>0 且 temporal Δ>0 且 overall 不回归（Δ ≥ −0.5pp），answer_context_tokens_mean ≈1059（±10%）。
2. **Given** 通过，**When** 更新 [results.md](../../docs/evaluation/results.md) / [budget-ablation](../../docs/evaluation/reports/budget-ablation.md) / README，**Then** 声明"MemOS 对齐预算下 multi-hop/temporal 真实超越"，附配对统计证据 + 口径。
3. **Given** 任意被采纳组合，**When** 检查默认栈，**Then** 仅在显式 flag（如 `--iris`）下启用，默认行为不变（与 003/008 一致，不悄悄改默认）。

## 风险与诚实边界

- **EvalSufficiency 依赖 Qwen 能力**：EviMem 用 GPT-4o-mini 做评估；Qwen3.6-35B 的证据充分性判别能力可能弱于 GPT-4o-mini，导致 tier 不可靠、迭代不收敛。US1 是这个风险的直接闸。
- **成本/延迟**：IRIS 每题 2~3k+1 次 LLM 调用（k 轮 sufficiency + refine + 1 答题）；本地 vllm near-free 但延迟上升。需报告每题平均调用数与延迟。
- **预算对齐的严格性**：IRIS 多轮检索累积证据，但送答题器的最终上下文须仍 ≈1059 tok（同 MemOS）。若 IRIS 靠"多塞证据"取胜而预算超 MemOS，则不是真实超越——须在 US3 核对 answer_context_tokens。
- **staged 止损**：US1 不过则 STOP，不扩展 US2/US3（低成本止损，避免在证伪的核心假设上堆建设）。
- **不外推**：结果只在固定 v4-flash 同栈 tk7 下成立；不写成通用系统排名（与 budget-ablation 一致的诚实口径）。

## 参考

- EviMem: Evidence-Gap-Driven Iterative Retrieval for Long-Term Conversational Memory, arXiv 2604.27695（IRIS + LaceMem，LoCoMo temporal/multi-hop 大幅提升）。
- MemOS: A Memory OS for AI System, arXiv 2507.03724（竞品 tree/graph 机制）。
- 前置：[budget-ablation](../../docs/evaluation/reports/budget-ablation.md)、[A 测量 verdict](../../docs/evaluation/experiment-verdicts.md)（021 行）、[003 assoc 引擎](../003-bio-retrieval-locomo/spec.md)。
