# Feature Specification: turn 粒度检索 + 邻窗装配 — 同预算 gold 密度提升

**Feature Branch**: `047-chunk-granularity-density`

**Created**: 2026-08-18

**Status**: Draft(specify 阶段,待维护者确认后进 plan)

**Input**: User description: "quota28 这个,只有两个 facts,这和跑原文没啥区别,当前记忆,Rag,GraphRAG,领域,在这一块还有什么结构化的元信息,既可以缩减 tok-p 又可以涨点的方案吗" + 附加指令 "接下来SDD规范,先继续找文章,必须在结构上的突破,去压缩这个top-k token消耗" + "走specs前,再次看下,docs目录踩过的坑,避免重复踩坑"。

**调研正本**: [docs/research/structured-memory-token-compression-survey.md](../../docs/research/structured-memory-token-compression-survey.md)(7 篇 alphaXiv 深读 + docs verdict 全量对账,含 5 坑清单与方案修正记录)。

## 背景与问题定位

k30 quota28 收线于 clean 89.74%(1382/1540),逐题 **46 救/42 翻**:28 个 900-char
chunk 满槽让上下文从 3614 → 6957 tok(1.9×),gold chunk 混着 4–5 个 turn 的邻居噪声,
决定性证据被稀释(040 式非单调在 quota 维度重演)。k30-chunk-quota-28-verdict 自己指出:
"下一杠杆须解决翻车 42 题(更细的 quota 点位或 chunk 排序,均未试)"。

**机制假设**:检索单元从 900-char(混 5–10 turns)细化到 turn/近-turn 粒度后,同槽位
命中更精准(gold 是 turn 粒度,D3:14 邻居噪声被剔除)、同 token 预算可容纳数倍单元
(28×900 chars → 同 token 可放 ~90-120 turn 级单元)——**gold 密度提升而非内容压缩**。
外部证据:EverMemOS 语义分段 vs 固定切块 +4.6pp 同候选同预算(arXiv:2601.02163,
[structured-content-directions](../../docs/research/structured-content-directions.md) A2 条目,
标注"唯一未测的写侧粒度结构")。

**与已证伪家族的边界**(docs 对账结论,spec 预注册):
- 非"压缩/精炼/展开"(026 −4.5pp / 045 −14.22pp / 030-T013 −3.44pp / 040 79% 体量账
  ——强 reader 栈上压 token 与涨分不可兼得):verbatim 原文一字不删,只是**检索单元
  变细**;token 下降是密度副产品,**分数目标与 token 目标解耦,分数优先**。
- 非"写侧 query-agnostic 构造"(024/025/027/028 + LazyMem 双向证伪):chunk 切分是
  确定性的(turn 边界打包,无 LLM、无表示改写),抽取 fact 全部不动。
- 非"分数族路由"(046 完美信号账封印):不改排序信号,只改单元粒度与装配。

## User Scenarios & Testing *(mandatory)*

### User Story 1 — 本地离线粒度 sweep(零 answerer/judge 成本,P1 止损门)

维护者先花**零 LLM API 成本**验证粒度假设,全部离线:复制 009-bge-chunks-store 正本
(HF `wallfacers/engram-locomo-artifacts` 的 canonical 店,fastembed bge-large sidecar
本地重嵌),对每档粒度仅重建 chunk 部分(`ingestChunks` 幂等 reconcile:fact 不动、
changed chunk 删旧写新、嵌入只嵌新 chunk)。

**sweep 设计(二维)**:
- 维度一 = 粒度档:`--chunk-target-chars` ∈ {900(现状锚), 450, 250, 150(近 turn 级)};
  每档独立 store-dir(不同值的店不可比,flag 自带告警)。
- 维度二 = 装配预算:每档 dump 宽池(`--attribution-trace --wide-dump`,retrieval-only
  零 LLM)后,离线重放 `applyChunkQuota`(sweep_quota.py 方法论,quota12 重放 0/1540
  错配的自洽校验沿用),扫 (top-k × quota) 网格——**主对照用 token-parity 口径**:同
  answer-context token 预算(如 ≈3614 的 quota12 锚与 ≈6957 的 quota28 锚两条线)下,
  turn 级(k 更大)vs 900-char(k=30)的覆盖对比。

