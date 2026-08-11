# Feature Specification: Memory-Specific Reranker Training(记忆专用重排序模型)

**Feature Branch**: `037-memory-reranker-training`

**Created**: 2026-08-11

**Status**: Draft

**Input**: User description: "接下来的一个大方向：为检索到的内容做重排序（reranking），记忆相关的重排序训练。对标 MemOS memos-reranker（0.6B 轻量版 0.5元/万tokens、4B 增强版 2元/万tokens、限时免费）+ memos-extractor + 即将推出的 memos-embedding。轻松起步、超低成本获取记忆服务。先在 HuggingFace 找现成重排序模型，再训练专门用于记忆的模型。"

## Decision and Scope

**这是接续 90pp 冲刺后的下一个大方向：一条"记忆模型"线，V1 只做 reranker 一条（extractor / embedding 是后续独立 feature）。** 产品叙事对标 MemOS 的模型服务形态（轻量模型 + 按量计费 + 限时免费，"按需使用、随规模成长"），技术路径是"先在 HF 选现成轻量重排模型建立记忆场景基准，再用记忆检索数据训练记忆专用重排模型"。

**方向动因（维护者明确）：云厂商 reranker 太贵，目标是用自托管小模型替代贵云 reranker API。** 008 云 rerank（gte-rerank-v2）单日烧 ~¥150 的教训是成本动因的源头——本 feature 不是纯涨分探索，而是**成本驱动的模型替代方向**：训练一个 0.6B 级记忆专用重排模型，自托管（本地 vllm sidecar）跑出与云 reranker 相当或更好的记忆检索效果，推理成本对标/显著低于云 API。小模型替代优先于涨分承诺；若小模型 e2e 不转化，其"成本替代"价值仍按此定位评估（不比云 API 贵、效果不劣于通用本地重排即可）。

本 feature 的**核心命题（新假设，项目从未测过）**：

> 一个用记忆检索数据（LoCoMo ground-truth evidence）专门训练的重排模型，能否学会记忆场景特有的排序信号——时序保序、跨会话关联、fact 溯源——从而把召回提升转化为**端到端答题提升**？

008 US1 只测过**通用现成重排模型**（bge-reranker-v2-m3），从未测过"记忆专用训练"。这是本 feature 与 008 的本质区别。

### Prior Art（alphaXiv 核实的文献依据）

**MemReranker（2605.06132）是本方向最直接的先例——正是 MemOS 自家 memos-reranker 的研究**：0.6B/4B 基于 Qwen3-Reranker，多教师 LLM 蒸馏（GPT+Qwen 集成 → Elo/Bradley-Terry 标定软标签），训练配方 = BCE pointwise 蒸馏（第一段）+ InfoNCE contrastive（第二段，DeAR 两段范式）；训练数据含记忆专用多轮对话（时序约束/因果推理/指代消解）。它在 LOCOMO 检索级指标上大幅超越 BGE-reranker-v2-m3（0.6B MAP 0.7150 vs 0.6708，+4.4pp；temporal 类 0.7811 vs 0.7489——正是 008 被害的类），0.6B 追平 GPT-4o-mini。**关键：MemReranker 只报检索级指标（MAP/MRR/NDCG），没有端到端 QA 配对**——它验证了"记忆专用训练能赢检索"，但"是否转化为端到端答题"（008 铁律）仍是本项目要测的独特问题。

