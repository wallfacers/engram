#!/bin/bash
# 028 US3 deploy: merge LoRA -> full model, launch local vllm sidecar (OpenAI-compatible).
# Blackwell: FLASHINFER_CUDA_ARCH_LIST=12.0 + CUDA cu13 + VLLM_USE_FLASHINFER_SAMPLER=0
# (see /root/autodl-tmp/serve-ans80g.sh on the eval box).
set -e
V=/root/autodl-tmp/023-venv/bin
export HF_HOME=/root/autodl-tmp/hf-cache
export PATH=$V:$PATH

LORA=${1:?usage: export_deploy.sh <lora_dir> <merged_out> [port]}
MERGED=${2:?usage: export_deploy.sh <lora_dir> <merged_out> [port]}
PORT=${3:-8002}

# merge LoRA into base (train_sft.py saved adapter only)
$V/python - <<EOF
import torch
from peft import PeftModel
from transformers import AutoModelForCausalLM, AutoTokenizer
base = "${LORA%/lora}"
tok = AutoTokenizer.from_pretrained(base, trust_remote_code=True)
m = AutoModelForCausalLM.from_pretrained(base, torch_dtype=torch.bfloat16, trust_remote_code=True)
m = PeftModel.from_pretrained(m, "$LORA").merge_and_unload()
m.save_pretrained("$MERGED")
tok.save_pretrained("$MERGED")
print("merged ->", "$MERGED")
EOF

export FLASHINFER_CUDA_ARCH_LIST="12.0"
export CUDA_HOME=$V/lib/python3.12/site-packages/nvidia/cu13
export CUDA_PATH=$CUDA_HOME
export VLLM_USE_FLASHINFER_SAMPLER=0
nohup $V/python -m vllm.entrypoints.openai.api_server \
  --model "$MERGED" --served-model-name engram-event-extractor \
  --dtype auto --port "$PORT" --max-model-len 8192 \
  --gpu-memory-utilization 0.5 --trust-remote-code \
  > /root/autodl-tmp/028-runs/extract-${PORT}.log 2>&1 &
echo "extractor sidecar PID=$! port=$PORT"
