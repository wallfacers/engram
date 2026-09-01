# Feature Specification: engram skill 隐式记忆触发 + 三工具数据飞轮

**Feature Branch**: `048-implicit-memory-flywheel`

**Created**: 2026-08-29

**Status**: Tasks generated; formal lifecycle/remediation pending re-analysis（2026-09-01）

**Input**: User description: "有用户反馈有的时候,没记录记忆,你要做的是,弄一个数据集,去测试一些比较刁钻的场景,记录记忆和读取记忆的场景,而不是得用户显示的提示记录下,去读取记忆这样的提示采取用skill,你得搞数据飞轮的方式不断优化这个记录记忆skill" + 附加任务 "在 opencode v2 / codex / claude code 等工具上测试(claude --settings ~/.claude/settings.json.aly_qwen_w / codex -c model_provider=aq -c model=qwen3.8-flash --yolo / opencode2);codex、opencode 这类支持标准 agents/skills 目录的工具优先用标准目录,避免在 ~/.codex/skills 这类私有目录重复安装;修改 README 安装说明" + 维护者拍板(2026-08-29):隐式记录采用**直接写 + 当轮告知**契约。

## 背景与问题定位

### 用户反馈与根因

用户反馈"有的时候没记录记忆"。根因不是引擎丢数据,而是 **skill 触发契约把隐式场景整体排除**——020 交付的 `skills/engram/SKILL.md` 三处显式立场:

1. description: "Use … whenever a **user asks to** remember/记住, recall/召回 …" —— 触发词全部锚定显式请求;
2. 正文第一段: "Use this skill only for a request about durable engram memory … **Activation never makes ordinary conversation persist automatically**";
3. §3: "`write` … require **explicit user intent** … **generic conversation is not**"。

用户在对话中自然透露稳定事实("我以后都用 Neovim 写 Go 了,别再给我 VSCode 建议")时,skill 不触发、agent 不写 —— 记忆凭空丢失。读取侧同理:后续会话问"我上次定的事务隔离级别是什么",agent 不会主动查记忆,除非用户明说"查记忆"。

### 020 遗留 blocker:飞轮缺失

020 validation-report T036–T038 记录:"no local/existing-flat-rate agent runner is recorded … The held-out trigger rates therefore **cannot be claimed**"。触发评测数据集(32 条:16 显式正例 + 16 近邻负例)从未被真实 runner 跑过,SC-009 的 90%/10% 门从未实测。**没有 runner 就没有飞轮**:失败案例不可发现、skill 修订不可验证、回归不可防护。

现状三要素已齐,飞轮可建:(a) 三个可非交互驱动的 agent CLI 已就位——2026-08-29 当时实测为 claude 2.1.251(`--settings ~/.claude/settings.json.aly_qwen_w`,GLM 代理)、codex-cli 0.150.1(`-c model_provider=aq -c model=qwen3.8-flash --yolo`,Profile V2 modelflare)、opencode2 v0.0.0-beta-18600;版本会漂移,正式 run 必须在启动时重新采集;(b) 既有授权渠道均为包月/中转,增量成本可忽略;(c) 020 已有 32 条触发用例可作回归层。

### 安装目录重复:本机实证

维护者指出 codex/opencode 支持标准 skills 目录,应优先用标准目录、避免私有目录重复安装。本机实证:`~/.agents/skills/`(标准共享目录)与 `~/.codex/skills/`、`~/.config/opencode/skills/` 中 gsap 系列、find-skills 等**同时存在多份拷贝**——重复安装问题真实发生。install.md 已写"never copy it into additional client directories by hand",但 README 安装段与各工具当前版本(尤其 opencode v2 beta)的实际扫描契约需实测核对后统一正本。

### 方向变更声明(推翻 020 既有立场,需显著记录)

