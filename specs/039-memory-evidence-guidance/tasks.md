# Tasks: Portable Memory Evidence Guidance

**Input**: Design documents from `specs/039-memory-evidence-guidance/`

**Tests**: Contract and parity tests are mandatory. Write each behavioral test
before its implementation and observe the expected failure.

## Phase 1: Contract Foundation

- [x] T001 Create spec, plan, research, data model, quickstart and versioned contract under `specs/039-memory-evidence-guidance/`.
- [x] T002 Add the canonical operational reference at `skills/engram/references/evidence-guidance.md`.

---

## Phase 2: User Story 1 - Safe evidence-to-answer workflow (Priority: P1)

**Goal**: Skill-enabled agents use bounded memory evidence without treating it
as instructions or inventing missing personal facts.

**Independent Test**: Static cross-surface tests find the v1 marker and every
required invariant in the Skill/reference and MCP instructions.

### Tests

- [x] T003 [US1] Add failing version/invariant/forbidden-language tests in `mcpserver/skill_contract_test.go`.

### Implementation

- [x] T004 [US1] Add the evidence synthesis workflow and reference link to `skills/engram/SKILL.md` while keeping frontmatter trigger-focused.
- [x] T005 [US1] Document the full operational contract in `skills/engram/references/evidence-guidance.md`.

---

## Phase 3: User Story 2 - Truthful MCP discovery metadata (Priority: P2)

**Goal**: Generic MCP clients discover concise server-wide evidence rules and
the side effects of every tool.

**Independent Test**: In-memory initialization and `tools/list` expose the
versioned instructions, descriptions and exact annotation profile.

### Tests

- [x] T006 [US2] Add failing initialization-instruction and tool-annotation tests in `mcpserver/tools_test.go`.

### Implementation

- [x] T007 [US2] Configure `mcp.ServerOptions.Instructions` and precise tool descriptions/annotations in `mcpserver/server.go`.

---

## Phase 4: User Story 3 - Bounded search and public provenance (Priority: P3)

**Goal**: Search responses state their bounded scope and expose inspectable
public provenance without changing retrieval.

**Independent Test**: A local search response contains correct envelope and hit
metadata while `mcpserver/parity_test.go` still matches the direct retriever.

### Tests

- [x] T008 [US3] Add failing search-envelope and provenance assertions in `mcpserver/tools_test.go` and preserve parity coverage.

### Implementation

- [x] T009 [US3] Add `scope`, effective `limit`, `returned`, IDs and source metadata to the adapter structs/mapping in `mcpserver/tools.go`.
- [x] T010 [US3] Update exact search semantics and reusable behavior cases in `skills/engram/references/mcp.md`, `skills/engram/evals/evals.json`, and `docs/guides/mcp-server.md`.

---

## Phase 5: Verification and Reproducibility

- [x] T011 Run `gofmt` on touched Go files and `git diff --check`.
- [x] T012 Run `CGO_ENABLED=0 go test -count=1 ./mcpserver`.
- [x] T013 Run `CGO_ENABLED=0 go build ./...`.
- [x] T014 Confirm `git diff --name-only -- memory embedding provider store internal` is empty and review the quickstart commands.
- [x] T015 Record offline verification and the intentionally unrun Skill behavior handoff in `specs/039-memory-evidence-guidance/validation-report.md`.

---

## Dependencies & Execution Order

- T001–T002 freeze the contract before behavior changes.
- US1, US2 and US3 are independently useful, but T003/T006/T008 must fail before
  their matching implementation tasks.
- T011–T014 follow all implementation tasks.

## Implementation Strategy

Deliver the smallest portable contract first, then enrich protocol discovery,
then add machine-readable search metadata. Do not add a model-backed answer tool
or change engine behavior if an adapter field is unavailable; record that limit
instead.
