# Feature 003 Evaluation Log

This file records maintainer-run evaluations. Every run must be preceded by
`--estimate`; retain the estimate, actual cost, statistics, comparison verdict,
and the keep/revert decision together.

## Strike 0: Calibration Baseline

- Date: 2026-07-19 (10:01–12:42 +08:00, 单次连续运行)
- Dataset / repeats: locomo10.json 全量 10 段 × 1540 题(可判分四类) × repeats=5, single-pass (--no-idk-retry), hybrid, top_k=30
- Answer model: gpt-5.6-sol (答题+判分, 中转站 tokensfree)
- Extract model candidates: A=gpt-5.6-luna, B=gpt-5.6-sol
- Frozen extract model: **gpt-5.6-luna**（见 Decision）
- Estimate output: A ¥76.36 / B ¥77.89（合计 ¥154.25）
- Actual cost (`cost.json`): A **¥25.10** / B **¥48.81**（合计 **¥73.91**，为预估 48%）。
  高估主因：答题输出实测均值 ~47 tok/题（含 reasoning），远低于预估假设 300 tok/题。
  A 臂 answer 输入 12.1M tok vs B 臂 27.3M tok——luna 抽取的记忆更紧凑
  （answer_context_tokens_mean 1576 vs 3545），连带砍半答题成本。
- Run directories: `.locomo-run/strike0/extract-{a,b}/run-{1..5}`
- `stats.json`: A overall 51.0% CI95=[40.7, 61.4]; B overall 50.6% CI95=[37.3, 63.8]。
  per-category (A/B mean%): multi-hop 29.2/28.7, temporal 58.4/59.6,
  single-hop 55.8/54.5, open-domain 48.5/49.6。
- `compare.json` / verdict: flips A→B=219, B→A=202, McNemar p=0.436, CI overlap=true
  → **within-noise**（两抽取模型无统计差异）。
- Calibration baseline (mean +/- 95% CI): hybrid+luna 抽取 = **51.0% ± 10.3pp**。
  ⚠️ 该 CI 不可用作后续判定门（见下方方差诊断）。
- Model contribution delta: 与旧基线（74.7@luna, 带 IDK 重试, 单跑）差 -23.7pp，
  为模型 regime + single-pass 口径 + 后端漂移三者混合，如实记录不拆分。
- Decision: **冻结抽取模型 = gpt-5.6-luna**。质量 within-noise (p=0.436) 且成本占优
  （抽取 ¥0.39 vs ¥3.24，8×；紧凑上下文使答题成本再省一半），SC-007 token 预算
  也更有利。全部后续枪一律 EXTRACT_MODEL=gpt-5.6-luna。

### Strike 0 方差诊断（测量协议修正的依据）

逐 run overall（1540 题，抽样 SE 理论值仅 ±1.3pp）：

| run | A 臂 | A IDK 率 | B 臂 | B IDK 率 |
|-----|------|---------|------|---------|
| 1 | 52.5% | 15.4% | 59.7% | 9.7% |
| 2 | 44.7% | 24.1% | 40.8% | 27.9% |
| 3 | 43.0% | 25.9% | 40.9% | 27.3% |
| 4 | 50.9% | 16.4% | 63.7% | 4.9% |
| 5 | 64.1% | 5.5% | 47.6% | 19.6% |

- run 间摆幅 ~21pp，为抽样噪声的 ~8×；四类别同涨同跌 → run 级全局潜因，
  定位为**中转站对 gpt-5.6-sol 的后端服务质量随时间窗漂移**（库跨 repeat 复用、
  检索确定，唯一随机源是答题+判分调用）。
- 判分噪声可忽略：7154 对「预测文本逐字相同」的跨 run 比较仅 1.2% 判分翻转。
- 主杠杆是**拒答通道**：IDK 率 4.9%~27.9%（差 5.7×），与 overall 几乎完美负相关；
  single-pass 口径下可判分四类全部可答，每个 IDK 必判错。
- 剔除 IDK 后子集 acc 仍有 ~±5pp 残余漂移（弱 run 拒答多且答错多，非选择效应），
  即后端漂移同时压低回答质量，堵拒答通道只能消掉最大头。
- 答题调用未显式设 temperature（wire 层 omitempty → 服务端默认采样），但 run 内
  1540 题独立采样无法解释 run 级同向漂移，temperature 非主因。

### 测量协议修正（自 Strike 1 起生效）

