# Quickstart: Validate Multi-hop Chunk-First Contract Repair

## 1. Offline contract gate

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench \
  -run 'Test(MultiHop|Assemble|AnswerRegime)'
CGO_ENABLED=1 go test -race -count=1 ./cmd/locomo-bench \
  -run 'Test(MultiHopCanonical|MultiHopPrompt|MultiHopCap)'
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
git diff --name-only -- memory embedding provider store internal
```

Expected: tests/build pass；最后一条无输出。

## 2. Retrieval-only 64-question diagnostic

Prerequisites: persisted `009-bge-chunks-store` and local bge-large-compatible embedding endpoint.
This mode makes zero answer/judge calls.

```bash
export SPECIFY_FEATURE_DIRECTORY=specs/033-chunk-first-contract-repair

./locomo-bench \
  --data testdata/locomo/locomo.json \
  --store-dir .locomo-run/009-bge-chunks-store \
  --run-dir <scratch>/033-diag-treatment \
  --chunks --retrieval hybrid --top-k 30 --chunk-quota 12 \
  --only-questions specs/033-chunk-first-contract-repair/diagnosis/probe-64.txt \
  --evidence-assembly --assembly-diagnose --trace-mediation=false

./locomo-bench \
  --data testdata/locomo/locomo.json \
  --store-dir .locomo-run/009-bge-chunks-store \
  --run-dir <scratch>/033-diag-legacy \
  --chunks --retrieval hybrid --top-k 30 --chunk-quota 12 \
  --only-questions specs/033-chunk-first-contract-repair/diagnosis/probe-64.txt \
  --evidence-assembly --assembly-legacy-entity-order \
  --assembly-diagnose --trace-mediation=false
```

Expected: both have 64 records and identical input candidate closures; treatment multi-hop records report
`entity_order=kind_layered` and 100% chunk-before-fact; legacy reports `legacy_grouped`; answer/judge calls are 0.

## 3. Paid 64-question three-arm gate

Do not run without an explicit budget approval. Credentials stay in environment variables and never enter scripts,
tracked files, logs, or tool output. Export `LOCOMO_NO_THINKING=0`, leave legacy IDK retry enabled
(`--no-idk-retry=false`), use the same binary, fresh run dirs, and interleave arm launches in one time window.
Run `run033.sh --estimate <scratch>` first, then `--smoke <scratch>` and poll with `--status`. The full `--start`
refuses to run until the one-question smoke has a non-empty answer, positive provider input usage, one judge call,
and the same binary digest. A/B/C then use independently copied, manifest-identical snapshots of the frozen store;
they never concurrently open the source SQLite files.
Freeze the v4-pro answerer through its OpenAI-compatible endpoint; the driver requires at least 1000 smoke input
tokens so an Anthropic cache-miss-only usage count cannot be mistaken for complete context accounting.

Common recipe:

```text
--chunks --retrieval hybrid --top-k 30 --chunk-quota 12
--force-answer --judge-mem0-aligned --repeats 3
aggregate concurrency 32: A=11, C=11, B=10 when launched simultaneously
--trace-mediation=false
```

Arms:

```text
A baseline (probe-64):  --evidence-assembly=false
B legacy (18 multi-hop): --evidence-assembly --assembly-legacy-entity-order
C treatment (probe-64): --evidence-assembly --assembly-audit
```

B also uses `--assembly-audit`. This audit is write-only and runs inside the same paid answer pass; do not substitute
the retrieval-only `--assembly-diagnose` mode, which exits before answer/judge calls. For all 19 IDs in
`diagnosis/chunk-gold-19.txt`, pair each A result's provider `answer_context_tokens` with C's assembly record and
report `total_tokens`, `cap`, `tokens_estimated`, admitted units, input candidates, and
`truncated=(admitted<input candidates)` for every repeat. Use `diagnosis/chunk-gold-map.json` only in the
post-result analyzer to report whether the frozen gold chunk is admitted. `tokens_estimated=true` forbids a
token-exact claim.

After all results are frozen, report C-vs-A separately for the 16 IDs in `diagnosis/chunk-gold-rank19.txt` and the
remainder. The source backstop says 14, but its own rank list and frozen trace produce 16 for the literal `rank>=19`
predicate; the explicit list is authoritative and is never supplied to the execution driver.

GO only if C−A on target-32 is at least +8 and A−C on guard-32 is at most 1. Always report B↔C flips
separately on the 18 multi-hop questions. Planned primary answer/judge decisions are 438; any adaptive IDK
answer/rewrite provider calls are additional and must be recorded. Otherwise write NO-GO verdict and stop before
full-set.

## 4. Full-set promotion gate

Run only after §3 GO. Use A and C, same binary and frozen receipt, 1540 questions × 3 reps. Success requires:

- treatment ≥1387/1540 (strictly >90.00%);
- paired treatment uplift is positive and fully reported;
- no category with a net-negative exact McNemar result significant at `p < 0.05` after Holm correction;
- 1540/1540 coverage and complete call/cost/protocol artifacts;
- no paid cloud reranker/recall.

The July 89.03 result is historical context only, never the paired control.
