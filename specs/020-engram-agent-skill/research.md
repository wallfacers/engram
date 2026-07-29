# Phase 0 Research: engram Agent Skill

**Feature**: `020-engram-agent-skill`

**Date**: 2026-07-29

**Scope**: 一个 canonical Agent Skill、三端安装与发现、现有 MCP/CLI 工作流编排及发布验证

本文件记录实现前已经收口的技术决策。结论来自开放规范、三个目标客户端的官方文档、
`skills@1.5.20` 固定源码以及 engram 当前运行时代码和测试；不存在未决澄清项。

## R-001：使用开放格式和单一发布源

**Decision**

canonical package 固定在 `skills/engram/`，入口为 `skills/engram/SKILL.md`。YAML
frontmatter 的可移植交集只包含：

```yaml
---
name: engram
description: <what the skill does and when it should trigger>
---
```

`name` 必须与目录名一致，满足 1–64 字符的小写字母、数字和单连字符规则；description
必须同时表达能力与触发时机，并保持在 1–1024 字符。完整 `SKILL.md` 不超过 500 行，并按
`engram-body-token-estimate-v1` 保持在 5,000 estimated tokens 以内。license、skill version
和运行时契约放在包内文件，不依赖客户端专用 frontmatter。

**Rationale**

[Agent Skills specification](https://agentskills.io/specification) 把 `SKILL.md`、name 和
description 定义为最低公共契约。Claude Code、Codex 和 OpenCode 都能消费该结构，但对
`allowed-tools`、hooks、model 等扩展字段并无稳定交集。只保留两项公共字段可避免一个客户端
的配置改变另一个客户端的语义。

**Alternatives considered**

- 在 `.claude/skills`、`.agents/skills` 和 `.opencode/skills` 各提交一份：会产生正文漂移。
- 把 skill 放在仓库根：能被发现，但不利于仓库以后容纳其他 skill。
- 使用客户端专用 frontmatter：超出三端稳定交集，且不是当前工作流所需。

## R-002：冻结三端发现与显式调用契约

**Decision**

| 客户端 | 项目作用域 | 个人作用域 | 显式调用 | 安装后处理 |
|---|---|---|---|---|
| Claude Code | `.claude/skills/engram` | `~/.claude/skills/engram` | `/engram` | 新建顶层 skills 目录时重启；更新后重新调用或开新会话 |
| Codex | 从 CWD 到 repo root 的 `.agents/skills/engram` | `$HOME/.agents/skills/engram` | `$engram` 或 `/skills` | 正常自动发现；未出现时重启 |
| OpenCode | 兼容 `.agents/skills/engram` | 兼容 `~/.agents/skills/engram` | 明确要求使用 `engram` skill；agent 加载 `skill({name:"engram"})` | 重启并新建会话 |

手动安装和通用安装器均利用 `.agents/skills` 同时服务 Codex 与 OpenCode；不额外双写
`.codex/skills`、`.opencode/skills` 或 `~/.config/opencode/skills`。

**Rationale**

[Claude Code skills](https://code.claude.com/docs/en/skills)、
[Codex skills](https://learn.chatgpt.com/docs/build-skills) 和
[OpenCode skills](https://opencode.ai/docs/skills) 均支持上述路径。共享 `.agents/skills`
既是 Codex 当前官方路径，也是 OpenCode 明示的兼容路径，可减少重复发现与版本漂移。
OpenCode stable 文档没有承诺 `/engram`，因此不把 V2 的 `/id` 行为写成稳定契约。

**Alternatives considered**

- 同时写入每个客户端的所有兼容目录：可能使同一客户端发现两个同名 skill。
- 把 OpenCode V2 调用语法当 stable：上游仍在演进，不适合作为发布门。

## R-003：采用固定版本的通用安装器

**Decision**

主安装后端采用 `skills@1.5.20`，固定其 npm 版本；远程安装需要 Node.js `>=22.20.0`、
`npx`、Git 和网络。发布入口只使用包含 `skills/engram` 的预声明 immutable Git release
tag URL。tag 名称固定由 package version 派生为 `engram-skill-v<skill-version>`，初始值为
`engram-skill-v0.1.0`；在最终 package 内容冻结前写入包内安装正本和所有用户入口。最终内容
形成候选提交后，维护者才把该 tag 创建在该精确提交上，且发布后不得移动或复用。三端、个人
作用域的发布命令形态为：

```bash
npx --yes skills@1.5.20 add \
  https://github.com/wallfacers/engram/tree/<ENGRAM_SKILL_TAG>/skills/engram \
  -g -a claude-code -a codex -a opencode
```

项目作用域只删除 `-g`；单客户端只保留对应 `-a`。发布文档必须把
`<ENGRAM_SKILL_TAG>` 替换为预声明 tag literal；最终候选中不得保留占位符。维护者发布 tag
后必须验证它指向候选提交且 package digest 相同。commit SHA 仍可作为外部审计证据，但不得
写入会改变同一 package 的自引用安装命令。`wallfacers/engram@engram` 可作为 latest 简写，
但不作为可复现安装证明。

**Rationale**

固定源码
[`vercel-labs/skills@e173b8c`](https://github.com/vercel-labs/skills/tree/e173b8c88f2581cfdaa1b6767c6519a08155790e)
的 `add` 命令原生支持单个 skill、三个显式 agent target、global/project scope、symlink
与 copy fallback。安装器只是下载期依赖，安装后的 skill 不依赖 Node、npm 或网络。

**Alternatives considered**

- 发布 engram 专用 npm 安装器：为一个目录映射增加新的发行物、供应链和长期维护面。
- 增加 `engram skill install`：要求用户先安装二进制，并扩大当前已冻结 CLI 契约。
- `gh skill`：仍为 public preview，且当前非交互 agent 参数不能一次覆盖三个目标。
- 不固定安装器或仓库 tag：无法重现安装矩阵，也不能证明三端读取同一版本。
- 在 package 内写入自身 commit SHA：写入动作会改变 commit 与 digest，形成不可满足的发布循环。

## R-004：用显式确认解决同名冲突

**Decision**

canonical 快速命令不带安装器的 `-y/--yes`。安装说明在命令前无条件列出会被替换的标准
目标路径，安装器随后输出 target/`overwrites` 摘要并在写入前取得通用确认；多目录安装还
允许选择 symlink 或 copy。推荐 symlink，使 Claude Code 路径指向 canonical
`.agents/skills/engram`；Windows 或受限文件系统使用 `--copy`，或接受 symlink 失败后的
自动 copy fallback。

`-y` 只记录为高级自动化选项，并明确表示“用户已经授权替换摘要中的所有同名目标”。它
不得出现在安全默认 quickstart。同版本或新版本升级使用完整 `add` 命令和固定新 tag，
不把 `skills update` 作为正本，因为它不会可靠保留原 agent target 与安装模式。

安装中断后不得报告成功；恢复步骤是使用同一 tag 重跑完整命令，再核对每个目标的 package
version/`engram-package-sha256-v1` digest 和实际发现结果。

**Rationale**

`skills@1.5.20` 只检查路径是否存在，写入时会递归删除后重建；它没有来源、版本或
no-clobber guard。它的 global overwrite 检测还会按 agent registry 检查 Codex/OpenCode
专用目录，而实际 universal install 写入 `~/.agents/skills`，所以摘要不能作为唯一 guard。
裸 `-y` 会静默覆盖用户自建内容。固定目标路径的前置警告加上保留的写入确认，是不引入
自研安装器的最小诚实方案。

**Alternatives considered**

- 以 `-y` 作为默认：最短，但不能满足同名自建 skill 的覆盖边界。
- 为上游增加本地 provenance wrapper：能更强地保护冲突，但显著扩大本 feature 范围。
- 把重复安装描述为原子 no-op：与上游“删除后重建”的真实行为不符。

## R-005：提供无 Node、离线和 copy 后备路径

**Decision**

包内 `references/install.md` 同时给出：

- 从已经检出的同一 `skills/engram/` 目录执行本地 `npx ... add ./skills/engram`；
- 无 Node 时，把 canonical package 复制到 `.agents/skills/engram`，并复制或链接到
  `.claude/skills/engram`；
- 个人作用域使用 `~/.agents/skills/engram` 与 `~/.claude/skills/engram`；
- 项目作用域使用仓库根下对应目录；
- 重启/重新调用、版本核对、冲突备份和恢复步骤。

手动路径不创建第三份正文源，不修改 MCP 配置，也不安装 `engram` 或 `engram-mcp`。

**Rationale**

开放格式不规定安装协议，远程 `npx` 也不能在首次使用时离线。标准目录 copy/symlink 是
最小、可审计、跨平台的后备，并保证 skill 正常运行不依赖下载工具。

**Alternatives considered**

- 只支持远程 npx：违反离线和无 command runner 的 edge case。
- 把 CLI/MCP 二进制与 skill 一起安装：混淆三个独立产品入口，并会静默修改用户环境。

## R-006：使用渐进披露的技能包

**Decision**

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

`SKILL.md` 只保留触发边界、MCP-first 决策流程、namespace/secret/state-change 安全规则和
结果格式。具体参数在同一层 `references/` 中按需读取；引用深度为一。`contract.json`
是 version、真实 surface 和 intent mapping 的机器可读正本；人读参考不能另建不同集合。

**Rationale**

metadata 始终进入上下文，正文只在触发时加载，reference 再按需加载。把完整 CLI/MCP
手册塞进正文会增加每次调用成本；把引用再分层则降低发现可靠性。

**Alternatives considered**

- 所有内容放入 `SKILL.md`：重复加载并接近规范建议上限。
- 引用已有 `docs/` 的仓库相对路径：skill 安装后该路径不存在。
- 在 skill 中捆绑可执行脚本：当前工作流只编排已存在的 MCP/CLI，无重复算法需要脚本。

## R-007：以运行时实现冻结 MCP/CLI capability map

**Decision**

MCP 固定工具为 `memory_write`、`memory_search`、`memory_list`、`memory_get`、
`memory_delete`；只有 server 已配置 LLM caller 时才注册 `memory_ingest`。不存在
`memory_curate`、`memory_stats`、`memory_export`、`memory_namespaces` 或
`memory_version`。

CLI 固定命令为 `add`、`search`、`get`、`list`、`delete`、`ingest`、`curate`、
`stats`、`export`、`namespaces` 和 `version`。全局 flag 必须放在 command 之前；
`version` 是唯一不需要 data dir 的命令。contract test 直接对照
`mcpserver.NewServer(...).tools/list` 和 `cmd/engram` 的 `knownCommands`，不得用正则解析
Go 源码，也不得让 manifest 自比较。

**Rationale**

[mcpserver/server.go](../../mcpserver/server.go) 与
[cmd/engram/run.go](../../cmd/engram/run.go) 是当前运行时真相。feature 004 的旧契约早于
`curate`，只引用历史 specs 会漏掉已交付命令。包内 Go 测试可直接访问真实集合并复用
MCP in-memory transport。

**Alternatives considered**

- 解析源码文字：容易把注释、错误文案或格式变化误判为公共契约。
- 只验证 `contract.json` 与 reference：属于 tautology，无法发现 adapter 改名。
- 导出新的 Go API 供测试：会为测试扩大公共产品契约。

## R-008：MCP-first，但不跨 surface 猜测存储身份

**Decision**

重叠的 write/search/get/list/delete/ingest intent 在 MCP 已连接且对应 tool 存在时只走
MCP；MCP 不可用时才走 CLI。curate/stats/export/namespaces/version 是 CLI-only。
从 MCP 切换到 CLI 执行 CLI-only 操作前，必须确认 CLI data dir 和 namespace 指向用户
期望的存储；两个表面的结果不能双写、合并或相互证明。

状态变化仅由明确用户意图触发。普通聊天不自动写入；ingest 和 curate 还要求相应 LLM
能力。每次执行只使用一个 namespace，并以工具真实结果报告 success、empty、not found、
degraded 或 failure。

**Rationale**

Skill 定义工作流，MCP/CLI 执行产品操作。MCP 提供结构化 schema，故适合作为已连接时的
首选；CLI 补齐非重叠管理能力。但当前适配面没有可证明 MCP data dir 与 CLI data dir
相同的公共 identity，因此跨 surface 自动延续上下文会破坏隔离。

**Alternatives considered**

- MCP 与 CLI 双写以“提高成功率”：可能写入两个不同数据库。
- 在 skill 中重做检索或 curation：违反 engine/adapter separation。
- 为 CLI-only intent 发明 MCP tool：会产生虚假调用。

## R-009：保持 namespace、离线与秘密边界

**Decision**

namespace 缺省为 `default`；合法式为 `^[A-Za-z0-9._-]{1,64}$`，并额外拒绝 `.`,
`..`、`/` 和 `\`。切换项目、用户或 namespace 时明确目标，不做隐式跨 namespace 搜索。

基础 CRUD 和关键词降级检索不要求模型。只有结构上已知未配置 embedding 时才说明 semantic
缺失；不探测引擎吞掉的单信号错误。ingest 与 curate 只在用户明确请求且 LLM 可用时执行。
API key、token、密码等秘密不写入 memory、命令参数、tracked file、示例或测试产物；provider
secret 只通过 `ENGRAM_EMBED_API_KEY` 与 `ENGRAM_LLM_API_KEY` 等既有环境变量进入。

**Rationale**

这些边界直接继承宪法和两个 adapter 的校验。Skill 位于 agent 决策层，应该防止越界输入，
但不能假装比 engine 更了解每个检索信号的运行状态。

**Alternatives considered**

- 自动生成跨项目 namespace：会把命名策略隐藏在 skill 中。
- 为了语义检索默认推荐 hosted model：违反 local-first 和零付费默认。
- 把 secret 保存后再遮罩输出：秘密已经进入持久存储，遮罩无济于事。

## R-010：分层验证格式、契约、安装、发现和行为

**Decision**

发布门分五层：

1. Node.js 标准库 validator 检查 frontmatter、name/description、长度、引用、唯一包、
   `contract.json`、eval schema 与 secret fixture；测试先证明每类违规会失败。
2. `mcpserver/skill_contract_test.go` 和 `cmd/engram/skill_contract_test.go` 将 manifest
   对照真实 MCP tools 与 CLI commands。
3. 隔离 HOME/XDG/npm cache/temp 的本地 source 安装矩阵验证三端单独、组合、global、
   project、copy/symlink、同版本重跑、冲突取消和恢复；不得触碰真实 home 或 MCP config。
4. 发布候选在真实 Claude Code、Codex、OpenCode 版本上执行发现与显式调用 smoke，并记录
   client version、scope、path、source tag、candidate commit、digest 和结果。
5. `evals/evals.json` 运行 with-skill 与 without-skill 行为对照；`trigger-evals.json`
   至少含 20 个正负近邻请求。用 skill-creator 的 benchmark 和静态 review viewer 让维护者
   审阅，直到关键 assertion 全通过且触发指标满足 spec。

开放规范的 `skills-ref validate` 仅在其准确版本已固定且本地可用时作为 advisory
交叉检查；缺少该工具不阻断发布。若实际运行后失败，必须先修复 package，或由维护者对照当前
开放规范记录该 validator 版本的 incompatibility disposition；仓库 validator 才是 engram
package contract 的权威门。
维护者 review 必须产生明确的 `approved-no-comments`、`changes-requested` 或
`approved-after-changes` disposition；没有响应是 blocker，不得记录成空反馈通过。

**Rationale**

安装器 exit 0 不能证明客户端真正发现 skill，静态引用检查也不能证明工具名存在。每层使用
最接近事实源的验证，避免自比较。

**Alternatives considered**

- 只运行 Markdown lint：无法发现过期工具名或安装路径。
- CI 中只检查安装目录：无法证明显式调用。
- 只做人工 smoke：难以稳定重现格式、冲突和契约错误。

## R-011：安装正本随 skill 分发，产品入口只做同步摘要

**Decision**

详细安装、升级、路径和故障排查正本为 `skills/engram/references/install.md`。根
`README.md`、`README.zh-CN.md` 和 `docs/README.md` 展示同一 canonical quick command
并链接正本；`docs/guides/cli.md` 与 `docs/guides/mcp-server.md` 只增加依赖关系和交叉链接。
CI validator 对所有入口中的命令做精确同步检查。

现有 CLI guide 关于 `engram --help` 会列出完整命令/参数的说法与运行时不符；本 feature
纠正文案，但不新增 CLI help command。skill 使用 bundled CLI reference，不依赖该 help。

**Rationale**

安装正本必须随 standalone package 到达用户，又不能在 docs 中维护第二份完整说明。入口保留
一条可复制命令，详细信息回链一个文件，符合 feature 019 的 current-canonical 规则。

**Alternatives considered**

- 新建另一份完整 `docs/guides/agent-skill.md`：会与包内 install reference 漂移。
- 让每个 README 自行维护安装矩阵：命令和路径容易不一致。
- 在 020 顺手实现完整 CLI help：改变 adapter 公共面，超出范围。

## R-012：纯适配变更不触发 LoCoMo

**Decision**

实现只修改 `skills/`、adapter package 内测试、验证脚本、CI 和文档；
`memory/`、`embedding/`、`provider/`、`store/`、`internal/` diff 必须为空。验证运行现有
MCP/CLI parity、namespace/isolation、离线测试及全量 `CGO_ENABLED=0` build/test/vet。
不运行 LoCoMo，不配置按量计费的 engram provider 或 hosted reranker/recall model。需要
agent inference 的真实客户端和 behavior/trigger eval 只允许使用本地执行路径或维护者已有、
不会产生按调用增量费用的授权；否则记录 release blocker。

**Rationale**

该 feature 不改变 retrieval、extraction、curation、storage 或 embedding 行为。通过实际
adapter contract 和 parity 证明 invariant-by-construction，比重跑无归因价值的模型评测更
符合宪法 IV 和零费用目标。

**Alternatives considered**

- 为 skill 文案重跑 LoCoMo：无法测量安装或 agent 编排质量，且产生不必要费用。
- 修改 engine 以方便 skill：破坏 adapter separation；缺失能力应另开 engine increment。

## R-013：冻结跨平台 package digest

**Decision**

所有 source、copy、symlink 与 remote 安装证据统一使用 `engram-package-sha256-v1`。算法把
package root 本身的 symlink 解析到目标目录，拒绝 package 内部 symlink；递归收集所有 regular
file，以 UTF-8 relative POSIX path 的原始字节序排序；每个 UTF-8 文本文件把 CRLF/CR 规范化
为 LF，并按 `path + NUL + normalized-byte-length + NUL + normalized-bytes + NUL` 依次送入
SHA-256，输出小写十六进制。mtime、mode 和目录条目不参与摘要；非 UTF-8 或含 NUL 的文件失败。

**Rationale**

三端组合、copy/symlink fallback、Windows checkout 和远程安装都必须对同一逻辑 package
得到相同 digest。固定文件集合、排序、换行与 symlink 语义后，version/digest 才是可复现证据，
且不会因 tar 元数据或平台换行产生假失败。

**Alternatives considered**

- 对目录直接打 tar 后哈希：文件顺序、mtime、mode 与 tar 实现会造成平台漂移。
- 只哈希 `contract.json`：无法检测正文、references、evals 或 license 漂移。
- 跟随 package 内部 symlink：可能越界读取并使安装内容依赖外部文件。

## R-014：冻结无依赖 token 估算

**Decision**

`engram-body-token-estimate-v1` 对完整 `SKILL.md` 先把 CRLF/CR 规范化为 LF，忽略 Unicode
whitespace，分别计数 ASCII 非空白 code points `A` 与非 ASCII 非空白 code points `U`，
结果为 `ceil(A / 4) + U`。行数按规范化后的完整文件计算，尾部单个 LF 不产生额外空行；
发布门要求不超过 500 行且估算值不超过 5,000。

**Rationale**

该口径可由 Node.js 标准库确定性实现，对中英文都不依赖客户端私有 tokenizer，也符合
“约 5,000 tokens”的开放规范建议。它是预算估算而非声称等同任何模型 tokenizer。

**Alternatives considered**

- 使用未固定的模型 tokenizer：增加依赖并随模型或版本漂移。
- 只按 UTF-8 bytes/4：会系统性低估中文等多字节文本。
- 只检查行数：无法限制非常长的单行正文。

## R-015：零增量费用评估与显式人工结论

**Decision**

behavior/trigger eval 与真实客户端 smoke 在启动前记录 `execution_cost_class`，只接受 `local`
或 `existing-flat-rate`；任何 `metered`、新建 pay-as-you-go key 或无法确认计费方式的路径都
阻断执行。每轮记录 runner/client、model（若可见）、调用数和 `incremental_model_cost=0`。
维护者 review 必须写入明确 disposition；没有响应不算审核完成，`changes-requested` 也不能
作为最终通过状态，必须迭代到 `approved-no-comments` 或 `approved-after-changes`。

**Rationale**

真实客户端显式调用和 skill-creator A/B eval 需要 agent inference，而“不调用任何模型”会与
验收目标冲突。冻结为“无新增按调用费用”既允许使用本地模型或已有固定授权完成真实验证，也
维持不为评测配置付费 provider、不使用 hosted reranker、不产生 LoCoMo 费用的成本门禁。

**Alternatives considered**

- 临时配置按量计费 API key：违背零费用门，并可能把 secret 带入测试环境。
- 跳过真实客户端调用：无法宣称三端支持。
- 把未回复视为无修改意见：绕过了人工质量门。
