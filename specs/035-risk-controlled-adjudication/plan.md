# Implementation Plan: Risk-Controlled Second-Pass Adjudication

**Branch**: `worktree-035-risk-controlled-adjudication` | **Date**: 2026-08-10 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `specs/035-risk-controlled-adjudication/spec.md`

## Summary

Extend the benchmark-only 034 workflow with a separate second-pass audit protocol. It validates the frozen parent
label-free journal/seal, derives a 477-question risk queue, collapses duplicate normalized answers, builds two
candidate-order/role-complementary views per question, and conservatively changes the parent answer only on strict dual
support/contradiction convergence. The run is explicit paid opt-in and creates exactly 954 one-attempt call receipts;
hidden historical labels load only after a new 1540-decision seal validates. No retrieval, reranker, engine, or default
benchmark behavior changes.

## Technical Context

**Language/Version**: Go 1.25.0 with `CGO_ENABLED=0`

**Primary Dependencies**: Go standard library; existing 034 adjudication schemas/helpers; existing `provider.Provider`,
OpenAI/Anthropic adapters, `usageModelCaller`, atomic JSON writers, exact McNemar and Holm helpers; no new dependency

**Storage**: Immutable JSON/JSONL artifacts in an operator-selected session-scratch directory; read-only validation of
the frozen 034 artifact directory; no database access and no mutation of parent artifacts

**Testing**: Go unit/contract/CLI integration tests with injected offline usage callers and spy hidden loaders;
`CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` plus `CGO_ENABLED=0 go build ./...`

**Target Platform**: Linux/WSL2 CLI; hosted audit is optional, explicit, and default-off

**Project Type**: Existing LoCoMo benchmark CLI / evaluation infrastructure

**Performance Goals**: Validate 1540 parent inputs offline; build 477 packets and 954 views deterministically; bound
hosted in-flight calls at operator concurrency 32; schedule exactly 954 one-attempt calls for a valid Stage-0 seal

**Constraints**: No hidden labels before seal; provider views hide parent selection/control/multiplicity/source; no raw
provider output/error/key persistence; no automatic retries; no fourth answer; no adaptive call short-circuit; parent and
new artifacts remain immutable; no changes under `memory/`, `embedding/`, `provider/`, `store/`, or `internal/`

**Scale/Scope**: Frozen LoCoMo cat 1–4 cohort, 1540 final decisions, 477 risk questions, 954 audit calls, two or three
unique normalized-answer groups per risk packet, E01–E30 evidence inherited byte-for-byte from 034

## Constitution Check

*GATE: Passed before Phase 0 and re-checked after Phase 1.*

| Principle | Pre-design gate | Post-design evidence |
|---|---|---|
| I. Local-first, offline by default | PASS — build/validate/stub/seal/score fixtures remain fully offline; hosted audit is explicit paid opt-in. | PASS — CLI contract requires `--adjudication-audit-allow-paid`; ordinary and offline modes never inspect provider env/network. |
| II. Engine/adapter separation | PASS — benchmark harness only; no engine contract or runtime change. | PASS — source plan adds audit files under `cmd/locomo-bench` and minimally edits `main.go`; engine diff gate is explicit. |
| III. Contract-first & namespace isolation | PASS — new modes/artifacts/parser/resolver/seal are specified before code; no namespace behavior changes. | PASS — CLI and artifact contracts freeze strict schemas, hidden boundary, error semantics, and digests. |
| IV. Evaluation regression gate | PASS — no retrieval/extraction/curation/storage/embedding change; 034 baseline is cryptographically rebound and recomputed. | PASS — score reports paired new-vs-parent flips, exact statistics, category/Holm, temporal safeguard, and conservative GO/NO-GO. |
| V. Graceful degradation & honest scale | PASS — audit failure retains parent answer; exact 1540/477/954 scope and historical-only claim are explicit. | PASS — terminal failures are sealed, raw errors are absent, worst instability bounds gate promotion, and GO cannot become a formal score. |

No constitutional violation requires a complexity exception.

## Phase 0 Research Conclusions

All design unknowns are resolved in [research.md](research.md):

