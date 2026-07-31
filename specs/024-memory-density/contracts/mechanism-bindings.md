# Contract: 022.v1 Mechanism Bindings — write_dedup / neighbor_extend

**Date**: 2026-07-31 | **Feature**: [spec.md](../spec.md)

## Status

向后兼容新增（additive）。不改动 022.v1 schema 的既有字段与既有 `mechanism_flags` key 语义；不 bump MAJOR（宪法 III）。

## Schema Extension

`experiment.mechanism_flags` 新增两个 key（原 `idk_retry` / `iris` / `rerank` 保持原语义不变）：

```jsonc
"mechanism_flags": {
  "idk_retry": false,        // 既有，不变
  "iris": false,             // 既有，不变
  "rerank": false,           // 既有，不变
  "write_dedup": false,      // NEW：写入时冗余抑制（默认关）
  "neighbor_extend": false   // NEW：命中后邻居扩展（默认关）
}
```

## Semantics

| Key | 默认 | 语义 | 作用阶段 |
|---|---|---|---|
| `write_dedup` | `false` | 新增 atomic fact 投影前做语义冗余判定，抑制重复投影创建；evidence ledger 无损 | 写入（ingestion/extraction 投影创建） |
| `neighbor_extend` | `false` | 检索命中 fact 后沿共享 evidence 扩展兄弟 fact（depth-1 有界），在候选冻结后、answerer 组装前 | 查询（formal materialize 后） |

## Contract Rules

1. **fail closed**：任一机制 flag 在非 formal 上下文（无 `--eval-protocol`）出现时，`validateMechanismArms` / CLI 校验 MUST 拒绝（复用既有 `supportedArmMechanisms` 框架与 fail-closed 模式）。本 feature 不引入"静默忽略"路径。
2. **独立开关**：两 key 独立生效、独立配置；不得隐式联动（spec FR-009）。
3. **protocol 独立性**：携带新机制 flag 的 protocol 独立冻结，不修改任何已冻结的 022 protocol 资产（spec Assumptions）。
4. **hash 影响**：protocol 的 `experiment.mechanism_flags` 参与 `protocol_hash`；开启任一机制会产生与关闭态不同的 hash（机制可归因）。聚合规则、budget、retrieval 字段不受本扩展影响。
5. **降级**：`write_dedup` 开启时若无 embedding 端点，判定 MUST 走纯离线 Jaccard 路径（宪法 V）；`neighbor_extend` 开启时无邻居则行为与关闭一致（spec FR-008）。
6. **可审计**：开启 `write_dedup` 的 run MUST 在成本/统计产物中输出抑制判定计数（判定/抑制/疑似误伤），供误伤率评估（spec FR-005 / SC-001）。

## Backward Compatibility

- 旧 022.v1 protocol（无新 key）读入时：新 key 缺省为 `false`，行为与现状逐字节一致（spec FR-004 / FR-008 的"默认关"等价于"无此 key"）。
- 既有 `mechanism_flags` key 的取值与语义不变；`validateFormalMechanismBinding` 只新增对新 key 的校验，不改既有校验。

## Consumers

- `freezeFormalProtocol` / `validateFormalMechanismBinding`（`cmd/locomo-bench/eval_runner.go`）
- `validateMechanismArms` / CLI flag 解析（`cmd/locomo-bench/main.go`）
- 引擎侧：`pipeline.storeFact`（write_dedup）、formal materialize 候选冻结后（neighbor_extend）
