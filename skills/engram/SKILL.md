---
name: engram
description: >-
  Use engram's local-first persistent and cross-session memory through MCP tools or
  the CLI whenever a user asks to remember/记住, recall/召回, search, inspect or get
  saved facts, list, delete, ingest conversations, curate, inspect stats, export,
  inspect namespaces, or diagnose the version. Preserve namespace isolation, offline
  behavior, and secret safety; do not use for ordinary RAM, cache, generic database,
  or transient chat context.
---

# engram memory workflow

Use this skill only for a request about durable engram memory. Do not activate it
for ordinary RAM, caches, generic databases, transient chat context, or a request
to merely summarize a conversation. Activation never makes ordinary conversation
persist automatically.

## 1. Preflight without changing state

1. Classify one intent: `write`, `search`, `get`, `list`, `delete`, `ingest`,
   `curate`, `stats`, `export`, `namespace-discovery`, or `version`.
2. Inspect the connected tool list for real engram MCP names; never infer a
   connection from prose or configuration.
3. If the needed MCP tool is absent, probe `engram version` to determine whether
   the CLI is executable. Do not use `engram --help` as a command reference.
4. If neither surface provides the intent, report `blocked`: the skill may be
   installed while MCP and CLI tooling still need setup. Do not simulate a call.

## 2. Select one namespace and one surface

Use the user-provided namespace, an already-established session namespace, or
`default`. Before a call, reject `.`, `..`, any `/` or `\\`, and every id that
does not match `^[A-Za-z0-9._-]{1,64}$`. Report the selected namespace even when
the adapter defaulted it.

For overlapping intents, choose the connected MCP tool first. If it is absent,
use the semantically equivalent CLI command. `curate`, `stats`, `export`,
`namespace-discovery`, and `version` are CLI-only. When a CLI-only operation is
reached from MCP, confirm the CLI data directory and namespace first: identical
names do not prove the two surfaces point at the same store.

Never double-write, silently merge, or use a success from one surface as proof
that the other changed. Execute one operation once unless the user explicitly
asked for a sequence such as write then search.

## 3. Check mutation, model, and safety boundaries

`write`, `delete`, `ingest`, and `curate` require explicit user intent. If their
target, namespace, or destructive scope is ambiguous, ask before calling. A clear
"remember this" request is explicit write intent; generic conversation is not.

`ingest` and `curate` also require a configured LLM. For MCP ingest, the exact
`memory_ingest` tool must appear in the live tool list. Missing LLM capability is
`blocked`, not a successful extraction or curation. Do not enable background
curation or invent a curation MCP tool.

Stop before writing, ingesting, or exporting likely API keys, tokens, passwords,
private keys, or similar secrets. Do not repeat them in commands or evidence;
explain that provider credentials use the existing environment-only configuration
channels and offer to store a non-secret description instead.

Respect adapter content limits: an entry is at most 1,200 Unicode code points and
a retrieval trigger is at most 120 code points on one line. Preserve a
content-rejected response and ask the user to shorten or split non-secret content;
never silently truncate it.

Base write, search, get, list, and delete work locally without hosted services.
Missing embedding can degrade search to available signals; report only the
returned structural degradation and never probe engine-internal failures. Never
recommend a hosted reranker or recall model as a prerequisite or scoring lever.

## 4. Route the intent

| Intent | Preferred surface | Rule |
|---|---|---|
| write, search, get, list, delete | MCP, then CLI | one namespace; preserve actual response |
| ingest | live `memory_ingest`, then CLI | explicit intent, LLM required, only user/assistant turns |
| curate | CLI | explicit intent, LLM and confirmed CLI store |
| stats, export, namespace discovery | CLI | confirm required data directory; export no secrets |
| version | CLI | `engram version` needs no data directory |

Read [the MCP reference](references/mcp.md) before an MCP call and [the CLI reference](references/cli.md)
before a CLI command. Read [the machine contract](references/contract.json) when validating names or
intent mappings, and [the installation reference](references/install.md) only for setup, discovery, or upgrade.

## 5. Report operation evidence

For every completed, empty, blocked, degraded, not-found, or failed request,
respond concisely in this shape:

```text
surface: mcp | cli
namespace: <id> | n/a
operation: <intent>
status: success | empty | not-found | degraded | blocked | failed
evidence: <actual tool result or concise CLI output>
next step: <only when needed>
```

Keep `deleted:false`, empty search/list, get-not-found, zero extracted facts,
nonzero CLI exits, and adapter errors distinct from success. A completed curation
pass does not by itself prove an entry was merged or evicted.
