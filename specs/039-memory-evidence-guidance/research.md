# Research: Portable Memory Evidence Guidance

## Decision 1: Separate activation metadata from evidence policy

**Decision**: Keep Skill frontmatter focused on when engram should trigger. Put
the detailed evidence policy in the Skill body and a versioned reference.

**Rationale**: Frontmatter is a routing surface. Long synthesis instructions
would reduce trigger precision and are invisible to MCP clients that do not
install the Skill.

**Alternatives rejected**:

- Copy the full evaluation prompt into frontmatter: mixes activation with answer
  policy and couples the product to an experimental host prompt.
- Put no guidance in the Skill: leaves Skill-enabled agents with tool mechanics
  but no safe evidence-to-answer behavior.

## Decision 2: MCP initialization carries only portable invariants

**Decision**: Use SDK `ServerOptions.Instructions` for a concise
`memory-evidence-guidance/v1` summary. It covers untrusted evidence, bounded
search, identity/property/time matching, partial answers, conflict and missing
evidence, mutation intent, namespaces and secrets.

**Rationale**: Initialization is the only server-wide instruction surface
available to generic MCP clients. It is advisory, so all correctness and safety
checks remain in server handlers.

**Alternatives rejected**:

- Add `memory_answer`: would make a replaceable LLM mandatory for a thin memory
  adapter and duplicate host answer responsibilities.
- Depend only on tool descriptions: repeats cross-cutting rules and cannot state
  a coherent result-interpretation contract.

## Decision 3: Use MCP annotations as truthful hints, not permissions

**Decision**: Every tool declares read-only, destructive, idempotent and
open-world hints. Reads are closed-world. Write/delete/lifecycle tools declare
their mutation risk. Ingest tools declare possible open-world model interaction.
All mutating tools conservatively use `idempotent=false` except `memory_delete`,
whose repeated identical call has no additional state effect.

**Rationale**: Tool annotations improve generic-client planning but the MCP SDK
explicitly defines them as untrusted hints. Conservative idempotency avoids
claiming replay safety when timestamps, extraction or lifecycle receipts may
change.

## Decision 4: Say “ranked subset” in data, not only prose

**Decision**: Add `scope:"ranked_subset"`, effective `limit`, and `returned` to
every search response. Do not add `total` because the existing retriever does not
compute an exhaustive count.

**Rationale**: A top-k response can equal its limit while more matches exist.
Machine-readable boundedness prevents consumers from interpreting absence as a
complete negative result.

**Alternatives rejected**:

- Infer a total from returned length: false when truncation occurs.
- Add another full scan: changes performance and adapter behavior for metadata.

## Decision 5: Expose only existing public provenance

**Decision**: Serialize `Result.ID`, `ProjectionID`, `ProjectionKind`, and
`SourceSessionID` in addition to the existing hit fields. Preserve `event_date`
and `created_at` while documenting their distinct meanings.

**Rationale**: These fields already cross the public engine boundary. Exposing
them is additive and helps reproducibility without an engine contract change.

**Alternatives rejected**:

- Invent evidence IDs for ordinary search: search results do not currently
  expose a stable Evidence lineage ID, and fabricating one would be misleading.
- Add an engine API in adapter scope: violates engine/adapter separation.

## Decision 6: Reproducibility is semantic-version plus contract tests

**Decision**: Both Skill and MCP surface the exact marker
`memory-evidence-guidance/v1`. Tests check the version, required invariants,
forbidden specialization language, annotations and response fields.

**Rationale**: Skill prose and concise MCP instructions serve different contexts
and should not be byte-identical. A version plus invariant checks catches drift
without forcing unreadable duplication.
