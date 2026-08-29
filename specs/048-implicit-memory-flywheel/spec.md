# Feature Specification: engram skill 隐式记忆触发 + 三工具数据飞轮

**Feature Branch**: `048-implicit-memory-flywheel`

**Created**: 2026-08-29

**Status**: Draft(specify 阶段,待维护者确认后进 plan)

**Input**: User description: "有用户反馈有的时候,没记录记忆,你要做的是,弄一个数据集,去测试一些比较刁钻的场景,记录记忆和读取记忆的场景,而不是得用户显示的提示记录下,去读取记忆这样的提示采取用skill,你得搞数据飞轮的方式不断优化这个记录记忆skill" + 附加任务 "在 opencode v2 / codex / claude code 等工具上测试(claude --settings ~/.claude/settings.json.glm_w / codex -p tf --yolo / opencode2);codex、opencode 这类支持标准 agents/skills 目录的工具优先用标准目录,避免在 ~/.codex/skills 这类私有目录重复安装;修改 README 安装说明" + 维护者拍板(2026-08-29):隐式记录采用**直接写 + 当轮告知**契约。

## 背景与问题定位

### 用户反馈与根因

用户反馈"有的时候没记录记忆"。根因不是引擎丢数据,而是 **skill 触发契约把隐式场景整体排除**——020 交付的 `skills/engram/SKILL.md` 三处显式立场:

1. description: "Use … whenever a **user asks to** remember/记住, recall/召回 …" —— 触发词全部锚定显式请求;
2. 正文第一段: "Use this skill only for a request about durable engram memory … **Activation never makes ordinary conversation persist automatically**";
3. §3: "`write` … require **explicit user intent** … **generic conversation is not**"。

用户在对话中自然透露稳定事实("我以后都用 Neovim 写 Go 了,别再给我 VSCode 建议")时,skill 不触发、agent 不写 —— 记忆凭空丢失。读取侧同理:后续会话问"我上次定的事务隔离级别是什么",agent 不会主动查记忆,除非用户明说"查记忆"。

### 020 遗留 blocker:飞轮缺失

020 validation-report T036–T038 记录:"no local/existing-flat-rate agent runner is recorded … The held-out trigger rates therefore **cannot be claimed**"。触发评测数据集(32 条:16 显式正例 + 16 近邻负例)从未被真实 runner 跑过,SC-009 的 90%/10% 门从未实测。**没有 runner 就没有飞轮**:失败案例不可发现、skill 修订不可验证、回归不可防护。

现状三要素已齐,飞轮可建:(a) 三个可非交互驱动的 agent CLI 已就位——claude 2.1.251(`--settings ~/.claude/settings.json.glm_w`,GLM 代理)、codex-cli 0.150.1(`-p tf --yolo`,Profile V2 modelflare)、opencode2 v0.0.0-beta-18600;(b) 既有授权渠道均为包月/中转,增量成本可忽略;(c) 020 已有 32 条触发用例可作回归层。

### 安装目录重复:本机实证

维护者指出 codex/opencode 支持标准 skills 目录,应优先用标准目录、避免私有目录重复安装。本机实证:`~/.agents/skills/`(标准共享目录)与 `~/.codex/skills/`、`~/.config/opencode/skills/` 中 gsap 系列、find-skills 等**同时存在多份拷贝**——重复安装问题真实发生。install.md 已写"never copy it into additional client directories by hand",但 README 安装段与各工具当前版本(尤其 opencode v2 beta)的实际扫描契约需实测核对后统一正本。

### 方向变更声明(推翻 020 既有立场,需显著记录)

020 spec.md:"若它改变语义或制造隐式写入,反而会降低用户对持久记忆的信任"——该立场于 2026-08-29 由维护者基于用户反馈推翻:**隐式场景应直写并在当轮告知**。信任风险由新护栏替代旧禁令:(1) durable-fact 边界精确化(见 FR-002);(2) 刁钻负例集 + 误触发率硬门(≤10%);(3) 当轮告知保证可发现、可纠正。020 spec 不改写,以本 spec 为准。

## User Scenarios & Testing *(mandatory)*

### User Story 1 — 隐式写入:自然透露的稳定事实直写 + 当轮告知 (Priority: P1)

用户在普通对话中自然透露 durable fact,不使用任何记忆指令措辞。agent 必须识别、经 engram 写入、并在**同一轮**用一行自然语言告知"已记住 X"(不是请求确认、不是事后补写)。

