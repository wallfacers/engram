# Implementation Plan: Retrieval-Side Temporal Window Recall

**Branch**: `013-temporal-window-recall` | **Date**: 2026-07-24 | **Spec**: [spec.md](./spec.md)

**Input**: Feature specification from `specs/013-temporal-window-recall/spec.md`

## Summary

把 temporal 从"RRF 融合后的软乘子"(`applyTemporal`,只作用于已被语义/关键词门控的池,够不着深埋 gold)升级为**独立召回臂**:一条按 event 日期范围拉取相交事实的查询,产出自己的 rank list,作为 RRF 平权第 4 路进融合。这把落在 query 时间窗内、但语义/关键词漏掉的深埋事实拉进候选池,直击 LoCoMo temporal 类 82.24% 短板。

**纪律先于机制**:先跑一个零答题/judge token 的 **retrieval-only 召回诊断门(US1)**证实瓶颈在召回侧(gold 深埋池外 + 时间窗臂能救起),诊断 GO 才建引擎召回臂(US2),臂落地后端到端配对 eval 把关(US3,宪法 IV)。

**关键存储发现(简化面积)**:migration **v4(`v4TemporalIndexes`)已建** `idx_memory_entries_event_start` + `idx_memory_entries_event_end`;`event_start`/`event_end` 列(nullable unix seconds)+ `event_date`(nullable micros)已在 schema;`temporalIntersects` 与 `ranksFromOrder(names)→map[string]int` helper 已存在。**本 feature 无需新 migration**——引擎改动缩至:新增一个只读 `EntryStore` 方法 + retriever 接一路信号。

## Technical Context

**Language/Version**: Go 1.25.0(No CGO,纯 Go / 可交叉编译)

**Primary Dependencies**: 标准库 `database/sql` + `modernc.org/sqlite`(纯 Go);无新增第三方依赖。复用既有 `memory/temporal.go`(`ParseTemporalIntent`/`TimeWindow`/`temporalIntersects`/`TemporalScore`)。

**Storage**: SQLite(`memory_entries`),`event_start`/`event_end`(INTEGER unix seconds,nullable)、`event_date`(INTEGER micros,nullable);索引 `idx_memory_entries_event_start`/`_event_end` 已由 migration v4 建。**本 feature 不新增 migration、不改 schema。**

**Testing**: `go test`(offline,向量 stub / nil client)。引擎单测:召回臂范围查询正确性 + parity(臂关 byte-identical)+ 降级 + 深埋 gold 抬升。适配器:US1 诊断在固化 store 上跑;US3 box 端到端。

**Target Platform**: 引擎 host-agnostic / offline;US1/US3 eval 在远端 box(bge-large + 本地答题栈)近免费跑,引擎本身不依赖 box。

**Project Type**: 纯 Go 库(引擎)+ 薄适配器(locomo-bench)。单项目布局。

**Performance Goals**: 召回臂 = 一条 `WHERE event_end >= ? AND event_start <= ?` 范围查询(命中已有索引);LoCoMo ~2755 facts 量级下 O(命中数);honest-scale ~100k class 下仍为索引扫描,可接受。

**Constraints**: offline-capable;parity byte-identical(臂关时);tuning-free(RRF 平权,无 temporal 专属权重);无云 rerank(死规则)。

**Scale/Scope**: 单用户 ~100k-entry class。引擎净增:1 个只读方法(`NamesByEventWindow`)+ retriever 内一路信号接线 + 1 个内部排名 helper。适配器净增:US1 诊断子命令 + US3 eval flag。

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

