# Contract: 条件压缩（Consolidation，US3）

**契约对象**: 压缩操作符（`cmd/locomo-bench/consolidate.go`）
**范围**: 证据超预算时的 when/which 决策（Retain or Consolidate）；默认关，opt-in；引擎零改动

## 前提（Retain or Consolidate 预算交叉结论）

- **宽松预算（engram cap 3600）下原文保留优先**：LoCoMo 证据短，原文基本装得下；MERGE 在 256-token 宽松档 **−0.107 显著为负**（表 3）。
- 压缩只在「证据确定超预算」时才有潜在价值（LoCoMo 紧预算 Abstract/Merge 48.0 vs 保留 12.9）。
- 因此本 feature **默认不压缩**，US3 是「预算交叉在 engram 实测」的条件性验证，非默认路径。

## when（是否压缩）

1. 装配器（US1）对候选集精确 token 记账（批量 `/tokenize`）。
2. `Σ token > cap`（3600）→ 超预算，进入压缩候选。
3. `Σ token ≤ cap` → 不压缩，原文完整保留（**默认**，Merge 默认关，FR-007）。
4. 压缩仅当显式 `--consolidate` 启用（否则超预算也只是截断/丢弃，不生成）。

## which（用哪个操作符）

超预算时按论文证据排序：`Abstract` / `Merge`（跨条操作）优先于 `Rewrite`（单条改写）；具体选哪个由配对实测决定，不预设普适最优。

| 操作符 | 行为 | 依据 |
|---|---|---|
| `Abstract` | 跨候选提炼一条高层记录 | LoCoMo 紧预算 48.0（并列最强） |
| `Merge` | 合并多条互补候选为一条 | LoCoMo 紧预算 48.0；宽松档 −0.107 显著负 |
| `Rewrite` | 单条各自改写后打包 | 压缩时最弱（26.7） |

- 压缩输出 MUST ≤ cap（精确 token 校验，复用 US1 记账）。
- 被替换的原文单元记录 `replaced_unit_ids`（审计，出问题可回溯）。

## 输出

压缩后装配（`EvidenceAssembly` 变体）：`units[]` 含压缩生成的 EvidenceUnit（`kind=consolidated`）+ `replaced_unit_ids` + 精确 `total_tokens ≤ cap`。

## 错误语义

| 情形 | 行为 |
|---|---|
| 预算内 | 不压缩，原文保留（与 US1 逐字节一致） |
| 超预算但未启用 `--consolidate` | 不生成，装配器截断/丢弃到 cap（现状行为） |
| 压缩生成失败 | 回退原文装配 + 截断（宪法 V） |
| 全关（默认） | 现有路径原样走（parity） |

## 验收

1. 默认：预算内证据原文完整、无任何压缩（与 US1 装配 parity）。
2. 超预算 + `--consolidate`：压缩后 `total_tokens ≤ cap`，`replaced_unit_ids` 完整记录。
3. e2e：压缩 arm vs 保留 arm 配对，e2e 不显著回退（重演预算交叉，或诚实报告负结论——008 纪律）。
