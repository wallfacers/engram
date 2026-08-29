# Implementation Plan: engram skill 隐式记忆触发 + 三工具数据飞轮(048)

## Summary

解除 020 的显式触发封锁:engram skill 增加隐式写入(直接写+当轮告知)与隐式读取(持久事实相关问题先查记忆)契约;构建四模块刁钻场景触发数据集;用三个真实 agent CLI(claude / codex / opencode2 非交互模式)作自动 runner,批跑判分、失败归档、修订 skill、全量重跑——交付一个可持续转动的数据飞轮,并顺手完成三工具 skills 目录规范实测与 README 安装正本统一。引擎零改动。

## Technical Context

**(已于 2026-08-29 本机实测的事实,非文档转述)**

三工具非交互事件流通道(判分依据):

| 工具 | 版本 | 非交互调用 | 输出 | 实测 |
|---|---|---|---|---|
| claude | 2.1.251 | `claude --settings ~/.claude/settings.json.glm_w -p "<msg>" --output-format stream-json --verbose` | JSONL 事件流(result + 中间 tool_use 事件) | ✅ "ok" 返回,GLM 代理工作 |
| codex | 0.150.1 | `codex -p tf --yolo exec --json "<msg>"` | JSONL(thread/turn/item 事件,item 含 agent_message、tool_call) | ✅ "ok" 返回 |
| opencode2 | v0.0.0-beta-18600 | `opencode2 run --format json "<msg>"` | JSONL(part 流:text/step_finish/tool part) | ✅ "ok" 返回 |

- **触发判定锚点 = engram 操作痕迹,不是"skill 加载"事件**:MCP tool_call 名称以 `memory_`/`mcp__engram__` 前缀出现,或 CLI 进程调用 `engram <cmd>`。skill 加载本身在三家输出里暴露程度不一,而操作痕迹三家都有且不可伪造(宿主真实执行了调用)。正例判"期望操作发生",负例判"无任何 engram 操作"。
- **双重验证**:写入类 case 除输出事件外,跑完后用 engram CLI 直接查专用 store,确认条目真实落盘(防"报告了但没写");读取类 case 的前置记忆用 CLI 直接种子(不依赖模型写入)。
- **评测环境全离线**:engram-mcp stdio + 专用 `--data-dir`,不配 embedding/LLM(写侧 `memory_write`、读侧 keyword+entity 降级检索均不需要;判的是"是否触发操作",不是"检索质量"——宪法 I)。MCP 未配 LLM 时 `memory_ingest` 等工具不出现,不影响本评测。
- **skill 安装(飞轮关键)**:`npx skills@1.5.20 add ./skills/engram --global` 默认 Symlink → `~/.agents/skills/engram → <repo>/skills/engram`,**修订源目录即生效**,无需每圈重装;claude 侧 `~/.claude/skills/engram → ~/.agents/skills/engram` 二级链接。opencode2 beta 对 symlink 与目录扫描的实际行为待 P1 实测;若某工具拒 symlink,该工具用 Copy 模式并把"同步重装"纳入 runner 圈循环。
- **隔离**:per-tool data-dir(三工具互不污染,且可对账各工具实际写了什么);每轮评测前清空重建;namespace 用 skill 内契约默认值,同时检验 skill 的 namespace 报告纪律。
- **权限**:codex `--yolo` 全批;claude `-p` 配 `--allowedTools 'mcp__engram__*'`+bash 白名单(只放行 engram CLI);opencode2 `--auto`。

**runner 形态**:新增 `cmd/skill-eval`(Go,CGO=0,纯 adapter 侧工具——只 spawn 三个外部 CLI、解析 JSONL、判分、出报告;不 import 引擎包之外的任何引擎内部)。它是数据飞轮的持续基础设施,不是一次性脚本;放 `cmd/` 与 `locomo-bench` 并列。

## Constitution Check

1. **Local-first/offline** ✅ 评测链零 embedding/零 LLM server 依赖;skill 契约不引入在线能力。
2. **Engine/adapter separation** ✅ `git diff --name-only -- memory embedding provider store internal` 必须为空;runner 在 `cmd/skill-eval`,skill 包在 `skills/engram`;若发现需要引擎新入口,停下升级为显式契约增量。
3. **Contract-first** ✅ skill 行为契约变更(隐式直写+告知)在 spec FR-001..006 冻结;SKILL.md/reference/contract.json 三面同步 + skill 版本 bump;MCP/CLI 外部契约不变。
4. **Eval regression gate** ✅ 本 feature 不触碰检索/抽取/curation/storage/embedding 代码;skill 不进 locomo-bench 路径。不变式以"引擎测试全绿 + parity goldens 不动 + git diff 引擎为空"证明,不重跑 LoCoMo(在交付报告显式陈述)。
5. **Graceful degradation & honest scale** ✅ 判分如实区分 runner-unavailable / case 失败 / 宿主限制;三工具触发差异如实按工具分解报告,不混合不美化。

