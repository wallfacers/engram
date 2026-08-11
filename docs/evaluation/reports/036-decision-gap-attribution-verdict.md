# 036 Decision-Gap Attribution — Real-Data Verdict

**Date**: 2026-08-11
**Scope**: 逐题归因 034/035 的 33 题决策缺口（candidate oracle 1411 − selected 1378），零模型调用，纯只读诊断。
**Result**: `decision_gap_attribution`，**不是正式 LoCoMo 分数**，不改任何决策，作为后续机制 spec 的数据前提。

## 运行方式

真实 034/035 产物就在本机（session-scratch，非 spec 计划时假设的"另一台电脑"），三个候选文件按 custody digest
反查精确匹配到 `locomo-pro/matrix-2026-07-26/locomo-pro/r{1,2,3}/results-hybrid.jsonl`。零 provider 调用，
纯内存计算秒级完成。产物：`~/.claude/session-scratch/034-stage0.DR07JV/decision-gap-attribution.json`。

## 数字与 spec/verdict 对账

| 指标 | 归因结果 | 034 verdict 口径 | 一致 |
|---|---|---:|---:|
| gap_count | **33** | oracle 1411 − selected 1378 | ✓ |
| control_only_loss | **13** | verdict「13 control-only losses」 | ✓ |
| both_wrong（accepted override） | **9** | verdict「9 both selected and control wrong」 | ✓ |
| fallback_gaps（non-trigger 6 + triggered 5） | **11** | data-model 定义 | ✓ |
| 四类别 gaps 之和 | 33 | 等于缺口总数 | ✓ |
| 035 audit_source | 035-audit | 交叉生效 | ✓ |

修复了一个聚合语义 bug（见下）：修复前 `both_wrong=20` 把 11 个 fallback 缺口（6 non-trigger + 5 触发）错误吸收进
override 桶，导致 SC-002（13/9 对齐 verdict）不满足；修复后 33 = 13 + 9 + 11 严格互斥穷尽。

## 失败模式分布（主导 = factually_wrong）

- **factually_wrong 22（67%）**：全部 22 个 accepted override 缺口（13 control-only + 9 both-wrong）都落在此类 ——
  裁决器在有正确候选可选时，基于自洽证据选了非正确候选。**直接指向 spec 方向 2「生成前置 / 喂血缘原始 wording」**。
- **semantic_equivalence 7（21%）**：全部落在 fallback/非触发（6 not_triggered + 1 触发），即控制与正确候选规范化后
  等价。指向方向 3「候选多样性 / 答案规范化」。
- **evidence_insufficient 4（12%）**：全部为 triggered `low_confidence` fallback（fallback 的 evidence_ids 为空），
  符合契约（selected 决策要求 ≥1 evidence，故此类只在 fallback 出现）。
- **unclear 0**：所有缺口可自动判定，无主观调参。

## 类别分布（temporal 是最大单类）

| 类别 | gaps | control_only | both_wrong |
|---|---|---:|---:|
| temporal | **12** | 6 | 2 |
| single-hop | 10 | 3 | 4 |
| open-domain | 6 | 2 | 2 |
| multi-hop | 5 | 2 | 1 |
| **sum** | **33** | **13** | **9** |

temporal 12 缺口里 6/12 是 control-only loss 且 factually_wrong —— 裁决器把正确控制改成了自洽但错误的候选。
结合 032（答题侧时序契约已 GO）、014（temporal 检索侧多路证伪），时间域真差距更可能落在**裁决对时间锚定候选的
甄别**上，而非再次改检索。

## 035 交叉（US3）

- 21/33 缺口带 035 审计：**in_risk_queue=21（全部风险题）、parent_refuted=7、unique_alternative=10**。
- 12 行 `audit_unavailable`（fallback/非触发不在 477 风险队列），显式标注，不阻塞。
- 7 个缺口 035 双视图已判父答案被反驳 —— 这些是"证据形态问题"候选（spec 方向 1）；10 个有唯一支持替代，说明
  候选多样性存在但裁决未选。

## 正确候选 slot（r1 承载最多）

correct_candidate_slot 分布：C1=18（r1）、C3=8、C2=7。33 缺口里 18 个的正确候选在 r1 —— 与 034 历史栈一致，
r1 的候选生成能力最强，缺口更在于**多候选间甄别**而非生成缺失。

## 聚合语义修复（本轮改动）

1. **`validateAdjudicationAttributionOptions`**：删除 `adjudicationMaxTokens != 0` 检查 —— max-tokens 是 034 run 模式
   专用 flag（默认 512），归因模式不该因共享 flag 默认值被拒。修复前 CLI 跑归因永远报错（真实 bug，任务表声称的
   `_cli_test.go` 实际缺失，flag 路径从未被测试覆盖）。
2. **`aggregateAttribution`**：fallback 判定从 `triggered && reason != ""` 改为 `reason != ""`，且 fallback 缺口不再
   计入 control_only/both_wrong override 拆分 —— 对齐 data-model（`fallback_gaps = non-trigger + triggered`）与
   SC-002（13/9）。fixture 全 fallback，此前无法暴露此 bug。

## Gate 状态

- `CGO_ENABLED=0 go build ./...` exit 0；`go vet ./cmd/locomo-bench` exit 0；全仓 `go test -count=1 ./...` exit 0；
  `gofmt` / `git diff --check` clean。
- 引擎目录（`memory/ embedding/ provider/ store/ internal/`）变更零。
- 无 credential / raw endpoint / hidden verdict 泄漏（gap 行只带 frozen digest 与判定状态）。

## 后续机制 spec 的数据前提

- **主方向（factually_wrong 22）**：裁决前生成前置 / 对 selected 候选做血缘原始 wording 复核，目标 accepted override
  缺口（13 control-only + 9 both-wrong）。
- **次方向（semantic_equivalence 7）**：候选生成多样性 + 答案规范化去歧，目标 fallback/非触发缺口。
- **temporal 12 优先**：单类最大，且 control-only loss 密集 —— 建议机制 spec 先落 temporal 类别条件化。
- 035 已判父答案被反驳的 7 题可作为证据形态方向的先导 cohort。
