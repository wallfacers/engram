# LongMemEval 子集先行 · 跨 benchmark 复现 coverage≠answer

**日期**: 2026-07-26 · **状态**: 设计已确认（brainstorming 逐段确认）· **性质**: adapter-only 增量 + 评测设计

## 1. 为什么做这件事

`docs/paper-outline-eval-reliability.md` 的 **RQ6「结论能否从 LoCoMo 外推到
LongMemEval」** 当前状态是「engram 一手结果为空」，并带一条硬约束：

> LoCoMo 与 LongMemEval 至少共享一条可复现结论，否则标题和外推范围收窄到 LoCoMo。

同时 2026-07-25 一天之内在 LoCoMo 上零成本连续判死六个方向（015 桥接 / chunk 实体
索引 / 抽取覆盖 / H1 位置 / H2 冗余 / A embedding 模型升级，见
[`locomo-score-levers.md`](../../locomo-score-levers.md)），结论是 **engram 在当前
LoCoMo harness 下已接近架构上限**，继续刷分边际回报极低。

因此本特性的目标是**换战场做方法学复现，不是追分**：在第二个 benchmark 上验证
「coverage≠answer」与「检索侧 / 答题侧分账」是否同向成立。

**证伪同样有价值**：若结论不成立，说明 engram 的 LoCoMo 结论是 benchmark-specific，
战略应转回检索侧。设计中预先写死判据，就是为了防止事后为了「复现成功」而调判据。

## 2. 范围

**数据集**：`xiaowu0162/longmemeval-cleaned`（HF）。取两份：

| 文件 | 题数 | haystack 规模（中位） | 用途 |
|---|---:|---|---|
| `longmemeval_oracle.json` | 500 | 2 session / 23 消息（均 21.9） | 完美证据上限臂 |
| `longmemeval_s_cleaned.json` | 500 | 48 session / 491 消息（均 493.5） | 真实检索臂（抽样 100） |

已下载到 `testdata/longmemeval/`，并已加入 `.gitignore`（commit `1fc86f8`）；
`sample.json` 作为手写 schema 夹具保持入库。

**版本口径（必须在报告中明示）**：官方已**废弃**原始 `xiaowu0162/longmemeval`，
替代为 `longmemeval-cleaned`，废弃理由是「移除了会干扰答案正确性的噪声历史 session」。
竞品发表的 LongMemEval 数字（Mem0 94.4、MemOS 89.20）多半基于旧版，**不可与本次
数字直接对比**。这条版本差异本身是评测可靠性的一手素材，应记录而非回避。

报告一律写 **LongMemEval-S (cleaned)** 与 **LongMemEval-oracle**，并写明子集规模，
**不得简称全量**（论文纪律 X-LME）。

**判分口径**：本仓 mem0-aligned judge。理由：本次要比的是**我们自己的结论跨
benchmark 是否成立**，judge 必须与所有既有 LoCoMo 判决同源。官方 per-question-type
judge 列为未来工作，本次不做。

**新基线**：宪法 IV —— LongMemEval 是**独立新基线**，不替代也不覆盖 LoCoMo 85.71%，
首跑即声明。

**不做**：官方 judge、abstention 子集（cleaned 版不含该题型）、`longmemeval_m`
（2.7GB）、LongMemEval-V2、以及 §5 的 oracle-injection 干预臂（由本设计结果触发）。

## 3. adapter 增量（引擎零改动）

全部改动在 `cmd/locomo-bench/`。`git diff --name-only -- memory embedding provider
store internal` 必须为空。

实跑现有 loader 对真实数据验出四处缺口：

| 缺口 | 现状 | 补法 |
|---|---|---|
| **G1 题型名** | 数据集用 `single-session-preference`，loader 里是 `preference` ⇒ 硬报错（已实测复现） | `longMemEvalTypes` 补 `single-session-preference`；保留 `preference` 兼容旧版 |
| **G2 会话日期** | 每 session 的日期在独立的 `haystack_dates` 数组里，`longMemEvalRecord` **根本没有该字段**；数组形式 session 返回零值日期 ⇒ 全部回落到问题日期，**时间信息全丢** | 增 `haystack_dates` / `haystack_session_ids` 字段，按下标 zip 进 `session.Date`；**长度不匹配硬报错**，不静默降级 |
| **G3 证据尺子** | `has_answer` / `answer_session_ids` 未解析；`turn.DiaID` 未填 ⇒ `chunkTurns` 为空 ⇒ **精确轮次覆盖恒为 0**，没有尺子 | 给每个 turn 合成 `DiaID = "D<session>:<turn>"`；`has_answer:true` 的消息写进 `locomoQA.Evidence` |
| **G4 无证据题** | 21/500 题没有任何 `has_answer` 标记 | `Evidence` 为空 ⇒ 现有 `gradeable=false` 路径自然排除，**不做特殊处理** |

**G3 是承重件，也是最省的设计**：合成 DiaID 之后，LongMemEval 的证据与 LoCoMo
同形，`evidenceRecallAt` / `chunkTurns` / `buildAttributionTrace` 全部**原样复用**，
且「精确消息召回 == 精确轮次召回」，两个 benchmark 的覆盖数字直接可比 —— 这正是
复现结论所必需的。

