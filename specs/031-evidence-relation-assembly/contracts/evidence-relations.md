# Contract: 证据关系计算与结构上下文注入（Evidence Relation Assembly）

**Feature**: 031 | **Status**: Draft | **Owner**: 031 实现
**输入来源**: 030 `assembleEvidence` 已产出的有序 `[]memory.Result` + 问题类别 + 装配配置
**Scope**: 只动 `cmd/locomo-bench`；engine（`memory/ embedding/ provider/ store/ internal/`）零改动。

## 1. 输入契约

| 输入 | 类型 | 约束 |
|---|---|---|
| `hits` | `[]memory.Result` | 030 排序后的有序候选证据；`Content` 非空；`EventDate` 可空 |
| `category` | `int` | LoCoMo 类别（multi-hop / temporal 生效，其余跳过） |
| `cfg` | `assemblyConfig` | 复用 030：`Cap`、`SystemPrompt`、`QuestionID`、`CurrentDate` 等 |
| `enabled` | `bool` | `--relation-context` 开关，默认 false |

## 2. 输出契约

调用：`computeRelationContext(ctx, hits, category) (block *StructuralContextBlock, err error)`

| 输出 | 类型 | 说明 |
|---|---|---|
| `block` | `*StructuralContextBlock` | `nil` 表示不注入（非 multi-hop/temporal 类别，或关系为空——fail-soft） |
| `err` | `error` | 仅基础设施错误返回；关系计算本身不产生错误（空结果 = nil block） |

`block.Text` 渲染形状（注入 answerer 用户 prompt 的结构上下文块，放在证据列表之后）：

```
[relations]
A --related_to(X)--> B      # 依据：共享实体 X
B --temporal_next--> C      # 依据：2026-03-10 → 2026-05-21
D --caused_by--> E          # 依据：due to
[/relations]
```

规则：
- 每行一条关系边：`From --Type(依据)--> To`
- 行序：multi-hop 按链序（related_to 按共享实体数降序，caused_by 按因→果）；temporal 按时间后继链
- 块 token 计入 030 exact-token 记账；超 cap 时随被截断证据一起消失

## 3. 错误与降级语义

| 场景 | 行为 |
|---|---|
| `enabled=false` | 不调用关系计算，装配路径逐字节不变（parity） |
| 非 multi-hop/temporal 类别 | 返回 `nil, nil`（不产出块） |
| 关系为空（无共享实体/日期/因果） | 返回 `nil, nil`（fail-soft，不臆造关系） |
| 单证据出边超 cap | 截断至 K（related_to 4 / temporal_next 1 / caused_by 2），不报错 |
| trace 叠加且证据越界 | 复用 `trace_gate` 的 closed candidate boundary 校验，越界证据不注入（fail-closed） |
| token 记账失败（counter 不可用） | 沿用 030 的 `TokensEstimated` 降级路径，不阻断装配 |

## 4. Parity 契约（SC-004，硬门）

- `--relation-context` 关闭（默认）：`computeRelationContext` 不被调用；`renderAssembledPrompt` 输出与 030 逐字节一致。
- 校验：`CGO_ENABLED=0 go test ./cmd/locomo-bench -run TestRelationContextParity`——同一输入，开启与关闭的装配输出在关闭路径上逐字节相同（开启路径仅在 prompt 追加块）。

## 5. 确定性契约

- 同输入两次调用 → 逐字节相同（纯函数：无随机、无模型、无时间依赖；排序用稳定排序 + 确定性 tie-break 为 `SourceID` 字典序）。
- `EventDate` 归一化键复用 030 `assemblyDateRank`（`2006-01-02`）；无日期单元置尾。

## 6. 容量契约（FR-005）

- 单证据出边：`related_to ≤ 4`、`temporal_next ≤ 1`、`caused_by ≤ 2`。
- 整块 token 计入 `assemblyConfig.Cap`（不额外放水——不大力出奇迹）。

## 7. 引擎零改动（FR-008，硬门）

合并前 `git diff --name-only -- memory embedding provider store internal` 必须为空。
关系计算只用 `memory.Result` 公开字段（`Content` / `EventDate` / `Name` / `Score`）与标准库。
