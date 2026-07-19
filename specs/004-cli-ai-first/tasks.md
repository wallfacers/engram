# Tasks: engram CLI (AI-first)

**Input**: Design documents from `/specs/004-cli-ai-first/`
**Prerequisites**: plan.md, spec.md, research.md, data-model.md, contracts/cli-commands.md

**Tests**: Included and load-bearing — retrieval parity (SC-002), namespace path-escape
(SC-004), offline CRUD (SC-001), and one-shot vector durability (SC-003) are hard
gates per the constitution and CLAUDE.md, so their tests are required, not optional.

**Organization**: Grouped by user story (US1 P1 offline CRUD MVP → US2 P2 ingest →
US3 P3 inspect/export). All new code lives in `cmd/engram/` (`package main`); the
engine (`memory/ embedding/ provider/ store/ internal/`) and `mcpserver/` are not
modified or imported (except read-only `internal/version`).

## Format: `[ID] [P?] [Story] Description`

- **[P]**: parallelizable (different file, no dependency on an incomplete task)
- **[Story]**: US1 / US2 / US3 (setup/foundational/polish carry no story label)

---

## Phase 1: Setup (Shared Infrastructure)

- [ ] T001 Create `cmd/engram/main.go` — `package main`, thin entry that calls `run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)` and `os.Exit`s the returned code.
- [ ] T002 Verify the new package builds under the hard gate: `CGO_ENABLED=0 go build ./cmd/engram` (skeleton `run` returns 0).

---

## Phase 2: Foundational (Blocking Prerequisites)

**Purpose**: config, namespace safety, engine assembly, dispatch, diagnostics — every command depends on these. No user story can start until this phase is done.

- [ ] T003 [P] Implement `cmd/engram/errors.go` — exit-code constants (0/1/2/3/4/5/6 per contracts/cli-commands.md) and a diagnostic helper that writes `<what> — <next action>` to stderr and returns the code.
- [ ] T004 [P] Implement `cmd/engram/config.go` — load Config from flags + `ENGRAM_*` env (flag wins) per data-model.md; API keys env-only; never echo keys.
- [ ] T005 [P] Write `cmd/engram/config_test.go` — flag-wins-over-env; missing `--data-dir` → usage error (exit 2); assert keys never appear in rendered output/errors.
- [ ] T006 [P] Implement `cmd/engram/namespace.go` — validator `^[A-Za-z0-9._-]{1,64}$`, reject `.`/`..`/separators, resolve `<data-dir>/<ns>.db` and assert the cleaned path stays inside the data dir.
- [ ] T007 [P] Write `cmd/engram/namespace_test.go` — SC-004 escape table (`../outside`, `a/b`, `a\b`, absolute, `.`, `..`) all rejected (exit 5) and **zero files created outside the data dir**.
- [ ] T008 Implement `cmd/engram/engine.go` — assemble an engine handle from Config via public API only (`store.Open`→`NewEntryStore`/`NewVectorStore`/`NewEmbedder`/`NewRetriever`→`pipeline.New`; embedder/pipe nil-safe); `Close()` drains `Embedder.Close()` then closes the store. No `mcpserver` import.
- [ ] T009 Implement `cmd/engram/render.go` — AI-friendly markdown helpers in the `RenderExport` house style (deterministic, pinned-first sort, snippet). Shared by read commands.
- [ ] T010 Implement `cmd/engram/run.go` — global-flag parse, subcommand dispatch table, stdout=document / stderr=diagnostics stream discipline, unknown command → exit 2.

---

## Phase 3: User Story 1 — Offline memory read/write (Priority: P1) 🎯 MVP

**Goal**: `add/search/get/list/delete` work fully offline with AI-friendly markdown.
**Independent test**: empty temp data dir, no endpoints → add→search/get/list→delete round-trip, asserting markdown names the fact.

- [ ] T011 [P] [US1] Write `cmd/engram/commands_test.go` — offline round-trip (SC-001): `add`→`search`(matching)→`get`→`list`→`delete`→`get`(not found, exit 3); assert markdown content and exit codes. (Red first.)
- [ ] T012 [P] [US1] Write `cmd/engram/parity_test.go` — SC-002: seed entries, assert `engram search <q>` hit set/order == direct `memory.Retriever.Search(q, limit)` on the same store.
- [ ] T013 [US1] Implement `add` handler in `cmd/engram/commands.go` — validate name, build `memory.Entry`, `EntryStore.Upsert`, `Embedder.Enqueue`; budget error → exit 6; markdown write confirmation.
- [ ] T014 [US1] Implement `search` handler — `Retriever.Search(query, limit)` (default 8; `--limit<=0` → exit 2); render hits (name/score/snippet/content); honest `degraded.semantic` marker when embedding absent (FR-006).
- [ ] T015 [P] [US1] Implement `get` handler — `EntryStore.GetByName`; not found → exit 3 with `... — run: engram list`; render stable fields only.
- [ ] T016 [P] [US1] Implement `list` handler — `EntryStore.List`; render all, pinned-first; empty store → valid empty doc.
- [ ] T017 [P] [US1] Implement `delete` handler — `EntryStore.Delete`; not found → exit 3; markdown deletion confirmation.
- [ ] T018 [US1] Confirm the MVP: `CGO_ENABLED=0 go test -count=1 ./cmd/engram/...` green; T011/T012 pass.

