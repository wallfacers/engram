# 051 T19 P3 (slice 11) — openjev-shim typesafe upstream mode (real Jev)

Branch `codex/051-jev-relevance-filter`, base HEAD `e38c878` (`git status --porcelain -- cmd/` was
empty at start). Work surface: `cmd/openjev-shim/` **only** — `memory/ embedding/ provider/ store/
internal/ filter/` and `cmd/locomo-bench/` have a zero-byte diff (`git diff --name-only` checked).
The shim now speaks two upstream protocols behind the same pointer-protocol inside edge:

- `OPENJEV_UPSTREAM=chat` (**default**, byte-identical to the shipped behavior) — one OpenAI-compatible
  `chat.completions` call against a local model.
- `OPENJEV_UPSTREAM=typesafe` (**new**) — one TypeSafe **systemone** request per pointer request against
  the real Jev evaluation model (`typesafe-ai/jev`), so a D-arm run can be labeled `jev` rather than
  `openjev`. Protocol translation only; no engine or contract change.

| item | state |
|---|---|
| upstream mode selection + fail-closed config | **done** (`OPENJEV_UPSTREAM`, key/endpoint/model knobs) |
| pointer → systemone translation (ids, state, criteria, instructions) | **done** (need ids preserved exactly) |
| systemone → pointer response translation (missing `yes` → 0, clamp, usage) | **done** (driven by the real `filter/jev` client in-test) |
| gateway headers (ai-gateway host only) | **done** (`ai-gateway-protocol-version`, `ai-evaluation-model-specification-version`, `ai-model-id`) |
| retry 429/529/503 (2 retries, 0.5s/1.0s, deadline-bounded) + 502 otherwise | **done** |
| >256 questions → 413 guard, 30s client-deadline semantics | **kept unchanged** (typesafe mode included) |
| TDD | 14 new tests, red (undefined symbols) → green |
| gates | build ✅ · vet ✅ · `./cmd/openjev-shim/ ./filter/...` ✅ · gofmt clean ✅ |

---

## 1. Env knobs (`parseConfig`, `cmd/openjev-shim/main.go:91-103`)

| env | default | required | notes |
|---|---|---|---|
| `OPENJEV_UPSTREAM` | `chat` | no | `chat` \| `typesafe`; case/space-tolerant; anything else → error naming `OPENJEV_UPSTREAM` (`exit 2`) |
| `OPENJEV_UPSTREAM_BASE_URL` | `http://127.0.0.1:8000/v1` | chat only, has default | chat mode only; unchanged |
| `OPENJEV_UPSTREAM_MODEL` | — | **chat mode only** | unchanged error text `OPENJEV_UPSTREAM_MODEL is required: …`; **not** required in typesafe mode (the body pins the typesafe model instead) |
| `OPENJEV_TYPESAFE_ENDPOINT` | `https://ai-gateway.vercel.sh/v4/ai/evaluation-model` | no | must be http(s); used verbatim, **no** path is appended |
| `OPENJEV_TYPESAFE_MODEL` | `typesafe-ai/jev` | no | sent as the body `model` and as `ai-model-id` |
| `OPENJEV_TYPESAFE_API_KEY` | — | **yes in typesafe mode** | error names `OPENJEV_TYPESAFE_API_KEY` when missing/blank; sent only as `Authorization: Bearer …` |
| `OPENJEV_MAX_QUESTIONS` | 256 | no | unchanged, both modes |
| `OPENJEV_MAX_COMPLETION_TOKENS` / `OPENJEV_THINKING` | 0 / off | no | unchanged, chat mode only |

Fail-closed happens in `applyDefaults` (`shim.go:102-137`), which both `parseConfig` and
`newShimServer` call, so a hand-built config without a key also refuses to start. Nothing in the
typesafe path prints, returns or errors the key: the startup line
(`shim.go:192-203`, now `logStartup`) carries `upstream=typesafe, endpoint …, model …` only, and the
502 bodies were asserted key-free in tests.

## 2. Translation mapping

### Request: pointer → systemone (`typesafeRequestBody`, `shim.go:786-806`)