**证据原料完整性（已实测）**：`has_answer` 覆盖 479/500 题（中位 2 条 gold 消息）；
`answer_session_ids` 覆盖 500/500 题。oracle 版 500/500 题的
`haystack_session_ids == answer_session_ids`，即**零干扰项**；S 版证据 session
仅占 haystack 的 **4.1%**（中位），约 24 倍稀释。

## 4. 实验设计

两臂共用 canonical 配方：
`--chunks --chunk-quota 12 --top-k 30 --force-answer --judge-mem0-aligned --retrieval hybrid`

| 臂 | 数据 | 规模 | 重复 |
|---|---|---|---|
| **ORACLE** | `longmemeval_oracle.json` | 全 500 题 | 1 |
| **S** | `longmemeval_s_cleaned.json` | 分层抽样 100 题 | 3 |

S 臂用 `repeats=3` 压方差：建店只做一次，答题走 box 近免费。

### 4.1 抽样

按 6 题型比例最大余数法，平局按题型总数降序：

| 题型 | 总数 | 配额 |
|---|---:|---:|
| multi-session | 133 | 27 |
| temporal-reasoning | 133 | 27 |
| knowledge-update | 78 | 15 |
| single-session-user | 70 | 14 |
| single-session-assistant | 56 | 11 |
| single-session-preference | 30 | 6 |
| **合计** | **500** | **100** |

固定随机种子，抽中的 `question_id` 列表写进 run 产物，可复现。

### 4.2 两道免费前置门（零 LLM 调用；不过不许往下走）

| 门 | 判据 | 不过意味着 |
|---|---|---|
| **G-尺子** | oracle 取 30 题 smoke，实测精确消息覆盖 **≥ 0.95**（`--coverage-only`） | loader 或证据尺子仍有 bug，后续所有数字不可信 |
| **G-向量** | 每次建店后断言 `count(memory_embeddings WHERE model = <当前模型>) == 应有行数` | 触发了 `Embedder.Backfill` 的有界队列丢弃（见下），语义信号静默部分失明 |

**G-向量 的由来**：`memory.Embedder.Backfill` 是有界的 —— 队列满即丢弃，靠下一次
Backfill 补（`memory/embedder.go:236-255` 注释明写）。2026-07-25 的 A 实验中，一趟
build 只回填了 2569/4892 行，**33% 语料对语义信号不可见且检索器按设计静默降级不
报错**，一度把候选模型成绩压低 6.4pp，差点被当成模型结论报出去。该断言必须固化为
硬门禁，不能靠肉眼。

### 4.3 分析

**主分析（观察性）**：S 臂逐题精确消息覆盖分桶（全覆盖 / 部分 / 零覆盖）→ 各桶
正确率 → 条件增益。与 LoCoMo 正本对照：

| LoCoMo 正本 | 值 |
|---|---:|
| 全覆盖正确率 | 90.0% |
| 零覆盖正确率 | 54.8% |
| **条件增益** | **35.2pp** |

**辅分析（上限锚）**：ORACLE 正确率 vs S 臂同题配对。**报告必须明写该对比含
「干扰项负荷」混杂** —— oracle 的 haystack 本身就小 24 倍，覆盖率与干扰项负荷在
本 benchmark 的设计中天然绑定，拆不开。因此该数字只作**上界**，不作因果。

### 4.4 判据（测前写死，事后不得放宽）

- **复现**：S 臂条件增益落在 **20–50pp**（LoCoMo 35.2pp ± 15pp），**且**检索侧当量
  （零覆盖题数 × 条件增益）**小于**答题侧当量（全覆盖仍答错的题数）——「答题侧是
  更大的块」这一结论同向成立。
- **证伪**：条件增益 **> 60pp**（覆盖几乎决定一切），**或**检索侧当量 > 答题侧当量。
- **落在两者之间**：报告为 **INCONCLUSIVE**，不许四舍五入到任一侧；此时才考虑
  §5 的干预臂。

**统计诚实**：100 题分桶后每桶可能仅二三十题，Wilson 区间会宽。配对比较用 McNemar。
**任一桶 n < 20 时，该桶结论标记为「不可判」而非硬报数字。**

## 5. 已知混杂与触发式后续（本次不做）

ORACLE 与 S 的差异 = 覆盖率 **+** 干扰项负荷，二者绑定。若要拆开，需要一个
**oracle-injection 干预臂**：在 S store 上把 gold 强行选进上下文，而 top-k 与
chunk-quota 与基线完全相同 ⇒ 覆盖升高、上下文规模不变，对标 LoCoMo US4 的
「+15.457pp coverage → −0.06pp answer」。

现有 `pcicOracleSelect` 已实现「从候选池贪心最大覆盖选择」，但被硬限制在
`--coverage-only`（`cmd/locomo-bench/main.go:261`：`oracle is allowed only with
--coverage-only`），这道护栏防止 oracle 分数被误报成真实成绩，**不该随手拆**。
且它只能从候选池里挑，gold 不在池里就抬不动，需要补逻辑。

