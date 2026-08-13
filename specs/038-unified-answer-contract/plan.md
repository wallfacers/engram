# Implementation Plan: Unified Evidence-Grounded Answer Contract

**Branch**: `038-unified-answer-contract` | **Date**: 2026-08-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/038-unified-answer-contract/spec.md`

## Summary

Replace the experimental LongMemEval-only entity prompt with one default-off,
dataset-independent answer contract in `cmd/locomo-bench`. When enabled, the
same system prompt is selected for every dataset and category and is used by
legacy, B0/B1, compiler/replay, validation, and fixed-gold answer paths. The
contract treats memories as untrusted evidence, separates factual grounding
from useful advice/inference, resolves identity/time/update/aggregation
semantically, and permits natural uncertainty without scorer-specific wording.

The historical prompt stack remains unchanged as the control arm. A `+unified`
arm suffix supports same-process paired runs. Ambiguous prompt composition is
rejected during the isolated experiment, and both ordinary journals and formal
protocols bind the actual prompt bytes. LoCoMo and LongMemEval are regression
sets only. The checked-in 17-case behavior file is a development smoke suite,
not independent generalization evidence; promotion additionally requires a
separately authored, sufficiently sized held-out suite with blinded human
labels.

## Technical Context

**Language/Version**: Go 1.25.0, `CGO_ENABLED=0`

**Primary Dependencies**: Go standard library and existing
`cmd/locomo-bench` provider/evaluation infrastructure; no new dependency

**Storage**: No schema changes. Existing SQLite stores are read-only inputs to
the prompt experiment; results remain in gitignored run directories.

**Testing**: Offline Go unit/contract tests, full `go build ./...` and
`go test -count=1 ./...`; model-backed paired pilot/full runs only when the
answer, embedding, and judge endpoints are available

**Target Platform**: Linux/WSL2 locally; optional rented Linux GPU evaluator

**Project Type**: Evaluation CLI / host answer-layer adapter

**Performance Goals**: Treatment adds no model call and no retrieval work; the
only per-answer change is the system-prompt bytes. Evaluation reports token and
latency deltas rather than assuming they are negligible.

**Constraints**: Default-off; no benchmark examples, names, category labels,
gold phrases, paid reranker, or engine changes; control and treatment differ
only in answer system prompt; long runs detached; secrets remain environment
only.

**Scale/Scope**: One system prompt, LoCoMo categories 1–5, LongMemEval category
IDs 6–12, all answer execution paths, a 17-case synthetic development-smoke
cohort, then paired
LoCoMo 1,540-question and diagnostic LongMemEval 500-question runs when the
frozen endpoint stack is available. A promotion decision is out of scope until
a separate held-out behavior cohort meets the frozen sample-size and human
review protocol.

## Constitution Check

*GATE: Passed before Phase 0 and re-checked after Phase 1.*

| Principle | Result | Evidence |
|---|---|---|
| I. Local-first, offline by default | PASS | The contract is a local constant and all structural tests are offline. Model endpoints remain optional and replaceable; the flag is default-off. |
| II. Engine/adapter separation | PASS | All implementation changes are limited to `cmd/locomo-bench`, feature artifacts, and evaluation reports. `memory/`, `embedding/`, `provider/`, `store/`, and `internal/` remain untouched. |
| III. Contract-first & namespace isolation | PASS | Prompt semantics, routing, incompatibilities, prompt digest, and evaluation gates are frozen before implementation. No API, schema, or namespace behavior changes. |
| IV. Evaluation regression gate | PASS | This is an answer-layer eval-config change, not an engine algorithm change. Control is byte-preserved; treatment requires paired regression and reliability gates before promotion. |
| V. Graceful degradation & honest scale | PASS | The contract explicitly handles partial, missing, stale, and conflicting evidence. LoCoMo/LME results are labelled regression or post-hoc diagnostics, never real-world proof. |

**Post-Phase-1 re-check**: The design introduces no engine dependency, schema,
network requirement, namespace bypass, or scale claim. No violation requires a
complexity exception.

## Project Structure

### Documentation (this feature)

```text
specs/038-unified-answer-contract/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── fixtures/
│   ├── README.md
│   └── behavior-cases.json
├── contracts/
│   ├── answer-contract.md
│   └── evaluation-protocol.md
├── checklists/requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── runner.go                       # unified prompt and single routing hook
├── main.go                         # opt-in flag/arm, validation, journal digest
├── eval_runner.go                  # formal prompt digest/provenance
├── eval_compile_bridge.go          # existing answer hook retained
├── eval_fixed_gold_oracle.go       # existing answer hook retained
├── eval_source_validate.go         # existing answer hook retained
├── unified_answer_contract_probe.go # paired development-smoke runner
├── unified_answer_contract_eval.go  # fail-closed provider-byte pair audit
├── bench_test.go                   # routing, incompatibility, digest tests
├── eval_fixed_gold_oracle_test.go  # fixed-gold prompt-path test
├── unified_answer_contract_probe_test.go
├── unified_answer_contract_eval_test.go
└── main_unified_answer_contract_probe_test.go

docs/evaluation/reports/
├── lme-entity-verify-verdict-2026-08-13.md
└── unified-answer-contract-verdict-2026-08-13.md

memory/ embedding/ provider/ store/ internal/  # MUST remain unchanged
```

**Structure Decision**: Keep the contract in the answer host that already owns
answer prompt selection. Reuse the one answer-path hook introduced during the
entity-prompt audit, but remove its dataset/category specialization. No engine
entry point is needed.

## Implementation Strategy

1. Lock failing tests for a category-independent prompt, constant system bytes
   with/without runtime date, prompt-content denylist, paired arm selection,
   fail-fast composition, and prompt-byte fingerprints.
2. Remove the unshipped `--lme-entity-verify` prompt family and replace it with
   `--unified-answer-contract` plus `+unified` paired-arm support.
3. Route every answer path through `answerSystemPromptForEval`; in unified mode
   it returns one constant and reads no dataset/category value.
4. Preserve all historical prompts exactly as the control. Reject force,
   abstain, typed/temporal prompt, counter-refine, temporal scaffold, trace
   mediation, and category-budget composition during the isolated experiment;
   require IDK retrieval retries to be disabled.
5. Bind actual prompt bytes in normal run fingerprints and formal prompt
   digests so changed contracts cannot resume into old journals/manifests.
6. Run offline tests/build and confirm engine-zero-diff.
7. Run the 17-case behavior file only as a development smoke check. Freeze a
   fresh binary/store/config manifest, run a paired pilot, and only proceed to
   full repeats when endpoint health and pilot gates pass. If the endpoint is
   absent, record the block rather than inventing a score.
8. Keep promotion blocked until a separately authored held-out cohort is frozen,
   sufficiently sized for the declared confidence bound, and blindly reviewed
   by humans. Passing the 17 smoke cases cannot satisfy this requirement.

## Complexity Tracking

No constitution violations.
