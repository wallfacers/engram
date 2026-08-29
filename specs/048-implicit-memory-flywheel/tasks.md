# Tasks: engram skill 隐式记忆触发 + 三工具数据飞轮(048)

**Generated**: 2026-08-29 | **Plan**: [plan.md](plan.md) | **Spec**: [spec.md](spec.md)

任务按 plan 五 Phase:环境目录实测 → 数据集 → runner → 飞轮第一圈 → 收尾。**红线**:引擎五目录(memory/ embedding/ provider/ store/ internal/)零改动;评测链全离线(无 embedding/无 LLM server);判分确定性(同输出重判一致);数据集只增不减;skill 契约三面同步 + 版本 bump;发布 tag 仅维护者授权;三工具调用参数固化在 runner 不进数据集;失败不中断批跑、runner-unavailable ≠ case 失败。

## Phase 1 · 环境与目录实测

- [x] T001 基线确认:`CGO_ENABLED=0 go build ./...`、`CGO_ENABLED=0 go test -count=1 ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空;`.specify/feature.json` 指向 048
- [x] T002 构建评测二进制到会话 scratchpad:`CGO_ENABLED=0 go build -o <scratch>/bin/engram ./cmd/engram` + `engram-mcp` 同理;`engram version` 冒烟
- [x] T003 三工具 skill 安装(标准目录优先):`npx skills@1.5.20 add <repo>/skills/engram --global`(Symlink 默认)→ `~/.agents/skills/engram` 共享拷贝;claude 侧确认 `~/.claude/skills/engram` 链接;逐工具列举"发现的 engram skill 路径",断言每工具恰好一份、无私有目录重复拷贝;**opencode2 beta 的目录扫描行为以实测为准**(若不扫 `~/.agents/skills` 则记录其实际发现路径并更新策略);symlink 不被某工具识别时该工具降级 Copy 模式并在飞轮圈加同步步骤
- [x] T004 三工具 MCP 接入:各工具配置 engram-mcp(stdio,`--data-dir <scratch>/skill-eval-data/<tool>`,无 embedding/LLM);确认 `memory_write`/`memory_search` 出现在各工具工具列表;权限通道确认(codex `--yolo`、claude `--allowedTools 'mcp__engram__*'`、opencode2 `--auto`)
- [x] T005 安装正本修订:`skills/engram/references/install.md` 按实测目录行为更新(标准共享目录优先、claude 唯一例外走 symlink、明示禁止向 `~/.codex/skills` 等私有目录重复拷贝);README.md / README.zh-CN.md 安装段同步

## Phase 2 · 数据集([P] 可并行)

- [x] T006 [P] 构造 `skills/engram/evals/implicit-write.json`:pos≥24(偏好/约束/身份/项目约定/长期决策+状态更新全谱;刁钻:间接表达、吐槽式、顺带一提、纠旧值、一次多条、中英混合)+ neg≥24(transient、RAM/cache 近邻、明确拒绝、秘密、他人事实、文件操作假触发、"仅本次对话");每条含 id/语言/场景类别/prompt/期望判定(确定性可观察形态)
- [x] T007 [P] 构造 `skills/engram/evals/implicit-read.json`:pos≥24(直接问持久事实、指代前情"按老规矩"、任务隐含约定、枚举型、空结果如实)+ neg≥24(纯技术题、时事、假设性、与用户无关);期望判定含"是否发生 memory_search 调用 + 空结果如实报告"
- [x] T008 结构校验器(Go,`cmd/skill-eval` 内置 `validate` 子命令):模块齐备、正负配比门(各模块 pos:neg ≥ 40%:40% 量级)、判定字段完备、id 唯一、中英覆盖非零;`trigger-evals.json` 以 regression 模块并入(文件内容零改动)

## Phase 3 · runner

- [x] T009 [P] JSONL 归一化解析层 + 单测:三家事件流(claude stream-json tool_use / codex item 流 tool_call / opencode2 part 流 tool part)归一为统一事件(engram_call{tool,namespace,可见参数} / assistant_text);fixture 用 2026-08-29 实测样本 + 构造样本;claude 无 MCP 时 tool_use 缺席的形态也入 fixture
- [x] T010 [P] 确定性判分器 + 单测:输入(归一事件 + store 二次验证结果)→ 判定 pass/fail + 四类失败(false-negative/false-positive/wrong-op/wrong-report);同输入重判一致(单测断言);读取类前置种子与空结果路径覆盖
- [x] T011 runner CLI(`cmd/skill-eval`):批跑(单工具串行、三工具并行、--timeout 180s/case、per-tool data-dir 清重建、读取类先 CLI 种子)、runner-unavailable 检测与降级、failures.jsonl 输出、汇总报告(模块×工具矩阵 + SC 口径率);冒烟:单工具 3 case 端到端
- [x] T012 三工具全量**基线跑**(现状显式契约 skill):产出基线报告(预期隐式 pos 大面积 false-negative——用户反馈的可复现证据)

## Phase 4 · 飞轮第一圈

- [x] T013 失败归档与归因:基线 failures.jsonl 逐条 root cause 分类(触发词缺失/契约过窄/契约矛盾/宿主特有);无"未分类"残留
- [x] T014 SKILL.md 隐式契约修订:description 扩展(自然透露 durable fact → 直写+当轮告知;持久事实相关 → 先查)+ 正文"隐式触发边界"节(类目+不写类目+告知义务+更新旧值语义)+ `references/contract.json` 版本 bump;020 操作契约不动;行数/token 预算重核(超限移 references);三面同步
- [x] T015 重跑 + 前后对比报告(模块×工具矩阵;维护者补充纪律:不全量补跑,失败 --only 单补 + --sample 抽样防回归);SC-1 codex✅/claude❌、SC-2 未达(Round-3 输入归档)、SC-3✅、SC-4 相对基线不降✅;见 validation-report + failbook
- [x] T016 修复的失败例回填数据集对应模块(只增不减,来源标记 flywheel-round-1)

## Phase 5 · 收尾

- [x] T017 skill 包校验器过门:020 既有 validator(Node 套件)对修订后 `skills/engram` 全绿(行数/引用一致/digest 重算);CI 快查
- [x] T018 全量门复核:`CGO_ENABLED=0 go build ./...` + `go test -count=1 ./...` + `go vet ./...` 全绿;`git diff --name-only -- memory embedding provider store internal` 为空(引擎零改动证明);README/install.md/zh-CN 三处一致性终检
[x] T019 交付文档:`specs/048-.../validation-report.md`(基线 vs 修订对比、SC 判定、宿主差异记录、宪法 IV 不重跑 LoCoMo 的显式陈述)+ `docs/guides/skill-eval.md`(飞轮操作手册:跑一圈的标准流程);维护者授权后按 020 发布纪律打 tag(未授权则记录候选 commit)

## Dependencies

```
T001 → T002 → T003 → T004 → T005
T006 ∥ T007 (依赖仅 T001;与 Phase 1 并行)
T008 (依赖 T006,T007)
T009 ∥ T010 (依赖仅 T001)
T011 (依赖 T002,T004,T008,T009,T010)
T012 (依赖 T011)
T013 (依赖 T012) → T014 → T015 → T016
T017 (依赖 T014) ;T018,T019 (依赖全部)
```
