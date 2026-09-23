# Installation Discovery Receipt (T060)

**Date**: 2026-09-03
**Method**: read-only local probes (`--version`, directory enumeration, `readlink`/`diff`); no skill was installed or modified, no agent session was started.
**Sanitization**: paths and versions only. One probed config file carries a live API key in plaintext (`~/.config/opencode/opencode.jsonc`); its contents are deliberately NOT quoted here, and the receipt records only that the file exists and which top-level keys it has. The maintainer may want to move that key to an env-backed secret.

## Probed versions

| Tool | Version |
|---|---|
| claude | 2.1.259 (Claude Code) |
| codex | codex-cli 0.153.0 |
| opencode2 | v0.0.0-beta-18743 |

## Discovery behavior observed

### Claude Code — skills directory + symlink (supported)

- Discovery root: `~/.claude/skills/<skill-name>/SKILL.md` (user scope).
- `~/.claude/skills/` is populated almost entirely with **symlinks into a shared canonical tree**, `~/.agents/skills/<name>` — the maintainer's existing convention for one-copy skills.
- engram is installed exactly this way: `~/.claude/skills/engram -> /home/wushengzhou/.agents/skills/engram`, and the canonical `SKILL.md` is currently **byte-identical** to the repo's `skills/engram/SKILL.md`.
- Conclusion: Claude discovery supports the canonical + symlink layout natively. This is the layout the skill's install guidance should keep (T061).

### Codex CLI — skills directory, private-copy convention in practice (supported)

- Discovery root: `~/.codex/skills/<skill-name>/` (exists; also a `.system/` skills subtree managed by codex itself).
- All four currently installed skills (`find-skills`, `playwright-best-practices`, `playwright-cli`, `ui-ux-pro-max`) are **real directories**, not symlinks — private copies are the observed convention on this machine, but nothing probed indicates symlinks are rejected.
- engram is **not installed** for codex today.
- Conclusion: codex discovers `~/.codex/skills/`; install guidance must add engram there **without a second byte-copy of the skill** (symlink preferred; recorded honestly if codex proves to require a real dir, which is T063's execution evidence to settle).

### OpenCode — no native skill mechanism found (reported, not guessed)

- No skills directory under `~/.config/opencode/` (`skills/` absent) and none under `~/.opencode/`.
- `opencode2 --help` advertises no skill subcommand or flag.
- The probed config (`opencode.jsonc`) carries provider/model configuration only (top-level `provider` key observed; contents not quoted — see sanitization note); `AGENTS.md` files are its instruction-injection convention, not a skill loader.
- Conclusion: as of v0.0.0-beta-18743, OpenCode shows **no native agent-skills discovery**. Per the honest-reporting rule, the install guidance (T061) must state this rather than invent a path, and T063's three-tool smoke should record OpenCode's outcome from its actual mechanism (AGENTS.md injection or none) rather than assume parity.

## Cross-tool state against the one-skill goal

| Check | Result |
|---|---|
| Canonical copy exists | `~/.agents/skills/engram/` (SKILL.md + references + evals) |
| Repo ↔ canonical drift | none at probe time (SKILL.md byte-identical) |
| Claude sees exactly one engram | yes, via symlink |
| Codex sees engram | **no** (not installed) |
| OpenCode sees engram | **no** (no native mechanism) |
| Private duplicate copies found | none (`find` over the four roots matched exactly one SKILL.md) |

## Feed-forward

- T059's assertions should encode: canonical-dir presence, claude symlink resolution, no second `SKILL.md` copy under any tool root, three-tool execution records, ≥1 implicit-write smoke pass.
- T061 must instruct: claude = symlink (already correct); codex = add `~/.codex/skills/engram` without duplicating bytes; opencode = documented as unsupported, with the AGENTS.md alternative stated as an explicit non-skill fallback, never presented as discovery.
- T063 must bind the observed package digest and set `scoring_equivalent=false` if the mutable skill has diverged from the evaluated snapshot by then.
