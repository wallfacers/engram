# Contract: Canonical Skill Package

**Feature**: `020-engram-agent-skill`

**Contract version**: 1

## 1. Package boundary

The distributable unit is exactly `skills/engram/`. A valid release contains:

```text
skills/engram/
├── SKILL.md
├── LICENSE
├── references/
│   ├── mcp.md
│   ├── cli.md
│   ├── install.md
│   └── contract.json
└── evals/
    ├── evals.json
    └── trigger-evals.json
```

No client-specific `SKILL.md` is a source artifact. `.claude/skills` and `.agents/skills` are installation
destinations created only in isolated tests or user environments; they are not committed package copies.

## 2. `SKILL.md` frontmatter

Only two keys are allowed:

```yaml
---
name: engram
description: >-
  Use engram's local-first, cross-session memory through its MCP tools or CLI.
  Use whenever a user asks engram to remember/记住, recall/召回, search or inspect
  saved facts, list, delete, ingest conversations, run curation, inspect stats,
  export, inspect namespaces, or diagnose the engram version—even when persistent memory is
  described indirectly. Prefer connected engram MCP tools and fall back to the
  engram CLI while preserving namespace isolation, offline behavior, and secret
  safety. Do not use for ordinary RAM, caches, databases, or transient chat context.
---
```

The implementation may tighten wording through trigger evaluation, but it must retain these semantic
elements:

- product identity: `engram`;
- surface identity: `MCP` and `CLI`;
- positive intents: remember/记住, recall/召回, search, get/inspect, list, delete, ingest,
  curation/curate, stats, export, namespace discovery, version diagnosis;
- persistent or cross-session memory, including indirect phrasing;
- MCP-first and CLI fallback;
- namespace, offline and secret boundaries;
- near-miss exclusions for RAM, cache, generic database and transient context.

Validation rules:

- `name` is exactly `engram` and equals the parent directory.
- `name` is 1–64 characters and matches `^[a-z0-9]+(?:-[a-z0-9]+)*$`.
- description is a non-empty scalar of at most 1,024 Unicode code points.
- unknown frontmatter keys fail validation.

## 3. Body contract

The complete `SKILL.md`, including frontmatter, must stay at or below 500 normalized lines and 5,000
estimated tokens under `engram-body-token-estimate-v1`:

1. Normalize CRLF and lone CR to LF.
2. Line count is `0` for an empty file; otherwise it is the LF-split field count minus one when the file
   ends in LF, so a single final newline does not create an extra line.
3. Ignore Unicode whitespace. Let `A` be the remaining ASCII Unicode-code-point count and `U` the
   remaining non-ASCII Unicode-code-point count.
4. The deterministic estimate is `ceil(A / 4) + U`.

This is a portable context-budget estimate, not a claim that it matches a particular model tokenizer.
The body contains these sections:

1. Trigger boundary and non-target near misses.
2. Preflight: detect MCP tools and/or the `engram` executable without changing user state.
3. Resolve exactly one namespace and one data-store context.
4. Select one surface using the MCP-first routing contract.
5. Apply mutation, LLM, offline, namespace and secret safety checks.
6. Execute exactly one intended operation unless the user explicitly requested a sequence.
7. Report surface, namespace, result evidence, degradation/failure and next step.
8. Reference routing: when to read `references/mcp.md`, `references/cli.md` and
   `references/install.md`.

The body must not:

- contain a second full MCP or CLI command manual;
- claim ordinary conversation is automatically persisted;
- invent MCP tools or CLI commands;
- implement retrieval, extraction or curation algorithms;
- tell the agent to probe engine-internal signal failures;
- embed API keys, tokens, passwords, endpoint credentials or real memory content from tests;
- install binaries or modify MCP client configuration.

## 4. Progressive disclosure

- Every operational reference is linked directly from `SKILL.md` using a relative path rooted at the
  skill directory.
- No required instruction may need a reference-to-reference hop.
- References may link to official web documentation for human context, but the executable workflow must
  remain usable offline after installation.
- `references/mcp.md` and `references/cli.md` derive their public-name sets from
  `references/contract.json`.
- `references/install.md` is the only detailed installation/upgrade/discovery source.

## 5. Machine-readable contract

`references/contract.json` has this shape:

