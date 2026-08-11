# Data Model: Memory-Specific Reranker Training

**Date**: 2026-08-11 | **Spec**: [spec.md](spec.md) | **Research**: [research.md](research.md)

训练数据、训练配置、产物与评测报告的实体定义。schema 真相在 [contracts/training-data-schema.md](contracts/training-data-schema.md)（训练 JSONL 逐字段契约）。

---

## Entity: 重排训练样本（RerankTrainingSample）

每个样本是一个 (query, document, label, 元数据) 四元组，由 `tools/build_training_data.py` 确定性派生（可审计、可复现）。

| 字段 | 类型 | 说明 | 派生规则 |
|---|---|---|---|
| `sample_id` | string | 全局唯一样本 ID（可审计回源） | `{source}-{conv_id}-{qa_index}-{n}` |
| `schema_version` | int | 数据 schema 版本（**行内字段**，非仅说明文字） | 固定 1（review F） |
| `qa_id` | string | 源 question 标识（group-aware） | LoCoMo `{conv_id}-q{index}`；MSC 派生 |
| `query_group_id` | string | 同源 question 分组 key（multi-positive） | = qa_id（review C3） |
| `query` | string | 检索查询（记忆回忆问题） | LoCoMo: `qa.question`；MSC: persona/跨会话派生 query |
| `document` | string | 待排序的候选记忆片段 | LoCoMo: evidence 定位 turn；MSC: 对话 span |
| `document_kind` | enum | `fact` / `chunk` / `observation` | 与 runtime 候选同源序列化（review E） |
| `candidate_source` | string | 候选来源（baseline top-pool / evidence 定位） | 从真实 baseline top-pool 导出（review E） |
| `label` | float [0,1] | 相关度监督标签 | LoCoMo positive=1.0 / negative=0.0；分级可选 |
| `is_positive` | bool | 正/负样本标记 | 由 evidence 定位判定 |
| `positives` | string[] | 同一 question 的**全量正样本**（多 evidence） | qa.evidence 全部定位（review C3，实测 ~423/1986 多 evidence） |
| `category` | string | LoCoMo 四类 / MSC 派生类 | qa.category（1→single-hop, 2→temporal, 3→multi-hop, 4→open-domain）；MSC `msc-persona`/`msc-cross-session` |
| `temporal_label` | bool | 是否时序类（**仅在文本可见时间信号存在时**为真） | LoCoMo category==temporal **且** R7 probe 通过；MSC 跨会话引用须先过 R7（review C4 修正） |
| `negative_type` | string \| null | 负样本类型（`in-dialogue` / `temporal-hard` / `cross-session` / null） | 负采样协议（R3） |
| `split` | enum | `train` / `heldout` | 按真实 conv ID（conv-26/30/41/42/43/44/47/48/49/50）划分（review D2） |
| `source` | string | 数据源（`locomo` / `msc`） | 构建时写入 |
| `evidence_refs` | string[] \| null | 正样本的证据定位（LoCoMo turn/observation 引用） | 来自 qa.evidence / observation provenance |

**验证规则**：
- query/document 非空；label ∈ [0,1]；`schema_version` 行字段必须存在。
- positive 样本的 `evidence_refs` / `positives` 必须能定位到源对话（可审计）。
- **multi-positive**：同一 `query_group_id` 的所有正样本不得互为负样本；近重复与 evidence-overlap 候选排除出负池。
- 时序 hard negative 必须满足"语义相关但时间窗口错误"**且文本可见时间信号存在**（R7 probe 通过）；不满足的样本不得标 `temporal_label=true`。
- `split=heldout` 的对话（真实 conv ID）不出现在训练集；source ID ↔ bench ordinal ↔ question ID 映射保存为 manifest。

**状态流转**：无（训练数据是一次性构建的静态产物，不复用可变状态）。

---

## Entity: 训练配置（TrainingConfig）

训练复现性的唯一真相（记录进训练产物模型卡）。