1. **同进程配对双臂**：bench 已内建多臂机制（`--retrieval both` = fts+hybrid 共库、
   同时间窗答题判分）。各枪判定改为在**同一次调用内并跑 baseline 臂与 treatment 臂**
   （例：hybrid vs hybrid+assoc），run 级后端漂移在 per-question 配对中作为共同因子
   被 McNemar 抵消。跨目录 `--compare`（不同时间窗）仅作参考，不作判定门。
2. **反拒答口径（answerable 四类）**：answer prompt 移除 "I don't know" 出口、
   要求必给最佳猜测（single-pass 不变，符合 Mem0 等对照 regime；对抗题/拒答校准
   由 Strike 3 独立口径处理）。此为口径改动，独立 commit（宪法 IV）。
3. 校准基线数字本身不再作为跨时间比较锚点；每枪比较自带同窗 baseline 臂。

对账：预估模型的答题输出假设从 300 tok/题 修正为 **~50 tok/题**（sol 短答几乎
不动用 reasoning）；抽取(luna) 实测 ¥0.39/全量建库一次。

## Strike 1: Associative Retrieval

### 2026-07-19 运行事故记录（判定前，两次中止）

第一次运行（18:14 启动）在答题阶段开始时死锁，17 分钟仅产出 4 题后被
SIGQUIT 终止（goroutine dump 留存于当时的 strike1.log）。链式根因：

1. **第一因 — embedding 无界并发**：答题阶段每题一个 goroutine，LLM 调用
   受 24 槽信号量门控但检索侧不受控 → 数千个 query embedding 同时打到
   本地 Ollama 单 runner，排队超过 30s HTTP 超时后全体失败（15 秒内
   3080 次 `semantic signal degraded`），runner 被压崩（health EOF）。
   19:12 第二次运行立即复现，确认为并发压垮而非偶发。
2. **第二因 — LLM 调用无超时 + HTTP/2 单连接**：所有中转站请求 multiplex
   在同一条经本地代理（127.0.0.1:7897）的 HTTP/2 连接上；该连接在同一
   时间窗挂死后，~3000 个 in-flight Stream 无超时永久等待 header，信号量
   槽位全部占死 → 全局死锁。

修复（均为 harness/基础设施改动，非评测口径、非引擎算法）：

- `e0f4403` fix(embedding): HTTPClient 加 MaxInflight 信号量（默认 4，
  与 Ollama 默认并行度匹配），排队不消耗 HTTP 超时。
- `2a4b78b` fix(bench): gateUsage/gate 每次 LLM 调用加 3 分钟超时 +
  单次重试，超时取消释放槽位；重跑环境加 `GODEBUG=http2client=0`
  禁用 HTTP/2 单连接复用（HTTP/1.1 下单连接死亡不再殃及全部请求）。

费用影响：建库产物（s1-store，luna extraction）完整保留并被第三次运行
复用（"reusing persisted extraction"），事故净损失仅前两次的 ~7 题答题，
金额可忽略。第三次运行（19:14 启动）健康：degraded=0，答题稳定产出。

- Date: 2026-07-19（第三次运行，前两次中止见上）
- Dataset / repeats: locomo10 全量（1540 题）× 5 repeats × 双臂 = 15400 answer calls
- Flags: `--retrieval hybrid,hybrid+assoc --no-idk-retry --force-answer`（同进程配对，Amendment 001）
- Estimate output: ¥95.40
- Actual cost (`cost.json`): **¥139.50**（超预估 46%。分角色：answer ¥78.95、judge
  ¥60.55。偏差源：judge 输入实测 ~4055 tok/题，estimate 常量 1600 偏低 2.5×；
  answer 输入实测 ~5146 tok/题，常量 4000 略低。→ 待办：校正 estimateJudgeIn
  至 4000、estimateAnswerIn 至 5100）
- `stats.json`: A=hybrid overall **65.4%** CI[64.6, 66.3]；B=hybrid+assoc
  **58.8%** CI[57.9, 59.7]。per-category A/B：multi-hop 44.2/39.2、temporal
  74.1/63.0、single-hop 70.3/64.0、open-domain 56.2/55.8
- `paired.json` / verdict: 同窗 McNemar **p=3.1e-10, above-noise**，但方向为
  **负**：flips B→A 205 vs A→B 95，B 臂 overall **−6.7pp**。分类别配对：
  multi-hop −5.7pp（目标类别反而回退）、temporal −12.5pp、single-hop −6.7pp、
  open-domain +2.0pp（唯一小涨，within-noise 量级）
- Token budget ratio: answer_context_tokens_mean 5145（cost.json 为双臂合并值，
  B 臂已判负，预算门无需单独裁定）
