# engram CLI reference

Use this reference only after `engram version` confirms that the executable is
available. Do not rely on `engram --help`: it is not the command reference.
The exact public set is in [`contract.json`](contract.json).

## Global flags and safety

Place global flags before the command:

```text
--data-dir
--namespace
--embed-base-url
--embed-model
--llm-base-url
--llm-model
--llm-provider
```

API keys are environment-only (`ENGRAM_EMBED_API_KEY` and
`ENGRAM_LLM_API_KEY`); never put them in a generated command, memory entry,
example, or log. `version` is the one command that needs neither a data
directory nor a namespace. All other store commands need a known data directory;
when switching from MCP for a CLI-only intent, first confirm that directory and
namespace rather than assuming it is the same store.

## Commands

| Intent | Command | Evidence and boundary |
|---|---|---|
| write/update | `engram [global flags] add --name <name> --content <text> [--trigger <text>] [--category <value>] [--pinned]` | `# added` and the added name; explicit intent only |
| search | `engram [global flags] search "<query>" [--limit <n>]` | rendered hits or an empty result; limit must be positive |
| exact get | `engram [global flags] get <name>` | rendered entry or not-found exit code |
| list | `engram [global flags] list` | rendered entries, possibly empty |
| delete | `engram [global flags] delete <name>` | `# deleted` only for an actual deletion |
| ingest | role-tagged stdin into `engram [global flags] ingest` | explicit request plus LLM; `# ingested` and extracted count |
| curate | `engram [global flags] curate` | CLI-only, explicit request plus LLM; `completed` is not proof of a merge/eviction |
| stats | `engram [global flags] stats` | CLI-only; data-store identity confirmed |
| export | `engram [global flags] export` | CLI-only; do not export likely secrets |
| namespace discovery | `engram --data-dir <dir> namespaces` | CLI-only; explicit data directory |
| version | `engram version` | no data directory required |

For ingest, provide one turn per line, using only `user:` or `assistant:`:

```text
user: I prefer jasmine tea.
assistant: I will remember it when explicitly asked.
```

## Exit and error semantics

The CLI returns `0` for success, `1` for engine failures, `2` for invalid usage,
`3` for not found, `4` for a missing capability such as LLM ingest/curate,
`5` for an invalid namespace, and `6` for rejected content or trigger budgets.
Keep a nonzero exit status and its diagnostic as a failure or block. Do not turn
an empty search, not-found get/delete, or missing LLM into a fabricated result.

The entry-content budget is 1,200 Unicode code points and the optional trigger
budget is 120 code points on one line. Preserve an exit-6 rejection and ask the
user to shorten or split non-secret content; do not silently truncate it.

Without embedding, CRUD and keyword retrieval remain local and usable; rendered
search output reports the known semantic degradation. Never recommend a paid
cloud reranker or recall model as a requirement. Reject likely credentials before
write, ingest, or export, and keep secrets in the existing environment-only
configuration path.
