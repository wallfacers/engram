---
title: 047 — LoCoMo 全量运行:中断与恢复后的 1-rep clean verdict + 450@k150q90 配方
date: 2026-08-20
tags: [evaluation, locomo, chunk-granularity, spec-047, incident, verdict]
status: chain9(450@k150q90)1-rep clean +2.01pp p=.011 显著;3-rep 未做(维护者成本决策)
---

# 047 全量运行:中断与 1-rep clean verdict

## chain9(2026-08-20 下午):450-char × k150q90 — 047 线首个显著全量赢

前一节判定 450@k75q45(+1.36pp ns)后,归因显示 k75 档拿不到 k150 的体量题。
chain9 补测未测格子:**450-char 池 + k150 级预算**(q90),1-rep 全量 1540,
维护者指令"跑一次,别全跑,太贵了"。run 14:03-16:37(2h34m),store 复用
`047-store-450`,binary/协议与同批 ctl 一致(`--top-k 150 --chunk-quota 90
--chunk-target-chars 450 --chunk-max-chars 550`)。

| 配方 | 均值上下文 | 在线(同批) | clean 1-rep | 配对 vs ctl(clean) | p |
|---|---:|---:|---:|---|---:|
| 900-char k30q28(生产配方,ctl) | ~7.2K | 88.64% | 85.97% | — | — |
| 450-char k75q45 | 7.8K | 89.87% | 87.34% | +1.36pp(58:79) | .087 |
| **450-char k150q90(chain9)** | **14,473** | **91.30%** | **87.99%** | **+2.01pp(55:86)** | **.011 ✓** |

- clean 重判:1540 次调用 0 错误(~¥0.5);脚本
  `/tmp/qwen38-eval/047-full/clean_rejudge_chain9.py`,产物
  `clean-450k150q90-run1.jsonl`。
- 类别分解(净赢 = 31 题):single-hop +18(90.0→92.2%)、open-domain +5
  (60.4→65.6%)、multi-hop +5(86.9→88.7%)、temporal +3(82.2→83.2%)——
  全类别净正,非单一类别驱动。
- **vs 450/k75q45:+0.65pp(50:60)p=.39 ns** —— 在 450 池上把预算翻倍
  (7.8K→14.5K)的边际增益小;047 线的赢主要来自粒度本身,k150 配额只是补齐
  体量题下限。
- 在线 91.30% 是本栈批次内最高在线分;与 900@k150 混合口径 91.10%(Aug-18 批,
  k30 majority + 80 题重判归因)相比 **~15% token 节省**(14.5K vs ~17K),
  但两者口径不同,严格同口径对照需 900@k150 全量 clean(未跑,费钱)。
- 成本:box ~3.3h(¥18 级)+ 在线 judge + clean 重判 ~¥1;远端产物备份
  `/root/autodl-tmp/eval-backup-20260820-chain9/`,本地
  `/tmp/qwen38-eval/047-full/`;机器已关机、凭证已清。

**判定:GO(方向+幅度+显著性)**。是否转正生产配方(k30q28→k150q90,
token 7.2K→14.5K 换 +2.01pp 显著)= 维护者决策;3-rep majority 预计再 +
0.3-0.9pp(ctl 三 rep 波动参考),README 口径为 3-run clean majority,加行前
需补 rep 或标注 1-rep。



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
