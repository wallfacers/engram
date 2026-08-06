# Data Model: 读侧证据装配结构（Evidence Mediation）

**Date**: 2026-08-06 · **Spec**: [spec.md](spec.md) · 契约详情见 [contracts/](contracts/)

## 实体

### EvidenceUnit（证据单元）

装配的最小单元——候选集里的一条 chunk 或 fact 原文，带稳定标识与精确 token 记账（FR-002）。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `source_id` | string | ✓ | 稳定来源 ID（候选集闭包边界内的 ID） |
| `text` | string | ✓ | 证据原文 |
| `kind` | string | ✓ | `chunk` / `fact`（按 `Name` 前缀 `"chunk-"` 判定，复用 chunks.go:210 逻辑） |
| `token_count` | int | ✓ | 真实 tokenizer 精确 count（批量 `/tokenize`；fallback 时 `estimated=true`） |
| `estimated` | bool | 降级时 | token 是否估算（estimateTokens fallback，宪法 V 显式标记） |
| `score` | float | ✓ | 检索融合分 |
| `event_date` | string | 有则填 | `YYYY-MM-DD`（类别结构排序用） |

**Validation**: `token_count ≥ 1`；`estimated=true` 时必须全局标记该装配「估算降级」。

### EvidenceAssembly（证据装配，US1）

装配器输出——token 精确、类别结构化的有序证据包；是 answerer 上下文的唯一来源。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `question_id` | string | ✓ | 配对子集问题 ID（`conv-N-q-M`） |
| `category` | int | ✓ | LoCoMo 类别（1=multi-hop, 2=temporal, 3=open-domain, 4=single-hop） |
| `units` | EvidenceUnit[] | ✓ | 装配后的有序证据（chunk 优先 + 类别结构） |
| `structure` | string | ✓ | `temporal` / `entity` / `generic`（类别条件策略，FR-004） |
| `total_tokens` | int | ✓ | 逐条 token 之和（≤ cap，FR-002 零估算误差） |
| `cap` | int | ✓ | answer-context 预算（3600，`defaultAnswerContextCap`） |
| `chunk_fraction` | float | ✓ | chunk 文本 token / total_tokens（SC-002 指标，029 的 1% 修复） |
| `tokens_estimated` | bool | 降级时 | 整包是否估算降级（vllmTokenCounter 不可用） |

**Validation**: `total_tokens ≤ cap`；`structure` 与 `category` 匹配（temporal→temporal, multi-hop→entity）；关闭时与现有路径 parity。

### GroundedTrace（接地证据链，US2）

引用链中介的四层产物（MemChain 式），sidecar 生成、校验门把关。

| 字段 | 类型 | 必填 | 说明 |
|---|---|---|---|
| `plan` | object | ✓ | `{intent, memory_types, temporal_scope, evidence_requirement, target_count}` |
| `trace` | TraceStep[] | ✓ | 有序接地链；每步 `{role, cited_ids[], statement, next_relation}` |
| `actions` | Action[] | ✓ | `{action: KEEP/DROP/MERGE/REFINE/ADD, cited_ids[], rationale, transformed?}` |
| `evidence` | Evidence[] | ✓ | 最终证据 E（只喂 answerer）；每 `{text, cited_ids[]}` |

**Validation（fail-closed 门，纯 Go 确定性）**:
- 所有 `cited_ids ⊆ 候选集 C_q`（闭包，FR-006）；越界 → 丢弃该条
- 解析失败 → 重试一次 → 再失败回退现有路径（宪法 V）
- `evidence` 为空 → 回退
- E 内每条 evidence MUST 可回溯到 ≥1 个 trace step 的 `cited_ids`

### FailClosedGate（失败关闭门）

校验门的状态记录（US2/US3）。

| 值 | 含义 | 后续 |
|---|---|---|
| `valid` | trace 合法，E 通过闭包校验 | 用 E 作答 |
| `invalid_citation` | 引用候选集外 ID | 丢弃该条，重校验 |
| `parse_failed` | JSON/解析失败 | 重试一次 |
| `fallback` | 重试仍失败 / E 为空 | 回退现有装配路径 |

### ConsolidationOperator（压缩操作符，US3）

仅当证据超预算且显式启用时替换原文（默认关，FR-007）。

| 字段 | 类型 | 说明 |
|---|---|---|
| `enabled` | bool | 默认关；显式 `--consolidate` 启用 |
| `budget` | int | 预算 cap（3600） |
| `operator` | string | `abstract` / `merge` / `rewrite`（跨条操作优先于单条改写） |
| `replaced_unit_ids` | string[] | 被压缩替换的原文单元（审计） |
| `output_tokens` | int | 压缩后精确 count（≤ budget） |

## 关系

```text
candidates (C_q, 检索结果, 闭包)
   │
   ├─(US1)──▶ EvidenceAssembly ◀── EvidenceUnit[N]（token 精确 + chunk 优先 + 类别结构）
   │                 │
   │                 └─(渲染)─▶ answerer prompt（buildAnswerPrompt 扩展）
   │
   └─(US2)──▶ GroundedTrace（plan → trace → actions → evidence）
                  │
                  └─▶ FailClosedGate ─▶ Evidence（最终证据 E）─▶ answerer
   └─(US3)──▶ ConsolidationOperator（超预算 opt-in，替换 EvidenceUnit[N]）
```

- 检索候选集 C_q 是全部引用链的**闭包边界**（每条 evidence/trace 引用必须落在 C_q 内）。
- US1 装配是 US2/US3 的地基（`chunk-quota` 装配结构先固化，再叠引用链/压缩）。

## 状态流转

```text
检索 → 候选集 C_q
  ├─ US1: 装配器（token 记账 + chunk 优先 + 类别结构）→ EvidenceAssembly → answerer
  │         └─(tokenizer 不可用)─▶ estimateTokens fallback（estimated=true）
  ├─ US2: trace 中介（sidecar opt-in）→ FailClosedGate → Evidence → answerer
  │         └─(解析失败)─▶ 重试一次 ─▶ (再失败 / E 空 / 越界引用丢弃后空) ─▶ 回退 US1 装配
  └─ US3: 压缩（opt-in）→ 仅超预算时 Abstract/Merge → 输出 ≤ cap → answerer
            └─(预算内)─▶ 不压缩（原文保留）
```

- 默认（全关）：走现有路径，逐字节不变（SC-004 parity）。
- 降级链：精确 tokenizer → estimateTokens fallback；trace → 重试 → 回退 US1；压缩 → 回退原文。绝不产生空 answerer 上下文（宪法 V）。
