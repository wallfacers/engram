# T19 P3 blocker-3 hotfix — Jev counter probe must name the frozen answerer model

Branch `codex/051-jev-relevance-filter`, HEAD `c9bbba2` (clean: `git status --porcelain -- cmd/` empty before start).
Work surface: `cmd/locomo-bench/` ONLY. Engine untouched (`git diff --name-only -- memory embedding provider store internal` empty).

## Bug (mechanically confirmed pre-fix, not just read)

`validateJevCounterFingerprint` probed the tokenizer with a FAKE model id:

```
cmd/locomo-bench/jevfilter_protocol.go:783 (pre-fix)
    count, err := counter.CountInput(ctx, evidencecompiler.AnswerInput{Model: "fingerprint-probe", System: "s", User: "u"})
```

`vllmTokenCounter.CountInput` marshals `input.Model` straight into `vllmTokenizeRequest.Model`
(`token_counter.go:98-104`) and posts to `{base}/tokenize` (terminal `/v1` stripped,
`token_counter.go:157`); vLLM rejects an unknown model id with HTTP 404 → `probe jev token counter:
vLLM token request failed with status 404` → every `--jev-arms` pass died before any paid call.
The rest of the run path already names the frozen answerer (`Model: protocol.Models.Answerer.ID`
at `eval_runner.go:709, 747, 762, 765, 805, 904`); the probe was the sole deviation.

Behavioural proof of the pre-fix state (throwaway test, since deleted — repo restored):

```
=== RUN   TestZZTempCurrentProbeModel
    zz_temp_probe_bug_test.go:23: PRE-FIX probe sends model="fingerprint-probe" system="s" user="u"
--- PASS: TestZZTempCurrentProbeModel (0.00s)
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.006s
```

## Change (exact locations)

- `cmd/locomo-bench/jevfilter_protocol.go:779` — signature now
  `validateJevCounterFingerprint(ctx context.Context, counter evidencecompiler.TokenCounter, want, answererModel string) error`
- `cmd/locomo-bench/jevfilter_protocol.go:783-790` — probe body: fail-closed blank-model guard
  (`jev token counter probe requires the frozen answerer model id`, no placeholder fallback), then
  `CountInput(ctx, evidencecompiler.AnswerInput{Model: answererModel, System: "s", User: "u"})`
- `cmd/locomo-bench/jevfilter_protocol.go:1157` (call site) —
  `validateJevCounterFingerprint(ctx, counter, protocol.Budget.CounterFingerprint, protocol.Models.Answerer.ID)`
- Untouched: `count.Fingerprint != want` comparison + its message, nil-counter refusal
  (`jev arms require a token counter`), blank-`want` early return.

Call-site list (whole repo): `grep -rn validateJevCounterFingerprint --include=*.go .` → definition
`jevfilter_protocol.go:779` + production caller `jevfilter_protocol.go:1157` + 5 test call sites
(`jevfilter_protocol_test.go:1161,1181,1198,1211,1216`). No other callers, no wrappers.

Ordering decision (see Deviations): nil-counter → blank-`want` → blank-model. A blank frozen
fingerprint means there is nothing to prove, so no probe (and therefore no model requirement)
applies — this keeps the pre-existing blank-`want` behaviour literally unchanged. The both-blank
degenerate combination is unreachable from a validated protocol: `validateEvalProtocol` requires
`isDigest(budget.CounterFingerprint)` and a non-blank answerer id (`eval_protocol.go:245-251, 257`).

## TDD: red → green

RED (tests added first, against the old 3-arg signature):

```
# github.com/wallfacers/engram/cmd/locomo-bench [github.com/wallfacers/engram/cmd/locomo-bench.test]
cmd/locomo-bench/jevfilter_protocol_test.go:1161:79: too many arguments in call to validateJevCounterFingerprint
	have (context.Context, *jevProbeRecordingCounter, string, string)
	want (context.Context, evidencecompiler.TokenCounter, string)
... (5 sites, build failed)
FAIL	github.com/wallfacers/engram/cmd/locomo-bench [build failed]
```

GREEN (targeted, after the fix):

