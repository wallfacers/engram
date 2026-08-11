# Implementation Plan: Memory-Specific Reranker Training(记忆专用重排序模型)

**Branch**: `037-memory-reranker-training` | **Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/037-memory-reranker-training/spec.md`

## Summary

**主需求（Spec + Clarifications 收口）**：训练一个 0.6B 级记忆专用重排模型，用**成本驱动的小模型替代昂贵云 reranker API**（008 云 rerank ~¥150/天为反例）。技术路径：先在 HF 现成轻量重排模型（Qwen3-Reranker-0.6B，Apache 2.0）上建立记忆场景端到端基准（US1），再用记忆检索数据（LoCoMo ground-truth evidence 为主 + MSC 等记忆数据集补充）做两段式 LoRA 训练（BCE pointwise + InfoNCE contrastive，MemReranker/DeAR 范式），以**全量 LoCoMo 端到端配对为唯一 GO 门**（008 铁律）+ temporal 类不劣（修复 008 −9）+ 留出对话泛化诊断。

**关键设计决定**（详见 [research.md](research.md)）：
- 基座 Qwen3-Reranker-0.6B（Apache 2.0 已核验，MemReranker 同款）；**vLLM 版本与 serving 命令需实测冻结**（vllm#19229 坑，不许假设"原生支持"）。
- 无教师蒸馏——LoCoMo `qa.evidence` ground-truth 溯源标注是主监督信号（文献里没有的强监督资产，避开 028 弱教师蒸馏上限；2507.08336 支持无强教师时 ground-truth contrastive 是 robust 基线）；**"0.6B 点式损失更优"为待验证假设，三 checkpoint 消融**。
- 训练预算硬上限 ¥100/8 GPU·时（**跨 run 累计硬门**）、单卡 RTX 4090 24GB ~1–3h（spec Clarifications Q2）。
- 引擎与 locomo-bench **零改动**：复用现有 `embedding.Reranker` 接口 + `--retrieval 'hybrid,hybrid+rerank'` 臂 + `EMBED_RERANK_MODEL` env（rerank 与 embedding 共享 `EMBED_BASE_URL`；**无 `EMBED_RERANK_BASE_URL`、无 `--arm` flag**，2026-08-11 review 代码核实）。
- 训练脚本落位 `specs/037-memory-reranker-training/tools/`（028 惯例，spec 产物不进引擎）。
- **2026-08-11 外部 review 修订已内化**：P0 修正（default-off 契约收紧、serving/bench 命令可执行化、score equation 冻结、US1/US2 跨 run 配对）、P1 修正（multi-positive schema、temporal 文本可判别性审计 R7、真实 conv ID split、预算机器门禁、hash manifest）。详见各产物。

## Technical Context

**Language/Version**：
- 评测 orchestration：Go 1.25（项目标准，locomo-bench 复用，零改动）。
- 数据构建 + 训练：Python 3.11+（`build_training_data.py` / `train_reranker.py` + torch / transformers≥4.51 / peft / datasets）——模型线训练工具链的合理例外，引擎仍纯 Go（宪法技术约束只约束引擎核心路径）。

**Primary Dependencies**：
- Go：现有 `embedding.Reranker`（embedding/rerank.go）、locomo-bench（`--rerank` flag + `EMBED_RERANK_MODEL` env，main.go:452,3059）。
- Python：torch、transformers、peft、datasets、vllm≥0.8.5。
- 数据：`testdata/locomo/locomo.json`（已有）；MSC HF `gonced8/multi-session_chat` / `nayohan/multi_session_chat`；LongMemEval 验证集 `xiaowu0162/longmemeval-cleaned` 或官方 mem0ai/LongMemEval。

**Storage**: 训练产物（模型权重、JSONL、日志）放 AutoDL 数据盘 `/root/autodl-tmp/`（磁盘卫生规则）；评测产物 `.locomo-run/037-*`（gitignored）。

**Testing**：
- 数据构建：`tools/test_training_data.py` 对 [training-data-schema](contracts/training-data-schema.md) 做 fail-closed 校验（确定性、可审计）。
- 训练：`tools/test_train_smoke.py`（smoke + score 分布 sane，对标 008 BGE 左偏反例）。
- 评测：locomo-bench 端到端配对（008 协议），引擎零改动 → 引擎既有测试全绿即证明没碰引擎。

**Target Platform**: 训练 = AutoDL Linux GPU（RTX 4090 24GB）；评测 = WSL2 + remote-eval-box（answer/judge near-free）；推理 = 本地 vllm sidecar（default-off opt-in）。

**Project Type**: 模型训练探索线（ML 训练管线（Python 数据构建+训练）+ Go 评测 orchestration 复用，引擎零改动）。

**Performance Goals**：
- 训练：≤3h 单卡、≤¥100/8 GPU·时（spec Clarifications Q2）。
- 推理：对标 memos-reranker 0.5/2 元每万 token 量级、~200ms 级延迟（MemReranker 0.6B 参考）；**绝不依赖付费云 reranker API**（死亡规则）。

**Constraints**：
- 引擎不可改（宪法 II）：本 feature 产物全部落在 `specs/037-memory-reranker-training/tools/`（Python）+ 复用现有 Go 面。
- 训练产物本地接入 default-off opt-in；SaaS 线分数单列不回填（死亡规则）；**cross-encoder 永不进本地默认栈**（review B3）。
- 推理端禁付费云 reranker（死亡规则）；AutoDL 磁盘卫生 + 用完停机。
- max_len 2048–4096（engram chunk 短）；bf16；seed 固定可复现。
- **冻结项（2026-08-11 review 修订）**：① score equation（训练↔合并↔vLLM 同一 `yes_logit−no_logit`/scalar head + instruction/template/截断，三方排序一致性测试）；② vLLM 实测版本与完整命令（pooling runner + HF overrides + Jinja 模板；vllm#19229 坑，不许假设"原生支持"）；③ 训练数据 schema（`schema_version`/`qa_id`/`query_group_id`/`positives`/`split` 行字段 + multi-positive 规则）；④ 评测 preflight（rerank 请求成功/失败计数，零成功 → INVALID）；⑤ 泛化否决门（heldout 对话 + LME 500，不只诊断）；⑥ 预算累计硬门（跨 run 记账、¥100/8 GPU·时超限自动停）。

**Scale/Scope**：训练样本 ~LoCoMo 8k pointwise + MSC 数千级；0.6B LoRA；评测全量 1540 配对。诚实声明：单用户 ~10 万条级记忆场景，无百万级语料承诺（宪法 V）。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 状态 | 说明 |
|---|---|---|
| **I. 本地优先离线** | ✅ PASS | 训练产物经本地 vllm sidecar 服务、default-off opt-in；训练用 AutoDL GPU 是**开发/训练时工具**（与 028/023 先例一致），非产品运行时依赖；产品核心路径仍离线可跑。 |
| **II. 引擎/适配分离** | ✅ PASS | 本 feature **零引擎改动**：只复用现有 `embedding.Reranker` 接口 + locomo-bench rerank 臂；训练脚本落在 spec 目录 `tools/`，不触碰 `memory/ embedding/ provider/ store/ internal/`。验证：`git diff --name-only -- memory embedding provider store internal` 必须为空。 |
| **III. 契约优先** | ✅ PASS | 引擎侧 Reranker 契约已冻结（不新增）；新契约仅 `contracts/training-data-schema.md`（训练 JSONL，schema_version=1）与 `contracts/rerank-serving.md`（vllm 端点，复用既有协议）。 |
| **IV. 评测回归门** | ✅ PASS | 探索线 default-off：评测配对（hybrid+rerank vs hybrid）不影响本地默认基线；任何转正决策先过端到端配对（008 铁律）；评测协议与算法分步提交（训练配方超参改动属本 feature 内部，评测协议不变）。 |
| **V. 优雅降级/诚实规模** | ✅ PASS | nil reranker → 静默无重排（已实现）；规模诚实声明（数据量级、单卡训练、成本预算）；超边界（4B、evidence 输出）标记为后续。 |

**Gate 结论**：全部 PASS，无违规需 Complexity Tracking。

## Project Structure

### Documentation (this feature)

```text
specs/037-memory-reranker-training/
├── plan.md              # This file
├── spec.md              # /speckit-specify + /speckit-clarify 输出
├── research.md          # Phase 0：基座/配方/数据协议/评测接入（R1–R6）
├── data-model.md        # Phase 1：训练样本/配置/产物/评测报告实体
├── quickstart.md        # Phase 1：端到端验证引导（场景 1–5 + 排查）
├── contracts/
│   ├── training-data-schema.md   # 训练 JSONL 逐字段契约 + fail-closed 校验
│   └── rerank-serving.md         # vllm /v1/rerank 接入契约
├── checklists/requirements.md    # 质量清单（16/16）
├── tools/               # 实现阶段产出（/speckit-tasks）
│   ├── build_training_data.py    # LoCoMo + MSC → 训练 JSONL（确定性、负采样协议）
│   ├── train_reranker.py         # 两段式 LoRA（BCE pointwise + InfoNCE contrastive）
│   ├── test_training_data.py     # schema fail-closed 校验
│   └── test_train_smoke.py       # 训练 smoke + 产物校验
└── tasks.md             # Phase 2 输出（/speckit-tasks，非本命令创建）
```

### Source Code (repository root)

```text
# 引擎与适配层零改动（硬门，宪法 II）——验证：git diff --name-only -- memory embedding provider store internal 为空
# 新增文件全部在 specs/037-memory-reranker-training/tools/（Python 训练链，spec 产物）
specs/037-memory-reranker-training/tools/
├── build_training_data.py   # 数据构建（LoCoMo evidence + MSC 派生，负采样：随机/时序hard/跨会话）
├── train_reranker.py        # 两段式 LoRA 训练（stage1 BCE pointwise → stage2 InfoNCE listwise）
├── test_training_data.py    # 数据 schema fail-closed 校验 + temporal-hard 抽检
└── test_train_smoke.py      # smoke：1 epoch 收敛 + score 分布 sane + 产物可加载

# 评测/推理复用（不新增）：locomo-bench --rerank + EMBED_RERANK_MODEL env + embedding.Reranker
```

**Structure Decision**：训练工具放 spec 内 `tools/`（028 惯例 `specs/028/tools/` 的 8 个脚本先例；区别于 023 的跨 spec 复用 `training/planner/`——本 feature 是单 spec 产物）。评测/推理完全复用现有 Go 面，零新增 Go 文件。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

无违规（Constitution Check 全 PASS）。训练链使用 Python 是模型线工具链的必要例外，已在 Technical Context 与 Constitution Check（原则 II 说明）记录理由，不构成对"引擎纯 Go"承诺的违反——引擎核心路径（`memory/ embedding/ provider/ store/ internal/`）本 feature 完全不触碰。
