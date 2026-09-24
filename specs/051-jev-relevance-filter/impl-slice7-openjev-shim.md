# 051 P1.5 — openjev shim (slice 7): implementation report

Branch `codex/051-jev-relevance-filter`. Fresh worker finishing a crashed worker's
`cmd/openjev-shim/` slice; all four priorities (P0 hang → P1 extraction → P2 deadline
wiring → P3 test completeness) are done and every configured gate is green.

| item | state |
|---|---|
| P0 — `go test ./cmd/openjev-shim/...` never finishing | **fixed** (was an infinite loop in the reasoning-span scanner) |
| P1 — robust thinking-strip + JSON extract | **done** (`balancedJSONObject` + `dropReasoningSpans`; no offset math on tags) |
| P2 — deadline wiring for the eval filter + contract note | **done** (`cmd/locomo-bench` only; `filter/` untouched) |
| P3 — test completeness (a)–(g), healthz, flags | **done** (22 tests in the shim package, all green) |
| engine discipline (`memory/ embedding/ provider/ store/ internal/ filter/`) | **zero diff** |
| gates | build ✅ · vet ✅ · tests (openjev-shim, filter, memory, mcpserver) ✅ · `-race` ✅ (see §7 for the CGO conflict) |

---

## 1. What the shim is, and how to run it

`cmd/openjev-shim` serves the Jev pointer wire (contract §2) on the inside edge and
translates **one** pointer request into **one** OpenAI-compatible `chat.completions`
call on the outside edge: query + memories + `noul` questions are rendered into one
prompt that demands a single strict-JSON object of probabilities. It is a translation
layer, not Jev — the numbers are a local approximation, so eval claims stay labeled
`openjev` (data-model Backend enum).

```bash
CGO_ENABLED=0 go build -o /root/autodl-tmp/bin/openjev-shim ./cmd/openjev-shim
OPENJEV_UPSTREAM_BASE_URL=http://127.0.0.1:8000/v1 \   # default http://127.0.0.1:8000/v1
OPENJEV_UPSTREAM_MODEL=<the pinned vllm model id> \    # REQUIRED (no default)
OPENJEV_MAX_QUESTIONS=256 \                            # optional, default 256, larger -> 413
  /root/autodl-tmp/bin/openjev-shim --port 8020 &      # --port default 8020, loopback only
curl -s http://127.0.0.1:8020/healthz                  # -> ok
```

Eval-side wiring (P2/P3 executor notes):

```bash
export ENGRAM_JEV_BASE_URL=http://127.0.0.1:8020   # the client appends its Path, default /answers
export ENGRAM_JEV_MODEL=<shim label, pinned>       # the shim answers with OPENJEV_UPSTREAM_MODEL
export ENGRAM_JEV_API_KEY=<any non-empty string>   # the shim ignores auth; the harness only
                                                   # requires the key to be non-empty
# ENGRAM_JEV_PATH: leave empty -> client default /answers, which is what the shim serves
```

No credentials are needed by the shim, it reads none, and it binds `127.0.0.1` only.
`Authorization: Bearer …` from the client is accepted and ignored.

## 2. Final wire (real transcript, not a mock-up)

Captured end-to-end on this machine: real `openjev-shim` binary + a fake upstream whose
assistant content is a reasoning span (with a *draft* inside it), prose, then the answer
object with one out-of-range value. Command transcript:

```
== GET /healthz ==
ok [http 200]

== POST /answers (canonical pointer request) ==
{"probabilities":{"need_m0":0.9,"need_m1":1,"need_m2":0},"usage":{"prompt_tokens":312,"completion_tokens":14}}
[http 200]

== upstream reply the shim parsed (fake Qwen3.6 content) ==
<think>m0 looks relevant, I'd say {draft 0.9}, m1 maybe 1.4.</think>
Here is the answer:
{"need_m0": 0.9, "need_m1": 1.4}

Hope that helps!

== shim log ==
2026/09/24 13:30:16 listening on http://127.0.0.1:8124 (upstream http://127.0.0.1:8123/v1, model Qwen3.6-35B-A3B-FP8, max_questions=256)
2026/09/24 13:30:19 answers ok: questions=3 attempts=1 elapsed=1.736691ms
```

