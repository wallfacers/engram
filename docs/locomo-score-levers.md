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

---

## Feature 009 — 归因门控(attribution,2026-07-23)

固定栈同 008(embedding 换 box vllm bge-large 1024d)。引擎零改(全在 `cmd/locomo-bench/` adapter)。逐条正本:[`specs/009-retrieval-attribution-gate/eval-log.md`](../specs/009-retrieval-attribution-gate/eval-log.md)。

**US1 逐题检索归因 trace**:retrieval-only 把错题切四象限(Q1 对 / Q2 答题侧 / Q3 排序靶心 / Q4 抽取侧),零答题成本。首版有两 adapter bug(`outranked_by` 结构性恒空;`covers_gold` 对 fact 命中失明→Q3=94.7% 伪影),修复后(fact 感知覆盖 + wide-pool outranked,commit 7c8f194)Q3→58.4%、Q1→34.8%、SC-002 outranked 非空 100%。fact↔turn 用词法内容匹配桥接(fact 只有 session 级溯源),`--fact-coverage-tau` 默认 0.8(偏严,已知软限制)。

### ⭐ 决定性诊断:瓶颈是**深层召回**,不是 top-K 排序

修复后可信证据显示:真 US2 靶心(Q3 且答错)的 gold 在宽池里**中位 rank 71–90、无一 ≤30**、70/156 在 100+;outranked_by 信号弥散(时间锚 19% / 近重 5%,**无单一机制主导**)。

- **US2 排序机制 = STOP(NO-GO)**:tuning-free 重排(score-aware RRF / MMR / 实体·时间锚)**救不动 rank-90 的 gold**——它们重排 top 候选,而 gold 根本没进候选前列。这不是排序问题。
- 与 008「reranker coverage +15pp 但端到端 NO-GO」、[US2 open-domain 提示证伪]「短板在检索覆盖非答题深度」**三处独立印证同一结论**。
- **后续拿分方向 = 召回/检索深度**,非排序精修:更强 embedder、混合信号召回扩召、chunk 粒度/抽取覆盖。**不是** cross-encoder 重排(死规则:云/付费 rerank 禁用;本地 reranker 已证 coverage 幻觉)。

### 口径 gotcha(归因专有)

- **SC-004 确定性依赖嵌入后端**:vllm-GPU 查询嵌入非确定(`embed_probe` unstable,bit_identical 0.875)→ trace byte 级不可复现(差异在 rrf_score 尾数);但**象限分布两跑完全相同、0/1540 换象限**,结论可复现。要 byte 一致须用确定性 CPU 嵌入器(fastembed)。覆盖判定本身纯词法确定。

### ⭐ bge-large 端到端 = GO 候选(008 US3 coverage 赢**已转化**为答题分,2026-07-23)

009 诊断说「后续方向 = 更强 embedder」。这轮把 008 US3 的 bge-large 从 coverage 候选**推到端到端答题验证**——可复现流程/踩坑正本见 [`locomo-e2e-eval-reproduction.md`](./locomo-e2e-eval-reproduction.md)。

栈:box vllm Qwen 答题 + deepseek-v4-flash **mem0-aligned** judge + `--chunks --top-k 30 --chunk-quota 12 --force-answer`。

| store / embedder | overall | Δ |
|---|---|---|
| bge-small @ `007-us2/cov-store`(控制/自检) | **84.03%**(1294/1540) | 复现记录基线 84.22%(±0.2pp)→ 管线验证 ✓ |
| **bge-large @ 全新 q12 店** | **85.45%**(1316/1540) | **+1.42pp / 净 +22 题** |

分类:single-hop +1.7 · multi-hop +2.1 · temporal −0.6 · **open-domain +4.2**(3/4 类涨,含最难的两类)。

- **与 008 reranker 决定性不同**:reranker coverage +15pp 但端到端 NO-GO(幻觉);bge-large coverage +3.79pp **真转化**成答题 +1.42pp。**这是首个已转化的召回赢**。
- **可移植/合规**:bge-large 开源权重、离线可跑(fastembed CPU / vllm),**非付费云 rerank**,不碰死规则;符合 Constitution I/V。可作默认 embedder 升级路径候选。
- **口径**:85.45% 是可比数(force-answer + mem0-aligned,同竞品口径)→ 对 MemOS 88.83 的 gap 从 ~4.6pp 收窄到 **~3.4pp**。
**repeats=3 坐实(答题噪声带)**:bge-large `OVERALL mean=85.4%, ci95=[84.9%, 85.9%]`(single-hop [87.7,89.9] / multi-hop [84.5,90.6] / temporal [77.8,86.0] / open-domain [50.3,71.2],后者 n=96 带宽)。bge-small 两锚点 **84.03%(今天 fresh 控制)+ 84.22%(记录基线)均 ~84.1%,落在 bge-large 95% CI 下界 84.9% 之外** → **+1.3pp 扛过了 temp=1.0 答题非确定,是真信号非噪声**。

- **诚实 caveat(剩一道硬闸未过)**:bge-large 店是**今天全新抽取**、bge-small 是旧 cov-store → +1.3pp **仍混重抽取方差**,非 bit-identical 纯 embedder 隔离。缓解论证:**per-类增益画像与 US3 coverage 画像吻合**(open-domain +4.2↔coverage +5.4、multi-hop +2.1↔+4.8),指向增益由 embedder 驱动而非抽取运气。**完全隔离**需同抽取的 bge-small 对照(受阻于 fresh 建店抽取瓶颈 ~45min + 单线程 sidecar 嵌入,本轮未跑;留作出货前最后一闸)。
- **本轮元教训**:先前 59%→70% 全是**漏 `--judge-mem0-aligned`**(+ chunk-quota 0)的配置伪影,与 embedder 无关;控制自检(cov-store 复现 84%)是把伪影和真信号分开的唯一手段。踩坑全表见 reproduction runbook。

### ⭐ cat-top-k 多跳扩预算 = GO(第二个已转化召回赢,叠加在 bge-large 上,2026-07-23)

009 诊断的另一半:multi-hop enumeration 题「需要多 session 的证据」但被 top-k30 卡住(gold 在深层)。`--cat-top-k "1=150"` **只把 category-1(multi-hop)的检索预算提到 150**,其余类不动。栈同上(bge-large 店 + canonical recipe)。

**隔离验证(`--only-category 1`,repeats=3)**:

| multi-hop only | mean | ci95 | 三跑 |
|---|---|---|---|
| A 控制 top-k30 | 86.8% | [83.1, 90.4] | 87.9 / 87.2 / 85.1 |
| **B cat-top-k 1=150** | **92.0%** | **[90.6, 93.3]** | 91.8 / 91.5 / 92.6 |

**+5.2pp,B CI 下界 90.6 > A CI 上界 90.4 = 统计分离;每个 B 跑严格 > 每个 A 跑。**

**整体成对验证(全 1540 题,同批 back-to-back,repeats=3)**:

| category | A 基线 | B cat-top-k 1=150 | Δ |
|---|---|---|---|
| **OVERALL** | **85.1% [84.4,85.8]** | **86.0% [85.0,87.0]** | **+0.9pp** |
| multi-hop | 86.9% [84.5,89.2] | 90.2% [88.0,92.4] | **+3.3pp** |
| single-hop | 88.3% | 88.7% | +0.4(未动,答题噪声) |
| temporal | 81.7% | 81.7% | 0(未动 ✓) |
| open-domain | 62.8% | 64.6% | +1.8(未动,答题噪声) |

- **run-level 完全分离**:A `{84.8, 85.1, 85.4}` vs B `{85.6, 86.2, 86.4}` → **min(B) 85.6 > max(A) 85.4**,3v3 无重叠(Mann-Whitney p≈0.05)。独立 95% CI 略有交叠([84.4,85.8]∩[85.0,87.0]),但杠杆**构造上只动 multi-hop 检索**,其余三类检索 bit-identical → 整体增益**全部可归因于 multi-hop +3.3pp**,非多跳类的差异是纯 temp=1.0 答题抖动(应记为零信号)。
- **纯客户端 / 合规**:只是**检索预算旋钮**(per-category top-k override),无模型、无付费云 rerank,不碰死规则。
- **可叠加**:这是叠在 bge-large 之上的第二个召回杠杆。bge-large + cat-top-k 合计 vs 原 bge-small 基线 ≈ **+2.0pp**(84.0 → 86.0)。对 MemOS 88.83 的 gap 收到 **~2.8pp**。
- **诚实 scale caveat**:multi-hop 题的 answer-context 从 ~3678 涨到 ~8759 tokens(2.4×);全集平均 3614→4546(+26%,因 cat-1 占 ~18%)。计费答题器上是真成本;box 上近免费。tuning-free(150 是「给足多 session 证据」的直觉值,未网格搜索,可能非最优)。
- **产物**:`.locomo-run/009-cat1-{A-tk30,B-tk150}`(隔离)+ `.locomo-run/009-full-{A-base,B-cattopk}`(整体成对),regime.json 均 `judge=mem0-aligned;judge_model=deepseek-v4-flash`,cost.json `unpriced_models` 含 deepseek-v4-flash(judge 真跑)。

### ✗ opinion-pass 抽取覆盖 = NO-GO(净负,coverage 污染 precision,2026-07-23)

想攻最大短板 open-domain(64.6%)。`--opinion-pass` 在复用主抽取基础上补跑一遍聚焦 opinions/preferences/traits 的 ADD-only 抽取(每 conv +243~354 条,全库 ~2800 条,约翻倍 fact 数),嵌入走 bge-large。栈叠 cat-top-k,整体 repeats=3,对照 = B_full(base+cat-top-k)。

| category | B_full 对照 | opinion-pass+cat-top-k | Δ |
|---|---|---|---|
| **OVERALL** | **86.0% [85.0,87.0]** | **85.4% [85.1,85.8]** | **−0.6pp 净负** |
| open-domain(靶心) | 64.6% | 65.6% [49.9,81.4] | +1.0(**n=96 三跑 68.8/69.8/58.3 = 纯噪声,增益幻觉**) |
| single-hop | 88.7% | 87.8% | **−0.9(污染)** |
| temporal | 81.7% | 80.8% | **−0.9(污染)** |
| multi-hop | 90.2% | 90.5% | +0.3(噪声) |

- **run-level 反向完全分离**:opinion `{85.3,85.5,85.5}` **严格低于** B_full `{85.6,86.2,86.4}`(max opinion 85.5 < min B_full 85.6)→ 净负是真信号非噪声。
- **机制**:无差别把 ~2800 条 opinion 灌进同一 RRF 池 → 靶心 open-domain 边际得益被 n=96 方差淹没,而全局稀释把 single-hop/temporal 各拖 −0.9。**category-blind 覆盖扩张适得其反**。
- **要救**得靠 **category-conditional 检索**(只对 open-domain 题浮现 opinion 条目)——新机制,非现成 flag,留作 future work。判分口径合法(deepseek mem0-aligned,unpriced 含 deepseek-v4-flash)。产物 `.locomo-run/009-full-C-opinion` + `.locomo-run/009-opinion-store`(store 已被 opinion 污染,非正本;bge-large 正本仍是 `009-bge-chunks-store`)。
- **教训**:open-domain 短板**不是**靠加抽取覆盖能便宜拿的;[[008-us2-opendomain-verdict]] 说「短板在检索覆盖非答题深度」——但覆盖得**精准定向**,粗放翻倍反伤全局。

### ~ cluster-sweep = INCONCLUSIVE(+0.4pp 落噪声带,配对对照证伪表观增益,2026-07-23)

`--cluster-sweep`(一跳实体簇扩展,检索层,预算封顶 1.5×,无 per-question LLM)叠 cat-top-k。**先犯错后纠正的教科书案例**:

- **对旧 B_full 比**:sweep 86.5% vs 旧 B_full 86.0%,表观 +0.5pp,且 multi-hop「+1.6」、temporal「+1.4」——看着像 GO。
- **配对新鲜对照(B2ctrl,同批 cat-top-k 无 sweep,repeats=3)戳穿**:B2ctrl 86.1% {85.6,86.2,86.4} vs sweep 86.5% {86.3,86.3,86.9} = **真 Δ +0.4pp,CI 重叠 [85.0,87.1]∩[85.7,87.3],run-level 重叠(sweep min 86.3 < ctrl max 86.4)**。
- **表观 multi-hop +1.6 是幻觉**:B2ctrl multi-hop **91.8% = sweep 91.8%**(完全相同)——旧 B_full 的 90.2 只是 temp=1.0 低抽;cat-top-k 已把 multi-hop 拉满,sweep 加不动。temporal/single-hop 各 +0.8(一致正倾但重叠),open-domain −2.0(n=96 噪声,探测跑却 +2.1)。
- **判定:非干净 GO。** +0.4pp 不可分离于答题噪声;要坐实需 repeats≥8 缩 CI 或配对 McNemar。**元教训:必须对新鲜同批控制比,不能对隔批 baseline 比**——隔批比会把答题噪声漂移误报成机制增益(这正是 [[locomo-answer-nondeterministic]] 警告的)。产物 `.locomo-run/009-full-{E-sweep3,B2-ctrl}`。

---

## Feature 010 — 多查询检索(提质型深召回,2026-07-24)

009 收口预言「下一步 = query 分解让 gold 不加量升进 top-30」。010 把这枪做实并**在门②近免费处止损证伪**:引擎新增 `SearchMulti`(每子查询各跑三信号 hybrid → 复用 RRF k=60 做 RRF-of-RRF 融合,`len==1` 退化保真、纯 Go/offline、无 query-时 LLM),adapter 用答题 LLM 把多跳题拆 ≤4 子查询喂入。逐条正本:[`specs/010-multi-query-retrieval/`](../specs/010-multi-query-retrieval/)。

### ✗ query 分解 + RRF-of-RRF = NO-GO(门②离线召回诊断即证伪,未耗门③答题窗口,2026-07-24)

固化店 `009-bge-chunks-store`(bge-large 1024d + chunks)上,对 multi-hop(category **1**,n=282)跑 single vs multi 召回诊断(retrieval-only,不调答题/judge):

| 指标 | single | multi(分解) | 判定 |
|---|---|---|---|
| gold 进 top-30 变动 | — | **entered 2 / left 10** | **净 −8 题掉出**,分解伤召回 |
| coverage@30 | 0.031 | 0.031 | delta **+0.0004**(实质零) |
| mean gold rank(top-30 口径) | 17.96 | 18.78 | 后移 **+0.82 名**(变差) |

- **三信号方向一致 → 分解在 multi-hop 上没提质,反轻微伤召回**。严格按止损门跳过门③(省下答题窗口的钱),NO-GO。
- **机制**:009 已证 gold 在宽池里中位 rank 71–90——它对**每个子查询同样是弱命中**。RRF-of-RRF 要顶起一个 doc,需它在多个子列表里都出现**且靠前**;gold 在各子列表都深埋,`1/(60+rank)` 贡献都极小,融合顶不动。分解反把「对多个子查询都中等命中」的噪声 doc 顶进 top-30,**挤出** gold(解释 left=10)。即「垃圾进垃圾出」:分解改变了池的构成,没改变 gold 的**向量可发现性**。
- **与 008 reranker / 009 cluster-sweep 同族的诚实处理**:引擎 `SearchMulti` 作为纯 Go 融合机制保留(退化保真、可移植、default 未接入任何默认路径);adapter `--multi-query`/`--recall-diagnostic` 保留为**默认关诊断能力**。
- **符号勘误(给未来避坑)**:`recall_diagnostic.json` 的 `*_delta` 口径 = `single − multi`(`multiquery.go:538/553`),故 `mean_gold_rank_at_30_delta` **负值 = multi 排名后移 = 变差**,与直觉相反;判定以无歧义的 `gold_entered/left_top_30` 主信号为准。
- **止损纪律兑现**:门② retrieval-only 近免费,三信号一致即断,门③(1540×3 答题 + judge)未启动;box 跑完即关机停计费。产物 `.locomo-run/010-recall/`。

---

## Feature 011 — 写入侧 dual-index alias 影子向量(提质型深召回,2026-07-24)

**动机**:承 010 把提质方向收窄到**写入侧**——瓶颈是 gold fact 的**向量可发现性**。fact 抽取的 `aliases`(概念锚点,009 店 conv0 52% 的 fact 有)已免费产出、已落库,却从不参与嵌入。本枪让有 alias 的 fact 产一条 `#alias` 影子向量(aliases 合并嵌入),检索时 semantic 命中后 **max-pool 归并回源 fact**(取 `max(text_cosine, alias_cosine)`,不双重计票),再进现有 RRF(k=60)。文献锚:doc2query(1904.08375)/ Doc2Query++(2510.09557)证 dual-index max-pool 是 dense 正解、单向量 append 会稀释 bge-large。**无 α、无付费 reranker、不扩 top-k、不加 context**。

### ✓ 门① 纯 Go 契约 = PASS;✗ 门② 分层召回诊断 = NO-GO(止损,未耗门③答题窗口,2026-07-24)

- **门①(US1 引擎 + US2 adapter)全绿**:US1 max-pool 归并 + 退化保真(无 alias/chunk/text 向量逐字节不变、`!hasShadows` 快路径)6 测试;US2 方案 A 两店隔离(canonical 10 db 全程 0 影子未污染、baseline 剥离=0、treatment>0、抽取 calls=0)9 测试。引擎/adapter 分属两 commit(`3184272` / `00229a4`),`git diff -- memory embedding provider store internal` 空。
- **门② 配对分层召回诊断(retrieval-only,box bge-large 8001,near-free)** —— 主判据「gold 有 alias」子层 gold 是否净升 top-30(`rank_delta=treatment−baseline`,负=前移=变好):

| 目标类 | 子层 n | 子层 rank_delta | 子层 entered/left | coverage@30 Δ | 全局 rank_delta |
|---|---|---|---|---|---|
| multi-hop(cat1) | 123 | **+0.057**(未前移) | 1 / 0(噪声级) | **0** | +0.505(变差) |
| open-domain(cat3) | 25 | **+0.72**(变差) | 0 / 0 | **0** | +1.41,净 **−1** 掉出 |

- **两个目标类子层均未净升 top-30**(mean rank 不前移、coverage@30 delta 恒 0),严格触发止损门 → **NO-GO,不启动门③**(省 box 1540×3 答题 + judge 窗口)。
- **机制(为何零到微负)**:max-pool 只会抬一个 fact 的 semantic 分不会降,但它**对非 gold 的有 alias fact 同样抬分**,把 gold 相对挤下去 → 净效应零到微负。coverage@30 delta 恒 0 = 没有任何 gold 跨越 top-30 边界。**短概念标签(`painting`/`self-acceptance`)不比 fact 原文多提供可发现性,且对称地抬噪声**——这正坐实 spec 预标注的「52% 覆盖 + 短标签天花板」。
- **与 008 reranker / 010 分解同族的诚实处理**:引擎 dual-index 归并作为纯 Go、退化保真、可移植的**新增能力保留**(离线默认对无 alias 店零改);adapter `--alias-shadow off|baseline|treatment` 保留为**默认关能力**。均不进默认栈、不报为赢。
- **止损纪律兑现**:门② near-free 即断,门③未启动;box bge-large 跑完即停(GPU 0 MiB)、隧道拆、凭据清。产物 `.locomo-run/011-recall/`(multi-hop)、`.locomo-run/011-recall-cat3/`(open-domain)。

---

## Feature 012 — 写入侧 Doc2Query 伪查询影子向量(提质型深召回,2026-07-24)

**动机**:承 011 把写入侧收窄——011 的 `#alias` 影子用**已有短概念标签**,证伪于「短标签不比 fact 原文多提供可发现性」。012 换弹药:为每条 fact 用答题 LLM 生成 **2-3 条伪查询**(「这条 fact 能回答的问题」),嵌成 `#query` 影子向量,检索时 `max(text_cosine, alias_cosine, query_cosine)` 归并回源。文献锚:Doc2Query++(2510.09557)证 **dense bi-encoder 只在 dual-index max-pool 下**从 query-gen 获益(naive append 伤 dense),且 dense 偏好**LLM 拟人问句** > 关键词式扩展(正解释 011 alias 失败)。闭合「陈述↔问句」嵌入不对称。**无 α、无付费 reranker、不扩 top-k、不加 context**。检索器源码零改(011 的 content-agnostic max-pool 复用,`resolveShadow` 加认 `#query` 后缀即生效)。

### ✓ 门① 纯 Go 契约 = PASS;✗ 门② 分层召回诊断 = NO-GO(止损,未耗门③答题窗口,2026-07-24)

- **门①全绿**:US1 引擎(migration v5 `memory_fact_queries` + `PutFactQueries/FactQueries` + `#query` 影子嵌入 + `queryEmbedText` verbatim join + max-pool 归并)6 测试;US2 adapter(解耦 `--doc2query-build` 一次性预建 + 两店隔离 + baseline 剥离/treatment 保留 + `gold_has_query` 分层 + extractNever 守零抽取 + 固定温度 0.2 非零防 omitempty + 300-fact 溢出覆盖)14 测试。引擎/adapter 分属两 commit(`cebf866` / `294ca0d`),`git diff -- memory embedding provider store internal` 空。build 产物 **2755 facts / 8250 queries(avg 3/fact)**。
- **门② 配对分层召回诊断(retrieval-only,box bge-large 8001,near-free)** —— 主判据「gold 有 query」子层 gold 净升 top-30(`entered>left` 且 mean rank 前移)**且** coverage@30 Δ>0(`delta=treatment−baseline`,rank 负=前移=变好):

| 目标类 gold_has_query 子层 | n | entered/left | rank@30 Δ | full-pool rank Δ | coverage@30 Δ | 全局 full-pool rank Δ |
|---|---|---|---|---|---|---|
| multi-hop(cat1) | 207 | 0 / 0 | +0.227(变差) | +0.792(变差) | **0.00000** | +2.673(变差) |
| open-domain(cat3) | 51 | **2 / 0** | −0.627(变好) | +0.745(变差) | **0.00000** | +3.837(变差) |

- **两类均触发止损门**:multi-hop 零移动 + rank 后移(两条件全败);open-domain 有 2 gold 进 top-30 且 rank@30 前移 −0.627(**微正信号,略强于 011 的 entered=0**),但 **coverage@30 Δ 恒 0**——进 top-30 的 fact 对应 gold turn 已被其他检索项覆盖,**无新增 turn 覆盖 = 端到端无预期增益**(coverage 是端到端必要条件),coverage 条件败 → **NO-GO,不启动门③**。
- **机制(为何零到微负,与 011 同族)**:max-pool 只抬不降,但**对 207/282、51/96 条「有 query」的非 gold fact 同样抬分**,gold 相对位置几乎不变(cat3 +2 entered 属噪声级),全局 full-pool rank 反因对称抬噪后移 +2.67~3.84。伪查询虽是「拟人问句」(Doc2Query++ 说的 dense 偏好型),仍未给 gold 带来**超过原文的判别性可发现性**——**gold 埋在 rank 71-90 是向量空间的深层问题,写入侧影子表示(概念标签 011 / 伪查询 012)都改变不了池的构成而非 gold 的可发现性**。
- **决定性收敛**:**两次独立的写入侧表示尝试(011 alias 短标签、012 doc2query 拟人问句)在门②同点 NO-GO**——加上 010 query 侧分解证伪,**「靠改写/影子表示把 gold 顶进 top-30」方向三向证伪**。瓶颈不在表示的措辞,在 dense 单塔对 multi-hop/open-domain gold 的**深层召回上限**。剩余真杠杆回到**检索侧结构**(实体图遍历 / 检索侧时间窗,strategy 文档 P0),非写入侧再换弹药。
- **诚实 caveat**:box-GPU 查询嵌入非确定([[locomo-answer-nondeterministic]] 同族,009 SC-004),rank 尾数有抖动;但 `entered/left` 与 `coverage@30 Δ=0` 主信号鲁棒(跨类一致)。
- **同族诚实处理**:引擎 `#query` dual-index 归并作为纯 Go、退化保真、可移植的**新增能力保留**(无 `memory_fact_queries` 行的店逐字节零改);adapter `--doc2query off|baseline|treatment` + `--doc2query-build` 保留为**默认关能力**。均不进默认栈、不报为赢。US3(抽取流水线内生成 `queries`)**取消实现**——门③未过,无 shipped 路径。
- **止损纪律兑现**:门② near-free 即断,门③未启动;box 跑完即停、隧道拆、凭据清。产物 `.locomo-run/012-recall-cat1/`、`.locomo-run/012-recall-cat3/`、预建店 `.locomo-run/012-build/doc2query-store`。