- **Distillation vs Contrastive（2507.08336）**：教师强于学生时知识蒸馏全面优于对比学习（0.5B–3B 跨架构一致）；同容量教师无优势。→ 训练策略选择取决于可得教师强度。
- **Prism-Reranker（2604.23734）**：重排器额外输出"evidence 压缩段"替代原文进下游上下文（Qwen3-Reranker-4B 自蒸馏 +1.54 BEIR-QA NDCG@10）；与 engram evidence-mediation 方向可后续衔接，但其端到端价值论文自认未验证。
- **jina-reranker-v3.5（2607.18152）**：0.6B 打平 4B 通用重排（BEIR 63.2），域特化训练（法律/金融 +9.6）证明"0.6B 定向训练可关小与 4B 通用模型的差距"；**non-commercial 许可**排除出训练起点。
- **xMemory（2602.02007）**：诊断共鸣——agent memory 是高度相关/近重复的交互流，flat top-k 相似度检索必然返回冗余（Gina DoorDash 时序锚点被埋案例与 008 temporal 失败同构）；它走结构解法（decouple→aggregate）而非重排解法。

### 历史约束（为什么"训练模型"之前失败，本 feature 如何规避）

| 教训 | 来源 | 本 feature 的规避 |
|---|---|---|
| **coverage ≠ 端到端分**：本地通用 reranker 拿 turn@30 +15.457pp 但 e2e −0.06pp（p=1.0）NO-GO；temporal 类被砸 −9 题（单轮语义相关性挤掉时序上下文） | 008 US1 + 008 铁律 | GO 门 = LoCoMo 全量**端到端配对**；中间指标（coverage/NDCG）只作诊断不作 verdict；temporal 类单独报告 |
| **中间指标成功 ≠ e2e 转化**：训练抽取器把时间锚定 5%→96.9%、schema 100%，但 e2e −1.2pp NO-GO；**弱教师蒸馏上限 = 教师**（教师自身未转化，学生无法超越） | 028 US2 | 训练监督以 LoCoMo **ground-truth evidence**（真实溯源标注）为主信号——这是文献里没有的资产；LLM-judge 软标签仅在教师足够强时作补充（2507.08336：教师强于学生时 KD 才占优） |
| **训练投入沉没**：QLoRA planner 149/149 planner_error，训练效果从未进入测量 | 023 | 训练前先跑 US1 零成本现成模型基准；训练后必有端到端配对测量，不允许"烧钱没测量"；**训练预算硬上限 ¥100 / 8 GPU·时** |
| **云端/付费重排不得作为本地涨分杠杆**：DEATH RULE | 宪法 | 训练产物接入形态区分两条线：本地自托管（default-off opt-in）与 SaaS 计费（分数单独口径、不回填本地基线） |

### 范围边界

- **做**：现成 0.6B 级重排模型的记忆场景基准（US1）→ 记忆专用重排模型训练 + 端到端验证（US2）→ 产物与接入形态评估（US3）。
- **不做**：memos-extractor（抽取器线，028 已证写侧 event 不转化）、memos-embedding（embedding 线，bge-large 已转正）、4B 级模型训练（0.6B 起步，4B 是后续放大决策）。
- 本 feature 是**探索线**：不承诺涨点，只承诺"每次投入都进入端到端测量"。

## Clarifications

### Session 2026-08-11

- Q: GO 门评测协议——全量配对 vs 留出干净评测？ → A: Option A——全量 LoCoMo 配对为唯一 GO 门（与 008/历史同口径可比），留出对话子集作二级泛化诊断（FR-004 与 FR-007 的一致性收口）。
- Q: 本 feature 的方向定位是什么？ → A: 成本驱动的模型替代——云厂商 reranker 太贵（008 云 rerank ~¥150/天），目标是用自托管 0.6B 小模型替代贵云 reranker API；小模型替代优先于涨分承诺。
- Q: 训练 GPU 需求与预算上限？ → A: 0.6B LoRA 单卡 **RTX 4090 24GB 或 A100 40G** 足够（实际占用 ≤12GB，无需 80G Blackwell）；时长 ~1–3 小时（LoCoMo 规模数据 3 epochs + 两段式）；成本 ¥5–15 一档，**预算上限 ¥100 / 8 GPU·时**（含重试）；max_len 降到 2048–4096（engram chunk 短，别学 MemReranker 的 8192 纯浪费）；训练产物放数据盘 `/root/autodl-tmp`。
- Q: 训练数据范围？ → A: **LoCoMo ground-truth 为主 + 补充其他记忆相关数据集**（HF 候选：MSC Multi-Session Chat 系列等，**query→span 监督为启发式派生、噪声监督**）——目的是构建**通用记忆重排模型**（非仅跑分），LongMemEval 500 只作验证集不进训练。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 现成轻量重排模型的记忆场景端到端基准 (Priority: P1)