**No-paid-cloud 死规** ✅ 三 runner 全走维护者既有渠道(glm_w / tf / opencode2 内置配置),零新增云依赖。

## Project Structure

### Documentation (this feature)

```
specs/048-implicit-memory-flywheel/   spec.md ✅ plan.md tasks.md validation-report.md
skills/engram/references/install.md   三工具目录实测后修订(标准目录优先正本)
README.md / README.zh-CN.md           安装段与 install.md 对齐
docs/guides/skill-eval.md             runner 使用指南(飞轮操作手册)
```

### Source Code (repository root)

```
cmd/skill-eval/            新增:三工具批跑 runner + 确定性判分器 + 报告器
skills/engram/             SKILL.md(隐式契约)、references/contract.json(版本 bump)
skills/engram/evals/       implicit-write.json、implicit-read.json(新);trigger-evals.json(回归层,内容不动)
```

## 关键技术决策(plan 冻结)

1. **判定锚 = engram 操作痕迹 + store 落盘二次验证**(上文);skill 加载事件只作辅助诊断字段,不进判分。
2. **飞轮第一圈的顺序**:先用**现状 skill(显式契约)跑全量基线**——预期隐式正例大面积失败(false-negative),这一步同时证明用户反馈真实可复现并留下对照数;然后修订 SKILL.md 隐式契约,全量重跑,前后对比 = 飞轮第一圈的量化证据。
3. **数据集进 skill 包**(`skills/engram/evals/`),沿 020 纪律:结构校验器内置正负配比门;`trigger-evals.json` 原样并入为 regression 模块(内容零改动)。飞轮回填的新 case 写进对应模块 JSON(只增不减)。本地迭代期不发布 tag;digest/版本冻结只在最后发布步(020 发布纪律)。
4. **SKILL.md 修订面**:description 从"whenever a user asks to remember"扩展到"whenever the conversation reveals a durable user/project fact …record it proactively and report in the same turn; when a task depends on remembered facts, search first";正文加"隐式触发边界"一节(durable fact 判定类目 + 不写类目 + 当轮告知义务 + 更新旧值语义);020 既有操作契约(preflight、surface 选择、安全边界、报告格式)不动。103 行预算与 1,100 token 量级需重核(skill-creator 指南),超了就把细节移入 references 并加一跳引用。
5. **每工具调用参数固化进 runner 配置**(claude/codex/opencode2 的 flags、settings、权限白名单、超时),不在数据集里重复;超时默认 180s/case,单工具串行、三工具并行(三家端点独立,无互相争抢)。
6. **失败归档即 runner 输出物**:`failures.jsonl`(case id、失败类型四分类、原始输出定位、root cause 字段先留空由人工/分析步填),不是事后手工整理。

## Phases

- **P1 环境与目录实测**:构建 CLI+MCP;三工具按"标准目录优先"安装 skill(symlink 链);三工具 MCP 接入 engram-mcp(专用 data-dir);逐工具列举"发现的 engram skill 路径 + engram 工具列表",固化目录行为事实;修正 install.md/README/zh-CN。→ 交付 SC-6 前半 + 任务#2。
- **P2 数据集**:四模块 case 构造(中英双语、刁钻边界全谱、每条含确定性期望);`trigger-evals.json` 并入;结构校验器(配比门)。→ SC 前置。
- **P3 runner**:`cmd/skill-eval` TDD:JSONL 解析(三家格式)、判分器(四类失败)、store 二次验证、报告(failures.jsonl + 汇总);单工具冒烟→三工具全量跑通。→ FR-009..012。
- **P4 飞轮第一圈**:现状 skill 全量基线 → 失败归档 → SKILL.md 隐式契约修订(三面同步+版本 bump)→ 全量重跑 → 对比报告(按模块×工具)。→ SC-1..5。
- **P5 收尾**:skill 包校验器过门(digest/行数/引用一致)、README 三处一致终检、引擎零改动 diff 证明、validation-report、(可选,维护者授权后)发布 tag。

## Complexity Tracking

| Metric | Value |
|---|---|
| ALTER files (skill/docs) | ~8 |
| ADD files (runner+dataset) | ~10 |
| DELETE files | 0 |
| Estimated harness LOC (Go) | ~600-800 |
| Dataset size (case 数) | ≥120(四模块各≥24 + 回归32) |
| 飞轮圈数 | ≥1 全量(P4) |
| 引擎改动 | 0(硬门) |
