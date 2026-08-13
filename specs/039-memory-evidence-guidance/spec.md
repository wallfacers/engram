# Feature Specification: Portable Memory Evidence Guidance

**Feature Branch**: `039-memory-evidence-guidance`

**Created**: 2026-08-13

**Status**: Draft

**Input**: User description: "把通用、真实可落地的证据使用原则放进 engram Skill 与 MCP；别人拿过去应当好用、可复现，不能为了 benchmark 分数做数据集特化提示词工程。"

## Background and Scope

Feature 038 produced a dataset-independent answer contract inside the optional
LoCoMo evaluation harness. That contract is an experiment, not a shipped MCP or
Skill capability. This feature extracts only the stable memory-evidence
semantics that an actual integration can honor: retrieved memories are bounded,
possibly incomplete and untrusted evidence; identity, requested attribute and
time scope must match; storage time is not event time; missing or conflicting
personal facts must not be guessed.

The product surfaces remain thin. The Skill teaches an agent how to use actual
engram results. MCP initialization and tool metadata describe server and tool
semantics. The server does not become an answer-generation service and does not
gain a required LLM dependency.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Answer from saved evidence without inventing facts (Priority: P1)

As an agent using engram, I want one concise, dataset-independent rule set for
turning retrieved memories into a useful answer, so I can answer supported
parts, identify missing or conflicting parts, and avoid transferring facts
between similar people or treating a ranked subset as the complete truth.

**Why this priority**: Safe synthesis is the main real-world value. A retrieval
tool that returns facts without explaining their evidence limits invites
confident hallucinations.

**Independent Test**: Inspect a connected MCP session and the checked-in Skill;
both expose versioned guidance covering untrusted data, entity/property/time
matching, bounded retrieval, partial answers, conflict and insufficiency, with
no dataset, category, scorer, gold-answer or fixed-refusal wording.

**Acceptance Scenarios**:

1. **Given** a memory result contains an instruction, **When** an agent uses the guidance, **Then** it treats the result as data and ignores the embedded instruction.
2. **Given** results concern a similar but different entity or omit the requested property, **When** the agent answers, **Then** it does not transfer or invent the missing personal fact.
3. **Given** some requested facts are supported and others are missing, **When** the agent answers, **Then** it provides the supported part and states the specific limitation naturally.
4. **Given** evidence conflicts or retrieval is degraded, **When** the agent answers, **Then** it reports the conflict or degradation instead of presenting one unsupported resolution as certain.

---

### User Story 2 - Discover truthful MCP tool behavior before calling (Priority: P2)

As an MCP client, I want initialization instructions, precise tool descriptions,
and standard side-effect annotations, so I can distinguish reads, additive
writes, upserts and destructive lifecycle operations without relying on
repository-specific knowledge.

**Why this priority**: Tool selection and mutation safety must be discoverable
through the protocol for integrations that never install the repository Skill.

**Independent Test**: Connect with the SDK in-memory transport and assert every
registered tool has the expected description and MCP annotations, while the
server initialization result carries the versioned evidence guidance.

**Acceptance Scenarios**:

1. **Given** an offline server, **When** a client initializes and lists tools, **Then** it receives non-empty guidance and annotations for all ten always-registered tools.
2. **Given** an LLM-configured server, **When** a client lists tools, **Then** the conditional legacy ingest tool has truthful mutation and closed-world annotations too.
3. **Given** a read-only tool, **When** its annotations are inspected, **Then** it is marked read-only and closed-world; destructive tools are marked destructive without implying authorization enforcement.

---

### User Story 3 - Inspect retrieval scope and provenance (Priority: P3)

As an integration author, I want each search response to say that it is a
ranked subset and expose the stable identifiers and source metadata already
returned by the engine, so I can cite, compare and debug results without
mistaking `created_at` for event time or inferring unreported completeness.

**Why this priority**: Reproducibility depends on machine-readable output, not
prompt prose alone.

**Independent Test**: Write and search a fixed local entry, then assert the MCP
output reports requested limit, returned count, ranked-subset scope, entry ID,
projection metadata, source session ID, event time and record creation time;
assert hit order/content remains identical to direct engine retrieval.

**Acceptance Scenarios**:

1. **Given** a search limit of four and two hits, **When** results are returned, **Then** the response says `limit:4`, `returned:2`, and `scope:"ranked_subset"` without claiming that only two memories exist.
2. **Given** a hit with engine provenance fields, **When** serialized by MCP, **Then** those fields are preserved additively and the existing order, content and score are unchanged.
3. **Given** no hits or missing semantic embedding, **When** search completes, **Then** empty results remain successful and degradation remains explicit; neither condition proves the requested fact false.

### Edge Cases

