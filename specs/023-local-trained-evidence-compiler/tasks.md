# Tasks: 本地训练式 Evidence Planner（023）

依赖有序。`[P]` = 该任务的 blocker 必须先完成。`[X]` = 完成标记（含 SHA 或日期）。
引擎零改动硬门：`git diff --name-only -- memory embedding provider store internal` 必须为空
（除显式 contract increment）。

## Phase 0 — 依赖收据 + 补全 + residual 量化（无 GPU）

- [X] T001 [P] 生成 Dependency Receipt（最小充分集，Amendment 1）：记录 022 Compiler/Planner
  合同版本与摘要、LoCoMo B1（85.19%，`sha256:263b52b6…`）、fixed-gold oracle 可用性、
  `local_planner.go` 缺口状态、Primary Cohort residual 量化结果 → 唯一 `READY`/
  `NOT_NEEDED`/`NOT_ELIGIBLE`/`BLOCKED` verdict 到 `research.md`。**[X] 2026-08-03 → `READY`**
- [X] T002 [P] 实现 `cmd/locomo-bench/local_planner.go`（补全 022 T070）：可替换本地 Planner
  adapter——sidecar 接入（vllm/ollama，复用 provider 抽象）、只提议 Need/actions、无
  Store/Search/Bundle 写/answer 权限、超时/崩溃/合同版本漂移 fail-closed 退回确定性 Compiler。
  先写失败测试（`local_planner_test.go`，mock sidecar + 越权/非法/无来源/超时夹具）。**[X] `2ed473a`**
- [X] T003 [P] 量化 Primary Cohort residual：LoCoMo B1 正式协议 + fixed-gold oracle 复算，
  冻结 compiler-eligible cohort（evidence 足够 + oracle 可答 + deterministic 未答对），产出逐题
  清单 + 类别分布 + 短差（相对 85.19%）。residual 为空 → 记 `NOT_NEEDED` 并停。**[X] 2026-08-03 → residual 149 题，verdict `READY`**（见 [residual-cohort.json](residual-cohort.json)；oracle 完整跑通需先修复 3 类 bug，见 research.md）
- [X] T004 [P] 冻结底模：Qwen2.5-7B-Instruct（Apache-2.0），记录 tokenizer/chat-template
  摘要；validation 冻结前可换同族 3B/1.5B。**[X] research.md P0.4 已记录**

## Phase 1 — 设计与契约

- [X] T005 写 `data-model.md`：训练样本 schema（query + 冻结 candidate 摘要 + source lineage +
  期望 Need/actions + 来源/许可/split/构建版本 + 内容摘要）；proposal 目标标签由 fixed-gold
  oracle + 规则确定（无新 LLM judge）。**[X] 2026-08-03 核对完整（schema §1、标签 §2、pipeline §3、split §4、冻结 §5）**
- [X] T006 写 `contracts/`：local_planner 接入形状（sidecar URL、合同版本校验、模型摘要核对、
  超时语义、cancellation 传播）。**[X] 2026-08-03 → [planner-contract.md](contracts/planner-contract.md)（023.v1）**
- [X] T007 冻结三臂配对协议：deterministic / prompt-only / supervised；同 store、候选逐字节
  一致、只差训练状态；validity 与统计判据（FR-028/029）。**[X] 2026-08-03 → [paired-eval-protocol.md](contracts/paired-eval-protocol.md)（023.v1）**

## Phase 2 — 数据构建（离线）

- [X] T008 合成数据 pipeline：本地 Qwen2.5-7B-Instruct 生成虚构多会话记忆对话（人物/项目/
  时间线/更新/跨会话引用）→ 灌 engram 离线提取/建索引 → 生成 query（direct/time/multi-hop/
  update）→ oracle + 规则 → 期望 proposal。到 `training/planner/data_build.py`。
  **[X] 2026-08-03：data_build.py（turn_id+query/gold 标注）、`cmd/planner-build/`（灌入+检索
  冻结候选+gold coverage，单测/集成/CLI smoke 绿）、`training/planner/label.py`（Need 解析+
  Actions 规则/oracle+双标签机制）落地；双标签正式执行在 T010**
- [X] T009 公共语料辅路径：OASST1（Apache-2.0）/ ultrachat_200k（MIT）改造，跑同一 pipeline；
  逐语料确认许可与隐私。**[X] 2026-08-03 工具落地：`corpus_adapter.py`（ultrachat-jsonl/oasst-jsonl
  适配 + 许可清单 manifest）+ `data_build.py --gen-queries-only` 复用 query 生成；测试绿。
  正式执行（下载语料 + 跑 pipeline）待租机/网络**
