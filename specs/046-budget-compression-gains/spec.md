# Feature Specification: 编译瓶颈双臂 — 自适应证据预算压缩与排序涨点

**Feature Branch**: `046-budget-compression-gains`

**Created**: 2026-08-17

**Status**: Closed (2026-08-17 — pre-implementation NO-GO:两条臂经 docs 对账被 040/008+037 既有 verdict 实质覆盖,不进入 plan。对账正本: [closure.md](closure.md))

**Input**: User description: "继续找论文,arxiv mcp,045 no go了,继续看下,压缩top-k预算,和涨点两条路,毕竟locomo数据集才87pp"

## User Scenarios & Testing *(mandatory)*

### User Story 1 - 零模型成本离线编译诊断门(两臂共用,止损,P1)

维护者先花**零 answerer/judge 成本**验证两条路各自的机制前提(全部离线:已存 032-store + 已存检索分数/宽池结果 + dataset gold 仅作诊断标签):

**(a) 压缩臂前提**:从 top-150 池按确定性信号做查询自适应截断,gold-in-context 存活曲线如何?是否存在"gold 存活不降的显著压缩点"?信号候选(由本诊断门横向比选):RRF 融合分排序曲线的 knee/尾稳定性变体(TAA-k:几何 knee + 局部 EVT 拟合优度,arXiv:2606.11907,training-free 纯客户端 4ms/题);rerank 分数阈值变体(SmartSearch score-adaptive:τ = α·max score,top-K 预选后剪枝,arXiv:2603.15599,表 5:LoCoMo α=0.03 省 56% token 仅丢 1.3% recall)。诊断 MUST 对 040 的题级对账(56 救/31 害)逐题核验:31 害题与 56 救题的分数曲线是否可分——不可分则压缩臂 NO-GO。

**(b) 涨点臂前提**:本地离线开源 cross-encoder(一次性下载、本地推理,如 435M 级 mxbai-rerank-large-v1;具体选型 plan 定)对 1540 题宽池重打分,gold rank 分布 vs 现行 RRF(009 锚:中位 71-90)改善多少?recovery@30 提升幅度是否达到值得 probe 的量级?

门不过 → 对应臂 NO-GO 关闭,留下关闭报告。执行位置:本地(embedding sidecar + rerank 模型均本地可跑)或组合批同开机第一段。

**Why this priority**: 040 归因(30→150 增量 79% 是上下文体量)+ 045 NO-GO(−14.22pp)已证明"小体量装 150 信息"失败;008 铁律(coverage 不转化)三度应验。两条臂的前提都有一半风险可以不花 answerer/judge 成本测掉。SmartSearch 的核心洞察直接支持此门:LoCoMo 检索 recall 98.6% 但截断后 gold 仅 22.5% 存活、mean gold rank 195→8 靠 rerank——瓶颈在编译(排序→截断)不在检索,而这个诊断完全可离线复算。

**Independent Test**: 已存 store + 已存检索分数 + (b) 需本地 rerank 模型推理;零 answerer/judge 调用,产出两臂判定 + gold-survival 曲线 + 009 对账 rank 分布 + 040 题级风险清单,不含任何 e2e 机制代码。

**Acceptance Scenarios**:

1. **Given** 已存 032-store 与 LoCoMo gold, **When** 在离线仿真中对 top-150 池做各候选信号的自适应截断并计算 gold 存活率与截断后 token 均值, **Then** 产出"信号 × 压缩比 × gold 存活"对照表;存在至少一个信号点满足 gold 存活 ≥ 全量装配的 97% 且 token 均值 ≤ k150 装配的 70% → 压缩臂 GO 进入 US2;不存在 → NO-GO 关闭压缩臂。
2. **Given** 040 的 56 救/31 害题级结论, **When** 逐题核验两群的截断信号可分性, **Then** 31 害题被截断的题数与识别率单独上报;31 害题不可识别(<80% 保留)则该信号不得作为压缩信号。
3. **Given** 本地开源 rerank 模型对宽池重打分, **When** 与现行 RRF 排序对比 gold rank 分布, **Then** recovery@30 与中位 rank 改善量上报;recovery@30 改善 <5pp → 涨点臂 NO-GO(参照 008 US1 曾 +15.457pp coverage 未转化的量级门槛)。
4. **Given** 诊断涉及 gold 标签, **When** 运行时机制设计, **Then** gold 只进诊断不进任何运行时决策(零问题标注红线)。

---

### User Story 2 - 压缩臂:查询自适应证据预算截断(默认关)+ 1-rep probe(P2)

harness 实现装配层截断机制:默认配方字节不动;旗标开启时每题按 US1 定稿的信号自适应决定装配条数(替代固定 k 截断),下限保护(保底条数 + singleton fallback)。box 上 1-rep 同批配对 probe(Step A 协议:同 store、同 judge、同批顺序执行),对照臂 = 现行配方(k30 生产锚与 k150 思考栈锚中取 US1 仿真最接近的那个,plan 定稿)。

