# Implementation Plan: 本地训练式 Evidence Planner

**Branch**: `023-local-trained-evidence-compiler` | **Date**: 2026-08-02 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/023-local-trained-evidence-compiler/spec.md`（含 2026-08-02 Amendment）

## Summary

在 022 冻结的固定候选与 Evidence Compiler 合同之上，训练一个**可自托管的本地 Evidence
Planner**：输入 query + 冻结 candidates，输出符合 022 proposal 合同的 Evidence Need +
受限 actions，全部经 022 fail-closed 校验，失败退回确定性 Compiler。默认底模
**Qwen2.5-7B-Instruct**（Apache-2.0，产物可分发可推荐），LoRA/QLoRA 微调，单卡 24 GiB、
一次正式重建 ≤ 24 GPU-hours、Planner p95 ≤ 2.0s。核心验证是**单变量配对**：同 store、候选
逐字节一致，只差 Planner 训练状态（deterministic → prompt-only → supervised），确认训练
是否带来可归因、可迁移的涨点。022 依赖已冻结（Compiler 合同、fixed-gold oracle、LoCoMo B1
85.19%）；缺失件（`local_planner.go` 接入点、residual cohort 量化）由本 feature 前置任务补全。

## Technical Context

**Language/Version**: Go 1.25.0（CGO_ENABLED=0 硬门，引擎/harness）；训练侧 Python（TRL/
PEFT，独立于 Go build，不进 CI 构建门）

**Primary Dependencies**:
- `memory/evidencecompiler`（022 已冻结：contracts/need/extract/validate/render/resolve + compiler.go 门面，测试全绿）
- `cmd/locomo-bench/eval_fixed_gold_oracle.go`（022 已实现，residual 判定工具）
- `cmd/locomo-bench/local_planner.go`（022 T070 未交付 → **本 feature 前置补齐**，harness 侧 adapter，engine 零改动）
- `provider/` LLM 抽象（复用，Planner 以 sidecar 方式接入，见 Key Decision 2）

**Storage**: 复用 022 Evidence Ledger / evidencecompiler 合同，不新增 schema。训练数据、模型
产物、缓存放**数据盘**（服务器 `autodl-tmp` 或等价），Git 只跟踪脚本与摘要，不跟踪权重/大数据
（gitignore）。

**Testing**: `CGO_ENABLED=0 go test -count=1`（引擎离线测试，向量 stub）；`local_planner.go`
用 mock sidecar 离线测试；三臂配对走 `cmd/locomo-bench` formal protocol（同 store、候选逐字节
一致、单变量）。

**Target Platform**: Linux（训练/评测在租用 24 GiB 单卡；开发在 WSL2）

**Project Type**: Go library（零引擎改动）+ harness adapter + 独立 Python 训练 pipeline

**Performance Goals**: 并发 1 Planner p95 ≤ 2.0s（7B 短 proposal 输出，vllm/ollama sidecar）；
训练总用量 ≤ 24 GPU-hours；峰值显存 ≤ 24 GiB

**Constraints**: 默认关、确定性 fallback（FR-019/020）；纯离线、付费 teacher 调用数 = 0
（FR-013/021）；禁止付费云 reranker/answerer 作为正式涨点（DEATH RULE）；配对必须同 store +
候选逐字节一致（025/026 纪律）

**Scale/Scope**: 单用户 ~100k entry；LoCoMo 1,540（Primary，residual cohort 在此量化）+
LongMemEval-S 500（Cross-Benchmark Guard，后补）

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| 原则 | 判定 | 依据 |
|---|---|---|
| I. Local-first、offline 默认 | PASS | 训练数据本地合成/公共许可语料、训练本地 GPU、Planner 自托管 sidecar；无任何托管依赖 |
| II. Engine/adapter 分离 | PASS | 训练产物经 `cmd/locomo-bench/local_planner.go`（adapter）接入；`memory/evidencecompiler` 不动；engine 零改动 |
| III. Contract-first、namespace 隔离 | PASS | 只消费 022 冻结 proposal 合同；Planner 无 Store/Search/Bundle 写/answer 权限；训练资产零 namespace 数据 |
| IV. 评测回归门 | **PENDING** | 三臂配对必须确认 deterministic control 不回归基线，且 supervised 增益统计显著，否则不 merge |
| V. 优雅降级 & 诚实规模 | PASS | 无 Planner/超时/崩溃 → 确定性 Compiler fallback，基础路径继续离线工作；不承诺超 100k entry |

## Project Structure

### Documentation (this feature)

```text
specs/023-local-trained-evidence-compiler/
├── spec.md                # feature spec（含 2026-08-02 Amendment）
├── plan.md                # this file
├── research.md            # Phase 0: 依赖收据 + residual 量化 + 选型决策
├── data-model.md          # Phase 1: 训练样本 schema / proposal 目标标签定义
├── quickstart.md          # Phase 1: 数据构建 + 训练 + 配对跑法
├── contracts/             # Phase 1: 023 增量契约（local_planner 接入形状）
├── model-card.md          # Phase 5: 底模/adapter 摘要、许可、数据版本
└── tasks.md               # Phase 2 (/speckit-tasks)
```

### Source Code (repository root)

```text
# 继承 022 资产（不改引擎契约）
memory/evidencecompiler/          # 022 已冻结
cmd/locomo-bench/eval_fixed_gold_oracle.go  # 022 已实现

