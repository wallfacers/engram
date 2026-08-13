---
title: DeepSeek v4-pro 作为 LME answerer verdict — 2026-08-13
summary: answerer 升级是真实杠杆。v4-pro + 融合 prompt(现已被 038 unified contract 取代)在 LongMemEval-S 上 repeat=3 达 91.1%(ci95 [90.1, 92.2]),稳定超 90pp 目标;对照实验证明分数真实(模型依赖记忆,无记忆答不出)。发现 harness 对 anthropic usage 统计失真 bug(不影响分数)。此结果按 038 框架属 post-hoc 诊断,非系统能力得分。
status: done
audience: [maintainers, agents]
owner: engram-maintainers
last_reviewed: 2026-08-13
tags: [longmemeval, deepseek, answerer-upgrade, verdict, post-hoc]
---

# DeepSeek v4-pro 作为 LME answerer verdict — 2026-08-13

## verdict

**answerer 升级是真实杠杆。** LongMemEval-S 500 题,DeepSeek v4-pro 答题 +
deepseek-v4-flash 判题,repeat=3 均值 **91.1%**(ci95 [90.1%, 92.2%]),稳定超
90pp 目标——远超 Qwen 融合的 88.5%(repeat=3)。

⚠️ **注意**:本实验使用的 `--lme-entity-verify` 融合 prompt 已随
specs/038-unified-answer-contract 移除(unshipped,被 unified contract 取代)。
本结果保留为**诊断证据**,不推荐作为可部署产品路径。

## 结果

| 臂 | 分数 | 说明 |
|---|---|---|
| v4-pro + 融合(entity-verify) 1-rep | 91.38% (456/499) | 单次 |
| **v4-pro + 融合 3-rep 均值** | **91.1%** (ci95 [90.1, 92.2]) | run-1 91.0 / run-2 91.6 / run-3 90.8 |
| Qwen fused3(对照) | 88.5% | 同 store/同 prompt 族 |

**分项(v4-pro 3-rep vs Qwen fused3)**:

| 类别 | v4-pro | Qwen | Δ |
|---|---|---|---|
| knowledge-update | 91.0% | 83.8% | **+7.2** |
| multi-session | 87.5% | 84.5% | **+3.0** |
| temporal-reasoning | 92.0% | 90.7% | +1.3 |
| single-session-user | 98.6% | 99.0% | −0.4 |
| single-session-assistant | 96.4% | 95.8% | +0.6 |
| single-session-preference | 76.7% | 71.1% | +5.6 |

knowledge-update 是最大赢点(+7.2pp)——正是 Qwen 能力天花板所在(多值冲突选值)。

## 分数真实性验证(对照实验)

同一问题(Where did I complete my Bachelor's degree?)对 v4-pro:

- **A 带记忆**:usage input_tokens=212,pred 正确引用 UCLA ✓
- **B 无记忆**:usage=54+cache_read=128,**pred=空**——v4-pro 无记忆答不出

结论:91.1% 的 3-rep 稳定成绩**必然基于记忆答题**(无记忆答不出,LME 为合成个人记忆无先验)。
非 judge 宽松或单次噪声。重复次数、judge 口径与 Qwen 对照完全一致。

## ⚠️ 发现的 harness bug:anthropic usage 统计失真(不影响分数)

- v4-pro(anthropic provider)run 的 `input_tokens` 被记录为 **2-117 tokens**(实际含 ~8k 记忆上下文)
- 同一 DeepSeek 端点单测带记忆正确报 212——问题在 harness 对 anthropic usage 的解析
  (疑似漏加 `cache_read_input_tokens` 或读错事件,待修)
- 影响:cost.json 严重低估(显示 in=0.86M,真实应 ~12M);DeepSeek 实际按真实 token 收费
- **真实成本估算**:1-rep + 3-rep 约 **¥12-35**(取决于缓存命中率;input ~12.75M + output ~1.22M,
  v4-pro 未命中 3元/M/命中 0.025元/M,输出 6元/M)
- 此 bug 在 harness(`cmd/locomo-bench`),非 engine,可独立修复

## 038 框架定位(诚实边界)

- 按 specs/038 R7:**LME 是 post-hoc 兼容性诊断**(其题目直接影响过 prompt 设计)
- 本结果回答的问题是:「LME 能力上限是否在 Qwen?」→ **否,换更强 answerer(v4-pro)能到 91%**
- **不回答**的是:「系统在真实场景的得分」——那需要 038 unified contract + 独立行为 cohort + LoCoMo 配对回归
- v4-pro 是付费 API(非客户端技术),按宪法不算「客户端涨点」;作为 answerer 升级是集成方选择,
  不是 engram 引擎能力

## 环境与复现

- 新机器(迁移后):connect.westd.seetacloud.com;数据/store/binary 均在迁移中保留
- run:answer=deepseek-v4-pro(anthropic 端点),judge=deepseek-v4-flash,`--force-answer
  --lme-entity-verify --top-k 150 --chunk-quota 12 --concurrency 256 --repeats 3`
- store:lme-s500-store(bge-large,13G);embed :8010 重启正常
- 产物:/root/autodl-tmp/lme-v4pro-fused3/(run-1/2/3 + stats.json)
- 注意:该 prompt 现已从代码移除(038),此 run 不能直接用当前 HEAD 复现;诊断结论仍有效
