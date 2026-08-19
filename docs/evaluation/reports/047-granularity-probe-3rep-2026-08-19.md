---
title: 047 US2 — 450 档粒度 probe + 3-rep majority verdict(203 题子集配对)
date: 2026-08-19
tags: [evaluation, locomo, chunk-granularity, spec-047]
status: GO(3-rep majority +11.3pp, p=0.001);open-domain 负向待全量复核
---

# 047 US2 probe/3-rep verdict

Spec: [047-chunk-granularity-density](../../../specs/047-chunk-granularity-density/spec.md) ·
前置 US1: [047-granularity-sweep-2026-08-18](047-granularity-sweep-2026-08-18.md) ·
原始数据: box `/root/autodl-tmp/eval-backup-0819-1539/` + 本地 `/tmp/qwen38-eval/047-3rep/`

## 一句话结论

**GO:450 档(k75q45)vs 900 锚(k30q28)同批配对,1-rep probe +9.9pp(p=0.009)→
3-rep majority +11.3pp(p=0.001)不降反升;机制靶点(91 关键题)+23.1pp,temporal
+24.0pp(p=0.002);普通题无回归(+1.8pp ns);token +8.0%。唯一负向 open-domain
−13.3pp(n=15,ns)。**

## 设计(2026-08-19 维护者预算指令:不全量,子集 probe)

- **子集 203 题** = 91 关键题(43 翻+48 救,由 046-qwen38 两次 3-rep majority
  per-question 配对算出,与 quota28 verdict 的 42/46 吻合自验)+ 112 分层随机
  (category 比例,seed=47)。multi-hop 22% 略富集(机制靶点)。
- 两臂同批顺序:ctl = 032 店 k30q28;grA = 450 店(`--chunk-target-chars 450
  --chunk-max-chars 550`)k75q45。binary = master(locomo-bench-047,
  `--per-call-timeout 15m`),`--judge-mem0-aligned --no-idk-retry`,
  `--only-questions` 子集模式,concurrency 32。
- 1-rep probe(chain7)→ 维护者批 3-rep 确证(chain8,`--repeats 3` 全重跑——
  harness 无续跑语义,run-dir 已有 run-1 会被重写;原 1-rep 数据在
  eval-backup-0819-1348)。

## 结果

**3-rep majority per-question 配对**(两臂各取 run-1/2/3 多数 correct;零超时行):

| 切片 | n | ctl | grA | diff | 翻转(ctl对→grA错 vs 反向) | McNemar p |
|---|---:|---:|---:|---:|---|---:|
| **ALL-203** | 203 | 73.4% | **84.7%** | **+11.3pp** | 12 vs 35 | **0.001** |
| KEY-91 | 91 | 51.6% | 74.7% | +23.1pp | 10 vs 31 | 0.001 |
| RANDOM-112 | 112 | 91.1% | 92.9% | +1.8pp | 2 vs 4 | 0.688 |
| temporal | 50 | 60.0% | 84.0% | **+24.0pp** | 1 vs 13 | 0.002 |
| single-hop | 93 | 78.5% | 88.2% | +9.7pp | 6 vs 15 | 0.078 |
| multi-hop | 45 | 77.8% | 86.7% | +8.9pp | 3 vs 7 | 0.344 |
| open-domain | 15 | 73.3% | 60.0% | **−13.3pp** | 2 vs 0 | 0.500 |

**体量 parity(FR-005)**:answer_context_tokens 均值 ctl=7205 vs grA=7780(**+8.0%**)。
增益不能全归粒度——但 +8% token 换 +11.3pp 的交换率远优于体量族(040:30→150 是
2.4× token 换 ~1.4pp),且 US1 已证池可达性(18→37/42)是主贡献。grB(k60q36,
−18% token 档)**未跑**(维护者预算决策砍掉)。

**1-rep probe 对照**(chain7):ALL +9.9pp(p=0.009)/KEY-91 +19.8pp/temporal +28.0pp
(p=0.001)/open-domain −13.3pp——3-rep majority 后整体更强,信号非单次噪声。

## 与预注册门对账(FR-004)