020 spec.md:"若它改变语义或制造隐式写入,反而会降低用户对持久记忆的信任"——该立场于 2026-08-29 由维护者基于用户反馈推翻:**隐式场景应直写并在当轮告知**。信任风险由新护栏替代旧禁令:(1) durable-fact 边界精确化(见 FR-002);(2) 刁钻负例集 + 误触发率硬门(≤10%);(3) 当轮告知保证可发现、可纠正。020 spec 不改写,以本 spec 为准。

## Clarifications

### Session 2026-08-31

- Q: 现有 172 条评测集与新增独立 holdout 应如何计入正式分数? → A: 采用双正式分数:现有 172 条保留为正式 `dev/regression score`,新增封存 holdout 作为正式 `generalization score`;两者分别报告,禁止合并。
- Q: 封存 holdout 的最低规模是多少? → A: 96 条:四个 implicit 模块各 20 条,另有 16 条 trap。
- Q: 封存 holdout 如何控制生成来源偏置? → A: 不使用人工造题;Claude Code(`claude --settings ~/.claude/settings.json.aly_qwen_w`)、Codex(`codex -c model_provider=aq -c model=qwen3.8-flash --yolo`)和使用免费模型的 OpenCode2 各生成 32 条,由统一造题 prompt 驱动并记录可复现 provenance。
- Q: 不使用人工时,谁负责确认 holdout 的期望标签与判分规则? → A: 生成 case 的 CLI 不得自审;另外两条 CLI 独立盲审,二者一致才入库,分歧 case 作废并重生补足来源配额。
- Q: 两项正式分数应采用什么运行与重试口径? → A: 每个宿主、每个正式 split 独立全量运行 3 次,中位数为正式分数并保留全部原始结果;定向重试不得覆盖主分。

### Lane Unification Decision 2026-09-01

维护者拍板:claude lane 使用 `~/.claude/settings.json.aly_qwen_w`（阿里云百炼桥）,codex lane 使用 `-c model_provider=aq -c model=qwen3.8-flash --yolo`（Aliyun Bailian `aq` 供应商）——**两条 lane 的底层模型统一为 qwen3.8-flash**,以统一成本与缓存行为;OpenCode2 维持免费模型 lane。本文本中历史记录（Input 引言、2026-08-29/31 实测快照）保留原样,以本节为现行正本。注意:该决策与 holdout 批次"三 host resolved model 两两不同"不变式存在张力,见 data-model §4 的对应修订。

### Plan Gate Resolution 2026-08-31

维护者选择 A:两个正式 split 采用相同的适用门槛,并以**每个宿主各自的 3-run 中位数**验收;跨宿主汇总只能作参考,不得掩盖任一宿主未达标。`SC-4` 的 020 显式回归门只适用于含该模块的 `dev/regression`;holdout 的 trap 正/负例分别沿用正例 `≥90%`、负例误触发 `≤10%` 的门槛。

现有 172 条冻结为不可变的正式 `dev/regression core`;飞轮后续回填以 append-only extension 保存并全量回归,但不得静默改变 172 的正式分母。这样同时满足“172 条作为正式分数”与“数据只增不减”。

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

正式评测采用双 split:现有 172 条冻结为正式 `dev/regression core score`,飞轮新增题进入单独 extension manifest;另建 96 条封存 holdout 作为正式 `generalization score`。holdout 由 Claude Code、Codex、OpenCode2 三条独立 authoring lane 各生成 32 条;作者不得自审,另两条 lane 对标签与机器判分规则盲审一致后方可入库。

每条 case 的期望判定必须**从 runner 可观察输出确定性推出**(skill 是否加载、是否发生 engram 调用、调用类型、是否告知),不允许"回答质量"式主观判分。数据集只增不减(防回归),版本号随内容演进。

**Why this priority**: 数据集是飞轮的燃料与防护栏;没有它,skill 修订是盲改。

**Independent Test**: 数据集 JSON 结构校验(模块覆盖、正负均衡、双语占比、判分字段完备)通过;全部 admitted holdout case 的期望判定在入库前由另外两条非作者 CLI 独立盲审并得到一致结论(FR-008 全量口径;抽样仅可作为入库后的附加核验,不构成验收本身),不引入人工执行者或仲裁。

