# Tasks: Memory-Specific Reranker Training(记忆专用重排序模型)

**Input**: Design documents from `/specs/037-memory-reranker-training/`

**Prerequisites**: plan.md (required), spec.md (required for user stories), research.md, data-model.md, contracts/, quickstart.md

**Tests**: 数据构建 fail-closed 校验（contracts/training-data-schema.md）与训练三方排序一致性（review C2）是 spec/契约的明确要求，作为任务落地；训练脚本本身走"数据先校验、smoke 先跑"的验证式开发。

**Organization**: 按 user story 分组；引擎零改动（宪法 II）是本 feature 的贯穿硬门，Polish 阶段有 git diff 验证任务。

## Format: `[ID] [P?] [Story] Description`

- **[P]**: 可并行（不同文件、无依赖）
- **[Story]**: US1/US2/US3
- 训练工具路径统一在 `specs/037-memory-reranker-training/tools/`

---

## Phase 1: Setup (Shared Infrastructure)

**Purpose**: 训练工具链 scaffold + 数据源就位

- [x] T001 创建训练工具链骨架 `specs/037-memory-reranker-training/tools/`：`requirements.txt`（torch / transformers≥4.51 / peft / datasets / vllm）、空模块占位（`build_training_data.py` / `train_reranker.py` / `test_*.py` / 脚本说明头）
- [x] T002 [P] 从 HF 拉取 MSC Multi-Session Chat 数据集并**冻结镜像/revision/许可**（`gonced8/multi-session_chat` 或 `nayohan/multi_session_chat`），存 AutoDL 数据盘或本地缓存，记录到 `research.md` R3b（query 需启发式派生的数据形态确认）
- [x] T003 [P] 本地 WSL2 Python 3.11+ 环境冒烟（`python3 -c "import torch, transformers, peft; ..."` 通过；记录版本到 `tools/requirements.txt` 注释），确认评测侧 Go 1.25 + locomo-bench 可 `go build ./cmd/locomo-bench`

---

## Phase 2: Foundational (Blocking Prerequisites)

**⚠️ CRITICAL**: 无这些基础，任何 user story 的评测/训练都不可信（review C6/D3 冻结项）

- [x] T004 实测冻结 vLLM serving Qwen3-Reranker-0.6B：处理 vllm#19229 坑（`--runner pooling` + HF overrides + 专用 Jinja 模板），产出**可复现的 serve 命令 + 模型 revision**，写入 `contracts/rerank-serving.md`（不允许"假设原生支持"）
- [x] T005 [P] 评测 preflight 脚本 `tools/preflight.py`：探活 `/v1/rerank` + 请求成功/失败计数，**零成功即标记 INVALID**（禁止 retriever.go:707-710 静默回退后出报告）；与 locomo-bench run-dir 关联
- [x] T006 [P] 预算记账/watchdog 脚本 `tools/budget_watchdog.py`：跨 run 累计 GPU 时/费用（`¥100 / 8 GPU·时` 硬门，超限自动停）+ 每 run manifest（seed / hash / exit status / e2e report ID 关联，review D3/F）
- [x] T007 确认评测基线命令：`EMBED_RERANK_MODEL` + `EMBED_BASE_URL`（**无 `EMBED_RERANK_BASE_URL`**）+ `--retrieval 'hybrid,hybrid+rerank'`（**无 `--arm`**，main.go:1639,3058）；记录 `git diff --name-only -- memory embedding provider store internal` 为空作为引擎零改动基线

**Checkpoint**: Foundation ready —— US1 可零成本启动；US2 训练链前置就绪

---

## Phase 3: User Story 1 - 现成轻量重排模型的记忆场景端到端基准 (Priority: P1) 🎯 MVP

**Goal**: 现成 Qwen3-Reranker-0.6B base 在 LoCoMo 全量配对下的记忆场景 e2e 现状基线（零训练成本）

**Independent Test**: locomo-bench 配对表（`hybrid` vs `hybrid+rerank`，全量 1540）+ 四类别 + McNemar + flip + paired CI + vs 008 对比；preflight 确认 rerank 调用数 > 0

- [x] T008 [P] [US1] 起 transformers HTTP serve Qwen3-Reranker-0.6B base（vLLM cu13 不兼容 CUDA 12.8 驱动，改用 FastAPI + transformers；同时加载 BGE-small 供 embedding），写 `rerank_server.py` 至远程 `/root/autodl-tmp/037-reranker/`
- [x] T009 [US1] 跑 locomo-bench 全量配对：`--run-dir /root/autodl-tmp/.locomo-run/037-us1 --retrieval 'hybrid,hybrid+rerank'`（SeetaCloud 224 vCPU 远程直跑，零 SSH 隧道）
- [x] T010 [US1] 生成 US1 配对报告 → `specs/037-memory-reranker-training/reports/us1-paired.md`
- [x] T011 [US1] US1 报告校验：preflight rerank OK、四类别含 temporal、与 008 对比已附
- [x] 2026-08-12 补充：机器从 AutoDL 迁移至 SeetaCloud（CUDA 驱动 12.8 ↔ vLLM cu13 不兼容），已用 transformers 替代方案绕过

