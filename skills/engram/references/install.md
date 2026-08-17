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

## Quick install: user scope, shared universal directory

Before confirming, review the possible replacement targets. The default command
keeps **one shared copy** in the standards-based universal skills directory
`~/.agents/skills/engram`; every other client — including Claude Code, which
only reads its own `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/` — gets a symlink
to that single copy. Prefer this shape: a shared universal directory avoids
installing the package multiple times, and per-client `--agent` scoping is only
for explicitly targeting one client's own directory (see below). The default
command deliberately keeps the installer's write confirmation; `npx --yes`
authorizes fetching the pinned installer, not a silent target overwrite.

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram --global
```

Choose `Symlink` when the filesystem permits it, or `Copy` on Windows and
restricted filesystems. A symlink failure may fall back to an equivalent copy.
The release tag is derived from `references/contract.json` as
`engram-skill-v<skill.version>` before package content is frozen; the literal
tag above is `engram-skill-v0.1.0`.

## Other scopes and targets

For the current project, omit `--global`. The shared copy lands in
`<repo>/.agents/skills/engram` with per-client symlinks (for example
`<repo>/.claude/skills/engram`); review them before confirming:

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/engram-skill-v0.1.0/skills/engram
```

Add exactly one of `--agent claude-code`, `--agent codex`, or `--agent opencode`
only when you explicitly want to scope the install to that client's own
directories instead of the shared universal directory. This is not needed for
discovery: Codex and OpenCode read `.agents/skills/` natively and Claude Code
follows its symlink. Add `--global` for a user installation and omit it for a
project installation. Add `--copy` to choose copying up front while retaining
the final target confirmation.

An advanced automation path may append `--yes` or `-y` only after you have
explicitly decided to replace every listed target. Do not use it when a target
may be user-maintained or of unknown provenance.

## Discovery and invocation

| Client | Project path | User path | Explicit use | Reload |
|---|---|---|---|---|
| Claude Code | `.claude/skills/engram` | `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram` | `/engram` | Restart if the top-level skills directory was newly created; otherwise invoke it again or open a new session. |
| Codex | `.agents/skills/engram` | `~/.agents/skills/engram` (shared; `${CODEX_HOME:-~/.codex}/skills/` is also scanned) | `$engram` or choose it from `/skills` | It normally auto-discovers; restart if absent. |
| OpenCode | `.agents/skills/engram` | `~/.agents/skills/engram` (shared; `${XDG_CONFIG_HOME:-~/.config}/opencode/skills/` is also scanned) | Ask it to load and use the `engram` skill | Restart and open a new session. |

At both scopes the package exists once, in `.agents/skills/engram`
(`~/.agents/skills/engram` at user scope); Claude Code reads
`.claude/skills/engram`, which is a symlink into that shared copy, and
Codex/OpenCode scan the shared directory natively (in addition to their own
`~/.codex/skills/` and `~/.config/opencode/skills/`). The installed package is
discovered as-is — never copy it into additional client directories by hand.
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
project: <repo>/.agents/skills/engram (single shared copy), plus <repo>/.claude/skills/engram symlink for Claude Code
user:    ~/.agents/skills/engram (single shared copy), plus ~/.claude/skills/engram symlink for Claude Code
```

When `skills@1.5.20` is already cached, a local package can also be installed
without network access:

```bash
npx --offline skills@1.5.20 add ./skills/engram --global
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
