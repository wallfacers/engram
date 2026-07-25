# Phase 0 · Research：LongMemEval 子集先行

**日期**: 2026-07-26 · **上游事实源**: [设计文档](../../docs/superpowers/specs/2026-07-26-longmemeval-subset-design.md)

所有结论均以**实读代码或实测数据**为依据，不采信推断。

---

## R1 · 轮次标识合成格式必须严格匹配既有正则

**Decision**: 合成 `DiaID = fmt.Sprintf("D%d:%d", sessionIndex, turnIndex)`，
其中 `sessionIndex` 与 `turnIndex` 均**从 1 起**。

**Rationale**: `cmd/locomo-bench/evidence.go:12` 的
`evidenceReferencePattern = ^D(\d+):(\d+)$` 是**锚定的完全匹配**，任何前后缀
（如 `LME-D1:2`、`D1:2 `）都会被 `evidenceReferences` 静默丢弃，导致证据集合为空
而**不报错** —— 恰好复现本特性要修的那类静默失效。因此格式无自由度。

`parsedGoldTurns` 亦按 `D%d:%d` 重新格式化，故合成值与解析值必须逐字一致。

**Alternatives considered**: 自定义标识 + 为 LongMemEval 另写一套覆盖计量 —— 否决，
违背「下游零改动」这一核心省力点，且会产生两把不可比的尺子。

---

## R2 · 会话级召回无需额外改动，已自动成立

**Decision**: 不为会话级召回写任何新代码。

**Rationale**: 摄入侧 `chunks.go:185` 与 `main.go:1033` 均以
`fmt.Sprintf("conv%d-sess%d", conv.ID, s.Index)` 写入 `SourceSessionID`；
计量侧 `sourceSessionPattern = (?:^|-)sess(\d+)(?:-|$)` 从中取回会话号。
而 `parseLongMemEvalConversation` 已经在设置 `session{Index: index + 1}`。
黄金会话号则由 R1 合成的 DiaID 经 `evidenceSessions` 取出。两端同源，天然一致。

**含义**：数据集的 `answer_session_ids`（形如 `answer_4be1b6b4_2` 的字符串）
**不参与**召回计量，其价值是**数据自洽性校验**（见 R3）。

---

## R3 · 数据自洽性已实测确认，长度校验是护栏而非补丁

**实测结果**（两份数据集各 500 题）：

| 检查项 | oracle | S (cleaned) |
|---|---:|---:|
| 含 `has_answer` 的会话数 | 854 | 854 |
| 其中不在 `answer_session_ids` 内的 | **0** | **0** |
| `haystack_sessions` / `haystack_dates` / `haystack_session_ids` 三者长度不一致的题 | **0** | **0** |

**Decision**: FR-003 的「长度不匹配即报错」保留，定位为**防御性护栏**。

**Rationale**: 现有数据永不触发该分支，但静默回落正是 G2 缺陷的成因；把它变成硬错误
可保证未来换数据集版本时立刻暴露，而不是又一次悄悄塌缩。规格中如实标注它是护栏，
避免读者误以为在修一个已发生的故障。

---

## R4 · 分层抽样的载体：一次性脚本产出子集文件，**不**新增 bench 开关

**Decision**: 用一次性脚本从 `longmemeval_s_cleaned.json` 按配额抽出 100 题，
写出**子集 JSON 文件**与 `question_id` 清单；评测直接 `--data <子集文件>`。
脚本与两份产物归档到 HF 私仓，不入库。

**Rationale**:

1. 现有筛选能力不够用 —— `selectQuestions` 只支持 `--only-category`（单题型）与
   `--max-questions`（取前 N），`--max-convs` 对 LongMemEval 等价于「取前 N 题」，
   **都不能做按题型配额的分层抽样**。
2. 若新增 `--question-ids` 之类开关，等于为一次性评测扩大 bench 的命令行契约面，
   而该开关对 LoCoMo 无用。宪法 II 的精神是不为宿主特定需求膨胀通用表面。
3. 子集文件本身就是**最强的可复现产物** —— 比「种子 + 算法」更不易漂移，
   直接满足 FR-014。

**Alternatives considered**: 新增 bench 开关（否决，见 2）；在 `selectQuestions`
内置分层逻辑（否决，会影响 LoCoMo 路径，属引擎外但仍是共用代码的行为变更）。

---

## R5 · 向量完整性门的载体：独立脚本，**不**塞进 bench

**Decision**: G-向量门实现为独立的只读检查脚本，作为 quickstart 中**每次建库后的
强制步骤**；bench 代码不变。

**Rationale**:

1. 若在 bench 内加硬断言，会改变**既有 LoCoMo 流程**的语义 —— `--retrieval fts`
   臂本就不配置 embedding client，向量行数本就为 0，硬断言会把一条合法路径变成错误。
2. 该断言是**运行纪律**（人的流程），不是 bench 的功能契约；把纪律固化成工具的
   失败模式，会在未来产生误报并诱导使用者去关掉它。
3. 独立脚本可同时用于 LoCoMo 与 LongMemEval 的任何一次建库，复用面更大。

**满足 FR-010 的方式**: quickstart 将其列为**门禁步骤**，未通过不得进入下一阶段；
门禁的执行证据（实测行数）随产物落盘。

