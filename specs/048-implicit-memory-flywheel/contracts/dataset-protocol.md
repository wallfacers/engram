# Contract: Agent Memory Trigger Bench Dataset & Holdout Protocol

**Feature**: `048-implicit-memory-flywheel`

**Contract version**: 3

**Date**: 2026-09-01

## 1. Scope and split boundary

The benchmark has two separate formal splits:

| Split | Cases | Purpose | May guide skill tuning? |
|---|---:|---|---|
| `dev-regression core` | 172 | official development/regression score | yes, through the flywheel |
| `holdout` | 96 | official generalization score | **no** |

The two results are two score families, not input partitions for one aggregate score. A report that publishes `172 + 96`, an average, a weighted mean, or a single combined pass rate violates this contract.

The 172 current cases remain an immutable official core manifest. A corrected/backfilled case receives a new ID under append-only `dev-extension`; its source → successor relationship is recorded in the extension manifest's append-only `extension_lineage` mapping, never by rewriting the core payload. It is not deleted or silently relabeled, and it does not change the official 172 denominator. Full dev flywheel reruns cover core + extension, while the headline `dev/regression score` continues to use core172 only.

The core172 manifest pre-registers these exact counts: implicit-write `28 positive / 28 negative`, implicit-read `28 positive / 28 negative`, trap `18 read-positive / 6 write-negative / 4 read-negative`, and regression `32` (`16 should-trigger / 16 should-not-trigger`). Its language policy reports `zh=72 / en=68` for the 140 implicit/trap cases that carry an explicit `lang`; the 32 legacy regression cases have no `lang` field and are reported separately as `regression_unclassified=32`, never folded into zh/en. The manifest case-ID list, not directory traversal, is authoritative. Legacy `skills/engram/evals/evals.json` is explicitly non-scoring and MUST NOT enter the core payload digest or case count.

Tracked dev data/metadata uses fixed names: `dev-regression-core.manifest.json`, `dev-extension.json`, `dev-extension.manifest.json`, and `dev-family-index.json` under `skills/engram/evals/`. `dev-extension.json` is the only append-only successor payload; it starts empty and never enters the core172 digest or denominator. Versioned prompt assets live under `skills/engram/evals/prompts/`. No real holdout case plaintext is stored in these paths.

## 2. Holdout composition

The following counts are exact at seal time:

| Module | Count | zh | en |
|---|---:|---:|---:|
| implicit-write-pos | 20 | 10 | 10 |
| implicit-write-neg | 20 | 10 | 10 |
| implicit-read-pos | 20 | 10 | 10 |
| implicit-read-neg | 20 | 10 | 10 |
| trap-read-pos | 8 | 4 | 4 |
| trap-write-neg | 4 | 2 | 2 |
| trap-read-neg | 4 | 2 | 2 |
| **Total** | **96** | **48** | **48** |

The authoring quota is also exact:

| Authoring host | IWP | IWN | IRP | IRN | TRP | TWN | TRN | Total |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Claude Code | 7 | 7 | 6 | 6 | 3 | 2 | 1 | 32 |
| Codex | 7 | 6 | 7 | 6 | 3 | 1 | 2 | 32 |
| OpenCode2 | 6 | 7 | 7 | 8 | 2 | 1 | 1 | 32 |
| **Total** | **20** | **20** | **20** | **20** | **8** | **4** | **4** | **96** |

Each authoring host contributes 16 zh and 16 en cases. `mixed` may be used only when the case record declares its primary scoring-language bucket; the final total remains 48/48. Authors do not choose `family_id`: a holdout author candidate must return it as null, and the controller assigns `hfam-<sha256>` from the canonical blind-semantic projection only after a committed admission CAS. A direct translation or close paraphrase must resolve to the same controller family and is therefore rejected; no family may occur in both splits, and no holdout translation pair is admitted.

The exact slot scheduler uses these `zh/en` counts:

| Authoring host | IWP | IWN | IRP | IRN | TRP | TWN | TRN | Total zh/en |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Claude Code | 4/3 | 3/4 | 3/3 | 3/3 | 1/2 | 1/1 | 1/0 | 16/16 |
| Codex | 3/4 | 3/3 | 4/3 | 3/3 | 2/1 | 0/1 | 1/1 | 16/16 |
| OpenCode2 | 3/3 | 4/3 | 3/4 | 4/4 | 1/1 | 1/0 | 0/1 | 16/16 |
| **Total** | **10/10** | **10/10** | **10/10** | **10/10** | **4/4** | **2/2** | **2/2** | **48/48** |

