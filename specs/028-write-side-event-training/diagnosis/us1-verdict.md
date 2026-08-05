# 028 US1 Verdict: 教师抽取器零训练验证 — **GO**

**日期**: 2026-08-05 · **门**: [pair-gate](../contracts/pair-gate.md) US1 · **Spec**: [spec](../spec.md)

## 结论

**GO → 进 US2（训练抽取器）。** 假设"抽取能力是写侧 event 结构的瓶颈"被坐实：托管教师（DeepSeek-flash + 时间锚定强化 prompt）把语义锚定率从 7B 的 ~5% 拉到 **86.4%**，端到端 event 臂从 027 的 −26.2pp（p=0.0016 显著落后）收窄到 **−6.0pp（p=0.44，不显著，与 chunk 持平）**。

## 教师抽取（5518/5882 events，1.8h，SaaS 线成本）

| 指标 | 7B（027） | 教师（028） | 门 |
|---|---|---|---|
| 语义锚定率 | ~5% | **86.4%**（1329 时间语义事件）| ≥50% ✅ |
| raw 锚定率 | 5% | 39.9% | — |
| schema 合法率 | — | 100% | ≥95% ✅ |
| 失败率 | — | 6.2%（schema 172 + parse/json，多为无事件寒暄）| 可接受 |
| 坏 ts 格式 | — | 0 | — |

## 配对（84 题 × 3 reps majority，同 store/answerer/judge）

| 臂 | majority | vs chunk | McNemar |
|---|---|---|---|
| chunk（027 基线）| 42/84 = **50.0%** | — | — |
| **teacher event（028）** | 37/84 = **44.0%** | **−6.0pp** | a=16 b=11, **p=0.4421** |

分类别：multi-hop (C✓T✗−C✗T✓)=+3、temporal +2——方向都略倾向 chunk，但均不显著。

### 与 027 对比（关键）

| | 027 event(7B) | 028 event(teacher) |
|---|---|---|
| majority | 23.8%（20/84）| **44.0%**（37/84）|
| vs chunk | −26.2pp | −6.0pp |
| McNemar | p=0.0016（显著落后）| p=0.44（持平）|

**教师时间锚定把 event 表示从"显著崩盘"拉回"与 chunk 持平"。** 写侧结构本身有效，瓶颈是抽取能力。

## GO 判定（pair-gate US1）

1. 语义锚定率 86.4% ≥ 50% ✅
2. event−chunk −6.0pp ≥ −10pp ✅

**假设成立 → 进 US2：训练一个时间锚定抽取器**（目标：时间锚定率 ≥70% + 端到端 event ≥ chunk，008 铁律）。

## 训练数据已就绪（US2 输入）

- `~/.claude/engram-028/train-028-v1.jsonl`：**5313 条**（teacher 事件 + 原文 + 绝对时间标签），**语义锚定率 100%**，by_conv 均匀（389–626/conv）
- 已修复 dia_id 跨 conv 重复的匹配 bug（key = `conv_id:dia_id`）

## 产物

- 教师投影：`~/.claude/engram-028/teacher-project.json`（5518 events）
- 配对结果：`~/.claude/engram-028/pair-teacher/run-{1,2,3}.jsonl`
- 远端备份：`/root/autodl-tmp/eval-backup-028-us1/`（已关机）
- 成本：DeepSeek-flash 5882 调用（低个位数美元量级，SaaS 线），AutoDL 计时已停（配对后关机）
