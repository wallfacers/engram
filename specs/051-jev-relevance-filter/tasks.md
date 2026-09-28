# Tasks: Jev 候选相关性过滤（051-jev-relevance-filter）

**Input**: [spec.md](spec.md), [plan.md](plan.md), [research.md](research.md), [data-model.md](data-model.md), [contracts/relevance-filter.md](contracts/relevance-filter.md), [contracts/memory-search.md](contracts/memory-search.md), [contracts/write-gate.md](contracts/write-gate.md), [quickstart.md](quickstart.md), 互审记录见 [plan-review.md](plan-review.md) + [plan-review-2-relay-flash.md](plan-review-2-relay-flash.md) + [plan-review-2-mimo-flash.md](plan-review-2-mimo-flash.md)

**Generated**: 2026-09-23（双审 block 判定修复后）。

**Tests**: Required（TDD：先写失败测试再实现；全部离线可跑、`CGO_ENABLED=0`）。每条契约规则至少一条测试（contracts/ 为权威）。

**Scope guard**: 本期只做读过滤 + 写门（FR-008 明确不做清单为准）；引擎公开契约增量按 contracts/ 冻结文本实现，禁止顺手重构既有检索路径（parity 锁定）。

## Phase 0: 文档与治理（无代码，可先行）

- [ ] T001 将两份规范性设计文档（`docs/engram-probe-curation-design.md`、`docs/engram-环境核对记忆设计.md`）纳入版本库（当前 untracked）
- [ ] T002 [US1] 修订 `AGENTS.md` + `CLAUDE.md` 死亡条款（同一 commit）：Jev 过滤与写门面（读写双侧）获准 shipped/推荐/报涨分，记录修订缘由与生效范围（**仅限本面**，其他云打分模型仍受原条款约束）；历史 91.43% 归因不改口

## Phase 1: filter 包核心（纯离线，全部 TDD）

**Purpose**: 接口 + 策略 + Jev 客户端 + 写门，无 store 依赖，阻塞后续一切。

- [ ] T03 [P] [US1] `filter/filter.go` + `filter/filter_test.go`：`Candidate/FilterMeta/Policy/RelevanceFilter` 类型、`Select` 纯函数（p≥θ 截 KShowMax=12 → 空则 p≥0.35 截 3 → 仍空诚实空；pinned+trigger 命中注入优先占位计入 KShowMax、不计 Dropped、病态 override 注记；trigger 命中 = query 大小写不敏感包含）、`ENGRAM_JEV_RELAX=0` kill-switch 语义、`None{}` 实现、typed-nil 构造纪律（对标 `embedding.NewReranker`）
- [ ] T04 [P] [US4] `filter/gate.go` + `filter/gate_test.go`：`WriteGate` 接口、`GateDecision{Route: write|skip|defer_to_packet}`（本期只产生 write/skip，defer_to_packet 占位）、本地密钥形状预判器（短路：`user_requested`=true→write；否则 skip；**不发云**）
- [ ] T05 [US1] `filter/jev/jev.go` + `jev_test.go`（httptest）：pointer 模式请求形状（共享 `state{query,memories}` + 每记忆一道条件化 noul 题，指令含「对当前这个问题是否必要」）、分片并发 32–64/片 >64 条、任一片失败整体 `Degraded`（不部分采用）、整过滤 deadline 1s（ctx 派生）/单请求 min(剩余,1s)、不重试、30s 同桶负缓存、usage×单价计费（无 usage→0+unknown 注记）
- [ ] T06 [US4] `filter/jev` 写门实现 + 测试：四题判定（一次性/偏好/密钥/needs_probe 恒 0）、输入映射（`draft=content`、`user_turn` 缺省→draft-only 判定、`user_requested` 控密钥例外）、发云前密钥形状子串遮蔽、500ms fail-open（失败=无门）

## Phase 2: 引擎接线（memory/）

