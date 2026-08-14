---
title: AutoDL 慢跑排查手册 — vllm 满载但记录不涨
summary: 2026-08-14 实战排查记录。核心是区分两种"慢"：(a) 正常批式 flush（单 conversation 前几分钟 0 记录）与 (b) 病理性请求风暴（vllm 持续满载答题、结果记录冻结、conversation 永不完成、答案全废）。给出复制即用的诊断命令、健康基线（~52 答案/分钟 @k150+thinking）、已排除项清单、有界复现方法。根因未闭合，复发时的下一步=SIGQUIT 拿 goroutine dump。
status: active
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-14
tags: [operations, autodl, vllm, locomo-bench, troubleshooting, performance]
---

# AutoDL 慢跑排查手册（vllm 满载但记录不涨）

## TL;DR 判据

先看三个数（命令见 §3），30 秒定位属于哪种情况：

| 现象 | 结论 | 动作 |
|---|---|---|
| `conversation done` 在推进 + records 每 6–9 分钟一批涨几百 | **健康**，别动 | 等（k150+thinking 全量 3 reps×2 臂 ≈ 3h） |
| `conversation done`=0 恒定 + records 冻结几十条 + `num_requests_running`=32 恒满 | **病理（请求风暴）**，答案在生成但全废 | `kill -QUIT <pid>` 拿 goroutine dump，别让它烧 |
| 进程消失、无 run.exit | 被 SIGHUP 杀（没 setsid）或 OOM | 按原脚本 setsid 重启（注意 fail-closed 拒绝 resume，只能全新 run-dir 整跑） |

**健康基线（2026-08-14 实测）**：k150+thinking、concurrency 32、RTX PRO 6000 Blackwell 96G、
Qwen3.6-35B-A3B-FP8 → **~52 答案/分钟**。1 conv×1 rep（304 答案/6min）与 2 conv×3 reps
（1398 答案/27min、exit 0）两次有界验证一致。全量 1540×3×2=9240 答案 ≈ 3 小时。
任何显著低于此的速率（<20/分钟）先按本手册查，不要直接归因"模型慢"。

## 两种"慢"，先分清

1. **正常批式 flush（不是病）**：locomo-bench 的结果记录按 conversation 接近完成时批量落盘。
   一个 conversation（~150 题×2 臂）健康耗时 6–9 分钟，期间 records 可能一直是 0，然后一口气
   +300 左右。**前 7–10 分钟 records=0 是正常的**；判据看 `conversation done` 是否推进、
   vllm 是否在干活。
2. **病理性请求风暴（真病）**：vllm 每 分钟完成 ~50 个满尺寸（~10k prompt）请求、GPU 100%、
   32 槽恒满，但 records 冻结、`conversation done` 永不出现。2026-08-14 病例：41 条记录后冻结，
   之后 70 分钟 ~3500 个请求全部作废。**harness 在发答案请求但结果全部被丢弃**，等下去只会烧钱。

## 诊断命令（复制即用）

```bash
# 1) harness 推进吗：conversation 完成数 + 各 rep 记录数
grep -c 'conversation done' <run-dir>/run.log
for d in <run-dir>/run-*; do echo -n "$(basename $d)=$(cat $d/results-*.jsonl 2>/dev/null | wc -l) "; done
# 注意：--repeats 1 时结果写在 run-dir 根目录（无 run-1/ 子目录）；--repeats 3 写 run-1/2/3/。
# 监控错路径会把健康 run 误判成卡死（本次实战踩过两次）。

# 2) 记录冻结检测：文件 mtime 停了就是冻结
stat -c '%y %n' <run-dir>/run-*/results-*.jsonl

# 3) vllm 在干嘛：在飞数 + 完成速率（60s 两次采样差值）+ 吞吐
curl -s http://127.0.0.1:8000/metrics | grep -E 'num_requests_(running|waiting)'
curl -s http://127.0.0.1:8000/metrics | grep '^vllm:request_success_total'   # stop 计数器，采样差值=完成率
curl -s http://127.0.0.1:8000/metrics | grep preemption                     # 抢占应为 0

# 4) 请求从哪来（排外部盗用）：8000 的 ESTABLISHED 应全部来自 127.0.0.1
awk 'NR>1 && $4=="01" {split($3,a,":"); if (a[2]=="1F40") print a[1]}' /proc/net/tcp | sort | uniq -c

# 5) 服务端请求速率佐证：vllm 自身日志每条 POST 一行（stdout 落点见 /proc/<vllm-pid>/fd/1）
F=$(readlink /proc/<vllm-pid>/fd/1); tail -200 $F | grep -c 'POST /v1/chat/completions'
```