The holdout validator uses this exact split-specific matrix. It must not reuse the existing dev trap minima (`pos≥12`, `neg≥8`), which are impossible for a 16-case trap layer.

### 2.1 Pre-registered scenario coverage

Every holdout case also belongs to exactly one closed `scenario_bucket`:

| Scenario bucket | Total | Per author | zh/en | Module coverage |
|---|---:|---:|---:|---|
| `durable-preference` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `identity-biography` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `project-convention` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `environment-tooling` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `supersession-time` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `transience-boundary` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `attribution-secret-boundary` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |
| `workspace-session-conflict` | 12 | 4 each | 6/6 | 10 implicit + 2 trap |

Within each bucket, all four implicit modules must appear at least once, the trap pair is exactly one `trap-read-pos` plus one negative trap, and the global module totals in section 2 still apply. The scheduler solves the author × module × language × scenario constraints before generation; an author cannot substitute a self-invented category for a scheduled bucket. `category` remains a more specific bucket-local taxonomy field.

The dataset card and final report include non-gating bias slices by `evaluated host × author lane × module × language`, per-scenario metrics, self-author versus other-author gap, and an author/reviewer attempt funnel by scenario plus closed reject reason. These diagnostics never select cases or affect PASS. If a consumed version later exposes a material author-lane/scenario skew, do not edit or enlarge it in place; generate a new version (and, if needed, a genuinely independent fourth source) under a new protocol/seal.

Before holdout generation, the harness builds and freezes a `DevFamilyIndex` from the 172 retained cases. Core validation is two-stage: a `pre-index` pass validates manifest-authoritative IDs/counts, retained 020 semantics, deterministic rules and path safety without requiring legacy `family_id`; after index generation, a `family-aware` pass requires every retained ID to map to exactly one family and performs cross-split/mirror checks. `dev-family-index-v2` sorts core case IDs, extracts prompt/user turns, seed name/content and machine-rule fields, and normalizes them with LF conversion, ASCII lowercase, Unicode whitespace folding and sorted object/collection fields. Equal normalized digests join directly. Cross-language entries with equal module/category/machine-rule-shape digests become mirror candidates; each is independently reviewed in fresh Claude/Codex/OpenCode sessions using `dev-family-index-review-v1` through a bounded worker pool, and joins only when all three return `same_family=true` AND their three `canonical_family_digest` topic slugs pairwise share at least one dash-token (v2 topic alignment; the superseded v1 required byte-equal slugs and rejected 40/52 unanimous pairs on wording granularity alone, with zero true semantic divergences — see `receipts/dev-family-index-v1-superseded.json`). A `same_family` disagreement, an empty slug, or slugs with no pairwise token overlap do not join; humans cannot edit the result. Stable family IDs derive from sorted connected components, including singletons. The derivation receipt binds input payload digest, algorithm/normalizer versions, pair list, review prompt digest, three provenance receipts, frozen concurrency, observed max-in-flight, each pair result and output digest; validation with concurrency greater than one must observe actual overlap.

Authoring validation performs exact/normalized duplicate checks; the two blind reviewers additionally receive the actual anonymous, label-free summary payloads of both the frozen dev-family index and already accepted holdout families, plus canonical digests/revisions, and must each return a novelty result. Every summary entry contains only a controller-generated opaque family ID, language membership and sorted blind semantic projections of prompt/user turns, seed name/content and workspace path/content; it excludes labels/rules, author/reviewer identity, quota/batch/source/attempt and provenance. A `FamilySummaryPayload` also binds `projection_version`, the complete source index/state digest, source-family count and ordered entry root. Validation reprojects it from that full source and requires a one-to-one source-family/summary-entry mapping; deleting a source family and merely recomputing summary-local digests is invalid. Digest-only review is invalid.

Admission uses an atomic accepted-family revision/CAS and an append-only `AdmissionReceipt` chain. Every CAS attempt records the author attempt/receipt, the two exact review attempts/records, blind/private candidate and four-dimensional quota digests, reviewed/pre/post revisions and state digests, `committed|stale`, the controller-generated family reference for a commit, and a previous-receipt digest. Stale review cannot change state and must be rerun in fresh sessions against the newest summary. Seal validation starts from the empty state and replays the complete chain to the final accepted-family state; commits must correspond one-to-one with the 96 admitted cases. A non-null nearest-family reference must name an entry in the exact materialized payload. Any same-family/cross-language mirror collision rejects the candidate. This prevents a holdout family from overlapping a dev family even though legacy v1 case files did not originally carry `family_id`, including under concurrent authoring/review.