- [ ] T07 [US1] `memory/retriever.go` + 测试：`Result` 增补 `Pinned` 字段；`SearchFiltered(ctx, query, pool, show, flt, pol)` 组合（宽池 → Filter+Select → pinned 先/p 降序；`flt==nil` 等价 `Search(min(pool,show))`；截断归属：组合层截 show（降级/none）、Select 截 KShowMax/RelaxMax；**过滤失败→返回 Search 前 show 条 + Degraded=true 且 error==nil**，error 仅底层 Search 失败非空）
- [ ] T08 [US1] parity 锁定测试：`Search/SearchMulti/SearchWithDiagnostics` 签名与行为逐字节不变；降级展示集 ≡ C@8 语义（=Search 前 limit 条）

## Phase 3: MCP + CLI（A+B 双落地）

- [ ] T09 [US3] `mcpserver/config.go` + 测试：`ENGRAM_JEV_BASE_URL/MODEL/API_KEY`、`ENGRAM_JEV_THETA/RELAX_THETA/KSHOW_MAX/RELAX`、`ENGRAM_JEV_WRITE_GATE`、`ENGRAM_SEARCH_POOL/ENGRAM_FILTER`（服务端旋钮）；校验/夹取规则各一条测试（pool<limit→limit；theta∉(0,1)→拒绝；RelaxTheta>Theta→夹为 Theta；jev&pool==limit 正常过滤；pool ≤500 硬夹）
- [ ] T10 [US1] `mcpserver/tools.go`：`memory_search` +`candidate_pool`/`filter`/`theta` + 输出 `pool_size`/`filter{backend,theta,kept,dropped,degraded,latency_ms,cost_usd}`（omitempty）；**parity 不变式契约测试**（未传新参数且 `ENGRAM_SEARCH_POOL/FILTER` 未设 → 与今天逐字节一致、不输出新字段）；全灭两段式→`hits:[]`；失败/无 key→RRF 前 limit 条+degraded:true；离线零网络断言（filter="jev" 无 key → 0 网络请求）
- [ ] T11 [US4] `mcpserver/tools.go`：`memory_write` +可选 `user_turn`/`user_requested` + 写门 opt-in 接线（`ENGRAM_JEV_WRITE_GATE=1` 才启用；fail-open 测试：门失败=无门照写；密钥例外测试：user_requested→写入且不回显）
- [ ] T12 [US1] `cmd/engram-filter/`：thin CLI（stdin 候选+query → 短名单 JSON + 遥测），复用同一 filter 包；B 路径验证测试：CLI 输出与 MCP 路径同语义（共享包 parity）

## Phase 4: 评测 harness（cmd/locomo-bench，038 协议机器）

- [ ] T13 [US2] `multiquery.go retrieveWithQuotaDiagnostics → SearchFiltered → packer` 接线（波及 coverage/attribution/abstain_probe/eval_runner 全部调用方；`pcic_test.go` 固定元数断言更新）；`--top-k` 内部拆 pool-k/show-k；shown 以 packer 注入数为测量点
- [ ] T14 [US2] 四臂 recipe（A limit=8 / B k=150 全塞 / C 截断：门限 12+对照 8 / D Jev θ）+ **D-noRelax 变体**（`ENGRAM_JEV_RELAX=0`）；category-5 declared block（refusal-scoring、总体声明）；空注指标 = `hits == []`；recall@shown 逐题等预算 + C@12/C@8 双口径（evidenceRecallAt 接入）；B↔D 翻错题双向清单
- [ ] T15 [US2] 038 协议接入：四臂统一冻结 `--answer-input-cap=32768` + **B 无截断硬断言**（bundle≤cap 且 packer 未触顶，触顶 run 无效）；manifest 扩展 `filter_model` 槽 + 机制键 `filter.jev.v1` + θ/池预注册字段（sweep 仅诊断）；合法性工件（formal call journal / `evalArtifactValidity.isComplete` / B0 receipts / pilot+warm-up / 3-rep 同窗）；Jev 调用独立 `filter` 类进逐行 audit + usage/cost；run 内配对 + 精确 McNemar + 配对 CI（paired_eval.go），SC-001..007 总判定经 HOLD 进 `promotionVerdictFor`；`--estimate` 成本预估步骤
- [ ] T16 [US2] 指标输出：SC-001..007 全量判定文件（含 D-noRelax SC-004 双判据：≥C 且绝对下限 ≥50% 预注册）

## Phase 5: skill、验收与落账

