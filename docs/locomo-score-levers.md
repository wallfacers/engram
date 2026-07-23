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

## 杠杆总账(2026-07-23 收口)

box vllm 全本地栈(Qwen 答题 + bge-large 嵌入 + deepseek mem0-aligned judge)、canonical recipe、repeats=3 下,叠加式探完一轮:

| 杠杆 | 判定 | Δ overall | 类型 | 机制 |
|---|---|---|---|---|
| bge-large embedder | **GO**(shipped) | +1.3pp | **提质** ✓ | 同 top-k,向量更强 → 召回转化 |
| cat-top-k `1=150` | 有效但**非首选/不设默认** | +0.9pp | **加量** ✗ | 多跳扩检索预算(context 2.4× 税) |
| opinion-pass | NO-GO | −0.6pp | 加量 | 粗放覆盖污染全局 precision |
| filter-pool | 不可测/成本差 | — | 加量 | LLM-per-question 大 context 压垮 box vllm |
| cluster-sweep | INCONCLUSIVE | +0.4(噪声内) | 加量 | 实体簇扩展,配对对照证伪表观增益 |

**杠杆哲学(maintainer 定调,2026-07-23)**:只认**提质**的赢(同预算/同 top-k 下把对的证据捞得更准,如 bge-large),**反感加量**(撑 top-k / 扩池 / 喂更多 context —— 分是真的但拿成本税换,不可移植,换个部署就塌)。产品是设备/应用习惯记忆,集成方无限 context 预算不存在。**据此 cat-top-k 从「头条 GO」降级为 optional/非默认**;本表所有「加量」型即便涨分也不进默认栈。

**净出货(提质路线):bge-large → +1.3pp(~85.4%)**,是唯一符合哲学的干净赢。cat-top-k 作为 optional 旋钮保留(多跳 enumeration 需多 session 证据时可开),非默认。

**下一步 = 提质型深召回**:让 gold 在**不加量**下从 rank 71-90 升进 top-30 —— query 分解(多跳题拆子查询各自精检)/ HyDE(闭合 query↔fact 语义差)/ 更好的 fact 写入表示。剩余 gap(open-domain 64%)同理走「精准浮现」非「粗放灌入」。按 maintainer workflow 走 brainstorm→SDD,非继续 box 上试 flag。
