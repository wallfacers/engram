# LoCoMo 跑分杠杆台账

本文件是 LoCoMo 检索/答题**杠杆实验的持久正本**(tracked,跨环境不失传)——记录每条杠杆的 verdict、真实数字、口径边界。本地 session memory 只做快速召回,结论以此为准。逐条细节见对应 `specs/NNN-*/eval-log.md`。

**通用口径**:所有 coverage 数为 `--coverage-only` 的 exact-turn recall(turn@k),零 answer/judge 调用(免费);分母 = 有可解析 gold turn evidence 的题(locomo10 全量为 1532)。coverage 增益是端到端答题增益的**必要非充分**条件;声明答题分需另跑端到端。所有采纳杠杆按宪法 V **默认关 / opt-in**。

---

## Feature 008 — score levers(2026-07-22)

固定栈:答题/抽取 = 本地 vllm `Qwen/Qwen3.6-35B-A3B-FP8`;embedding = 本地 fastembed;judge = `deepseek-v4-flash`(mem0-aligned)。所有 sidecar 纯本地、无云依赖。引擎零改(`git diff -- memory embedding provider store internal` 全空)。

| 杠杆 | 层 | 免费 coverage 闸 | **端到端答题(决胜)** |
|---|---|---|---|
| **US1 本地 reranker** `bge-reranker-v2-m3` | retrieval | ✅ +15.457pp turn@k | ❌ **NO-GO −0.06pp(p=1.0)**——coverage 幻觉 |
| **US3 大 embedder** `bge-large-en-v1.5` 1024d | embedding | ✅ +3.793pp turn@k | 未端到端验(候选/备胎;coverage 幻觉风险同 US1) |
| **US2 open-domain 五步提示** | answer | — | ❌ NO-GO −2.1pp(cat-3, p=0.774) |

> **008 决定性教训(US4)**:coverage/turn@k 增益**不等于**答题增益。US1 reranker 拿 +15.457pp 召回,端到端答题 **−0.06pp(McNemar p=1.0, within-noise)**——它 helps 3 类 +8 但把 **temporal 砸 −9**(cross-encoder 按单轮相关性重排,挤掉时序上下文)。**以后杠杆一律以端到端答题分为准,coverage 只作诊断,不作 verdict。**

### ⭐ 新诚实参考点(US4,无 reranker)

engram 端到端 **overall 83.70%**(mem0-aligned judge, 本地 Qwen3.6-35B 栈, top-k30, 全量 1540)。取代旧 luna/strict-judge 50.7% 伪影。

| 类别 | 正确率 | n | 差距诊断 |
|---|---:|---:|---|
| single-hop | 86.68% | 841 | 已接近 MemOS 级;大 n 有杠杆 |
| multi-hop | 85.82% | 282 | 已接近 MemOS 级 |
| **temporal** | **82.24%** | 321 | 次弱,脆(reranker 会害);时序推理 |
| **open-domain** | **56.25%** | 96 | **最弱**,coverage 加满也不动(54→56)→答题/推理/判题问题,非检索 |

**vs 目标**:MemOS 88.83(gap ~5.1pp)/ Mem0 92.5(gap ~8.8pp)。**拉平方向 = open-domain + temporal + single-hop 精度,不是堆检索召回。**

> ⚠️ **口径注**:83.70% 是 `force_answer=false`(**允许拒答**)下拿的,比 Mem0/OmniMemEval 的**强制作答无 IDK**口径更严。对标竞品的可比数字见下 force-answer 行。

### force-answer 口径对齐 A/B(2026-07-22)

`--force-answer` on vs off(off 臂=上表 83.70% hybrid;单变量=是否允许拒答 + force* prompt)。全量 1540,mem0-aligned judge,引擎零改。