Request (frozen canonical shape, identical json tags to `filter/jev`, verified by
`TestPointerWireShapeRoundTrip` and by a real-client `TestEngineClientRoundTrip`):

```json
{"model":"qwen3.6-shim-pinned","state":{"query":"what is the deploy code?","memories":{"m0":{"name":"m0","text":"The deploy code is YAML-42."},"m1":{"name":"m1","text":"I like tea."},"m2":{"name":"m2","text":"The team ships on Fridays."}}},"questions":{"need_m0":{"type":"noul","instructions":"Memory m0 is necessary to answer or act on the query."},"need_m1":{"type":"noul","instructions":"Memory m1 is necessary to answer or act on the query."},"need_m2":{"type":"noul","instructions":"Memory m2 is necessary to answer or act on the query."}}}
```

Response: `{"probabilities":{…}, "usage":{…}}` — canonical shape only (`questions` /
`results` are never used by the shim), one entry per requested key: pass-through
(`need_m0` 0.9), clamped (`need_m1` 1.4 → 1), missing-from-reply → 0 (`need_m2`).
`usage` is the upstream's, so `FilterMeta.InputTokens/OutputTokens` are accounted rather
than unknown. Exactly one upstream attempt was made (the draft in the reasoning span was
not adopted).

## 3. P0 — the hang root cause and fix

Reproduced first, with a bounded timeout:

```
$ CGO_ENABLED=0 timeout -k 5 150 go test -count=1 -timeout 90s ./cmd/openjev-shim/... 2>&1 | tail -70
…
github.com/wallfacers/engram/cmd/openjev-shim.TestReasoningTraceIsNotAdoptedAsTheAnswer(0xc00060ce00)
	/home/wushengzhou/workspace/github/engram/cmd/openjev-shim/shim_test.go:622 +0x1a7
…
github.com/wallfacers/engram/cmd/openjev-shim.stripReasoning({0xc00047a300?, 0xafd7b0?})
	/home/wushengzhou/workspace/github/engram/cmd/openjev-shim/shim.go:394 +0x8f
FAIL	github.com/wallfacers/engram/cmd/openjev-shim	90.011s
```

**Root cause:** the old `stripReasoning` loop searched the *close* tag with
`strings.Index(text[open:], thinkClose)` and the `thinkClose` constant in the file was
the **empty string** (the crashed worker's tag literals were written as `""`, i.e. the
angle-bracket tag was lost before it reached disk — `sed -n '30,42p' … | od -c` showed
`t h i n k C l o s e   =   "   "`, and the test file's own tag literals were mangled the
same way). `strings.Index(s, "")` returns `0`, never `-1`, so `closing = 0`,
`text = text[:open] + text[open+0+0:]` → **text unchanged → infinite loop** on the first
open-tag hit (the strip was correct only in its intent, not in its data).

**Fix** (`cmd/openjev-shim/shim.go`):

* the tag pair now lives in `reasoningSpans` (shim.go:426) built from fragments
  (`"<" + "think" + ">"` …) so a tag-hostile write/render pipeline cannot silently
  corrupt it again, plus the `-ing` spelling as a second pair;
* `dropReasoningSpans` / `dropReasoningSpan` (shim.go:436/446) use `strings.Cut` only —
  **no offset arithmetic of its own** — and an empty tag returns the text untouched
  instead of looping (structurally non-terminating input is impossible);
* a truncated (unterminated) span swallows the remainder, which is the honest reading:
  a model that never closed its thought has no final answer after it.

Mutation-checked (this is how the old bug would surface now): with `reasoningSpans`
forced back to `{{"", ""}}`, `go test -run Reasoning -timeout 30s ./cmd/openjev-shim/`
**fails in 0.06 s** (draft adopted, no strip) — it does not hang. Bash output of that
mutation run confirmed both the fix's guard and the test's independence.

## 4. P1 — robust thinking-strip + JSON extract

`firstProbabilityObject` (shim.go:357):