| pointer protocol | systemone body | note |
|---|---|---|
| — | `model: <OPENJEV_TYPESAFE_MODEL>` | **not** the pointer request's `model` (that is the client's label) |
| — | `state.descriptor: "engram read-side relevance filter"` | fixed label |
| `state.query` | `state.query` | verbatim |
| `state.memories{m0:{name,text}}` | `state.memories{m0:{name,text}}` | **as given**, sent once (not once per question); nil → `{}` |
| `questions.<id>` keys | `questions.<id>` keys | **exact same ids** (`need_m0`…), one question per pointer key |
| `questions.<id>.type` (e.g. `noul`) | `questions.<id>.type: "choice"` | fixed by the systemone protocol |
| — | `criteria: {yes:"this memory is needed to answer the query", no:"this memory is not needed"}` | exact strings from the T19 endpoint confirmation |
| `questions.<id>.instructions` | `instructions: {goal:"decide whether the memory is needed for the query", rules:["judge strictly by relevance to answering the query"]}` | exact strings |

Real captured body (temporary probe test, since deleted; `state`/`questions` key order is
alphabetical because Go marshals maps sorted):

```json
{
  "model": "typesafe-ai/jev",
  "questions": {
    "need_m0": {
      "criteria": {"no": "this memory is not needed", "yes": "this memory is needed to answer the query"},
      "instructions": {"goal": "decide whether the memory is needed for the query",
                       "rules": ["judge strictly by relevance to answering the query"]},
      "type": "choice"
    }
  },
  "state": {
    "descriptor": "engram read-side relevance filter",
    "memories": {"m0": {"name": "memory-m0", "text": "body-m0"}},
    "query": "what is the deploy code?"
  }
}
```

### Headers (`applyTypesafeHeaders`, `shim.go:811-823`)

Always `Content-Type: application/json` + `Authorization: Bearer <key>`. Additionally, **only when the
endpoint host contains `ai-gateway`** (`isAIGatewayEndpoint`, `shim.go:825-833`):
`ai-gateway-protocol-version: 0.0.1`, `ai-evaluation-model-specification-version: 4`,
`ai-model-id: <typesafe model>`. A loopback/self-hosted systemone endpoint gets none of the three
(asserted on the wire).

### Response: systemone → pointer (`shim.go:875-903`, `usageOf` at `shim.go:836-842`)

| systemone | pointer response | rule |
|---|---|---|
| `answers.<id>.probabilities["yes"]` | `probabilities.<id>` | missing `yes` (or a missing answer) → `0.0` = drop; value clamped into `[0,1]` by the existing handler (`clampProbability`) |
| `answers.<id>.choice` | — | **not** used: the probabilities map is authoritative |
| `usage.inputTokens` | `usage.prompt_tokens` | |
| `usage.outputTokens` | `usage.completion_tokens` | |
| no `usage` | `usage` omitted | unknown stays unknown, never reported as 0 |

Field names are the ones chat mode already emits and the engine reads (`filter/jev/jev.go:613-618`
`pointerUsage`, `accountUsage` at `:537` tries `input_tokens` then `prompt_tokens`) — pinned by
driving the **real `jev.Client`** through the typesafe shim in-test (`TestEngineClientRoundTripAgainstTypesafeUpstream`),
which asserts `meta.InputTokens=333 / meta.OutputTokens=32`.

### Timing, retry, failure

- 429 / 529 / 503 → retry, `maxTypesafeRetries = 2`, backoff `0.5s` then `1.0s`
  (`typesafeBackoff`, `shim.go:687-690`): the initial call **plus two retries**; a retried call's usage
  is summed (existing `addUsage`).
- Everything else (transport error, other non-2xx, non-systemone 200 body) → terminal 502
  `{"error":{"message":"upstream error: typesafe upstream returned status <N>[: <upstream message>]"}}`
  — the status is always named.
- The whole retry budget lives inside **one** upstream deadline (`context.WithTimeout(ctx,
  UpstreamTimeout)` created once in `scoreTypesafe`, `shim.go:708-730`), and each backoff is
  `select`-guarded on that context (`sleepContext`, `shim.go:771-779`): the client's 30s deadline
  (051 eval) is never extended, and a backoff can never outlive it. The `>256 → 413` guard is
  untouched and stays upstream-mode independent.

