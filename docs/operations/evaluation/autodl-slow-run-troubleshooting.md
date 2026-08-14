---
title: AutoDL 慢跑排查手册 — "记录冻结"是车队效应，不是病
summary: 2026-08-14 完整排查（含 goroutine dump 实证）。核心结论：locomo-bench 的 10-conversation 并行启动 + 单一 32 槽 FIFO 信号量产生车队效应——每个 repeat 的前 ~50-60 分钟 judge 请求排在 3040 个原始 answer 请求后面拿不到槽，records 冻结在头 2 分钟挤过去的 ~40 条，之后随 judge 排空一口气 +3000。这不是 bug、不要杀 run。判健康的指标是 vllm 请求速率（~50-65/min）而非记录数。本文档初版曾把该现象误诊为"病理性请求风暴"，导致两个健康 run 被误杀——教训一并记录。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [operations, autodl, vllm, locomo-bench, troubleshooting, convoy, queueing]
---

# AutoDL 慢跑排查手册（"记录冻结"= 车队效应，不是病）

## TL;DR 判据（30 秒）

| 现象 | 结论 | 动作 |
|---|---|---|
| `conversation done`=0、records 冻结在 ~40 条、**vllm 请求速率 ~50-65/min 且 running=32** | **健康，repeat 的 answer 相**（10-conv 全量前 ~50-60 分钟就这样） | **不要杀**。记录会在 judge 排空时（每 repeat ~55-65 分钟处）一口气 +3000，随后 10 个 `conversation done` 连发，进入下一 repeat |
| vllm `num_requests_running`=0、请求速率 ~0、无 convdone 推进 | 真停滞 | 查进程/日志错误 |
| 进程消失、无 run.exit | 被 SIGHUP 杀（没 setsid）或出错退出 | 看日志尾部；重启=原脚本（fail-closed 拒绝 resume，只能全新 run-dir 整跑） |

**健康基线（2026-08-14 实测）**：k150+thinking、concurrency 32、Qwen3.6-35B-FP8 →
vllm 完成请求 **~60-65/min**（≈52 答案/分钟）。全量 1540×3 reps×2 臂 = 9240 答案 ≈ **3-3.5 小时**。
1 conv（304 答案）≈ 6min；2 conv×3 reps（1398 答案）≈ 27min；均已 exit=0 验证。

## 根因：并行 conversation 启动 × 单一 FIFO 信号量 = 车队效应

代码事实（`cmd/locomo-bench/main.go`）：

1. 每个 conversation 一个 goroutine **并行**处理（`for ci := range convs { go ... }`），
   10 conv = ~3040 个 (question, arm) goroutine 同时启动；
2. answer 和 judge 走**同一个** 32 槽信号量（`gateUsage(sem, ...)`，`runner.go:181` 的 select），
   Go channel 阻塞发送严格 FIFO；
3. 每个 question 先 answer（排队一次）后 judge（answer 完成后**排到队尾**再排一次）。

推论：t=0 时 3040 个原始 answer 发送占据整个队列；任何 judge 重入都在 3040 个原始
answer 之后 → **前 ~3040/65 ≈ 47-60 分钟 judges 拿不到槽**，records 停留在启动头 2 分钟
（队列尚未塞满时）挤过去的那 ~40 条。之后 judges 集中排空（DeepSeek 1-2s/次 × 32 槽 ≈
几分钟），records 一口气 +3000，10 个 `conversation done` 连发。每个 repeat 重复一遍。

**goroutine dump 实证**（2026-08-14 18:18，run 启动后 17 分钟，box
`/root/autodl-tmp/042-scratch/goroutine-dump-18q.log`）：3215 个 goroutine 中 3037 个在
observer wrap 的模型调用处（`unified_answer_contract_eval.go:70`），其中 2142 个等 answer
槽、895 个等 judge 槽，全部 `[select]` 在 `runner.go:181`；32 个 streamLoop 在飞；
**零 mutex 死锁、零 starvation、零 IO 挂起**——纯粹是排队。

三种规模的实测全部吻合该模型：1 conv（队列 304：answer 相 4.7min + flush）、2 conv×3 reps
（608/repeat：t+7min 一口气 456 条）、10 conv（3040/repeat：冻结 ~50min 后 bulk flush）。

## 判健康的正确指标（按优先级）

