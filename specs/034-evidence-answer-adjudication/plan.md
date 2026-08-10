# Implementation Plan: Evidence-Grounded Answer Adjudication

**Branch**: `worktree-034-evidence-answer-adjudication` | **Date**: 2026-08-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/034-evidence-answer-adjudication/spec.md`

## Summary

Add a benchmark-only, three-phase answer-adjudication workflow to `cmd/locomo-bench`: (1) build deterministic,
label-blind packets from three frozen candidate journals plus a canonical top-30 evidence trace; (2) run an explicitly
opted-in answer-side verifier that may select only a packet candidate and otherwise falls back to a deterministic text
control; (3) seal every decision before a separate process joins historical verdicts for Stage-0 scoring. The execution
surface never reads gold or correctness, makes no retrieval/rerank change, and leaves the engine and default benchmark
paths byte-identical.

## Technical Context

**Language/Version**: Go 1.25.0, `CGO_ENABLED=0`

**Primary Dependencies**: Go standard library; existing `provider.Provider`, `provider/openai`, `memory.EntryStore`,
the registered pure-Go SQLite driver, and `cmd/locomo-bench` digest/statistics helpers; no new dependency

**Storage**: Immutable JSON/JSONL artifacts in an operator-selected adjudication directory; the ten frozen SQLite
conversation stores are opened with `mode=ro&immutable=1`, then read only through public `memory.EntryStore` methods

**Testing**: Go unit, contract, and CLI integration tests with injected offline `usageModelCaller` stubs; full
`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` and `CGO_ENABLED=0 go build ./...`

**Target Platform**: Linux/WSL2 CLI; hosted OpenAI-compatible verifier is optional and default-off

**Project Type**: Existing benchmark CLI / evaluation infrastructure

**Performance Goals**: Deterministically materialize 1540 packets and validate 771 triggered packets offline; bound model
concurrency at the requested 32 calls; stream decisions to a crash-auditable journal and sort only at seal time

**Constraints**: No hidden labels in execution artifacts; no paid cloud reranker/recall; one verifier attempt per
triggered packet; no fourth answer; no source-store mutation; no secret persistence; no changes below `memory/`,
`embedding/`, `provider/`, `store/`, or `internal/`; exact candidate-generation context is unavailable for 8 questions
and must not be claimed

**Scale/Scope**: Frozen LoCoMo cat 1–4 cohort, 1540 questions, three candidates/question, one canonical top-30 evidence
bundle/question, 771 planned verifier calls

## Constitution Check

*GATE: Passed before Phase 0 and re-checked after Phase 1.*

| Principle | Pre-design gate | Post-design evidence |
|---|---|---|
| I. Local-first, offline by default | PASS — build, validation, stub execution, seal, and score are offline; hosted verifier requires an explicit mode and paid-call acknowledgement. | PASS — contracts make `ADJUDICATOR_*` optional and require no network for packet/score paths. |
| II. Engine/adapter separation | PASS — feature is confined to the existing benchmark adapter and consumes public engine/provider APIs. | PASS — planned source diff is only `cmd/locomo-bench/` plus feature docs; engine directories stay untouched. |
| III. Contract-first & namespace isolation | PASS — artifact and CLI contracts precede code; no memory namespace behavior changes. | PASS — schema, state transitions, error semantics, and digest rules are frozen in `contracts/`. |
| IV. Evaluation regression gate | PASS — no retrieval/extraction/curation/storage/embedding behavior changes; default benchmark path remains off/parity. | PASS — offline parity tests and an empty engine diff prove invariance; Stage-0 is explicitly diagnostic, not a new baseline. |
| V. Graceful degradation & honest scale | PASS — invalid/failed verifier outputs fall back deterministically; reconstructed evidence limitation is declared. | PASS — receipts expose fallbacks, 8 context-parity exceptions, unpriced usage, and historical-label instability. |

No constitutional violation requires a complexity exception.

## Phase 0 Research Conclusions

The decisions and rejected alternatives are recorded in [research.md](research.md). All technical unknowns are resolved:

- the feature extends the existing bench CLI through early-dispatched build/run/score modes;
- execution input is a sanitized projection, never the label-bearing `result` type;
- source artifacts and source DBs receive custody hashes but cannot influence label-blind permutation digests;
- canonical evidence is reconstructed from sanitized trace names through temporary store copies and public APIs;
- strict JSON candidate selection, one provider attempt, deterministic fallback, append-only journal, and terminal seal
  make partial/error behavior auditable;
- score joins hidden verdicts only after seal validation and reports historical majority, executable control, oracle,
  instability sensitivity, selected score, and category gates separately.

## Phase 1 Design

### Build phase

Strictly decode each candidate file into a sanitized input type containing only identity, question/category, answer,
regime, retrieval flags, and context-token receipts. Strictly decode the attribution trace into only `(conv,q)` and
ordered `(name,rank)` hits. Raw file hashes go to a custody receipt; sanitized digests alone determine protocol,
candidate canonicalization, trigger decisions, and packet permutation.

For each question, canonicalize candidates by normalized text, original text, and a label-independent sanitized-source
digest. Trigger exactly when the three ASCII-normalized answers are not all equal. Keep all three candidate texts in the
packet (including duplicates), but omit source/run identity. Reconstruct evidence lines in trace rank order through an
immutable read-only handle to the relevant `convN.db`; hash source DBs before and after. Record the 8 candidate-context
token-drift questions (5 triggered) as provenance limitations, not as cohort selectors. Reports show all 771 triggers,
the 766 context-parity triggers, and the 5 drift triggers separately.

The build writes public `manifest.json` + `packets.jsonl`, a score-only private slot map with no labels, and a custody
receipt. Any missing/duplicate question, hit, entry, rank, candidate, evidence, or receipt fails the whole build before
network use. Because the ordinary historical journals lack a formal protocol binding model revision to outputs, custody
records their archived deepseek-v4-pro identity only as a legacy operator claim, never verified provenance.

### Run and seal phase

`--adjudication-run` reads only public packets/manifest. It computes fallback directly from blinded packet text and never
loads the score-only slot map or raw sources. A hosted provider additionally requires `--adjudication-allow-paid` and
complete `ADJUDICATOR_PROVIDER`, `ADJUDICATOR_BASE_URL`,
`ADJUDICATOR_MODEL`, `ADJUDICATOR_MODEL_REVISION`, and `ADJUDICATOR_API_KEY` environment configuration. No key or raw
provider error text is persisted.

The prompt requires strict JSON selecting an existing blinded candidate slot, at least one existing evidence ID, and
`high` confidence. Unknown fields, code fences, free answers, invalid slots/citations, lower confidence, or provider
failure produce the deterministic text-control fallback. Each triggered packet gets at most one provider attempt.
STARTED and terminal journal records are fsynced; an orphan STARTED makes resume invalid rather than risking a duplicate
paid call. Non-triggered packets produce zero calls and deterministic control decisions.

After every packet has exactly one terminal decision, the runner sorts decisions by question ID, writes immutable
`sealed-decisions.jsonl`, and writes `seal.json` containing protocol/packet/prompt/model/binary/decision digests,
planned and actual calls, usage, retries (=0), fallbacks, exit state, and optional priced cost. Missing price metadata is
reported as `unpriced`, never as zero cost.

### Score phase

`--adjudication-score` first validates the seal without opening label-bearing inputs. Only then does it strictly reload
the same three raw candidate files, verify custody hashes, and join each blinded selection to its historical verdict.
The report keeps these metrics distinct:

- historical verdict-majority: 1371/1540 (not an answer selector);
- executable deterministic text control: 1368/1540;
- candidate oracle: 1411/1540;
- trigger cohort: 771; correctness-mixed: 96, of which 88 triggered;
- judge instability: 13 questions, of which 5 triggered, with best/worst sensitivity bounds;
- selected-answer historical mapping, paired flips, per-category results, fallback/call/usage/cost receipts.

Stage-0 is GO only when the selected answer maps correct on at least 1387/1540, at least 69/88 triggered mixed-verdict
questions are selected correctly, no category has a Holm-corrected significant negative regression, and every
integrity/contamination/completeness gate passes. The artifact remains labelled historical mapping; a formal score
requires later paired candidate generation and independent rejudging.

## Project Structure

### Documentation (this feature)

```text
specs/034-evidence-answer-adjudication/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── adjudication-cli.md
│   └── artifact-schemas.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── main.go                         # additive flags + early mode dispatch only
├── answer_adjudication.go          # pure normalization/build/selection/scoring logic
├── answer_adjudication_artifact.go # strict I/O, custody, journal, seal validation
├── answer_adjudication_cli.go      # build/run/score orchestration + provider wiring
├── answer_adjudication_test.go
└── answer_adjudication_cli_test.go
```

**Structure Decision**: Keep all implementation in the existing LoCoMo benchmark `main` package so it can reuse prompt
rendering, provider callers, exact paired statistics, and atomic artifact helpers without exporting benchmark-only APIs
or creating a second evaluation command. New files isolate the workflow; existing `main.go` changes are limited to
options, flags, and pre-`--data` dispatch.

## Complexity Tracking

No constitution violations.
