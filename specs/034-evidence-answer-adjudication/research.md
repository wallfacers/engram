# Research: Evidence-Grounded Answer Adjudication

## R1 — Workflow placement

**Decision**: Add three early-dispatched modes to `cmd/locomo-bench`: build, run, and score.

**Rationale**: The existing package already owns LoCoMo result types, prompt rendering, provider calls, exact McNemar and
Holm statistics, and atomic/digest helpers. Separate phases provide a process boundary between label-free execution and
hidden scoring while keeping default benchmark behavior unchanged.

**Alternatives considered**:

- A new command would duplicate or force-export benchmark-only helpers.
- Extending judge-audit is invalid because that workflow selects by correctness discordance and its reviewer packets
  intentionally contain gold answers.
- One combined build/run/score process cannot prove that decisions were sealed before hidden labels became visible.

## R2 — Sanitization and label invariance

**Decision**: Strict line scanning with narrow sanitized structs; raw hashes are custody-only, while execution digests
derive solely from allowed fields.

**Rationale**: Historical `result` records contain `gold` and `correct`. Go can decode the same JSON into a narrow type
without materializing those fields. The attribution trace similarly contains gold evidence and correctness fields, so
only identity and ranked hit names may enter the sanitized projection. If raw hashes influenced permutation or packets,
mutating a hidden label would incorrectly alter execution.

**Alternatives considered**:

- Reusing tolerant result readers could silently skip malformed lines and shrink the denominator.
- Loading full `result` values and deleting fields afterwards unnecessarily exposes labels to execution logic.
- Using raw artifact SHA-256 as the permutation seed violates the label-mutation invariance test.

## R3 — Trigger, canonical candidates, and executable control

**Decision**: Normalize with historical ASCII rules: lowercase and retain only `a-z0-9`. Trigger when the three values
are not all equal. Canonicalize independently of input order. The fallback/control chooses the normalized group with the
largest support and then the lexicographically smallest original answer; a three-way tie is therefore also resolved by
original answer text. If the winning original answer is byte-identical in multiple sources, historical scoring uses the
smallest sanitized source digest as the final label-blind tie-break. Runtime output is identical whichever duplicate
slot is selected; the canonical score tie prevents legacy judge randomness from becoming a fake gain or loss.

**Rationale**: This reproduces 771/1540 label-blind triggers and an executable text-control mapping of 1368/1540. It
preserves visible 2:1 self-consistency while hiding source run identity. Historical 1371/1540 is a majority over three
hidden verdict booleans, not a realizable majority-answer selector, so it cannot be the runtime fallback.

**Alternatives considered**:

- Source file order is label-blind but not invariant to run reordering.
- Falling back to historical majority correctness would directly consume hidden verdicts.
- Deduplicating candidate text would remove legitimate self-consistency evidence.

## R4 — Evidence source and provenance limitation

**Decision**: Reconstruct one canonical evidence bundle per question from sanitized attribution trace hit names and the
same-recipe persisted store. Label it unified adjudication evidence, not exact candidate provenance.

**Rationale**: The trace provides ordered top-30 entry names; `memory.EntryStore.EntriesByName` can resolve their content
through the public engine API. The three answer journals did not save hit/context bodies. Context-token receipts drift on
8/1540 questions (5 triggered), and one run used a much wider context, so exact per-candidate prompts are unrecoverable.

**Alternatives considered**:

- Treating the trace as exact historical context would be a false provenance claim.
- Excluding the five triggered drift rows would change the preregistered cohort from 771 to 766 and bias selection by a
  post hoc artifact property; instead, report the limitation for all eight rows.
- Re-running retrieval would introduce embedding/retrieval nondeterminism and change the experiment.

## R5 — Frozen store access

**Decision**: Hash every source `convN.db`, reject unexpected WAL/SHM sidecars, open it with SQLite
`mode=ro&immutable=1`, resolve entries only through public `memory.EntryStore` methods, close it, and verify source
hash/sidecar state remains unchanged.

**Rationale**: `store.Open` intentionally applies pragmas/migrations and may create WAL state. An immutable read-only
connection prevents source mutation; the benchmark does not issue direct SQL or depend on schema columns and uses the
engine's public entry lookup for all content.

**Alternatives considered**:

- Direct SQL queries against an immutable URI would bypass the public engine API.
- Opening the source with `store.Open` risks mutating the evidence receipt.
- Copying DBs adds avoidable I/O and can be inconsistent if an unregistered WAL exists.

## R6 — Verifier contract and paid-call safety

**Decision**: Use the existing provider abstraction with a dedicated `ADJUDICATOR_*` environment namespace,
temperature zero, thinking disabled, fixed output budget, exactly one provider attempt, and an explicit
`--adjudication-allow-paid` acknowledgement. Accept only strict JSON selecting a packet slot, at least one present
evidence ID, and `high` confidence.

**Rationale**: It reuses tested OpenAI-compatible streaming and usage accounting without changing provider code. One
attempt makes paid-call count and crash recovery unambiguous. The closed output schema prevents a fourth generated
answer; every invalid output deterministically falls back.

**Alternatives considered**:

- Reusing `LOCOMO_*` silently could call the wrong endpoint/model and mix answerer with verifier receipts.
- Transparent retries obscure the actual paid attempt count and risk duplicated calls after crashes.
- Similarity-matching a free-text output back to candidates creates an unregistered selector.

## R7 — Crash safety, sealing, and resume

**Decision**: Fsync append-only STARTED → COMPLETED/FAILED journal records, refuse orphan STARTED on resume, then sort
terminal decisions and create an immutable decision-set seal before scoring.

**Rationale**: A crash after request transmission cannot reveal whether the provider charged or returned a choice.
Failing closed avoids a second paid attempt and preserves exactly-once audit semantics. Sorted sealed output is stable
regardless of concurrency completion order.

**Alternatives considered**:

- Retrying orphan calls risks double spend and response-selection bias.
- Appending only terminal records leaves an unaudited send/receive window.
- Treating the concurrent journal itself as the seal makes artifact bytes schedule-dependent.

## R8 — Historical scoring and promotion

**Decision**: Score only after seal validation. Report historical verdict-majority, executable control, oracle,
trigger/mixed cohorts, selected mapping, category pairs, and normalized-answer judge-instability bounds separately.

**Rationale**: There are 13 questions where one normalized answer has conflicting historical verdicts; 5 are triggered.
A verifier cannot semantically distinguish identical normalized answers by their old slot label. The old join is a cheap
Stage-0 screen, while formal >90 requires independent paired rejudging. The ordinary candidate journals also lack a
formal protocol manifest, so their archived model identity remains an operator claim rather than cryptographic evidence.

**Alternatives considered**:

- Selecting the favorable duplicate slot would convert judge randomness into fake gain.
- Calling the old-label mapping a new LoCoMo score would overstate the evidence.
- Overall score alone could hide a significant per-category regression.
