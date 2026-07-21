# Phase 0 Research: Strike 3 — Abstention Gate

All decisions below were converged during the pre-spec brainstorm; this file records them with rationale and rejected alternatives. No open `NEEDS CLARIFICATION` remains.

## R1 — What produces the abstention signal, given hybrid retrieval never returns empty?

- **Decision**: Synthesize the signal offline from two sources, computed per question over already-retrieved candidates: (a) **typed-claim match** — grade the query's demand entity(+slot) against candidates' `pcic_meta` claims as `no-match | entity-only | entity+slot`; (b) **retrieval-confidence** — a normalized score from the candidates' existing relevance scores. Keep the raw signals separate from the abstain/answer decision so the probe can sweep a threshold.
- **Rationale**: engram's RRF hybrid always returns top-k, so there is no natural "empty lookup" to trigger Synthius-style abstention. The closest offline proxies are "no structured claim supports the premise" (typed-claim) and "nothing retrieved is confidently relevant" (confidence). Both are computable with zero answer/judge LLM.
- **Alternatives rejected**: (i) query-time LLM to judge premise support — violates local-first/offline (Constitution I) and the project's standing "no query-time LLM" line; (ii) engine-level empty-result semantics — would require engine change (Constitution II).

## R2 — How is the query's demand entity(+slot) derived offline?

- **Decision**: Reuse feature 005's `derivePCICSignals` / `EntitySignalsForQuery` (engine public API, no LLM) for the demand **entity**. For **slot**, start with entity-only matching in the probe and let the probe measure whether it separates; do not build a bespoke query-side slot extractor unless the probe shows entity-only is the bottleneck and slot data is present.
- **Rationale**: entity derivation is already proven offline. The known failure mode — adversarial traps where the entity exists but the premise/slot does not (e.g. "What did Caroline realize after her charity race?") — is exactly what the probe is designed to expose. Building slot extraction speculatively risks repeating PCIC's over-engineering; the confidence signal is the intended complement where entity-only claim-match is blind.
- **Alternatives rejected**: query-side slot extraction via LLM (offline violation); heavy regex/pattern slot taxonomy up front (speculative, YAGNI — defer until the probe justifies it).

## R3 — Which retrieval-confidence source?

- **Decision**: Primary confidence = the relevance score already attached to retrieved candidates in the correct baseline stack (**rerank score** when the reranker is configured — the 003 baseline is `hybrid+rerank` — else **embedding cosine**). The probe records which source it used and MAY report both when both are available.
- **Rationale**: The correct comparison baseline is `hybrid+rerank`; the coverage-only path already computes rerank scores, so using them adds zero marginal answer/judge cost. Embedding cosine is the offline-only fallback (Constitution V degradation). Rerank scores were shown in 003 to separate relevant from irrelevant strongly, making them the better discriminator hypothesis.
- **Alternatives rejected**: a separately trained confidence model (scope creep, needs data); RRF fused-rank position alone (coarser than the score).

## R4 — ROC and the free gate arithmetic

- **Decision**: For each signal (and a simple combination), sweep the decision threshold τ over the observed score range; at each τ compute **adversarial-recall** = flagged adversarial / 446 and **answerable-false-abstain** = flagged answerable / 1540. Emit the ROC points, trapezoidal **AUC**, and the operating point closest to the gate. GO iff some signal has a point with **adversarial-recall ≥ 40% AND answerable-false-abstain ≤ 5%**; equivalently net ≥ +100 questions over force-answer (gain = 446·recall, loss = 1540·falseabstain·0.676).
- **Rationale**: The adversarial/answerable labels are known from LoCoMo `category`, and — crucially — the abstain decision's correctness is **definitional** (adversarial + abstain = correct via `adversarialGold`; answerable + abstain = wrong). So the gate's own contribution to the frontier is computable entirely offline, no LLM. The 40%/5% point yields ≈ +126 net questions, clearly worth the paid completion; below it, the paid step is not justified.
- **Alternatives rejected**: AUC-only gate (decouples from the operating decision); requiring a *single combined accuracy* to rise (rejected in favor of the frontier framing chosen in the brainstorm — operating point C).

## R5 — Combining the two signals

- **Decision**: Report each signal's 1D ROC plus one simple combination (abstain if `confidence < τc` OR `claim-match == no-match`), swept over a small grid. Pick the reported "best" by the gate condition, not by fitting.
- **Rationale**: The probe is a diagnostic, not a trained classifier. A transparent OR/grid keeps it reproducible and avoids overfitting to 10 conversations.
- **Alternatives rejected**: logistic regression / learned weights (overfit risk on a tiny labeled set; opaque threshold).

## R6 — Reusing existing abstention scaffolding for the paid arms (US2)

- **Decision**: Reuse `adversarialGold` (judge convention: declining is correct) and the `--abstain-prompt` scaffold (1:4 refusal in-context ratio) unchanged. **Hard-gate** arm: on a flagged question, emit a canonical decline string and skip the answer model. **Soft-hint** arm: inject a low-confidence hint into the abstain prompt and let the model decide. Baseline arm: force-answer (existing).
- **Rationale**: Judged-comparison discipline and the refusal prompt already exist and are tested; reusing them keeps US2 minimal and the McNemar pairing valid (same-process arms).
- **Alternatives rejected**: a new judge convention for abstention (unnecessary; would break comparability with the recorded convention).

## R7 — Cost & phasing discipline

- **Decision**: US1 (probe) is zero answer/judge cost and requires no rebuild (reuse persisted store + `pcic_meta`). US2 (paired frontier answering) runs only after a recorded GO **and** explicit maintainer authorization, bounded to the 446 adversarial + an answerable sample at low repeats, with an `--estimate` produced first. Eval-result commit separate from mechanism commit (Constitution IV).
- **Rationale**: Mirrors the PCIC-lite free-gate that already saved a paid run. The cost firewall is the feature's headline discipline.
- **Alternatives rejected**: running the paired answering unconditionally (defeats the free gate); full 1986-question paid run at high repeats (unbounded cost with no incremental signal beyond the sample).