评测维护者把 0.6B 级现成重排模型（候选：`BAAI/bge-reranker-v2-m3` apache-2.0 / `Qwen/Qwen3-Reranker-0.6B`）经本地 vllm sidecar（OpenAI 兼容 `/v1/rerank`）接入 locomo-bench 的 `hybrid+rerank` 臂，跑 LoCoMo 全量端到端配对。产出与 008 的 bge-reranker-v2-m3 结果可比的记忆场景现状基线。

**Why this priority**: "轻松起步"——零训练成本，复用现有 Reranker 接口与评测臂，半天内可出结果。它回答"通用轻量重排在记忆场景到底行不行"，是 US2 训练的对照臂与动机。

**Independent Test**: 复用 locomo-bench `hybrid+rerank` 臂（已存在），端到端配对 vs 无重排基线，四类别分解 + McNemar；与 008 记录可比。

**Acceptance Scenarios**:

1. **Given** 本地 vllm 已起 0.6B 现成重排模型且 `/v1/rerank` 可用、`ENGRAM_RERANK_*` 已配置，**When** 跑 `hybrid+rerank` 臂全量 LoCoMo，**Then** 输出端到端配对表（总体 + single-hop / multi-hop / temporal / open-domain）+ McNemar p，含 temporal 单独行。
2. **Given** 与 008 相同模型（bge-reranker-v2-m3），**When** 复跑，**Then** 结果与 008 记录同口径可比（同栈同基线），确认或修正 008 的 −0.06pp 结论。
3. **Given** US1 任一现成模型端到端配对仍 NO-GO（temporal 依旧被害或总体不转化），**When** 记录结论，**Then** 它成为 US2"为什么需要记忆专用训练"的动机证据，不当作失败封死方向。

---

### User Story 2 - 记忆专用重排模型训练与端到端验证 (Priority: P1)

维护者从 LoCoMo `qa.question / evidence`（ground-truth provenance）构建 `(query, positive_chunk, negative_chunks)` 训练三元组——负样本包含 (a) 同对话随机 chunk、(b) **语义相关但时序错误/不相关的 hard negative**（针对 008 的 temporal −9）、(c) 跨会话干扰 chunk。训练配方采用文献验证过的两段式（MemReranker/DeAR）：**BCE pointwise 蒸馏/回归 + InfoNCE contrastive 第二段**，基座默认 Qwen3-Reranker-0.6B（MemReranker 同款，解码器架构为后续 evidence 输出扩展留路），单卡 LoRA、成本受控，以端到端配对为唯一 GO 门验证。

**Why this priority**: 这是本 feature 的核心赌注——验证"记忆专用训练能否把重排从 coverage 幻觉变成端到端转化"。ground-truth evidence 是文献里没有的强监督信号（LoCoMo 真实溯源标注），避开 028 的弱教师蒸馏上限。

**Independent Test**: 训练数据从 locomo.json 确定性派生（可审计）；训练产物在相同 locomo-bench 协议下端到端配对 vs US1 基线 + vs 无重排基线；中间指标（val NDCG / coverage / 分数标定分布）只作诊断旁证（MemReranker 证明检索级可赢，本项目测的是 e2e 转化）。

**Acceptance Scenarios**:

1. **Given** 训练数据集由 qa 的 evidence 标注派生且含时序 hard negative，**When** 训练记忆专用 0.6B reranker（LoRA），**Then** 训练 loss 收敛且 val 中间指标可报告（诊断用，不作 GO 依据）。
2. **Given** 训练产物接入 `/v1/rerank`，**When** 分别跑冻结的 US1 与 US2 全量端到端配对（跨 run 逐题配对，`--compare`），**Then** 总体不劣（008 铁律：配对 ≥ 基线），且 **temporal 类不劣于基线**（修复 008 −9 的目标）；全量配对含训练对话的污染必须标注，不能据此单独声称未见对话泛化。
3. **Given** 端到端显著转正（p < 0.05 且幅度 > 噪声标尺），**When** 验证覆盖到 LongMemEval 500 交叉子集 + 留出对话，**Then** 不跨数据集/未见对话翻车（**泛化否决门**：任一不过则不得宣称泛化能力；LME v4-pro 教训：不跨数据集外推涨点）。

---

### User Story 3 - 产物与接入形态评估（对标 memos-reranker 服务线）(Priority: P2)

维护者产出模型卡（参数量 / 训练数据 / 许可 / 推理成本 / 端到端结果）、训练+推理成本测算，并评估两种接入形态：(i) 本地自托管 sidecar（default-off opt-in，宪法 I/V）；(ii) SaaS 计费线（0.6B/4B 两级定价对标 memos-reranker，分数单独口径）。

**Why this priority**: 产品叙事是"超低成本获取记忆服务"，但本地栈默认仍须 default-off（死亡规则）。先把产物形态与成本账算清，接入/定价决策留给后续 feature。

**Independent Test**: 模型卡 + 成本表 + 接入文档可审查；本地接入与 SaaS 线的分数归属明确分离。

**Acceptance Scenarios**:

1. **Given** US2 产物已定，**When** 计算训练成本（GPU 时数/费用）与推理成本（tok/千次、每 1 万 token 价格），**Then** 产出一张可与 memos-reranker（0.5/2 元每万 token）对照的成本表。
2. **Given** 产物要接入本地，**When** 评估，**Then** 明确 default-off、opt-in，不改变本地默认检索行为；SaaS 线分数单列，不回填 LoCoMo 本地基线。

---

### Edge Cases

