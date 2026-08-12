#!/bin/bash
# serve_trained.sh — serve 记忆专用重排训练产物（T018，transformers 路线）
#
# 背景（2026-08-12 冻结）:
#   vLLM 0.27 cu13 与 CUDA 12.8 驱动不兼容（US1 T008）；且 vLLM serve 训练产物有
#   兼容 bug（serve merged 返回 base 分数 / --enable-lora 报 modules_to_save 不支持）。
#   → 改用 transformers + FastAPI 聚合端点（rerank_server.py），serve merged 产物。
#
# 用法（远程 GPU 机器，SeetaCloud/AutoDL）:
#   bash tools/serve_trained.sh <rerank-model-dir> [port]
#     默认 rerank-model-dir = /root/autodl-tmp/037-reranker/ckpts/bce-infonce/merged
#   bash tools/serve_trained.sh /root/autodl-tmp/037-reranker/models/Qwen3-Reranker-0.6B   # US1 对照
#
# 评测消费（locomo-bench，零改动）:
#   EMBED_BASE_URL=http://<host>:<port>/v1 EMBED_RERANK_MODEL=engram-memory-reranker-0.6b-v1 \
#   go run ./cmd/locomo-bench --data testdata/locomo/locomo.json \
#       --run-dir <run-dir> --retrieval 'hybrid,hybrid+rerank' ...
#
# 注意:
#   - rerank 与 embedding 共享同一端点（无 EMBED_RERANK_BASE_URL，contracts/rerank-serving.md）
#   - 启动前确认 GPU 空闲（残留 EngineCore 进程名不含 vllm，用 nvidia-smi 按 PID 杀）
#   - AutoDL/SeetaCloud 需 export LD_LIBRARY_PATH 指向 conda env lib（libstdc++ 旧）
set -euo pipefail
MODEL_DIR="${1:-/root/autodl-tmp/037-reranker/ckpts/bce-infonce/merged}"
PORT="${2:-8000}"

# conda env（T004 实测冻结；SeetaCloud 用实际 env 名）
if [ -d /root/autodl-tmp/miniconda3/envs/rerank-py313/bin ]; then
  export PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/bin:$PATH
  export LD_LIBRARY_PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/lib:$LD_LIBRARY_PATH
fi

echo "serving rerank model: $MODEL_DIR on :$PORT"
exec python3 tools/rerank_server.py --rerank-model "$MODEL_DIR" --port "$PORT"
