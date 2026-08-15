# Feature Specification: 确定性次模证据装填

**Feature Branch**: `045-submodular-packing`

**Created**: 2026-08-16

**Status**: Draft

**Input**: User description: "确定性次模证据装填:检索池加宽(top-150 池,hybrid 检索引擎零改动,harness 层实现)+ 预算化贪心次模选择(四项目标:relevance 模块项=现有 RRF 分归一 + query set-cover + facility-location 代表性 + concave 多样性,全用已存 embedding 与现有分数,零新增模型调用;cost-scaled 贪心 + singleton fallback),token 预算锚定现行 k30 chunk-quota-12 装配实际体量(体量配对)。默认关旗标。止损门前置:离线装填保真门(answer-in-context 覆盖逼近 top-150,零模型零 box);e2e 先 1-rep 同批配对 probe,GO 才 3-rep clean 正批 + LME 零重调迁移。组合批:同一次 box 开机并入 042 counterfactual 信号重验(测量修复自包含,不依赖将被 044 清理删除的文件)。文献依据 docs/research/low-topk-recall-context-survey-2026-08-15.md(What Survives Into Context arXiv:2607.00725);040 verdict:30→150 差距 79% 是上下文体量、21% 是召回。"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 离线装填保真门(止损,P1)

维护者先花**零 LLM/judge 成本**验证机制的理论前提:从 top-150 检索池内、在 k30 等效 token 预算下做次模装填,装填后上下文的 **answer-in-context**(gold 答案作为连续 span 出现在最终上下文)能否逼近 top-150 全量装配。门不过(装填 AIC < top-150 装配的 95%,或 token 超预算)→ 整个 feature NO-GO 关闭。执行位置:本地已配 embedding sidecar 则完全本地;否则作为组合批**同一开机的第一段**在 box 跑(仅 embedding sidecar,不动 vllm),NO-GO 即刻关机。

**Why this priority**: P2 期望本就压低(文献 scope 警告:reader>7B 时装填优势消失)。"k30 体量装下 k150 信息密度"这一前提有一半风险(信息论层面)可以不花 LLM 成本测掉——测不过就不必为剩下的一半(reader 吸收)花 probe+正批的 box 钱(NO-GO 最坏成本 ≈ 1 小时机时)。这也是 survey 的直接建议:answer-in-context 是比 gold_in_pool 更强的离线诊断(ΔR²=+0.17,经干预实验证实因果)。

**Independent Test**: 已存 store + 已存 embedding + dataset gold 答案 + hybrid 检索的 query embedding sidecar(唯一模型侧依赖),零 LLM/answerer/judge 调用,产出三口径 AIC 对照表(现行 k30 装配 / 次模装填 / top-150 全量)+ go/no-go 判定,不含任何 e2e 机制代码。

**Acceptance Scenarios**:

1. **Given** 已存 032-store 与 LoCoMo gold 答案, **When** 在 k30 等效 token 预算下从 top-150 池装填并计算三口径 AIC, **Then** 装填 AIC ≥ top-150 装配的 95% 且装填 token ≤ 预算 → GO 进入 Story 2;任一不满足 → NO-GO 并记录关闭报告。
2. **Given** gold span 匹配受大小写/空白/别名影响, **When** 计算 AIC, **Then** 匹配规范化规则在门计算前冻结,匹配审计随门报告(池内任何条目都匹配不到 gold 的题目单列,不静默剔除)。
3. **Given** 个别题预算内放不下任何条目, **When** 装填, **Then** singleton fallback 生效(至少保留 relevance 最高一条),该情况单列上报。

---

### User Story 2 - 次模装填机制(默认关)+ 1-rep 同批配对 probe(P2)

harness 实现装填机制:默认配方**字节不动**;旗标开启时检索池加宽至 150、装填层在锚定预算内做确定性贪心次模选择,替代现行 top-k 截断装配。box 上先跑 **1-rep 同批配对 probe**(Step A 协议:同 store、同 judge、同批顺序执行),配对差显著非负才 GO。

**Why this priority**: 单变量(只换装配层、体量配对)配对 probe 把 box 成本压到最低;GO 门挡住"离线好看、e2e 显著负"的翻车(043 Step A 先例:−3.44pp, p=1.4e-04)。