```
=== RUN   TestValidateJevCounterFingerprintProbesWithTheFrozenAnswererModel
--- PASS: TestValidateJevCounterFingerprintProbesWithTheFrozenAnswererModel (0.00s)
=== RUN   TestValidateJevCounterFingerprintRefusesABlankAnswererModel
--- PASS: TestValidateJevCounterFingerprintRefusesABlankAnswererModel (0.00s)
=== RUN   TestValidateJevCounterFingerprintStillRefusesDrift
--- PASS: TestValidateJevCounterFingerprintStillRefusesDrift (0.00s)
=== RUN   TestValidateJevCounterFingerprintKeepsNilCounterAndBlankWant
--- PASS: TestValidateJevCounterFingerprintKeepsNilCounterAndBlankWant (0.00s)
PASS
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.005s
```

Tests added (all in `cmd/locomo-bench/jevfilter_protocol_test.go`, nearest jevfilter test file,
same package, reusing the file's `context`/`strings` imports + one new
`github.com/wallfacers/engram/memory/evidencecompiler` import; `jevProbeRecordingCounter` records
`lastInput` and `calls`):

- (a) `TestValidateJevCounterFingerprintProbesWithTheFrozenAnswererModel:1160` — stub records the
  probe's `AnswerInput`; asserts `lastInput.Model == "Qwen/Qwen3.6-35B-A3B-FP8"` (the frozen
  answerer id) and explicitly rejects the `"fingerprint-probe"` literal; exactly one counter call.
- (b) `TestValidateJevCounterFingerprintRefusesABlankAnswererModel:1174` — `""`, `"   "`, `"\t\n"`
  all refuse with an error naming the missing answerer model, and the counter is never called
  (fail closed, no placeholder fallback, no panic).
- (c) `TestValidateJevCounterFingerprintStillRefusesDrift:1196` — fingerprint mismatch still refuses
  with the existing `... differs from the frozen "sha256:answerer-template-r1"` message.
- (d) `TestValidateJevCounterFingerprintKeepsNilCounterAndBlankWant:1209` — nil counter → existing
  `jev arms require a token counter`; blank/whitespace `want` → nil error, zero probe calls.

No pre-existing test asserted the old `"fingerprint-probe"` literal (`grep -rn
fingerprint-probe --include=*.go .` pre-fix had exactly one hit: the production line `:783`), so no
other assertions needed updating. The only remaining `"fingerprint-probe"` string is the negative
assertion inside test (a).

## Gates (parent's exact command)

```
$ bash -c 'CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./cmd/locomo-bench/ && CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/ ./filter/...'
ok  	github.com/wallfacers/engram/cmd/locomo-bench	45.711s
ok  	github.com/wallfacers/engram/filter	0.007s
ok  	github.com/wallfacers/engram/filter/jev	4.389s
GATE_EXIT=0
```

`gofmt -l` on both changed files: clean. `git diff --cached --name-only`: empty. Diff stat:
`jevfilter_protocol.go +15/-4`, `jevfilter_protocol_test.go +89`.

## Deviations

None from the requested mechanics. Two notes for the reviewer:

1. Check ordering (documented above): blank-`want` early return is kept ahead of the blank-model
   guard, so the pre-existing blank-`want` behaviour is preserved verbatim
   (`validateJevCounterFingerprint(ctx, counter, "", "")` still returns nil — same as pre-fix — and
   no probe occurs, so no placeholder can ever be sent).
2. Parameter-passing was chosen over "fully-built probe input" (smallest diff: one param + one
   call-site arg; the probe's `"s"`/`"u"` payload stays private to the function). The model-vs-want
   argument order is `(..., want, answererModel)`.

## Residual risk

No test drives `runJevArmProtocol`'s pre-flight (no existing harness; `grep -rn runJevArmProtocol
cmd/locomo-bench/*_test.go` → comment only), so the call-site wiring `protocol.Models.Answerer.ID`
is read/diff-verified rather than test-pinned. An httptest `/tokenize` server driving
`runJevArms` would close that gap but was out of this slice's declared test scope (a)-(d).
No live box re-run was performed here (no endpoint available in-session); field confirmation is the
parent's next `--jev-arms` pass.