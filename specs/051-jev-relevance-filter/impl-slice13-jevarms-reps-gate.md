# T19 slice 13 — cmd/locomo-bench: `--jev-arms-reps` parameterization (impl report)

Branch `codex/051-jev-relevance-filter`, base HEAD `e8a61e3`; `git status --porcelain -- cmd/` verified empty before starting.
Work surface: **cmd/locomo-bench/ only** — `git diff --name-only -- memory embedding provider store internal filter` is empty (engine untouched; no `filter/` source changed).

## Flag semantics

`--jev-arms-reps N` (int, **default 3**):

- **3 = canonical**: the §23 frozen majority-of-3 protocol. Existing behavior is byte-identical (manifest bytes, digests, refusals keep their shape).
- **1 = pilot**: gated single answer per question, majority-of-1 (the maintainer-ordered 1-rep × 1540-question pilot). Honest name, honest semantics — not a hack around the freeze.
- **any other value (0, 2, 4, 5, negative)** → refused:
  `--jev-arms-reps must be 1 (pilot: one answer per question, majority-of-1) or 3 (canonical 3-repetition majority protocol): N has no majority semantics`
- `opt.repeats` must **equal** the declared `--jev-arms-reps` (both directions refused), keeping the old message shape, parameterized:
  `--jev-arms is frozen at the --jev-arms-reps %d-repetition majority protocol, got --repeats %d`
- Checks run **before** the `--estimate` exemption, so an estimate of a misdeclared pilot is refused too; the freeze exemption (`--eval-freeze-protocol` → registration-env check) and the estimate exemption keep their exact positions.

## Hardcoded-3 audit — every spot, and how it was handled

| # | Location | Disposition |
|---|----------|-------------|
| 1 | `jevfilter_arms.go:52` `jevArmAnswerRepetitions = 3` | **Parameterized around**: kept as canonical default; added `jevArmPilotRepetitions = 1` + `validJevArmsReps()` ({1,3} membership). |
| 2 | `jevfilter_protocol.go:940-941` `validateJevArmsOptions` `opt.repeats != 3` refusal | **Parameterized**: flag-membership check + `opt.repeats != opt.jevArmsReps`. |
| 3 | `jevfilter_protocol.go:1058-1059` same refusal in `runJevArms` (run-time backstop) | **Parameterized** identically (defense in depth, mirrors the design). |
| 4 | `jevfilter_protocol.go:119` `newJevFilterRegistration` writes `AnswerRepetitions: 3` | **Parameterized**: takes `reps`; `jevRegistrationForRun` passes `opt.jevArmsReps`. Freeze (`attachJevArmsRegistrationForFreeze`) and run both flow through it, so the sealed registration records 1 for the pilot. |
| 5 | `jevfilter_protocol.go:153-154` `validateJevFilterRegistration` refuses `!= 3` | **Parameterized**: refuses `!validJevArmsReps(...)` — {1,3} accepted, everything else still refused (message names both legal values). |
| 6 | `jevfilter_protocol.go:671` `planJevArmCost` echoes `AnswerRepetitions: 3` | **Parameterized**: echoes the `repetitions` arg (call-site passes `opt.repeats`, which pre-flight pins to the declared reps). Estimate printout now honestly shows pilot reps. |
| 7 | `eval_runner.go:1392` `freezeFormalProtocol` hardcodes `Aggregation.AnswerRepetitions: 3` | **Parameterized via `jevFreezeAnswerRepetitions(opt)`**: `opt.jevArms` → declared reps; every other freeze keeps literal 3 → **non-jev freeze manifests byte-identical** (task point 4: the reps=1 freeze now produces `answer_repetitions: 1`). |
| 8 | `eval_runner.go:1494` `freezeB0ContinuityProtocol` hardcodes 3 | **Left as-is (refused-by-design)**: B0 continuity is the separate frozen legacy-product continuity receipt protocol, not the arms protocol; its 3-rep pin is a validity gate for *that* manifest. A jev pilot runs its own stores/arms; forcing reps-agnosticism here would weaken the B0 receipt freeze. Reported, not silently changed. |
| 9 | `eval_fixed_gold_oracle.go:87` `AnswerRepetitions != 3` refusal | **Left as-is**: pins the frozen three-repetition B1 legacy control for the fixed-gold oracle diagnostic — a different, already-frozen artifact. Not a jev-arms path. |
| 10 | `eval_protocol.go:211` manifest validation (`< 1 || even`) | **Already generic**: 1 is odd ≥ 1, passes; rule stays `majority_correctness`, judge reps stay 1. |
| 11 | `majorityCorrectness` (`paired_eval.go:16`) / `jevArmMajorityOutcomes` / `jevArmContrastFor` (McNemar pairing) / per-question collapse | **Already generic**: odd-count majority; majority-of-1 = the single answer (unit test t6). Even measured counts still fail loudly. |
| 12 | `eval_artifact.go` 236/288/394/722, `eval_b0_continuity.go` 44/207, `eval_runner.go` 205/263, `eval_fixed_gold_oracle.go` 515/574/1246/1552-1586 | **Already generic**: all read `protocol.Aggregation.AnswerRepetitions` from the manifest, never a literal 3. |
| 13 | `jevfilter_verdict.go` — SC gates | **Already generic**: SC-002's `1.0/3.0` is the 33.3% **token** gate (unrelated); `report.Replications` just records the run's actual reps; no gate assumes 3. |
| 14 | run loop `jevfilter_protocol.go:1187-1243` (`repetitions := opt.repeats`) | **Already generic** loop bound; equality with the declaration is pinned by (2)/(3), and the manifest ↔ registration ↔ opt chain (`verifyJevFilterRegistrationBinding` + `validateJevProtocolBinding:262-263`) keeps all three consistent. |
| 15 | `--jev-same-window-reps` (`validateJevDeclaredPrerequisites:859`) | **Already reps-agnostic**: unconditional boolean declaration; no 3-assumption in its validation. Kept **required at reps=1** (trivially satisfiable, but the receipt stays — no silent gate drop). Flag help reworded "all three repetitions" → "all answer repetitions (per --jev-arms-reps)". |
| 16 | three "three-rep…" prose comments in metrics/contrast docs | Reworded to "declared-reps" for honesty (comment-only). |

