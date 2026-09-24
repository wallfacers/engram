# 051 T19 P1.5c — openjev shim upstream budget + thinking switch (slice 7b)

Branch `codex/051-jev-relevance-filter`. Work surface: **`cmd/openjev-shim/` only**.
Two real-box smoke defects fixed, TDD (tests written first → red → implementation → green).
All four gates green; 29 top-level tests in the package (22 pre-existing, untouched in
behaviour **except one** stale `max_tokens` assertion; 7 new).

| item | state |
|---|---|
| DEFECT 1 — under-budget `max_tokens` → `finish_reason=length` → unparseable → retry → 502 | **fixed** (`maxTokensFor(questions, override)`: `24q+1024`, floor 1280, cap 8192) |
| DEFECT 2 — thinking prose burns the completion budget | **fixed at the wire** (`chat_template_kwargs.enable_thinking=false` in the outbound body, `OPENJEV_THINKING` escape hatch) |
| INBOUND pointer wire | **unchanged** — `TestPointerWireShapeRoundTrip` green, unmodified |
| engine discipline (`memory/ embedding/ provider/ store/ internal/ filter/`) | **zero diff** (`git diff --name-only -- …` empty) |
| gates | build ✅ · vet ✅ · `go test ./cmd/openjev-shim/... ./filter/...` ✅ · `-race` (CGO_ENABLED=1) ✅ · gofmt clean |

> **Parameter correction applied.** My brief's DEFECT 1 numbers (`24q+128`, floor 256) were
> superseded mid-run by parent forensics from the P2b box smoke: the upstream emits
> **~2,497 chars ≈ 625+ tokens of inline thinking on every call** (no `reasoning_content`
> key, no parser — a fixed per-call tax independent of question count). The shipped formula is
> therefore `24*questions + 1024`, **floor 1280, cap 8192**. A 2-question request at 256 tokens
> would still truncate on the real upstream; at 1280 it clears the tax. The correction was
> incorporated **before the first green run** — the red-state evidence below is a compile-level
> red on the (older) helper signature, and the boundary tests as committed are all on the
> corrected numbers.
> Cross-check: the concurrent P2b worker's `specs/051-jev-relevance-filter/tasks.md` entry
> records the identical parameters (24q+1024 / floor 1280 / cap 8192 / 2,497 chars ≈ 625+ tok).

---

## 1. DEFECT 1 — the budget

`maxTokensFor(questions, override int) int` — one helper, `cmd/openjev-shim/shim.go`:

```go
func maxTokensFor(questions, override int) int {
	if override > 0 {
		return override // operator override: verbatim, floor and cap included
	}
	budget := maxTokensPerQuestion*questions + responseTokenAllowance // 24q + 1024
	if budget < minCompletionTokens {                                // 1280
		return minCompletionTokens
	}
	if budget > maxCompletionTokens {                                // 8192
		return maxCompletionTokens
	}
	return budget
}
```

Constants (all named, all documented at definition site):

| constant | value | why |
|---|---|---|
| `maxTokensPerQuestion` | 24 | a real shard entry `"need_m123": 0.85,` costs 12–15 tokens after BPE splits the key digits → ~2x headroom. The old 4 was the truncation. |
| `responseTokenAllowance` | 1024 | the measured per-call inline-thinking tax (~2,497 chars ≈ 625+ tok, not exposed as `reasoning_content`) + object braces + the echoed response skeleton. |
| `minCompletionTokens` | 1280 | the tax alone is ~625 tok, so `24q+1024 = 1072` at q=2 still truncates a real reply; 1280 leaves ~1k for the JSON. |
| `maxCompletionTokens` | 8192 | one over-wide shard must not ask the upstream for unbounded output. |

Chosen boundary values (all asserted in `TestMaxTokensForBudgetBoundaries`):

