# Quickstart: Verify Portable Memory Evidence Guidance

All checks are local and require no model or embedding endpoint.

## 1. Run the adapter contract tests

```bash
CGO_ENABLED=0 go test -count=1 ./mcpserver
```

The tests initialize an in-memory MCP client/server pair, inspect server
instructions and tool annotations, exercise `memory_search`, and retain the
direct `Retriever.Search` parity assertion.

## 2. Build the repository

```bash
CGO_ENABLED=0 go build ./...
```

## 3. Confirm adapter-only scope

```bash
git diff --name-only -- memory embedding provider store internal
```

Expected output: empty.

## 4. Inspect the two shipped guidance surfaces

```bash
rg -n 'memory-evidence-guidance/v1' \
  mcpserver skills/engram
```

Expected: MCP initialization instructions, Skill workflow/reference, and their
contract tests. The Skill frontmatter remains a short activation description;
the detailed policy lives below it.

## Evaluation note

This feature changes adapter metadata and additive output serialization only.
It does not change retrieval, extraction, curation, storage or embedding, so the
appropriate regression proof is offline MCP/direct-engine parity rather than a
model-backed score run.

The three `evidence-*` cases in `skills/engram/evals/evals.json` are the handoff
set for a separate Skill behavior run. Compare the feature version against the
pre-feature Skill at commit `35b45c1` using the same prompts and connected-tool
fixtures. Store run transcripts and review artifacts outside the repository.
Passing those cases is development evidence, not a claim about all user queries.
