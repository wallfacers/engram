# Data Model: Evidence-Grounded Answer Adjudication

## CandidateSourceReceipt

One sanitized candidate journal source.

| Field | Meaning / validation |
|---|---|
| `sanitized_digest` | SHA-256 over sorted allowed fields only; drives canonicalization |
| `raw_custody_digest` | SHA-256 over original bytes; stored outside execution manifest |
| `question_count` | Exactly 1540 for the frozen Stage-0 cohort |
| `answer_regime` | One non-empty value, identical across all questions and sources |
| `retrieval_flags` | One non-empty value, identical across sources for each question |

Raw paths, gold answers, correctness labels, and source run names are not execution fields.

## SanitizedCandidate

| Field | Meaning / validation |
|---|---|
| `question_id` | Unique non-empty identity, consistent with `(conv,q)` |
| `conv`, `q` | Non-negative source coordinates |
| `question`, `category`, `category_name` | Identical across all three sources |
| `answer` | Non-empty original candidate answer |
| `normalized_answer` | ASCII lowercase with every non-`a-z0-9` byte removed |
| `context_tokens` | Non-negative provenance receipt; never used to trigger/select |
| `source_digest` | Sanitized journal digest; never exposed in a verifier packet |

## DeterministicTextControl

One label-blind fallback selection for every question.

1. Group the three candidates by `normalized_answer`.
2. Choose the group with greatest support.
3. Break group/representative ties by original answer lexical order.
4. If byte-identical answers remain tied, their formal output is identical; Stage-0 slot labels are handled by
   judge-instability sensitivity rather than treated as semantic distinctions.

## SanitizedEvidenceTrace

| Field | Meaning / validation |
|---|---|
| `question_id` | Derived from `(conv,q)`; exactly one trace per candidate question |
| `hits` | Exactly the frozen ordered evidence candidates required by the protocol |
| `hit.name` | Unique within question and resolvable in `convN.db` |
| `hit.rank` | Dense, unique, one-based sequence |

Gold evidence, `covers_gold`, quadrant, and correctness trace fields are never decoded into this entity.

## EvidenceItem

| Field | Meaning / validation |
|---|---|
| `evidence_id` | Packet-local dense ID `E1..En` |
| `content` | Stored entry content rendered with existing event/recorded markers |
| `rank` | Preserves sanitized trace order |
| `content_digest` | SHA-256 over rendered content |

Entry names and source session IDs stay in the private receipt, not the provider packet.

## AdjudicationPacket

| Field | Meaning / validation |
|---|---|
| `schema` | Fixed `034.adjudication.packet.v1` |
| `protocol_hash` | Digest of label-free build manifest |
| `packet_id` | Deterministic digest-derived ID |
| `question_id`, `question`, `category` | Sanitized benchmark identity/content |
| `evidence` | Non-empty ordered `EvidenceItem` list |
| `candidates` | Exactly three `C1..C3` blinded slots; duplicate answer text is allowed |
| `triggered` | True iff normalized candidate texts are not all equal |
| `packet_digest` | SHA-256 over packet fields excluding itself |

Forbidden fields include gold answer/evidence, verdict/correctness, original run ID, source digest, majority-correctness,
and candidate provenance labels.

## SlotMap

Score-only, label-free mapping from `(packet_id, C1..C3)` to a canonical candidate identity and exact answer. It is
required only after sealing to join a selected slot to historical source verdicts. The run phase never opens it and
computes deterministic fallback directly from packet candidate text.

## AdjudicationDecision

| Field | Meaning / validation |
|---|---|
| `packet_id`, `packet_digest` | Must match one packet |
| `state` | `selected` or `fallback` |
| `selected_slot` | Existing `C1..C3`; always populated after fallback resolution |
| `selected_answer_digest` | Must equal the chosen packet candidate digest |
| `evidence_ids` | Non-empty existing IDs for `selected`; empty for fallback |
| `confidence` | `high` for selected; `fallback` otherwise |
| `fallback_reason` | Closed enum for non-trigger, provider failure, invalid output, low confidence, or packet invalidity |
| `input_tokens`, `output_tokens` | Non-negative provider usage |
| `provider_attempts` | 0 for non-trigger; exactly 1 for triggered terminal decision |
| `decision_digest` | SHA-256 over terminal decision fields |

## CallJournalRecord

State machine per triggered packet:

```text
absent → STARTED → COMPLETED
                 ↘ FAILED (terminal fallback)
```

An orphan `STARTED` is not retryable and prevents sealing. Non-triggered decisions do not enter the paid-call journal.

## SealedDecisionSet

Contains protocol/packet/prompt/model/binary/decision-set digests; counts for planned/started/completed/failed calls;
token usage; retry count; fallback reasons; pricing status/cost; and terminal validity. It is valid only with exactly one
decision for all 1540 packets and exactly 771 terminal triggered decisions for the frozen receipts.

## HiddenScoreJoin

Created only after a valid seal. It loads the three original result journals, validates custody, and maps selected slots
to historical verdicts. The join derives:

- historical verdict-majority and executable text-control outcomes;
- candidate oracle and selected outcome;
- correctness-mixed and triggered-mixed strata;
- normalized-answer judge-instability strata and sensitivity bounds;
- paired flips, category comparisons, and final GO/NO-GO gates.

This entity is diagnostic and cannot be promoted to a formal benchmark result.
