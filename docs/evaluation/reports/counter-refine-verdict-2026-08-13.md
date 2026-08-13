---
title: LME counter-refine 全量验证 verdict — 2026-08-13
summary: M3 反证据验证门（score-increase-plan 2026-08-12）Qwen 全量验证。死锁排查（build/answer 500 并发 SQLite + store 残留锁）→ harness 并发修复（build=16 / answer=32）。同批重判 counter-refine vs 三臂 A baseline，McNemar 归因。结果待重判。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [research, longmemeval, counter-refine, verdict, harness-infra]
---

# LME counter-refine 全量验证 verdict — 2026-08-13

## 背景

[score-increase-plan](score-increase-plan-2026-08-12.md) 的 **M3 反证据验证门**（`--counter-refine`，default-off）
harness 已在 `1cf1a2b` 实现，flash 先导 2/10 正向信号，**Qwen 全量验证此前被云机占用阻塞**。
本报告 = 在克隆机（RTX 6000D 85G）上跑通 Qwen 全量验证。

**机制**：草稿 `a0` → 从检索 hits 选含 `a0` 关键词的记忆作反证据 → REVISE（+1 LLM call）→
空/IDK/err 回退 `a0`（fail-safe）。**实测 453/500 题（90.6%）pred 含反证据分析**，机制确实在跑。

## 死锁排查与 harness 修复（宪法 IV 归因）

### 现象
全量 500 在 build 或 answer 阶段反复死锁（`buildWG.Wait` / `wg.Wait` 永久等待，0 answered），
SIGQUIT goroutine dump 显示大量 `sync.Mutex.Lock`（modernc SQLite）+ `chan receive`（embedder drain）+ GC 风暴。

### 两个独立根因
1. **build/answer 阶段 500 并发 goroutine 无限制**：`main.go` 的 build（`for ci := range convs` 全启动）
   与 answer（同模式）都对 500 个 conv 起 goroutine，`concurrency` 只限制 LLM 调用不限制 SQLite 操作。
   500 并发 SQLite 句柄打开/检索 → 锁 convoy 死锁。
   （三臂 8-12 能跑是 store 干净 + 时序未撞上；58b180d 的 commit message 已自述
   "500-parallel LME retrieve phase serialized on the SQLite mutex ~18min"。）
2. **kill -9 留下的 store 残留锁**：进程被 SIGKILL 后 `.db-wal`/`.db-shm` 残留（500 个 × 4-5MB），
   其中 `-shm` 的残留锁让后续 run 打开 store 时永久等锁，且不清理会连锁恶化。

### 修复（`cmd/locomo-bench/main.go`，纯 eval harness，不触引擎）
- build 阶段加 `buildSem`（并发 16）
- answer 阶段加 `ansSem`（复用 `opt.concurrency`=32，即 vllm `max-num-seqs` 上限）
- run 前清理 `*.db-wal`/`*.db-shm`（残留锁）

修复后全量验证：build 500 完成 → answer 正常启动（GPU 100%、vllm 32 reqs 打满），无死锁。

### 并发安全经验值
| 操作 | 约束 | 安全并发 |
|---|---|---|
| build（SQLite 写：chunks/FTS/backfill） | SQLite 写锁 | ≤16 |
| answer/retrieve（SQLite 读 + vllm） | vllm `max-num-seqs` | ≤32 |
| judge（云端 DeepSeek API） | 无本地资源约束，但受 answer 瓶颈 | 共享 32 足够 |

## 方法（同批重判）

- 数据：counter-refine results（500，新跑）+ **三臂 A baseline predicted（500，8-12 复用，不重跑）**
- `rejudge_cr.py`：clean 提取（`extractFinalAnswer`）→ DeepSeek anthropic 端点
  （`thinking:disabled`、temp 0、max_tokens 512、judge prompt 与 harness 逐字一致）→ **1000 次调用同批交错**
- McNemar exact（配对二项）

## 结果

**⚠️ 同批重判被机器关机中断（judged 300/1000）**，counter-refine vs baseline 的 McNemar 归因**未完成**。

- 重判脚本 `rejudge_cr.py`（同批交错 1000 次 judge）在 300/1000 时机器关机（AutoDL 停机）。
- 重判输出在克隆机数据盘 `/root/autodl-tmp/rejudge-cr.out`（关机保留，开机可续跑）。
- **当前只有 harness 原始分数**（非归因依据）：counter-refine 432/500 = **86.4%**（run 时 judge，跨批不可比）。
- 三臂 A baseline 的 predicted（500 条，8-12）已在数据盘，续跑重判即可归因。

## verdict

**待续**：counter-refine 机制已确认在跑（453/500 REVISE 触发），Qwen 全量链路已跑通，
但同批重判被关机中断，**无法下涨点/NO-GO 结论**。续跑重判后补 verdict。

## 诚实边界

- 单次 run（repeats=1），与三臂 A（1-rep）同批才可比；绝对分不与历史 3-rep 84.60% 比（跨批 judge 漂移 ±2.5pp）。
- 453/500 REVISE 触发率是 pred 文本含反证据关键词的计数，非"REVISE 改变答案"率。
- build/answer 并发修复是 eval harness 改动，检索/引擎行为不变（`--counter-refine` 本身 default-off）。
- harness 原始 432/500 = 86.4% **不可作为归因**（judge 跨批漂移），必须等同批重判。
