All verification complete. Findings below — returned inline as the authoritative artifact (no file-write tool is available in this review session; the runtime persists this response to the configured path).

## Review

### Prior findings — resolution status vs revised plan (v1.1)

1. **[BLOCKER-1] [RESOLVED]** — plan.md:96 Complexity row 3 now ships the B path as `cmd/engram-filter` thin CLI reusing the same filter package (structure: plan.md:80 "B 路径（FR-001 A+B 双落地）"), reconciling with FR-001 (spec.md:101) instead of rejecting it.
2. **[BLOCKER-2] [PARTIAL]** — transmission boundary fully fixed (contracts/write-gate.md:22 item 0: local secret-shape short-circuit, never-to-cloud, substring masking, 500ms fail-open, only manual `memory_write`) and FR-007 scope now names read+write + CLAUDE.md same commit (spec.md:107); but plan.md:94 Complexity row still says "仅限本 Jev 过滤面", contradicting FR-007 (→ F3).
3. **[BLOCKER-3] [PARTIAL]** — spec.md:128 adds the declared category-5 block, stated population, D-noRelax, and an operational definition; however the definition is tautological on that population and C is structurally constant, so SC-004 still cannot fail as gated (→ F1 BLOCKER).
4. **[MAJOR-4] [PARTIAL]** — research.md:49 (R7a) explicitly names four frozen manifests, mechanism-key registration, per-contrast intended variables (A↔B=budget, B↔D=filter) and the context-parity-gate replacement, plus blanket "按 038 正式协议机器执行"; per-row audit, per-repeat receipts, pilot/warm-up sequencing and the literal mechanism-key name remain unnamed (→ F5).
5. **[MAJOR-5] [PARTIAL]** — spec.md:125 SC-001 now mandates run-internal pairing + exact McNemar p + paired CI via paired_eval.go (no cross-run); promotionVerdictFor linkage is only "需显式声明" (research.md:49, required-but-not-stated) and no paired test is required for the SC-005/A↔C degraded claim (→ F4).
6. **[MAJOR-6] [RESOLVED]** — research.md R3a pre-registers θ/RelaxTheta/KShowMax/pool into the run manifest before the gated run and declares any sweep diagnostic-only, never the reported gate result.
7. **[MAJOR-7] [RESOLVED]** — spec.md:129 declares the degraded shown set ≡ arm-C shown set (C as proxy, no extra arm); contracts/relevance-filter.md:49,53-54 gives `SearchFiltered(ctx, query, pool, show, flt, pol)` with truncation ownership and layered errors (filter failure → `Degraded=true, error==nil`; only Search errors propagate).
8. **[MAJOR-8] [RESOLVED]** — spec.md:127 equalizes the gate budget (C@12 vs D, C@8 reference) and names the existing `evidenceRecallAt` seam as metric source.
9. **[MAJOR-9] [RESOLVED]** — FR-003 (spec.md:103) adds the `ENGRAM_JEV_RELAX=0` kill-switch + mandatory D-noRelax variant with SC-004 measured on it, and both variants are reported (quickstart.md:49); residual metric vacuity is tracked under F1.
10. **[MAJOR-10] [RESOLVED]** — plan.md:116 directs "Pinned 补进 `memory.Result`" (current code lacks it: memory/retriever.go:110-122) and contracts/relevance-filter.md:44 fixes trigger-match = case-insensitive containment, pinned counts toward KShowMax, pathological pinned>KShowMax override + telemetry note (data-model wording nit → F10).
11. **[MAJOR-11] [RESOLVED]** — contracts/memory-search.md:30-31 output JSON now carries `latency_ms` + `cost_usd`, matching FR-005 (spec.md:105); data-model.md's summary is stale (→ F6 NIT).
12. **[MAJOR-12] [RESOLVED]** — research R4a / contracts/relevance-filter.md:13: 1s whole-filter deadline (ctx-derived), per-request min(remaining,1s), no retries, 30s negative cache → direct degrade; write-gate 500ms fail-open (write-gate.md:22).
13. **[MAJOR-13] [RESOLVED]** — plan.md:77 names the seam `multiquery.go retrieveWithQuotaDiagnostics → SearchFiltered → packer`, shown measured at packer injection, `--top-k` split internally into pool-k/show-k.
14. **[MAJOR-14] [RESOLVED]** — plan.md:82 "AGENTS.md + CLAUDE.md … 两处镜像同 commit 修订" plus FR-007 (spec.md:107) same-commit mirror requirement.
15. **[MINOR-15] [RESOLVED]** — plan.md:29 Scale/Scope now ≤500 (matching contracts/memory-search.md:17) and the contract enumerates pool<limit→limit, theta∉(0,1)→reject, RelaxTheta>Theta→clamp, jev&pool==limit→filter top-limit, each with one test.
16. **[MINOR-16] [PARTIAL]** — when-unset acceptance exists only for `ENGRAM_JEV_*` (quickstart.md:55, spec.md:70); `ENGRAM_SEARCH_POOL`/`ENGRAM_FILTER` stay filed under skills/engram/SKILL.md (plan.md:81), are absent from the config.go knob list (plan.md:76), and no doc qualifies parity "when pool/filter env unset" or states clients cannot set server env (→ F2 MAJOR).
17. **[MINOR-17] [PARTIAL]** — research.md:49 pins the eval model to a concrete manifest revision ("禁 jev-latest 浮标"), but contracts/relevance-filter.md:21 and quickstart.md:27 still literally show `jev-latest` (→ F7).
18. **[MINOR-18] [OPEN]** — ordering is defined (contracts/relevance-filter.md:45,53: pinned-first then p-desc) but no statement anywhere that `score` stays RRF (non-monotonic vs order) nor a re-sort policy decision — grep across spec/plan/research/contracts/quickstart finds none.
19. **[NIT-19] [PARTIAL]** — no-retry ✓ (contracts/relevance-filter.md:13), only-manual-write-gated ✓ (write-gate.md:22), `--estimate` ✓ (quickstart.md:49), design-doc commit planned ✓ (plan.md:83; file still untracked per watchdog); the offline "filter=jev + no keys ⇒ zero network attempt" assertion is not pre-declared (→ F11).