durable fact 判定边界(正例类):
- 稳定偏好:工具/编辑器/语言/框架/饮品/作息等("以后都给我用 pnpm,别再 npm 了");
- 约束与禁令:发布冻结窗、饮食限制、工作时段("我周五下午不排会");
- 身份与角色:职业、团队、常驻城市、时区;
- 项目约定:分支策略、代码风格、目录习惯("这个仓库的提交信息一律中文");
- 长期决策与状态变更:"我换到 M2 MacBook 了"、"上次说的服务器,已经从 Ubuntu 换成 Debian 了"(**更新**旧行,不是追加重复)。

不写边界(负例类,详见 FR-002):一次性任务上下文、明确拒绝、秘密/凭证、与用户无关的通用知识讨论、明确的 transient 表述("这周先这样")。

**Why this priority**: 用户反馈的核心痛点;写入是记忆系统的入口,丢失即全链路失效。独立可测:给定隐式透露 prompt,判定 skill 是否触发 + 是否完成一次写入 + 是否当轮告知。

**Independent Test**: 隐式写入正例集批跑,每条 case 从 runner 可观察输出判定"触发且写入且告知"三要素。

**Acceptance Scenarios**:

1. **Given** agent 已装 engram skill 且 MCP/CLI 可用, **When** 用户说"以后都给我用 pnpm,别再 npm 了"(无任何记忆指令词), **Then** skill 触发,一次写入(偏好类),当轮一行告知;不追问"要我记住吗"。
2. **Given** 记忆中已有旧值("用户服务器是 Ubuntu"), **When** 用户自然提到"服务器已经换成 Debian 了", **Then** agent 更新/取代旧值而非追加矛盾新条目,并在当轮告知更新语义。
3. **Given** 用户明确说"这件事别记下来", **When** 对话继续, **Then** agent 不写、不当轮告知、不反复确认。
4. **Given** 用户透露 API key/密码类内容, **When** agent 识别到秘密, **Then** 拒写并沿 020 秘密契约引导(存非秘密描述)。

### User Story 2 — 隐式读取:涉及持久事实的问题先查记忆再答 (Priority: P1)

后续任意会话中,用户的问题或任务**与用户/项目的持久事实相关**时("我用的什么包管理器?"、"帮我把新模块按我们之前的分层约定放一下"),agent 必须先查 engram 再回答/行动,不要求用户说"查记忆"。读取后的证据判读沿 evidence-guidance v3(有界子集、冲突不猜、枚举全扫)。

读取边界:与用户持久事实无关的普通问题("Go 的 defer 是什么语义")**不查**——防每个问题都先打一次检索(延迟与 token 双输)。

**Why this priority**: 读取侧是"记了没人用"的对称痛点;且读取正例直接验证上一会话的写入是否可回环。独立可测:给定读取类 prompt,判定是否发生 engram 检索调用 + 回答是否以检索证据为据。

**Independent Test**: 隐式读取正例集批跑(配套前置:同 runner 先完成对应写入),判定"触发检索 + 答案有据"。

**Acceptance Scenarios**:

1. **Given** 上一会话已隐式写入 pnpm 偏好, **When** 新会话用户问"帮我把这个项目的依赖装一下"(未提记忆), **Then** agent 先查记忆,据检索结果选 pnpm,回答可指出依据来源。
2. **Given** 记忆中无相关事实, **When** 用户问"我的常用编辑器是什么", **Then** agent 查后如实报告未存,不编造、不反问用户"你为什么不先记"。
3. **Given** 用户问与个人持久事实无关的纯技术问题, **When** agent 回答, **Then** 不发生 engram 检索调用。

### User Story 3 — 刁钻场景触发数据集(版本化、确定性判分) (Priority: P1)

构建触发评测数据集(020 的 32 条并入为回归层),覆盖刁钻边界,正负均衡、中英双语。四个模块:
- **implicit-write-pos**:隐式透露 durable fact(US1 正例类全谱 + 刁钻:间接表达、吐槽式透露、对话中途顺带一提、纠正旧值);
- **implicit-write-neg**:不写类(transient、RAM/cache/数据库近邻干扰、"别记"、秘密、他人事实不当归属、"只在本次对话记住");
- **implicit-read-pos**:应查记忆(US2 正例 + 刁钻:指代前情("按老规矩")、跨会话事实引用、列表枚举型问题);
- **implicit-read-neg**:不应查(纯技术题、时事题、与用户无关的假设性问题)。

