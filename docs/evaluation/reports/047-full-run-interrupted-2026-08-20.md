---
title: 047 — LoCoMo 全量 3-rep 运行中断记录
date: 2026-08-20
tags: [evaluation, locomo, chunk-granularity, spec-047, incident]
status: INCOMPLETE — 不得作为全量分数或 92% 结论
---

# 047 全量运行中断记录

本记录冻结 2026-08-20 的全量正式运行状态。AutoDL 实例在运行期间因欠费不可用；
因此本次**没有**得到可发布的 LoCoMo 全量分数、配对差、p 值或 clean verdict。
203 题 probe 的结论仍以
[047-granularity-probe-3rep-2026-08-19](047-granularity-probe-3rep-2026-08-19.md)
为准，不能把其中的约 92% 外推称为全量正式分数。

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
