# Contract: Rerank Serving（vllm `/v1/rerank` 端点）

**Date**: 2026-08-11 | **Spec**: [spec.md](../spec.md)

训练产物（Qwen3-Reranker-0.6B base 或训练后合并模型）经 vllm 服务的接口契约。引擎侧契约（`embedding.Reranker`）已存在且冻结，本契约只描述**训练产物如何接入**——引擎与 locomo-bench 零改动。

## 服务端：vllm rerank 端点（版本已实测冻结，T004 2026-08-11）

- **实测版本**：vllm **0.27.0** / torch 2.13.0+cu130 / Python **3.13**（AutoDL RTX 4090）。Python 3.11 不可用（flashinfer 0.6.16 的 `array.array[int]` 语法需 3.13）。
- **完整冻结命令**（已实测，`/v1/rerank` 返回正确排序，score 0.99996 vs 干扰项 ~0.0005）：

  ```bash
  export PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/bin:$PATH
  export LD_LIBRARY_PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/lib:$LD_LIBRARY_PATH
  vllm serve <model_dir> \
    --runner pooling \
    --hf-overrides '{"architectures": ["Qwen3ForSequenceClassification"], "classifier_from_token": ["no", "yes"], "is_original_qwen3_reranker": true}' \
    --chat-template <model_dir>/chat_template.jinja \
    --port 8000 --served-model-name Qwen3-Reranker-0.6B \
    --max-model-len 32768 --gpu-memory-utilization 0.8
  ```

- **必须项**（vllm#19229 系列坑，实测确认）：
  1. `--runner pooling`（否则默认 pipeline/generate，无 rerank 端点）——vllm 0.27 无 `--task` flag（`python -m api_server` 与顶层 `vllm` 均不接受）。
  2. `--hf-overrides` 强制 `architectures=Qwen3ForSequenceClassification` + `classifier_from_token=["no","yes"]` + `is_original_qwen3_reranker=true`——否则 Qwen3-Reranker config（`Qwen3ForCausalLM`）被当 chat 模型，无 rerank 端点。
  3. `--chat-template` 指向模型自带 `chat_template.jinja`。
  4. `LD_LIBRARY_PATH` 指向 conda env lib（AutoDL 系统 libstdc++ 旧，缺 CXXABI_1.3.15）。
  5. 启动前确认 GPU 空闲（残留 EngineCore 进程名不含 'vllm'，`pkill -f vllm` 漏杀——用 `nvidia-smi --query-compute-apps=pid` 查并按 PID kill）。
- **端点**：`POST {base}/v1/rerank`（OpenAI 兼容 de-facto 标准，embedding/rerank.go 已实现该协议客户端）。
- **请求**：`{"model": ..., "query": ..., "documents": [...], "top_n": N, "return_documents": true}`。
- **响应**：`{"results": [{"index": i, "relevance_score": 0.0–1.0}, ...]}`。
- **score equation 冻结**：训练目标（`yes_logit−no_logit` 或新增 scalar head 的 sigmoid）必须与 vLLM 服务的分数一致；**训练、合并 checkpoint、HTTP `/v1/rerank` 三方排序一致性测试必做**（review C2）；instruction/template/截断顺序训练与 serving 同一套。

## 评测接入（locomo-bench，已存在，零改动）

```
EMBED_RERANK_MODEL=<model-id>        # cmd/locomo-bench/main.go:3059
EMBED_BASE_URL=<vllm base>           # rerank 与 embedding 共享此端点（main.go:3058 只读 EMBED_BASE_URL；不存在 EMBED_RERANK_BASE_URL）
# 多臂配对用 --retrieval 逗号列表（main.go:1639）；无 --arm flag
go run ./cmd/locomo-bench --data testdata/locomo/locomo.json --run-dir ./.locomo-run/037 \
    --retrieval 'hybrid,hybrid+rerank' ...
```

- `--rerank` flag（main.go:452）向后兼容全局开；配对 run 用臂后缀。
- **若需独立 rerank 端点**（embedding 与 rerank 不同 vllm）：提供 feature-local router 聚合到同一 `EMBED_BASE_URL`（保持 locomo-bench 零改动）。
- **preflight fail-closed**：评测前记录 rerank 请求成功/失败计数，**零成功或失败 → 评测标记 INVALID**；禁止静默回退（retriever.go:707-710）后出 GO 报告。
- 未配置 rerank（nil Reranker）→ 静默退化为无重排（宪法 V 优雅降级，embedding/rerank.go 已实现）。

## 训练产物 serving（AutoDL 或本地）

- 产物 = base + LoRA adapter 合并后的模型（`merge_and_unload`），或 base + adapter 组合加载。
- 放数据盘 `/root/autodl-tmp/<model>/`（AutoDL 磁盘卫生规则）。
- 推理成本目标：对标 memos-reranker 0.5/2 元每万 token 量级、~200ms 级延迟（MemReranker 0.6B 参考）；**推理端绝不依赖付费云 reranker API**（死亡规则）。

## 契约边界

- 本 feature **不修改** `embedding/rerank.go`、locomo-bench 的 rerank 臂（已满足 US1/US2 评测需要）。若训练产物需要新服务能力（如 evidence 输出），那是 US3 之后的新契约，另行 spec（宪法 II）。
- 请求/响应 schema 变更需走契约升级（宪法 III）。