**Independent Test**: 旗标 golden 测试(关=现行配方逐字节一致)+ 1-rep 配对差与 McNemar p 值单独交付。

**Acceptance Scenarios**:

1. **Given** 旗标关闭, **When** 运行评测, **Then** 与现行 k30 unified 配方逐字节一致(默认路径零扰动)。
2. **Given** 旗标开启, **When** 装填执行, **Then** 四项目标齐备(relevance 模块项主导 + query set-cover + facility-location 代表性 + concave 多样性),零新增模型调用,同输入同输出(确定性,tie-break 用 stable id)。
3. **Given** 1-rep probe 两臂同批执行完毕, **When** 配对检验, **Then** 机制臂配对差 ≥0 且 McNemar 不显著为负 → GO 进入 Story 3;显著为负 → NO-GO 关闭。
4. **Given** 装填层异常(池空/条目 embedding 缺失), **When** 处理该题, **Then** 逐条降级(该条多样性贡献记 0,不剔除、不阻塞),池空则整题回退现行装配,不报错。
5. **Given** probe 两臂, **When** 汇报, **Then** answer-context tokens 均值(体量 parity)随配对差一并报告——机制臂不得靠"装更多 token"赢。

---

### User Story 3 - LoCoMo 3-rep clean 正批(P3)

probe GO 后按 008 铁律正批:同批配对、repeats≥3、store 复用、clean 判题口径,机制臂 vs 对照臂(现行 k30 unified 配方),产出可入 result-matrix 的行(含 p 值、context parity、逐题翻转清单)。

**Why this priority**: 1-rep probe 只判方向;above-noise 的净增结论只能来自 3-rep clean majority 配对。

**Independent Test**: 单独交付 3-rep 配对结果行;对照臂同批重跑(不引用历史锚作对照)。

**Acceptance Scenarios**:

1. **Given** probe GO, **When** 3-rep 正批执行, **Then** 机制臂 clean majority 配对不显著低于对照臂,且达到 ≥90.0% 为 SC-003 达成;净差与 p 值入 result-matrix。
2. **Given** 正批结束, **When** 审计, **Then** 每题装填决策(候选池大小、选中集、预算消耗、被弃头部条目及原因)可复算。

---

### User Story 4 - LME 零重调迁移门(P4)

用 LoCoMo 上定稿的全部参数(池宽、预算锚、四项权重、fallback 规则)**不做任何重调**,直接在 LongMemEval-S(k30, unified, clean, 3-rep)配对验证:非回退 90.2% 锚即通过。

**Why this priority**: 维护者红线——机制须换场景零重调成立;不过迁移门不得声称可移植、不得设默认。

**Independent Test**: 单独跑 LME 配对批,交付迁移门判定。

**Acceptance Scenarios**:

1. **Given** LoCoMo 定稿参数, **When** LME 同配方运行机制臂, **Then** 机制臂 clean 3-rep 不显著低于 90.2% 锚(非回退)。
2. **Given** 迁移通过, **When** 汇总, **Then** 产出双数据集 result-matrix 行(含 p 值、token parity、翻转清单)。

---

### 组合批 ride-along(非本 feature 的 US,同一次 box 开机执行)

**042 counterfactual 信号重验**:043 verdict 实锤 042 信号采集走过的 `utilityMapFinalSignal` content-后缀前置在当前 vllm 栈结构性失败(304/304 `content_not_generated_suffix`),042 的"信号 5/5958 NO-GO"归因未经有效测量。重验 = 2-conv slice 用**自包含的修复测量**(logprob 通道 temperature=0 + 鲁棒 final-span 映射,公式复刻 1eb9cdd,但不 import 任何将被 044-default-off-cleanup 删除的 042/043 文件)重采信号,交付"有效采集率 + 判别力(AUC)"判定。**只补证据,不自动翻案 counterfactual 方向**——翻案与否是维护者决策。

### Edge Cases

