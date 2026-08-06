#!/bin/bash
# 028 US2 SFT train (AutoDL): Qwen 3B LoRA time-anchored event extractor.
# FR-003: 超参/seed 记录到 model dir 的 train-config.json（可复现）。
set -e
V=/root/autodl-tmp/023-venv/bin
export HF_HOME=/root/autodl-tmp/hf-cache
export PATH=$V:$PATH

DATA=${1:?usage: train.sh <train.jsonl> <out_dir> [base_model]}
OUT=${2:?usage: train.sh <train.jsonl> <out_dir> [base_model]}
BASE=${3:-Qwen/Qwen2.5-3B-Instruct}
BASE_CACHE_ID="hub/models--${BASE//\//--}"   # huggingface_hub 新版缓存结构在 $HF_HOME/hub/ 下

# 模型未缓存则经 hf-mirror 下载（AutoDL 无法直连 huggingface.co；下载后才能 offline）
if [ ! -d "$HF_HOME/$BASE_CACHE_ID" ]; then
  echo "== downloading $BASE via hf-mirror =="
  HF_ENDPOINT=https://hf-mirror.com $V/python -c "from huggingface_hub import snapshot_download; snapshot_download('$BASE')"
fi
export HF_HUB_OFFLINE=1   # 之后训练走本地缓存

# 依赖检查（023-venv 可能缺 peft/datasets，装到数据盘）
$V/pip install -q peft datasets trl 2>/dev/null || true

mkdir -p "$OUT"
python train_sft.py --data "$DATA" --base "$BASE" --out "$OUT" --epochs 3 --lr 2e-4 --lora-r 16 --seed 42
cat > "$OUT/train-config.json" <<EOF
{"data": "$DATA", "base": "$BASE", "epochs": 3, "lr": 2e-4, "lora_r": 16, "seed": 42, "framework": "transformers+peft", "date": "$(date -u +%FT%TZ)"}
EOF
echo "SFT done -> $OUT/lora"
