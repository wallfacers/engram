# Implementation Plan: Decision-Gap Attribution(决策缺口归因)

**Branch**: `036-decision-gap-attribution` | **Date**: 2026-08-11 | **Spec**: [spec.md](spec.md)

**Input**: Feature specification from `/specs/036-decision-gap-attribution/spec.md`

## Summary

034 证明候选空间上界 1411/1540,裁决只选对 1378;035 证明"更保守的双视图改选"救不回(1539/1540 保留)。33 题缺口(oracle − selected)需要先逐题归因清楚,才能决定后续机制喂原始 wording 还是生成前置。本 feature 是一个 **benchmark-only 的纯诊断**:零模型调用、零决策变更、零新基线,只读 034 的 seal/decisions/custody + 三份候选 + 可选 035 audit,输出可审计的逐行归因清单与聚合分布。

**技术路线**:复用 034 现有的只读 loader(`loadAndValidateAdjudicationPublic` + `loadAdjudicationHiddenInputs`,它们已含 seal/custody 完整验证),在 `scoreAdjudicationDecisions` 的逐题重建逻辑之上展开为逐行,不做任何 provider 调用。开发阶段用现有 034 测试 fixture 模式(`writeAdjudicationPublicFixture` + 手工 `adjudicationHiddenInputs`)先完成脚本与测试;真实 034/035 数据从维护者另一台电脑补入后即可跑。

## Technical Context

**Language/Version**: Go 1.25(CGO_ENABLED=0 硬门)

**Primary Dependencies**: 仅标准库 + 现有 `cmd/locomo-bench` 内类型与函数;不引入新依赖

**Storage**: 只读 JSON artifact(034 manifest/packets/decisions/seal/custody + 三份 `results-hybrid.jsonl` + 可选 035 audit seal);无 DB 写入

**Testing**: `CGO_ENABLED=0 go test -count=1 ./cmd/locomo-bench` + 全仓 build;离线 fixture(复用 `writeAdjudicationPublicFixture`,见 `answer_adjudication_test.go:526`)

**Target Platform**: Linux / 跨平台 CLI(locomo-bench)

**Project Type**: Go CLI(benchmark 诊断子命令)

**Performance Goals**: 1540 行全量归因 < 数秒(纯内存计算,无网络)

**Constraints**: 零 provider 调用;不修改任何 034/035 artifact;不接触 `memory/ embedding/ provider/ store/ internal/`;默认 benchmark 与既有 CLI 模式 byte-identical

**Scale/Scope**: 1540 题 / 33 缺口 / 4 类别 / 3 失败模式

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Constitution | Status | 说明 |
|---|---|---|
| I. Local-first, offline | PASS | 纯本地只读,零网络;仅当用户显式提供 034/035 目录才读取 |
| II. Engine/adapter separation | PASS | 只新增 `cmd/locomo-bench/*.go`(adapter 侧),引擎目录零变更 |
| III. Contract-first & isolation | PASS | 只读复用 034 冻结契约;不改 schema/API;新代码全在追加文件 |
| IV. Eval regression gate | N/A→PASS | 纯诊断不改变任何检索/提取/裁决算法路径;不改 eval-config。证明方式 = 默认路径 parity(测试断言 034/035 既有 CLI 测试通过)+ 引擎 diff 为空。不产生新基线,不触发 eval。 |
| V. Graceful degradation & honest scale | PASS | 035 seal 缺失时 US3 列为空并显式标注,不阻塞 US1/US2;归因结果标注 `decision_gap_attribution`,不作正式分数 |

## Project Structure

### Documentation (this feature)

```text
specs/036-decision-gap-attribution/
├── spec.md              # 已就绪(Draft)
├── plan.md              # 本文件
├── research.md          # Phase 0(可跳过:本 feature 无未决研究问题;若需要写 fixture 数值说明)
├── data-model.md        # Phase 1(034 artifact 结构已在 answer_adjudication_artifact.go,引用即可)
├── contracts/           # Phase 1(归因 report JSON schema)
├── quickstart.md        # 运行/验证步骤
└── tasks.md             # Phase 2
```

### Source Code (repository root)

```text
cmd/locomo-bench/
├── answer_adjudication_attribution.go      # NEW 归因核心:逐行重建 + 缺口标记 + 聚合
├── answer_adjudication_attribution_artifact.go  # NEW 归因 report JSON schema + 读写 + 校验
├── answer_adjudication_attribution_cli.go  # NEW CLI 模式 dispatch + fixture/真实数据入口
├── answer_adjudication_attribution_test.go # NEW 离线测试(逐行/缺口/聚合/035交叉/tamper)
└── main.go                                 # EDIT: +--adjudication-attribution flag/dispatch(最小)
```

**Structure Decision**: 追加三个新文件 + main.go 最小 flag/dispatch,完全对齐 034(`answer_adjudication.go`/`_artifact.go`/`_cli.go` 三文件分层)与 035(audit 独立文件)的既有惯例。不加私有包,不碰引擎。

