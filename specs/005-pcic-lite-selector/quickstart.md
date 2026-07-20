# Quickstart: PCIC-lite Span Selector

Validation guide proving the feature end-to-end. Prerequisites, then the free coverage gate,
then the gated paid step. Uses the reused artifacts from the rerank work.

## Prerequisites

- Prebuilt LoCoMo stores (`--store-dir <stores>`, bge-embedded, gitignored) and the DashScope
  reranker sidecar (bge embeddings + `gte-rerank-v2` forward) as in the rerank eval.
- Env: `EMBED_BASE_URL`/`EMBED_MODEL` (local sidecar), `EMBED_RERANK_MODEL=gte-rerank-v2`
  (+ `DASHSCOPE_API_KEY`), and for the annotate/answer steps `LOCOMO_BASE_URL`/`LOCOMO_API_KEY`/
  `LOCOMO_PROVIDER=openai`/`LOCOMO_MODEL=gpt-5.6-luna`. Secrets via env only.
- `CGO_ENABLED=0 go build ./...` green; engine dirs unchanged
  (`git diff --name-only -- memory embedding provider store internal` empty).

## Step 1 — One-time offline annotation (build-time cost, ~¥5–15)

```bash
go run ./cmd/locomo-bench --pcic-annotate \
  --data testdata/locomo/locomo10.json --store-dir <stores> --pcic-meta <dir>/pcic_meta.json
```

**Expected**: writes `pcic_meta.json` (header + per-span typed claims); re-running is a cache
hit (no LLM calls). No engine store rows added.

## Step 2 — FREE coverage gate (zero answer tokens)

```bash
go run ./cmd/locomo-bench --coverage-only --chunks --concurrency 8 \
  --retrieval hybrid+rerank,hybrid+rerank+pcic,hybrid+rerank+oracle \
  --top-k 30 --chunk-quota 12 --pcic-meta <dir>/pcic_meta.json \
  --data testdata/locomo/locomo10.json --store-dir <stores> --run-dir <dir>/cov-pcic
```

**Expected** (`coverage.json` + printed matrix): three arms with `turn_recall` per category
and `selection_survival` / `complement_drop` / `anchor_violation`. Read against the gate:

- SC-001: `hybrid+rerank+pcic` turn_recall ≥ baseline **+2pp overall or +4pp multi-hop**.
- SC-002: no category < −1pp vs `hybrid+rerank`.
- SC-003: `complement_drop` ≤ 0.05. SC-004: `anchor_violation` = 0.
- Sanity: `oracle` arm ≈ the realizable ceiling (≈ recall@60), bounding the selector.

**Decision**: gate PASS → Step 3. Gate FAIL → stop; zero answer tokens spent, record the
verdict in `eval-log.md`.

## Step 3 — GATED paid paired answer eval (multi-hop pilot, ~¥15)

Only if Step 2 passed.

```bash
go run ./cmd/locomo-bench --chunks --concurrency 8 \
  --retrieval hybrid+rerank,hybrid+rerank+pcic \
  --top-k 30 --chunk-quota 12 --only-category 1 --pcic-meta <dir>/pcic_meta.json \
  --data testdata/locomo/locomo10.json --store-dir <stores> --run-dir <dir>/ans-pcic
```

**Expected**: `paired.json` with per-arm accuracy, flips, McNemar p, verdict for
`hybrid+rerank` vs `hybrid+rerank+pcic`. SC-007: adoption requires an above-noise positive.

## Offline unit validation (no endpoints)

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/
```

**Expected**: selector role/gate/budget tests, sidecar round-trip + degradation tests,
coverage metric tests, and the parity test (`hybrid`/`hybrid+rerank` unchanged when a `+pcic`
sibling arm is present) all pass.