**判据(033 口径修正,预注册)**:
- 主指标:`all-gold-turns-covered@token-parity`(非 `gold_in_pool` 的 any 命中——033
  教训:any≠回答链完整)+ `turn_recall`(均值)+ 装配 token 均值,三列同报。
- 类别分解:multi-hop/计数类的 all-coverage 单列(009 "turn@k 对 fact 级 assoc 失明"
  的前车之鉴——细粒度可能断关联链,single-hop 涨/multi-hop 塌则假设不成立)。
- 翻车题核验:quota28 的 42 翻车题逐题验证新粒度下原 gold 内容是否保留在装配集内
  (翻车机制假设的直接检验)。

**门**:存在 ≥1 个(粒度档 × 预算)组合满足 token-parity 下 all-gold-turn-coverage
≥ 900-char 锚 且 multi-hop all-coverage 不塌方(≥ 锚 −2pp)→ 进 US2;不存在 →
NO-GO 关闭,留关闭报告。gold 只进诊断不进运行时(零问题标注红线)。

**Independent Test**: 每档交付 dump + 重放覆盖表(粒度 × k × quota × {any, all,
turn_recall, token, 类别分解})+ 42 翻车题核验清单;零 answerer/judge 调用。

**Acceptance Scenarios**:

1. **Given** 009 正本店副本与 LoCoMo gold, **When** 对 4 档粒度各重建 chunk 索引并
   dump 宽池、离线重放 quota 网格, **Then** 产出"粒度 × token-parity 预算 × 覆盖指标"
   对照表,含 900-char 锚行;判定按预注册门执行并记录于 sweep 报告。
2. **Given** 42 翻车题清单(quota28 verdict), **When** 逐题核验最优粒度档的装配集,
   **Then** 每题报告原 gold chunk 对应 turns 的保留/丢失状态;保留率 <50% 则翻车机制
   假设被证伪,单独记录。
3. **Given** 本地 sweep 判方向, **When** 读数, **Then** 绝对数不与 box 对标(跨实现
   序漂移,quota28 教训:本地 net+32 → box +4);sweep 只输出方向性 GO/NO-GO。

---

### User Story 2 — 邻窗装配机制(harness 实现,默认关)+ 单变量 probe(P2)

US1 GO 后,若最优档的孤立 turn 装配存在 multi-hop 断链风险(US1 类别分解显示),
实现**邻窗装配**(LazyMem 机制,确定性零模型):检索命中 turn 级单元后,装配时向
±w 邻域扩展(w∈{0,1,2} 由 US1 数据定)、跨命中重叠去重(idx 级合并,022 血缘 span
回收同构——low-topk survey 标注"机制配对已 +10,确定性小改")。对冲 009 assoc 失明
与 045 碎片化(统一粒度+邻窗 ≠ 贪心选短项,answerer 上下文保持连贯)。

若 US1 显示某档粒度无断链风险,US2 降级为"纯粒度切换"(零新代码,只改
`--chunk-target-chars` 重建店),邻窗机制不做。

**实现位置**:全部在 harness(`cmd/locomo-bench/` chunks.go 装配层/新文件);引擎五
目录零改动(宪法 II)。邻窗合并基于 chunkTurns 的 DiaID 序(确定性,stable id tie-break)。
旗标默认关;关闭时与现行配方逐字节一致(golden 锁定)。

**probe(US1 GO 档位)**:box 单变量配对——机制臂 = 最优粒度店 + 邻窗装配(若启用),
对照臂 = 现行 900-char quota28 配方(clean 锚 89.74% 同批重跑,不引历史数)。同批、
同 judge(`--judge-mem0-aligned` + flash clean 重判口径)、3-rep。工程铁律:`--per-call-timeout
15m`(思考模型长上下文 SSE)、embed `--max-num-seqs 1`(并发非确定)、若 LME 迁移
`EMBED_TRUNCATE_PROMPT_TOKENS=-1`(杂讯 chunk 400 会 fail-closed)。

**Independent Test**: 旗标 golden 测试(关=逐字节一致)+ 3-rep clean majority 配对差
+ McNemar p + 两臂 answer-context tokens 均值同表(体量 parity 强制同报)。

**Acceptance Scenarios**:

1. **Given** 旗标关闭, **When** 运行评测, **Then** 与现行配方逐字节一致(golden 测试)。
2. **Given** 旗标开启(或纯粒度店), **When** 装配, **Then** 邻窗扩展确定性零模型调用、
   跨命中去重幂等;同输入同输出。
