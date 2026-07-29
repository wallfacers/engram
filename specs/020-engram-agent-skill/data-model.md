# Data Model: engram Agent Skill

**Feature**: `020-engram-agent-skill`

**Date**: 2026-07-29

本 feature 不新增数据库表。这里的 data model 描述 Git 跟踪的 skill package、安装结果、
agent 执行上下文和可重复验证记录。

## 1. CanonicalSkillPackage

engram skill 的唯一发布源。

| 字段 | 类型 | 约束 |
|---|---|---|
| `name` | string | 固定为 `engram`；与父目录和 frontmatter 一致 |
| `description` | string | 1–1024 字符；同时包含能力与触发时机 |
| `skill_version` | semver string | 初始 `0.1.0`；记录在 `contract.json` |
| `contract_version` | positive integer | manifest schema 版本；初始 `1` |
| `release_tag` | immutable Git tag | 固定为 `engram-skill-v<skill_version>`；在最终内容冻结前确定，发布后不得移动或复用 |
| `license` | file reference | `LICENSE` 必须随 package 安装 |
| `body` | Markdown | 完整 `SKILL.md` 不超过 500 行，且 `engram-body-token-estimate-v1` 不超过 5,000 |
| `references` | set\<relative path\> | 只允许从 `SKILL.md` 一层到达，目标必须存在 |
| `mcp_tools` | set\<string\> | 与 runtime `tools/list` 契约一致 |
| `cli_commands` | set\<string\> | 与 runtime `knownCommands` 一致 |
| `content_digest` | SHA-256 | 按 `engram-package-sha256-v1` 对完整 package 计算 |

### Validation

- 仓库内 canonical package 数量必须为 1。
- 不允许三端专用 `SKILL.md` 正文副本。
- frontmatter 只允许 `name` 与 `description`。
- manifest 中每个 intent 至少有真实 surface 或明确 unsupported reason。
- release tag、skill version、digest algorithm version 与 digest 在同一发布记录中保持一致。

## 2. InstallationTarget

一个 client、scope 和 package version 的安装目标。

| 字段 | 类型 / 允许值 | 约束 |
|---|---|---|
| `client` | `claude-code \| codex \| opencode` | 正式支持范围仅三者 |
| `scope` | `project \| user` | 命令显式选择；不得隐式扩散到另一 scope |
| `discovery_path` | absolute or project-relative path | 必须匹配 client contract |
| `canonical_path` | path | Codex/OpenCode 为 `.agents/skills/engram`；Claude 可链接它 |
| `install_mode` | `symlink \| copy` | symlink 优先；copy 为显式或自动 fallback |
| `source_ref` | immutable Git tag or local path | 远程发布只接受预声明 release tag |
| `skill_version` | semver string | 与 package manifest 相同 |
| `content_digest` | SHA-256 | 三端对同一 release 必须相同 |
| `existing_kind` | `absent \| managed \| unknown` | `unknown` 覆盖前必须人工确认 |
| `status` | InstallationStatus | 见状态转换 |

### Derived discovery paths

| Scope | Claude Code | Codex + OpenCode |
|---|---|---|
| project | `<repo>/.claude/skills/engram` | `<repo>/.agents/skills/engram` |
| user | `${CLAUDE_CONFIG_DIR:-~/.claude}/skills/engram` | `~/.agents/skills/engram` |

### InstallationStatus

```text
absent
  └─ inspect → ready
existing
  └─ inspect → conflict-reported
conflict-reported
  ├─ cancel → unchanged
  └─ explicit-confirmation → ready
ready
  └─ install → installed-unverified
installed-unverified
  ├─ digest/path failure → recovery-required
  └─ verify → discovered
discovered
  ├─ explicit invocation succeeds → callable
  └─ invocation fails → recovery-required
recovery-required
  └─ rerun same source tag/path + verify all targets → discovered
```

