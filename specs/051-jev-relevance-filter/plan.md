# Implementation Plan: Jev 候选相关性过滤（宽池 + 按问题筛，读写双门）

**Branch**: `051-jev-relevance-filter` | **Date**: 2026-09-23 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/051-jev-relevance-filter/spec.md`

## Summary

在已有 BM25+向量+实体 RRF 之**后**加一段按当前问题的候选相关性过滤（TypeSafe Jev 校准概率，pointer 模式），把「评测才用得起的 k=150 宽池」变成「生产每轮用得起的宽池」：覆盖接近 150、展示条数 ≤12、无相关记忆时诚实返回空。同时提供可选写前门（一次性/密钥拦截，fail-open）。全部 opt-in、默认行为逐字节不变。四臂 LoCoMo 跑分（A/B/C/D）为合入硬门。

## Technical Context

**Language/Version**: Go 1.25.0，`CGO_ENABLED=0` 纯 Go（硬门）

**Primary Dependencies**: 标准库 `net/http`（Jev HTTP 适配器，无 SDK 依赖、核心路径禁止 import 死绑 TypeSafe）；既有 `embedding.Reranker` 可选 sidecar 模式作为范式；`github.com/modelcontextprotocol/go-sdk` v1.5.0（MCP 契约增量）

**Storage**: 不变——无 schema 迁移、无新表/列（filter 是读路径计算层；遥测随工具结果返回并由评测 harness 落文件）

**Testing**: `go test`（stub RelevanceFilter + httptest 录制 Jev 响应，全离线）；MCP in-memory transport 契约测试（parity 不变式）；评测走 `cmd/locomo-bench` 四臂（远程 GPU 评测箱，run-dir 全部 `/root/autodl-tmp/`）

**Target Platform**: Linux/WSL2 开发；纯 Go 交叉编译

**Project Type**: 引擎库（新包 `filter/`）+ 引擎薄接线（`memory/`、`mcpserver/`）+ 评测 harness 扩展

**Performance Goals**: Jev 单请求 p50 ≈ 0.3s、≈$0.0004/req 量级（SC-007 超一个数量级需归因）；池 >64 条分片并发（单片 32–64）；未配置时零附加延迟，配置但端点挂起时首查 ≤1s（写门 ≤500ms），30s 负缓存后归零

**Constraints**: 离线默认；opt-in 默认关；密钥只经环境变量；默认调用行为 parity（逐字节一致）；单用户 10 万条级

**Scale/Scope**: candidate_pool 硬夹 ≤ 500（推荐/评测档 64/100/150，门限跑 150 预注册）；k_show_max = 12；单用户规模，不作百万级声明

## Constitution Check

*GATE: Phase 0 前已过；Phase 1 后复核（见文末「复核」）*

| # | 条款 | 判定 | 依据 |
|---|------|------|------|
| I | 本地优先、默认离线 | ✅ 过（带记录在案的修订） | Jev 为 opt-in 默认关 sidecar；无 key/离线/失败 = 今天的 RRF 路径；FR-007 死亡条款修订为维护者 2026-09-23 明示拍板（clarify Q1 选项 C），记入 Complexity Tracking |
| II | 引擎/适配器分离 | ✅ 过 | 接口与 HTTP 客户端在引擎（对标 `embedding/`），MCP 仅薄接线；不改引擎对外形态即可加集成面（新增可选参数是显式契约增量） |
| III | 契约先行 + namespace 隔离 | ✅ 过 | `contracts/` 先冻结再实现；新增均为可选参数/新接口（向后兼容，MINOR 级）；无 schema 变更；过滤发生在单 namespace store 检索之后，隔离语义不动 |
| IV | 评测回归门 | ✅（计划满足） | 四臂 LoCoMo 1,540、unified contract、clean judge、3-rep 多数；SC-001..007 逐项判定，不达标标 HOLD；eval-config 提交与算法改动分开（归因） |
| V | 优雅降级 + 诚实规模 | ✅ 过 | fail-open；`FilterMeta.Degraded` 只报结构性事实；规模声明不变（池 150 级、单用户） |

## Project Structure

### Documentation (this feature)

```text
specs/051-jev-relevance-filter/
├── spec.md              # 已有（clarify 完成，5 问已录入）
├── plan.md              # 本文件
├── research.md          # Phase 0
├── data-model.md        # Phase 1
├── quickstart.md        # Phase 1
├── contracts/           # Phase 1
│   ├── relevance-filter.md   # 接口 + Jev pointer 协议 + 阈值策略
│   ├── memory-search.md      # MCP memory_search 契约增量 + parity 不变式
│   └── write-gate.md         # 写前门决策合同（route 词汇表）
├── plan-review.md       # 跨厂商互审记录（jev-pi-router FR-008）
└── tasks.md             # Phase 2（/speckit-tasks 产出，本命令不创建）
```

### Source Code (repository root)

```text
filter/                          # NEW 引擎包（纯接口 + 策略 + Jev 客户端，无 store 依赖）
├── filter.go                    # RelevanceFilter 接口、FilterMeta、Policy、Select() 纯函数、None 实现
├── filter_test.go
├── gate.go                      # WriteGate 接口、GateDecision（route: write|skip|defer_to_packet）
├── jev/
│   ├── jev.go                   # TypeSafe Jev HTTP 适配器（pointer 模式、分片并发、usage 计费）
│   └── jev_test.go              # httptest 录制响应，全离线
memory/
├── retriever.go                 # +SearchFiltered(...) 薄组合（Search 宽池 → filter.Select → 排序/pinned 注入）
└── retriever_test.go            # stub 过滤器合同测试 + 降级路径
mcpserver/
├── tools.go                     # memory_search +candidate_pool/+filter 输出 filter 元数据；memory_write 可选写门接线
├── config.go                    # ENGRAM_JEV_BASE_URL/MODEL/API_KEY、ENGRAM_JEV_THETA 等、写门开关、ENGRAM_SEARCH_POOL/ENGRAM_FILTER（服务端默认旋钮，MCP 客户端无法设服务端 env）
└── *_test.go                    # 契约测试：parity 不变式、degraded、空结果两段式
cmd/locomo-bench/                # 四臂 A/B/C/D recipe + 指标（pool recall@150/recall@shown/短名单精确率/空注率/翻错题）；内部接线缝 = multiquery.go retrieveWithQuotaDiagnostics → SearchFiltered → packer，shown 以 packer 注入数为测量点，--top-k 拆内部 pool-k/show-k；改动波及 retrieveWithQuotaDiagnostics 全部调用方（coverage/attribution/abstain_probe/eval_runner）与 pcic_test 固定元数断言更新
cmd/engram-filter/               # B 路径（FR-001 A+B 双落地）：thin CLI，stdin 候选+query → 短名单 JSON，复用 filter 包同一语义与遥测
skills/engram/SKILL.md           # 生产旋钮：ENGRAM_SEARCH_POOL / ENGRAM_FILTER（无 key 行为与今天相同）
AGENTS.md + CLAUDE.md            # 死亡条款修订（FR-007，两处镜像同 commit 修订，记录缘由与生效范围）
docs/engram-probe-curation-design.md + docs/engram-环境核对记忆设计.md  # 规范性设计来源（当前 untracked，随本 feature 纳入版本库）
docs/evaluation/results.md       # 四臂结果落正本（LoCoMo 口径措辞）
docs/evaluation/experiment-verdicts.md  # 实验裁决速查条目
```

**Structure Decision**: 单项目库 + 薄适配器布局（与现状一致）。新引擎包 `filter/` 规避 import cycle（`filter` 不依赖 `memory`；`memory` 单向依赖 `filter`，范式同 `embedding`）。B 路径 = `cmd/engram-filter` 薄 CLI（复用同一 filter 包）；评测接线缝与 shown 测量点已在代码树注释命名。

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|-----------|------------|-------------------------------------|
| 修订 AGENTS.md 死亡条款（付费云打分模型可 shipped/推荐/报涨分，仅限本 Jev 过滤与写门面——读写双侧） | 维护者 2026-09-23 明示拍板（clarify Q1 选项 C）；Jev 是校准概率 System-One 取舍层而非 cross-encoder rerank，定位是生产成本/噪声压缩（D ≥ B − 0.5pp 压缩对齐为门） | 「仅诊断不进产品」被否：维护者定调「该接的」；但历史 91.43% 归因不改口（灌上下文的分），诚实性保留在 FR-006 |
| 新增引擎包 `filter/` | 接口被 `memory/` 与 `mcpserver/` 共用，且签名需独立于 `memory.Result` | 塞进 `embedding/` 语义不合；放 `memory/` 内部会让 Jev 实现包回引 `memory` 成 cycle |
| MCP `memory_search` 新增可选参数 + `cmd/engram-filter` 薄 CLI（B 路径） | FR-002/FR-001 明确要求 A+B 双落地；评测 harness 与 MCP 客户端必须打到同一路径 | skill 侧**独立实现**过滤被否：语义不共享、parity 无法证明；B 路径以薄 CLI 复用同一 filter 包满足「共享语义与遥测」 |

## Phase 0/1 摘要

- **research.md**：7 项技术决策（接口形态、Jev 协议客户端、阈值策略集中化、分片并发、计费遥测、写门放置、评测口径），全部无 NEEDS CLARIFICATION 残留。
- **data-model.md**：`Candidate/FilterMeta/Policy/RelevanceFilter/WriteGate/GateDecision` + MCP 输入输出增量。
- **contracts/**：三个合同先冻结（接口语义、MCP parity 不变式、写门 route 词汇表 + fail-open 语义）。
- **quickstart.md**：离线构建/测试、stub/httptest 验证、可选真 key 冒烟、四臂评测运行手册引用。

## 互审记录（jev-pi-router FR-008：计划互审）

- **状态**: 互审进行中——
  - 派发 1（2026-09-23 15:18）：glm/glm-5.3 后台 reviewer **失败**——429 配额耗尽（每周/每月上限，code 1310，限额 2026-09-24 17:21:33 重置）。按 429 分诊属 quota 型：glm **立即禁选**至 quota_until（自动解锁）；已写入决策引擎 vendor_failures（quota_block 已记录）。
  - 重咨询（2026-09-23 15:19）：引擎 chosen=mimo/mimo-v2.6-pro（与生产方同厂商，违反 FR-008 跨厂商互审，**不执行**）；fallback_order 仅 glm（禁选中）→ 跨厂商强模型池对该任务暂时耗尽（pool_exhausted）。
  - 派发 2（2026-09-23 15:2x，维护者显式授权降级）：reviewer = **deepseek/deepseek-flash**（flash 档降级，跨厂商独立性保持：deepseek ≠ 生产方 mimo；仅 tier 从 strong 降为 flash，属运营者明示例外）；任务文本与派发 1 逐字一致，保证可比。→ VERDICT: block（已处置，见下）
  - 派发 3（2026-09-23 16:xx，维护者指令）：复核 review = **glm/glm-5.3-flash**（维护者点名，任务=验证 20 条 findings 修复 + 新缺口）；另探活 relay/cmd-deepseek-v4.1-flash（中转站 cmd）健康度。产出：`plan-review-2-glm-flash.md`。
    - 探活结果：**relay/cmd-deepseek-v4.1-flash ✅ 可用**（实调返回 OK）。
    - glm-flash 派发 **失败**——429 同一 glm 厂商级配额（code 1310，同一 reset 2026-09-24 17:21:33，request 2026092316012230a590bfe23a4465）：glm strong/flash 同池共封。封禁维持至 quota_until 自动解锁。
  - 派发 4（2026-09-23 16:xx，维护者指令）：**双 reviewer 并行**——relay/cmd-deepseek-v4.1-flash（跨厂商）+ mimo/mimo-v2.6-flash（同厂商，FR-008 独立性对该路弱化，维护者明示；与 relay 路构成双审面板，冲突由主会话仲裁）。产出：`plan-review-2-relay-flash.md` + `plan-review-2-mimo-flash.md`。
    - **4a mimo-flash（已回）**：VERDICT **block**——19 条 prior findings：11 RESOLVED / 6 PARTIAL / 2 OPEN；新 findings F1–F11。唯一 BLOCKER = **F1（SC-004 同义反复）**：金块缺失题上 gold=∅，「全部条目与 gold 无重叠」恒真 → 指标永真不能失败；另一读法（hits==[]）下 C 恒非空 → D≥C 恒真。建议修：per-question 成功 = `hits == []` + D 臂绝对下限。F2（MAJOR）= parity 不变式需限定「ENGRAM_SEARCH_POOL/FILTER 未设时」+ 旋钮 reader 点名 config.go。F3–F11 = NIT 级文本修正（Complexity 行措辞同步、promotionVerdictFor/A↔C 显式声明、038 合法性工件具名、jev-latest 占位符、B 路径验证步骤、臂表 C@12/D-noRelax、data-model 措辞、离线零网络断言）。产物已入仓。
    - **4b relay-flash（已回）**：VERDICT **block**——F1（与 4a 同点）+ **F3 BLOCKER（arm B 在 038 强制 `--answer-input-cap` 下被 packer 静默截断，B↔D / SC-002 对照失真）** + F2/F4/F5/F11 MAJOR + F6–F10 MINOR + NIT-1..5。产物已入仓。
  - **双审仲裁（2026-09-23）**：两路**收敛无冲突**（F1 同点；relay 额外 F3/F4/F5/F11）。合并修复 F1–F11 + NIT-1..5 已全部按两路 prescribed minimal fix 落文档（union，零驳回）；F1 修法取共同建议：空注判据 = `hits == []` 单一判据 + D-noRelax 绝对下限 ≥50%（预注册）；F3 修法：四臂统一冻结 `--answer-input-cap=32768` + B 无截断硬断言。修复均为 plan-stage 文本/范围修正（两路 reviewer 均确认非实现缺陷），修后计划可进 /speckit-tasks。
  - **实现澄清（Slice-1 仲裁，2026-09-23）**：切片 1（filter/ 包 T03–T06）落地，主会话独立复跑 gate 绿（build + filter 测试 + 全仓测试）；run 被标 failed = 运行时 acceptance gate 误报（时序敏感测试 flake 嫌疑，列入代码审重点）。执行器上报 13 条实现歧义全部裁决并回写契约：SelectForQuery 规范入口、GateRequest/GateWithOptions、FilterMeta.Notes、Policy.RelaxDisabled、None=1.0、负缓存客户端级、Kept/Dropped 组合层覆盖、放宽段 pinned 语义、空 trigger 不旁路确认、写门判定优先级、响应形态与 path 评测前钉版（T15/T19 前置）。
- **结论（2026-09-23 15:5x）**: `plan-review.md` = reviewer 原文（deepseek/deepseek-flash，fresh context，**VERDICT: block**；3 BLOCKER + 11 MAJOR + 4 MINOR + NIT）。主会话裁决：**全部采纳，1 项微调**——两段式放宽保留维护者拍板默认（clarify Q5 选项 A），增补 `ENGRAM_JEV_RELAX=0` kill-switch 与 D-noRelax 评测变体、SC-004 在 D-noRelax 上测量（不推翻拍板，补测量严谨性）。修复全部落 plan/spec/contracts/research/quickstart。
- 修复落点速查：B 路径 = `cmd/engram-filter` 薄 CLI（FR-001 A+B 落实）｜写门传输边界 = 本地密钥形状预判短路 + 发云遮蔽 + 500ms fail-open｜SC-001 = run 内配对 + 精确 McNemar + 配对 CI（paired_eval.go）｜SC-003 等预算（C@12 门 / C@8 参考）｜SC-004 = category-5 declared block + 空注操作定义（D-noRelax）｜SC-005 = 降级集 ≡ C 展示集｜θ/pool 预注册进 manifest、sweep 仅诊断｜038 冻结清单/机制键/intended variable 重声明/上下文 parity 门替代｜SearchFiltered 增 `show` 参数、错误语义分层｜Pinned 补进 `memory.Result`、trigger 命中定义、计入 KShowMax｜filter 遥测补 latency_ms/cost_usd｜过滤 deadline 1s + 30s 负缓存 + 不重试｜AGENTS.md+CLAUDE.md 同 commit｜candidate_pool 夹取与校验表｜Jev 模型钉版本进 manifest｜评测前成本预估步骤｜设计文档纳管。
- **产出**: `plan-review.md`（reviewer 原样结论，已入仓）；本节即裁决记录（FR-008 要求「结论与工件同处」）。

  - **代码审（code-review-1，mimo/mimo-v2.6-pro 跨厂商 strong）**：VERDICT **block**——1 BLOCKER（jevArmRetrieve defer-after-return，RetrievalMs 恒 0）+ 5 MAJOR（RetrievalCalls 恒 0 使排除审计真空、manifest attach-after-digest 违 freeze-before-digest 硬规则且注释虚标、chunk-quota 下 D 臂与 A/C 跨 recipe 破坏单变量、部分失败 run 可写 verdict:GO 且无全队列覆盖校验、filter.WriteGate 规范形错位 jev 包）+ MINOR/NIT 若干（读路径密钥不遮蔽、SC-005 空证据通过、SC-006 常量通过、SC-007 仅 D 臂+p50、merge 静默跳过、flake 倒挂时序、自比测试、文档 stale）。**主会话仲裁：全部采纳零驳回**；两处拍板：①读路径 memories() 也过 MaskSecrets（护 write-gate 硬约束 2「密钥只入本地」，契约 §2 注记 wire=遮蔽文本）；②空 draft → RouteNoGate 对齐无门行为。修复 = 切片 6，修后 resume 原 reviewer 复核翻案。
  - **代码审复核（closure，run 0b10f8a9，原 reviewer resume）**：VERDICT **approve**——20/20 findings CLOSED（含 file:line 证据），6 偏离全 sound（2 强于处方），4 新测试非凑数，仲裁两决定原样落地。新 NIT×3：n6 测试注释与常量矛盾（待微修）、n7 QuestionCount≤0 时覆盖检查 fail-open（裁决：接受现状——freeze 路径必填，仅手写 manifest 受影响，记残余）、n8 code-review-1.md 混入原始笔记（已修：裁剪为正式审查文本，closure 单独存 code-review-1-closure.md）。