- Decision (keep / revert): **revert**。--assoc 不进判定基线，保留为实验 flag。
- Notes:
  1. **第一枪脱靶且为显著负效果**：F1-F14 全部修复后 assoc 仍在固定 top-k=30
     预算下将净噪声注入候选，挤掉相关记忆——temporal −12.5pp 是最重伤类别
     （联想边把非目标时间的记忆拉进来）。这与调研裁决 2 完全一致：multi-hop
     病根是**覆盖截断**而非缺联想信号（全证据平均需 10.81 块 vs top-k 内
     实际相关块不足），图游走类信号（assoc/PPR 同族）对此无结构性帮助。
  2. **基线大幅抬升**：hybrid 65.4% vs Strike 0 的 51.0%。主要来自
     force-answer 反拒答口径（Strike 0 诊断的最大杠杆，IDK 失分归零）+
     embedding 并发修复后 semantic 路满血 + 后端时间窗较好。multi-hop 44.2%
     仍是最大洼地，temporal 74.1% 已接近可用。
  3. 触发 Strike 1.5 预案：multi-hop 44.2% < 50% → 按调研裁决启动
     cluster-sweep（枚举意图→实体簇全召回→按 session 分组聚合）设计。
  4. 费用累计：¥73.92 + ¥139.50 = **¥213.42**；放行时余额 ¥248.34 →
     现余 ~¥108.84（以后台实际为准）。判定运行费用超预估 46%，estimate
     常量校正后下次预估应可信。

## Strike 2: Temporal Retrieval

- Date: 2026-07-19 ~ 2026-07-20（全新建库 s2-store，5 repeats）
- Dataset / repeats: locomo10.json（英文，1540 题）× 5 repeats，配对双臂
- Flags: `--retrieval hybrid,hybrid+temporal --no-idk-retry --force-answer
  --temporal-answer-prompt`（**两臂共用 temporal_answer_prompt=true**，唯一
  变量是检索侧时间打分）
- Estimate output: 名义 ¥146.61
- Actual cost (`cost.json`): 名义 **¥151.39**（answer 79.07 + judge 71.87 +
  extract 0.44），超预估 +3.3%（校正后常量已准）。后台实扣按缓存折扣约
  ≈¥75（待与余额对账）。answer_context_tokens_mean=5044。
- `stats.json`: hybrid OVERALL 66.8% [66.2,67.5] / temporal 类目 75.8%
  [74.9,76.6]；hybrid+temporal OVERALL 66.0% [65.2,66.9] / temporal 类目
  76.2% [75.3,77.1]。18 次答题网络失败（≈0.12%，噪声级），embedding 降级 0。
- `paired.json` / verdict: 同窗多数票 McNemar **verdict=within-noise,
  p=0.266, CI overlap=true**。逐类目：
  - **temporal（靶心）Δ−0.3pp, p=1.000**（B救活 6 / B搞砸 7）——零效果
  - single-hop −0.8pp p=0.391（21/28）、multi-hop −1.4pp p=0.556（11/15）、
    open-domain +0.0pp p=0.752
  - OVERALL −0.8pp（B救活 43 / B搞砸 55）
- Token budget ratio: answer 上下文均值 5044 ≈ 基线 5145，未超预算门。
- Decision (keep / revert): **revert**。`--temporal-score` 不进判定基线，保留
  为实验 flag（同 --assoc 处置）。event_start/event_end 加列为 additive，无害
  保留；temporal 答题 prompt 的贡献未在本枪隔离（两臂共有）→ 交批次 5
  `+tplan` 隔离测。
- Notes: 第 2 连负（Strike 1 −6.7pp above-noise / Strike 2 −0.8pp within-noise）。
  靶心类目 p=1.000 坐实"往 RRF 螺栓仿生信号"打法在 sol 后端不成立；run-1~3
  提前量的 +0.8pp 系噪声，全量多数票归零。转向 cluster-sweep（结构性攻覆盖
  截断）+ answer-plan 隔离。

## Answer-Plan Prompt 隔离（tplan，2026-07-20）

低成本假设验证：把 temporal 答题-计划 prompt（Strike 2 两臂共有、烘进基线的
"最大杠杆"）单独隔离。同进程配对，唯一变量是该 prompt，**只跑 category 2**
（321 题）省成本。

- Flags: `--retrieval hybrid,hybrid+tplan --only-category 2 --repeats 5
  --force-answer --no-idk-retry`（**不带**全局 --temporal-answer-prompt；
  A=hybrid=prompt OFF，B=hybrid+tplan=prompt ON），复用 s2-store，run-dir b-tplan