### Remaining / new gaps vs OBJECTIVES

- **Obj 1** ✓ (wide pool, calibrated filter, ≤12 shown, honest empty all specified; degrade→RRF top-show).
- **Obj 2** mostly ✓ except SC-004 (F1) and significance extras (F4); per-segment latency/cost/token ✓ (SC-007, R5).
- **Obj 3** ✓ except the parity invariant is not stated "when env unset" (F2) — an explicit hard-constraint wording; secrets/local pre-detection/write-in-turn/two-surface one-commit amendment all ✓ (FR-007, write-gate item 0).
- **Obj 4** ✓ on deliverable inventory (filter/, SearchFiltered signature+layering, MCP fields, engram-filter, write gate, four arms, D-noRelax, shown-at-packer, skill knobs, results.md scoping) except validity-artifact naming (F5) and knob-reader/parity wording (F2).

### Findings

- **[BLOCKER] F1 — SC-004's operational definition is a tautology with a constant comparator; the gate cannot fail (spec.md:128, quickstart.md:49).** Population is defined as gold-absent ("金块不存在的题目（category-5）"), so the gold evidence set is ∅ (harness convention: `evidenceRecallAt` is ungradeable with no parseable refs, coverage.go:19-22); therefore the clause "全部条目与 gold 无重叠" is true for *any* hit set, the OR is always true, and the rate ≡ 1 for both C and D. Under the only alternative reading ("success = hits empty"), C is pure truncation of a populated RRF pool and is never empty, so C ≡ 0 and `D ≥ C` is again trivially true. Either way objective 2's "empty-injection rate (D-noRelax) ≥ C" is unattestable — the same acceptance defect that made prior BLOCKER-3 a block. Smallest fix: define per-question success as `hits == []` alone (or specify gold source + overlap function where gold exists), and add an absolute floor for D (e.g., D's honest-empty rate on category-5 ≥ X), since any `D ≥ C` comparison against a structurally constant C can never discriminate.
- **[MAJOR] F2 — Parity invariant is unconditional while env defaults change no-param behavior; pool/filter knob reader unnamed (contracts/memory-search.md:5,:40 vs quickstart.md:55, spec.md:62; plan.md:76,:81).** With `ENGRAM_SEARCH_POOL=150` set, a no-new-param call emits `pool_size`/wide-pool results (behavior matrix row memory-search.md:43-44), directly contradicting the HARD "byte-identical" invariant and its "破坏此不变式 = 阻断合入" clause; objective 3 requires the invariant be stated "when env unset". Fix: qualify the invariant and matrix default row with "when ENGRAM_SEARCH_POOL/ENGRAM_FILTER unset", name `mcpserver/config.go` as the reader (currently omitted from its knob list), and document that clients cannot set server env. Closes MINOR-16.
- **[MINOR] F3 — plan.md:94 Complexity row still scopes the death-rule amendment to "仅限本 Jev 过滤面", contradicting FR-007's read+write scope (spec.md:107).** Sync the one row so the AGENTS.md text isn't drafted from the stale wording (BLOCKER-2 residual).
- **[MINOR] F4 — MAJOR-5 residual: promotionVerdictFor linkage and degraded-contrast significance not stated (research.md:49; spec.md:129).** The plan mandates the declaration but doesn't make it; and SC-005 (degraded ≥ A) has no pairing/McNemar requirement though A↔C is the operative contrast. Fix: one sentence now ("SC-001..007 outcome feeds promotionVerdictFor via HOLD; SC-005 judged by paired A↔C stats").
- **[MINOR] F5 — MAJOR-4 residual: 038 validity artifacts unnamed (research.md:49).** Per-row provider-call audit, per-repeat validation receipts, pilot/warm-up/3-rep-in-one-window sequencing, and the literal new mechanism-key name appear only via blanket "按 038 正式协议机器执行". Fix: enumerate them (and the key name) in plan/tasks so they are checkable.
- **[NIT] F6 — data-model.md's MCP output increment omits `latency_ms`/`cost_usd`**, stale vs contracts/memory-search.md:30-31 and FR-005 (contracts declared authoritative, so summary only).
- **[NIT] F7 — `jev-latest` still literal** in the contract wire example (contracts/relevance-filter.md:21) and quickstart.md:27; eval pinning itself is fixed — use a `<pinned-revision>` placeholder to prevent copy-paste.
- **[NIT] F8 — No quickstart validation step for the `cmd/engram-filter` B-path deliverable** (plan.md:80; none of §1–§6 exercises it). Add a one-liner asserting its shortlist equals the MCP path's (shared-package parity).
- **[NIT] F9 — quickstart.md:43 arm table still shows C gated at 8** while revision line :49 gates C@12; D-noRelax row absent from the table.
- **[NIT] F10 — data-model.md:18 justifies Candidate fields via `memory.Entry` while mapping from `memory.Result`** (which lacks Pinned today, retriever.go:110-122); the "add Pinned to Result" instruction lives only in the plan fix index (plan.md:116) — move it into the structure/data-model section.
- **[NIT] F11 — Offline "no network attempt" assertion (filter=jev, no keys) not pre-declared** (quickstart §2 covers httptest failures only) — NIT-19 residual.

### Correct

- Layered error semantics match objective 4 exactly (contracts/relevance-filter.md:54: filter errors never propagate; only Search errors do).
- Write-gate item 0 is an exemplary BLOCKER-2 fix: local pre-detection short-circuit, masking, 500ms fail-open, manual-write-only scope.
- SC-003 now equal-budget with the existing `evidenceRecallAt` seam named; latency discipline (1s deadline, 30s negative cache, no retries) answers MAJOR-12 fully.
- B-path reconciliation in Complexity Tracking is honest and satisfies FR-001's shared-semantics requirement.
- Working tree: no staged or unstaged tracked changes; all 051 artifacts remain untracked (plan-stage normal; design doc commit still pending as planned in plan.md:83).

**VERDICT: block**

Blocking items: F1 only — objective 2's empty-injection criterion remains non-failable as specified (the core outcome prior BLOCKER-3 demanded). F2 must land before tasks (explicit objective-3 wording). All findings are plan-stage text fixes; nothing reviewed is an implementation defect.