**Checkpoint**: US1 落地 = 本 feature 的**零成本 MVP**（半天出结果），是 US2 训练的对照臂与动机证据

---

## Phase 4: User Story 2 - 记忆专用重排模型训练与端到端验证 (Priority: P1)

**Goal**: 训练 0.6B 记忆专用重排模型，全量端到端配对为唯一 GO 门（008 铁律）

**Independent Test**: 跨 run 配对（US1 vs US2 `--compare`）：总体不劣（non-inferiority 预注册）+ temporal 不劣（修复 008 −9）+ heldout/LME 泛化否决门

### 训练数据与审计（R7 → 数据构建）

- [x] T012 [P] [US2] R7 temporal 可判别性审计 `tools/audit_temporal_payload.py`：从真实 baseline top-pool 导出 ≥50 组 `Entry.Content` payload，判定文本可见时间信号是否足以区分"答案时间窗口内/外"（research R7）；产出审计报告，冻结相似度模型/revision/阈值/时间窗口/假负例排除规则（**未过 audit 的样本不得标 temporal_label**）
- [x] T013 [US2] 数据构建 `tools/build_training_data.py`：LoCoMo evidence → **multi-positive 三元组**（同一 qa 全量 positives，group-aware）+ MSC 派生（persona-recall / cross-session）+ 负采样（in-dialogue / temporal-hard / cross-session，**同 group 正例不互作负例**）+ 真实 conv ID split（train: conv-26/30/41/42/43/44/47；heldout: conv-48/49/50）+ `schema_version`/`qa_id`/`query_group_id`/`split` 行字段 + `--seed`
- [x] T014 [US2] 数据校验 `tools/test_training_data.py`：fail-closed 执行 [training-data-schema](contracts/training-data-schema.md) 全部 9 条规则（含 multi-positive 整组拒绝、temporal-hard 文本可见信号、split 隔离）+ 无 evidence 的 4 题与异常 evidence 引用进拒绝 ledger

### 训练

- [x] T015 [US2] 训练脚本 `tools/train_reranker.py`：两段式 LoRA（stage1 BCE pointwise sigmoid 回归 label → stage2 InfoNCE listwise multi-positive mask），score_head/template/截断**冻结唯一实现**（`yes_logit−no_logit` 与新增 scalar head 二选一并记录），seed 固定，manifest 写 hash/超参/依赖版本
- [x] T016 [P] [US2] 训练 smoke 校验 `tools/test_train_smoke.py`：1 epoch 收敛 + **训练/合并 checkpoint/vLLM `/v1/rerank` 三方排序一致性** + score 分布不过度聚簇（008 BGE 左偏反例）+ 峰值 VRAM/tok/s/p95 长度实测（200/1000 样本，外推全量预算）
- [x] T017 [US2] AutoDL 全量训练三 checkpoint（`base` / `bce` / `bce-infonce`）到 `/root/autodl-tmp/037-reranker/<ckpt>/`，预算 watchdog（T006）门禁，seed 复现验证（**烧钱必须有测量**，spec Clarifications Q2）

### 端到端验证（GO 门）

- [ ] T018 [US2] serve 训练产物（transformers 路线，非 vLLM）：**2026-08-12 冻结**——vLLM cu13↔CUDA 12.8 不兼容（US1 T008）+ vLLM serve 训练产物 bug（merged 分数=base / --enable-lora 冲突），改用 `tools/rerank_server.py`（FastAPI 聚合 /v1/rerank + /v1/embeddings，score equation 与训练冻结一致）serve merged checkpoint（T017 已在 AutoDL merge 完），写 `tools/serve_trained.sh`；**起 serve + preflight 冒烟待远程验证**
- [ ] T019 [US2] 跑 US2 locomo-bench 全量配对：`--run-dir ./.locomo-run/037-us2 --retrieval 'hybrid,hybrid+rerank'`（三 checkpoint 各自或择优）
- [ ] T020 [US2] 跨 run 配对 `--compare ./.locomo-run/037-us1 ./.locomo-run/037-us2`：**GO 门判定** = 总体不劣（non-inferiority margin 预注册，p>0.05 本身不证明不劣）+ temporal 类不劣（修复 008 −9）+ 分类逐题 McNemar + flip
- [ ] T021 [US2] 泛化否决门：留出对话（conv-48/49/50）+ LongMemEval 500 交叉子集评测，任一不过 → 不得宣称"未见对话/跨数据集"泛化

**Checkpoint**: US2 GO 门判定完成；显著转正才触发 SC-005 的"是否发布为 opt-in sidecar"决策（**cross-encoder 永不进本地默认栈**）

---

## Phase 5: User Story 3 - 产物与接入形态评估（对标 memos-reranker 服务线）(Priority: P2)

**Goal**: 模型卡 + 成本账 + 接入形态（本地 opt-in sidecar / SaaS 单列）

**Independent Test**: 模型卡（TrainingConfig 字段齐全 + hash manifest）、成本表（对标 0.5/2 元每万 token）、本地/SaaS 分数归属线分离