- [ ] T17 [US3] `skills/engram/SKILL.md`：生产旋钮文档（`ENGRAM_SEARCH_POOL`/`ENGRAM_FILTER`，服务端 env 语义 +「未设时与今天一致」）；无 env parity 验收
- [ ] T18 [US1] quickstart §1–§4 全绿：`CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./...`（无任何 `ENGRAM_JEV_*` 全绿）
- [ ] T19 [US2] 四臂评测执行【运维触发：远程 GPU 评测箱，run-dir 全部 `/root/autodl-tmp/`，setsid detach，先 `--estimate`】→ 结果落 `docs/evaluation/results.md`（**LoCoMo 口径措辞**）+ `experiment-verdicts.md`；eval 配置提交与算法改动**分开**；不达标标 HOLD

## 依赖关系

- T01/T02 独立先行；T03/T04 并行；T05 依赖 T03；T06 依赖 T04+T05；T07 依赖 T03；T08 依赖 T07
- T09 → T10/T11（依赖 T06/T07）；T12 依赖 T05+T07；T13 依赖 T07；T14/T15 依赖 T13（T15 与 T14 可并行）；T16 依赖 T14+T15
- T17/T18 依赖 T09–T12；T19 依赖全部（人工/运维触发）

## 实现派发纪律（jev-pi-router）

代码全部派 flash 执行器（路由决策引擎先行）；每片完成跑 gate（`CGO_ENABLED=0 go build ./... && CGO_ENABLED=0 go test -count=1 ./<pkg>...`）；代码审跨厂商（glm 解锁前按维护者先例允许 flash 档降级，记档）；一 writer 顺序切片，禁止并发写同一工作副本。

**派发日志**：① 2026-09-23 派 opencode-go/deepseek-flash（T03–T06）→ **失败**：429 GoUsageLimitError（月度用量尽，无 reset 时间）→ `opencode-go` quota 封禁（vendor_failures）；按 fallback_order/vendor_failures 重决策后续派。
② 2026-09-23 切片 2（T07–T08）派 relay/cmd-deepseek-v4.1-flash（run 9f31fffa）→ **完成**（主会话独立复验绿：116/0 纯增量、parity 8/8、12 新测试）；run 标 failed = gate 误报（与切片 1 同模式）。7 条实现歧义已仲裁钉入契约 §4.5。
③ 2026-09-23 切片 3（T09–T12）派 deepseek/deepseek-flash（run c5eff7e8）→ **完成**（mcpserver 配置/memory_search/memory_write/engram-filter CLI，35 新测试 + 全仓绿）；run 标 failed = gate 误报。8 条歧义仲裁钉入（hits→results 命名统一、env 校验、惰性 theta、backend 诚实性、legacy 无 pool_size、结构性 fail-open、显式 none 走 legacy）。范围注记：registry/provider/cmd/engram-mcp 为合法 DI 接线，记档接受。
④ 2026-09-23 切片 4（T13–T16）派 deepseek/deepseek-flash（run bb5f3538）→ **完成**（locomo-bench 四臂 harness 6 新文件 +41 测试；`_arm.go`→`jevfilter_*.go` 地雷已排；主会话复验：engine 目录零触碰、locomo 测试绿；未跑付费评测）。9 条歧义仲裁（R7c）。⑤ 切片 5 = FilterMeta token 字段 + T17 skill 文档 + T18 验收（golden-byte parity 加固）。→ **完成**（run eb252b60，deepseek-flash：token 透传+journal 字段+SKILL.md 旋钮+golden-byte parity（变异检测证明非自比）；全仓 26 包绿；quickstart §4/§5 留给运维步）。⑥ 代码审（mimo-v2.6-pro，run 32f8577e）→ **block**（1 BLOCKER+5 MAJOR，见 code-review-1.md）；仲裁全采纳 → 切片 6 修复（含读路径遮蔽、空 draft 对齐、quota=0 钉死、freeze-before-digest 重排、partial-run 防 GO、flake 边距），修后原 reviewer resume 复核。

