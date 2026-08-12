# Quickstart: Memory-Specific Reranker Training

**Date**: 2026-08-11 | **Spec**: [spec.md](spec.md) | **Data model**: [data-model.md](data-model.md) | **Contracts**: [training-data-schema](contracts/training-data-schema.md), [rerank-serving](contracts/rerank-serving.md)

端到端验证引导（无完整实现代码；细节在 tasks.md / 实现阶段）。

## 验证场景总览

| # | 场景 | 验证什么 | 门 |
|---|---|---|---|
| 1 | 数据构建 + schema 校验 | 训练 JSONL 确定性派生、fail-closed 校验（multi-positive / split / temporal 文本可见） | 全部样本过校验、temporal-hard 抽检复核 |
| 2 | 训练 smoke（小数据子集） | 训练管线可跑、loss 收敛、峰值 VRAM 实测 | 200/1000 样本实测 VRAM/tok/s 后外推 |
| 3 | 全量训练（AutoDL，三 checkpoint） | base / BCE-only / BCE→InfoNCE 消融 | 预算累计门 ≤¥100/8 GPU·时、seed 复现 |
| 4 | US1 现成模型基准 | Qwen3-Reranker-0.6B base 端到端配对 | 008 同协议配对表（冻结 run） |
| 5 | US2 训练产物端到端配对 | 记忆专用重排 e2e 转化（GO 门） | 跨 run 配对 ≥ US1 + temporal 不劣 + heldout/LME 泛化否决门 |

## 前置

- 数据：`testdata/locomo/locomo.json`（已有，**真实 conv ID = conv-26/30/41/42/43/44/47/48/49/50**，LoCoMo 为唯一主源）。MSC 可选补充——**T002 冻结结论（2026-08-11）：`gonced8` 镜像 GPL-3.0 排除、`nayohan` 镜像无许可声明 → 许可未核验，暂不下载**；派生逻辑已在 `build_training_data.py --msc` 接口保留，许可确认后再启用。
- 工具链：Python 3.11+（torch/transformers≥4.51/peft/datasets/vllm）；Go 1.25（locomo-bench）。
- 许可核验：Qwen3-Reranker-0.6B = Apache 2.0（research R1 已核）+ 冻结 revision；MSC 数据许可按镜像卡核验。
- 机器：本地 WSL2（数据构建/评测 orchestration）+ AutoDL RTX 4090 24GB（训练，产物放 `/root/autodl-tmp/`）+ remote-eval-box（answer/judge）。

## 场景 1：数据构建 + 校验（本地，零 GPU）

```bash
cd specs/037-memory-reranker-training
python3 tools/build_training_data.py \
    --locomo ../../testdata/locomo/locomo.json \
    --out train-r1.jsonl \                            # --msc 可选（T002 许可未核验暂缺）
    --train-convs conv-26,conv-30,conv-41,conv-42,conv-43,conv-44,conv-47 \
    --heldout-convs conv-48,conv-49,conv-50 \
    --seed 42
python3 tools/test_training_data.py train-r1.jsonl   # fail-closed：任何不满足 schema 即退出
```

**预期**：JSONL 严格符合 [training-data-schema](contracts/training-data-schema.md)（含 `schema_version`/`qa_id`/`query_group_id`/`positives`/`split` 行字段）；多 evidence 全部为 positive（同 group 不互作负例）；temporal-hard 负样本人工抽检（≥50 条）**文本可见时间信号存在**（R7 probe 先行）。

## 场景 2：训练 smoke（小数据 + VRAM 实测）

```bash
python3 tools/train_reranker.py --data train-r1.jsonl --subset 200 \
    --epochs 1 --output smoke-run/     # 两段式 BCE+InfoNCE，LoRA
python3 tools/test_train_smoke.py smoke-run/   # 产物可加载 + 训练/合并/vLLM 三方排序一致性 + score 分布 sane
```

**预期**：1 epoch 收敛；**记录峰值 VRAM、tok/s、p95 长度**并外推全量训练预算；score 分布不过度聚簇（对标 008 BGE 左偏反例）。

## 场景 3：全量训练（AutoDL，三 checkpoint 消融 + 预算门禁）

```bash
# 预算累计记账：每次 run 记录 GPU 时/费用/seed/hash；跨 run 达 ¥100 或 8 GPU·时自动停
for ckpt in base bce bce-infonce; do
  python3 tools/train_reranker.py --data train-r1.jsonl --epochs 3 \
      --checkpoint-suffix $ckpt \
      --output /root/autodl-tmp/037-reranker/$ckpt/ 2>&1 | tee /root/autodl-tmp/037-reranker/$ckpt/train.log
done
```

