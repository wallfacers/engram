# 033 TDD RED Receipt

**Observed**: 2026-08-10, after contract tests and before implementation.

Build remained green:

```bash
CGO_ENABLED=0 go build ./...
```

The new multi-hop contract suite failed as required:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench -run 'TestMultiHop'
```

Exit code: `1`.

Observed failures:

- `TestMultiHopCanonicalOrder`: old output was
  `fact-alice, chunk-alice, fact-bob, chunk-bob, fact-z, chunk-z`, proving
  group-major order let facts cross chunks.
- `TestMultiHopStableTieBreak`: equal-score order changed with input
  permutation, proving the old sorter lacked SourceID tie-breaking.
- `TestMultiHopDegenerateInputs/all_ungrouped`: old output kept a fact before
  a chunk.
- `TestMultiHopPromptMatchesAssemblyUnits`: the recorded sequence contained a
  fact→chunk crossing.
- `TestMultiHopEntityHeaderCanRepeatAcrossKinds`: the old renderer emitted one
  entity block and kept that entity's fact before later chunks instead of
  reopening the entity under the fact layer.

The private-label non-interference fixture and single-kind/empty cases already
passed; they remain regression guards during GREEN.

## US2 control-mode RED

After adding the legacy flag/fingerprint/audit/parity contracts, the targeted
test build failed before implementation as expected:

```bash
CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench \
  -run 'Test(AssemblyLegacy|AnswerRegimeFingerprintSeparatesAssembly|MultiHopLegacy|MultiHopEntityOrderAudit|NonMultiAssemblyModes)'
```

Exit code: `1`. The compiler reported the absent
`assemblyLegacyEntityOrder`, `validateAssemblyOptions`, `EntityOrder`,
`InputCandidateCount`, `InputClosureSHA256`, and entity-order constants. This
proves the tests preceded the benchmark control and receipt implementation.
