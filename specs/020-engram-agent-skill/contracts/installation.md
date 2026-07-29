# Contract: Installation, Discovery and Upgrade

**Feature**: `020-engram-agent-skill`

**Installer baseline**: `skills@1.5.20`

**Installer source baseline**:
[`e173b8c88f2581cfdaa1b6767c6519a08155790e`](https://github.com/vercel-labs/skills/tree/e173b8c88f2581cfdaa1b6767c6519a08155790e)

## 1. Preconditions and non-goals

Remote command installation requires:

- Node.js `>=22.20.0`;
- `npx`/npm and Git;
- network access to npm and the engram Git repository;
- a predeclared immutable engram release tag containing the exact final `skills/engram/` package.

Installation copies or links only the canonical skill package. It does not install `engram`,
`engram-mcp`, a model, or a provider; it does not edit MCP client configuration, `PATH`, shell startup
files or tracked secret files.

## 2. Published commands

`<ENGRAM_SKILL_TAG>` is a specification-only placeholder. The release sequence is normative:

1. Derive the unused release tag as `engram-skill-v<skill.version>` before final package content is
   frozen; the initial value is `engram-skill-v0.1.0`.
2. Replace the placeholder in the package, README and docs with that literal tag.
3. Run source/release validation and create the exact candidate commit; record its package version and
   `engram-package-sha256-v1` digest.
4. Only with explicit maintainer authorization, create and publish the predeclared tag at that candidate
   commit.
5. Verify the published tag resolves to the candidate commit and produces the recorded digest before
   running remote installation smoke.

Shipped instructions may not contain the placeholder, a mutable branch or a commit-SHA self-reference.
The commit SHA may be recorded externally as evidence, but writing it back into the hashed package would
change the commit and is therefore invalid. A published release tag is immutable: it must never be moved
to a different commit or reused for different package bytes.

### All three clients, user scope

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/<ENGRAM_SKILL_TAG>/skills/engram --global --agent claude-code --agent codex --agent opencode
```

This is the canonical quick command. Immediately above it, the user guide must state that an existing
`engram` at `~/.agents/skills/engram` or
`${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram` may be replaced. `npx --yes` only authorizes fetching
the pinned installer package; there is deliberately no trailing `skills --yes`. The installer must
obtain confirmation before writing. When asked for installation method, select:

- `Symlink` for one canonical installed copy and easy synchronized updates;
- `Copy` on Windows, restricted filesystems or environments that disallow symlinks.

### All three clients, current project

Immediately above this command, warn that `<repo>/.agents/skills/engram` and
`<repo>/.claude/skills/engram` are the possible replacement targets.

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/<ENGRAM_SKILL_TAG>/skills/engram --agent claude-code --agent codex --agent opencode
```

### One client

Keep exactly one of:

```text
--agent claude-code
--agent codex
--agent opencode
```

Add `--global` for user scope; omit it for project scope.

### Explicit copy

Append `--copy`. This removes the installation-method prompt but preserves the final target/overwrite
confirmation.

### Explicit managed replacement

Appending the installer `--yes`/`-y` is an advanced option that means the user explicitly authorizes
replacement of every listed `engram` target:

```bash
npx --yes skills@1.5.20 add https://github.com/wallfacers/engram/tree/<ENGRAM_SKILL_TAG>/skills/engram --global --agent claude-code --agent codex --agent opencode --yes
```

It is not the default quick command and must not be suggested when target provenance or local
modification is unknown.

## 3. Discovery paths

### Symlink mode

| Scope | Canonical package | Claude Code view | Codex view | OpenCode view |
|---|---|---|---|---|
| project | `<repo>/.agents/skills/engram` | `<repo>/.claude/skills/engram` symlink | canonical | canonical |
| user | `~/.agents/skills/engram` | `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram` symlink | canonical | canonical |

Codex and OpenCode are universal `.agents/skills` consumers in the pinned installer. Do not create
additional `.codex/skills` or `.opencode/skills` copies.

### Copy mode

The installer writes equivalent package contents to Claude's path and `.agents/skills/engram`;
Codex and OpenCode share the latter. All completed copies must have the same `contract.json` version and
`engram-package-sha256-v1` digest.

If symlink creation fails, the installer may fall back to copy. A successful result must name the actual
path; tests verify discovery rather than assuming the requested mode.

## 4. Invocation and reload

| Client | Explicit invocation | Reload rule |
|---|---|---|
| Claude Code | `/engram` | Restart when a new top-level skills directory was created; after an update invoke again or start a new session |
| Codex | `$engram` or choose `engram` from `/skills` | Changes should auto-discover; restart if absent |
| OpenCode | Ask to use the `engram` skill; verify native `skill` loading | Restart OpenCode and open a new session |

Installation success and tool readiness are distinct:

- skill discovered but no MCP/CLI: report that engram tooling still needs setup;
- MCP connected: use the MCP workflow;
- MCP absent and CLI present: use the CLI fallback;
- neither present: link the existing MCP/CLI guides without fabricating a call.

## 5. Collision and idempotency

Before a write:

1. Resolve the exact target paths for the chosen scope.
2. The guide unconditionally lists those paths and warns that same-name content may be replaced.
3. Treat the installer's `overwrites` summary as supplemental: version 1.5.20 can miss the universal
   global `.agents/skills` path while checking Codex/OpenCode's registry paths.
4. If any target is unexpected or user-maintained, cancel at the generic confirmation and leave it
   unchanged.
5. Let the user back it up, rename it, keep it, or explicitly replace it.
6. Write only after the generic installation confirmation.

After a completed same-version reinstall:

- each client discovers exactly one `engram`;
- all discovered packages report the same version and digest;
- files removed from the source release are not left behind;
- no client-specific old body remains.

The upstream implementation deletes and recreates a destination; “idempotent” describes the verified
final state, not an atomic no-op.

## 6. Upgrade and recovery

Canonical upgrade procedure:

1. Predeclare the new immutable release tag and replace the old literal tag before freezing the candidate.
2. Run the full `add` command again with the same explicit clients, scope and intended mode.
3. Review the overwrite summary.
4. Confirm.
5. Restart/reload each client.
6. Verify version, digest, discovery count and explicit invocation.

Do not use `skills update` as the documented path because it may not preserve the original agent list or
copy/symlink decision.

If interrupted:

- do not treat an installer exit, partial path or one successful client as overall success;
- rerun the exact same tag and full target command;
- verify all three targets before use;
- if recovery still fails, restore the user's backup or remove only the exact incomplete target after
  explicit user approval.

## 7. Manual and offline installation

Use the already obtained canonical `skills/engram/` directory. Never reconstruct files from snippets.

### Project scope

```text
<repo>/.agents/skills/engram       ← copy canonical package
<repo>/.claude/skills/engram       ← symlink to the above, or copy the same package
```

### User scope

```text
~/.agents/skills/engram            ← copy canonical package
~/.claude/skills/engram            ← symlink to the above, or copy the same package
```

When Node is available and `skills@1.5.20` is already cached, a local-source install may use:

```bash
npx --offline skills@1.5.20 add ./skills/engram --global --agent claude-code --agent codex --agent opencode
```

First-time remote installation is not offline. Normal skill use after either installation method has no
Node/npm/network dependency.

## 8. Installation smoke contract

Every automated case uses a session scratch directory and isolates:

```text
HOME
XDG_CONFIG_HOME
XDG_STATE_HOME
XDG_CACHE_HOME
TMPDIR
NPM_CONFIG_CACHE
NPM_CONFIG_USERCONFIG
CLAUDE_CONFIG_DIR
CODEX_HOME
GH_CONFIG_DIR
```

It also sets `DISABLE_TELEMETRY=1`, `DO_NOT_TRACK=1` and `CI=1`, uses a scratch current working
directory, and installs from the local package. After each case it asserts:

- only expected scratch paths changed;
- repository and real home status did not change;
- `SKILL.md`, references, evals and license arrived;
- target version and `engram-package-sha256-v1` digest are exact;
- the expected client discovery path exists exactly once;
- MCP configs and executable paths are unchanged.

Required cases:

- each client alone in project scope and user scope;
- three clients together in both scopes;
- symlink and `--copy`;
- same version twice;
- existing unknown content then cancel;
- explicit managed replacement;
- simulated interrupted target then recovery;
- predeclared release-tag remote install after the tag-to-candidate/digest check.
