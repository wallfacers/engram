# Research Notes: Memory-Specific Reranker Training

**Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

解决 plan Technical Context 中所有 NEEDS CLARIFICATION。每个 R 条目按 Decision / Rationale / Alternatives 记录，并给出可审计依据（文献 arXiv id 或项目历史文件）。

---

## R1. 基座选型：Qwen3-Reranker-0.6B（Apache 2.0 确认）

**Decision**: 主基座用 `Qwen/Qwen3-Reranker-0.6B`（0.6B，Apache 2.0，上下文 32K，100+ 语言，指令感知）。训练产物 LoRA adapter 基于它发布。

**Rationale**:
- **许可已核验**：HF 模型卡明确标注 Apache 2.0，无微调产物发布限制条款 → 主基座许可疑虑解除（spec Clarifications Q2 遗留项已闭环）。训练产物可发布（对标 memos-reranker 服务线）。
- **MemReranker 同款基座**（2605.06132）：MemOS 自家的记忆重排模型正是基于 Qwen3-Reranker 训练，0.6B 在 LOCOMO 检索级 MAP 0.7150 vs BGE-v2-m3 0.6708，证明此基座在记忆场景可训练出好结果。
- **指令感知（instruction-aware）**：Qwen3-Reranker 支持 `<Instruct>/<Query>/<Document>` 模板，官方建议自定义 instruct（MemReranker 的 intent-focusing / entity-augmentation 指令即基于此）；这对记忆场景的指代消解/意图澄清有直接价值。
- **vllm 原生支持**（vllm≥0.8.5）→ 训练产物可直接经 vllm 的 OpenAI 兼容 `/v1/rerank` 端点服务，接入现有 locomo-bench `--rerank` 臂（`EMBED_RERANK_MODEL` env），引擎零改动。

**Alternatives considered**:
- `BAAI/bge-reranker-v2-m3`（0.6B，apache-2.0）：008 已测（e2e −0.06pp NO-GO），encoder-only 无指令感知，无法做 Prism-Reranker 式 evidence 输出扩展。保留为**对照臂**（008 已有同模型数据，用于验证"通用 vs 记忆专用"的差异）。
- `jina-reranker-v3.5`（0.6B）：BEIR 63.2 打平 4B，但 **non-commercial 许可**，排除出训练起点（spec 已记录）。
- 从零训练小 cross-encoder：无必要——Qwen3-Reranker 是现成的强重排基座，LoRA 微调成本低。

## R2. 训练配方：两段式 BCE pointwise + InfoNCE contrastive（MemReranker/DeAR 范式）

**Decision**: 在 Qwen3-Reranker-0.6B 上做 LoRA 两段式训练：
- **第一段（pointwise）**：sigmoid 后 BCE 回归 ground-truth 相关度标签（二值 0/1 或分级 0–1）。
- **第二段（contrastive）**：InfoNCE listwise，in-batch negatives，增强 hard-sample 判别。
- LoRA r=16, alpha=32，目标模块 attention + MLP；bf16；3 epochs；lr 2e-5；max_len 2048–4096；单卡 RTX 4090 24GB；seed 固定记录。

**Rationale**:
- **两段式范式的文献依据（引用边界精确化，2026-08-11 review 修订）**：MemReranker（2605.06132）用 BCE pointwise（第一段）+ InfoNCE contrastive（第二段）。**注意**：BiXSE 是 graded-label 双编码器（非本项目 cross-encoder + 二值标签），其"点式损失标定更好"不能直接外推；DeAR 的两段式是 pointwise→listwise CoT SFT（非 BCE→InfoNCE）；2507.08336 比较的是对比学习 vs 蒸馏，**不证明 ground-truth BCE 优于 InfoNCE**。→ **"0.6B 点式损失更优"降为待验证假设**，用三 checkpoint 消融（base / BCE-only / BCE→InfoNCE）验证，防第二段破坏 temporal 后无法归因。
- **监督信号 = ground-truth evidence，不用教师蒸馏**：2507.08336（Distillation vs Contrastive）说"教师强于学生时 KD 占优、同容量无优势、无强教师时 CL 是 robust baseline"。本项目用 LoCoMo **真实溯源标注**（qa.evidence）作 ground-truth——这是文献里没有的强监督资产，且**无教师依赖**（避开 028 的弱教师蒸馏上限 + 省一次教师推理成本）。2507.08336 的结论支持：无强教师时 contrastive/pointwise-on-ground-truth 是 robust 基线。
- **032/034 教训延续**：任何训练产物都必须进端到端配对（008 铁律），中间指标（NDCG/coverage）只作诊断。

