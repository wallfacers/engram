#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: $0 --estimate|--smoke|--start|--status <session-scratch-dir>" >&2
  exit 64
}

fail() {
  echo "run033: $*" >&2
  exit 1
}

[[ $# -eq 2 ]] || usage
MODE=$1
SCRATCH=$(realpath -m -- "$2")
ROOT=$(git -C "$(dirname -- "$0")" rev-parse --show-toplevel)
MAIN_ROOT=$(git -C "$ROOT" worktree list --porcelain | awk '/^worktree / {print $2; exit}')
FEATURE="$ROOT/specs/033-chunk-first-contract-repair"
DATA=${LOCOMO_033_DATA:-"$MAIN_ROOT/testdata/locomo/locomo.json"}
STORE=${LOCOMO_033_STORE_DIR:-"$MAIN_ROOT/.locomo-run/009-bge-chunks-store"}
BIN="$SCRATCH/locomo-bench-033"
PROBE="$FEATURE/diagnosis/probe-64.txt"
MULTI="$FEATURE/diagnosis/multi-hop-18.txt"
SMOKE="$FEATURE/diagnosis/smoke-1.txt"
EMBED_URL=${EMBED_BASE_URL:-http://127.0.0.1:8010/v1}
EMBED_ID=${EMBED_MODEL:-BAAI/bge-large-en-v1.5}

case "$SCRATCH/" in
  "$ROOT/"*|"$MAIN_ROOT/"*) fail "scratch dir must be outside every repository worktree" ;;
esac
[[ "$SCRATCH" != "/" ]] || fail "refusing filesystem root as scratch dir"
[[ -f "$DATA" ]] || fail "dataset not found: $DATA"
[[ -d "$STORE" ]] || fail "persisted store not found: $STORE"
[[ -f "$PROBE" && -f "$MULTI" && -f "$SMOKE" ]] || fail "frozen execution cohorts are missing"

common_args=(
  --data "$DATA"
  --chunks
  --retrieval hybrid
  --top-k 30
  --chunk-quota 12
  --force-answer
  --judge-mem0-aligned
  --repeats 3
  --trace-mediation=false
  --no-idk-retry=false
)

arm_args() {
  case "$1" in
    a) printf '%s\0' --only-questions "$PROBE" --evidence-assembly=false ;;
    b) printf '%s\0' --only-questions "$MULTI" --evidence-assembly --assembly-legacy-entity-order --assembly-audit ;;
    c) printf '%s\0' --only-questions "$PROBE" --evidence-assembly --assembly-audit ;;
    *) fail "unknown arm $1" ;;
  esac
}

arm_concurrency() {
  case "$1" in
    a|c) echo 11 ;;
    b) echo 10 ;;
    *) fail "unknown arm $1" ;;
  esac
}

build_binary() {
  mkdir -p -- "$SCRATCH"
  CGO_ENABLED=0 go -C "$ROOT" build -o "$BIN" ./cmd/locomo-bench
  sha256sum "$BIN"
}