因此干预臂**不在本次范围**，仅当 §4.4 判为 INCONCLUSIVE 时才触发，并且届时必须：
显式独立开关、强标注为诊断、**永不作为出货分数**。

## 6. 执行顺序与成本

| 阶段 | 内容 | 依赖 | 成本 |
|---|---|---|---|
| **P0** | adapter 增量 G1–G3 + 离线单测 | 无 | 零（无任何模型调用） |
| **P1** | **G-尺子门**：oracle 30 题 smoke 建店 + `--coverage-only` | 本地 GPU + 小额抽取口 | 约 658 条消息抽取 ≈ 0.11× LoCoMo |
| **P2** | ORACLE 全 500 建店 + 答题 | box 排队 | ≈ **1.86×** LoCoMo 建店 |
| **P3** | S 臂 100 题建店 + 答题 ×3 | box 排队 | ≈ **8.4×** LoCoMo 建店（大头） |
| **P4** | 分析 + 判决归档（`locomo-score-levers.md` 或新台账）+ HF 私仓归档 | 无 | 零 |

**P0 与 P1 不依赖远程评测机，可立即开始** —— 该机当前由另一 agent 跑 MemOS 复现，
P2/P3 需排队。这条写进设计是为了避免「等机器」成为阻塞。

**P1 的模型依赖**：embedding 走本地 GPU（2026-07-25 已验证本地服务栈可复现归档向量，
逐条余弦 p50 = 0.9999）；抽取需要一次小规模 LLM 调用，走小额付费口。**成本按脚本
实测 usage 计量并记录，不预先拍数。**

**成本基准（实测，非估计）**：

| 语料 | 规模 | 相对 LoCoMo |
|---|---:|---:|
| LoCoMo 全量（10 conv） | 272 session / **5,882 turn** | 1× |
| LongMemEval-oracle 全 500 | **10,960 条消息**（均 21.9/题） | 1.86× |
| LongMemEval-S 抽样 100 | **≈ 49,350 条消息**（均 493.5/题） | 8.4× |
| P1 smoke（oracle 30 题） | ≈ 658 条消息 | 0.11× |

## 7. 测试形态（TDD，全离线零模型调用）

现有 `cmd/locomo-bench/longmemeval_test.go` 用手写 `sample.json`，其 session 是
**对象形式**（`{"session_id","date","messages"}`），与真实数据集的**数组套数组 +
独立 `haystack_dates`** 形状不符 —— 测试一直是绿的，但断言的是现实中不存在的
schema。这正是 G2 长期未被发现的原因。

处置：保留 `sample.json`（覆盖对象形式分支），**新增一个手写的真实形状夹具**
（不从数据集拷内容，规避再分发问题）。先写失败测试再实现：

| 测试 | 断言 |
|---|---|
| 题型名 | `single-session-preference` 能加载（当前硬报错） |
| 日期 zip | 各 session 日期**互不相同**且等于 `haystack_dates` 对应项（当前全等于问题日期） |
| 日期长度不匹配 | **返回错误**，不静默回落 |
| DiaID 合成 | 形如 `D2:5`，session/turn 下标正确 |
| 证据提取 | `has_answer:true` 的消息进入 `Evidence`，其余不进 |
| 无证据题 | `Evidence` 为空 ⇒ 下游 `gradeable=false` |
| 覆盖尺子 | `evidenceRecallAt` 在合成证据上返回预期 turn/session recall |

回归门：`CGO_ENABLED=0 go build ./...` 零错误；`CGO_ENABLED=0 go test -count=1 ./...`
全绿。

## 8. 宪法核对

| 原则 | 判定 | 依据 |
|---|---|---|
| **I 本地优先，默认离线** | PASS | 仅 adapter 改动，引擎离线能力与降级语义不受影响；P0/P1 全本地 |
| **II 引擎与适配层分离** | PASS | 只动 `cmd/locomo-bench/`；`git diff --name-only -- memory embedding provider store internal` 必须为空 |
| **III 契约优先与命名空间隔离** | PASS | 新增 loader 字段，不改任何既有导出符号的签名或语义；无 schema 变更 |
| **IV 评测回归门禁** | PASS | 本次**不触碰**检索/抽取/策展/存储/嵌入 ⇒ LoCoMo 基线 invariant by construction，用零 diff + 引擎测试全绿证明，**不重跑 LoCoMo**；LongMemEval 首跑声明为独立新基线 |
| **V 优雅降级与规模诚实** | PASS | 报告明示 S (cleaned) / oracle 与子集规模，不简称全量；版本废弃事实如实记录 |

## 9. 交付物

1. `cmd/locomo-bench/` 的 G1–G3 增量 + 新夹具 + 单测
2. 分层抽样脚本与抽中的 `question_id` 列表（进 run 产物）
3. G-尺子门与 G-向量门的实测值
4. ORACLE / S 两臂的 coverage 与 accuracy，逐题产物
5. 一份判决：复现 / 证伪 / INCONCLUSIVE，写入台账并回填
   `paper-outline-eval-reliability.md` 的 RQ6 状态
