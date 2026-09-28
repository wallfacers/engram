# T19 slice 14 — shard-size capacity fix (ShardSize clamp [32,64] → [6,64] + eval wiring 12)

Branch `codex/051-jev-relevance-filter`, base HEAD `4fd0dd6` (clean before work:
`git status --porcelain -- cmd/ filter/` empty).

## What changed

### EDIT 1 — engine clamp, `filter/jev/jev.go`
- `filter/jev/jev.go:34` — `minShardSize = 32` → **`minShardSize = 6`** (`maxShardSize = 64`
  and `defaultShardSize = 48` untouched → production behavior byte-identical).
- `filter/jev/jev.go:65-67` — doc comment on `Config.ShardSize` updated from
  "clamped into [32,64] (default 48)" to:
  ```
  // ShardSize is the number of candidates per shard, clamped into [6,64]
  // (default 48). The floor is small because a hosted endpoint may reject large
  // pointer requests outright; a caller with room to spare keeps 48.
  ```
- Nothing else in `filter/jev` changed; the clamp block at `jev.go:123-131` and
  `shardCandidates` (`jev.go:337`) are untouched.

### EDIT 2 — harness wiring, `cmd/locomo-bench/jevfilter_protocol.go`
- `cmd/locomo-bench/jevfilter_protocol.go:752-761` — new const (rationale comment +
  value):
  ```
  // jevFilterShardSize is the per-request candidate count the eval's filter client
  // shards with, below the engine's production default of 48. Measured on the
  // 2026-09-24 pilot: the gateway 503-storms ~48-question pointer requests (~32k
  // tokens) at concurrency 32 — the shim's two retries exhausted and 96% of filter
  // rows degraded — while ~6-question requests (~4k tokens) answered in 0.8s under
  // the same load. 12-question shards (~9k tokens) stay small enough to pass, and
  // total ~18% FEWER tokens than 48-question shards because the per-question
  // pointer schema overhead dominates the shared-state copy. The engine default
  // (48) is untouched: this pins the eval, not the product.
  const jevFilterShardSize = 12
  ```
- `cmd/locomo-bench/jevfilter_protocol.go:801` — `ShardSize: jevFilterShardSize,` added to
  the `jev.Config` returned by `jevFilterConfig` (the site that already pins
  `Deadline`/`PerRequestTimeout`); `buildJevFilterClient` (`:764`) consumes it unchanged.

No files touched outside `filter/jev/` and `cmd/locomo-bench/`; `git diff --name-only -- memory
embedding provider store internal` is empty. Diff: 4 files, +233/-6.

## TDD evidence — red first, then green

Red state (before the two edits):
```
--- FAIL: TestFilterRespectsAConfiguredShardSizeBelowTheOldFloor (0.05s)
    jev_test.go:302: requests = 5, want 25 (150 candidates at 6 per shard)
--- FAIL: TestNewClampsShardSizeIntoTheDocumentedWindow (0.00s)
    --- FAIL: TestNewClampsShardSizeIntoTheDocumentedWindow/the_eval_size_is_respected (0.00s)
        jev_test.go:339: ShardSize = 32, want 12
FAIL	github.com/wallfacers/engram/filter/jev	0.074s
```
```
cmd/locomo-bench/jevfilter_protocol_test.go:1445:22: undefined: jevFilterShardSize
...
FAIL	github.com/wallfacers/engram/cmd/locomo-bench [build failed]
```
Green state after both edits: see gates below; the harness end-to-end leg now observes
13 requests of 12 memories (with the old 32 floor it would be 5 requests of 32).

## Sharding behavior table (config value → shard shape)

| `Config.ShardSize` | pool 25 (threshold 16) | pool 150 (threshold default 64) | observed shard shape |
|---|---|---|---|
| `0` / unset | — | 4 requests | 48,48,48,6 (default 48) |
| `1` | — | — | coerced up to 6 (`New`) |
| `6` | 5 requests | 25 requests | 6,6,6,6,1 / 25×6 — ≤6 respected |
| `12` (eval wiring) | — | 13 requests | 12×12,6 (~9k tokens/shard) |
| `48` (explicit, production) | — | 4 requests | 48,48,48,6 (unchanged) |
| `200` | — | 3 requests | 64,64,22 (coerced down to ceiling) |
| `1024` | — | — | coerced down to 64 (`New`) |
| pool ≤ threshold | 1 request | 1 request | single shard (no split) |

