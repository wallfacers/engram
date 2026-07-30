# Specification Quality Checklist: 本地训练式 Evidence Planner

**Purpose**: Validate specification completeness, testability, and non-overlap with Feature 022
before proceeding to planning
**Created**: 2026-07-30
**Feature**: [spec.md](../spec.md)

## Content Quality

- [x] No implementation details (languages, frameworks, private APIs, or file-level design)
- [x] Focused on maintainer, integrator, and evaluation value
- [x] Written so research and product maintainers can validate outcomes
- [x] All mandatory sections completed

## Requirement Completeness

- [x] No [NEEDS CLARIFICATION] markers remain
- [x] Requirements are testable and unambiguous
- [x] Success criteria are measurable
- [x] Success criteria are technology-agnostic
- [x] All acceptance scenarios are defined
- [x] Edge cases are identified
- [x] Scope is clearly bounded
- [x] Dependencies and assumptions are identified

## Feature Readiness

- [x] All functional requirements have clear acceptance criteria
- [x] User scenarios cover dependency handoff, data provenance, runtime safety, and score attribution
- [x] Feature meets measurable outcomes defined in Success Criteria
- [x] Feature 022 ownership and serial handoff are explicit
- [x] No direct Feature 022 implementation surface is claimed by Feature 023
- [x] No implementation details leak into specification

## Validation Record

### Iteration 1 — Scope collision audit

The proposed source-grounded fixed-candidate Compiler ladder was rejected as a separate parallel
feature. Feature 022 already owns source-expanded B1, exact/fit-aware packing, deterministic Need,
`EXTRACT`, optional local Planner integration, `MERGE`, Bundle validation, and the fixed-candidate
evaluation arms. Repeating that scope would be a hard contract, harness, and implementation collision.

The specification was narrowed to a strict successor: Feature 023 owns only training assets, training
stages, a self-hostable Planner artifact, and its independent promotion report. Feature 022 retains the
Ledger, retrieval/representation, deterministic Compiler, proposal validation, fallback, token gate, and
evaluation protocol. Specification review may run in parallel; data construction, training, integration,
and formal evaluation require a final merged 022 Dependency Receipt.

### Iteration 2 — Causal replay correction

The first draft incorrectly required final answer-input bytes to be identical across treatment arms.
That would make a useful Planner treatment impossible because a valid changed proposal intentionally
changes the Bundle evidence payload. FR-025 and SC-007 now require byte-identical Candidate/source
inputs and identical renderer/static-prompt/cap fingerprints, while allowing only the validated Bundle
evidence payload to differ.

### Iteration 3 — Training target validity

Training sample requirements were tightened so every target proposal must pass the same contract,
lineage, and source/span reconstruction checks as the formal 022 path. Invalid target proposals admitted
to a formal Training Asset are therefore measurable at zero.

### Iteration 4 — Independent adversarial review

Independent review found and the specification resolved three blocking ambiguities:

1. Contract-valid labels were not necessarily semantically useful. The spec now requires a
   query/evidence adequacy rubric, two independent label judgments with adjudication, and a stratified
   human audit whose adequacy rate and confidence bound must both pass before training.
2. The phrase “other benchmark” did not identify a unique primary. The Dependency Receipt now selects
   exactly one Primary Benchmark before training from the larger frozen target shortfall, with an
   explicit tie-break; the remaining benchmark is the Cross-Benchmark Guard.
3. Planner-internal timeout and caller cancellation had been conflated. Internal planning timeout may
   fall back; caller cancellation/deadline must propagate unchanged with no fallback or answer call,
   matching the 022 outward contract.

After these corrections, all blocking findings are closed.

### Iteration 5 — Verdict closure

Follow-up review found a valid but non-GO case that had no unique verdict: a positive, significant
Primary gain paired with a Guard loss beyond -0.5pp that was not itself significant. FR-029 now defines
an exhaustive, mutually exclusive order: validity failure is `INVALID`; non-positive Primary effect or
significant Guard/category harm is `STOP`; all GO conditions yield `GO`; every other valid result is
`HOLD`.

## Notes

- Names such as Candidate, Evidence Need, Proposal, Bundle, and Planner refer to Feature 022's frozen
  outward contract. They are dependency semantics, not a prescribed internal implementation.
- The score gate reuses Feature 022's pre-registered paired-effect and cross-benchmark/category
  non-regression discipline; negative `HOLD`/`STOP` results are valid outcomes.
- No plan or implementation should begin until the final 022 Dependency Receipt is `READY`.
- No blocking ambiguity remains; the specification is ready for `/speckit-clarify` or, if the dependency
  boundary is accepted as final, `/speckit-plan`.
