# Feature Specification: Strike 3 — Abstention Gate for Adversarial Questions

**Feature Branch**: `006-strike3-abstention-gate`

**Created**: 2026-07-21

**Status**: Draft

**Input**: User description: "借鉴 Synthius 'typed-claim 结构化事实库 + 检索空结果=前提不成立=拒答' 的思想,为 engram 设计一个拒答判定机制,赢下 LoCoMo 446 道 category-5 对抗题。复用已标注的 pcic_meta typed-claim + 现有 abstention 判分脚手架。第一步必须是零成本离线探针:量化检索置信度/typed-claim 匹配能否把对抗题与可答题分开(拒答闸的 precision/recall),过免费门再花钱答题。引擎与正式 schema 零改,拒答逻辑走 adapter/bench。宪法 IV:对抗题是新口径新基线,eval 结果单独提交。"

## Context & Motivation *(informative)*

LoCoMo carries **446 category-5 adversarial (false-premise) questions** — the second-largest slice after single-hop (841). Their answer is *not present* in the conversation; the dataset's `adversarial_answer` field is a **trap**, and the correct response is to **decline** ("not mentioned"). These questions are **entirely excluded** from the current answerable baseline (overall ≈ 67.6% is computed over the 1540 answerable questions in categories 1–4).

The current standard regime is **force-answer** (no "I don't know" exit) — under it, every adversarial question is necessarily answered, hence necessarily wrong. This feature opens abstention as a *distinct operating point*, targeting the adversarial slice while honestly measuring the cost it imposes on answerable questions (false abstention).

**Honesty constraint (Constitution IV):** adversarial accuracy is a **new metric with a new baseline** — it is not additive to the 67.6% answerable number. Results are reported as a trade-off frontier (force-answer point ↔ abstain point), and eval-result commits are separate from mechanism commits.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Free offline abstention probe decides GO/NO-GO before any paid answering (Priority: P1)

As the maintainer, before spending a cent on answering, I run a **zero-cost offline probe** that scores every question with candidate abstention signals (typed-claim match and retrieval confidence) and reports how well each signal — and their combination — separates the 446 adversarial questions from the 1540 answerable ones. The probe emits a full ROC / AUC and a single GO/NO-GO verdict against a pre-declared gate. If the gate is not cleared, no paid answering happens (mirrors the PCIC-lite free-gate discipline).

**Why this priority**: This is the MVP and the cost firewall. It can produce a decisive verdict — including a NO-GO that saves money — using only the already-built store and offline signals, with no answer/judge LLM calls. It is independently valuable regardless of whether US2 ever runs.

**Independent Test**: Run the probe on the existing 10-conversation store with `pcic_meta` present; confirm it (a) invokes zero answer/judge LLM calls, (b) emits per-signal ROC/AUC and the best operating point, (c) prints a GO/NO-GO verdict against the declared gate, (d) writes no engine state and leaks no secret.

**Acceptance Scenarios**:

1. **Given** a built store + `pcic_meta` sidecar, **When** the probe runs over all 1986 questions, **Then** it produces, for each of {typed-claim match, retrieval confidence, combined}, an ROC curve (adversarial-recall vs answerable-false-abstain), the AUC, and the operating point closest to the gate — with zero answer/judge tokens consumed.
2. **Given** the probe output, **When** any signal has an operating point with adversarial-recall ≥ 40% **and** answerable-false-abstain ≤ 5%, **Then** the verdict is **GO** and the qualifying signal + threshold are recorded.
3. **Given** the probe output, **When** no signal reaches that operating point, **Then** the verdict is **NO-GO**, the full diagnostic is retained, and the feature stops without paid answering.
4. **Given** no `pcic_meta` sidecar is available, **When** the probe runs, **Then** the typed-claim signal drops out silently and the probe still reports the retrieval-confidence signal (graceful per-signal degradation).

### User Story 2 - Paid paired frontier evaluation of abstention operating points (Priority: P2)

Only after US1 returns GO **and** the maintainer explicitly authorizes the spend, I run a same-process paired answering evaluation that measures the trade-off frontier: the force-answer point (OP0, existing), a prompt-only abstain point (OP1, baseline), a hard-gate point (OP2a — flagged questions are declined without calling the answer model), and a soft-hint point (OP2b — a low-confidence signal is surfaced to the answer model, which makes the final call). Each point reports adversarial accuracy, answerable accuracy, and a combined figure, with McNemar significance where arms are paired.

