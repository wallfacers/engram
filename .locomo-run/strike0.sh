#!/usr/bin/env bash
set -euo pipefail

data_path=${1:?usage: .locomo-run/strike0.sh DATA_PATH [RUN_ROOT]}
run_root=${2:-.locomo-run/strike0}
repeats=${REPEATS:-5}
extract_a=${EXTRACT_MODEL_A:-${EXTRACT_MODEL:-${LOCOMO_MODEL:-deepseek-v4-pro}}}
extract_b=${EXTRACT_MODEL_B:-${LOCOMO_MODEL:-deepseek-v4-pro}}

export LOCOMO_PRICE_TABLE=${LOCOMO_PRICE_TABLE:-'{"gpt-5.6-sol":{"in":1.25,"out":10.0},"gpt-5.4-mini":{"in":0.15,"out":0.6}}'}

mkdir -p "$run_root"
if [[ -e "$run_root/extract-a" || -e "$run_root/extract-b" ]]; then
  echo "refusing to overwrite existing Strike 0 run directories: $run_root" >&2
  exit 1
fi
go build ./cmd/locomo-bench

./locomo-bench --data "$data_path" --retrieval hybrid --repeats "$repeats" \
  --estimate --no-idk-retry

EXTRACT_MODEL="$extract_a" ./locomo-bench --data "$data_path" --retrieval hybrid \
  --run-dir "$run_root/extract-a" --store-dir "$run_root/store-a" \
  --repeats "$repeats" --no-idk-retry
EXTRACT_MODEL="$extract_b" ./locomo-bench --data "$data_path" --retrieval hybrid \
  --run-dir "$run_root/extract-b" --store-dir "$run_root/store-b" \
  --repeats "$repeats" --no-idk-retry

./locomo-bench --compare "$run_root/extract-a" "$run_root/extract-b" --no-idk-retry