**Alternatives considered**:
- 纯 KD（多教师 LLM 蒸馏 Elo 软标签，MemReranker 做法）：效果可能更好，但引入教师成本 + 028 蒸馏上限风险；本 feature 先走纯 ground-truth 路径，数据不足时再考虑（spec Assumptions 已记 LLM-judge 软标签为可选补充）。
- 纯 InfoNCE 单段：BiXSE 证明点式损失标定更好，单段 contrastive 分数聚簇、阈值不可设（008 的 BGE 左偏问题），故两段式。
- SFT 生成式（Prism-Reranker 式 evidence 输出）：US3 之后扩展，不进 V1。

## R3. 训练数据构建协议

**Decision**:
- **主源 LoCoMo**（locomo.json，10 对话 conv-26/30/41/42/43/44/47/48/49/50，~1540 可答）：query = `qa.question`；**全量 positives** = 含 `qa.evidence` provenance 的 chunk（同一 question 多 evidence 时全部为正，group-aware multi-positive；实际数据确认 ~423/1986 多 evidence，单 evidence 占多数）；negatives = (a) 同对话随机 chunk、(b) **语义相关但时序错误/不相关的 hard negative**（008 temporal −9 的针对性样本）、(c) 跨会话干扰 chunk；**同一 question 的正例、近重复与 evidence-overlap 候选不得互作负例**。标签：正样本 1、负样本 0（或按相关性分级）。
- **补充 MSC Multi-Session Chat**（HF `gonced8/multi-session_chat` / `nayohan/multi_session_chat`）：**该镜像无显式 QA 字段**（结构 = id / init_personas / sessions，session 有 dialogue / personas / time_elapsed）→ query 需派生（见 R3b）。目的 = 提升通用性（非仅跑分），LoCoMo 数据量不足时的多样性补充。
- **验证集 LongMemEval 500**（spec Clarifications Q3 决定：不进训练）：社区清洗版 `xiaowu0162/longmemeval-cleaned` 或官方 github mem0ai/LongMemEval。
- **评测/训练 split**：LoCoMo 按对话 split（训练对话 + 留出对话作泛化诊断），GO 门用全量配对（Clarifications Q1）。

**Rationale**:
- LoCoMo evidence 是 ground-truth 溯源标注（question → 直接答案来源），比教师蒸馏干净，比通用 retrieval 数据域匹配（评测 GO 门就在 LoCoMo）。
- MSC 补通用性：多会话对话记忆 + persona 记忆，与 LoCoMo 结构互补；多语言版可增多样性。
- 负采样协议直接针对 008 失败模式：temporal hard negative（语义相关但时间窗口错误）教模型"相关性 ≠ 时序正确"。

**Alternatives considered**:
- 只用 LoCoMo：数据量小（~8k pointwise 样本），通用性差（只对 LoCoMo 好 = benchmark-gaming，用户明确拒绝）。
- 引入通用检索数据（MS MARCO/BEIR）：非记忆域，与"记忆专用"定位不符，放弃。
- 教师蒸馏造数据（MemReranker 50K 多轮合成）：引入教师依赖，先不采用。

### R3b. MSC query 派生策略（无显式 QA 的应对）

**Decision**: 用两种派生模式（可并行、可组合）：
- **Persona-recall**：`init_personas`/session `personas` 的每条 persona 事实 → 模板 query（如 "What is one of {speaker}'s personal facts?"）；positive = 对话中体现该 persona 的 span。
- **Cross-session reference**：后续 session 中回指前文事实的句子 → query；positive = 前文对应 span（跨会话记忆检索的真实形态）。

**Rationale**: 无显式 QA 字段时，从 persona + 跨会话回指派生 query，得到"query → relevant span"的训练对，与重排任务同构。