病理特征组合：`request_success_total` 的 stop 计数 ~40–60/分钟在涨、prompt 吞吐 5–10k tok/s、
gen 吞吐 ~1.1k tok/s（prompt:gen ≈ 7:1，与 k150 答案形态一致 → 请求确实是答题）、32 槽恒满、
但 records mtime 停更、无 `conversation done`。

## 已排除项（2026-08-14 逐项实证，别再查一遍）

| 嫌疑 | 排除证据 |
|---|---|
| judge（DeepSeek API）慢/挂 | 直连实测 3 次 0.85–1.1s、200 OK |
| SSE 流卡死（16384 老坑） | 直发 10.5k prompt 流式请求 5.9s 完整返回含 usage chunk；answer vllm max-model-len=32768 |
| KV cache 溢出/抢占重算 | `num_preemptions_total=0`、kv 使用率 18% |
| harness 重试风暴 | 代码 `gateUsageAttempts(attempts=1)`，regime `provider_attempts=1` |
| 外部客户端盗用 8000 | 32 条 ESTABLISHED 全部 127.0.0.1；ps 无其他消费者 |
| embed 侧瓶颈 | bge `--max-num-seqs 1` 在位；backfill 启动时 ~40s 内完成 |
| 环境性单次故障 | rm -rf 后同 flag 重启复现（16:35 第二次同样病发） |

容器限制：`strace` 不可用（ptrace 被禁），`ss`/`/proc/net/tcp` 部分输出异常，用 §3 的 awk 解析。

## 有界复现方法（下次排查的标准第一步）

不要直接在全量上猜。用小规模探针定位触发面，每个只要几分钟：

```bash
# 5 题探针（~40s）：验证端到端健康。健康=10 答案 10 请求。
# 单对话全量（~6min，304 答案）：--conversations 1
# 两对话 3 reps（~27min，1398 答案）：--conversations 2 --repeats 3
```

2026-08-14 结果：`--conversations 1`×1rep 与 `--conversations 2`×3reps **全绿**（52 答/分钟、
exit 0），病理只在 10-conv 全量出现过（两次）。触发面与 conversations 数量/repeats 组合相关，
精确阈值未测。18:01 第三次全量重试起步速度 ≈ 病历 5 倍（7 分钟 39 条 vs 34 分钟 41 条），
谨慎乐观但未确认。

## 运维 gotcha（与病理无关但这次全踩了一遍）

1. **启动必须 setsid**：15:07 的 run 就是前台启动，SSH 会话一断整条 bash 连 harness 被 SIGHUP，
   死状=日志无错误、无 run.exit、记录停更——与病理表象相近，先用 run.exit/日志尾部区分。
2. **unified 配对协议 fail-closed 拒绝 resume**：`refuses journal resume ... use a fresh
   --run-dir`。半截 run 只能整跑，不能续；杀前想清楚。
3. **监控路径陷阱**：`--repeats 1` 结果在 run-dir 根目录、`--repeats 3` 在 run-N/ 子目录。看错
   路径会把健康 run 误判为 0 记录卡死。
4. **单次 attempt 语义**：任何一次 provider 调用失败即整个 run 出错退出（provider_attempts=1，
   为成本账目真实性设计）。瞬时网络抖动=整 run 报废，重跑前先看 run.exit 与日志尾部。
5. Go 程序诊断：SIGQUIT 会 dump 全部 goroutine 到 stderr（=run.log）**并杀死进程**——只对
   已经废掉的 run 用；不要对健康 run 用（023 的老教训）。

## 根因状态：OPEN

病理根因未闭合。已知：答案请求持续发出并完成、结果不落盘、conversation 永不完成、
2-conv 规模不复现。下一次复发时的标准动作：

1. 确认病理判据（§TL;DR 第二行）成立；
2. `kill -QUIT <harness-pid>`，等 5–10s；
3. 在 run.log 尾部找 goroutine dump，重点看：blocked 在 channel/semaphore 的 goroutine 数量
   与栈位置（journal 写锁 / agg mutex / sqlite 单写连接 / qwg.Wait）；
4. dump 归档到 `docs/operations/evaluation/` 并更新本手册。

## 相关

- 现场交接与该 run 的业务背景：[042-unified-k150-run-handoff-2026-08-14.md](042-unified-k150-run-handoff-2026-08-14.md)
- 远程 GPU 常规 runbook：[remote-gpu-runbook.md](remote-gpu-runbook.md)、[locomo-runbook.md](locomo-runbook.md)
- 2026-08-14 病历现场（未删）：box `/root/autodl-tmp/042-scratch/sick-run-forensic/`；
  探针与推演脚本：box `/root/autodl-tmp/042-scratch/`