1. **vllm 请求完成速率**：`request_success_total` 的 stop 计数器 60s 采样差值 ≈ 50-65/min → 在干活；
2. `num_requests_running` ≈ 32 恒满 → 队列健康；
3. records 数**只在 repeat 边界附近有信息量**——repeat 前半程冻结是预期行为；
4. `conversation done` 在 10-conv 全量下**每 repeat 只会在 ~55-65 分钟处连发出现**，
   不要用"20 分钟没有 convdone"当死亡判据。

## 已排除项（别再查）

judge（DeepSeek 实测 0.85–1.1s）、SSE 流（10.5k prompt 5.9s 完整返回）、KV/抢占（0 preemption、
18% 使用率）、重试循环（`gateUsageAttempts` attempts=1）、外部客户端（32 连接全 127.0.0.1）、
embed（`--max-num-seqs 1` 在位）、mutex 死锁（dump 实证无）。

## 误诊教训（2026-08-14 实录，防重演）

本手册初版把"records 冻结 + vllm 满载"误诊为"病理性请求风暴、答案全废"，据此**错杀了两个
健康 run**（16:35 启动者死于 ~75min 处——事后核对 17:37→17:50 记录 31→41 在涨，正处于 judge
排空起点；18:01 启动者死于 17min 处）。两次误杀合计浪费 ~2.5 小时机时。教训：

- **大规模并行 run 的"无产出"必须先看请求速率（服务端计数器），不能只看产出记录**；
- 产出侧的批式 flush 语义会放大"看起来卡死"的错觉；
- 下 kill 决定前，若 vllm 仍在满速服务，先做一次 goroutine dump（对将死 run 用 SIGQUIT）
  或 60s 速率采样，用数据确认真停滞。

## 运维 gotcha

1. **启动必须 setsid**：SSH 会话断开 = SIGHUP = 整条 bash 连 harness 一起死（日志无错误、
   无 run.exit 是其死状）。
2. **unified 配对协议 fail-closed 拒绝 resume**：`refuses journal resume ... use a fresh
   --run-dir`。半截 run 只能整跑。
3. **监控路径陷阱**：`--repeats 1` 结果在 run-dir 根目录，`--repeats 3` 在 run-N/ 子目录。
4. **单次 attempt 语义**：一次 provider 调用失败即整 run 出错退出（成本账目真实性设计）。
5. SIGQUIT = goroutine dump + 杀进程，只对已判定废弃的 run 用。
6. 容器限制：strace 不可用（ptrace 禁用），部分 ss 输出异常（用 /proc/net/tcp awk 解析）。

## 诊断命令（复制即用）

```bash
# 请求速率（判健康的第一指标，60s 两次采样）
curl -s http://127.0.0.1:8000/metrics | grep '^vllm:request_success_total.*stop'
curl -s http://127.0.0.1:8000/metrics | grep -E 'num_requests_(running|waiting)'

# 产出与推进
grep -c 'conversation done' <run-dir>/run.log
for d in <run-dir>/run-*; do echo -n "$(basename $d)=$(cat $d/results-*.jsonl 2>/dev/null | wc -l) "; done

# vllm 自身请求日志（POST 速率佐证；stdout 落点见 /proc/<vllm-pid>/fd/1）
F=$(readlink /proc/<vllm-pid>/fd/1); tail -200 $F | grep -c 'POST /v1/chat/completions'

# 请求来源核查（应全为 127.0.0.1）
awk 'NR>1 && $4=="01" {split($3,a,":"); if (a[2]=="1F40") print a[1]}' /proc/net/tcp | sort | uniq -c
```

## 有界复现/验证方法

排查慢跑的标准阶梯（每步几分钟到半小时）：5 题探针（40s，端到端）→ `--conversations 1`
（6min，304 答案）→ `--conversations 2 --repeats 3`（27min，1398 答案）→ 全量。小规模全绿
+ 全量"冻结"= 车队效应的典型指纹（队列长度随 conversation 数线性放大）。

## 相关

- 现场交接：[042-unified-k150-run-handoff-2026-08-14.md](042-unified-k150-run-handoff-2026-08-14.md)
- 远程 GPU runbook：[remote-gpu-runbook.md](remote-gpu-runbook.md)、[locomo-runbook.md](locomo-runbook.md)
