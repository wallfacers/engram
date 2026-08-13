# Implementation Plan: Portable Memory Evidence Guidance

**Branch**: `039-memory-evidence-guidance` | **Date**: 2026-08-13 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/039-memory-evidence-guidance/spec.md`

## Summary

Ship a compact, versioned memory-evidence contract on the two adapter surfaces
that agents actually consume. The engram Skill body and reference explain how
to synthesize answers from bounded memory evidence. MCP initialization exposes
the same core invariants even without the Skill, while per-tool descriptions
and standard annotations describe side effects. `memory_search` additively
reports bounded scope and public engine provenance. No answer service, model
dependency, retrieval change or engine modification is introduced.

## Technical Context

**Language/Version**: Go 1.25.0, Markdown/JSON contracts, `CGO_ENABLED=0`

**Primary Dependencies**: Existing `github.com/modelcontextprotocol/go-sdk/mcp`
v1.5.0; existing engram public engine API; no new dependency

**Storage**: Existing per-namespace SQLite stores; no schema or persistence change

**Testing**: Offline Go contract/integration/parity tests using MCP SDK in-memory
transport; static Skill/reference contract checks

**Target Platform**: Local MCP clients on Linux/macOS/Windows through the existing
pure-Go stdio server

**Project Type**: Thin MCP adapter plus distributable Agent Skill

**Performance Goals**: No additional search or model calls; only additive
serialization fields and protocol metadata

**Constraints**: Local-first, stdio, namespace-isolated, secrets via environment,
no network requirement, no new answer endpoint, no protected engine-directory diff

**Scale/Scope**: One versioned guidance contract, ten always-on tools, one
conditional legacy ingest tool, and the existing top-k search response

## Constitution Check

*GATE: Passed before Phase 0 and re-checked after Phase 1.*

| Principle | Result | Evidence |
|---|---|---|
| I. Local-first, offline by default | PASS | Guidance is static metadata. No service or model is required; offline tools and degradation remain unchanged. |
| II. Engine/adapter separation | PASS | Changes are limited to `mcpserver/`, `skills/engram/`, and `specs/039-*`; engine packages are read-only dependencies. |
| III. Contract-first & namespace isolation | PASS | Version, instructions, annotations and additive JSON fields are frozen before code. Every operation stays in one validated namespace. |
| IV. Evaluation regression gate | PASS | No retrieval/extraction/curation/storage/embedding behavior changes. Existing direct-retriever parity is the appropriate adapter proof; no score claim is made. |
| V. Graceful degradation & honest scale | PASS | The contract explicitly says results are a bounded subset and that empty/degraded results are not proof of absence. |

**Post-Phase-1 re-check**: The design adds no engine entry point, schema,
background task, endpoint, or scale claim. MCP annotations remain hints; server
validation and explicit mutation intent stay authoritative.

## Project Structure

### Documentation (this feature)

```text
specs/039-memory-evidence-guidance/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── quickstart.md
├── contracts/
│   └── evidence-guidance.md
├── checklists/
│   └── requirements.md
└── tasks.md
```

### Source Code (repository root)

```text
mcpserver/
├── server.go                    # initialization guidance, descriptions, annotations
├── tools.go                     # additive search envelope/provenance mapping
├── tools_test.go                # MCP metadata and response contract tests
├── parity_test.go               # direct retriever parity remains authoritative
└── skill_contract_test.go       # cross-surface guidance/version contract

skills/engram/
├── SKILL.md                     # activation metadata plus operational synthesis workflow
├── evals/evals.json             # portable behavior cases for a separate runner
└── references/
    ├── evidence-guidance.md     # versioned portable guidance
    └── mcp.md                   # exact MCP response semantics

docs/guides/mcp-server.md        # user-facing discovery and search-scope guide

memory/ embedding/ provider/ store/ internal/  # MUST remain unchanged
```

**Structure Decision**: Put execution hints where consumers discover them:
Skill body for Codex workflows, MCP initialize instructions for generic MCP
clients, and per-tool descriptions/annotations for tool planning. Keep the
machine response additive and the engine untouched.

## Implementation Strategy

1. Add failing contract tests for initialization guidance, annotation profiles,
   bounded search metadata, provenance serialization and Skill/MCP alignment.
2. Define the versioned human-readable contract, add reusable behavior cases,
   and update the Skill body; keep frontmatter concise and trigger-oriented.
3. Configure MCP server instructions and tool metadata using SDK-native fields.
4. Extend only the MCP search output adapter with public fields already present
   on `memory.Result`; preserve the direct retriever order and content.
5. Update the MCP reference and quickstart examples.
6. Run offline build/touched-package tests and prove protected engine directories
   have zero diff. Do not run a model-backed benchmark because no engine behavior
   or product score claim changed.

## Complexity Tracking

No constitution violations.