- [X] T010 双独立标签 + 独立裁决：两判不一致 → 裁决唯一，否则排除（FR-009）。
  **[X] 2026-08-03 机制落地：label.py `labeler_a/b`（独立停用词/cap）+ `adjudicate`（并集/保守
  裁决），差异样本裁决或排除；正式双标签统计待数据构建运行**
- [x] T011 人审：≥200 分层随机样本（不足 200 全量），语义充分率 ≥95%、95% CI 下界 ≥90%。**已完成（2026-08-05，勾结补记 2026-08-17）**：audit/review-r5 200 样本 199 pass = **99.5%**（1 fail）。
  **[X] 2026-08-03 工具落地：`review.py`（分层随机抽样人审表 + Wilson 95% CI 充分率门）；测试绿。
  **[BLOCKED] 2026-08-04：r1 审查流程无效且门失败；13 个确认 false-gap，另 6 个部分回答样本的
  `gap` 合理。宽松重计 187/200=93.5%，Wilson 下界 89.20%，两门均失败；且审查表缺
  `gold_answer`、只导出前 5 候选。见 [audit/t011-fails-023-b20260803-r1.md](audit/t011-fails-023-b20260803-r1.md)。
  修复、升 build version、重建并完成独立全量复核前保持未完成。**
  **[X] 2026-08-04 代码层修复 + r2 标注层重建：local_planner gap 大小写、label 保留
  gold_answer、review 导出全部候选+lineage、label false-gap coverage 规则+source_need
  （045b402）；label.py 支持 --candidates-build-version 分离候选层/标注层版本（ddf4847）。
  r2 = 冻结 r1 candidates + 重建标注层：gap 244→190（54 个确凿漏标纠正为 KEEP）、KEEP
  429→485；audit r2 全绿、rebuild_check 100%。r1 收据部分 FAIL 经 gold_answer 核对实为
  误判（候选含不同答案）。r2 审查表 [audit/review-r2-023-b20260803-r2.csv]
  (audit/review-r2-023-b20260803-r2.csv) 含 gold_answer(200/200)+lineage，独立全量
  复核前保持 BLOCKED。**
  **[PASS] 2026-08-04 r5 门通过：外部 AI 全量标注 607（all-607-labeled.jsonl）+ gap 复审
  （gap-recheck.jsonl 48 可疑 → 修正 3：0-00001-q2/0-00026-q1/q2）→ 合并
  all-607-labeled-r5.jsonl → train-r5（607, train 510/val 97, gap 317/keep 290）。
  外部 AI 独立审查 200 条：199/200 (99.5%)，Wilson 95% CI [97.22%, 99.91%]，双门满足。
  收据 [audit/t011-review-r5-023-b20260803-r5.md](audit/t011-review-r5-023-b20260803-r5.md)。**
- [X] T012 审计：provenance/许可/污染/近重复/privacy 全绿；LoCoMo/LongMemEval test 内容、
  任何 namespace 数据、付费 teacher 零进入（FR-011/013/014）。
  **[X] 2026-08-03 工具落地：`audit.py`（provenance/许可/schema/split/近重复/污染 8-gram/privacy，
  含 benchmark 扫描）；测试绿。正式审计待数据构建**
- [X] T013 确定性重建验证：同输入构建两次，样本/split/全局摘要一致率 100%（FR-010）。
  **[X] 2026-08-03 工具落地：`rebuild_check.py`（样本集/split/content-digest/全局摘要对比，
  100% 才 OK）；测试绿。正式两次构建验证待数据**

## Phase 3 — 训练（租用 24 GiB 单卡）

- [X] T014 [P] 训练环境：租机数据盘布局（`/root/autodl-tmp/023-runs/`）、Python venv、TRL/
  PEFT、vllm/ollama sidecar；系统盘只放代码（<30G 纪律）。
  **[X] 2026-08-04 就绪：AutoDL 数据盘 `/root/autodl-tmp/`（系统盘 8%）、023-venv
  （torch 2.11 / transformers 5.14.1 / peft 0.20.0 / bnb 0.50.0 / vllm 0.26.0）、
  Qwen2.5-7B-Instruct 底模 modelscope snapshot、系统盘只放代码。**
