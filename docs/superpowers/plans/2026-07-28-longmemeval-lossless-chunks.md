# LongMemEval Lossless Chunking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop truncating oversized benchmark turns, preserve every code point in bounded chunks, and make persisted eval stores safely refresh changed chunk content and embeddings.

**Architecture:** Keep the change inside the `cmd/locomo-bench` adapter. `buildSessionChunks` will emit an oversized turn as multiple independently retrievable, speaker-attributed chunks; each fragment retains the source DiaID, while existing set-based coverage continues to deduplicate it. `ingestChunks` will reconcile deterministic chunk entries so changed content invalidates derived vectors and obsolete entries are removed.

**Tech Stack:** Go 1.25 standard library, existing `memory.EntryStore`, offline Go tests.

---

### Task 1: Preserve oversized turns without exceeding the chunk cap

**Files:**
- Modify: `cmd/locomo-bench/chunks_test.go`
- Modify: `cmd/locomo-bench/coverage_test.go`
- Modify: `cmd/locomo-bench/chunks.go`

- [x] **Step 1: Write the failing lossless-split tests**

Update `TestBuildSessionChunks` so the oversized turn contains a marker after code point 1100, then assert:

```go
long := strings.Repeat("x", 1300) + " ANSWER_AFTER_THE_OLD_CAP " + strings.Repeat("界", 300)

if !strings.Contains(joined, "ANSWER_AFTER_THE_OLD_CAP") {
	t.Fatal("oversized turn lost content after the old hard cap")
}

var reconstructed strings.Builder
longFragments := 0
for _, chunk := range chunks {
	if slices.Contains(chunk.DiaIDs, "D3:3") {
		longFragments++
		reconstructed.WriteString(strings.TrimPrefix(chunk.Text, "Caroline: "))
	}
}
if longFragments < 2 {
	t.Fatalf("oversized turn fragments = %d, want at least 2", longFragments)
}
if reconstructed.String() != long {
	t.Fatalf("oversized turn was not preserved exactly")
}
```

Retain the existing hard-cap assertion and change the DiaID assertion so ordinary turns appear once while `D3:3` appears on every fragment of that turn.

Add a coverage characterization test proving two retrieved fragments carrying the same DiaID count as one retrieved gold turn.

- [x] **Step 2: Run the focused test and verify RED**

Run:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/ -run '^TestBuildSessionChunks$'
```

Expected: FAIL because the marker after code point 1100 is missing and the oversized turn has only one fragment.

- [x] **Step 3: Implement boundary-aware lossless splitting**

Add helpers in `chunks.go`:

```go
func splitOversizedTurn(speaker, text string) []string
func preferredChunkCut(runes []rune, limit int) int
```

`splitOversizedTurn` must:

- repeat `speaker + ": "` on every fragment;
- reserve prefix space so every emitted chunk is at most `chunkMaxChars`;
- target `chunkTargetChars`;
- prefer newline, sentence punctuation, then whitespace before the target;
- fall back to a rune boundary without dropping or trimming any code point.

Change `buildSessionChunks` so an oversized turn flushes the pending normal-turn chunk, emits all fragments separately with the same DiaID, and then resumes normal packing.

- [x] **Step 4: Run focused tests and verify GREEN**

Run:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/ -run '^(TestBuildSessionChunks|TestChunkTrigger)$'
CGO_ENABLED=0 go build ./...
```

Expected: PASS and build exit 0.

### Task 2: Reconcile persisted chunks and invalidate stale embeddings

**Files:**
- Modify: `cmd/locomo-bench/chunk_upsert_test.go`
- Modify: `cmd/locomo-bench/chunks.go`

- [x] **Step 1: Write failing persisted-store tests**

Add one test that seeds:

```go
legacy := &memory.Entry{
	Name: "chunk-c0-s1-000", Content: "legacy truncated content",
	Category: "chunk", SourceSessionID: "conv0-sess1",
}
stale := &memory.Entry{
	Name: "chunk-c0-s1-999", Content: "obsolete chunk",
	Category: "chunk", SourceSessionID: "conv0-sess1",
}
```

Insert vectors for both entries, run `ingestChunks` with an oversized turn, then assert the changed entry and obsolete entry no longer have vector rows and `chunk-c0-s1-999` no longer exists.

Add a second assertion to the idempotency test: insert a vector after the first ingest, rerun unchanged input, and assert the vector remains.

- [x] **Step 2: Run the focused tests and verify RED**

Run:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/ -run '^TestChunk'
```

Expected: FAIL because current upsert preserves stale vector rows and obsolete chunk entries.

- [x] **Step 3: Implement adapter-local reconciliation**

In `ingestChunks`:

```go
existing, err := es.List(ctx)
```

Index only entries whose names start with `fmt.Sprintf("chunk-c%d-", conv.ID)`. For each expected chunk:

- if the existing content differs, call `es.Delete` before `Upsert` so embeddings/entities are removed transactionally;
- if content is identical, upsert without deleting so a valid embedding remains;
- record the name in an expected-name set.

After writing, delete previously existing conversation chunks absent from the expected-name set. Return errors rather than silently retaining a partially reconciled chunk set.

- [x] **Step 4: Run focused and package tests and verify GREEN**

Run:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/ -run '^TestChunk'
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench/
CGO_ENABLED=0 go build ./...
```

Expected: PASS and build exit 0.

### Task 3: Document the corrected measurement and validate the operator dataset

**Files:**
- Modify: `specs/016-longmemeval-crossbench/verdict.md`
- Modify: `docs/benchmark-expansion-plan.md`

- [x] **Step 1: Add the post-baseline correction note**

Document:

- baseline commit `a40b48a` used truncating chunks and remains historical;
- four stable `single-session-assistant` errors had answer-bearing text after code point 1100;
- turn coverage is DiaID-level and must not be described as answer-span visibility;
- corrected chunking requires rebuilt/backfilled chunk vectors before a new score can replace 80.80%;
- no score gain is claimed until the comparable 500-question rerun completes.

- [x] **Step 2: Validate the local dataset as strict JSON**

Run:

```bash
jq -e 'length == 500 and ([.[].question_id] | unique | length == 500)' \
  /home/wushengzhou/workspace/github/engram/testdata/longmemeval/longmemeval_s_cleaned.json
```

Expected before replacement: non-zero exit due to trailing data. Replace the ignored local file only from the official LongMemEval source, retain the corrupt copy recoverably outside tracked paths, then rerun the same command and record the source plus SHA256 in the local run notes.

- [x] **Step 3: Run final verification**

Run:

```bash
gofmt -w cmd/locomo-bench/chunks.go cmd/locomo-bench/chunks_test.go cmd/locomo-bench/chunk_upsert_test.go
CGO_ENABLED=0 go build ./...
CGO_ENABLED=0 go test -count=1 ./...
git diff --name-only -- memory embedding provider store internal
git diff --check
```

Expected: build and all tests pass, `git diff --name-only` produces no output, and `git diff --check` exits 0.

- [x] **Step 4: Commit the isolated fix**

```bash
git add cmd/locomo-bench/chunks.go \
  cmd/locomo-bench/chunks_test.go \
  cmd/locomo-bench/chunk_upsert_test.go \
  cmd/locomo-bench/coverage_test.go \
  specs/016-longmemeval-crossbench/verdict.md \
  docs/benchmark-expansion-plan.md \
  docs/superpowers/plans/2026-07-28-longmemeval-lossless-chunks.md
git commit -m "fix(locomo-bench): preserve oversized LongMemEval turns"
```