### 归因数据流

```
--adjudication-attribution <dir> --adjudication-candidate r1.jsonl --adjudication-candidate r2.jsonl --adjudication-candidate r3.jsonl [--adjudication-audit <035dir>]
        │
        ▼
loadAndValidateAdjudicationPublic(dir, requireFrozen=false)   // manifest + packets(含 seal/decision 校验)
        │
        ▼
loadAdjudicationHiddenInputs(dir, candidatePaths)              // custody 校验 + slot maps + 3×1540 correct labels
        │
        ▼
buildAttributionRows(manifest, packets, hidden)                // 逐题循环(复用 score 重建语义)
        │   每题: correctBySlot / normalizedBySlot / oracle / majority /
        │         controlSlot=canonicalHistoricalSlotForSameAnswer(adjudicationTextControlSlot)
        │         selectedSlot=canonicalHistoricalSlotForSameAnswer(decision.SelectedSlot)
        │         gap = oracle && !selectedCorrect
        │         失败模式: 证据不足 / 事实错 / 语义等价(依据 evidence 引用覆盖 + 规范化等价)
        ▼
aggregateRows(rows)                                             // 33 缺口 / 13 control-only / 9 both-wrong / 4 类分布
        │
        ▼
crossAudit(rows, auditDecisions)                               // US3: 035 状态(风险内外 / 父答案被反驳 / 唯一替代);seal 缺失→空列
        ▼
writeAttributionReport(dir)                                     // decision_gap_attribution JSON
```

### 关键复用点(不重造)

| 现有函数 | 位置 | 归因用途 |
|---|---|---|
| `loadAndValidateAdjudicationPublic` | `answer_adjudication_artifact.go:728` | 加载+验证 manifest/packets(含 seal/decision digest 校验) |
| `loadAdjudicationHiddenInputs` | `answer_adjudication_artifact.go:1201` | custody 验证 + slot maps + hidden correct |
| `adjudicationTextControlSlot` | `answer_adjudication.go:94` | 确定性文本 control |
| `canonicalHistoricalSlotForSameAnswer` | `answer_adjudication.go:129` | control/selected 的规范 slot tie-break(**必须复用,否则 33 不可复算**) |
| `normalizeAdjudicationAnswer` | `answer_adjudication.go:79` | 规范化等价判定 |
| `majorityCorrectness` | `paired_eval.go:16` | 三方多数 |
| `categoryLabel` | `dataset.go:211` | 类别名 |
| `loadAndValidateAdjudicationAuditParent` | `answer_adjudication_audit_cli.go:18` | US3 035 交叉(读父 receipt) |
| `writeAdjudicationPublicFixture` | `answer_adjudication_test.go:526` | 测试 fixture 构造 |

### 失败模式判定规则(FR-004)

对每个缺口行,按以下顺序判定(全部可复核):
1. **语义等价混淆**:正确候选与 selected 的 `normalizeAdjudicationAnswer` 相等 → 直接判定,标注两者 digest。
2. **证据不足**:selected 的 `EvidenceIDs` 不覆盖任何正确候选可定位的证据,或正确候选对应 slot 的证据在 packet 中缺失/不可定位 → 判定"证据不足"。
3. **事实错**:selected 证据自洽(有有效 evidence 引用)但非正确候选 → 判定"事实错"。
4. 无法自动判定 → 标 `unclear`,计入结论的置信度说明。

判定依据字段:每行输出 `mode_evidence`(正确候选被引用的 evidence ID)、`mode_normalized_equal`(bool)、`mode_reason`。

### US3 交叉(FR-006)

读 035 audit seal 的 decisions(`loadAndValidateAdjudicationAuditParent` 或等效只读路径),对每缺口题标注:
- `in_risk_queue`(是否在 477 风险题)
- `parent_refuted_any_view`(任一视图反驳父答案)
- `unique_alternative`(有唯一支持替代)
035 目录缺失/无效 → 三字段全部为空并标注 `audit_unavailable`,不阻塞。

## Complexity Tracking

无 Constitution 违规;本 feature 不引入新包、不改引擎、不加依赖。

## 风险与缓解

| 风险 | 缓解 |
|---|---|
| 034 真实数据在另一台电脑,本机无法端到端验证 33 数字 | fixture 上先验证逻辑正确(13/9 分解、类别聚合、tamper fail-closed);真实数据补入是纯输入替换,不改代码 |
| `canonicalHistoricalSlotForSameAnswer` 语义复杂导致 33 不可复算 | 直接复用该函数(不复制逻辑),测试断言 fixture 上 gap 数 = oracle − selected |
| 035 seal 结构可能随 audit feature 演进 | US3 只读,seal 缺失显式降级,不阻塞 US1/US2 |
| 失败模式判定有主观性 | 规则写死 + `unclear` 标记 + 判定依据字段,不做人工调参 |