**Acceptance Scenarios**:

1. **Given** 数据集生成, **When** 运行结构校验, **Then** 四模块齐备、每模块正/负例数满足预注册配比、每条含确定性判定描述、中英文均有覆盖,校验输出汇总表。
2. **Given** 020 既有 32 条, **When** 并入回归层, **Then** 逐条保留原 should_trigger 语义,无语义漂移。
3. **Given** 172 条 immutable `dev/regression core`、append-only dev extension 与 96 条封存 holdout 均存在, **When** 生成正式报告, **Then** 分别输出 core172 `dev/regression score` 与 holdout96 `generalization score`;extension 单列为非 headline 全量回归,不得改变 core 分母或合并为单一分数。
4. **Given** 某 authoring lane 生成 holdout candidate, **When** 另外两条 lane 独立盲审, **Then** 仅双审一致的 candidate 入库;分歧 candidate 作废并重生,最终三条 lane 各贡献 32 条合格 case。

### User Story 4 — 三工具自动 runner:批跑、判分、报告 (Priority: P1)

用三个真实 agent CLI 作评测 runner(claude / codex / opencode2 的非交互模式),对数据集全量批跑,产出**工具 × case** 判定矩阵与汇总报告(各模块触发率/误触发率,按工具分解)。判分器输入为 runner 原始输出(含工具调用痕迹),输出确定性判定;同一输出重跑判分结果必须一致。

**Why this priority**: 直接解除 020 T036–T038 blocker;飞轮的执行机构。三工具即三个独立"模型+宿主"组合,天然暴露 skill 文本在不同宿主解析下的触发差异。

**Independent Test**: 对固定数据集快照,单工具全量跑通并产出报告;对同一份历史输出,判分器两次运行判定完全一致。

**Acceptance Scenarios**:

1. **Given** 数据集与 runner 配置, **When** 发起批跑, **Then** 每条 case 记录原始输出 + 判定,失败的 case 不中断批跑,最终输出汇总报告。
2. **Given** 任一工具 CLI 不可用或配置失效, **When** 批跑, **Then** 该工具标记为 runner-unavailable 并继续其余工具,报告如实区分"工具不可用"与"case 失败"。
3. **Given** 一个工具与一个正式 split, **When** 计算正式分数, **Then** 执行 3 次独立全量运行并取中位数,保存三次原始结果与 provenance;定向重试仅作诊断,不得覆盖主分。
4. **Given** mutable `skills/engram` 与已验证的正式快照同时存在, **When** prepare 或运行 primary series, **Then** 只能装载带完整文件清单、package digest 与 immutable anchor 的 `FrozenSkillPackageSnapshot`;源目录后续修改不得改变、替换或冒充已评分快照。
5. **Given** holdout ordinal 1 已建立 binding 后 series 变为 `INVALID`, **When** 恢复正式评测, **Then** 新 series 必须重算出完全相同的稳定 `CandidateBindingV1` digest，但必须有新的 series manifest、fresh roots 与该新 manifest/core leg 专属的 `pre-holdout` attestation；先从零重跑 core172，再关联既有 binding 后重跑 holdout96 的三宿主 × 三 ordinal 完整矩阵，旧 series 的任何成功 run/manifest/pre-holdout receipt 不得拼入新主分。

### User Story 5 — 数据飞轮:失败归档 → skill 修订 → 全量重跑 (Priority: P2)

飞轮闭环:批跑报告 → 失败案例归档(分类:false-negative 漏写漏读 / false-positive 过度触发 / wrong-op 触发但操作错 / wrong-report 触发但没告知)+ root cause 归因(触发词缺失?description 过窄?正文自相矛盾?宿主特有行为?)→ SKILL.md 修订(三面同步:description、正文、references,版本号 bump,沿 020 契约纪律)→ **数据集全量重跑**(非只跑失败例)→ 前后对比入档。本 feature 交付期内至少完整转一圈并留下量化改善证据;失败例修复后**回填进数据集**成为永久回归资产。