⑦ 收尾：closure 复核（run 0b10f8a9）**approve**（20/20 CLOSED、6 偏离 sound、4 测试非凑数）；全量 gate 过（vet + 26 包 0 FAIL）；n8 工件卫生修毕（code-review-1.md 裁为正式文本，closure 存 code-review-1-closure.md）；n7 裁决接受现状。**T02 完成**：AGENTS.md+CLAUDE.md 死亡条款修订（读写双侧）commit `c388baa` 于分支 `codex/051-jev-relevance-filter`（pre-commit hook 禁 master 直提，按其政策开分支；T01 设计文档随 docs commit 纳入）。n6 微修：router 选 qianwenai/deepseek-v4.1-flash 不在 Pi 注册表（解析失败）→ 同族改派 relay/cmd-deepseek-v4.1-flash（记档）。
⑧ 【T19 配置变更 · 维护者 2026-09-23】模型钉版更新（eval-config，独立于算法）：答题/抽取/判题 = **deepseek-flash**（DeepSeek-V4.1-Flash），端点 `https://api.deepseek.com/anthropic`（替换 deepseek-v4-flash；判题同款 clean judge 口径不变）；AutoDL 不租 84G，改租跑 **Qwen3.6** 的合适小卡；预算估算待补。
⑨ 【T19 启动 · 维护者 2026-09-23「开始跑分」】维护者提供：AutoDL 盒（vGPU-48GB×1，已装 SSH 免密）、DeepSeek 判题 key（**只进 env 通道，不在此记录**）。`--no-idk-retry` 偏离按「开始跑分」视为**已签核**（记档）。模型钉版：判题 = deepseek-flash @ api.deepseek.com/anthropic；答题/抽取/过滤 = Qwen3.6 @ 盒上 vllm。作战计划（flash 子代理分阶段执行）：
- **P0（主会话）**：凭证卫生（0600 env 文件于 ~/.engram-eval-secrets.env，仓库外；一次性密码装 SSH 公钥后全免密）✅
- **P1（flash）盒子 bootstrap**：磁盘纪律（系统盘只放码，venv/模型/缓存/日志 → /root/autodl-tmp）→ vllm + Qwen3.6（Instruct，选 48G vGPU 装得下且 --max-model-len 32768 可跑的最大档，bf16 不行用 AWQ/int4；ModelScope 优先）→ embedding 服务（照 docs/operations/evaluation/remote-gpu-runbook.md 复刻）→ 健康检查 → **filter 端点探测**（pointer 协议 1 次试探 DeepSeek /answers；不通 → 报告并进 P1.5）
- **P1.5（条件触发）**：DeepSeek 不支持 pointer/noul 协议 → 盒上落 openjev shim（翻译层：pointer 请求 → chat completions → 概率），显式小片
- **P2（flash）harness 部署 + 冒烟**：locomo-bench 交叉编译 scp 上盒 + 数据集 + `--estimate` + 判题 1 次连通 + 建库冒烟（throwaway run-dir）
- **P3（flash）正式四臂**：freeze --jev-arms（注册 env 齐）+ 全声明（含 --jev-b0-continuity-declared）+ --no-idk-retry + --chunk-quota=0 → gated pass + degraded pass → merge verdict（run-dir 全在 /root/autodl-tmp/051-jev-arms-*）
- **P4**：结果落 results.md + experiment-verdicts.md（LoCoMo 口径，注明答案模型 Qwen3.6 绝对数不跨模型比）+ eval-config commit 独立

- **P0.5（并行，维护者 2026-09-24 指令）向量库准备**：relay/cmd-deepseek-v4.1-flash 子代理，工作面 = 本地 + HF + DeepSeek API（**不碰盒子**，与 P1 零冲突）。内容：HF 数据可用性核查（token 已入 env 通道，不记录）、locomo 数据校验（1540 题/288 抽取单元/category-5 块）、抽取前置（EXTRACT_MODEL=deepseek-flash，≤300 调用 ≤¥10 预算闸）、本地/盒侧 embedding + store 建库编排与 staging。维护者并行化指令：单 AI 太慢，多线并发。