## Validation (gate commands, exact)

```
$ CGO_ENABLED=0 go build ./...
(no output — BUILD OK)

$ CGO_ENABLED=0 go vet ./cmd/locomo-bench/
(no output — VET OK)

$ CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/ ./filter/...
ok  	github.com/wallfacers/engram/cmd/locomo-bench	42.688s
ok  	github.com/wallfacers/engram/filter	0.005s
ok  	github.com/wallfacers/engram/filter/jev	4.437s

$ gofmt -l filter/jev/jev.go filter/jev/jev_test.go cmd/locomo-bench/jevfilter_protocol.go cmd/locomo-bench/jevfilter_protocol_test.go
(no output — GOFMT CLEAN)
```

New tests, verbose:
```
--- PASS: TestFilterShardPayloadStaysWithinBounds (0.06s)
--- PASS: TestShardCandidatesHonorsAConfiguredSizeBelowTheOldFloor (0.00s)
--- PASS: TestFilterRespectsAConfiguredShardSizeBelowTheOldFloor (0.03s)
--- PASS: TestNewClampsShardSizeIntoTheDocumentedWindow (0.00s)   [6 subtests: unset→48, 1→6, 6→6, 12→12, 64→64, 1024→64]
--- PASS: TestFilterDefaultConfigStillShardsAt48 (0.01s)
--- PASS: TestJevFilterConfigPinsTheMeasuredShardSize (0.06s)     [cmd/locomo-bench]
```

No existing test broke: the two pre-existing shard-shape tests keep their explicit
`ShardSize` (barrier test 48, token-sum test 48) and `TestFilterShardPayloadStaysWithinBounds`
(200 → 64) still passes; its stale "[32,64]" comment/assertion were re-pointed at
`minShardSize`/`maxShardSize`.

## Tests added

- `filter/jev/jev_test.go:253` `TestShardCandidatesHonorsAConfiguredSizeBelowTheOldFloor` (t1, direct
  on the sharding logic: 25 candidates, threshold 16, size 6 → 5 shards 6,6,6,6,1, idx order preserved)
- `filter/jev/jev_test.go:283` `TestFilterRespectsAConfiguredShardSizeBelowTheOldFloor` (t1 client-level:
  150 candidates at size 6 → exactly 25 requests × 6 memories, all 150 probabilities merged, not degraded)
- `filter/jev/jev_test.go:323` `TestNewClampsShardSizeIntoTheDocumentedWindow` (t2 + t3: below-floor/above-ceiling
  coercion, floor/ceiling/eval values respected, unset → 48)
- `filter/jev/jev_test.go:348` `TestFilterDefaultConfigStillShardsAt48` (t3/t5: plain default end-to-end → 4 reqs, max 48)
- `cmd/locomo-bench/jevfilter_protocol_test.go:1438` `TestJevFilterConfigPinsTheMeasuredShardSize` (t4: cfg.ShardSize == 12
  including under a pinned deadline, plus an end-to-end 150-candidate run against a fake `/answers`
  server → 13 requests, max 12, all probabilities returned, not degraded — proves 12 survives the engine clamp)
- `filter/jev/jev_test.go:240-245` — refreshed stale 32..64 window comment/assertion to the
  `minShardSize`/`maxShardSize` constants.

## Deviations

None. Both edits landed exactly at (or adjacent to) the guessed sites; the Config construction site is
`jevFilterConfig` in `cmd/locomo-bench/jevfilter_protocol.go` as expected.

## Residual risks / notes

- `specs/051-jev-relevance-filter/t19-p3-pilot.md:78` (historical pilot record) and the morning brief
  still describe the old `[32,64]` clamp — dated artifacts, out of this slice's file scope; the
  implementation matches the brief's S14 plan exactly.
- Changing `filter/jev` (read-filter surface, 051 amendment) technically touches a scoring path; the
  production default is pinned unchanged by tests (48 / 4 requests at pool 150), so no product-metric
  effect without the eval's explicit `ShardSize: 12`. A gated P4 re-run (not in this slice) is what
  demonstrates the throughput/cost effect claimed by the rationale comment.
- The eval wiring is unconditional on the arms path (no flag): consistent with how `Deadline`/
  `PerRequestTimeout` are already pinned there.
- No secrets involved; no staged files (`git diff --cached` empty).