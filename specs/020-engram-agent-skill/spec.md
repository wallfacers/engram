# Feature Specification: engram Agent Skill

**Feature Branch**: `020-engram-agent-skill`

**Created**: 2026-07-28

**Status**: Draft

**Input**: User description: "项目的 MCP 和 CLI 已经具备；按照 Claude Skills / Agent Skills 规范创建项目 skill，支持方便的一条命令安装，并明确支持 Claude Code、Codex 与 OpenCode，其他客户端自行兼容。"

## Background and Scope

engram 已提供 MCP server 与 AI-first CLI，但 agent 仍需自行理解何时搜索、何时写入、
如何选择 namespace、何时允许 ingest 或 curation，以及 MCP 不可用时如何切换到 CLI。
本 feature 增加一个可分发的 `engram` Agent Skill，把这些已冻结的使用流程包装成按需加载的
操作知识；它不新增记忆能力，不复制引擎算法，也不改变 MCP 或 CLI 的既有契约。

交付范围包含一个符合 Agent Skills 开放规范的唯一技能包、面向三个目标客户端的命令安装
入口、安装与使用说明，以及可重复的兼容性和行为验证。明确支持范围仅为 Claude Code、
Codex 和 OpenCode；其他客户端可以消费开放格式，但项目不为其维护专用安装适配、兼容性
分支或验收承诺。

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 一条命令安装并发现技能 (Priority: P1)

作为已经使用 Claude Code、Codex 或 OpenCode 的 engram 用户，我希望复制一条命令就能把
`engram` skill 安装到个人环境，并在目标客户端中立即发现或显式调用它，而不必克隆仓库、
手工复制目录或维护三份文件。

**Why this priority**: 技能只有能被低摩擦安装和发现才形成实际产品入口；安装步骤分散或每个
客户端维护一份副本会直接造成使用失败与版本漂移。

**Independent Test**: 在三个隔离的全新用户环境中分别执行文档给出的安装命令，再启动对应
客户端并显式调用 `engram` skill；三个环境均只发现一个同名技能且加载同一版本即通过。另在
一个隔离环境中执行面向全部三个客户端的组合命令，验证一次执行完成三处安装。

**Acceptance Scenarios**:

1. **Given** 用户已安装任一受支持客户端和主安装命令所需的通用命令运行器，**When** 用户执行文档中的单客户端安装命令，**Then** 对应客户端发现且能够显式调用唯一的 `engram` skill。
2. **Given** 用户同时使用三个受支持客户端，**When** 用户执行文档中的组合安装命令，**Then** Claude Code、Codex 与 OpenCode 均能发现同一版本的 `engram` skill，且无需手工复制文件。
3. **Given** 用户希望只在当前项目或所有项目中使用技能，**When** 用户选择对应安装作用域，**Then** 安装位置只影响所选作用域，文档清楚说明其影响。
4. **Given** 同一版本已经安装，**When** 用户再次执行安装命令，**Then** 安装结果保持单一且可用，不产生第二份同名技能或不同版本的并存副本。

---

### User Story 2 - 可靠执行记忆工作流 (Priority: P2)

作为使用 engram 的 agent，我需要在用户要求记住、检索、查看、删除、抽取、整理或导出记忆时，
按照稳定的流程调用现有 MCP 工具或 CLI 命令，并把结果作为证据返回，而不是猜测工具名、重做
检索逻辑或把普通聊天静默写入长期记忆。

**Why this priority**: MCP 和 CLI 已经提供能力，skill 的核心价值是让 agent 一致、正确地编排
这些能力；若它改变语义或制造隐式写入，反而会降低用户对持久记忆的信任。

**Independent Test**: 给安装了技能的 agent 一组覆盖写入、检索、精确读取、删除、显式 ingest、
curation 与导出的代表性请求；分别在“仅 MCP 可用”“仅 CLI 可用”两种环境运行，核对它只使用
实际存在的公共工具或命令、选择正确 namespace，并以真实返回值完成任务。

**Acceptance Scenarios**:

1. **Given** engram MCP 工具已经连接，**When** 用户要求写入一条事实并随后召回，**Then** agent 使用现有 MCP 写入与检索工具在同一 namespace 完成往返，并依据工具返回确认结果。
2. **Given** MCP 工具不可用但 `engram` CLI 可用，**When** 用户发出同样请求，**Then** agent 使用语义等价的 CLI 命令完成往返，并清楚说明当前使用的是 CLI 路径。
3. **Given** MCP 与 CLI 都可用，**When** 用户没有指定使用面，**Then** agent 按技能规定的一致优先级选择一个表面，不重复写入，也不把两个可能不同的数据目录或 namespace 静默合并。
4. **Given** 用户要求从对话中抽取记忆或运行 curation，**When** 相应模型能力未配置或操作未被显式请求，**Then** agent 不假装成功、不自动执行，并给出缺失条件或需要确认的下一步。
5. **Given** 用户询问过去保存的事实，**When** 检索无命中，**Then** agent 如实报告没有证据，不编造一条记忆来回答。

---

### User Story 3 - 保持离线、隔离与秘密安全 (Priority: P3)

作为重视本地数据控制的用户，我希望 skill 延续 engram 的默认离线和 namespace 隔离边界：
基础读写与降级检索不要求云服务，任何可选模型依赖都被如实说明，秘密不会被写入记忆、命令
参数、日志或示例配置。

**Why this priority**: 本地优先、隔离与诚实降级是 engram 的产品根基；skill 位于用户与适配器
之间，必须强化这些边界而不是用“自动化”绕过它们。

**Independent Test**: 在无网络、无 embedding、无 LLM 配置的环境中执行基础写入、检索、
查看和删除流程；验证流程成功或按既有能力诚实降级。再以非法 namespace、包含秘密的输入和
需要 LLM 的请求运行安全用例，验证没有越界写入、秘密回显或虚假成功。

**Acceptance Scenarios**:

1. **Given** 没有配置任何模型端点，**When** 用户执行基础记忆读写与检索，**Then** skill 使用可用的本地路径并如实说明已知的语义信号缺失，不要求用户购买或启用托管服务。
2. **Given** 用户没有指定 namespace，**When** 请求会读写长期记忆，**Then** agent 使用单一、稳定且可解释的默认 namespace；涉及切换或跨项目访问时先明确目标，不执行隐式跨 namespace 查询。
3. **Given** namespace 含路径分隔符、`.`、`..` 或其他非法形式，**When** agent 准备调用 MCP 或 CLI，**Then** skill 要求修正标识，不尝试绕过适配器校验。
4. **Given** 输入中出现 API key、token、密码或其他秘密，**When** 用户未明确要求保存且未说明风险，**Then** agent 不把秘密写入 engram，并提醒使用环境变量等既有安全配置通道。

---

### User Story 4 - 单一来源维护三端兼容 (Priority: P4)

作为 engram 维护者，我需要只修改一个技能源目录，就能验证 Claude Code、Codex 与 OpenCode
安装结果和工作流没有漂移；详细命令参考按需加载，主技能说明保持精简，版本与当前 MCP/CLI
契约一致。

**Why this priority**: 三端各存一份实现会很快产生过期命令和不同安全规则；单一来源和自动验证
让该适配面能够随项目演进而持续可靠。

**Independent Test**: 修改隔离副本中的技能版本或一条参考说明，运行标准格式校验、三客户端
安装矩阵和代表性工作流测试；验证三端读取同一修改、主技能没有重复完整命令手册，且失效的
MCP 工具名或 CLI 命令会使验证失败。

**Acceptance Scenarios**:

1. **Given** 维护者更新 canonical skill package，**When** 运行发布前验证，**Then** 三个受支持客户端的安装产物来自同一源版本，不存在需同步的客户端专用正文副本。
2. **Given** 主技能只需决定工作流，**When** agent 需要具体 MCP 或 CLI 细节，**Then** 它按需读取同级参考资料，而不是在每次触发时加载全部命令说明。
3. **Given** MCP 或 CLI 的公共名称发生变化，**When** 技能仍引用旧名称，**Then** 契约一致性验证失败并指出具体引用，阻止发布过期技能。