1. drop reasoning spans (§3);
2. scan for a `{`; hand the offset to `balancedJSONObject` (shim.go:389), which walks
   braces byte by byte with a string/escape-aware counter (`"` toggles string state, `\`
   escapes inside a string) and returns the slice through the matching `}`;
3. `json.Unmarshal` that slice into `map[string]float64`; accept it only if it carries at
   least one *requested* key (so a reply that answers nothing is a retry, not a silent
   all-zeros);
4. otherwise advance one brace and try again (bounded by `maxJSONAttempts = 64`).

There is **no substring math on tags**: prose and code fences ahead of the object are
simply skipped by the brace scan, and the nested-wrapper shape
(`{"probabilities": {…}}`) resolves because the outer object fails the flat decode and
the scan advances into the inner one.

Tolerated: leading reasoning span(s), prose before/after the object, a stray balanced
brace pair in that prose (`The set {m0, m1} …`), code fences, and a nested wrapper.
Answer object with non-numeric extra fields (`{"need_m0":0.2,"why":"…"}`) is **not**
accepted by the strict flat decode → one stricter retry → 502; that strictness matches
the client, which also decodes `map[string]float64` and would reject such a payload.
Clamping ([0,1]) and missing-key → 0 stay where they were, in `handleAnswers`
(shim.go:183, `clampProbability` shim.go:471). Unparseable reply: one retry with the
`JSON ONLY` reminder (shim.go:211 `maxAttempts = 2`), then HTTP 502.

## 5. P2 — deadline wiring (no engine change)

Evidence:

| what | where |
|---|---|
| 30 s eval bound (new) | `cmd/locomo-bench/jevfilter_protocol.go:736` `const jevFilterTimeout = 30 * time.Second` |
| both knobs set from it | `cmd/locomo-bench/jevfilter_protocol.go:756-767` `jevFilterConfig`: `Deadline: timeout`, `PerRequestTimeout: timeout` |
| used by the arms' client | `cmd/locomo-bench/jevfilter_protocol.go:741` `buildJevFilterClient` → `jev.New(jevFilterConfig(opt, pol))` (its only production call site is `runJevArms`) |
| engine config knobs (unchanged) | `filter/jev/jev.go:59-118` (`Deadline` whole-call, `PerRequestTimeout` per request = `min(remaining, cap)`, both default 1 s) |
| contract note | `specs/051-jev-relevance-filter/contracts/relevance-filter.md:37` — "Backend wiring（openjev）": same wire, response shape unchanged, caller must widen **both** knobs to ≥30 s |

Both knobs are needed (`Filter` wraps the parent ctx with `Deadline`, each shard request
takes `min(remaining, PerRequestTimeout)`), which the pre-existing
`TestJevDeadlineIsCallerWidenable` in the shim package also pins behaviourally. An
explicitly set `opt.jevDeadline` still wins over the 30 s default (the field is retained
and used; it is 0 for every arm run, so the arms get 30 s / 30 s).

Two new tests (`cmd/locomo-bench/jevfilter_protocol_test.go`):

* `TestJevFilterConfigWidensTimeoutsForTheLocalShim` (:1258) — asserts the wiring values
  (30 s / 30 s), that address/model/key/path/policy survive, and that an explicit
  override is honoured for both knobs.
* `TestBuildJevFilterClientSurvivesALocalShimCall` (:1290) — end-to-end: the harness's
  own client against a fake `/answers` that answers after **1.5 s**; returns
  probabilities and `Degraded=false`.

Mutation check (proof the tests are not tautological): with
`jevFilterTimeout = time.Second` the config test fails (`Deadline = 1s, want 30s`) and
the 1.5 s call degrades (`context deadline exceeded`, `degraded: true`) — i.e. the old
wiring is exactly what the tests catch. File restored byte-identically (md5 verified).

`mcpserver` (`mcpserver/provider.go:41`) and `cmd/engram-filter` keep the 1 s production
defaults; nothing under `filter/` changed.

## 6. P3 — tests (all `httptest`, no network)

`cmd/openjev-shim/shim_test.go` — 22 tests, all green:

| requirement | test |
|---|---|
| (a) golden pass-through + clamp + missing→0 | `TestAnswersGoldenWithClampingAndMissingKeys` (:234) |
| (b) first garbage → retry → success | `TestUnparseableReplyRetriesOnceWithStrictReminder` (:288) |
| (c) double garbage → 502 | `TestDoubleGarbageReturnsBadGateway` (:336) |
| (d) > `OPENJEV_MAX_QUESTIONS` → 413, no upstream call | `TestTooManyQuestionsReturns413WithoutUpstreamCall` (:358) |
| (e) 5xx / timeout / unreachable → 502 | `TestUpstreamErrorReturnsBadGatewayWithoutRetry` (:372), `TestUpstreamTimeoutReturnsBadGateway` (:395), `TestUnreachableUpstreamReturnsBadGateway` (:422) |
| (f) concurrency | `TestConcurrentAnswersAreRaceClean` (:431) — 16 goroutines, `-race` clean |
| (g) wire round-trip with `filter/jev`'s tags | `TestPointerWireShapeRoundTrip` (:476) frozen bytes + `TestEngineClientRoundTrip` (:517) driving the **real** `filter/jev` client |
| engine-known caller deadline semantics | `TestJevDeadlineIsCallerWidenable` (:559) |
| thinking span + prose + exact sample from the task | `TestThinkingSpanProseAndJSON` (:649) |
| draft inside a span is not adopted | `TestReasoningTraceIsNotAdoptedAsTheAnswer` (:628) |
| both tag spellings | `TestReasoningSpellingsAreBothDropped` (:670) |
| extractor table (span/prose/stray braces/trailing prose/fence+nested) | `TestFirstProbabilityObjectScansBalancedBraces` (:686) |
| brace-aware walker (braces inside strings, escapes, truncated object) | `TestBalancedJSONObjectSkipsBracesInsideStrings` (:731) |
| nested wrapper / prose | `TestNestedAndProseWrappedJSONIsAccepted` (:742) |
| `/healthz` → `ok`, 405/404/400 routing, oversized body | `TestHealthzAndRouting` (:758), `TestOversizedBodyIsRejected` (:803) |
| flags/env (`--port` default 8020, base-URL default, model required, max-questions parsing) | `TestParseConfigFromEnv` (:816), `TestParseConfigPortFlag` (:863), `TestNewShimServerDefaults` (:881) |

The test file's reasoning-tag literals are an **independent** copy of the wrappers
(`upstreamThinkOpen` … built from fragments, shim_test.go:619), so a corrupted constant
on the shim side cannot make those tests pass vacuously — the mutation run in §3 shows
exactly that failure mode being caught.

## 7. Gates (verbatim results)

```
$ CGO_ENABLED=0 go build ./...
== build ok ==
$ CGO_ENABLED=0 go vet ./cmd/openjev-shim/... ./filter/...
== vet ok ==
$ CGO_ENABLED=0 go test -count=1 -timeout 240s ./cmd/openjev-shim/... ./filter/... ./memory/... ./mcpserver/...
ok  	github.com/wallfacers/engram/cmd/openjev-shim	6.523s
ok  	github.com/wallfacers/engram/filter	0.013s
ok  	github.com/wallfacers/engram/filter/jev	4.692s
ok  	github.com/wallfacers/engram/memory	4.246s
ok  	github.com/wallfacers/engram/memory/curation	0.949s
ok  	github.com/wallfacers/engram/memory/eventstore	0.012s
ok  	github.com/wallfacers/engram/memory/evidencecompiler	0.025s
?   	github.com/wallfacers/engram/memory/evidencecompiler/internal/contracts	[no test files]
ok  	github.com/wallfacers/engram/memory/evidencecompiler/internal/extract	0.012s
ok  	github.com/wallfacers/engram/memory/evidencecompiler/internal/need	0.018s
ok  	github.com/wallfacers/engram/memory/evidencecompiler/internal/render	0.012s
ok  	github.com/wallfacers/engram/memory/evidencecompiler/internal/resolve	0.011s
ok  	github.com/wallfacers/engram/memory/evidencecompiler/internal/validate	0.010s
ok  	github.com/wallfacers/engram/memory/pipeline	0.391s
ok  	github.com/wallfacers/engram/memory/prompt	0.005s
ok  	github.com/wallfacers/engram/mcpserver	28.107s
== tests ok ==
$ CGO_ENABLED=0 go test -race -count=1 -timeout 240s ./cmd/openjev-shim/...      # <- as configured
go: -race requires cgo; enable cgo by setting CGO_ENABLED=1
exit=2
```

The `-race` line of the configured gate **cannot pass as written**: Go requires cgo for
the race detector, so `CGO_ENABLED=0 go test -race` is rejected by the toolchain before
any test runs (environment conflict, not a code failure). Re-run with cgo enabled —
which is the only way to run the race check at all — plus the package this slice changed:

```
$ CGO_ENABLED=1 go test -race -count=1 -timeout 240s ./cmd/openjev-shim/...
ok  	github.com/wallfacers/engram/cmd/openjev-shim	7.317s
== race ok ==
$ CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/locomo-bench/
ok  	github.com/wallfacers/engram/cmd/locomo-bench	43.962s
== locomo ok ==
exit=0
```

Repo hygiene: `git diff --name-only -- memory embedding provider store internal` → empty;
`filter/` → empty; nothing staged (`git diff --cached --stat` → empty). Changed files:
`cmd/openjev-shim/{main.go,shim.go,shim_test.go}` (new), `cmd/locomo-bench/jevfilter_protocol.go`,
`cmd/locomo-bench/jevfilter_protocol_test.go`,
`specs/051-jev-relevance-filter/contracts/relevance-filter.md`. Scratch harnesses/binary
lived outside the repo (session scratchpad).

## 8. Deviations (explicit)

1. **`-race` gate needs `CGO_ENABLED=1`** — see §7; the configured command is unsatisfiable
   as written. The repo's own hard gate (`CGO_ENABLED=0` build/vet/test) is untouched and
   green.
2. **Two reasoning-tag spellings** (`think`/`/think` and `thinking`/`/thinking`) are
   dropped instead of one. Small extension beyond the literal ask; rationale: the real
   upstream's exact wrapper is unverifiable from here and a missed close tag would have
   been read as "unterminated span" (answer discarded → 502). Tags are fragment-built and
   covered by tests.
3. **Repo report path did not exist**: `specs/051-jev-relevance-filter/impl-slice7-openjev-shim.md`
   was absent in this working copy (no three-line leftover present); per the runtime
   override this report is written to the run's artifact path
   (`…/outputs/71038493-…/specs/051-jev-relevance-filter/impl-slice7-openjev-shim.md`).
   The parent can copy it into `specs/051-jev-relevance-filter/` if a tracked copy is wanted.
4. **`opt.jevDeadline` retained as an explicit override** (default 30 s) rather than
   deleted; the field keeps a single reader and an arm run always takes the 30 s default.
5. **Strict flat decode of the answer object** (documented in §4): a non-numeric extra
   field triggers the retry path rather than being ignored.

## 9. Open risks / next steps

* **Not exercised against the real box** (no access here): the shim has only ever talked
  to httptest fakes. First smoke on the box is `curl /healthz` + one pointer POST (the
  §2 transcript is the template); the two things to watch are (i) whether vllm returns
  the JSON in `message.content` — a reply that leaves only `reasoning_content` (empty
  content) is refused by design (`assistantText`, shim.go:270) → retry → 502, so enable
  the reasoning parser or make sure the model closes its thought; (ii) the wrapper
  spelling, already covered for both common pairs.
* **`FilterMeta.Backend` still reads `"jev"`** for an openjev run: the engine's
  `Backend` constants are `none`/`jev` only, and `filter/jev` sets it internally; the
  `openjev` label lives in the registration/artifacts (model revision, base-URL host),
  not in code. Making it a code-level label would be an engine change — deliberately not
  taken here.
* **Operational**: the eval harness refuses to start without a non-empty
  `ENGRAM_JEV_API_KEY`; for openjev the operator must set a placeholder (the shim ignores
  auth). The shim's own upstream timeout is 120 s, comfortably longer than the client's
  30 s, so the client hangs up first (no half-consumed upstream call).
* **429/negative cache**: the client keeps a 30 s negative cache per failing endpoint;
  a shim restart mid-run therefore degrades for up to 30 s — expected, not a defect.