## 3. Changes, exact locations

| file | lines | change |
|---|---|---|
| `cmd/openjev-shim/shim.go` | 55-82 | `shimConfig` gains `UpstreamMode`, `TypesafeEndpoint`, `TypesafeModel`, `TypesafeAPIKey` (documented; key never logged) |
| | 101-137 | `applyDefaults`: mode normalize/validate (`OPENJEV_UPSTREAM` named on error), model required only in chat mode, typesafe endpoint/model defaults + http(s) check, **key required** in typesafe mode |
| | 182-203 | `serve` now calls the extracted `logStartup`, which prints `upstream=typesafe, endpoint …, model …` (never the key) / `upstream=chat, …` |
| | 268-292 | `score` is now a dispatcher (`typesafe` → `scoreTypesafe`, else `scoreChat`); the former `score` body is unchanged as `scoreChat` |
| | 636-903 | new typesafe section: mode/endpoint/header/question/criteria constants, `typesafeBackoff`, `typesafeRetryableStatus`, `scoreTypesafe`, `callTypesafeUpstream`, `sleepContext`, `typesafeRequestBody`, `applyTypesafeHeaders`, `isAIGatewayEndpoint`, `usageOf`, and the `typesafe*` wire types + `typesafeResponse.probabilities` |
| `cmd/openjev-shim/main.go` | 8-46 | package doc: two upstream modes + the full env table (secrets note) |
| | 73-78, 91-103 | `parseConfig` doc + new env reads |
| `cmd/openjev-shim/shim_typesafe_test.go` | new (694 lines) | 14 tests + fake typesafe endpoint/recorder + systemone wire mirrors (independent copies, so a rename on either side fails) |

Nothing else was touched. `specs/051-jev-relevance-filter/tasks.md` shows as modified in
`git status` — that is the parent's own pre-existing T19 log edit, not this slice.

## 4. Tests (TDD: written first, red on `undefined: upstreamModeTypesafe`, then implemented)

| test | what it pins |
|---|---|
| `TestTypesafeTranslationPreservesNeedIDsAndState` | body shape: model, descriptor, query, memories as given, exact need ids (not memory ids), `type:choice`, criteria yes/no, instructions goal/rules, memory text sent **once**, endpoint path used verbatim, response ids + usage |
| `TestTypesafeResponseMappingClampsAndDefaultsMissingYes` | 0.87 pass-through; answer without `yes` → 0; `1.4` → 1; `-0.2` → 0; absent answer → 0; usage 10/4; reply without usage → usage omitted |
| `TestEngineClientRoundTripAgainstTypesafeUpstream` | real `filter/jev` client through the typesafe shim: probs `[0.9,0.2,0.9]`, not degraded, `in:333 out:32` (field-name contract) |
| `TestTypesafeHeadersForGatewayAndOtherHosts` | gateway host → all three gateway headers + bearer + JSON; other host → none of the three |
| `TestTypesafeRequestHeadersOnTheWire` | loopback endpoint really received bearer/JSON and **no** `ai-model-id` |
| `TestTypesafeRetriesRetryableStatusesThenSucceeds` (429, 529, 503) | one retry then success; measured backoff ≈0.5s (≥400ms, ≤2s) |
| `TestTypesafeRetryExhaustionReturns502WithTheStatus` | 3 calls (initial + 2 retries), second gap ≥900ms, 502 names `429` + upstream message, key absent from the body |
| `TestTypesafeNonRetryableStatusReturns502WithoutRetry` (400, 401, 500) | 1 call each, 502 names the status, key absent |
| `TestTypesafeBackoffIsBoundedByTheUpstreamDeadline` | `UpstreamTimeout=100ms` + always-429 → 1 call, returns in 0.13s (backoff cut by the deadline) |
| `TestTypesafeUnparseableReplyReturns502` | 200 with a non-systemone body → 502, no retry |
| `TestTypesafeTooManyQuestionsReturns413WithoutUpstreamCall` | 413 guard holds in typesafe mode, zero upstream calls |
| `TestParseConfigUpstreamModeEnv` | default is `chat`; typesafe needs only the key; endpoint/model defaults; overrides win; `" TypeSafe "` accepted |
| `TestParseConfigUpstreamModeFailClosed` | typesafe without/with blank key → error naming `OPENJEV_TYPESAFE_API_KEY`; unknown mode → `OPENJEV_UPSTREAM`; chat still requires the pinned model; `newShimServer` refuses a keyless typesafe config and a bad endpoint |
| `TestStartupLogNamesTheUpstreamWithoutTheKey` | startup line has `upstream=typesafe`/endpoint/model; the key never appears; chat line has `upstream=chat` |

