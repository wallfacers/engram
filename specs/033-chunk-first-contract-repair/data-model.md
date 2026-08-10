# Data Model: Multi-hop Chunk-First Contract Repair

## EvidenceCandidate

装配器的只读输入节点。

| Field | Meaning | Validation |
|---|---|---|
| `source_id` | 稳定候选 ID | 非空；同一输入闭包内预期唯一 |
| `text` | 原始候选文本 | 不得由本 feature 改写 |
| `kind` | `chunk` 或 `fact` | 由既有 ID 规则确定 |
| `score` | 检索相关分 | 降序比较；同分不作为最终 tie-break |
| `entity` | 首个可提升实体或空 | 仅用于层内分组 |
| `ordinal` | 输入闭包中的原位置 | 只作异常重复 ID 的最终稳定兜底 |

## EntityGroup

同一实体下的候选视图。组 coverage 使用完整输入闭包计算，避免 chunk/fact 分层后改变旧语义。

| Field | Meaning | Validation |
|---|---|---|
| `entity` | 分组标签 | 非空；空实体进入 ungrouped |
| `coverage` | 完整闭包中属于该实体的候选数 | 非负；组排序 coverage desc、entity asc |
| `members` | 该实体全部候选 | 输入候选仅属于一个实体组 |

## CanonicalEvidenceSequence

唯一可被装配记录、截断与 renderer 消费的 flat sequence。

顺序不变量：

1. 全部 chunk 位于全部 fact 之前；
2. 每个 kind layer 内按 EntityGroup 的全闭包 coverage 顺序遍历；
3. 每组内 score desc、source_id asc、ordinal asc；
4. 每层 ungrouped 位于实体组之后；
5. 输入 ID+text 多重集与未截断 canonical sequence 的多重集完全一致。

## AdmittedEvidencePrefix

预算下真正进入 answer context 的 canonical prefix。

| Field | Meaning | Validation |
|---|---|---|
| `units` | canonical sequence 的前缀 | 不允许中间跳项或尾部后再加入 |
| `total_tokens` | 完整 system+user prompt token 数 | 精确 counter 可用时 ≤ cap |
| `cap` | 冻结上下文预算 | 与 control/treatment receipt 一致 |
| `tokens_estimated` | 是否使用估算降级 | 必须如实记录 |
| `entity_order` | `kind_layered` 或 `legacy_grouped` | treatment/control 可审计 |

状态转换：

```text
Input closure
  ├─ legacy control ──> Legacy grouped sequence ──> longest budget prefix
  └─ treatment      ──> Canonical kind-layered sequence ──> longest budget prefix
                                                        └─> streaming prompt
```

## PairedEvaluationReceipt

| Field group | Required contents |
|---|---|
| Data | dataset path/hash、question IDs、样本数 |
| Store | store path/receipt、embedding fingerprint |
| Answer | provider/model、`LOCOMO_NO_THINKING=0`、prompt、token cap、legacy IDK retry enabled、repeats=3 |
| Judge | provider/model、mem0-aligned mode |
| Treatment | evidence assembly、entity order、trace/relation/consolidate states |
| Results | A/C absolute score、B/C multi-hop attribution、paired flips、exact McNemar、Holm category rows、primary/retry calls、cost |

任何 treatment 字段缺失、resume fingerprint 不一致或 coverage 不完整时，结果不得用于突破声明。
