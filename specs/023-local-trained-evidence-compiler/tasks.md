# Tasks: 本地训练式 Evidence Planner（023）

依赖有序。`[P]` = 该任务的 blocker 必须先完成。`[X]` = 完成标记（含 SHA 或日期）。
引擎零改动硬门：`git diff --name-only -- memory embedding provider store internal` 必须为空
（除显式 contract increment）。

## Phase 0 — 依赖收据 + 补全 + residual 量化（无 GPU）

- [ ] T001 [P] 生成 Dependency Receipt（最小充分集，Amendment 1）：记录 022 Compiler/Planner
  合同版本与摘要、LoCoMo B1（85.19%，`sha256:263b52b6…`）、fixed-gold oracle 可用性、
  `local_planner.go` 缺口状态、Primary Cohort residual 量化结果 → 唯一 `READY`/
  `NOT_NEEDED`/`NOT_ELIGIBLE`/`BLOCKED` verdict 到 `research.md`。
- [ ] T002 [P] 实现 `cmd/locomo-bench/local_planner.go`（补全 022 T070）：可替换本地 Planner
  adapter——sidecar 接入（vllm/ollama，复用 provider 抽象）、只提议 Need/actions、无
  Store/Search/Bundle 写/answer 权限、超时/崩溃/合同版本漂移 fail-closed 退回确定性 Compiler。
  先写失败测试（`local_planner_test.go`，mock sidecar + 越权/非法/无来源/超时夹具）。
- [ ] T003 [P] 量化 Primary Cohort residual：LoCoMo B1 正式协议 + fixed-gold oracle 复算，
  冻结 compiler-eligible cohort（evidence 足够 + oracle 可答 + deterministic 未答对），产出逐题
  清单 + 类别分布 + 短差（相对 85.19%）。residual 为空 → 记 `NOT_NEEDED` 并停。
- [ ] T004 [P] 冻结底模：Qwen2.5-7B-Instruct（Apache-2.0），记录 tokenizer/chat-template
  摘要；validation 冻结前可换同族 3B/1.5B。

## Phase 1 — 设计与契约

- [ ] T005 写 `data-model.md`：训练样本 schema（query + 冻结 candidate 摘要 + source lineage +
  期望 Need/actions + 来源/许可/split/构建版本 + 内容摘要）；proposal 目标标签由 fixed-gold
  oracle + 规则确定（无新 LLM judge）。
- [ ] T006 写 `contracts/`：local_planner 接入形状（sidecar URL、合同版本校验、模型摘要核对、
  超时语义、cancellation 传播）。
- [ ] T007 冻结三臂配对协议：deterministic / prompt-only / supervised；同 store、候选逐字节
  一致、只差训练状态；validity 与统计判据（FR-028/029）。

## Phase 2 — 数据构建（离线）

- [ ] T008 合成数据 pipeline：本地 Qwen2.5-7B-Instruct 生成虚构多会话记忆对话（人物/项目/
  时间线/更新/跨会话引用）→ 灌 engram 离线提取/建索引 → 生成 query（direct/time/multi-hop/
  update）→ oracle + 规则 → 期望 proposal。到 `training/planner/data_build.py`。
- [ ] T009 公共语料辅路径：OASST1（Apache-2.0）/ ultrachat_200k（MIT）改造，跑同一 pipeline；
  逐语料确认许可与隐私。
- [ ] T010 双独立标签 + 独立裁决：两判不一致 → 裁决唯一，否则排除（FR-009）。
- [ ] T011 人审：≥200 分层随机样本（不足 200 全量），语义充分率 ≥95%、95% CI 下界 ≥90%。
- [ ] T012 审计：provenance/许可/污染/近重复/privacy 全绿；LoCoMo/LongMemEval test 内容、
  任何 namespace 数据、付费 teacher 零进入（FR-011/013/014）。
- [ ] T013 确定性重建验证：同输入构建两次，样本/split/全局摘要一致率 100%（FR-010）。

## Phase 3 — 训练（租用 24 GiB 单卡）

- [ ] T014 [P] 训练环境：租机数据盘布局（`/root/autodl-tmp/023-runs/`）、Python venv、TRL/
  PEFT、vllm/ollama sidecar；系统盘只放代码（<30G 纪律）。
- [ ] T015 supervised 臂训练：TRL SFT + LoRA（QLoRA 备选），seq ≤2048，r=16，单 epoch；冻结
  配置（数据摘要/底模摘要/config/随机性/输出摘要/完成状态，FR-015）；一次重建 ≤24 GPU-hours。
- [ ] T016 prompt-only 对照：同底模零训练，仅 prompt 模板；与 supervised 除训练状态外全同。
- [ ] T017 合并 LoRA → 冻结推理产物（含权重/adapter 摘要、tokenizer 摘要、合同版本、许可）。

## Phase 4 — 集成 + 三臂配对评测

- [ ] T018 `cmd/locomo-bench/planner_eval_bridge.go`：supervised/prompt-only 臂产物 → 022
  validator → formal replay；候选逐字节一致校验（FR-025/SC-007）。
- [ ] T019 三臂配对评测：LoCoMo B1 协议、同 store；validity 全绿（candidate/source/span/
  citation/within-cap、answerer=1、retrieval=0、无 IDK retry）。
- [ ] T020 统计 + verdict：Primary Cohort majority Δ（≥+2.0pp）、exact McNemar（多重校正
  p<0.05）、Guard overall（≥−0.5pp）、类别 non-regression、validity 阻塞项 → GO/HOLD/STOP/
  INVALID（FR-029 闭包、FR-030 每阶段独立 verdict）。

## Phase 5 — Promotion verdict + 资产

- [ ] T021 产品推荐门（FR-031）：全量正确题严格 > deterministic control、双基准/保护类别
  non-regression；不满足 → 研究产物，不进推荐。
- [ ] T022 写 `model-card.md` + data card（FR-022/032/034）：合同版本、权重/adapter 摘要、
  tokenizer、底模许可（Apache-2.0）、数据版本、24 GiB/24 GPU-hours/p95 实测、诚实边界。
- [ ] T023 结果登记：GO → `docs/evaluation/results.md` + `experiment-verdicts.md`（023 从
  未裁决区移入裁决表）；非 GO → 归档为研究产物并如实记录。

## 硬门（贯穿）

- 引擎零改动：每次改动后 `git diff --name-only -- memory embedding provider store internal` 为空。
- 全量测试：`CGO_ENABLED=0 go test -count=1 ./...` 绿。
- 配对纪律：所有正式臂同 store、候选逐字节一致、只差训练状态；报告 candidate oracle 区分
  compiler miss vs candidate miss。
- 诚实报告：residual 归因在双基准收口前基于单基准（LoCoMo），任何结果不得跨基准外推；
  SaaS/95+/模型助手为独立后续计划，本 feature 不承载。