---

## Feature 013 — 检索侧时间窗召回臂(把 temporal 从后处理乘子升级为 RRF 第4路,2026-07-24)

strategy 文档检索侧结构 P0 之②。假设:temporal 82.24% 卡在召回侧——`applyTemporal` 是**融合后软乘子**,只作用于已被语义/关键词门控的池,够不着深埋 gold;拟建**独立时间窗召回臂**(`NamesByEventWindow` 按 event_date 范围拉取 → RRF 平权第4路)把深埋 temporal gold 抬进 top-K。纪律:先跑**免费四层召回诊断门(US1)**证瓶颈在召回侧,GO 才建引擎机制。逐条正本:[`specs/013-temporal-window-recall/`](../specs/013-temporal-window-recall/)。

### ✓ 门① US1 契约 = PASS(引擎零改、20 断言绿);✗ 门① 四层召回诊断 = NO-GO(cause=**解析器**,止损,未建 US2/US3,2026-07-24)

`--temporal-diagnostic`(适配器 only、引擎零改、零答题/judge/抽取 token、box bge-large 重嵌 query、`--chunks --top-k 30 --chunk-quota 12`)在 `009-bge-chunks-store` 上跑 temporal 类 n=321:

| 层 | 度量 | 值 | 判据 |
|----|------|-----|------|
| **L0 parse_coverage** | temporal query 解析出时间窗占比 | **0.196(63/321)** | **← 首个失败层**(<0.50) |
| L1 event_date_coverage | gold 事实带 event 日期占比 | 0.773(126/163) | ✓ 健康 |
| L2 buried_ratio | gold 深埋 top-30 外占比 | 0.140(45/321),rank_pool **p50=64 p90=155** | ✓ 确深埋 |
| L3 oracle_lift@30 | 纯 event∈window 臂抬起的深埋 gold | 0.333(**6/18** buried facts) | ✓ 有天花板但基数极小 |

- **决定性归因 = query 侧时间解析器,不是召回臂**:`ParseTemporalIntent` 只对 **19.6%** 的 temporal query 解析出时间窗。臂由"有时间意图"门控 → **对 80% 的 temporal 题永不点火**。即便建了臂,其可及集 = 63/321 题,其中仅 **18** 条 gold 深埋、oracle 上界只抬 **6** 条——端到端是舍入误差。**严格触发止损门(L0 败)→ NO-GO,不建 US2 召回臂、不启动 US3 box 配对答题**(省 1540×3 答题+judge 窗口)。
- **反常识但重要 — 召回臂前提在「窗解析成功时」结构成立**:L1(77% 事实有 event_date)+ L2(gold 深埋 p50 rank 64)+ L3(oracle 抬 33% 深埋)三层健康,说明"按时间窗拉深埋 gold"这条机制**本身没错**——它只是被一个鲜少点火的解析器卡死。**教训:temporal 的真杠杆在上游 query 侧时间理解(为何 80% temporal 题解析不出窗),不在下游召回臂**。免费门在写一行引擎机制前锁定了这一点——正是它的设计目的(不重蹈 008「先建后验」覆辙)。
- **与写入侧三向证伪(010/011/012)的关系**:那三个证伪的是"表示改写顶不动深召回";013 证伪的是"检索侧时间窗召回臂的**排序前提(点火率)不成立**"。两类 NO-GO 不同因,但同指向一个诚实事实:**LoCoMo temporal 短板的可及杠杆在 query 侧解析覆盖,不在 fact 侧表示、也不在 event_date 召回结构**。
- **保留能力**:适配器 `--temporal-diagnostic` 作为可复用的免费诊断保留(引擎零改);引擎侧 US2 召回臂**取消实现**(门①未过,无 shipped 前提)。既有 `ParseTemporalIntent`/`applyTemporal` 不动。
- **止损纪律兑现**:门① near-free 即断,US2/US3 未启动;产物 `.locomo-run/013-temporal-diag/`(`temporal_diagnostic.json` + 321 行 questions jsonl)。box 隧道跑完即拆(bundled setsid 编排:`ssh -f -N` 在无 tty 的 setsid 子进程里 askpass 才触发;沙箱会杀裸持久隧道,故把隧道+诊断打包进一个 detached 脚本存活到跑完)。**box vllm teardown 遇 SSH 会话层 255(疑短时多次连接限流/机器启停),GPU 归零待维护者侧核**。

---

## 杠杆总账(2026-07-23 收口)

box vllm 全本地栈(Qwen 答题 + bge-large 嵌入 + deepseek mem0-aligned judge)、canonical recipe、repeats=3 下,叠加式探完一轮:

| 杠杆 | 判定 | Δ overall | 类型 | 机制 |
|---|---|---|---|---|
| bge-large embedder | **GO**(shipped) | +1.3pp | **提质** ✓ | 同 top-k,向量更强 → 召回转化 |
| cat-top-k `1=150` | 有效但**非首选/不设默认** | +0.9pp | **加量** ✗ | 多跳扩检索预算(context 2.4× 税) |
| opinion-pass | NO-GO | −0.6pp | 加量 | 粗放覆盖污染全局 precision |
| filter-pool | 不可测/成本差 | — | 加量 | LLM-per-question 大 context 压垮 box vllm |
| cluster-sweep | INCONCLUSIVE | +0.4(噪声内) | 加量 | 实体簇扩展,配对对照证伪表观增益 |
| query 分解 + RRF-of-RRF | **NO-GO**(门②止损) | —(召回即伤:top-30 净 −8) | 提质(意图) | 多跳拆子查询融合;gold 各子查询均弱命中,顶不动反挤出 |
| alias 影子(011,写入侧) | **NO-GO**(门②止损) | —(子层不前移,coverage Δ=0) | 提质(意图) | 短概念标签不比原文多可发现性,对称抬噪 |
| doc2query `#query` 影子(012,写入侧) | **NO-GO**(门②止损) | —(cat3 +2 entered 但 coverage Δ=0) | 提质(意图) | 拟人伪查询同样不给 gold 超原文判别性,gold 深埋是向量上限 |
| 检索侧时间窗召回臂(013) | **NO-GO**(门①止损,cause=解析器) | —(臂对 80% temporal 题不点火) | 结构(检索侧) | `ParseTemporalIntent` 只解析出 19.6% temporal 题的窗;臂前提在窗成立时结构 OK(L1-3 健康)但被解析器门控;真杠杆在上游 query 侧时间解析覆盖 |
| 实体图遍历 assoc(014,003 遗产已建) | **NO-GO**(免费诊断 PASS→e2e 证伪) | alone 净≈0(配对 −29 ns);叠 cattopk −1.15pp(配对 −53 χ²5.1) | 结构(检索侧) | 图游走第4路 cov@30 +2.6pp/深层正交救回/零 coverage 伤害,但答分不转化(008 铁律)——救 gold 的同时注入实体共现干扰候选;不替代 cattopk、叠加反伤 |

**杠杆哲学(maintainer 定调,2026-07-23)**:只认**提质**的赢(同预算/同 top-k 下把对的证据捞得更准,如 bge-large),**反感加量**(撑 top-k / 扩池 / 喂更多 context —— 分是真的但拿成本税换,不可移植,换个部署就塌)。产品是设备/应用习惯记忆,集成方无限 context 预算不存在。**据此 cat-top-k 从「头条 GO」降级为 optional/非默认**;本表所有「加量」型即便涨分也不进默认栈。

**净出货(提质路线):bge-large → +1.3pp(~85.4%)**,是唯一符合哲学的干净赢。cat-top-k 作为 optional 旋钮保留(多跳 enumeration 需多 session 证据时可开),非默认。

**下一步 = 转向检索侧结构(写入侧表示/query 改写三向证伪后,2026-07-24 收口)**:让 gold 在**不加量**下从 rank 71-90 升进 top-30 的努力,现已三向证伪——**010 query 侧分解**(顶不动各子查询都弱命中的 gold)、**011 alias 短标签影子**、**012 doc2query 拟人问句影子**(两者门②同点 NO-GO:影子对称抬噪、不给 gold 超原文的判别性)。**结论:瓶颈不在 query 措辞、也不在 fact 影子表示,而在 dense 单塔对 multi-hop/open-domain gold 的深层召回上限——这是「顶一个已经埋在 rank 71-90 的 gold」这一问题本身对无监督 dense 检索的天花板。** 剩余真杠杆离开「表示改写」这条线,回到 **strategy 文档的检索侧结构 P0**:① 实体图遍历(`memory_entities` 平表→可 1-hop 走边,直击 multi-hop,EcphoryRAG/HippoRAG2)、② 检索侧时间窗(`event_date`→范围 + T_score,MemOS MemReader 式,直击 temporal)。这些是**结构性新机制**(engine contract-first + 宪法 IV 门),非 box 上试 flag;open-domain 短板同理走 category-conditional「精准浮现」。按 maintainer workflow 走 brainstorm→SDD。**注**:提质型的写入/query-侧影子表示这条便宜线已探尽,不再投。

**更新(013 收口,2026-07-24)**:检索侧结构 P0 之② **检索侧时间窗** 已经门①免费诊断证伪其**这一版形态**(event_date 召回臂)——不是机制错,是**点火率错**:`ParseTemporalIntent` 只解析出 19.6% temporal 题的时间窗,臂对 80% 的题永不点火(L1-3 前提在窗成立时全健康)。**⇒ temporal 的真杠杆前移到 query 侧时间解析覆盖**(为何 80% temporal 题解析不出窗;含相对/隐式/多锚时间表达),这是一个**独立的 query-理解 feature**,不是 event_date 召回结构。检索侧时间窗召回臂**留待解析覆盖上去之后再评估**(届时 L1-3 已证其天花板)。**⇒ 当前唯一未验的检索侧结构 P0 = ① 实体图遍历(直击 multi-hop)**;temporal 改走"解析覆盖优先"。

---

## temporal 瓶颈分诊 — 答错子集 gold-in-topK 切分(近免费,离线 join,2026-07-24)

013 的四层诊断只看 `parse_coverage`,遂把 temporal 真杠杆推向 query 侧解析覆盖(召回侧)。但那是对**全体** temporal 题的召回视角,没并入**答对错**。本诊断补上这一刀:纯离线 join **013 诊断的每题 gold-rank**(`temporal_diagnostic_questions.jsonl`,bge-large `--chunks --top-k 30 --chunk-quota 12`,gold_rank≤30 = gold 确在答题 30 项上下文内)× **009-full-B-cattopk 全量跑的每题正误**(cat-top-k 不影响 temporal,均值 0.817 与 base 同;3 rep 多数投票)。零 token、零 box、零引擎改。

temporal 多数投票 acc **82.9%**,答错 **55/321**。切分:

| 子集 | n | gold 在 top-30 答题上下文 | gold 埋 top-30 外 |
|---|---|---|---|
| **答错(rate<0.5)** | 55 | **38(69.1%)= 答题侧** | 17(30.9%)= 召回侧 |
| 三 rep 全错(0/3) | 34 | 20(58.8%) | 14(41.2%) |
| 答对(rate≥0.5) | 266 | 238(89.5%) | 28(10.5%) |

- **主瓶颈 = 答题侧(gold 已喂进却答错),不是召回侧**。这**反转** 013 的初步指向:query 侧解析覆盖(召回侧)在 answered-wrong 口径下最多只能救 **8/55**(召回侧 17 题里现解析器能点火的),约 1.5% temporal——舍入级;而 38/55 是 gold 明明在 30 项答题上下文里、模型时序推理/去歧错。
- **答题侧 38 题失败模式**(眼验 + 关键词量化):**±1 月/年 误归属/精度去歧 21(55%)**(检索到邻近事件贴错日期,如"Jon in Rome"GOLD June/PRED May)· **相对表达未解析成绝对 10(26%)**("next month"/"last year"/"the week before X"被回显而非按会话锚解析)· **时长/区间算术 7(18%)**("how long / how many months"类端点在上下文里但没相减,PRED 回显区间或"not specified")。
- **⇒ temporal 下一个真杠杆 = 答题侧时序推理契约(纯 client-side / 零成本 / 提质型)**,优先级高于 013 指的 query 侧解析覆盖:① 相对→绝对日期解析(针对会话 date 锚,不回显相对短语)· ② 时长/区间算术契约(识别两端点相减输出 duration)· ③ 按 date 对齐候选去歧(压 ±1 月/年 误归属,最大但最杂)。均为答题 prompt/answer-contract 改,非引擎检索改。
- **纪律**:又一次"先诊断后建"避免建错方向——013 差点把力气全押 query 侧解析覆盖(召回侧),补上答对错口径后真金在答题侧。产物 `.locomo-run/013-temporal-diag/` 复用,无新 box 消耗。

