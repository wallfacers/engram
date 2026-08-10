# Data Model: Risk-Controlled Second-Pass Adjudication

## ParentBaselineReceipt

Identity of the frozen, label-free 034 parent run.

| Field | Validation |
|---|---|
| `parent_protocol_hash` | Exact frozen 034 protocol digest |
| `parent_packet_set_digest` | Recomputed from 1540 validated packets |
| `parent_decision_set_digest` | Recomputed from 1540 validated decisions |
| `parent_prompt_digest` | Must match the current frozen 034 prompt |
| `parent_manifest/seal/calls raw digests` | Bind exact source bytes without paths |
| `parent_question/triggered counts` | 1540 / 771 |
| `parent_selected/fallback counts` | 718 / 822; fallback includes 769 non-trigger + 53 triggered |
| `parent_provider_attempts/retries` | 771 / 0 |

The parent validator may read only manifest, packets, label-free calls, decisions, and seal. It never opens the old
score, resolver slot map, custody, or raw candidate journals.

## AuditManifest

| Field | Validation |
|---|---|
| `schema`, `protocol_hash` | Fixed 035 v1 identity and canonical digest |
| `parent` | Complete `ParentBaselineReceipt` |
| `normalizer`, `queue_rule` | Fixed ASCII normalization and risk rule IDs |
| `view_seed_digest` | Digest only; raw seed is not needed after build |
| `entailment_prompt_digest`, `falsification_prompt_digest` | Current prompt identities |
| `resolver_digest` | Frozen conservative resolver contract |
| `question/risk/override/fallback/retain counts` | 1540 / 477 / 424 / 53 / 1063 |
| `view/planned_call counts` | 954 / 954 |
| `audit_packet_set_digest` | Canonical numeric-order packet set |
| `resolver_map_set_digest` | Canonical 1540-row label-free resolver map |

Paths, timestamps, hidden labels, current/control markers, provider configuration, and credentials are absent.

## AnswerGroup

One semantic candidate group formed before provider exposure.

| Field | Validation |
|---|---|
| `group_digest` | Digest of normalized answer plus sorted exact member digests |
| `representative_answer`, `representative_answer_digest` | Label-blind lexical representative from existing candidates |
| `member_answer_digests` | One or more sorted exact digests; union across groups covers all three parent candidates |

Provider views expose only a view-local slot and representative text. They do not expose group digest, multiplicity,
parent C-slot, current/control status, source identity, or member list.

## AuditView

| Field | Validation |
|---|---|
| `view_id` | `entailment` or `falsification` |
| `view_digest` | Digest over role, question/evidence, and view-local candidate order |
| `candidates` | Two or three `A1..An` slots mapped one-to-one to answer groups |

The falsification view is a fixed one-position rotation of the seeded entailment order, so candidate order always differs.
Evidence order and content remain byte-identical across views.

## AuditPacket

| Field | Validation |
|---|---|
| `schema`, `protocol_hash`, `packet_id`, `packet_digest` | Fixed identities/digests |
| `conv`, `q`, `question_id`, `category`, `question` | Match the parent packet |
| `context_parity` | Informational parent receipt; never changes queue/resolver |
| `evidence` | Exactly the parent E01–E30 items and content digests |
| `views` | Exactly entailment + falsification |

There are exactly 477 packets. Risk reason, parent decision, parent slot, fallback reason, and text-control identity are
not fields.

## ResolverMapRecord

Label-free, non-provider-facing state needed to preserve or switch a parent decision.

| Field | Validation |
|---|---|
| `packet_id`, `parent_packet_digest` | Match parent packet |
| `parent_decision_digest` | Match validated parent decision |
| `parent_selected_slot/answer_digest/group_digest` | Exact retained decision identity |
| `text_control_group_digest` | Recomputed; used only to validate queue membership |
| `groups` | Group digest to frozen representative parent slot/answer digest |
| `risk` | True for 477 packets; false for 1063 retained rows |

The file has 1540 numeric-order records and no correctness/verdict/source fields. It is never rendered into a provider
prompt.

## CandidateAssessment

Strict provider-returned assessment for one view-local slot:

```text
slot
support.value = yes | no | unclear
support.evidence_ids
contradiction.value = yes | no | unclear
contradiction.evidence_ids
```

For either axis, `yes` requires at least one unique present E-ID; `no` and `unclear` require an empty list. Every view
slot appears exactly once and no unknown fields are allowed. Both axes may be `yes`; that is a valid conflicting
assessment but can never cause a switch.

## AuditCallRecord

State key: `(packet_id, view_id)`.

```text
absent -> STARTED -> COMPLETED
                  -> FAILED
```

STARTED binds protocol, parent identity, packet/view digests, attempt=1, and an input digest including provider/model/
revision/endpoint/max-token/binary/prompt. COMPLETED stores only a validated structured assessment and terminal digest.
FAILED stores a closed reason (`provider_failed`, `invalid_response`, `invalid_usage`) and usage, never raw output/error.

An orphan STARTED permanently invalidates the directory. A terminal FAILED is complete and forces retain. A completed
view is never called again; an unstarted sibling view may still run.

## SecondPassDecision

| Field | Validation |
|---|---|
| Parent identities | packet + parent decision digests match resolver map |
| `final_slot`, `final_answer_digest` | Existing parent candidate and group representative |
| `audit_terminal_digests` | Exactly two for risk rows; empty for retain/zero-call rows |
| `resolution` | `retained_nonrisk`, `retained_audit`, or `switched_dual_convergence` |
| `resolution_reason` | Closed enum; no provider free text |
| Usage/attempts | 2 attempts for risk rows, 0 for non-risk rows |
| `decision_digest` | Canonical digest excluding itself |

All 1540 decisions sort by numeric `(conv,q)`. A switched answer must satisfy the frozen resolver against both terminal
assessments; validation recomputes rather than trusting `resolution`.

## AuditSeal

| Field group | Contents |
|---|---|
| Parent binding | Full parent receipt and resolver-map digest |
| 035 binding | Protocol, audit packet set, prompt, resolver, binary, provider/model/revision/endpoint digests |
| Call integrity | Canonical sorted terminal-state digest; 954 started/terminal/attempts, 0 retries |
| Decision integrity | 1540 decisions, decision-set digest, retained/switched/reason counts |
| Usage/cost | Input/output tokens, closed failures, `unpriced|priced|declared_zero`, optional estimated CNY |
| Validity | No orphan/duplicate, all identities/counts/digests consistent |

Raw concurrent journal bytes are not the canonical call-state digest.

## AuditStage0Score

Created only after parent and audit seal validation, then hidden-input loading.

- frozen parent mapping 1378/1540 and triggered mixed 61/88;
- new mapping, point and judge-instability best/worst bounds;
- 1540/477/954/13/5 denominator receipts;
- parent-only/new-only paired flips and overall exact two-sided McNemar;
- category deltas, exact tests, Holm correction, and temporal net change;
- changed-risk cohort metrics, audit agreement/failure counts, usage/cost/integrity;
- gate reasons and `GO|NO_GO`, always labelled `historical_verdict_mapping`.

GO requires every threshold in FR-012. It does not constitute a formal LoCoMo score.