Regression proof for the default path: `shim_test.go` is **unmodified** (`git status` shows no diff
for it) and all 29 pre-existing top-level tests pass untouched, including the golden chat body/budget/thinking
tests, the 503→502 single-call test, and the engine-client round trip. No test contacts the real
endpoint or the network (httptest only); no credential literal exists outside the test fixtures.

## 5. Gates (pasted, run in `~/workspace/github/engram`)

```
$ CGO_ENABLED=0 go build ./...
$ CGO_ENABLED=0 go vet ./cmd/openjev-shim/
$ CGO_ENABLED=0 go test -count=1 -timeout 300s ./cmd/openjev-shim/ ./filter/...
ok  	github.com/wallfacers/engram/cmd/openjev-shim	9.937s
ok  	github.com/wallfacers/engram/filter	0.006s
ok  	github.com/wallfacers/engram/filter/jev	4.654s
gate_exit=0

$ gofmt -l cmd/openjev-shim/          # (no output = clean)
$ git diff --name-only -- memory embedding provider store internal filter   # (empty)
$ git diff --name-only -- cmd/locomo-bench                                  # (empty)
$ git diff --cached --name-only                                             # (empty, nothing staged)
```

## 6. Deviations

None from the assigned direction. Three interpretation calls, all recorded here:

1. **"retry (up to 2, backoff 0.5s/1.0s)"** is implemented as *two retries* (initial call + 2 = 3
   attempts max), because two backoff values were specified. Exhaustion (all 429) therefore makes 3
   upstream calls, asserted in `TestTypesafeRetryExhaustionReturns502WithTheStatus`.
2. **`OPENJEV_UPSTREAM_MODEL` is required only in chat mode.** Typesafe mode pins the body `model`
   from `OPENJEV_TYPESAFE_MODEL` (default `typesafe-ai/jev`), so requiring the chat model would make a
   typesafe run need an irrelevant variable; the key is the only required typesafe knob, as specified.
3. **A nil `state.memories` is marshalled as `{}`, not `null`** (the pointer client always sends a map,
   so this only affects hand-made requests) — keeps the shared state an object.

## 7. Residual risks / handoff

- The `ai-gateway` host branch is covered at function level, not on the wire: an `httptest` server
  cannot borrow the `ai-gateway.vercel.sh` host, so the end-to-end test asserts header *absence* for a
  loopback endpoint while `TestTypesafeHeadersForGatewayAndOtherHosts` asserts present-for-gateway.
  A real gateway smoke (one paid call) is the only thing that closes this fully; no such call was made
  here (the parent already proved HTTP 200 from both the local machine and the box).
- 529 is treated as retryable but was not exercised against the real gateway; 429/503 are the ones the
  gateway is documented to return.
- Box wiring (no code change needed): `OPENJEV_UPSTREAM=typesafe`,
  `OPENJEV_TYPESAFE_API_KEY=$AI_GATEWAY_API_KEY` (values stay in `~/.engram-eval-secrets.env`, env
  channel only), optional `OPENJEV_TYPESAFE_ENDPOINT/MODEL`; the eval side keeps
  `ENGRAM_JEV_BASE_URL=http://127.0.0.1:8020`, `ENGRAM_JEV_API_KEY=<non-empty>`, and must set
  `ENGRAM_JEV_MODEL=typesafe-ai/jev` (pinned; the harness refuses floating tags) so the registration
  names the model actually measured.
- `-race` was not run: this repo is CGO-disabled (the race detector needs CGO), the gate does not
  request it, and the shim's concurrency test plus the immutable-server design are otherwise unchanged.
- Not run here: the four-arm T19 eval — that is the Constitution IV gate and needs the box + budget.