| 门 | 判据 | 结果 |
|---|---|---|
| probe 门 | 整体配对差 ≥0 | **PASS**(+11.3pp 显著) |
| 关键题门 | KEY 集不显著为负 | **PASS 超额**(+23.1pp) |
| 超时卫生 | none-token 行 | 0(15m 超时下零超时,vs 8min 常量时代 1-2.4%) |

## 诚实边界

- **子集外推**:203 题非全量 1540;91 关键题构造上偏难(绝对值 73.4%/84.7% 不可
  与全量锚 89.7% 对标)。全量结论需全量 3-rep(维护者预算另议)。RANDOM-112 的
  +1.8pp(ns)是"普通题无回归"的证据,不是增益证据。
- **open-domain −13.3pp 两次一致负向**(1-rep 与 3-rep 同值,n=15):与 US1 归因
  一致(open-domain 是池外召回短板,粒度细化不救);n 小不显著,但方向记录在案,
  全量复核时单列。
- **binary 口径**:master(locomo-bench-047)非 042-bin——两臂同 binary 配对自洽,
  但绝对分不与 042 时代历史锚直接比(FR-004 对照臂同批重跑,合规)。
- **上下文 +8%**:严格 token-parity 线(k60q36=grB 臂)未验证。
- 判题为在线 mem0-aligned judge(deepseek-v4-flash),flash clean 离线重判待做
  (凌晨空闲时段,~¥3-5)。

## 成本账(机器 ¥6/h,2026-08-19 11:20–15:40)

- 机器 ~4.3h ≈ **¥26**(含 ~2.5h 排障损耗:vllm 环境错重启、误杀健康 run、全量残留
  漏杀——详见下节 gotcha)
- judge:在线 203×1×2 + 203×3×2 = 1624 次 ≈ **¥6-8**(deepseek flash)
- 全量外推参考:1540×3×2 judge ≈ ¥25-40 + 机器 ~7.5h ¥45

## 工程 gotcha(本次新增)

1. **box vllm 必须用 023-venv 启动**:系统 vllm 的 torch 是 CUDA 12.x,Blackwell
   SM 12.x 直接 `RuntimeError: SM 12.x requires CUDA >= 12.9`;023-venv( cu13
   +`VLLM_USE_FLASHINFER_SAMPLER=0`)正常。判别法:成功 log 的 `api_utils.py` 行号
   与 8010 embed(023-venv)一致。
2. **results 落盘有缓冲,wc -l 瞬时读数可假**:12:42 读 13 行、kill 后实读 582 行。
   进度判断用 python 逐行数 + mtime,或 vllm gen tokens 速率;**不要按单次 wc 判
   run 死刑**(本次误杀健康 run 的直接原因)。
3. **kill 漏杀**:chain wrapper 与 bench 是两组 PID;`pgrep -x locomo-bench` 在进程
   fork/exec 间隙可假阴性。杀完必须复查 `ps -eo pid,cmd | grep locomo-bench`。
4. **`--repeats 3` 无续跑语义**:已有 run-1 会被重写(全重跑 3 rep)。需要续跑必须
   保底备份;或者接受 3 倍成本。
5. master binary 的 `--per-call-timeout 15m` 把 8min 常量超时错题(1-2.4%)压到 0。
6. 本机 vllm(27B dense,32 并发)decode ~375 tok/s:ctl 臂 ~7-12 题/min,k75 上下文
   臂 ~4.8 题/min——子集 × 3-rep × 2 臂的时间主项。

## 下一步(2026-08-19 维护者决策:不跑全量,token 成本)

- **全量 1540 3-rep 确证推迟**(预算 ~¥105-120,维护者判定太贵);结果以 probe
  配对差 + 外推(~92%)收口,已备注 README 中英文 Benchmarks。
- flash clean 离线重判未做(在线 mem0-aligned judge 口径;重判 1218 次高峰 ~¥5/空闲 ~¥2.5,留待下次)。
- grB(k60q36)token-parity 臂未跑;US3 LME 迁移未启动。
- 恢复点:下次开机器直接全量 3-rep(命令=chain7 去掉 --only-questions,或见
  runbook);450 店/203 子集/两臂数据均在 eval-backup-0819-1539 + 本地
  /tmp/qwen38-eval/047-3rep/。