- replay the complete label-free 034 call/seal validation, but derive queue membership only from packets/decisions;
- freeze the label-blind 424 override + 53 fallback queue and exclude 1063 zero-call retains;
- collapse the three candidates into two or three normalized-answer groups before provider exposure;
- create entailment-first and falsification-first views using guaranteed-different deterministic permutations;
- require separate support/contradiction assessments with closed values and evidence citations;
- retain parent by default and switch only on strict dual convergence to one unique alternative;
- use a new two-dimensional crash journal and canonical sorted terminal-state digest;
- validate both seals before hidden loading, recompute parent 1378 rather than read the old score;
- require significance, temporal non-regression, and worst-bound gates in addition to >90 thresholds.

## Phase 1 Design

### Offline build

`--adjudication-audit-build` first invokes the frozen 034 public + journal + seal validators against the parent directory.
It then groups parent packet candidates by the frozen ASCII normalizer, computes the text-control group, and applies the
risk rule to the validated parent decision. Candidate member multiplicity and parent slot identity go only to the
label-free resolver map.

For each risk question, canonical groups receive one representative existing answer. The entailment view applies a
seeded permutation; the falsification view rotates it by one, so even two-group packets swap positions. Provider-facing
packets include only question/category, unchanged E01–E30 evidence, roles, view-local A-slots, and representative text.
The build atomically writes `audit-manifest.json`, `audit-packets.jsonl`, and `resolver-map.jsonl`, makes zero calls, and
refuses any existing execution/seal/score artifacts.

### Audit run and resolution

`--adjudication-audit-run` reads only the three 035 build artifacts. It requires a complete dedicated
`ADJUDICATOR_*` configuration and explicit paid acknowledgement. Prompt construction receives an audit view only; tests
prove no current/control/source/group-multiplicity data is rendered.

The runner schedules a fixed work list of 954 `(packet,view)` units with an independent additive journal. STARTED is
fsynced before the provider call. Valid structured assessment produces COMPLETED; provider, usage, or strict parse
failure produces terminal FAILED with a closed reason. Both are one attempt; raw output/error is discarded. Existing
terminal units may resume with identity equality; any orphan STARTED refuses the directory.

After every unit is terminal, the resolver loads the label-free map and validates both assessments. It emits 1540
numeric-order final decisions, then seals parent/build/call/decision/provider identities, counts, usage, failures, and
pricing. Failed/disagreeing/conflicting/ambiguous audits retain parent. The resolver never changes within a normalized
group and never selects a non-parent answer.

### Post-seal score

`--adjudication-audit-score` revalidates parent label-free artifacts and the entire 035 manifest/packet/map/journal/
decision/seal chain before calling a hidden loader. Only then may it reuse parent custody/slot-map/raw-candidate helpers.
The scorer recomputes the 034 baseline mapping and all frozen denominators, maps 035 final answers with the same
duplicate-answer canonical tie, and emits paired/category/temporal/instability results.

GO requires all FR-012 gates. The result remains `historical_verdict_mapping`; because the queue/resolver was proposed
after seeing 034 aggregate results, GO only authorizes a separate preregistered formal paired-rejudge feature.

## Project Structure

### Documentation (this feature)

```text
specs/035-risk-controlled-adjudication/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   ├── audit-cli.md
│   └── artifact-schemas.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── main.go                                  # additive options/flags + unified 034/035 exclusivity/dispatch
├── answer_adjudication_audit.go             # grouping, views, prompts, strict parser, resolver, score logic
├── answer_adjudication_audit_artifact.go    # 035 schemas, canonical I/O, journal, decisions, seal validation
├── answer_adjudication_audit_cli.go         # build/validate/run/score orchestration + provider wiring
├── answer_adjudication_audit_test.go
└── answer_adjudication_audit_cli_test.go
```

Existing 034 files are consumed through stable unexported helpers and otherwise remain unchanged. No 031/033
assembly/relation/trace file and no engine directory is edited.

**Structure Decision**: Keep the additive workflow in the existing `main` package to reuse frozen 034 validators,
provider callers, exact statistics, and hidden-input custody without exporting benchmark-only APIs. Separate audit files
isolate the new schema and two-view journal; modifying the 034 runner/journal would create avoidable regression risk.

## Complexity Tracking

No constitution violations.