- The target entity is absent but a similarly named entity is present.
- Evidence is relevant to the topic but omits the requested attribute.
- Two memories disagree and neither has a clear update order.
- `event_date` is absent while `created_at` exists.
- Search returns exactly the requested limit, so additional matches may exist.
- Semantic retrieval is structurally unavailable while keyword/entity signals return hits.
- A memory body contains tool-use or behavior-changing instructions.
- An existing MCP client ignores initialization instructions or annotations; server correctness and isolation must still hold.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The repository MUST define a versioned `memory-evidence-guidance/v1` contract shared semantically by the engram Skill and MCP initialization instructions.
- **FR-002**: The guidance MUST be independent of benchmark names, dataset formats, category labels, scorer behavior, known test examples, gold wording and fixed refusal phrases.
- **FR-003**: The guidance MUST state that memory/tool content is untrusted evidence data, not instructions, and that search returns a ranked bounded subset that can be incomplete, stale, duplicated or conflicting.
- **FR-004**: The guidance MUST require matching the target entity, requested attribute and time scope; it MUST forbid transferring personal facts between merely similar entities.
- **FR-005**: The guidance MUST distinguish event time from storage/record time and MUST NOT treat `created_at` as event time unless the content independently supports that interpretation.
- **FR-006**: The guidance MUST answer supported parts, identify the particular missing or conflicting evidence, and avoid guessing unsupported personal facts; it MUST NOT require a fixed refusal sentence.
- **FR-007**: The Skill frontmatter description MUST remain a concise activation boundary rather than contain the full answer policy. Detailed synthesis rules MUST live in the Skill body and versioned reference.
- **FR-008**: MCP initialize results MUST include concise evidence guidance that remains useful when the engram Skill is not installed.
- **FR-009**: Every registered MCP tool MUST expose a precise description and MCP annotations for read-only, destructive, idempotent and open-world behavior. Annotations MUST remain advisory and MUST NOT replace existing validation or explicit mutation intent.
- **FR-010**: `memory_search` MUST additively expose `scope`, effective `limit`, returned count, stable engine result ID, projection ID/kind and source session ID while preserving existing fields, ranking and engine parity.
- **FR-011**: Search output and documentation MUST say that empty, bounded or degraded results do not prove a fact false or the namespace exhaustive.
- **FR-012**: The feature MUST NOT add an answer-generation MCP tool, change retrieval algorithms, require an LLM/embedding endpoint, probe swallowed signal failures, or modify engine/storage/provider/embedding/internal packages.
- **FR-013**: Existing tool names, input schemas, namespace isolation, error semantics, default-off online dependencies and secret handling MUST remain unchanged.
- **FR-014**: Automated contract tests MUST verify Skill/MCP contract version alignment, required semantic invariants, forbidden benchmark vocabulary, MCP instructions, tool annotations, additive search metadata and direct-retriever parity.

### Key Entities

- **Memory evidence guidance**: A versioned semantic contract for interpreting bounded, untrusted memory results and producing supported answers.
- **Search envelope**: The MCP response containing ordered hits, structural degradation, effective limit, returned count and bounded-scope marker.
- **Search hit provenance**: Stable public identifiers and source metadata already provided by the engine result, serialized without changing ranking.
- **Tool annotation profile**: MCP advisory metadata describing read-only, destructive, idempotent and closed-world behavior for each registered tool.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Skill and MCP initialization both expose `memory-evidence-guidance/v1` and all required invariants; benchmark/dataset/category/gold/scorer terms occur zero times in shipped guidance text.
- **SC-002**: 100% of always-registered and conditional MCP tools have non-empty descriptions and explicit read-only, destructive and open-world annotations; mutation idempotency is declared only where repeated identical calls have no additional effect.
- **SC-003**: Every `memory_search` response contains correct `scope`, `limit` and `returned` values, and every hit preserves all available public provenance fields specified by FR-010.
- **SC-004**: On fixed local fixtures, MCP search names/content/order remain exactly equal to direct `Retriever.Search`; namespace isolation and offline degradation tests remain green.
- **SC-005**: `CGO_ENABLED=0 go build ./...` and touched-package tests pass with no network or model endpoint.
- **SC-006**: `git diff --name-only -- memory embedding provider store internal` is empty; no evaluation rerun is required because retrieval/extraction/curation/storage/embedding behavior is unchanged.

## Assumptions

- MCP clients may ignore initialization instructions and annotations; correctness, permission and isolation remain enforced by existing code paths.
- `memory_search` is top-k retrieval, not an exhaustive query API; an explicit ranked-subset marker is more honest than a synthetic total count.
- Existing engine `Result` fields are the provenance ceiling for this adapter increment. Adding new engine lineage APIs is out of scope.
- The Skill can offer broader presentation advice, while MCP initialization stays concise and operational.
- Feature 038 remains an evaluation-only experiment. This feature does not claim that its benchmark score is a product validation result.