| 类别 | off(拒答) | on(force) | 净(题) |
|---|---:|---:|---:|
| temporal | 82.24% | 83.80% | **+5** |
| multi-hop | 85.82% | 86.88% | **+3** |
| single-hop | 86.68% | 86.92% | +2 |
| **open-domain** | 56.25% | 54.17% | **−2** |
| **OVERALL** | 83.70% | **84.22%** | **+8(+0.52pp)** |

- **verdict:边际正 / 口径对齐**。+0.52pp(144 flips 净 +8),**大概率在单跑噪声带内**(答题非确定性 temp=1.0)+ force flag 混淆(同时换 prompt)。**非算法涨点,是向竞品口径靠拢**——84.22% 是对标 MemOS/Mem0 的**可比数**(gap MemOS ~4.6pp)。
- **机制**:收益全来自 temporal/multi-hop 的**事实题**强制猜回;**open-domain 反 −2**(强制猜 opinion 题比拒答更差,IDK 15→0 但净负)。**⇒ open-domain 56% 不是弃答问题,是真推理/口径难度**;open-domain 杠杆改走 OD-2(多候选输出)/ OD-3(抽取软线索),**force-answer 救 open-domain(OD-1)已死**。

### US1 — 本地 reranker(旗舰,决胜杠杆)

- overall turn@30 **77.012% → 92.468% = +15.457pp**(超 +4pp 闸 ≈3.9×);每类 +14~20.7pp(multi-hop +20.743、temporal +14.564、open-domain +14.896、single-hop +14.090);session recall +2.378pp。
- 本地双端点 sidecar(bge-small 384d embed + bge-reranker-v2-m3 cross-encoder),源码零外网调用、本地文件加载;自检 `models=2`、forbidden-list 拦截云型号。batch=50 median 122ms。
- **意义**:把 007 复盘中被死规则判负的**付费** gte-rerank 赢面(曾 +8.3pp)转成**纯本地、可移植、零云**的合法赢,幅度翻倍。这是 pure-Go/offline 可复现的正当拿分路径。
- **限定**:coverage 增益,非答题正确率;端到端声明待 US4 授权。默认关。

### US3 — 大 embedder(候选/备胎,可与 US1 叠)

- overall turn@30 **77.012% → 80.804% = +3.793pp**;open-domain +5.441 最亮、multi-hop +4.778。
- **代价**:大向量纯 Go `TopKCosine` 扫描 **2.37×**(399µs vs 168µs);换维度必**整店重建**。
- **诚实边界**:large 重建 2832 facts vs small 2688(chunks 同 1056),抽取重跑过 → +3.79pp **含重抽取方差,非纯 embedder bit-identical 因果隔离**;冻结重建契约下过闸。

### US2 — open-domain 五步推理提示(证伪)

- cat-3 单变量 A/B:旧 63.5% → 新 61.5% = **−2.1pp**;McNemar b=7/c=5/**p=0.774**(不显著);其余三类由选择路径不变无回归。
- **结论**:归纳/CoT 提示对 open-domain 无正收益,反略降。**短板在检索覆盖,不在答题推理深度**(US1/US3 的 open-domain 大幅正增益反证此点)。commits 61b5311(mechanism)+ 5e172c9(eval)。

### 口径 gotcha(影响所有配对推理)

- **答题非确定性**:`newUsageModelCallerWithUsage`(runner.go)不设 `Temperature`,零值被 `omitempty` 省略 → 远端 vllm 默认 `temperature=1.0, do_sample=true`(**不是** temp=0)。配对 McNemar 只是共享采样配置下的**单次配对观测**,不可宣称确定性差分。

### 下一步(需授权)

- **US4**:US1 已过闸 → 端到端默认预算(top-k 30)重跑 `hybrid` vs `hybrid+rerank`,声明新参考点 + false→true flip 抽查;reranker 保持默认关。US1+US3 联合收益尚未验证。
- 细节正本:[`specs/008-locomo-score-levers/eval-log.md`](../specs/008-locomo-score-levers/eval-log.md);对标目标见 [`competitive-benchmarks.md`](./competitive-benchmarks.md)。
