# Contract: US2 排序选项 + 诊断暴露(engine, contract-first, 默认关)

## RetrieverOptions 增量(additive)

```go
type RetrieverOptions struct {
    // ...现有字段不变...
    RankingRefine bool // US2:定向排序改动开关;零值=现三信号等权 RRF 逐字节不变(默认关)
    // 机制参数(若 score-aware RRF / MMR 需要)在 tasks 阶段据 US1 证据定,均须默认零值=旧行为
}
```

**契约保证**:
- 零值调用者(现有所有 caller)行为**逐字节不变**(FR-008 / SC-006 parity)。
- 打开时机制为纯 Go / offline / `CGO_ENABLED=0` 可构建 / 无云·付费 reranker(FR-009)。

## SearchDiagnostics 增量(additive,支撑 per-signal rank)

```go
type SearchDiagnostics struct {
    // ...现有字段不变...
    PerSignalRanks map[string]map[string]int // entry name → signal(sem/kw/entity)→ 1-indexed rank;默认 nil
}
```

**契约保证**:
- 默认 nil(不请求诊断时不填充,零开销)。
- 仅为归因/诊断用途;不改变检索结果排序本身。
- additive:现有 caller 忽略即可,无破坏性变更(宪法 III)。

## 三道门(FR-011,判定契约)

| 门 | 断言 | 失败后果 |
|---|---|---|
| ① 纯 Go 契约 | `CGO_ENABLED=0 go build/test ./...` 绿;parity golden 关时逐字节等基线;新排序单测过 | 阻断实现 |
| ② 离线归因 | US1 trace 复跑,Q3 象限 gold 平均排名上升;无云/付费杠杆 | 阻断上端到端 |
| ③ 端到端决胜 | 同机配对 McNemar above-noise + overall 及任一非目标类不显著回退;超越反证基线(−0.3pp/p=1.0、−0.06pp/p=1.0) | 判 NO-GO 出货,保留默认关诊断能力,结论落 eval-log + 台账 |

coverage 增益 **MUST NOT** 单独作 GO 依据(FR-012)。US2 引擎改动与 US1 adapter 改动**分开提交**(FR-013)。
