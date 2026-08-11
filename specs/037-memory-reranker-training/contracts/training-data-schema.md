# Contract: Reranker Training Data Schema

**Date**: 2026-08-11 | **Spec**: [spec.md](../spec.md) | **Data model**: [data-model.md](../data-model.md)

训练 JSONL 的逐字段契约。`tools/build_training_data.py` 的输出必须严格符合此 schema；`tools/test_training_data.py` 对其做 fail-closed 校验（任何样本不满足即报错退出，杜绝脏数据进训练）。

## 格式

每行一个 JSON 对象（NDJSON）。UTF-8。确定性派生：同一输入 + 同一 seed → 完全相同的输出。

## 字段

| 字段 | 必填 | 类型 | 约束 | 说明 |
|---|---|---|---|---|
| `sample_id` | ✓ | string | `^[a-z0-9-]+$` | 全局唯一，可审计回源 |
| `schema_version` | ✓ | int | ≥1 | **行内字段**（非仅说明文字），当前 1 |
| `qa_id` | ✓ | string | 非空 | 源 question 标识 |
| `query_group_id` | ✓ | string | 非空，= qa_id | multi-positive 的 group key |
| `query` | ✓ | string | 非空、长度 ≤ 512 | 检索查询 |
| `document` | ✓ | string | 非空、长度 ≤ 4096 | 候选记忆片段 |
| `document_kind` | ✓ | enum | `fact` \| `chunk` \| `observation` | 与 runtime 候选同源序列化 |
| `candidate_source` | ✓ | string | 非空 | baseline top-pool / evidence 定位 |
| `label` | ✓ | number | 0.0–1.0 | 相关度监督标签 |
| `is_positive` | ✓ | bool | — | 正/负样本 |
| `positives` | 条件 | string[] | 正样本必填（同 question 全量）、负样本 null | multi-positive（多 evidence） |
| `category` | ✓ | enum | `single-hop` \| `multi-hop` \| `temporal` \| `open-domain` \| `msc-persona` \| `msc-cross-session` | 类别（temporal 单独可切） |
| `temporal_label` | ✓ | bool | — | 是否时序类（**仅文本可见时间信号存在时为真**） |
| `negative_type` | 条件 | enum \| null | `in-dialogue` \| `temporal-hard` \| `cross-session`，负样本必填、正样本 null | 负样本类型 |
| `evidence_refs` | 条件 | string[] \| null | 正样本必填（≥1 条）、负样本 null | 溯源定位 |
| `split` | ✓ | enum | `train` \| `heldout` | 按真实 conv ID 划分 |
| `conv_id` | ✓ | string | 非空 | 源对话标识（LoCoMo conv-XX / MSC id） |
| `epoch` | 可选 | int | ≥0 | 构建版本（迭代可审计） |

## 校验规则（fail-closed）

1. **必填字段缺失**（含 `schema_version`、`qa_id`、`query_group_id`、`split`）→ 该样本被拒绝，脚本报错列出 sample_id。
2. **`label` 越界**（<0 或 >1）→ 拒绝。
3. **正样本无 `evidence_refs` / `positives`** 或无法定位到源对话 turn/observation → 拒绝。
4. **负样本缺 `negative_type`** → 拒绝。
5. **multi-positive**：同一 `query_group_id` 的所有正样本不得互为负样本；近重复与 evidence-overlap 候选排除出负池（违反即整组拒绝）。
6. **`temporal-hard` 负样本**必须额外满足：语义相关（与 query 的 embedding 相似度高于阈值）且**不在正确答案的时间窗口内**，**且其 `document` 文本含可判别的时间信号**（R7 probe 通过）——确定性规则判定 + 人工抽检集（≥50 条）复核。
7. **类别/数据源不一致**：`source=locomo` 的 category 必须 ∈ LoCoMo 四类；`source=msc` 的必须 ∈ msc-* 类。
8. **split 隔离**：`split=heldout` 的真实 conv ID（conv-26/30/41/42/43/44/47/48/49/50 中指定留出的）不得出现在训练集；source ID ↔ bench ordinal ↔ question ID 映射随 manifest 保存。
9. **同源过拟合防线**：GO 门全量配对含训练对话，污染必须标注；留出对话 + LME 500 是泛化否决门，不是可忽略诊断。

## 训练/验证划分（写入构建脚本参数）

- LoCoMo：`--train-convs`（默认对话 1–8）+ `--heldout-convs`（留出对话，泛化诊断）；GO 门评测用全量（008 同口径）。
- MSC：派生子集与 LoCoMo 混合训练；LongMemEval 500 **不进训练**，只作验证集（Clarifications Q3）。

## 版本

`schema_version: 1`。破坏性变更（字段增删/语义变化）bump 版本并附迁移说明（宪法 III 契约精神）。
