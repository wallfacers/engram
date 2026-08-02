# Data Model: 023 训练样本与目标标签

**Status**: active ｜ **Date**: 2026-08-02 ｜ **Spec**: [spec.md](spec.md) ｜ **Plan**: [plan.md](plan.md)

## 1. 训练样本 schema（Training Example）

每样本一行 JSONL（`training/planner/data/train.jsonl`），是训练与审计的最小单位（FR-007）：

```json
{
  "id": "023-b20260802-r1-000123",
  "query": "Alice 在 2024 年 5 月买了什么？",
  "query_date": "2026-01-15",
  "category": "temporal",
  "candidates": [
    {
      "id": "c0", "kind": "chunk", "rank": 0, "score": 0.8421,
      "text": "…原始证据文本（verbatim）…",
      "source_ids": ["s7"],
      "source_session_id": "sess-09", "event_date": "2024-05-12"
    }
  ],
  "sources": {
    "s7": {"session_id": "sess-09", "ordinal": 3, "content_digest": "sha256:…", "occurred_at": "2024-05-12T00:00:00Z"}
  },
  "target": {
    "need": {
      "entities": ["Alice"], "time_constraints": ["2024-05"], "operands": [],
      "list_cardinality": {"known": false, "count": 0}, "update_state": ""
    },
    "actions": [
      {"kind": "KEEP", "candidate_id": "c0", "source_id": "s7"}
    ]
  },
  "data_source": "synthetic",
  "license": "cc-by-4.0-synthetic",
  "split": "train",
  "build_version": "023-b20260802-r1",
  "content_digest": "sha256:…"
}
```

### 字段纪律
- `candidates` 是**冻结的检索输出**（id/rank/text/source_ids 与 engram 检索逐字节一致），训练/推理不得改动（FR-017）。
- `sources` 是 source lineage（session/ordinal/digest），保证 proposal 的 source/span 可复原到 lineage（FR-007/008）。
- `target.actions` 只能是冻结 action union：`KEEP` / `EXTRACT` / `DROP` / `MERGE` / `FETCH_SOURCE`（FR-016）。
- `content_digest` 覆盖整行规范化 JSON，用于确定性重建与污染审计（FR-010）。

## 2. 目标 proposal 标签（target Need/actions）

### 2.1 Need 约束（确定性构建，不依赖 LLM judge）

由 query 解析 + 规则构建，**复用 022 need builder 的语义**（entity/time/operand/cardinality/update，不依赖 benchmark category）：
- **Entities**：query 中的专有实体（人物/项目/组织名）
- **TimeConstraints**：query 明示的时间表达式（`2024-05`、`last month` 归一为可解析形式）
- **Operands**：比较/计数操作数（`count`、`older than`…）
- **ListCardinality**：query 是否要求计数/列举（known + count）
- **UpdateState**：query 是否询问最新/当前状态（`now`/`latest`）

任何 query 明示的约束 MUST 保留在 Need 中；planner 不得删除（spec Edge Case）。

### 2.2 Actions（oracle + 规则确定"answer 必需的最小证据集"）

- **Step 1 — 充分性覆盖**：用 gold answer 的 source spans（`fixed-gold oracle` 产物）确定哪些 candidate 的 source 覆盖答案必需信息 → 这些 candidate 是**必需的**。
- **Step 2 — 约束过滤**：必需 candidate 若不含 query 明示实体/时间 → 标记不足（该样本记 `gap`，仍可作负样本，见 §3）。
- **Step 3 — cap 内优先级**：对齐 022 编译器语义——原文装得下 → `KEEP`/`FETCH_SOURCE` 保留原始 span；装不下 → 按 relevance 排序 `EXTRACT`；仍不够才 `MERGE`（逐句绑 source）。**cap 用 022 B1 的 5000 token 口径**。
- **Step 4 — 双独立标签 + 独立裁决**：两个独立 labeler 对 Need 约束、必要 source spans、action 选择不一致 → 独立裁决唯一，否则排除（FR-009）。
- **Step 5 — 人审**：≥200 分层随机样本（不足 200 全量），语义充分率 ≥95%、95% CI 下界 ≥90%（FR-009）。

### 2.3 负样本与 gap

- 候选不含必要信息（oracle 判证据不足）→ 样本 `target.actions=[]` + `need.gap` 标记，训练模型学会**不越权**（FR-016 fail-closed 行为）。
- 这类样本计入全量，但**不**进入 compiler-eligible cohort 的涨点核算（那由 T003 冻结）。

## 3. 生成流程（pipeline）

```
[Python] data_build.py 合成对话生成（本地 Qwen2.5-7B-Instruct，OpenAI 兼容端点）
   ↓ 虚构多会话记忆对话（人物/项目/时间线/更新/跨会话引用）
[Go]    planner-build 工具：灌入 engram（提取+索引，离线）→ 生成 query → 检索冻结候选
   ↓ 每 query 的冻结 candidates + source lineage + fixed-gold oracle 覆盖
[Python] label.py：Need 解析 + Actions 规则/oracle 标签 → 双标签 + 裁决 → 人审导出
   ↓
[审计]  provenance/许可/污染/近重复/privacy（FR-014）→ 确定性重建验证（FR-010）
```

- **Go/Python 分工**：合成对话生成与训练在 Python；**engram 索引/检索在 Go**（`cmd/planner-build/`，复用 engine 公开 API + locomo-bench 的 store 构建逻辑，engine 零改动）。`data_build.py` 通过约定的 JSONL 接口与 Go 工具交接（对话进、候选出）。
- **污染红线**：LoCoMo/LongMemEval test conversations/questions/answers/judge 输出、任何 namespace/用户数据、付费 teacher 输出 **零进入**（FR-011/013）。

## 4. 数据源与 split

| 源 | 许可 | 用途 | 处理 |
|---|---|---|---|
| synthetic（本地 Qwen 生成） | 自有（cc-by-4.0-synthetic 声明） | 主路径：受控分布、零泄漏 | 虚构记忆对话 → engram 自举 |
| OASST1 | Apache-2.0 | 辅路径：真实对话多样性 | 按对话树改造 → 同 pipeline |
| ultrachat_200k | MIT | 辅路径：真实对话多样性 | 同上 |

- **split 隔离**：以来源 conversation（或更大近重复组）为单位，同组不得跨 train/validation/独立 evaluation（FR-012）；近重复组跨 split → 整组归单一 split 或拒绝该构建。

## 5. 冻结与版本

- 每个训练阶段（prompt-only / supervised）冻结：数据摘要（行数/源分布/split 计数）、底模与 tokenizer 摘要、训练 config、随机性、输出摘要、完成状态（FR-015）。
- 训练数据/recipe/Planner 产物变更**分开归档**，任何分数变化可定位到单一阶段（FR-033）。
- 本 schema 与 `local_planner.go` 的 snake_case wire format **对齐**（`need`/`actions`/`entities`/`time_constraints`/`operands`/`list_cardinality`/`update_state`/`gap`；`kind`/`candidate_id`/`source_id`/`span`/`sentences`/`reason_code`）——训练输出可直接进 adapter 解析，无二次转换。