`installed-unverified` 或 `recovery-required` 不能报告为成功。成功组合安装要求三个 target
均为 `callable`，且 version/`engram-package-sha256-v1` digest 相同。

## 3. EngramSurface

agent 当前可使用的现有 adapter。

| 字段 | 类型 / 允许值 | 说明 |
|---|---|---|
| `kind` | `mcp \| cli` | adapter 类型 |
| `availability` | `available \| unavailable \| unknown` | 只依据工具发现或真实 probe |
| `identity` | string | MCP server `engram` 或 CLI executable/version |
| `capabilities` | set\<operation\> | 从 tool list 或 contract manifest 得出 |
| `data_dir_identity` | `known(value) \| unknown` | 两个 surface 不能凭假设判为相同 |
| `namespace_mechanism` | `per-call \| global-flag-or-env` | MCP 每次 input；CLI command 前 flag/env |
| `embedding_state` | `configured \| unconfigured \| unknown` | 只陈述结构上已知状态 |
| `llm_state` | `configured \| unconfigured \| unknown` | 决定 ingest/curate 能力 |

### Capability sets

- MCP always: `write`, `search`, `list`, `get`, `delete`
- MCP conditional: `ingest`
- CLI: `write`, `search`, `list`, `get`, `delete`, `ingest`, `curate`, `stats`,
  `export`, `namespace-discovery`, `version`

## 4. MemoryIntent

从用户请求分类出的一个记忆目标。

| 字段 | 类型 | 说明 |
|---|---|---|
| `operation` | enum | `write`, `search`, `get`, `list`, `delete`, `ingest`, `curate`, `stats`, `export`, `namespace-discovery`, `version` |
| `mutates_state` | boolean | write/delete/ingest/curate 为 true |
| `explicitness` | `explicit \| ambiguous \| absent` | state change 必须为 explicit |
| `target_name` | optional string | write/get/delete 的 entry name |
| `payload` | optional value | content、query 或 conversation messages |
| `required_capability` | operation | surface 必须提供 |
| `preferred_surface` | `mcp \| cli` | 重叠操作优先 MCP；CLI-only 固定 CLI |
| `evidence_requirement` | enum | written/deleted flag、entry、results、exit/output |

### Intent routing

```text
request
  → classify exactly one MemoryIntent
  → reject non-memory near miss or absent state-change intent
  → resolve CapabilityState
  → resolve one NamespaceContext
  → apply secret and mutation safety checks
  → choose one EngramSurface
  → execute once
  → report OperationEvidence
```

## 5. NamespaceContext

一次工作流使用的唯一隔离空间。

| 字段 | 类型 | 约束 |
|---|---|---|
| `id` | string | default 为 `default`; `^[A-Za-z0-9._-]{1,64}$` |
| `source` | `user \| existing-session \| default` | 必须可解释 |
| `surface` | `mcp \| cli` | 绑定单一 surface |
| `data_dir_identity` | `known(value) \| unknown` | 跨 surface 时重新确认 |
| `validated` | boolean | 调用前为 true |

