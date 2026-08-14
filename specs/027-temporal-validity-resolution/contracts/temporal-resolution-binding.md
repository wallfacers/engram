# Contract: 022.v1 Mechanism Bindings — temporal_resolution

**Date**: 2026-08-06 | **Feature**: [spec.md](../spec.md)

## Status

向后兼容新增（additive）。不改动 022.v1 schema 的既有字段与既有 `mechanism_flags` key 语义；
不 bump MAJOR（宪法 III）。沿用 [024 mechanism-bindings](../../024-memory-density/contracts/mechanism-bindings.md)
的契约形态与 026 compiler 的接线模式。

## Schema Extension

`experiment.mechanism_flags` 新增一个 key（既有 `idk_retry` / `iris` / `rerank` / `write_dedup` /
`neighbor_extend` / `compiler` / `episode_cluster` 保持原语义不变）：

```jsonc
"mechanism_flags": {
  "idk_retry": false,           // 既有，不变
  "iris": false,                // 既有，不变
  "rerank": false,              // 既有，不变
  "write_dedup": false,         // 既有（024），不变
  "neighbor_extend": false,     // 既有（024），不变
  "compiler": false,            // 既有（026），不变
  "episode_cluster": false,     // 既有（025），不变
  "temporal_resolution": false  // NEW：查询时时间有效性解析（默认关）
}
```

## Semantics

| Key | 默认 | 语义 | 作用阶段 |
|---|---|---|---|
| `temporal_resolution` | `false` | 固定候选池内、查询时、确定性组织已命中证据的时间结构：当前值解析 / 演化链组装 / 时间窗约束；不改变候选选择与检索打分 | 查询（formal materialize 后、bundle 组装前） |

**与 legacy temporal flags 的关系**：本 key **不同于** `temporalScore` / `temporalHardFilter` /
`conflictResolution` / `supersededPenalty`（013/014/017/024 遗留，`validateFormalLegacyMechanismOptions`
已拒绝它们进 formal B1）。本 key 走 additive mechanism 分支（`densityMechanismFlagsForOptions`），
不触发 legacy 拒绝。两者不得混用归因。

## Contract Rules

1. **fail closed**：`--temporal-resolution` 在非 formal 上下文（无 `--eval-protocol`）出现时，
   CLI 校验 MUST 拒绝（复用既有 fail-closed 模式）。不引入"静默忽略"路径。
2. **独立开关**：本 key 独立生效，与其它 mechanism flag 独立配置；不得隐式联动。
3. **protocol 独立性**：携带 `temporal_resolution` 的 protocol 独立冻结，不修改任何已冻结的
   022/024/025/026 protocol 资产。
4. **hash 影响**：`experiment.mechanism_flags` 参与 `protocol_hash`；开启本 key 产生与关闭态
   不同的 hash（机制可归因）。聚合规则、budget、retrieval 字段不受影响。
5. **候选不变**：三臂共享 `compileFormalSources` 同一 flat source list（候选逐字节一致），
   解析器只读 `Source.OccurredAt`，不改变候选 ID、rank、文本或 source 集。
6. **零 LLM**：解析器纯 Go 确定性，MUST NOT 调用 LLM 判定事实主题或演化关系；无 embedding/
   LLM 端点时可完整离线运行（宪法 V）。
7. **可审计**：开启 `temporal_resolution` 的 run MUST 在统计/归因产物中输出 per-question 解析
   audit（mode / group_count / versions_considered / superseded_excluded / window_excluded /
   unresolved_time / resolution_oracle），供归因（spec FR-008）。

## Backward Compatibility

- 旧 022.v1 protocol（无本 key）读入时：本 key 缺省为 `false`，行为与现状逐字节一致（默认关
  等价于无此 key）。
- 既有 `mechanism_flags` key 的取值与语义不变；`validateFormalMechanismBinding` 只新增对本
  key 的校验，不改既有校验。

## Consumers

- `densityMechanismFlagsForOptions` / `mergeMechanismFlags` / `formalTreatmentFreeze`
  （`cmd/locomo-bench/eval_runner.go`）—— 加性机制 flag 接线
- CLI flag 解析 + fail-closed 校验（`cmd/locomo-bench/main.go`）
- `temporal_resolution.go`（027 新增）—— 解析器实现，消费 `compileFormalSources` 的 source list