- **I. Local-first, offline by default** — ✅ 召回臂是纯 Go SQL 范围查询,无网络。临时/eval 用 box 但**引擎不依赖它**;时间意图解析已是确定性词法(离线)。
- **II. Engine/adapter separation** — ✅ 召回臂算法在引擎(`memory/`);US1 诊断与 US3 eval flag 在适配器(`cmd/locomo-bench/`),只调引擎公共 API。**US1 诊断用既有引擎只读方法(`List`/`EntriesByName`/`Retriever.Search`)测天花板,零引擎改动**——保证诊断 NO-GO 时引擎从未被动过(理想止损)。适配器不重实现召回臂。
- **III. Contract-first & namespace isolation** — ✅ 冻结 `EntryStore.NamesByEventWindow(ctx, start, end) → []string` 签名 + RRF 平权集成语义 + 默认关/降级契约(见 contracts/)。namespace 隔离不受影响(每 store 独立)。加读方法为**加性契约增量**,不破既有 API。
- **IV. Evaluation regression gate (NON-NEGOTIABLE)** — ✅ US3 = 端到端配对 eval(repeats≥3,temporal 类答题分 + 总分不回归),以答题分为准(008 铁律)。US1 是前置免费诊断门。eval-config 改动与算法改动分开 commit(FR-016)。
- **V. Graceful degradation & honest scale** — ✅ 无时间意图 → 臂不产 rank,静默掉出 RRF 和;事实无 event 日期 → 不进臂候选集,不报错。scale 诚实(索引扫描,~100k class;非 ANN)。

**Gate 结论:PASS,无违规,无需 Complexity Tracking。**

## Project Structure

### Documentation (this feature)

```text
specs/013-temporal-window-recall/
├── plan.md              # This file
├── research.md          # Phase 0: 关键决策(oracle 上界形态 / arm 排名序 / 乘子交互 / 索引复用)
├── data-model.md        # Phase 1: TimeWindow · event-dated fact · 召回臂 rank · 诊断分层结果
├── quickstart.md        # Phase 1: US1 诊断跑法 + US2 单测跑法 + US3 box 配对 eval 跑法
├── contracts/
│   ├── engine-namesbyeventwindow.md   # NamesByEventWindow 签名 + 区间相交语义 + 降级
│   ├── retriever-temporal-arm.md      # 召回臂进 RRF 第4路 + parity + tuning-free 契约
│   └── locomo-diagnostic.md           # US1 四层诊断输出 + GO/NO-GO 判据 + US3 eval flag
├── checklists/requirements.md         # (已由 /speckit-specify 生成)
└── tasks.md             # /speckit-tasks 输出(本命令不建)
```

### Source Code (repository root)

```text
memory/                          # ENGINE(仅 US2 触碰,US1 零改)
├── entrystore.go                #   + NamesByEventWindow(ctx, start, end) → []string(只读范围查询,复用 v4 索引)
├── retriever.go                 #   + 时间窗召回臂:构建 rank list 加入 signals；+ temporalRecallRanks helper
├── temporal.go                  #   复用(ParseTemporalIntent / TimeWindow / temporalIntersects);预计不改
├── entrystore_test.go           #   + NamesByEventWindow 范围/相交/降级单测
└── retriever_test.go            #   + 臂 parity(关时 byte-identical)/ 深埋 gold 抬升 / 降级单测

cmd/locomo-bench/                # ADAPTER(US1 诊断 + US3 eval)
├── temporal_diagnostic.go       #   + US1 四层召回诊断(只读,复用 012 runRecallDiagnostic 同构)
├── temporal_diagnostic_test.go  #   + 诊断分层/判据单测
└── main.go / runner.go          #   + US3 eval flag 接线(臂 off/on 配对)

store/migrations.go              # 不改(v4 已建 event 索引;本 feature 无新 migration)
```

**Structure Decision**: 单项目 Go 库 + 薄适配器。引擎改动严格限于 `memory/entrystore.go` + `memory/retriever.go`(+ 各自 `_test.go`),且仅在 US1 诊断 GO 后触碰;`store/` 与 schema **零改**。US1 完全走适配器 + 既有引擎只读 API。验证隔离:`git diff --name-only -- memory embedding provider store internal` 在 US1 阶段必须为空,在 US2 阶段只反映 `memory/entrystore.go`+`memory/retriever.go`(+tests)。

## Complexity Tracking

> 无宪法违规,无需填写。