### Edge Cases

- 主安装命令运行器不存在或安装时无法联网时，文档提供不依赖该运行器的手动标准目录安装
  方式，并明确正常使用 skill 不需要安装命令运行器持续在线。
- 只安装了三个目标客户端中的一部分时，安装流程只需保证所选目标成功，不应把未安装客户端
  误报为 engram 运行故障。
- 目标位置已有用户自建的同名 skill 时，不得无提示地覆盖；应清楚报告冲突并要求用户选择更新、
  替换、改名或保留。
- 旧版本升级中断时，安装流程不得把部分完成状态报告为成功；恢复说明必须要求重跑同一固定版本命令，并重新验证每个目标的版本与发现结果。
- 发布命令需要引用自身随附安装说明时，release tag 名称必须在最终 package 内容冻结前预先
  确定并写入文档；最终内容提交后才可由维护者把该 tag 指向精确候选提交。不得使用需要把
  自身 commit SHA 写回同一 package 的循环发布流程。
- skill 已安装但 MCP 未连接且 CLI 不在 `PATH` 时，agent 应区分“技能已加载”与“engram 工具
  尚未配置”，给出准确的设置入口，不得虚构一次成功调用。
- MCP 与 CLI 指向不同数据目录或 namespace 时，agent 不得自动合并结果、双写或用一个表面的
  成功推断另一个表面也已更新。
- embedding 端点不可用时，基础检索可按产品契约降级；LLM 不可用时 ingest 与 curation 不得
  被描述为已完成。
- 命令输出为空、记忆不存在、内容超限或适配器返回结构化错误时，skill 必须保留失败语义和
  建议下一步，不能把错误文本当作记忆内容。
- Windows 或受限文件系统不支持符号链接时，安装方案应能使用等价的复制方式完成目标客户端
  安装，同时仍保持发布源唯一。
- 其他 Agent Skills 客户端可以自行尝试标准包；项目文档不得把未经测试的客户端列入支持矩阵
  或为其增加会使 canonical skill 分叉的专用扩展。

## Requirements *(mandatory)*

### Scope Boundaries

- 本 feature 新增的是 MCP/CLI 之上的技能适配与分发层，不新增记忆操作、自动记忆策略、
  后台服务、远程传输或存储格式。
- skill 安装只安装技能包；engram CLI 二进制安装和 MCP server 注册仍通过各自现行入口完成。
  skill 可以检查依赖并链接或说明设置步骤，但不得静默修改用户的 MCP 配置或系统级可执行路径。
- 正式支持与兼容性验证仅覆盖 Claude Code、Codex 和 OpenCode；不建设其他客户端的插件、
  marketplace 包、专用目录副本或兼容性测试。
- 不修改 `memory/`、`embedding/`、`provider/`、`store/`、`internal/` 下的引擎实现，不改变
  MCP tool、CLI command、namespace、错误或降级语义。

### Functional Requirements

**开放格式与单一来源**

- **FR-001**: 项目 MUST 交付一个符合当前 Agent Skills 开放规范的 canonical skill package，且入口文件为 `SKILL.md`。
- **FR-002**: skill 的稳定标识 MUST 为 `engram`；名称、父目录和所有安装后显示的标识 MUST 一致，并满足开放规范的命名约束。
- **FR-003**: `SKILL.md` MUST 提供非空的名称与 description；description MUST 同时说明技能能做什么、何时应触发，并覆盖 engram、长期记忆、MCP、CLI、记住、召回、检索、ingest 与 curation 等真实意图。
- **FR-004**: 项目 MUST 只维护一份技能正文和一套同级资源作为发布源；三个目标客户端的安装结果 MUST 来自该源，不得提交三份需人工同步的客户端专用实现。
- **FR-005**: skill MUST 采用渐进披露：主说明包含决策流程、安全边界和输出要求，详细 MCP/CLI/install 参考按需读取；主说明与引用深度 MUST 满足 Agent Skills 规范建议的上下文和文件层级限制。

**安装与发现**