每条 case 的期望判定必须**从 runner 可观察输出确定性推出**(skill 是否加载、是否发生 engram 调用、调用类型、是否告知),不允许"回答质量"式主观判分。数据集只增不减(防回归),版本号随内容演进。

**Why this priority**: 数据集是飞轮的燃料与防护栏;没有它,skill 修订是盲改。

**Independent Test**: 数据集 JSON 结构校验(模块覆盖、正负均衡、双语占比、判分字段完备)通过;抽样 case 的期望判定可由两名独立执行者得出一致结论。

**Acceptance Scenarios**:

1. **Given** 数据集生成, **When** 运行结构校验, **Then** 四模块齐备、每模块正/负例数满足预注册配比、每条含确定性判定描述、中英文均有覆盖,校验输出汇总表。
2. **Given** 020 既有 32 条, **When** 并入回归层, **Then** 逐条保留原 should_trigger 语义,无语义漂移。

### User Story 4 — 三工具自动 runner:批跑、判分、报告 (Priority: P1)

用三个真实 agent CLI 作评测 runner(claude / codex / opencode2 的非交互模式),对数据集全量批跑,产出**工具 × case** 判定矩阵与汇总报告(各模块触发率/误触发率,按工具分解)。判分器输入为 runner 原始输出(含工具调用痕迹),输出确定性判定;同一输出重跑判分结果必须一致。

**Why this priority**: 直接解除 020 T036–T038 blocker;飞轮的执行机构。三工具即三个独立"模型+宿主"组合,天然暴露 skill 文本在不同宿主解析下的触发差异。

**Independent Test**: 对固定数据集快照,单工具全量跑通并产出报告;对同一份历史输出,判分器两次运行判定完全一致。

**Acceptance Scenarios**:

1. **Given** 数据集与 runner 配置, **When** 发起批跑, **Then** 每条 case 记录原始输出 + 判定,失败的 case 不中断批跑,最终输出汇总报告。
2. **Given** 任一工具 CLI 不可用或配置失效, **When** 批跑, **Then** 该工具标记为 runner-unavailable 并继续其余工具,报告如实区分"工具不可用"与"case 失败"。

### User Story 5 — 数据飞轮:失败归档 → skill 修订 → 全量重跑 (Priority: P2)

飞轮闭环:批跑报告 → 失败案例归档(分类:false-negative 漏写漏读 / false-positive 过度触发 / wrong-op 触发但操作错 / wrong-report 触发但没告知)+ root cause 归因(触发词缺失?description 过窄?正文自相矛盾?宿主特有行为?)→ SKILL.md 修订(三面同步:description、正文、references,版本号 bump,沿 020 契约纪律)→ **数据集全量重跑**(非只跑失败例)→ 前后对比入档。本 feature 交付期内至少完整转一圈并留下量化改善证据;失败例修复后**回填进数据集**成为永久回归资产。

**Why this priority**: P2 而非 P1——需要 US1–US4 先存在;但它是"持续优化"诉求的机制本体,交付期内必须至少转一圈以证明飞轮可转。

**Independent Test**: 飞轮一圈的完整工件链:基线报告 → 失败归档(root cause 标注)→ skill 修订 diff → 重跑报告 → 前后对比结论。

**Acceptance Scenarios**:

1. **Given** 基线报告存在失败案例, **When** 归档归因, **Then** 每例有失败类型 + root cause 分类,无"未分类"残留。
2. **Given** skill 修订提交, **When** 全量重跑, **Then** 修订前失败的目标案例改善,且原有通过案例的回归量被报告(数据集只增不减保证可查)。

### User Story 6 — 安装目录规范正本 + README 统一 + 三工具安装实测 (Priority: P2)

以三工具当前版本实测为准(codex 0.150.1 / opencode2 beta / claude 2.1.251),核对各自真实扫描的 skills 目录,确立"**标准共享目录优先,私有目录不重复安装**"正本:codex、opencode 原生扫描标准目录(`~/.agents/skills/` 用户域、`<repo>/.agents/skills/` 项目域)的,只装标准目录一份;仅 claude code 只认自家 `~/.claude/skills/`,用符号链接指向共享拷贝。README.md / README.zh-CN.md / install.md 三处一致;明示"不要再往 ~/.codex/skills 等私有目录拷贝"。三工具安装后各发现**恰好一份** engram skill,并各完成一次隐式写入冒烟(US1 场景 1 的最小版)。