## 3. Versioned prompt inputs

Three UTF-8 files are canonical inputs:

- `dev-family-index-review-v<N>` — classifies deterministic mirror candidates while building the frozen core family index.
- `holdout-authoring-v<N>` — creates exactly one candidate for a scheduled quota slot.
- `holdout-review-v<N>` — independently validates labels and machine rules.

`lf-normalized-sha256-v1` is computed as follows:

1. Require valid UTF-8 and reject NUL.
2. Convert CRLF and lone CR to LF; change no other byte.
3. SHA-256 the resulting bytes.
4. Emit lowercase 64-character hexadecimal output.

The author/review prompt version and digest are written into every candidate/review receipt and the final manifest. A batch cannot mix prompt digests.

All structured digests in this protocol use `agent-memory-trigger-canonical-json-v1` unless an algorithm is explicitly named otherwise. Parse against the closed typed schema before hashing and reject duplicate or unknown keys, invalid UTF-8, NUL, floats and wrong JSON types. Sort object keys by raw UTF-8 bytes; preserve each schema-defined array order; emit no insignificant whitespace; encode integers in shortest decimal form without `+`, leading zero or `-0`; emit lowercase boolean/null literals; escape only JSON-required quote/backslash/control characters, using the short JSON control escapes where defined and lowercase `\u00xx` otherwise. A receipt's own digest field is excluded from its preimage, while referenced child digests remain included.

## 4. CLI authoring lanes

The only authors are these CLI lanes:

| Host | Required invocation identity | Required provenance |
|---|---|---|
| Claude Code | `claude --settings ~/.claude/settings.json.aly_qwen_w …` | CLI version, settings digest, reported resolved model or `unavailable` |
| Codex | `codex -c model_provider=aq -c model=qwen3.8-flash --yolo …` | CLI version, provider `aq` + model `qwen3.8-flash`, reported resolved model or `unavailable` |
| OpenCode2 | `opencode2 … --model <free-model>` | CLI version, explicit free-model id, resolved model, `billing_class=free` |

The exact noninteractive flags are frozen by the invocation-template digest. It is permitted to change flags only before a new batch starts; changing them creates a new batch and prompt/config receipt. Every author and reviewer attempt by a given host must resolve to the same non-`unavailable` model identity throughout the batch; hosts may share one underlying model by explicit maintainer decision (2026-09-01: all three lanes unified on Bailian qwen3.8-flash) — reviewer independence is carried by the distinct host harnesses plus the label-blind envelope, not by model diversity. Otherwise the batch cannot seal. No user writes a case, label, review, or tie-break.

The orchestration service must:

1. Create canonical private quota slots from section 2; every slot binds exactly `author × module × language × scenario_bucket`, and its digest is carried only in private author/admission receipts.
2. Start a new ephemeral CLI session and isolated input/state workspace for every candidate; the exact author child receives only its quota/prompt materialization, not a readable private root, generation audit, accepted set, prior receipt or sibling workspace.
3. Require a strict JSON private candidate that conforms to `TriggerCaseV2` plus `AuthoringReceipt`, rejects unknown keys recursively, and has `family_id=null`; any author-supplied family reference is invalid. The author receipt must reference the exact author attempt and four-dimensional quota-slot digest.
4. Reject malformed, duplicate-ID, duplicate-family, unsafe-path, secret-like, subjective-rule, or out-of-quota output.
5. Before every child launch, append an immutable `AttemptStarted` event to the batch event chain, with a unique `attempt_id`, continuous sequence, previous-event digest, stage/host/provenance and exact prompt-input digest. Append exactly one immutable `AttemptTerminal` for that ID, even if launch itself fails; it references the start event, any authoring/review/isolation/transcript receipts and a stage-specific closed outcome. No event is updated in place. Retain a private, secrets-filtered transcript receipt and an `AuthorReviewIsolationReceipt` for every launched attempt; the latter records closed exact-child probes for private-root traversal/list/read, generation-audit/receipt reads, sibling-workspace reads and own-input readability. Every author/review receipt must join one-to-one through `attempt_id`; no rejected/stale/failed event or receipt may be deleted, overwritten or omitted from the seal aggregate.
6. Continue candidate generation until the relevant accepted quota is filled; rejected candidates never change quota counts.

All model-calling work uses a bounded worker pool honoring the configured concurrency. The tool output is never mixed across authoring lanes.