**Why this priority**: top-k150 是 90pp 的 2.4× 上下文税(deepseek-api-cost 记忆:150 配方 3-rep 成本 ¥153-251 vs k30 ¥108);压缩臂目标是让 90pp 栈以显著更小的均值 token 出货——单变量(只换截断层)配对 probe 把 box 成本压最低。

**Independent Test**: 旗标 golden 测试(关=现行配方逐字节一致)+ 1-rep 配对差与 McNemar p 值 + 两臂 answer-context tokens 均值单独交付。

**Acceptance Scenarios**:

1. **Given** 旗标关闭, **When** 运行评测, **Then** 与现行配方逐字节一致(golden 锁定)。
2. **Given** 旗标开启, **When** 装配, **Then** 截断信号确定性、零新增模型调用(纯 RRF 曲线变体)或仅用已装本地 rerank(α 阈值变体,与 US3 共享依赖则显式声明);同输入同输出(tie-break 用 stable id)。
3. **Given** 1-rep probe 两臂同批执行, **When** 配对检验, **Then** 机制臂配对差 ≥0 且 McNemar 不显著为负 → GO;显著为负 → NO-GO 关闭。
4. **Given** 截断信号不点火或池浅, **When** 处理该题, **Then** 回退保底装配(不低于现行 k),逐题降级不报错(宪法 V)。
5. **Given** 两臂结果, **When** 汇报, **Then** answer-context tokens 均值随配对差一并报告——机制臂 MUST NOT 靠装更多 token 赢,压缩收益(token 均值降幅)与分数差同表呈现。

---

### User Story 3 - 涨点臂:本地开源 rerank 编译层(默认关,opt-in)+ 1-rep probe(P2)

harness 实现 opt-in 的本地离线 cross-encoder 编译层:对宽池重排(或与 RRF 融合,融合权重 US1 数据定稿),以 thinking 栈(90pp 配方)为对照跑 1-rep 同批配对 probe。报告 MUST 包含与既往三次 rerank 证伪(008 US1 / 037 / 045)的差异对账:reranker 强度(表 9 类 recovery 数据)、基线排序质量、栈时代错题画像(当前 open-domain 68.8% 短板 = 召回主导)。

**Why this priority**: SmartSearch 在同数据集(LoCoMo-10, 1540 题)上显示 reranker 强度是 +7.2pp 的最大单一因子(MiniLM 84.7 → mxbai-large+ColBERT 91.9,自家口径),且 open-ended/temporal 弱项归因是 answerer 推理非检索——与 engram 错题画像互补。死亡规则合规:本地开源模型离线推理,非 paid cloud,非默认栈,opt-in。

**Independent Test**: 1-rep 配对差 + McNemar p + open-domain/temporal 子集分解 + 证伪对账表,单独交付。

**Acceptance Scenarios**:

1. **Given** 旗标关闭, **When** 运行评测, **Then** 与现行配方逐字节一致。
2. **Given** 旗标开启, **When** rerank 编译层执行, **Then** 模型本地推理(无网络调用),推理失败/超时逐题降级回 RRF 排序,不阻塞(宪法 V)。
3. **Given** 1-rep probe, **When** 配对检验, **Then** 总分配对差 ≥0 且 McNemar 不显著为负、open-domain 子集差 >0 → GO;显著为负 → NO-GO 关闭。
4. **Given** probe 结果, **When** 汇报, **Then** 与 008 US1(coverage +15.457pp / e2e −0.06pp)和 037(自训 reranker e2e −1.1pp)的对账归因随报告交付——解释本次差异来源或承认同一铁律再次应验。

---

### User Story 4 - 3-rep clean 正批 + LME 零重调迁移(P3)

GO 臂按 008 铁律正批:同批配对、repeats≥3、store 复用、clean 判题口径(flash 同批重判),对照臂同批重跑(不引用历史锚)。LME 用 LoCoMo 定稿的全部参数零重调配对验证(SmartSearch 主张:score-adaptive 截断使单一配置跨数据集成立——这正是零重调门的文献依据)。

**Why this priority**: 1-rep probe 只判方向;above-noise 净增结论与"压缩后分数不掉"的置信结论只能来自 3-rep clean majority 配对。

**Independent Test**: 3-rep 配对结果行(含 p 值、token parity、翻转清单)入 result-matrix;LME 迁移门判定单独交付。

**Acceptance Scenarios**:

1. **Given** probe GO, **When** 3-rep 正批执行, **Then** 机制臂 clean majority 配对不显著低于对照臂;压缩臂另需 token 均值降幅 ≥30% 相对 k150 装配(US1 门的方向目标,正批实测)。
2. **Given** LoCoMo 定稿参数, **When** LME 同配方运行, **Then** 不显著低于 LME 现行锚(k30 unified clean);MUST NOT 因 LME 表现回改参数(回改即 in-sample 特调)。
3. **Given** 正批结束, **When** 审计, **Then** 每题截断/rerank 决策(信号值、选中集、token 消耗、降级事件)可复算。

---

### 组合批 ride-along(同一次 box 开机顺序执行)

US1(b) 本地不可跑时 → 同开机第一段仅 rerank 模型推理(不动 answerer vllm),NO-GO 即刻关机;US2/US3 probe 同批顺序执行;GO 臂接 3-rep 正批 + LME;跑完即关(省钱必停)。rerank 模型与权重放 box 数据盘(/root/autodl-tmp),不占系统盘。

### Edge Cases

- 自适应 k 低于保底下限(信号过激)→ 保底条数托底,该题单列审计。
- RRF 融合分跨题尺度不可比 → 截断信号 MUST 用排序曲线形状/相对量(归一化、α·max 比值),不用绝对分数阈值。
- 池浅(<保底条数)→ 用全池,不补齐不报错。
- rerank 模型加载失败/单题超时 → 逐题降级回 RRF,降级率随结果报告。
- rerank 与截断叠加(涨点臂排序 + 压缩臂截断)→ probe 阶段单变量归因,组合实验留独立批;若压缩臂信号依赖 rerank 分数而涨点臂 NO-GO → 压缩臂 MUST 回退纯 RRF 信号独立过门,否则连带 NO-GO。
- 31 害题(040)被截断 → US1 门已前置识别;运行期每题截断审计可复算风险清单。
- SmartSearch 91.9%/93.5% 数字 → 不引用为 engram 可达锚(gpt answerer + 自家 judge 口径,其自证跨协议 full-context 摆动 14pp;judge 口径是头号不可比源——memory-paper-audit 已核);只借机制与诊断方法。
- 与 032 思考契约、038 unified 契约的叠加 → 本 feature 不改答题侧契约,单变量归因。
- 并行 worktree 冲突 → 新增代码全部落专属新文件 + main.go 旗标注册区,不改任何已证伪机制的文件。

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: 压缩臂截断信号 MUST 确定性、纯客户端;纯 RRF 变体 MUST 零新增模型调用;α 阈值变体 MUST 仅依赖本地离线开源 rerank 模型。MUST NOT 依赖任何托管/付费 rerank/recall API(宪法死亡规则)。
- **FR-002**: 涨点臂 rerank 模型 MUST 本地离线推理(一次性下载、本地部署),opt-in 默认关;旗标关闭时评测路径与现行配方逐字节一致(golden 测试锁定)。
- **FR-003**: 机制 MUST 在 harness 层实现(cmd/locomo-bench),引擎五目录(memory/ embedding/ provider/ store/ internal/)MUST 零改动(宪法 II);复用现有宽检索池与引擎既有 Reranker 接口形态;若需引擎新入口,显式提出契约增量,不得绕过。
- **FR-004**: US1 离线诊断门 MUST 零 answerer/judge 调用;gold 标签 MUST 只用于离线诊断与既有判题口径,MUST NOT 进入运行时装配/截断/rerank 决策。
- **FR-005**: 诊断门判据 MUST 冻结于门计算前(gold 存活匹配规范化、recovery@k 定义、31 害题识别率口径);审计清单随门报告。
- **FR-006**: 1-rep probe(US2/US3)GO 门 = 机制臂配对差 ≥0 且 McNemar 不显著为负;显著为负 MUST NO-GO 关闭对应臂。probe 协议 MUST 同批、同 store(032-store 复用)、同 judge、同批顺序执行。
- **FR-007**: 3-rep 正批(US4)MUST 遵守 008 铁律:同批配对、repeats≥3、store 复用、clean 判题口径;结果行 MUST 含 p 值、token parity、逐题翻转清单,入 result-matrix。
- **FR-008**: 两臂 answer-context tokens 均值 MUST 随每份 e2e 结果报告(体量 parity);压缩臂收益与分数差 MUST 同表呈现,不得只报其一。
- **FR-009**: 涨点臂报告 MUST 包含与 008 US1 / 037 / 045 既往证伪的差异对账(reranker 强度、基线排序、栈时代错题画像),失败时如实记录铁律应验。
- **FR-010**: 机制参数(信号选型、α、保底条数、融合权重)MUST 只在 LoCoMo 上经 US1+probe 定稿;LME MUST 零重调,MUST NOT 因 LME 表现回改参数。
- **FR-011**: 系统 MUST 记录每题审计(信号值、自适应 k/选中集、token 消耗、rerank 分数、降级事件),供复算与失败归因。
- **FR-012**: 机制 MUST NOT 引入数据集/问题级标注或特化(维护者红线);MUST NOT 碰写侧构造与答题侧契约(改动全部在查询时装配层)。
- **FR-013**: 新增模型调用路径(rerank 推理)MUST 用 worker pool 遵守 --concurrency(工程铁律:禁止顺序循环)。