**Why this priority**: P2 而非 P1——需要 US1–US4 先存在;但它是"持续优化"诉求的机制本体,交付期内必须至少转一圈以证明飞轮可转。

**Independent Test**: 飞轮一圈的完整工件链:基线报告 → 失败归档(root cause 标注)→ skill 修订 diff → 重跑报告 → 前后对比结论。

**Acceptance Scenarios**:

1. **Given** 基线报告存在失败案例, **When** 归档归因, **Then** 每例有失败类型 + root cause 分类,无"未分类"残留。
2. **Given** skill 修订提交, **When** 全量重跑, **Then** 修订前失败的目标案例改善,且原有通过案例的回归量被报告(数据集只增不减保证可查)。

### User Story 6 — 安装目录规范正本 + README 统一 + 三工具安装实测 (Priority: P2)

以运行时版本实测为准;2026-08-31 本机 plan preflight 为 codex-cli 0.149.1、opencode2 v0.0.0-next-16927、claude 2.1.251,不得继续把 2026-08-29 的版本号当成稳定前提。核对各自真实扫描的 skills 目录,确立"**标准共享目录优先,私有目录不重复安装**"正本:codex、opencode 原生扫描标准目录(`~/.agents/skills/` 用户域、`<repo>/.agents/skills/` 项目域)的,只装标准目录一份;仅 claude code 只认自家 `~/.claude/skills/`,用符号链接指向共享拷贝。README.md / README.zh-CN.md / install.md 三处一致;明示"不要再往 ~/.codex/skills 等私有目录拷贝"。三工具安装后各发现**恰好一份** engram skill,并均执行一次隐式写入冒烟(US1 场景 1 的最小版);至少一款工具必须完整通过,其余工具无论通过或因宿主限制失败都如实记录。

**Why this priority**: 用户明确要求的交付物;且 runner 环境(US4)本身就要求三工具安装正确无重复,二者共用一次实测。

**Independent Test**: 三工具逐一刻画"发现的 engram skill 路径列表",断言每工具恰好一份且路径符合标准目录策略;三工具均执行隐式冒烟并记录结果,其中至少一款完整通过。

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
- **FR-006**: skill 契约变更 MUST 三面同步(SKILL.md description、正文、references/contract.json)并 bump skill 版本号;修订后的 skill 包 MUST 通过 020 既有的包校验(行数/引用一致/digest)。正式校验 MUST 由 `skill-eval package validate` 生产：递归枚举完整 package、记录排序后的逐文件 digest 与 `engram-package-sha256-v1` package digest、调用并绑定既有 020 validator，再原子物化带 immutable anchor 的 `FrozenSkillPackageSnapshot`。每个 primary series 在 prepare 前 MUST 绑定该快照及其 `SkillPackageValidationReceipt`;receipt 的文件清单、snapshot/package/validator digest 与 anchor 必须匹配实际被评快照。draft、mutable source、缺失/失败/不同 digest 的 receipt 或校验后才补做的 receipt 均不得进入正式运行。正式 series 与 evaluated CLI MUST 只使用 frozen snapshot；之后对 `skills/engram/**` 的任何改动是另一未评 revision，不得替换已评分快照或被宣称为同一正式 package。

**数据集与判分**