---

## 实体图遍历 assoc(检索侧结构 P0 之①)— 免费诊断 PASS → e2e **NO-GO**(2026-07-24)

检索侧结构 P0 之①「实体图遍历(直击 multi-hop)」的了断。**重大定性**:该引擎机制**早已存在**——feature 003 Strike 1「实体平表→联想图」已建 `memory/graph.go`(`memory_entity_edges` co 共现边 + IDF 种子 2-hop `WalkEntityGraph`)+ `associativeRanks`(游走候选→原 query 向量 cosine 重排 → RRF 第4路)+ `--assoc --assoc-depth 2` flag,**default-off、从未端到端验过**。边表已填(153 条 co/conv,无 syn)。**不是待建,是待验**——再建即重复引擎、违反适配器红线。

### ✓ 免费召回诊断 = PASS(强信号)

`--attribution-trace` 同 store(009-bge-chunks-store,bge-large,canonical `--chunks --top-k 30 --chunk-quota 12`)跑 assoc 开/关两遍,retrieval-only、零答题 token。assoc 作 RRF 第4路 **全类抬 cov@30、零附带伤害(dropped=0)**:multi-hop +1.8%(抬5)、temporal +4.7%(抬15)、single +2.4%(抬20)、ALL +2.6%(抬40/跌0)。且被抬的是**深层/正交救回**(gold 从 base pool_rank 43-126、甚至不在池 rank=-1 → 直接进 top rank 1-18),这是 doc2query/alias/reranker 都动不了的——assoc 用**正交轴(实体共现图)**够到 dense 单塔埋在 rank 57-126 的 gold。诊断判 GO,值得上 e2e。

### ✗ e2e 配对 = NO-GO(coverage 没转化 + 叠加反伤)

box 全本地栈、canonical recipe、repeats=3,五臂同 store 配对:

| arm | overall | reps | 类型 |
|---|---|---|---|
| base(**冷启动首臂**) | 82.92% | [83.05,83.44,82.27] | ⚠ 异常低 |
| **base2(复跑,可靠)** | **85.17%** | [84.48,85.13,85.91] | bge-large 基线(吻合历史 85.45) |
| assoc | 84.55% | [84.74,84.61,84.29] | 提质 |
| cattopk | 85.80% | [85.84,85.65,85.91] | 加量 |
| stack(assoc+cattopk) | 84.66% | [84.55,84.42,85.0] | — |

配对 McNemar:**assoc vs base2 = net −29(χ²1.6 ns)→ 不胜基线,无增益**;**stack vs cattopk = net −53(χ²5.1 显著)→ assoc 叠 cattopk 反伤**;cattopk vs base2 = +29(χ²2.2,吻合历史 +0.6~0.9pp)。

- **决定性归因**:强 coverage 信号(+2.6pp cov@30、深层正交救回、零 coverage 伤害)**没转化成答分**——**又一次 008 铁律**(coverage≠答分,同 008 US1 reranker coverage +15pp→e2e NO-GO)。机制层面:图游走第4路在救回深埋 gold 的同时,注入"与 query 实体共现但非 gold"的干扰候选挤占融合 top-30;alone 净≈0,叠在 cattopk 上净负 −1.15pp。**你期望的「assoc 提质替代 cattopk 加量」证伪**——cattopk 严格更强(85.80 vs 84.55)且独家抬 temporal(+3.7pp),assoc 对 temporal/open-domain 纹丝不动。
- **⚠ 方法论 gotcha(重要,险酿假 GO)**:同配置 base(82.92)vs base2(85.17)差 **2.25pp**——**答题模型冷启后第一个臂被系统性压低 ~2pp**(vllm 刚起、KV cache 冷/共卡竞争)。若信首臂冷 base,assoc 表观 +1.62pp(配对 +75 χ²9.6)会被误判为 GO;是**复跑 base2 重基线**救回。**规程:box 冷启后首个臂作 warm-up 丢弃或必复跑基线**,否则冷启动惩罚会伪装成 treatment 效应。详见 [`locomo-e2e-eval-reproduction.md` §踩坑](./locomo-e2e-eval-reproduction.md)。
- **保留能力**:`--assoc` 引擎路径不动(留 default-off,003 遗产);诊断产物 `.locomo-run/014-assoc-diag/`(trace-base/assoc.jsonl + coverage-summary + e2e-verdict)。引擎零改(全程适配器+诊断,`git diff -- memory embedding provider store internal` 空)。
- **⇒ 检索侧结构 P0 收口**:① 实体图遍历 e2e 证伪(coverage 不转化)· ② 检索侧时间窗 013 证伪(解析器点火率)。**两个 P0 结构杠杆均 NO-GO**。temporal 剩余真杠杆在**答题侧时序推理契约**(见上「temporal 瓶颈分诊」),非检索侧结构。

---

## 答题侧时序推理契约(Feature 014)— 强化契约 e2e **NO-GO**(翻车),旧简单契约有正苗头未坐实(2026-07-24)

「temporal 瓶颈分诊」把真杠杆指向答题侧后,SDD 014 强化 `cmd/locomo-bench` 的 `forceTemporalAnswerPrompt`(category-2 force+temporal 路径,`--temporal-answer-prompt` 开关触发)为压三诊断失败模式(±1 去歧 / 相对→绝对 / 时长相减)的**四锚重 CoT**。box 全本地栈四臂配对 e2e 判 **NO-GO**,且是反转性的。

### 门:干净 top-k 30(无 cat-top-k)四臂配对

维护者规范:默认 top-k 30,cat-top-k 这类"大力出奇迹"只作后续无奈之举,不进默认门(同资源更耗)。故基线是干净 bge-large top-k 30。四臂同 store（009-bge-chunks-store）、canonical recipe、repeats=3：

| arm | overall(maj) | temporal(maj) | 说明 |
|---|---|---|---|
| base(冷启动首臂) | 0.8565 | 0.8287 | ⚠ 冷,漂移 −0.78pp |
| **old-tplan(旧弱契约,从未评过)** | 0.8630 | **0.8629** | 归因锚 |
| **new-tplan(强化四锚)** | 0.8558 | 0.8287 | 处理臂 |
| base2(干净锚) | 0.8643 | 0.8380 | 配 new-tplan |

配对 McNemar(temporal,n=321,3-rep 多数投票):
- **主门 new-tplan vs base2:net −3,χ²0.085,ns → 无提升,FAIL**。overall 亦不高于 base2。
- **归因 new-tplan vs old-tplan:net −11,χ²2.56(p≈0.11)→ 强化契约比它替换掉的旧简单契约更差**。
- 参考 old-tplan vs base2:net +8,χ²1.36,**ns**(temporal +2.5pp 但不显著)。

### 结论 + 教训

- **强化契约 NO-GO(翻车)**:诊断对(答题侧有杠杆),但四锚重 CoT 是**负贡献**——"reason silently"重枚举 + "EXACT MATCH…DIFFERENT event…never close enough"的强硬去歧,让答题器**过度拒绝邻近候选/想太多**,temporal 掉到 base 以下。**又一次 008 铁律**:表观合理的 prompt 工程,端到端答分说了算。**"强化过头"**——比"什么都不加"和"旧简单契约"都差。
- **旧简单契约(old-tplan)是唯一正苗头,但未坐实**:仅"list 每条 [event:] 日期 / normalize / compare / never decline"的原有弱契约(**从没被评测过**,canonical recipe 默认不带 `--temporal-answer-prompt`)temporal **+2.5pp**、overall 中性。零新代码(只是打开现有 flag)。但 **McNemar ns(p~0.24,单 run n=321)** → 够不上 GO 门。**记 backlog:后续多-rep(5-8 rep 或多 seed)专测 old-tplan vs base2 temporal,看 +2.5pp 能否跨显著门**;若显著即零成本 GO。
- **处置**:强化常量 + 四锚单测已 `git revert`(还原 `forceTemporalAnswerPrompt` 为旧契约);引擎全程零改(纯适配器)。SDD 正本 `specs/014-temporal-answer-contract/`;`--temporal-answer-prompt` 开关 + old-tplan-baseline.md 保留供 backlog 确认。
- **方法论兑现**:冷启动纪律再次生效——base 冷首臂 0.8565 vs base2 0.8643(−0.78pp),主门用 base2 不用冷首臂。

### ✗ backlog 兑现:old-tplan 8-rep 确认 = **ns,收口 inconclusive**(2026-07-25)

按下方 recipe 原样执行(box 全本地栈,bench 直接跑在 box 上;店从 HF 拉回 box,`009-bge-chunks-store`;warm-up 1 rep 丢弃 → base2 8 reps → old-tplan 8 reps,`--only-category 2`,canonical recipe 干净 top-k 30):

| 臂 | temporal maj acc(8-rep 多数投票) | per-rep 带 |
|---|---:|---|
| warm-up(丢弃) | 82.2% | 店温度核对 ✓ 落历史带 |
| base2 | **85.36%** | 79.1–83.2% |
| old-tplan | **86.29%**(+0.93pp) | 79.4–85.4% |

- **配对 McNemar:b=18 / c=21,net +3,χ²=0.231(门 3.841)→ ns,FAIL。** 上轮四臂的 +2.5pp(net +8)是单 run 抖动的慷慨端;8-rep 多数投票把它压回 +0.93pp / net +3。
- **处置:`--temporal-answer-prompt` 维持默认关,不出货。** temporal 答题侧 prompt 类杠杆(强化四锚版 + 旧简单版)至此**双双收口**——前者显著更差,后者 ns。temporal 剩余升级路径只剩确定性日期脚手架(Option B,TIMELINE 块),属新机制、需单独 SDD。
- 口径注:regime 四要素两臂核验一致(仅差 `temporal_answer_prompt=true`);同店同批 back-to-back,检索跨臂逐字节一致。产物:HF `wallfacers/engram-locomo-artifacts` 下 `014b-oldtplan-confirm/`(三臂逐题 + 下节 trace)。judge=deepseek-v4-flash 官方端点。

### backlog(已兑现,存档):old-tplan 显著性确认 recipe(下次 box 空档跑,~45-60min)

目标:把 old-tplan(打开现有 `--temporal-answer-prompt` = 旧简单契约)的 temporal +2.5pp 从"趋势(p~0.24)"推过显著门,或证伪。零新代码——只是启用现有 flag。

- **两臂,各 repeats=8**(或多 seed),同 store `009-bge-chunks-store`,canonical recipe,**干净 top-k 30 无 cat-top-k**:
  - `old-tplan`:`--temporal-answer-prompt`(当前 HEAD 的 `forceTemporalAnswerPrompt` 已 revert 回旧契约,直接用当前 binary 即可,无需还原)。
  - `base2`:同 recipe 不带 `--temporal-answer-prompt`。
