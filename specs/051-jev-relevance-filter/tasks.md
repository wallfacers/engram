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