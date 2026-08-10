# Quickstart: Evidence-Grounded Answer Adjudication

This guide validates the offline contracts first. It intentionally does not contain a credential.

## 1. Build and test offline

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
CGO_ENABLED=0 go build ./...
```

Expected: both commands exit 0; no network is required.

## 2. Build frozen packets without model calls

Set operator-local paths outside the repository. Candidate ordering is not semantically significant.

```bash
export ADJ_STAGE_DIR=/path/to/session-scratch/034-stage0
export ADJ_CANDIDATE_1=/path/to/r1/results-hybrid.jsonl
export ADJ_CANDIDATE_2=/path/to/r2/results-hybrid.jsonl
export ADJ_CANDIDATE_3=/path/to/r3/results-hybrid.jsonl
export ADJ_TRACE=/path/to/trace.jsonl
export ADJ_STORE_DIR=/path/to/frozen-store

go run ./cmd/locomo-bench \
  --adjudication-build "$ADJ_STAGE_DIR" \
  --adjudication-candidate "$ADJ_CANDIDATE_1" \
  --adjudication-candidate "$ADJ_CANDIDATE_2" \
  --adjudication-candidate "$ADJ_CANDIDATE_3" \
  --adjudication-trace "$ADJ_TRACE" \
  --store-dir "$ADJ_STORE_DIR" \
  --adjudication-seed 034-stage0-v1
```

Expected frozen receipt: 1540 questions, 771 triggers, 8 candidate-context parity exceptions (5 triggered), no model
calls, and no source-store digest changes.

## 3. Validate the execution surface offline

```bash
go run ./cmd/locomo-bench --adjudication-validate "$ADJ_STAGE_DIR"
```

Expected: public packet/manifest digests, counts, strict schemas, and contamination checks pass. Validation deliberately
does not open the score-only slot map, custody receipt, raw candidates, API-key environment, or network.

## 4. Optional paid verifier run

Only do this after reviewing the manifest's planned-call count and supplying a newly rotated key through the environment.
Never reuse a key that appeared in chat, shell history, a log, or another persisted artifact.
The command is long-running on WSL2, so detach it and keep logs in the session scratch directory.

```bash
export ADJUDICATOR_PROVIDER=openai
export ADJUDICATOR_BASE_URL=https://provider.example/v1
export ADJUDICATOR_MODEL=provider-model-id
export ADJUDICATOR_MODEL_REVISION=provider-model-revision
export ADJUDICATOR_API_KEY='set-live-in-shell-only'

setsid bash -c 'go run ./cmd/locomo-bench \
  --adjudication-run "$ADJ_STAGE_DIR" \
  --adjudication-allow-paid \
  --concurrency 32 \
  --adjudication-max-tokens 512 \
  >"$ADJ_STAGE_DIR/run.log" 2>&1; echo $? >"$ADJ_STAGE_DIR/run.exit"' \
  </dev/null >/dev/null 2>&1 & disown
```

Poll once without a foreground sleep loop:

```bash
[ -f "$ADJ_STAGE_DIR/run.exit" ] && cat "$ADJ_STAGE_DIR/run.exit" || tail -n 20 "$ADJ_STAGE_DIR/run.log"
```

Expected on completion: exactly one terminal decision per packet, at most one provider attempt per triggered packet,
sorted sealed decisions, usage totals, and either priced cost or an explicit `unpriced` state.

## 5. Historical Stage-0 score after sealing

Run the score phase separately and re-supply the three raw candidate files so custody hashes can be verified.

```bash
go run ./cmd/locomo-bench \
  --adjudication-score "$ADJ_STAGE_DIR" \
  --adjudication-candidate "$ADJ_CANDIDATE_1" \
  --adjudication-candidate "$ADJ_CANDIDATE_2" \
  --adjudication-candidate "$ADJ_CANDIDATE_3"
```

Expected: a report explicitly labelled historical verdict mapping. GO requires selected ≥1387/1540, selected-correct
≥69/88 on the post-seal triggered mixed-verdict stratum, no Holm-significant negative category, and all validity gates
green. Even a GO is not a new formal LoCoMo score; it only authorizes a separately preregistered paired rejudge.
