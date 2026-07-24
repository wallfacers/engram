# Contract: LoCoMo temporal 召回诊断 + eval flag (adapter)

**Package**: `cmd/locomo-bench` · **Stability**: 适配器工具,零引擎改动(US1)/ eval flag(US3)

## US1 — 四层召回诊断(retrieval-only,零答题/judge token)

**入口**(拟):`--temporal-diagnostic`(复用 012 `runDoc2QueryRecallDiagnostic` 同构基建),在固化 bge-large store 上跑,只调既有引擎只读 API(`Retriever.Search` / `EntriesByName` / `List` / `ParseTemporalIntent`)。

**输入**:固化 store + LoCoMo temporal 类问题(category=temporal)+ gold fact 标注。

**输出**(分层表 + 判定):

| 层 | 度量方式(只读) | 输出 |
|----|----------------|------|
| Layer 0 | 对每条 temporal query 调 `ParseTemporalIntent`,统计 ok 占比 | `parse_coverage` |
| Layer 1 | 对每条 temporal gold 事实,查其 event_start/end/date 是否非空 | `event_date_coverage` |
| Layer 2 | 对每条 temporal query 跑当前 `Retriever.Search`(臂关),记 gold rank 分布 + 落候选池 cutoff 外比例 | `buried_ratio`, rank 分位 |
| Layer 3 | 对每条 temporal query,用 `ParseTemporalIntent` 窗 + 读事实 event 时间做纯 event∈window 拉取(oracle),统计其中 Layer 2 深埋 gold 进 top-30 的数量(配对 delta) | `oracle_lift@30` |
| 判定 | 见下 | `GO` / `NO-GO` + `cause` |

**GO 判据(四层全过)**:`parse_coverage` 有意义 **且** `event_date_coverage` 有意义 **且** `buried_ratio` 显著(gold 确深埋)**且** `oracle_lift@30` 显著(有天花板)。任一层塌 → `NO-GO`,`cause ∈ {解析器, 抽取侧, 非召回瓶颈(gold 已在top), 天花板不足}`。

**成本**:零答题/judge token(纯检索 + 只读)。box bge-large 或本地,near-free。

**隔离断言**:US1 阶段 `git diff --name-only -- memory embedding provider store internal` **必须为空**(诊断纯适配器 + 既有引擎只读 API)。

## US3 — 端到端配对 eval flag(条件于 US2)

**入口**(拟):`--temporal-arm {off|on}`(或复用既有 temporal 选项组合),在同一固化 store + 同一答题/judge 栈下跑臂 off/on 配对。

**契约**:
- repeats≥3 覆盖 temp=1.0 答题噪声;temporal 类答题分做配对 McNemar。
- 以**答题分**为准(coverage 仅诊断,008 铁律)。
- GO:temporal 类答题分 on ≥ off 且在噪声带外 **且** 非 temporal 类总分不回归。否则记 within-noise / 污染 NO-GO,诚实收口。
- eval-config 改动与算法改动分开 commit(宪法 IV,FR-016)。
- 结论落 tracked `docs/locomo-score-levers.md`(不只进本地 memory)。