| questions | computed `24q+1024` | sent | boundary exercised |
|---|---|---|---|
| 0 | 1072 | **1280** | floor |
| **2** | 1072 | **1280** | floor (brief's named case) |
| 5 | 1144 | **1280** | floor — the golden test's count |
| 32 | 1792 | **1792** | slope |
| **64** | 2560 | **2560** | the real shard size (old budget: 320 → truncated) |
| 298 | 8176 | **8176** | last size below the cap |
| 299 | 8200 | **8192** | first size past the cap |
| 1024 | 25648 | **8192** | cap |
| any, `OPENJEV_MAX_COMPLETION_TOKENS=512` | — | **512** | override wins over computed |
| q=2, override 128 / q=64, override 16384 | — | 128 / 16384 | override is verbatim (may sit below the floor / above the cap) |

**Override semantics (explicit design call, flagged for review):** `override > 0` returns the
operator's number **verbatim**, bypassing floor and cap. Rationale: the env var exists so an
operator can fit an upstream context window or shrink a test's cost without a rebuild, and a
silently re-clamped override is a silent behaviour change. `0`/unset = computed. If a reviewer
prefers the cap (or floor) to also bind the override, it is a one-line change in `maxTokensFor`
plus two table rows.

## 2. DEFECT 2 — the thinking switch

`chatRequest` gains one field; the shim sets it unless thinking is explicitly enabled:

```go
	call := chatRequest{Model: …, Messages: …, Temperature: 0, MaxTokens: maxTokensFor(len(keys), s.cfg.MaxCompletionTokens)}
	if !s.cfg.Thinking {
		// vllm exposes the chat template's thinking switch as a request-body
		// extension rather than a serve-time flag.
		call.ChatTemplateKwargs = map[string]bool{"enable_thinking": false}
	}
	body, err := json.Marshal(call)
```

```go
	ChatTemplateKwargs map[string]bool `json:"chat_template_kwargs,omitempty"` // nil => field omitted
```

Real outbound body (captured this run: built binary + fake upstream + a **64-question** pointer
request; `messages` elided to roles, tail shown raw):

```json
{
  "model": "Qwen/Qwen3.6-35B-A3B-FP8",
  "messages": ["system", "user"],
  "temperature": 0,
  "max_tokens": 2560,
  "chat_template_kwargs": {"enable_thinking": false}
}
```
raw tail: `…"need_m63": 0.0}"}],"temperature":0,"max_tokens":2560,"chat_template_kwargs":{"enable_thinking":false}}`
→ `POST /answers` **200**, all 64 keys answered, usage passed through.
Startup log line (real): `… max_questions=256, max_completion_tokens=0 (0 = computed), thinking=false`.

## 3. Environment semantics

| env | values | meaning |
|---|---|---|
| `OPENJEV_MAX_COMPLETION_TOKENS` | unset / `0` | compute it (`24q+1024`, floor 1280, cap 8192) |
| | positive int | sent verbatim as `max_tokens` |
| | negative / non-numeric (`abc`, `1.5`) | **`parseConfig` error** (exit 2), never silently ignored |
| `OPENJEV_THINKING` | unset / `off` (default) | send `chat_template_kwargs:{"enable_thinking":false}` |
| | `on` (case-insensitive, trimmed) | omit the field entirely — for an upstream that rejects unknown body keys |
| | anything else (`maybe`, `true`) | **`parseConfig` error** named with `OPENJEV_THINKING` |
| `OPENJEV_UPSTREAM_BASE_URL` / `_MODEL` / `OPENJEV_MAX_QUESTIONS` | unchanged | previous slice |

`thinking=false` in the startup log means "thinking off" = the field **is** sent (the log prints
the config field, whose default is the shipped behaviour).

## 4. Test list

New (7 top-level, 22 sub-cases) — all in `cmd/openjev-shim/shim_test.go`:
1. `TestMaxTokensForBudgetBoundaries` — the 10-row table above (floor 2 & 5, 32, 64, 298/299 cap
   pair, wide cap, 3 override rows).
2. `TestUpstreamBodyBudgetAndThinkingOffByDefault` — handler level, 2 questions: `max_tokens == 1280`
   **and** the exact bytes carry `"chat_template_kwargs":{"enable_thinking":false}` (raw-body check),
   probabilities normal, exactly 1 upstream call.
3. `TestUpstreamBudgetScalesWithQuestionCount` — 5 q → 1280, 64 q → 2560 through `/answers`, with the
   model's answer still surviving (`need_m00 == 0.9`).
4. `TestUpstreamBudgetHonorsTheCapForWideShards` — 400 q (MaxQuestions raised) → `max_tokens == 8192`.
5. `TestOpenJEVThinkingOnOmitsChatTemplateKwargs` — config sourced from `parseConfig` with
   `OPENJEV_THINKING=on`: decoded kwargs nil **and** no `chat_template_kwargs` substring in the raw
   body, probabilities still normal.
6. `TestOpenJEVMaxCompletionTokensOverrideReachesTheBody` — `OPENJEV_MAX_COMPLETION_TOKENS=3333`
   → body `max_tokens == 3333`, probabilities normal.
7. `TestParseConfigBudgetAndThinkingEnv` — unset/0/positive override, `on`/` ON `/`off`, both explicit,
   plus 5 invalid values rejected with a named `OPENJEV_` error.

Updated (1, as permitted):
- `TestAnswersGoldenWithClampingAndMissingKeys` — the only test asserting the old body value; now expects
  `256 → 1280` for its 5-question request (`24*5+1024 = 1144`, floored).

Test-infrastructure changes (no behaviour asserted elsewhere): the fake upstream now records the exact
request bytes (`rec.rawAt(i)` — a decoded nil map cannot distinguish *omitted* from *sent as null*) and
`newShimForTest` delegates to `serveShimForTest`, so an env-parsed config and a hand-built one take the
identical path. `TestPointerWireShapeRoundTrip` and the other 21 tests are byte-unchanged.

TDD evidence: tests first → `CGO_ENABLED=0 go test ./cmd/openjev-shim/...` failed to build
(`too many arguments in call to maxTokensFor`, `cfg.MaxCompletionTokens undefined`,
`cfg.Thinking undefined`) → implementation → green.

## 5. Gates (verbatim)

```
$ bash -c 'CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go vet ./cmd/openjev-shim/... \
    && CGO_ENABLED=0 go test -count=1 -timeout 240s ./cmd/openjev-shim/... ./filter/...'
ok  	github.com/wallfacers/engram/cmd/openjev-shim	6.536s
ok  	github.com/wallfacers/engram/filter	0.005s
ok  	github.com/wallfacers/engram/filter/jev	4.604s

$ CGO_ENABLED=1 go test -race -count=1 -timeout 240s ./cmd/openjev-shim/...
ok  	github.com/wallfacers/engram/cmd/openjev-shim	7.450s

$ CGO_ENABLED=0 go test -count=1 -v ./cmd/openjev-shim/ | grep -cE '^--- PASS'
29            # 22 pre-existing + 7 new; zero top-level failures
$ gofmt -l cmd/openjev-shim/
              # clean
```

Real-binary smoke (built to a scratch dir outside the repo, deleted afterwards): shim + fake
upstream + 64-question pointer request → the body in §2, `POST /answers` 200 with 64/64 keys.

## 6. Scope / hygiene

- `git diff --stat`: `cmd/openjev-shim/main.go`, `shim.go`, `shim_test.go` only.
- `git diff --name-only -- memory embedding provider store internal filter mcpserver` → **empty**.
- `git diff --cached --name-only` → **empty** (nothing staged).
- **Not mine, not touched**: `specs/051-jev-relevance-filter/tasks.md` (modified) and
  `specs/051-jev-relevance-filter/t19-p2b-deploy-smoke.md` (untracked) are the concurrent P2b
  worker's in-flight work — read only, left exactly as found.
- No secrets introduced anywhere; scratch build dir was a `mktemp -d` (system `/tmp`) removed in the
  same command — noted as a minor deviation from the scratchpad rule, nothing was left behind.

## 7. Residual risks / next steps

1. **The 1024 allowance is a measured floor, not a guarantee**: if the upstream's prompt grows
   (more memories) so does its thinking. `OPENJEV_MAX_COMPLETION_TOKENS` is the escape hatch, and
   `finish_reason` is already surfaced in the 502 message, so a recurrence is diagnosable.
2. **`chat_template_kwargs` support is vllm-specific** — it is unverified against the real box from
   here (no box access in this slice). If vllm rejects the key, `OPENJEV_THINKING=on` restores the
   previous body byte-for-byte (tested). Box re-smoke is the next step, and it is the experiment
   that tells us whether the thinking tax is gone entirely (then the budget is merely comfortable).
3. **Override is verbatim** (§1) — the one semantic a reviewer could reasonably want changed.