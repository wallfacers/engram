# Research: Risk-Controlled Second-Pass Adjudication

## R1 — Parent 034 validation and inheritance

**Decision**: Treat the frozen 034 run as a parent protocol. Build first replays validation of its manifest, packets,
label-free call journal, sealed decisions, and seal, then derives the 035 queue only from packets and decisions.

**Rationale**: The journal is available and lets 035 independently re-check that the parent seal has no orphan or
duplicate call state. `stage0-score.json`, slot map, custody, raw candidates, gold, and correctness remain unopened.
Frozen mode binds the exact parent protocol, packet-set, decision-set, prompt, seal-file, and call-journal receipts.

**Alternatives considered**:

- Trust only four parent files and inherit the seal attestation: label-blind but cannot replay orphan detection.
- Read the old score to obtain 1378: leaks hidden outcomes into build and is forbidden.
- Copy or modify the 034 directory: breaks immutability and custody.

## R2 — Label-blind risk queue

**Decision**: Include a question iff its valid 034 decision is (a) accepted and its selected normalized answer differs
from the deterministic text-control normalized answer, or (b) a triggered fallback. This recomputes 424 + 53 = 477;
all other 1063 decisions are retained with zero calls.

**Rationale**: Every 034 control-only harm is inside the observable override cohort, while fallback-only repair has a
known mathematical ceiling below 90. The rule uses decision mechanics rather than hidden correctness or mixed labels.
Thirty-one questions whose selected slot differs from control but normalized answer is identical stay outside.

**Alternatives considered**:

- Only 53 fallbacks: even oracle repair reaches only 1383/1540.
- Known 22 rescuable errors or 88 mixed rows: direct hidden-label selection.
- All 771 triggers: higher cost and reopens 294 accepted decisions with no observable semantic conflict.

## R3 — Normalize before provider exposure

**Decision**: Collapse the three parent candidates into two or three unique normalized-answer groups before view
construction. Hide original multiplicity and parent C-slots. Each group binds all exact member digests and one frozen,
label-blind representative answer.

**Rationale**: Repeated answer slots must not receive extra votes or let the provider infer the 2:1 control. Group-level
assessment matches the semantic resolver and prevents favorable duplicate-slot historical mapping.

**Alternatives considered**:

- Show all three slots: duplicate text creates false multiplicity and positional asymmetry.
- Deduplicate only exact text: punctuation/case variants can still encode the control group.
- Let the provider generate a canonical answer: creates an unregistered fourth answer.

## R4 — Two complementary blind views

**Decision**: Create two domain-separated views per risk packet. View A uses a deterministic seeded permutation of
answer groups; view B rotates that ordering by one position, guaranteeing a different order. Evidence order is identical.
Roles are fixed as entailment-first and falsification-first. Both calls always execute.

**Rationale**: Candidate order changes reduce shared position anchoring while a fixed evidence order preserves the
experimental input. Separate calls and receipts prevent one self-consistency response from masquerading as two audits.
The same model may be used, so no statistical independence is claimed.

**Alternatives considered**:

- One call returning two opinions: still one model trajectory.
- Two random permutations: can accidentally match and complicates reproducibility.
- Adaptive second call only when the first suggests switching: changes call pattern based on model output and violates
  the fixed 954-call protocol.

## R5 — Assessment contract

**Decision**: For every view-local answer-group slot, require exact JSON with separate support and contradiction fields.
Each is `yes|no|unclear` plus evidence IDs. `yes` requires non-empty, unique E01–E30 citations; `no` and `unclear`
require an empty citation list. Every group appears exactly once; unknown fields, text, or direct recommendations fail.

**Rationale**: A single supported/contradicted/insufficient label cannot represent conflicting evidence. Separate fields
let the conservative resolver reject any current or alternative group that has both support and contradiction.

**Alternatives considered**:

- Direct recommended slot + confidence: repeats 034 and does not expose why an override is safe.
- Free-form rationale: hard to validate, may generate a fourth answer, and risks raw-response retention.
- Requiring citations for `unclear`: asks the model to cite absence and increases invalid output.

## R6 — Conservative resolver

**Decision**: Retain the exact parent answer by default. Switch only when both valid views say the current group has
`contradiction=yes` and `support!=yes`, both say the same unique alternative has `support=yes` and
`contradiction!=yes`, and neither view has another supported alternative. A switch uses the pre-frozen representative
of that answer group.

**Rationale**: The 034 deficit came from 13 harmful overrides. The resolver therefore optimizes change precision, not
change volume. Any failure, disagreement, conflict, ambiguity, or insufficient evidence retains the parent decision.

