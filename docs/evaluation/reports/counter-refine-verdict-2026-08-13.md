---
title: LME counter-refine 全量组合臂验证 — 2026-08-13
summary: M3 反证据验证门的 Qwen 全量 500 运行完成；同批 clean 重判中 counter-refine+trace 组合臂 86.40% vs trace-off baseline 86.80%，McNemar p=0.8776。该组合配方无正向信号，但 trace 未对齐使 counter-refine 独立效应不可识别；可按成本收益停止投入，不能写成 isolated causal NO-GO。
status: done
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [research, longmemeval, counter-refine, diagnostic, harness-infra]
---

# LME counter-refine 全量组合臂验证 — 2026-08-13

## 裁决

**已测试的 `counter-refine + trace-mediation` 组合配方没有正向信号；counter-refine 的独立因果效应未识别。**

同批 clean 重判（1000/1000 judge 调用完成）：

| 臂 | 得分 | 命中 |
|---|---:|---:|
| counter-refine + trace-mediation | 86.40% | 432/500 |
| A baseline：force-answer、trace off | 86.80% | 434/500 |
| 观测差 | −0.40pp | −2 |

配对 discordants：组合臂对 / baseline 错 = 20，组合臂错 / baseline 对 = 22；McNemar exact 双侧
`p=0.8776`。这表明该**组合臂**在这次单次全量运行中没有净收益，且差异落在噪声内。

由于两臂的 trace 配置不同，观测差同时包含 counter-refine、trace 及其交互，不能推出
counter-refine 独立增量为零或为负。若按额外一次 answer 调用/题的成本收益门，可以停止继续投入；若要
下 isolated causal verdict，仍需补 `--trace-mediation=false --counter-refine` 的同配置对照。

## 背景与机制

[score-increase-plan](score-increase-plan-2026-08-12.md) 的 M3 机制为：草稿 `a0` → 从已检索 hits
中选择与草稿相关的候选内反证据 → REVISE/KEEP → 在空结果、IDK 或调用错误时保留 `a0`。flag
`--counter-refine` 默认关闭。

flash 先导曾在 10 道错题上救回 2 道，足以支持启动全量验证，但它是小样本方向信号，不是 LoCoMo
全量结论。本次 LME 组合臂没有复现出端到端净收益。

## 并发问题与 harness 修复

### 观察

早期全量运行在 build/answer 阶段长时间无进展。SIGQUIT 栈显示大量 goroutine 等待 modernc SQLite
mutex、embedder drain 与 GC；旧实现会为 500 个 LongMemEval conversation 同时启动 goroutine，而
`--concurrency` 只限制 LLM 调用，不限制 SQLite build/retrieve 工作。

### 已落地修复

commit `9037bde` 在 eval harness 中加入：

- build 阶段并发门：16；
- answer/retrieve 阶段并发门：`opt.concurrency`，本次为 32。

修复后本机观察到 build 500 完成、answer 正常推进并跑完全量。16/32 是本次硬件与工作负载上验证可用
的设置，不是通用“安全上限”。改动仅在评测 harness，engine 未修改。

### WAL/SHM 边界

被 SIGKILL 的实验目录曾遗留 `*.db-wal`/`*.db-shm`，但残留文件本身不能证明存在永久 OS 文件锁，
本次提交也没有实现自动删除 sidecar。对需保留的 SQLite store 不应直接删 WAL：应先确认无活进程并做
checkpoint/integrity 检查；对完全可再生的实验 store，清理整个明确 run store 后重建更安全。

## 方法

- 数据：新组合臂 results 500 条 + 复用的三臂 A baseline predicted 500 条；
- clean：使用与 harness 一致的 final-answer 提取；
- judge：DeepSeek endpoint，thinking disabled、temperature 0、max tokens 512；
- 两臂 1000 次 judge 调用同批交错，以减小跨批 judge 漂移；
- 配对键：同一 LME question；显著性为双侧 exact McNemar。

重判此前在 300/1000 时因机器关机中断，随后从已有产物续跑到 1000/1000。`lme-cr-200` 是被全量
任务替代的空 run，不参与结果。

## 配置混杂（硬边界）

- 组合臂运行时没有显式关闭 `--trace-mediation`，因此采用默认 `true`；`trace-gate.jsonl` 500 行及
  cost 中 trace calls=650 / 7.4M input tokens 证实 trace 路径确实执行；
- A baseline 显式设置 `--trace-mediation=false`。

因此本报告的唯一稳健结论是“这个混合配方没有正向信号”。不能用 LoCoMo 上不同模型、检索栈或数据集
的 trace/relation 结果替代本实验缺失的 LME trace 对照；另一项 LME 探索中
`trace-mediation + relation-context` 组合甚至呈负向，更说明效应符号不能先验假定。

## 诚实边界与复现缺口

- 单次 answer generation；不显著不等于等效，粗略区间仍容许小幅正向或负向；
- “453/500 pred 含反证据分析”只能证明相关文本出现，不能等同 453 次有效 REVISE 或答案改变；
- 绝对分不与历史 3-repeat 84.60% 跨批比较；
- `rejudge_cr.py`、`run-cr.sh`、`lme-contract.sh` 与 artifact SHA256 未入仓，当前证据不能由仓库独立
  复现；后续实验必须提交命令、数据/模型 revision、失败重试计数和 hashes；
- 旧产物的 answer-regime 字段没有绑定 trace/counter/relation 等后处理开关；当前版本新建的 run-dir
  已把这些开关纳入 fingerprint，并让 LME 实验 prompt 绑定实际字节 digest，配置变化会 fail-fast。
  这不能追溯补齐旧 `regime.json` 的缺失状态，因此旧 run-dir 一律不得续跑；
- 远程盘点时 `lme-counterrefine` 的三类 500 行产物完整，且无运行中进程；该运行状态记录不是长期
  artifact 保证。

## 下一步

默认保持 off。基于“组合臂零收益 + 每题额外调用”的成本，可将本路线停止投入；这是一项资源决策，
不是 counter-refine isolated effect 已被因果证伪。只有维护者仍需要独立归因时，才值得补完全对齐的
trace-off paired repeats。
