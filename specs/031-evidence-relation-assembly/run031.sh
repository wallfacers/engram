#!/bin/bash
# 031 US3 pairing (specs/031/tasks.md T012-T013): keep vs keep+relation vs
# keep+relation+trace, 84-question subset x 3 reps majority, arm-to-arm on the
# shared flash store (same store/answerer/judge/cap — 008 discipline).
#
# NOTE: this is the skeleton (T002); the exact arm list / retrieval tier is
# finalized in T012 per the flash probe conclusion — the relation-context
# increment shows up under weak retrieval / hard subset (probe: strong-retrieval
# trace Δ is within-noise p=0.78), so the paired run may switch --retrieval to a
# weaker tier and/or drop the overlay arm after the first subset read.
#
# Requires: .locomo-run/flash-84/run.env (0o600, gitignored, DeepSeek key).
set -a
source /home/wallfacers/project/engram/.locomo-run/flash-84/run.env
set +a
LOC=/home/wallfacers/project/engram/.locomo-run
BIN=$LOC/locomo-bench
DATA=/home/wallfacers/project/engram/testdata/locomo/locomo10.json
STORE=$LOC/flash-84/store
QUESTIONS=/home/wallfacers/project/engram/specs/030-evidence-mediation/diagnosis/phase0-ids-029-84.txt
RUN_DIR=$LOC/flash-84/run-031
BASE_FLAGS="--chunks --retrieval hybrid --top-k 30 --chunk-quota 12 --force-answer --judge-mem0-aligned --concurrency 32 --evidence-assembly --only-questions $QUESTIONS --repeats 3"

echo "=== [keep] 84 x 3 (assembly, no relation-context) ==="
$BIN --data "$DATA" --store-dir "$STORE" --run-dir "$RUN_DIR/keep" $BASE_FLAGS --trace-mediation=false
echo "keep_exit=$?"

echo "=== [relation] 84 x 3 (assembly + relation-context) ==="
$BIN --data "$DATA" --store-dir "$STORE" --run-dir "$RUN_DIR/relation" $BASE_FLAGS --trace-mediation=false --relation-context
echo "relation_exit=$?"

echo "=== [relation+trace] 84 x 3 (overlay, optional arm) ==="
$BIN --data "$DATA" --store-dir "$STORE" --run-dir "$RUN_DIR/relation-trace" $BASE_FLAGS --trace-mediation --relation-context
echo "relation_trace_exit=$?"

echo "=== compare keep vs relation ==="
$BIN --compare "$RUN_DIR/keep" "$RUN_DIR/relation"
