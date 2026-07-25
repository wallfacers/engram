# Phase 1 · Data Model：LongMemEval 子集先行

**日期**: 2026-07-26

本特性**不引入任何持久化 schema 变更**（无 migration，不动 `store/`）。此处的
「数据模型」指评测工具内部的**解析结构**与**产物结构**。

---

## 1. 数据集输入结构（外部契约，只读）

`longmemeval_oracle.json` / `longmemeval_s_cleaned.json` 均为题目数组，每题：

```jsonc
{
  "question_id":   "8a2466db",
  "question_type": "temporal-reasoning",      // 6 种之一
  "question":      "...",
  "answer":        "...",
  "question_date": "2023/04/10 (Mon) 23:07",
  "haystack_sessions":    [ [ {msg}, {msg} ], [ {msg} ] ],   // 数组套数组
  "haystack_dates":       [ "2023/04/10 (Mon) 17:50", ... ], // 与 sessions 等长
  "haystack_session_ids": [ "answer_4be1b6b4_2", ... ],      // 与 sessions 等长
  "answer_session_ids":   [ "answer_4be1b6b4_2" ]            // 证据会话子集
}
```

消息：

```jsonc
{ "role": "user" | "assistant", "content": "...", "has_answer": true }
```

**实测不变式**（500 题全量核实，见 research R3）：

- `len(haystack_sessions) == len(haystack_dates) == len(haystack_session_ids)`
- 含 `has_answer` 的会话，其 id ∈ `answer_session_ids`
- oracle 版：`haystack_session_ids == answer_session_ids`（零干扰项）

---

## 2. 解析结构的增量

### 2.1 `longMemEvalRecord`（新增两字段）

| 字段 | JSON 键 | 类型 | 说明 |
|---|---|---|---|
| `HaystackDates` | `haystack_dates` | `[]string` | 逐会话日期，与 `HaystackSessions` **按下标对应** |
| `HaystackSessionIDs` | `haystack_session_ids` | `[]string` | 逐会话原始标识，仅用于自洽性校验与产物追溯 |

既有字段（`QuestionID` / `Question` / `Answer` / `QuestionType` / `QuestionDate` /
`HaystackSessions`）**签名与语义不变**。

### 2.2 `longMemEvalMessage`（新增一字段）

| 字段 | JSON 键 | 类型 | 说明 |
|---|---|---|---|
| `HasAnswer` | `has_answer` | `bool` | 该消息是否为答案证据 |

### 2.3 `longMemEvalTypes`（追加一条）

追加 `{12, "single-session-preference"}`，**复用** id 12（详见 research R8）。
同时 `categoryLabel(12)` 的返回值改为 `"single-session-preference"`。

---

## 3. 转换规则（`parseLongMemEvalConversation`）

输入一题的 `haystack_sessions` + `haystack_dates`，输出一个 `conversation`。

### 3.1 会话日期绑定

```
若 len(dates) != len(sessions):
    返回错误（FR-003，硬失败，不回落）
session[i].Date = parseLoCoMoDate(dates[i])
session[i].Index = i + 1        // 1-based，已是既有行为
```

**注意**：数组形式的会话不含内嵌日期，此前 `parseLongMemEvalSession` 对该分支返回
零值时间，导致全部回落到 `question_date`。对象形式分支（内嵌 `date`）保留原行为，
其内嵌日期**优先于**外部数组（对象形式本就自带日期，外部数组不适用）。

### 3.2 轮次标识合成

对第 `i` 个会话（0-based）的第 `j` 条消息（0-based）：

```
DiaID = "D" + (i+1) + ":" + (j+1)
```

必须严格匹配 `^D(\d+):(\d+)$`（research R1）。**被跳过的空消息不占用序号** ——
序号按**实际写入 `s.Turns` 的顺序**递增，以保证 `Evidence` 与 `Turns` 一一对应。

### 3.3 黄金证据抽取

```
Evidence = [ 该消息的 DiaID | 消息.HasAnswer == true ]
```

顺序 = 会话序、会话内消息序。无 `has_answer` 的题 ⇒ `Evidence` 为空切片
⇒ 下游 `evidenceRecallAt` 返回 `gradeable=false`（FR-007，既有路径）。

### 3.4 与既有实体的对接

| 既有实体 | 取值 | 由谁保证 |
|---|---|---|
| `locomoQA.Evidence` | §3.3 的 DiaID 列表 | 本次新增 |
| `locomoQA.Category` | `longMemEvalCategoryID(题型)` | 既有 |
| `locomoQA.CategoryName` | 题型字符串 | 既有 |
| `turn.DiaID` | §3.2 | 本次新增 |
| `session.Index` | `i+1` | 既有 |
| `SourceSessionID` | `conv<题序>-sess<Index>` | 既有（`chunks.go:185`） |

⇒ 会话级召回经 `sourceSessionPattern` 自动成立（research R2），无需新代码。

---

## 4. 产物结构

### 4.1 分层抽样产物（US3）

| 文件 | 内容 |
|---|---|
| `longmemeval_s_subset100.json` | 抽出的 100 题，格式与源文件完全一致，可直接 `--data` |
| `subset100_question_ids.json` | `{"seed":…, "quota":{题型:配额}, "question_ids":[…]}` |

两者均 **gitignore**，归档至 HF 私仓。子集文件本身即可复现性凭证（FR-014）。

### 4.2 向量完整性门产物（US2/US3）

```jsonc
{ "store_dir": "...", "model": "...", "stores": [
    {"db": "conv0.db", "entries": 41, "vectors": 41, "missing": 0}, … ],
  "total_missing": 0, "pass": true }
```

`total_missing > 0` ⇒ `pass: false` ⇒ 禁止进入下一阶段（FR-010）。

### 4.3 分账产物（US3）

```jsonc
{ "arm": "S-subset100", "repeats": 3,
  "buckets": [ {"name":"full","n":…, "correct":…, "accuracy":…, "wilson":[lo,hi], "judgeable": true}, … ],
  "conditional_gain_pp": …,
  "retrieval_side_equiv": …, "answer_side_equiv": …,
  "criterion": "复现|证伪|无法判定",
  "criterion_text_sha": "…"   // 与测量前登记的判据原文比对（SC-008）
}
```

`judgeable: false` 当且仅当该桶 `n < 20`（FR-018）。

---

## 5. 状态流转（门禁）

```
读入数据集 ──长度校验失败──► 硬错误，终止
     │
   成功
     ▼
G-尺子门（oracle 30 题，零 LLM）──覆盖 < 0.95──► 停止，本特性作废
     │
   ≥0.95
     ▼
建库 ──► G-向量门 ──missing > 0──► 停止，补齐后重测
     │
   pass
     ▼
答题 + 判分 ──► 分桶分账 ──► 对照登记判据 ──► 复现 / 证伪 / 无法判定
```