- token 预算恰好等于条目长度(cost-scaled 贪心语义)→ 允许放入并停止;规则冻结,不允许超预算。
- 条目无 embedding(向量缺失)→ 多样性/代表性项对该条计 0 贡献,不剔除、不阻塞(逐条降级,宪法 V)。
- 贪心并列(tie)→ stable id 升序 tie-break,确定性可复算。
- 池不足 150(小对话)→ 就用全池,不补齐、不报错。
- AIC 匹配不到 gold(数据集别名差异)→ 入分母,审计报告单列;不悄悄改规范化规则迁就分数。
- 装填排挤头部高相关条目 → relevance 模块项权重 MUST 为最大单一项,防止退化为纯多样性选择(survey MMR 反例:MMR 单独用显著变差)。
- 与 022 血缘 span / trace 中介的组合 → 本 feature 不组合任何其他装配机制(单变量归因);组合实验留待独立批。
- 044-default-off-cleanup 并行 → 本 feature 只新增专属文件(submodular_packing*.go 等)+ main.go 新旗标区;不修改任何 044 删除目标文件的内容(新增 flag 注册在 main.go 的注册区,与删除的 flag 不同段,合并冲突可解)。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 装填选择器 MUST 确定性、纯客户端、零新增模型调用:relevance 用现有 RRF 融合分,coverage/diversity 用已存 embedding;同输入必同输出。
- **FR-002**: 目标函数 MUST 四项齐备(relevance 模块项 + query set-cover + facility-location 代表性 + concave 多样性)且 relevance 为最大单一权重项;MUST NOT 退化为纯 top-k 截断或纯 MMR。
- **FR-003**: token 预算 MUST 锚定同批对照臂(现行 k30 chunk-quota-12 装配)的实际体量(测量锚定,不用硬编码常数);两臂 answer-context tokens 均值 MUST 随每份 e2e 结果报告(体量 parity)。
- **FR-004**: 机制 MUST 复用现行装配已存在的宽检索池(harness 现状 ≥300 条;如确需加宽,只调现有引擎检索接口的 k 参数,在 harness 层实现),引擎五目录(memory/ embedding/ provider/ store/ internal/)MUST 零改动(宪法 II);若需引擎新入口,显式提出契约增量,不得绕过。
- **FR-005**: 机制 MUST 以默认关闭的旗标提供;旗标关闭时评测路径 MUST 与现行 k30 unified 配方逐字节一致(golden 测试锁定)。
- **FR-006**: 离线保真门(US1)MUST 零 LLM/answerer/judge 调用;唯一模型侧依赖是 hybrid 检索的 query embedding(与锚配方同一 sidecar)。本地已配 embedding sidecar 时 MUST 可完全本地执行;未配时 US1 作为组合批**同一开机的第一段**在 box 执行(仅 embedding sidecar,不动 vllm),NO-GO 即刻关机。门判据 = 装填 AIC ≥ 同池 top-150 装配的 95% 且装填 token ≤ 预算;不过即 NO-GO 关闭整个 feature。
- **FR-007**: answer-in-context 的匹配规范化规则(大小写/空白/分词归一)MUST 在门计算前冻结;gold 匹配审计(含池内全不可匹配题目清单)MUST 随门报告;AIC 是止损必要条件与诊断指标,MUST NOT 单独作为 e2e 出货依据(008 教训:coverage 不可单独作为出货依据)。
- **FR-008**: 1-rep probe(US2)GO 门 = 机制臂配对差 ≥0 且 McNemar 不显著为负;显著为负 MUST NO-GO 关闭。probe 协议 MUST 同批、同 store(032-store 复用)、同 judge、同批顺序执行。
- **FR-009**: 3-rep 正批(US3)MUST 遵守 008 铁律:同批配对、repeats≥3、store 复用、clean 判题口径;结果行 MUST 含 p 值、context parity、逐题翻转清单,入 result-matrix。
- **FR-010**: 机制参数(池宽、预算锚、四项权重)MUST 只在 LoCoMo 上定稿;LME(US4)MUST 零重调,MUST NOT 因 LME 表现回改参数(回改即 in-sample 特调,feature 失败)。
- **FR-011**: 系统 MUST 记录每题装填审计(候选池大小、选中条目集与顺序、每步预算消耗、被弃条目及弃因),供复算与失败归因。
- **FR-012**: 机制 MUST NOT 依赖任何托管 reranker/recall 模型(宪法死亡规则);MUST NOT 引入数据集/问题级标注或特化(维护者红线);MUST NOT 碰写侧构造(全部改动在查询时装配层)。
- **FR-013**: 组合批 ride-along(042 信号重验)MUST 自包含实现(不 import counterfactual_utility*.go / confidence_deepen*.go 或其他 044-default-off-cleanup 删除目标);重验结论 MUST 只陈述测量事实(有效采集率、AUC),翻案权留维护者。
- **FR-014**: 新增模型调用路径(如有)MUST 用 worker pool 遵守 --concurrency(工程铁律:禁止顺序循环);US1 离线门不涉及模型调用,不受此条约束。