- **纪律**:box 冷启后先跑一个 warm-up 臂丢弃(或复跑 base 做锚);paired McNemar 只对干净复跑基线(踩坑#10)。
- **判据**:temporal(n=321)3+rep 多数投票配对 McNemar,net 需 χ²>3.841(约 net≥±14 @ n=36 discordant)才显著;并核 overall 不回退。显著且 overall 不降 = **零成本 GO**(启用现有 flag 即出货);仍 ns = old-tplan 收口 inconclusive。
- **注意**:answer temp=1.0 非确定,单 run 观测有抖动;8-rep 多数投票是为压这抖动、非声明确定性差分([[locomo-answer-nondeterministic]] 精神)。

---

## single-hop / open-domain 错题分诊 — 近免费 trace × 答对错 join(2026-07-25)

temporal 分诊(上文)证明"先切答题侧/召回侧再选杠杆"是唯一不建错方向的办法。本轮把同一刀补到**从未逐题分诊过的 single-hop(最大错题绝对量)**与 open-domain。方法:box 上全量 `--attribution-trace`(retrieval-only,1540 题,0 答题/judge 调用,近免费)拿逐题 `gold_rank_topk/gold_rank_pool` × `009-full-A-base` 3-rep 多数投票逐题正误,离线 join。产物:HF `014b-oldtplan-confirm/014b-trace/`。

| 类 | n | 错题(maj) | gold 在 top-30 答题上下文(答题侧) | gold 埋 top-30 外(召回侧) | 召回侧深度 |
|---|---:|---:|---:|---:|---|
| **single-hop** | 841 | 94 | **58(62%)** | 36(38%) | pool p50=142 p90=239,不在池仅 1 |
| **open-domain** | 96 | 33 | 17(52%) | 16(48%) | pool p50=186 p90=232,不在池 2 |

- **single-hop 主瓶颈也在答题侧(62%)**,与 temporal(69%)同构。答错样例眼验的失败模式:**答案粒度过粗**(GOLD "a cup with a dog face" → PRED "pots")、**答错主体/方面**(问 Melanie 的反应 → 答孩子们的感受)、**多候选挑错事件**(问 road trip 后放松方式,GOLD hike → PRED painting)。部分属 judge 边界(语义接近但表述偏),部分是真检索去歧/细节保持问题——**尚未逐题分类量化,是下一步的输入**。
- **single-hop 召回侧 38% 的 gold 埋深 p50=142**——与 multi-hop/open-domain gold 深埋同一堵墙(dense 单塔深召回上限,010/011/012 三向证伪的对象),便宜杠杆同样够不着,不另立方向。
- **open-domain 近半错题(48%)是召回侧且埋更深(p50=186)**——坐实 opinion-pass NO-GO 时的判断:证据(观点/偏好类)在池深处,粗放扩覆盖救不了,要救得走 **category-conditional 精准浮现**(结构性新机制)。答题侧 17 题多为真推理难题(如 "Would Caroline be considered religious?" GOLD "Somewhat" → PRED "No"),prompt 类杠杆在 temporal 上已两连败,不优先。
- **⚠ 教训前置**:014 证明"看着合理的答题 prompt 强化"端到端可以是负贡献。single-hop 答题侧 58 题在动手前必须先完成**失败模式逐题分类量化**(粒度/主体/去歧/judge 边界各占多少),且任何契约改动过同样的配对 McNemar 门,不再凭眼验样例直接上杠杆。

### single-hop 答题侧 58 题逐题分类(2026-07-25,单标注者全量眼验 3-rep 预测)

| 失败模式 | n | 占比 | 典型样例 |
|---|---:|---:|---|
| **A 去歧挑错**(同人物/话题的另一条真事实,非题目所锚) | **28** | **48%** | 问 Oct 13 分享的画 → 答了另一幅;问 December deal → 答了 beverage endorsement(gold: outdoor gear) |
| **B 粒度过粗**(细节在上下文却答成上位概念) | 11 | 19% | gold "purple"(rank 1!)→ 答 "bright and bold";gold "a cup with a dog face" → 答 "pots" |
| **C 答错主体/方面**(问 X 的反应答了 Y 的状态) | 5 | 9% | 问 Melanie 的 reaction → 答孩子们 enjoyed it |
| **D 假性"未提及"/人物归属错** | 6 | 10% | gold 在 top-30 却答 "no mention";Tim 的事安到 John 头上 |
| **F 相对时间粒度**(gold 相对表达,答绝对年份/回显 session 日期) | 4 | 7% | gold "when she was 10" → 答 "2003";gold "at an early age" → 答 session 日期 |
| **E judge/gold 边界**(答案 arguably 对) | 3 | 5% | 问 pets → 答蛇的名字 "Susie and Seraphim"(gold "snakes") |
| 其他/乱码 | 1 | 2% | — |

- **主结论:A+B = 39/58(67%)是同一族——答题模型在 gold 已在 30 项上下文(常在 rank≤20)时的细粒度选择/细节保持失败**,与 temporal 的 ±1 误归属同构。该族的 prompt 契约杠杆已在 temporal 上**两连败**(014 强化版显著更差、旧简版 ns),对 single-hop 复刻 prompt 契约先验极低,**不作为下一杠杆**。
- **可下手的诚实方向(按性价比)**:① **答题模型强度归因诊断**(近免费):把这 58 题原上下文喂给更强答题模型(如 deepseek-v4-pro)看翻转率——若大量翻转,说明剩余 gap 相当部分是 harness 答题模型上限(竞品用更强 answerer 的可比性问题),不是 engram 检索/记忆问题;这直接改变"还有多少 gap 值得追"的判断。② **D 族(10%)指向抽取侧人物归属/caption 保真**,是引擎质量的真信号但量小。③ E+F(12%)是 judge/gold 口径,不值得单独追。
- 分类清单(逐题 qid)与原始 58 题全文:HF `014b-oldtplan-confirm/` + 本地 scratchpad `sh58-classification.md`(单标注者判断,存档供复核)。

### ⭐ 答题模型强度归因诊断:58 题换 deepseek-v4-pro 重答 = **43% 翻转**(2026-07-25)

方法:精确复刻 harness 答题路径(trace 的 top-30 名单 × 店内容重建 `buildAnswerPrompt`、同 `forceAnswerSystemPrompt`、同 mem0-aligned judge deepseek-v4-flash),只把答题模型 Qwen3.6-35B → **deepseek-v4-pro**,3-rep 多数投票。约 ¥1 级成本。

| 失败模式 | 翻转 | 解读 |
|---|---:|---|
| A 去歧挑错 | **15/28 (54%)** | 半数是答题模型上限,强模型能选对 |
| C 答错主体 | **4/5 (80%)** | 几乎全是答题模型上限 |
| B 粒度过粗 | **2/11 (18%)** | 强模型也答不出——细节不在文字上下文里 |
| D 假性未提及 | 3/6 | 混合 |
| F 相对时间 | **0/4 (0%)** | gold 相对表达 vs 绝对回答,模型无关 |
| **合计** | **25/58 (43.1%)** | |

**caption 缺口量化**(gold 词面在 blip_caption vs 对话 text,免费离线):未翻转 33 题中 **12 题 gold 只在图片 caption 里**——店里根本没有这条证据(`--image-captions` 默认关,caption 从未进抽取输入),这些是**伪"答题侧"**(实为 ingestion 覆盖缺口)。扩展到全量 220 道错题:**caption-borne 共 18 题(single-hop 17 + multi-hop 1)= 全救回也只值 ~1.2pp overall 天花板**(temporal/open-domain 为 0——它们的 NEITHER 高是 gold 聚合/推断型,词面法对其低估,诚实 caveat)。

**三个结论(改变 gap 叙事)**:
1. **single-hop 答题侧的最大块(43%)是 harness 答题模型上限,不是 engram 检索/记忆缺陷**。25 题 ≈ +1.6pp overall 当量。若 temporal 答题侧 38 题同率翻转,答题模型上限的总当量可能到 +2~3pp——**对 MemOS(gap ~3.4pp)的差距可能有一半左右是答题模型可比性伪影**(竞品用 gpt-4o-mini 级 answerer)。坐实需扩测其他类答题侧错题(近免费)。
2. **`--image-captions`(现成 adapter flag,默认关)是真覆盖杠杆但天花板 ~1.2pp**:换抽取输入 → 需重建店 + 完整 e2e 门(宪法 IV),排优先级时按 ≤+1pp 预期。
3. F/E 族(judge/gold 口径,~12%)不值得追;B 族剩余 = caption 缺口为主。
产物:scratchpad `answerer_probe.py` + `answerer-probe-results.json`(推 HF `014b-oldtplan-confirm/singlehop-triage/`)。

### ⭐ 答题模型上限总当量(全类扩测 + 反向探针,2026-07-25)

同方法扩到全部四类:正向 = 各类答题侧错题**全集**(gold_rank_topk≤30 且 A-base 3-rep 多数错)换 v4-pro;反向 = 各类**答对**题分层抽样 40,测 v4-pro 反把对题答错的率。

**成本(事后离线重放精算,2026-07-25 补)**:探针脚本**未插桩 usage**(`call()` 丢弃 `usage`、无 `cost.json`),原记"~¥2"是先验拍值、无实测依据。拉 HF store+trace 用同一 `build_prompt` 逻辑重放全部 prompt + tiktoken 计数得:answer(v4-pro)**738 次 / 2,407,422 in-tok**(3262/次)/ 5,292 out-tok;judge(v4-flash)**738 次 / 265,217 in-tok**(359/次)。按 DeepSeek 官方单价折算 = **$0.381 ≈ ¥2.70**(@7.1),原估"~¥2"偏低 26%、同量级。

> ⚠️ **勘误(2026-07-26,插桩实测后修正)**:上面这笔离线重放把 judge 输出按 **22 tok/次**
> 估,实测是 **728 tok/次**(v4-flash judge 带推理输出,`completion_tokens` 含 reasoning)。
> 该笔的 judge out 因此低估约 33×,真值应在 **¥3.7** 量级而非 ¥2.70。**离线重放补算补不回
> reasoning tokens——只有插桩 `usage` 算得准**,这是对 [[probe-scripts-must-instrument-usage]]
> 的加强:即便事后重放"偏差仅 -0.4%"的结论,也只对 **输入** token 成立。详见下节实测 cost.json。

**校准实测(同日,2 次调用花 $0.00013)**:取同一真实 prompt 发 v4-pro 读回 `usage` —— 实测 `prompt_tokens` 3418 vs tiktoken 估 3403,**偏差仅 -0.4%**(token 量估算可信到 1% 内);`prompt_cache_hit_tokens` 3328/3418 = **97.4% 命中**,`miss` 90 = 缓存 64-tok 块的尾块。据此建命中模型(rep1 只命中 system 前缀 185/312 tok,rep2/3 命中 total−90)得 answer 命中 **66%**、judge **71%**,即"3 reps 串行 ⇒ 2/3 命中"假设被实测证实,硬上界 ¥7.8 不会发生。残留误差仅 judge 输出按 22tok 估(占比<1%)与未记录的 retries;权威口径仍是平台账单。**教训:自制探针一律累加 `usage` 并落 cost.json,不许拍估算。**

| 类 | 正向翻正(实测全集) | 反向翻错(40/类抽样) |
|---|---:|---:|
| temporal | **27/41 (66%)** | 1/40 (2.5%) |
| multi-hop | 13/28 (46%) | 0/40 (0%) |
| single-hop | 25/58 (43%) | 1/40 (2.5%) |
| open-domain | 6/17 (35%) | **5/40 (12.5%)** |

- **净当量:+71 −33(反向外推)= +38 题 ≈ +2.5pp overall ⇒ answerer-parity 预期 ≈ 87.9%**(区间 ~87.3–88.4,反向抽样二项误差),对 MemOS 88.83 **差距基本抹平但可能仍略低**。
- **temporal 答题侧 66% 翻正是最强信号**:此前"答题侧时序推理契约"两连败的根因大概率是 **Qwen3.6-35B 执行不动那种细粒度契约**,不是契约方向错——强模型不用契约就答对了。014 收口结论(prompt 杠杆已死)不变,但归因修正为"answerer 能力上限"。
- **open-domain 反向 12.5% 翻错**:主观/推断题上强模型答得"不一样"而非更好,n=96 小类,judge 边界性强——answerer 升级救不动 open-domain(与 008 US2、opinion-pass 结论一致)。
- **gap 叙事定论**:对 MemOS 的 ~3.4pp 差距,**~2.5pp(约七成)是 harness answerer 可比性伪影**;引擎侧真剩余 ≈ 1pp 级(caption 缺口 ~1.2pp 天花板与之量级吻合)。**对 Mem0 92.5 的差距即便 parity 后仍有 ~4.6pp,那部分才是真功课**。
- 产物:scratchpad `answerer_probe2.py` + `answerer-probe2-results.json`(推 HF 同目录)。确证性 full parity run(1540×3,~¥30-80)可把 87.9% 预估变实测,属 diagnostic、不进默认栈、不改诚实参考点 85.4%。

### ❌ 015 离线固结·跨 session 桥接合成 — 门 0 判死(2026-07-25)

**方案**: 引擎第三个写入阶段(在线抽取 / 策展减法之外的"加法")——离线跨 session 枚举
共享稀有实体的证据对,LLM 合成一条新的桥接 fact 作为同构记忆落库,把多跳答案变成一跳可
命中。生物学轴 = 互补学习系统(海马回放→新皮层固结);大模型轴 = 2026 重构式记忆一簇。
**Mem0/MemOS/AtomMem 三家均无离线固结**(全是在线逐 session 抽取),空白属实。
设计/spec/plan/tasks 见 `specs/015-consolidation-bridging/`。

**门 0(纯本地、零模型调用、零成本)判决: 判死。** 判据 A 候选召回 **0/33 = 0.0%**
(死线 <40%)。但真正的杀手是分母:

| 口径 | 可及题数 | 全量当量 | 说明 |
|---|---:|---:|---|
| 设计时**拍脑袋**假设 | ~113 | +7.3pp | multi-hop 失败率按 ~40% 拍的,**未查证** |
| 实测 multi-hop 失败 | 35 | +2.27pp | 实测失败率仅 **12.4%**,multi-hop 是四类里第二好 |
| 扩全类别 + gold 跨 session | 58 | +3.77pp | single_hop 失败 94 题中跨 session **为 0** |
| **实测可桥接(作用于 fact)** | **5** | **+0.32pp** | 严格测量 |
| 覆盖率修正上界 | ~16 | ~1.0pp | 按 fact 承载率 27% 修正 |

**根因(比判死本身更有价值)**: 目标集里 gold 证据 **73% 由 chunk 承载,仅 27% 由 fact
承载**;而 **chunk 完全没有实体索引**(conv0: 83 chunk / 0 有实体)。故:
1. 候选枚举(基于共享实体)结构性地**只看得见 fact**,gold 却主要在 chunk 上 ⇒ 命中率必然 0;
2. 更根本地,**fact 抽取层没有覆盖大部分 gold 信息**——不是"fact 之间缺连接",而是
   "fact 里根本没有那些信息"。桥接 fact 救不了本就不在 fact 里的东西。

**证伪门的价值兑现**: 门 0 零成本、零引擎代码即判死,避免了 28 项任务 / 一个新引擎包
(`memory/consolidation/`)+ v6 migration 的全部投入。三个外部 codex agent 一个都没启动。
**教训与 answerer 探针同源: 死线的价值依据(失败题基数)必须先实测再写死,不许拍。**
本次死线数值本身没挪,是它背后的奖品实测缩水 3.3-23 倍。

**衍生的新方向(尚未验证)**: chunk 是主力召回来源却游离于实体信号之外。
(a) 给 chunk 建实体索引 → 三路 RRF 的实体信号目前对 73% 的 gold 载体失效;
(b) 抽取覆盖: 为何 gold 信息没被抽成 fact。二者都比桥接更贴近实测瓶颈。

产物: scratchpad `gate0.py`;数据取自 HF `009-eval-runs/009-full-A-base` + `014b-trace`
+ `009-bge-chunks-store`。

### ❌ chunk 实体索引 — 零成本诊断判死(2026-07-25),附**失败题象限正本**

015 判死后的接续方向:三路 RRF 中实体信号对 chunk 完全失效(chunk 零实体索引),
而 gold 大量由 chunk 承载。方案:用**已有 fact 实体词表**(LLM 已把质量关)对 chunk
做字符串匹配建索引 —— 纯 Go、零模型、零依赖。

**先测分母(015 的教训)——失败题象限正本(A-base 3-rep 多数错,n=220):**

| 象限 | 题数 | 当量 | 含义 |
|---|---:|---:|---|
| **Q2 答题侧**(gold 已进 top-30) | **144** | **9.35pp** | 检索改进救不了 |
| **Q3 排序**(在 pool 未进 top-30) | 73 | 4.74pp | 排序杠杆的真分母 |
| **Q4 抽取/覆盖**(不在 pool) | 3 | 0.19pp | **抽取覆盖不是瓶颈** |

⇒ **检索侧总空间仅 4.93pp,答题侧 9.35pp 才是最大块**。Q4=3 题**当场零成本毙掉
"抽取覆盖"方向(b)**。Q3 的 gold_rank_pool 中位 **142**(max 294),要提进 top-30
需跨越极大距离。

**判决:天花板 ≈0.52pp,判死。** 精度/召回对撞(Q3 73 题上实测):

| df 上限 | 有信号题 | 选择性(命中chunk占比) | gold 位次中位 | gold 进 top30 |
|---:|---:|---:|---:|---:|
| 不限 | 73 | **100.0%** | 45 | 24 |
| 50 | 23 | 19.7% | 9 | 10 |
| 20 | 14 | 9.7% | **2** | **8** |
| 10 | 10 | 3.4% | 1 | 5 |

放开 ⇒ query 实体命中**全部** chunk(选择性 100%,均值 98.5%),gold 位次仅第 45/111
—— **对称抬升、相对位置不变**;收紧 ⇒ 区分度好(gold 位次 2)但只剩 14 题有信号。
最优工作点 df≤20 也仅 8 题 gold 排前 ≈ **0.52pp**,且这已是"实体信号内部排序"的
乐观口径,不等于最终 RRF 进 top-30。

### ⭐ 四连败的共同机理(011/012/010/chunk实体) — 战略结论

| 杠杆 | 死法 |
|---|---|
| 011 dual-index alias | 影子向量对称抬噪,gold 相对位置不变 |
| 012 doc2query shadow | 同上 |
| 010 multi-query retrieval | 同上 |
| chunk 实体索引 | query 实体命中 100% 的 chunk,同上 |

**engram 的检索瓶颈不是"信号不够多",而是"新信号没有区分度"。** 四次尝试撞的是
同一堵墙:新信号要么对称抬升所有候选(无区分度),要么区分度够但覆盖极少题。
**继续在检索侧加信号,回报递减已被四次实测确认。**

结合象限正本:检索侧总空间 4.93pp 且极难撬动,而 Q2 答题侧 9.35pp(扣掉 ~2.5pp
answerer 伪影后仍有 ~6.9pp)才是最大未开发块 —— 但 014 已证"答题侧 prompt 契约"
两连败(根因是 Qwen3.6-35B 执行不动细粒度契约,非契约方向错)。

产物:scratchpad `chunk_entity_probe.py` / `selectivity.py` / `idf_variant.py`。

### ❌ 上下文工程双假设 H1/H2 — 零成本判死(2026-07-25)

四连败机理("加信号 ⇒ 对称抬噪")促使转向**减法**方向:gold 已在上下文却答错,
问题或许在"够着了也用不上"。两个零成本假设,双双判死。

**H1 lost-in-the-middle — 死。** gold 在 top-30 中的位置 vs 正确率(n=1371):

| gold rank | 1-5 | 6-10 | 11-15 | 16-20 | 21-25 | 26-30 |
|---|---:|---:|---:|---:|---:|---:|
| 正确率 | 91.2% | 89.6% | 89.6% | 89.9% | 84.3% | **93.6%** |

位置与正确率**无关**(末段 26-30 反而最高)。⇒ 重排序/砍 top-k 无效。

**H2 上下文冗余/噪声淹没 — 死。** 证据全覆盖的失败组(119)与成功组(1072)对比:

| 指标 | 失败组 | 成功组 |
|---|---:|---:|
| 上下文条数 | 30 | 30 |
| gold 条数 | 1 | 1 |
| 噪声比 | 96.7% | 96.7% |
| gold 最前 rank | 19 | 19 |
| chunk 条数 | 12 | 12 |

**两组上下文结构逐项完全一致** ⇒ 失败与上下文的组织方式**无因果关系**,
去冗余/精排/压缩均无据可依。

### ⭐⭐ 证据覆盖正本 — 检索侧 vs 答题侧的最终分账(2026-07-25)

按 exact-turn 证据覆盖率重切全体 1540 题(比 gold_rank 象限更严,要求**全部**
evidence 进 top-30 而非至少一条):

| 证据覆盖 | 题数 | 正确率 | 错题 | 当量 |
|---|---:|---:|---:|---:|
| **1.0 全覆盖** | 1191 | **90.0%** | 119 | **7.73pp** |
| 0.5-0.99 部分 | 146 | 87.0% | 19 | 1.23pp |
| 0.01-0.5 少量 | 33 | 81.8% | 6 | 0.39pp |
| **0 零覆盖** | 166 | **54.8%** | 75 | 4.87pp |

**关键条件量:证据全覆盖 ⇒ 90.0% 正确;零覆盖 ⇒ 54.8%。条件增益仅 35.2pp。**

⇒ **检索侧完美化(把 166 题零覆盖全部变全覆盖)的绝对上限 = 166×0.352 ≈ 58 题
≈ +3.8pp**,且需要检索**零缺陷**,而该方向已四连败。

⇒ **答题侧(证据全覆盖仍答错)= 119 题 = 7.73pp,是最大未开发块**;但其中约一半
由 answerer 模型能力解释(v4-pro 翻正 43-66%),且 H1/H2 已证与上下文组织无关,
014 已证 prompt 契约无效 —— **剩余部分是题目难度/judge 边界,非工程可解**。

**战略结论:engram 在当前 LoCoMo harness 下已接近架构上限。** 单日零成本连续
判死五个方向(015 桥接 / chunk 实体 / 抽取覆盖 / H1 位置 / H2 冗余),全部未花钱、
未写引擎代码。继续在 LoCoMo 刷分的边际回报已被实测证明极低。

产物:scratchpad 诊断脚本若干。

### ⭐⭐ A. embedding 模型升级 — 死(2026-07-25,零成本,本地 GPU)

**问题**:bge-large 是否是检索瓶颈?换更强的开源 embedder 能否吃下那 3.8pp 检索侧池子?

**死线(测前写死)**:换模型后精确轮次覆盖**从「非全」变「全」的净题数 < 25 题
判死**(25×35.2% 条件转化 ≈ 0.57pp,低于此不值得动全栈重建 + e2e 门);净值 ≤ 0 直接死。

**设计**:同一 store 副本、同一 chunks、同一 canonical 配方,**只换 embedding 模型**。
全部本地 RTX 5070 跑,`--coverage-only` + `--attribution-trace`,`answer_calls=0
judge_calls=0`,**零 token 花费,不占用远程评测机**(该机当时由另一 agent 跑 MemOS)。
刻意排除 e5-mistral 一类 7B embedder —— 与「本地优先可嵌入」的诚实规模声明冲突,
赢了也不能出货。

| 臂 | 模型 | 覆盖@30 | Δ | 转正 | 转负 | 净 | 符号检验 p |
|---|---|---:|---:|---:|---:|---:|---:|
| A0 | `bge-large-en-v1.5`(现役) | **0.808** | — | — | — | — | — |
| A1 | `mxbai-embed-large-v1` (335M) | 0.802 | −0.6pp | 29 | 37 | **−8** | 0.39 |
| A2 | `Qwen3-Embedding-0.6B` (595M) | 0.804 | −0.4pp | 126 | 134 | **−8** | 0.66 |
| A0p | bge + 官方 query instruction 前缀 | 0.803 | −0.5pp | 27 | 29 | **−2** | 0.89 |

**三臂净值全负、全不显著 ⇒ 判死。** Qwen3 翻上 126 题又翻下 134 题 —— 大幅重排
但净值为零,典型的「不同但不更好」。

**附带否掉的假设**:engram 对 query 与 document 走同一条**无前缀**编码路径,曾疑心
这对 instruction-tuned 模型不公平。实测 bge / mxbai / Qwen3 三者加官方 query 前缀
分别为 −0.5pp / −0.4pp(0.744→0.740,匹配口径) / +0.3pp(0.744→0.747,匹配口径)
—— **query-instruction 协议不是杠杆**,engram 现有的对称用法没有吃亏。

**结论:检索瓶颈不在 embedding 模型。** 换代、换更大、换协议都动不了覆盖率,
这与「四连败共同机理」一致 —— 新信号要么对称抬升全部候选(无区分度),要么有区分度
但覆盖极少题。

#### ⚠️ 过程中发现的真实工程陷阱(与本判决同等重要)

`memory.Embedder.Backfill` 是**有界**的:队列满即丢弃,靠下一次 Backfill 补
(`memory/embedder.go:236-255` 注释明写)。**换 embedding 模型后跑一趟 build,
只回填约 256 条/conv**,本次实测 3812 条 entry(含 alias 影子共 4892 行)一趟只补上
2569 行 —— **33% 语料对语义信号不可见,而检索器按设计静默降级、不报错**。

该伪影一度把两个候选模型的成绩压到 0.744(−6.4pp),**差点被当成「新模型更差」的
模型结论报出去**;补齐到 4892 行后真实值是 0.802/0.804(−0.6/−0.4pp)。

**教训:任何换 embedding 模型的评测,必须先断言
`count(memory_embeddings WHERE model=<new>) == 目标行数`,再读分数。** 一趟 build
不足以完成回填。这条同样适用于未来 SaaS 形态的模型迁移。

---

## ⭐ answerer = deepseek-v4-flash 配对实测(2026-07-26,304 题逐题配对)

**问题**:answerer-parity 那 +2.46pp 必须用 v4-pro 才拿得到吗?便宜一档的 v4-flash 能吃下多少?

**方法**:v4-pro 探针的**严格配对复刻**——任务集直接从 v4-pro 两份 results.json 读 qid
(58 题 single-hop + 86 fwd + 160 rev = **304 题**),同 top-30 上下文(trace 名单 × 店内容
重建 `buildAnswerPrompt`)、同 category force system prompt、同 mem0-aligned judge
(v4-flash)、3-rep 多数投票。**唯一变量 = answerer**。prompt token 实测 3264/次 vs
v4-pro 那次记录的 3262/次(偏差 0.06%)⇒ prompt 复刻正确。

| 类 | 正向翻正 flash | v4-pro | 反向翻错 flash | v4-pro |
|---|---:|---:|---:|---:|
| temporal | **27/41 (66%)** | 27/41 (66%) | 2/40 (5%) | 1/40 (2%) |
| multi-hop | **14/28 (50%)** | 13/28 (46%) | 0/40 (0%) | 0/40 (0%) |
| single-hop | **23/58 (40%)** | 25/58 (43%) | 1/40 (2%) | 1/40 (2%) |
| open-domain | **3/17 (18%)** | 6/17 (35%) | 5/40 (12%) | 5/40 (12%) |

按 v4-pro 同口径外推(正向=错题全集实测翻正,反向=抽样翻错率 × 各类答对池)分类别:

| 类别 | n | base(A-base 3-rep 多数) | **flash-parity** | Δ | pro-parity | Δ |
|---|---:|---:|---:|---:|---:|---:|
| single-hop | 841 | 88.82% | **89.34%** | +0.51 | 89.57% | +0.75 |
| multi-hop | 282 | 87.59% | **92.55%** | **+4.96** | 92.20% | +4.61 |
| temporal | 321 | 81.93% | **86.25%** | **+4.31** | 88.29% | +6.36 |
| open-domain | 96 | 65.62% | **60.55%** | **−5.08** | 63.67% | −1.95 |
| **OVERALL** | **1540** | **85.71%** | **87.49%** | **+1.77** | 88.17% | +2.46 |

- **flash 与 pro 统计不可区分**:McNemar 配对 n=304,flash 对/pro 错 = 11,flash 错/pro 对 = 16,
  **双尾精确 p=0.442**;逐题一致率 **91.1%**。差异唯一集中在 open-domain(flash+2 / pro+5)。
  temporal 那 0.43pp 的差全部来自反向抽样 **2/40 vs 1/40 的一题之差被 ×6.6 外推放大**,非真信号。
- **收益结构**:全在 multi-hop(+4.96)与 temporal(+4.31);single-hop 几乎不动(+0.51);
  **open-domain 反而 −5.08pp** —— 强 answerer 在主观/推断题上"答得不一样而非更好",与 008 US2、
  opinion-pass、v4-pro 反向 12.5% 翻错三处一致。**⇒ open-domain 换 answerer 救不动,已封口。**
- **口径注**:base 85.71% 是 A-base 3-rep **多数投票**聚合,与诚实参考点 85.4%(3-rep mean)
  差 0.31pp,同一批 run 的两种聚合。flash/pro 两列是 **304 题配对探针外推,不是全量 run 实测**。
- **处置同 v4-pro:属 diagnostic,不进默认栈、不改诚实参考点 85.4%。** 用途是把"对 MemOS 的
  gap 有多少是 answerer 可比性伪影"用便宜三倍的模型再确认一次——flash 口径下同样成立。

**实测成本(usage 全程插桩,`cost.json` 落盘)**:1824 次调用(912 answer + 912 judge),
in **3,309,949** tok(缓存命中 **66.7%**)、out **669,738** tok → **$0.3479 ≈ ¥2.47**,692 秒,2 次 retry。

> **关键成本发现**:out 成本 $0.1875 **超过** in 成本 $0.1604。分解后 answer 输出仅 **6 tok/次**
> (LoCoMo 短答),**judge 输出高达 728 tok/次**(带 reasoning)。此前所有按"judge out ≈22 tok"
> 做的估算全部严重低估 judge 侧。全量 1540×3 修正后:answer(flash)≈¥5.2 + judge(flash)≈¥7.3
> = **≈¥12.5**(旧口径估 ¥6.1);若 judge 改 v4-pro,增量 **≈+¥15**(out 单价 0.87 × 3.36M tok),
> 而非按 22 tok 估的 +¥1.4~3.9。

产物:`.locomo-run/014c-flash-probe/`(`flash_probe.py` / `analyze.py` /
`flash-probe-results.json`(304 题逐题,含 `pro_maj_correct` 配对列)/ `cost.json` / `run.log`;
已从会话 scratchpad 持久化,凭据已核无泄漏)。
✅ **已归档 HF**(2026-07-26):私有集 `wallfacers/engram-locomo-artifacts` 下
[`014c-flash-probe/`](https://huggingface.co/datasets/wallfacers/engram-locomo-artifacts/tree/main/014c-flash-probe)
(5 文件,已回读仓库列表核验)。**注**:归到平级的 `014c-flash-probe/` 而非原计划的
`014b-oldtplan-confirm/` 子目录——它是独立一轮探针(answerer=flash),与 014b 三臂产物不同批。

---

## 确定性日期脚手架(Feature 017,TIMELINE 块)— e2e **NO-GO(落在噪声内)**(2026-07-27)

**立意**:temporal 分诊坐实主瓶颈在**答题侧**(答错题 69% gold 已在 top-30),且 prompt 契约
两连败(014 强化版显著更差、旧简单版 ns)。017 换一条路:**不要求模型推理,用确定性 Go 代码
把日期算完**——从每条记忆自带的 `[event:]` 结构化日期抽出时间线、按时序排序、把窄集合的相对
表述(`next month` / `two weeks ago` / `yesterday`)锚定到该条自己的日期上、并算出首尾跨度,
作为 `TIMELINE` 块前置进 category-2 答题上下文。模型只做**选择**,不做**计算**。
默认关(`--temporal-date-scaffold`),canonical recipe 逐字节不受影响(golden 基线测试钉死)。

### 门:temporal n=321,三臂 + warm-up,各 8 rep 多数投票

box 全本地栈(bench 直接跑在 box 上),同店 `009-bge-chunks-store`(bge-large 1024d + chunks),
canonical recipe(`--chunks --top-k 30 --chunk-quota 12 --retrieval hybrid --force-answer
--judge-mem0-aligned`,干净 top-k 30 无 cat-top-k),`--only-category 2`,judge=deepseek-v4-flash。
臂序刻意为 **warmup → base → scaffold → ref**:base 与 scaffold 相邻(主对比受漂移影响最小),
`ref` 跨整段作**保守**噪声标尺。

> **口径偏离(刻意,已声明)**:三臂跑 `--only-category 2`(321 题)而非全量 1540。理由:
> 开关**只在 `category==2` 生效**,非 temporal 题的答题提示**逐字节不变**(US1 golden 基线
> 测试所证),跑它们只是在计费 GPU 上重测已知不变的东西(全量将从 ~26 分钟涨到 ~2 小时)。
> 代价是 overall 只能**投影**而非直测 —— 上面第 3 项按此如实标注。

| 臂 | temporal maj acc(8-rep) | per-rep 带 | ctx tok 均值 |
|---|---:|---|---:|
| warm-up(丢弃) | 81.93%(1 rep) | — | 3693 |
| `base` | **82.24%** | 79.1–84.7% | 3676 |
| `scaffold` | **82.87%**(+0.62pp) | **81.6–84.7%** | **4264** |
| `ref`(= base 复跑) | **81.31%**(−0.93pp) | 78.2–84.1% | 3675 |

**五项必产数字**:

1. **temporal 变化:+0.62pp**(82.24% → 82.87%);
2. **配对 McNemar(scaffold vs 干净 base)**:b=12 / c=14,**net +2,χ²=0.154(门 3.841),精确二项 p=0.845 → ns**;
3. **overall 回退检查**:非 temporal 提示**逐字节不变**(US1 golden 基线测试证明,开关只在
   `category==2` 生效)⇒ overall 投影 **+0.13pp**,不回退也不显著;
4. **噪声标尺 `ref` vs `base`:−0.93pp**(net −3,χ²=0.474,ns)——**标尺绝对值 0.93pp
   已经大于处理臂的 +0.62pp**;
5. **token 增量:+588 tok/题(+16.0%)**,中位 +588、max +1591。

### 判定与归因

**NO-GO**:GO 判据要求「temporal 配对显著抬升 AND overall 不回退」,**第一条不满足**
(p=0.845,离显著门差一个数量级)。

归因按三分法:**「落在噪声内」**,不是「思路错」也不是 014 式「上下文被稀释」。依据:

- **不是没点火**:逐题 token 配对显示**点火率 100%(321/321)**,每题都拿到了 TIMELINE 块;
  实现按契约工作(US1 的 22 条断言 + 100% 点火共同排除了"功能没生效"这一解释);
- **不是被稀释**:scaffold 的 per-rep **下界反而抬高**(81.6% vs base 79.1% / ref 78.2%),
  上界持平(84.7%)——多出的 588 tok 没有把答题器压垮,只是没换来净新增正确;
- **就是太小**:+0.62pp = 321 题里净 +2 题,而同配置复跑的标尺是 −0.93pp / net −3。
  **一个比自己噪声标尺还小的差分,不能宣称有效**(FR-012 设 `ref` 臂的全部意义)。
  名义上限 2.47pp(答题侧 temporal 38 题)本就标注"实际远低",实测兑现不足其 1/4 且不显著。

### 处置 + 教训

- **`--temporal-date-scaffold` 维持默认关,不出货,不产出移植文档。** 回滚路径:
  `cmd/locomo-bench/timeline.go` + `timeline_test.go` 可原子删除,其余是签名扩展(传 `""` 即还原)。
  代码保留的唯一理由是它是**这条路已被走过的证据**,不是待启用的开关。
- **temporal 方向至此三连收口**:强化 prompt 契约(显著更差)/ 旧简单契约(ns)/ 确定性日期脚手架
  (ns 且小于噪声)。三条路覆盖了「让模型推理」与「替模型算完」两种范式,**temporal 答题侧
  38 题的名义空间没有廉价兑现路径**。
- **又一次 008 铁律的变体**:这次连中间信号都是满分(点火 100%、算得对、降级正确、确定性),
  端到端仍然 ns。**"实现正确" 与 "有用" 之间没有推论关系**,只有配对实测能连接两者。
- **方法学兑现**:冷启动纪律再次生效(warm-up 81.93% 明显低于随后 base 的 82.24% 与其 per-rep 上界);
  `ref` 标尺第一次**直接改写了结论口径**——没有它,+0.62pp 会被读成"小幅有效"。

产物:`.locomo-run/017-scaffold/`(warmup/base/scaffold/ref 四臂 × 逐题 jsonl + `regime.json` +
`stats.json` + `cost.json` + `context_parity.jsonl`,4.7M,gitignored)。
`scaffold` 臂 `regime.json` 含 `temporal_date_scaffold=true`(开关生效已核,T033)。
实测成本:box GPU 约 **45 分钟**(含冷启与一次判题 401 返工),答题/抽取全本地零付费,
judge 侧 26 rep × 321 题的 deepseek-v4-flash 微付费(`cost.json` 记 0,deepseek 不在价表)。
产物已逐文件扫描凭据,**零命中**(唯一命中 `43078` 系 ci95 浮点尾数误报)。
SDD 正本:[`specs/017-temporal-date-scaffold/`](../specs/017-temporal-date-scaffold/)。

---

## `--image-captions`(剩余方向 #4)— e2e **NO-GO**(机制生效但盘子太小 + 附带稀释,2026-07-27)

**立意**:全量错题分诊实测 **caption-borne 18 题**(single-hop 17 + multi-hop 1)——gold 词面
只出现在图片 `blip_caption` 里,而 caption **从未进过抽取输入**(`--image-captions` 默认关),
店里根本没有这条证据。这是**唯一一条机理与"五连败"不同的剩余杠杆**:不是排序、不是表示改写、
不是 prompt,是 **ingestion 覆盖缺口**。locomo10 实测 1226/5882 turn(20.8%)带 caption。

### 门:全量 1540,四臂,两套**同会话新建的同源店**

**基线必须是重建的店,不能复用 `009-bge-chunks-store`** —— flag 的 help 自己写了
「changes extraction input, so stores built with/without it are not comparable」。
拿旧店当 base 会把"抽取重跑的漂移"记到 caption 头上。代价是多建一次店(8 分钟,box 本地近免费)。

| 臂 | 店 | reps | maj acc | per-rep 带 | ctx tok |
|---|---|---:|---:|---|---:|
| warm-up(丢弃) | ctrl | 1 | 85.13% | — | 3619 |
| `ctrl` | 018-ctrl-store(新建,无 caption) | 5 | **87.21%** | 85.6–86.2% | 3614 |
| `cap` | 018-cap-store(新建,`--image-captions`) | 5 | **86.49%** | 85.2–86.4% | 3631 |
| `ref` | ctrl 复跑 | 5 | **86.36%** | 84.8–85.8% | 3617 |

**必产数字**:

1. **overall 变化:−0.71pp**(87.21% → 86.49%);
2. **配对 McNemar(cap vs ctrl)**:b=86 / c=75,**net −11,χ²=0.752,p=0.431 → ns**;
3. **噪声标尺 `ref` vs `ctrl`:−0.84pp**(net −13,χ²=2.600,p=0.136)——**标尺比处理效应还大**;
   参考 `cap` vs `ref` = **+0.13pp**(ns)⇒ 换个基线符号就翻,**双向都在噪声里**;
4. **token:+17/题(+0.47%)**,可忽略(与 017 的 +16% 完全不同性质);
5. **分类拆解**:single-hop(靶心所在)**−0.24pp / net −2**;temporal **−3.12pp / net −10**;
   multi-hop +0.35pp;open-domain ±0。

### 靶心诊断:机制**确实生效**,但盘子只有个位数

离线复算 caption-borne 子集(gold 词面 ≥80% 命中 caption 且 <50% 命中对话正文,比台账当初
在 220 错题内的口径更严,全量 1540 上得 **8 题**):

| caption-borne 子集(n=8) | ctrl | cap |
|---|---:|---:|
| 准确率 | 25.0% | **50.0%**(救回 3 / 打坏 1) |

**⇒ 这不是"flag 没生效"**:caption 确实进了抽取与 chunk(店 37M vs 33M,chunk 数普遍上升),
确实把图像证据变可检索,靶心上净 +2 题。**但 +2 题 = +0.13pp**,而 caption 文本折进每个 turn
使 exact-turn 召回**全类下降**(免费覆盖信号 turn@k 0.808 → 0.795,single-hop 0.877 → 0.865,
temporal 0.825 → 0.807),附带损失把那点收益吃干净还倒欠。

### 处置 + 教训

- **`--image-captions` 维持默认关,不出货,不进 canonical recipe。** 它作为**可选**能力保留
  (真实场景若图像证据密度远高于 LoCoMo 的 20.8%,权衡可能反转);但在 LoCoMo 口径下
  **无可测端到端增益**。
- **台账 ~1.2pp 上限的兑现率 ≈ 1/10**:那 18 题是"全救回"的理论天花板,实测靶心只净 +2,
  且不含附带伤害。**这是本仓第 N 次验证「名义上限 ≠ 可得增量」**;今后台账上限应默认按
  "乐观端 × 未扣附带伤害"读。
- **与 017 的对照很有价值**:017 是「中间信号满分(100% 点火)但端到端 ns」;018 是
  「靶心确实被打中(+2 题)但被自身副作用抵消」。**两种不同的失败机理,同一个结论——
  只有配对 e2e 能分辨。**
- **剩余未验方向由两项收缩为一项**:只剩 **#2 category-conditional 精准浮现(open-domain)**,
  名义上限 1.04pp,新机制需 SDD。

产物:`.locomo-run/018-image-captions/`(warmup/ctrl/cap/ref 四臂逐题 jsonl + regime/stats/cost
+ 两套店的 coverage,15M,gitignored)。实测成本:box GPU 约 **75 分钟**(建两套店 17min +
四臂 56min),答题/抽取全本地零付费,judge 侧 16 rep × 1540 题微付费。产物已扫凭据,**零命中**。

---

## 剩余未验方向盘点(2026-07-26 收口后重排)

截至本日,**五连败**(011 alias / 012 doc2query / 010 multi-query / chunk 实体索引 /
A embedding 模型升级)+ 检索侧结构双 P0 证伪 + 答题侧 prompt 契约两连败 + H1/H2 判死 +
015 桥接判死。检索侧总空间实测仅 **4.93pp**(证据覆盖口径下完美检索绝对上限 **+3.8pp**),
答题侧 7.73pp 中 **1.77~2.46pp 已确认为 answerer 伪影**(上节)。**仍未验的只剩三项:**

> **2026-07-27 再更新(当日两轮)**:#3(确定性日期脚手架)由 **017** 端到端证伪、
> #4(`--image-captions`)由 **018** 端到端证伪(均见上方 verdict 节),与 #1 的作废合计 ——
> **剩余未验方向由四项收缩为一项:只剩 #2(category-conditional 精准浮现,open-domain,
> 名义上限 1.04pp,新机制需 SDD)**。

| # | 方向 | 上限(台账实测分母) | 为何未被"五连败机理"覆盖 | 成本 |
|---|---|---:|---|---|
| ~~**1**~~ | ~~**judge 口径补齐**(补 Mem0 的"部分给分"+"±14 天容差")~~ | ~~**~1.7pp 保守**~~ | **❌ 已作废 —— 该工作早已由 spec 007 完成,见下方更正** | — |
| **2** | **category-conditional 精准浮现**(open-domain) | open-domain 召回侧 16 题 ≈ **1.04pp** | **只对特定类别开信号**,不做全局对称抬升——正是 opinion-pass 净负的根因 | 新机制,需 SDD |
| ~~**3**~~ | ~~**确定性日期脚手架**(014 Option B,TIMELINE 块)~~ | ~~temporal 答题侧 38 题 ≈ **2.47pp**(实际远低)~~ | **❌ 已证伪(2026-07-27)** —— 017 三臂 e2e **+0.62pp / p=0.845 / 小于 `ref` 噪声标尺 0.93pp**,见上节 verdict | 已花:box ~45min |
| ~~**4**~~ | ~~**`--image-captions`**~~ | ~~**~1.2pp**(caption-borne 18 题,已实测)~~ | **❌ 已证伪(2026-07-27)** —— 018 四臂 e2e **−0.71pp / p=0.431 / 小于 `ref` 噪声标尺 0.84pp**;靶心确被打中(+2 题)但被自身召回稀释吃掉,见上节 verdict | 已花:box ~75min |

**新证据对 2/3 的支持(来自上节)**:open-domain 在 answerer-parity 后**反而降 5.08pp**
⇒ 坐实 #2 是它唯一的路;temporal 答题侧 **66% 可被强 answerer 翻正**(四类最高)
⇒ #3 那 38 题的失败是"模型算不动日期"而非"信息不在上下文",正是确定性脚手架的靶心。

### 🚨 更正(2026-07-27):上表 #1「judge 口径补齐」是**基于过期源码对比写的,该工作早已完成**

**核实结论:`judgeMem0AlignedSystemPrompt`(`cmd/locomo-bench/runner.go:502-513`)已经包含
那两条规则**——「部分给分」在 L505(*"Give partial credit when the prediction includes at least
one correct item from a gold list"*)、「±14 天日期容差 + 时长 ±50% + 相对日期」在 L508。
由 **spec 007** 落地,`--judge-mem0-aligned` flag + anti-放水 golden 门(26/26)俱在。
**且本文全部现行基线(85.71% / 89.03% / 同栈对跑)本来就是用它跑出来的**
(见 [`results-matrix-2026-07-26.md` §1](./results-matrix-2026-07-26.md):「判题固定
deepseek-v4-flash + mem0-aligned prompt」)。

**错在哪**:#1 的依据引自 [`competitive-benchmarks.md` §6](./competitive-benchmarks.md),
那份源码逐条对比作于 **2026-07-21**,对比的是**旧的 strict judge**(`judgeSystemPrompt`,
runner.go:494-500)——那一版确实缺这两条。007 在**同一天**新增了对齐版常量,但 §6 与本表
都没回填,于是「一个具体、可修的口径缺口」这句话在自家 commit 已经修掉之后又被引用了一次。

**⇒ 那 ~1.7pp 不是可得增量,它早已计入现行基线。** 不得再作为待验杠杆、不得计入剩余空间、
更不得叠加到任何其他杠杆的收益上。剩余未验方向由四项改为**三项**(#2 / #3 / #4)。

**教训(方法学)**:台账引用另一份文档的源码结论时,必须核对**该结论作出之后本仓是否已经
改过那段代码**。跨文档引用 + 同日 commit,是这次误判的成因。

**并行线状态(2026-07-26)**:MemOS 同栈复现 **✅ 已出分**
(见 [`memos-inhouse-locomo-repro.md`](./memos-inhouse-locomo-repro.md) §6);
LongMemEval **已启动**(gitignore 就位,commit 1fc86f8)。

### ⚠️ 外部锚点变了:上表四方向的"追赶 MemOS"动机已消失(2026-07-26)

MemOS 自家代码跑在 **engram 同款答题模型 + 同款 embedder + 同一 judge** 上 = **82.40%**,
**低于** engram 同口径 `009-full-A-base` 的 **85.71%** 达 **3.31pp**;
leaderboard 那个 88.83 里的 **6.43pp 是 regime 红利**(answerer 强度 + judge 宽松度),不是能力。

| | OVERALL | multi-hop | temporal | open-domain | single-hop |
|---|---:|---:|---:|---:|---:|
| MemOS @ engram 同栈 | 82.40% | **89.36%** | 82.55% | 59.38% | 82.64% |
| engram `009-full-A-base` | **85.71%** | 87.59% | 81.93% | **65.62%** | **88.82%** |

**对本文的三处直接影响**:

1. **本文开头"vs 目标:MemOS 88.83(gap ~5.1pp)"这条目标线作废** —— 它对着的是含 6.43pp
   伪影的数字。剩余四方向的排序**不再有"竞品已经做到了"这个外部背书**,只剩各自的台账上限
   (1.7 / 1.04 / 2.47 / 1.2 pp)撑着。**别再用"MemOS 能到 88 所以我们也能"论证任何杠杆。**
2. **"堆记忆组织/graph/reranker"这条路被外部证据关小**:MemOS 默认栈同时带
   tree/graph 记忆 + 本地 `bge-reranker-v2-m3` + `fine` 深检索,**在同栈下只换来 multi-hop
   +1.77pp,总分还输 3.31pp**。这与本文 008 US1(同一 reranker,端到端 −0.06pp)、
   五连败检索侧结论**互相印证**:engram 剩余 gap 不在"记忆组织形态"。
   (⚠️ 这条修正了 `memos-inhouse-locomo-repro.md §5.5` 当时"差距收窄到记忆组织形态"的推断——
   那是在**假定 MemOS 领先**的前提下写的,前提已翻。)
3. **本文"#1 judge 口径补齐"的性质变了,优先级更高**:此前它是"对齐竞品口径以缩小 gap";
   现在 engram 已领先,补齐 judge 口径变成**对外发布/论文口径的可信度问题**——
   要宣称"engram > MemOS",judge 必须站得住,而本文已实证 engram judge 比 Mem0/OmniMemEval 严。
   仍属宪法 IV 口径改动:单独 commit、声明新基线、明标"非算法涨点"。

**⚠️ 引用该结论必须带的 caveat**(全文见 repro 文档 §6.3):两边各用**自家默认检索预算**,
engram 实测喂 answerer **3262 tok/次** vs MemOS **~1059 tok/次(≈3 倍)**,+3.31pp 里
"上下文更多"的贡献未剥离;MemOS 只跑 1 次答题、无误差带;**未做 1540 题配对 McNemar**
(engram 逐题 pred 在 HF,拉回来 join 是零 token 成本的加固动作)。