### Key Entities

- **TruncationSignal**: 单题截断决策信号——RRF 排序曲线的几何/统计变体(knee + EVT 尾稳定性)或本地 rerank 分数比值(τ = α·max);跨题可比(相对量),确定性。
- **AdaptiveBudget**: 单题装配条数 k* + 保底下限 + singleton fallback;带 token 消耗与逐条弃因审计。
- **CompileDiagnosis**: 离线诊断产物——gold-survival 曲线(信号 × 压缩比)、recovery@k 与 rank 分布对账(009 锚)、040 题级风险清单(56/31 可分性);两臂 GO/NO-GO 判定输入。
- **LocalRerankerConfig**: 涨点臂依赖——本地离线开源 cross-encoder 的选型/加载/超时与降级策略;opt-in,失败逐题回退 RRF。

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: 离线编译诊断门交付:两臂判定 + gold-survival 对照表 + rank 分布对账 + 040 题级风险清单(零 answerer/judge 成本);任一臂门不过即关闭该臂并留关闭报告。
- **SC-002**: 压缩臂 e2e:1-rep probe 配对差 ≥0 且不显著负;3-rep clean 分数不显著低于对照锚,且 answer-context tokens 均值较 k150 装配降幅 ≥30%(体量 parity 同报)。
- **SC-003**: 涨点臂 e2e:1-rep probe 总分不显著负且 open-domain 子集差 >0;3-rep 正批 open-domain 子集配对非负——达不到即诚实 NO-GO 并交付证伪对账,不做无依据调参续命。
- **SC-004**: LME 零重调迁移:同参数不显著低于 LME 现行锚(k30 unified clean);参数零改动有 commit 证据。
- **SC-005**: 全部结果可复算:同批同 judge、token parity、逐题翻转清单与审计入库 result-matrix;文献引用(alphaXiv 实文)与机制来源标注于报告。

## Assumptions

- **期望管理(写进 spec 的诚实定位)**:040 归因(79% 体量/21% 召回)与 045 NO-GO(−14.22pp)是压缩臂最大风险——"150 的分数可在小体量保持"这一前提已被次模装填打脸一次,截断 ≠ 装填(保序截断不重排不混装)但共享同一风险面,US1 门为此前置;008 铁律(coverage 不转化)是涨点臂最大风险——SmartSearch 的 +7.2pp 是其自家口径/自家基线(grep 乱序),engram 基线是 RRF 三信号融合,增量必然更小,probe 可能再次验证铁律,关闭即诚实结论。
- SmartSearch 数字不作 engram 锚(协议不可比,其自证跨协议 14pp 摆动);借鉴的是:编译瓶颈诊断法、score-adaptive 截断机制、reranker 强度是排序增量最大单一因子的证据、单配置跨数据集主张。
- TAA-k 与 040 gap-knee 同族(knee 检测起点),差异在局部 EVT 统计验证与尺度鲁棒性;不预设其绕过 040 结论,由 US1 题级可分性检验裁决。
- rerank 三次证伪对账假设:008 US1(2026-07 弱栈时代 + 弱 reranker)、037(自训 7B 特化)、045(无 rerank 的次模装填)均未测"通用强开源 reranker + thinking 栈 open-domain 召回短板"组合;此假设可能错,probe 检验。
- 死亡规则合规:涨点臂用本地离线开源模型(opt-in 默认关),诊断报告 MUST 明示非 paid cloud、非默认栈;若维护者判定本地 rerank 也不可作为涨点主张,涨点臂降级为纯诊断结论交付。
- 已存 032-store(LoCoMo + LME)与已存 embedding 复用;judge = flash clean 同批重判(2026-08-12 口径);answerer 思考栈(Qwen 在线,vllm 32768)。
- 压缩臂对照锚(k30 生产 87.9% vs k150 思考 90.13% 多数票)在 plan 阶段经 US1 仿真数据定稿为二者之一,spec 不预设。
- 组合批:全部 box 实验合并一次开机跑完即关;rerank 模型与缓存放数据盘。
- eval-config 改动与算法改动分开 commit(宪法 IV 归因纪律);旗标默认值与算法实现分批提交。
- 本 feature 只动评测 harness;probe GO 且正批达标后,是否下沉为引擎公共能力是独立的未来契约增量。
- 后续待深挖(不阻塞本 spec,plan 阶段可选):Retain-or-Consolidate(arXiv:2607.17545,预算依赖算子选择)、PReM(arXiv:2607.14327)、Know Before You Fetch 已在 041 文献审计覆盖(closed-book probe 成本警告)。