- **FR-007**: 数据集 MUST 版本化且只增不减;MUST 含四模块(implicit-write-pos / implicit-write-neg / implicit-read-pos / implicit-read-neg)并并入 020 的 32 条为回归层;中英文 MUST 均有覆盖;每条 case MUST 声明可从 runner 可观察输出确定性判定的期望。现有 172 条 MUST 冻结为不可变的正式 `dev/regression core score`;后续飞轮回填 MUST 进入 append-only dev extension 并做全量回归,但 MUST NOT 改变 core172 的正式分母。每个 sealed comparison 确认的 fail-to-pass 修复 MUST 恰好回填一个新 extension ID，并带 source/supersession lineage 与 manifest membership；不可修改 core payload。另建独立、封存且不参与 skill 调优的 96-case holdout,作为正式 `generalization score`:四个 implicit 模块各 20 条,另有 16 条 trap。holdout 还 MUST 使用预注册的 8 个闭集 scenario bucket(每桶 12、每 author 4、每桶 zh/en=6/6、10 implicit+2 trap)，并输出不计门的 author-lane/scenario bias slices；每个 bias rate cell MUST 同时报 numerator、denominator、独立 case 数和 low-N 标记；不得在同一 version 内按分数补样或改样。holdout 不使用人工造题;Claude Code(`claude --settings ~/.claude/settings.json.aly_qwen_w`)、Codex(`codex -c model_provider=aq -c model=qwen3.8-flash --yolo`)和使用免费模型的 OpenCode2 必须各生成 32 条,并使用统一、版本化的造题 prompt。每条 holdout case MUST 记录 authoring host、实际模型标识、脱敏配置摘要和造题 prompt digest;seal 前 author/reviewer batch 的三个 host 实际模型标识 MUST 可得、host 内稳定,不得以 host 品牌代替模型身份;三宿主 harness MUST 互异,底层模型允许相同(2026-09-01 维护者拍板三 lane 统一百炼 qwen3.8-flash),盲审独立性由宿主 harness 与 label-blind envelope 承担。dataset payload digest MUST 只覆盖 case files，completed manifest 的 digest 独立计算，禁止自引用。正式 holdout 执行 MUST 通过可复核的隔离 preflight:受测 CLI child 对完整 holdout root 与审查工件的 traversal/list/read、对并发 sibling case workspace、prior-case state 和 retired workspace 的 read 均被拒绝，而当前 materialized workspace 可读；若 `concurrency > 1`，每个 worker MUST 有独立的同等隔离边界。每个 formal case MUST 使用 disposable、从不复用的 HOME/XDG/cache/session/container state root；core 与 holdout allocator必须分离且 holdout roots 在其 leg 前从未使用。每个 denied probe 都必须有 controller-side target-existence/content/policy proof，不能把未创建目标的 `not-found` 当作隔离成功。formal 执行上下文还 MUST 与 author/review 的 state roots 隔离。仅把路径放在仓库外或单独设置一个不可读文件都不构成隔离证明。该 `generalization score` 表示未参与 skill 调优且会话/执行上下文隔离的合成 holdout 结果，MUST NOT 表述为任一底层模型从未生成或审核过这些 synthetic case。报告 MUST 分列两项分数且 MUST NOT 合并。
- **FR-008**: 数据集 MUST 通过结构校验(模块齐备、正负配比达预注册阈值、scenario 覆盖、判定字段完备、无重复 case)。core172 校验分 `pre-index`（legacy case 可尚无 `family_id`）与 family-aware 两阶段，后者必须绑定冻结 `DevFamilyIndex`。holdout 的 authoring CLI MUST NOT 参与该 case 的标签复审;其余两条 CLI MUST 从 recursively closed `BlindCandidateV1` 独立推导 module/language/scenario/category 与完整机器判分规则，unknown/alias/nested extension 必须 fail closed，并且**不得知道 author 提议的 expect/module/language/scenario/category/machine rules**。仅二者完整推导一致、且 controller 在提交后与私有 author candidate/四维 slot 原子比对一致的 case 可入库。author 自报 `family_id` 必须拒绝；最终 opaque family identity 只能由 controller 从 blind semantic projection 生成并在 admission CAS commit 后写入。reviewer-visible digest MUST 只覆盖 canonical de-labeled candidate projection；相同 blind projection 即使私有 label/rule/slot 不同也必须得到同一 digest。reviewer MUST 实际接收匿名、label-free dev/accepted-holdout `FamilySummaryPayload`，其 digest 必须绑定完整 source state/count/root/projection；不能只拿 digest，且 validator 必须能从完整 source state 一一重投影。accepted-family admission MUST 使用 append-only `AdmissionReceipt` CAS chain，陈旧摘要上的 review 不得入库。author/reviewer audit MUST 使用 append-only `AttemptStarted`/`AttemptTerminal` event chain，所有 receipt 与 attempt 一一 join；分歧 case MUST 作废并重生,直至每个 authoring host 各有 32 条合格 case。dataset manifest MUST 用显式 `payload_files` 证明 case-ID union/file digests，并冻结 canonical JSON 与 anchor preimage。dataset seal MUST 覆盖完整 event/admission chains 及所有已启动 attempt 的 stage-isolation receipt，不能借由遗漏 rejected/stale/failed attempt 隐藏模型漂移或隔离违规。盲审 envelope MUST 去除 author、author-specific quota slot、batch/source、authoring/review receipt、author 提议标签/规则与任何可推导作者 lane 的字段;它只能给出独立推导所需的匿名输入和已冻结 family 摘要。author/reviewer CLI child 还 MUST 只读自己的 ephemeral input workspace，不能 traversal/list/read private holdout root、generation audit、author receipt、prior review 或并发 sibling workspace；每个 denied probe 要有 controller target-existence proof，dataset seal MUST 绑定该 stage-isolation receipt，不能只依赖匿名 JSON schema。