- **训练数据量不足**：LoCoMo 可答 ~1540 题 → 三元组数量有限。缓解：补充 MSC Multi-Session Chat 等多会话记忆数据集（非仅跑分、求通用）、跨会话组合增强、同一 question 派生多种负样本；LongMemEval 500 保留作验证集不进训练。
- **temporal 类训练学不会时序保序**：cross-encoder 按单轮相关性打分，时序是结构信号。缓解：显式时序 hard negative（"语义相关但时间窗口错误"）、temporal 类训练样本上采样、必要时训练目标引入时序保序项。
- **重排模型在长 chunk / 多事实 chunk 上退化**：chunk 内多事实时相关性分数粒度粗。缓解：chunk 长度裁剪、fact 级重排与 chunk 级重排对照（利用 observation fact 粒度）。
- **现成模型许可限制微调产物发布**：Qwen3-Reranker 许可对微调产物发布有条款（MemReranker 已以其为基座发布模型，说明路径可行，但需核自身许可条款）；jina-reranker-v3.5 为 non-commercial（排除）；bge-reranker-v2-m3 为 apache-2.0。缓解：Qwen3-Reranker-0.6B 为主基座但先核许可，bge-reranker-v2-m3 为 apache-2.0 后备对照臂。
- **检索级赢 ≠ 端到端赢**：MemReranker 证明记忆专用训练能在检索级大幅赢，但**没有任何论文证明它转化为端到端 QA 配对**（008 铁律）。缓解：US2 的 GO 门必须是端到端配对，检索级（MAP/NDCG/coverage）只作诊断；若检索级赢而 e2e 不转化，按 008 结论收口为"第 N 次证伪"而非继续扩训练投入。
- **评测污染**：训练数据与评测集同源（LoCoMo）→ 过拟合风险。缓解：**GO 门用全量配对（与 008/历史同口径可比，见 Clarifications Q1）但必须标注含训练对话的污染**；留出对话子集 + LongMemEval 500 升为**泛化否决门**（不满足即不能宣称"未见对话/跨数据集"泛化，不只作可忽略诊断）；source ID ↔ bench ordinal ↔ question ID 映射必须保存（真实 conv ID 为 conv-26/30/41/42/43/44/47/48/49/50）。
- **temporal 可判别性**：重排输入只有文本（retriever 只传 `Entry.Content`，无结构化 `EventDate`）→ **时序 hard negative 只依赖文本中可见的时间关系**；训练/宣称前先做真实 payload 可判别性审计（≥50 组）；若必须用 EventDate 信号，需另立 engine 公共契约增量（037 不得暗改引擎）。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 系统 MUST 支持在 locomo-bench `hybrid+rerank` 臂接入任意 OpenAI 兼容 `/v1/rerank` 端点（复用现有 `embedding.Reranker` 接口，不改引擎）。
- **FR-002**: 系统 MUST 构建重排训练数据集——主源 LoCoMo ground-truth evidence（每条含 query / **全量 positives（多 evidence 时全部为正，group-aware multi-positive）** / negatives / category / 时序标签 / split，负样本含随机同对话 + 时序 hard negative + 跨会话干扰；**同一 question 的多个正例、近重复与 evidence-overlap 候选不得互作负例**），并补充其他记忆相关数据集（如 MSC Multi-Session Chat 系列，**query→span 监督为启发式派生（HF 镜像无显式 QA），标注为"噪声派生监督"、需人工精度门**）以提升通用性（**非仅跑分**）；训练监督以 ground-truth evidence 为主，LLM-judge 软标签仅作可选补充；LongMemEval 500 保留作验证集不进训练。
- **FR-003**: 系统 MUST 支持对 0.6B 现成重排模型做两段式训练（BCE pointwise 回归 + InfoNCE contrastive 第二段）、单卡 LoRA、成本受控；基座默认 Qwen3-Reranker-0.6B（需核许可条款），bge-reranker-v2-m3 作为对照臂（008 已有同模型数据）。
- **FR-004**: 系统 MUST 以 LoCoMo 全量端到端配对为训练产物的唯一 GO 门；coverage/NDCG 等中间指标只作诊断不作 verdict。
- **FR-005**: 系统 MUST 单独报告 temporal 类的端到端配对，作为 008 −9 回归的修复验证。
- **FR-006**: 训练产物接入形态 MUST 区分本地自托管（default-off opt-in）与 SaaS 计费线；SaaS 线分数单独口径，不回填本地基线（死亡规则边界）；**主目标形态是自托管小模型替代贵云 reranker API，推理成本须对标/显著低于云 API**。
- **FR-007**: 系统 MUST 按对话 split 隔离训练集与评测集，报告在"未见对话"上的端到端表现，防止同源过拟合。

### Key Entities *(include if feature involves data)*