- **P1 完成**（run d487fda6→续命 1158767a）：vllm 0.29.0 + Qwen/Qwen3.6-35B-A3B-FP8（35G/53 文件全校验）@:8000 + BAAI/bge-large-en-v1.5 @:8010（dim1024，max-num-seqs=1 确定性）双服务 health 已证，45.4/49.1G 共存；磁盘纪律 +114M 系统盘。**Jev 探测：DeepSeek 无 /answers 指针端点（404×2）→ P1.5 触发 = openjev 本地 shim**（指针协议→盒上 Qwen3.6 chat→概率；data-model「openjev（未来本地近似）」枚举落地；claims 标注本地近似口径）。遗留注意：bge 512 截断风险、判题 thinking 块需容错、vllm 0.29 起服务需 ninja。EXTRACT_MODEL 口径待 P0.5 报告后于 P2 定稿（Qwen3.6 本地 vs deepseek-flash 前置 drafts 作交叉校验）。

- **P0.5 完成**（run 881cc455）：HF 核查（token=whoami-v2；LoCoMo 仅社区镜像，KimmoZZZ 与仓库 sha256 一致；Qwen3.6/bge 零 gated）；数据 1540+446=1986 ✓、真实抽取单元 272；抽取 272/272 成功（deepseek-flash，384,595 in/237,658 out tok，<¥10，路径级 300 调用预算闸）；**向量库 10/10 建成**（~/engram-store-staging/stores，41MB，bge-large-en-v1.5/1024d，4257 facts 0 缺向量，挂载复用 0 重抽，k=30/quota=12 recall 0.808 == 历史 canon）。**口径定稿：EXTRACT_MODEL=deepseek-flash**（P3 零抽取复用 store；manifest 钉版+注明 embedfix 补全工序）。**引擎 bug 立档（backlog，独立增量不混 051）**：memory.Embedder 256 槽队列 Backfill 超量静默丢弃→store 部分缺向量→hybrid 掉 keyword-only；locomo-bench 应 Backfill 循环到不动点（t19-p05-store-prep.md §4.2 证据链）。**P3 卫生**：verifyFormalGitProvenance 要求干净树→freeze 必须在协议 commit 的干净 checkout（box clone）跑。
- **P2 拆两段**：P2a（盒侧传输+验证，与 P1.5 并行）→ P2b（P1.5 落地后：交叉编译 locomo-bench+shim 上盒 + 全链冒烟 + freeze 准备）。

- **P2b 完成**（run 707f121a→85b847a2 复活链）：双二进制+REV 上盒（573ae16）、盒上干净 clone（**bundle clone 需 `-b <branch>`**——单 ref bundle 无 HEAD，已记档）、shim :8020 运行。**冒烟取证（决定性）**：vllm 无 reasoning parser，Qwen3.6-FP8 thinking 内联在 message.content（实测 2,497 字符≈625+ tok 固定税/次），max_tokens=2048 时 JSON 在末端正常析出 → 预算+剥离可收敛。**P1.5c 参数纠偏**：公式改 24q+1024（floor 1280, cap 8192）以吸收 thinking 税 + body 级 chat_template_kwargs 双保险。5a estimate 精确匹配（23100/23100/9240）。5b freeze PASS（refusal 记档：--jev-pilot-gate-confirmed 必填、--eval-budget-profile low|high 必填、--counter-fingerprint 必须带 sha256: 前缀）。**P3 前置清单**：P1.5c 二进制部署+真冒烟复验、--token-counter-calibrate 产物做 fingerprint 重新 freeze（smoke 的是占位）、真实声明 receipts、速率基线核对（~52 答案/分钟）。P3 命令块已交付（freeze→gated→degraded→同 run-dir 自动 merge 两 pass verdict）。

- **P2c 完成**（run f30fa5a7）：shim rev 5700048 上盒（sha256 a3397dda…，旧二进制留 .old-573ae16 回滚）；**2 问复验 completion=21 tok / 64 问 771 tok 全 64 键——chat_template_kwargs 在 body 级彻底灭杀 thinking 税**（fallback 未动用）；FP 纠偏：--counter-fingerprint 是**输入**非输出，复用历史 `sha256:4806660dd8…`（022/023/033/042/043 同栈有据）；**决策(A) 域=LOCOMO_NO_THINKING=0（思考开）**——校准 delta=0（8 fixtures）+ 与 k150+thinking 冻结配方同域；**backlog#2**：preflight /tokenize 不渲染 chat_template_kwargs 而运行时渲染（+2 tok）→ 思考关域全体答案误判 drift（独立小片后续修）；re-freeze 预演通过（rev 5700048，protocol_hash sha256:21dbe9d1…）；P3-RUNBOOK.md 落盒。**P3 按维护者常设指令「开始跑分」点火**（思考域已披露可否决；计量盒空烧止损）。

