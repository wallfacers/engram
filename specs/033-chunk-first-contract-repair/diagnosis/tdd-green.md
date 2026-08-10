# 033 US1 TDD GREEN Receipt

**Observed**: 2026-08-10.

After implementing the canonical kind-layered order and streaming renderer:

```bash
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'Test(MultiHop|Assemble)'
```

Both commands exited `0`; test output:

```text
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.005s
```

The passing suite covers global chunk-before-fact, coverage-first entity
groups, deterministic SourceID ties, candidate multiset preservation,
single-kind/ungrouped/empty inputs, private-label non-interference,
Units↔prompt order, repeated entity headers across kind layers, and exact-cap
canonical-prefix retention.

## US2 legacy control GREEN

The benchmark-only legacy flag, validation, answer-regime fingerprint,
input-closure receipt, `entity_order` audit, pre-033 sorter/renderer, run-dir
resume rejection and cat 2/3/4 non-interference contracts pass.

```text
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench
ok  github.com/wallfacers/engram/cmd/locomo-bench  54.512s

go test -race -count=1 ./cmd/locomo-bench -run <033 contracts>
ok  github.com/wallfacers/engram/cmd/locomo-bench  1.035s
```