**Why this priority**: 用户明确要求的交付物;且 runner 环境(US4)本身就要求三工具安装正确无重复,二者共用一次实测。

**Independent Test**: 三工具逐一刻画"发现的 engram skill 路径列表",断言每工具恰好一份且路径符合标准目录策略;隐式冒烟各通过一例。

**Acceptance Scenarios**:

1. **Given** 按正本命令安装, **When** 在三工具中列举已发现的 engram skill, **Then** 每工具恰好一份,路径在标准共享目录或其符号链接,无私有目录重复拷贝。
2. **Given** 已安装 skill 的三工具, **When** 各发送一条隐式透露 prompt, **Then** 至少一款工具完成"触发+写入+告知"冒烟(其余工具如因宿主限制失败,如实记录差异,不静默)。

## Edge Cases

- **纠错更新 vs 追加**:用户纠正旧值时,新条目应使旧条目失效/被取代,读取侧不得新旧混答(写入侧判"更新语义",读取侧判"不给出已被取代的值")。
- **他人事实归属**:"我同事张三周末爬山"——可记为关于张三的事实,但读取侧不得把张三的属性当用户本人属性(evidence-guidance 身份匹配原则沿用)。
- **时间限定事实**:"这周我在赶 A 项目"(transient,不记)vs"我每个季度末都要发版"(周期性,记)。数据集两类都要有。
- **假触发词陷阱**:"帮我写个脚本把这个记住在文件里"(文件操作,非 engram)、"这个数据库的 memory 参数怎么调"(RAM 近邻,020 负例层已有,保留)。
- **一次透露多条**:一句话里两个 durable fact("我用 Neovim,时区柏林")——期望至少各写一条或一条合并含两者;判定描述需预先写死可接受形态。
- **读取侧空结果**:查了没有 → 如实说没存,不编造、不抱怨用户(US2 场景 2)。
- **skill 触发但 MCP/CLI 均不可用**:沿 020 blocked 契约如实报告缺失条件,不模拟调用——隐式场景同样适用。
- **中英混合单句**:"以后 deploy 都走 gh-actions 吧"——按 durable fact 处理,语言不改变判定。
- **宿主差异**:同一 prompt 三工具触发不一致是**预期发现**而非 runner bug;报告按工具分解,skill 修订以"三工具一致通过"为收敛目标,单工具持续异常则归档为宿主限制并如实记录。

## Requirements *(mandatory)*

### Functional Requirements

**触发契约(skill 行为,跨工具一致)**

- **FR-001**: 隐式 durable-fact 场景(US1 正例类全谱)MUST 触发 engram skill、完成恰好一次写入、并在同一轮回复中告知所记内容;MUST NOT 先请求确认再写。
- **FR-002**: 以下场景 MUST NOT 写入:一次性任务上下文与明确 transient 表述;用户明确拒绝记录;秘密/凭证(沿 020 秘密契约);与用户/项目持久事实无关的通用讨论;他人事实被误记为用户本人属性。
- **FR-003**: 与用户/项目持久事实相关的问题或任务 MUST 在回答/行动前执行 engram 检索,并以检索证据为答;检索为空 MUST 如实报告未存。
- **FR-004**: 与持久事实无关的问题 MUST NOT 发起 engram 检索。
- **FR-005**: 020 既有 32 条显式/近邻触发用例的期望行为 MUST 保持不变(回归层)。
- **FR-006**: skill 契约变更 MUST 三面同步(SKILL.md description、正文、references/contract.json)并 bump skill 版本号;修订后的 skill 包 MUST 通过 020 既有的包校验(行数/引用一致/digest)。

**数据集与判分**

- **FR-007**: 数据集 MUST 版本化且只增不减;MUST 含四模块(implicit-write-pos / implicit-write-neg / implicit-read-pos / implicit-read-neg)并并入 020 的 32 条为回归层;中英文 MUST 均有覆盖;每条 case MUST 声明可从 runner 可观察输出确定性判定的期望。
- **FR-008**: 数据集 MUST 通过结构校验(模块齐备、正负配比达预注册阈值、判定字段完备、无重复 case)。

**runner 与飞轮**