- **FR-006**: 项目 MUST 提供一条可复制的命令，将由版本派生、不可变 release tag `engram-skill-v<skill-version>` 标识的 `engram` skill 安装到 Claude Code、Codex 与 OpenCode 的个人作用域；tag 名称 MUST 在最终 package 内容冻结前写入所有用户入口，并在内容冻结后指向精确候选提交。该命令 MUST 支持在一次执行中选择全部三个目标，且除安装方式与覆盖摘要确认外不得要求用户手工编排步骤。
- **FR-007**: 项目 MUST 同时提供每个目标客户端的单独安装命令，以及当前项目作用域与个人全局作用域的明确选择；默认快速开始 MUST 说明选用的作用域及影响。
- **FR-008**: 主命令安装路径 MUST NOT 要求用户预先克隆 engram 仓库、手工定位 `SKILL.md` 或逐客户端复制文件。
- **FR-009**: 安装流程 MUST 保持单一可发现实例：成功完成的同版本重装具有幂等结果，成功升级后不得遗留可同时加载的旧副本；安装说明 MUST 在命令前列出可能被替换的标准目标路径，默认命令 MUST 在写入前取得明确确认，非交互覆盖只允许作为用户已明确选择替换的高级用法。
- **FR-010**: 项目 MUST 提供标准目录的手动安装后备路径，供主安装命令运行器不可用或离线安装场景使用；后备路径仍 MUST 使用 canonical package，不能引入客户端专用正文。
- **FR-011**: 安装说明 MUST 给出三个目标客户端各自的发现位置、显式调用方式、重新加载要求与最小故障排查，并明确其他客户端不在正式支持矩阵内。
- **FR-012**: skill 安装完成后的正常使用 MUST NOT 依赖安装命令运行器、npm 服务或持续网络连接。

**记忆工作流**

- **FR-013**: skill MUST 在 MCP 已连接时优先使用现有 MCP memory tools，在 MCP 不可用但 CLI 可用时使用语义等价的 `engram` commands；两者都不可用时 MUST 报告缺失依赖和准确的设置入口。
- **FR-014**: skill MUST 只引用当前产品实际提供的 MCP tools 与 CLI commands，不得发明工具名、绕过适配器或在提示中重实现存储、检索、抽取、curation 算法。
- **FR-015**: skill MUST 覆盖至少以下用户目标：写入或更新一条显式记忆、相关性检索、精确读取、列表、删除、显式对话 ingest、显式 curation、统计、导出、namespace 发现和版本诊断；当某目标只存在于一个适配面时 MUST 选择该适配面或如实说明限制。
- **FR-016**: 每次读写 MUST 使用一个明确且稳定的 namespace；skill MUST 阻止隐式跨 namespace 访问、双写和把不同数据目录的结果当成同一存储。
- **FR-017**: 普通聊天 MUST NOT 因 skill 激活而被自动持久化；写入、ingest、delete、curation 等会改变状态的操作 MUST 来源于用户明确意图，并在目标或范围含糊时先确认。
- **FR-018**: skill MUST 以 MCP/CLI 的真实结果为记忆证据；无命中、not found、能力缺失或其他失败 MUST 如实返回，不得补造记忆、吞掉错误或报告虚假成功。

**本地优先与安全**

- **FR-019**: skill MUST 保持默认离线：基础写入、检索、读取、列表和删除不得要求 hosted service；缺少 embedding 时只陈述结构上可知的降级，不探测或改写引擎的静默按信号降级。
- **FR-020**: 需要 LLM 的 ingest 或 curation MUST 只在相应能力已配置且用户明确请求时执行；缺失时 MUST 给出诚实诊断，不得默认推荐或启用付费云 reranker、recall model 或单一 hosted provider。
- **FR-021**: skill MUST 提醒并执行 namespace 标识边界，不得构造含路径逃逸的 namespace，也不得尝试绕过 MCP/CLI 的校验。
- **FR-022**: skill、安装说明、示例、测试产物和 agent 输出 MUST NOT 包含、记录或建议把 API key、token、密码等秘密写入 tracked file、命令参数或 memory entry；秘密只通过项目现有的环境变量通道进入 provider。