3. **Given** 3-rep clean 配对, **When** 检验, **Then** 机制臂配对差 ≥0 且 McNemar 不
   显著为负 → GO 进 US3;显著为负 → NO-GO 关闭并如实记录(040/045 家族铁律应验)。
4. **Given** 两臂结果, **When** 汇报, **Then** token 均值差与分数差同表;机制臂 MUST
   NOT 靠装更多 token 赢(token-parity 是设计约束,非事后辩解)。

---

### User Story 3 — 3-rep clean 正批定稿 + LME 零重调迁移(P3)

US2 GO 档位跑正批定稿(若 US2 已是 3-rep 配对则本 US 只做 LME 臂):LoCoMo 定稿参数
零重调迁移 LongMemEval-S 500(LME 建店注意:turn 级重切复用同机制,embedding 是
GPU-bound ~11-12h 级,box 数据盘执行)。LME 的超长 turn 已有 lossless 拆分逻辑
(2026-07-28 plan 交付),粒度变细不引入新截断。

**Independent Test**: LME 配对(同参数零改动有 commit 证据)+ clean majority 判定。

**Acceptance Scenarios**:

1. **Given** LoCoMo 定稿参数, **When** LME 同配方运行, **Then** 不显著低于 LME 现行
   锚(k30 unified clean 93.40% 口径);MUST NOT 因 LME 表现回改参数(回改即 in-sample
   特调)。
2. **Given** 正批结束, **When** 审计, **Then** 每题装配单元清单(token、邻窗扩展事件、
   去重合并记录)可复算。

---

### Edge Cases

- 短 turn 合并:target-chars 下限受 buildSessionChunks 语义约束(累积到 target 才
  flush,~150 chars 已接近一 turn 一 chunk);过细档(如 <100)与 turn 级无差异,sweep
  不含。
- 超长 turn:已有 lossless 拆分,粒度变细不改变其行为;LME 杂讯 chunk(SVG/浮点表/
  外语,0.031%)在 turn 级依旧存在,迁移臂必须 truncate 防 fail-closed。
- 040 体量账反向风险:turn 级 k30 的 token 仅 ~1/5,体量敏感题可能因上下文变薄回落
  ——probe 必须 3-rep 非 1-rep(029 教训:1-rep probe 方向判断被 3-rep 推翻过);
  token-parity 臂(同 token 更大 k)是主对照而非小 k 臂。
- quota 语义随粒度漂移:quota28 在 turn 级等于"28 turns",网格扫描必须重标定
  (quota/k 联动,不做单点迁移)。
- store 重建的 chunk 名冲突:reconcile 按 name 索引,同店改粒度会删旧写新——但跨值
  不可比,sweep 每档独立 store-dir,不复用。
- 嵌入成本:LoCoMo 10 conv 每 fact 不动、仅 chunk 重嵌(本地 fastembed,CPU 可过夜/
  GPU 小时级);LME 500 是 GPU-bound 大活,只在 US3 执行。
- FTS/BM25 行为变化:更短 chunk 的 BM25 长度归一化改变 keyword 信号分布——sweep 的
  三信号融合行为随之变,这是机制的一部分(不视为 bug),对照表如实呈现。
- 并行 worktree:本 feature 全部改动落 harness 新文件 + flag 注册区;无并行 feature
  时主工作区执行,feature.json 指针独占。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 全部机制 MUST 在 harness 层实现(`cmd/locomo-bench/`);引擎五目录
  (`memory/ embedding/ provider/ store/ internal/`)MUST 零改动(宪法 II)。粒度切换
  复用现有 `--chunk-target-chars` flag;邻窗装配若实现,MUST 新增旗标且默认关,关闭时
  与现行配方逐字节一致(golden 测试锁定)。
- **FR-002**: US1 离线门 MUST 零 answerer/judge 调用;gold 标签 MUST 只用于离线诊断,
  MUST NOT 进入任何运行时决策(零问题标注红线)。
- **FR-003**: US1 判据 MUST 在门计算前冻结:all-gold-turns-covered 定义(gold turns
  全集 = parsedGoldTurns(qa.Evidence) 的 DiaID 并集)、token-parity 预算线(3614/
  6957 两档)、multi-hop 塌方阈值(−2pp)、42 翻车题核验口径。审计清单随门报告。