```json
{
  "schema_version": 1,
  "skill": {
    "name": "engram",
    "version": "0.1.0"
  },
  "mcp": {
    "always": [
      "memory_delete",
      "memory_get",
      "memory_list",
      "memory_search",
      "memory_write"
    ],
    "conditional": {
      "memory_ingest": "llm"
    }
  },
  "cli": {
    "commands": [
      "add",
      "curate",
      "delete",
      "export",
      "get",
      "ingest",
      "list",
      "namespaces",
      "search",
      "stats",
      "version"
    ]
  },
  "intents": [
    {
      "name": "write",
      "mutating": true,
      "mcp": "memory_write",
      "cli": "add",
      "condition": null
    }
  ]
}
```

The complete `intents` array has exactly these entries:

| Intent | Mutating | MCP | CLI | Condition |
|---|---:|---|---|---|
| `write` | yes | `memory_write` | `add` | explicit user intent |
| `search` | no | `memory_search` | `search` | — |
| `get` | no | `memory_get` | `get` | — |
| `list` | no | `memory_list` | `list` | — |
| `delete` | yes | `memory_delete` | `delete` | explicit user intent |
| `ingest` | yes | `memory_ingest` | `ingest` | explicit intent + LLM |
| `curate` | yes | `null` | `curate` | explicit intent + LLM |
| `stats` | no | `null` | `stats` | CLI store identity confirmed |
| `export` | no | `null` | `export` | CLI store identity confirmed |
| `namespace-discovery` | no | `null` | `namespaces` | CLI data dir confirmed |
| `version` | no | `null` | `version` | — |

Arrays are stored in lexical order except `intents`, whose order is the table above. Unknown commands,
missing required commands, duplicate intents or an MCP value outside the actual server tool set fail.

## 6. Version, license and package identity

- Initial package version is `0.1.0`.
- A behavioral or installation contract change increments the package version before publishing a new
  immutable Git release tag.
- The release tag is derived exactly as `engram-skill-v<skill.version>` and, once published, must never
  move to different package bytes.
- Breaking workflow or manifest changes increment MAJOR and include migration notes in
  `references/install.md`.
- `LICENSE` is a package-local copy of the repository license so a standalone installation retains its
  terms; validation compares its LF-normalized bytes with the repository root license.
- The release record binds the predeclared release tag, `skill.version`, digest algorithm identifier and
  package digest.

### `engram-package-sha256-v1`

Every source, copy, symlink and remote-install comparison uses this exact algorithm:

1. Resolve a symlink at the package root to its directory target. Reject any symlink inside the package.
2. Recursively collect every regular file under the resolved root. Reject an empty package, a non-UTF-8
   file or a file containing NUL.
3. Convert every relative path to `/`-separated UTF-8 and sort paths by their raw UTF-8 byte sequence.
4. Normalize CRLF and lone CR in each file to LF. Do not include directory entries, mtime, mode, owner or
   the package root's absolute path.
5. For every file in order, feed these bytes to one SHA-256 state:
   `relative-path`, NUL, base-10 normalized byte length with no leading zeroes, NUL, normalized file
   bytes, NUL.
6. Emit the lowercase 64-character hexadecimal digest.

The package must not store its own digest inside a hashed file. The validation report records it beside
the immutable release tag and package version.

## 7. Eval resources

`evals/evals.json` follows the skill-creator schema:

```json
{
  "skill_name": "engram",
  "evals": [
    {
      "id": 1,
      "prompt": "Remember that my preferred editor is Helix in the project namespace.",
      "expected_output": "Use one real engram write path and report its evidence.",
      "files": [],
      "expectations": [
        "Uses exactly one supported write surface",
        "Uses one explicit namespace",
        "Reports the real write result"
      ]
    }
  ]
}
```

It covers direct/indirect memory intents, MCP-only, CLI-only, offline, LLM missing, invalid namespace,
secret input, empty result and cross-surface mismatch.

`evals/trigger-evals.json` is an array of at least 20 realistic objects:

```json
{"query": "realistic user prompt", "should_trigger": true}
```

Positive and negative near-miss cases each contribute at least 8 entries. Generic RAM/cache/database and
transient-chat tasks are required negatives.

## 8. Acceptance

The package contract passes only when:

- the local deterministic validator and its negative fixtures pass;
- an auxiliary Agent Skills reference validator accepts the package;
- all relative references exist and remain one hop deep;
- the manifest matches runtime MCP and CLI sets through Go package tests;
- with-skill behavior and trigger evaluation meet the feature success criteria;
- no second canonical body exists in the repository.