**runner 与飞轮**

- **FR-009**: runner MUST 以非交互模式驱动 claude / codex / opencode2 三工具,支持按数据集批跑、逐 case 保存原始输出与判定、失败不中断;单工具不可用 MUST 标记 runner-unavailable 并不影响其余工具。每个工具 × 正式 split(`dev/regression`与`holdout`) MUST 独立全量运行 3 次;每个 run 的原始输出与环境 provenance MUST 留档,中位数为该工具、该 split 的正式分数。任何真实 holdout author/review/seal、正式 package snapshot、series prepare 或 holdout ordinal 1 开始前 MUST 存在对应 scope 的 passing `GreenTestReceipt`;receipt 必须绑定实际执行的固定测试 argv/output 与当前 runner/judge/validator/snapshot/series digests，缺失、失败、过期或 digest 漂移均 fail closed。`series prepare` MUST 自动完成最终 frozen snapshot/template/worker-slot 绑定的 staged-workspace canary，并把 receipt map seal 入 series manifest；每个 primary case MUST 绑定一个已预检 worker slot，实际 child identity/template/access boundary 不得漂移。diagnostic mode 也 MUST 显式接收并 honor `--concurrency`，但永远不能计入正式分。定向重试可作为诊断资料,但 MUST NOT 覆盖任何完成的主运行结果。`official-dual` 在 holdout binding 后变为 `INVALID` 时，recovery MUST 使用相同稳定 `CandidateBindingV1` digest 与新 series ID，但不得复用旧 manifest/runtime/pre-holdout receipt：它先从零跑完 core172 三宿主 × 三 ordinal，再建立绑定新 manifest/core completion 的 fresh `pre-holdout` attestation、关联既有 binding，最后从零跑完 holdout96 三宿主 × 三 ordinal；最终报告只能引用完整恢复 series，旧 series 仅保留为 binding-ledger 证据。
- **FR-010**: 判分器 MUST 确定性(同一输出重复判分结果一致),MUST 区分 false-negative / false-positive / wrong-op / wrong-report 四类失败。
- **FR-011**: 飞轮 MUST 闭环:失败归档(含 root cause 分类)→ skill 修订 → 数据集**全量**重跑 → 前后对比报告;修复的失败例 MUST 回填数据集。
- **FR-012**: runner 与判分 MUST 只使用维护者既有授权渠道(claude aly_qwen_w / codex tf / opencode2 配置),MUST NOT 引入新的按调用付费云依赖(宪法 I、V)。

**安装目录与文档**

