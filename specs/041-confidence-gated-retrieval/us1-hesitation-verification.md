# 041 US1 验证：犹豫信号区分度（生死前提中间判定）

**日期**: 2026-08-14 | **数据**: 远程 AutoDL box `032-think3`（top-k 30 thinking run）+ `tk150-full3`（top-k 150），只读拉取到本地 scratchpad。

## 结论（边缘 GO）

自动文本规则的犹豫信号区分度 **2/3 rep 达门槛**（recall 波动 58–63%，fp 稳定 23–24%）。040 verdict 的「89% 犹豫」是**人工宽松判读**且基于 **3-rep 多数票的 56 增量题**；自动规则在**单 rep 全量错题**上的真实能力边界就在 60% 附近。机制方向成立（fp 稳定可控），但 US1 硬门槛未全 rep 达标。

## 检测器规则（当前，经 032-think3 条件概率调优）

| 权重 | 信号 | 短语 | 依据（wrong 占比 vs right，lift） |
|---|---|---|---|
| +3 | 拒答 | isIDK（don't know / no information / not mentioned） | not mentioned 36.4× |
| +3 | 强不确定 | not sure / uncertain / not confident / unsure / not certain / **maybe it** | maybe it 8× |
| +3 | 多候选 | either / not sure which / **or maybe** | or maybe 5× |
| +2 | 猜测 | could be / might be / may be / possibly / maybe / perhaps / **guess** | guess 3.6× |
| +1 | 低确信 | i think / i guess / i believe / probably / likely / approximately / **seem** | seem 4.5× |

加粗 = 本次条件概率 sweep 新增（单类别去重加分，`maybe it`/`or maybe`/`guess`/`seem` 来自 lift 分析）。

## 3-rep 区分度（threshold=3.0，单 rep 独立检测）

| run | wrong | wrong_hesitant | recall | right_fp | 门槛 |
|---|---|---|---|---|---|
| run-1 | 175 | 106 | **60.6%** | 22.7% | ✅ |
| run-2 | 178 | 103 | **57.9%** | 24.4% | ❌（差 2.1pp） |
| run-3 | 191 | 121 | **63.4%** | 23.4% | ✅ |
| 平均 | — | — | **~60.6%** | **~23.5%** | 边缘 |

门槛：wrong recall ≥60%（research Decision 2），right fp ≤30%。

## 增量题分析（run-1，单 rep：30 错 150 对 = 63 题）

- 增量题犹豫率 **63.5%**（40/63）——略高于全量错题 60.6%，但远低于 040 的 89%。
- 150 害题（30 对 150 错 = 46 题）犹豫率 63.0%——「避害」也能触发约 2/3。
- 结论：自动检测器对「迭代目标题（增量 + 避害）」的覆盖 ~63%，对「无证据自信错题」（~40/175）天然是盲区。

## 对 US2 的影响估算

- recall ~60%：60% 的错题触发加深 → 有机会救回；40% 自信盲区（含 ~23% 完全无犹豫表达）。
- fp ~23%：23% 对题加深 → 平均预算 ≈ 0.77×30 + 0.23×180 = **64.5 条 ≈ 2.33× 省**（vs 150），仍显著。
- 最终净收益由 US2 配对评测裁决（宪法 IV）。

## 阈值校准（US3，--confidence-calibrate）

对每个 threshold∈[0,6]（步长 0.5）重算区分度 + 预算（avg_ev=(1-fp)*30+fp*180）：

| run | PASS 带 | 最优点（recall/fp/avg_ev） |
|---|---|---|
| run-1 | t=2.5, 3.0 | t=3.0: 0.606 / 0.227 / 64.1 |
| run-2 | **无**（recall 峰值 57.9%） | 无解 |
| run-3 | t=2.5, 3.0 | t=3.0: 0.634 / 0.234 / 65.1 |

**结论**：`--confidence-threshold` 默认 3.0 是稳健冻结值——在信号达标的 2/3 run 中正好是 PASS 带最优平衡点（recall 最高、fp 达标、预算 ~2.3× 省）；run-2 无解是信号能力地板（非阈值问题）。更松（≤2.0）fp 爆 39%；更严（≥3.5）recall 掉 <50%。

## 决策请求

US1 硬门槛未全 rep 达标（run-2 57.9%）。选项：
1. **进 US2 用配对评测裁决**（推荐）：recall ~60% + fp ~23% 已足够检验「犹豫→加深」是否净赢；US2 净收益才是最终标准。
2. 严格按 spec 停线：自动规则 recall 边缘，机制信号强度不足以支撑迭代收益。
3. 调整门槛到实证值（~58%）：更诚实但弱化「生死前提」的过滤作用。