**Alternatives considered**:

- Either-view agreement or confidence threshold: expands the harm surface.
- Revert every override to control: erases the 23 prior rescues and returns toward 1368.
- Choose among exact duplicates after scoring: label leakage.

## R7 — Independent journal and deterministic seal

**Decision**: Use a new call journal keyed by `(risk_packet_id, view_id)`. Fsync STARTED before sending and one terminal
COMPLETED/FAILED record after. Orphan STARTED is permanently non-resumable in that directory. Failed views are complete
and force retain. A valid seal requires 954 starts, terminals, and attempts, zero retries, 1540 final decisions, and a
canonical call-state digest sorted by numeric question identity and view.

**Rationale**: The 034 journal is packet-keyed and cannot safely represent two calls. Additive types keep the frozen 034
workflow untouched. Sorting terminal state rather than hashing raw concurrent journal order makes the seal reproducible.

**Alternatives considered**:

- Generalize the 034 journal: risks changing a completed protocol.
- Retry orphan calls: can duplicate spend and select on crash outcomes.
- Hash raw journal bytes: concurrency makes schedule part of the identity.

## R8 — Artifact split and hidden-loader boundary

**Decision**: Build writes an audit manifest, 477 provider-safe packets, and a label-free resolver map containing parent
decision/group digests. Run writes the audit journal, 1540 final decisions, and audit seal. Score validates the parent
label-free inputs and all 035 artifacts before a hidden loader may open parent slot map/custody and three raw candidate
journals. It recomputes both 1378 baseline and new mapping; it never reads the old score.

**Rationale**: The run needs the parent selection to retain safely, but the provider must not see current/control. A
separate resolver map makes that boundary testable. Recomputing the baseline catches custody drift.

**Alternatives considered**:

- Keep a source path in the manifest: path-dependent and non-portable.
- Make the provider-facing packet carry current/control: creates confirmation bias.
- Load hidden inputs before journal/seal validation: contaminates execution and score-first tests.

## R9 — CLI and source layout

**Decision**: Add four `--adjudication-audit-*` modes plus `--adjudication-source`, seed, paid acknowledgement, and
output cap. Reuse `--concurrency`, the existing three `--adjudication-candidate` score arguments, provider construction,
usage caller, exact statistics, and 034 public/hidden validation helpers. Add new audit logic/artifact/CLI files and tests;
only minimally edit `main.go` and the global adjudication-mode exclusivity check.

**Rationale**: The existing package owns all required unexported helpers. New types preserve 034 byte/behavior parity and
avoid collision with 031/033 assembly surfaces.

**Alternatives considered**:

- A new command/package: forces export or duplicates benchmark helpers.
- Reuse the 034 mode names and directory: artifact/schema collision.
- Modify assembly or retrieval: unrelated to the diagnosed answer-selection failure.

## R10 — Provider and secret contract

**Decision**: Reuse dedicated `ADJUDICATOR_*` provider variables; require explicit audit paid acknowledgement, safe URL,
model revision, binary digest, fixed zero temperature/thinking-disabled behavior, and a 768-token output cap. Persist
only endpoint digest, structured terminal assessments, usage, and optional prices; never raw response/error or key.

**Rationale**: These variables are already isolated from answerer/judge/embedding configuration. A larger output cap is
needed for two fields across two or three answer groups. Hosted use remains optional and default-off.

**Alternatives considered**:

- Fall back to `LOCOMO_*` or `JUDGE_*`: can silently call the wrong service.
- Persist raw responses for later policy tuning: violates the frozen resolver and secret/error discipline.
- Hosted reranker/recall: prohibited and irrelevant to answer adjudication.

## R11 — Promotion and contamination

**Decision**: Stage-0 GO requires all integrity counts, point and judge-instability worst bounds >=1387/1540, triggered
mixed point and worst bounds >=69/88, new-only minus old-only >=9, overall exact two-sided McNemar p<0.05, non-negative
temporal net change, no Holm-significant negative category, and complete usage/cost identity. Output remains
`historical_verdict_mapping`.

**Rationale**: A net +9 with many opposing flips can be noise; temporal already showed weakness in 034; and 13/5
historical judge-instability can move a point estimate across 90. The new queue/resolver was proposed after seeing 034
aggregate results, so even a GO is exploratory and only authorizes a fresh formal paired-rejudge spec.

**Alternatives considered**:

- Threshold-only GO: permits noisy or instability-dependent wins.
- Select the best resolver after scoring: continued tuning on the same hidden labels.
- Claim formal >90 directly: unsupported by historical verdict mapping and legacy candidate provenance.