| 字段 | 值 | 依据 |
|---|---|---|
| base_model | `Qwen/Qwen3-Reranker-0.6B`（Apache 2.0）+ 冻结 revision | R1 |
| adapter | LoRA r=16, alpha=32, target=attn+mlp | R2 |
| score_head | **冻结唯一实现**（`yes_logit−no_logit` 与新增 scalar head 二选一并记录）——训练↔合并↔vLLM 同一 score equation | review C2 |
| template | 冻结 `<Instruct>/<Query>/<Document>` 模板 + instruction 文本 + 截断顺序 | review C2/E |
| stage1_loss | BCE pointwise（sigmoid 回归 label） | R2（**待验证假设**，三 checkpoint 消融确认，review C1） |
| stage2_loss | InfoNCE listwise（in-batch negatives，multi-positive mask） | R2 |
| epochs | 3（stage1 ~2 + stage2 ~1，冻结） | R2 / 028 先例 |
| lr / scheduler | 2e-5 / cosine（**冻结唯一值**，运行 manifest 解析） | 028 先例（lr 2e-5, AdamW）；review F |
| max_len | 2048–4096（冻结） | R2 / R5（engram chunk 短） |
| precision | bf16 | R5 |
| batch | per-device 8–16 + grad accum（有效 batch 32–64） | R2 / MemReranker 配置缩比 |
| seed | 固定记录 | FR-003 可复现 |
| gpu | RTX 4090 24GB（单卡），200/1000 样本先实测峰值 VRAM + tok/s 再外推 | R5 / review C5 |
| budget_ceiling | ¥100 / 8 GPU·时，**机器可执行累计硬门**（跨 run 记账、超限自动停） | spec Clarifications Q2 / review D3 |

---

## Entity: 训练产物（RerankerModelArtifact）

| 字段 | 说明 |
|---|---|
| model_id | `engram/engram-memory-reranker-0.6b-v1`（或训练 run 标识） |
| adapter_path | LoRA adapter 权重路径（AutoDL 数据盘 + HF 备份可选） |
| merged_model | 与 base 合并后的模型（供 vllm serving） |
| model_card | 参数量/训练数据规模/许可/成本/端到端结果（对标 memos-reranker） |
| eval_report | 端到端配对报告引用（见下） |
| license | Apache 2.0（base）+ 训练数据许可核验 |

---

## Entity: 端到端配对报告（PairedEvalReport）

| 字段 | 说明 |
|---|---|
| protocol | 008 协议：US1 与 US2 **分别冻结运行**、跨 run 逐题配对（`--compare`），全量 1540 配对；污染标注（全量含训练对话） |
| overall | 总体正确数/率 + McNemar p + flip 计数（b/c）+ paired CI（预注册 non-inferiority margin） |
| by_category_paired | 四类别**逐题配对**（single/multi/temporal/open-domain）+ 各自 McNemar + flip |
| heldout_gate | 留出对话 + LongMemEval 500 **泛化否决门**（任一不过 → 不得宣称泛化） |
| vs_008 | 与 008 bge-reranker-v2-m3 结果的同口径对比 |
| hash_manifest | 模型 / 数据 / 模板 / 代码 hash（复现性） |
| rerank_telemetry | rerank 请求成功/失败计数（**零成功 → 评测标记 INVALID**） |
| retrieval_diag | MAP/NDCG/coverage 中间指标（只作诊断，不作 verdict） |

---

## 关系

```
TrainingConfig ──1:1──▶ 训练 run ──1:1──▶ RerankerModelArtifact
RerankTrainingSample ──n:1──▶ 训练 run
RerankerModelArtifact ──1:1──▶ PairedEvalReport（US2 GO 门判定）
```

## 规模假设（诚实声明）

- 训练样本量级：LoCoMo ~8k pointwise（~1.5k listwise tuple）+ MSC 派生子集（数千级，按需）。
- 训练时长：~1–3h 单卡（远小于 MemReranker 的 ~1M 样本 / 8×A800）。
- 评测：全量 1540 配对（单次 ~30–60min 推理，复用 remote-eval-box answerer）。
