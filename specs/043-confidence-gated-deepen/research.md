# Research: confidence-gated gap-guided deepening (043)

**Date**: 2026-08-15
**来源**: spec.md + docs/research/low-topk-recall-context-survey-2026-08-15.md + docs/evaluation/reports/040-adaptive-topk-verdict.md + 042 verdict 链

## R1 · 门控信号:文本犹豫 vs logprob 置信

**Decision**: 2-conv pilot 两者都测,按 pilot AUC 择一;机制只消费选定的那一种。默认预期 logprob 置信(数值连续、阈值可定稿、不依赖措辞模式)。

**Rationale**: 040 实测 93% 信息不足时 answerer 表达犹豫(文本信号有实证基础);logprob 是数值信号,阈值语义清晰、跨模型可迁移。S2G-RAG 的 judge 误报率 6.44%(偏保守)说明保守门可行。042 的 logprob 基础设施(counterfactual_utility_http)已存在,复用成本低。

**Alternatives**: (a) 只用文本犹豫——跨模型措辞漂移风险,pilot 若 AUC 高可保留为备选;(b) 训练小分类器判犹豫——引入训练依赖,违反零训练偏好,拒绝;(c) 复用 042 counterfactual keep/deepen 标签——已 5/5958 NO-GO 关闭,拒绝。

## R2 · gap schema:领域无关四字段

**Decision**: `GapItem{category: bridge_entity|attribute|relation|evidence_span|other, target, slot, description}`,沿用 S2G-RAG schema 原样(其 schema 本身就是为领域无关设计的)。

**Rationale**: schema 是输出格式契约不是答题 prompt,不触碰 038 冻结契约(FR-004)。字段全部通用槽位,无数据集语义;description 兜底自由文本。

**Alternatives**: 自由文本 gap(029 教训:自由导航漂移);更复杂的多实体 join schema(S2G 自己也承认表达力/稳定性权衡,先简后繁)。

## R3 · gap→查询映射:确定性拼接

**Decision**: `query = concat(target, slot)`,两者皆空 fallback `description`,再皆空 fallback 原问题。纯字符串拼接,无模型调用。补检走现行 hybrid 检索,同一 conversation store。

**Rationale**: FR-006(可复算);S2G-RAG 同款映射已在其消融中验证(去掉结构化 gap 掉 15.8 EM 的最大单项)。

**Alternatives**: LLM 改写补检查询(029 证伪家族);多 gap 并发检索(先单 gap 单查询,机制最小)。

## R4 · 加深预算与轮次

**Decision**: 每题最多 1 轮加深;补检取 top-N_deepen(默认与 round-0 k 同量级,N=30;只追加去重后条目,不重排 round-0)。平均每题检索条数目标 ≤60(SC-003)。

**Rationale**: FR-008 与 029 划界(单轮非导航);040 理论上限 91.75% 的假设就是"犹豫题给到高预算证据";追加不替换与 021 划界(slot 挤压 round-0 是其死因)。

**Alternatives**: 按犹豫度分级预算(引入连续调参面,易滑向拟合;拒绝);多轮直到自信(029 家族)。

## R5 · 阈值定稿纪律(防 in-sample)

**Decision**: 犹豫阈值只在 LoCoMo 2-conv pilot + LoCoMo 全量配对批上定稿一次(规则:阈值取 pilot AUC 最优点,不扫全量测试集调参);LME 上零改动。若 LME 回退,不许回改参数——回改即 feature 失败(FR-010)。

**Rationale**: entity-verify 教训(看过测试集调参不可泛化);no-dataset-scoring-labels 红线。

**Alternatives**: 全量扫参取最优(明令禁止);per-dataset 阈值(禁止)。

## R6 · 评测协议

**Decision**: 008 铁律全套——同批配对(arm 交错)、3-rep majority、`--store-dir` 复用、clean 判题(离线重判工具)、context parity 校验、p 值入 result-matrix。Step A 臂(unified×trace-mediation)并入同批 box run 作为顺风对照。

**Rationale**: 单次 run 噪声 ~8.6pp(037);clean 是唯一可跨批口径(042);宪法 IV。

## R7 · 引擎边界

**Decision**: 全部改动限定 cmd/locomo-bench(harness);引擎 memory/ 等零改动。补检索通过 harness 现有的检索调用入口完成,不新增引擎 API。

**Rationale**: 宪法 II(引擎/适配器分离);030/042 先例(读侧机制全部 harness 层)。若发现确需引擎入口,显式提契约增量再议。

**Alternatives**: 在引擎 retriever 加 deepen 钩子(违反本次范围,且机制未过门)。

## R8 · 信号 pilot 的对照构造

**Decision**: 2-conv pilot 的"信息不足"ground truth 用「k30 答错且 k150 答对」题(042 已有 k150 配对数据可离线对齐)+「k30/k150 都对」的对照,避免新标注。

**Rationale**: 复用既有配对 run 的逐题对错,零新标注、零测试集泄漏(标签只来自已有 run 的 judge 结果,不人工看题调参)。

**Alternatives**: 人工标 2-conv 犹豫(慢且有主观性);用 gold evidence 命中与否当标签(033 教训:gold_in_pool 不等于回答链完整)。