**验证与文档**

- **FR-023**: 发布前 MUST 验证 Agent Skills frontmatter、名称、description 长度、相对文件引用、主说明长度、确定性 token 估算、package 内容摘要和目录结构均符合冻结的开放格式与项目契约。
- **FR-024**: 发布前 MUST 在隔离环境对 Claude Code、Codex 与 OpenCode 逐一执行安装、发现和显式调用 smoke test，并执行一次三目标组合安装测试；这些调用只能使用本地执行路径或维护者已有且不会产生按调用增量费用的授权，缺少合格路径时发布 MUST 阻断而不是产生临时付费调用。
- **FR-025**: 发布前 MUST 在同一零增量费用口径下运行代表性行为用例，至少包含直接触发、间接触发、非目标近邻请求、仅 MCP、仅 CLI、完全离线、缺失 LLM、非法 namespace 和秘密输入，并取得维护者明确的审核结论；没有审核响应不得等同于“无修改意见”。
- **FR-026**: 契约一致性验证 MUST 将 skill 中引用的 MCP tool 与 CLI command 对照当前适配器公开面；任一未知或遗漏的必需名称 MUST 使验证失败。
- **FR-027**: 项目的当前用户入口 MUST 展示同一条 canonical 快速安装命令、三个正式支持客户端和 skill 与 CLI/MCP 的依赖关系，不得维护相互冲突的安装说明。
- **FR-028**: 本 feature MUST 保持引擎目录 diff 为空；作为纯适配工作，以现有 MCP/CLI parity、技能契约校验和三客户端安装矩阵证明不触发 LoCoMo 算法回归重跑。

### Key Entities

- **Canonical Skill Package**: `engram` 技能的唯一发布源；包含标准入口、按需参考和必要的确定性验证资源，具有名称、版本、license 与契约基线。
- **Installation Target**: 一个受支持客户端及其作用域组合；属性包括客户端类型、个人或项目作用域、发现位置、调用方式和已安装版本。
- **Engram Surface**: skill 可编排的现有适配面，取值为 MCP 或 CLI；包含可用能力、数据目录、namespace 与已知模型配置状态，但不持有引擎私有状态。
- **Memory Intent**: 用户希望完成的记忆目标，例如 write、search、get、list、delete、ingest、curate、stats、export、namespace discovery 或 version diagnosis。
- **Namespace Context**: 一次工作流明确选择的隔离空间；包括合法标识、选择来源与当前适配面，不允许由 skill 合并多个空间。
- **Capability State**: agent 能从配置或工具发现中诚实确定的状态，例如 MCP connected、CLI available、embedding configured、LLM configured 或 unavailable。
- **Compatibility Verification Case**: 将客户端、安装作用域、触发请求、预期表面、预期操作和安全不变量关联起来的可重复验收记录。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 在三个隔离的全新环境中，Claude Code、Codex 与 OpenCode 的单客户端安装命令成功率均为 100%，安装后显式调用 `engram` skill 的成功率为 100%。
- **SC-002**: 面向全部三个客户端的 canonical 组合命令在一次执行后使三端各发现且仅发现一个同版本 `engram` skill；除复制命令、查看目标路径、选择安装方式、确认写入和重新加载客户端外不需要其他手工编排。
- **SC-003**: 同版本安装命令连续执行两次后，每个目标客户端可发现的 `engram` skill 数量仍为 1；升级用例中可发现的旧版本残留数量为 0。
- **SC-004**: Agent Skills 标准验证对 frontmatter、命名、description、目录、引用和长度的检查通过率为 100%，未知或客户端专用的非必要 frontmatter 字段数量为 0。
- **SC-005**: 对当前 MCP tools 与 CLI commands 的契约对照覆盖率为 100%；skill 中引用的不存在名称数量为 0，FR-015 所列目标均有一个真实可执行路径或明确的能力限制。
- **SC-006**: 在“仅 MCP”与“仅 CLI”用例中，写入后检索到同一事实的往返成功率均为 100%；agent 的重复写入、跨 namespace 泄漏和虚假成功次数均为 0。
- **SC-007**: 在无网络、无 embedding、无 LLM 配置的运行环境中，基础写入、读取、列表、删除和存在关键词命中的降级检索用例成功率为 100%；ingest 与 curation 的虚假成功次数为 0。
- **SC-008**: 在非法 namespace 与秘密输入用例集中，路径逃逸调用次数、秘密写入 memory 的次数、秘密出现在示例或测试产物中的次数均为 0。
- **SC-009**: 一组至少 20 个触发评估请求中，所有显式 engram 请求均能加载技能；直接或间接表达持久记忆目标的正例加载率不低于 90%，仅讨论一般内存、缓存、数据库或普通聊天上下文的近邻负例误加载率不高于 10%。
- **SC-010**: canonical skill 正文副本数量为 1，三端专用正文副本数量为 0；主 `SKILL.md` 保持在 500 行以内，并按 package contract 冻结的确定性估算口径保持在 5,000 estimated tokens 以内；所有按需资源均由一层相对引用到达。
- **SC-011**: 引擎公共行为、MCP/CLI 的既有语义一致性和离线能力相对 feature 开始时保持不变；本 feature 不配置或调用按量计费的 engram 模型 provider、hosted reranker/recall model，也不运行 LoCoMo；真实客户端与行为/触发评估只使用本地或维护者已有的零增量费用授权，本 feature 新增模型与 LoCoMo 费用均为 0。