**Checkpoint**: US1 is a shippable offline memory CLI.

---

## Phase 4: User Story 2 — Ingest from conversation (Priority: P2)

**Goal**: `engram ingest` extracts facts when an LLM is configured; clean failure when not; vectors durable on exit.
**Independent test**: stub `pipeline.ModelCaller` → ingest → new entries via `list`; no-LLM → exit 4.

- [ ] T019 [P] [US2] Write `cmd/engram/ingest_test.go` — with a stub `pipeline.ModelCaller` returning facts: stdin turns → entries stored + count rendered; no-LLM config → exit 4 with the required diagnostic.
- [ ] T020 [P] [US2] Write `cmd/engram/lifecycle_test.go` — FR-008/SC-003: with a stub embedding client, `add` then a fresh handle open shows the entry's vector present (drain-before-exit); assert no vector lost.
- [ ] T021 [US2] Implement `ingest` handler in `cmd/engram/commands.go` — parse stdin turns (`user:`/`assistant:` per quickstart) into `[]pipeline.Message`; require `handle.pipe != nil` else exit 4; `Pipeline.Ingest`; render extracted count + new entry names.
- [ ] T022 [US2] Verify drain-on-exit path in `engine.go` `Close()` covers `add` and `ingest` (T020 green); ingest tests green.

**Checkpoint**: memory can be grown from dialogue; offline MVP still intact.

---

## Phase 5: User Story 3 — Inspect & export (Priority: P3)

**Goal**: `stats/export/namespaces/version` for operability and backup.
**Independent test**: known store across two namespaces → stats counts, namespace list, full export, version.

- [ ] T023 [P] [US3] Write `cmd/engram/ops_test.go` — `stats` counts; `namespaces` lists exactly the present `<ns>.db` and no others; `export` contains every entry; `version` prints and exits 0.
- [ ] T024 [P] [US3] Implement `stats` handler — `EntryStore.Count`/`CountNonPinned`/`ManifestSizeEstimate`; render markdown summary.
- [ ] T025 [P] [US3] Implement `export` handler — `EntryStore.List` → `memory.RenderExport`; stream to stdout.
- [ ] T026 [P] [US3] Implement `namespaces` handler — enumerate `<data-dir>/*.db`, strip suffix, render sorted list; ignore non-`.db` files.
- [ ] T027 [P] [US3] Implement `version` handler — print `internal/version`.`Version` (read-only import); exit 0.

**Checkpoint**: full 10-command surface complete.

---

## Phase 6: Polish & Cross-Cutting

- [ ] T028 [P] Write `cmd/engram/e2e_test.go` — build the real binary and run one `os/exec` end-to-end smoke (offline add→search) asserting stdout markdown + exit 0.
- [ ] T029 [P] Add `docs/cli.md` (operator/agent guide: build, config table, 10 commands, offline verify, agent wiring) and add a Knowledge-Map pointer line to `CLAUDE.md`.
- [ ] T030 Engine-untouched gate: assert `git diff --name-only -- memory embedding provider store internal` is empty and `mcpserver/` is not imported by `cmd/engram` (SC-006); run `CGO_ENABLED=0 go vet ./cmd/engram/...`.
- [ ] T031 Full-suite gate: `CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./...` all green.
- [ ] T032 **LAST — provisional Entry fields**: against the then-frozen Entry shape, extend `get`/`list`/`export` renderers to surface `EventStart`/`EventEnd` (and any newly-stabilized field), with a test; do this only after T031, so the CLI body first ships on stable fields. Re-run T031.

---

## Dependencies & Execution Order

- **Setup (T001-T002)** → **Foundational (T003-T010)** block everything.
  - Within Foundational: T003-T007 are `[P]` (distinct files); T008 needs Config
    (T004); T009-T010 need errors (T003). T010 dispatch needs T008/T009.
- **US1 (T011-T018)** depends only on Foundational → **MVP**.
- **US2 (T019-T022)** depends on Foundational + `add` (T013) for the lifecycle test.
- **US3 (T023-T027)** depends on Foundational; independent of US1/US2 behavior.
- **Polish (T028-T032)** after the stories; **T032 is strictly last** (after T031).

## Parallel Opportunities

- Foundational: T003, T004(+T005), T006(+T007) in parallel.
- US1: tests T011/T012 in parallel; handlers T015/T016/T017 in parallel (distinct
  logic, same file `commands.go` — coordinate edits or split per-file if needed).
- US3: T024-T027 all `[P]`.
- Stories US1/US3 can proceed in parallel once Foundational is done; US2 waits on T013.

## Implementation Strategy

MVP = Phases 1-3 (US1): a shippable offline AI-first memory CLI. Add US2 (ingest)
and US3 (ops) as independent increments. Keep the engine untouched throughout
(T030 gate); serialize provisional Entry fields dead last (T032) against the frozen
shape, honoring the agreed sequencing.