store_manifest() {
  local dir=$1
  (
    cd "$dir"
    sha256sum ./*.db | sed 's# \./# #' | sort -k2
  )
}

copy_store() {
  local destination=$1
  [[ ! -e "$destination" ]] || fail "store snapshot already exists: $destination"
  cp -a -- "$STORE" "$destination"
  cmp -s <(store_manifest "$STORE") <(store_manifest "$destination") || fail "copied store manifest mismatch: $destination"
}

read_arm_args() {
  local arm=$1
  local -n destination=$2
  mapfile -d '' -t destination < <(arm_args "$arm")
}

estimate_arm() {
  local arm=$1
  local -a specific
  read_arm_args "$arm" specific
  echo "=== arm-$arm estimate ==="
  LOCOMO_NO_THINKING=0 RERANK_MODEL= "$BIN" "${common_args[@]}" --concurrency "$(arm_concurrency "$arm")" --store-dir "$STORE" "${specific[@]}" --estimate
}

launch_arm() {
  local arm=$1
  local run_dir="$SCRATCH/arm-$arm"
  local log="$SCRATCH/arm-$arm.log"
  local exit_file="$SCRATCH/arm-$arm.exit"
  local -a specific
  read_arm_args "$arm" specific
  [[ ! -e "$run_dir" && ! -e "$log" && ! -e "$exit_file" ]] || fail "arm-$arm artifacts already exist; choose a fresh scratch dir"
  setsid -f bash -c '
    log=$1
    exit_file=$2
    shift 2
    "$@" >"$log" 2>&1
    status=$?
    printf "%s\n" "$status" >"$exit_file"
  ' bash "$log" "$exit_file" env LOCOMO_NO_THINKING=0 RERANK_MODEL= \
    EMBED_BASE_URL="$EMBED_URL" EMBED_MODEL="$EMBED_ID" "$BIN" \
    "${common_args[@]}" --concurrency "$(arm_concurrency "$arm")" --store-dir "$SCRATCH/store-$arm" "${specific[@]}" --run-dir "$run_dir" </dev/null >/dev/null 2>&1
}

launch_smoke() {
  local run_dir="$SCRATCH/smoke"
  local log="$SCRATCH/smoke.log"
  local exit_file="$SCRATCH/smoke.exit"
  [[ ! -e "$run_dir" && ! -e "$log" && ! -e "$exit_file" ]] || fail "smoke artifacts already exist; choose a fresh scratch dir"
  copy_store "$SCRATCH/store-smoke"
  setsid -f bash -c '
    log=$1
    exit_file=$2
    shift 2
    "$@" >"$log" 2>&1
    status=$?
    printf "%s\n" "$status" >"$exit_file"
  ' bash "$log" "$exit_file" env LOCOMO_NO_THINKING=0 RERANK_MODEL= \
    EMBED_BASE_URL="$EMBED_URL" EMBED_MODEL="$EMBED_ID" "$BIN" \
    "${common_args[@]}" --store-dir "$SCRATCH/store-smoke" \
    --only-questions "$SMOKE" --evidence-assembly=false --repeats 1 --no-idk-retry=true \
    --run-dir "$run_dir" </dev/null >/dev/null 2>&1
}

validate_smoke() {
  local exit_file="$SCRATCH/smoke.exit"
  local results="$SCRATCH/smoke/results-hybrid.jsonl"
  local cost="$SCRATCH/smoke/cost.json"
  [[ -f "$exit_file" && $(<"$exit_file") == "0" ]] || return 1
  [[ -f "$results" && -f "$cost" ]] || return 1
  [[ $(wc -l <"$results") -eq 1 ]] || return 1
  jq -e '.answer_context_tokens >= 1000 and (.predicted | length > 0)' "$results" >/dev/null || return 1
  jq -e '.by_role.answer.calls == 1 and .by_role.answer.in_tokens > 0 and .by_role.judge.calls == 1' "$cost" >/dev/null || return 1
  sha256sum "$BIN" | cut -d' ' -f1 >"$SCRATCH/smoke.ok"
}

status_arm() {
  local arm=$1
  local exit_file="$SCRATCH/arm-$arm.exit"
  local log="$SCRATCH/arm-$arm.log"
  if [[ -f "$exit_file" ]]; then
    echo "arm-$arm exit=$(<"$exit_file")"
  elif [[ -f "$log" ]]; then
    echo "arm-$arm running"
  else
    echo "arm-$arm not-started"
  fi
  [[ ! -f "$log" ]] || tail -n 1 -- "$log"
}

status_smoke() {
  if validate_smoke; then
    echo "smoke valid"
  elif [[ -f "$SCRATCH/smoke.exit" ]]; then
    echo "smoke invalid exit=$(<"$SCRATCH/smoke.exit")"
  elif [[ -f "$SCRATCH/smoke.log" ]]; then
    echo "smoke running"
  else
    echo "smoke not-started"
  fi
  [[ ! -f "$SCRATCH/smoke.log" ]] || tail -n 1 -- "$SCRATCH/smoke.log"
}

case "$MODE" in
  --estimate)
    [[ ${LOCOMO_MODEL:-} == "deepseek-v4-pro" ]] || fail "set LOCOMO_MODEL=deepseek-v4-pro before freezing the estimate"
    build_binary
    estimate_arm a
    estimate_arm b
    estimate_arm c
    echo "planned_primary_answer_decisions=438"
    echo "planned_primary_judge_decisions=438"
    echo "adaptive_idk_answer_rewrite_calls=additional_and_measured_at_runtime"
    ;;
  --start)
    [[ -x "$BIN" ]] || fail "frozen binary missing; run --estimate with this scratch dir first"
    [[ ${LOCOMO_MODEL:-} == "deepseek-v4-pro" ]] || fail "LOCOMO_MODEL must remain deepseek-v4-pro"
    [[ ${LOCOMO_PROVIDER:-} == "openai" ]] || fail "LOCOMO_PROVIDER must remain openai so provider usage reports the complete prompt context"
    [[ -n ${LOCOMO_PROVIDER:-} && -n ${LOCOMO_BASE_URL:-} && -n ${LOCOMO_API_KEY:-} ]] || fail "answerer LOCOMO_PROVIDER/BASE_URL/API_KEY are not all available in this process"
    [[ -n ${JUDGE_PROVIDER:-} && -n ${JUDGE_BASE_URL:-} && -n ${JUDGE_MODEL:-} && -n ${JUDGE_API_KEY:-} ]] || fail "judge JUDGE_PROVIDER/BASE_URL/MODEL/API_KEY are not all available in this process"
    validate_smoke || fail "a successful one-question --smoke is required before the paid probe"
    [[ $(<"$SCRATCH/smoke.ok") == "$(sha256sum "$BIN" | cut -d' ' -f1)" ]] || fail "binary changed after smoke"
    curl -fsS --max-time 3 "$EMBED_URL/models" | rg -Fq -- "$EMBED_ID" || fail "frozen local embedding model is not available at $EMBED_URL"
    copy_store "$SCRATCH/store-a"
    copy_store "$SCRATCH/store-b"
    copy_store "$SCRATCH/store-c"
    launch_arm a
    launch_arm c
    launch_arm b
    echo "started arms a/c/b in one time window; poll with: $0 --status $SCRATCH"
    ;;
  --smoke)
    [[ -x "$BIN" ]] || fail "frozen binary missing; run --estimate with this scratch dir first"
    [[ ${LOCOMO_MODEL:-} == "deepseek-v4-pro" ]] || fail "LOCOMO_MODEL must remain deepseek-v4-pro"
    [[ ${LOCOMO_PROVIDER:-} == "openai" ]] || fail "LOCOMO_PROVIDER must be openai so provider usage reports the complete prompt context"
    [[ -n ${LOCOMO_PROVIDER:-} && -n ${LOCOMO_BASE_URL:-} && -n ${LOCOMO_API_KEY:-} ]] || fail "answerer LOCOMO_PROVIDER/BASE_URL/API_KEY are not all available in this process"
    [[ -n ${JUDGE_PROVIDER:-} && -n ${JUDGE_BASE_URL:-} && -n ${JUDGE_MODEL:-} && -n ${JUDGE_API_KEY:-} ]] || fail "judge JUDGE_PROVIDER/BASE_URL/MODEL/API_KEY are not all available in this process"
    curl -fsS --max-time 3 "$EMBED_URL/models" | rg -Fq -- "$EMBED_ID" || fail "frozen local embedding model is not available at $EMBED_URL"
    launch_smoke
    echo "smoke started; poll with: $0 --status $SCRATCH"
    ;;
  --status)
    status_smoke
    status_arm a
    status_arm b
    status_arm c
    ;;
  *) usage ;;
esac
