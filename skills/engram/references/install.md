# Install the engram skill

This is the single detailed installation, upgrade, discovery, and recovery
reference. It supports Claude Code, Codex, and OpenCode. Other Agent Skills
consumers may use the open-format package themselves, but are not part of this
support or verification matrix.

## What installation does (and does not) do

The command installs only this skill package. It does not install the `engram`
CLI or `engram-mcp`, configure an MCP client, alter `PATH`, install a model, or
write credentials. Install and configure those dependencies separately through
the repository's existing CLI and MCP guides.

Remote installation needs Node.js `>=22.20.0`, `npx`/npm, Git, and network
access. It pins the downloader to `skills@1.5.20`. Normal use after installation
does not require Node, npm, or network access.

## Quick install: all three clients, user scope

Before confirming, review the possible replacement targets. With `--global` and
all three agents selected, the installer writes one target per client:
`${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram`,
`${CODEX_HOME:-~/.codex}/skills/engram`, and
`${XDG_CONFIG_HOME:-~/.config}/opencode/skills/engram`. The default command
deliberately keeps the installer's write confirmation; `npx --yes` authorizes
fetching the pinned installer, not a silent target overwrite.

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram --global --agent claude-code --agent codex --agent opencode
```

Choose `Symlink` when the filesystem permits it, or `Copy` on Windows and
restricted filesystems. A symlink failure may fall back to an equivalent copy.
The release tag is derived from `references/contract.json` as
`engram-skill-v<skill.version>` before package content is frozen; the literal
tag above is `engram-skill-v0.1.0`.

## Other scopes and targets

For the current project, omit `--global`. Review
`<repo>/.claude/skills/engram` and `<repo>/.agents/skills/engram` before
confirming:

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram --agent claude-code --agent codex --agent opencode
```

For one client, keep exactly one of `--agent claude-code`, `--agent codex`, or
`--agent opencode`. Add `--global` for a user installation and omit it for a
project installation. Add `--copy` to choose copying up front while retaining
the final target confirmation.

An advanced automation path may append `--yes` or `-y` only after you have
explicitly decided to replace every listed target. Do not use it when a target
may be user-maintained or of unknown provenance.

## Discovery and invocation

| Client | Project path | User path | Explicit use | Reload |
|---|---|---|---|---|
| Claude Code | `.claude/skills/engram` | `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram` | `/engram` | Restart if the top-level skills directory was newly created; otherwise invoke it again or open a new session. |
| Codex | `.agents/skills/engram` | `${CODEX_HOME:-~/.codex}/skills/engram` | `$engram` or choose it from `/skills` | It normally auto-discovers; restart if absent. |
| OpenCode | `.agents/skills/engram` | `${XDG_CONFIG_HOME:-~/.config}/opencode/skills/engram` | Ask it to load and use the `engram` skill | Restart and open a new session. |

At project scope Codex and OpenCode share `.agents/skills/engram`, while Claude
Code reads `.claude/skills/engram`. At user scope, `skills@1.5.20` writes Claude
Code to `~/.claude/skills/engram` and both Codex and OpenCode to
`~/.agents/skills/engram`. Codex and OpenCode scan `~/.agents/skills/` (in
addition to their own `~/.codex/skills/` and `~/.config/opencode/skills/`), so
the installed package is discovered as-is — no post-install copy is needed.
A discovered skill is not proof that MCP or CLI tooling is configured. If
neither is available, it must report that condition rather than pretending an
operation ran.

## Upgrade, collisions, and recovery

For an upgrade, use the full explicit command again with the new immutable tag,
selected clients, scope, and intended mode. Review the overwrite summary,
confirm, reload each client, then verify one discovered `engram` package per
client with the expected version and `engram-package-sha256-v1` digest.

If a same-name target is unexpected, cancel at the confirmation, then back it
up, rename it, retain it, or explicitly replace it. The installer can delete
and recreate a destination, so successful same-version reinstallation means a
verified final state—not an atomic no-op.

If installation is interrupted, do not report partial state as success. Rerun
the exact same immutable tag and full target command, then verify every target.
Restore a user backup or remove an incomplete exact target only with the user's
approval if recovery still fails.

## Manual and offline fallback

Use an already obtained copy of this canonical `skills/engram/` directory; do
not reconstruct files from snippets. Copy or symlink it into each target
client's discovery path for the chosen scope:

```text
project: <repo>/.claude/skills/engram, and <repo>/.agents/skills/engram (shared by Codex and OpenCode)
user:    ~/.claude/skills/engram, ~/.codex/skills/engram, and ~/.config/opencode/skills/engram
```

When `skills@1.5.20` is already cached, a local package can also be installed
without network access:

```bash
npx --offline skills@1.5.20 add ./skills/engram --global --agent claude-code --agent codex --agent opencode
```

First-time remote installation is not offline. Regardless of installation
method, all copies must retain the package's exact version and digest.

## Release provenance

The release tag is derived from `references/contract.json` as
`engram-skill-v<skill.version>` before package content is frozen. The literal
tag is written to this reference and the user-facing docs, source/release
validation runs, and a candidate commit is formed. Only an explicitly
authorized maintainer may then publish the predeclared tag at that exact commit.
Remote smoke verifies tag → candidate commit → `engram-package-sha256-v1`
digest before installation. A commit SHA may be release evidence but is never
written back into this hashed package, which would create a self-reference.