- **FR-004**: US2 probe MUST 同批配对、同 judge(`--judge-mem0-aligned`)、同批顺序
  执行、repeats ≥3;对照臂同批重跑不引历史锚。显著为负 MUST NO-GO。
- **FR-005**: 两臂 answer-context tokens 均值 MUST 与分数差同表呈现(体量 parity);
  机制臂 MUST NOT 靠装更多 token 赢。
- **FR-006**: 邻窗装配(若实现)MUST 确定性、零模型调用、基于 chunkTurns DiaID 序;
  跨命中去重幂等;同输入同输出(stable id tie-break)。
- **FR-007**: LME 迁移 MUST 零重调(参数与 LoCoMo 定稿逐字节同);MUST NOT 因 LME
  表现回改。
- **FR-008**: 每题审计 MUST 可复算:装配单元清单、token、邻窗扩展/去重事件、(probe)
  逐题翻转清单,入 result-matrix。
- **FR-009**: box 执行 MUST 遵守:run-dir 在 /root/autodl-tmp、`--max-num-seqs 1`
  (embed)、`--per-call-timeout 15m`、跑完即关机(空闲必停)。
- **FR-010**: MUST NOT 引入数据集/问题级特化(维护者红线);MUST NOT 碰写侧抽取与
  答题侧契约(单变量归因:只动 chunk 粒度与装配)。
- **FR-011**: eval-config 改动与机制改动分开 commit(宪法 IV 归因纪律)。

### Key Entities

- **GranularitySweepReport**: US1 产物——粒度档 × token-parity 预算 × {any, all,
  turn_recall, token 均值, 类别分解} 对照表 + 42 翻车题核验 + 预注册门判定。
- **NeighborAssembly**(条件实现): 装配期 ±w 邻域扩展 + 跨命中 idx 级去重合并;
  输入 chunkTurns 序,输出连贯窗口集;确定性。
- **TokenParityBudget**: 装配预算口径——按锚配方 answer-context token 均值(3614/
  6957)对齐的(k × quota)组合集,粒度比较的公平基准。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 离线粒度 sweep 交付(零 LLM 成本):4 档粒度 × 2 条 token-parity 预算线
  的覆盖对照表 + 类别分解 + 42 翻车题核验 + GO/NO-GO 判定;任一门不过即关闭并留
  关闭报告。
- **SC-002**: e2e(若 US1 GO):3-rep clean 配对差 ≥0 且 McNemar 不显著为负;token
  均值差不掩盖分数差(同表);multi-hop 子集不显著为负。
- **SC-003**: LME 零重调迁移(若 e2e GO):同参数不显著低于 LME 现行锚。
- **SC-004**: 全部结果可复算:同批同 judge、逐题翻转清单、装配审计、result-matrix
  登记;verdict 文档落 tracked docs/(换环境不丢)。

## Assumptions

- **期望管理**:主风险 = 040 体量账(79% 增量靠体量)——turn 级天然缩体量,可能与
  体量收益反向;token-parity 主对照正是为此设计(同 token 下比较,隔离"单元精度"
  与"体量"两个变量)。次风险 = 009 turn@k assoc 失明与 045 碎片化——邻窗装配是
  对冲,但 probe 仍可能验证家族铁律,NO-GO 即诚实结论。EverMemOS +4.6pp 是弱 reader
  栈口径,不作 engram 可达锚,只作机制存在性证据。
- 文献定位:survey 修正后的结论——强 reader 栈上"缩 token 且涨点"的文献结论不可
  直接迁移;本 feature 的 honest 表述是"**同 token 预算下检索单元更精准**",token
  下降是副产品,分数与 token 目标解耦、分数优先(维护者哲学:预算下提质)。
- 基建假设:009 正本店可从 HF 拉取(已验证);fastembed bge-large sidecar 本地跑通
  (已验证);sweep_quota.py/wide-dump 方法论沿用(自洽校验 0/1540 错配先例)。
- probe/正批成本:box 重开一次(租卡 ~20min 起);LoCoMo 3-rep 配对 ≈3-4h(参照
  t15 干净重跑预估);LME 建店是大活(GPU-bound ~11-12h)只在 US3 且 LoCoMo GO 后。
- 若维护者后续要 caption late-binding(033 的 10/16 钥匙)或跨 hit 合并去重的独立
  变体:另开 feature,本 spec 不含(单变量归因纪律)。
