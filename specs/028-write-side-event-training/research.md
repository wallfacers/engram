# Research: 写入侧事件抽取训练化

**Date**: 2026-08-05 · **Spec**: [spec.md](spec.md) · **Plan**: [plan.md](plan.md)

本文件解决 plan Technical Context 中的方法未知项。关键证据已在 2026-08-05 前经 alphaXiv 逐篇核实（见 [lever-batch](../../docs/research/lever-batch-local-vs-saas.md) 与 [027 verdict](../../docs/evaluation/reports/027-write-side-event-verdict.md)），此处只做决策 consolidate，不重复调研。

## R1. 教师抽取器选型（US1）

- **Decision**: 用 **DeepSeek-v4-pro**（托管 API）作为教师抽取器。
- **Rationale**:
  - 已可用且有稳定 key（memory：lme-v4pro-answerer-parity 用过 v4-pro）；低成本（缓存 96%）
  - 时间理解/日期解析能力强于 7B；027 抽取 prompt 可直接复用，加"相对时间→绝对时间"强化指令即可
  - AtomMem 的教师是 GPT-4o + 人工精修，但我们没有 GPT-4o 直接通道，DeepSeek-v4-pro 是等价可用替代
- **Alternatives considered**: GPT-4o（不可直接调用）；Qwen3.6-35B（本地但要起 vllm 占机器）；DeepSeek-v4-flash（便宜但推理弱于 pro，时间锚定是难点，选 pro 稳妥）

## R2. 时间锚定策略（US1/US2 核心）

- **Decision**: 抽取指令强制"相对时间 → 绝对时间"，输出仍走 027 event schema 的 `AbsoluteTS`/`RelativeRef` 字段；训练数据中绝对时间为标注标签。
- **Rationale**:
  - 027 失败根因 = 7B 把绝对日期泛化成相对词（仅 5% 锚定）。教师 + 强化指令应把锚定率拉到 ≥50%（US1 门）
  - AtomMem 把"last Friday→绝对日期"当**显式训练目标**（核心贡献），US2 训练时同样以绝对时间为监督信号
- **Alternatives considered**: 确定性 date 解析回填（正则/规则，但对话里相对时间依赖上下文，规则无法全覆盖）；原文时间戳直通（只对原文含日期的情况有效，无法解决"对话里说 last Friday"）

## R3. 训练数据构建（US2）

- **Decision**: 两阶段管线（AtomMem 同构）：**教师批量标注 → 人工精修 500–1000 条** → 事件级训练 JSONL。
- **Rationale**:
  - AtomMem 用 GPT-4o 教师 + 人工精修构建高质量抽取训练集（两阶段：教师抽取 + 人工校错去噪/消解代词/时间锚定/分解长句）
  - 人工精修是质量天花板；500–1000 条事件级样本对小模型 SFT 足够（LazyMem 4B 在类似规模有效）
  - 训练数据从 LoCoMo 10 对话（5882 消息）抽取事件，天然含 temporal/multi-hop 需要的结构
- **Alternatives considered**: 纯教师标注无人审（幻觉/锚定错误会学进模型，违反 FR-002 可审计）；用已有 7B 抽取结果（正是失败数据，无意义）

## R4. 训练方法（US2）

- **Decision**: **Qwen 1.5B–4B 级 SFT**，训练目标为"输入对话片段 → 时间锚定的双视角 event JSON"；**RL 为可选第二步**（仅 SFT 后端到端未转化时考虑）。
- **Rationale**:
  - LazyMem 实证：4B prompt-only 0.41 → SFT 0.72 → RL 0.85，**SFT 是主力、RL 是补强**
  - 时间锚定是"从对话推绝对日期"的监督任务，SFT 直接可学；先 SFT 看端到端，不盲目上 RL（不大力出奇迹）
  - 小模型（1.5B–4B）在 AutoDL 单卡（24G–48G）可训，符合 SaaS 线成本可控
- **Alternatives considered**: 直接 RL（成本高、SFT 未验证先别上）；蒸馏 7B（失败模型无蒸馏价值）；用 35B 训练（过重）

## R5. 训练/推理环境（US2/US3）

- **Decision**: AutoDL 租 GPU 训练（参考 memory：027 Blackwell 环境的盘位/GPU 纪律）；训练后量化导出，部署为**本地 vLLM sidecar**（US3）。
- **Rationale**:
  - AutoDL 已有成熟流程（memory：autoDL 盘位纪律、vllm 启动踩坑链）；训练在 `/root/autodl-tmp`（数据盘），空闲必停
  - 部署默认本地 sidecar（量化），托管可选——engram 引擎保持 local-first，SaaS 只放宽训练侧（Assumption 已定）
- **Alternatives considered**: 托管推理（第三方推理 API，模型不可控）；纯云端（违背 engram local-first 产品叙事）

## R6. 成本估算（诚实声明）

- **US1（教师验证）**: DeepSeek-v4-pro 抽取 5882 消息 + 84 题配对 ≈ 低个位数美元（缓存命中率高）；零 GPU 成本
- **US2（训练）**: AutoDL 单卡 SFT 数小时 ≈ 数十元级；教师标注 + 人工精修是主要人力成本
- **US3（部署）**: 本地 sidecar，无增量云成本
- 全程不碰付费云 rerank/recall（DEATH RULE 在 SaaS 线外仍守评测纪律）

## 与既有文档的关系

- 027 失败点与配对口径：[027 verdict](../../docs/evaluation/reports/027-write-side-event-verdict.md)
- SaaS 线约束与 LazyMem/AtomMem 证据：[lever-batch](../../docs/research/lever-batch-local-vs-saas.md)
- 训练式本地 Evidence Planner 同线先例：spec 023（训练式编译器）