## TDD — red first

New file `cmd/locomo-bench/jevfilter_arms_reps_test.go` (7 tests, t1–t7 as tasked). First run failed to compile against the unmodified tree (unknown field `jevArmsReps`, undefined `jevFreezeAnswerRepetitions`) — red confirmed; then implemented, then one intermediate red (`t5b` needed `jevArms: true` in the fixture opt — a fixture bug, fixed in the test).

- t1 `TestJevArmsRepsOnePilotPassesPreFlight` — reps=1 + repeats=1 passes.
- t2 `TestJevArmsRepsCanonicalDefaultUnchanged` — 3/3 passes; repeats ∈ {1,2,5} against 3 refused naming `--repeats`.
- t3 `TestJevArmsRepsRefusesCountsWithoutMajoritySemantics` — reps ∈ {0,2,4,5,-1} refused with the exact {1,3} message.
- t4 `TestJevArmsRepsMustMatchRepeats` — 1+3 and 3+1 both refused; pilot message names both `--jev-arms-reps 1` and `--repeats 3`.
- t5 `TestJevRegistrationRecordsDeclaredReps` + `TestJevFreezeManifestCarriesPilotReps` — pilot registration/`attachJevArmsRegistrationForFreeze` seals `answer_repetitions: 1`; binding accepts a consistent 1/1 manifest and refuses aggregation-3 vs registration-1 drift; `jevFreezeAnswerRepetitions(options{})==3` keeps non-jev freezes canonical.
- t6 `TestJevArmMajorityOutcomesSingleRepetition` — majority-of-1 = the single answer (true and false cases), unmeasured rows excluded, canonical 2-of-3 still collapses.
- t7 `TestJevSameWindowRepsStillRequiredForPilot` — refused without the declaration at reps=1, accepted with it.

Existing tests updated (fixtures only, no assertion weakened): `jevfilter_protocol_test.go` — registration helper passes the canonical reps; the tamper case `"repetitions"` now corrupts to **2** (1 became legal by design — the refusal for a no-majority count is still asserted); two `validateJevArmsOptions` fixtures declare `jevArmsReps: jevArmAnswerRepetitions`.

**Reachable-seam note (t5 caveat):** the full `freezeFormalProtocol` file write isn't unit-reachable in a dirty tree (`currentCleanGitProvenance` requires a clean git checkout); the reps decision it makes is extracted into `jevFreezeAnswerRepetitions` and tested directly, plus the manifest-level attach/binding chain is tested end-to-end.

## Gates (pasted)

```
$ bash -c 'cd …/engram && CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./cmd/locomo-bench/ && CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/ ./filter/...'
ok  	github.com/wallfacers/engram/cmd/locomo-bench	47.183s
ok  	github.com/wallfacers/engram/filter	0.008s
ok  	github.com/wallfacers/engram/filter/jev	4.398s
```

gofmt: all files touched by this change are clean. `gofmt -l cmd/locomo-bench/` lists 13 **pre-existing** offenders (each verified unformatted at HEAD, e.g. `attribution.go`, `runner.go`, `eval_fixed_gold_oracle.go`) — untouched, out of slice.

Engine/adapter boundary: `git diff --name-only -- memory embedding provider store internal filter` → empty. `git diff --cached` → empty (nothing staged). Pre-existing untracked files in `docs/` are not mine and were left alone.

## Deviations / residual risks

1. **Deviation (from DESIGN §2 wording):** the registration-level validator (`validateJevFilterRegistration`) now accepts {1,3} rather than an arbitrary declared value — it cannot see per-call options and must keep the sealed-manifest check meaningful; anything outside the two pre-registered protocols is still refused. Consistent with "parameterize honestly, refuse the rest".
2. **Left hardcoded by design (reported, not changed):** `freezeB0ContinuityProtocol` and the fixed-gold B1 control keep their 3-rep pins — parameterizing them would weaken *other* frozen validity gates, which the task forbids. A pilot run's B0 continuity receipts therefore still come from the canonical 3-rep B0 protocol; this is the honest, unchanged pairing.
3. Pilot runs remain gated: every existing declaration (`--jev-pilot-gate-confirmed`, `--jev-warmup-disposed`, `--jev-same-window-reps`), the SC verdicts, and the B0-continuity requirement are untouched at reps=1.
4. `--repeats` remains a free int outside `--jev-arms` (unchanged global behavior); the new flag only constrains jev-arms invocations, as scoped.
5. Constitution IV: this is eval-harness-only (`cmd/locomo-bench/`), no retrieval/extraction/curation/storage engine path changed → parity/gate suite green is the applicable regression evidence; the actual pilot run itself is the operator step that consumes this flag.