- **P3 blocker 热修落地**（run a69f13cd，qianwenai/deepseek-v4.1-flash）：jevfilter_protocol.go:963 单条件修复（验 evalProtocolPath 路径非空替代超前 formalProtocol nil 检查；:1114 运行时后盾不动）；TDD 红→绿（3 断言含「空路径+已绑协议仍拒」钉死替代非补充）；gate 全绿；零偏离。P3-launch 交接资产齐备（链脚本可复用、§5 九步、protocol.json diff 必须恰 {created_at,protocol_hash,git.commit}）。重发射 gate 改 content-grep（修我第二次 gate 构造错）；最终验收锚 chain.done=0 + merged verdict。

- **P3 blocker-2 热修落地**（run 45b91795，qianwenai/deepseek-v4.1-flash；relay 3bef7da6 空响应死循环→维护者禁用该 api_ref 并提 router bug `jev-pi-router/bugs/2026-09-24-relay-…md`）：eval_runner.go switch 白名单 +注册标记键（常量引用）+ validateFormalMechanismBinding 对称一致性校验（flag⟺注册共存且 key 匹配）；TDD t1–t6 红→绿；025/027/density/treatment 绑定测试全过；我手动复跑 gate exit=0（failed 标签=gate flake）。**819ed551 R1–R8 resume 清单就绪**（$D2=formal-20260924T145959 有效 freeze；链脚本 D 行换 $D3；vllm 基线 8.0；D3 diff 必须恰 {created_at,protocol_hash,git.commit}）。

- **P3 blocker-3 热修落地**（run 93c75a2c，qianwenai/deepseek-v4.1-flash，router 又选 relay 已按维护者禁令覆盖）：探针签名 +answererModel 参数（调用点喂 protocol.Models.Answerer.ID），空 id fail-closed；blank-want 早退顺序保留；TDD 4 测试红→绿（含机械前置证据）；gate 全绿。**点#3 日志证明四臂 run 已真实启动**（conversations=10、store 复用、jev four-arm run pass=gated）——三连门全在新增 jev 编排层，历史答题机器无恙。剩余已知门=零。

- **P3 第四次点火实况**（run 840b5b6c）：三 blocker 全死后**答题流真启动**（$D4=formal-20260924T154807，probe 过、五臂分裂、GPU 100%），但 **filter 100% 降级**（82/82 行 degraded=true——30s 死线被 32 路思考开答案车队挤爆，D 臂测的是透传）+ 速率 13.6/min→ETA 56.6h → 主管止损 kill（chain.done=143，沉没 ~12 分钟）。
- **真 Jev 端点确认**（维护者指引 → jev-pi-router/jev-ultrafast 背景）：Vercel AI Gateway `https://ai-gateway.vercel.sh/v4/ai/evaluation-model` + `AI_GATEWAY_API_KEY`（.bashrc，已无回显入 ~/.engram-eval-secrets.env）+ 模型 `typesafe-ai/jev`（网关 id ≠ 原生 jev-latest）+ 专用头（protocol 0.0.1 / eval-spec 4 / ai-model-id）；协议 systemone：{model,state,questions:{id:{type:choice,criteria,instructions}}}→{answers:{id:{choice,probabilities(2位小数)}}}。**双端探针 200**（本机+AutoDL 盒，盒出网✓）；48 题批量 0.89s（5.2k/1.5k tok）——30s 死线余量 10-15×。全量 token 规模估算 ~148M in/43M out（9,240 调用×150 题）。**方案：shim 加 typesafe 上游模式（协议翻译层，引擎/契约不动）**。

