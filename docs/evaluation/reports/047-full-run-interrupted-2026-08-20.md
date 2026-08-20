---
title: 047 — LoCoMo 全量运行:中断与恢复后的 1-rep clean verdict
date: 2026-08-20
tags: [evaluation, locomo, chunk-granularity, spec-047, incident, verdict]
status: 1-rep clean 判定完成(+1.36pp ns);3-rep 未做(维护者成本决策)
---

# 047 全量运行:中断与 1-rep clean verdict

**最终判定(2026-08-20 恢复后)**:机器续费恢复后核验发现 treatment run-1 已
完整落盘(1540 行,欠费中断发生在 run-2 建目录时)——**1-rep 全量配对零额外
跑动成本成立**。维护者指令"不要跑 3 次全量,先跑一次"就此满足,未启动任何新 run。

| 口径 | ctl 900/k30q28 | trt 450/k75q45 | 配对差 | 翻转 | p |
|---|---:|---:|---:|---|---:|
| 在线 mem0-aligned judge | 88.64% (run-1) | 89.87% | +1.23pp | 41:60 | 0.073 |
| **clean 重判**(flash, 同批) | **85.97%** | **87.34%** | **+1.36pp** | 58:79 | 0.087 |

- clean 重判:2026-08-20 本地 32-worker 3080 次调用 0 错误(~¥2,协议逐字复刻
  harness:`judgeMem0AlignedSystemPrompt` + `buildJudgePrompt` + `extractFinalAnswer`
  + `parseJudgeVerdict`;脚本 `/tmp/qwen38-eval/047-full/clean_rejudge.py`,
  产物 `clean-{ctl,trt}-run1.jsonl`)。
- judge 作弊量 ~2.5-2.7pp(在线 vs clean),两臂近似对称抵消,配对差稳定 +1.2~1.4pp。
- ctl 三 rep 高度一致(88.64/88.83/88.64%),majority 88.96%——单 rep 波动小,
  1-rep 判定可信度高于一般预期。
- token:ctl 7204 vs trt 7774(+7.9%,与 203 probe 一致)。

## 对 probe 外推的修正(重要)

203 题子集 probe 的 +11.3pp 是 91 道关键题富集放大的;全量摊薄后 **+1.36pp
(clean, p=0.087, 未达 0.05)**。probe 报告中"全量外推 ~92%"**被实测推翻**——
真实全量 1-rep clean = 87.34%,外推高估 ~4.7pp(子集富集 + 加权外推假设
"88 关键题行为代表全量同类题"不成立)。README 已同步修正。

**诚实结论**:450 档方向为正、幅度小、未达显著;未超过 k150 混合口径(91.10%)。
是否转正生产配方(k30q28 → k75q45 换 +1.36pp ns 与 +7.9% token)由维护者决策;
3-rep 显著性确证(约 ¥30/3.5h)为可选后续。

## 中断事实记录(2026-08-20 凌晨,原样保留)

本记录冻结 2026-08-20 的全量正式运行状态。AutoDL 实例在运行期间因欠费不可用。

## 已启动的正式配置

- 源码 commit：`26b9e006379abe6d4e1c0586072ab8cf4a4ff398`；Linux bench binary SHA-256：
  `ace5a3e55ee1afb108bd723fee198cf08e8c1929f56969d773ea1503caca0d22`。
- 数据：LoCoMo 全量 1,540 题；两臂各 3 repeats；`--concurrency 32`、
  `--per-call-timeout 15m`、`--judge-mem0-aligned`、`--no-idk-retry`。
- control：既有 `032-store`，900-char，`k30 q28`。
- treatment：既有 `047-store-450`，`--chunk-target-chars 450 --chunk-max-chars 550`，
  `k75 q45`。
- 回答/抽取：本机 vLLM `Qwen/Qwen3.8-27B`；嵌入：本机
  `BAAI/bge-large-en-v1.5`；在线 judge：`deepseek-v4-flash`。
- clean 重判脚本已部署为 32-worker、非思考 Flash、mem0-aligned watcher；它只会在
  双臂全数成功后启动。

## 已验证进度

| 时间（北京时间） | 事实 | 状态 |
|---|---|---|
| 00:38 | control 臂启动 | 已验证 |
| 04:37 | control `900-k30q28` 写入 exit code `0` | 已验证 |
| 04:36–04:37 | treatment `450-k75q45` 启动，vLLM 32 路执行、无排队 | 已验证 |
| 04:37 | 诊断计数：`stop=4596`、`length=26`、`error=0` | 仅健康诊断，非评分结果 |
| 后续 | AutoDL 欠费，机器不可用 | 中断 |

`length` 是 vLLM 的结束原因计数，不能直接等同于 benchmark 错题或超时。上表的计数也
不替代结果 JSONL、manifest seal 与成对统计校验。

## 未完成项与影响

1. treatment 没有确认写入成功退出标记；`full.exit` 未写入。
2. clean rejudge 未启动，因而没有 9,240 个 clean verdict、majority 聚合或 McNemar
   统计。
3. 结果目录的完整性、每个 repeat 的 1,540 行、manifest/seal、在线 judge 用量及实际
   费用均未在本地复核；本仓库不保存这些远端 run 产物。
4. 因上述缺口，本次运行**不能**更新 README/Benchmarks，也不能支持“全量 92%”或任何
   全量优劣结论。

## 恢复要求

机器恢复后先检查数据盘上的
`/root/autodl-tmp/047-full-20260820-26b9e00/` 与备份是否仍在，再验证两个 arm 的全部
`run-1..3` 结果、退出码和 manifest。不要根据部分 JSONL 或 vLLM 计数补造统计。

若 treatment 或其 3-repeat 目录不完整，使用新的 run-dir 重跑**完整的双臂 3-rep
配对实验**：harness 的 `--repeats 3` 没有安全续跑语义，不能把部分 repeat 与新结果拼接。
完成后仅在空闲时段运行 clean rejudge，并以 clean majority、完整性校验与实际 usage
生成正式报告。实例结束后先备份小型结果/日志/store，再关机。

## 同次维护

[`scripts/deepseek-cost.py`](../../../scripts/deepseek-cost.py) 已按用户提供的
DeepSeek V4 新价更新：高峰价为 Flash ¥0.10/¥3/¥9、Pro ¥0.30/¥9/¥27 每百万
tokens（cache-hit / cache-miss / output）；新增 `peak|offpeak` 参数，空闲时段按半价。