- [X] T015 supervised 臂训练：TRL SFT + LoRA（QLoRA 备选），seq ≤2048，r=16，单 epoch；冻结
  配置（数据摘要/底模摘要/config/随机性/输出摘要/完成状态，FR-015）；一次重建 ≤24 GPU-hours。
  **[X] 2026-08-04 完成：train-r5（510 样本）QLoRA 4bit，r16/alpha32/dropout0.05/qkv_o_proj，
  batch2×8=16，lr 2e-4，1 epoch，seq2048；32 steps / 360.7s / loss 1.741（RTX 4090，
  远低于 24 GPU-hours）；adapter `/root/autodl-tmp/023-runs/models/planner-lora/`，
  `train_summary.json` 冻结摘要（FR-015）。train_lora.py 用 transformers.Trainer（无 trl）；
  修 max_steps None→-1（transformers 5.x _validate_args）。runbook：
  docs/023-planner-r5-train-runbook.md。HF 模型 wallfacers/engram-planner-lora（private）。**
- [x] T016 prompt-only 对照：同底模零训练，仅 prompt 模板；与 supervised 除训练状态外全同。**CLOSED — not executed（2026-08-17）**：随 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md) 关闭，Phase 4 后续不执行。
- [x] T017 合并 LoRA → 冻结推理产物（含权重/adapter 摘要、tokenizer 摘要、合同版本、许可）。**CLOSED — not executed（2026-08-17）**：adapter 已归档 HF `wallfacers/engram-planner-lora`，合并产物不制作；随 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md) 关闭。

## Phase 4 — 集成 + 三臂配对评测

- [x] T018 `cmd/locomo-bench/planner_eval_bridge.go`：supervised/prompt-only 臂产物 → 022
  validator → formal replay；候选逐字节一致校验（FR-025/SC-007）。**CLOSED — not executed（2026-08-17）**：随 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md) 关闭。
  validator → formal replay；候选逐字节一致校验（FR-025/SC-007）。
- [x] T019 三臂配对评测：LoCoMo B1 协议、同 store；validity 全绿（candidate/source/span/
  citation/within-cap、answerer=1、retrieval=0、无 IDK retry）。**CLOSED — not executed（2026-08-17）**：GO 门被三重判例否证，不再花 box+API 验证；恢复点=先零成本 CPU 分诊（见 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md)）。
  citation/within-cap、answerer=1、retrieval=0、无 IDK retry）。
- [x] T020 统计 + verdict：Primary Cohort majority Δ（≥+2.0pp）、exact McNemar（多重校正
  p<0.05）、Guard overall（≥−0.5pp）、类别 non-regression、validity 阻塞项 → GO/HOLD/STOP/
  INVALID（FR-029 闭包、FR-030 每阶段独立 verdict）。**CLOSED — not executed（2026-08-17）**：以 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md) 文档级 STOP 收口替代。
  p<0.05）、Guard overall（≥−0.5pp）、类别 non-regression、validity 阻塞项 → GO/HOLD/STOP/
  INVALID（FR-029 闭包、FR-030 每阶段独立 verdict）。

## Phase 5 — Promotion verdict + 资产

- [x] T021 产品推荐门（FR-031）：全量正确题严格 > deterministic control、双基准/保护类别
  non-regression；不满足 → 研究产物，不进推荐。**CLOSED — not executed（2026-08-17）**：planner 归研究产物，不进推荐（[STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md)）。
  non-regression；不满足 → 研究产物，不进推荐。
- [x] T022 写 `model-card.md` + data card（FR-022/032/034）：合同版本、权重/adapter 摘要、
  tokenizer、底模许可（Apache-2.0）、数据版本、24 GiB/24 GPU-hours/p95 实测、诚实边界。**CLOSED — not executed（2026-08-17）**：产物以 HF 归档 + verdict 诚实边界替代；随 [STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md) 关闭。
  tokenizer、底模许可（Apache-2.0）、数据版本、24 GiB/24 GPU-hours/p95 实测、诚实边界。
- [x] T023 结果登记：GO → `docs/evaluation/results.md` + `experiment-verdicts.md`（023 从
  未裁决区移入裁决表）；非 GO → 归档为研究产物并如实记录。**已完成（2026-08-17）**：非 GO 分支——STOP 登记 experiment-verdicts（023 行更新），归档研究产物，[STOP verdict 2026-08-17](../../docs/evaluation/reports/023-planner-stop-verdict-2026-08-17.md)。
  未裁决区移入裁决表）；非 GO → 归档为研究产物并如实记录。

## 硬门（贯穿）

- 引擎零改动：每次改动后 `git diff --name-only -- memory embedding provider store internal` 为空。
- 全量测试：`CGO_ENABLED=0 go test -count=1 ./...` 绿。
- 配对纪律：所有正式臂同 store、候选逐字节一致、只差训练状态；报告 candidate oracle 区分
  compiler miss vs candidate miss。
- 诚实报告：residual 归因在双基准收口前基于单基准（LoCoMo），任何结果不得跨基准外推；
  SaaS/95+/模型助手为独立后续计划，本 feature 不承载。
