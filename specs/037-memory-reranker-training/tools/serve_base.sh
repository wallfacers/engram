#!/bin/bash
# serve_base.sh — vllm serve Qwen3-Reranker-0.6B（T004 实测冻结命令）
#
# 冻结依据: contracts/rerank-serving.md（vllm 0.27.0 / torch 2.13 / py3.13, 2026-08-11 实测）
# 用法（AutoDL）:
#   bash tools/serve_base.sh <model_dir> [port]
set -euo pipefail
MODEL_DIR="${1:-/root/autodl-tmp/037-reranker/models/Qwen3-Reranker-0.6B}"
PORT="${2:-8000}"
export PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/bin:$PATH
export LD_LIBRARY_PATH=/root/autodl-tmp/miniconda3/envs/rerank-py313/lib:$LD_LIBRARY_PATH

echo "serving $MODEL_DIR on :$PORT (runner=pooling)"
exec vllm serve "$MODEL_DIR" \
  --runner pooling \
  --hf-overrides '{"architectures": ["Qwen3ForSequenceClassification"], "classifier_from_token": ["no", "yes"], "is_original_qwen3_reranker": true}' \
  --chat-template "$MODEL_DIR/chat_template.jinja" \
  --port "$PORT" --served-model-name Qwen3-Reranker-0.6B \
  --max-model-len 32768 --gpu-memory-utilization 0.8