### Key Entities

- **EvidencePool**: 单题的候选证据集(top-150 检索结果),条目带 RRF 融合分、已存 embedding、token 长度、stable id。
- **PackingObjective**: 四项目标组合函数(relevance 模块项 + query set-cover + facility-location 代表性 + concave 多样性),cost-scaled 贪心 + singleton fallback;权重在 LoCoMo 定稿后冻结。
- **PackedContext**: 装填产物——选中条目集(有序)、token 预算消耗、逐条弃因;旗标关闭时不存在此实体(走现行装配)。
- **AnswerInContext**: 离线诊断指标——gold 答案是否作为连续 span 出现在最终上下文;带冻结的匹配规范化规则与审计清单;定位为诊断与止损门,非出货依据。
- **ReverifyReport**(ride-along): 042 信号重验产物——有效采集率、双通道一致性、信号判别力(AUC);不含翻案结论。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 离线保真门交付:装填 AIC ≥ top-150 装配的 95% 且 token ≤ k30 等效预算(GO);否则 feature 关闭并留下关闭报告(零模型成本完成)。
- **SC-002**: 1-rep 同批配对 probe:机制臂配对差 ≥0 且 McNemar 不显著为负(GO);显著为负则关闭。
- **SC-003**: LoCoMo(1,540 题)k30 unified clean 3-rep majority 机制臂达到 **≥90.0%**(相对 87.9% 锚净增 ≥2.1pp,above-noise)——期望压低(条件 iv 警告),达不到即诚实关闭,不做无依据调参续命。
- **SC-004**: LME 零重调迁移:同参数机制臂 clean 3-rep 不显著低于 90.2% 锚。
- **SC-005**: 体量配对证明:机制臂 answer-context tokens 均值不显著高于对照臂(收益来自信息密度,非体量)。
- **SC-006**: 全部结果可复算:配对臂同批同 judge、context parity 通过、逐题翻转清单与装填审计入库 result-matrix。

## Assumptions

- **期望管理(写进 spec 的诚实定位)**:文献 scope map 指出装填赢需 reader 弱到证据密度是瓶颈(3B 赢、7B 被吸收、14B 反转);我们的 answerer 是强模型,条件 iv 大概率不满足——probe/正批失败属预期内结果,关闭即诚实结论,不是意外。
- 040 归因(79% 上下文体量 / 21% 召回)在 unified 契约下仍大体成立;装填打的是 79% 份额中"信息密度"可置换的部分。
- 已存 032-store(LoCoMo + LME)与已存 embedding 可直接复用;US1 全量 1540 题离线可跑(纯本地计算),若耗时不可接受先以 2-conv 起步并在报告中声明覆盖面。
- gold 答案只用于离线诊断(AIC)与判题(既有口径),MUST NOT 进入运行时装配决策(零问题标注红线)。
- **组合批**:本 feature 全部 box 实验(1-rep probe、GO 后 3-rep 正批、LME 迁移、042 重验)合并为一次开机,跑完即关(省钱必停);probe 与正批可在同一次开机内顺序衔接(先 probe 判 GO 再跑正批)。
- **并行协调**:044-default-off-cleanup 在另一 worktree 并行删除已证伪机制的默认关旗标与专属文件;本 feature 不修改其删除目标文件,新增代码全部落在专属新文件 + main.go 旗标注册区;其 spec 前提"master 无 043 代码"已被 e6625d8 合并改变,该窗口需自行同步(已向维护者标记)。
- eval-config 改动与算法改动分开 commit(宪法 IV 归因纪律);旗标默认值与算法实现分批提交。
- 本 feature 只动评测 harness(cmd/locomo-bench);probe GO 且正批达标后,是否把装填下沉为引擎公共能力是独立的未来契约增量,不在本 feature 范围。