- **FR-013**: 安装正本 MUST 确立"标准共享目录优先"策略:原生扫描标准目录的工具只装标准目录一份;仅认私有目录的工具(claude code)以符号链接指向共享拷贝;MUST 明示禁止向私有目录重复拷贝。
- **FR-014**: 安装策略 MUST 以三工具当前版本实测发现行为为依据(不引用过时文档),README.md、README.zh-CN.md、install.md 三处 MUST 一致。
- **FR-015**: 三工具安装实测 MUST 断言:每工具恰好发现一份 engram skill,路径符合 FR-013 策略。

**范围不变式(硬边界)**

- **FR-016**: 本 feature MUST NOT 修改引擎代码(memory/ embedding/ provider/ store/ internal/)——只动 skill 包、评测数据集、runner、文档;若发现需要引擎新公共入口, MUST 停下按宪法 II 作为显式引擎契约增量上报,不得绕过。
- **FR-017**: 评测路径 MUST NOT 触碰 LoCoMo/LongMemEval 基线链路(skill 不进 locomo-bench),宪法 IV 的重跑义务因此不触发;此判断 MUST 在交付报告中显式陈述。

### Key Entities

- **TriggerCase**:id、模块(implicit-write-pos/neg、implicit-read-pos/neg、regression)、语言、闭集 scenario bucket 与 bucket 内 category、prompt/多轮 session、期望判定(expected_trigger + 期望可观察形态,如"发生一次 write 调用 + 当轮含告知")、来源(初始构造 / 飞轮回填)、split(`dev/regression`或`holdout`)、score membership(`core172`/`dev-extension`/`holdout96`)与 family id;holdout 另含 authoring host、实际模型标识、脱敏配置摘要、造题 prompt digest、两个非作者 reviewer host 及其独立推导 label/novelty verdict。
- **CoreExecutionPlanReceipt**:与 skill/purpose 无关地冻结 core172 manifest、runner/judge、每 host 稳定 tool identity、timeout/concurrency、三 ordinal seeds、normalized worker identity/boundary 与 disposable-state child template；SC-5 before/after 引用同一个 seal。
- **FrozenSkillPackageSnapshot / SkillPackageValidationReceipt**:由唯一 package-validation producer 从 mutable source 原子生成；snapshot 含完整排序文件清单、逐文件 digest、package digest 与 immutable anchor，receipt 绑定既有 020 validator revision/result。任何 primary series 只评 snapshot。
- **GreenTestReceipt / WorkspaceCanaryReceipt**:前者在不可逆动作前绑定固定测试命令/输出与适用的 runner/judge/validator/snapshot/series digests；后者由 `series prepare` 自动以最终 frozen snapshot/invocation/cwd/materialization/worker-slot 执行并绑定可见性结果。两者均是正式 series seal 与 score eligibility 的输入。
- **RunReport**:runner 工具标识、实际模型标识、脱敏环境配置摘要、frozen-snapshot/skill/dataset/runner/core-plan/package-validation/green-test/protected-execution/workspace-canary digest、正式 split、run ordinal、逐 case(原始输出定位、state-isolation receipt、已准备 worker-slot/probe、判定、失败类型)、按模块×工具的单次结果与 3-run 中位数。`ToolProvenance.source_revision` 只覆盖 `cmd/skill-eval` runner source subtree/binary，不含 skill、dataset、spec/docs 或 artifacts。
- **FailureArchive**:case id、失败类型(false-negative / false-positive / wrong-op / wrong-report)、root cause 分类(触发词缺失 / 契约过窄 / 契约矛盾 / 宿主特有)、修复指针对应的 skill 版本、共同 core-plan digest 与 sealed before/after references；只接受 dev core receipt。

## Success Criteria *(mandatory)*

### Measurable Outcomes

在最终 runner/judge/CLI 契约冻结前采集的探索性 baseline 仅用于诊断,不属于正式 primary series,不得作为 SC-5 的 FailureArchive 或前后对比依据。SC-5 的可比 baseline 必须在最终 runner/judge/CLI 契约冻结后，以不可变的修订前 skill 快照和完整 core172 重新采集。所有正式 primary series 均使用以下冻结门槛;若需修改门槛,必须通过新的 spec 决策。