**Alternatives considered**: bench 内 `--assert-vectors` 开关（否决，见 1/2 —— 
一个默认关闭的断言开关等于没有门禁，一个默认开启的会破坏 fts 臂）。

---

## R6 · 时间锚（`temporalNowForConversation`）本次不受影响

**Decision**: 不改时间锚逻辑，不启用任何时间机制。

**Rationale**: `temporalNowForConversation` 取**会话日期的最大值**作为 "now"，
而 LongMemEval 的真实 "now" 是 `question_date`。修好 G2 后
`max(session date) ≤ question_date`，两者不等。但该函数仅在
`--temporal-score` / `--temporal-hard-filter` 等时间机制开启时才影响检索，
而本特性的 canonical 配方
（`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned --retrieval hybrid`）
**不含任何时间机制**，故不受影响。

**记录为已知边界**：若未来在 LongMemEval 上启用时间机制，必须先裁定
「now 用 `question_date` 还是最大会话日期」，否则 temporal-reasoning 题型的结论不可信。

---

## R7 · 每题一库的规模与命名

**Decision**: 沿用既有「一 conversation 一 store」结构，不做改动。

**Rationale**: `loadBenchmarkDataset` 已将每道 LongMemEval 题映射为一个
`conversation`（`item.Conversation.ID = i`），建库路径按 `conv%d.db` 落盘，
天然一题一库。chunk 命名 `chunk-c%d-s%d-%03d` 含 conv 序号，且各库物理隔离，
无冲突。500 个 SQLite 文件对本机与评测机均无压力。

**含义**：`--store-dir` 下会出现 500 个库文件（ORACLE 臂）与 100 个（S 臂），
需分目录存放以免两臂互相覆盖 —— 已写入 quickstart。

---

## R8 · 题型到类别号的映射：复用 id 12，并同步渲染标签

**先厘清机制（实读代码，初稿在此处曾写错）**：报告分组有**两套并存**的口径 ——

| 侧 | 函数 | 分组键 | 渲染 |
|---|---|---|---|
| 答题侧 | `aggregator.add`（`main.go:1801`） | **类别号 int** | `categoryLabel(int)` |
| 覆盖侧 | `coverageAccumulator.add`（`coverage.go:182`） | **类别名 string** | 原样 |

`categoryLabel` 对未登记的号返回 `"category-13"` 这类无名标签
（`default: "category-" + strconv.Itoa(c)`）。

**Decision**:

1. 向 `longMemEvalTypes` **追加** `{12, "single-session-preference"}`，
   即**复用 id 12**，与既有 `{12, "preference"}` 并列为同义名；
2. 将 `categoryLabel(12)` 的返回值由 `"preference"` 改为
   `"single-session-preference"`。

**Rationale**:

- 复用 12 而非新开 13：新开 13 会让答题侧报告出现无名桶 `category-13`，
  且 `--only-category` / `--cat-top-k` 这类按号定位的开关会与语义同一的
  `preference` 分裂成两个目标。
- 同步改标签：不改的话，同一次运行里答题侧报告写 `preference`、覆盖侧报告写
  `single-session-preference`，两份产物对同一批题用不同名字，事后对账极易出错。
  该标签仅服务于 LongMemEval 题型（LoCoMo 用 1–5），改动无外溢；既有测试断言的是
  类别**名字符串**而非标签，不受影响。

`abstention`(11) 在 cleaned 数据集中不出现，保留条目不产生任何行为。

**Alternatives considered**: 新开 id 13（否决，见上）；保留标签不改（否决 ——
留下一处跨产物命名不一致，属于给未来的自己埋坑）。

---

## R9 · 现有测试断言的是不存在的 schema

**Decision**: 保留 `testdata/longmemeval/sample.json` 与既有测试（覆盖**对象形式**
会话分支），**新增**一份手写的真实形状夹具（数组套数组 + 独立 `haystack_dates`
+ `has_answer`），不从数据集拷贝内容。

**Rationale**: `longmemeval_test.go` 一直是绿的，但它加载的 `sample.json` 用
`{"session_id","date","messages"}` 对象形式，与真实数据集的形状不符 —— 这正是
G2 长期未被发现的原因。手写新夹具而非切数据集，规避再分发问题，也让边界条件
（长度不匹配、无 `has_answer`）可以被刻意构造。

`parseLongMemEvalSession` 的对象分支仍被真实存在的其他 LongMemEval 变体使用，
不应删除。

---

## R10 · `has_answer` 字段当前完全未被解析

**Decision**: `longMemEvalMessage` 增 `HasAnswer bool \`json:"has_answer"\`` 字段；
`longMemEvalRecord` 增 `HaystackDates []string` 与 `HaystackSessionIDs []string`。

**Rationale**: 实读 `cmd/locomo-bench/longmemeval.go:30-41`，`longMemEvalMessage`
仅有 Role/Speaker/Content/Text/Date，`longMemEvalRecord` 仅有
QuestionID/Question/Answer/QuestionType/QuestionDate/HaystackSessions。
三个字段均**不存在**，是纯新增，不改任何既有字段的语义。

---

## 未解决项

无。全部裁定均有代码或实测数据支撑。
