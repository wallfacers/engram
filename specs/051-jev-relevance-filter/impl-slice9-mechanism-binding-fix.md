# T19 P3 blocker-2 hotfix — formal mechanism binding accepts the baked Jev registration

Branch `codex/051-jev-relevance-filter`, base commit `3ad5b3c` (working copy clean apart from
pre-existing untracked scratch files, untouched). Work surface: `cmd/locomo-bench/` only.
No `main.go` change, no `--opt` plumbing, no engine change
(`git diff --name-only -- memory embedding provider store internal filter` → empty).

## Verified bug (reproduced)

The freeze path writes the 051 registration into the manifest before the digest
(`freezeFormalProtocol` → `attachJevArmsRegistrationForFreeze` → `attachJevFilterRegistration`,
`jevfilter_protocol.go:230-241`): `Experiment.MechanismFlags["filter.jev.v1"] = true` and
`Experiment.Filter = &registration`. The run-side binding check
`validateFormalRunnerOptions` → `validateFormalMechanismBinding` (`eval_runner.go:466`) then
demanded `isFormalControlMechanismFlags(exp.MechanismFlags)`, whose allowlist switch had no
`filter.jev.v1` case → `default: return false` → refusal
`formal b1/legacy_count_packer manifest contains non-control mechanism flags`.
Every correctly registered `--jev-arms` manifest was therefore unrunnable, even though its digest
legitimately covers the registration. Red-state proof (new test, pre-fix source):

```
--- FAIL: TestValidateFormalMechanismBindingJevRegistrationCoherence (0.00s)
    eval_runner_test.go:769: frozen registered b1 control rejected: formal b1/legacy_count_packer manifest contains non-control mechanism flags
FAIL
FAIL	github.com/wallfacers/engram/cmd/locomo-bench	0.006s
```

## Struct/const facts used (no duplicated literals)

- `cmd/locomo-bench/eval_protocol.go:137` — `Filter *jevFilterRegistration` `` `json:"filter,omitempty"` `` on `evalExperimentProtocol` (pointer, so unregistered protocols keep byte-identical canonical bytes/hashes).
- `cmd/locomo-bench/jevfilter_protocol.go:83-104` — registration fields: `MechanismKey` (`mechanism_key`), `FilterModel` (`filter_model`), `FilterBaseURLHost`, `Theta`, `RelaxTheta`, `RelaxMax`, `KShowMax`, `Pool`, `AnswerInputCap`, `AnswerRepetitions`, `EmptyInjectionFloor`, `Arms`, `PilotGateConfirmed`, `WarmupDisposed`, `SameWindowReps`.
- `cmd/locomo-bench/jevfilter_protocol.go:37` — `const jevFilterMechanismKey = "filter.jev.v1"` (used directly in both edits; no string literal added).
- The coherence check reads exactly `exp.Filter != nil` and `exp.Filter.MechanismKey`; it deliberately does **not** call `validateJevProtocolBinding`, which would also impose budget/aggregation equality and widen this slice beyond the binding seam.

## Edits

1. `cmd/locomo-bench/eval_runner.go:564` — `isFormalControlMechanismFlags` allowlist switch now
   accepts `jevFilterMechanismKey` (registration marker: may be present+true or absent; not a
   treatment). Doc comment updated at `eval_runner.go:546-552` (marker baked by freeze so the
   digest covers it; per-arm behavior selected at runtime; presence is checked against the
   registration itself in `validateFormalMechanismBinding`).
2. `cmd/locomo-bench/eval_runner.go:481-492` — fail-closed manifest-coherence check for exactly
   this key inside the b1 branch of `validateFormalMechanismBinding`:
   `exp.MechanismFlags[jevFilterMechanismKey] != (registrationKey != "")` → refuse
   `"... %s registration is incoherent: mechanism flag=%v with registration key %q"`; and
   `registrationKey != "" && registrationKey != jevFilterMechanismKey` → refuse
   `"... %s registration is incoherent: registered mechanism key %q"`.
   Symmetric by construction: flag⇒registration and registration⇒flag. Manifests with neither the
   marker nor a registration (frozen 022/024/025/027 assets) are untouched.
   Doc comment on `validateFormalMechanismBinding` extended at `eval_runner.go:465-467`.

Validation-only change: no struct, JSON tag, or canonicalization code touched, so no manifest
bytes and no protocol digest change.

## TDD test

`cmd/locomo-bench/eval_runner_test.go:743-826` —
`TestValidateFormalMechanismBindingJevRegistrationCoherence`. The primary case builds the manifest
through the real freeze half (`attachJevFilterRegistration(protocol, jevTestRegistration())`), then
calls `validateFormalMechanismBinding`. Covered cases (task t1-t6):

| case | fixture | expected |
|---|---|---|
| t1 | freeze-written b1 control: 3 legacy keys false + marker true + matching registration | passes (was refused) |
| t2 | t1 + `evil=true` → refused; t1 + unmatched `write_dedup=true` → refused | refused |
| t3 | marker true + `Filter=nil` → refused; marker true + `Filter.MechanismKey="filter.other.v1"` → refused | refused |
| t4 | registration present + marker absent → refused; registration present + marker `false` → refused | refused |
| t5 | pure legacy control (3 keys, no extra keys, no `Filter`) | passes (022 backward compat) |
| t6 | marker + registration + `idk_retry=true` | refused |

## Gates (all green, post-fix)

```
$ CGO_ENABLED=0 go build ./...
BUILD_EXIT=0

$ CGO_ENABLED=0 go vet ./cmd/locomo-bench/
VET_EXIT=0

$ CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/ ./filter/...
ok  	github.com/wallfacers/engram/cmd/locomo-bench	46.757s
ok  	github.com/wallfacers/engram/filter	0.005s
ok  	github.com/wallfacers/engram/filter/jev	4.362s
TEST_EXIT=0
```

Existing mechanism-binding tests (025 representation/episode-cluster, 027 compiler-additive,
024 density, treatment binding) all still pass:

```
--- PASS: TestFormalRunnerOptionsRequireLegacyControlAndRejectTreatments (0.00s)
--- PASS: TestValidateFormalMechanismBindingDensityArms (0.00s)
--- PASS: TestValidateFormalMechanismBindingJevRegistrationCoherence (0.00s)
--- PASS: TestValidateFormalMechanismBindingTreatment (0.00s)
--- PASS: TestValidateFormalMechanismBindingCompilerAdditive (0.00s)
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.020s
```

## Deviations / residual risks

- Deviation from the literal wording "if the flag is absent or false, behavior unchanged": the
  coherence check is symmetric, so a lone registration (flag absent/false + `Filter != nil`) is now
  refused — this is exactly task t4. No real manifest can be in that state (the freeze writes both
  fields atomically and `validateJevProtocolBinding` already rejects the lone registration), so the
  only affected inputs are incoherent ones; the intended "unchanged" set (no registration at all)
  is unchanged and asserted by t5.
- Residual risk: the check is unit-level (`validateFormalMechanismBinding`); no end-to-end
  `--jev-arms` run was executed (needs the rented GPU box + keys, out of scope for this slice).
  The remaining P3 run path also requires `verifyJevFilterRegistrationBinding` /
  `validateJevProtocolBinding`, both already green.
- Diff: `cmd/locomo-bench/eval_runner.go` +28/-2, `cmd/locomo-bench/eval_runner_test.go` +85.
  No staged files; pre-existing untracked junk in docs/ left untouched.