**Alternatives considered**:
- 找带完整 QA 的 MSC 版本（官方 multi-session-chat repo / nayohan transformed）：可行但引入额外数据获取成本，先以派生为主，若效果不足再补。

**执行结果（2026-08-11，T002 冻结）**: 候选 mirror 结构与许可核验结论：
- `nayohan/multi_session_chat`（361 downloads, sha 78b67491c438, parquet）：结构 = 每行一个 session（`dialoug_id`/`session_id` 有序多会话 + `persona1`/`persona2` persona 列表 + `dialogue` turns + `speaker`），**无 `time_elapsed` 时间戳**（cross-session 时间关系只能按 session_id 顺序推断）；train 17.9k / val 3k / test 2.51k。**许可无声明**。
- `gonced8/multi-session_chat`（jsonl）：**license:gpl-3.0**（copyleft，训练数据有风险）→ **排除**。
- `nayohan/multi_session_chat_transformed`（dataID + first~fifth_session_dialogue 合并行）：更适合 cross-session 派生（多 session 一行），但许可同源无声明。
- **冻结决策**：MSC **本批不下载**——LoCoMo 为唯一主源先行（数据链不阻塞）；MSC 作可选补充，许可核验后（官方源/明确声明）再引入。派生逻辑在 `build_training_data.py --msc` 接口保留（T013 已占位）。

## R4. 评测接入协议（引擎零改动，复用现有面）

**Decision**:
- 训练产物经 **vllm** 起 OpenAI 兼容 `/v1/rerank` 端点（Qwen3-Reranker-0.6B base 或训练产物）。**vLLM 版本需实测冻结**：vLLM ≥0.8.5 调 Qwen3 rerank 有已知失败案例（vllm-project/vllm#19229）；官方 pooling score 示例要求 `--runner pooling` + HF overrides + 专用 Jinja 模板。冻结实测版本 + 完整命令。
- locomo-bench 用已有 `--rerank` flag + `EMBED_RERANK_MODEL` env；**rerank 与 embedding 共享 `EMBED_BASE_URL` 端点**（main.go:3058 只读 `EMBED_BASE_URL`，**不存在 `EMBED_RERANK_BASE_URL`**）；多臂用 `--retrieval 'hybrid,hybrid+rerank'`（main.go:1639 逗号拆分，**无 `--arm` flag**）；若需独立 rerank 端点，提供 feature-local router 聚合到同一 `EMBED_BASE_URL`（保持 locomo-bench 零改动）。
- 配对实验协议（008 协议）：US1（现成模型）与 US2（训练产物）**分别冻结运行**，跨 run 逐题配对用 `--compare`（main.go:361 存在）；全量 1540 配对 + 四类别分解 + McNemar + flip 计数 + paired CI；temporal 类单独报告；全量配对含训练对话的污染必须标注；留出对话 + LongMemEval 500 作**泛化否决门**（不只诊断）。
- **评测前 fail-closed preflight**：记录 rerank 请求成功/失败计数，**零成功或失败即标记 INVALID**，禁止静默回退（retriever.go:707-710 的错误回退路径）后出 GO 报告。

**Rationale**:
- 引擎 `embedding.Reranker` 接口（embedding/rerank.go）与 locomo-bench rerank 臂均已存在且 008 验证过（008-local-rerank/scratchpad/rerank_sidecar.py 是先例）→ **本 feature 零引擎改动**（宪法 II 满足）。
- 端到端配对是唯一 GO 门（008 铁律）；vllm 0.8.5+ 原生支持 Qwen3-Reranker。

**Alternatives considered**: 008 的自写 HTTP sidecar（rerank_sidecar.py）：vllm 原生支持后无需自写，直接用 vllm 更稳。

## R5. GPU / 成本（spec Clarifications Q2 定量化）

**Decision**: RTX 4090 24GB 单卡（实际占用 ≤12GB），~1–3h（LoCoMo+MSC 规模数据 3 epochs + 两段式），预算上限 ¥100 / 8 GPU·时（含重试）。max_len 2048–4096。训练产物放数据盘 `/root/autodl-tmp/`（AutoDL 磁盘卫生规则）。

