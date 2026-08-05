# Implementation Plan: 写入侧事件抽取训练化

**Branch**: `028-write-side-event-training` | **Date**: 2026-08-05 | **Spec**: [spec.md](spec.md)

**Input**: 走 spec 规范开 SaaS 线——训练抽取器做写侧结构（SaaS 线第一步）

## Summary

027 实测证明写侧 event 结构的失败点是**抽取器能力**：7B 无训练把绝对日期泛化成相对词（时间锚定率仅 5%），端到端 −26.2pp。本 feature 验证**训练能否解掉这个失败点**（AtomMem 证明训练级抽取器时间锚定后 temporal +31.1pp）：US1 用托管教师零训练验证"能力是瓶颈"→ US2 训练时间锚定抽取器并 027 复测（008 铁律端到端转化）→ US3 部署接入（default-off、单独口径）。全程复用 027 已建的 eventstore 引擎 + harness 配对跑法，SaaS 线允许训练算力但分数不回填本地。

## Technical Context

**Language/Version**: Go 1.25（harness/引擎，不变）· Python 3.13（训练数据构建 + 训练脚本，作为 feature 可复现资产）

**Primary Dependencies**:
- `memory/eventstore`（027 已建）：Event 类型、`Validate`/`ValidateLenient`、fail-closed `Extractor`、可重建 `Project`——训练抽取器接入点
- `cmd/locomo-bench`（027 已建）：`--build-event-project` 并发抽取 + `--representation event` 渲染 + `--only-questions` 配对跑法
- DeepSeek API（教师抽取，US1 零训练验证）
- AutoDL + vLLM + 开源模型（US2 训练 + 推理；参考 memory 里的 AutoDL 盘位/GPU 纪律）

**Storage**: 027 `event-project.json` 投影（复用）；训练数据 JSONL（可审计）；**不碰 SQLite schema，不改引擎**

**Testing**: `CGO_ENABLED=0 go test ./memory/eventstore/ ./cmd/locomo-bench/`（引擎不变回归）· 配对验证 = 84 题 majority + McNemar（复用 027 pair_analysis 脚本）· 时间锚定率/合法率/幻觉审计脚本

**Target Platform**: Linux（AutoDL 训练 + 推理；WSL2 本地配对验证）

**Project Type**: 研究型验证 feature（harness 调用方 + 训练管线；非引擎核心改动）

**Performance Goals**: US1 教师时间锚定率相对 7B（5%）→ ≥50 绝对点；US2 训练抽取器时间锚定率 ≥70%；端到端 event 臂 ≥ chunk 臂（008 铁律唯一 GO 门）

**Constraints**: 本地默认路径零行为变化（US3 default-off）· SaaS 线分数单独口径、不回填本地 · 训练/托管允许但遵守"空闲必停"与盘位纪律 · 训练数据/产物可审计可复现（FR-002/003）

**Scale/Scope**: 84 题配对为每阶段 GO 门（US1/2）；全量 1540 在 US3 后按需；训练数据以 LoCoMo 对话为起点（5882 消息 → 事件级标注），垂直场景数据为可选扩充

## Constitution Check

*GATE: 五项原则逐条核对。本 feature 是 SaaS 独立产品线（CLAUDE.md 已定：训练式本地 Evidence Planner 同属此线），本地默认路径不受影响。*

| 原则 | 核对 | 状态 |
|---|---|---|
| **I 本地优先，默认离线** | SaaS 线**允许训练/托管**（显式豁免，CLAUDE.md "SaaS 方向为独立 opt-in 产品线"）。但本地默认路径零变化：US3 接入 default-off，训练抽取器不可用时 fail-closed 退回 7B/原文 chunk（027 已有）。 | PASS（显式豁免 + 默认路径不变） |
| **II 引擎/适配分离** | 训练抽取器经 027 已建的 `eventstore.ModelCaller` 接口接入，不碰 `memory/` 引擎内部；验证仅改 harness 调用方（换抽取 LLM）与 scripts。 | PASS |
| **III 契约优先** | 复用 027 event 契约（Event schema/Validate）；新增训练数据 schema 与配对口径在 contracts/ 冻结后实现。 | PASS |
| **IV 评测回归门禁** | 每阶段配对（同 store/answerer/judge/token cap、3 reps majority、McNemar）＋单独口径；算法/口径改动分开提交。 | PASS |
| **V 优雅降级与诚实规模** | fail-closed 保持；规模诚实：84 题门禁先行，全量/泛化标注为未验证边界；SaaS 分数单独声明。 | PASS |

*门禁结论：无违背；SaaS 豁免在 CLAUDE.md "SaaS 方向为独立 opt-in 产品线，分数单独口径声明，不得回填为本地涨点" 有明文依据。*

## Project Structure

### Documentation (this feature)

```text
specs/028-write-side-event-training/
├── plan.md              # 本文件
├── research.md          # Phase 0：训练方法/教师选型/数据构建决策
├── data-model.md        # Phase 1：训练集/抽取器模型/配对分数表
├── quickstart.md        # Phase 1：US1-3 可跑验证指南
├── contracts/           # Phase 1：训练数据 schema / 教师调用 / 配对口径
├── checklists/requirements.md  # spec 质量清单（已过）
└── tasks.md             # Phase 2（/speckit-tasks）
```

### Source Code (repository root)

```text
# 引擎与 harness：复用（仅当需要时最小改动）
cmd/locomo-bench/eventstore_eval.go   # 复用 --build-event-project/--representation event
                                      #   可能加：时间锚定强化 prompt flag（--event-anchor-prompt）
memory/eventstore/                    # 不动（引擎 untouchable）

# 训练/数据管线（feature 可复现资产，跟随 028 交付）
specs/028-write-side-event-training/
├── tools/
│   ├── build_training_data.py        # 教师标注 → 事件级训练 JSONL（含时间锚定强制）
│   ├── audit_anchoring.py            # 时间锚定率/合法率/幻觉抽样审计
│   ├── train.sh                      # SFT 训练入口（AutoDL，llama-factory/transformers）
│   └── export_deploy.sh              # 量化导出 + 本地 vLLM sidecar 配置
└── data/                             # gitignore 或 HF 托管（不 commit 大文件）
```

**Structure Decision**: 引擎/harness 全部复用 027 已建资产，新代码只落在 `specs/028/tools/`（训练/数据/审计脚本，纯 Python，feature 内交付）与 harness 调用方的可选 flag。训练产物（模型权重/大数据）不 commit，走 HF 或 AutoDL 盘位。训练算力/教师 API 属 SaaS 线成本。

## Complexity Tracking

> 无宪法违背，无需 justification。SaaS 豁免为政策明示（见 Constitution Check I）。

## Phase 0/1 输出（research/data-model/contracts/quickstart）

见同目录 `research.md`、`data-model.md`、`contracts/`、`quickstart.md`。