# 023 增量
cmd/locomo-bench/
├── local_planner.go              # [023 前置补全 022 T070] 可替换本地 Planner adapter（sidecar 接入）
├── local_planner_test.go         # [023] mock sidecar + fail-closed 测试
└── planner_eval_bridge.go        # [023] 三臂配对：planner 产物 → 022 validator → formal replay
training/planner/                 # [023] Python 训练 pipeline（独立于 Go build）
├── data_build.py                 #   合成对话生成 + 公共语料改造 + engram 自举 → 训练样本
├── label.py                      #   双标签 + 独立裁决 + 人审抽样导出
├── train_lora.py                 #   TRL SFT + LoRA（QLoRA 降级）
├── serve.sh                      #   vllm/ollama sidecar 启动脚本
└── configs/                      #   冻结的训练配置（data/build/train/eval 摘要）
```

**Structure Decision**: 引擎层零改动。训练是独立 Python 工具链（不进 CGO 门）；Go 侧只加
`cmd/locomo-bench/local_planner.go`（Planner adapter）+ 配对桥。Planner 模型经本地 sidecar
（vllm/ollama，复用 `provider` 抽象）调用，与 embedder/LLM 的 sidecar 模式一致，保持离线可退化。

## Complexity Tracking

> **Fill ONLY if Constitution Check has violations that must be justified**

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| 无 | 引擎不动；训练侧 Python 独立、不进 Go 构建；adapter 是 harness 层 | — |

## Phases

### Phase 0: 依赖收据 + residual 量化 + 选型（前置，无 GPU）

- **P0.1 依赖收据（最小充分集）**：记录 022 Compiler/Planner 合同版本与摘要、LoCoMo B1
  （85.19%，`sha256:263b52b6…`）、fixed-gold oracle 可用性、`local_planner.go` 缺口状态 →
  产出唯一 `READY` verdict（Amendment 1 定义的门）。缺失件即后续 P0.2/P0.3。
- **P0.2 local_planner.go 补全（TDD）**：实现 022 T070 定义的 Planner adapter——sidecar 接入、
  只提议 Need/actions、无 Store/answer 权限、超时/崩溃 fail-closed。engine 零改动
  （`git diff --name-only -- memory embedding provider store internal` = 空）。
- **P0.3 Primary Cohort residual 量化**：用 LoCoMo B1 正式协议 + fixed-gold oracle 复算，
  冻结 compiler-eligible cohort（candidate evidence 足够、oracle 可答、确定性 Compiler 未答对），
  产出逐题清单 + 类别分布 + 相对 85.19% 的短差。residual 若为空 → 记 `NOT_NEEDED`，不训练。
- **P0.4 底模冻结**：Qwen2.5-7B-Instruct（Apache-2.0）为默认；validation 冻结前可换同族
  3B/1.5B。记录 tokenizer/chat-template 摘要。

### Phase 1: 设计与契约

- **D1**：训练样本 schema（query + 冻结 candidate 摘要 + source lineage + 期望 Need/actions +
  来源/许可/split/构建版本 + 内容摘要）→ data-model.md
- **D2**：proposal 目标标签如何从 fixed-gold oracle + 规则确定（无需新 LLM judge，避免教师依赖）
- **D3**：local_planner.go 接入形状（sidecar URL、合同版本校验、模型摘要核对）→ contracts/
- **D4**：三臂配对协议（deterministic / prompt-only / supervised，同 store、候选逐字节一致）

### Phase 2: 数据构建（离线，无 GPU 或少量）

- 主路径：本地 Qwen2.5-7B-Instruct 生成**虚构多会话记忆对话**（人物/项目/时间线/更新/跨会话
  引用）→ 灌 engram 离线提取/建索引 → 生成 query（direct/time/multi-hop/update 类）→
  fixed-gold oracle + 规则 → 期望 proposal
- 辅路径：OASST1（Apache-2.0）/ ultrachat_200k（MIT）真实对话改造，跑同一 pipeline
- 双独立标签 + 独立裁决（不一致则排除）；≥200 分层随机样本人审，语义充分率 ≥95%、95% CI
  下界 ≥90%（FR-009）
- 审计：provenance/许可/污染/近重复/privacy 全绿；LoCoMo/LongMemEval test 内容、任何
  namespace 数据、付费 teacher 零进入（FR-011/013/014）
- 确定性重建：同输入构建两次，样本/split/摘要一致率 100%（FR-010）

### Phase 3: 训练（租用 24 GiB 单卡）

- TRL SFT + LoRA（QLoRA 降级备选）；seq ≤ 2048；LoRA r=16，target q/k/v/o
- 单 epoch，样本量由 P0.3 residual 与数据构建量决定（目标数千~数万级）
- 冻结训练配置（FR-015）：输入摘要、底模摘要、config、随机性、输出摘要、完成状态
- 一次正式重建 ≤ 24 GPU-hours：训练 ~8–12h + 数据验证 + 冻结重放，余量缓冲

### Phase 4: 集成 + 三臂配对评测

- supervised 臂：合并 LoRA → 本地 sidecar（vllm）→ local_planner.go → 022 validator
- 配对：LoCoMo B1 协议、同 store、候选逐字节一致，deterministic / prompt-only / supervised
  三臂，validity 全绿（candidate/source/span/citation/within-cap、answerer=1、retrieval=0）
- 统计：Primary Cohort majority Δ、exact McNemar（多重校正 p<0.05）、Guard overall、
  类别 non-regression、validity 阻塞项
- verdict：GO/HOLD/STOP/INVALID（FR-029 闭包）；每阶段独立 verdict（FR-030）

### Phase 5: Promotion verdict + 资产

- 产品推荐门（FR-031）：全量正确题严格 > deterministic control 且双基准/保护类别 non-regression
- model-card.md + data card（FR-022/032/034）：合同版本、权重/adapter 摘要、tokenizer、
  底模许可（Apache-2.0）、数据版本、24 GiB/24 GPU-hours/p95 实测
- 结果登记到 `docs/evaluation/results.md` / `experiment-verdicts.md`（仅 GO 才进推荐，否则研究产物）
- SaaS 方向（95+/模型助手）为独立后续计划，本 feature 不承载（Amendment 5）

## Key Technical Decisions

1. **底模 = Qwen2.5-7B-Instruct（Apache-2.0）**：许可最宽松（无 gating/商用上限），产物可分发
   可推荐（FR-022 要求可分发许可）；7.6B BF16 ~15 GiB，单卡 24 GiB LoRA/QLoRA 训练 + p95≤2s
   推理均可行；同族 3B/1.5B 可降级且 tokenizer 一致。Llama-3.1/Gemma-2 gated + 商用条款受限，
   不满足可分发要求。
2. **Planner 接入 = 本地 sidecar（vllm/ollama），复用 `provider` 抽象**：与 embedder/LLM 的
   sidecar 模式一致；`local_planner.go` 只做合同校验 + fail-closed 转发，不内嵌推理（无 CGO）。
   模型缺失/超时/崩溃 → 确定性 Compiler fallback（FR-019）。
3. **训练数据 = 本地合成自举为主 + 公共许可语料为辅**：合成虚构记忆对话完全受控、零泄漏、
   离线；公共语料（OASST1/ultrachat_200k）提供真实对话多样性。目标标签用 fixed-gold oracle
   + 规则确定，不引入付费 teacher（FR-013）。
4. **依赖补全前置化**：`local_planner.go`（022 T070 缺口）与 residual cohort 量化作为 023
   Phase 0 前置任务补全，而不是等 022 收尾——这是 maintainer 2026-08-02 决策（Amendment 1），
   引擎零改动，归因风险如实记录（双基准前不得跨基准外推）。
5. **配对纪律（025/026 教训）**：三臂必须同 store、候选逐字节一致、只差训练状态；报告
   candidate oracle 区分 compiler miss vs candidate miss；小 delta 不单独作 promotion 依据。
6. **训练是 Python 工具链，不进 CGO 门**：`training/planner/` 独立于 Go build；Go 侧 CI 只验
   证 engine 测试 + local_planner mock 测试 + 配对桥的 byte-replay。