**Rationale**: 0.6B bf16 LoRA 权重 ~1.3GB，激活在 gradient checkpointing + 小 batch 下 <12GB；数据规模小（远小于 MemReranker 的 ~1M+50K），单卡数小时足够。028 先例（3B LoRA 3 epochs 全本地）证明成本可控。

**Alternatives considered**: 8×A800（MemReranker 配置）——本项目数据量 2 个数量级小于它，纯浪费；A100 40G 备选（更稳但更贵）。

## R6. 训练工具链与脚本落位

**Decision**: 训练工具放 `specs/037-memory-reranker-training/tools/`（沿用 028 的 spec 内 tools/ 惯例，spec 产物不进引擎）：
- `build_training_data.py`（LoCoMo + MSC → 训练 JSONL，确定性派生、可审计，负采样协议）
- `train_reranker.py`（两段式 LoRA：BCE pointwise + InfoNCE contrastive）
- `test_training_data.py` / `test_train_smoke.py`（数据 schema 校验 + 训练 smoke）
- 依赖：Python 3.11+, torch, transformers(≥4.51), peft, datasets, vllm(≥0.8.5)。**Python 训练链是模型线的合理例外**——引擎仍纯 Go（宪法技术约束只约束引擎核心路径）。

**Rationale**: 028 先例（specs/028/tools/train_sft.py 等 8 个脚本）验证此结构；训练本质是 ML 工具链，Python 生态（LoRA/BCE/InfoNCE）成熟，不影响引擎的纯 Go 承诺。

**Alternatives considered**: 023 的 repo 根 `training/planner/`——那是跨 spec 复用；本 feature 是单 spec 产物，放 spec 内更内聚。

## R7. temporal 可判别性审计（review C4 引入，P1 前置）

**Decision**: 训练/宣称任何 temporal 能力前，先做**真实 payload 可判别性 probe**：
- 从真实 baseline top-pool 导出 ≥50 组 (query, 候选 documents) 的 `Entry.Content` 文本。
- 判定：text 中是否存在足以区分"正确答案时间窗口内 vs 外"的**文本可见时间信号**（自然语言日期/相对时间/事件顺序词）。
- 只有文本可见的时间关系可用于 temporal hard negative 训练；**未传递 `EventDate` 的样本不得用于训练或宣称 temporal 能力**。
- 冻结相似度模型/revision/阈值、时间窗口、tie-break、假负例排除规则。

**Rationale**: retriever 只传 `Entry.Content`（retriever.go:701），无结构化 `EventDate`；chunk 的 EventDate 存在于 store（chunks.go:257-271）但 reranker 看不到。若文本不可判别，temporal hard negative 生成是假信号——必须先用 probe 证明信号存在。

**Alternatives considered**: 给 rerank 输入拼接 EventDate（需改 engine 契约）——**037 不得暗改引擎**；若 probe 证明文本信号不足且 temporal 是必要条件，另行 spec 提 engine 公共契约增量。

**执行结果（2026-08-11，T012 落地，reports/r7-temporal-audit.md）**: 审计 **FAIL**（阈值 A/B/C 冻结判定）：
- A 通过：答案文档时间信号率 **75.5%**（283/375）——答案可从文本定位时间。
- B 通过：hard 池（窗口外 + 语义相关）日期可提取率 100%（会话日期结构化可用）。
- C **不通过**：hard 池时间信号率仅 **18.7%**（全对话池 13.6%）——**LoCoMo turns 绝大多数不含显式文本时间信号**。
- **设计影响**（关键）：① temporal-hard 负样本池受文本信号稀疏限制（能用的语义相关+有信号候选约 18.7%），T013 生成 temporal-hard 946 条（远小于 3× 配额）；② temporal 判别实际依赖 **query 侧日期信息 + 会话位置**（24.5% 答案文档无信号，如 "When did Melanie go camping in June?" 月份在问题里不在文档里）；③ 严格契约执行下 24.5% 的 temporal 答案文档 temporal_label=false。→ temporal-hard 训练是**弱信号通道**，temporal 端到端目标（修复 008 −9）需降低预期；若 text-only 不可行，temporal 仍是被害类，需另行评估（可能为 engine 契约增量，037 不暗改）。
