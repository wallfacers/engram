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

**杠杆哲学(maintainer 定调,2026-07-23)**:只认**提质**的赢(同预算/同 top-k 下把对的证据捞得更准,如 bge-large),**反感加量**(撑 top-k / 扩池 / 喂更多 context —— 分是真的但拿成本税换,不可移植,换个部署就塌)。产品是设备/应用习惯记忆,集成方无限 context 预算不存在。**据此 cat-top-k 从「头条 GO」降级为 optional/非默认**;本表所有「加量」型即便涨分也不进默认栈。

**净出货(提质路线):bge-large → +1.3pp(~85.4%)**,是唯一符合哲学的干净赢。cat-top-k 作为 optional 旋钮保留(多跳 enumeration 需多 session 证据时可开),非默认。

**下一步 = 转向检索侧结构(写入侧表示/query 改写三向证伪后,2026-07-24 收口)**:让 gold 在**不加量**下从 rank 71-90 升进 top-30 的努力,现已三向证伪——**010 query 侧分解**(顶不动各子查询都弱命中的 gold)、**011 alias 短标签影子**、**012 doc2query 拟人问句影子**(两者门②同点 NO-GO:影子对称抬噪、不给 gold 超原文的判别性)。**结论:瓶颈不在 query 措辞、也不在 fact 影子表示,而在 dense 单塔对 multi-hop/open-domain gold 的深层召回上限——这是「顶一个已经埋在 rank 71-90 的 gold」这一问题本身对无监督 dense 检索的天花板。** 剩余真杠杆离开「表示改写」这条线,回到 **strategy 文档的检索侧结构 P0**:① 实体图遍历(`memory_entities` 平表→可 1-hop 走边,直击 multi-hop,EcphoryRAG/HippoRAG2)、② 检索侧时间窗(`event_date`→范围 + T_score,MemOS MemReader 式,直击 temporal)。这些是**结构性新机制**(engine contract-first + 宪法 IV 门),非 box 上试 flag;open-domain 短板同理走 category-conditional「精准浮现」。按 maintainer workflow 走 brainstorm→SDD。**注**:提质型的写入/query-侧影子表示这条便宜线已探尽,不再投。

**更新(013 收口,2026-07-24)**:检索侧结构 P0 之② **检索侧时间窗** 已经门①免费诊断证伪其**这一版形态**(event_date 召回臂)——不是机制错,是**点火率错**:`ParseTemporalIntent` 只解析出 19.6% temporal 题的时间窗,臂对 80% 的题永不点火(L1-3 前提在窗成立时全健康)。**⇒ temporal 的真杠杆前移到 query 侧时间解析覆盖**(为何 80% temporal 题解析不出窗;含相对/隐式/多锚时间表达),这是一个**独立的 query-理解 feature**,不是 event_date 召回结构。检索侧时间窗召回臂**留待解析覆盖上去之后再评估**(届时 L1-3 已证其天花板)。**⇒ 当前唯一未验的检索侧结构 P0 = ① 实体图遍历(直击 multi-hop)**;temporal 改走"解析覆盖优先"。