Every authoring lane contributes 32 accepted cases, so each non-author reviewer lane performs 64 independent reviews. Admission happens before any evaluated-host score exists; no candidate may be selected, rejected or reshaped based on a host's trigger performance.

## 5. Blind dual review

For every schema-valid candidate:

1. Construct `BlindCandidateV1`, a recursively closed allowlist. Its top level contains exactly `schema_version=blind-candidate-v1`, one of `prompt|turns`, `seed_memories`, and path-sorted `workspace_files`. Blind turns allow only `session/role/content/setup_only`; seed memories only `name/content/event_date`; workspace files only `path/content/sha256`. Reject unknown/duplicate keys, aliases, nested extensions and any free-form “safe judging context”; do not accept then strip them. Wrap only that candidate, `blind_candidate_digest`, review prompt digest, the actual anonymous dev/accepted-holdout family-summary payloads plus their source-bound canonical digests, and accepted-family revision in the review envelope. Omit IDs/family/translation, split/membership/status, author host/model/config, authoring receipt, private candidate digest, author-specific quota slot, batch/source, candidate ordinal, author rationale, prior review output **and the author's proposed expect/module/language/scenario/category/machine rules**. `blind_candidate_digest` is SHA-256 over `agent-memory-trigger-canonical-json-v1` bytes of the exact validated `BlindCandidateV1`. Two private candidates with the same blind projection but different private labels/rules/slots/family proposals must expose the same reviewer-visible candidate digest. The envelope MUST NOT contain any field or digest from which the authoring lane or proposed label can be derived.
2. Materialize that same envelope independently into two fresh isolated reviewer input/state workspaces and send it to the two hosts other than the author. A reviewer child can read its own envelope but cannot traverse/list/read the private root, generation audit, author receipt, prior review or an active sibling workspace.
3. Require each reviewer to return strict `ReviewRecord` JSON containing its exact review `attempt_id` and author `attempt_id`, independently inferred module, language, scenario bucket, category, the complete `ExpectedBehavior` machine rules, `accept|reject`, novelty verdict/nearest existing-family reference (dev or accepted holdout), a closed reason code, provenance and review timestamp. The normalized-label digest is controller-recomputed from all inferred dimensions and all machine fields except human-only `observable`; an opaque digest without its preimage fields is invalid.
4. Accept only if both reviewers return `accept` and `novel=true`, their exact inferred fields and recomputed label digests match each other, those fields match the private author candidate, author/module/language/scenario match the complete private quota slot, and an atomic CAS confirms the reviewed accepted-family revision/source-state digest is still current immediately before admission. Under the CAS lock append an `AdmissionReceipt`: `committed` advances revision by one, adds the controller-generated family entry and binds the final case; `stale` leaves state unchanged and binds no case.
5. On either rejection, non-novelty, parse error, timeout, provenance/isolation/model-identity violation, label disagreement, private-slot mismatch or stale-family CAS, append the corresponding terminal/admission evidence. A stale-family attempt may rerun both reviews in fresh sessions against the newest summary; all other failures regenerate a new candidate for the same quota slot.

No reviewer can review its own candidate. No human can reconcile a split vote. Review-generated prose is audit metadata only; the machine rules are the scored contract.

## 6. Deterministic testability rules

Every admitted case must be judged entirely from:

- normalized engram operation trace;
- post-turn isolated engram store dump;
- deterministic staged workspace digest, where applicable; and
- predeclared `expect` rules.

`expect` may require operation types/counts, store inclusion/exclusion, answer inclusion/exclusion, same-turn acknowledgment, or honest not-found language. It must not require subjective answer quality, inferred intent beyond the stated rules, arbitrary style, or manual review.

The updated schema supports `turns` and session boundaries so holdout can cover durable write → new-session read, revised facts, long seeded distractors, cross-language recall and workspace-versus-memory conflict. Legacy one-prompt cases remain valid through the v1 loader mapping.

Current public `engram add` and MCP `memory_write` inputs do not accept structured event time. If `SeedMemory.event_date` is non-null, the v2 runner validates `YYYY-MM-DD` and prepends `[event_date=YYYY-MM-DD]` to the seeded content before using the existing CLI path. The seed receipt records both the source field and rendered-content digest. Silent omission or a claim that engine `EventDate` was populated violates this contract.

## 7. Dataset sealing, confidentiality and use lifecycle

### 7.1 Integrity receipt

