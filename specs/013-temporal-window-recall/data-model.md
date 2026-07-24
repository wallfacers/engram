# Phase 1 Data Model: Retrieval-Side Temporal Window Recall

本 feature **不新增 schema、不新增 migration**。以下是参与实体及其(既有)存储形态,以及新增的内存态结构。

## 既有持久实体(复用,零改)

### Event-dated fact(`memory_entries` 行)

| 字段 | 类型 | 说明 |
|------|------|------|
| `name` | TEXT | 主键(逻辑),召回臂返回的标识 |
| `event_start` | INTEGER (unix seconds, nullable) | 事件起,migration v4 加 |
| `event_end` | INTEGER (unix seconds, nullable) | 事件止,migration v4 加 |
| `event_date` | INTEGER (unix micros, nullable) | 事件点日期,migration v2 加;`event_start`/`event_end` 均空时的回退锚 |

**索引(既有,migration v4)**:`idx_memory_entries_event_start`、`idx_memory_entries_event_end`。召回臂范围查询命中之。

**事件时间边界规约(与 `applyTemporal` 一致)**:
- 有 `event_start` 且有 `event_end` → 用 [start, end]。
- 仅一端 → 另一端取同值(点事件)。
- 两端皆空 → 回退 [event_date, event_date];event_date 亦空 → 该事实无时间边界,**不进召回臂候选集**(降级)。

### TimeWindow(query 解析产物,`memory/temporal.go`,复用)

| 字段 | 类型 | 说明 |
|------|------|------|
| `Start` | time.Time | 窗起,零值 = 下界无界(after 开区间) |
| `End` | time.Time | 窗止,零值 = 上界无界(before 开区间) |
| `Intent` / `State` / `AnchorEntity` / `Fuzzy` | — | 既有元数据,召回臂只用 Start/End |

由 `ParseTemporalIntent(query, anchor)` 确定性解析;`ok=false` 时无时间意图 → 召回臂不点火。

## 新增内存态结构(无持久化)

### 召回臂候选(temporal recall candidate)

召回臂内部产物,不落库:
- 输入:`TimeWindow{Start, End}` + store。
- `NamesByEventWindow(ctx, start, end)` 返回**事件区间与窗相交**的事实 name 列表。
- 相交谓词:`event_end >= window.Start AND event_start <= window.End`(边界回退按上规约;半开区间某侧无界时该侧条件恒真)。
- 排名序:按 gap(事件区间到窗的最近距离)升序,完全落窗内 gap=0,次级按 name 升序(确定性)。
- 交 `ranksFromOrder(names) → map[string]int`,成为 RRF 第 4 路信号。

### 诊断分层结果(US1,适配器态,不落引擎)

| 层 | 度量 | 判据 |
|----|------|------|
| Layer 0 | temporal 类 query 中 `ParseTemporalIntent` ok 的占比 | 低 → 病因解析器/Fork B |
| Layer 1 | temporal 类 gold 事实带可解析 event 日期的占比 | 低 → 病因抽取侧 |
| Layer 2 | temporal 类 gold 在当前 fused 排名的 rank 分布(尤其 > 候选池 cutoff 的比例) | 深埋 → 召回瓶颈坐实 |
| Layer 3 | 纯 event_date∈window oracle 臂把多少 Layer 2 深埋 gold 抬进 top-30(配对 delta) | 抬升显著 → 机制有天花板 |
| 判定 | GO(四层全过)/ NO-GO(+病因归属) | — |

## 状态流 / 交互

```
query ──ParseTemporalIntent──> TimeWindow?
  │ ok=false                        │ ok=true
  ▼                                 ▼
[无 temporal 臂]           NamesByEventWindow(Start,End)
  │                                 │ names(相交, gap 升序)
  ▼                                 ▼
signals = {bm25, vec, ent}   signals += ranksFromOrder(names)   ← 第4路(平权)
  └────────────► fuseRRF(signals...) ◄──────────────┘
                       │
                       ▼
              applyTemporal(乘子, 保留)  ← Fork A:池内精修,加性
                       ▼
              applySupersededPenalty ... → top-K
```

**降级不变量**:`ParseTemporalIntent` ok=false 或 `NamesByEventWindow` 返回空 → `signals` 不含第 4 路 → 结果与臂未引入时 byte-identical(parity)。事实无边界 → 不在 names → 不报错。
