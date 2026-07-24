# Contract: EntryStore.NamesByEventWindow (engine, US2)

**Package**: `memory` · **Type**: `*EntryStore` · **Stability**: 加性契约增量(不破既有 API)

## Signature (frozen)

```go
// NamesByEventWindow returns the names of entries whose event interval
// intersects [start, end], ordered by temporal proximity to the window
// (fully-inside first, then ascending gap), name-ascending as the stable
// tiebreak. A zero start or end means that side is unbounded (before/after
// open intervals). Entries with no event_start/event_end fall back to
// event_date; entries with no usable event time at all are excluded.
func (s *EntryStore) NamesByEventWindow(ctx context.Context, start, end time.Time) ([]string, error)
```

## Semantics

- **相交谓词**:事实事件区间 `[es, ee]` 与窗 `[start, end]` 相交 ⇔ `ee >= start AND es <= end`。
  - 边界回退:`es`/`ee` 由 `event_start`/`event_end` 取;某端空 → 取另一端;两端皆空 → 取 `event_date`(两端相同);`event_date` 亦空 → 排除该事实。
  - 半开区间:`start` 为零值 → 下界无界(谓词左侧恒真);`end` 为零值 → 上界无界(谓词右侧恒真);两者皆零 → **返回空**(无窗不召回,避免全表返回)。
- **排序**:按 gap 升序(完全落窗内 gap=0),次级 `name` 升序。gap = 事件区间到窗的最近时间距离。
- **索引**:命中既有 `idx_memory_entries_event_start` / `idx_memory_entries_event_end`(migration v4)。**不新增 migration。**
- **降级**:查询错误返回 `(nil, err)`,由调用方(retriever)记 `slog.Warn` 并让该信号缺席(不整体失败)。

## Test obligations (offline, free)

1. **相交正确**:窗内 / 部分相交(跨边界)/ 完全在窗外 三类事实,只返回前两类。
2. **回退**:仅 `event_date`(无 start/end)的事实,其 date 落窗内 → 返回;date 落窗外 → 不返回。
3. **半开区间**:`start` 零值 → 返回所有 `es <= end` 的事实;`end` 零值 → 返回所有 `ee >= start`;两者零值 → 空。
4. **降级**:无任何 event 时间的事实从不出现在结果;空结果不是错误。
5. **排序确定性**:同一输入多次调用返回 byte-identical 顺序(gap 升序 + name 次级序)。
