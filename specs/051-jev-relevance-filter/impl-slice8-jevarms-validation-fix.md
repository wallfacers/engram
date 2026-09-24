# T19 P3 blocker hotfix — `--jev-arms` pre-flight refused every run-mode pass (051)

Branch: `codex/051-jev-relevance-filter`. Work surface: `cmd/locomo-bench/` only.
Nothing staged; engine untouched (`git diff --name-only -- memory embedding provider store internal` empty).

## The change

`cmd/locomo-bench/jevfilter_protocol.go:963-964` (one condition + its message):

```go
-	if opt.formalProtocol == nil {
-		return fmt.Errorf("--jev-arms requires a frozen protocol (--eval-freeze-protocol) whose manifest carries the filter registration and the answer-input cap")
+	if strings.TrimSpace(opt.evalProtocolPath) == "" {
+		return fmt.Errorf("--jev-arms requires a frozen protocol via --eval-protocol (whose manifest carries the filter registration and the answer-input cap)")
```

Nothing else changed: `main.go` untouched (no reorder), the run-time backstop in
`runJevArmProtocol` (`jevfilter_protocol.go:1114-1116`, `protocol == nil` with the
"see --eval-freeze-protocol" wording) untouched, the freeze early-return
(`opt.evalFreezeProtocol != ""` → `validateJevArmsRegistrationEnvironment`) untouched.

### Field-name confirmation (as required)

- `options.evalProtocolPath` — declared `cmd/locomo-bench/main.go:83`; populated by flag
  parsing at `main.go:294`: `flag.StringVar(&opt.evalProtocolPath, "eval-protocol", "", "frozen 022.v1 protocol manifest; …")`.
  This is the PATH string the `--eval-protocol` flag sets, available before validation.
- The pointer `options.formalProtocol` (`main.go:92`) is assigned only at `main.go:544`
  (`opt.formalProtocol = &protocol`), inside the `if opt.evalProtocolPath != ""` block that
  runs `prepareFormalEvalRun` at `main.go:525`. `validateJevArmsOptions(opt, arms)` is called
  at `main.go:489` — i.e. before that assignment, hence the reported always-refuse.
- `--eval-freeze-protocol` is a different field (`evalFreezeProtocol`) and returns earlier;
  `eval_runner.go:1294` still forbids using both at once, so the run pass legitimately cannot
  carry the freeze flag — the flag check is now the only satisfiable gate.
- Fail-closed preserved: when `evalProtocolPath != ""`, `prepareFormalEvalRun` either errors
  out or binds `formalProtocol` (`main.go:525-544`), and the dispatch
  (`jevfilter_protocol.go:1085`) still passes `opt.formalProtocol` to `runJevArmProtocol`,
  whose nil backstop (line 1114) stays.

## TDD — red before, green after

New test: `cmd/locomo-bench/jevfilter_protocol_test.go:1054`
`TestValidateJevArmsOptionsRequiresTheFrozenProtocolFlag` — three assertions:

1. run-mode options with `evalProtocolPath = "protocol.json"` and `formalProtocol == nil`
   (the fixture asserts the nil pointer, so it cannot drift from the real call-site state)
   → **no error**;
2. same options with `evalProtocolPath = ""` → error whose text contains
   `--jev-arms requires a frozen protocol via --eval-protocol`;
3. empty path + a bound `formalProtocol` → still refused (pins that the pointer check was
   replaced, not merely supplemented).

No pre-existing test asserted the old condition/message; repo-wide grep for
`requires a frozen protocol` outside the two source sites returns only the new test, and the
old message string existed in exactly the one edited line. So no assertion needed updating —
zero other test edits.

RED (before the fix):

```
$ CGO_ENABLED=0 go test -count=1 -timeout 120s -run 'TestValidateJevArmsOptionsRequiresTheFrozenProtocolFlag' ./cmd/locomo-bench/
--- FAIL: TestValidateJevArmsOptionsRequiresTheFrozenProtocolFlag (0.00s)
    jevfilter_protocol_test.go:1081: a gated run-mode invocation carrying --eval-protocol was refused before the protocol is bound: --jev-arms requires a frozen protocol (--eval-freeze-protocol) whose manifest carries the filter registration and the answer-input cap
FAIL
FAIL	github.com/wallfacers/engram/cmd/locomo-bench	0.011s
```

GREEN (after the fix):

```
$ CGO_ENABLED=0 go test -count=1 -timeout 120s -run 'TestValidateJevArms' ./cmd/locomo-bench/
ok  	github.com/wallfacers/engram/cmd/locomo-bench	0.011s
```

## Gate output (configured gate, pasted)

```
$ CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./cmd/locomo-bench/ && CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/ ./filter/...
ok  	github.com/wallfacers/engram/cmd/locomo-bench	76.161s
ok  	github.com/wallfacers/engram/filter	0.025s
ok  	github.com/wallfacers/engram/filter/jev	4.585s
GATE EXIT=0
```

(`go build ./...` and `go vet ./cmd/locomo-bench/` printed nothing and exited 0.)

## Deviations

None. Exact condition, exact message, no reorder, no touch to the run-time backstop or the
freeze path. No file outside `cmd/locomo-bench/` was modified by this slice
(`specs/051-jev-relevance-filter/tasks.md` was already modified before this slice started —
untouched here).

## Residual risks / notes

- `TestValidateJevArmsOptions…` asserts at the `validateJevArmsOptions` seam, because its
  caller (`run()` at `main.go:489`) is a flag-parsing entry point with no unit seam. The
  call-site ordering (pre-flight at 489 before the protocol bind at 544) is verified by
  reading the code, not by an executable test; the test comment records the invariant.
- Pre-existing, unrelated nit (unchanged by this fix, fail-closed): the pre-flight's
  `!opt.noIDKRetry` refusal fires before `prepareFrozenEvalOptions` would force
  `noIDKRetry = true`, so a run-mode pass still must pass `--no-idk-retry` explicitly.
- Value-path confidence: this fix unblocks the *pre-flight* for gated/degraded run passes
  (`--jev-arms --eval-protocol … --no-idk-retry --chunks --store-dir --run-dir
  --token-counter-base-url …`). Whether a real pass then clears the remaining runtime checks
  is a real-run question, not covered by this offline slice (no box/endpoints touched).
- No secrets involved. No staged files.