- [ ] T022 [P] [US3] 模型卡 `specs/037-memory-reranker-training/reports/model-card.md`：参数量/训练数据规模/许可（Apache 2.0）/score_head/template/成本/端到端配对结果 + hash manifest（数据/模板/代码/seed，review F）
- [ ] T023 [P] [US3] 训练 + 推理成本表：训练（GPU 时/费用，预算 watchdog 实账）vs 推理（tok/千次、每 1 万 token 价格、~200ms 延迟参考）对标 memos-reranker 0.5/2 元每万 token（**推理端绝不依赖付费云 reranker**）
- [ ] T024 [US3] 接入形态评估文档：本地自托管 sidecar（default-off opt-in，宪法 I/V）vs SaaS 计费线（**分数单独口径、不回填本地基线**）；4B/SaaS 交付物明确排除在 037 之外（review B3）

---

## Phase 6: Polish & Cross-Cutting Concerns

**Purpose**: 硬门验证 + 全场景复跑 + 结论落档

- [ ] T025 引擎零改动验证：`git diff --name-only -- memory embedding provider store internal` 为空 + `CGO_ENABLED=0 go test -count=1 ./...` 全绿（parity/isolation 测试实际断言）
- [ ] T026 quickstart.md 全场景实跑：场景 1（数据构建校验）→ 场景 2（smoke）→ 场景 3（全量训练）→ 场景 4/5（US1/US2 配对），验证命令与实际 flag/env 一致
- [ ] T027 verdict 落 tracked docs：`docs/evaluation/reports/037-memory-reranker-verdict.md`（GO 门判定、三 checkpoint 消融结果、temporal 表现、成本账、泛化否决门；失败则按 008 收口为"第 N 次证伪"）

---

## Dependencies & Execution Order

### Phase Dependencies

- **Setup (P1)**: 无依赖
- **Foundational (P2)**: 依赖 Setup；阻塞所有 user story
- **US1 (P3)**: 依赖 Foundational（T004/T005/T007）
- **US2 (P4)**: 依赖 Foundational + US1 配对基线（T020 需要 US1 run-dir 作对照）；数据链（T012→T013→T014）与训练链（T015→T016→T017→T018）串行
- **US3 (P5)**: 依赖 US2 产物
- **Polish (P6)**: 依赖全部

### User Story Dependencies

- **US1 (P1)**: Foundational 后可独立跑（**不依赖数据构建**）——零成本 MVP，半天出结果
- **US2 (P1)**: 依赖 US1 的配对基线（T020 跨 run 对照）；训练链数据构建可在 US1 评测期间并行推进
- **US3 (P2)**: 依赖 US2 产物

### Parallel Opportunities

- Setup: T002/T003 并行
- Foundational: T005/T006 并行（T004 先行）
- **US1 与 US2 的数据链并行**：US1 评测跑期间，US2 的 T012（R7 审计）与 T013（数据构建）可同时进行（不同文件、无依赖）
- US2 内：T012 与 US1 并行；T016 独立
- US3: T022/T023 并行

---

## Parallel Example: US1 评测 与 US2 数据构建 并行

```bash
# 线程 A（US1）：起 base vLLM + 跑配对
Task: "tools/serve_base.sh && locomo-bench --run-dir ./.locomo-run/037-us1 ..."

# 线程 B（US2 数据链，本地零 GPU）：
Task: "python3 tools/audit_temporal_payload.py ..."   # T012 R7 审计
Task: "python3 tools/build_training_data.py ..."      # T013 数据构建（依赖 T012 结论）
```

---

## Implementation Strategy

### MVP First (US1 Only)

1. Setup（T001–T003）→ Foundational（T004–T007）
2. **US1（T008–T011）**：零成本现成模型基准落地 = MVP，半天出结果
3. **STOP and VALIDATE**：US1 配对表是否与 008 可比、temporal 是否依旧被害——决定 US2 的动机强度

### Incremental Delivery

1. Setup + Foundational → Foundation ready
2. US1 → 配对表（MVP）
3. US2 数据链（T012–T014）→ 训练链（T015–T018）→ GO 门配对（T019–T021）
4. US3（T022–T024）
5. Polish（T025–T027）：引擎零改动验证 + quickstart 全场景 + verdict 落档

### 关键纪律（贯穿）

- **008 铁律**：只有端到端配对是 GO 门；coverage/NDCG 只作诊断
- **预算硬门**：¥100/8 GPU·时 累计、超限自动停（T006）
- **引擎零改动**：任何任务不触碰 `memory/ embedding/ provider/ store/ internal/`（T007/T025 验证）
- **死亡规则**：推理端自托管；SaaS 单列；cross-encoder 永不进本地默认栈

---

## Notes

- [P] 任务 = 不同文件、无依赖
- [Story] 标签映射到 spec user story
- 每个 checkpoint 可独立验证
- 训练产物、JSONL、日志写 AutoDL 数据盘 `/root/autodl-tmp/` 或 session scratchpad，不落 repo 根
- 提交节奏：每个逻辑任务组一次 commit（用户确认后）；引擎 diff 门禁在 T025 强制验证