- **FR-009**: runner MUST 以非交互模式驱动 claude / codex / opencode2 三工具,支持按数据集批跑、逐 case 保存原始输出与判定、失败不中断;单工具不可用 MUST 标记 runner-unavailable 并不影响其余工具。
- **FR-010**: 判分器 MUST 确定性(同一输出重复判分结果一致),MUST 区分 false-negative / false-positive / wrong-op / wrong-report 四类失败。
- **FR-011**: 飞轮 MUST 闭环:失败归档(含 root cause 分类)→ skill 修订 → 数据集**全量**重跑 → 前后对比报告;修复的失败例 MUST 回填数据集。
- **FR-012**: runner 与判分 MUST 只使用维护者既有授权渠道(claude glm_w / codex tf / opencode2 配置),MUST NOT 引入新的按调用付费云依赖(宪法 I、V)。

**安装目录与文档**

- **FR-013**: 安装正本 MUST 确立"标准共享目录优先"策略:原生扫描标准目录的工具只装标准目录一份;仅认私有目录的工具(claude code)以符号链接指向共享拷贝;MUST 明示禁止向私有目录重复拷贝。
- **FR-014**: 安装策略 MUST 以三工具当前版本实测发现行为为依据(不引用过时文档),README.md、README.zh-CN.md、install.md 三处 MUST 一致。
- **FR-015**: 三工具安装实测 MUST 断言:每工具恰好发现一份 engram skill,路径符合 FR-013 策略。

**范围不变式(硬边界)**

- **FR-016**: 本 feature MUST NOT 修改引擎代码(memory/ embedding/ provider/ store/ internal/)——只动 skill 包、评测数据集、runner、文档;若发现需要引擎新公共入口, MUST 停下按宪法 II 作为显式引擎契约增量上报,不得绕过。
- **FR-017**: 评测路径 MUST NOT 触碰 LoCoMo/LongMemEval 基线链路(skill 不进 locomo-bench),宪法 IV 的重跑义务因此不触发;此判断 MUST 在交付报告中显式陈述。

### Key Entities

- **TriggerCase**:id、模块(implicit-write-pos/neg、implicit-read-pos/neg、regression)、语言、场景类别、prompt、期望判定(expected_trigger + 期望可观察形态,如"发生一次 write 调用 + 当轮含告知")、来源(初始构造 / 飞轮回填)。
- **RunReport**:runner 工具标识、数据集版本、逐 case(原始输出定位、判定、失败类型)、按模块×工具汇总率。
- **FailureArchive**:case id、失败类型(false-negative / false-positive / wrong-op / wrong-report)、root cause 分类(触发词缺失 / 契约过窄 / 契约矛盾 / 宿主特有)、修复指针对应的 skill 版本。

## Success Criteria *(mandatory)*

### Measurable Outcomes

1. **SC-1 隐式写入正例通过率**(触发 + 恰好一次写入 + 当轮告知,三工具合并):修订后 ≥90%;首轮基线实测如实记录,不限门。
2. **SC-2 隐式读取正例通过率**(先查后答 + 空结果如实):修订后 ≥90%。
3. **SC-3 负例误触发率**(write-neg + read-neg 合并,三工具合并):≤10%。
4. **SC-4 显式回归**:020 的 32 条:正例加载率 ≥90% 且负例误加载率 ≤10% 保持不降。
5. **SC-5 飞轮实证**:交付期内 ≥1 完整圈(基线 → 归档 → 修订 → 全量重跑 → 对比),修订前失败案例改善且回归量被量化报告。
6. **SC-6 三工具安装实测**:每工具恰好一份 engram skill;≥1 工具完成隐式写入冒烟,其余工具结果如实记录。
7. **SC-7 判分确定性**:同一历史输出重判两次,判定完全一致;判分零人工酌情项。

## Assumptions

- 隐式直写 + 当轮告知契约已获维护者授权(2026-08-29 拍板,见方向变更声明)。
- 三个 CLI 的非交互模式可从输出确定性判定 skill 加载与 engram 调用(plan 阶段核实各工具的具体输出通道;若某工具无法暴露调用痕迹,该工具降级为"仅冒烟不判分"并如实记录,不伪造判定)。
- 三工具的模型端点(glm_w / tf / opencode2 各自配置)增量成本可忽略,已获维护者默许用于批跑。
- engram skill 触发后的写入/检索语义、evidence-guidance v3、秘密契约、namespace 纪律均沿 020 不变;本 feature 只改**何时触发**与**飞轮机制**,不改触发后的操作契约。
- 评测判定所需的 MCP/CLI 在 runner 环境可用(MCP server 本地启动或 CLI 装好;具体接入方式属 plan 细节)。