额外拒绝 `.`、`..`、`/`、`\`。一次 workflow 不允许多个 `NamespaceContext`，除非用户明确
发起新的、独立的第二次操作；skill 不聚合跨 namespace 结果。

## 6. CapabilityState

agent 在执行前能诚实确定的能力快照。

| 字段 | 类型 | 规则 |
|---|---|---|
| `mcp_connected` | boolean | 以当前工具列表为准 |
| `mcp_tools` | set\<string\> | 不从文案猜测 |
| `cli_available` | boolean | 以 executable probe 为准 |
| `cli_version` | optional string | `engram version` 的真实输出 |
| `embedding` | `configured \| unconfigured \| unknown` | 未知时不宣称 semantic 可用或不可用 |
| `llm` | `configured \| unconfigured \| unknown` | ingest/curate 执行前必须确认可用 |
| `selected_surface` | optional EngramSurface | 每次 operation 至多一个 |
| `block_reason` | optional string | 缺失依赖时给准确下一步 |

### Selection rules

1. intent 在 MCP tool set 中：选择 MCP。
2. MCP 不提供该 intent 且 CLI 提供：选择 CLI；若从 MCP 上下文切换，先确认 data dir 与
   namespace。
3. 两者都不可用：block，不模拟执行。
4. conditional capability 不可用：block，不回退成语义不同的操作。

## 7. OperationEvidence

agent 向用户报告的真实执行证据。

| 字段 | 类型 | 说明 |
|---|---|---|
| `surface` | `mcp \| cli` | 实际使用的 adapter |
| `namespace` | string or `n/a` | version 等无 namespace intent 可为 n/a |
| `operation` | MemoryIntent.operation | 实际执行目标 |
| `status` | `success \| empty \| not-found \| degraded \| blocked \| failed` | 保留 adapter 语义 |
| `evidence` | structured result or concise excerpt | 不得补造 |
| `degradation` | optional string | 仅结构上可知事实 |
| `next_step` | optional string | 失败、缺能力或恢复时提供 |

`empty`、`not-found` 和 `deleted:false` 不是虚构成功；`curate completed` 只表示 pass 完成，
不推断一定发生 merge/evict。

## 8. CompatibilityVerificationCase

格式、安装、发现、触发或工作流的一条可重复验收记录。

| 字段 | 类型 | 约束 |
|---|---|---|
| `id` | stable string/integer | 在集合内唯一 |
| `kind` | `format \| install \| discovery \| trigger \| workflow \| safety \| contract` | 决定 runner |
| `client` | optional supported client | discovery/install case 必填 |
| `scope` | optional scope | install case 必填 |
| `source_ref` | optional immutable Git tag | remote install case 必填 |
| `prompt` | optional string | trigger/workflow case 必填 |
| `environment` | map | MCP-only、CLI-only、offline、LLM missing 等 |
| `execution_cost_class` | `local \| existing-flat-rate \| metered \| unknown` | agent inference case 必填；仅前两者允许执行 |
| `incremental_model_cost` | decimal | 已执行 agent inference case 必须为 `0` |
| `expected_surface` | optional surface | workflow case |
| `expected_operation` | optional operation | workflow case |
| `expectations` | list\<string\> | 客观可验证且有人读名称 |
| `review_disposition` | optional enum | behavior review 可为 `approved-no-comments \| changes-requested \| approved-after-changes`；release 只接受两个 approved 状态 |
| `actual_evidence` | optional object | run 后填充 |
| `result` | `not-run \| pass \| fail \| blocked` | fail 必须保留证据 |

### Required coverage

- 三个 client × project/user 的安装与发现；
- 三个 client 的显式调用；
- 一次三端组合安装与一次同版本重跑；
- 已有同名 target 的取消与明确替换；
- MCP-only、CLI-only、offline、LLM missing；
- 非法 namespace、secret input、empty/not found；
- 至少 20 个 trigger queries，正负近邻各 8–10 个以上；
- behavior/trigger 与真实 client case 的零增量费用证据；
- behavior review 的显式维护者 disposition；
- `contract.json` 对真实 MCP/CLI surface 的完整对照。

## Relationships

```text
CanonicalSkillPackage 1 ─────< InstallationTarget
CanonicalSkillPackage 1 ─────< CompatibilityVerificationCase
CanonicalSkillPackage 1 ───── 1 contract.json

MemoryIntent 1 ───── 1 CapabilityState
CapabilityState 1 ───── 0..1 EngramSurface
MemoryIntent 1 ───── 1 NamespaceContext
MemoryIntent 1 ───── 0..1 OperationEvidence

CompatibilityVerificationCase * ───── 0..1 InstallationTarget
CompatibilityVerificationCase * ───── 0..1 MemoryIntent
```
