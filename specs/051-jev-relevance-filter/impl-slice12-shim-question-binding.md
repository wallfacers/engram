# T19 slice 12 — openjev-shim typesafe translation: self-contained question binding

Branch: `codex/051-jev-relevance-filter`, base HEAD `239f573` (clean for `cmd/` at slice start).
Work surface: `cmd/openjev-shim/` only. Engine untouched (`git diff --name-only -- memory embedding provider store internal` empty).

## Diff summary (2 files, +264/−19)

- **`cmd/openjev-shim/shim.go`** (+90/−~12)
  - `typesafeRequestBody` now delegates each key to `typesafeQuestionFor(key, memories, query)`.
  - `typesafeQuestionFor`: resolved key → self-contained goal
    `decide whether the memory '<memory text>' is needed to answer the query '<query>'`
    plus rules `["judge strictly by relevance to answering the query", "answer no for memories about unrelated people, topics, or facts"]`; unresolved key → byte-identical generic question (goal `decide whether the memory is needed for the query`, single rule) and no error.
  - `lookupTypesafeMemory`: key convention + robust fallback (below).
  - `typesafeMemoryExcerpt`: 2000-char bound with `…`; backs off to a rune boundary (never splits a UTF-8 rune); `unicode/utf8` import added.
  - Constants added: `typesafeGoalFormat`, `typesafeAgnosticRule`, `needQuestionPrefix`, `maxTypesafeGoalMemoryChars`; existing `typesafeGoal`/`typesafeRule` comments re-scoped as the generic fallback.
  - Unchanged: criteria strings, `type:"choice"`, `state {descriptor, query, memories}` (memories still sent in full), headers, retry/backoff, response mapping, 413 guard, chat mode.
- **`cmd/openjev-shim/shim_typesafe_test.go`** (+193/−~7): existing translation test updated to the per-question shape; 4 new focused tests.
- **`cmd/openjev-shim/shim_test.go`**: unmodified (verified missing from `git status`); chat-mode tests all green.

## Lookup convention found in the parser

Client side: `filter/jev/jev.go` — `candidateRef` (line ~329) documents *"state.memories is keyed by the memory id (contract §2), while the conditional question is keyed `need_<memory id>`"*; `shardCandidates` builds `qKey: "need_" + memKey` (memKey = candidate ID, or `c<idx>` when empty/duplicate), and `memories(shard)` keys the state map by exactly `memKey`. `retrievalQuestions` puts the query into the pointer instruction, but the shim is what must bind it for systemone.

So the shim's resolution is: `memories[key]` first (covers a map keyed by the question id), then `memories[strings.TrimPrefix(key, "need_")]` when the prefix is present. Miss → generic fallback. This also keeps the write-gate call (`durable`/`preference`/`secret` against memory `draft`) on the generic question, unchanged.

## Tests (TDD: red first — compile-level red, then green)

New/updated in `shim_typesafe_test.go`:
- `TestTypesafeTranslationPreservesNeedIDsAndState` (updated): per-id literal goals for `need_m1`/`need_m2` (catches cross-binding), both measured rules, criteria unchanged, state memories as given; the old "memory text once in the body" count is now 2 (shared state + its own goal).
- `TestTypesafeQuestionBindsItsOwnMemoryAndQuery` (new): the measured scenario — query "What is Ana's favorite food?" with `m0`="Ana's favorite food is sushi." and `m1`="Bob plays chess every weekend."; exact goal per question with its OWN text and the query.
- `TestTypesafeQuestionRulesCarryTheMeasuredRecipe` (new): rules slice exact-match, second rule verbatim.
- `TestTypesafeQuestionFallsBackToGenericInstructionsOnMemoryMiss` (new): `need_m9` with no state entry and write-gate key `durable` both keep the generic goal + generic single rule, HTTP 200, every key still answered.
- `TestTypesafeQuestionGoalTruncatesLongMemoryText` (new): wire-level goal == `…` + first 2000 chars; `state.memories["m0"].text` still the full text; exact-bound passthrough; multi-byte rune boundary yields valid UTF-8, no U+FFFD.
- `TestEngineClientRoundTripAgainstTypesafeUpstream` (t6): untouched, passes (probabilities/usage contract intact).
- Chat mode (t7): `shim_test.go` untouched, whole package green.

## Gate output

```
== gofmt ==
(empty — clean)
== build ==
ok
== vet ==
ok
== tests ==
ok  	github.com/wallfacers/engram/cmd/openjev-shim	10.075s
ok  	github.com/wallfacers/engram/filter	0.008s
ok  	github.com/wallfacers/engram/filter/jev	4.689s
```
Verbose package run: 47 top-level PASS, 0 FAIL. No real-endpoint calls (httptest only); no secrets in tests.

## Deviations / notes

1. The generic fallback keeps the **single** rule (exact current behavior), per "fail safe: keep the generic instructions"; only resolved questions get the second measured rule. Both variants are pinned by tests.
2. One pre-existing test assertion changed beyond the goal/rules text: the shared-state "memory text appears once" count became 2 (state + own goal) — an intended consequence of self-contained binding, re-pinned with an explanatory comment.
3. Truncation backs off to a rune boundary before appending `…`; without it a byte-bound cut would reach the wire as U+FFFD (json replaces invalid UTF-8).
4. Robustness added to the lookup is limited to checking the full question key before the stripped one — no speculative key-shape machinery.
5. `specs/051-jev-relevance-filter/tasks.md` is modified in the working tree but **not by this slice** (parent's slice-11/12 log; mtime predates this slice's first edit). Left untouched. Unrelated untracked files also left untouched.

For s13 (locomo-bench `--repeats` parameterization): no shim-side follow-up needed; typesafe mode now sends self-contained questions.