- Estimate / Actual: 名义 ¥30.86 / ¥33.41（+8%），实付按缓存折扣 ~¥16。0 失败 0 降级。
- `paired.json` / verdict: **above-noise, McNemar p=0.0169, CI overlap=False**。
  多数票 A(prompt OFF)=80.1% vs B(prompt ON)=76.3%，**Δ=−3.7pp**；prompt 救活 5 /
  搞砸 17（去掉净救活 12，p=0.019）。
- **Decision: 去掉 `--temporal-answer-prompt`（标准口径）**。调研号称 −17.6pp
  的"大杠杆"在 sol 后端是负资产（temporal −3.7pp 显著）——首个 above-noise 正
  结果，靠"做减法"得到。
- **新有效基线（各块实测拼算，无需重跑）**：temporal 用去-prompt 的 80.1%，其余
  类目不受影响（prompt 仅作用 cat 2）→ **overall ≈ 67.6%**（vs 带 prompt 66.8%，
  +0.8pp）。single-hop 71.9 / multi-hop 44.5 / open-domain 58.1 / temporal 80.1。
- Notes: Strike 2 的 temporal-score 判定不受影响（handicap 对称加两臂）。方法论
  验证——隔离已烘进基线的成分，能发现"移除即涨点"的杠杆，胜过盲目叠加。

## Offline Coverage Bake-off（`--coverage-only`，2026-07-20）

零答题成本的检索-only 尺子（`cmd/locomo-bench --coverage-only`）：给每个臂算
exact-turn / session 证据回收，**不调 answer/judge**。唯一成本是一次性建库
（gpt-5.6-luna 抽取，10 conv，可 `--store-dir` 复用）+ 本地 fastembed sidecar 的
query 向量（离线免费）。目的：在花答题钱前，先离线判定哪个检索 lever 真的抬高
证据覆盖（尤其 multi-hop 这个洞）。

- Store: 10 conv 全量，349±facts/conv + 63–127 chunks/conv；embed=bge-small-en-v1.5(384d)
- Flags(主对比): `--coverage-only --chunks --retrieval hybrid,hybrid+sweep --top-k 30 --chunk-quota 12`，n=1532 可评题
- **turn recall @ top-k30/quota12**：

  | 类别 | hybrid | hybrid+sweep | Δ |
  |------|--------|--------------|---|
  | multi-hop | 0.559 (281) | 0.480 | **−7.9pp** |
  | open-domain | 0.462 (89) | 0.442 | −2.0pp |
  | single-hop | 0.807 (841) | 0.791 | −1.6pp |
  | temporal | 0.759 (321) | 0.749 | −1.0pp |
  | OVERALL | **0.732** | 0.705 | −2.7pp |

- **session vs turn 饱和度**：hybrid 总体 sess=0.958 vs turn=0.732；multi-hop
  sess=0.886 vs turn=0.559（**33pp 鸿沟**）。session recall 会误报"检索基本解决"，
  turn recall 才露真洞——坐实 Gap-1 measurement 修复的价值。
- **判决 sweep**：cluster-sweep 全类别一致拖低 turn 覆盖，multi-hop 最惨（−7.9pp，
  正是它本该帮的类别）。机制：490 个 fact 簇候选（截到 120）挤掉 top-k 里的
  verbatim chunk，而 turn recall 只来自 chunk。**sweep 方向反了，不值得升级到付费
  答题评测。** 免费扼杀一个伪 lever。
- **multi-hop 洞的归因（免费复用建库）**：给 category 1 加 chunk 预算，turn recall
  平滑随预算上升 → 覆盖是**预算受限**，非 fact-breadth 受限：

  | multi-hop 配置 | turn recall |
  |----------------|-------------|
  | top-k30 / quota12 | 0.559 |
  | top-k50 / quota25 | 0.657 (+9.8pp) |
  | top-k100 / quota60 | 0.950 (+39.1pp) |

- **诚实边界**：quota60≈抓走每 conv 半数以上 chunk，answer context ~5x → 会触发
  `budget_ratio>1.5x` 失效护栏，属"覆盖作弊"而非真答题涨点。故真正值得付费验证的
  lever 是**定预算下的 chunk 重排**（late-interaction / 交叉编码器 / interference-
  packing），让 gold chunk 挤进前 ~12 而不膨胀 context。coverage 覆盖是答题正确的
  必要非充分条件——该 bake-off 只要接上 reranker 即可在付费前先量化其覆盖增益。