`agent-memory-trigger-dataset-sha256-v1` computes the dataset payload digest over case payload files only. `DatasetManifestV2` and `DatasetSeal` are explicitly excluded:

1. Read the manifest's sorted `payload_files[]`, each containing `relative_path`, LF-normalized file digest and sorted unique `case_ids`; collect exactly those files and no manifest, seal, directory-discovered or legacy extra file.
2. Verify every manifest `case_id` appears in exactly one payload-file entry, every file-declared case exists in that file, and the union is exactly the manifest `case_ids`; verify each named file digest.
3. Convert paths to `/`-separated UTF-8 relative paths; sort by raw UTF-8 bytes.
4. Normalize each file by `lf-normalized-sha256-v1` step 2.
5. Feed one SHA-256 state per file as `path`, NUL, decimal byte length, NUL, normalized bytes, NUL.
6. Emit lowercase hex digest.

After writing that case-only digest into `DatasetManifestV2.payload_digest`, populate every remaining pre-seal field including counts, `sealed_at`, the attempt event-chain root/start/terminal/reason counts, admission-chain root/count/final family-state/summary digests and isolation aggregate. Then compute `DatasetSeal.manifest_digest` over `agent-memory-trigger-canonical-json-v1` serialization of the completed typed manifest excluding only its `seal` object. No digest preimage may contain the field that stores that same digest. Writing or mutating a manifest field after this calculation invalidates the seal.

The immutable anchor signs or content-addresses the exact canonical `DatasetAnchorV1` object containing schema version, dataset ID/version, manifest digest and dataset payload digest. A `git-tag` must be an annotated tag whose target blob bytes exactly equal that object; a `detached-signature` must verify those exact bytes under a configured trusted public-key fingerprint; an `immutable-object` ID must be the content address of those exact bytes. Anchor preimage/content digests, tag target/signature/object content and trusted-key evidence must all match; a caller-provided opaque `anchor_id` alone is invalid.

The final manifest is valid only when its case/module/author/language/scenario counts, case IDs/payload-file mapping, prompt digests, stable host-stable non-`unavailable` author/reviewer model identities across **every** attempt, closed-schema label-blind review receipts, replayed AdmissionReceipt CAS chain/final source-bound family summary, append-only started/terminal event chain, all launched author/review isolation receipts, payload digest, canonical manifest and verified anchor all match. Seal validation recomputes both chains and aggregates; a missing/duplicate/unmatched attempt event or receipt, omitted rejected/stale/failed attempt, missing/duplicate admission, hidden model drift, reason/count mismatch, missing launched-attempt isolation receipt or orphan committed case fails closed.

The dataset seal proves case admission, provenance, immutable payload integrity and the aggregate of **author/reviewer-stage** exact-child isolation receipts. It MUST NOT contain or claim a future evaluated-child `ProtectedExecutionReceipt`, `HoldoutBindingReceipt`, formal worker-capacity result or formal execution access-probe result. Those depend on the final evaluated candidate and invocation templates and are created later by formal `series prepare` or ordinal 1.

### 7.2 Confidentiality boundary

A digest proves integrity, not secrecy. Before official evaluation, holdout case content must live outside the tuning checkout in a protected directory/check-out/user/container. The evaluated CLI receives only the materialized case workspace and current prompt. It must not receive the complete holdout, author/review audit, dev failure archive, or a readable path to them. Every formal holdout artifact that can contain case plaintext — including raw/normalized event streams, store dumps, workspace receipts, case receipts and failure receipts — inherits the same protected-root boundary; only plaintext-free aggregate report/seal summaries may leave it.

Before a formal series is sealed, `series prepare` must separately prove the exact evaluated child cannot traverse, list or read the protected root; cannot read author/review audit or state roots; can read its own workspace; and, when concurrency is greater than one, cannot read any simultaneously active sibling workspace. Every primary case then runs with a disposable, never-reused HOME/XDG/cache/session/container state root and records denied reads of controller-confirmed prior-case state plus retired-case workspace. Core and holdout use disjoint allocators; holdout roots are reserved and untouched until its leg starts. Every concurrent worker needs an independent user/container/mount/ACL-equivalent boundary, and formal roots must be distinct from author/review roots. For every denied probe, the controller must record target existence/content and actual parent-policy digest immediately before child launch; `not-found` without that proof is not success. A repository-external path or separately chmod'd sentinel is not sufficient evidence.