**预期**：≤3h/ckpt、累计 ≤¥100；seed 复现；三 checkpoint 各带模型卡（data-model TrainingConfig，score_head/template 冻结）。

## 场景 4：US1 现成模型基准（冻结 run，与训练并行）

```bash
# **serving（2026-08-12 冻结，替代 vLLM）**：vLLM cu13↔CUDA 12.8 不兼容 + serve 训练产物
# 分数=base bug → 用 tools/server.py（transformers + FastAPI 聚合 /v1/rerank + /v1/embeddings）
# serve base 或 merged 训练产物，仅改 --model 路径。启动前 export HF_HUB_OFFLINE=1
# （AutoDL 无 HF 外网；bge-small 需本地缓存）。
python3 tools/server.py --model <model_dir> --port 8000
# rerank 与 embedding 共享 EMBED_BASE_URL（不存在 EMBED_RERANK_BASE_URL）；多臂用 --retrieval，无 --arm
EMBED_RERANK_MODEL=Qwen/Qwen3-Reranker-0.6B \
EMBED_BASE_URL=http://<host>:8000/v1 \
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo.json \
    --run-dir ./.locomo-run/037-us1 --retrieval "hybrid,hybrid+rerank" \
    ... >us1.log 2>&1; echo $? >us1.exit' </dev/null >/dev/null 2>&1 & disown
# preflight：rerank 请求成功/失败计数，零成功 → INVALID（禁止静默回退后出报告）
```

**预期**：配对表（总体 + 四类别 + McNemar + flip + paired CI）；与 008 bge-reranker-v2-m3 记录同口径可比；temporal 单独行（确认通用模型是否依旧被害）；**全量配对含训练对话的污染标注**。US1 实测：base rerank −0.4pp（NO-GO，multi-hop 被害 −4.3pp）。

## 场景 5：US2 训练产物端到端配对（GO 门）

```bash
# tools/server.py 换 serve merged 训练产物（ckpts/bce-infonce/merged）；同场景 4 协议跑 US2
EMBED_RERANK_MODEL=engram-memory-reranker-0.6b-v1 \
EMBED_BASE_URL=http://<host>:8000/v1 \
setsid bash -c 'go run ./cmd/locomo-bench --data testdata/locomo/locomo.json \
    --run-dir ./.locomo-run/037-us2 --retrieval "hybrid,hybrid+rerank" \
    ... >us2.log 2>&1; echo $? >us2.exit' </dev/null >/dev/null 2>&1 & disown
# 跨 run 逐题配对：go run ./cmd/locomo-bench --compare ./.locomo-run/037-us1 ./.locomo-run/037-us2
#   注意：--compare 要求 run-dir 只有 1 个 arm results 文件（US1/US2 各含 hybrid +
#   hybrid+rerank 两个 → ambiguous）；先复制单臂到独立目录再 compare
```

**GO 门判定（008 铁律）**：
- 跨 run 配对 US2 vs US1：**总体不劣**（预注册 non-inferiority margin，p>0.05 本身不证明"不劣"）；
- **temporal 类不劣**（修复 008 −9）；
- **heldout 对话 + LME 500 泛化否决门**：任一不过 → 不得宣称泛化能力；
- 显著转正（p<0.05、幅度>噪声标尺）→ 触发 SC-005 的"是否发布为 opt-in sidecar"决策（cross-encoder 永不进本地默认栈）。

**US2 实测（2026-08-12）：NO-GO**——merged rerank run 内 −1.1pp（未转化）；temporal +1.6pp 唯一正向。
⚠️ **方法警告**：单次 run 的 extraction+answer 噪声 ~8.6pp（US1/US2 hybrid 基线 68.1 vs 59.5%），
跨 run 单臂对比不可靠；**判定以 run 内配对为准，且需 repeats ≥3 + `--store-dir` 复用**。

## 常见失败与排查

- **训练 loss 不收敛**：max_len/有效 batch 过小、负采样太易 → 检查 `negative_type` 分布 + multi-positive mask。
- **temporal 仍被害**：时序 hard negative 文本无可见时间信号（R7 probe 失败）→ 先审计 payload，再决定是否另立 engine 契约增量（037 不得暗改）。
- **score 聚簇**（008 BGE 左偏反例）：第一段 BCE 未充分训练或 score_head 不一致 → 检查 label 分布 + 三方排序一致性测试。
- **vLLM 起不来 / rerank 无调用**：版本/runner 未实测冻结 → 查 vllm#19229 案例，冻结实测命令；preflight 必须 catch 零成功 → INVALID。
- **评测配对翻车**：跑前确认 `EMBED_RERANK_MODEL` 指向正确端点、`--retrieval` 臂格式正确（无 `--arm`）。