- **Slice 11 落地**（run a882dc09，deepseek/deepseek-flash）：shim 双上游模式（chat 默认字节不变 / typesafe 翻译 systemone）；14 新测试+29 旧测试全绿；网关头/重试 429/529/503/死线内退避/413 守卫全保留；key 零泄漏（启动行/502 体断言）。**维护者拍板：先跑一次（1-rep × gated-only × 1540×5=7,700 答案 ≈ 6-9h，网关 ~49M/14M tok，判题 1,540 次）；degraded pass + 全量 3-rep 待试点结果再定**。试点需新 freeze（--repeats 1 + ENGRAM_JEV_MODEL=typesafe-ai/jev 注册真身份）+ gated-only 链。

- **Pilot 5th 门 + 校准双发现**（run f215eb30）：真 Jev 部署+网关冒烟全通（48 题 2.84s、wire-header 闭）但 **`--repeats 1` 被拒**（jevfilter_protocol.go:940 无条件 ==3——jev-arms 冻结在 3-rep 多数协议，需显式试点参数化，非 bug）；**校准探针**：共享 state 绑定零分离（无关记忆也 0.95）→ **per-question 自包含绑定（instructions 携记忆文本+查询）完美二值分离（1.0/0.0）**——shim typesafe 翻译需小修。两片串行：s12 绑定修复（cmd/openjev-shim）→ s13 reps 参数化（cmd/locomo-bench）→ 部署+分离冒烟+试点 freeze+点火。

- **Slice 12 落地**（run 82eeeae1，qianwenai/deepseek-v4.1-flash）：typesafe 每题自包含绑定（goal 携记忆文本+查询、双规则含「无关答 no」；查键 need_<memKey>→memories[memKey]，未中→泛型回退保写门路径；2000 字 rune 边界截断）；TDD 6 测试，47 顶层全绿，chat 模式未动。**s13 排队：--jev-arms-reps 参数化（{1,3}——2 无多数语义故拒）**。

- **Slice 13 落地**（run cf62e844，qianwenai/qwen3.8-flash）：--jev-arms-reps {1,3}（默认 3 字节不变、双向 mismatch 拒、2 无多数语义拒）；16 处硬编码 3 审计（B0 连续性/固定金标按设计保留 3-rep）；manifest answer_repetitions=1 全链一致；TDD 7 测试。**终冲刺：部署 s12 绑定 shim + s13 runner → 分离冒烟 → 1-rep 试点 freeze → gated-only 点火**。

- **试点点火+止损（夜间自主）**：s12/s13 部署→分离冒烟完美（1/0/0/0）→freeze 6 域精确→gated 链点火 16:55；20 分钟后 H1（网关对 ~32k token 大分片 503 风暴→filter 96% 降级）触发 STOP→决策 A：证据离盒（pilot-artifacts/）→PID 击杀 143→看门狗 ~17:26 自动关机止损。**复盘修正：worker 的 H2「答题冻结」是指标误读（grep 了 length 标签；stop 标签 817−基线266≈28 答案/分=管线健康）——H1 是唯一真问题**。修复方案备好（晨报）：S14=ShardSize clamp [32,64]→[6,64]+eval 设 12（引擎字段本就存在；总 token 省 ~18%；小请求实测通过）+可选 S15 shim 在飞上限；等维护者晨间拍板。

- **S14 落地**（run b1143574，qianwenai/deepseek-v4.1-flash；router 选的 tokensfree/gpt-6-sol 不在 Pi 注册表→覆盖，第 2 个 router 失配数据点）：clamp [6,64] + eval 接线 ShardSize=12（TDD 6 测试、生产默认 48 钉死）；盒子重启（新凭据 paramiko 装公钥、服务已拉起、shim typesafe 就位、盒钟漂移 09/28 记档）。**S15 belt 暂缓**：先测 12-题片健康度，503 复发再上有实测依据的 cap。重发射排队。

- **§10 试点健康点火（2026-09-28 09:25）**：充值恢复 key→分离复冒烟完美（1.8s）→$D7 freeze（diff 恰 3 域）→gated-only 链 launch（runner 3998）+看门狗重挂（4028）。**S14 验证成功：196/196 行 degraded:false（100%）、p50 延迟 ~1.1s、12 题片 attempts=1 ~1s、503 风暴清零**、~26 答案/分、零 INVALID。ETA ~4.9h（≈14:20 完成→看门狗 ~14:30 自动关机）。收数器 14:25 挂。
