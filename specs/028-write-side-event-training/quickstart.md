# Quickstart: 写入侧事件抽取训练化

**Date**: 2026-08-05 · **Spec**: [spec.md](spec.md) · **Contracts**: [training-data-schema](contracts/training-data-schema.md) · [teacher-extract-prompt](contracts/teacher-extract-prompt.md) · [pair-gate](contracts/pair-gate.md)

本指南是可跑的验证路径。实现细节在 tasks.md；引擎契约与配对口径见 contracts。

## 前置条件

- 027 已收口资产：`memory/eventstore` 引擎、`cmd/locomo-bench` event 配对跑法、84 题子集（`phase0-ids.txt`）、store（009-bge-chunks-store）、本机 `pair_analysis.py`
- AutoDL 机器 + vllm（answerer 35B）+ bge embedding sidecar；DeepSeek judge key（env-only）
- 教师 API key（DeepSeek，env-only，SaaS 线成本）

## US1 — 教师抽取器零训练验证（P1，先行门禁）

**目标**：验证"抽取能力是瓶颈"——教师（DeepSeek-v4-pro）抽 event + 时间锚定，重跑 84 题配对。

```bash
# 1. 教师抽取（复用 027 的 build-event，换 EVENT_LLM 为教师 + 锚定强化 prompt）
cd /root/autodl-tmp/028-runs
locomo-bench --data /root/autodl-tmp/023-runs/locomo.json \
  --store-dir /root/autodl-tmp/027-runs/009-bge-chunks-store --chunks \
  --build-event-project teacher-project.json \
  --event-llm-base-url <teacher-endpoint> --event-llm-model deepseek-v4-pro

# 2. 配对（event 臂 = 教师投影，chunk 臂 = 027 基线，同 answerer/judge）
locomo-bench --data ... --store-dir ... --chunks \
  --only-questions phase0-ids.txt --retrieval hybrid --repeats 3 \
  --top-k 30 --chunk-quota 12 --force-answer --no-idk-retry --judge-mem0-aligned \
  --representation event --event-project teacher-project.json \
  --run-dir pair-us1 > pair-us1.log 2>&1

# 3. 审计 + 配对（本机）
python3 ~/.claude/engram-027/pair_analysis.py   # majority + McNemar
python3 specs/028/tools/audit_anchoring.py teacher-project.json  # 时间锚定率
```

**判定（pair-gate US1）**：`time_anchor_rate` ≥50 绝对点 **且** event−chunk ≥ −10pp → GO 进 US2；否则 STOP。

## US2 — 训练时间锚定抽取器（P1）

**目标**：教师标注 + 人工精修构建训练集 → SFT 训练 → 027 复测。

```bash
# 1. 构建训练数据（教师标注 → 人工精修 500–1000 条）
python3 specs/028/tools/build_training_data.py --input teacher-project.json \
  --human-refined refine.jsonl --out train-028-v1.jsonl
#    审计：audit.json（合法率/锚定率/来源分布）

# 2. SFT 训练（AutoDL 单卡）
bash specs/028/tools/train.sh --base Qwen/Qwen2.5-3B \
  --data train-028-v1.jsonl --out /root/autodl-tmp/028-models/qwen3b-028-r1

# 3. 量化导出 + 本地 sidecar 起 vllm（参考 027 Blackwell 环境）
bash specs/028/tools/export_deploy.sh --model qwen3b-028-r1 --quant int8

# 4. 复测配对（同 US1 步骤 2/3，EVENT_LLM 换训练模型）
```

**判定（pair-gate US2）**：锚定率 ≥70% + 合法率 ≥95% + 幻觉 ≤5% + **event−chunk ≥ 0** → GO 进 US3；否则记录 NO-GO。

## US3 — 部署与接入（P2）

**目标**：训练抽取器接 027 写侧路径，default-off、单独口径。

```bash
# 1. 默认关回归（本地基线不回归）
CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./memory/eventstore/ ./cmd/locomo-bench/
#    现有配对基线（LoCoMo 85.71%）不回归

# 2. 开启训练抽取器写侧（单独口径登记）
#    EVENT_LLM_BASE_URL=本地 sidecar + 训练模型 → --build-event-project → --representation event
```

**判定（pair-gate US3）**：默认路径零行为变化 + 开启配置分数单独登记（不回填本地）。

## 预期结果（对照 pair-gate 门）

| 阶段 | 关键预期 |
|---|---|
| US1 | 时间锚定率 5%→≥50%；event−chunk 从 −26.2pp 收窄到 ≥−10pp（假设成立信号） |
| US2 | 锚定率 ≥70%；end-to-end event ≥ chunk（008 铁律，SaaS 写侧能力坐实） |
| US3 | 本地基线零回归；SaaS 分数单独口径 |

## 成本提示（SaaS 线）

- US1 教师 API：低个位数美元（缓存高命中）；零 GPU
- US2 AutoDL 单卡 SFT：数十元级；**空闲必停**
- 不碰付费云 rerank/recall（评测纪律在 SaaS 线外仍守）
