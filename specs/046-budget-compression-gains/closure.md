# 046 budget-compression-gains — Pre-Implementation Closure(2026-08-17)

**状态**:spec 起草当日关闭,零实验成本。两条臂在动手前与 tracked docs 既有 verdict 对账,
均被**实质性覆盖(证伪)**,不进入 plan。本文是对账正本。

**触发**:维护者指令"先看下 docs 这个路子是不是验证过了,如果没有再开始下一步"。

## 一句话结论

压缩臂(查询自适应截断)被 **040 的完美信号上限账**判死(89.2% < 90.13%);涨点臂(本地强
reranker)被 **008 + 037 两代通用 reranker e2e 证伪**判死(bge-v2-m3 档 −0.06pp,机制归因
"语义相似度排序破坏多跳推理链")。本轮 alphaXiv 新证据(SmartSearch/TAA-k)复核后**不构成
翻案理由**——其增益前提(乱序 grep 基线)与证据形态(仅 recall 仿真)在 engram 栈不成立。

## 压缩臂:TAA-k / score-adaptive 截断 — 已被 040 覆盖

决定性证据:[040 verdict](../../docs/evaluation/reports/040-adaptive-topk-verdict.md)
"即使 gap-knee 完美,分数 = 1332 + 11 + 31 = 1374 = **89.2%,掉 0.9pp。NO-GO**"。

- 这是**分数曲线信号驱动自适应 k 的理论上限账**(假设信号完美识别):56 题增量中 42 题
  (79%)是"gold 已在 top-30、answerer 需要更多上下文"的**体量题**。
- 体量需求对一切相关性分数隐形——RRF 曲线、TAA-k 的 EVT 尾稳定性、rerank 分数的 α·max
  阈值,测的都是 query-passage 相关性,而体量题需要的恰是"低相关的上下文"。
- 所以 TAA-k(arXiv:2606.11907)换更稳的拐点检测**不改变上限**:瓶颈不是拐点精度,
  是信号本身看不见体量。收缩方向(识别哪些题不需要 150)同样被覆盖:42 体量题
  "看起来容易(gold 在头部)但需要更多",与真正易题在分数曲线上不可分。
- 045 次模装填(−14.22pp)从装填侧二次撞墙,同属"小体量保 150 信息"失败族。
- SmartSearch(arXiv:2603.15599)的 score-adaptive 截断(表 5,α=0.03 省 56% token 仅丢
  1.3% recall)**只有 recall 仿真,无 e2e**:其 e2e 主结果(91.9)用的是固定预算 B1 配置。
  "gold 存活 ≠ answer 对"正是 040 的 42 体量题教训——recall 仿真回答不了 answer 掉不掉。

## 涨点臂:本地强开源 reranker — 已被 008 + 037 覆盖

- [008 US1](../../docs/evaluation/experiment-verdicts.md):**bge-reranker-v2-m3**
  (≈ SmartSearch 表 9 的 87% recovery 档,非弱模型)coverage +15.457pp → e2e **−0.06pp**。
- [037 US1](../../docs/evaluation/reports/037-memory-reranker-verdict.md):Qwen3-Reranker-0.6B
  e2e **−0.4pp**(multi-hop −12 题);US2 自训 LoRA **−1.1pp**。机制归因:**通用 reranker
  的语义相似度排序破坏多跳推理链**——这是机制性结论,不是模型强度问题。
- mxbai-rerank-large-v1(435M)的翻案论证不成立:recovery 优势 vs bge 档仅 ~3.5pp
  (90.9 vs 87.4,SmartSearch 表 9),而其表 8 显示 bge-large→mxbai-large 的 **e2e 差仅
  1.3pp**——008 已在 87% 档 NO-GO,+3.5pp recovery 逆转不了 −0.06pp e2e。
- SmartSearch +7.2pp 的前提是**乱序 grep 基线**(gold mean rank 195→8);engram 是 RRF
  三信号融合基线(gold 中位 rank 71-90,009),rerank 增量空间不同量级。
- 死亡规则再确认:即便翻案,本地 rerank 也只能 opt-in 诊断,不得作涨点主张。

## 本轮 alphaXiv 文献侦察沉淀(不浪费的收获)

- **SmartSearch**(arXiv:2603.15599):LoCoMo-10 同数据集的"编译瓶颈"诊断方法论
  (检索 recall 98.6% vs 截断后 gold 存活 22.5%)值得保留为诊断框架;其跨协议敏感性
  自证(full-context 77.1↔91.2,14pp)再次确认 judge 口径不可比,**数字一律不引用为锚**。
- **TAA-k**(arXiv:2606.11907):training-free 纯客户端 4ms/题,若未来出现"分数信号
  可见"的新场景(如纯召回型数据集)可复用;在 LoCoMo 被 040 上限账封死。
- **MemRefine**(arXiv:2606.13177):是**存储预算**压缩(写侧 store delete/merge/preserve),
  与 top-k 上下文税正交;engram curation(near-dup dedup + LLM judge)已有同类能力。
- **Retain-or-Consolidate**(arXiv:2607.17545)/ **PReM**(arXiv:2607.14327):未深挖,
  但均属"预算约束下的记忆管理"族,若未来重启须先过本对账的同样的门。

## spec 起草时的两处判断失误(防重犯记录)

1. 046 spec US1 门把 040 当作"只证伪了加深方向"——漏算"完美信号上限 89.2%"这行账
   **同样覆盖收缩方向**(任何分数→k 映射的统一上限)。
2. "强 reranker 未测"论证未核 008 的具体模型档位:bge-reranker-v2-m3 已是 87% recovery
   档,不是弱模型;档位差(+3.5pp recovery ≈ +1.3pp e2e)不足以构成"未测缝"。

**教训(通用)**:起草 exploration spec 前,必须先把候选方向与
[experiment-verdicts.md](../../docs/evaluation/experiment-verdicts.md) 及相关 verdict
报告**逐行对账**(尤其"理论上限账"类结论),alphaXiv 新文献只提供机制假设,不提供
翻案证据——除非新证据直接攻击旧 verdict 的归因机制(本次 SmartSearch/TAA-k 均未做到)。

## 剩余未测方向(本 spec 范围外,仅记录;2026-08-17 归因收口更新)

- ~~open-domain 池外召回~~ **已判死**:后续零成本归因(见
  [locomo-error-retrieval-attribution-2026-08-17](../../docs/evaluation/reports/locomo-error-retrieval-attribution-2026-08-17.md))
  实测全量池外仅 14/1540(0.9%)、152 错题池外仅 4 题——本 closure 起草时"gold 不在宽池"
  的假设是误读,90% 错题是 gold 已在上下文答错(answerer 侧)。
- **temporal 锚定契约 × Qwen thinking 栈确认**(032 遗留):2026-08-17 维护者否决——载体
  `--temporal-answer-prompt` 是 LoCoMo 类别特化提示词,特化 prompt 不作方案手段(红线);
  temporal 偏移题归入 answerer 能力带,LoCoMo 杠杆线收线(见归因 doc)。
- LME 侧:剩余差距归因为 Qwen 能力天花板(entity-verify verdict,+0.7pp 后 88.5%),
  无快速杠杆。
