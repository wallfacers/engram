# Research: 读侧证据装配结构（Evidence Mediation）

**Date**: 2026-08-06 · **来源**: 代码库探查（cmd/locomo-bench 装配/检索/token 基建）+ MemChain（2607.24097）/ Retain or Consolidate（2607.17545）alphaXiv 精读

## 决策 1：token 精确计数 —— 复用现有 `vllmTokenCounter`，扩展批量 `/tokenize`；`estimateTokens` 仅作离线 fallback

- **Decision**: FR-002 的精确 token 记账复用 `cmd/locomo-bench/token_counter.go` 的 `vllmTokenCounter`（调 vLLM `/tokenize` endpoint，已含 3 次重试 + fingerprint + 运行时 usage 校准 `calibrateEvalTokenCounterAgainstRuntime`），扩展为**批量 `/tokenize`**（一次请求多条证据文本，逐条精确 count，满足「逐条记账 + 零估算误差」且成本受控）；`estimateTokens`（agentic_nav.go:419，`len(runes)/4`）保留为离线 fallback，启用时显式标记降级（宪法 V）。
- **Rationale**: 项目**无纯 Go tokenizer**（go.mod 零命中，CGO=0 硬约束下引入新 tokenizer 依赖风险高）；vllmTokenCounter 已实现/已测试/已与答题模型运行时 usage 校准——它是「答题模型对应 tokenizer」的精确来源，且 harness 已连 vLLM（029 nav_http.go 同栈）。主答案路径（`buildAnswerPrompt`+`toMemories`）目前**完全无 token 记账**，`estimateTokens` 仅导航路径用——FR-002 是接入，不是替换。
- **Alternatives considered**: (a) 引入纯 Go tokenizer（tiktoken 等）——无 CGO 下可行但新依赖+维护成本，且需保证与 vLLM tokenizer 完全一致（BPE 版本漂移风险），拒绝；(b) 保留估算+全局校准系数——仍是估算，不满足「逐条精确」，拒绝。

## 决策 2：配对评测口径 —— 完全复用 029 基建（`--only-questions` + `--repeats 3` + majority + McNemar）

- **Decision**: US2/US3 的 e2e 配对复用既有机制：`--only-questions <whitelist>`（`conv-N-q-M` 逐行）+ `--repeats 3` + `majorityCorrectness` + `exactMcNemarTwoSided`（`paired_eval.go`），whitelist 复用 029 `phase0-ids.txt`（temporal 59 + multi-hop 25 = 84 题）；全量 LoCoMo 1540（省略 `--only-questions`）作 US1 装配不回归佐证与最终口径。
- **Rationale**: `paired_eval.go` 已实现 majority（奇数 repeat 多数）与 exact McNemar（条件二项分布，非卡方）；`--only-questions` 解析在 `selectQuestions`（main.go:2788）纯 ID 匹配、不区分类别——84 题的「temporal+multi-hop」来自 whitelist 文件内容本身。008 铁律（majority ≥ 基线）为唯一 GO 门。
- **Alternatives considered**: 自建配对协议——重复造轮子且破坏可比性，拒绝。

## 决策 3：装配侧 chunk 优先 —— 独立于检索侧 `applyChunkQuota`，新增 chunk-fraction 诊断

- **Decision**: FR-003 的「chunk 原文优先」在**装配器内**实现（按 `Name` 前缀 `"chunk-"` 区分 chunk/fact，chunk 先入、fact 补足，到 cap 为止），与检索侧 `applyChunkQuota`（chunks.go:207-230 的槽位保留）解耦；新增 `chunk_fraction` 诊断字段（当前代码无此统计，029 的 1% 是手工观测）由 `assembly_diagnose.py` 产出。
- **Rationale**: `applyChunkQuota` 是**检索侧 top-K 窗口槽位保留**（`retrievalFor(qa.Category)` → quota 槽），不保证装配侧 chunk 占比——029 证明检索侧有 chunk 槽、导航装配仍 1%。装配器对已进入候选的证据重排（chunk 优先 + 类别结构），才是 SC-002 的修复点。3654 tokens 是 `--chunk-quota 12` 的观测值（12 chunk ≈ 2700 + prompt），作为装配校准锚点。
- **Alternatives considered**: 只调 `--chunk-quota` 参数（检索侧）——不修装配侧稀释，且参数语义是「槽位」非「占比」，拒绝。

## 决策 4：类别条件装配策略 —— per-category 选择器；temporal 复用 `buildTimelineBlock`，multi-hop 新建实体组织

- **Decision**: FR-004 用 per-category 装配策略选择器（复用 `retrievalFor(qa.Category)` 的 per-category 覆盖模式）：temporal（cat 2）→ 复用 `buildTimelineBlock`（timeline.go:70，017 遗产，完整时间线渲染器 + 三不变量）；multi-hop（cat 1）→ **新建实体组织装配**（实体分组 + 按实体序呈现，借用 IRIS slot-merge 的去重/topK 思想）；open-domain/single-hop → 通用装配（chunk 优先 + 分数序）。
- **Rationale**: `buildTimelineBlock` 是唯一的现成类别条件渲染器（`category != temporalCategory` 时返回空，三不变量：NEVER INVENT / DEGRADE / DETERMINISTIC），temporal 直接复用零成本；IRIS（021 `iris-loop.md`）是**检索侧多轮循环**（sufficiency→refine→re-search→slot merge），**不是答案侧实体组织**，且 021 为 closed-no-go——multi-hop 实体装配必须新建，但可借鉴 slot-merge 的去重/边界纪律。category 常量（dataset.go:211-224）：1=multi-hop, 2=temporal, 3=open-domain, 4=single-hop。
- **Alternatives considered**: 所有类别统一装配结构——违背 021「temporal≠graph」实证，拒绝；multi-hop 复用 IRIS——它是检索侧且已证伪，拒绝。

## 已排除方向（防重复打假）

- 写侧 event/图替换原文（027/028/014 三次证伪）
- 检索侧单次表示改进（021 六次证伪）
- 结构化导航（树+图层级，029 US3 门禁跳过）
- 付费云 rerank/recall 涨点（DEATH RULE）
- 引入纯 Go BPE tokenizer 依赖（决策 1 拒绝）
