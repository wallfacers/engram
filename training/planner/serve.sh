#!/usr/bin/env bash
# Serve the 023 planner model as an OpenAI-compatible sidecar that
# local_planner.go talks to (--planner-base-url/--planner-model).
#
# Two modes:
#   serve.sh lora --model Qwen2.5-7B-Instruct --adapter models/planner-lora [--port 8000]
#     vLLM serves the base model and loads the trained LoRA adapter on demand.
#   serve.sh merged --adapter models/planner-lora [--port 8000]
#     (post-merge snapshot; see merge step in README)
#
# Run with the venv that has vllm installed (server-spec.md §训练环境).
set -euo pipefail

MODE="${1:-lora}"
PORT="${PORT:-8000}"
MODEL="${MODEL:-Qwen2.5-7B-Instruct}"
ADAPTER="${ADAPTER:-models/planner-lora}"

case "$MODE" in
  lora)
    # vLLM --enable-lora: the adapter is selectable per request via the
    # "model" override; local_planner sends base model id by default, so we
    # export it directly with the adapter baked in.
    exec python3 -m vllm.entrypoints.openai.api_server \
      --model "$MODEL" \
      --served-model-name "$MODEL" \
      --enable-lora --lora-modules "planner=$ADAPTER" \
      --max-model-len 4096 \
      --gpu-memory-utilization 0.92 \
      --port "$PORT"
    ;;
  merged)
    echo "mode 'merged' requires a merged checkpoint (see README); nothing to serve."
    exit 1
    ;;
  *)
    echo "usage: serve.sh {lora|merged}" >&2
    exit 1
    ;;
esac