**Why this priority**: Converts the offline gate signal into a measured answer-accuracy frontier. Gated behind US1 and behind explicit cost authorization; it is the only phase that spends money.

**Independent Test**: With GO recorded and spend authorized, run the paired eval on the adversarial slice plus an answerable sample; confirm the frontier table is produced, the hard-gate point skips the answer model on flagged questions, and eval results are committed separately from mechanism code.

**Acceptance Scenarios**:

1. **Given** a GO verdict and authorization, **When** the paired eval runs, **Then** it emits a frontier table with OP0/OP1/OP2a/OP2b rows, each showing (adversarial accuracy, answerable accuracy, combined) plus per-arm McNemar verdicts against the paired baseline.
2. **Given** the hard-gate operating point, **When** a question is flagged for abstention, **Then** the answer model is **not** called for that question and the response is the canonical decline (scored correct for adversarial, incorrect for answerable, per the existing adversarial gold convention).
3. **Given** the eval completes, **When** results are recorded, **Then** the adversarial metric is declared as a new baseline with rationale, and the eval-result commit is separate from any mechanism-code commit.

### Edge Cases

- **Adversarial trap where the entity IS in memory but the premise/slot is not** (e.g. "What did Caroline realize after her charity race?" — Caroline exists, the "charity-race realization" does not): an entity-only claim match will fail to flag this; the probe must reveal whether slot-level matching or the confidence signal catches it. This is expected and is precisely what the probe measures — not a defect.
- **No candidates retrieved / empty retrieval**: treated as maximum abstention confidence (premise absent).
- **`pcic_meta` present but stale/partial** (some turns unannotated): claim-match uses only available claims; missing turns contribute no claim, never a false match.
- **Reranker not configured**: confidence signal falls back to embedding cosine (the vector space used at build time); the probe records which confidence source was used.
- **Ties at the threshold** (signal exactly at τ): decision rule is deterministic and documented (e.g. `≥ τ ⇒ abstain`), so the ROC is reproducible.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST compute, per question, offline (no answer/judge LLM), two abstention signals: (a) a **typed-claim match** signal derived from the query's demand entity(+slot) against the retrieved candidates' `pcic_meta` typed-claims, graded no-match / entity-only / entity+slot; and (b) a **retrieval-confidence** signal from the top-1 and top-k aggregate relevance scores.
- **FR-002**: The signal decision (abstain vs answer) MUST be threshold-separable so the offline probe can sweep the threshold and produce a full ROC without recomputing signals.
- **FR-003**: The offline probe MUST label each question adversarial (LoCoMo category 5) vs answerable (categories 1–4) and report, for each signal and their combination, the ROC (adversarial-recall vs answerable-false-abstain), the AUC, and the operating point closest to the declared gate.
- **FR-004**: The offline probe MUST consume **zero** answer-model and judge-model tokens and MUST NOT mutate engine state or write any secret to disk.
- **FR-005**: The offline probe MUST emit a machine-readable artifact and print a single **GO/NO-GO** verdict evaluated against the declared gate (SC-003).
- **FR-006**: On absence of `pcic_meta`, the typed-claim signal MUST drop out silently and the probe MUST still report the retrieval-confidence signal; on absence of a reranker, the confidence signal MUST fall back to embedding cosine and record the source used.
- **FR-007**: The paid frontier evaluation (gated behind a GO verdict) MUST measure at minimum four operating points — force-answer (OP0), prompt-only abstain (OP1), hard-gate (OP2a), soft-hint (OP2b) — each reporting adversarial accuracy, answerable accuracy, and a combined figure.
- **FR-008**: At the hard-gate operating point, a question flagged for abstention MUST NOT invoke the answer model; its response MUST be the canonical decline, scored via the existing adversarial-gold convention.
- **FR-009**: Paired arms MUST run in the same process/time window so run-level backend drift cancels in McNemar (per the established paired-eval discipline).
- **FR-010**: All mechanism logic MUST live in the benchmark/adapter layer; the engine packages (`memory/ embedding/ provider/ store/ internal/`) and the shipped schema/migrations MUST be unchanged (verified by an empty `git diff` over those paths).
- **FR-011**: Adversarial accuracy MUST be reported as a new baseline with explicit rationale; eval-result artifacts/commits MUST be separate from mechanism-code commits (Constitution IV).
- **FR-012**: The paid frontier evaluation MUST NOT run without both (a) a recorded GO verdict from the probe and (b) explicit maintainer cost authorization.