The repository may retain only protocol files, generic fixtures and an opaque seal receipt until public release. A plaintext holdout committed to the same filesystem as a `--yolo` evaluated agent is not a protected formal test and cannot yield a `generalization score` claim. Because the same three host families author, review and evaluate synthetic cases, even a valid score is described only as **untuned/session-isolated synthetic holdout generalization evidence**, never as proof that an underlying model has not generated or reviewed the cases.

### 7.3 Consumption

The protected holdout receipt freezes the version to one stable `CandidateBindingV1` digest. That digest covers the immutable snapshot/anchor/package-validation receipt, runner/judge/validator, exact core/holdout dataset identities, core plan, stable tool/config/execution-policy and `series-prepare` `stable_identity_digest`. It explicitly does **not** include a series ID, `FormalSeriesManifest.manifest_digest`, exact per-series green/protected/canary receipts, unique roots/times, or a `pre-holdout` receipt.

Before holdout ordinal 1, the current `official-dual` series must already have a complete core172 leg. The controller then creates a fresh `pre-holdout` `GreenTestReceipt` bound to that exact sealed manifest, its stable candidate digest and its complete core-leg receipt set. Only after verifying it may the controller atomically create the first protected `HoldoutBindingReceipt` attempt, or associate a recovery attempt with the existing receipt, and start holdout. If the series becomes invalid after binding, recovery appends a binding-ledger event and prepares a new series ID whose new manifest independently recomputes the same stable candidate digest. It must rerun core172 for all three hosts and all three ordinals in fresh roots, create a new series-specific `pre-holdout` receipt after that core leg, associate the new manifest/receipt pair with the binding ledger, and only then rerun holdout96 for all hosts and ordinals. It may not continue, join or reuse any successful ordinal/split/green-test receipt from the invalid series; the final `OfficialScoreReport` can reference only the complete recovery series. A changed stable binding input requires a new holdout version, new CLI generation, new dual review and a new seal. After any complete three-ordinal holdout series, mark the holdout `consumed` whether it passes or fails. It may not become a tuning source or be reused as unseen evidence for a changed candidate.

## 8. Validation failures

`skill-eval validate --split holdout` must fail closed on any of the following:

- count, author, language or module matrix mismatch;
- scenario bucket count/author/language/module-coverage mismatch or an unknown bucket;
- an author candidate supplies a family ID, a committed case lacks the controller-generated family ID, an author is also a reviewer, reviewer hosts are the same, review envelope exposes a proposed label/rule, reviewers disagree on module/language/scenario/category/complete machine rules, the unanimous reviewer label mismatches the private author/four-dimensional slot, or a stale accepted-family CAS changes state/admits a case;
- `BlindCandidateV1` or `ReviewRecord` has an unknown/duplicate/aliased/nested-extra field, omits an inferred-label preimage, or reviewer-visible candidate digest is not the exact canonical closed-schema digest, differs for identical blind projections, or includes a private candidate/label/slot/provenance digest;
- family-summary source-state/count/root/projection binding is missing or mismatched, a source family is omitted/duplicated, family-summary payload is missing/digest-mismatched, or a nearest-family reference is absent from the exact payload;
- a missing/invalid author or reviewer stage-isolation receipt, missing controller target-existence proof, reused ephemeral state root, readable private/audit/author-receipt/prior-review/sibling target, unreadable own input, or access-policy/template/identity mismatch;
- a missing, duplicate, renumbered, overwritten, unpaired or chain-broken AttemptStarted/AttemptTerminal event; an orphan/multiply joined authoring/review receipt; an omitted rejected/stale/failed attempt; reason/count mismatch; or model/isolation drift hidden outside the aggregate;
- a missing/reordered/forked AdmissionReceipt, invalid previous digest, CAS pre/post mismatch, stale receipt that changes state, committed receipt without exact attempt/review/quota/family join, or final accepted-family state not in one-to-one correspondence with the 96 cases;
- missing/unknown resolved-model provenance for any author/reviewer attempt;
- OpenCode `billing_class` not equal to `free`;
- holdout author/reviewer lanes with an `unavailable`, host-drifting, or non-distinct resolved model ID;
- duplicate ID/family, cross-split family collision or translation pair;
- missing/extra/duplicate payload-file mapping, case-id union mismatch, named-file digest mismatch, directory-discovered extra input, non-canonical manifest, or unverifiable/mismatched anchor preimage/content/key;
- unsafe `id` or workspace path;
- no deterministic `expect` machine rules;
- unsealed, stale, digest-mismatched or consumed-as-new dataset;
- secret-like material in public manifest or case payload.
