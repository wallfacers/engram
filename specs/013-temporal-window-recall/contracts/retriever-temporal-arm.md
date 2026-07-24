# Contract: Temporal recall arm → RRF 4th signal (engine, US2)

**Package**: `memory` · **Site**: `Retriever.SearchWithDiagnostics` · **Stability**: 加性,默认关

## Integration point (frozen)

在 `retriever.go` 现有信号构建处(`signals := []map[string]int{bm25, vec, ent}` 之后、`fuseRRF` 之前)加入时间窗召回臂:

```go
// 现状:
//   var temporal *TimeWindow
//   if r.options.TemporalScore { if parsed, ok := ParseTemporalIntent(query, r.options.Now); ok { temporal = &parsed } }
//   ... signals := []map[string]int{bm25, vec, ent} (+associative)
//   fused := fuseRRF(signals...)
//   if temporal != nil { fused = r.applyTemporal(ctx, fused, *temporal) }   // 保留(Fork A)
//
// 新增(temporal != nil 时):
//   signals = append(signals, r.temporalRecallRanks(ctx, *temporal))         // ← 第4路,平权
```

`temporalRecallRanks(ctx, window)`:调 `entries.NamesByEventWindow(ctx, window.Start, window.End)` → `ranksFromOrder(names)`。查询失败 → `slog.Warn` + 返回 nil map(信号缺席)。

## Invariants

- **默认关 / parity**:召回臂仅在 `r.options.TemporalScore == true` **且** `ParseTemporalIntent` 返回 ok(即既有 `temporal != nil` 分支)时构建。两者任一不满足 → `signals` 不含第 4 路 → 检索结果与臂未引入时 **byte-identical**(既有 parity golden 不破)。
- **tuning-free**:第 4 路作为 RRF 平权信号(k=60),**不引入** temporal 专属融合权重 / 不改 `fuseRRF` 的 k。
- **降级**:`NamesByEventWindow` 返回空或 err → 第 4 路为空 map,自然掉出 RRF 和(与 nil embedding client → 语义信号缺席同机制)。
- **与乘子共存(Fork A)**:`applyTemporal` 软乘子保留在 `fuseRRF` 之后不动;召回臂在 `fuseRRF` 之前。两者同向加性,不得因臂+乘子同开对同一事实产生破坏性双重加权(由测试 5 把关)。
- **引擎/适配器**:算法只在 `memory/`;适配器不重实现。

## Test obligations (offline, free)

1. **parity(关)**:`TemporalScore=false` 或无时间意图 query → 检索结果序与臂未引入时逐字节相同(对既有 golden / 直接对照)。
2. **深埋 gold 抬升**:构造一条语义/关键词排名在候选池 cutoff 之外、但 event_date 落窗内的事实 + 一条带时间意图的 query → 臂开启后该事实进入融合 top-K(臂关时不进)。
3. **降级—无意图**:无时间意图 query → 臂不产 rank,结果 == 三信号基线。
4. **降级—无边界**:所有事实无 event 时间 → 臂候选空,检索不报错,结果 == 三信号基线。
5. **乘子交互无害**:臂 + `applyTemporal` 同开时,in-window 事实不被过度提升到破坏其他信号的程度;near-window 事实的乘子降权不导致其跌出本应命中的 top-K(以固定小样本断言相对序)。
6. **tuning-free 不回归**:RRF k 常量未变;无新增 temporal 权重 option。