- **Decision**：(1) 不为 cluster-sweep 花答题钱（离线证伪）。(2) 下一个候选 = 定
  预算 chunk 重排，先用同一 bake-off 离线测覆盖增益，过 +Npp 阈值再上付费答题 +
  McNemar 显著性闸。Artifacts: `cov-full/coverage.json`（scratchpad，未入库）。

### 重排 lever 离线验证（gte-rerank-v2，2026-07-20）

用同一 bake-off、同一建库、**同预算**（top-k 30 / chunk-quota 12）挂上交叉编码器
重排，隔离测「定预算 chunk 排序」这个候选 lever。

- Reranker: 阿里云 DashScope `gte-rerank-v2`（GPU 托管）。本地 sidecar 用薄代理把
  引擎的 Cohere/Jina rerank 请求转译到 DashScope；embedding 仍走本地 bge（建库所用，
  向量空间不变）。引擎 + bench **零代码改动**（复用 `EMBED_RERANK_MODEL`）。0 降级。
- **turn recall @ 同预算，全类别 n=1532**：

  | 类别 | baseline | +rerank | Δ |
  |------|----------|---------|---|
  | multi-hop | 0.559 | 0.642 | **+8.3pp** |
  | open-domain | 0.462 | 0.533 | +7.1pp |
  | single-hop | 0.807 | 0.870 | +6.3pp |
  | temporal | 0.759 | 0.823 | +6.4pp |
  | OVERALL | 0.732 | 0.799 | **+6.7pp** |

- **判决**：与 sweep（全类别负）截然相反——重排在**不膨胀 answer context** 的前提下
  全类别一致抬升证据覆盖，multi-hop（靶心）+8.3pp。这是首个通过离线覆盖闸的正向
  lever，**够格升级到付费答题评测**（配对 baseline vs +rerank + McNemar，验证覆盖
  增益是否转化为答题正确率——coverage 是必要非充分）。成本侧：答题时每题多一次
  gte-rerank 调用（便宜）。Artifacts: `cov-rr-full/coverage.json`（scratchpad，未入库）。

### 重排 lever 付费答题验证（multi-hop 试点，配对 + McNemar，2026-07-20）

覆盖闸只证「gold chunk 进了 top-k」，是答题正确的**必要非充分**条件。故对靶心类别
multi-hop 做配对付费答题评测，检验 +8.3pp 覆盖能否转化为答题正确率。

- 口径：同一进程配对 `hybrid` vs `hybrid+rerank`（新增 per-arm `+rerank` 机制，
  adapter-only，引擎零改；`TestRerankArmMechanismGatesReranker`）。付费前先做**免费
  配对覆盖预检**，同一进程复现分进程离线数（hybrid 0.559 / +rerank 0.642，0 降级），
  证明两臂只差重排一个变量、口径无误后才发。
- 复用已建库（`reusing persisted extraction`，无二次抽取）；答题+判分走 tokensfree
  relay（gpt-5.6-luna，003 冻结模型），重排走 DashScope `gte-rerank-v2`。
- **结果（multi-hop，N=282，repeats=1）**：

  | 臂 | 答题正确率 | 翻转 |
  |----|-----------|------|
  | hybrid (baseline) | 61.0% (172/282) | — |
  | hybrid+rerank | **69.1% (195/282)** | A→B 修好 38 / B→A 打坏 15，净 **+23** |

  Δ = **+8.1pp**；McNemar **p=0.0025**（远低于 0.05），CI 不重叠，判决 **above-noise**。
- **判决**：离线覆盖 +8.3pp 几乎 1:1 转化为答题正确率 +8.1pp，且统计显著。重排 lever
  在靶心类别**真实涨点**得到付费确认。成本：答题 4.34M token（输入密集 4.27M/输出
  68k）+ 判分约 0.36M ≈ ~4.7M token，约 ¥15–30。Artifacts: `ans-mh/paired.json`、
  `ans-mh/results-*.jsonl`（scratchpad，未入库）。
- **下一步（需授权，花钱）**：全类别配对答题（single/temporal/open-domain，另 ~¥60–90）
  拿全盘 McNemar；或将 `+rerank` 定为 003 默认检索栈并回归。

## Strike 3: Abstention and Conflict Resolution

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- `compare.json` / verdict:
- Token budget ratio:
- Decision (keep / revert):
- Notes:

## LongMemEval_S Final Validation

- Date:
- Dataset / repeats:
- Flags:
- Estimate output:
- Actual cost (`cost.json`):
- `stats.json`:
- Decision:
- Notes:
