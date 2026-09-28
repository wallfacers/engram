# T19 · B/D 最小对照 run — 收数报告（2026-09-28 19:2x）

**任务来源**：维护者 16:3x 令（停五臂 $D8、跑 1540 题 k150 vs k150+jev 最小对照省资源）。
Run：`$D9 = bd-pilot-20260928T163756`（盒上 `/root/autodl-tmp/051-jev-arms-runs/`），16:38:17 点火
（runner PID 24790），19:13:39 测量完成，wall **2h35m**（五臂版预计 5.6h，省 55%）。
本地产物：`pilot-artifacts/bd-*`（6 件）；原始 journal 在 `.scratch/t19-bd-collect/`（3,080 filter 行 + 7,700 rows）。

## 1. Run 状态：测量完整，rc=1 是第三个 harness 缝（已修代码根治）

| 事实 | 值 |
|---|---|
| 测量 | **rows=7700/7700**（五臂全测：检索/打包/filter journal），answer+judge 仅 B/D=3,080 次，invalid=false |
| jevRunValidity（T19 receipt 修复） | **全绿**：1540/1540 题、7700/7700 行、五臂行数相等、identity/cap/compliance 全 1（`bd-summary.json`） |
| gated.exit | **1** — `B0 continuity was declared but b0_continuity_summary.json is missing` |
| 根因与修复 | **三号缝（commit `96ef9fe`）**：jev 门要求 b0_continuity_summary.json，但它是 B0 实验专属产物（`Stage != "b0"` 构造性 invalid），arms run 永远无法合法产出——$D7 时代被更早的 receipts 错误挡住从未走到此层。修复=声明时写**诚实 jev 形态声明回执**（declared=true + judge 模型/题集 digest/control hash 锚 + 明注「无机器 B0 run 背书」），门看到它、fail-closed 不弱化。**3-rep 正式链不会复发**（B0+receipts 两缝都已根治）。 |
| 附注 | verdict 本身已完整落盘（fail-closed 语义=失败也写）；SC 数字与 contrasts 全部有效，`validity_error` 只是记录字段。 |

## 2. 核心数字（1,540 题配对，judge=deepseek-flash，1-rep）

| 臂 | 正确率 | mean shown | mean answer-input tok |
|---|---|---|---|
| **B（k150 全塞）** | **80.00%** | 150.0 | 8,408 |
| **D（k150 + jev θ=0.5）** | **78.70%** | 5.11 | 817 |

**B→D = −1.30pp，McNemar p=0.142（不显著）**；翻转 B-only 94 / D-only 74 / both 1,138。
压缩：**10.3× token**（8,408→817）+ 29× 条目（150→5.11）。空注入：D 1.36%（21/1540）、
D-noRelax 2.53%（39/1540）、B 0%。

### 与上轮（语义信号死的 $D7）对照——BUG ① 的真实代价

| 指标 | $D7（BUG ①：embed 全程 0 流量） | $D9（修复后） | 变化 |
|---|---|---|---|
| B 绝对水位 | 60.65% | **80.00%** | **+19.35pp ← 语义信号值这么多** |
| B→D 差距 | −3.77pp，**p=3.4e-05（显著回归）** | **−1.30pp，p=0.14（不显著）** | 差距收窄 2.5pp + 显著性翻转 |
| D 空注入 | 7.99% | 1.36% | −6.6pp |
| D mean tokens | 420 | 817 | 池内容变实（kept 文本更长） |

**判读**：
1. **BUG ①（语义信号静默丢失）是上轮绝对水位崩塌的主因**（+19.3pp），证据形态嫌疑相应降权——
   B 从 60.65 回到 80.00，与「k150+1-rep+无多数票」的合理预期一致（历史 91.10 = 3-rep 多数票
   +thinking+tk150 完整配方，1-rep 单次天花板 ~86-88一带，80.00 在合理带内）。
2. **SC-001 仍 HOLD**（−1.30pp > −0.5pp 容差），但「no significant regression」半边已过。
   θ=0.5 仍偏狠（kept mean 5.10、60/3080 行 kept=0），θ 降到 ~0.35-0.4 或 KShowMax 提到 16
   是把 D 拉进 −0.5pp 门的首批旋钮——下次 run 最便宜的验证。
3. SC-002 PASS（D=9.7% of B tokens）；SC-007 PASS（filter p50 1,091ms / p95 2,176ms / $0，
   3,068 次真实调用）。

## 3. Filter journal digest（3,080 行）

- degraded **13 行（0.42%）**→7 题：`(0,58) (1,78) (4,59) (5,61) (5,62) (6,75) (9,65)`
  （6× negative-cache 30s 窗口内免网络调用降级 + 7× shard failure 降级走 top-N 兜底）
- 真实调用延迟（n=3,068）：p50 1,092ms / p95 2,176ms / max 15.9s
- selection：kept mean 5.10 / median 4 / kept=0 行 60（1.9%）
- 预算：retrieval_calls 7,700 = 冻结上限（精确打满）；filter_calls 3,080 独立类

## 4. 降级行补齐（维护者 11:3x 令「一定要补齐」——待下次开机执行）

按既定时序（收数→关机→下次开机补齐→再关机），本次未执行，**7 题清单已冻结在上方**。
补齐实现设计（供下次开工）：
- 需 `--jev-backfill` 类 harness 增量（单题重走 retrieve→filter→pack→answer→judge），
  rows 里 shown 文本未持久化、derived（SC-003/005 输入）也未持久化，故补齐需重走管线而非
  改行；同时建议 run 落盘 `jev_arms_derived.jsonl`（顺手修掉持久化缺口）。
- 成本 ≈ 7 题 ×（2 filter 调用 + 1 重答 + 1 重判）≈ 几毛钱；影响上限 ~0.45pp（7/1540）。
- 产出：原版/补齐版 verdict 并列，标注「pilot + degraded-row backfill patch」。

## 5. 下一步决策点（供维护者）

1. **θ 调优追试**（首选，最便宜）：θ 0.5→0.35-0.4 或 KShowMax 12→16，B/D 复刻本轮命令，
   目标把 −1.30pp 拉进 −0.5pp 门（p 已不显著，只差幅度）。
2. **3-rep 全量**：三缝已根治（receipts d605170 + B0 96ef9fe + answer-arms d554e40），正式链
   不会白烧——但建议先过 θ 门再上 3-rep（否则 SC-001 大概率仍 HOLD）。
3. B 水位 80.00 已进合理带，证据形态诊断降权为次优先。

## 6. 盒子处置

按维护者指令：收数完成、文档 push 后**关机**（本次已授权）。小结果已双保险
（本地 pilot-artifacts/bd-* + 已 push；盒上 run 目录原样保留在数据盘）。

*End of report.*
