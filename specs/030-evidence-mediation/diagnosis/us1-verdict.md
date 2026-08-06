# US1 Verdict: 预算诚实的证据装配地基

**Date**: 2026-08-06 · **Tasks**: T006-T011 · **Spec**: [spec.md](../spec.md) US1 · **契约**: [evidence-assembly.md](../contracts/evidence-assembly.md)

## 结论

**机制 GO**（装配器正确性全绿）+ **真实 e2e chunk_fraction 达标待评测环境确认**（需 hybrid + chunk-quota 的 bge-large/远端环境）。关键真实发现：**keyword-only 检索下 chunks 不进候选（chunk_fraction=0），确认 029 根因 A——装配器无法补偿检索侧 chunk 缺失，chunk 保底必须在检索侧（hybrid + chunk-quota）或评测口径保证候选含 chunk**。

## 验证证据

### 1. 装配器核心单测（离线，全绿）

| 测试 | 断言 | 状态 |
|---|---|---|
| `TestAssembleChunkFirst` | chunk 全部先于 fact，组内 score desc | ✅ |
| `TestAssembleTemporalOrder` | temporal 类别按 event_date asc，structure=temporal | ✅ |
| `TestAssembleChunkFractionThreshold` | **SC-002**：候选含 chunk 时 chunk_fraction ≥ 0.5（029 是 ~0.01） | ✅ |
| `TestAssembleCapTruncation` | 超 cap 精确截断，TotalTokens ≤ cap | ✅ |
| `TestAssembleExactTotal` | stub 精确 count → TotalTokens 精确、tokens_estimated=false | ✅ |
| `TestAssembleTokensEstimatedFallback` | 无 tokenizer → estimate 记账 + tokens_estimated=true（宪法 V） | ✅ |
| `TestAssembleRender` / `TestAssembleEmptyHits` | 渲染含全部 units + QUESTION；空候选渲染 (none) | ✅ |
| Foundational `TestAssemblyTokenCounter*` | 精确 counter unavailable/exact/error 传播 | ✅ |

### 2. 真实 store 端到端诊断（零模型成本）

在 008-embed-large-store（bge-large 1024d，conv0/conv1，--chunks）上跑 `--assembly-diagnose`（keyword 检索，10 题）：

```
questions: 10
structures: {temporal: 4, generic: 3, entity: 3}   ← 类别条件结构分类正确
chunk_fraction: median 0（见下）
tokens_estimated: 1.0（本地无 vllm tokenizer，estimate fallback）
units_per_question: 30（topK）
```

- **端到端管线工作**：检索 → 装配器 → 诊断 JSONL → `assembly_diagnose.py` 分析全链路通。
- **structure 分类正确**：temporal/entity/generic 按类别路由（FR-004 机制验证）。
- **chunk_fraction=0 是检索侧限制**：keyword（FTS BM25）下长 chunk 分数低，top-101 候选全 fact（`applyChunkQuota` 无 chunk 可保）。**这正是 029 根因 A 的独立确认**——基线的 3654 tokens 来自 hybrid（semantic 召回 chunks）+ `--chunk-quota 12`，不是 keyword。
- 本地无 bge-large sidecar（embed_server.py 硬编码 bge-small，008 store 是 1024d），hybrid 检索需评测环境。

## 门禁判定（tasks.md）

| 门禁 | 判定 | 依据 |
|---|---|---|
| token 记账零估算误差（SC-001） | ✅ 机制 / ⏳ 真实 e2e | 单测精确（stub）；真实精确 count 需 vllm tokenizer（US2 配对环境验证） |
| chunk_fraction ≥ 阈值（SC-002） | ✅ 机制 / ⏳ 真实 e2e | 单测 ≥0.5；真实 store 需 hybrid + chunk-quota 环境 |
| 默认关 parity（SC-004） | ✅ | `--evidence-assembly` 未设时路径逐字节不变（条件分支） |

**US1 机制 GO** → US2/US3 可实现（装配器是地基，已正确）。

## 真实发现（值得记录）

1. **keyword-only 检索 chunks 匹配差是 029「chunk fraction 1%」的检索侧机制**：不是装配侧问题，装配器（chunk-first 排序）在候选含 chunk 时正确工作。
2. **装配器无法补偿检索侧缺失**：候选里没有 chunk，装配器变不出 chunk。chunk 保底必须靠检索侧 `--chunk-quota` + hybrid（基线口径），或评测时保证候选含 chunk。
3. **US2 配对必须用基线同款检索口径**（hybrid + chunk-quota 12），否则装配 arm 与基线比较不公平。

## 待评测环境确认（US2 配对时）

- hybrid + `--chunk-quota 12` 下真实 store 的 chunk_fraction（SC-002 完整达标）
- vllm tokenizer 在线时 TotalTokens 精确（SC-001 完整）
- `tokens_estimated=false`（精确计数器路径）