### Key Entities *(include if feature involves data)*

- **Abstention signal**: per-question pair of (typed-claim match grade, retrieval-confidence score) plus the derived abstain/answer decision at a given threshold. Depends on retrieved candidates, `pcic_meta` claims, and engine entity signals; produced entirely offline.
- **Probe report**: per-signal ROC points, AUC, best operating point, the gate comparison, and the GO/NO-GO verdict. The sole US1 deliverable.
- **Operating point**: a labeled configuration on the trade-off frontier (OP0 force-answer, OP1 prompt-only, OP2a hard-gate, OP2b soft-hint) carrying (adversarial accuracy, answerable accuracy, combined) and, where paired, a McNemar verdict.
- **Adversarial question**: LoCoMo category-5 item whose correct response is to decline; its `adversarial_answer` is a trap, not truth (existing convention reused).

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The offline probe scores all 1986 questions and emits per-signal ROC + AUC + best operating point using **zero** answer/judge LLM calls (verified by a test asserting no answer/judge caller is invoked).
- **SC-002**: The probe runs to a verdict on the existing 10-conversation store with no new build required (reuses persisted extraction + `pcic_meta`), keeping US1 at zero incremental model cost.
- **SC-003 (FREE GATE — the cost firewall)**: **GO** if and only if some signal (typed-claim, confidence, or combination) has an operating point satisfying **all three** of: **adversarial-recall ≥ 40%**, **answerable-false-abstain ≤ 5%**, and **net ≥ +100 questions** over force-answer across the 1986-question set (net = 446·recall − 1540·false-abstain·0.676 baseline answerable accuracy). Otherwise **NO-GO**, and no paid answering occurs. (Correction: an earlier draft called the net-≥100 form "equivalent" to the recall/false-abstain pair; it is not — the three are independent AND-ed criteria, which is what the harness enforces. A point can pass net while missing the recall/false-abstain corner, so all three must hold.)
- **SC-004**: Regardless of GO/NO-GO, the probe retains a full diagnostic (ROC/AUC per signal) so a NO-GO yields an actionable finding, not just a stop.
- **SC-005**: If (and only if) US2 runs, the frontier table reports all four operating points with adversarial/answerable/combined figures, and the hard-gate point demonstrably issues zero answer-model calls on flagged questions.
- **SC-006**: The engine and shipped schema are provably untouched: `git diff --name-only` over `memory embedding provider store internal` and the migrations is empty for all mechanism commits.
- **SC-007**: The adversarial result is recorded as a declared new baseline with rationale, in an eval-result commit separate from mechanism code.

## Assumptions

- The maintainer runs evaluations offline against a locally built 10-conversation store; the already-annotated `pcic_meta` sidecar (from feature 005 US2) is available and reused. Its dia_id keys are conversation-scoped.
- The query's demand entity(+slot) is derivable offline from existing engine entity signals (no query-time LLM), reusing the mechanism proven in feature 005.
- Retrieval-confidence is available offline: rerank scores when a reranker is configured (DashScope `gte-rerank-v2` per the 003 setup), else embedding cosine over the build-time vector space.
- The existing abstention scoring scaffolding is reused as-is: the adversarial-gold judge convention and the abstain-oriented answer prompt (with its 1:4 refusal in-context ratio).
- The 0.676 baseline answerable accuracy used in the SC-003 net calculation is the current effective answerable baseline; it is a planning constant for the gate arithmetic, not a claim requiring re-measurement.
- Paired same-process evaluation with McNemar is the judged-comparison discipline; cross-window comparisons are reference-only.
- US2 cost is bounded by the adversarial slice (446) plus an answerable sample at low repeats; the exact spend estimate is produced before the run and authorized explicitly.
