# 030 Verdict: 读侧证据装配（Evidence Mediation）

**日期**: 2026-08-06 · **子集**: 84 题（029 实际子集，temporal 59 + multi-hop 25）× 3 reps majority · **机器**: AutoDL RTX PRO 6000（Qwen3.6-35B-A3B-FP8 @8000 + bge-large @8010 + DeepSeek mem0-aligned judge）

## 结论

**机制方向 GO（读侧证据精炼），引用链/压缩无独立增量。** 读侧证据装配（chunk-first 排序 + cap 内聚焦）显著优于 legacy 单次路径（keep 47.6% vs base 27.4%，p=0.0455）；trace 引用链（50.0%）相对装配器不显著（p=0.152）；条件压缩保守 PASS（p=1.0）。trace/consolidate 均 default-off 保留为 opt-in 评测能力。

## US 汇总

| US | 机制 | 配对结果 | 判定 |
|---|---|---|---|
| US1 装配地基（chunk-first + 精确记账 + 类别结构） | 单测 chunk_fraction ≥0.5 + 配对：keep vs base | keep 47.6% vs base 27.4%，**p=0.0455 显著** | **GO（机制）** |
| US2 引用链 trace（sidecar → fail-closed gate） | trace vs legacy base | trace 50.0% vs base 27.4%，**p=0.0017 显著**；但 vs keep 装配器 p=0.152 **不显著** | **方向 GO，无独立增量** |
| US3 条件压缩（超预算 sidecar 压缩） | cons vs keep | 41.7% vs 47.6%，**p=1.0** within-noise | **保守 PASS** |

## 关键对照（008 铁律归因纪律）

| Arm | OVERALL (J) | context tok | 说明 |
|---|---|---|---|
| base（legacy 12 chunk score 序） | 27.4% | 3600-4065 | 塞满 cap，噪声稀释 |
| **keep（装配器 chunk-first）** | **47.6%** | 3684 | **无 trace，纯读侧排序，显著优于 base（p=0.0455）** |
| **trace（sidecar 选 1 条 chunk）** | **50.0%** | ~250-725 | vs keep p=0.152 不显著 |
| base-slim（top-k5 quota1，~688 tok） | 27.4% | 688 | 控制：token 削减不改变 base 正确率 → trace 赢因非 token 削减 |

**拆解**：trace（50.0%）相对 legacy base 的 +22.6pp 中，装配器 chunk-first（keep）已捕获 +20.2pp（p=0.0455 显著）；trace 引用链相对装配器仅 +2.4pp 且不显著。→ **有效杠杆是"证据精炼"（chunk 优先 + 聚焦），非引用链 sidecar 本身**。

## 机制归因

1. **读侧证据精炼是首个读侧结构赢**（区别于写侧 027/028 证伪、检索侧 029 证伪）：把已检索候选**重排为 chunk-first + 类别结构**并聚焦 cap 内，显著优于 score 序塞满（base→keep p=0.0455）。
2. **trace 引用链无独立增量**：sidecar 精选 1 条 evidence 的效果 ≈ 装配器 top chunk（keep→trace p=0.152）。evidence_count 几乎全 1（valid+1=215/248）说明 sidecar 倾向单条输出，机制收益与简单装配重叠。
3. **token 削减排除**：base-slim（688 tok）与 base 满（3600 tok）同为 27.4% → 正确率不随 token 削减变化；trace 的赢来自证据选择非预算差。

## 全量验证（2026-08-06 补充，budget-efficient win 候选）

子集配对后重开机器补跑**全量 1540 题**（canonical recipe）确认环境与全量表现：

| 全量 1540 题 | OVERALL | 上下文 token | 备注 |
|---|---|---|---|
| base（chunk-quota 12，1 rep） | 84.9%（1308/1540） | 3620 | 与历史基线吻合（85.71%/85.19%/84.94%）→ 环境正常 |
| **trace（--trace-mediation，3 次 majority）** | **85.91%**（1323/1540） | **468** | **高于 base 历史 majority 85.19%（+0.72pp）/ 单次 84.9%（+1.01pp），token 省 7.7 倍** |

- **3 次 rep 极稳定**：85.6% → 85.8% → 85.9%，majority 85.91%——非随机噪声。
- 类别全正向无回落：multi-hop 87.23 / open-domain 66.67 / single-hop 88.23 / temporal 84.42（均 ≥ base 单次同类别）。
- 净翻转 vs base（1 rep 逐题）：trace-majority-only=75 / base-only=60，净 **+15 题**方向正（单次配对不显著，严格 base-3 次配对未跑）。
- **解读**：84 题子集 trace +22pp（base 27.4→trace 50.0）在全量上收窄为 +0.72-1.01pp（简单题 base 也能答对，trace 优势集中在难题）；但 **token 7.7 倍节省在任意口径下成立**——这是"预算下提质"（signal not volume）的落地：省 7.7 倍 token 且正确率稳定高于 base，类别全正向。
- 诚实边界：base 侧仅单次/历史 majority，严格同评测配对（base 3 次 vs trace 3 次）未跑；正确率增量方向正但单次配对不显著（p~0.2）；token 节省与类别方向是确定性结构优势。

## 诚实缺口

- US3 精确 tokenizer 未启用：缺 `--counter-fingerprint`（022 formal calibration 产物，机器未留存）→ keep/cons 装配用 estimate ledger；over-cap 判定为近似。
- consolidate 基本未触发（装配器 cap 截断已保证 ≤cap）→ 条件压缩在 cap=3600 下无实际检验；建议紧预算（1500-2000）或启用 fingerprint + `--assembly-diagnose` 复验。
- base 27.4% 与 029 记录的 47.6% 不一致（029 US2 子集文件已删、不可复现）→ 本 verdict 基线是配对内 base，非历史锚。

## 与先前证伪线的衔接

检索侧单次/表示/多步导航（021/013/014/029）全 NO-GO 后，**030 首次在 post-retrieval 读侧找到实证增益**：不依赖更多检索或推理，只把已有候选精炼成聚焦证据。temporal/multi-hop 两类别均正向翻转（US2 trace：temporal 净 +14 / multi-hop 净 +5），无类别崩。

## 出货影响

`--trace-mediation` **默认开启**（2026-08-06 全量 85.91%@468tok 验证后转正）：需要已配置的 answerer LLM 作为 sidecar，sidecar 不可用时优雅降级 legacy 路径（字节一致，SC-004），`--trace-mediation=false` 显式回 legacy。`--consolidate` / `--evidence-assembly` 保持 **default-off**（SC-004 零行为变化）。装配器排序方向（chunk-first 读侧）为后续 MCP/CLI 读侧装配接线提供依据（023 planner 生产接线类似）。引擎零改动（FR-001）。