- **重排训练样本**: query（LoCoMo question）+ positive（含 evidence 的 chunk/observation）+ negatives（随机/hard-时序/跨会话）+ category + 时序标签；从 locomo.json 确定性派生，可审计。
- **重排模型产物**: 模型卡（参数量 / 训练数据来源与规模 / 许可 / 训练+推理成本 / 端到端配对结果）+ 评测报告（总体 + 四类别 + McNemar p）。
- **端到端配对报告**: 与 008 协议同口径的配对实验记录（vs 无重排基线、vs US1 现成模型），含 flip 计数与 McNemar p。
- **对话 split**: 训练/评测对话划分记录，保证"未见对话"上的泛化证据。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: US1 落地——LoCoMo 全量端到端配对表（现成 0.6B 重排 vs 无重排，四类别 + McNemar p），与 008 记录可比。
- **SC-002**: US2 记忆专用模型端到端配对 **不劣于** US1 基线（008 铁律），且 **temporal 类不劣**（修复 008 −9）；若显著转正（p < 0.05、幅度 > 噪声标尺）则达成超越。
- **SC-003**: 训练投入可测量——每次训练产物都有对应端到端配对结果（023 反例：不允许"烧钱没测量"）；训练成本受控（**单卡 24–40GB、~1–3 小时、预算上限 ¥100 / 8 GPU·时**，超限即停止并先跑 e2e 再决定是否加投）。
- **SC-004**: 产物形态与成本账交付——模型卡 + 训练/推理成本表；**自托管小模型的推理成本显著低于云 reranker API**（对标 memos-reranker 0.5/2 元每万 token 量级，008 云 rerank ~¥150/天为反例），本地/SaaS 分数归属线明确分离。
- **SC-005**: 端到端显著转正只触发"是否发布为**显式 opt-in sidecar 产物**"的决策（死亡规则：cross-encoder 永不进本地默认栈，default-off opt-in 是唯一形态），本 feature 不做发布决策；4B / SaaS 交付物明确排除在 037 训练与本地交付范围之外。

## Assumptions

- 训练监督以 LoCoMo **ground-truth evidence**（真实溯源标注）为主信号——这是文献（MemReranker 等）没有的强监督资产，规避 028 的"弱教师蒸馏上限"；LLM-judge 软标签仅在教师足够强时作补充（2507.08336：教师强于学生时 KD 才占优）。
- 训练配方采用文献验证的两段式：BCE pointwise + InfoNCE contrastive（MemReranker/DeAR），基座默认 Qwen3-Reranker-0.6B（需核许可），bge-reranker-v2-m3 作 apache-2.0 对照臂。
- 0.6B 级模型 + LoRA、单卡可训——规避 023 的"训练投入沉没"，训练前先跑 US1 零成本基准。
- 训练与评测在现有 AutoDL 云 GPU + 本地 vllm sidecar 上进行（remote-eval-box runbook 与 AutoDL 磁盘卫生规则适用）；**推理端绝不依赖付费云重排 API**（死亡规则：训练中的 teacher/judge 一次性标注与推理端 paid rerank 是两回事，后者仍禁）。
- **方向定位 = 成本驱动的小模型替代**：主目标是用自托管 0.6B 记忆专用重排模型替代昂贵云 reranker API（008 云 rerank ~¥150/天为反例），推理成本对标 memos-reranker 0.5/2 元每万 token 量级；涨分不是唯一验收，成本替代价值单列评估。
- **训练数据源（HF 已核实）**：LoCoMo（已有 locomo.json）为主；补充 **MSC Multi-Session Chat**（NeurIPS 2021 基准，HF 镜像 `gonced8/multi-session_chat` / `nayohan/multi_session_chat`，含多语言版；结构 = init_personas + sessions + dialogue，**HF 镜像无显式 QA——query→span 监督需启发式派生（persona-recall / cross-session reference），标注为"噪声派生监督"**，冻结数据镜像/revision/派生算法/质量门）；**LongMemEval**（验证集，社区清洗版 `xiaowu0162/longmemeval-cleaned` 或官方 github mem0ai/LongMemEval）；PerLTQA 待核（xMemory 论文引用，HF 未直接搜到）。筛选准则：真实多会话对话记忆 + 明确 relevant span 标注（LoCoMo 为 ground-truth，MSC 为噪声派生）。
- SaaS 计费线分数单独口径、不回填本地基线（死亡规则）；本地默认栈在端到端转正前保持 default-off。
- Prism-Reranker 式"重排器顺带输出 evidence 压缩段"（与 evidence-mediation 衔接）是 US3 之后的扩展方向，不进 V1 范围。
- 大方向若验证失败（端到端依旧不转化），本 feature 按探索线收口为"第 N 次证伪"文档，不阻塞后续 extractor/embedding 模型线的独立探索。