1. **SC-1 隐式写入正例通过率**(触发 + 恰好一次写入 + 当轮告知):每个宿主在 `dev/regression` 与 holdout 上各自的 3-run 中位数均 ≥90%。
2. **SC-2 隐式读取正例通过率**(先查后答 + 空结果如实):每个宿主在 `dev/regression` 与 holdout 上各自的 3-run 中位数均 ≥90%。
3. **SC-3 负例误触发率**(write-neg + read-neg 合并):每个宿主在 `dev/regression` 与 holdout 上各自的 3-run 中位数均 ≤10%。
4. **SC-4 显式回归与 trap 安全门**:`dev/regression` 中 020 的 32 条对每个宿主保持正例加载率中位数 ≥90% 且负例误加载率中位数 ≤10%;holdout 的 trap-read-pos 对每个宿主通过率中位数 ≥90%,trap 负例误触发率中位数 ≤10%。
5. **SC-5 飞轮实证**:交付期内 ≥1 完整圈(可比基线 → 归档 → 修订 → 全量重跑 → 对比),可比 baseline 必须是 core172 的三宿主 × 三 ordinal `dev-comparison` 完整 series；baseline 与 post-change core leg 必须引用同一 sealed `CoreExecutionPlanReceipt`，其中冻结 runner、judge、core 数据集、每 host 稳定 `tool_identity_digest`、timeout、concurrency、case-order seeds、normalized worker identity/boundary 与 disposable-state execution/isolation template。`captured_at`、purpose、series ID 与 unique artifact IDs 不是 identity 输入；skill package digest 是唯一有意变量。每个宿主 × case 以三次 terminal pass/fail 的二元中位数判 before/after 状态。至少一个来自冻结 baseline 的 median-fail case 必须在修订后转为 median-pass,且全部 median-pass→median-fail 回归量被量化报告;comparison 只能读取 sealed core172 receipts、不能触及 holdout plaintext/child receipt，extension 单列且不能替代 core172 对比。若没有任何 baseline 失败项改善,本标准判定 FAIL。
6. **SC-6 三工具安装实测**:每工具恰好一份 engram skill;≥1 工具完成隐式写入冒烟,其余工具结果如实记录。
7. **SC-7 判分确定性**:同一历史输出重判两次,判定完全一致;判分零人工酌情项。
8. **SC-8 双正式分数稳定性**:每个工具在 `dev/regression` 与 holdout 上均完成 3 次独立全量运行;两项正式分数均取各自 3 次结果的中位数,所有原始运行结果与 provenance 可追溯,且不以定向重试覆盖主分。holdout binding 后发生 INVALID 时，只有新 series 完整重跑两 split 后的单一 series 可产生正式报告；跨 series 拼接一律无效。
9. **SC-9 宿主独立验收**:SC-1～SC-4 的正式结论 MUST 按宿主分别判定;跨宿主汇总只能作为补充统计,任一宿主未达标时不得用合并平均值宣称三宿主整体通过。

## Assumptions

- 隐式直写 + 当轮告知契约已获维护者授权(2026-08-29 拍板,见方向变更声明)。
- 三个 CLI 的非交互模式应从输出确定性判定 skill 加载与 engram 调用。若某工具无法暴露正式判分所需调用痕迹,其 formal host × split series MUST 标记 `INVALID`,不得从 SC-8/SC-9 静默排除；"仅冒烟"只适用于 US6/SC-6 的安装发现验证,不能替代正式分数。
- 三工具的模型端点(aly_qwen_w / aq / opencode2 各自配置)增量成本可忽略,已获维护者默许用于批跑。
- engram skill 触发后的写入/检索语义、evidence-guidance v3、秘密契约、namespace 纪律均沿 020 不变;本 feature 只改**何时触发**与**飞轮机制**,不改触发后的操作契约。
- 评测判定所需的 MCP/CLI 在 runner 环境可用(MCP server 本地启动或 CLI 装好;具体接入方式属 plan 细节)。