## Assumptions

- 技能稳定名称采用 `engram`，与 MCP server identity 和 CLI binary 保持一致。
- 主快速安装面向个人全局作用域，因为用户通常希望在多个项目中调用 engram；项目作用域作为
  明确选项提供，安装命令不会替用户选择或创建 namespace。
- 主命令安装路径使用已被 Agent Skills 生态采用、能从 Git 仓库选择单个 skill 并指定多个
  agent target 的通用安装器；其具体版本、锁定与校验方式在 plan 阶段冻结。
- 用户入口中的远程来源使用在最终 package 提交前由 skill version 确定的
  `engram-skill-v<skill-version>` release tag；该 tag 在内容冻结后由维护者明确授权创建，
  不使用需要把最终 commit SHA 写回 package 的自引用方式。
- 主安装器所需的 Node.js / command runner 只属于下载安装路径，不是 skill 正常运行依赖；
  无该环境的用户使用手动标准目录后备方案。
- skill 包本身随 engram 仓库和项目 license 分发；版本与仓库 release 或明确的 skill metadata
  对齐，不建立独立在线服务。
- 用户已经或将单独安装 engram CLI、注册 engram MCP server；安装 skill 不自动赋予工具能力，
  也不静默修改客户端 MCP 配置。
- MCP 是结构化、已连接时的首选表面；CLI 是未连接 MCP 时的本地后备。两个表面的重叠操作沿用
  已有 parity 契约，非重叠命令只通过实际提供该能力的表面执行。
- 显式调用语法和发现目录以三个客户端在实现时的当前官方文档为准；这些产品契约可能变化，
  因此兼容矩阵在每次发布前重验，不把其他客户端的当前行为当作保证。
- 真实客户端和 skill-creator 评估只在执行路径被记录为本地或现有零增量费用授权时运行；
  任何需要新建按量计费凭据或产生临时模型费用的情况都作为发布 blocker。
- 不为其他 Agent Skills 客户端增加专用兼容层；只要它们能读取开放标准包，用户可以自行安装
  和验证，项目不宣称支持。

## Dependencies

- Agent Skills 开放规范及 Claude Skills 对“skill 提供流程知识、MCP 提供工具与数据”的边界。
- Claude Code、Codex 与 OpenCode 当前公开的 skill 发现目录和显式调用契约。
- feature 002 已冻结的 MCP tools、feature 004 已冻结的 CLI commands，以及当前 curation
  生命周期和 namespace 隔离语义。
- 现行的 CLI 与 MCP 用户指南，作为二进制安装、配置和故障排查的 canonical 文档入口。
